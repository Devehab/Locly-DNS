//go:build darwin

package router

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// owner is the user the LaunchAgent belongs to: the person who ran sudo, or
// the current user.
func owner() (*user.User, error) {
	if os.Geteuid() == 0 {
		if n, err := strconv.Atoi(os.Getenv("SUDO_UID")); err == nil && n > 0 {
			return user.LookupId(strconv.Itoa(n))
		}
		return nil, fmt.Errorf("run this without sudo: the router runs as your own user")
	}
	return user.Current()
}

func agentPaths(u *user.User) (plist, logPath string) {
	return filepath.Join(u.HomeDir, "Library", "LaunchAgents", LaunchdLabel+".plist"),
		filepath.Join(u.HomeDir, "Library", "Logs", "localdns-router.log")
}

// State reports whether the LaunchAgent is installed.
func State() ServiceState {
	st := ServiceState{Kind: "launchd"}
	u, err := owner()
	if err != nil {
		return st
	}
	plist, logPath := agentPaths(u)
	st.Path, st.Log = plist, logPath
	_, err = os.Stat(plist)
	st.Installed = err == nil
	return st
}

// Enable installs and starts the LaunchAgent.
func Enable(exe string) error {
	u, err := owner()
	if err != nil {
		return err
	}
	plist, logPath := agentPaths(u)
	if err := os.MkdirAll(filepath.Dir(plist), 0o750); err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o750)
	if err := os.WriteFile(plist, []byte(launchdPlist(exe, logPath)), 0o644); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		_ = os.Chown(plist, uid, gid)
	}
	domain := "gui/" + u.Uid
	_ = exec.Command("launchctl", "bootout", domain+"/"+LaunchdLabel).Run()
	if out, err := exec.Command("launchctl", "bootstrap", domain, plist).CombinedOutput(); err != nil {
		// Older macOS or no GUI session: fall back to the legacy command.
		if out2, err2 := exec.Command("launchctl", "load", "-w", plist).CombinedOutput(); err2 != nil {
			return fmt.Errorf("launchctl: %s %s", strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
		}
	}
	return nil
}

// Disable stops and removes the LaunchAgent.
func Disable() error {
	u, err := owner()
	if err != nil {
		return err
	}
	plist, _ := agentPaths(u)
	_ = exec.Command("launchctl", "bootout", "gui/"+u.Uid+"/"+LaunchdLabel).Run()
	if _, err := os.Stat(plist); err == nil {
		_ = exec.Command("launchctl", "unload", "-w", plist).Run()
	}
	if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WritePID is a no-op on macOS (launchd tracks the process).
func WritePID() func() { return func() {} }
