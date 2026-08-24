package websearch

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// Dedicated download logger. The corelib package cannot rely on the GUI's
// global log routing, so download progress goes to its own file:
// <maclawpath.LogsDir()>/download.log (usually ~/.maclaw/logs/download.log).
var (
	dlOnce   sync.Once
	dlLogger *log.Logger
	dlFile   *os.File
	dlMu     sync.Mutex
	dlLines  int
)

const downloadLogMaxBytes = 5 << 20 // 5MB, rotated to download.log.1

func downloadLogger() *log.Logger {
	dlOnce.Do(func() {
		dir := maclawpath.LogsDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return
		}
		path := filepath.Join(dir, "download.log")
		if st, err := os.Stat(path); err == nil && st.Size() > downloadLogMaxBytes {
			_ = os.Rename(path, path+".1")
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		dlFile = f
		dlLogger = log.New(f, "", log.LstdFlags)
	})
	return dlLogger
}

// dlogf writes one line to the download log. Never fails the caller.
func dlogf(format string, args ...interface{}) {
	dlMu.Lock()
	defer dlMu.Unlock()
	l := downloadLogger()
	if l == nil {
		return
	}
	l.Printf(format, args...)
	dlLines++
	if dlLines >= 128 {
		dlLines = 0
		rotateDownloadLogIfLargeLocked()
	}
}

// rotateDownloadLogIfLargeLocked rotates the log mid-process when it has
// grown past the cap (init-time rotation only covers process restarts).
// Caller must hold dlMu.
func rotateDownloadLogIfLargeLocked() {
	if dlFile == nil {
		return
	}
	st, err := dlFile.Stat()
	if err != nil || st.Size() <= downloadLogMaxBytes {
		return
	}
	path := filepath.Join(maclawpath.LogsDir(), "download.log")
	_ = dlFile.Close()
	dlFile = nil
	dlLogger = nil
	dlOnce = sync.Once{} // next dlogf re-opens a fresh file
	_ = os.Rename(path, path+".1")
}

// LogDownloadf is the exported sink so sibling packages (e.g. corelib/browser
// for browser-side downloads) can write to the same download log.
func LogDownloadf(format string, args ...interface{}) {
	dlogf(format, args...)
}

// CloseDownloadLogger releases the shared download log file handle. It is
// intended for tests and process shutdown: the next dlogf call re-opens the
// file, mirroring the rotation path. On Windows an open handle prevents
// removing the log's directory (e.g. testing.TempDir cleanup).
func CloseDownloadLogger() {
	dlMu.Lock()
	defer dlMu.Unlock()
	if dlFile != nil {
		_ = dlFile.Close()
	}
	dlFile = nil
	dlLogger = nil
	dlOnce = sync.Once{}
}

// resetDownloadLoggerForTest redirects the download log to a fresh file under
// dir and returns a restore function. Used by tests to keep go-test output out
// of the real ~/.maclaw/logs/download.log.
func resetDownloadLoggerForTest(dir string) func() {
	dlMu.Lock()
	defer dlMu.Unlock()
	if dlFile != nil {
		_ = dlFile.Close()
	}
	dlFile = nil
	dlLogger = nil
	dlOnce = sync.Once{}
	prev := maclawpath.BaseDir()
	maclawpath.SetBaseDir(dir)
	return func() {
		dlMu.Lock()
		defer dlMu.Unlock()
		if dlFile != nil {
			_ = dlFile.Close()
		}
		dlFile = nil
		dlLogger = nil
		dlOnce = sync.Once{}
		maclawpath.SetBaseDir(prev)
	}
}

// redactHeaderKeys returns sorted-ish header names for logging without values,
// so Cookie/Authorization secrets never hit the log file.
func redactHeaderKeys(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, strings.TrimSpace(k))
	}
	return fmt.Sprintf("%v", keys)
}
