//go:build !windows

package elevate

import (
	"errors"
	"io"
	"os"
	"os/exec"
)

var errUnavailable = errors.New("administrator rights are not available")

// Sudo elevates with sudo(8).
type Sudo struct{}

// Default returns the platform's Elevator.
func Default() Elevator { return Sudo{} }

// Available implements Elevator.
func (Sudo) Available(interactive bool) bool {
	if os.Geteuid() == 0 {
		return false // already root; elevating would not help
	}
	path, err := exec.LookPath("sudo")
	if err != nil {
		return false
	}
	if interactive {
		return true
	}
	// Non-interactive callers (scripts, AI agents) must never hang on a
	// password prompt: only elevate when sudo needs no password.
	return exec.Command(path, "-n", "true").Run() == nil
}

// Run implements Elevator.
func (Sudo) Run(exe string, args []string, interactive bool, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	path, err := exec.LookPath("sudo")
	if err != nil {
		return 1, errUnavailable
	}
	sudoArgs := []string{}
	if !interactive {
		sudoArgs = append(sudoArgs, "-n")
	}
	sudoArgs = append(sudoArgs, "--", exe)
	sudoArgs = append(sudoArgs, args...)
	cmd := exec.Command(path, sudoArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

// Describe implements Elevator.
func (Sudo) Describe() string { return "sudo" }
