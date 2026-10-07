// Package elevate re-runs the current command with administrator rights for a
// single invocation. LocalDNS never installs a privileged daemon: when the
// hosts file needs to change, only that one command runs elevated (via sudo
// on macOS and Linux) and then exits.
package elevate

import "io"

// Elevator runs a command with administrator rights.
type Elevator interface {
	// Available reports whether Run can work right now. In non-interactive
	// mode it only returns true when no password prompt is needed.
	Available(interactive bool) bool
	// Run executes exe with args elevated, wiring the given standard streams,
	// and returns the command's exit code.
	Run(exe string, args []string, interactive bool, stdin io.Reader, stdout, stderr io.Writer) (int, error)
	// Describe names the mechanism for messages, e.g. "sudo".
	Describe() string
}

// None is an Elevator that is never available.
type None struct{}

// Available implements Elevator.
func (None) Available(bool) bool { return false }

// Run implements Elevator.
func (None) Run(string, []string, bool, io.Reader, io.Writer, io.Writer) (int, error) {
	return 1, errUnavailable
}

// Describe implements Elevator.
func (None) Describe() string { return "none" }
