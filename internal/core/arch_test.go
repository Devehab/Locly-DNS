package core

import (
	"os/exec"
	"strings"
	"testing"
)

// TestCoreHasNoNetworkingOrProcessExecution proves, from the dependency
// graph, that the business logic cannot open sockets, run a DNS server,
// make outbound requests or launch programs. Only the UI package (loopback
// HTTP) and the CLI (sudo, browser) may do those things.
func TestCoreHasNoNetworkingOrProcessExecution(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not available")
	}
	forbidden := []string{"net", "net/http", "net/rpc", "net/smtp", "crypto/tls", "os/exec", "plugin"}
	pkgs := []string{
		"github.com/devehab/locly-dns/internal/core",
		"github.com/devehab/locly-dns/internal/hosts",
		"github.com/devehab/locly-dns/internal/config",
		"github.com/devehab/locly-dns/internal/validate",
		"github.com/devehab/locly-dns/internal/platform",
		"github.com/devehab/locly-dns/internal/fsutil",
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		cmd := exec.Command(goBin, append([]string{"list", "-deps"}, pkgs...)...)
		cmd.Env = append(cmd.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list (%s): %v", goos, err)
		}
		deps := map[string]bool{}
		for _, d := range strings.Fields(string(out)) {
			deps[d] = true
		}
		for _, f := range forbidden {
			if deps[f] {
				t.Errorf("GOOS=%s: core packages depend on %q; networking and process execution are not allowed in core", goos, f)
			}
		}
	}
}
