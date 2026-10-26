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
	cmd := exec.Command("age", "-e", "-R", e.recipientsFile)
	cmd.Stdin = r
	return runToFile("age encryption failed", cmd, outputPath)
}

// DecryptReader streams the decrypted contents of inputPath. Every configured
// identity that exists on disk is passed to age, so an archive encrypted for
// any of them decrypts.
func (e *AgeEncryptor) DecryptReader(inputPath string) (io.ReadCloser, error) {
	identityFiles, err := e.existingIdentityFiles()
	if err != nil {
		return nil, err
	}

	args := []string{"-d"}
	for _, identity := range identityFiles {
		args = append(args, "-i", identity)
	}
	args = append(args, "--", inputPath)

	cmd := exec.Command("age", args...)
	return startCmdReader("age decryption failed", cmd)
}

func (e *AgeEncryptor) existingIdentityFiles() ([]string, error) {
	if len(e.identityFiles) == 0 {
		return nil, errors.New("no age identity files configured")
	}

	var existing []string
	for _, loc := range e.identityFiles {
		if _, err := os.Stat(loc); err == nil {
			existing = append(existing, loc)
		}
	}
	if len(existing) == 0 {
		return nil, fmt.Errorf("age identity file not found in configured locations: %v", e.identityFiles)
	}
	return existing, nil
}

// runToFile runs cmd with its stdout connected to outputPath, created 0600 so
// the encrypted archive is never world-readable (the tools' own -o/--output
// would create it with the default umask). The file is removed on failure.
func runToFile(op string, cmd *exec.Cmd, outputPath string) (err error) {
	outFile, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer func() {
		if cerr := outFile.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("%s: %w", op, cerr)
		}
		if err != nil {
			_ = os.Remove(outputPath)
		}
	}()

	var stderr bytes.Buffer
	cmd.Stdout = outFile
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return cmdError(op, runErr, &stderr)
	}
	return nil
}
