package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		name := GenerateArchiveName(backupDir, false, "")
		if !strings.HasPrefix(name, "/backups/dotfiles-") {
			t.Errorf("unexpected prefix: %s", name)
		}
		if !strings.HasSuffix(name, ".tar.gz") {
			t.Errorf("expected .tar.gz suffix, got %s", name)
		}
	})

	t.Run("age encrypted archive", func(t *testing.T) {
		name := GenerateArchiveName(backupDir, true, "age")
		if !strings.HasSuffix(name, ".tar.gz.age") {
			t.Errorf("expected .tar.gz.age suffix, got %s", name)
		}
	})

	t.Run("gpg encrypted archive", func(t *testing.T) {
		name := GenerateArchiveName(backupDir, true, "gpg")
		if !strings.HasSuffix(name, ".tar.gz.gpg") {
			t.Errorf("expected .tar.gz.gpg suffix, got %s", name)
		}
	})

	t.Run("includes timestamp", func(t *testing.T) {
		name := GenerateArchiveName(backupDir, false, "")
		base := filepath.Base(name)
		if len(base) < len("dotfiles-20250110_120000.tar.gz") {
			t.Errorf("name too short: %s", name)
		}
	})
}

func TestStats(t *testing.T) {
	t.Parallel()

	stats := Stats{
		FilesBackedUp:  100,
		FilesSkipped:   5,
		FilesExcluded:  10,
		SensitiveFiles: 3,
		TotalSize:      1024 * 1024 * 10, // 10 MB
	}

	t.Run("json serialization", func(t *testing.T) {
		data, err := json.Marshal(stats)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var parsed Stats
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if parsed.FilesBackedUp != stats.FilesBackedUp {
			t.Errorf("files backed up mismatch: got %d, want %d", parsed.FilesBackedUp, stats.FilesBackedUp)
		}
		if parsed.TotalSize != stats.TotalSize {
			t.Errorf("total size mismatch: got %d, want %d", parsed.TotalSize, stats.TotalSize)
		}
	})
}

func TestBackupResult(t *testing.T) {
	t.Parallel()

	result := BackupResult{
		Success:          true,
		Archive:          "/backups/test.tar.gz",
		Encrypted:        true,
		EncryptionMethod: "age",
		Stats: Stats{
			FilesBackedUp: 50,
		},
	}

	t.Run("json serialization", func(t *testing.T) {
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var parsed BackupResult
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if parsed.Success != result.Success {
			t.Error("success mismatch")
		}
		if parsed.Archive != result.Archive {
			t.Errorf("archive mismatch: got %s, want %s", parsed.Archive, result.Archive)
		}
		if parsed.Encrypted != result.Encrypted {
			t.Error("encrypted mismatch")
		}
	})

	t.Run("error result", func(t *testing.T) {
		errResult := BackupResult{
			Success: false,
			Error:   "backup failed: disk full",
		}

		data, err := json.Marshal(errResult)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		if !strings.Contains(string(data), "disk full") {
			t.Error("expected error message in JSON")
		}
	})
}

func TestRestoreResult(t *testing.T) {
	t.Parallel()

	result := RestoreResult{
		Success:      true,
		Archive:      "/backups/test.tar.gz",
		SafetyBackup: "/backups/pre-restore/backup.tar.gz",
		Categories:   []string{"shell", "git"},
		DryRun:       false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed RestoreResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(parsed.Categories) != 2 {
		t.Errorf("expected 2 categories, got %d", len(parsed.Categories))
	}
}

func TestListResult(t *testing.T) {
	t.Parallel()

	result := ListResult{
		Success: true,
		Backups: []BackupInfo{
			{
				Archive:   "/backups/dotfiles-20250110_120000.tar.gz",
				Timestamp: "2025-01-10T12:00:00",
				Size:      1024 * 1024,
				Encrypted: false,
			},
			{
				Archive:    "/backups/dotfiles-20250111_120000.tar.gz.age",
				Timestamp:  "2025-01-11T12:00:00",
				Size:       2048 * 1024,
				Encrypted:  true,
				Encryption: "age",
			},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var parsed ListResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(parsed.Backups) != 2 {
		t.Errorf("expected 2 backups, got %d", len(parsed.Backups))
	}
	if parsed.Backups[1].Encrypted != true {
		t.Error("expected second backup to be encrypted")
	}
}

func TestBackupInfo(t *testing.T) {
	t.Parallel()

	info := BackupInfo{
		Archive:      "/backups/dotfiles-20250110_120000.tar.gz.age",
		Timestamp:    "2025-01-10T12:00:00",
		Size:         5 * 1024 * 1024,
		Encrypted:    true,
		Encryption:   "age",
		Hostname:     "my-macbook",
		FileCount:    150,
		MetadataPath: "/backups/dotfiles-20250110_120000.json",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	if !strings.Contains(string(data), `"archive"`) {
		t.Error("expected 'archive' field in JSON")
	}
	if !strings.Contains(string(data), `"file_count"`) {
		t.Error("expected 'file_count' field in JSON")
	}
	if !strings.Contains(string(data), `"metadata_path"`) {
		t.Error("expected 'metadata_path' field in JSON")
	}
}
