//go:build !windows

package main

import (
	"os"
	"syscall"
)

// redirectStderr redirects file descriptor 2 (stderr) to the given file.
//
// On Unix, dup2 atomically replaces fd 2 with a copy of f's descriptor.
// After this call, ALL writes to stderr — from Go's log package,
// fmt.Fprintf(os.Stderr,...), corelib packages, CGo, or child processes —
// go to the log file. This is the standard Unix mechanism for output
// redirection and is used by professional TUI applications to prevent
// library logging from corrupting the alternate screen buffer.
func redirectStderr(f *os.File) {
	_ = syscall.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}
