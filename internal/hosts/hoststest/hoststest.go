// Package hoststest provides throwaway hosts files so tests never touch the
// real /etc/hosts (or the Windows hosts file), including in CI.
package hoststest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devehab/locly-dns/internal/hosts"
)

// DefaultContent resembles a stock hosts file with a few custom user entries
// that LocalDNS must never modify.
const DefaultContent = `##
# Host Database
#
# localhost is used to configure the loopback interface
# when the system is booting.  Do not change this entry.
##
127.0.0.1	localhost
255.255.255.255	broadcasthost
::1             localhost

# custom entries added by the user
10.0.0.5	nas.home.arpa nas
192.168.1.20	printer.lan
`

// TemporaryHostsFile is a hosts file in a per-test temporary directory, e.g.
// /tmp/TestAdd123/001/hosts. It is deleted automatically when the test ends.
type TemporaryHostsFile struct {
	*hosts.DiskFile
	t *testing.T
}

// New creates a TemporaryHostsFile containing content.
func New(t *testing.T, content string) *TemporaryHostsFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("create temporary hosts file: %v", err)
	}
	return &TemporaryHostsFile{DiskFile: hosts.NewDiskFile(path), t: t}
}

// Content returns the file's current content.
func (f *TemporaryHostsFile) Content() string {
	f.t.Helper()
	b, err := os.ReadFile(f.Path())
	if err != nil {
		f.t.Fatalf("read temporary hosts file: %v", err)
	}
	return string(b)
}

// SetContent overwrites the file, simulating an edit by the user or another
// program.
func (f *TemporaryHostsFile) SetContent(content string) {
	f.t.Helper()
	if err := os.WriteFile(f.Path(), []byte(content), 0o644); err != nil {
		f.t.Fatalf("write temporary hosts file: %v", err)
	}
}
