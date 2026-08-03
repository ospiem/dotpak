// Package schedule manages the daily backup schedule (launchd on macOS,
// crontab on Linux).
package schedule

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/osutils"
	"github.com/ospiem/dotpak/internal/output"
)

const (
	darwin = "darwin"
	linux  = "linux"
)

// Install sets up a daily backup at the given hour. configFile, when not
// empty, is passed to the scheduled command via --config.
func Install(hour int, configFile string, out *output.Output) error {
	switch runtime.GOOS {
	case darwin:
		return installLaunchd(hour, configFile, out)
	case linux:
		return installLinuxCron(hour, configFile, out)
	default:
		return errors.New("cron install is supported on macOS and Linux only")
	}
}

// Uninstall removes the daily backup schedule.
func Uninstall(out *output.Output) error {
	switch runtime.GOOS {
	case darwin:
		return uninstallLaunchd(out)
	case linux:
		return uninstallLinuxCron(out)
	default:
		return errors.New("cron uninstall is supported on macOS and Linux only")
	}
}

// Status reports the schedule state. cfg may be nil when the config could not
// be loaded; platform-specific extras (like the macOS FDA check) are skipped
// in that case.
func Status(cfg *config.Config, out *output.Output) error {
	switch runtime.GOOS {
	case darwin:
		return launchdStatus(cfg, out)
	case linux:
		return linuxCronStatus(out)
	default:
		return errors.New("cron status is supported on macOS and Linux only")
	}
}

// LogPath returns the platform-specific path of the scheduled-backup log.
func LogPath() (string, error) {
	home, err := osutils.HomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == darwin {
		return filepath.Join(home, "Library", "Logs", "dotpak", "backup.log"), nil
	}
	return filepath.Join(home, ".local", "share", "dotpak", "backup.log"), nil
}

// backupArgs builds the scheduled command line: dotpak cron run [--config path].
func backupArgs(execPath, configFile string) []string {
	if configFile != "" {
		return []string{execPath, "--config", configFile, "cron", "run"}
	}
	return []string{execPath, "cron", "run"}
}

// resolvedExecutable returns the symlink-resolved path of the running binary;
// macOS Full Disk Access must be granted to the real path.
func resolvedExecutable() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, evalErr := filepath.EvalSymlinks(execPath); evalErr == nil && resolved != "" {
		return resolved, nil
	}
	return execPath, nil
}

// shellQuote quotes a value for safe use in a shell command line. Anything
// outside a conservative allowlist of characters is single-quoted, so shell
// metacharacters (;, |, &, backticks, ...) can never break out.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	isSafe := func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return true
		case r == '_' || r == '-' || r == '.' || r == '/' || r == ':' || r == ',' || r == '+' || r == '@' || r == '%':
			return true
		default:
			return false
		}
	}
	safe := true
	for _, r := range value {
		if !isSafe(r) {
			safe = false
			break
		}
	}
	if safe {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
