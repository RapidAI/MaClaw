//go:build windows

package guiapp

import (
	"fmt"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// The launch retry exists for the low-end-machine case: a just-downloaded
// installer is still held by antivirus/indexer opens, which surface as
// sharing or access errors that clear within a second or two. A declined
// elevation prompt and genuinely terminal failures must never be retried —
// retrying a cancelled UAC would stack a second prompt behind the first.
func TestInstallerLaunchErrRetryableClassifiesFailures(t *testing.T) {
	if installerLaunchErrRetryable(nil) {
		t.Fatal("nil error is not a launch failure")
	}

	for _, code := range []uintptr{2, 3, 5, 26, 28, 30} {
		err := &shellExecuteError{code: code}
		if !installerLaunchErrRetryable(err) {
			t.Fatalf("SE_ERR code %d should be retried: %v", code, err)
		}
	}
	for _, code := range []uintptr{0, 8, 31, 32} {
		err := &shellExecuteError{code: code}
		if installerLaunchErrRetryable(err) {
			t.Fatalf("SE_ERR code %d must not be retried: %v", code, err)
		}
	}

	for _, errno := range []syscall.Errno{
		windows.ERROR_FILE_NOT_FOUND,
		windows.ERROR_PATH_NOT_FOUND,
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_LOCK_VIOLATION,
	} {
		if !installerLaunchErrRetryable(errno) {
			t.Fatalf("errno %d should be retried", errno)
		}
		if !installerLaunchErrRetryable(fmt.Errorf("outer: %w", errno)) {
			t.Fatalf("wrapped errno %d should be retried", errno)
		}
	}

	if installerLaunchErrRetryable(windows.ERROR_CANCELLED) {
		t.Fatal("a declined elevation prompt must not be retried")
	}
	if installerLaunchErrRetryable(fmt.Errorf("no such host")) {
		t.Fatal("unclassified errors must not be retried")
	}
}
