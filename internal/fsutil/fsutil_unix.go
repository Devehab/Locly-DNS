//go:build !windows

package fsutil

import (
	"io/fs"
	"syscall"
)

const wOK = 0x2

func checkWritable(path string) error {
	if err := syscall.Access(path, wOK); err != nil {
		return &fs.PathError{Op: "access", Path: path, Err: err}
	}
	return nil
}
