package desktopd

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Process log files. systemd 237 (Ubuntu 18.04) rejects StandardOutput=append:
// with "Failed to parse output specifier" and then leaves stdout on the journal,
// so a unit that names logs/desktopd.log never creates that file. desktopd
// writes the file itself. Stderr is still mirrored by default, which keeps
// journalctl -u maclaw-desktopd working on every systemd version.

const (
	defaultLogPath     = "logs/desktopd.log"
	defaultLogMaxBytes = 32 << 20 // 32 MiB
	defaultLogMaxFiles = 5
)

// ConfigureProcessLog points the standard log at a rotating file. An empty
// DESKTOPD_LOG_FILE uses logs/desktopd.log, relative to the process working
// directory (the systemd unit's WorkingDirectory). "off", "stderr", or "-"
// leaves logging on stderr only. DESKTOPD_LOG_STDERR=0 skips the stderr copy.
func ConfigureProcessLog() error {
	path := strings.TrimSpace(os.Getenv("DESKTOPD_LOG_FILE"))
	switch strings.ToLower(path) {
	case "off", "stderr", "-":
		return nil
	case "":
		path = defaultLogPath
	}
	maxBytes := int64(defaultLogMaxBytes)
	if raw := strings.TrimSpace(os.Getenv("DESKTOPD_LOG_MAX_BYTES")); raw != "" {
		n, err := parseByteSize(raw)
		if err != nil {
			return fmt.Errorf("DESKTOPD_LOG_MAX_BYTES: %w", err)
		}
		maxBytes = n
	}
	maxFiles := defaultLogMaxFiles
	if raw := strings.TrimSpace(os.Getenv("DESKTOPD_LOG_MAX_FILES")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return fmt.Errorf("DESKTOPD_LOG_MAX_FILES must be a positive integer, not %q", raw)
		}
		maxFiles = n
	}
	w, err := newRotatingFile(path, maxBytes, maxFiles)
	if err != nil {
		return err
	}
	out := io.Writer(w)
	if mirrorStderr() {
		out = stderrTee{file: w, stderr: os.Stderr}
	}
	log.SetOutput(out)
	log.Printf("[desktopd] logging to %s (rotate every %d bytes, keep %d)", path, maxBytes, maxFiles)
	return nil
}

func mirrorStderr() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DESKTOPD_LOG_STDERR"))) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// parseByteSize accepts a positive integer byte count, or the same number
// with a K/M/G suffix (binary KiB/MiB/GiB for K/M/G and KiB/MiB/GiB; decimal
// for KB/MB/GB).
func parseByteSize(raw string) (int64, error) {
	s := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "KIB"):
		s, mult = strings.TrimSuffix(s, "KIB"), 1<<10
	case strings.HasSuffix(s, "MIB"):
		s, mult = strings.TrimSuffix(s, "MIB"), 1<<20
	case strings.HasSuffix(s, "GIB"):
		s, mult = strings.TrimSuffix(s, "GIB"), 1<<30
	case strings.HasSuffix(s, "KB"):
		s, mult = strings.TrimSuffix(s, "KB"), 1000
	case strings.HasSuffix(s, "MB"):
		s, mult = strings.TrimSuffix(s, "MB"), 1000*1000
	case strings.HasSuffix(s, "GB"):
		s, mult = strings.TrimSuffix(s, "GB"), 1000*1000*1000
	case strings.HasSuffix(s, "K"):
		s, mult = strings.TrimSuffix(s, "K"), 1<<10
	case strings.HasSuffix(s, "M"):
		s, mult = strings.TrimSuffix(s, "M"), 1<<20
	case strings.HasSuffix(s, "G"):
		s, mult = strings.TrimSuffix(s, "G"), 1<<30
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("size %q is invalid", raw)
	}
	if n > (1<<62)/mult {
		return 0, fmt.Errorf("size %q is too large", raw)
	}
	return n * mult, nil
}

// stderrTee writes the persistent copy first, then the journal copy. A full
// disk must not drop the line from stderr, and a closed stderr must not drop
// the line from the file.
type stderrTee struct {
	file   io.Writer
	stderr io.Writer
}

func (t stderrTee) Write(p []byte) (int, error) {
	n, err := t.file.Write(p)
	if t.stderr != nil {
		_, _ = t.stderr.Write(p)
	}
	if err != nil {
		return n, err
	}
	return len(p), nil
}

// rotatingFile appends to path and renames it aside once the next write would
// pass maxBytes. path.1 is the newest rotated file, path.<maxFiles> the oldest.
// The standard logger serializes writes, and the mutex covers any other caller.
type rotatingFile struct {
	path     string
	maxBytes int64
	maxFiles int

	mu   sync.Mutex
	f    *os.File
	size int64
}

func newRotatingFile(path string, maxBytes int64, maxFiles int) (*rotatingFile, error) {
	if maxBytes <= 0 {
		maxBytes = defaultLogMaxBytes
	}
	if maxFiles < 1 {
		maxFiles = 1
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	w := &rotatingFile{path: path, maxBytes: maxBytes, maxFiles: maxFiles}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingFile) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.f = f
	w.size = info.Size()
	return nil
}

func (w *rotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil && w.f == nil {
			if openErr := w.open(); openErr != nil {
				return 0, err
			}
		}
	}
	if w.f == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate shifts path.N aside. A failed rename keeps the current file open so
// the line is still persisted.
func (w *rotatingFile) rotate() error {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.maxFiles))
	for i := w.maxFiles - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", w.path, i)
		to := fmt.Sprintf("%s.%d", w.path, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		_ = w.open()
		return err
	}
	w.size = 0
	return w.open()
}
