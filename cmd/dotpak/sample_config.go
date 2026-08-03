package main

// getSampleConfig returns the commented config template written by
// `dotpak config init`. TestSampleConfigMatchesDefaults keeps its values in
// sync with config.DefaultConfig().
func getSampleConfig() string {
	return `# Dotpak configuration file
# See https://github.com/ospiem/dotpak for documentation

# Items to backup
items = [
    # Shell
    ".zshrc",
    ".bashrc",
    ".profile",
    ".zprofile",
    ".bash_profile",
    ".zsh",
    ".zshenv",
    ".oh-my-zsh/custom",
    ".config/fish",
    ".p10k.zsh",
    # Git
    ".gitconfig",
    ".gitignore_global",
    ".config/git",
    # Editors
    ".vimrc",
    ".config/nvim",
    ".emacs",
    ".emacs.d",
    ".config/helix",
    ".config/zed",
    # Terminal
    ".tmux.conf",
    ".config/alacritty",
    ".config/kitty",
    ".config/wezterm",
    ".config/starship.toml",
    ".config/zellij",
    # macOS
    ".config/raycast",
    # Node.js
    ".npmrc",
    ".nvmrc",
    ".yarnrc",
    ".config/yarn",
    ".bunfig.toml",
    # Python
    ".config/pip",
    ".config/ruff",
    ".config/mypy",
    ".condarc",
    ".jupyter",
    # Ruby
    ".gemrc",
    ".irbrc",
    ".pryrc",
    # Java
    ".gradle",
    ".m2/settings.xml",
    # Rust
    ".cargo/config.toml",
    ".rustup/settings.toml",
    # Go
    ".config/go",
    # DevOps
    ".ansible",
    ".ansible.cfg",
    ".config/podman",
    # AI tools (settings)
    ".claude/settings.json",
    ".claude/projects",
    ".codex/config.toml",
    ".codex/skills",
]

# Sensitive items (only backed up with encryption)
sensitive = [
    # SSH
    ".ssh",
    # GPG
    ".gnupg",
    # Cloud credentials
    ".aws",
    ".config/gcloud",
    ".azure",
    ".kube",
    ".s3cfg",
    ".yandex",
    # Terraform
    ".terraform.d",
    ".terraformrc",
    # Python credentials
    ".pypirc",
    # Docker (may contain registry auth)
    ".docker",
    # Shell history
    ".zsh_history",
    ".bash_history",
    ".lesshst",
    # AI tools (auth/tokens)
    ".claude.json",
    ".codex/auth.json",
    ".ai",
]

[backup]
# Where to store backups
backup_dir = "~/backups/dotfiles"

# Number of backups to keep (0 keeps all)
max_backups = 14

# Encryption: "age" | "gpg" | "none"
encryption = "none"

# Path to age recipients file (for age encryption)
# age_recipients = "~/.config/age/recipients.txt"

# Path to age identity files (for age decryption)
# age_identity_files = ["~/.config/age/keys.txt"]  # required for decrypting age backups

# GPG recipient (for GPG encryption)
# gpg_recipient = "your@email.com"

# Exclude patterns
[excludes]
patterns = [
    # General
    ".git",
    ".idea",
    "*.log",
    "*.swp",
    "*.bak",
    ".DS_Store",
    "*.sock",
    "*.cache",
    # CI/CD and dev artifacts
    ".circleci",
    ".github",
    ".travis.yml",
    ".gitlab-ci.yml",
    "Makefile",
    "Dockerfile",
    "*.md",
    "LICENSE*",
    "COPYING*",
    "Gemfile*",
    "*.spec",
    "*.rb",
    "test",
    "tests",
    "spec",
    ".editorconfig",
    ".gitignore",
    ".gitattributes",
    ".rspec",
    ".rubocop*",
    ".ruby-version",
    # Python
    "*.pyc",
    "__pycache__",
    ".venv",
    "venv",
    # Node
    "node_modules",
    # Java/Gradle/Maven
    ".gradle/caches",
    ".gradle/daemon",
    ".m2/repository",
    # Terraform
    "*.tfstate",
    "*.tfstate.*",
    # GPG transient
    "S.gpg-agent*",
    "random_seed",
    "*.status",
    # Docker transient
    ".token_seed*",
    "buildx/refs",
    "buildx/activity",
    "buildx/.lock",
    # SSH transient
    "known_hosts.old",
    # Zsh compiled
    "*.zwc",
    # Emacs
    "*~",
    "#*#",
    ".emacs.d/elpa",
    ".emacs.d/eln-cache",
    # Vim/Neovim
    ".config/nvim/lazy-lock.json",
    # Ruby version managers (large)
    ".rbenv/versions",
    ".rvm/gems",
    ".rvm/rubies",
    # oh-my-zsh cloned plugins artifacts
    "gitstatus/src",
    "gitstatus/deps",
    "gitstatus/usrbin",
    "*.png",
    "*.gif",
    "*.jpg",
    "*.svg",
    "test-data",
    "docs",
    # Misc dev files
    "*.sh",
    "DESCRIPTION",
    "URL",
    "VERSION",
    "ZSH_VERSIONS",
    ".revision-hash",
    ".version",
]

# Named profiles
# Use with: dotpak backup --profile work
# [profile.work]
# extra_items = [".config/slack"]

# Hostname-specific settings (applied automatically)
# [host.my-macbook]
# extra_items = [".config/work-specific"]
`
}
