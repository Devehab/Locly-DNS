// Package fsutil contains small filesystem helpers shared by LocalDNS
// packages.
package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// CheckWritable reports whether path could be written (or, if it does not
// exist yet, created) by the current process, without modifying anything
// visible. Permission problems wrap fs.ErrPermission.
func CheckWritable(path string) error {
	for p := filepath.Clean(path); ; {
		_, err := os.Stat(p)
		if err == nil {
			return checkWritable(p)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return err
		}
		p = parent
	}
}

// IsPermission reports whether err is a permission error.
func IsPermission(err error) bool {
	return errors.Is(err, fs.ErrPermission)
}
