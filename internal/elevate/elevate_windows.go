//go:build windows

package elevate

import "errors"

var errUnavailable = errors.New("run LocalDNS from a terminal opened with 'Run as administrator'")

// Default returns the platform's Elevator. On Windows, elevation through UAC
// opens a separate console whose output cannot be captured, so LocalDNS asks
// the user to start an administrator terminal instead.
func Default() Elevator { return None{} }
