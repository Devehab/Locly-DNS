//go:build windows

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
	installDirSuffix      = "LocalDNS"
)

// removeBinary removes the running localdns.exe.
//
// Windows refuses to delete an executable while it runs, but it does allow
// renaming it. So the binary is first moved aside, which makes the
// `localdns` command disappear immediately, and a short-lived hidden
// PowerShell helper deletes the renamed file (retrying for up to 30 seconds)
// once this process has exited. The installer's directory is removed too
// when it ends up empty, and its entry in the user PATH is removed right away.
func removeBinary(path string) error {
	dir := filepath.Dir(path)
	ownDir := strings.EqualFold(filepath.Base(dir), installDirSuffix)
	if ownDir {
		if err := removeFromUserPath(dir); err != nil {
			return err
		}
	}

	aside := path + ".uninstalled"
	_ = os.Remove(aside) // leftover from an earlier attempt
	if err := os.Rename(path, aside); err != nil {
		return err
	}

	script := "$f = " + psQuote(aside) + "; " +
		"for ($i = 0; $i -lt 60 -and (Test-Path -LiteralPath $f); $i++) { " +
		"Start-Sleep -Milliseconds 500; Remove-Item -LiteralPath $f -Force -ErrorAction SilentlyContinue }; "
	if ownDir {
		script += "if (-not (Get-ChildItem -LiteralPath " + psQuote(dir) + " -Force)) { " +
			"Remove-Item -LiteralPath " + psQuote(dir) + " -Force -ErrorAction SilentlyContinue }"
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow | createNewProcessGroup, HideWindow: true}
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
