// Package archive provides shared tar.gz writing helpers used by both the
// backup and restore (safety backup) paths.
package archive

import (
	"archive/tar"
	"compress/gzip"
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

	written, err := io.Copy(tw, file)
	if err != nil {
		return fmt.Errorf("adding %s: %w", relPath, err)
	}

	// A file that shrank between Lstat and read would otherwise leave the tar
	// stream short, poisoning every subsequent WriteHeader. Pad the entry to
	// the declared size so the archive stays consistent, and report the file.
	if written < header.Size {
		if padErr := padTarEntry(tw, header.Size-written); padErr != nil {
			return fmt.Errorf("padding %s after size change: %w", relPath, padErr)
		}
		return fmt.Errorf("%s changed while reading: wrote %d of %d bytes (entry zero-padded)",
			relPath, written, header.Size)
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
