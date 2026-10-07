//go:build !windows

package cli

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
)

var errRootBrowser = errors.New("not opening a browser as root; open the URL yourself")

func removeBinary(path string) error {
	return os.Remove(path)
}

// openBrowser opens url in the user's default browser. A browser is never
// started as root: under `sudo` on macOS it is opened in the session of the
// user who ran sudo, and otherwise the URL is left for the user to open.
func openBrowser(url string) error {
	if os.Geteuid() != 0 {
		if runtime.GOOS == "darwin" {
			return exec.Command("open", url).Start()
		}
		return exec.Command("xdg-open", url).Start()
	}
	uid, user := os.Getenv("SUDO_UID"), os.Getenv("SUDO_USER")
	if runtime.GOOS == "darwin" && uid != "" && uid != "0" && user != "" {
		return exec.Command("launchctl", "asuser", uid, "sudo", "-u", user, "open", url).Start()
	}
	return errRootBrowser
}
