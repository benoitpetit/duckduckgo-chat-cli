//go:build !linux

// Package ptytest allocates pseudo-terminals so tests can exercise the code
// paths that only run on a real terminal.
package ptytest

import (
	"errors"
	"os"
)

// ErrUnsupported is returned on platforms without a /dev/ptmx equivalent.
var ErrUnsupported = errors.New("ptytest: pseudo-terminals are not supported on this platform")

// Open returns a connected master/slave pseudo-terminal pair.
func Open() (master, slave *os.File, err error) {
	return nil, nil, ErrUnsupported
}

// Capture collects everything write emits on the pseudo-terminal.
func Capture(master, slave *os.File, write func() error) (string, error) {
	return "", ErrUnsupported
}
