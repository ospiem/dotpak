// Package crypto provides encryption and decryption functionality using age and GPG.
package crypto

import (
	"errors"
	"io"
	"os/exec"
	"strings"
)

// Method represents an encryption method.
type Method string

const (
	// MethodNone represents no encryption.
	MethodNone Method = ""
	// MethodAge represents age encryption.
	MethodAge Method = "age"
	// MethodGPG represents GPG encryption.
	MethodGPG Method = "gpg"
)

// Encryptor defines the interface for encryption/decryption operations.
type Encryptor interface {
	// EncryptReader encrypts data from r and writes the result to outputPath.
	EncryptReader(r io.Reader, outputPath string) error
	// Decrypt decrypts inputPath to outputPath.
	Decrypt(inputPath, outputPath string) error
	// Available returns true if the encryption tool is available.
	Available() bool
}

// Options holds configuration for encryption/decryption.
type Options struct {
	// AgeRecipientsFile is the path to the age recipients file (for encryption).
	AgeRecipientsFile string
	// AgeIdentityFiles is a list of paths to age identity files (for decryption).
	AgeIdentityFiles []string
	// GPGRecipient is the GPG recipient ID or email.
	GPGRecipient string
}

// DetectMethod detects the encryption method from a file path based on its extension.
func DetectMethod(filePath string) Method {
	if strings.HasSuffix(filePath, ".age") {
		return MethodAge
	}
	if strings.HasSuffix(filePath, ".gpg") {
		return MethodGPG
	}
	return MethodNone
}

// NewEncryptor creates a new Encryptor for the specified method.
func NewEncryptor(method Method, opts Options) (Encryptor, error) {
	switch method {
	case MethodAge:
		return NewAgeEncryptor(opts)
	case MethodGPG:
		return NewGPGEncryptor(opts)
	case MethodNone:
		return nil, errors.New("no encryption method specified")
	default:
		return nil, errors.New("unknown encryption method: " + string(method))
	}
}

// HasAge checks if age is available on the system.
func HasAge() bool {
	_, err := exec.LookPath("age")
	return err == nil
}

// HasGPG checks if gpg is available on the system.
func HasGPG() bool {
	_, err := exec.LookPath("gpg")
	return err == nil
}
