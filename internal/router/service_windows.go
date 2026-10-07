//go:build windows

package router

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	runKey                = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	createNoWindow        = 0x08000000
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

func pidFile() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "LocalDNS", "router.pid")
}

// State reports whether the router starts at login.
func State() ServiceState {
	st := ServiceState{Kind: "login item", Path: runKey + `\` + WindowsRunKey}
	st.Installed = exec.Command("reg", "query", runKey, "/v", WindowsRunKey).Run() == nil
	return st
}

// Enable starts the router at every login (for the current user, no admin
// rights needed) and starts it now, without a console window. env applies
// to the router started now (hosts file and config overrides).
func Enable(exe string, env map[string]string) error {
	value := `"` + exe + `" router run`
	if out, err := exec.Command("reg", "add", runKey, "/v", WindowsRunKey, "/t", "REG_SZ", "/d", value, "/f").CombinedOutput(); err != nil {
		return fmt.Errorf("reg add: %s", strings.TrimSpace(string(out)))
	}
	stopRunning()
	cmd := exec.Command(exe, "router", "run")
	cmd.Env = os.Environ()
	for _, k := range sortedKeys(env) {
		cmd.Env = append(cmd.Env, k+"="+env[k])
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | detachedProcess | createNewProcessGroup, HideWindow: true}
	return cmd.Start()
}

// Disable stops the router and removes it from login items.
func Disable() error {
	_ = exec.Command("reg", "delete", runKey, "/v", WindowsRunKey, "/f").Run()
	stopRunning()
	return nil
}

func stopRunning() {
	b, err := os.ReadFile(pidFile())
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 && pid != os.Getpid() && isLocalDNS(pid) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	}
	_ = os.Remove(pidFile())
}

// ServiceLog is a no-op on Windows.
func ServiceLog() io.Writer { return io.Discard }

// WritePID records the running router so Disable can stop it. The returned
// function removes the record.
func WritePID() func() {
	_ = os.MkdirAll(filepath.Dir(pidFile()), 0o750)
	_ = os.WriteFile(pidFile(), []byte(strconv.Itoa(os.Getpid())), 0o644)
	return func() { _ = os.Remove(pidFile()) }
}

// isLocalDNS checks that pid is still a localdns process, so a stale PID file
// never stops an unrelated program that reused the number.
func isLocalDNS(pid int) bool {
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH").Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), "localdns")
}
