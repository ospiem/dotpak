package backup

import (
	"context"
	"os/exec"
	"time"
)

// commandTimeout bounds the package-manifest commands (brew, mas, apt-mark,
// go). They are best-effort, and a hung one must not block the backup — or
// the launchd/cron job — after the archive is already written.
const commandTimeout = 2 * time.Minute

// runCommand runs an external command with a timeout.
func runCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

// runCommandOutput runs a command with a timeout and returns its stdout.
func runCommandOutput(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).Output()
	return string(output), err
}
