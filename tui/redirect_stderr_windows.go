//go:build windows

package main

import (
	"log"
	"os"

	"golang.org/x/sys/windows"
)

// redirectStderr redirects all stderr output to the given file.
//
// On Windows, three separate mechanisms must be covered:
//
//  1. SetStdHandle — redirects the Win32 STD_ERROR_HANDLE so that child
//     processes and C/CGo code writing to stderr go to the log file.
//
//  2. os.Stderr = f — redirects Go code that uses fmt.Fprintf(os.Stderr,...)
//     or os.Stderr.WriteString(...). Unlike Unix where dup2 atomically
//     replaces the fd, Windows os.File objects hold a private HANDLE that
//     is NOT updated by SetStdHandle. Reassigning the Go variable is the
//     only way to redirect Go-level writes.
//
//  3. log.SetOutput(f) — redirects Go's standard log package. Its internal
//     writer captured the original os.Stderr at init time and is not
//     affected by reassigning os.Stderr.
//
// Together these three cover every output path: Go's log package, Go's
// fmt/os packages, CGo, and child processes.
func redirectStderr(f *os.File) {
	// 1. Win32 process-level handle (child processes, C code).
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
	// 2. Go-level os.Stderr (fmt.Fprintf, os.Stderr.Write, etc.).
	os.Stderr = f
	// 3. Go's standard log package (captured original os.Stderr at init).
	log.SetOutput(f)
}
