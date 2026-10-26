package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ospiem/dotpak/internal/backup"
	"github.com/ospiem/dotpak/internal/config"
	"github.com/ospiem/dotpak/internal/output"
)

// Run executes one scheduled backup (the hidden "dotpak cron run" command):
// it appends a timestamped section to the backup log, runs the backup with
// secrets included, and records the JSON result. Warnings go to the log too —
// launchd discards the job's stderr, so nothing may be left there.
func Run(loadConfig func() (*config.Config, error)) error {
	logPath, err := LogPath()
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

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(logFile, "error: %v\n", err)
		return err
	}

	out := output.New(output.ModeQuiet, false)
	out.SetErrWriter(logFile)

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
