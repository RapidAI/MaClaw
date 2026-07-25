package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const hubLogRotateBytes = 10 * 1024 * 1024

// hubLogFiles keeps process-lifetime file handles open so log.SetOutput stays valid.
var hubLogFiles struct {
	mu    sync.Mutex
	files []*os.File
}

// ConfigureLogging redirects the standard library logger to both stderr and files
// under logging.dir (from config). When dir is empty, logging stays on stderr only.
//
// Files:
//   - hub.log            — all hub logs
//   - registration.log   — only onboarding/registration lines
//
// Writers are flushed independently (not io.MultiWriter) so a broken stderr sink
// cannot suppress file writes.
func ConfigureLogging(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create logging dir %s: %w", dir, err)
	}

	hubPath := filepath.Join(dir, "hub.log")
	regPath := filepath.Join(dir, "registration.log")
	rotateLogIfLarge(hubPath)
	rotateLogIfLarge(regPath)

	hubFile, err := os.OpenFile(hubPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open hub log %s: %w", hubPath, err)
	}

	regFile, err := os.OpenFile(regPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_ = hubFile.Close()
		return fmt.Errorf("open registration log %s: %w", regPath, err)
	}

	hubLogFiles.mu.Lock()
	// Close any previous handles (e.g. reconfigure in tests) to avoid leaks.
	for _, f := range hubLogFiles.files {
		_ = f.Close()
	}
	hubLogFiles.files = []*os.File{hubFile, regFile}
	hubLogFiles.mu.Unlock()

	log.SetOutput(&hubLogWriter{
		file:    hubFile,
		regFile: regFile,
		stderr:  os.Stderr,
	})
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("[hub] logging initialized hub_log=%s registration_log=%s", hubPath, regPath)
	return nil
}

func rotateLogIfLarge(logPath string) {
	if info, err := os.Stat(logPath); err == nil && info.Size() > hubLogRotateBytes {
		prev := logPath + ".1"
		_ = os.Remove(prev)
		_ = os.Rename(logPath, prev)
	}
}

// hubLogWriter mirrors GUI detailAwareLogWriter: independent sinks, registration tee.
type hubLogWriter struct {
	file    io.Writer
	regFile io.Writer
	stderr  io.Writer
}

func (w *hubLogWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	isReg := isHubRegistrationLogLine(string(p))
	var firstErr error
	if w.file != nil {
		if _, err := w.file.Write(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if isReg && w.regFile != nil {
		if _, err := w.regFile.Write(p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if w.stderr != nil {
		_, _ = w.stderr.Write(p) // best-effort console/journal
	}
	if firstErr != nil {
		return 0, firstErr
	}
	return len(p), nil
}

func isHubRegistrationLogLine(line string) bool {
	lower := strings.ToLower(line)
	needles := []string{
		"[onboarding-email]",
		"[onboarding-sms]",
		"[onboarding-auth]",
		"[registration-sms]",
		"[registration-contact]",
		"[onboarding] ",
		"[onboarding]",
	}
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// prefixFilterWriter is retained for unit tests of needle matching semantics.
type prefixFilterWriter struct {
	w       io.Writer
	needles []string
}

func (p prefixFilterWriter) Write(b []byte) (int, error) {
	if p.w == nil || len(b) == 0 {
		return len(b), nil
	}
	s := string(b)
	for _, n := range p.needles {
		if n != "" && strings.Contains(s, n) {
			if _, err := p.w.Write(b); err != nil {
				return 0, err
			}
			return len(b), nil
		}
	}
	return len(b), nil
}
