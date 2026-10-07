package hosts

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/devehab/locly-dns/internal/fsutil"
)

// ErrChanged is returned by Replace when the file changed on disk after it
// was read, so the edit was not applied.
var ErrChanged = errors.New("hosts file changed while LocalDNS was editing it; nothing was written, please retry")

// File is where a hosts file lives. Production code uses DiskFile on the
// operating system's hosts file; tests use a temporary copy (see hoststest).
type File interface {
	// Path is the file's location, for messages.
	Path() string
	// Read returns the current content.
	Read() ([]byte, error)
	// Replace writes next, but only if the content is still old.
	Replace(old, next []byte) error
	// CheckWritable reports whether Replace could succeed, without modifying
	// anything. Permission problems wrap fs.ErrPermission.
	CheckWritable() error
}

// DiskFile is a hosts file on the local filesystem.
type DiskFile struct {
	path string
}

// NewDiskFile returns a File backed by path.
func NewDiskFile(path string) *DiskFile { return &DiskFile{path: path} }

// Path implements File.
func (f *DiskFile) Path() string { return f.path }

// Read implements File.
func (f *DiskFile) Read() ([]byte, error) { return os.ReadFile(f.path) }

// CheckWritable implements File.
func (f *DiskFile) CheckWritable() error { return fsutil.CheckWritable(f.path) }

// Replace implements File. The file is rewritten in place (not replaced by a
// new file) so its owner, permissions, attributes and inode are preserved;
// this matters for /etc/hosts on macOS, bind-mounted hosts files in
// containers, and the Windows hosts file. If the write fails part-way, the
// original content is restored.
func (f *DiskFile) Replace(old, next []byte) (err error) {
	fh, err := os.OpenFile(f.path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := fh.Close(); err == nil {
			err = cerr
		}
	}()

	current, err := io.ReadAll(fh)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, old) {
		return ErrChanged
	}
	if err := writeAt0(fh, next); err != nil {
		if rerr := writeAt0(fh, old); rerr != nil {
			return fmt.Errorf("writing %s failed (%w) and restoring it failed too (%w)", f.path, err, rerr)
		}
		return err
	}
	return nil
}

func writeAt0(fh *os.File, data []byte) error {
	if _, err := fh.WriteAt(data, 0); err != nil {
		return err
	}
	if err := fh.Truncate(int64(len(data))); err != nil {
		return err
	}
	return fh.Sync()
}
