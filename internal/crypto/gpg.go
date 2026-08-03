package crypto

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
)

// GPGEncryptor implements Encryptor using GPG.
type GPGEncryptor struct {
	recipient string
}

// NewGPGEncryptor creates a new GPGEncryptor.
func NewGPGEncryptor(opts Options) *GPGEncryptor {
	return &GPGEncryptor{
		recipient: opts.GPGRecipient,
	}
}

// Available returns true if gpg is installed.
func (e *GPGEncryptor) Available() bool {
	return HasGPG()
}

// EncryptReader encrypts data from r and writes the result to outputPath.
func (e *GPGEncryptor) EncryptReader(r io.Reader, outputPath string) error {
	if e.recipient == "" {
		return errors.New("gpg recipient not specified")
	}

	//nolint:gosec // g204: recipient is a gpg key id from config, passed as a flag argument
	cmd := exec.Command("gpg", "--batch", "--yes", "--encrypt", "--output", outputPath, "--recipient", e.recipient)
	cmd.Stdin = r
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return cmdError("gpg encryption failed", err, &stderr)
	}

	return nil
}

// DecryptReader streams the decrypted contents of inputPath. Passphrase entry
// goes through the gpg agent/pinentry; --batch keeps gpg from blocking on a
// tty so non-interactive runs fail cleanly instead of hanging.
func (e *GPGEncryptor) DecryptReader(inputPath string) (io.ReadCloser, error) {
	cmd := exec.Command("gpg", "--batch", "--decrypt", "--", inputPath)
	return startCmdReader("gpg decryption failed", cmd)
}
