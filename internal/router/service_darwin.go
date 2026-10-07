//go:build darwin

package router

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// macOS lets ordinary users open port 80 only on all network interfaces,
// never on 127.0.0.1 alone. So the router is a LaunchDaemon: launchd opens
// 127.0.0.1:80 and passes it to the router, which runs as ServiceUser
// (see launchdPlist). Installing it needs administrator rights once.
const (
	daemonPlist = "/Library/LaunchDaemons/" + LaunchdLabel + ".plist"
	daemonLog   = "/Library/Logs/localdns-router.log"
	// supportDir holds a root-owned copy of the binary that ServiceUser can
	// run (home folders are private) and that the user can't swap out.
	supportDir = "/Library/Application Support/LocalDNS"
)

func daemonBinary() string { return filepath.Join(supportDir, "localdns") }

// legacyAgent is the per-user LaunchAgent installed by LocalDNS 0.2.0,
// which could not open 127.0.0.1:80. Enable and Disable remove it.
func legacyAgent() (plist, uid string) {
	var u *user.User
	var err error
	if os.Geteuid() == 0 {
		n, aerr := strconv.Atoi(os.Getenv("SUDO_UID"))
		if aerr != nil || n <= 0 {
			return "", ""
		}
		u, err = user.LookupId(strconv.Itoa(n))
	} else {
		u, err = user.Current()
	}
	if err != nil {
		return "", ""
	}
	return filepath.Join(u.HomeDir, "Library", "LaunchAgents", LaunchdLabel+".plist"), u.Uid
}

func removeLegacyAgent() {
	plist, uid := legacyAgent()
	if plist == "" || !exists(plist) {
		return
	}
	_ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+LaunchdLabel).Run()
	_ = exec.Command("launchctl", "unload", "-w", plist).Run()
	_ = os.Remove(plist)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// State reports whether the LaunchDaemon is installed.
func State() ServiceState {
	st := ServiceState{Kind: "launchd", Path: daemonPlist, Log: daemonLog, Installed: exists(daemonPlist)}
	if !st.Installed {
		if plist, _ := legacyAgent(); plist != "" && exists(plist) {
			st.Installed, st.Outdated, st.Path = true, true, plist
		}
	}
	return st
}

// Enable installs (or updates) and starts the LaunchDaemon. It needs root.
func Enable(exe string, env map[string]string) error {
	if os.Geteuid() != 0 {
		return ErrNeedsAdmin
	}
	removeLegacyAgent()
	if err := os.MkdirAll(supportDir, 0o755); err != nil { //nolint:gosec // G301: ServiceUser must be able to run the binary
		return err
	}
	err := copyBinary(exe, daemonBinary())
	if err != nil {
		return fmt.Errorf("copy %s: %w", exe, err)
	}
	prepareLog()
	if err := os.WriteFile(daemonPlist, []byte(launchdPlist(daemonBinary(), env)), 0o644); err != nil {
		return err
	}
	// Replace a running copy (an update). bootout returns before launchd has
	// fully unloaded it, so bootstrap is retried for a few seconds.
	_ = exec.Command("launchctl", "bootout", "system/"+LaunchdLabel).Run()
	var out []byte
	for i := 0; i < 15; i++ {
		if out, err = exec.Command("launchctl", "bootstrap", "system", daemonPlist).CombinedOutput(); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	// Older macOS: fall back to the legacy command.
	if out2, err2 := exec.Command("launchctl", "load", "-w", daemonPlist).CombinedOutput(); err2 != nil {
		return fmt.Errorf("launchctl: %s %s", strings.TrimSpace(string(out)), strings.TrimSpace(string(out2)))
	}
	return nil
}

// copyBinary copies src to dst as a root-owned executable nobody else can
// modify.
func copyBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".localdns-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// prepareLog creates the log file and gives it to ServiceUser, who can't
// create files in /Library/Logs.
func prepareLog() {
	f, err := os.OpenFile(daemonLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_ = f.Close()
	if u, err := user.Lookup(ServiceUser); err == nil {
		uid, uerr := strconv.Atoi(u.Uid)
		gid, gerr := strconv.Atoi(u.Gid)
		if uerr == nil && gerr == nil {
			_ = os.Chown(daemonLog, uid, gid)
		}
	}
}

// ServiceLog is where the router writes when launchd runs it: standard
// output and error are the listening socket then, never a terminal. Both
// are pointed at the log file (or /dev/null) so nothing, not even a crash
// report, is written to the socket.
func ServiceLog() io.Writer {
	f, err := os.OpenFile(daemonLog, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		f, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return io.Discard
		}
	}
	_ = syscall.Dup2(int(f.Fd()), 1)
	_ = syscall.Dup2(int(f.Fd()), 2)
	return f
}

// Disable stops and removes the LaunchDaemon (needs root when installed).
func Disable() error {
	removeLegacyAgent()
	if !exists(daemonPlist) && !exists(supportDir) {
		return nil
	}
	if os.Geteuid() != 0 {
		return ErrNeedsAdmin
	}
	_ = exec.Command("launchctl", "bootout", "system/"+LaunchdLabel).Run()
	if exists(daemonPlist) {
		_ = exec.Command("launchctl", "unload", "-w", daemonPlist).Run()
	}
	if err := os.Remove(daemonPlist); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(daemonLog)
	return os.RemoveAll(supportDir)
}

// WritePID is a no-op on macOS (launchd tracks the process).
func WritePID() func() { return func() {} }
