package hosts_test

import (
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/devehab/locly-dns/internal/hosts"
	"github.com/devehab/locly-dns/internal/hosts/hoststest"
)

func TestDiskFileReplace(t *testing.T) {
	f := hoststest.New(t, "old content\n")
	if err := f.Replace([]byte("old content\n"), []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if got := f.Content(); got != "new\n" {
		t.Fatalf("got %q", got)
	}
	// Shrinking truncates leftovers from the longer content.
	if err := f.Replace([]byte("new\n"), []byte("n")); err != nil {
		t.Fatal(err)
	}
	if got := f.Content(); got != "n" {
		t.Fatalf("got %q", got)
	}
}

func TestDiskFileReplaceDetectsConcurrentEdit(t *testing.T) {
	f := hoststest.New(t, "v1\n")
	f.SetContent("edited by someone else\n")
	err := f.Replace([]byte("v1\n"), []byte("v2\n"))
	if !errors.Is(err, hosts.ErrChanged) {
		t.Fatalf("expected ErrChanged, got %v", err)
	}
	if got := f.Content(); got != "edited by someone else\n" {
		t.Fatalf("file was modified: %q", got)
	}
}

func TestDiskFilePreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	f := hoststest.New(t, "x\n")
	if err := os.Chmod(f.Path(), 0o640); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(f.Path())
	if err := f.Replace([]byte("x\n"), []byte("y\n")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(f.Path())
	if after.Mode() != before.Mode() || !os.SameFile(before, after) {
		t.Fatalf("file identity or mode changed: %v -> %v", before.Mode(), after.Mode())
	}
}

func TestCheckWritable(t *testing.T) {
	f := hoststest.New(t, "x\n")
	if err := f.CheckWritable(); err != nil {
		t.Fatalf("temp file should be writable: %v", err)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("read-only check needs a non-root unix user")
	}
	if err := os.Chmod(f.Path(), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := f.CheckWritable(); err == nil {
		t.Fatal("read-only file reported as writable")
	}
}
