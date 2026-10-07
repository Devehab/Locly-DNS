package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckWritable(t *testing.T) {
	dir := t.TempDir()
	if err := CheckWritable(dir); err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	// A path that does not exist yet is writable if its nearest existing
	// parent is.
	if err := CheckWritable(filepath.Join(dir, "a", "b", "c")); err != nil {
		t.Fatalf("missing path: %v", err)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission checks need a non-root unix user")
	}
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	err := CheckWritable(filepath.Join(ro, "child"))
	if !IsPermission(err) {
		t.Fatalf("expected permission error, got %v", err)
	}
}
