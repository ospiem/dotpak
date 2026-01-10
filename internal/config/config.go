// Package config handles TOML configuration loading for dotpak.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/ospiem/dotpak/internal/utils"
)

// Config represents the main configuration structure.
type Config struct {
	Backup    BackupConfig          `toml:"backup"`
	Items     []string              `toml:"items"`
	Sensitive []string              `toml:"sensitive"`
	Excludes  ExcludesConfig        `toml:"excludes"`
	Profiles  map[string]Profile    `toml:"profile"`
	Hosts     map[string]HostConfig `toml:"host"`
}

// BackupConfig holds backup-related settings.
type BackupConfig struct {
	BackupDir        string   `toml:"backup_dir"`
	MaxBackups       int      `toml:"max_backups"`
	Encryption       string   `toml:"encryption"`
	AgeRecipients    string   `toml:"age_recipients"`
	AgeIdentityFiles []string `toml:"age_identity_files"`
	GPGRecipient     string   `toml:"gpg_recipient"`
}

// ExcludesConfig holds file exclusion patterns.
type ExcludesConfig struct {
	Patterns []string `toml:"patterns"`
}

// Profile represents a named backup profile.
type Profile struct {
	Items          []string       `toml:"items"`
	Sensitive      []string       `toml:"sensitive"`
	ExtraItems     []string       `toml:"extra_items"`
	ExtraSensitive []string       `toml:"extra_sensitive"`
	Excludes       ExcludesConfig `toml:"excludes"`
}

// HostConfig represents hostname-specific settings.
type HostConfig struct {
	ExtraItems     []string       `toml:"extra_items"`
	ExtraSensitive []string       `toml:"extra_sensitive"`
	Excludes       ExcludesConfig `toml:"excludes"`
}

// DefaultConfig returns a config with sensible defaults.
// If home directory cannot be determined, paths will use relative paths.
func DefaultConfig() *Config {
	home, err := utils.HomeDir()
	if err != nil {
		// fall back to relative paths if home dir cannot be determined
		home = "."
	}
	return &Config{
		Backup: BackupConfig{
			BackupDir:        filepath.Join(home, "backups", "dotfiles"),
			MaxBackups:       14,
			Encryption:       "none",
			AgeRecipients:    "", // user must explicitly configure
			AgeIdentityFiles: nil,
		},
		Items: []string{
			// shell
			".zshrc", ".bashrc", ".profile", ".zprofile", ".bash_profile",
			".zsh", ".oh-my-zsh/custom", ".config/fish", ".p10k.zsh", ".zshenv",
			// git
			".gitconfig", ".gitignore_global", ".config/git",
			// editors
			".vimrc", ".config/nvim",
			".emacs", ".emacs.d",
			".config/helix", ".config/zed",
			// terminal
			".tmux.conf", ".config/alacritty",
			".config/kitty", ".config/wezterm",
			".config/starship.toml", ".config/zellij",
			// macOS
			".config/raycast",
			// node.js
			".npmrc", ".nvmrc", ".yarnrc",
			".config/yarn", ".bunfig.toml",
			// python
			".config/pip", ".config/ruff",
			".config/mypy", ".condarc",
			".jupyter",
			// ruby
			".gemrc", ".irbrc", ".pryrc",
			// java
			".gradle", ".m2/settings.xml",
			// rust
			".cargo/config.toml",
			".rustup/settings.toml",
			// go
			".config/go",
			// DevOps (configs only)
			".ansible", ".ansible.cfg",
			".config/podman",
			// AI tools (settings)
			".claude/settings.json", ".claude/projects",
			".codex/config.toml", ".codex/skills",
		},
		Sensitive: []string{
			// SSH
			".ssh",
			// GPG
			".gnupg",
			// cloud credentials
			".aws",
			".config/gcloud",
			".azure",
			".kube",
			".s3cfg",
			".yandex",
			// terraform
			".terraform.d",
			".terraformrc",
			// python credentials
			".pypirc",
			// docker (may contain registry auth)
			".docker",
			// shell history
			".zsh_history",
			".bash_history",
			".lesshst",
			// AI tools (auth/tokens)
			".claude.json",
			".codex/auth.json",
			".ai",
		},
		Excludes: ExcludesConfig{
			Patterns: []string{
				// general
				".git", ".idea", "*.log", "*.swp", "*.bak",
				".DS_Store", "*.sock", "*.cache",
				// CI/CD and dev artifacts
				".circleci", ".github", ".travis.yml", ".gitlab-ci.yml",
				"Makefile", "Dockerfile", "*.md", "LICENSE*", "COPYING*",
				"Gemfile*", "*.spec", "*.rb", "test", "tests", "spec",
				".editorconfig", ".gitignore", ".gitattributes",
				".rspec", ".rubocop*", ".ruby-version",
				// python
				"*.pyc", "__pycache__", ".venv", "venv",
				// node
				"node_modules",
				// java/Gradle/Maven
				".gradle/caches", ".gradle/daemon", ".m2/repository",
				// terraform
				"*.tfstate", "*.tfstate.*",
				// GPG transient
				"S.gpg-agent*", "random_seed", "*.status",
				// docker transient
				".token_seed*", "buildx/refs", "buildx/activity", "buildx/.lock",
				// SSH transient
				"known_hosts.old",
				// zsh compiled
				"*.zwc",
				// emacs
				"*~", "#*#", ".emacs.d/elpa", ".emacs.d/eln-cache",
				// vim/Neovim
				".config/nvim/lazy-lock.json",
				// ruby version managers (large)
				".rbenv/versions", ".rvm/gems", ".rvm/rubies",
				// oh-my-zsh cloned plugins artifacts
				"gitstatus/src", "gitstatus/deps", "gitstatus/usrbin",
				"*.png", "*.gif", "*.jpg", "*.svg",
				"test-data", "docs",
				// misc dev files
				"*.sh", "DESCRIPTION", "URL", "VERSION", "ZSH_VERSIONS",
				".revision-hash", ".version",
			},
		},
		Profiles: make(map[string]Profile),
		Hosts:    make(map[string]HostConfig),
	}
}

// DefaultConfigPath returns the default config file path.
// Returns empty string if home directory cannot be determined.
func DefaultConfigPath() string {
	home, err := utils.HomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "dotpak", "config.toml")
}

// Load reads configuration from a TOML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil // use defaults if config doesn't exist
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// start with empty config so config file completely replaces defaults
	cfg := &Config{
		Profiles: make(map[string]Profile),
		Hosts:    make(map[string]HostConfig),
	}

	if _, decodeErr := toml.Decode(string(data), cfg); decodeErr != nil {
		return nil, fmt.Errorf("parsing config: %w", decodeErr)
	}

	if cfg.Backup.MaxBackups == 0 {
		cfg.Backup.MaxBackups = 14
	}
	if cfg.Backup.Encryption == "" {
		cfg.Backup.Encryption = "none"
	}

	cfg.Backup.BackupDir = expandPath(cfg.Backup.BackupDir)
	cfg.Backup.AgeRecipients = expandPath(cfg.Backup.AgeRecipients)
	cfg.Backup.AgeIdentityFiles = expandPaths(cfg.Backup.AgeIdentityFiles)

	// expand ~ in Items and Sensitive paths
	for i, item := range cfg.Items {
		cfg.Items[i] = expandPath(item)
	}
	for i, item := range cfg.Sensitive {
		cfg.Sensitive[i] = expandPath(item)
	}

	return cfg, nil
}

// LoadWithProfile loads config and applies a profile.
func LoadWithProfile(path, profileName string) (*Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	// apply hostname-specific config if available
	hostname, err := utils.Hostname()
	if err == nil {
		if hostCfg, ok := cfg.Hosts[hostname]; ok {
			cfg.applyHostConfig(hostCfg)
		}
	}

	if profileName != "" {
		profile, ok := cfg.Profiles[profileName]
		if !ok {
			return nil, fmt.Errorf("profile not found: %s", profileName)
		}
		cfg.applyProfile(profile)
	}

	return cfg, nil
}

func (c *Config) applyHostConfig(host HostConfig) {
	if len(host.ExtraItems) > 0 {
		c.Items = append(c.Items, host.ExtraItems...)
	}
	if len(host.ExtraSensitive) > 0 {
		c.Sensitive = append(c.Sensitive, host.ExtraSensitive...)
	}
	if len(host.Excludes.Patterns) > 0 {
		c.Excludes.Patterns = append(c.Excludes.Patterns, host.Excludes.Patterns...)
	}
}

func (c *Config) applyProfile(profile Profile) {
	if len(profile.Items) > 0 {
		c.Items = profile.Items
	}
	if len(profile.Sensitive) > 0 {
		c.Sensitive = profile.Sensitive
	}
	if len(profile.ExtraItems) > 0 {
		c.Items = append(c.Items, profile.ExtraItems...)
	}
	if len(profile.ExtraSensitive) > 0 {
		c.Sensitive = append(c.Sensitive, profile.ExtraSensitive...)
	}
	if len(profile.Excludes.Patterns) > 0 {
		c.Excludes.Patterns = append(c.Excludes.Patterns, profile.Excludes.Patterns...)
	}
}

// GetBackupItems returns the list of items to backup.
func (c *Config) GetBackupItems() []BackupItem {
	items := make([]BackupItem, 0, len(c.Items))
	for _, path := range c.Items {
		items = append(items, BackupItem{Path: path})
	}
	return items
}

// GetSensitiveItems returns the list of sensitive items.
func (c *Config) GetSensitiveItems() []BackupItem {
	items := make([]BackupItem, 0, len(c.Sensitive))
	for _, path := range c.Sensitive {
		items = append(items, BackupItem{Path: path})
	}
	return items
}

// BackupItem represents an item to backup.
type BackupItem struct {
	Path string
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path // return unexpanded on error
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func expandPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	expanded := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		expanded = append(expanded, expandPath(path))
	}
	return expanded
}
