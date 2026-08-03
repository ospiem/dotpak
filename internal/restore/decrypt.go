package restore

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/crypto"
	"github.com/ospiem/dotpak/internal/osutils"
)

// openArchiveStream returns a reader over the (possibly decrypted) tar.gz
// stream of archivePath. Decryption is streamed, so plaintext never touches
// disk. The caller must Close the reader and check the error: for encrypted
// archives it reports decryption failure.
func openArchiveStream(archivePath string, identityFiles []string) (io.ReadCloser, error) {
	method := crypto.DetectMethod(archivePath)
	if method == crypto.MethodNone {
		return os.Open(archivePath)
	}

	enc, err := crypto.NewEncryptor(method, crypto.Options{
		AgeIdentityFiles: identityFiles,
	})
	if err != nil {
		return nil, err
	}
	return enc.DecryptReader(archivePath)
}

// closeArchiveStream closes the archive stream and folds its verdict into
// readErr. For encrypted archives the Close error IS the decryption verdict
// (the tool's exit status plus its stderr), so it must not stay hidden behind
// the bare "EOF" that reading a stream the tool never filled produces.
func closeArchiveStream(rc io.Closer, readErr error) error {
	closeErr := rc.Close()
	switch {
	case closeErr == nil:
		return readErr
	case readErr == nil, maskedByDecryption(readErr):
		return closeErr
	default:
		// a genuine archive problem: keep it, but do not drop the exit status
		return fmt.Errorf("%w (closing archive stream: %w)", readErr, closeErr)
	}
}

// maskedByDecryption reports whether err is the downstream symptom of a broken
// decryption — an empty or garbage stream — rather than a real archive problem.
func maskedByDecryption(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, gzip.ErrHeader) ||
		errors.Is(err, gzip.ErrChecksum)
}

// ResolveAgeIdentity resolves identity files from a CLI override or config.
// When override is "-", the identity is read from stdin; any other value is
// used as a path directly (so process substitution works); "" falls back to
// the config.
//
// A stdin identity is staged in a 0600 file under ~/.cache/dotpak/tmp: one run
// decrypts the archive more than once (safety-backup scan, then extraction)
// while stdin can only be read once. The returned cleanup removes that file.
func ResolveAgeIdentity(override string, cfg *config.Config) ([]string, func(), error) {
	noop := func() {}

	if override == "-" {
		tmpFile, err := osutils.CreateTempFile("dotpak-identity-*")
		if err != nil {
			return nil, noop, fmt.Errorf("creating temp file for identity: %w", err)
		}

		path := tmpFile.Name()
		cleanup := func() { _ = os.Remove(path) }

		if _, copyErr := io.Copy(tmpFile, os.Stdin); copyErr != nil {
			_ = tmpFile.Close()
			cleanup()
			return nil, noop, fmt.Errorf("reading identity from stdin: %w", copyErr)
		}
		if closeErr := tmpFile.Close(); closeErr != nil {
			cleanup()
			return nil, noop, fmt.Errorf("writing identity file: %w", closeErr)
		}

		return []string{path}, cleanup, nil
	}

	if override != "" {
		return []string{override}, noop, nil
	}

	return resolveAgeIdentityFiles(cfg), noop, nil
}

// resolveAgeIdentityFor resolves the identity files needed to read archivePath.
// Archives that are not age-encrypted need none, so stdin is left untouched for
// them.
func resolveAgeIdentityFor(archivePath, override string, cfg *config.Config) ([]string, func(), error) {
	if crypto.DetectMethod(archivePath) != crypto.MethodAge {
		return nil, func() {}, nil
	}
	return ResolveAgeIdentity(override, cfg)
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
