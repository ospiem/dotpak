# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0] - 2026-08-03

### Security

- **Extraction containment**: entry parents are resolved with `EvalSymlinks` before writing and files are opened with remove-then-`O_EXCL`, so a crafted archive can no longer escape `$HOME` or write through a pre-existing symlink
- **Private by default**: restored parent directories are created 0700 and files are chmod'ed to the exact archive mode, so a restored `~/.ssh` or `~/.gnupg` is never world-readable
- **Safety backup is mandatory**: failing to create the pre-restore archive aborts the restore instead of continuing (`--no-backup` opts out); sensitive files are never silently written unencrypted — interactive prompt in normal mode, hard error under `--json`/`--quiet`
- **Streaming decryption**: decrypted archives no longer land in a temp file on the restore, diff, and contents paths
- **Decompression limits**: 1GB per entry and 10GB per archive

### Changed

- **BREAKING**: `restore` with `--json` or `--quiet` requires `--force` — it used to overwrite existing files without any confirmation
- `restore --age-identity -` requires `--force` (or `--dry-run`): the identity arrives on stdin, so the confirmation prompt would consume the piped key
- Warnings go to stderr in every mode, keeping them visible under `--quiet`/`--json` without corrupting JSON on stdout
- Config files are decoded over the defaults: omitted keys keep their default, and `max_backups = 0` now means "keep all" instead of falling back to the default
- Errors are reported exactly once (cobra's duplicate error and usage output is silenced)
- `internal/schedule` (launchd/crontab) and `internal/pkgrestore` (brew/apt/go) split out of `main.go`; tar and encryption plumbing shared via `internal/archive`

### Fixed

- FIFOs, sockets, and devices are skipped during backup — opening a FIFO with no writer blocked scheduled backups forever
- Archives are written to a `.partial` sibling and renamed into place, so an interrupted backup leaves no truncated file
- Item paths written as `~/…`, `$HOME/…`, or absolute under home are normalized instead of breaking the build
- A failed decryption reports the tool's verdict (`age: no identity matched…`) instead of a bare `EOF` from the emptied stream
- GPG decryption uses `--batch`, so it no longer hangs on an overwrite prompt in cron context
- Missing binaries and non-zero exits from `age`/`gpg` keep the underlying error and captured stderr
- Backup no longer reports success when the archive could not be written; per-file failures are counted honestly

## [0.3.0] - 2026-03-22

### Added

- `--age-identity` flag for `diff`, `restore`, and `contents` commands — allows passing an age identity file path or `-` to read from stdin
- Support for encrypted age identity files (e.g. protected by `age-plugin-yubikey`) via pipe: `age -d -i yubikey-identity encrypted-key.age | dotpak diff --age-identity - archive.age`

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
