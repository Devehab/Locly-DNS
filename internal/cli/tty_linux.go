//go:build linux

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t))) //nolint:gosec // G103: the ioctl needs a pointer to the termios struct
	return errno == 0
}

func enableANSI(*os.File) bool { return true }
