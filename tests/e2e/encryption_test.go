package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultNoEncryption(t *testing.T) {
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
`
	env.writeConfig(t, config)

	result := env.runBackup(t, "--dry-run")

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if result.Encrypted {
		t.Error("Expected unencrypted backup by default")
	}
}

func TestExplicitAgeEncryption(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not available")
	}
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not available")
	}

	env := setupTestEnv(t)
	env.createMockDotfiles(t)

	keysFile, recipientsFile := generateAgeKeys(t, env.homeDir)
	_ = keysFile

	config := `
items = [".zshrc"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"
age_recipients = "` + recipientsFile + `"
`
	env.writeConfig(t, config)

	result := env.runBackup(t, "--dry-run", "--encrypt", "age")

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if !result.Encrypted {
		t.Error("Expected encrypted backup")
	}

	if result.EncryptionMethod != "age" {
		t.Errorf("Expected 'age', got '%s'", result.EncryptionMethod)
	}
}

func TestNoEncryptFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not available")
	}

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.createMockSensitiveFiles(t)

	_, recipientsFile := generateAgeKeys(t, env.homeDir)

	config := `
items = [".zshrc"]
sensitive = [".ssh/id_ed25519"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "age"
age_recipients = "` + recipientsFile + `"
`
	env.writeConfig(t, config)

	result := env.runBackup(t, "--dry-run", "--no-encrypt")

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if result.Encrypted {
		t.Error("Expected unencrypted backup with --no-encrypt")
	}

	if result.Stats.SensitiveFiles != 0 {
		t.Errorf("Expected 0 sensitive files with --no-encrypt, got %d", result.Stats.SensitiveFiles)
	}
}

func TestSensitiveFilesWithEncryption(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not available")
	}

	env := setupTestEnv(t)
	env.createMockDotfiles(t)
	env.createMockSensitiveFiles(t)

	_, recipientsFile := generateAgeKeys(t, env.homeDir)

	config := `
items = [".zshrc"]
sensitive = [".ssh/id_ed25519", ".aws/credentials"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "age"
age_recipients = "` + recipientsFile + `"
`
	env.writeConfig(t, config)

	result := env.runBackup(t, "--dry-run")

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if !result.Encrypted {
		t.Error("Expected encrypted backup")
	}

	if result.Stats.SensitiveFiles != 2 {
		t.Errorf("Expected 2 sensitive files, got %d", result.Stats.SensitiveFiles)
	}
}

func TestCustomRecipientsFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not available")
	}

	env := setupTestEnv(t)
	env.createMockDotfiles(t)

	customDir := filepath.Join(env.homeDir, "custom")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("Failed to create custom directory: %v", err)
	}

	keysFile := filepath.Join(customDir, "keys.txt")
	recipientsFile := filepath.Join(customDir, "recipients.txt")

	cmd := exec.Command("age-keygen", "-o", keysFile)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to generate keys: %v", err)
	}

	cmd = exec.Command("age-keygen", "-y", "-o", recipientsFile, keysFile)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to extract public key: %v", err)
	}

	config := `
items = [".zshrc"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "none"
`
	env.writeConfig(t, config)

	result := env.runBackup(t, "--dry-run", "--encrypt", "age", "--recipients", recipientsFile)

	if !result.Success {
		t.Fatalf("Backup failed: %s", result.Error)
	}

	if !result.Encrypted {
		t.Error("Expected encrypted backup with custom recipients")
	}

	if result.EncryptionMethod != "age" {
		t.Errorf("Expected 'age', got '%s'", result.EncryptionMethod)
	}
}

func TestMissingRecipientsFile(t *testing.T) {
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
encryption = "age"
age_recipients = "/nonexistent/recipients.txt"
`
	env.writeConfig(t, config)

	result := env.runBackup(t, "--dry-run")

	if result.Success && result.Encrypted {
		t.Error("Should not encrypt with missing recipients file")
	}
}

func TestAgeEncryptDecryptCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not available")
	}
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not available")
	}

	env := setupTestEnv(t)
	env.createMockDotfiles(t)

	keysFile, recipientsFile := generateAgeKeys(t, env.homeDir)

	configAgeDir := filepath.Join(env.homeDir, ".config", "age")
	if err := os.MkdirAll(configAgeDir, 0700); err != nil {
		t.Fatalf("Failed to create config age directory: %v", err)
	}

	keysContent, err := os.ReadFile(keysFile)
	if err != nil {
		t.Fatalf("Failed to read keys file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configAgeDir, "keys.txt"), keysContent, 0600); err != nil {
		t.Fatalf("Failed to write keys: %v", err)
	}

	config := `
items = [".zshrc", ".gitconfig"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "age"
age_recipients = "` + recipientsFile + `"
age_identity_files = ["` + filepath.Join(configAgeDir, "keys.txt") + `"]
`
	env.writeConfig(t, config)

	originalZshrc, err := os.ReadFile(filepath.Join(env.homeDir, ".zshrc"))
	if err != nil {
		t.Fatalf("Failed to read original .zshrc: %v", err)
	}

	backupResult := env.runBackup(t)
	if !backupResult.Success {
		t.Fatalf("Backup failed: %s", backupResult.Error)
	}

	if !backupResult.Encrypted {
		t.Fatal("Expected encrypted backup")
	}

	if !strings.HasSuffix(backupResult.Archive, ".age") {
		t.Errorf("Expected .age suffix, got %s", backupResult.Archive)
	}

	if err := os.WriteFile(filepath.Join(env.homeDir, ".zshrc"), []byte("# modified"), 0644); err != nil {
		t.Fatalf("Failed to modify .zshrc: %v", err)
	}

	restoreResult := env.runRestore(t, backupResult.Archive, "--force", "--no-backup")
	if !restoreResult.Success {
		t.Fatalf("Restore failed: %s", restoreResult.Error)
	}

	restoredZshrc, _ := os.ReadFile(filepath.Join(env.homeDir, ".zshrc"))
	if string(restoredZshrc) != string(originalZshrc) {
		t.Error("Content not properly restored after decrypt")
	}
}

func generateAgeKeys(t *testing.T, homeDir string) (keysFile, recipientsFile string) {
	t.Helper()

	ageDir := filepath.Join(homeDir, ".config", "age")
	if err := os.MkdirAll(ageDir, 0700); err != nil {
		t.Fatalf("Failed to create age directory: %v", err)
	}

	keysFile = filepath.Join(ageDir, "keys.txt")
	recipientsFile = filepath.Join(ageDir, "recipients.txt")

	cmd := exec.Command("age-keygen", "-o", keysFile)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to generate age keys: %v", err)
	}

	cmd = exec.Command("age-keygen", "-y", "-o", recipientsFile, keysFile)
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to extract public key: %v", err)
	}

	return keysFile, recipientsFile
}

func TestGPGEncryptionConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	t.Parallel()

	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not available")
	}

	env := setupTestEnv(t)
	env.createMockDotfiles(t)

	config := `
items = [".zshrc"]

[backup]
backup_dir = "` + env.backupDir + `"
encryption = "gpg"
gpg_recipient = "test@example.com"
`
	env.writeConfig(t, config)

	result := env.runBackup(t, "--dry-run")

	if result.Success && result.Encrypted && result.EncryptionMethod != "gpg" {
		t.Errorf("Expected encryption method 'gpg' when configured, got '%s'", result.EncryptionMethod)
	}
}
