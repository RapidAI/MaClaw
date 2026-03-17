package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CodexSDKExecutionStrategy launches Codex in non-interactive SDK mode
// using `codex exec --json`.  Communication happens via structured JSONL
// on stdout.  User prompts are passed as trailing arguments to `codex exec`.
//
// Unlike Claude's bidirectional stream-json protocol, Codex exec is
// unidirectional: the initial prompt is given on the command line, and
// follow-up messages use `codex exec resume --last`.
type CodexSDKExecutionStrategy struct{}

func NewCodexSDKExecutionStrategy() *CodexSDKExecutionStrategy {
	return &CodexSDKExecutionStrategy{}
}

func (s *CodexSDKExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	execPath, err := resolveExecutablePath(cmd.Command)
	if err != nil {
		return nil, fmt.Errorf("codex-sdk: %w", err)
	}

	args := append([]string{}, cmd.Args...)
	c := buildExecCmd(execPath, args, cmd.Cwd, cmd.Env)

	pipes, err := createProcessPipes(c)
	if err != nil {
		return nil, fmt.Errorf("codex-sdk: %w", err)
	}

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("codex-sdk: start failed: cmd=%s args=%v cwd=%s: %w",
			execPath, args, cmd.Cwd, err)
	}

	rc := NewReaderCoordinator(128)
	handle := &CodexSDKExecutionHandle{
		cmd:       c,
		stdin:     pipes.Stdin,
		stdout:    pipes.Stdout,
		stderr:    pipes.Stderr,
		pid:       c.Process.Pid,
		startedAt: time.Now(),
		outputCh:  rc.Output(),
		exitCh:    make(chan PTYExit, 1),
		readerRC:  rc,
	}

	// Emit launch diagnostics into the output channel so they appear in the
	// session preview — this is critical for debugging headless remote launches.
	launchInfo := fmt.Sprintf("[codex-launch] pid=%d cmd=%s args=%v cwd=%s",
		handle.pid, execPath, args, cmd.Cwd)
	handle.outputCh <- []byte(launchInfo + "\n")

	// Log key environment variables (redact API keys).
	envDiag := codexEnvDiagnostics(cmd.Env)
	if envDiag != "" {
		handle.outputCh <- []byte("[codex-env] " + envDiag + "\n")
	}

	rc.Add(2)
	go handle.readStdout()
	go handle.readStderr()
	rc.CloseWhenDone()
	go handle.waitProcess()

	return handle, nil
}

// codexEnvDiagnostics returns a summary of key environment variables for
// debugging, with API keys redacted.
func codexEnvDiagnostics(env map[string]string) string {
	keys := []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL", "WIRE_API",
		"HTTP_PROXY", "HTTPS_PROXY"}
	var parts []string
	for _, k := range keys {
		v := env[k]
		if v == "" {
			continue
		}
		if strings.Contains(strings.ToLower(k), "key") || strings.Contains(strings.ToLower(k), "token") {
			if len(v) > 8 {
				v = v[:4] + "..." + v[len(v)-4:]
			} else {
				v = "***"
			}
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}

// CodexSDKExecutionHandle wraps a Codex process running in exec --json mode.
type CodexSDKExecutionHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	pid    int

	startedAt time.Time
	outputCh  chan []byte
	exitCh    chan PTYExit

	readerRC *ReaderCoordinator

	mu     sync.Mutex
	closed bool

	// threadID is the Codex thread/session ID reported in thread.started events.
	threadID string

	// stderrLines accumulates stderr output for diagnostics on early exit.
	stderrLines []string
}

func (h *CodexSDKExecutionHandle) PID() int {
	return h.pid
}

// Write sends a follow-up message to Codex.  Since `codex exec` is a
// one-shot command, follow-up messages spawn a new `codex exec resume --last`
// process.  For the initial prompt, the message is written to stdin which
// Codex reads as the task prompt.
func (h *CodexSDKExecutionHandle) Write(data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("codex session closed")
	}

	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	// Write the prompt text to stdin followed by a newline.
	// For `codex exec`, stdin is read as the task prompt when no
	// trailing argument is provided, or can be used for piped input.
	_, err := h.stdin.Write(append([]byte(text), '\n'))
	return err
}

func (h *CodexSDKExecutionHandle) Interrupt() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *CodexSDKExecutionHandle) Kill() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *CodexSDKExecutionHandle) Output() <-chan []byte {
	return h.outputCh
}

func (h *CodexSDKExecutionHandle) Exit() <-chan PTYExit {
	return h.exitCh
}

func (h *CodexSDKExecutionHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	_ = h.stdin.Close()
	return nil
}

// ThreadID returns the Codex thread ID if reported.
func (h *CodexSDKExecutionHandle) ThreadID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.threadID
}

func (h *CodexSDKExecutionHandle) readStdout() {
	defer h.readerRC.Done()

	scanner := bufio.NewScanner(h.stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Try to parse as Codex JSONL event
		var event CodexEvent
		if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
			// Not JSON — emit as raw output
			h.outputCh <- []byte(trimmed + "\n")
			continue
		}

		// Convert Codex event to human-readable text for the output pipeline
		text := codexEventToText(event)
		if text != "" {
			h.outputCh <- []byte(text + "\n")
		}

		// Track thread ID
		if event.Type == "thread.started" && event.ThreadID != "" {
			h.mu.Lock()
			h.threadID = event.ThreadID
			h.mu.Unlock()
		}
	}

	if err := scanner.Err(); err != nil {
		h.outputCh <- []byte(fmt.Sprintf("[codex-read-error] %v\n", err))
	}
}

func (h *CodexSDKExecutionHandle) readStderr() {
	defer h.readerRC.Done()
	scanner := bufio.NewScanner(h.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			h.outputCh <- []byte("[stderr] " + line + "\n")
			h.mu.Lock()
			if len(h.stderrLines) < 50 {
				h.stderrLines = append(h.stderrLines, line)
			}
			h.mu.Unlock()
		}
	}
}

func (h *CodexSDKExecutionHandle) waitProcess() {
	defer close(h.exitCh)

	err := h.cmd.Wait()
	elapsed := time.Since(h.startedAt)
	var codePtr *int
	if h.cmd.ProcessState != nil {
		code := h.cmd.ProcessState.ExitCode()
		codePtr = &code
	}

	// If the process exited very quickly (< 5s), it likely failed to start
	// properly.  Emit a diagnostic summary so the user can see what happened.
	if elapsed < 5*time.Second {
		exitCode := -1
		if codePtr != nil {
			exitCode = *codePtr
		}
		diag := fmt.Sprintf("[codex-exit] process exited in %v with code %d", elapsed.Round(time.Millisecond), exitCode)
		h.outputCh <- []byte(diag + "\n")

		h.mu.Lock()
		stderrCopy := append([]string{}, h.stderrLines...)
		h.mu.Unlock()
		if len(stderrCopy) > 0 {
			h.outputCh <- []byte("[codex-exit] stderr summary:\n")
			for _, line := range stderrCopy {
				h.outputCh <- []byte("  " + line + "\n")
			}
		}
		if err != nil {
			h.outputCh <- []byte(fmt.Sprintf("[codex-exit] error: %v\n", err))
		}
	}

	h.exitCh <- PTYExit{
		Code: codePtr,
		Err:  err,
	}

	_ = h.Close()
}
