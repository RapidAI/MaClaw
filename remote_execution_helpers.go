package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// --- Executable Validation ---

// resolveExecutablePath validates and resolves a command path.
// It handles relative paths via LookPath, checks accessibility,
// and rejects directories. All execution strategies share this logic.
func resolveExecutablePath(execPath string) (string, error) {
	if !filepath.IsAbs(execPath) {
		resolved, err := exec.LookPath(execPath)
		if err != nil {
			return "", fmt.Errorf("command not found: %s (PATH contains %d entries): %w",
				execPath, len(strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))), err)
		}
		execPath = resolved
	}
	info, err := os.Stat(execPath)
	if err != nil {
		return "", fmt.Errorf("command not accessible: %s: %w", execPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("command is a directory: %s", execPath)
	}
	return execPath, nil
}

// buildExecCmd creates an *exec.Cmd with the standard configuration used
// by all execution strategies: resolved path, args, working directory,
// merged environment, and hidden console window (Windows).
func buildExecCmd(execPath string, args []string, cwd string, env map[string]string) *exec.Cmd {
	c := exec.Command(execPath, args...)
	c.Dir = cwd
	c.Env = buildSDKEnvList(env)
	hideCommandWindow(c)
	return c
}

// --- Pipe Creation ---

// ProcessPipes holds the stdin/stdout/stderr pipes for a child process.
type ProcessPipes struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
}

// createProcessPipes creates stdin, stdout, and stderr pipes on the given
// exec.Cmd. This eliminates the repetitive pipe-creation boilerplate in
// every execution strategy.
func createProcessPipes(c *exec.Cmd) (ProcessPipes, error) {
	stdin, err := c.StdinPipe()
	if err != nil {
		return ProcessPipes{}, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return ProcessPipes{}, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return ProcessPipes{}, fmt.Errorf("stderr pipe: %w", err)
	}
	return ProcessPipes{Stdin: stdin, Stdout: stdout, Stderr: stderr}, nil
}

// --- Reader Goroutine Coordination ---

// ReaderCoordinator manages a set of reader goroutines that feed into
// a shared output channel. When all readers finish, the output channel
// is closed automatically.
type ReaderCoordinator struct {
	wg sync.WaitGroup
	ch chan []byte
}

// NewReaderCoordinator creates a coordinator with the given channel buffer size.
func NewReaderCoordinator(bufSize int) *ReaderCoordinator {
	return &ReaderCoordinator{
		ch: make(chan []byte, bufSize),
	}
}

// Add registers n reader goroutines. Call before starting goroutines.
func (rc *ReaderCoordinator) Add(n int) {
	rc.wg.Add(n)
}

// Done marks one reader goroutine as finished.
func (rc *ReaderCoordinator) Done() {
	rc.wg.Done()
}

// Output returns the shared output channel.
func (rc *ReaderCoordinator) Output() chan []byte {
	return rc.ch
}

// CloseWhenDone starts a goroutine that waits for all readers to finish,
// then closes the output channel.
func (rc *ReaderCoordinator) CloseWhenDone() {
	go func() {
		rc.wg.Wait()
		close(rc.ch)
	}()
}

// Wait blocks until all registered reader goroutines have called Done.
// This is useful for ensuring all output has been captured before
// processing the exit status of a process.
func (rc *ReaderCoordinator) Wait() {
	rc.wg.Wait()
}



// --- Output Result Application ---

// applyOutputResult applies an OutputResult to a RemoteSession's state.
// Must be called with s.mu held.
func applyOutputResult(s *RemoteSession, result OutputResult) {
	s.UpdatedAt = time.Now()

	if result.Summary != nil {
		s.Summary = *result.Summary
		s.Status = SessionStatus(result.Summary.Status)
	}

	if result.PreviewDelta != nil {
		s.Preview.SessionID = s.ID
		s.Preview.OutputSeq = result.PreviewDelta.OutputSeq
		s.Preview.UpdatedAt = result.PreviewDelta.UpdatedAt
		s.Preview.PreviewLines = append(s.Preview.PreviewLines, result.PreviewDelta.AppendLines...)
		if len(s.Preview.PreviewLines) > 500 {
			s.Preview.PreviewLines = s.Preview.PreviewLines[len(s.Preview.PreviewLines)-500:]
		}
	}

	for _, evt := range result.Events {
		s.Events = appendRecentEvents(s.Events, evt, maxRecentImportantEvents)
	}
}

// syncOutputResult sends an OutputResult to the Hub client.
// Must be called without s.mu held.
func syncOutputResult(hubClient *RemoteHubClient, result OutputResult) {
	if hubClient == nil {
		return
	}
	if result.Summary != nil {
		_ = hubClient.SendSessionSummary(*result.Summary)
	}
	if result.PreviewDelta != nil {
		_ = hubClient.SendPreviewDelta(*result.PreviewDelta)
	}
	for _, evt := range result.Events {
		_ = hubClient.SendImportantEvent(evt)
	}
}

// appendRawOutputLines appends lines to the session's raw output buffer,
// trimming to the max limit. Must be called with s.mu held.
func appendRawOutputLines(s *RemoteSession, lines []string) {
	s.RawOutputLines = append(s.RawOutputLines, lines...)
	if len(s.RawOutputLines) > 2000 {
		s.RawOutputLines = s.RawOutputLines[len(s.RawOutputLines)-2000:]
	}
}
