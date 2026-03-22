package restore

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/crypto"
	"github.com/ospiem/dotpak/internal/osutils"
)

func decryptWithAge(inputPath, outputPath string, identityFiles []string) (string, error) {
	identityFiles = normalizeIdentityFiles(identityFiles)
	enc, err := crypto.NewAgeEncryptor(crypto.Options{
		AgeIdentityFiles: identityFiles,
	})
	if err != nil {
		return "", err
	}
	if err = enc.Decrypt(inputPath, outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

func decryptWithGPG(inputPath, outputPath string) (string, error) {
	enc, err := crypto.NewGPGEncryptor(crypto.Options{})
	if err != nil {
		return "", err
	}
	if err = enc.Decrypt(inputPath, outputPath); err != nil {
		return "", err
	}
	return outputPath, nil
}

// ResolveAgeIdentity resolves identity files from a CLI override or config.
// When override is "-", identity is read from stdin into a temp file.
// When override is a path, it is used directly (supports process substitution).
// Returns the identity file list and a cleanup function for temp files.
func ResolveAgeIdentity(override string, cfg *config.Config) ([]string, func(), error) {
	noop := func() {}

	if override == "-" {
		tmpFile, err := osutils.CreateTempFile("dotpak-identity-*")
		if err != nil {
			return nil, noop, fmt.Errorf("creating temp file for identity: %w", err)
		}

		if _, copyErr := io.Copy(tmpFile, os.Stdin); copyErr != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpFile.Name())
			return nil, noop, fmt.Errorf("reading identity from stdin: %w", copyErr)
		}
		_ = tmpFile.Close()

		path := tmpFile.Name()
		cleanup := func() { _ = os.Remove(path) }
		return []string{path}, cleanup, nil
	}

	if override != "" {
		return []string{override}, noop, nil
	}

	return resolveAgeIdentityFiles(cfg), noop, nil
}

func resolveAgeIdentityFiles(cfg *config.Config) []string {
	if cfg != nil && len(cfg.Backup.AgeIdentityFiles) > 0 {
		return normalizeIdentityFiles(cfg.Backup.AgeIdentityFiles)
	}
	return nil
}

func normalizeIdentityFiles(identityFiles []string) []string {
	if len(identityFiles) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(identityFiles))
	for _, path := range identityFiles {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		normalized = append(normalized, path)
	}
	return normalized
}
