//go:build windows

package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	createNoWindow   = 0x08000000
	detachedProcess  = 0x00000008
	installDirSuffix = "LocalDNS"
)

// removeBinary deletes the running localdns.exe. Windows does not allow
// deleting a running executable, so a short-lived hidden PowerShell process
// removes it (and the install directory, if empty) after this process exits.
// The installer's entry in the user PATH is removed immediately.
func removeBinary(path string) error {
	dir := filepath.Dir(path)
	if strings.EqualFold(filepath.Base(dir), installDirSuffix) {
		if err := removeFromUserPath(dir); err != nil {
			return err
		}
	}
	script := "Start-Sleep -Seconds 2; " +
		"Remove-Item -LiteralPath " + psQuote(path) + " -Force -ErrorAction SilentlyContinue; "
	if strings.EqualFold(filepath.Base(dir), installDirSuffix) {
		script += "if (-not (Get-ChildItem -LiteralPath " + psQuote(dir) + " -Force)) { " +
			"Remove-Item -LiteralPath " + psQuote(dir) + " -Force -ErrorAction SilentlyContinue }"
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | detachedProcess, HideWindow: true}
	return cmd.Start()
}

func removeFromUserPath(dir string) error {
	script := "$d = " + psQuote(dir) + "; " +
		"$p = [Environment]::GetEnvironmentVariable('Path', 'User'); " +
		"if ($p) { " +
		"$n = (($p -split ';') | Where-Object { $_ -and ($_.TrimEnd('\\') -ne $d.TrimEnd('\\')) }) -join ';'; " +
		"if ($n -ne $p) { [Environment]::SetEnvironmentVariable('Path', $n, 'User') } }"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
