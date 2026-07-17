package commands

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// Package-level CLI stdout/stderr overrides. When nil, Stdout()/Stderr()
// return the live os.Stdout/os.Stderr so normal TUI usage and tests that
// redirect os.Stdout still work. CaptureOutput sets non-nil overrides so GUI
// handlers collect command output without process-wide redirection.
//
// CLI code in this package should write via Println/Printf/Eprintf/Stdout/Stderr
// rather than fmt.Println / os.Stdout directly on capturable paths.
//
// Writers returned by Stdout()/Stderr() are snapshots; prefer the Print*
// helpers which hold the stdio lock for the duration of the write so a concurrent
// CaptureOutput swap cannot race the write target.
var (
	stdioMu        sync.RWMutex
	stdoutOverride io.Writer // nil → os.Stdout
	stderrOverride io.Writer // nil → os.Stderr
)

func currentStdoutLocked() io.Writer {
	if stdoutOverride != nil {
		return stdoutOverride
	}
	return os.Stdout
}

func currentStderrLocked() io.Writer {
	if stderrOverride != nil {
		return stderrOverride
	}
	return os.Stderr
}

// Stdout returns the current CLI stdout writer snapshot.
// Prefer Print/Println/Printf when writing; they are race-safe vs CaptureOutput.
func Stdout() io.Writer {
	stdioMu.RLock()
	defer stdioMu.RUnlock()
	return currentStdoutLocked()
}

// Stderr returns the current CLI stderr writer snapshot.
// Prefer Eprint/Eprintln/Eprintf when writing; they are race-safe vs CaptureOutput.
func Stderr() io.Writer {
	stdioMu.RLock()
	defer stdioMu.RUnlock()
	return currentStderrLocked()
}

// swapStdio installs capture writers. Caller must serialize (captureMu).
// restore is panic-safe when deferred; nil override restores live os stdio.
func swapStdio(out, errW io.Writer) (restore func()) {
	stdioMu.Lock()
	oldOut, oldErr := stdoutOverride, stderrOverride
	stdoutOverride, stderrOverride = out, errW
	stdioMu.Unlock()
	return func() {
		stdioMu.Lock()
		stdoutOverride, stderrOverride = oldOut, oldErr
		stdioMu.Unlock()
	}
}

// withStdout runs fn with the current stdout while holding a read lock so
// swapStdio cannot change the target mid-write.
func withStdout(fn func(w io.Writer) (int, error)) (int, error) {
	stdioMu.RLock()
	defer stdioMu.RUnlock()
	return fn(currentStdoutLocked())
}

func withStderr(fn func(w io.Writer) (int, error)) (int, error) {
	stdioMu.RLock()
	defer stdioMu.RUnlock()
	return fn(currentStderrLocked())
}

// --- stdout helpers (preferred over fmt.Print* for capturable CLI output) ---

func Print(a ...any) (int, error) {
	return withStdout(func(w io.Writer) (int, error) { return fmt.Fprint(w, a...) })
}
func Println(a ...any) (int, error) {
	return withStdout(func(w io.Writer) (int, error) { return fmt.Fprintln(w, a...) })
}
func Printf(format string, a ...any) (int, error) {
	return withStdout(func(w io.Writer) (int, error) { return fmt.Fprintf(w, format, a...) })
}

// --- stderr helpers (flag usage, warnings) ---

func Eprint(a ...any) (int, error) {
	return withStderr(func(w io.Writer) (int, error) { return fmt.Fprint(w, a...) })
}
func Eprintln(a ...any) (int, error) {
	return withStderr(func(w io.Writer) (int, error) { return fmt.Fprintln(w, a...) })
}
func Eprintf(format string, a ...any) (int, error) {
	return withStderr(func(w io.Writer) (int, error) { return fmt.Fprintf(w, format, a...) })
}
