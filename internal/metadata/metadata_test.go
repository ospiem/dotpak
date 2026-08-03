package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ospiem/dotpak/internal/crypto"
)

func TestNew(t *testing.T) {
	t.Parallel()

	meta := New()

	t.Run("sets timestamp", func(t *testing.T) {
		if meta.Timestamp == "" {
			t.Error("expected timestamp to be set")
		}
		_, err := time.Parse("2006-01-02T15:04:05", meta.Timestamp)
		if err != nil {
			t.Errorf("invalid timestamp format: %s", meta.Timestamp)
		}
	})

	t.Run("sets hostname", func(t *testing.T) {
		if meta.Hostname == "" {
			t.Error("expected hostname to be set")
		}
		if strings.Contains(meta.Hostname, ".") {
			t.Errorf("hostname should not contain domain: %s", meta.Hostname)
		}
	})

	t.Run("initializes with empty values", func(t *testing.T) {
		if meta.Encrypted {
			t.Error("expected encrypted to be false")
		}
		if meta.EncryptionMethod != "" {
			t.Errorf("expected empty encryption method, got %s", meta.EncryptionMethod)
		}
	})
}

func TestSaveAndLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	metaPath := filepath.Join(tmpDir, "test.json")

	t.Run("save and load roundtrip", func(t *testing.T) {
		original := &Metadata{
			Timestamp:        "2025-01-10T12:00:00",
			Hostname:         "test-host",
			OSVersion:        "macOS 15.0",
			Encrypted:        true,
			EncryptionMethod: "age",
			Stats: Stats{
				FilesBackedUp:  100,
				FilesSkipped:   5,
				FilesExcluded:  10,
				SensitiveFiles: 3,
				TotalSize:      1024 * 1024,
			},
		}

		if err := original.Save(metaPath); err != nil {
			t.Fatalf("failed to save: %v", err)
		}

		loaded, err := Load(metaPath)
		if err != nil {
			t.Fatalf("failed to load: %v", err)
		}

		if loaded.Timestamp != original.Timestamp {
			t.Errorf("timestamp mismatch: got %s, want %s", loaded.Timestamp, original.Timestamp)
		}
		if loaded.Hostname != original.Hostname {
			t.Errorf("hostname mismatch: got %s, want %s", loaded.Hostname, original.Hostname)
		}
		if loaded.Encrypted != original.Encrypted {
			t.Errorf("encrypted mismatch: got %v, want %v", loaded.Encrypted, original.Encrypted)
		}
		if loaded.EncryptionMethod != original.EncryptionMethod {
			t.Errorf("encryption method mismatch: got %s, want %s", loaded.EncryptionMethod, original.EncryptionMethod)
		}
		if loaded.Stats.FilesBackedUp != original.Stats.FilesBackedUp {
			t.Errorf(
				"files backed up mismatch: got %d, want %d",
				loaded.Stats.FilesBackedUp,
				original.Stats.FilesBackedUp,
			)
		}
		if loaded.Stats.TotalSize != original.Stats.TotalSize {
			t.Errorf("total size mismatch: got %d, want %d", loaded.Stats.TotalSize, original.Stats.TotalSize)
		}
	})

	t.Run("save creates valid JSON", func(t *testing.T) {
		meta := New()
		meta.Stats.FilesBackedUp = 50

		path := filepath.Join(tmpDir, "valid.json")
		if err := meta.Save(path); err != nil {
			t.Fatalf("failed to save: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("saved file is not valid JSON: %v", err)
		}
		if !strings.Contains(string(data), "  ") {
			t.Error("expected indented JSON output")
		}
	})

	t.Run("load returns error for non-existent file", func(t *testing.T) {
		_, err := Load("/nonexistent/path.json")
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})

	t.Run("load returns error for invalid JSON", func(t *testing.T) {
		invalidPath := filepath.Join(tmpDir, "invalid.json")
		if err := os.WriteFile(invalidPath, []byte("not json"), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := Load(invalidPath)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestGetMetadataPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		archivePath string
		expected    string
	}{
		{
			name:        "tar.gz archive",
			archivePath: "/backups/dotfiles-20250110_120000.tar.gz",
			expected:    "/backups/dotfiles-20250110_120000.json",
		},
		{
			name:        "age encrypted archive",
			archivePath: "/backups/dotfiles-20250110_120000.tar.gz.age",
			expected:    "/backups/dotfiles-20250110_120000.json",
		},
		{
			name:        "gpg encrypted archive",
			archivePath: "/backups/dotfiles-20250110_120000.tar.gz.gpg",
			expected:    "/backups/dotfiles-20250110_120000.json",
		},
		{
			name:        "tar archive",
			archivePath: "/backups/dotfiles-20250110_120000.tar",
			expected:    "/backups/dotfiles-20250110_120000.json",
		},
		{
			name:        "relative path",
			archivePath: "backup.tar.gz",
			expected:    "backup.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetMetadataPath(tt.archivePath)
			if result != tt.expected {
				t.Errorf("got %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestGenerateArchiveName(t *testing.T) {
	t.Parallel()

	backupDir := "/backups"

	t.Run("unencrypted archive", func(t *testing.T) {
		name := GenerateArchiveName(backupDir, crypto.MethodNone)
		if !strings.HasPrefix(name, "/backups/"+ArchivePrefix) {
			t.Errorf("unexpected prefix: %s", name)
		}
		if !strings.HasSuffix(name, ".tar.gz") {
			t.Errorf("expected .tar.gz suffix, got %s", name)
		}
	})

	t.Run("age encrypted archive", func(t *testing.T) {
		name := GenerateArchiveName(backupDir, crypto.MethodAge)
		if !strings.HasSuffix(name, ".tar.gz.age") {
			t.Errorf("expected .tar.gz.age suffix, got %s", name)
		}
	})

	t.Run("gpg encrypted archive", func(t *testing.T) {
		name := GenerateArchiveName(backupDir, crypto.MethodGPG)
		if !strings.HasSuffix(name, ".tar.gz.gpg") {
			t.Errorf("expected .tar.gz.gpg suffix, got %s", name)
		}
	})

	t.Run("includes timestamp", func(t *testing.T) {
		name := GenerateArchiveName(backupDir, crypto.MethodNone)
		base := filepath.Base(name)
		if len(base) < len("dotfiles-20250110_120000.tar.gz") {
			t.Errorf("name too short: %s", name)
		}
	})
}
