//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation

#import <Foundation/Foundation.h>
#include <stdlib.h>

static char* nativeTempDir() {
	NSString *tempDir = NSTemporaryDirectory();
	return strdup([tempDir UTF8String]);
}
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const lockUniqueID = "maclaw-lock"

// cleanStaleLock removes the Wails SingleInstanceLock file if the owning
// process is no longer alive.  This prevents the "silent exit" problem where
// a previous crash leaves a lock file behind and every subsequent launch
// calls os.Exit(0) without showing any window.
func cleanStaleLock() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[startup] cleanStaleLock recovered from panic: %v\n", r)
		}
	}()

	cDir := C.nativeTempDir()
	tempDir := C.GoString(cDir)
	C.free(unsafe.Pointer(cDir))

	lockPath := filepath.Join(tempDir, lockUniqueID+".lock")

	f, err := os.OpenFile(lockPath, os.O_WRONLY, 0o600)
	if err != nil {
		// File doesn't exist or can't be opened — nothing to clean.
		return
	}

	// Try to acquire an exclusive non-blocking lock.
	// If we succeed, no other process holds it → the lock is stale.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// Another live instance actually holds the lock — leave it alone.
		f.Close()
		return
	}

	// We got the lock, meaning the previous owner is gone. Release, close, and remove.
	// NOTE: lockUniqueID must match SingleInstanceLock.UniqueId in main.go.
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	f.Close()
	if err := os.Remove(lockPath); err == nil {
		fmt.Println("[startup] removed stale lock file:", lockPath)
	}
}
