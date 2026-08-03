// Package crypto provides encryption and decryption functionality using age and GPG.
package crypto

import (
	"bytes"
	"errors"
	"fmt"
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

// Extension returns the file extension (with leading dot) appended to
// archives encrypted with this method, or "" for MethodNone.
func (m Method) Extension() string {
	switch m {
	case MethodAge:
		return ".age"
	case MethodGPG:
		return ".gpg"
	case MethodNone:
		return ""
	default:
		return ""
	}
}

// Encryptor defines the interface for encryption/decryption operations.
type Encryptor interface {
	// EncryptReader encrypts data from r and writes the result to outputPath.
	EncryptReader(r io.Reader, outputPath string) error
	// DecryptReader streams the decrypted contents of inputPath. The caller
	// must Close the reader and check its error: a non-nil Close error means
	// decryption failed and data read so far must not be trusted.
	DecryptReader(inputPath string) (io.ReadCloser, error)
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

// DetectMethod detects the encryption method from a file path based on its
// extension. It is the single authority for mapping file names to methods.
func DetectMethod(filePath string) Method {
	switch {
	case strings.HasSuffix(filePath, MethodAge.Extension()):
		return MethodAge
	case strings.HasSuffix(filePath, MethodGPG.Extension()):
		return MethodGPG
	default:
		return MethodNone
	}
}

// IsEncryptedPath reports whether the file path has a recognized encryption extension.
func IsEncryptedPath(filePath string) bool {
	return DetectMethod(filePath) != MethodNone
}

// NewEncryptor creates a new Encryptor for the specified method. It fails if
// the corresponding tool is not installed, so callers get a clear error before
// any data is processed.
func NewEncryptor(method Method, opts Options) (Encryptor, error) {
	switch method {
	case MethodAge:
		if !HasAge() {
			return nil, errors.New("age is not installed (required for age encryption)")
		}
		return NewAgeEncryptor(opts), nil
	case MethodGPG:
		if !HasGPG() {
			return nil, errors.New("gpg is not installed (required for gpg encryption)")
		}
		return NewGPGEncryptor(opts), nil
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

// cmdError builds an error that keeps the exec error (so [exec.ErrNotFound]
// and exit codes stay inspectable) and appends captured stderr when present.
func cmdError(op string, err error, stderr *bytes.Buffer) error {
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		return fmt.Errorf("%s: %w", op, err)
	}
	return fmt.Errorf("%s: %w: %s", op, err, msg)
}

// startCmdReader starts cmd and returns a reader over its stdout. Close reaps
// the process and reports its exit status.
func startCmdReader(op string, cmd *exec.Cmd) (io.ReadCloser, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if startErr := cmd.Start(); startErr != nil {
		return nil, cmdError(op, startErr, &stderr)
	}
	return &cmdReader{op: op, cmd: cmd, stdout: stdout, stderr: &stderr}, nil
}

type cmdReader struct {
	op      string
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	stderr  *bytes.Buffer
	closed  bool
	waitErr error
}

func (c *cmdReader) Read(p []byte) (int, error) {
	return c.stdout.Read(p)
}

func (c *cmdReader) Close() error {
	if c.closed {
		return c.waitErr
	}
	c.closed = true
	// closing stdout unblocks the subprocess if the consumer stopped early
	_ = c.stdout.Close()
	if err := c.cmd.Wait(); err != nil {
		c.waitErr = cmdError(c.op, err, c.stderr)
	}
	return c.waitErr
}
