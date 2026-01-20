# Contributing

## Setup

```bash
# macOS
brew install go age gnupg

# Linux
sudo apt install golang age gpg

# Build
git clone https://github.com/ospiem/dotpak.git
cd dotpak && make build
```

## Commands

```bash
make build      # Build
make test       # All tests
make lint       # Lint
make check      # Lint + test
```

## Structure

```
cmd/dotpak/         CLI
internal/
  config/           Config loading
  backup/           Backup logic
  restore/          Restore logic
  crypto/           age/gpg encryption
  metadata/         JSON metadata
  output/           Terminal output
tests/e2e/          E2E tests
```

## Adding Items

- Backup items: `internal/config/config.go` -> `DefaultConfig()` -> `Items`
- Sensitive items: same -> `Sensitive`
- Exclude patterns: same -> `Excludes.Patterns`
- Restore categories: `internal/restore/restore.go` -> `Categories`
