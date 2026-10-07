package core

import (
	"errors"
	"fmt"
)

// Code classifies an error so the CLI can choose an exit code and the UI an
// HTTP status. Codes are part of the JSON output contract.
type Code string

// Error codes.
const (
	CodeInvalidInput  Code = "invalid_input"     // bad hostname or address
	CodeNotFound      Code = "not_found"         // hostname is not managed and not in the hosts file
	CodeExists        Code = "already_exists"    // hostname already managed with a different address
	CodeConflict      Code = "conflict"          // hostname is defined outside the LocalDNS section
	CodeNotManaged    Code = "not_managed"       // removal requested for an entry LocalDNS does not own
	CodePermission    Code = "permission_denied" // hosts file or config not writable
	CodeHostsInvalid  Code = "hosts_invalid"     // LocalDNS section of the hosts file is malformed
	CodeHostsNotFound Code = "hosts_not_found"   // hosts file missing
	CodeConfigInvalid Code = "config_invalid"    // config.json is malformed
	CodeInternal      Code = "internal"          // anything else
)

// Error is a LocalDNS error with a stable code, a message and an optional
// hint telling the user (or agent) how to fix it.
type Error struct {
	Code    Code
	Message string
	Hint    string
	Err     error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Err }

// ErrorCode returns the Code of err, or CodeInternal.
func ErrorCode(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeInternal
}

// AsError converts any error into an *Error.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeInternal, Message: err.Error(), Err: err}
}

func newError(code Code, hint string, err error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Hint: hint, Err: err}
}
