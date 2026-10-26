package metadata

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ospiem/dotpak/internal/crypto"
)

// PartialSuffix marks in-progress archives so an interrupted backup never
// leaves a truncated file at the final path.
const PartialSuffix = ".partial"

// IsArchiveName reports whether name is a completed dotpak archive:
// dotfiles-<timestamp>.tar.gz[.age|.gpg]. Metadata siblings, .partial files
// and pre-restore safety archives are not.
func IsArchiveName(name string) bool {
	if _, ok := ArchiveTimestamp(name); !ok {
		return false
	}
	base := name
	if method := crypto.DetectMethod(base); method != crypto.MethodNone {
		base = strings.TrimSuffix(base, method.Extension())
	}
	return strings.HasSuffix(base, ".tar.gz")
}

// IsPartialName reports whether name is an in-progress (or abandoned) archive.
func IsPartialName(name string) bool {
	return strings.HasPrefix(name, ArchivePrefix) && strings.HasSuffix(name, PartialSuffix)
}

// ArchiveTimestamp returns the timestamp embedded in a backup-dir file name
// (archive, metadata sibling or .partial file) and whether it is well-formed.
func ArchiveTimestamp(name string) (string, bool) {
	rest, ok := strings.CutPrefix(name, ArchivePrefix)
	if !ok || len(rest) < len(TimestampFormat) {
		return "", false
	}
	ts := rest[:len(TimestampFormat)]
	if _, err := time.Parse(TimestampFormat, ts); err != nil {
		return "", false
	}
	return ts, true
}

// FormatArchiveTimestamp renders the timestamp of an archive name for display:
// dotfiles-20240115_143022.tar.gz -> "2024-01-15 14:30:22". Unparseable names
// yield "".
func FormatArchiveTimestamp(name string) string {
	ts, ok := ArchiveTimestamp(name)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s-%s-%s %s:%s:%s",
		ts[0:4], ts[4:6], ts[6:8], // year, month, day
		ts[9:11], ts[11:13], ts[13:15]) // hour, minute, second
}

// ListBackups returns the completed archives in backupDir, newest first, with
// their metadata siblings folded in when present.
func ListBackups(backupDir string) ([]BackupInfo, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("reading backup directory: %w", err)
	}

	var backups []BackupInfo
	for _, entry := range entries {
		name := entry.Name()
		if !IsArchiveName(name) {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			// file became unreadable between ReadDir and Info - skip it
			continue
		}

		fullPath := filepath.Join(backupDir, name)
		backupInfo := BackupInfo{
			Archive:   fullPath,
			Timestamp: FormatArchiveTimestamp(name),
			Size:      info.Size(),
			Encrypted: crypto.IsEncryptedPath(name),
		}

		if meta, loadErr := Load(GetMetadataPath(fullPath)); loadErr == nil {
			backupInfo.Hostname = meta.Hostname
			backupInfo.FileCount = meta.Stats.FilesBackedUp
			backupInfo.Encryption = meta.EncryptionMethod
		}

		backups = append(backups, backupInfo)
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp > backups[j].Timestamp
	})
	return backups, nil
}

// FindLatestBackup returns the path of the newest completed archive in
// backupDir, or "" when there is none.
func FindLatestBackup(backupDir string) string {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return ""
	}

	var latest string
	for _, entry := range entries {
		name := entry.Name()
		if IsArchiveName(name) && name > latest {
			latest = name
		}
	}
	if latest == "" {
		return ""
	}
	return filepath.Join(backupDir, latest)
}
