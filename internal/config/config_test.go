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

func TestExpandPath(t *testing.T) {
	t.Parallel()

	home, _ := os.UserHomeDir()

	tests := []struct {
		name     string
		input    string
		contains string // check if result contains this
	}{
		{
			name:     "expands tilde prefix",
			input:    "~/backups",
			contains: home,
		},
		{
			name:     "leaves absolute path unchanged",
			input:    "/usr/local/bin",
			contains: "/usr/local/bin",
		},
		{
			name:     "leaves relative path unchanged",
			input:    "relative/path",
			contains: "relative/path",
		},
		{
			name:     "only expands leading tilde",
			input:    "path/with/~/tilde",
			contains: "path/with/~/tilde",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandPath(tt.input)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("expected result to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestGetBackupItems(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Items: []string{".zshrc", ".config/nvim", ".vimrc"},
	}

	items := cfg.GetBackupItems()

	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}

	paths := make(map[string]bool)
	for _, item := range items {
		paths[item.Path] = true
	}

	if !paths[".config/nvim"] {
		t.Error("expected .config/nvim to exist")
	}
	if !paths[".zshrc"] {
		t.Error("expected .zshrc to exist")
	}
}

func TestGetSensitiveItems(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Sensitive: []string{".ssh/id_ed25519", ".aws/credentials", ".kube"},
	}

	items := cfg.GetSensitiveItems()

	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
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

func TestBackupItem(t *testing.T) {
	t.Parallel()

	item := BackupItem{
		Path: ".config/nvim",
	}

	if item.Path != ".config/nvim" {
		t.Errorf("expected path .config/nvim, got %s", item.Path)
	}
}
