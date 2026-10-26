package metadata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormatArchiveTimestamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"normal", "dotfiles-20250115_143022.tar.gz", "2025-01-15 14:30:22"},
		{"encrypted", "dotfiles-20250115_143022.tar.gz.age", "2025-01-15 14:30:22"},
		{"too short", "dotfiles-.tar.gz", ""},
		{"garbage timestamp", "dotfiles-xxxxxxxx_xxxxxx.tar.gz", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatArchiveTimestamp(tt.input); got != tt.expected {
				t.Errorf("FormatArchiveTimestamp(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestIsArchiveName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{"dotfiles-20250115_143022.tar.gz", true},
		{"dotfiles-20250115_143022.tar.gz.age", true},
		{"dotfiles-20250115_143022.tar.gz.gpg", true},
		{"dotfiles-20250115_143022.tar.gz.partial", false},
		{"dotfiles-20250115_143022.tar.gz.age.partial", false},
		{"dotfiles-20250115_143022.json", false},
		{"pre-restore-20250115_143022.tar.gz", false},
		{"Brewfile", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsArchiveName(tt.name); got != tt.want {
				t.Errorf("IsArchiveName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestListAndFindLatestBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{
		"dotfiles-20250101_120000.tar.gz",
		"dotfiles-20250102_120000.tar.gz.age",
		"dotfiles-20250103_120000.tar.gz.partial", // interrupted: never the latest
		"dotfiles-20250102_120000.json",
		"pre-restore-20250104_120000.tar.gz",
		"Brewfile",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	want := filepath.Join(dir, "dotfiles-20250102_120000.tar.gz.age")
	if got := FindLatestBackup(dir); got != want {
		t.Errorf("FindLatestBackup() = %q, want %q", got, want)
	}

	backups, err := ListBackups(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 2 {
		t.Fatalf("ListBackups() returned %d backups, want 2: %+v", len(backups), backups)
	}
	if backups[0].Archive != want || !backups[0].Encrypted {
		t.Errorf("newest backup = %+v, want encrypted %s first", backups[0], want)
	}

	if got := FindLatestBackup(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("FindLatestBackup(missing dir) = %q, want empty", got)
	}
}
