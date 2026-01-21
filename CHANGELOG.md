# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.2] - 2026-01-21

### Added

- macOS app bundle for Full Disk Access permission (required for scheduled backups to protected directories)
- `make app-bundle` target to build Dotpak.app locally
- CI test job runs before build in release workflow

## [0.1.1] - 2026-01-21

### Removed

- Removed outdated references to checksum and verify command from documentation

## [0.1.0] - 2026-01-20

Initial public release.

### Features

- **Two-tier backup system**: Regular items (always backed up) and sensitive files (only when encrypted)
- **Encryption**: Automatic detection of age or GPG with manual override options
- **Backup**: Create tar.gz archives with optional encryption
- **Restore**: Full or selective restoration by category
- **Scheduling**: Daily backups via launchd (macOS) or cron (Linux)
- **Profiles**: Named configurations for different environments
- **Hostname-aware**: Automatic per-machine configuration overrides
- **Safety backups**: Creates backup of existing files before restore (encrypted if source was)
- **Homebrew integration**: Backs up Brewfile and Mac App Store apps list
- **JSON output**: All commands support `--json` for scripting

### Commands

```
backup      Create backup archive
restore     Restore from archive
list        List available backups
diff        Show differences with current files
contents    List archive contents
config      Manage configuration (init, validate)
cron        Manage scheduled backups (install, uninstall)
version     Show version info
```

### Defaults

Includes 50+ common dotfiles for shell, editors, git, terminal emulators, and development tools. Run `dotpak config init` to see the full list.
