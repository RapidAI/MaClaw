package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ProgramLogger writes programming tool output (Claude Code, Codex, etc.)
// to <MaclawBaseDir>/logs/program.log, independent of the maclaw.log detail gate.
// It is always gated by corelib.IsLogDetailEnabled().
type ProgramLogger struct {
	mu     sync.Mutex
	writer io.WriteCloser
}

var programLogger = &ProgramLogger{}

// Init creates or opens <MaclawBaseDir>/logs/program.log for append.
// If the file exceeds 10 MB it is rotated to program.log.1.
func (l *ProgramLogger) Init() {
	l.initAt(corelib.MaclawLogsDir())
}

func (l *ProgramLogger) initAt(dir string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writer != nil {
		l.writer.Close()
		l.writer = nil
	}
	if err := prepareLogDir(dir); err != nil {
		return
	}
	logPath := filepath.Join(dir, "program.log")
	if err := rejectSymlinkFile(logPath); err != nil {
		return
	}
	rotateLogIfLarge(logPath)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	l.writer = f
}

// Close closes the program.log file handle.
func (l *ProgramLogger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writer != nil {
		l.writer.Close()
		l.writer = nil
	}
}

// WriteLines writes multiple lines to program.log, gated by IsLogDetailEnabled.
func (l *ProgramLogger) WriteLines(sessionID, tool string, lines []string) {
	if !corelib.IsLogDetailEnabled() || len(lines) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.writer == nil {
		return
	}
	for _, line := range lines {
		_, _ = l.writer.Write([]byte(fmt.Sprintf("[%s] [%s] %s\n", sessionID, tool, line)))
	}
}
