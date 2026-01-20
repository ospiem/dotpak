package backup

import (
	"os/exec"

	"github.com/ospiem/dotpak/internal/crypto"
)

// runCommand runs an external command.
func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

// runCommandOutput runs a command and returns its output.
func runCommandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.Output()
	return string(output), err
}

// HasAge checks if age is available.
func HasAge() bool {
	return crypto.HasAge()
}

// HasGPG checks if gpg is available.
func HasGPG() bool {
	return crypto.HasGPG()
}
