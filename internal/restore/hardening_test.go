package restore

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/output"
)

// writeArchiveWithSymlinks builds a tar.gz with regular files and symlink entries.
func writeArchiveWithSymlinks(t *testing.T, path string, files map[string]string, links map[string]string) {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range links {
		hdr := &tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0777}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func newQuietRestore(cfg *config.Config, homeDir string, opts *Options) *Restore {
	return &Restore{
		cfg:     cfg,
		homeDir: homeDir,
		opts:    opts,
		out:     output.New(output.ModeQuiet, false),
	}
}

// Symlinks into a dotfiles repo (absolute or via "..") are what stow and
// chezmoi set up; they must round-trip, while targets outside home must not.
func TestExtractStream_SymlinksIntoHomeAreRestored(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)
	outside := filepath.Join(filepath.Dir(setup.homeDir), "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}

	archivePath := filepath.Join(setup.backupDir, "links.tar.gz")
	writeArchiveWithSymlinks(t, archivePath,
		map[string]string{"dotfiles/zshrc": "# from repo"},
		map[string]string{
			".zshrc":      filepath.Join(setup.homeDir, "dotfiles", "zshrc"), // absolute, inside home
			".bashrc":     "dotfiles/zshrc",                                  // relative, no ".."
			".config/foo": "../dotfiles/zshrc",                               // climbs, stays inside
			".evil":       filepath.Join(outside, "secret"),                  // absolute, outside
			".evil2":      "../outside/secret",                               // climbs out of home
		})

	r := newQuietRestore(
		&config.Config{Backup: config.BackupConfig{BackupDir: setup.backupDir}},
		setup.homeDir,
		&Options{},
	)

	count, err := r.extractArchive(archivePath)
	if err != nil {
		t.Fatalf("extractArchive failed: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4 entries restored (1 file + 3 links), got %d", count)
	}

	for _, link := range []string{".zshrc", ".bashrc", ".config/foo"} {
		content, readErr := os.ReadFile(filepath.Join(setup.homeDir, link))
		if readErr != nil {
			t.Errorf("symlink %s was not restored: %v", link, readErr)
			continue
		}
		if string(content) != "# from repo" {
			t.Errorf("symlink %s resolves to %q", link, content)
		}
	}
	for _, link := range []string{".evil", ".evil2"} {
		if _, lstatErr := os.Lstat(filepath.Join(setup.homeDir, link)); !os.IsNotExist(lstatErr) {
			t.Errorf("symlink %s pointing outside home was created (err: %v)", link, lstatErr)
		}
	}
}

func TestSymlinkTargetWithinHome(t *testing.T) {
	t.Parallel()

	home := "/home/u"
	tests := []struct {
		link, target string
		want         bool
	}{
		{"/home/u/.zshrc", "/home/u/dotfiles/zshrc", true},
		{"/home/u/.zshrc", "dotfiles/zshrc", true},
		{"/home/u/.config/x", "../dotfiles/x", true},
		{"/home/u/.zshrc", "../dotfiles/x", false},
		{"/home/u/.zshrc", "/etc/passwd", false},
		{"/home/u/.zshrc", "/home/user2/x", false},
		{"/home/u/.zshrc", "", false},
		{"/home/u/.zshrc", "x\x00y", false},
	}
	for _, tt := range tests {
		if got := symlinkTargetWithinHome(tt.link, tt.target, home, home); got != tt.want {
			t.Errorf("symlinkTargetWithinHome(%q, %q) = %v, want %v", tt.link, tt.target, got, tt.want)
		}
	}
}

// The built-in floor must protect what it protected before matching became
// component-aware, even when the user's own sensitive list leaves it out.
func TestContainsSensitiveFiles_BuiltinFloor(t *testing.T) {
	t.Parallel()

	r := newQuietRestore(&config.Config{Sensitive: []string{".zshrc"}}, t.TempDir(), &Options{})
	for _, file := range []string{".terraform.d/credentials.tfrc.json", ".dockercfg", ".ssh/id_ed25519"} {
		if !r.containsSensitiveFiles([]string{file}) {
			t.Errorf("%s should be covered by the built-in floor", file)
		}
	}
}

// The safety backup must treat everything the config calls sensitive as
// sensitive, not only a built-in list: shell history and AI tool tokens are
// in the default sensitive list and must never land in a plaintext archive
// silently.
func TestContainsSensitiveFiles_UsesConfigSensitive(t *testing.T) {
	t.Parallel()

	r := newQuietRestore(config.DefaultConfig(), t.TempDir(), &Options{})

	for _, file := range []string{
		".zsh_history", ".claude.json", ".codex/auth.json", ".ssh/config", ".terraformrc",
		".terraform.d/credentials.tfrc.json", ".dockercfg",
	} {
		if !r.containsSensitiveFiles([]string{file}) {
			t.Errorf("%s should be sensitive", file)
		}
	}
	for _, file := range []string{".zshrc", ".sshrc", ".claude/settings.json", ".codex/config.toml"} {
		if r.containsSensitiveFiles([]string{file}) {
			t.Errorf("%s should not be sensitive", file)
		}
	}

	custom := &config.Config{Sensitive: []string{"~/work/secrets"}}
	custom.Sensitive = []string{"work/secrets"}
	r = newQuietRestore(custom, t.TempDir(), &Options{})
	if !r.containsSensitiveFiles([]string{"work/secrets/token"}) {
		t.Error("user-configured sensitive item must be honored")
	}
	if got := r.filterSensitiveFiles([]string{"work/secrets/token", ".zshrc"}); len(got) != 1 || got[0] != ".zshrc" {
		t.Errorf("filterSensitiveFiles() = %v, want [.zshrc]", got)
	}
}

func TestCreateSafetyBackup_ErrorsOnConfigSensitiveInQuietMode(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)
	createTestFile(t, filepath.Join(setup.homeDir, ".zsh_history"), "secret commands")

	archivePath := filepath.Join(setup.backupDir, "source.tar.gz")
	createTestArchive(t, archivePath, map[string]string{".zsh_history": "new"})

	cfg := config.DefaultConfig()
	cfg.Backup.BackupDir = setup.backupDir
	r := newQuietRestore(cfg, setup.homeDir, &Options{})

	if _, err := r.createSafetyBackup(archivePath); err == nil {
		t.Fatal("expected an error: sensitive file would be written unencrypted in quiet mode")
	}
	if matches, _ := filepath.Glob(filepath.Join(setup.backupDir, "pre-restore", "*")); len(matches) != 0 {
		t.Errorf("no safety archive must be written, found %v", matches)
	}
}

// diff must validate entry names like restore does, or a crafted archive can
// make it read files outside home.
func TestShowDiffStream_SkipsUnsafePaths(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)
	archivePath := filepath.Join(setup.backupDir, "evil.tar.gz")
	createTestArchive(t, archivePath, map[string]string{
		"../../../../../../etc/passwd": "root:x:0:0",
		"/etc/hosts":                   "127.0.0.1",
		".zshrc":                       "fine",
	})

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var buf bytes.Buffer
	out := output.New(output.ModeNormal, true)
	out.SetWriter(&buf)
	out.SetErrWriter(&buf)

	if err = showDiffStream(f, setup.homeDir, true, out); err != nil {
		t.Fatalf("showDiffStream failed: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "Skipping unsafe path: ../../../../../../etc/passwd") ||
		!strings.Contains(got, "Skipping unsafe path: /etc/hosts") {
		t.Errorf("unsafe entries were not skipped:\n%s", got)
	}
	if !strings.Contains(got, "1 new, 0 modified, 0 unchanged") {
		t.Errorf("only .zshrc should have been compared:\n%s", got)
	}
}

func ageKeys(t *testing.T) (identityFile, recipientsFile string) {
	t.Helper()
	for _, tool := range []string{"age", "age-keygen"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
	dir := t.TempDir()
	identityFile = filepath.Join(dir, "key.txt")
	recipientsFile = filepath.Join(dir, "recipients.txt")
	if out, err := exec.Command("age-keygen", "-o", identityFile).CombinedOutput(); err != nil {
		t.Fatalf("age-keygen: %v: %s", err, out)
	}
	if out, err := exec.Command("age-keygen", "-y", "-o", recipientsFile, identityFile).CombinedOutput(); err != nil {
		t.Fatalf("age-keygen -y: %v: %s", err, out)
	}
	return identityFile, recipientsFile
}

func ageEncryptFile(t *testing.T, recipientsFile, plainPath, encPath string) {
	t.Helper()
	if out, err := exec.Command("age", "-e", "-R", recipientsFile, "-o", encPath, plainPath).
		CombinedOutput(); err != nil {
		t.Fatalf("age -e: %v: %s", err, out)
	}
}

// With --no-backup nothing else reads the stream before extraction, so a
// truncated encrypted archive used to overwrite files before age reported
// the failure.
func TestRun_NoBackupVerifiesEncryptedArchiveFirst(t *testing.T) {
	t.Parallel()

	identityFile, recipientsFile := ageKeys(t)
	setup := setupTest(t)
	createTestFile(t, filepath.Join(setup.homeDir, ".zshrc"), "original")

	// incompressible content spanning several age chunks, so the first
	// chunks stream out before the truncated last one fails authentication
	noise := make([]byte, 300*1024)
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(setup.backupDir, "plain.tar.gz")
	createTestArchive(t, plain, map[string]string{".zshrc": string(noise)})
	encrypted := filepath.Join(setup.backupDir, "dotfiles-20250101_120000.tar.gz.age")
	ageEncryptFile(t, recipientsFile, plain, encrypted)

	// chop the tail: the payload decrypts partially, then age fails
	data, err := os.ReadFile(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(encrypted, data[:len(data)-200], 0600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Backup: config.BackupConfig{BackupDir: setup.backupDir}}
	r := newQuietRestore(cfg, setup.homeDir, &Options{NoBackup: true, Force: true, AgeIdentity: identityFile})

	result, runErr := r.Run(encrypted)

	content, _ := os.ReadFile(filepath.Join(setup.homeDir, ".zshrc"))
	if string(content) != "original" {
		t.Errorf(".zshrc was overwritten by a corrupt archive (%d bytes written)", len(content))
	}
	if runErr == nil || !strings.Contains(runErr.Error(), "verification failed") {
		t.Errorf("Run() error = %v, want archive verification failure", runErr)
	}
	if result.Success {
		t.Error("result.Success must be false")
	}
}

// Restoring an encrypted archive on a machine without recipients configured
// (the fresh-machine case) must not abort: the safety backup falls back to
// plaintext for non-sensitive files.
func TestCreateSafetyBackup_EncryptedSourceWithoutRecipients(t *testing.T) {
	t.Parallel()

	identityFile, recipientsFile := ageKeys(t)
	setup := setupTest(t)
	createTestFile(t, filepath.Join(setup.homeDir, ".zshrc"), "original")

	plain := filepath.Join(setup.backupDir, "plain.tar.gz")
	createTestArchive(t, plain, map[string]string{".zshrc": "new"})
	encrypted := filepath.Join(setup.backupDir, "dotfiles-20250101_120000.tar.gz.age")
	ageEncryptFile(t, recipientsFile, plain, encrypted)

	cfg := &config.Config{Backup: config.BackupConfig{BackupDir: setup.backupDir}}
	r := newQuietRestore(cfg, setup.homeDir, &Options{Force: true, AgeIdentity: identityFile})
	r.identityFiles = []string{identityFile}

	safetyPath, err := r.createSafetyBackup(encrypted)
	if err != nil {
		t.Fatalf("createSafetyBackup failed: %v", err)
	}
	if !strings.HasSuffix(safetyPath, ".tar.gz") {
		t.Errorf("safety backup %s should be plaintext when no recipients are configured", safetyPath)
	}

	// with recipients configured the safety backup is encrypted like the source
	cfg.Backup.AgeRecipients = recipientsFile
	safetyPath, err = r.createSafetyBackup(encrypted)
	if err != nil {
		t.Fatalf("createSafetyBackup with recipients failed: %v", err)
	}
	if !strings.HasSuffix(safetyPath, ".tar.gz.age") {
		t.Errorf("safety backup %s should be age-encrypted", safetyPath)
	}
	info, statErr := os.Stat(safetyPath)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("encrypted safety backup mode = %o, want 0600", perm)
	}
}

// tarLink is an ordered symlink entry: entry order matters for the chained
// escape below.
type tarLink struct{ name, target string }

func writeArchiveWithOrderedLinks(t *testing.T, path string, links []tarLink) {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for _, l := range links {
		hdr := &tar.Header{Name: l.name, Typeflag: tar.TypeSymlink, Linkname: l.target, Mode: 0777}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

// A lexically in-home target can resolve outside home once a symlink the
// archive itself created is followed: ".x/s -> .." turns ".x/s/../outside"
// into parent(home)/outside. Entry order lets the escaping link be created
// before the link it climbs through, so the check has to run after all links
// exist.
func TestExtractStream_SymlinkChainCannotEscapeHome(t *testing.T) {
	t.Parallel()

	setup := setupTest(t)
	outside := filepath.Join(filepath.Dir(setup.homeDir), "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}

	for name, links := range map[string][]tarLink{
		"hop first": {
			{".x/s", ".."},
			{".bashrc", ".x/s/../outside/secret"},
		},
		"escaping link first": {
			{".bashrc", ".x/s/../outside/secret"},
			{".x/s", ".."},
		},
		"absolute through hop": {
			{".x/s", ".."},
			// not filepath.Join: it would collapse "s/.." before the check does
			{".zshrc", setup.homeDir + "/.x/s/../outside/secret"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			if err := os.MkdirAll(home, 0755); err != nil {
				t.Fatal(err)
			}
			// rebase absolute targets onto this subtest's home
			rebased := make([]tarLink, len(links))
			for i, l := range links {
				rebased[i] = tarLink{l.name, strings.ReplaceAll(l.target, setup.homeDir, home)}
			}
			archivePath := filepath.Join(setup.backupDir, strings.ReplaceAll(name, " ", "-")+".tar.gz")
			writeArchiveWithOrderedLinks(t, archivePath, rebased)

			r := newQuietRestore(
				&config.Config{Backup: config.BackupConfig{BackupDir: setup.backupDir}},
				home,
				&Options{},
			)
			count, err := r.extractArchive(archivePath)
			if err != nil {
				t.Fatalf("extractArchive failed: %v", err)
			}
			if count != 1 {
				t.Errorf("expected only the harmless hop to survive, got count=%d", count)
			}

			for _, link := range []string{".bashrc", ".zshrc"} {
				if _, lstatErr := os.Lstat(filepath.Join(home, link)); !os.IsNotExist(lstatErr) {
					t.Errorf("%s escaping through a symlink chain was left in place (err: %v)", link, lstatErr)
				}
			}
			if _, lstatErr := os.Lstat(filepath.Join(home, ".x", "s")); lstatErr != nil {
				t.Errorf("hop pointing at home itself should be kept: %v", lstatErr)
			}
		})
	}
}

// A one-shot identity source (FIFO, process substitution) must be staged so
// both decryption passes can read it.
func TestResolveAgeIdentity_StagesOneShotSource(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // staging goes to ~/.cache/dotpak/tmp

	fifo := filepath.Join(t.TempDir(), "identity.fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	go func() {
		f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		_, _ = f.WriteString("AGE-SECRET-KEY-TEST\n")
		_ = f.Close()
	}()

	files, cleanup, err := ResolveAgeIdentity(fifo, nil)
	if err != nil {
		t.Fatalf("ResolveAgeIdentity failed: %v", err)
	}
	defer cleanup()

	if len(files) != 1 || files[0] == fifo {
		t.Fatalf("expected a staged copy, got %v", files)
	}
	for range 2 {
		content, readErr := os.ReadFile(files[0])
		if readErr != nil || string(content) != "AGE-SECRET-KEY-TEST\n" {
			t.Errorf("staged identity unreadable or wrong: %q, %v", content, readErr)
		}
	}
	info, _ := os.Stat(files[0])
	if info.Mode().Perm() != 0600 {
		t.Errorf("staged identity mode = %o, want 0600", info.Mode().Perm())
	}

	// a regular file is used as-is
	regular := filepath.Join(t.TempDir(), "key.txt")
	if err = os.WriteFile(regular, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	files, cleanup, err = ResolveAgeIdentity(regular, nil)
	if err != nil || len(files) != 1 || files[0] != regular {
		t.Errorf("regular identity should be passed through, got %v, %v", files, err)
	}
	cleanup()
}
