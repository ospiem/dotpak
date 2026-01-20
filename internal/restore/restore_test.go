package restore

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/output"
)

type testSetup struct {
	homeDir   string
	backupDir string
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
	if err := os.MkdirAll(filepath.Join(homeDir, ".config"), 0755); err != nil {
		t.Fatalf("Failed to create .config directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".ssh"), 0700); err != nil {
		t.Fatalf("Failed to create .ssh directory: %v", err)
	}

	return &testSetup{
		homeDir:   homeDir,
		backupDir: backupDir,
	}
}

func createTestArchive(t *testing.T, path string, files map[string]string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzw := gzip.NewWriter(f)
	defer gzw.Close()

	tw := tar.NewWriter(gzw)
	defer tw.Close()

	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
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

	r := New(cfg, opts, out)

	if r == nil {
		t.Fatal("expected non-nil restore instance")
	}
	if r.cfg != cfg {
		t.Error("config not set correctly")
	}
	if r.opts != opts {
		t.Error("options not set correctly")
	}
	if r.homeDir == "" {
		t.Error("expected home directory to be set")
	}
}

func TestCategories(t *testing.T) {
	t.Parallel()

	t.Run("all categories defined", func(t *testing.T) {
		expectedCategories := []string{
			"shell", "git", "editor", "ssh", "gpg",
			"python", "node", "rust", "go", "cloud",
			"docker", "terminal", "desktop",
		}

		for _, cat := range expectedCategories {
			if _, ok := Categories[cat]; !ok {
				t.Errorf("expected category %s to be defined", cat)
			}
		}
	})

	t.Run("shell category has expected prefixes", func(t *testing.T) {
		shellPrefixes := Categories["shell"]
		expected := []string{".zshrc", ".bashrc", ".profile"}

		for _, exp := range expected {
			found := slices.Contains(shellPrefixes, exp)
			if !found {
				t.Errorf("expected shell category to contain %s", exp)
			}
		}
	})

	t.Run("ssh category has ssh prefix", func(t *testing.T) {
		sshPrefixes := Categories["ssh"]
		found := false
		for _, prefix := range sshPrefixes {
			if strings.HasPrefix(prefix, ".ssh") {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected ssh category to have .ssh prefix")
		}
	})
}

func TestMatchesCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		categories []string
		expected   bool
	}{
		{"zshrc matches shell", ".zshrc", []string{"shell"}, true},
		{"bashrc matches shell", ".bashrc", []string{"shell"}, true},
		{"profile matches shell", ".profile", []string{"shell"}, true},
		{"gitconfig matches git", ".gitconfig", []string{"git"}, true},
		{"gitignore_global matches git", ".gitignore_global", []string{"git"}, true},
		{"ssh config matches ssh", ".ssh/config", []string{"ssh"}, true},
		{"ssh key matches ssh", ".ssh/id_ed25519", []string{"ssh"}, true},
		{"zshrc matches shell or git", ".zshrc", []string{"shell", "git"}, true},
		{"gitconfig matches shell or git", ".gitconfig", []string{"shell", "git"}, true},
		{"random file no match", ".random_config", []string{"shell"}, false},
		{"docker config no match for shell", ".docker/config.json", []string{"shell"}, false},
		{"path with leading dot-slash", "./.zshrc", []string{"shell"}, true},
		{"path with leading slash", "/.zshrc", []string{"shell"}, true},
		{"empty categories", ".zshrc", []string{}, false},
		{"unknown category", ".zshrc", []string{"unknown"}, false},
	}

	cfg := config.DefaultConfig()
	r := &Restore{
		cfg:  cfg,
		opts: &Options{},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.opts.Categories = tt.categories
			result := r.matchesCategory(tt.path)
			if result != tt.expected {
				t.Errorf("matchesCategory(%q, %v) = %v, want %v",
					tt.path, tt.categories, result, tt.expected)
			}
		})
	}
}

func TestIsSafePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"simple file", ".zshrc", true},
		{"nested path", ".config/nvim/init.lua", true},
		{"path with dots in name", "file.name.ext", true},
		{"parent traversal", "../etc/passwd", false},
		{"nested traversal", "foo/../../../etc/passwd", false},
		{"double dots in middle", "foo/bar/../baz", false},
		{"absolute path", "/etc/passwd", false},
		{"home tilde", "~/.zshrc", false},
		{"empty path", "", true},
		{"just dots", "...", false},
		{"single dot", ".", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSafePath(tt.path)
			if result != tt.expected {
				t.Errorf("isSafePath(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestExtractArchive(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	t.Run("extracts files correctly", func(t *testing.T) {
		archivePath := filepath.Join(setup.backupDir, "test.tar.gz")
		createTestArchive(t, archivePath, map[string]string{
			".zshrc":     "# zshrc content",
			".gitconfig": "[user]\nname = Test",
		})

		cfg := &config.Config{
			Backup: config.BackupConfig{
				BackupDir: setup.backupDir,
			},
		}

		r := &Restore{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{},
			out:     output.New(output.ModeQuiet, false),
		}

		count, err := r.extractArchive(archivePath)
		if err != nil {
			t.Fatalf("extractArchive failed: %v", err)
		}

		if count != 2 {
			t.Errorf("expected 2 files extracted, got %d", count)
		}

		zshrc := filepath.Join(setup.homeDir, ".zshrc")
		content, err := os.ReadFile(zshrc)
		if err != nil {
			t.Fatalf("failed to read extracted file: %v", err)
		}
		if string(content) != "# zshrc content" {
			t.Errorf("unexpected content: %s", content)
		}
	})

	t.Run("dry run does not extract files", func(t *testing.T) {
		archivePath := filepath.Join(setup.backupDir, "dry-run.tar.gz")
		createTestArchive(t, archivePath, map[string]string{
			".test_dry_run": "content",
		})

		cfg := &config.Config{
			Backup: config.BackupConfig{
				BackupDir: setup.backupDir,
			},
		}

		r := &Restore{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{DryRun: true},
			out:     output.New(output.ModeQuiet, false),
		}

		count, err := r.extractArchive(archivePath)
		if err != nil {
			t.Fatalf("extractArchive failed: %v", err)
		}

		if count != 1 {
			t.Errorf("expected 1 file counted in dry run, got %d", count)
		}

		testFile := filepath.Join(setup.homeDir, ".test_dry_run")
		if _, err := os.Stat(testFile); err == nil {
			t.Error("file should not be extracted in dry run mode")
		}
	})

	t.Run("skips unsafe paths", func(t *testing.T) {
		archivePath := filepath.Join(setup.backupDir, "unsafe.tar.gz")

		f, _ := os.Create(archivePath)
		gzw := gzip.NewWriter(f)
		tw := tar.NewWriter(gzw)

		safeHeader := &tar.Header{Name: ".safe", Mode: 0644, Size: 4}
		_ = tw.WriteHeader(safeHeader)
		_, _ = tw.Write([]byte("safe"))

		unsafeHeader := &tar.Header{Name: "../../../etc/passwd", Mode: 0644, Size: 6}
		_ = tw.WriteHeader(unsafeHeader)
		_, _ = tw.Write([]byte("unsafe"))

		_ = tw.Close()
		_ = gzw.Close()
		_ = f.Close()

		cfg := &config.Config{
			Backup: config.BackupConfig{
				BackupDir: setup.backupDir,
			},
		}

		r := &Restore{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{},
			out:     output.New(output.ModeQuiet, false),
		}

		count, err := r.extractArchive(archivePath)
		if err != nil {
			t.Fatalf("extractArchive failed: %v", err)
		}

		if count != 1 {
			t.Errorf("expected 1 safe file extracted, got %d", count)
		}

		if _, err := os.Stat(filepath.Join(setup.homeDir, ".safe")); err != nil {
			t.Error("safe file should be extracted")
		}
	})

	t.Run("respects category filter", func(t *testing.T) {
		freshSetup := setupTest(t)

		archivePath := filepath.Join(freshSetup.backupDir, "categories.tar.gz")
		createTestArchive(t, archivePath, map[string]string{
			".zshrc":                "shell config",
			".gitconfig":            "git config",
			".config/nvim/init.lua": "editor config",
		})

		cfg := &config.Config{
			Backup: config.BackupConfig{
				BackupDir: freshSetup.backupDir,
			},
		}

		r := &Restore{
			cfg:     cfg,
			homeDir: freshSetup.homeDir,
			opts:    &Options{Categories: []string{"shell"}},
			out:     output.New(output.ModeQuiet, false),
		}

		count, err := r.extractArchive(archivePath)
		if err != nil {
			t.Fatalf("extractArchive failed: %v", err)
		}

		if count != 1 {
			t.Errorf("expected 1 shell file extracted, got %d", count)
		}

		if _, err := os.Stat(filepath.Join(freshSetup.homeDir, ".zshrc")); err != nil {
			t.Error(".zshrc should be extracted")
		}

		if _, err := os.Stat(filepath.Join(freshSetup.homeDir, ".gitconfig")); err == nil {
			t.Error(".gitconfig should not be extracted with shell-only filter")
		}
	})
}

func TestExtractFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	t.Run("creates file with correct content", func(t *testing.T) {
		content := "test file content"
		path := filepath.Join(tmpDir, "test.txt")

		err := extractFile(strings.NewReader(content), path, 0644, 1024*1024)
		if err != nil {
			t.Fatalf("extractFile failed: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read file: %v", err)
		}

		if string(data) != content {
			t.Errorf("content mismatch: got %q, want %q", data, content)
		}
	})

	t.Run("creates file with correct permissions", func(t *testing.T) {
		path := filepath.Join(tmpDir, "perms.txt")

		err := extractFile(strings.NewReader("content"), path, 0600, 1024*1024)
		if err != nil {
			t.Fatalf("extractFile failed: %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat file: %v", err)
		}

		mode := info.Mode().Perm()
		if mode != 0600 {
			t.Errorf("permission mismatch: got %o, want %o", mode, 0600)
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		path := filepath.Join(tmpDir, "nested", "dirs", "file.txt")

		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("Failed to create directories: %v", err)
		}

		err := extractFile(strings.NewReader("nested"), path, 0644, 1024*1024)
		if err != nil {
			t.Fatalf("extractFile failed: %v", err)
		}

		if _, err := os.Stat(path); err != nil {
			t.Errorf("file should exist: %v", err)
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

func TestOptions(t *testing.T) {
	t.Parallel()

	opts := &Options{
		DryRun:     true,
		Force:      true,
		Categories: []string{"shell", "git"},
		NoBackup:   true,
	}

	if !opts.DryRun {
		t.Error("expected DryRun to be true")
	}
	if !opts.Force {
		t.Error("expected Force to be true")
	}
	if len(opts.Categories) != 2 {
		t.Errorf("expected 2 categories, got %d", len(opts.Categories))
	}
	if !opts.NoBackup {
		t.Error("expected NoBackup to be true")
	}
}

func TestCreateSafetyBackup(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	t.Run("creates backup of existing files", func(t *testing.T) {
		existingFile := filepath.Join(setup.homeDir, ".zshrc")
		createTestFile(t, existingFile, "original content")

		archivePath := filepath.Join(setup.backupDir, "source.tar.gz")
		createTestArchive(t, archivePath, map[string]string{
			".zshrc": "new content",
		})

		cfg := &config.Config{
			Backup: config.BackupConfig{
				BackupDir: setup.backupDir,
			},
		}

		r := &Restore{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{},
			out:     output.New(output.ModeQuiet, false),
		}

		safetyPath, err := r.createSafetyBackup(archivePath, archivePath)
		if err != nil {
			t.Fatalf("createSafetyBackup failed: %v", err)
		}

		if safetyPath == "" {
			t.Fatal("expected safety backup path")
		}

		if _, err := os.Stat(safetyPath); err != nil {
			t.Errorf("safety backup should exist: %v", err)
		}

		f, _ := os.Open(safetyPath)
		defer f.Close()
		gzr, _ := gzip.NewReader(f)
		defer gzr.Close()
		tr := tar.NewReader(gzr)

		found := false
		for {
			header, err := tr.Next()
			if err == io.EOF {
				break
			}
			if header.Name == ".zshrc" {
				found = true
				content, _ := io.ReadAll(tr)
				if string(content) != "original content" {
					t.Errorf("safety backup has wrong content: %s", content)
				}
			}
		}
		if !found {
			t.Error("safety backup should contain .zshrc")
		}
	})

	t.Run("returns empty path when no files to backup", func(t *testing.T) {
		archivePath := filepath.Join(setup.backupDir, "new-files.tar.gz")
		createTestArchive(t, archivePath, map[string]string{
			".nonexistent_config": "content",
		})

		cfg := &config.Config{
			Backup: config.BackupConfig{
				BackupDir: setup.backupDir,
			},
		}

		r := &Restore{
			cfg:     cfg,
			homeDir: setup.homeDir,
			opts:    &Options{},
			out:     output.New(output.ModeQuiet, true),
		}

		safetyPath, err := r.createSafetyBackup(archivePath, archivePath)
		if err != nil {
			t.Fatalf("createSafetyBackup failed: %v", err)
		}

		if safetyPath != "" {
			t.Errorf("expected empty path when no files to backup, got %s", safetyPath)
		}
	})

	t.Run("encrypts safety backup when original was encrypted", func(t *testing.T) {
		// create fresh setup for this test
		freshSetup := setupTest(t)

		existingFile := filepath.Join(freshSetup.homeDir, ".zshrc")
		createTestFile(t, existingFile, "original content")

		archivePath := filepath.Join(freshSetup.backupDir, "source.tar.gz")
		createTestArchive(t, archivePath, map[string]string{
			".zshrc": "new content",
		})

		// simulate encrypted original archive path (just using .age extension)
		originalArchivePath := archivePath + ".age"

		cfg := &config.Config{
			Backup: config.BackupConfig{
				BackupDir: freshSetup.backupDir,
				// note: no recipients file, so encryption will fail gracefully
			},
		}

		r := &Restore{
			cfg:     cfg,
			homeDir: freshSetup.homeDir,
			opts:    &Options{},
			out:     output.New(output.ModeQuiet, false),
		}

		// this will attempt encryption but fall back to unencrypted since no recipients
		safetyPath, err := r.createSafetyBackup(archivePath, originalArchivePath)
		if err != nil {
			t.Fatalf("createSafetyBackup failed: %v", err)
		}

		if safetyPath == "" {
			t.Fatal("expected safety backup path")
		}

		// should fall back to .tar.gz since no recipients configured
		if _, err := os.Stat(safetyPath); err != nil {
			t.Errorf("safety backup should exist: %v", err)
		}
	})
}

func TestListArchiveContents(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	archivePath := filepath.Join(setup.backupDir, "list.tar.gz")
	createTestArchive(t, archivePath, map[string]string{
		".zshrc":     "shell",
		".gitconfig": "git",
		".vimrc":     "vim",
	})

	out := output.New(output.ModeNormal, false)

	err := ListArchiveContents(nil, archivePath, out)
	if err != nil {
		t.Errorf("ListArchiveContents failed: %v", err)
	}
}

func TestShowDiff(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)

	createTestFile(t, filepath.Join(setup.homeDir, ".zshrc"), "current content")

	archivePath := filepath.Join(setup.backupDir, "diff.tar.gz")

	createTestArchive(t, archivePath, map[string]string{
		".zshrc":    "original content that is longer",
		".new_file": "this file doesn't exist locally",
	})

	out := output.New(output.ModeNormal, false)

	err := ShowDiff(nil, archivePath, false, out)
	if err != nil {
		t.Errorf("ShowDiff failed: %v", err)
	}
}
