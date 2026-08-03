// Package osutils provides shared utility functions for dotpak.
package osutils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FormatSize formats a byte size as a human-readable string.
func FormatSize(size int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)

	switch {
	case size >= gb:
		return fmt.Sprintf("%.2f GB", float64(size)/gb)
	case size >= mb:
		return fmt.Sprintf("%.2f MB", float64(size)/mb)
	case size >= kb:
		return fmt.Sprintf("%.2f KB", float64(size)/kb)
	default:
		return fmt.Sprintf("%d bytes", size)
	}
}

// HomeDir returns the user's home directory or an error.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return home, nil
}

// ExpandPath expands a leading "~" or "~/" to the user's home directory.
// The path is returned unchanged if home cannot be determined.
func ExpandPath(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := HomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// Hostname returns the hostname without the domain part, or an error.
func Hostname() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("getting hostname: %w", err)
	}
	for i := range len(hostname) {
		if hostname[i] == '.' {
			return hostname[:i], nil
		}
	}
	return hostname, nil
}
