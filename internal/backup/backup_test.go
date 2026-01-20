package backup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/output"
)

type testSetup struct {
	homeDir   string
	backupDir string
	cleanup   func()
}

func setupTest(t *testing.T) *testSetup {
	t.Helper()

	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	backupDir := filepath.Join(tmpDir, "backups")

	if err := os.MkdirAll(homeDir, 0755); err != nil {
		t.Fatalf("Failed to create home directory: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Failed to create backup directory: %v", err)
	}

	return &testSetup{
		homeDir:   homeDir,
		backupDir: backupDir,
		cleanup:   func() {}, // t.TempDir() handles cleanup
	}
}

func createTestFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	opts := &Options{}
	out := output.New(output.ModeQuiet, false)

	b := New(cfg, opts, out)

	if b == nil {
		t.Fatal("expected non-nil backup instance")
	}
	if b.cfg != cfg {
		t.Error("config not set correctly")
	}
	if b.opts != opts {
		t.Error("options not set correctly")
	}
	if b.homeDir == "" {
		t.Error("expected home directory to be set")
	}
}

func TestIsExcluded(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Excludes: config.ExcludesConfig{
			Patterns: []string{
				"*.log",
				"*.pyc",
				"__pycache__",
				".git",
				".DS_Store",
				"node_modules",
			},
		},
	}

	b := &Backup{cfg: cfg}

	tests := []struct {
		name     string
		path     string
		excluded bool
	}{
		{"log file", "app.log", true},
		{"nested log", "logs/debug.log", true},
		{"pyc file", "module.pyc", true},
		{"pycache dir", "__pycache__/module.cpython-312.pyc", true},
		{"ds store", ".DS_Store", true},
		{"git dir", ".git", true},
		{"git objects", ".git/objects/abc123", true},
		{"gitconfig should not match", ".gitconfig", false},
		{"gitignore should not match", ".gitignore", false},
		{"node modules", "node_modules", true},
		{"nested node modules", "project/node_modules/pkg/index.js", true},
		{"normal file", ".zshrc", false},
		{"config file", ".config/nvim/init.lua", false},
		{"text file", "notes.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := b.isExcluded(tt.path)
			if result != tt.excluded {
				t.Errorf("isExcluded(%q) = %v, want %v", tt.path, result, tt.excluded)
			}
		})
	}
}

func TestCollectItem(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	t.Run("collects single file", func(t *testing.T) {
		createTestFile(t, filepath.Join(setup.homeDir, ".zshrc"), "# zshrc content")

		cfg := &config.Config{
			Excludes: config.ExcludesConfig{},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			out:     output.New(output.ModeQuiet, false),
		}

		files, err := b.collectItem(".zshrc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		if files[0].RelPath != ".zshrc" {
			t.Errorf("expected .zshrc, got %s", files[0].RelPath)
		}
	})

	t.Run("collects directory recursively", func(t *testing.T) {
		configDir := filepath.Join(setup.homeDir, ".config", "myapp")
		createTestFile(t, filepath.Join(configDir, "config.toml"), "key = value")
		createTestFile(t, filepath.Join(configDir, "plugins", "plugin.lua"), "-- plugin")

		cfg := &config.Config{
			Excludes: config.ExcludesConfig{},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			out:     output.New(output.ModeQuiet, false),
		}

		files, err := b.collectItem(".config/myapp")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(files) != 2 {
			t.Errorf("expected 2 files, got %d", len(files))
		}
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		cfg := &config.Config{}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			out:     output.New(output.ModeQuiet, false),
		}

		_, err := b.collectItem(".nonexistent")
		if err == nil {
			t.Error("expected error for non-existent file")
		}
	})

	t.Run("excludes files matching patterns", func(t *testing.T) {
		configDir := filepath.Join(setup.homeDir, ".config", "app")
		createTestFile(t, filepath.Join(configDir, "config.json"), "{}")
		createTestFile(t, filepath.Join(configDir, "debug.log"), "log content")
		createTestFile(t, filepath.Join(configDir, ".DS_Store"), "macOS")

		cfg := &config.Config{
			Excludes: config.ExcludesConfig{
				Patterns: []string{"*.log", ".DS_Store"},
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			out:     output.New(output.ModeQuiet, false),
		}

		files, err := b.collectItem(".config/app")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(files) != 1 {
			t.Errorf("expected 1 file after exclusions, got %d", len(files))
		}
		if files[0].RelPath != ".config/app/config.json" {
			t.Errorf("expected config.json, got %s", files[0].RelPath)
		}
	})
}

func TestCollectFiles(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	createTestFile(t, filepath.Join(setup.homeDir, ".zshrc"), "export PATH=$PATH")
	createTestFile(t, filepath.Join(setup.homeDir, ".gitconfig"), "[user]\nname=Test")
	createTestFile(t, filepath.Join(setup.homeDir, ".ssh", "id_ed25519"), "PRIVATE KEY")
	createTestFile(t, filepath.Join(setup.homeDir, ".aws", "credentials"), "ACCESS_KEY")

	t.Run("collects regular items", func(t *testing.T) {
		cfg := &config.Config{
			Items:     []string{".zshrc", ".gitconfig"},
			Sensitive: []string{".ssh/id_ed25519", ".aws/credentials"},
			Excludes:  config.ExcludesConfig{},
		}

		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts: &Options{
				IncludeSecrets: false,
			},
			out: output.New(output.ModeQuiet, false),
		}

		files := b.collectFiles(false) // includeSecrets = false

		if len(files) != 2 {
			t.Errorf("expected 2 files, got %d", len(files))
		}

		for _, f := range files {
			if strings.Contains(f.RelPath, "ssh") || strings.Contains(f.RelPath, "aws") {
				t.Errorf("unexpected sensitive file: %s", f.RelPath)
			}
		}
	})

	t.Run("includes sensitive files when encryption enabled", func(t *testing.T) {
		cfg := &config.Config{
			Items:     []string{".zshrc"},
			Sensitive: []string{".ssh/id_ed25519", ".aws/credentials"},
			Excludes:  config.ExcludesConfig{},
		}

		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts: &Options{
				IncludeSecrets: true,
			},
			out: output.New(output.ModeQuiet, false),
		}

		files := b.collectFiles(true) // includeSecrets = true

		if len(files) != 3 {
			t.Errorf("expected 3 files (1 regular + 2 sensitive), got %d", len(files))
		}

		sensitiveCount := 0
		for _, f := range files {
			if f.Sensitive {
				sensitiveCount++
			}
		}
		if sensitiveCount != 2 {
			t.Errorf("expected 2 sensitive files, got %d", sensitiveCount)
		}
	})
}

func TestFormatSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		size     int64
		expected string
	}{
		{0, "0 bytes"},
		{100, "100 bytes"},
		{1023, "1023 bytes"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{1024*1024*1024 + 512*1024*1024, "1.50 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatSize(tt.size)
			if result != tt.expected {
				t.Errorf("formatSize(%d) = %s, want %s", tt.size, result, tt.expected)
			}
		})
	}
}

func TestCreateArchive(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	createTestFile(t, filepath.Join(setup.homeDir, ".zshrc"), "# zshrc content\nexport PATH=$PATH")
	createTestFile(t, filepath.Join(setup.homeDir, ".gitconfig"), "[user]\nname = Test")

	cfg := &config.Config{
		Backup: config.BackupConfig{
			BackupDir: setup.backupDir,
		},
	}

	b := &Backup{
		cfg:     cfg,
		homeDir: setup.homeDir,
		out:     output.New(output.ModeQuiet, false),
	}

	files := []FileInfo{
		{
			FullPath: filepath.Join(setup.homeDir, ".zshrc"),
			RelPath:  ".zshrc",
			Size:     35,
			ModTime:  time.Now(),
		},
		{
			FullPath: filepath.Join(setup.homeDir, ".gitconfig"),
			RelPath:  ".gitconfig",
			Size:     20,
			ModTime:  time.Now(),
		},
	}

	archivePath := filepath.Join(setup.backupDir, "test.tar.gz")
	if err := b.createArchive(archivePath, files); err != nil {
		t.Fatalf("createArchive failed: %v", err)
	}

	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive not created: %v", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	foundFiles := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error reading tar: %v", err)
		}
		foundFiles[header.Name] = true
	}

	if !foundFiles[".zshrc"] {
		t.Error("expected .zshrc in archive")
	}
	if !foundFiles[".gitconfig"] {
		t.Error("expected .gitconfig in archive")
	}
}

func TestResolveEncryption(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	recipientsFile := filepath.Join(setup.homeDir, ".config", "age", "recipients.txt")
	createTestFile(t, recipientsFile, "age1publickey...")

	t.Run("explicit none", func(t *testing.T) {
		cfg := &config.Config{
			Backup: config.BackupConfig{
				Encryption: "none",
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{EncryptionMethod: "none"},
			out:     output.New(output.ModeQuiet, false),
		}

		method, _, _, err := b.resolveEncryption()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if method != "" {
			t.Errorf("expected empty method for none, got %s", method)
		}
	})

	t.Run("explicit age with recipients", func(t *testing.T) {
		cfg := &config.Config{
			Backup: config.BackupConfig{
				Encryption:    "none",
				AgeRecipients: recipientsFile,
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{EncryptionMethod: "age"},
			out:     output.New(output.ModeQuiet, false),
		}

		method, recipients, _, err := b.resolveEncryption()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if method != "age" {
			t.Errorf("expected age, got %s", method)
		}
		if recipients != recipientsFile {
			t.Errorf("expected recipients file %s, got %s", recipientsFile, recipients)
		}
	})

	t.Run("default none encryption", func(t *testing.T) {
		cfg := &config.Config{
			Backup: config.BackupConfig{
				Encryption: "none",
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{},
			out:     output.New(output.ModeQuiet, false),
		}

		method, _, _, err := b.resolveEncryption()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if method != "" {
			t.Errorf("expected empty method for none, got %s", method)
		}
	})

	t.Run("gpg with recipient", func(t *testing.T) {
		cfg := &config.Config{
			Backup: config.BackupConfig{
				Encryption:   "gpg",
				GPGRecipient: "test@example.com",
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{},
			out:     output.New(output.ModeQuiet, false),
		}

		method, _, gpgRecipient, err := b.resolveEncryption()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if method != "gpg" {
			t.Errorf("expected gpg, got %s", method)
		}
		if gpgRecipient != "test@example.com" {
			t.Errorf("expected test@example.com, got %s", gpgRecipient)
		}
	})

	t.Run("command line recipients override config", func(t *testing.T) {
		customRecipients := filepath.Join(setup.homeDir, "custom-recipients.txt")
		createTestFile(t, customRecipients, "age1custom...")

		cfg := &config.Config{
			Backup: config.BackupConfig{
				Encryption:    "age",
				AgeRecipients: recipientsFile,
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts: &Options{
				RecipientsFile: customRecipients,
			},
			out: output.New(output.ModeQuiet, false),
		}

		method, recipients, _, err := b.resolveEncryption()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if method != "age" {
			t.Errorf("expected age, got %s", method)
		}
		if recipients != customRecipients {
			t.Errorf("expected custom recipients %s, got %s", customRecipients, recipients)
		}
	})

	t.Run("explicit age without recipients fails", func(t *testing.T) {
		cfg := &config.Config{
			Backup: config.BackupConfig{
				Encryption: "none",
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{EncryptionMethod: "age"},
			out:     output.New(output.ModeQuiet, false),
		}

		_, _, _, err := b.resolveEncryption()
		if err == nil {
			t.Error("expected error for explicit age without recipients")
		}
	})

	t.Run("explicit gpg without recipient fails", func(t *testing.T) {
		cfg := &config.Config{
			Backup: config.BackupConfig{
				Encryption: "none",
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{EncryptionMethod: "gpg"},
			out:     output.New(output.ModeQuiet, false),
		}

		_, _, _, err := b.resolveEncryption()
		if err == nil {
			t.Error("expected error for explicit gpg without recipient")
		}
	})

	t.Run("explicit age with nonexistent recipients file fails", func(t *testing.T) {
		cfg := &config.Config{
			Backup: config.BackupConfig{
				Encryption:    "none",
				AgeRecipients: "/nonexistent/recipients.txt",
			},
		}
		b := &Backup{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{EncryptionMethod: "age"},
			out:     output.New(output.ModeQuiet, false),
		}

		_, _, _, err := b.resolveEncryption()
		if err == nil {
			t.Error("expected error for explicit age with nonexistent recipients file")
		}
	})
}

func TestCleanupOldBackups(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	timestamps := []string{
		"20250101_120000",
		"20250102_120000",
		"20250103_120000",
		"20250104_120000",
		"20250105_120000",
	}

	for _, ts := range timestamps {
		createTestFile(t, filepath.Join(setup.backupDir, "dotfiles-"+ts+".tar.gz"), "archive")
		createTestFile(t, filepath.Join(setup.backupDir, "dotfiles-"+ts+".json"), "{}")
	}

	cfg := &config.Config{
		Backup: config.BackupConfig{
			BackupDir:  setup.backupDir,
			MaxBackups: 3,
		},
	}

	b := &Backup{
		cfg:     cfg,
		homeDir: setup.homeDir,
		out:     output.New(output.ModeQuiet, true),
	}

	b.cleanupOldBackups()

	matches, _ := filepath.Glob(filepath.Join(setup.backupDir, "dotfiles-*.tar.gz"))
	if len(matches) > 3 {
		t.Errorf("expected at most 3 backups, got %d", len(matches))
	}

	for _, ts := range []string{"20250105_120000", "20250104_120000", "20250103_120000"} {
		path := filepath.Join(setup.backupDir, "dotfiles-"+ts+".tar.gz")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected newest backup %s to exist", ts)
		}
	}
}

func TestCleanupOldBackups_NoCleanupWhenUnderLimit(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	for _, ts := range []string{"20250101_120000", "20250102_120000"} {
		createTestFile(t, filepath.Join(setup.backupDir, "dotfiles-"+ts+".tar.gz"), "archive")
	}

	cfg := &config.Config{
		Backup: config.BackupConfig{
			BackupDir:  setup.backupDir,
			MaxBackups: 5,
		},
	}

	b := &Backup{
		cfg:     cfg,
		homeDir: setup.homeDir,
		out:     output.New(output.ModeQuiet, false),
	}

	b.cleanupOldBackups()

	matches, _ := filepath.Glob(filepath.Join(setup.backupDir, "dotfiles-*.tar.gz"))
	if len(matches) != 2 {
		t.Errorf("expected 2 backups to remain, got %d", len(matches))
	}
}

func TestCleanupOldBackups_DisabledWhenZero(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	for _, ts := range []string{"20250101_120000", "20250102_120000", "20250103_120000"} {
		createTestFile(t, filepath.Join(setup.backupDir, "dotfiles-"+ts+".tar.gz"), "archive")
	}

	cfg := &config.Config{
		Backup: config.BackupConfig{
			BackupDir:  setup.backupDir,
			MaxBackups: 0, // disabled
		},
	}

	b := &Backup{
		cfg:     cfg,
		homeDir: setup.homeDir,
		out:     output.New(output.ModeQuiet, false),
	}

	b.cleanupOldBackups()

	matches, _ := filepath.Glob(filepath.Join(setup.backupDir, "dotfiles-*.tar.gz"))
	if len(matches) != 3 {
		t.Errorf("expected all 3 backups to remain when cleanup disabled, got %d", len(matches))
	}
}

func TestFileInfo(t *testing.T) {
	t.Parallel()

	now := time.Now()
	info := FileInfo{
		FullPath:  "/home/user/.zshrc",
		RelPath:   ".zshrc",
		Size:      1024,
		ModTime:   now,
		Sensitive: true,
	}

	if info.FullPath != "/home/user/.zshrc" {
		t.Errorf("unexpected full path: %s", info.FullPath)
	}
	if info.RelPath != ".zshrc" {
		t.Errorf("unexpected rel path: %s", info.RelPath)
	}
	if info.Size != 1024 {
		t.Errorf("unexpected size: %d", info.Size)
	}
	if !info.Sensitive {
		t.Error("expected sensitive to be true")
	}
}

func TestOptions(t *testing.T) {
	t.Parallel()

	opts := &Options{
		DryRun:           true,
		EncryptionMethod: "age",
		IncludeSecrets:   true,
		RecipientsFile:   "/path/to/recipients",
		GPGRecipient:     "user@example.com",
		Estimate:         true,
	}

	if !opts.DryRun {
		t.Error("expected DryRun to be true")
	}
	if opts.EncryptionMethod != "age" {
		t.Errorf("expected age, got %s", opts.EncryptionMethod)
	}
	if !opts.IncludeSecrets {
		t.Error("expected IncludeSecrets to be true")
	}
	if opts.RecipientsFile != "/path/to/recipients" {
		t.Errorf("unexpected recipients file: %s", opts.RecipientsFile)
	}
	if opts.GPGRecipient != "user@example.com" {
		t.Errorf("unexpected GPG recipient: %s", opts.GPGRecipient)
	}
	if !opts.Estimate {
		t.Error("expected Estimate to be true")
	}
}

func TestHasAge(t *testing.T) {
	t.Parallel()
	_ = HasAge()
}

func TestHasGPG(t *testing.T) {
	t.Parallel()
	_ = HasGPG()
}

func TestRunCommand(t *testing.T) {
	t.Parallel()

	t.Run("successful command", func(t *testing.T) {
		err := runCommand("echo", "hello")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("failed command", func(t *testing.T) {
		err := runCommand("false")
		if err == nil {
			t.Error("expected error for failed command")
		}
	})

	t.Run("nonexistent command", func(t *testing.T) {
		err := runCommand("nonexistent-command-12345")
		if err == nil {
			t.Error("expected error for nonexistent command")
		}
	})
}
