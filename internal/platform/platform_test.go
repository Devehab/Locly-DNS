package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSupportedOS(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if !SupportedOS(goos) {
			t.Errorf("%s should be supported", goos)
		}
	}
	if SupportedOS("plan9") {
		t.Error("plan9 should not be supported")
	}
}

func TestDefaultPaths(t *testing.T) {
	hosts, cfg := DefaultHostsFile(), DefaultConfigDir()
	if !filepath.IsAbs(hosts) || !filepath.IsAbs(cfg) {
		t.Fatalf("paths must be absolute: %q %q", hosts, cfg)
	}
	switch runtime.GOOS {
	case "windows":
		if filepath.Base(hosts) != "hosts" || filepath.Base(filepath.Dir(hosts)) != "etc" {
			t.Errorf("unexpected hosts path %q", hosts)
		}
	default:
		if hosts != "/etc/hosts" || cfg != "/etc/localdns" {
			t.Errorf("unexpected paths %q %q", hosts, cfg)
		}
	}
}
