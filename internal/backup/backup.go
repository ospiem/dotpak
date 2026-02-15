// Package backup implements the dotfiles backup functionality.
package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/crypto"
	"github.com/ospiem/dotpak/internal/metadata"
	"github.com/ospiem/dotpak/internal/osutils"
	"github.com/ospiem/dotpak/internal/output"
)

// Options holds backup options.
type Options struct {
	DryRun           bool
	EncryptionMethod string // "age", "gpg", "none"
	IncludeSecrets   bool
	RecipientsFile   string
	GPGRecipient     string
	Estimate         bool
}

// Backup performs the backup operation.
type Backup struct {
	cfg     *config.Config
	opts    *Options
	out     *output.Output
	homeDir string
	stats   metadata.Stats
}

// New creates a new Backup instance.
// Returns nil if the home directory cannot be determined.
func New(cfg *config.Config, opts *Options, out *output.Output) *Backup {
	home, err := osutils.HomeDir()
	if err != nil {
		out.Error("Cannot determine home directory: %v\n", err)
		return nil
	}
	return &Backup{
		cfg:     cfg,
		opts:    opts,
		out:     out,
		homeDir: home,
	}
}

// Run executes the backup.
func (b *Backup) Run() (*metadata.BackupResult, error) {
	result := &metadata.BackupResult{
		Success: false,
	}

	if b == nil {
		result.Error = "backup not initialized (home directory error)"
		return result, errors.New(result.Error)
	}

	if err := os.MkdirAll(b.cfg.Backup.BackupDir, 0700); err != nil {
		errMsg := fmt.Sprintf("creating backup directory: %v", err)
		if os.IsPermission(err) && runtime.GOOS == "darwin" {
			execPath, _ := os.Executable()
			resolvedPath, _ := filepath.EvalSymlinks(execPath)
			if resolvedPath == "" {
				resolvedPath = execPath
			}
			errMsg += fmt.Sprintf(
				"\n\nFull Disk Access may be required. "+
					"Add to System Settings → Privacy & Security → Full Disk Access:\n  %s",
				resolvedPath,
			)
		}
		result.Error = errMsg
		return result, nil
	}

	encMethod, recipientsFile, gpgRecipient, err := b.resolveEncryption()
	if err != nil {
		result.Error = err.Error()
		//nolint:nilerr // error captured in result.Error for structured JSON response
		return result, nil
	}

	b.out.Print("Collecting files...\n")
	files := b.collectFiles(encMethod != "")

	if len(files) == 0 {
		result.Error = "no files to backup"
		return result, nil
	}

	b.out.Print("Found %d files to backup\n", len(files))

	if b.opts.Estimate {
		var totalSize int64
		for _, f := range files {
			totalSize += f.Size
		}
		b.out.Print("\nEstimate:\n")
		b.out.Print("  Files: %d\n", len(files))
		b.out.Print("  Size: %s\n", formatSize(totalSize))

		result.Success = true
		result.Stats = b.stats
		return result, nil
	}

	if b.opts.DryRun {
		b.out.Print("\nDry run - would backup:\n")
		for _, f := range files {
			b.out.Print("  %s\n", f.RelPath)
		}

		if encMethod != "" {
			b.out.Print("\nWould encrypt with: %s\n", encMethod)
		}

		result.Success = true
		result.Encrypted = encMethod != ""
		result.EncryptionMethod = encMethod
		result.Stats = b.stats
		return result, nil
	}

	timestamp := time.Now().Format("20060102_150405")
	archivePath := filepath.Join(b.cfg.Backup.BackupDir, fmt.Sprintf("dotfiles-%s.tar.gz", timestamp))

	var finalArchive string
	if encMethod != "" {
		b.out.Print("Creating encrypted archive with %s...\n", encMethod)

		enc, encErr := crypto.NewEncryptor(crypto.Method(encMethod), crypto.Options{
			AgeRecipientsFile: recipientsFile,
			GPGRecipient:      gpgRecipient,
		})
		if encErr != nil {
			result.Error = fmt.Sprintf("encryption failed: %v", encErr)
			return result, nil
		}

		encryptedPath := archivePath + "." + encMethod
		if encErr = b.createEncryptedArchive(encryptedPath, files, enc); encErr != nil {
			_ = os.Remove(encryptedPath)
			result.Error = fmt.Sprintf("creating encrypted archive: %v", encErr)
			return result, nil
		}
		finalArchive = encryptedPath
	} else {
		b.out.Print("Creating archive: %s\n", filepath.Base(archivePath))
		if err = b.createArchive(archivePath, files); err != nil {
			errMsg := fmt.Sprintf("creating archive: %v", err)
			if os.IsPermission(err) && runtime.GOOS == "darwin" {
				execPath, _ := os.Executable()
				resolvedPath, _ := filepath.EvalSymlinks(execPath)
				if resolvedPath == "" {
					resolvedPath = execPath
				}
				errMsg += fmt.Sprintf(
					"\n\nFull Disk Access may be required. "+
						"Add to System Settings → Privacy & Security → Full Disk Access:\n  %s",
					resolvedPath,
				)
			}
			result.Error = errMsg
			return result, nil
		}
		finalArchive = archivePath
	}

	meta := metadata.New()
	meta.Encrypted = encMethod != ""
	meta.EncryptionMethod = encMethod
	meta.OSVersion = metadata.GetOSVersion()
	meta.Stats = b.stats

	metadataPath := metadata.GetMetadataPath(finalArchive)
	if err = meta.Save(metadataPath); err != nil {
		b.out.Warning("Failed to save metadata: %v\n", err)
	}

	b.backupHomebrew()
	b.backupMASApps()
	b.backupAptPackages()
	b.backupGoPackages()
	b.cleanupOldBackups()

	result.Success = true
	result.Archive = finalArchive
	result.Encrypted = meta.Encrypted
	result.EncryptionMethod = meta.EncryptionMethod
	result.Stats = b.stats

	b.out.Success("\nBackup complete: %s\n", filepath.Base(finalArchive))
	b.out.Print("  Files: %d\n", b.stats.FilesBackedUp)
	b.out.Print("  Skipped: %d\n", b.stats.FilesSkipped)
	if b.stats.FilesExcluded > 0 {
		b.out.Print("  Excluded: %d\n", b.stats.FilesExcluded)
	}
	if b.stats.SensitiveFiles > 0 {
		b.out.Print("  Sensitive: %d\n", b.stats.SensitiveFiles)
	}

	return result, nil
}

func (b *Backup) resolveEncryption() (method, recipientsFile, gpgRecipient string, err error) {
	method = b.opts.EncryptionMethod

	if method == "" {
		method = b.cfg.Backup.Encryption
	}

	if method == "none" || method == "" {
		return "", "", "", nil
	}

	if method == "age" {
		recipientsFile = b.opts.RecipientsFile
		if recipientsFile == "" {
			recipientsFile = b.cfg.Backup.AgeRecipients
		}
		if recipientsFile == "" {
			return "", "", "", errors.New("age encryption requested but no recipients file specified")
		}
		if _, statErr := os.Stat(recipientsFile); statErr != nil {
			return "", "", "", fmt.Errorf("age recipients file not found: %s", recipientsFile)
		}
		return "age", recipientsFile, "", nil
	}

	if method == "gpg" {
		gpgRecipient = b.opts.GPGRecipient
		if gpgRecipient == "" {
			gpgRecipient = b.cfg.Backup.GPGRecipient
		}
		if gpgRecipient == "" {
			return "", "", "", errors.New("gpg encryption requested but no recipient specified")
		}
		return "gpg", "", gpgRecipient, nil
	}

	return "", "", "", fmt.Errorf("unknown encryption method: %s", method)
}

func (b *Backup) collectFiles(includeSecrets bool) []FileInfo {
	var files []FileInfo
	var totalSize int64

	for _, item := range b.cfg.GetBackupItems() {
		collected, err := b.collectItem(item.Path)
		if err != nil {
			b.out.Verbose("Skipping %s: %v\n", item.Path, err)
			b.stats.FilesSkipped++
			continue
		}
		for _, f := range collected {
			totalSize += f.Size
		}
		files = append(files, collected...)
	}

	if includeSecrets && b.opts.IncludeSecrets {
		for _, item := range b.cfg.GetSensitiveItems() {
			collected, err := b.collectItem(item.Path)
			if err != nil {
				b.out.Verbose("Skipping sensitive %s: %v\n", item.Path, err)
				continue
			}
			for i := range collected {
				collected[i].Sensitive = true
				totalSize += collected[i].Size
			}
			files = append(files, collected...)
			b.stats.SensitiveFiles += len(collected)
		}
	}

	b.stats.FilesBackedUp = len(files)
	b.stats.TotalSize = totalSize
	return files
}

func (b *Backup) collectItem(relPath string) ([]FileInfo, error) {
	fullPath := filepath.Join(b.homeDir, relPath)

	info, err := os.Lstat(fullPath)
	if err != nil {
		return nil, err
	}

	if info.Mode()&os.ModeSymlink != 0 {
		if b.isExcluded(relPath) {
			b.stats.FilesExcluded++
			return nil, nil
		}
		return []FileInfo{{
			FullPath: fullPath,
			RelPath:  relPath,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
		}}, nil
	}

	// single file
	if !info.IsDir() {
		if b.isExcluded(relPath) {
			b.stats.FilesExcluded++
			return nil, nil
		}
		return []FileInfo{{
			FullPath: fullPath,
			RelPath:  relPath,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
		}}, nil
	}

	// directory - always recurse, but don't follow symlinked directories
	var files []FileInfo
	err = filepath.WalkDir(fullPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			b.out.Verbose("Cannot access %s: %v\n", path, err)
			b.stats.FilesSkipped++
			return nil
		}
		rel, relErr := filepath.Rel(b.homeDir, path)
		if relErr != nil {
			b.out.Verbose("Cannot compute relative path for %s: %v\n", path, relErr)
			b.stats.FilesSkipped++
			return nil
		}

		// symlink: add the symlink entry itself without following it.
		// WalkDir never descends into symlinks, so no SkipDir needed.
		// returning SkipDir for a non-directory entry would skip remaining
		// siblings in the parent directory, which we must avoid.
		if d.Type()&os.ModeSymlink != 0 {
			if b.isExcluded(rel) {
				b.stats.FilesExcluded++
				return nil
			}
			fi, infoErr := d.Info()
			if infoErr != nil {
				b.out.Verbose("Cannot stat %s: %v\n", path, infoErr)
				b.stats.FilesSkipped++
				return nil
			}
			files = append(files, FileInfo{
				FullPath: path,
				RelPath:  rel,
				Size:     fi.Size(),
				ModTime:  fi.ModTime(),
			})
			return nil
		}

		if d.IsDir() {
			if b.isExcluded(rel) {
				b.stats.FilesExcluded++
				return filepath.SkipDir
			}
			return nil
		}
		if b.isExcluded(rel) {
			b.stats.FilesExcluded++
			return nil
		}

		fi, infoErr := d.Info()
		if infoErr != nil {
			b.out.Verbose("Cannot stat %s: %v\n", path, infoErr)
			b.stats.FilesSkipped++
			return nil
		}

		files = append(files, FileInfo{
			FullPath: path,
			RelPath:  rel,
			Size:     fi.Size(),
			ModTime:  fi.ModTime(),
		})
		return nil
	})

	return files, err
}

func (b *Backup) isExcluded(path string) bool {
	name := filepath.Base(path)

	for _, pattern := range b.cfg.Excludes.Patterns {
		// check against basename (for patterns like "*.log", ".DS_Store")
		if matched, err := filepath.Match(pattern, name); err == nil && matched {
			return true
		}
		// check against full relative path
		if matched, err := filepath.Match(pattern, path); err == nil && matched {
			return true
		}
		// check if path is inside excluded directory (e.g., ".git/objects/...")
		// pattern ".git" should match ".git" or ".git/..." but NOT ".gitconfig"
		if name == pattern {
			return true
		}
		// check for directory prefix match
		if strings.HasPrefix(path, pattern+"/") || strings.HasSuffix(path, "/"+pattern) {
			return true
		}
		// check for pattern as path component (e.g., "foo/.git/bar")
		if strings.Contains(path, "/"+pattern+"/") {
			return true
		}
	}
	return false
}

func (b *Backup) cleanupOldBackups() {
	if b.cfg.Backup.MaxBackups <= 0 {
		return
	}

	groups := make(map[string][]string)

	entries, err := os.ReadDir(b.cfg.Backup.BackupDir)
	if err != nil {
		b.out.Verbose("Cannot read backup directory for cleanup: %v\n", err)
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "dotfiles-") {
			continue
		}

		parts := strings.SplitN(strings.TrimPrefix(name, "dotfiles-"), ".", 2)
		if len(parts) > 0 {
			timestamp := parts[0]
			groups[timestamp] = append(groups[timestamp], filepath.Join(b.cfg.Backup.BackupDir, name))
		}
	}

	var timestamps []string
	for ts := range groups {
		timestamps = append(timestamps, ts)
	}
	sort.Strings(timestamps)

	toRemove := len(timestamps) - b.cfg.Backup.MaxBackups
	if toRemove <= 0 {
		return
	}

	for i := range toRemove {
		ts := timestamps[i]
		for _, path := range groups[ts] {
			b.out.Verbose("Removing old backup: %s\n", filepath.Base(path))
			if rmErr := os.Remove(path); rmErr != nil {
				b.out.Verbose("Failed to remove old backup %s: %v\n", filepath.Base(path), rmErr)
			}
		}
	}
}

func (b *Backup) backupHomebrew() {
	brewfile := filepath.Join(b.cfg.Backup.BackupDir, "Brewfile")
	if err := runCommand("brew", "bundle", "dump", "--file="+brewfile, "--force", "--describe"); err != nil {
		b.out.Verbose("Homebrew backup failed: %v\n", err)
		return
	}

	// filter out go "..." lines (they're saved separately in go-packages.txt)
	content, err := os.ReadFile(brewfile)
	if err != nil {
		b.out.Verbose("Failed to read Brewfile: %v\n", err)
		return
	}

	var filtered []string
	for line := range strings.SplitSeq(string(content), "\n") {
		if !strings.HasPrefix(line, "go \"") {
			filtered = append(filtered, line)
		}
	}

	if err = os.WriteFile(brewfile, []byte(strings.Join(filtered, "\n")), 0600); err != nil {
		b.out.Verbose("Failed to write filtered Brewfile: %v\n", err)
		return
	}

	b.out.Verbose("Homebrew packages saved to Brewfile\n")
}

func (b *Backup) backupMASApps() {
	masFile := filepath.Join(b.cfg.Backup.BackupDir, "mas-apps.txt")
	commandOutput, err := runCommandOutput("mas", "list")
	if err != nil {
		b.out.Verbose("MAS backup failed: %v\n", err)
		return
	}
	if err = os.WriteFile(masFile, []byte(commandOutput), 0600); err != nil {
		b.out.Verbose("Failed to save MAS apps: %v\n", err)
	} else {
		b.out.Verbose("Mac App Store apps saved\n")
	}
}

func (b *Backup) backupAptPackages() {
	if runtime.GOOS != "linux" {
		return
	}
	aptFile := filepath.Join(b.cfg.Backup.BackupDir, "apt-packages.txt")
	commandOutput, err := runCommandOutput("apt-mark", "showmanual")
	if err != nil {
		b.out.Verbose("apt backup failed: %v\n", err)
		return
	}
	if err = os.WriteFile(aptFile, []byte(commandOutput), 0600); err != nil {
		b.out.Verbose("Failed to save apt packages: %v\n", err)
	} else {
		b.out.Verbose("apt packages saved\n")
	}
}

func (b *Backup) backupGoPackages() {
	// find Go bin directory
	goBinDir := os.Getenv("GOBIN")
	if goBinDir == "" {
		goPath := os.Getenv("GOPATH")
		if goPath == "" {
			goPath = filepath.Join(b.homeDir, "go")
		}
		goBinDir = filepath.Join(goPath, "bin")
	}

	entries, err := os.ReadDir(goBinDir)
	if err != nil {
		b.out.Verbose("Go packages backup skipped: %v\n", err)
		return
	}

	var packages []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		binPath := filepath.Join(goBinDir, entry.Name())
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		// skip non-executable files
		if info.Mode()&0111 == 0 {
			continue
		}

		// get module path using go version -m
		output, cmdErr := runCommandOutput("go", "version", "-m", binPath)
		if cmdErr != nil {
			continue
		}

		// parse output to find module path
		// format: <binary>\n\tpath\t<module_path>\n...
		for line := range strings.SplitSeq(output, "\n") {
			line = strings.TrimSpace(line)
			if modulePath, found := strings.CutPrefix(line, "path\t"); found {
				if modulePath != "" {
					packages = append(packages, modulePath)
				}
				break
			}
		}
	}

	if len(packages) == 0 {
		b.out.Verbose("No Go packages found to backup\n")
		return
	}

	sort.Strings(packages)

	goFile := filepath.Join(b.cfg.Backup.BackupDir, "go-packages.txt")
	content := strings.Join(packages, "\n") + "\n"
	if err = os.WriteFile(goFile, []byte(content), 0600); err != nil {
		b.out.Verbose("Failed to save Go packages: %v\n", err)
	} else {
		b.out.Verbose("Go packages saved (%d packages)\n", len(packages))
	}
}

// FileInfo holds information about a file to backup.
type FileInfo struct {
	FullPath  string
	RelPath   string
	Size      int64
	ModTime   time.Time
	Sensitive bool
}

func formatSize(size int64) string {
	return osutils.FormatSize(size)
}
