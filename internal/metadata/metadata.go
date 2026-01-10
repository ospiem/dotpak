// Package metadata handles backup metadata JSON files.
package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ospiem/dotpak/internal/utils"
)

// Metadata represents backup metadata.
type Metadata struct {
	Timestamp        string `json:"timestamp"`
	Hostname         string `json:"hostname"`
	OSVersion        string `json:"os_version,omitempty"`
	Encrypted        bool   `json:"encrypted"`
	EncryptionMethod string `json:"encryption_method,omitempty"`
	Stats            Stats  `json:"stats"`
}

// Stats represents backup statistics.
type Stats struct {
	FilesBackedUp  int   `json:"files_backed_up"`
	FilesSkipped   int   `json:"files_skipped"`
	FilesExcluded  int   `json:"files_excluded"`
	SensitiveFiles int   `json:"sensitive_files"`
	TotalSize      int64 `json:"total_size"`
}

// BackupResult represents the result of a backup operation.
type BackupResult struct {
	Success          bool   `json:"success"`
	Archive          string `json:"archive,omitempty"`
	Encrypted        bool   `json:"encrypted"`
	EncryptionMethod string `json:"encryption_method,omitempty"`
	Stats            Stats  `json:"stats"`
	Error            string `json:"error,omitempty"`
}

// RestoreResult represents the result of a restore operation.
type RestoreResult struct {
	Success      bool     `json:"success"`
	Archive      string   `json:"archive,omitempty"`
	SafetyBackup string   `json:"safety_backup,omitempty"`
	Categories   []string `json:"categories,omitempty"`
	DryRun       bool     `json:"dry_run"`
	Error        string   `json:"error,omitempty"`
}

// ListResult represents the result of a list operation.
type ListResult struct {
	Success bool         `json:"success"`
	Backups []BackupInfo `json:"backups"`
	Error   string       `json:"error,omitempty"`
}

// BackupInfo represents info about a single backup.
type BackupInfo struct {
	Archive      string `json:"archive"`
	Timestamp    string `json:"timestamp"`
	Size         int64  `json:"size"`
	Encrypted    bool   `json:"encrypted"`
	Encryption   string `json:"encryption,omitempty"`
	Hostname     string `json:"hostname,omitempty"`
	FileCount    int    `json:"file_count,omitempty"`
	MetadataPath string `json:"metadata_path,omitempty"`
}

// New creates a new Metadata with current timestamp and hostname.
func New() *Metadata {
	hostname, err := utils.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	return &Metadata{
		Timestamp: time.Now().Format("2006-01-02T15:04:05"),
		Hostname:  hostname,
	}
}

// Load reads metadata from a JSON file.
func Load(path string) (*Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var meta Metadata
	if err = json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}

	return &meta, nil
}

// Save writes metadata to a JSON file.
func (m *Metadata) Save(path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// GetMetadataPath returns the metadata path for an archive.
// archive.tar.gz -> archive.json
// archive.tar.gz.age -> archive.json.
func GetMetadataPath(archivePath string) string {
	base := archivePath

	for _, ext := range []string{".age", ".gpg"} {
		if before, ok := strings.CutSuffix(base, ext); ok {
			base = before
			break
		}
	}

	switch {
	case strings.HasSuffix(base, ".tar.gz"):
		base = strings.TrimSuffix(base, ".tar.gz")
	case strings.HasSuffix(base, ".tar"):
		base = strings.TrimSuffix(base, ".tar")
	}

	return base + ".json"
}

// GenerateArchiveName creates an archive name with timestamp.
func GenerateArchiveName(backupDir string, encrypted bool, method string) string {
	timestamp := time.Now().Format("20060102_150405")
	name := "dotfiles-" + timestamp + ".tar.gz"

	if encrypted {
		switch method {
		case "age":
			name += ".age"
		case "gpg":
			name += ".gpg"
		}
	}

	return filepath.Join(backupDir, name)
}

//nolint:nestif // OS version parsing requires navigating plist/os-release structure
func GetOSVersion() string {
	if data, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist"); err == nil {
		str := string(data)
		start := strings.Index(str, "<key>ProductVersion</key>")
		if start != -1 {
			str = str[start:]
			start = strings.Index(str, "<string>")
			if start != -1 {
				str = str[start+8:]
				before, _, ok := strings.Cut(str, "</string>")
				if ok {
					return "macOS " + before
				}
			}
		}
	}

	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		lines := strings.SplitSeq(string(data), "\n")
		for line := range lines {
			if after, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
				val := after
				val = strings.Trim(val, "\"")
				return val
			}
		}
	}

	return ""
}
