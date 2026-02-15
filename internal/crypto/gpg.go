package crypto

import (
	"bytes"
	"fmt"
	"io"
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

// EncryptReader encrypts data from r and writes the result to outputPath.
func (e *GPGEncryptor) EncryptReader(r io.Reader, outputPath string) error {
	args := []string{"--batch", "--encrypt", "--output", outputPath}
	if e.recipient != "" {
		args = append(args, "--recipient", e.recipient)
	}

	cmd := exec.Command("gpg", args...)
	cmd.Stdin = r
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gpg encryption failed: %s", stderr.String())
	}

	return nil
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
