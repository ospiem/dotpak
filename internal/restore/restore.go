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
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/ospiem/dotpak/internal/backup"
	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/crypto"
	"github.com/ospiem/dotpak/internal/metadata"
	"github.com/ospiem/dotpak/internal/osutils"
	"github.com/ospiem/dotpak/internal/output"
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

// Options holds restore options.
type Options struct {
	DryRun     bool
	Force      bool
	Categories []string
	NoBackup   bool
}

// Restore performs the restore operation.
type Restore struct {
	cfg     *config.Config
	opts    *Options
	out     *output.Output
	homeDir string
}

// New creates a new Restore instance.
// Returns nil if home directory cannot be determined.
func New(cfg *config.Config, opts *Options, out *output.Output) *Restore {
	home, err := osutils.HomeDir()
	if err != nil {
		out.Error("Cannot determine home directory: %v\n", err)
		return nil
	}
	return &Restore{
		cfg:     cfg,
		opts:    opts,
		out:     out,
		homeDir: home,
	}
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

// canEncrypt checks if encryption is properly configured.
func (r *Restore) canEncrypt() bool {
	if r.cfg.Backup.AgeRecipients != "" {
		if _, err := os.Stat(r.cfg.Backup.AgeRecipients); err == nil {
			return crypto.HasAge()
		}
	}
	if r.cfg.Backup.GPGRecipient != "" {
		return crypto.HasGPG()
	}
	return false
}

// promptForSensitiveBackup prompts the user for how to handle sensitive files in the safety backup
// when encryption is not available.
func (r *Restore) promptForSensitiveBackup(files []string) ([]string, error) {
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

// Run executes the restore from an archive.
func (r *Restore) Run(archivePath string) (*metadata.RestoreResult, error) {
	result := &metadata.RestoreResult{
		Success: false,
		Archive: archivePath,
		DryRun:  r != nil && r.opts.DryRun,
	}

	if r == nil {
		result.Error = "restore not initialized (home directory error)"
		return result, fmt.Errorf("%s", result.Error)
	}

	result.Categories = r.opts.Categories

	if _, err := os.Stat(archivePath); err != nil {
		result.Error = fmt.Sprintf("archive not found: %s", archivePath)
		return result, nil
	}

	tarPath := archivePath
	needsDecrypt := strings.HasSuffix(archivePath, ".age") || strings.HasSuffix(archivePath, ".gpg")

	if needsDecrypt {
		r.out.Print("Decrypting archive...\n")
		decrypted, err := r.decryptArchive(archivePath)
		if err != nil {
			result.Error = fmt.Sprintf("decryption failed: %v", err)
			return result, nil
		}
		tarPath = decrypted
		defer os.Remove(tarPath)
	}

	if !r.opts.NoBackup && !r.opts.DryRun {
		safetyPath, err := r.createSafetyBackup(tarPath, archivePath)
		if err != nil {
			r.out.Warning("Failed to create safety backup: %v\n", err)
		} else if safetyPath != "" {
			result.SafetyBackup = safetyPath
			r.out.Print("Created safety backup: %s\n", filepath.Base(safetyPath))
		}
	}

	if r.opts.DryRun {
		r.out.Print("\nDry run - would restore:\n")
	} else {
		r.out.Print("\nRestoring files...\n")
	}

	count, err := r.extractArchive(tarPath)
	if err != nil {
		result.Error = fmt.Sprintf("extraction failed: %v", err)
		return result, nil
	}

	result.Success = true

	if r.opts.DryRun {
		r.out.Print("\nWould restore %d files\n", count)
	} else {
		r.out.Success("\nRestored %d files\n", count)
	}

	return result, nil
}

func (r *Restore) decryptArchive(archivePath string) (string, error) {
	tmpFile, err := osutils.CreateTempFile("dotpak-decrypt-*.tar.gz")
	if err != nil {
		return "", err
	}
	_ = tmpFile.Close()
	outputPath := tmpFile.Name()

	if strings.HasSuffix(archivePath, ".age") {
		return decryptWithAge(archivePath, outputPath, resolveAgeIdentityFiles(r.cfg))
	}
	if strings.HasSuffix(archivePath, ".gpg") {
		return decryptWithGPG(archivePath, outputPath)
	}

	return "", errors.New("unknown encryption format")
}

func (r *Restore) createSafetyBackup(sourceArchive, originalArchive string) (string, error) {
	filesToBackup, err := r.findFilesToBackup(sourceArchive)
	if err != nil {
		return "", fmt.Errorf("scanning for files to backup: %w", err)
	}

	if len(filesToBackup) == 0 {
		r.out.Verbose("No existing files to backup\n")
		return "", nil
	}

	// check if safety backup contains sensitive files without encryption available
	if r.containsSensitiveFiles(filesToBackup) && !r.canEncrypt() {
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

	timestamp := time.Now().Format("20060102_150405")

	// encrypt safety backup if original archive was encrypted — stream directly
	method := crypto.DetectMethod(originalArchive)
	if method != crypto.MethodNone {
		enc, encErr := crypto.NewEncryptor(method, crypto.Options{
			AgeRecipientsFile: r.cfg.Backup.AgeRecipients,
			GPGRecipient:      r.cfg.Backup.GPGRecipient,
		})
		if encErr != nil {
			r.out.Warning("Failed to create encryptor for safety backup: %v\n", encErr)
			// fall through to unencrypted path below
		} else {
			encryptedPath := filepath.Join(preRestoreDir,
				fmt.Sprintf("pre-restore-%s.tar.gz.%s", timestamp, string(method)))

			pr, pw := io.Pipe()
			errCh := make(chan error, 1)
			go func() {
				errCh <- r.writeSafetyArchive(pw, filesToBackup)
				_ = pw.Close()
			}()

			if encErr = enc.EncryptReader(pr, encryptedPath); encErr != nil {
				_ = pr.Close() // unblock the writer goroutine
				if writeErr := <-errCh; writeErr != nil {
					r.out.Verbose("Safety archive write also failed: %v\n", writeErr)
				}
				_ = os.Remove(encryptedPath)
				r.out.Warning("Failed to encrypt safety backup: %v\n", encErr)
				// fall through to unencrypted path below
			} else if writeErr := <-errCh; writeErr != nil {
				_ = os.Remove(encryptedPath)
				return "", writeErr
			} else {
				return encryptedPath, nil
			}
		}
	}

	// unencrypted path
	archivePath := filepath.Join(preRestoreDir, fmt.Sprintf("pre-restore-%s.tar.gz", timestamp))

	outFile, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	defer outFile.Close()

	if err = r.writeSafetyArchive(outFile, filesToBackup); err != nil {
		return "", err
	}

	return archivePath, nil
}

// writeSafetyArchive writes a tar.gz stream of the given files to w.
func (r *Restore) writeSafetyArchive(w io.Writer, filesToBackup []string) (err error) {
	gzWriter := gzip.NewWriter(w)
	defer func() {
		if cerr := gzWriter.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	tarWriter := tar.NewWriter(gzWriter)
	defer func() {
		if cerr := tarWriter.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	for _, relPath := range filesToBackup {
		fullPath := filepath.Join(r.homeDir, relPath)
		if addErr := backup.AddFileToTar(tarWriter, fullPath, relPath); addErr != nil {
			r.out.Verbose("Failed to backup %s: %v\n", relPath, addErr)
			continue
		}
	}

	return nil
}

func (r *Restore) findFilesToBackup(sourceArchive string) ([]string, error) {
	file, err := os.Open(sourceArchive)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var filesToBackup []string

	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
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

func (r *Restore) extractArchive(tarPath string) (int, error) {
	file, err := os.Open(tarPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return 0, err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	count := 0
	var totalExtracted int64

	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return count, nextErr
		}

		if !isSafePath(header.Name) {
			r.out.Warning("Skipping unsafe path: %s\n", header.Name)
			continue
		}

		if len(r.opts.Categories) > 0 && !r.matchesCategory(header.Name) {
			continue
		}

		//nolint:gosec // g305: path validated by isSafePath() above and isPathWithinBase() below
		targetPath := filepath.Join(r.homeDir, header.Name)

		// defense-in-depth: verify resolved path is within home directory
		if !isPathWithinBase(targetPath, r.homeDir) {
			r.out.Warning("Skipping path that escapes home directory: %s\n", header.Name)
			continue
		}

		if r.opts.DryRun {
			r.out.Print("  %s\n", header.Name)
			count++
			continue
		}

		if totalExtracted+header.Size > osutils.MaxExtractTotalSize {
			return count, fmt.Errorf(
				"total extracted size exceeds limit of %s",
				osutils.FormatSize(osutils.MaxExtractTotalSize),
			)
		}

		if mkdirErr := os.MkdirAll(filepath.Dir(targetPath), 0755); mkdirErr != nil {
			r.out.Warning("Failed to create directory for %s: %v\n", header.Name, mkdirErr)
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			//nolint:gosec // g115: mode is masked to valid 9-bit permission range before conversion
			if mkdirErr := os.MkdirAll(targetPath, os.FileMode(header.Mode)&0o777); mkdirErr != nil {
				r.out.Warning("Failed to create directory %s: %v\n", header.Name, mkdirErr)
			}

		case tar.TypeReg:
			//nolint:gosec // g115: mode is masked to valid 9-bit permission range before conversion
			if extractErr := extractFile(
				tarReader,
				targetPath,
				os.FileMode(header.Mode)&0o777,
				osutils.MaxExtractFileSize,
			); extractErr != nil {
				r.out.Warning("Failed to extract %s: %v\n", header.Name, extractErr)
				continue
			}
			totalExtracted += header.Size
			count++

		case tar.TypeSymlink:
			if !isSafePath(header.Linkname) {
				r.out.Warning("Skipping symlink with unsafe target: %s -> %s\n", header.Name, header.Linkname)
				continue
			}
			// defense-in-depth: verify resolved symlink target is within home
			//nolint:gosec // g305: path validated by isPathWithinBase() immediately below
			resolvedTarget := filepath.Join(filepath.Dir(targetPath), header.Linkname)
			if !isPathWithinBase(resolvedTarget, r.homeDir) {
				r.out.Warning("Skipping symlink that escapes home: %s -> %s\n", header.Name, header.Linkname)
				continue
			}
			if rmErr := os.Remove(targetPath); rmErr != nil && !os.IsNotExist(rmErr) {
				r.out.Warning("Failed to remove existing file for symlink %s: %v\n", header.Name, rmErr)
			}
			if linkErr := os.Symlink(header.Linkname, targetPath); linkErr != nil {
				r.out.Warning("Failed to create symlink %s: %v\n", header.Name, linkErr)
			}
		}
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
		return true
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

// isPathWithinBase validates that targetPath is within baseDir after resolution.
// This provides defense-in-depth against path traversal attacks.
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

func extractFile(r io.Reader, path string, mode os.FileMode, maxSize int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()

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
func ListArchiveContents(cfg *config.Config, archivePath string, out *output.Output) error {
	tarPath := archivePath
	identityFiles := resolveAgeIdentityFiles(cfg)

	if strings.HasSuffix(archivePath, ".age") || strings.HasSuffix(archivePath, ".gpg") {
		tmpFile, err := osutils.CreateTempFile("dotpak-list-*.tar.gz")
		if err != nil {
			return err
		}
		_ = tmpFile.Close()
		defer os.Remove(tmpFile.Name())

		var decrypted string
		var decryptErr error

		if strings.HasSuffix(archivePath, ".age") {
			decrypted, decryptErr = decryptWithAge(archivePath, tmpFile.Name(), identityFiles)
		} else {
			decrypted, decryptErr = decryptWithGPG(archivePath, tmpFile.Name())
		}

		if decryptErr != nil {
			return decryptErr
		}
		tarPath = decrypted
		defer os.Remove(tarPath)
	}

	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	out.Print("Archive contents:\n\n")

	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nextErr
		}

		size := formatSize(header.Size)
		out.Print("  %-50s %10s\n", header.Name, size)
	}

	return nil
}

// fileContent holds file content for diff display.
type fileContent struct {
	name    string
	archive string // content from archive
}

// ShowDiff shows differences between archive and current files.
func ShowDiff(cfg *config.Config, archivePath string, verbose bool, out *output.Output) error {
	home, err := osutils.HomeDir()
	if err != nil {
		return err
	}
	tarPath := archivePath
	identityFiles := resolveAgeIdentityFiles(cfg)

	if strings.HasSuffix(archivePath, ".age") || strings.HasSuffix(archivePath, ".gpg") {
		tmpFile, tmpErr := osutils.CreateTempFile("dotpak-diff-*.tar.gz")
		if tmpErr != nil {
			return tmpErr
		}
		_ = tmpFile.Close()
		defer os.Remove(tmpFile.Name())

		var decrypted string
		var decryptErr error

		if strings.HasSuffix(archivePath, ".age") {
			decrypted, decryptErr = decryptWithAge(archivePath, tmpFile.Name(), identityFiles)
		} else {
			decrypted, decryptErr = decryptWithGPG(archivePath, tmpFile.Name())
		}

		if decryptErr != nil {
			return decryptErr
		}
		tarPath = decrypted
		defer os.Remove(tarPath)
	}

	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
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
		if header.Size < 10*1024*1024 { // limit to 10MB
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

func formatSize(size int64) string {
	return osutils.FormatSize(size)
}
