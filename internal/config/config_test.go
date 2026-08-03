package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	t.Run("backup settings", func(t *testing.T) {
		if cfg.Backup.MaxBackups != 14 {
			t.Errorf("expected MaxBackups=14, got %d", cfg.Backup.MaxBackups)
		}
		if cfg.Backup.Encryption != "none" {
			t.Errorf("expected Encryption=none, got %s", cfg.Backup.Encryption)
		}
	})

	t.Run("default items exist", func(t *testing.T) {
		expectedItems := []string{".zshrc", ".bashrc", ".gitconfig", ".vimrc", ".tmux.conf", ".profile"}
		for _, expected := range expectedItems {
			found := slices.Contains(cfg.Items, expected)
			if !found {
				t.Errorf("expected default item %s to exist", expected)
			}
		}
	})

	t.Run("default sensitive items exist", func(t *testing.T) {
		expectedSensitive := []string{".ssh", ".aws", ".gnupg"}
		for _, expected := range expectedSensitive {
			found := slices.Contains(cfg.Sensitive, expected)
			if !found {
				t.Errorf("expected default sensitive item %s to exist", expected)
			}
		}
	})

	t.Run("default excludes exist", func(t *testing.T) {
		expectedExcludes := []string{"*.pyc", "__pycache__", ".git", "*.log", ".DS_Store"}
		for _, pattern := range expectedExcludes {
			found := slices.Contains(cfg.Excludes.Patterns, pattern)
			if !found {
				t.Errorf("expected default exclude pattern %s to exist", pattern)
			}
		}
	})

	t.Run("profiles and hosts initialized", func(t *testing.T) {
		if cfg.Profiles == nil {
			t.Error("expected Profiles map to be initialized")
		}
		if cfg.Hosts == nil {
			t.Error("expected Hosts map to be initialized")
		}
	})
}

func TestDefaultConfigPath(t *testing.T) {
	t.Parallel()

	path := DefaultConfigPath()

	if !filepath.IsAbs(path) {
		t.Error("expected absolute path")
	}

	if filepath.Base(path) != "config.toml" {
		t.Errorf("expected config.toml, got %s", filepath.Base(path))
	}

	if !strings.Contains(path, ".config") || !strings.Contains(path, "dotpak") {
		t.Errorf("expected path to contain .config/dotpak, got %s", path)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("returns default config when file does not exist", func(t *testing.T) {
		cfg, err := Load("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("expected no error for missing config, got %v", err)
		}
		if cfg == nil {
			t.Fatal("expected default config, got nil")
		}
		if cfg.Backup.MaxBackups != 14 {
			t.Errorf("expected default MaxBackups=14, got %d", cfg.Backup.MaxBackups)
		}
	})

	t.Run("loads valid config file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `
items = [".zshrc", ".config/nvim"]
sensitive = [".ssh/id_ed25519"]

[backup]
backup_dir = "~/test/backups"
max_backups = 5
encryption = "age"
age_recipients = "~/test/recipients.txt"

[excludes]
patterns = ["*.tmp", "cache"]
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.Backup.MaxBackups != 5 {
			t.Errorf("expected MaxBackups=5, got %d", cfg.Backup.MaxBackups)
		}
		if cfg.Backup.Encryption != "age" {
			t.Errorf("expected Encryption=age, got %s", cfg.Backup.Encryption)
		}
		if len(cfg.Items) != 2 {
			t.Errorf("expected 2 items, got %d", len(cfg.Items))
		}
	})

	t.Run("applies defaults for unset values", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `
items = [".zshrc"]

[backup]
backup_dir = "~/backups"
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if cfg.Backup.MaxBackups != 14 {
			t.Errorf("expected default MaxBackups=14, got %d", cfg.Backup.MaxBackups)
		}
		if cfg.Backup.Encryption != "none" {
			t.Errorf("expected default Encryption=none, got %s", cfg.Backup.Encryption)
		}
	})

	t.Run("returns error for invalid TOML", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `invalid toml { content`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := Load(configPath)
		if err == nil {
			t.Error("expected error for invalid TOML")
		}
	})
}

func TestLoadWithProfile(t *testing.T) {
	t.Parallel()

	t.Run("applies profile extra items", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `
items = [".zshrc"]

[backup]
backup_dir = "~/backups"
encryption = "none"

[profile.work]
extra_items = [".config/work-app"]
extra_sensitive = [".work-secrets"]
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadWithProfile(configPath, "work")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := slices.Contains(cfg.Items, ".zshrc")
		if !found {
			t.Error("expected original item .zshrc to exist")
		}
		found = slices.Contains(cfg.Items, ".config/work-app")
		if !found {
			t.Error("expected profile item .config/work-app to exist")
		}
		found = slices.Contains(cfg.Sensitive, ".work-secrets")
		if !found {
			t.Error("expected profile sensitive item .work-secrets to exist")
		}
	})

	t.Run("profile can override items", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `
items = [".zshrc", ".bashrc"]

[backup]
backup_dir = "~/backups"
encryption = "none"

[profile.minimal]
items = [".zshrc"]
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadWithProfile(configPath, "minimal")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(cfg.Items) != 1 {
			t.Errorf("expected 1 item (profile override), got %d", len(cfg.Items))
		}
	})

	t.Run("returns error for non-existent profile", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `
items = [".zshrc"]

[backup]
backup_dir = "~/backups"
encryption = "none"
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadWithProfile(configPath, "nonexistent")
		if err == nil {
			t.Error("expected error for non-existent profile")
		}
	})

	t.Run("applies profile excludes", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `
items = [".zshrc"]

[backup]
backup_dir = "~/backups"
encryption = "none"

[excludes]
patterns = ["*.log"]

[profile.strict]
[profile.strict.excludes]
patterns = ["*.tmp", "*.cache"]
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadWithProfile(configPath, "strict")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(cfg.Excludes.Patterns) != 3 {
			t.Errorf("expected 3 exclude patterns, got %d: %v", len(cfg.Excludes.Patterns), cfg.Excludes.Patterns)
		}
	})
}

func TestNormalizeItemPath(t *testing.T) {
	t.Parallel()

	const home = "/home/user"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"relative path unchanged", ".zshrc", ".zshrc"},
		{"nested relative path unchanged", ".config/nvim", ".config/nvim"},
		{"tilde prefix stripped", "~/.zshrc", ".zshrc"},
		{"HOME prefix stripped", "$HOME/.config/git", ".config/git"},
		{"absolute under home made relative", "/home/user/.ssh", ".ssh"},
		{"absolute outside home left cleaned", "/etc/hosts", "/etc/hosts"},
		{"trailing slash cleaned", "~/.config/nvim/", ".config/nvim"},
		{"whitespace trimmed", "  ~/.vimrc ", ".vimrc"},
		{"bare tilde means home itself", "~", "."},
		{"empty stays empty", "", ""},
		{"home boundary not prefix-matched", "/home/username/.zshrc", "/home/username/.zshrc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeItemPath(tt.input, home); got != tt.want {
				t.Errorf("normalizeItemPath(%q, %q) = %q, want %q", tt.input, home, got, tt.want)
			}
		})
	}

	t.Run("unknown home keeps absolute path", func(t *testing.T) {
		if got := normalizeItemPath("/home/user/.ssh", ""); got != "/home/user/.ssh" {
			t.Errorf("expected absolute path unchanged with unknown home, got %q", got)
		}
	})

	t.Run("unknown home still strips tilde", func(t *testing.T) {
		if got := normalizeItemPath("~/.zshrc", ""); got != ".zshrc" {
			t.Errorf("expected tilde stripped with unknown home, got %q", got)
		}
	})
}

func TestLoadNormalizesItems(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := `
items = ["~/.zshrc", ".config/nvim"]
sensitive = ["$HOME/.ssh"]

[backup]
backup_dir = "~/backups"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !slices.Contains(cfg.Items, ".zshrc") {
		t.Errorf("expected ~/.zshrc normalized to .zshrc, got %v", cfg.Items)
	}
	if !slices.Contains(cfg.Items, ".config/nvim") {
		t.Errorf("expected .config/nvim unchanged, got %v", cfg.Items)
	}
	if !slices.Contains(cfg.Sensitive, ".ssh") {
		t.Errorf("expected $HOME/.ssh normalized to .ssh, got %v", cfg.Sensitive)
	}
}

func TestLoadDefaultsForOmittedKeys(t *testing.T) {
	t.Parallel()

	t.Run("omitted backup_dir keeps default", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `
items = [".zshrc"]
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Backup.BackupDir == "" {
			t.Error("expected omitted backup_dir to keep the default, got empty string")
		}
		if len(cfg.Items) != 1 {
			t.Errorf("expected items from file to replace defaults, got %d items", len(cfg.Items))
		}
	})

	t.Run("explicit max_backups zero is honored", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")

		content := `
[backup]
backup_dir = "~/backups"
max_backups = 0
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Backup.MaxBackups != 0 {
			t.Errorf("expected max_backups=0 (keep all) to be honored, got %d", cfg.Backup.MaxBackups)
		}
	})
}

func TestHostConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	hostname, _ := os.Hostname()
	for i, c := range hostname {
		if c == '.' {
			hostname = hostname[:i]
			break
		}
	}

	content := `
items = [".zshrc"]

[backup]
backup_dir = "~/backups"
encryption = "none"

[host.` + hostname + `]
extra_items = [".config/host-specific"]
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadWithProfile(configPath, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := slices.Contains(cfg.Items, ".config/host-specific")
	if !found {
		t.Error("expected host-specific item to be applied")
	}
}
