package schedule

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/osutils"
	"github.com/ospiem/dotpak/internal/output"
)

// launchdLabel identifies the dotpak LaunchAgent.
const launchdLabel = "dev.ospiem.dotpak"

func launchdPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func installLaunchd(hour int, configFile string, out *output.Output) error {
	home, err := osutils.HomeDir()
	if err != nil {
		return err
	}
	plistPath := launchdPlistPath(home)

	resolvedPath, err := resolvedExecutable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	// capture current PATH for scheduled execution (launchd uses minimal environment)
	currentPath := os.Getenv("PATH")
	if currentPath == "" {
		currentPath = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	}

	var argsXML strings.Builder
	for _, arg := range backupArgs(resolvedPath, configFile) {
		escaped, escErr := xmlEscapeText(arg)
		if escErr != nil {
			return fmt.Errorf("escaping argument: %w", escErr)
		}
		fmt.Fprintf(&argsXML, "\n        <string>%s</string>", escaped)
	}

	escapedPath, err := xmlEscapeText(currentPath)
	if err != nil {
		return fmt.Errorf("escaping PATH: %w", err)
	}

	escapedHome, err := xmlEscapeText(home)
	if err != nil {
		return fmt.Errorf("escaping HOME: %w", err)
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>%s
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>%d</integer>
        <key>Minute</key>
        <integer>0</integer>
    </dict>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>%s</string>
        <key>PATH</key>
        <string>%s</string>
    </dict>
</dict>
</plist>
`, launchdLabel, argsXML.String(), hour, escapedHome, escapedPath)

	if err = os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0755); err != nil {
		return fmt.Errorf("creating LaunchAgents directory: %w", err)
	}

	if err = os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("writing plist: %w", err)
	}

	// unload any previously loaded agent first: launchd keeps running the old
	// job definition otherwise, and a plain "load" over it fails
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err = exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("loading LaunchAgent (try 'launchctl load %s' manually): %w", plistPath, err)
	}

	out.Success("Installed daily backup at %d:00\n", hour)
	out.Print("Plist: %s\n", plistPath)
	out.Print("Binary: %s\n", resolvedPath)
	out.Print("\nFor protected directories (Desktop, Documents, Downloads, iCloud):\n")
	out.Print("  Add to System Settings → Privacy & Security → Full Disk Access:\n")
	out.Print("  %s\n", resolvedPath)
	return nil
}

func uninstallLaunchd(out *output.Output) error {
	home, err := osutils.HomeDir()
	if err != nil {
		return err
	}
	plistPath := launchdPlistPath(home)
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	if err = os.Remove(plistPath); err != nil {
		if os.IsNotExist(err) {
			out.Warning("LaunchAgent not installed\n")
			return nil
		}
		return fmt.Errorf("removing plist: %w", err)
	}

	out.Success("Uninstalled daily backup\n")
	return nil
}

func launchdStatus(cfg *config.Config, out *output.Output) error {
	home, err := osutils.HomeDir()
	if err != nil {
		return err
	}
	plistPath := launchdPlistPath(home)

	// check if plist exists
	if _, err = os.Stat(plistPath); err != nil {
		if os.IsNotExist(err) {
			out.Print("Status: not installed\n")
			out.Print("\nRun 'dotpak cron install' to set up scheduled backups\n")
			return nil
		}
		return fmt.Errorf("checking plist: %w", err)
	}

	out.Print("Status: installed\n")
	out.Print("Plist: %s\n", plistPath)

	// check launchctl status
	cmdOut, err := exec.Command("launchctl", "list", launchdLabel).CombinedOutput()
	if err != nil {
		out.Print("Launchd: not loaded (run 'launchctl load %s')\n", plistPath)
	} else {
		if strings.Contains(string(cmdOut), "PID") {
			out.Print("Launchd: loaded\n")
		} else {
			out.Print("Launchd: loaded (idle)\n")
		}
	}

	if cfg == nil {
		out.Print("FDA: unknown (cannot load config)\n")
		return nil
	}

	out.Print("FDA: %s\n", checkFDAStatus(cfg.Backup.BackupDir, home))
	return nil
}

func checkFDAStatus(backupDir, home string) string {
	expandedBackupDir := osutils.ExpandPath(backupDir)

	// check if backup_dir is in protected location
	protectedPrefixes := []string{
		filepath.Join(home, "Desktop"),
		filepath.Join(home, "Documents"),
		filepath.Join(home, "Downloads"),
		filepath.Join(home, "Library", "Mobile Documents"),
	}

	isProtected := false
	for _, prefix := range protectedPrefixes {
		if strings.HasPrefix(expandedBackupDir, prefix) {
			isProtected = true
			break
		}
	}

	if !isProtected {
		return "not required (backup_dir not in protected location)"
	}

	// try read-only access to the protected directory (or its parent)
	dir, err := os.Open(expandedBackupDir)
	if err != nil {
		if os.IsPermission(err) {
			return "NOT GRANTED - add dotpak binary to Full Disk Access"
		}
		// directory may not exist yet — try the parent
		parentDir := filepath.Dir(expandedBackupDir)
		dir, err = os.Open(parentDir)
		if err != nil {
			if os.IsPermission(err) {
				return "NOT GRANTED - add dotpak binary to Full Disk Access"
			}
			return fmt.Sprintf("unknown (%v)", err)
		}
	}
	_ = dir.Close()
	return "granted"
}

func xmlEscapeText(value string) (string, error) {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return "", err
	}
	return buf.String(), nil
}
