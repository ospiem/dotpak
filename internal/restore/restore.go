// Package restore implements the dotfiles restore functionality.
package restore

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/ospiem/dotpak/internal/archive"
	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/crypto"
	"github.com/ospiem/dotpak/internal/metadata"
	"github.com/ospiem/dotpak/internal/osutils"
	"github.com/ospiem/dotpak/internal/output"
)

// Extraction limits guard against decompression bombs.
const (
	maxExtractFileSize  = 1 << 30  // 1GB per file
	maxExtractTotalSize = 10 << 30 // 10GB total
)

// Categories maps category names to path prefixes.
var Categories = map[string][]string{
	"shell": {
		".zshrc",
		".bashrc",
		".profile",
		".zprofile",
		".bash_profile",
		".zshenv",
		".config/fish",
		".oh-my-zsh",
		".p10k.zsh",
	},
	"git":    {".gitconfig", ".gitignore_global", ".config/git"},
	"editor": {".vimrc", ".config/nvim", ".config/helix", ".config/zed", ".emacs", ".emacs.d", ".config/Code"},
	"ssh":    {".ssh/"},
	"gpg":    {".gnupg/"},
	"python": {".config/pip", ".config/ruff", ".config/mypy", ".jupyter", ".condarc"},
	"node":   {".npmrc", ".yarnrc", ".config/yarn", ".bunfig.toml"},
	"rust":   {".cargo/", ".rustup/settings.toml"},
	"go":     {".config/go/"},
	"cloud":  {".aws/", ".config/gcloud", ".azure/", ".s3cfg", ".yandex"},
	"docker": {".docker/config.json", ".config/podman"},
	"terminal": {
		".tmux.conf",
		".config/wezterm",
		".config/alacritty",
		".config/kitty",
		".config/starship.toml",
		".config/zellij",
	},
	"desktop": {"Library/Application Support", "Library/Preferences", ".local/share", ".config"},
	"ai":      {".claude", ".claude.json", ".codex", ".ai"},
}

// CategoryNames returns the sorted list of restore category names.
func CategoryNames() []string {
	names := make([]string, 0, len(Categories))
	for name := range Categories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validateCategories(categories []string) error {
	for _, cat := range categories {
		if _, ok := Categories[strings.ToLower(cat)]; !ok {
			return fmt.Errorf("unknown category %q (available: %s)", cat, strings.Join(CategoryNames(), ", "))
		}
	}
	return nil
}

// Options holds restore options.
type Options struct {
	DryRun      bool
	Force       bool
	Categories  []string
	NoBackup    bool
	AgeIdentity string
}

// Restore performs the restore operation.
type Restore struct {
	cfg     *config.Config
	opts    *Options
	out     *output.Output
	homeDir string
	// identityFiles are the age identities resolved once per run: a restore
	// decrypts the archive twice (safety-backup scan, then extraction) and a
	// stdin identity can only be read once.
	identityFiles []string
}

// New creates a new Restore instance.
func New(cfg *config.Config, opts *Options, out *output.Output) (*Restore, error) {
	home, err := osutils.HomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}
	return &Restore{
		cfg:     cfg,
		opts:    opts,
		out:     out,
		homeDir: home,
	}, nil
}

// sensitivePatterns are path prefixes that indicate sensitive files.
var sensitivePatterns = []string{
	".ssh", ".gnupg", ".aws", ".config/gcloud", ".azure",
	".kube", ".terraform", ".docker", ".pypirc",
}

// containsSensitiveFiles checks if any files match sensitive patterns.
func (r *Restore) containsSensitiveFiles(files []string) bool {
	for _, file := range files {
		for _, pattern := range sensitivePatterns {
			if strings.HasPrefix(file, pattern) {
				return true
			}
		}
	}
	return false
}

// configEncryptionMethod returns the encryption method usable with the current
// config and installed tools, or MethodNone.
func (r *Restore) configEncryptionMethod() crypto.Method {
	if r.cfg.Backup.AgeRecipients != "" && crypto.HasAge() {
		if _, err := os.Stat(r.cfg.Backup.AgeRecipients); err == nil {
			return crypto.MethodAge
		}
	}
	if r.cfg.Backup.GPGRecipient != "" && crypto.HasGPG() {
		return crypto.MethodGPG
	}
	return crypto.MethodNone
}

// promptForSensitiveBackup prompts the user for how to handle sensitive files in the safety backup
// when encryption is not available.
func (r *Restore) promptForSensitiveBackup(files []string) ([]string, error) {
	if r.out.Mode() != output.ModeNormal {
		// prompting would be invisible (and block) in quiet/JSON mode
		return nil, errors.New("safety backup contains sensitive files but no encryption is configured; " +
			"configure encryption, pass --no-backup, or run interactively to choose")
	}

	r.out.Warning("Safety backup contains sensitive files but no encryption is configured.\n")
	r.out.Print("Options:\n")
	r.out.Print("  1. Save without encryption\n")
	r.out.Print("  2. Skip sensitive files\n")
	r.out.Print("  3. Cancel restore\n")
	r.out.Print("\nChoice [1/2/3]: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return nil, errors.New("cancelled: no input received")
	}

	choice := strings.TrimSpace(scanner.Text())
	switch choice {
	case "1":
		r.out.Print("Proceeding with unencrypted safety backup...\n")
		return files, nil
	case "2":
		r.out.Print("Skipping sensitive files in safety backup...\n")
		return r.filterSensitiveFiles(files), nil
	case "3", "":
		return nil, errors.New("restore cancelled by user")
	default:
		return nil, fmt.Errorf("invalid choice: %s", choice)
	}
}

// filterSensitiveFiles removes sensitive files from the list.
func (r *Restore) filterSensitiveFiles(files []string) []string {
	var filtered []string
	for _, file := range files {
		isSensitive := false
		for _, pattern := range sensitivePatterns {
			if strings.HasPrefix(file, pattern) {
				isSensitive = true
				break
			}
		}
		if !isSensitive {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

// fail records err in the result for the JSON envelope and returns it as the
// authoritative error.
func fail(result *metadata.RestoreResult, err error) (*metadata.RestoreResult, error) {
	result.Error = err.Error()
	return result, err
}

// Run executes the restore from an archive. On failure the returned error is
// authoritative; result.Error carries the same message for the JSON envelope.
func (r *Restore) Run(archivePath string) (*metadata.RestoreResult, error) {
	result := &metadata.RestoreResult{
		Archive:    archivePath,
		DryRun:     r.opts.DryRun,
		Categories: r.opts.Categories,
	}

	if err := validateCategories(r.opts.Categories); err != nil {
		return fail(result, err)
	}

	if _, err := os.Stat(archivePath); err != nil {
		return fail(result, fmt.Errorf("archive not found: %s", archivePath))
	}

	identityFiles, cleanup, identityErr := resolveAgeIdentityFor(archivePath, r.opts.AgeIdentity, r.cfg)
	if identityErr != nil {
		return fail(result, identityErr)
	}
	defer cleanup()
	r.identityFiles = identityFiles

	if !r.opts.NoBackup && !r.opts.DryRun {
		safetyPath, err := r.createSafetyBackup(archivePath)
		if err != nil {
			return fail(result, fmt.Errorf("safety backup failed: %w (use --no-backup to restore without one)", err))
		}
		if safetyPath != "" {
			result.SafetyBackup = safetyPath
			r.out.Print("Created safety backup: %s\n", filepath.Base(safetyPath))
		}
	}

	if r.opts.DryRun {
		r.out.Print("\nDry run - would restore:\n")
	} else {
		r.out.Print("\nRestoring files...\n")
	}

	count, err := r.extractArchive(archivePath)
	if err != nil {
		return fail(result, fmt.Errorf("extraction failed: %w", err))
	}

	result.Success = true

	if r.opts.DryRun {
		r.out.Print("\nWould restore %d files\n", count)
	} else {
		r.out.Success("\nRestored %d files\n", count)
	}

	return result, nil
}

// createSafetyBackup archives existing files that the restore would overwrite.
// It fails hard instead of degrading: an unreadable file or a failed
// encryption aborts the restore, because the safety archive may hold the only
// copy of the data about to be overwritten.
func (r *Restore) createSafetyBackup(archivePath string) (string, error) {
	filesToBackup, err := r.findFilesToBackup(archivePath)
	if err != nil {
		return "", fmt.Errorf("scanning for files to backup: %w", err)
	}

	if len(filesToBackup) == 0 {
		r.out.Verbose("No existing files to backup\n")
		return "", nil
	}

	// encrypt if the source archive was encrypted; sensitive files also get
	// the config's encryption even when the source archive was plaintext
	method := crypto.DetectMethod(archivePath)
	sensitive := r.containsSensitiveFiles(filesToBackup)
	if method == crypto.MethodNone && sensitive {
		method = r.configEncryptionMethod()
	}

	if sensitive && method == crypto.MethodNone {
		filesToBackup, err = r.promptForSensitiveBackup(filesToBackup)
		if err != nil {
			return "", err
		}
		if len(filesToBackup) == 0 {
			r.out.Verbose("No files to backup after filtering\n")
			return "", nil
		}
	}

	preRestoreDir := filepath.Join(r.cfg.Backup.BackupDir, "pre-restore")
	if err = os.MkdirAll(preRestoreDir, 0700); err != nil {
		return "", err
	}

	timestamp := time.Now().Format(metadata.TimestampFormat)

	if method != crypto.MethodNone {
		enc, encErr := crypto.NewEncryptor(method, crypto.Options{
			AgeRecipientsFile: r.cfg.Backup.AgeRecipients,
			GPGRecipient:      r.cfg.Backup.GPGRecipient,
		})
		if encErr != nil {
			return "", fmt.Errorf("safety backup encryption unavailable: %w", encErr)
		}

		encryptedPath := filepath.Join(preRestoreDir,
			fmt.Sprintf("pre-restore-%s.tar.gz%s", timestamp, method.Extension()))
		if encErr = archive.EncryptStream(enc, encryptedPath, func(w io.Writer) error {
			return r.writeSafetyArchive(w, filesToBackup)
		}); encErr != nil {
			return "", fmt.Errorf("encrypting safety backup: %w", encErr)
		}
		return encryptedPath, nil
	}

	safetyPath := filepath.Join(preRestoreDir, fmt.Sprintf("pre-restore-%s.tar.gz", timestamp))

	outFile, err := os.OpenFile(safetyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	writeErr := r.writeSafetyArchive(outFile, filesToBackup)
	if closeErr := outFile.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(safetyPath)
		return "", writeErr
	}

	return safetyPath, nil
}

// writeSafetyArchive writes a tar.gz stream of the given files to w. Any file
// that cannot be read aborts the archive rather than being skipped silently:
// it may be the only copy of data the restore is about to overwrite.
func (r *Restore) writeSafetyArchive(w io.Writer, filesToBackup []string) error {
	return archive.WriteTarGz(w, func(tw *tar.Writer) error {
		for _, relPath := range filesToBackup {
			fullPath := filepath.Join(r.homeDir, relPath)
			if addErr := archive.AddFileToTar(tw, fullPath, relPath); addErr != nil {
				return fmt.Errorf("backing up %s: %w", relPath, addErr)
			}
		}
		return nil
	})
}

func (r *Restore) findFilesToBackup(archivePath string) ([]string, error) {
	rc, err := openArchiveStream(archivePath, r.identityFiles)
	if err != nil {
		return nil, err
	}

	files, scanErr := r.scanForExistingFiles(rc)
	if err = closeArchiveStream(rc, scanErr); err != nil {
		return nil, err
	}
	return files, nil
}

func (r *Restore) scanForExistingFiles(stream io.Reader) ([]string, error) {
	gzReader, err := gzip.NewReader(stream)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var filesToBackup []string

	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, nextErr
		}

		if header.Typeflag == tar.TypeDir || !isSafePath(header.Name) {
			continue
		}

		if len(r.opts.Categories) > 0 && !r.matchesCategory(header.Name) {
			continue
		}

		//nolint:gosec // g305: path validated by isSafePath() above
		targetPath := filepath.Join(r.homeDir, header.Name)
		if _, statErr := os.Stat(targetPath); statErr == nil {
			filesToBackup = append(filesToBackup, header.Name)
		}
	}

	return filesToBackup, nil
}

func (r *Restore) extractArchive(archivePath string) (int, error) {
	rc, err := openArchiveStream(archivePath, r.identityFiles)
	if err != nil {
		return 0, err
	}

	count, extractErr := r.extractStream(rc)
	return count, closeArchiveStream(rc, extractErr)
}

//nolint:gocognit // the extraction loop centralizes all per-entry safety checks
func (r *Restore) extractStream(stream io.Reader) (int, error) {
	gzReader, err := gzip.NewReader(stream)
	if err != nil {
		return 0, err
	}
	defer gzReader.Close()

	resolvedHome, err := filepath.EvalSymlinks(r.homeDir)
	if err != nil {
		return 0, fmt.Errorf("resolving home directory: %w", err)
	}

	tarReader := tar.NewReader(gzReader)
	count := 0
	failed := 0
	unsafeSkipped := 0
	var totalExtracted int64

	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return count, nextErr
		}

		if !isSafePath(header.Name) {
			r.out.Warning("Skipping unsafe path: %s\n", header.Name)
			unsafeSkipped++
			continue
		}

		if len(r.opts.Categories) > 0 && !r.matchesCategory(header.Name) {
			continue
		}

		//nolint:gosec // g305: path validated by isSafePath() above and ensureParentWithinHome() below
		targetPath := filepath.Join(r.homeDir, header.Name)

		// defense-in-depth: verify lexical path is within home directory
		if !isPathWithinBase(targetPath, r.homeDir) {
			r.out.Warning("Skipping path that escapes home directory: %s\n", header.Name)
			unsafeSkipped++
			continue
		}

		if r.opts.DryRun {
			r.out.Print("  %s\n", header.Name)
			count++
			continue
		}

		if totalExtracted+header.Size > maxExtractTotalSize {
			return count, fmt.Errorf(
				"total extracted size exceeds limit of %s",
				osutils.FormatSize(maxExtractTotalSize),
			)
		}

		if parentErr := ensureParentWithinHome(targetPath, resolvedHome); parentErr != nil {
			if errors.Is(parentErr, errEscapesHome) {
				r.out.Warning("Skipping %s: %v\n", header.Name, parentErr)
				unsafeSkipped++
			} else {
				r.out.Warning("Failed to create directory for %s: %v\n", header.Name, parentErr)
				failed++
			}
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			//nolint:gosec // g115: mode is masked to valid 9-bit permission range before conversion
			if mkdirErr := os.MkdirAll(targetPath, os.FileMode(header.Mode)&0o777); mkdirErr != nil {
				r.out.Warning("Failed to create directory %s: %v\n", header.Name, mkdirErr)
				failed++
			}

		case tar.TypeReg:
			//nolint:gosec // g115: mode is masked to valid 9-bit permission range before conversion
			if extractErr := extractFile(
				tarReader,
				targetPath,
				os.FileMode(header.Mode)&0o777,
				maxExtractFileSize,
			); extractErr != nil {
				r.out.Warning("Failed to extract %s: %v\n", header.Name, extractErr)
				failed++
				continue
			}
			if timeErr := os.Chtimes(targetPath, header.ModTime, header.ModTime); timeErr != nil {
				r.out.Verbose("Cannot restore mtime for %s: %v\n", header.Name, timeErr)
			}
			totalExtracted += header.Size
			count++

		case tar.TypeSymlink:
			if !isSafePath(header.Linkname) {
				r.out.Warning("Skipping symlink with unsafe target: %s -> %s\n", header.Name, header.Linkname)
				unsafeSkipped++
				continue
			}
			// defense-in-depth: verify resolved symlink target is within home
			//nolint:gosec // g305: path validated by isPathWithinBase() immediately below
			resolvedTarget := filepath.Join(filepath.Dir(targetPath), header.Linkname)
			if !isPathWithinBase(resolvedTarget, r.homeDir) {
				r.out.Warning("Skipping symlink that escapes home: %s -> %s\n", header.Name, header.Linkname)
				unsafeSkipped++
				continue
			}
			if rmErr := os.Remove(targetPath); rmErr != nil && !os.IsNotExist(rmErr) {
				r.out.Warning("Failed to remove existing file for symlink %s: %v\n", header.Name, rmErr)
				failed++
				continue
			}
			if linkErr := os.Symlink(header.Linkname, targetPath); linkErr != nil {
				r.out.Warning("Failed to create symlink %s: %v\n", header.Name, linkErr)
				failed++
				continue
			}
			count++

		default:
			r.out.Verbose("Skipping unsupported entry type %c: %s\n", header.Typeflag, header.Name)
		}
	}

	if unsafeSkipped > 0 {
		r.out.Warning("Skipped %d entries that would escape the home directory\n", unsafeSkipped)
	}
	if failed > 0 {
		return count, fmt.Errorf("failed to restore %d of %d files", failed, failed+count)
	}

	return count, nil
}

func (r *Restore) matchesCategory(path string) bool {
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")

	for _, cat := range r.opts.Categories {
		prefixes, ok := Categories[strings.ToLower(cat)]
		if !ok {
			continue
		}

		for _, prefix := range prefixes {
			prefix = strings.TrimPrefix(prefix, "./")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}

	return false
}

func isSafePath(path string) bool {
	if path == "" {
		return false
	}
	// check for null bytes (can be used to bypass string checks)
	if strings.ContainsRune(path, '\x00') {
		return false
	}
	if filepath.IsAbs(path) {
		return false
	}
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") {
		return false
	}
	cleaned := filepath.Clean(path)
	if strings.HasPrefix(cleaned, "..") {
		return false
	}
	if slices.Contains(strings.Split(path, "/"), "..") {
		return false
	}
	if slices.Contains(strings.Split(path, string(filepath.Separator)), "..") {
		return false
	}
	return true
}

// isPathWithinBase validates that targetPath is lexically within baseDir.
// This provides defense-in-depth against path traversal attacks; symlinked
// path components are handled separately by ensureParentWithinHome.
func isPathWithinBase(targetPath, baseDir string) bool {
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	// ensure the target path starts with base directory
	return strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) || absTarget == absBase
}

// errEscapesHome marks entries whose real (symlink-resolved) location falls
// outside the home directory.
var errEscapesHome = errors.New("path escapes home directory after resolving symlinks")

// ensureParentWithinHome creates the parent directory of targetPath (0700 —
// dotfiles are private by default, and restored ~/.ssh or ~/.gnupg must not be
// world-readable) and verifies that no symlinked component redirects the write
// outside the home directory.
func ensureParentWithinHome(targetPath, resolvedHome string) error {
	parent := filepath.Dir(targetPath)

	// resolve the deepest existing ancestor BEFORE creating anything, so
	// MkdirAll cannot create directories through an escaping symlink
	existing := parent
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		next := filepath.Dir(existing)
		if next == existing {
			break
		}
		existing = next
	}
	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	if !isPathWithinBase(resolvedExisting, resolvedHome) {
		return errEscapesHome
	}

	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}

	// re-resolve the full parent: components that already existed may
	// themselves be symlinks
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	if !isPathWithinBase(resolvedParent, resolvedHome) {
		return errEscapesHome
	}
	return nil
}

func extractFile(r io.Reader, path string, mode os.FileMode, maxSize int64) (err error) {
	// remove any existing file first: O_EXCL then guarantees the write never
	// follows a pre-existing symlink at the target, and re-creating applies
	// the archive's mode to previously existing files too
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		return rmErr
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// the OpenFile mode is masked by the umask; Chmod applies it exactly
	if err = file.Chmod(mode); err != nil {
		return err
	}

	limitedReader := io.LimitReader(r, maxSize)
	written, err := io.Copy(file, limitedReader)
	if err != nil {
		return err
	}

	if written == maxSize {
		buf := make([]byte, 1)
		if n, _ := r.Read(buf); n > 0 {
			return fmt.Errorf("file exceeds maximum size limit of %d bytes", maxSize)
		}
	}

	return nil
}

// ListArchiveContents lists the contents of an archive.
func ListArchiveContents(cfg *config.Config, archivePath, ageIdentity string, out *output.Output) error {
	identityFiles, cleanup, err := resolveAgeIdentityFor(archivePath, ageIdentity, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	rc, err := openArchiveStream(archivePath, identityFiles)
	if err != nil {
		return err
	}

	listErr := listContents(rc, out)
	return closeArchiveStream(rc, listErr)
}

func listContents(stream io.Reader, out *output.Output) error {
	gzReader, err := gzip.NewReader(stream)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	out.Print("Archive contents:\n\n")

	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}

		out.Print("  %-50s %10s\n", header.Name, osutils.FormatSize(header.Size))
	}

	return nil
}

// fileContent holds file content for diff display.
type fileContent struct {
	name    string
	archive string // content from archive
}

// maxDiffContentSize limits how much file content is read for comparison.
const maxDiffContentSize = 10 * 1024 * 1024

// ShowDiff shows differences between archive and current files.
func ShowDiff(cfg *config.Config, archivePath, ageIdentity string, verbose bool, out *output.Output) error {
	home, err := osutils.HomeDir()
	if err != nil {
		return err
	}
	identityFiles, cleanup, err := resolveAgeIdentityFor(archivePath, ageIdentity, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	rc, err := openArchiveStream(archivePath, identityFiles)
	if err != nil {
		return err
	}

	diffErr := showDiffStream(rc, home, verbose, out)
	return closeArchiveStream(rc, diffErr)
}

func showDiffStream(stream io.Reader, home string, verbose bool, out *output.Output) error {
	gzReader, err := gzip.NewReader(stream)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	var newFiles, unchangedFiles []string
	var modifiedFiles []fileContent

	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nextErr
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		//nolint:gosec // g305: path used only for stat comparison, no extraction
		currentPath := filepath.Join(home, header.Name)

		currentInfo, statErr := os.Stat(currentPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				newFiles = append(newFiles, header.Name)
			} else {
				// permission denied, broken symlink, etc. - treat as modified
				modifiedFiles = append(modifiedFiles, fileContent{name: header.Name})
			}
			continue
		}

		// read archive content to compare
		var archiveContent []byte
		if header.Size < maxDiffContentSize {
			archiveContent, _ = io.ReadAll(io.LimitReader(tarReader, header.Size))
		}

		// compare by size first, then by content
		isModified := currentInfo.Size() != header.Size
		if !isModified && len(archiveContent) > 0 {
			currentContent, readErr := os.ReadFile(currentPath)
			if readErr == nil {
				isModified = string(currentContent) != string(archiveContent)
			}
		}

		if isModified {
			fc := fileContent{name: header.Name}
			if verbose {
				fc.archive = string(archiveContent)
			}
			modifiedFiles = append(modifiedFiles, fc)
		} else {
			unchangedFiles = append(unchangedFiles, header.Name)
		}
	}

	diffOut := output.NewDiffOutput(out)

	if len(newFiles) > 0 {
		out.Print("\nNew files (%d):\n", len(newFiles))
		for _, f := range newFiles {
			diffOut.Added("  + " + f)
		}
	}

	if len(modifiedFiles) > 0 {
		out.Print("\nModified files (%d):\n", len(modifiedFiles))
		for _, fc := range modifiedFiles {
			diffOut.Header("  ~ " + fc.name)
			// show diff content if verbose and we have archive content
			if verbose && fc.archive != "" {
				showFileDiff(home, fc, out)
			}
		}
	}

	out.Print("\nSummary: %d new, %d modified, %d unchanged\n",
		len(newFiles), len(modifiedFiles), len(unchangedFiles))

	return nil
}

// maxDiffLines limits the number of diff lines shown per file.
const maxDiffLines = 20

// maxLineLength limits the length of each diff line.
const maxLineLength = 100

// showFileDiff displays the diff between archive and current file.
func showFileDiff(home string, fc fileContent, out *output.Output) {
	currentPath := filepath.Join(home, fc.name)
	currentContent, err := os.ReadFile(currentPath)
	if err != nil {
		return
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(string(currentContent), fc.archive, false)

	// count changes and collect diff lines
	var diffLines []struct {
		isDelete bool
		text     string
	}
	for _, d := range diffs {
		if d.Type == diffmatchpatch.DiffEqual {
			continue
		}
		lines := strings.Split(d.Text, "\n")
		for i, line := range lines {
			if line == "" && i == len(lines)-1 {
				continue
			}
			diffLines = append(diffLines, struct {
				isDelete bool
				text     string
			}{
				isDelete: d.Type == diffmatchpatch.DiffDelete,
				text:     line,
			})
		}
	}

	if len(diffLines) == 0 {
		return
	}

	// output diff lines with limit
	diffOut := output.NewDiffOutput(out)
	shown := 0
	for _, dl := range diffLines {
		if shown >= maxDiffLines {
			diffOut.Changed(fmt.Sprintf("    ... and %d more changes", len(diffLines)-shown))
			break
		}
		text := dl.text
		if len(text) > maxLineLength {
			text = text[:maxLineLength] + "..."
		}
		if dl.isDelete {
			diffOut.Removed("    - " + text)
		} else {
			diffOut.Added("    + " + text)
		}
		shown++
	}
}
