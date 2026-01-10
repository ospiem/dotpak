package restore

import (
	"strings"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/crypto"
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
