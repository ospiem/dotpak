package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ospiem/dotpak/internal/osutils"
)

// Validate checks a loaded config for problems that would break backup or
// restore and reports them all at once.
func Validate(cfg *Config) error {
	var issues []string

	backupDir := strings.TrimSpace(cfg.Backup.BackupDir)
	if backupDir == "" {
		issues = append(issues, "backup.backup_dir is required")
	} else {
		parentDir := filepath.Dir(osutils.ExpandPath(backupDir))
		if info, err := os.Stat(parentDir); err != nil {
			if os.IsNotExist(err) {
				issues = append(issues, fmt.Sprintf("backup.backup_dir parent does not exist: %s", parentDir))
			}
		} else if !info.IsDir() {
			issues = append(issues, fmt.Sprintf("backup.backup_dir parent is not a directory: %s", parentDir))
		}
	}

	if cfg.Backup.MaxBackups < 0 {
		issues = append(issues, "backup.max_backups must be >= 0 (0 keeps all backups)")
	}

	switch cfg.Backup.Encryption {
	case "age", "gpg", "none", "":
	default:
		issues = append(
			issues,
			fmt.Sprintf("backup.encryption must be age|gpg|none (got %q)", cfg.Backup.Encryption),
		)
	}

	if cfg.Backup.Encryption == "age" {
		if strings.TrimSpace(cfg.Backup.AgeRecipients) == "" {
			issues = append(issues, "backup.age_recipients is required when encryption=age")
		} else if _, err := os.Stat(osutils.ExpandPath(cfg.Backup.AgeRecipients)); err != nil {
			issues = append(issues, fmt.Sprintf("backup.age_recipients not found: %s", cfg.Backup.AgeRecipients))
		}
	}

	if cfg.Backup.Encryption == "gpg" && strings.TrimSpace(cfg.Backup.GPGRecipient) == "" {
		issues = append(issues, "backup.gpg_recipient is required when encryption=gpg")
	}

	for _, path := range cfg.Items {
		if strings.TrimSpace(path) == "" {
			issues = append(issues, "items contains empty path")
			break
		}
	}

	for _, path := range cfg.Sensitive {
		if strings.TrimSpace(path) == "" {
			issues = append(issues, "sensitive contains empty path")
			break
		}
	}

	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("config validation failed:\n- %s", strings.Join(issues, "\n- "))
}
