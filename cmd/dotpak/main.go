// Command dotpak - backup and restore Unix dotfiles.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ospiem/dotpak/internal/backup"
	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/crypto"
	"github.com/ospiem/dotpak/internal/metadata"
	"github.com/ospiem/dotpak/internal/osutils"
	"github.com/ospiem/dotpak/internal/output"
	"github.com/ospiem/dotpak/internal/pkgrestore"
	"github.com/ospiem/dotpak/internal/restore"
	"github.com/ospiem/dotpak/internal/schedule"
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
		// errors are reported exactly once via outputError / the JSON envelope
		SilenceErrors: true,
		SilenceUsage:  true,
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

			b, err := backup.New(cfg, opts, out)
			if err != nil {
				return outputError(out, err)
			}

			result, runErr := b.Run()
			if jsonOutput {
				_ = out.JSON(result)
				return runErr // the JSON envelope already carries the error
			}
			if runErr != nil {
				return outputError(out, runErr)
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
		dryRun      bool
		force       bool
		noBackup    bool
		only        string
		homebrew    bool
		apt         bool
		goRestore   bool
		ageIdentity string
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

Categories: ` + strings.Join(restore.CategoryNames(), ", "),
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			out := getOutput()

			cfg, err := loadConfig("")
			if err != nil {
				return outputError(out, err)
			}

			if homebrew {
				return outputWrap(out, pkgrestore.Homebrew(cfg.Backup.BackupDir, dryRun, out))
			}

			if apt {
				return outputWrap(out, pkgrestore.Apt(cfg.Backup.BackupDir, dryRun, out))
			}

			if goRestore {
				return outputWrap(out, pkgrestore.Go(cfg.Backup.BackupDir, dryRun, out))
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

			if !force && !dryRun {
				// the identity arrives on stdin, so the prompt would read the
				// key instead of the answer (and corrupt the identity)
				if ageIdentity == "-" {
					identityErr := errors.New(
						"--age-identity - reads stdin, leaving no input for the confirmation prompt; " +
							"pass --force (or --dry-run)",
					)
					return outputError(out, identityErr)
				}

				// non-interactive modes cannot show the prompt; require an
				// explicit --force instead of silently overwriting files
				if jsonOutput || quiet {
					confirmErr := errors.New(
						"restore overwrites existing files; pass --force (or --dry-run) with --json/--quiet",
					)
					return outputError(out, confirmErr)
				}

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
				DryRun:      dryRun,
				Force:       force,
				Categories:  categories,
				NoBackup:    noBackup,
				AgeIdentity: ageIdentity,
			}

			r, err := restore.New(cfg, opts, out)
			if err != nil {
				return outputError(out, err)
			}

			result, runErr := r.Run(archivePath)
			if jsonOutput {
				_ = out.JSON(result)
				return runErr // the JSON envelope already carries the error
			}
			if runErr != nil {
				return outputError(out, runErr)
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
	cmd.Flags().StringVar(&ageIdentity, "age-identity", "", "Age identity file for decryption ('-' for stdin)")

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
					Encrypted: crypto.IsEncryptedPath(name),
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
					out.Print("    Size: %s, Files: %d\n", osutils.FormatSize(b.Size), b.FileCount)
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
			if err := os.MkdirAll(dir, 0700); err != nil {
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

			if err = config.Validate(cfg); err != nil {
				return outputError(out, err)
			}

			out.Success("Config OK: %s\n", cfgPath)
			return nil
		},
	}
}

func diffCmd() *cobra.Command {
	var ageIdentity string

	cmd := &cobra.Command{
		Use:   "diff <archive>",
		Short: "Show differences between archive and current files",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			out := getOutput()
			cfg, err := loadConfig("")
			if err != nil {
				return outputError(out, err)
			}
			return outputWrap(out, restore.ShowDiff(cfg, args[0], ageIdentity, verbose, out))
		},
	}

	cmd.Flags().StringVar(&ageIdentity, "age-identity", "", "Age identity file for decryption ('-' for stdin)")

	return cmd
}

func contentsCmd() *cobra.Command {
	var ageIdentity string

	cmd := &cobra.Command{
		Use:   "contents <archive>",
		Short: "List archive contents",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			out := getOutput()
			cfg, err := loadConfig("")
			if err != nil {
				return outputError(out, err)
			}
			return outputWrap(out, restore.ListArchiveContents(cfg, args[0], ageIdentity, out))
		},
	}

	cmd.Flags().StringVar(&ageIdentity, "age-identity", "", "Age identity file for decryption ('-' for stdin)")

	return cmd
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
			return outputWrap(out, schedule.Install(cronHour, configFile, out))
		},
	}
	installCmd.Flags().IntVar(&cronHour, "hour", 15, "Hour for daily backup (0-23)")

	uninstallCmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove daily backup schedule",
		RunE: func(_ *cobra.Command, _ []string) error {
			out := getOutput()
			return outputWrap(out, schedule.Uninstall(out))
		},
	}

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show scheduled backup status",
		RunE: func(_ *cobra.Command, _ []string) error {
			out := getOutput()
			// nil config only degrades the FDA check in the status display
			cfg, _ := loadConfig("")
			return outputWrap(out, schedule.Status(cfg, out))
		},
	}

	runCmd := &cobra.Command{
		Use:    "run",
		Short:  "Run backup with logging (used by launchd/cron)",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return cronRun()
		},
	}

	cmd.AddCommand(installCmd, uninstallCmd, statusCmd, runCmd)
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

// outputError reports err to the user exactly once (cobra's own error
// printing is silenced) and returns it for the exit code.
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

// outputWrap passes nil through and reports non-nil errors via outputError.
func outputWrap(out *output.Output, err error) error {
	if err == nil {
		return nil
	}
	return outputError(out, err)
}

func cronRun() error {
	logPath, err := schedule.LogPath()
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return fmt.Errorf("creating log directory: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}
	defer logFile.Close()

	fmt.Fprintf(logFile, "---\n%s\n", time.Now().Format(time.RFC3339))

	cfg, err := loadConfig("")
	if err != nil {
		fmt.Fprintf(logFile, "error: %v\n", err)
		return err
	}

	out := output.New(output.ModeQuiet, false)

	b, err := backup.New(cfg, &backup.Options{IncludeSecrets: true}, out)
	if err != nil {
		fmt.Fprintf(logFile, "error: %v\n", err)
		return err
	}

	result, runErr := b.Run()
	enc := json.NewEncoder(logFile)
	enc.SetIndent("", "  ")
	if encErr := enc.Encode(result); encErr != nil {
		fmt.Fprintf(logFile, "error encoding result: %v\n", encErr)
	}
	if runErr != nil {
		fmt.Fprintf(logFile, "error: %v\n", runErr)
	}
	return runErr
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
	if !strings.HasPrefix(name, metadata.ArchivePrefix) {
		return false
	}
	base := name
	if method := crypto.DetectMethod(base); method != crypto.MethodNone {
		base = strings.TrimSuffix(base, method.Extension())
	}
	return strings.HasSuffix(base, ".tar.gz")
}

// extractTimestamp extracts and formats the timestamp from an archive filename.
// Archive names have the format: dotfiles-YYYYMMDD_HHMMSS.tar.gz[.age|.gpg]
// Example: dotfiles-20240115_143022.tar.gz -> "2024-01-15 14:30:22".
func extractTimestamp(name string) string {
	prefixLen := len(metadata.ArchivePrefix)
	tsLen := len(metadata.TimestampFormat) // YYYYMMDD_HHMMSS

	if len(name) < prefixLen+tsLen {
		return ""
	}
	ts := name[prefixLen : prefixLen+tsLen]
	// format: YYYYMMDD_HHMMSS -> YYYY-MM-DD HH:MM:SS
	return fmt.Sprintf("%s-%s-%s %s:%s:%s",
		ts[0:4], ts[4:6], ts[6:8], // year, Month, Day
		ts[9:11], ts[11:13], ts[13:15]) // hour, Minute, Second
}
