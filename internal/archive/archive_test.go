package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"strings"
	"testing"
)

func readEntries(t *testing.T, r io.Reader) map[string]string {
	t.Helper()

	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	entries := map[string]string{}
	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		data, readErr := io.ReadAll(tr)
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries[hdr.Name] = string(data)
	}
	return entries
}

func writeHeader(t *testing.T, tw *tar.Writer, name string, size int64) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: size}); err != nil {
		t.Fatal(err)
	}
}

// A file that grows after it was sized must come back as ErrTruncated with a
// complete entry and an intact stream: append-only files like shell history
// grow during every backup, and they are archived, not skipped.
func TestWriteEntryBody_FileGrew(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := WriteTarGz(&buf, func(tw *tar.Writer) error {
		writeHeader(t, tw, "history", 5)
		bodyErr := writeEntryBody(tw, strings.NewReader("1234567890"), 5, "history")
		if !errors.Is(bodyErr, ErrTruncated) {
			t.Fatalf("writeEntryBody() error = %v, want ErrTruncated", bodyErr)
		}
		writeHeader(t, tw, "after", 2)
		return writeEntryBody(tw, strings.NewReader("ok"), 2, "after")
	})
	if err != nil {
		t.Fatalf("WriteTarGz() error = %v", err)
	}

	entries := readEntries(t, &buf)
	if entries["history"] != "12345" {
		t.Errorf("truncated entry = %q, want the first 5 bytes", entries["history"])
	}
	if entries["after"] != "ok" {
		t.Errorf("entry after the truncated one = %q, want %q", entries["after"], "ok")
	}
}

// A file that shrank is zero-padded (so later entries survive) and reported
// as an ordinary error: its content is not trustworthy.
func TestWriteEntryBody_FileShrank(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := WriteTarGz(&buf, func(tw *tar.Writer) error {
		writeHeader(t, tw, "shrunk", 5)
		bodyErr := writeEntryBody(tw, strings.NewReader("12"), 5, "shrunk")
		if bodyErr == nil || errors.Is(bodyErr, ErrTruncated) {
			t.Fatalf("writeEntryBody() error = %v, want a non-truncation error", bodyErr)
		}
		writeHeader(t, tw, "after", 2)
		return writeEntryBody(tw, strings.NewReader("ok"), 2, "after")
	})
	if err != nil {
		t.Fatalf("WriteTarGz() error = %v", err)
	}

	entries := readEntries(t, &buf)
	if entries["shrunk"] != "12\x00\x00\x00" {
		t.Errorf("shrunk entry = %q, want zero-padded content", entries["shrunk"])
	}
	if entries["after"] != "ok" {
		t.Errorf("entry after the shrunk one = %q, want %q", entries["after"], "ok")
	}
}
