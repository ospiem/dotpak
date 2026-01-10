// Command dotpak - backup and restore Unix dotfiles.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ospiem/dotpak/internal/backup"
	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/metadata"
	"github.com/ospiem/dotpak/internal/output"
	"github.com/ospiem/dotpak/internal/restore"
	"github.com/ospiem/dotpak/internal/utils"
)

// Build information. Populated at build time via -ldflags.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

var (
	configFile string
	verbose    bool
	quiet      bool
	jsonOutput bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "dotpak",
		Short: "Backup and restore dotfiles",
		Long: `Dotpak - backup and restore dotfiles with encryption support.

Commands:
  backup   Create a backup of dotfiles
  restore  Restore dotfiles from backup
  list     List available backups
  config   Manage configuration

Examples:
  dotpak backup                     # Create backup (no encryption by default)
  dotpak backup --dry-run           # Preview what would be backed up
  dotpak config init                # Create config
  dotpak restore                    # Restore from latest backup
  dotpak restore backup.tar.gz.age  # Restore specific archive
  dotpak list                       # List available backups`,
	}

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Config file path")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Only show errors")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(backupCmd())
	rootCmd.AddCommand(restoreCmd())
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(diffCmd())
	rootCmd.AddCommand(contentsCmd())
	rootCmd.AddCommand(cronCmd())
	rootCmd.AddCommand(versionCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func backupCmd() *cobra.Command {
	var (
		dryRun         bool
		encrypt        string
		noEncrypt      bool
		noSecrets      bool
		recipientsFile string
		gpgRecipient   string
		estimate       bool
		profile        string
	)

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a backup of dotfiles",
		Long: `Create a backup of dotfiles and developer configurations.

Examples:
  dotpak backup                    # No encryption (default)
  dotpak backup --dry-run          # Preview what would be backed up
  dotpak backup --encrypt age      # Use age encryption
  dotpak backup --encrypt gpg      # Use GPG encryption
  dotpak backup --estimate         # Show estimated backup size
  dotpak backup -p work            # Use 'work' profile`,
		RunE: func(_ *cobra.Command, _ []string) error {
			out := getOutput()

			cfg, err := loadConfig(profile)
			if err != nil {
				return outputError(out, err)
			}

			opts := &backup.Options{
				DryRun:         dryRun,
				IncludeSecrets: !noSecrets,
				RecipientsFile: recipientsFile,
				GPGRecipient:   gpgRecipient,
				Estimate:       estimate,
			}

			if noEncrypt {
				opts.EncryptionMethod = "none"
			} else if encrypt != "" {
				opts.EncryptionMethod = encrypt
			}

			b := backup.New(cfg, opts, out)
			result, err := b.Run()
			if err != nil {
				return outputError(out, err)
			}

			if jsonOutput {
				_ = out.JSON(result)
			}

			if !result.Success {
				return errors.New(result.Error)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without changes")
	cmd.Flags().StringVar(&encrypt, "encrypt", "", "Encryption: age|gpg")
	cmd.Flags().BoolVar(&noEncrypt, "no-encrypt", false, "Disable encryption")
	cmd.Flags().BoolVar(&noSecrets, "no-secrets", false, "Exclude sensitive files")
	cmd.Flags().StringVar(&recipientsFile, "recipients", "", "Path to age recipients file")
	cmd.Flags().StringVar(&gpgRecipient, "gpg-recipient", "", "GPG recipient ID or email")
	cmd.Flags().BoolVar(&estimate, "estimate", false, "Estimate backup size")
	cmd.Flags().StringVarP(&profile, "profile", "p", "", "Use named profile")

	return cmd
}

func restoreCmd() *cobra.Command {
	var (
		dryRun    bool
		force     bool
		noBackup  bool
		only      string
		homebrew  bool
		apt       bool
		goRestore bool
	)

	cmd := &cobra.Command{
		Use:   "restore [archive]",
		Short: "Restore dotfiles from backup",
		Long: `Restore dotfiles from a backup archive.

If no archive is specified, restores from the latest backup.

Examples:
  dotpak restore                        # Latest backup
  dotpak restore backup.tar.gz          # Specific archive
  dotpak restore backup.tar.gz.age      # Encrypted archive
  dotpak restore --only shell,git       # Specific categories
  dotpak restore --homebrew             # Homebrew packages only
  dotpak restore --go                   # Go packages only

Categories: shell, git, editor, ssh, gpg, python, node, rust, go, cloud, docker, terminal, desktop`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			out := getOutput()

			cfg, err := loadConfig("")
			if err != nil {
				return outputError(out, err)
			}

			if homebrew {
				return handleHomebrew(cfg.Backup.BackupDir, dryRun, out)
			}

			if apt {
				return handleApt(cfg.Backup.BackupDir, dryRun, out)
			}

			if goRestore {
				return handleGo(cfg.Backup.BackupDir, dryRun, out)
			}

			var archivePath string
			if len(args) > 0 {
				archivePath = args[0]
			} else {
				archivePath = findLatestBackup(cfg.Backup.BackupDir)
				if archivePath == "" {
					return outputError(out, fmt.Errorf("no backups found in %s", cfg.Backup.BackupDir))
				}
				out.Print("Using latest backup: %s\n", filepath.Base(archivePath))
			}

			var categories []string
			if only != "" {
				categories = strings.Split(only, ",")
				for i := range categories {
					categories[i] = strings.TrimSpace(categories[i])
				}
			}

			if !force && !dryRun && !jsonOutput {
				out.Print("\nRestore from: %s\n", filepath.Base(archivePath))
				if len(categories) > 0 {
					out.Print("Categories: %s\n", strings.Join(categories, ", "))
				}
				out.Print("\nContinue? [y/N] ")

				var response string
				_, _ = fmt.Scanln(&response)
				if strings.ToLower(response) != "y" {
					out.Print("Canceled.\n")
					return nil
				}
			}

			opts := &restore.Options{
				DryRun:     dryRun,
				Force:      force,
				Categories: categories,
				NoBackup:   noBackup,
			}

			r := restore.New(cfg, opts, out)
			result, err := r.Run(archivePath)
			if err != nil {
				return outputError(out, err)
			}

			if jsonOutput {
				_ = out.JSON(result)
			}

			if !result.Success {
				return errors.New(result.Error)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without changes")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmations")
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "Skip creating safety backup")
	cmd.Flags().StringVar(&only, "only", "", "Categories to restore (comma-separated)")
	cmd.Flags().BoolVar(&homebrew, "homebrew", false, "Restore Homebrew packages only")
	cmd.Flags().BoolVar(&apt, "apt", false, "Restore apt packages only (Linux)")
	cmd.Flags().BoolVar(&goRestore, "go", false, "Restore Go packages only")

	return cmd
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		RunE: func(_ *cobra.Command, _ []string) error {
			out := getOutput()

			cfg, err := loadConfig("")
			if err != nil {
				return outputError(out, fmt.Errorf("loading config: %w", err))
			}
			backupDir := cfg.Backup.BackupDir

			entries, err := os.ReadDir(backupDir)
			if err != nil {
				return outputError(out, fmt.Errorf("reading backup directory: %w", err))
			}

			var backups []metadata.BackupInfo

			for _, entry := range entries {
				name := entry.Name()
				if !isArchiveFile(name) {
					continue
				}

				fullPath := filepath.Join(backupDir, name)
				info, infoErr := entry.Info()
				if infoErr != nil {
					// file became unreadable between ReadDir and Info - skip it
					continue
				}

				backupInfo := metadata.BackupInfo{
					Archive:   fullPath,
					Timestamp: extractTimestamp(name),
					Size:      info.Size(),
					Encrypted: hasEncryptionExt(name),
				}

				metaPath := metadata.GetMetadataPath(fullPath)
				if meta, loadErr := metadata.Load(metaPath); loadErr == nil {
					backupInfo.Hostname = meta.Hostname
					backupInfo.FileCount = meta.Stats.FilesBackedUp
					backupInfo.Encryption = meta.EncryptionMethod
				}

				backups = append(backups, backupInfo)
			}

			sort.Slice(backups, func(i, j int) bool {
				return backups[i].Timestamp > backups[j].Timestamp
			})

			result := &metadata.ListResult{
				Success: true,
				Backups: backups,
			}

			if jsonOutput {
				return out.JSON(result)
			}

			if len(backups) == 0 {
				out.Print("No backups found in %s\n", backupDir)
			} else {
				out.Print("Available backups:\n\n")
				for _, b := range backups {
					enc := ""
					if b.Encrypted {
						enc = fmt.Sprintf(" [%s]", b.Encryption)
					}
					out.Print("  %s%s\n", filepath.Base(b.Archive), enc)
					out.Print("    Size: %s, Files: %d\n", formatSize(b.Size), b.FileCount)
					if b.Hostname != "" {
						out.Print("    Host: %s\n", b.Hostname)
					}
					out.Print("\n")
				}
			}

			return nil
		},
	}
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	cmd.AddCommand(configInitCmd())
	cmd.AddCommand(configValidateCmd())

	return cmd
}

func configInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create sample config file",
		RunE: func(_ *cobra.Command, _ []string) error {
			out := getOutput()

			cfgPath := configFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}

			dir := filepath.Dir(cfgPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return outputError(out, fmt.Errorf("creating config directory: %w", err))
			}

			if _, err := os.Stat(cfgPath); err == nil {
				return outputError(out, fmt.Errorf("config file already exists: %s", cfgPath))
			}

			if err := os.WriteFile(cfgPath, []byte(getSampleConfig()), 0600); err != nil {
				return outputError(out, fmt.Errorf("writing config: %w", err))
			}

			out.Success("Created config file: %s\n", cfgPath)
			return nil
		},
	}
}

func configValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate config file",
		RunE: func(_ *cobra.Command, _ []string) error {
			out := getOutput()

			cfgPath := configFile
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}
			if cfgPath == "" {
				return outputError(out, errors.New("cannot determine config path"))
			}

			if _, err := os.Stat(cfgPath); err != nil {
				if os.IsNotExist(err) {
					return outputError(out, fmt.Errorf("config file not found: %s", cfgPath))
				}
				return outputError(out, fmt.Errorf("reading config: %w", err))
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return outputError(out, err)
			}

			if err = validateConfig(cfg); err != nil {
				return outputError(out, err)
			}

			out.Success("Config OK: %s\n", cfgPath)
			return nil
		},
	}
}

func diffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <archive>",
		Short: "Show differences between archive and current files",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			out := getOutput()
			cfg, err := loadConfig("")
			if err != nil {
				return outputError(out, err)
			}
			return restore.ShowDiff(cfg, args[0], verbose, out)
		},
	}
}

func contentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "contents <archive>",
		Short: "List archive contents",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			out := getOutput()
			cfg, err := loadConfig("")
			if err != nil {
				return outputError(out, err)
			}
			return restore.ListArchiveContents(cfg, args[0], out)
		},
	}
}

func cronCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Manage automatic backups",
	}

	var cronHour int

	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install daily backup schedule",
		RunE: func(_ *cobra.Command, _ []string) error {
			out := getOutput()
			if cronHour < 0 || cronHour > 23 {
				return outputError(out, fmt.Errorf("hour must be between 0 and 23, got %d", cronHour))
			}
			return installCron(cronHour, out)
		},
	}
	installCmd.Flags().IntVar(&cronHour, "hour", 15, "Hour for daily backup (0-23)")

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove daily backup schedule",
		RunE: func(_ *cobra.Command, _ []string) error {
			out := getOutput()
			return uninstallCron(out)
		},
	}

	cmd.AddCommand(installCmd, uninstallCmd)
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("dotpak %s\n", version)
			fmt.Printf("  commit:  %s\n", commit)
			fmt.Printf("  built:   %s\n", buildDate)
			fmt.Printf("  go:      %s\n", runtime.Version())
			fmt.Printf("  os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}

func getOutput() *output.Output {
	mode := output.ModeNormal
	if quiet {
		mode = output.ModeQuiet
	} else if jsonOutput {
		mode = output.ModeJSON
	}
	return output.New(mode, verbose)
}

func loadConfig(profile string) (*config.Config, error) {
	cfgPath := configFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	return config.LoadWithProfile(cfgPath, profile)
}

func outputError(out *output.Output, err error) error {
	if jsonOutput {
		_ = out.JSON(map[string]any{
			"success": false,
			"error":   err.Error(),
		})
	} else {
		out.Error("%v\n", err)
	}
	return err
}

func validateConfig(cfg *config.Config) error {
	var issues []string

	backupDir := strings.TrimSpace(cfg.Backup.BackupDir)
	if backupDir == "" {
		issues = append(issues, "backup.backup_dir is required")
	} else {
		expanded := backupDir
		if strings.HasPrefix(expanded, "~/") {
			if home, err := utils.HomeDir(); err == nil {
				expanded = filepath.Join(home, expanded[2:])
			}
		}
		parentDir := filepath.Dir(expanded)
		if info, err := os.Stat(parentDir); err != nil {
			if os.IsNotExist(err) {
				issues = append(issues, fmt.Sprintf("backup.backup_dir parent does not exist: %s", parentDir))
			}
		} else if !info.IsDir() {
			issues = append(issues, fmt.Sprintf("backup.backup_dir parent is not a directory: %s", parentDir))
		}
	}

	if cfg.Backup.MaxBackups < 0 {
		issues = append(issues, "backup.max_backups must be >= 0")
	}

	switch cfg.Backup.Encryption {
	case "age", "gpg", "none", "":
	default:
		issues = append(
			issues,
			fmt.Sprintf("backup.encryption must be age|gpg|none (got %q)", cfg.Backup.Encryption),
		)
	}

	if cfg.Backup.Encryption == "age" {
		if strings.TrimSpace(cfg.Backup.AgeRecipients) == "" {
			issues = append(issues, "backup.age_recipients is required when encryption=age")
		} else {
			recipientsPath := cfg.Backup.AgeRecipients
			if strings.HasPrefix(recipientsPath, "~/") {
				if home, err := utils.HomeDir(); err == nil {
					recipientsPath = filepath.Join(home, recipientsPath[2:])
				}
			}
			if _, err := os.Stat(recipientsPath); err != nil {
				issues = append(issues, fmt.Sprintf("backup.age_recipients not found: %s", cfg.Backup.AgeRecipients))
			}
		}
	}

	if cfg.Backup.Encryption == "gpg" && strings.TrimSpace(cfg.Backup.GPGRecipient) == "" {
		issues = append(issues, "backup.gpg_recipient is required when encryption=gpg")
	}

	for _, path := range cfg.Items {
		if strings.TrimSpace(path) == "" {
			issues = append(issues, "items contains empty path")
			break
		}
	}

	for _, path := range cfg.Sensitive {
		if strings.TrimSpace(path) == "" {
			issues = append(issues, "sensitive contains empty path")
			break
		}
	}

	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("config validation failed:\n- %s", strings.Join(issues, "\n- "))
}

func handleHomebrew(backupDir string, dryRun bool, out *output.Output) error {
	// sanitize and validate the brewfile path
	cleanBackupDir := filepath.Clean(backupDir)
	brewfile := filepath.Join(cleanBackupDir, "Brewfile")

	// resolve to absolute path and verify it's within backup directory
	absBrewfile, err := filepath.Abs(brewfile)
	if err != nil {
		return outputError(out, fmt.Errorf("invalid brewfile path: %w", err))
	}
	absBackupDir, err := filepath.Abs(cleanBackupDir)
	if err != nil {
		return outputError(out, fmt.Errorf("invalid backup directory: %w", err))
	}
	if !strings.HasPrefix(absBrewfile, absBackupDir+string(filepath.Separator)) {
		return outputError(out, errors.New("brewfile path escapes backup directory"))
	}

	// verify it's a regular file (not a symlink to outside)
	info, err := os.Lstat(absBrewfile)
	if err != nil {
		return outputError(out, fmt.Errorf("brewfile not found: %s", brewfile))
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return outputError(out, errors.New("brewfile cannot be a symlink"))
	}

	out.Print("Restoring Homebrew packages from %s...\n", brewfile)

	if dryRun {
		out.Print("\nDry run - would run: brew bundle install --file=%s\n", brewfile)
		return nil
	}

	//nolint:gosec // G204: absBrewfile is validated to be within backup directory above
	cmd := exec.Command("brew", "bundle", "install", "--file="+absBrewfile, "--no-lock")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Run(); err != nil {
		return outputError(out, fmt.Errorf("brew bundle failed: %w", err))
	}

	out.Success("Homebrew packages restored\n")
	return nil
}

const linux = "linux"
const darwin = "darwin"

func handleApt(backupDir string, dryRun bool, out *output.Output) error {
	if runtime.GOOS != linux {
		return outputError(out, errors.New("apt restore only available on Linux"))
	}
	aptFile := filepath.Join(filepath.Clean(backupDir), "apt-packages.txt")
	if _, err := os.Stat(aptFile); err != nil {
		return outputError(out, errors.New("apt-packages.txt not found in backup"))
	}
	if dryRun {
		out.Print("Dry run - would install packages from: %s\n", aptFile)
		return nil
	}
	out.Print("To restore apt packages, run:\n")
	out.Print("  xargs sudo apt install -y < %s\n", aptFile)
	return nil
}

func handleGo(backupDir string, dryRun bool, out *output.Output) error {
	goFile := filepath.Join(filepath.Clean(backupDir), "go-packages.txt")
	content, err := os.ReadFile(goFile)
	if err != nil {
		return outputError(out, errors.New("go-packages.txt not found in backup"))
	}

	var packages []string
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			packages = append(packages, line)
		}
	}

	if len(packages) == 0 {
		out.Print("No Go packages to restore\n")
		return nil
	}

	out.Print("Restoring %d Go packages...\n", len(packages))

	if dryRun {
		out.Print("\nDry run - would run:\n")
		for _, pkg := range packages {
			out.Print("  go install %s@latest\n", pkg)
		}
		return nil
	}

	var installed, failed int
	for _, pkg := range packages {
		out.Verbose("Installing %s...\n", pkg)
		//nolint:gosec // G204: pkg comes from go-packages.txt backup file created by this tool
		cmd := exec.Command("go", "install", pkg+"@latest")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err = cmd.Run(); err != nil {
			out.Warning("Failed to install %s: %v\n", pkg, err)
			failed++
		} else {
			installed++
		}
	}

	if failed > 0 {
		out.Print("Go packages: %d installed, %d failed\n", installed, failed)
	} else {
		out.Success("Installed %d Go packages\n", installed)
	}
	return nil
}

func installCron(hour int, out *output.Output) error {
	switch runtime.GOOS {
	case darwin:
		return installLaunchdCron(hour, out)
	case linux:
		return installLinuxCron(hour, out)
	default:
		return outputError(out, errors.New("cron install is supported on macOS and Linux only"))
	}
}

func uninstallCron(out *output.Output) error {
	switch runtime.GOOS {
	case darwin:
		return uninstallLaunchdCron(out)
	case linux:
		return uninstallLinuxCron(out)
	default:
		return outputError(out, errors.New("cron uninstall is supported on macOS and Linux only"))
	}
}

func cronBackupArgs(execPath string) []string {
	args := []string{execPath, "backup"}
	if configFile != "" {
		args = append(args, "--config", configFile)
	}
	return args
}

func installLaunchdCron(hour int, out *output.Output) error {
	home, err := utils.HomeDir()
	if err != nil {
		return outputError(out, err)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.user.dotpak.plist")

	execPath, err := os.Executable()
	if err != nil {
		return outputError(out, fmt.Errorf("getting executable path: %w", err))
	}

	args := cronBackupArgs(execPath)
	var argsXML strings.Builder
	for _, arg := range args {
		argsXML.WriteString(fmt.Sprintf("        <string>%s</string>\n", arg))
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.user.dotpak</string>
    <key>ProgramArguments</key>
    <array>
%s    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Hour</key>
        <integer>%d</integer>
        <key>Minute</key>
        <integer>0</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>%s/Library/Logs/dotpak/backup.log</string>
    <key>StandardErrorPath</key>
    <string>%s/Library/Logs/dotpak/backup-error.log</string>
</dict>
</plist>
`, argsXML.String(), hour, home, home)

	if err = os.MkdirAll(filepath.Join(home, "Library", "Logs", "dotpak"), 0755); err != nil {
		return outputError(out, fmt.Errorf("creating logs directory: %w", err))
	}
	if err = os.MkdirAll(filepath.Join(home, "Library", "LaunchAgents"), 0755); err != nil {
		return outputError(out, fmt.Errorf("creating LaunchAgents directory: %w", err))
	}

	if err = os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return outputError(out, fmt.Errorf("writing plist: %w", err))
	}

	if err = exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		out.Warning("Failed to load LaunchAgent: %v\n", err)
	}

	out.Success("Installed daily backup at %d:00\n", hour)
	out.Print("Plist: %s\n", plistPath)
	return nil
}

func uninstallLaunchdCron(out *output.Output) error {
	home, err := utils.HomeDir()
	if err != nil {
		return outputError(out, err)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", "com.user.dotpak.plist")
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	if err = os.Remove(plistPath); err != nil {
		if os.IsNotExist(err) {
			out.Warning("LaunchAgent not installed\n")
			return nil
		}
		return outputError(out, fmt.Errorf("removing plist: %w", err))
	}

	out.Success("Uninstalled daily backup\n")
	return nil
}

const linuxCronMarker = "# dotpak"

func installLinuxCron(hour int, out *output.Output) error {
	home, err := utils.HomeDir()
	if err != nil {
		return outputError(out, err)
	}

	execPath, err := os.Executable()
	if err != nil {
		return outputError(out, fmt.Errorf("getting executable path: %w", err))
	}

	logDir := filepath.Join(home, ".local", "share", "dotpak")
	if err = os.MkdirAll(logDir, 0755); err != nil {
		return outputError(out, fmt.Errorf("creating logs directory: %w", err))
	}

	cronCmd := buildCronCommand(cronBackupArgs(execPath))
	logOut := shellQuote(filepath.Join(logDir, "backup.log"))
	logErr := shellQuote(filepath.Join(logDir, "backup-error.log"))
	cronLine := fmt.Sprintf("0 %d * * * %s >> %s 2>> %s %s", hour, cronCmd, logOut, logErr, linuxCronMarker)

	existing, err := readCrontab()
	if err != nil {
		return outputError(out, err)
	}

	lines, _ := filterDotpakCron(existing)
	lines = append(lines, cronLine)

	if err = writeCrontab(strings.Join(lines, "\n") + "\n"); err != nil {
		return outputError(out, err)
	}

	out.Success("Installed daily backup at %d:00\n", hour)
	out.Print("Cron entry: %s\n", cronLine)
	return nil
}

func uninstallLinuxCron(out *output.Output) error {
	existing, err := readCrontab()
	if err != nil {
		return outputError(out, err)
	}

	lines, removed := filterDotpakCron(existing)
	if !removed {
		out.Warning("Cron entry not installed\n")
		return nil
	}

	if len(lines) == 0 {
		if err = exec.Command("crontab", "-r").Run(); err != nil {
			return outputError(out, fmt.Errorf("removing crontab: %w", err))
		}
		out.Success("Uninstalled daily backup\n")
		return nil
	}

	if err = writeCrontab(strings.Join(lines, "\n") + "\n"); err != nil {
		return outputError(out, err)
	}

	out.Success("Uninstalled daily backup\n")
	return nil
}

func buildCronCommand(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func readCrontab() (string, error) {
	out, err := exec.Command("crontab", "-l").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no crontab for") {
			return "", nil
		}
		return "", fmt.Errorf("reading crontab: %w", err)
	}
	return string(out), nil
}

func writeCrontab(content string) error {
	tmp, err := os.CreateTemp("", "dotpak-crontab-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err = tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// crontab <file> is atomic - if it fails, the original crontab is preserved
	//nolint:gosec // G204: tmp.Name() is a temp file created by this function
	if err = exec.Command("crontab", tmp.Name()).Run(); err != nil {
		return fmt.Errorf("installing crontab: %w", err)
	}
	return nil
}

func filterDotpakCron(existing string) ([]string, bool) {
	lines := strings.Split(strings.TrimRight(existing, "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	removed := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(line), linuxCronMarker) {
			removed = true
			continue
		}
		filtered = append(filtered, line)
	}

	return filtered, removed
}

func findLatestBackup(backupDir string) string {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return ""
	}

	var archives []string
	for _, entry := range entries {
		name := entry.Name()
		if isArchiveFile(name) {
			archives = append(archives, filepath.Join(backupDir, name))
		}
	}

	if len(archives) == 0 {
		return ""
	}

	sort.Strings(archives)
	return archives[len(archives)-1]
}

func isArchiveFile(name string) bool {
	return strings.HasPrefix(name, "dotfiles") &&
		(strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.gz.age") || strings.HasSuffix(name, ".tar.gz.gpg"))
}

func hasEncryptionExt(name string) bool {
	return strings.HasSuffix(name, ".age") || strings.HasSuffix(name, ".gpg")
}

// extractTimestamp extracts and formats the timestamp from an archive filename.
// Archive names have the format: dotfiles-YYYYMMDD_HHMMSS.tar.gz[.age|.gpg]
// Example: dotfiles-20240115_143022.tar.gz -> "2024-01-15 14:30:22".
func extractTimestamp(name string) string {
	// minimum length: "dotfiles-" (9) + "YYYYMMDD_HHMMSS" (15) = 24
	const prefixLen = 9 // len("dotfiles-")
	const tsLen = 15    // len("YYYYMMDD_HHMMSS")
	const minNameLen = prefixLen + tsLen

	if len(name) < minNameLen {
		return ""
	}
	ts := name[prefixLen : prefixLen+tsLen]
	if len(ts) != tsLen {
		return ts
	}
	// format: YYYYMMDD_HHMMSS -> YYYY-MM-DD HH:MM:SS
	return fmt.Sprintf("%s-%s-%s %s:%s:%s",
		ts[0:4], ts[4:6], ts[6:8], // year, Month, Day
		ts[9:11], ts[11:13], ts[13:15]) // hour, Minute, Second
}

// formatSize wraps utils.FormatSize for local use.
func formatSize(size int64) string {
	return utils.FormatSize(size)
}

func getSampleConfig() string {
	return `# Dotpak configuration file
# See https://github.com/ospiem/dotpak for documentation

# Items to backup
items = [
    # Shell
    ".zshrc",
    ".bashrc",
    ".profile",
    ".zprofile",
    ".bash_profile",
    ".zsh",
    ".zshenv",
    ".oh-my-zsh/custom",
    ".config/fish",
    ".p10k.zsh",
    # Git
    ".gitconfig",
    ".gitignore_global",
    ".config/git",
    # Editors
    ".vimrc",
    ".config/nvim",
    ".emacs",
    ".emacs.d",
    ".config/helix",
    ".config/zed",
    # Terminal
    ".tmux.conf",
    ".config/alacritty",
    ".config/kitty",
    ".config/wezterm",
    ".config/starship.toml",
    ".config/zellij",
    # macOS
    ".config/raycast",
    # Node.js
    ".npmrc",
    ".nvmrc",
    ".yarnrc",
    ".config/yarn",
    ".bunfig.toml",
    # Python
    ".config/pip",
    ".config/ruff",
    ".config/mypy",
    ".condarc",
    ".jupyter",
    # Ruby
    ".gemrc",
    ".irbrc",
    ".pryrc",
    # Java
    ".gradle",
    ".m2/settings.xml",
    # Rust
    ".cargo/config.toml",
    ".rustup/settings.toml",
    # Go
    ".config/go",
    # DevOps
    ".ansible",
    ".ansible.cfg",
    ".config/podman",
    # AI tools (settings)
    ".claude/settings.json",
    ".claude/projects",
    ".codex/config.toml",
    ".codex/skills",
]

# Sensitive items (only backed up with encryption)
sensitive = [
    # SSH
    ".ssh",
    # GPG
    ".gnupg",
    # Cloud credentials
    ".aws",
    ".config/gcloud",
    ".azure",
    ".kube",
    ".s3cfg",
    ".yandex",
    # Terraform
    ".terraform.d",
    ".terraformrc",
    # Python credentials
    ".pypirc",
    # Docker (may contain registry auth)
    ".docker",
    # Shell history
    ".zsh_history",
    ".bash_history",
    ".lesshst",
    # AI tools (auth/tokens)
    ".claude.json",
    ".codex/auth.json",
    ".ai",
]

[backup]
# Where to store backups
backup_dir = "~/backups/dotfiles"

# Number of backups to keep
max_backups = 7

# Encryption: "age" | "gpg" | "none"
encryption = "none"

# Path to age recipients file (for age encryption)
# age_recipients = "~/.config/age/recipients.txt"

# Path to age identity files (for age decryption)
# age_identity_files = ["~/.config/age/keys.txt"]  # required for decrypting age backups

# GPG recipient (for GPG encryption)
# gpg_recipient = "your@email.com"

# Exclude patterns
[excludes]
patterns = [
    # General
    ".git",
    ".idea",
    "*.log",
    "*.swp",
    "*.bak",
    ".DS_Store",
    "*.sock",
    "*.cache",
    # CI/CD and dev artifacts
    ".circleci",
    ".github",
    ".travis.yml",
    ".gitlab-ci.yml",
    "Makefile",
    "Dockerfile",
    "*.md",
    "LICENSE*",
    "COPYING*",
    "Gemfile*",
    "*.spec",
    "*.rb",
    "test",
    "tests",
    "spec",
    ".editorconfig",
    ".gitignore",
    ".gitattributes",
    ".rspec",
    ".rubocop*",
    ".ruby-version",
    # Python
    "*.pyc",
    "__pycache__",
    ".venv",
    "venv",
    # Node
    "node_modules",
    # Java/Gradle/Maven
    ".gradle/caches",
    ".gradle/daemon",
    ".m2/repository",
    # Terraform
    "*.tfstate",
    "*.tfstate.*",
    # GPG transient
    "S.gpg-agent*",
    "random_seed",
    "*.status",
    # Docker transient
    ".token_seed*",
    "buildx/refs",
    "buildx/activity",
    "buildx/.lock",
    # SSH transient
    "known_hosts.old",
    # Zsh compiled
    "*.zwc",
    # Emacs
    "*~",
    "#*#",
    ".emacs.d/elpa",
    ".emacs.d/eln-cache",
    # Vim/Neovim
    ".config/nvim/lazy-lock.json",
    # Ruby version managers (large)
    ".rbenv/versions",
    ".rvm/gems",
    ".rvm/rubies",
    # oh-my-zsh cloned plugins artifacts
    "gitstatus/src",
    "gitstatus/deps",
    "gitstatus/usrbin",
    "*.png",
    "*.gif",
    "*.jpg",
    "*.svg",
    "test-data",
    "docs",
    # Misc dev files
    "*.sh",
    "DESCRIPTION",
    "URL",
    "VERSION",
    "ZSH_VERSIONS",
    ".revision-hash",
    ".version",
]

# Named profiles
# Use with: dotpak backup --profile work
# [profile.work]
# extra_items = [".config/slack"]

# Hostname-specific settings (applied automatically)
# [host.my-macbook]
# extra_items = [".config/work-specific"]
`
}
