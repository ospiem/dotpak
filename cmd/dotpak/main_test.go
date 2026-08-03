package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ospiem/dotpak/internal/config"
)

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

func TestIsArchiveFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{"dotfiles-20250115_143022.tar.gz", true},
		{"dotfiles-20250115_143022.tar.gz.age", true},
		{"dotfiles-20250115_143022.tar.gz.gpg", true},
		{"dotfiles-20250115_143022.tar.gz.partial", false},
		{"dotfiles-20250115_143022.json", false},
		{"pre-restore-20250115_143022.tar.gz", false},
		{"Brewfile", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isArchiveFile(tt.name); got != tt.want {
				t.Errorf("isArchiveFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

// TestSampleConfigMatchesDefaults guards against drift between the commented
// template written by `dotpak config init` and config.DefaultConfig() — the
// two must describe the same configuration.
func TestSampleConfigMatchesDefaults(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	samplePath := filepath.Join(tmpDir, "config.toml")
	if err := os.WriteFile(samplePath, []byte(getSampleConfig()), 0600); err != nil {
		t.Fatal(err)
	}

	sample, err := config.Load(samplePath)
	if err != nil {
		t.Fatalf("sample config does not parse: %v", err)
	}
	defaults := config.DefaultConfig()

	if sample.Backup.MaxBackups != defaults.Backup.MaxBackups {
		t.Errorf("max_backups drifted: sample %d, default %d",
			sample.Backup.MaxBackups, defaults.Backup.MaxBackups)
	}
	if sample.Backup.Encryption != defaults.Backup.Encryption {
		t.Errorf("encryption drifted: sample %q, default %q",
			sample.Backup.Encryption, defaults.Backup.Encryption)
	}
	if sample.Backup.BackupDir != defaults.Backup.BackupDir {
		t.Errorf("backup_dir drifted: sample %q, default %q",
			sample.Backup.BackupDir, defaults.Backup.BackupDir)
	}

	assertSameSet(t, "items", sample.Items, defaults.Items)
	assertSameSet(t, "sensitive", sample.Sensitive, defaults.Sensitive)
	assertSameSet(t, "excludes.patterns", sample.Excludes.Patterns, defaults.Excludes.Patterns)
}

func assertSameSet(t *testing.T, what string, got, want []string) {
	t.Helper()

	gotSorted := slices.Clone(got)
	wantSorted := slices.Clone(want)
	slices.Sort(gotSorted)
	slices.Sort(wantSorted)

	if !slices.Equal(gotSorted, wantSorted) {
		for _, v := range wantSorted {
			if !slices.Contains(gotSorted, v) {
				t.Errorf("%s: sample config is missing %q", what, v)
			}
		}
		for _, v := range gotSorted {
			if !slices.Contains(wantSorted, v) {
				t.Errorf("%s: sample config has extra %q", what, v)
			}
		}
	}
}
