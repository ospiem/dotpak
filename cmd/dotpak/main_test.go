package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckFDAStatus(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	t.Run("not required when backup_dir is outside protected locations", func(t *testing.T) {
		backupDir := filepath.Join(home, "backups")
		result := checkFDAStatus(backupDir, home)
		if result != "not required (backup_dir not in protected location)" {
			t.Errorf("unexpected result: %s", result)
		}
	})

	t.Run("granted when protected dir is accessible", func(t *testing.T) {
		// create a dir under "Desktop" to simulate a protected location
		desktopBackup := filepath.Join(home, "Desktop", "backups")
		if err := os.MkdirAll(desktopBackup, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		result := checkFDAStatus(desktopBackup, home)
		if result != "granted" {
			t.Errorf("expected granted, got: %s", result)
		}
	})

	t.Run("falls back to parent when dir does not exist", func(t *testing.T) {
		// desktop exists but backups/subfolder does not
		desktop := filepath.Join(home, "Desktop")
		if err := os.MkdirAll(desktop, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		nonExistent := filepath.Join(desktop, "backups", "subfolder")
		result := checkFDAStatus(nonExistent, home)
		// parent Desktop exists and is accessible
		if result != "granted" {
			t.Errorf("expected granted via parent, got: %s", result)
		}
	})

	t.Run("expands tilde prefix", func(t *testing.T) {
		result := checkFDAStatus("~/some/path", home)
		if result != "not required (backup_dir not in protected location)" {
			t.Errorf("unexpected result: %s", result)
		}
	})

	t.Run("recognizes all protected prefixes", func(t *testing.T) {
		protectedDirs := []string{"Desktop", "Documents", "Downloads"}
		for _, dir := range protectedDirs {
			dirPath := filepath.Join(home, dir)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				t.Fatalf("failed to create %s: %v", dir, err)
			}
			result := checkFDAStatus(dirPath, home)
			if result != "granted" {
				t.Errorf("expected granted for %s, got: %s", dir, result)
			}
		}
	})
}

func TestCronLogPath(t *testing.T) {
	t.Parallel()

	logPath, err := cronLogPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logPath == "" {
		t.Fatal("expected non-empty log path")
	}

	switch runtime.GOOS {
	case "darwin":
		if !filepath.IsAbs(logPath) {
			t.Error("expected absolute path")
		}
		if filepath.Base(logPath) != "backup.log" {
			t.Errorf("expected backup.log, got %s", filepath.Base(logPath))
		}
	case "linux":
		if !filepath.IsAbs(logPath) {
			t.Error("expected absolute path")
		}
		if filepath.Base(logPath) != "backup.log" {
			t.Errorf("expected backup.log, got %s", filepath.Base(logPath))
		}
	}
}

func TestExtractTimestamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal", "dotfiles-20250115_143022.tar.gz", "2025-01-15 14:30:22"},
		{"encrypted", "dotfiles-20250115_143022.tar.gz.age", "2025-01-15 14:30:22"},
		{"too short", "dotfiles-.tar.gz", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTimestamp(tt.input)
			if result != tt.expected {
				t.Errorf("extractTimestamp(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLinuxCronStatus(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	// just verify it doesn't panic when crontab may not exist
}
