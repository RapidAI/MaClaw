// Package weixin — wxlog.go provides a structured file logger for WeChat
// channel diagnostics. Logs are written to ~/.maclaw/logs/im_wx.log.
package weixin

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	wxLogFileName   = "im_wx.log"
	wxLogMaxBytes   = 5 * 1024 * 1024 // 5 MB — truncate on open if exceeded
	wxLogTimeFormat = "2006-01-02 15:04:05.000"
)

// WxLog is a concurrency-safe file logger for WeChat IM diagnostics.
type WxLog struct {
	mu   sync.Mutex
	file *os.File
}

var (
	globalWxLog     *WxLog
	globalWxLogOnce sync.Once
)

// SetLogDetailEnabled is kept for the app-wide detailed-log switch, but WeChat
// channel diagnostics are always written to im_wx.log. Voice delivery failures
// need an independent trace even when general detailed logging is disabled.
func SetLogDetailEnabled(_ bool) {
}

// GetWxLog returns the singleton WxLog instance, creating the log file on
// first call. Safe to call from any goroutine.
func GetWxLog() *WxLog {
	globalWxLogOnce.Do(func() {
		dir := maclawLogsDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("[wxlog] cannot create log dir: %v", err)
			return
		}
		path := filepath.Join(dir, wxLogFileName)

		// Truncate if file exceeds max size.
		if info, err := os.Stat(path); err == nil && info.Size() > wxLogMaxBytes {
			_ = os.Truncate(path, 0)
		}

		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Printf("[wxlog] cannot open log file: %v", err)
			return
		}
		globalWxLog = &WxLog{file: f}
	})
	return globalWxLog
}

func maclawLogsDir() string {
	return filepath.Join(maclawBaseDir(), "logs")
}

func maclawBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".maclaw")
	}
	defaultDir := filepath.Join(home, ".maclaw")
	data, err := os.ReadFile(filepath.Join(defaultDir, "config.json"))
	if err != nil {
		return defaultDir
	}
	var partial struct {
		DataDir string `json:"data_dir"`
	}
	if json.Unmarshal(data, &partial) != nil {
		return defaultDir
	}
	if dataDir := strings.TrimSpace(partial.DataDir); dataDir != "" {
		return dataDir
	}
	return defaultDir
}

// Log writes a structured line: timestamp | stage | direction | uid | message
func (w *WxLog) Log(stage, direction, uid, format string, args ...any) {
	if w == nil || w.file == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s | %-20s | %-3s | %-20s | %s\n",
		time.Now().Format(wxLogTimeFormat), stage, direction, uid, msg)
	w.mu.Lock()
	_, _ = w.file.WriteString(line)
	w.mu.Unlock()
}

// Close closes the underlying file.
func (w *WxLog) Close() {
	if w == nil || w.file == nil {
		return
	}
	w.mu.Lock()
	_ = w.file.Close()
	w.mu.Unlock()
}
