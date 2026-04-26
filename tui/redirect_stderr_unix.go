//go:build !windows

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// redirectStderr redirects file descriptor 2 (stderr) to the given file.
//
// On Unix, dup2 atomically replaces fd 2 with a copy of f's descriptor.
// After this call, ALL writes to stderr — from Go's log package,
// fmt.Fprintf(os.Stderr,...), corelib packages, CGo, or child processes —
// go to the log file. This is the standard Unix mechanism for output
// redirection and is used by professional TUI applications to prevent
// library logging from corrupting the alternate screen buffer.
//
// We use unix.Dup2 from golang.org/x/sys/unix instead of syscall.Dup2
// because syscall.Dup2 does not exist on linux/arm64 (which only has dup3).
// unix.Dup2 handles this transparently across all architectures.
func redirectStderr(f *os.File) {
	_ = unix.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}
