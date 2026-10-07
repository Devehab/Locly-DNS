//go:build !darwin && !linux && !windows

package router

// State reports that no service manager is supported here.
func State() ServiceState { return ServiceState{Kind: "none"} }

// Enable is not supported on this OS.
func Enable(string) error { return ErrUnsupported }

// Disable is not supported on this OS.
func Disable() error { return nil }

// WritePID is a no-op.
func WritePID() func() { return func() {} }
