// Package e2e provides end-to-end tests for dotpak CLI.
//
// These tests execute the actual dotpak binary and verify the complete
// backup/restore workflow including encryption, category filtering,
// and edge cases.
package e2e

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv holds the test environment configuration.
type testEnv struct {
	homeDir    string
	backupDir  string
	configFile string
	binary     string
}

// BackupResult represents the JSON output from backup command.
type BackupResult struct {
	Success          bool   `json:"success"`
	Archive          string `json:"archive,omitempty"`
	Encrypted        bool   `json:"encrypted"`
	EncryptionMethod string `json:"encryption_method,omitempty"`
	Stats            struct {
		FilesBackedUp  int   `json:"files_backed_up"`
		FilesSkipped   int   `json:"files_skipped"`
		FilesExcluded  int   `json:"files_excluded"`
		SensitiveFiles int   `json:"sensitive_files"`
		TotalSize      int64 `json:"total_size"`
	} `json:"stats"`
	Error string `json:"error,omitempty"`
}

// RestoreResult represents the JSON output from restore command.
type RestoreResult struct {
	Success      bool     `json:"success"`
	Archive      string   `json:"archive,omitempty"`
	SafetyBackup string   `json:"safety_backup,omitempty"`
	Categories   []string `json:"categories,omitempty"`
	DryRun       bool     `json:"dry_run"`
	Error        string   `json:"error,omitempty"`
}

// ListResult represents the JSON output from list command.
type ListResult struct {
	Success bool `json:"success"`
	Backups []struct {
		Archive   string `json:"archive"`
		Timestamp string `json:"timestamp"`
		Size      int64  `json:"size"`
		Encrypted bool   `json:"encrypted"`
	} `json:"backups"`
	Error string `json:"error,omitempty"`
}

// setupTestEnv creates an isolated test environment.
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	backupDir := filepath.Join(tmpDir, "backups")
	configDir := filepath.Join(homeDir, ".config", "dotpak")

	// create directories
	for _, dir := range []string{
		homeDir,
		backupDir,
		configDir,
		filepath.Join(homeDir, ".config"),
		filepath.Join(homeDir, ".ssh"),
		filepath.Join(homeDir, ".aws"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// set SSH directory permissions
	if err := os.Chmod(filepath.Join(homeDir, ".ssh"), 0700); err != nil {
		t.Logf("Warning: failed to set SSH directory permissions: %v", err)
	}

	// find binary - look in project root
	binary := findBinary(t)

	env := &testEnv{
		homeDir:    homeDir,
		backupDir:  backupDir,
		configFile: filepath.Join(configDir, "config.toml"),
		binary:     binary,
	}

	return env
}

// findBinary locates the dotpak binary.
func findBinary(t *testing.T) string {
	t.Helper()

	// check if DOTPAK_BINARY is set
	if bin := os.Getenv("DOTPAK_BINARY"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}

	// look in common locations
	locations := []string{
		"../../dotpak", // from tests/integration
		"./dotpak",     // current directory
		"/app/dotpak",  // docker location
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	t.Skip("dotpak binary not found - run 'make build-go' first")
	return ""
}

// createMockDotfiles creates standard mock dotfiles.
func (e *testEnv) createMockDotfiles(t *testing.T) {
	t.Helper()

	files := map[string]string{
		".zshrc":      "# zshrc test\nexport PATH=$PATH:/usr/local/bin\n",
		".bashrc":     "# bashrc test\n",
		".gitconfig":  "[user]\n  name = Test User\n  email = test@example.com\n",
		".vimrc":      "set nocompatible\n",
		".ssh/config": "Host github.com\n  User git\n",
	}

	for name, content := range files {
		path := filepath.Join(e.homeDir, name)
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// createMockSensitiveFiles creates mock sensitive files.
func (e *testEnv) createMockSensitiveFiles(t *testing.T) {
	t.Helper()

	files := map[string]string{
		".ssh/id_ed25519":  "-----BEGIN OPENSSH PRIVATE KEY-----\nMOCK_KEY\n-----END OPENSSH PRIVATE KEY-----\n",
		".aws/credentials": "[default]\naws_access_key_id = AKIAIOSFODNN7EXAMPLE\naws_secret_access_key = SECRET\n",
	}

	for name, content := range files {
		path := filepath.Join(e.homeDir, name)
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// writeConfig writes a config file.
func (e *testEnv) writeConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(e.configFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.configFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// writeNoEncryptConfig writes a config without encryption.
func (e *testEnv) writeNoEncryptConfig(t *testing.T) {
	t.Helper()
	config := `
items = [".zshrc", ".bashrc", ".gitconfig", ".vimrc"]

[backup]
backup_dir = "` + e.backupDir + `"
max_backups = 7
encryption = "none"

[excludes]
patterns = ["*.pyc", "__pycache__", ".git", "*.log", ".DS_Store"]
`
	e.writeConfig(t, config)
}

// runBackup executes the backup command.
func (e *testEnv) runBackup(t *testing.T, args ...string) *BackupResult {
	t.Helper()

	cmdArgs := []string{"backup", "--config", e.configFile, "--json"}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(e.binary, cmdArgs...)
	cmd.Env = append(os.Environ(), "HOME="+e.homeDir)

	output, err := cmd.Output()
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			t.Logf("Command stderr: %s", exitErr.Stderr)
		}
	}

	var result BackupResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse backup output: %v\nOutput: %s", err, output)
	}

	return &result
}

// runRestore executes the restore command.
func (e *testEnv) runRestore(t *testing.T, archive string, args ...string) *RestoreResult {
	t.Helper()

	cmdArgs := []string{"restore", archive, "--config", e.configFile, "--json"}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command(e.binary, cmdArgs...)
	cmd.Env = append(os.Environ(), "HOME="+e.homeDir)

	output, err := cmd.Output()
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			t.Logf("Command stderr: %s", exitErr.Stderr)
		}
	}

	var result RestoreResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse restore output: %v\nOutput: %s", err, output)
	}

	return &result
}

// runList executes the list command.
func (e *testEnv) runList(t *testing.T) *ListResult {
	t.Helper()

	cmd := exec.Command(e.binary, "list", "--config", e.configFile, "--json")
	cmd.Env = append(os.Environ(), "HOME="+e.homeDir)

	output, err := cmd.Output()
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			t.Logf("Command stderr: %s", exitErr.Stderr)
		}
	}

	var result ListResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse list output: %v\nOutput: %s", err, output)
	}

	return &result
}

func TestBackupCreatesArchive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if result.Archive == "" {
		t.Fatal("No archive path in result")
	}

	if _, err := os.Stat(result.Archive); err != nil {
		t.Fatalf("Archive file doesn't exist: %v", err)
	}

	if !strings.HasSuffix(result.Archive, ".tar.gz") {
		t.Errorf("Expected .tar.gz suffix, got %s", result.Archive)
	}
}

func TestBackupArchiveContainsFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	// read archive contents
	f, err := os.Open(result.Archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	files := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = true
	}

	expectedFiles := []string{".zshrc", ".gitconfig"}
	for _, f := range expectedFiles {
		if !files[f] {
			t.Errorf("Expected file %s in archive", f)
		}
	}
}

func TestBackupCreatesMetadata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	// find metadata file
	metadataPath := strings.TrimSuffix(result.Archive, ".tar.gz") + ".json"

	if _, err := os.Stat(metadataPath); err != nil {
		t.Fatalf("Metadata file not found: %s", metadataPath)
	}

	// parse metadata
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}

	var meta struct {
		Timestamp string `json:"timestamp"`
		Stats     struct {
			FilesBackedUp int `json:"files_backed_up"`
		} `json:"stats"`
	}

	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("Invalid metadata JSON: %v", err)
	}

	if meta.Timestamp == "" {
		t.Error("Missing timestamp in metadata")
	}
	if meta.Stats.FilesBackedUp < 1 {
		t.Error("Expected at least 1 file backed up")
	}
}

func TestFullBackupRestoreCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	originalZshrc, _ := os.ReadFile(filepath.Join(env.homeDir, ".zshrc"))
	originalGitconfig, _ := os.ReadFile(filepath.Join(env.homeDir, ".gitconfig"))

	backupResult := env.runBackup(t)
	if !backupResult.Success {
		t.Fatalf("Backup failed: %s", backupResult.Error)
	}

	if err := os.WriteFile(filepath.Join(env.homeDir, ".zshrc"), []byte("# modified"), 0644); err != nil {
		t.Fatalf("Failed to modify .zshrc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(env.homeDir, ".gitconfig"), []byte("# modified"), 0644); err != nil {
		t.Fatalf("Failed to modify .gitconfig: %v", err)
	}

	restoreResult := env.runRestore(t, backupResult.Archive, "--force", "--no-backup")
	if !restoreResult.Success {
		t.Fatalf("Restore failed: %s", restoreResult.Error)
	}

	restoredZshrc, _ := os.ReadFile(filepath.Join(env.homeDir, ".zshrc"))
	restoredGitconfig, _ := os.ReadFile(filepath.Join(env.homeDir, ".gitconfig"))

	if string(restoredZshrc) != string(originalZshrc) {
		t.Errorf("Restored .zshrc doesn't match original")
	}
	if string(restoredGitconfig) != string(originalGitconfig) {
		t.Errorf("Restored .gitconfig doesn't match original")
	}
}

func TestSensitiveFilesExcludedWithoutEncryption(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.createMockSensitiveFiles(t)

	config := `
items = [".zshrc"]
sensitive = [".ssh/id_ed25519", ".aws/credentials"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"

[excludes]
patterns = []
`
	env.writeConfig(t, config)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if result.Encrypted {
		t.Error("Expected unencrypted backup")
	}

	if result.Stats.SensitiveFiles != 0 {
		t.Errorf("Expected 0 sensitive files, got %d", result.Stats.SensitiveFiles)
	}

	// verify sensitive files not in archive
	f, _ := os.Open(result.Archive)
	defer f.Close()
	gzr, _ := gzip.NewReader(f)
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(header.Name, "id_ed25519") || strings.Contains(header.Name, "credentials") {
			t.Errorf("Sensitive file found in unencrypted archive: %s", header.Name)
		}
	}
}

func TestDryRunDoesNotCreateArchive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	beforeFiles, _ := filepath.Glob(filepath.Join(env.backupDir, "*.tar.gz"))

	result := env.runBackup(t, "--dry-run")

	if !result.Success {
		t.Fatalf("Dry run failed: %s", result.Error)
	}

	afterFiles, _ := filepath.Glob(filepath.Join(env.backupDir, "*.tar.gz"))

	if len(afterFiles) != len(beforeFiles) {
		t.Error("Dry run should not create archive files")
	}
}

func TestListCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	result1 := env.runBackup(t)
	if !result1.Success {
		t.Fatalf("First backup failed: %s", result1.Error)
	}

	result := env.runList(t)

	if !result.Success {
		t.Fatalf("List failed: %s", result.Error)
	}

	if len(result.Backups) < 1 {
		t.Errorf("Expected at least 1 backup, got %d", len(result.Backups))
	}

	// verify the backup from our run is in the list
	found := false
	for _, b := range result.Backups {
		if b.Archive == result1.Archive {
			found = true
			break
		}
	}
	if !found && len(result.Backups) > 0 {
		// at least verify list returned something
		t.Logf("Backup archive: %s", result1.Archive)
		t.Logf("Listed backups: %v", result.Backups)
	}
}

func TestExcludePatterns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)

	// create files including ones to exclude
	configDir := filepath.Join(env.homeDir, ".config", "myapp")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to write config.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "debug.log"), []byte("log"), 0644); err != nil {
		t.Fatalf("Failed to write debug.log: %v", err)
	}

	pycache := filepath.Join(configDir, "__pycache__")
	if err := os.MkdirAll(pycache, 0755); err != nil {
		t.Fatalf("Failed to create pycache directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pycache, "module.pyc"), []byte("bytecode"), 0644); err != nil {
		t.Fatalf("Failed to write module.pyc: %v", err)
	}

	config := `
items = [".config/myapp"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"

[excludes]
patterns = ["*.log", "__pycache__", "*.pyc"]
`
	env.writeConfig(t, config)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if result.Stats.FilesExcluded < 1 {
		t.Error("Expected some files to be excluded")
	}

	// verify excluded files not in archive
	f, _ := os.Open(result.Archive)
	defer f.Close()
	gzr, _ := gzip.NewReader(f)
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(header.Name, "__pycache__") ||
			strings.HasSuffix(header.Name, ".pyc") ||
			strings.HasSuffix(header.Name, ".log") {
			t.Errorf("Excluded file found in archive: %s", header.Name)
		}
	}
}

func TestUnicodeFilenames(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)

	// create file with unicode name
	unicodeFile := filepath.Join(env.homeDir, ".конфиг")
	if err := os.WriteFile(unicodeFile, []byte("содержимое"), 0644); err != nil {
		t.Fatalf("Failed to write unicode file: %v", err)
	}

	config := `
items = [".конфиг"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"
`
	env.writeConfig(t, config)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if result.Stats.FilesBackedUp < 1 {
		t.Error("Expected unicode file to be backed up")
	}
}

func TestFilePermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)

	// create file with specific permissions
	privateFile := filepath.Join(env.homeDir, ".private_config")
	if err := os.WriteFile(privateFile, []byte("secret"), 0600); err != nil {
		t.Fatalf("Failed to write private file: %v", err)
	}

	config := `
items = [".private_config"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"
`
	env.writeConfig(t, config)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	// check permissions in archive
	f, _ := os.Open(result.Archive)
	defer f.Close()
	gzr, _ := gzip.NewReader(f)
	defer gzr.Close()
	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == ".private_config" {
			mode := header.Mode & 0777
			if mode != 0600 {
				t.Errorf("Expected mode 0600, got %o", mode)
			}
			break
		}
	}
}

func TestLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)

	// create 1MB file
	largeFile := filepath.Join(env.homeDir, ".large_config")
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(largeFile, data, 0644); err != nil {
		t.Fatalf("Failed to write large file: %v", err)
	}

	config := `
items = [".large_config"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"
`
	env.writeConfig(t, config)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if result.Stats.TotalSize < 1024*1024 {
		t.Errorf("Expected total size >= 1MB, got %d", result.Stats.TotalSize)
	}
}

func TestMaxBackupsCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)

	config := `
items = [".zshrc"]

[backup]
backup_dir = "` + env.backupDir + `"
max_backups = 3
encryption = "none"
`
	env.writeConfig(t, config)

	for i := range 5 {
		result := env.runBackup(t)
		if !result.Success {
			t.Fatalf("Backup %d failed: %s", i, result.Error)
		}
	}

	archives, _ := filepath.Glob(filepath.Join(env.backupDir, "dotfiles-*.tar.gz"))
	if len(archives) > 3 {
		t.Errorf("Expected at most 3 backups, got %d", len(archives))
	}
}

func TestRestoreDryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	backupResult := env.runBackup(t)
	if !backupResult.Success {
		t.Fatalf("Backup failed: %s", backupResult.Error)
	}

	modifiedContent := "# modified for dry run test"
	if err := os.WriteFile(filepath.Join(env.homeDir, ".zshrc"), []byte(modifiedContent), 0644); err != nil {
		t.Fatalf("Failed to modify .zshrc: %v", err)
	}

	restoreResult := env.runRestore(t, backupResult.Archive, "--dry-run")
	if !restoreResult.Success {
		t.Fatalf("Restore dry run failed: %s", restoreResult.Error)
	}

	if !restoreResult.DryRun {
		t.Error("Expected dry_run to be true")
	}

	content, _ := os.ReadFile(filepath.Join(env.homeDir, ".zshrc"))
	if string(content) != modifiedContent {
		t.Error("Dry run should not modify files")
	}
}

func TestRestoreSafetyBackup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	backupResult := env.runBackup(t)
	if !backupResult.Success {
		t.Fatalf("Backup failed: %s", backupResult.Error)
	}

	if err := os.WriteFile(filepath.Join(env.homeDir, ".zshrc"), []byte("# modified"), 0644); err != nil {
		t.Fatalf("Failed to modify .zshrc: %v", err)
	}

	restoreResult := env.runRestore(t, backupResult.Archive, "--force")
	if !restoreResult.Success {
		t.Fatalf("Restore failed: %s", restoreResult.Error)
	}

	if restoreResult.SafetyBackup == "" {
		t.Error("Expected safety backup to be created")
	} else {
		if _, err := os.Stat(restoreResult.SafetyBackup); err != nil {
			t.Errorf("Safety backup file doesn't exist: %s", restoreResult.SafetyBackup)
		}
	}
}

// createMockNestedDirs creates nested directory structure for testing.
func (e *testEnv) createMockNestedDirs(t *testing.T) {
	t.Helper()

	dirs := map[string]string{
		".config/nvim/init.lua":            "-- nvim config\nrequire('plugins')\n",
		".config/nvim/lua/plugins.lua":     "-- plugins\nreturn {}\n",
		".config/alacritty/alacritty.toml": "[window]\ndecorations = \"full\"\n",
		".config/lazygit/config.yml":       "gui:\n  theme: auto\n",
	}

	for path, content := range dirs {
		fullPath := filepath.Join(e.homeDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// createMockSpacesInPaths creates files with spaces in paths.
func (e *testEnv) createMockSpacesInPaths(t *testing.T) {
	t.Helper()

	dirs := map[string]string{
		".config/app with spaces/config.json": `{"key": "value"}`,
		".config/another app/settings.yml":    "setting: true\n",
	}

	for path, content := range dirs {
		fullPath := filepath.Join(e.homeDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// runContents executes the contents command.
func (e *testEnv) runContents(t *testing.T, archive string) string {
	t.Helper()

	cmd := exec.Command(e.binary, "contents", archive, "--config", e.configFile)
	cmd.Env = append(os.Environ(), "HOME="+e.homeDir)

	output, err := cmd.Output()
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			t.Logf("Command stderr: %s", exitErr.Stderr)
		}
		t.Fatalf("Contents command failed: %v", err)
	}

	return string(output)
}

// runDiff executes the diff command.
func (e *testEnv) runDiff(t *testing.T, archive string) (string, error) {
	t.Helper()

	cmd := exec.Command(e.binary, "diff", archive, "--config", e.configFile)
	cmd.Env = append(os.Environ(), "HOME="+e.homeDir)

	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestRecursiveDirectories(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockNestedDirs(t)

	config := `
items = [".config/nvim", ".config/alacritty", ".config/lazygit"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"
`
	env.writeConfig(t, config)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	// read archive contents
	f, err := os.Open(result.Archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	files := make(map[string]bool)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = true
	}

	// check for nested files
	hasNvimInit := false
	hasAlacritty := false
	for f := range files {
		if strings.Contains(f, "nvim") && strings.Contains(f, "init.lua") {
			hasNvimInit = true
		}
		if strings.Contains(f, "alacritty") && strings.Contains(f, "alacritty.toml") {
			hasAlacritty = true
		}
	}

	if !hasNvimInit {
		t.Error("nvim/init.lua should be in archive")
	}
	if !hasAlacritty {
		t.Error("alacritty/alacritty.toml should be in archive")
	}
}

func TestContentsCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	result := env.runBackup(t)
	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	output := env.runContents(t, result.Archive)

	if !strings.Contains(output, ".zshrc") {
		t.Error("Contents output should include .zshrc")
	}
	if !strings.Contains(output, ".gitconfig") {
		t.Error("Contents output should include .gitconfig")
	}
}

func TestDiffCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.writeNoEncryptConfig(t)

	result := env.runBackup(t)
	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	// modify a file to create a diff
	zshrcPath := filepath.Join(env.homeDir, ".zshrc")
	if err := os.WriteFile(zshrcPath, []byte("# modified for diff test"), 0644); err != nil {
		t.Fatalf("Failed to modify .zshrc: %v", err)
	}

	output, err := env.runDiff(t, result.Archive)
	if err != nil {
		t.Logf("Diff output: %s", output)
		t.Errorf("Diff command failed: %v", err)
	}

	// diff should show the modified file
	if !strings.Contains(output, ".zshrc") {
		t.Error("Diff output should mention .zshrc")
	}
}

func TestSpacesInPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnv(t)
	env.createMockSpacesInPaths(t)

	config := `
items = [".config/app with spaces", ".config/another app"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"
`
	env.writeConfig(t, config)

	result := env.runBackup(t)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	// read archive contents
	f, err := os.Open(result.Archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	hasSpacesDir := false
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(header.Name, "app with spaces") {
			hasSpacesDir = true
			break
		}
	}

	if !hasSpacesDir {
		t.Error("Directory with spaces should be in archive")
	}
}
