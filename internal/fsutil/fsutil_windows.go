//go:build windows

package fsutil

import (
	"os"
)

func checkWritable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		f, err := os.CreateTemp(path, ".localdns-check-*")
		if err != nil {
			return err
		}
		name := f.Name()
		_ = f.Close()
		return os.Remove(name)
	}
	// Opening for write access does not change the file's content or times.
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return f.Close()
}
