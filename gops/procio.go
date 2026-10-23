package gops

import "errors"

// ErrProcessNotFound is returned for a PID with no running process.
var ErrProcessNotFound = errors.New("process not found")
