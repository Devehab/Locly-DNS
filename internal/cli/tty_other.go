//go:build !linux && !darwin && !windows

package cli

import "os"

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func enableANSI(*os.File) bool { return true }
