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

func openBrowser(url string) error {
	if os.Geteuid() == 0 {
		// Never launch a browser as root.
		return errRootBrowser
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Start()
}
