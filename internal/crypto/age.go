package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// AgeEncryptor implements Encryptor using age.
type AgeEncryptor struct {
	recipientsFile string
	identityFiles  []string
}

// NewAgeEncryptor creates a new AgeEncryptor.
func NewAgeEncryptor(opts Options) (*AgeEncryptor, error) {
	enc := &AgeEncryptor{
		recipientsFile: opts.AgeRecipientsFile,
		identityFiles:  opts.AgeIdentityFiles,
	}
	return enc, nil
}

// Available returns true if age is installed.
func (e *AgeEncryptor) Available() bool {
	return HasAge()
}

// Encrypt encrypts a file using age.
func (e *AgeEncryptor) Encrypt(inputPath string) (string, error) {
	if e.recipientsFile == "" {
		return "", errors.New("age recipients file not specified")
	}

	if _, err := os.Stat(e.recipientsFile); err != nil {
		return "", fmt.Errorf("age recipients file not found: %s", e.recipientsFile)
	}

	outputPath := inputPath + ".age"

	//nolint:gosec // G204: age command with validated recipients file path
	cmd := exec.Command("age", "-e", "-R", e.recipientsFile, "-o", outputPath, inputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("age encryption failed: %s", stderr.String())
	}

	return outputPath, nil
}

// Decrypt decrypts a file using age.
func (e *AgeEncryptor) Decrypt(inputPath, outputPath string) error {
	identityFile, err := e.findIdentityFile()
	if err != nil {
		return err
	}

	cmd := exec.Command("age", "-d", "-i", identityFile, "-o", outputPath, inputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("age decryption failed: %s", stderr.String())
	}

	return nil
}

func (e *AgeEncryptor) findIdentityFile() (string, error) {
	if len(e.identityFiles) == 0 {
		return "", errors.New("no age identity files configured")
	}

	for _, loc := range e.identityFiles {
		if _, err := os.Stat(loc); err == nil {
			return loc, nil
		}
	}

	return "", fmt.Errorf("age identity file not found in configured locations: %v", e.identityFiles)
}
