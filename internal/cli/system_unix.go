//go:build !windows

package cli

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
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
	if runtime.GOOS != "darwin" {
		return errRootBrowser
	}
	// Open in the session of the user who ran sudo. The uid must be a plain
	// number and the account name comes from the system, not the environment.
	n, err := strconv.Atoi(os.Getenv("SUDO_UID"))
	if err != nil || n <= 0 {
		return errRootBrowser
	}
	u, err := user.LookupId(strconv.Itoa(n))
	if err != nil {
		return errRootBrowser
	}
	return exec.Command("launchctl", "asuser", u.Uid, "sudo", "-u", u.Username, "open", url).Start()
}
