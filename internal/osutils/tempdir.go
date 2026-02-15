package osutils

import (
	"os"
	"path/filepath"
)

// TempDir returns a dotpak-specific temporary directory (~/.cache/dotpak/tmp/)
// with 0700 permissions. This avoids leaving decrypted sensitive data in the
// system /tmp if the process crashes.
func TempDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cache", "dotpak", "tmp")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// CreateTempFile creates a temporary file in TempDir() with 0600 permissions.
func CreateTempFile(pattern string) (*os.File, error) {
	dir, err := TempDir()
	if err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, pattern)
}
