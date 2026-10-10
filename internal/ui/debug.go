package ui

import "os"

// DebugEnabled reports whether verbose CLI diagnostics are enabled.
func DebugEnabled() bool {
	return os.Getenv("DEBUG") == "true"
}

// Debugln writes a muted diagnostic only when DEBUG=true.
func Debugln(format string, a ...interface{}) {
	if DebugEnabled() {
		Mutedln("[debug] "+format, a...)
	}
}
