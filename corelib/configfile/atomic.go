// Package configfile provides atomic file write utilities for tool configuration files.
// Inspired by cc-switch's atomic_write pattern: write to temp file then rename,
// preventing half-written config corruption.
package configfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AtomicWrite writes data to path atomically: write temp file → rename.
// On Windows, removes target first since rename over existing file fails.
func AtomicWrite(path string, data []byte) error {
	start := time.Now()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	writeStart := time.Now()
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write temp %s: %w", tmp, err)
	}
	log.Printf("[config] AtomicWrite:write_temp=%s path=%s", time.Since(writeStart), path)

	if runtime.GOOS == "windows" {
		var lastErr error
		for i := 0; i < 5; i++ {
			removeStart := time.Now()
			_ = os.Remove(path) // best-effort remove before rename
			log.Printf("[config] AtomicWrite:remove_target attempt=%d duration=%s path=%s", i+1, time.Since(removeStart), path)
			renameStart := time.Now()
			if err := os.Rename(tmp, path); err == nil {
				log.Printf("[config] AtomicWrite:rename attempt=%d duration=%s path=%s", i+1, time.Since(renameStart), path)
				log.Printf("[config] AtomicWrite:done total=%s path=%s", time.Since(start), path)
				return nil
			} else {
				lastErr = err
				log.Printf("[config] AtomicWrite:rename_failed attempt=%d duration=%s path=%s err=%v", i+1, time.Since(renameStart), path, err)
				if !isWindowsRetryableRenameError(err) {
					break
				}
				time.Sleep(time.Duration(i+1) * 10 * time.Millisecond)
			}
		}
		_ = os.Remove(tmp)
		return fmt.Errorf("atomic rename %s → %s: %w", tmp, path, lastErr)
	}
	renameStart := time.Now()
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomic rename %s → %s: %w", tmp, path, err)
	}
	log.Printf("[config] AtomicWrite:rename duration=%s path=%s", time.Since(renameStart), path)
	log.Printf("[config] AtomicWrite:done total=%s path=%s", time.Since(start), path)
	return nil
}

func isWindowsRetryableRenameError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "access is denied") || strings.Contains(msg, "being used by another process")
}

// AtomicWriteJSON writes a JSON value atomically with pretty-printing.
func AtomicWriteJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	data = append(data, '\n')
	return AtomicWrite(path, data)
}
