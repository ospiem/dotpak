package backup

import (
	"archive/tar"
	"io"
	"os"

	"github.com/ospiem/dotpak/internal/archive"
	"github.com/ospiem/dotpak/internal/crypto"
)

// partialSuffix marks in-progress archives so an interrupted backup never
// leaves a truncated file at the final path.
const partialSuffix = ".partial"

// createArchive creates a tar.gz archive atomically: the stream is written to
// a temporary sibling and renamed into place only on success.
func (b *Backup) createArchive(archivePath string, files []FileInfo) error {
	tmpPath := archivePath + partialSuffix

	// create output file with restricted permissions
	outFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	writeErr := b.writeArchive(outFile, files)
	if closeErr := outFile.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(tmpPath)
		return writeErr
	}

	return os.Rename(tmpPath, archivePath)
}

// createEncryptedArchive streams a tar.gz archive directly into the encryptor,
// so that unencrypted data never touches disk. The encrypted output is also
// written to a temporary sibling and renamed into place on success.
func (b *Backup) createEncryptedArchive(outputPath string, files []FileInfo, enc crypto.Encryptor) error {
	tmpPath := outputPath + partialSuffix

	if err := archive.EncryptStream(enc, tmpPath, func(w io.Writer) error {
		return b.writeArchive(w, files)
	}); err != nil {
		return err // EncryptStream removes the partial file on failure
	}

	return os.Rename(tmpPath, outputPath)
}

// writeArchive writes a tar.gz stream to w from the collected files. Files
// that fail to be added are reported as warnings and counted in stats, so the
// final numbers reflect the archive's actual contents.
func (b *Backup) writeArchive(w io.Writer, files []FileInfo) error {
	return archive.WriteTarGz(w, func(tw *tar.Writer) error {
		added := 0
		for i, f := range files {
			b.out.Progress(i+1, len(files), f.RelPath)

			if addErr := archive.AddFileToTar(tw, f.FullPath, f.RelPath); addErr != nil {
				b.out.Warning("Skipping %s: %v\n", f.RelPath, addErr)
				b.stats.FilesSkipped++
				continue
			}
			added++
		}

		b.out.ClearProgress()
		b.stats.FilesBackedUp = added
		return nil
	})
}
