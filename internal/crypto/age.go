package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// AgeEncryptor implements Encryptor using age.
type AgeEncryptor struct {
	recipientsFile string
	identityFiles  []string
}

// NewAgeEncryptor creates a new AgeEncryptor.
func NewAgeEncryptor(opts Options) *AgeEncryptor {
	return &AgeEncryptor{
		recipientsFile: opts.AgeRecipientsFile,
		identityFiles:  opts.AgeIdentityFiles,
	}
}

// Available returns true if age is installed.
func (e *AgeEncryptor) Available() bool {
	return HasAge()
}

// EncryptReader encrypts data from r and writes the result to outputPath.
func (e *AgeEncryptor) EncryptReader(r io.Reader, outputPath string) error {
	if e.recipientsFile == "" {
		return errors.New("age recipients file not specified")
	}

	if _, err := os.Stat(e.recipientsFile); err != nil {
		return fmt.Errorf("age recipients file not found: %s", e.recipientsFile)
	}

	//nolint:gosec // g204: age command with validated recipients file path
	cmd := exec.Command("age", "-e", "-R", e.recipientsFile, "-o", outputPath)
	cmd.Stdin = r
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return cmdError("age encryption failed", err, &stderr)
	}

	return nil
}

// DecryptReader streams the decrypted contents of inputPath.
func (e *AgeEncryptor) DecryptReader(inputPath string) (io.ReadCloser, error) {
	identityFile, err := e.findIdentityFile()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("age", "-d", "-i", identityFile, "--", inputPath)
	return startCmdReader("age decryption failed", cmd)
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
