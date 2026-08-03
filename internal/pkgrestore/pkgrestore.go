// Package pkgrestore reinstalls packages from the manifests a backup saved
// alongside the archive (Brewfile, apt-packages.txt, go-packages.txt).
package pkgrestore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ospiem/dotpak/internal/output"
)

// Homebrew reinstalls Homebrew packages from the backup's Brewfile.
func Homebrew(backupDir string, dryRun bool, out *output.Output) error {
	// sanitize and validate the brewfile path
	cleanBackupDir := filepath.Clean(backupDir)
	brewfile := filepath.Join(cleanBackupDir, "Brewfile")

	// resolve to absolute path and verify it's within backup directory
	absBrewfile, err := filepath.Abs(brewfile)
	if err != nil {
		return fmt.Errorf("invalid brewfile path: %w", err)
	}
	absBackupDir, err := filepath.Abs(cleanBackupDir)
	if err != nil {
		return fmt.Errorf("invalid backup directory: %w", err)
	}
	if !strings.HasPrefix(absBrewfile, absBackupDir+string(filepath.Separator)) {
		return errors.New("brewfile path escapes backup directory")
	}

	// verify it's a regular file (not a symlink to outside)
	info, err := os.Lstat(absBrewfile)
	if err != nil {
		return fmt.Errorf("brewfile not found: %s", brewfile)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("brewfile cannot be a symlink")
	}

	out.Print("Restoring Homebrew packages from %s...\n", brewfile)

	if dryRun {
		out.Print("\nDry run - would run: brew bundle install --file=%s\n", absBrewfile)
		return nil
	}

	//nolint:gosec // g204: absBrewfile is validated to be within backup directory above
	cmd := exec.Command("brew", "bundle", "install", "--file="+absBrewfile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		return fmt.Errorf("brew bundle failed: %w", err)
	}

	out.Success("Homebrew packages restored\n")
	return nil
}

// Apt prints the command to reinstall apt packages from the backup manifest.
func Apt(backupDir string, dryRun bool, out *output.Output) error {
	if runtime.GOOS != "linux" {
		return errors.New("apt restore only available on Linux")
	}
	aptFile := filepath.Join(filepath.Clean(backupDir), "apt-packages.txt")
	if _, err := os.Stat(aptFile); err != nil {
		return errors.New("apt-packages.txt not found in backup")
	}
	if dryRun {
		out.Print("Dry run - would install packages from: %s\n", aptFile)
		return nil
	}
	out.Print("To restore apt packages, run:\n")
	out.Print("  xargs sudo apt install -y < %s\n", aptFile)
	return nil
}

// Go reinstalls Go binaries from the backup's go-packages.txt manifest.
func Go(backupDir string, dryRun bool, out *output.Output) error {
	goFile := filepath.Join(filepath.Clean(backupDir), "go-packages.txt")
	content, err := os.ReadFile(goFile)
	if err != nil {
		return errors.New("go-packages.txt not found in backup")
	}

	var packages []string
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			packages = append(packages, line)
		}
	}

	if len(packages) == 0 {
		out.Print("No Go packages to restore\n")
		return nil
	}

	out.Print("Restoring %d Go packages...\n", len(packages))

	if dryRun {
		out.Print("\nDry run - would run:\n")
		for _, pkg := range packages {
			out.Print("  go install %s@latest\n", pkg)
		}
		return nil
	}

	var installed, failed int
	for _, pkg := range packages {
		out.Verbose("Installing %s...\n", pkg)
		//nolint:gosec // g204: pkg comes from go-packages.txt backup file created by this tool
		cmd := exec.Command("go", "install", pkg+"@latest")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err = cmd.Run(); err != nil {
			out.Warning("Failed to install %s: %v\n", pkg, err)
			failed++
		} else {
			installed++
		}
	}

	if failed > 0 {
		out.Print("Go packages: %d installed, %d failed\n", installed, failed)
	} else {
		out.Success("Installed %d Go packages\n", installed)
	}
	return nil
}
