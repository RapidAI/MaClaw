//go:build !windows

package clawnet

import (
	"os"
	"path/filepath"
	"syscall"
)

// daemonLockPath returns the path to the cross-process lock file used to
// serialize daemon startup attempts.
func daemonLockPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "clawnet", "daemon.lock")
}

// acquireDaemonLock tries to obtain an exclusive, non-blocking lock on the
// daemon lock file. On success it returns the open file handle (which must
// be kept open to hold the lock) and a nil error. If another process already
// holds the lock it returns os.ErrExist.
//
// The lock is automatically released when the returned file is closed or the
// process exits — no manual cleanup needed.
func acquireDaemonLock() (*os.File, error) {
	p := daemonLockPath()
	if p == "" {
		return nil, os.ErrNotExist
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)

	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	// Try non-blocking exclusive lock.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, os.ErrExist
	}
	return f, nil
}
