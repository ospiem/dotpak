package schedule

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ospiem/dotpak/internal/osutils"
	"github.com/ospiem/dotpak/internal/output"
)

const linuxCronMarker = "# dotpak"

func installLinuxCron(hour int, configFile string, out *output.Output) error {
	home, err := osutils.HomeDir()
	if err != nil {
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting executable path: %w", err)
	}

	logDir := filepath.Join(home, ".local", "share", "dotpak")
	if err = os.MkdirAll(logDir, 0700); err != nil {
		return fmt.Errorf("creating logs directory: %w", err)
	}

	// capture current PATH for scheduled execution (cron uses minimal /usr/bin:/bin)
	currentPath := os.Getenv("PATH")
	if currentPath == "" {
		currentPath = "/usr/local/bin:/usr/bin:/bin"
	}

	cronCmd := buildCronCommand(backupArgs(execPath, configFile))
	logFile := shellQuote(filepath.Join(logDir, "backup.log"))
	// use shell group to add timestamp before each run, combine stdout/stderr
	cronLine := fmt.Sprintf(
		"0 %d * * * { echo '---'; date -Iseconds; %s; } >> %s 2>&1 %s",
		hour, cronCmd, logFile, linuxCronMarker,
	)
	pathLine := fmt.Sprintf("PATH=%s %s", currentPath, linuxCronMarker)

	existing, err := readCrontab()
	if err != nil {
		return err
	}

	lines, _ := filterDotpakCron(existing)
	lines = append(lines, pathLine, cronLine)

	if err = writeCrontab(strings.Join(lines, "\n") + "\n"); err != nil {
		return err
	}

	out.Success("Installed daily backup at %d:00\n", hour)
	out.Print("Cron entry: %s\n", cronLine)
	return nil
}

func uninstallLinuxCron(out *output.Output) error {
	existing, err := readCrontab()
	if err != nil {
		return err
	}

	lines, removed := filterDotpakCron(existing)
	if !removed {
		out.Warning("Cron entry not installed\n")
		return nil
	}

	if len(lines) == 0 {
		if err = exec.Command("crontab", "-r").Run(); err != nil {
			return fmt.Errorf("removing crontab: %w", err)
		}
		out.Success("Uninstalled daily backup\n")
		return nil
	}

	if err = writeCrontab(strings.Join(lines, "\n") + "\n"); err != nil {
		return err
	}

	out.Success("Uninstalled daily backup\n")
	return nil
}

func linuxCronStatus(out *output.Output) error {
	existing, err := readCrontab()
	if err != nil {
		return err
	}

	// look for dotpak entry
	found := false
	var cronLine string
	for line := range strings.SplitSeq(existing, "\n") {
		if strings.HasSuffix(strings.TrimSpace(line), linuxCronMarker) {
			if !strings.HasPrefix(line, "PATH=") {
				found = true
				cronLine = line
			}
		}
	}

	if !found {
		out.Print("Status: not installed\n")
		out.Print("\nRun 'dotpak cron install' to set up scheduled backups\n")
		return nil
	}

	out.Print("Status: installed\n")
	out.Print("Cron entry: %s\n", cronLine)
	return nil
}

func buildCronCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func readCrontab() (string, error) {
	out, err := exec.Command("crontab", "-l").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no crontab for") {
			return "", nil
		}
		return "", fmt.Errorf("reading crontab: %w", err)
	}
	return string(out), nil
}

func writeCrontab(content string) error {
	tmp, err := osutils.CreateTempFile("dotpak-crontab-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err = tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// crontab <file> is atomic - if it fails, the original crontab is preserved
	//nolint:gosec // g204: tmp.Name() is a temp file created by this function
	if err = exec.Command("crontab", tmp.Name()).Run(); err != nil {
		return fmt.Errorf("installing crontab: %w", err)
	}
	return nil
}

func filterDotpakCron(existing string) ([]string, bool) {
	lines := strings.Split(strings.TrimRight(existing, "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	removed := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(trimmed, linuxCronMarker) {
			removed = true
			continue
		}
		filtered = append(filtered, line)
	}

	return filtered, removed
}
