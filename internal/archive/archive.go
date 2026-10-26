// Package archive provides shared tar.gz writing helpers used by both the
// backup and restore (safety backup) paths.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ospiem/dotpak/internal/crypto"
)

// WriteTarGz writes a tar.gz stream to w. The add callback receives the tar
// writer and appends entries; close errors of the tar and gzip writers are
// propagated because they can signal truncated archives.
func WriteTarGz(w io.Writer, add func(tw *tar.Writer) error) (err error) {
	gzWriter := gzip.NewWriter(w)
	defer func() {
		if cerr := gzWriter.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	tarWriter := tar.NewWriter(gzWriter)
	defer func() {
		if cerr := tarWriter.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	return add(tarWriter)
}

// EncryptStream pipes the output of write into enc so plaintext never touches
// disk, writing the encrypted result to outputPath. The partial output file is
// removed on failure.
func EncryptStream(enc crypto.Encryptor, outputPath string, write func(w io.Writer) error) error {
	pr, pw := io.Pipe()

	errCh := make(chan error, 1)
	go func() {
		errCh <- write(pw)
		_ = pw.Close()
	}()

	if err := enc.EncryptReader(pr, outputPath); err != nil {
		_ = pr.Close() // unblock the writer goroutine
		_ = os.Remove(outputPath)
		if writeErr := <-errCh; writeErr != nil {
			return fmt.Errorf("%w; write error: %w", err, writeErr)
		}
		return err
	}

	if writeErr := <-errCh; writeErr != nil {
		_ = os.Remove(outputPath)
		return writeErr
	}
	return nil
}

// ErrTruncated reports that a file grew while it was being read: the archive
// holds a complete entry with the first header.Size bytes, so callers may
// treat it as archived (with a warning) rather than skipped.
var ErrTruncated = errors.New("file grew while reading")

// AddFileToTar adds a single file (or symlink) to a tar writer. Non-regular
// files such as FIFOs, sockets, and devices are rejected: opening a FIFO with
// no writer would block the backup forever.
func AddFileToTar(tw *tar.Writer, fullPath, relPath string) error {
	// use Lstat to detect symlinks without following them
	info, err := os.Lstat(fullPath)
	if err != nil {
		return err
	}

	// handle symlinks
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, readErr := os.Readlink(fullPath)
		if readErr != nil {
			return readErr
		}
		header, headerErr := tar.FileInfoHeader(info, linkTarget)
		if headerErr != nil {
			return headerErr
		}
		header.Name = filepath.ToSlash(relPath)
		return tw.WriteHeader(header)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("skipping non-regular file (%s)", info.Mode())
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}

	// use relative path as name
	header.Name = filepath.ToSlash(relPath)

	if err = tw.WriteHeader(header); err != nil {
		return err
	}

	return writeEntryBody(tw, file, header.Size, relPath)
}

// writeEntryBody copies exactly size bytes of r into the current tar entry and
// keeps the stream consistent when the file changed size after it was sized.
func writeEntryBody(tw *tar.Writer, r io.Reader, size int64, relPath string) error {
	written, err := io.Copy(tw, r)
	if errors.Is(err, tar.ErrWriteTooLong) {
		// the file grew after Lstat (append-only files like shell history do
		// this routinely); tar wrote exactly size bytes, so the entry is
		// complete and the stream stays consistent
		return fmt.Errorf("%w: %s archived with the first %d bytes", ErrTruncated, relPath, size)
	}
	if err != nil {
		return fmt.Errorf("adding %s: %w", relPath, err)
	}

	// a file that shrank between Lstat and read would otherwise leave the tar
	// stream short, poisoning every subsequent WriteHeader. Pad the entry to
	// the declared size so the archive stays consistent, and report the file.
	if written < size {
		if padErr := padTarEntry(tw, size-written); padErr != nil {
			return fmt.Errorf("padding %s after size change: %w", relPath, padErr)
		}
		return fmt.Errorf("%s changed while reading: wrote %d of %d bytes (entry zero-padded)",
			relPath, written, size)
	}

	return nil
}

func padTarEntry(tw *tar.Writer, remaining int64) error {
	zeros := make([]byte, 32*1024)
	for remaining > 0 {
		chunk := min(int64(len(zeros)), remaining)
		n, err := tw.Write(zeros[:chunk])
		if err != nil {
			return err
		}
		remaining -= int64(n)
	}
	return nil
}
