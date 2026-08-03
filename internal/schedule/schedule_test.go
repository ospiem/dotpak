package schedule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFDAStatus(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	t.Run("not required when backup_dir is outside protected locations", func(t *testing.T) {
		backupDir := filepath.Join(home, "backups")
		result := checkFDAStatus(backupDir, home)
		if result != "not required (backup_dir not in protected location)" {
			t.Errorf("unexpected result: %s", result)
		}
	})

	t.Run("granted when protected dir is accessible", func(t *testing.T) {
		// create a dir under "Desktop" to simulate a protected location
		desktopBackup := filepath.Join(home, "Desktop", "backups")
		if err := os.MkdirAll(desktopBackup, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		result := checkFDAStatus(desktopBackup, home)
		if result != "granted" {
			t.Errorf("expected granted, got: %s", result)
		}
	})

	t.Run("falls back to parent when dir does not exist", func(t *testing.T) {
		// desktop exists but backups/subfolder does not
		desktop := filepath.Join(home, "Desktop")
		if err := os.MkdirAll(desktop, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		nonExistent := filepath.Join(desktop, "backups", "subfolder")
		result := checkFDAStatus(nonExistent, home)
		// parent Desktop exists and is accessible
		if result != "granted" {
			t.Errorf("expected granted via parent, got: %s", result)
		}
	})

	t.Run("recognizes all protected prefixes", func(t *testing.T) {
		protectedDirs := []string{"Desktop", "Documents", "Downloads"}
		for _, dir := range protectedDirs {
			dirPath := filepath.Join(home, dir)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				t.Fatalf("failed to create %s: %v", dir, err)
			}
			result := checkFDAStatus(dirPath, home)
			if result != "granted" {
				t.Errorf("expected granted for %s, got: %s", dir, result)
			}
		}
	})
}

func TestLogPath(t *testing.T) {
	t.Parallel()

	logPath, err := LogPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(logPath) {
		t.Error("expected absolute path")
	}
	if filepath.Base(logPath) != "backup.log" {
		t.Errorf("expected backup.log, got %s", filepath.Base(logPath))
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", "''"},
		{"plain path", "/usr/local/bin/dotpak", "/usr/local/bin/dotpak"},
		{"safe chars unquoted", "backup-2024_01.log", "backup-2024_01.log"},
		{"space quoted", "/path/with space", "'/path/with space'"},
		{"semicolon quoted", "/tmp/a;reboot", "'/tmp/a;reboot'"},
		{"ampersand quoted", "a&b", "'a&b'"},
		{"pipe quoted", "a|b", "'a|b'"},
		{"backtick quoted", "a`id`b", "'a`id`b'"},
		{"redirect quoted", "a>b", "'a>b'"},
		{"subshell quoted", "$(id)", "'$(id)'"},
		{"glob quoted", "*.log", "'*.log'"},
		{"single quote escaped", "it's", `'it'"'"'s'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellQuote(tt.input); got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildCronCommand(t *testing.T) {
	t.Parallel()

	got := buildCronCommand([]string{"/usr/local/bin/dotpak", "backup", "--json", "--config", "/tmp/my config.toml"})
	want := "/usr/local/bin/dotpak backup --json --config '/tmp/my config.toml'"
	if got != want {
		t.Errorf("buildCronCommand = %q, want %q", got, want)
	}
}

func TestBackupArgs(t *testing.T) {
	t.Parallel()

	got := backupArgs("/bin/dotpak", "")
	if strings.Join(got, " ") != "/bin/dotpak cron run" {
		t.Errorf("unexpected args without config: %v", got)
	}

	got = backupArgs("/bin/dotpak", "/etc/dotpak.toml")
	if strings.Join(got, " ") != "/bin/dotpak --config /etc/dotpak.toml cron run" {
		t.Errorf("unexpected args with config: %v", got)
	}
}

func TestFilterDotpakCron(t *testing.T) {
	t.Parallel()

	existing := "0 1 * * * /usr/bin/other-job\n" +
		"PATH=/usr/bin:/bin # dotpak\n" +
		"0 15 * * * dotpak cron run # dotpak\n"

	lines, removed := filterDotpakCron(existing)
	if !removed {
		t.Error("expected dotpak entries to be detected")
	}
	if len(lines) != 1 || lines[0] != "0 1 * * * /usr/bin/other-job" {
		t.Errorf("expected only the foreign entry to remain, got %v", lines)
	}

	lines, removed = filterDotpakCron("0 1 * * * /usr/bin/other-job\n")
	if removed {
		t.Error("expected no dotpak entries")
	}
	if len(lines) != 1 {
		t.Errorf("expected foreign entry preserved, got %v", lines)
	}
}

func TestXMLEscapeText(t *testing.T) {
	t.Parallel()

	got, err := xmlEscapeText(`/path/with <weird> & "chars"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.ContainsAny(got, "<>") && !strings.Contains(got, "&lt;") {
		t.Errorf("expected XML-escaped output, got %q", got)
	}
	if !strings.Contains(got, "&amp;") {
		t.Errorf("expected & to be escaped, got %q", got)
	}
}
