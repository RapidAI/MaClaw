package browser

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// AuditLogger records operations performed in connect_user mode for security auditing.
// Only active when the session is connected to the user's real Chrome (not isolated).
type AuditLogger struct {
	mu      sync.Mutex
	logPath string
	file    *os.File
}

// NewAuditLogger creates an audit logger that writes to the given directory.
// The log file is created at {logDir}/browser_connect_audit.log.
func NewAuditLogger(logDir string) *AuditLogger {
	if logDir == "" {
		logDir = corelib.MaclawLogsDir()
		if logDir == "" {
			return &AuditLogger{} // no-op logger
		}
	}
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "browser_connect_audit.log")
	return &AuditLogger{logPath: logPath}
}

func (l *AuditLogger) write(entry string) {
	if l == nil || l.logPath == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		f, err := os.OpenFile(l.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
		l.file = f
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(l.file, "%s %s\n", timestamp, entry)
}

// LogConnect records a successful connection to the user's Chrome.
func (l *AuditLogger) LogConnect(sessionID, addr string) {
	l.write(fmt.Sprintf("[CONNECT] session_id=%s addr=%s", sessionID, addr))
}

// LogDisconnect records a disconnection.
func (l *AuditLogger) LogDisconnect(sessionID, reason string) {
	l.write(fmt.Sprintf("[DISCONNECT] session_id=%s reason_len=%d", sessionID, len([]rune(reason))))
}

// LogNavigation records a page navigation.
func (l *AuditLogger) LogNavigation(sessionID, url string) {
	l.write(fmt.Sprintf("[NAVIGATE] session_id=%s url=%s", sessionID, SafeURLForLog(url)))
}

// LogAction records a generic browser action.
func (l *AuditLogger) LogAction(sessionID, action, detail string) {
	l.write(fmt.Sprintf("[ACTION] session_id=%s action=%s detail_len=%d", sessionID, action, len([]rune(detail))))
}

func SafeURLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return fmt.Sprintf("<invalid len=%d>", len([]rune(raw)))
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String()
}

// Close flushes and closes the log file.
func (l *AuditLogger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
}

// globalAuditLogger is the package-level audit logger instance.
var globalAuditLogger *AuditLogger
var auditLoggerOnce sync.Once

// GetAuditLogger returns the singleton audit logger.
func GetAuditLogger() *AuditLogger {
	auditLoggerOnce.Do(func() {
		globalAuditLogger = NewAuditLogger(corelib.MaclawLogsDir())
	})
	return globalAuditLogger
}
