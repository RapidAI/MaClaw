// Package fileutil provides file utility functions including atomic file writes.
package fileutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

// AtomicWriteFile writes data to a file atomically by first writing to a
// temporary file in the same directory, then renaming it to the target path.
// This ensures that a crash during write never leaves a half-written file.
//
// If the target file already exists, its permissions are preserved regardless
// of the perm argument. If the target does not exist, perm is used.
//
// On rename failure due to cross-device errors (EXDEV on Unix, ERROR_NOT_SAME_DEVICE
// on Windows), the function falls back to a copy-and-rename strategy.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	// Preserve existing file permissions when the target already exists.
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}

	// Create a temp file in the same directory as the target to ensure
	// same-volume placement (required for atomic rename).
	tmp, err := os.CreateTemp(dir, ".tmp")
	if err != nil {
		return fmt.Errorf("atomic write: create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Ensure cleanup of the temp file on any failure path.
	success := false
	defer func() {
		if !success {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// Write data to the temp file.
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("atomic write: write data: %w", err)
	}

	// Set permissions on the temp file before rename.
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("atomic write: set permissions: %w", err)
	}

	// Sync to ensure data is flushed to disk before rename.
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("atomic write: sync: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("atomic write: close temp file: %w", err)
	}

	// Attempt atomic rename.
	if err := renameAtomicFile(tmpPath, path); err != nil {
		if isCrossDeviceError(err) {
			// Fall back to copy-and-rename for cross-device scenarios.
			if copyErr := crossDeviceFallback(tmpPath, path, perm); copyErr != nil {
				return fmt.Errorf("atomic write: cross-device fallback: %w", copyErr)
			}
			success = true
			return nil
		}
		return fmt.Errorf("atomic write: rename: %w", err)
	}

	success = true
	return nil
}

func renameAtomicFile(tmpPath, targetPath string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(tmpPath, targetPath)
	}

	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		if err := os.Rename(tmpPath, targetPath); err == nil {
			return nil
		} else {
			lastErr = err
			if !isRetryableWindowsRenameError(err) {
				return err
			}
		}

		removeErr := os.Remove(targetPath)
		if removeErr != nil && !os.IsNotExist(removeErr) && !isRetryableWindowsRemoveError(removeErr) {
			return lastErr
		}

		if err := os.Rename(tmpPath, targetPath); err == nil {
			return nil
		} else {
			lastErr = err
			if !isRetryableWindowsRenameError(err) {
				return err
			}
		}

		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}

	return lastErr
}

func isRetryableWindowsRenameError(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}

	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		var errno syscall.Errno
		if errors.As(linkErr.Err, &errno) {
			return errno == 5 || errno == 32
		}
	}
	return false
}

func isRetryableWindowsRemoveError(err error) bool {
	if runtime.GOOS != "windows" {
		return false
	}

	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		var errno syscall.Errno
		if errors.As(pathErr.Err, &errno) {
			return errno == 5 || errno == 32
		}
	}
	return false
}

// isCrossDeviceError checks whether an error is a cross-device link error
// (EXDEV on Unix, ERROR_NOT_SAME_DEVICE on Windows).
func isCrossDeviceError(err error) bool {
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) {
		return false
	}

	if runtime.GOOS == "windows" {
		// Windows: ERROR_NOT_SAME_DEVICE = 17
		var errno syscall.Errno
		if errors.As(linkErr.Err, &errno) {
			return errno == 17
		}
		return false
	}

	// Unix: EXDEV
	var errno syscall.Errno
	if errors.As(linkErr.Err, &errno) {
		return errno == syscall.EXDEV
	}
	return false
}

// crossDeviceFallback copies the temp file to the target path and removes
// the temp file. Used when os.Rename fails with a cross-device error.
func crossDeviceFallback(tmpPath, targetPath string, perm os.FileMode) error {
	src, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open temp file: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create target file: %w", err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return fmt.Errorf("copy data: %w", err)
	}

	if err := dst.Sync(); err != nil {
		_ = dst.Close()
		return fmt.Errorf("sync target: %w", err)
	}

	if err := dst.Close(); err != nil {
		return fmt.Errorf("close target: %w", err)
	}

	// Clean up the temp file.
	_ = os.Remove(tmpPath)

	return nil
}
