package crypto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filePath string
		expected Method
	}{
		{"age file", "/path/to/backup.tar.gz.age", MethodAge},
		{"gpg file", "/path/to/backup.tar.gz.gpg", MethodGPG},
		{"unencrypted tar.gz", "/path/to/backup.tar.gz", MethodNone},
		{"plain file", "/path/to/file.txt", MethodNone},
		{"age in path but not extension", "/path/age/file.tar.gz", MethodNone},
		{"gpg in path but not extension", "/path/gpg/file.tar.gz", MethodNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectMethod(tt.filePath)
			if result != tt.expected {
				t.Errorf("DetectMethod(%q) = %q, want %q", tt.filePath, result, tt.expected)
			}
		})
	}
}

func TestMethodExtension(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method Method
		want   string
	}{
		{MethodAge, ".age"},
		{MethodGPG, ".gpg"},
		{MethodNone, ""},
	}

	for _, tt := range tests {
		if got := tt.method.Extension(); got != tt.want {
			t.Errorf("Method(%q).Extension() = %q, want %q", tt.method, got, tt.want)
		}
	}
}

func TestIsEncryptedPath(t *testing.T) {
	t.Parallel()

	if !IsEncryptedPath("backup.tar.gz.age") {
		t.Error("expected .age path to be detected as encrypted")
	}
	if !IsEncryptedPath("backup.tar.gz.gpg") {
		t.Error("expected .gpg path to be detected as encrypted")
	}
	if IsEncryptedPath("backup.tar.gz") {
		t.Error("expected .tar.gz path to be detected as unencrypted")
	}
}

func TestNewEncryptor(t *testing.T) {
	t.Parallel()

	t.Run("age encryptor", func(t *testing.T) {
		enc, err := NewEncryptor(MethodAge, Options{})
		if HasAge() {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if _, ok := enc.(*AgeEncryptor); !ok {
				t.Error("expected AgeEncryptor type")
			}
		} else if err == nil {
			t.Error("expected error when age is not installed")
		}
	})

	t.Run("gpg encryptor", func(t *testing.T) {
		enc, err := NewEncryptor(MethodGPG, Options{})
		if HasGPG() {
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if _, ok := enc.(*GPGEncryptor); !ok {
				t.Error("expected GPGEncryptor type")
			}
		} else if err == nil {
			t.Error("expected error when gpg is not installed")
		}
	})

	t.Run("none method returns error", func(t *testing.T) {
		enc, err := NewEncryptor(MethodNone, Options{})
		if err == nil {
			t.Error("expected error for MethodNone")
		}
		if enc != nil {
			t.Error("expected nil encryptor")
		}
	})

	t.Run("unknown method returns error", func(t *testing.T) {
		enc, err := NewEncryptor(Method("unknown"), Options{})
		if err == nil {
			t.Error("expected error for unknown method")
		}
		if enc != nil {
			t.Error("expected nil encryptor")
		}
	})
}

func TestAgeEncryptor_EncryptReaderWithoutRecipients(t *testing.T) {
	t.Parallel()

	enc := NewAgeEncryptor(Options{})

	err := enc.EncryptReader(strings.NewReader("test"), filepath.Join(t.TempDir(), "test.tar.gz.age"))
	if err == nil {
		t.Error("expected error when recipients file not specified")
	}
}

func TestAgeEncryptor_EncryptReaderWithNonexistentRecipients(t *testing.T) {
	t.Parallel()

	enc := NewAgeEncryptor(Options{
		AgeRecipientsFile: "/nonexistent/recipients.txt",
	})

	err := enc.EncryptReader(strings.NewReader("test"), filepath.Join(t.TempDir(), "test.tar.gz.age"))
	if err == nil {
		t.Error("expected error when recipients file not found")
	}
}

func TestGPGEncryptor_EncryptReaderWithoutRecipient(t *testing.T) {
	t.Parallel()

	enc := NewGPGEncryptor(Options{})

	err := enc.EncryptReader(strings.NewReader("test"), filepath.Join(t.TempDir(), "test.tar.gz.gpg"))
	if err == nil {
		t.Error("expected error when gpg recipient not specified")
	}
}

func TestAgeEncryptor_DecryptReaderWithNoIdentity(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	enc := NewAgeEncryptor(Options{
		AgeIdentityFiles: []string{
			filepath.Join(tmpDir, "nonexistent1.txt"),
			filepath.Join(tmpDir, "nonexistent2.txt"),
		},
	})

	rc, err := enc.DecryptReader(filepath.Join(tmpDir, "test.tar.gz.age"))
	if err == nil {
		_ = rc.Close()
		t.Error("expected error when no identity file found")
	}
}

func TestMethod_Constants(t *testing.T) {
	t.Parallel()

	if MethodNone != "" {
		t.Errorf("MethodNone should be empty string, got %q", MethodNone)
	}
	if MethodAge != "age" {
		t.Errorf("MethodAge should be 'age', got %q", MethodAge)
	}
	if MethodGPG != "gpg" {
		t.Errorf("MethodGPG should be 'gpg', got %q", MethodGPG)
	}
}

func TestAgeEncryptor_FindIdentityFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	identityFile := filepath.Join(tmpDir, "identity.txt")
	if err := os.WriteFile(identityFile, []byte("AGE-SECRET-KEY-..."), 0600); err != nil {
		t.Fatalf("failed to create identity file: %v", err)
	}

	enc := NewAgeEncryptor(Options{
		AgeIdentityFiles: []string{
			filepath.Join(tmpDir, "nonexistent.txt"),
			identityFile,
		},
	})

	found, err := enc.findIdentityFile()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if found != identityFile {
		t.Errorf("expected %s, got %s", identityFile, found)
	}
}

func TestAgeEncryptor_NoDefaultIdentityFiles(t *testing.T) {
	t.Parallel()

	enc := NewAgeEncryptor(Options{})

	// no default identity files should be populated - user must explicitly configure them
	if len(enc.identityFiles) != 0 {
		t.Errorf("expected no default identity files, got %d", len(enc.identityFiles))
	}
}
