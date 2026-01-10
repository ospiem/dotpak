package crypto

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

// GPGEncryptor implements Encryptor using GPG.
type GPGEncryptor struct {
	recipient string
}

// NewGPGEncryptor creates a new GPGEncryptor.
func NewGPGEncryptor(opts Options) (*GPGEncryptor, error) {
	return &GPGEncryptor{
		recipient: opts.GPGRecipient,
	}, nil
}

// Available returns true if gpg is installed.
func (e *GPGEncryptor) Available() bool {
	return HasGPG()
}

// Encrypt encrypts a file using GPG.
func (e *GPGEncryptor) Encrypt(inputPath string) (string, error) {
	outputPath := inputPath + ".gpg"

	args := []string{"--encrypt", "--output", outputPath}
	if e.recipient != "" {
		args = append(args, "--recipient", e.recipient)
	}
	args = append(args, inputPath)

	cmd := exec.Command("gpg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gpg encryption failed: %s", stderr.String())
	}

	return outputPath, nil
}

// Decrypt decrypts a file using GPG.
func (e *GPGEncryptor) Decrypt(inputPath, outputPath string) error {
	cmd := exec.Command("gpg", "--decrypt", "--output", outputPath, inputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdin = os.Stdin // allow passphrase input

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gpg decryption failed: %s", stderr.String())
	}

	return nil
}
