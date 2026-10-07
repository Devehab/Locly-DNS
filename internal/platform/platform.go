// Package platform knows where things live on each supported operating
// system.
package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// Environment variables that override the default locations. They exist for
// testing and CI so that nothing touches the real hosts file.
const (
	EnvHostsFile = "LOCALDNS_HOSTS_FILE"
	EnvConfigDir = "LOCALDNS_CONFIG_DIR"
)

// Supported reports whether LocalDNS supports the current operating system.
func Supported() bool { return SupportedOS(runtime.GOOS) }

// SupportedOS reports whether goos is supported.
func SupportedOS(goos string) bool {
	switch goos {
	case "linux", "darwin", "windows":
		return true
	}
	return false
}

// DefaultHostsFile is the operating system's hosts file.
func DefaultHostsFile() string {
	if runtime.GOOS == "windows" {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return filepath.Join(root, "System32", "drivers", "etc", "hosts")
	}
	return "/etc/hosts"
}

// DefaultConfigDir is the system-wide LocalDNS config directory. It is
// system-wide (not per-user) because the hosts file it describes is
// system-wide: every user on the machine sees the same hostnames.
func DefaultConfigDir() string {
	if runtime.GOOS == "windows" {
		root := os.Getenv("ProgramData")
		if root == "" {
			root = `C:\ProgramData`
		}
		return filepath.Join(root, "LocalDNS")
	}
	return "/etc/localdns"
}
