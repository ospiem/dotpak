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

func TestHasAge(t *testing.T) {
	t.Parallel()
	// just verify it doesn't panic
	_ = HasAge()
}

func TestHasGPG(t *testing.T) {
	t.Parallel()
	// just verify it doesn't panic
	_ = HasGPG()
}

func TestNewEncryptor(t *testing.T) {
	t.Parallel()

	t.Run("age encryptor", func(t *testing.T) {
		enc, err := NewEncryptor(MethodAge, Options{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if enc == nil {
			t.Error("expected non-nil encryptor")
		}
		if _, ok := enc.(*AgeEncryptor); !ok {
			t.Error("expected AgeEncryptor type")
		}
	})

	t.Run("gpg encryptor", func(t *testing.T) {
		enc, err := NewEncryptor(MethodGPG, Options{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if enc == nil {
			t.Error("expected non-nil encryptor")
		}
		if _, ok := enc.(*GPGEncryptor); !ok {
			t.Error("expected GPGEncryptor type")
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

func TestAgeEncryptor_Available(t *testing.T) {
	t.Parallel()

	enc, err := NewAgeEncryptor(Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// result depends on whether age is installed
	_ = enc.Available()
}

func TestAgeEncryptor_EncryptReaderWithoutRecipients(t *testing.T) {
	t.Parallel()

	enc, err := NewAgeEncryptor(Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = enc.EncryptReader(strings.NewReader("test"), "/tmp/test.tar.gz.age")
	if err == nil {
		t.Error("expected error when recipients file not specified")
	}
}

func TestAgeEncryptor_EncryptReaderWithNonexistentRecipients(t *testing.T) {
	t.Parallel()

	enc, err := NewAgeEncryptor(Options{
		AgeRecipientsFile: "/nonexistent/recipients.txt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = enc.EncryptReader(strings.NewReader("test"), "/tmp/test.tar.gz.age")
	if err == nil {
		t.Error("expected error when recipients file not found")
	}
}

func TestAgeEncryptor_DecryptWithNoIdentity(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	enc, err := NewAgeEncryptor(Options{
		AgeIdentityFiles: []string{
			filepath.Join(tmpDir, "nonexistent1.txt"),
			filepath.Join(tmpDir, "nonexistent2.txt"),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = enc.Decrypt("/tmp/test.tar.gz.age", "/tmp/output.tar.gz")
	if err == nil {
		t.Error("expected error when no identity file found")
	}
}

func TestGPGEncryptor_Available(t *testing.T) {
	t.Parallel()

	enc, err := NewGPGEncryptor(Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// result depends on whether gpg is installed
	_ = enc.Available()
}

func TestOptions(t *testing.T) {
	t.Parallel()

	opts := Options{
		AgeRecipientsFile: "/path/to/recipients.txt",
		AgeIdentityFiles:  []string{"/path/to/identity1.txt", "/path/to/identity2.txt"},
		GPGRecipient:      "user@example.com",
	}

	if opts.AgeRecipientsFile != "/path/to/recipients.txt" {
		t.Errorf("unexpected AgeRecipientsFile: %s", opts.AgeRecipientsFile)
	}
	if len(opts.AgeIdentityFiles) != 2 {
		t.Errorf("expected 2 identity files, got %d", len(opts.AgeIdentityFiles))
	}
	if opts.GPGRecipient != "user@example.com" {
		t.Errorf("unexpected GPGRecipient: %s", opts.GPGRecipient)
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

	enc, err := NewAgeEncryptor(Options{
		AgeIdentityFiles: []string{
			filepath.Join(tmpDir, "nonexistent.txt"),
			identityFile,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

	enc, err := NewAgeEncryptor(Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// no default identity files should be populated - user must explicitly configure them
	if len(enc.identityFiles) != 0 {
		t.Errorf("expected no default identity files, got %d", len(enc.identityFiles))
	}
}
