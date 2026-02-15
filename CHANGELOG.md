# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-02-15

### Security

- **Plist redesign**: launchd now calls `dotpak cron run` directly instead of wrapping in `/bin/sh -c "..."` — Full Disk Access only needs to be granted to the dotpak binary, not to `/bin/sh` (which would give FDA to all shell scripts)
- **Streaming encryption**: encrypted backups are now piped directly from tar.gz into age/gpg — unencrypted sensitive data never touches disk
- **Secure temp files**: decrypted archives use `~/.cache/dotpak/tmp/` (0700) instead of system `/tmp`, reducing exposure if the process crashes
- **Symlink safety**: `filepath.Walk` replaced with `filepath.WalkDir` — symlinked directories are no longer followed recursively, preventing silent inclusion of files outside `$HOME`
- **GPG batch mode**: `--batch` flag added to GPG encryption to prevent interactive prompts in cron/launchd context

### Added

- Hidden `cron run` subcommand that handles log appending and timestamps internally (used by launchd/cron)
- `EncryptReader` method on `Encryptor` interface for streaming encryption via stdin pipe
- `utils.CreateTempFile()` / `utils.TempDir()` helpers for secure temporary files
- `cron status` subcommand with FDA check and launchd/cron status display
- `HOME` environment variable in launchd plist (launchd doesn't inherit it)

### Changed

- Exported `backup.AddFileToTar` and removed duplicate `addFileToSafetyBackup` from restore package
- Safety backups now also use streaming encryption (no intermediate unencrypted file)
- FDA check uses read-only `os.Open` instead of creating test files
- Symlink handling extended to file symlinks (not just directory symlinks); `SkipDir` replaced with `nil` return to avoid skipping siblings

### Removed

- `Encrypt(inputPath string)` method from `Encryptor` interface (replaced by `EncryptReader`)
- macOS app bundle (FDA granted to binary directly)

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
