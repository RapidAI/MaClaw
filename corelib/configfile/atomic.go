// Package configfile provides atomic file write utilities for tool configuration files.
// Delegates to corelib/fileutil.AtomicWriteFile for the core temp-file-then-rename
// pattern, adding directory auto-creation and logging on top.
package configfile

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

// AtomicWrite writes data to path atomically using fileutil.AtomicWriteFile.
// It ensures the parent directory exists before writing.
func AtomicWrite(path string, data []byte) error {
	start := time.Now()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}

	if err := fileutil.AtomicWriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("atomic write %s: %w", path, err)
	}
	log.Printf("[config] AtomicWrite:done total=%s path=%s", time.Since(start), path)
	return nil
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
