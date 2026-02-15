package backup

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ospiem/dotpak/internal/crypto"
)

// createArchive creates a tar.gz archive from the collected files.
func (b *Backup) createArchive(archivePath string, files []FileInfo) (err error) {
	// create output file with restricted permissions
	outFile, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := outFile.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	return b.writeArchive(outFile, files)
}

// createEncryptedArchive streams a tar.gz archive directly into the encryptor,
// so that unencrypted data never touches disk.
func (b *Backup) createEncryptedArchive(outputPath string, files []FileInfo, enc crypto.Encryptor) error {
	pr, pw := io.Pipe()

	errCh := make(chan error, 1)
	go func() {
		errCh <- b.writeArchive(pw, files)
		_ = pw.Close()
	}()

	if err := enc.EncryptReader(pr, outputPath); err != nil {
		_ = pr.Close() // unblock the writer goroutine
		if writeErr := <-errCh; writeErr != nil {
			return fmt.Errorf("%w; write error: %w", err, writeErr)
		}
		return err
	}

	return <-errCh
}

// writeArchive writes a tar.gz stream to w from the collected files.
func (b *Backup) writeArchive(w io.Writer, files []FileInfo) (err error) {
	// create gzip writer
	gzWriter := gzip.NewWriter(w)
	defer func() {
		if cerr := gzWriter.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// create tar writer
	tarWriter := tar.NewWriter(gzWriter)
	defer func() {
		if cerr := tarWriter.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	// add each file
	for i, f := range files {
		b.out.Progress(i+1, len(files), f.RelPath)

		if addErr := AddFileToTar(tarWriter, f.FullPath, f.RelPath); addErr != nil {
			b.out.Verbose("Failed to add %s: %v\n", f.RelPath, addErr)
			continue
		}
	}

	b.out.ClearProgress()
	return nil
}

// AddFileToTar adds a single file (or symlink) to a tar writer.
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

	// regular file handling
	file, err := os.Open(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// create tar header
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}

	// use relative path as name
	header.Name = filepath.ToSlash(relPath)

	// write header
	if err = tw.WriteHeader(header); err != nil {
		return err
	}

	// write file content
	if _, err = io.Copy(tw, file); err != nil {
		return err
	}

	return nil
}
