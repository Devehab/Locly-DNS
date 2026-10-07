//go:build linux

package router

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const unitPath = "/etc/systemd/system/" + SystemdUnit

// State reports whether the systemd unit is installed.
func State() ServiceState {
	_, err := os.Stat(unitPath)
	return ServiceState{Kind: "systemd", Path: unitPath, Installed: err == nil, Log: "journalctl -u " + SystemdUnit}
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

// Enable installs and starts the systemd unit (needs root).
func Enable(exe string) error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("%w: systemd is not available; run `sudo localdns router run` in a terminal instead", ErrUnsupported)
	}
	if os.Geteuid() != 0 {
		return ErrNeedsAdmin
	}
	user := os.Getenv("SUDO_USER")
	if user == "root" {
		user = ""
	}
	if err := os.WriteFile(unitPath, []byte(systemdUnit(exe, user)), 0o644); err != nil { //nolint:gosec // G703: fixed path; exe is our own binary
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	_ = systemctl("stop", SystemdUnit)
	return systemctl("enable", "--now", SystemdUnit)
}

// Disable stops and removes the systemd unit (needs root).
func Disable() error {
	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return nil
	}
	if os.Geteuid() != 0 {
		return ErrNeedsAdmin
	}
	_ = systemctl("disable", "--now", SystemdUnit)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = systemctl("daemon-reload")
	return nil
}

// WritePID is a no-op on Linux (systemd tracks the process).
func WritePID() func() { return func() {} }
