//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// acquireGUIDaemonLock tries to obtain an exclusive, non-blocking file lock
// to serialize clawnet daemon startup across processes. Returns the open file
// (keep it open to hold the lock) or os.ErrExist if another process holds it.
// The lock is auto-released when the file is closed or the process exits.
func acquireGUIDaemonLock() (*os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, os.ErrNotExist
	}
	p := filepath.Join(home, ".openclaw", "clawnet", "daemon.lock")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)

	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, os.ErrExist
	}
	return f, nil
}
