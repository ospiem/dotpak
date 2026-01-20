# dotpak

A simple, modern dotfiles backup tool for Unix systems. Single Go binary.

```
dotpak backup                    # backup dotfiles
dotpak restore                   # restore from latest
dotpak diff backup.tar.gz -v     # see what changed
```

## Features

- 📦 **Two-tier backup** — regular configs always, secrets only with encryption
- 🔐 **age & GPG** — modern encryption with automatic detection
- 🍺 **Homebrew/apt/Go** — backs up and restores your package lists
- 📅 **Scheduled backups** — launchd on macOS, cron on Linux
- 🎯 **Selective restore** — restore by category (shell, editor, cloud, etc.)
- 🔍 **Diff & verify** — compare archives with current files, check integrity

## Installation

| Platform             | Command                                                                   |
|----------------------|---------------------------------------------------------------------------|
| **macOS (Homebrew)** | `brew install ospiem/tap/dotpak`                                          |
| **Go**               | `go install github.com/ospiem/dotpak/cmd/dotpak@latest`                   |
| **From source**      | `git clone https://github.com/ospiem/dotpak && cd dotpak && make install` |

Or download binaries from [Releases](https://github.com/ospiem/dotpak/releases).

## Usage

```bash
dotpak config init              # creates ~/.config/dotpak/config.toml
dotpak backup                   # create backup
dotpak restore                  # restore from latest backup
dotpak restore --only shell,git # restore specific categories
dotpak restore --homebrew       # reinstall Homebrew packages
dotpak list                     # list available backups
dotpak diff <archive> -v        # show content differences
```

## Encryption

Sensitive files (`.ssh`, `.aws`, `.gnupg`, etc.) are **only backed up when encryption is enabled**.

```bash
# Setup age (recommended)
age-keygen -o ~/.config/age/keys.txt
age-keygen -y ~/.config/age/keys.txt > ~/.config/age/recipients.txt
dotpak backup  # auto-detects age
```

GPG also supported: `dotpak backup --encrypt gpg --gpg-recipient you@email.com`

## Configuration

`~/.config/dotpak/config.toml`:

```toml
# Regular configs (always backed up)
items = [".zshrc", ".gitconfig", ".config/nvim"]

# Secrets (only with encryption)
sensitive = [".ssh", ".aws", ".gnupg"]

[backup]
backup_dir = "~/backups/dotfiles"
max_backups = 7
encryption = "none"   # none | age | gpg

[excludes]
patterns = ["*.log", ".git", "node_modules"]
```

Run `dotpak config init` to generate a config with sensible defaults.

## Scheduled Backups

```bash
dotpak cron install --hour 15   # daily at 3pm
dotpak cron uninstall           # remove
```

Uses launchd on macOS, cron on Linux.

## Safety

- **Pre-restore backup** — before restoring, dotpak saves existing files to a safety archive
- **Encryption preserved** — safety backups are encrypted if the source was

## License

MIT