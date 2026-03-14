package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SDKExecutionStrategy launches Claude Code in SDK mode using
// --output-format stream-json --input-format stream-json.
// Communication happens via structured JSON on stdin/stdout instead of
// raw PTY byte streams.
type SDKExecutionStrategy struct{}

func NewSDKExecutionStrategy() *SDKExecutionStrategy {
	return &SDKExecutionStrategy{}
}

func (s *SDKExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	execPath := cmd.Command
	if !filepath.IsAbs(execPath) {
		resolved, err := exec.LookPath(execPath)
		if err != nil {
			return nil, fmt.Errorf("sdk: command not found: %s: %w", execPath, err)
		}
		execPath = resolved
	}
	if info, err := os.Stat(execPath); err != nil {
		return nil, fmt.Errorf("sdk: command not accessible: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("sdk: command is a directory: %s", execPath)
	}

	args := append([]string{}, cmd.Args...)
	c := exec.Command(execPath, args...)
	c.Dir = cmd.Cwd
	c.Env = buildSDKEnvList(cmd.Env)

	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("sdk: stdin pipe: %w", err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("sdk: stdout pipe: %w", err)
	}
	// Capture stderr for debugging but don't block on it
	stderr, err := c.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("sdk: stderr pipe: %w", err)
	}

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("sdk: start: %w", err)
	}

	handle := &SDKExecutionHandle{
		cmd:       c,
		stdin:     stdin,
		stdout:    stdout,
		stderr:    stderr,
		pid:       c.Process.Pid,
		outputCh:  make(chan []byte, 128),
		exitCh:    make(chan PTYExit, 1),
		msgCh:     make(chan SDKMessage, 64),
		ctrlReqCh: make(chan SDKControlRequest, 16),
	}

	handle.readerWg.Add(2)
	go handle.readStdout()
	go handle.readStderr()
	go func() {
		handle.readerWg.Wait()
		close(handle.outputCh)
	}()
	go handle.waitProcess()

	return handle, nil
}

// SDKExecutionHandle wraps a Claude Code process running in SDK mode.
type SDKExecutionHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	pid    int

	// outputCh emits synthetic "lines" for the output pipeline.
	// In SDK mode these are formatted text representations of SDK messages,
	// not raw terminal bytes.
	outputCh chan []byte
	exitCh   chan PTYExit

	// msgCh receives parsed SDK messages for structured processing.
	msgCh chan SDKMessage

	// ctrlReqCh receives permission requests from Claude Code.
	ctrlReqCh chan SDKControlRequest

	// readerWg tracks stdout/stderr reader goroutines so outputCh is
	// closed only after both finish, preventing send-on-closed-channel panics.
	readerWg sync.WaitGroup

	mu     sync.Mutex
	closed bool

	// autoApprove controls whether tool use requests are auto-approved.
	autoApprove atomic.Bool

	// claudeSessionID is the session ID reported by Claude Code.
	claudeSessionID atomic.Value
}

func (h *SDKExecutionHandle) PID() int {
	return h.pid
}

// Write sends a user message to Claude Code via stdin in stream-json format.
func (h *SDKExecutionHandle) Write(data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("sdk session closed")
	}

	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	msg := SDKUserInput{
		Type: "user",
		Message: SDKUserMessage{
			Role:    "user",
			Content: text,
		},
	}
	return h.writeJSON(msg)
}

// Interrupt sends an interrupt control request to Claude Code.
func (h *SDKExecutionHandle) Interrupt() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("sdk session closed")
	}

	req := SDKInterruptRequest{
		Type:      "control_request",
		RequestID: fmt.Sprintf("int_%d", time.Now().UnixNano()),
		Request:   SDKInterruptBody{Subtype: "interrupt"},
	}
	return h.writeJSON(req)
}

func (h *SDKExecutionHandle) Kill() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *SDKExecutionHandle) Output() <-chan []byte {
	return h.outputCh
}

func (h *SDKExecutionHandle) Exit() <-chan PTYExit {
	return h.exitCh
}

// Messages returns the channel of parsed SDK messages for structured processing.
func (h *SDKExecutionHandle) Messages() <-chan SDKMessage {
	return h.msgCh
}

// ControlRequests returns the channel of permission requests from Claude.
func (h *SDKExecutionHandle) ControlRequests() <-chan SDKControlRequest {
	return h.ctrlReqCh
}

// RespondToControlRequest sends a permission response back to Claude Code.
func (h *SDKExecutionHandle) RespondToControlRequest(requestID string, approved bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("sdk session closed")
	}

	behavior := "deny"
	if approved {
		behavior = "allow"
	}

	resp := SDKControlResponse{
		Type: "control_response",
		Response: SDKControlResponseBody{
			Subtype:   "success",
			RequestID: requestID,
			Behavior:  behavior,
		},
	}
	return h.writeJSON(resp)
}

// SetAutoApprove enables or disables automatic approval of tool use requests.
func (h *SDKExecutionHandle) SetAutoApprove(enabled bool) {
	h.autoApprove.Store(enabled)
}

// ClaudeSessionID returns the Claude Code session ID if reported.
func (h *SDKExecutionHandle) ClaudeSessionID() string {
	v := h.claudeSessionID.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

func (h *SDKExecutionHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	_ = h.stdin.Close()
	return nil
}

func (h *SDKExecutionHandle) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sdk: marshal: %w", err)
	}
	data = append(data, '\n')
	_, err = h.stdin.Write(data)
	return err
}

func (h *SDKExecutionHandle) readStdout() {
	defer h.readerWg.Done()
	defer close(h.msgCh)

	scanner := bufio.NewScanner(h.stdout)
	// Allow large lines (Claude can produce big JSON)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Try to parse as JSON
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			// Not JSON — emit as raw output line
			h.outputCh <- []byte(trimmed + "\n")
			continue
		}

		msgType, _ := raw["type"].(string)

		switch msgType {
		case "control_request":
			h.handleControlRequest([]byte(trimmed))
		case "control_cancel_request":
			h.handleControlCancel([]byte(trimmed))
		default:
			// Parse as SDK message
			var msg SDKMessage
			if err := json.Unmarshal([]byte(trimmed), &msg); err == nil {
				// Check for session init
				if msg.Type == "system" && msg.Subtype == "init" && msg.SessionID != "" {
					h.claudeSessionID.Store(msg.SessionID)
				}

				// Send to structured channel (non-blocking)
				select {
				case h.msgCh <- msg:
				default:
				}

				// Also convert to human-readable text for the output pipeline
				text := sdkMessageToText(msg)
				if text != "" {
					h.outputCh <- []byte(text + "\n")
				}
			}
		}
	}
}

func (h *SDKExecutionHandle) readStderr() {
	defer h.readerWg.Done()
	scanner := bufio.NewScanner(h.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			h.outputCh <- []byte("[stderr] " + line + "\n")
		}
	}
}

func (h *SDKExecutionHandle) waitProcess() {
	defer close(h.exitCh)

	err := h.cmd.Wait()
	var codePtr *int
	if h.cmd.ProcessState != nil {
		code := h.cmd.ProcessState.ExitCode()
		codePtr = &code
	}

	h.exitCh <- PTYExit{
		Code: codePtr,
		Err:  err,
	}

	_ = h.Close()
}

func (h *SDKExecutionHandle) handleControlRequest(data []byte) {
	var req SDKControlRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}

	// Auto-approve if enabled (yolo mode)
	if h.autoApprove.Load() {
		_ = h.RespondToControlRequest(req.RequestID, true)

		// Emit a synthetic output line
		toolName := req.Request.ToolName
		h.outputCh <- []byte(fmt.Sprintf("[auto-approved] Tool: %s\n", toolName))
		return
	}

	// Send to control request channel for external handling
	select {
	case h.ctrlReqCh <- req:
	default:
		// Channel full — auto-approve to avoid blocking Claude
		_ = h.RespondToControlRequest(req.RequestID, true)
		h.outputCh <- []byte(fmt.Sprintf("[auto-approved-overflow] Tool: %s\n", req.Request.ToolName))
	}
}

func (h *SDKExecutionHandle) handleControlCancel(data []byte) {
	var cancel SDKControlCancelRequest
	if err := json.Unmarshal(data, &cancel); err != nil {
		return
	}
	// Cancel request acknowledged — currently no pending tracking needed
	// since we auto-approve or forward to the control request channel.
}

// sdkMessageToText converts an SDK message to human-readable text for
// the output pipeline and preview display.
func sdkMessageToText(msg SDKMessage) string {
	switch msg.Type {
	case "system":
		if msg.Subtype == "init" {
			return fmt.Sprintf("Session initialized (id: %s)", msg.SessionID)
		}
		return ""

	case "assistant":
		if msg.Message == nil {
			return ""
		}
		var parts []string
		for _, block := range msg.Message.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					parts = append(parts, block.Text)
				}
			case "tool_use":
				inputStr := ""
				if block.Input != nil {
					if b, err := json.Marshal(block.Input); err == nil {
						inputStr = string(b)
						if len(inputStr) > 200 {
							inputStr = inputStr[:200] + "..."
						}
					}
				}
				parts = append(parts, fmt.Sprintf("[tool_use] %s: %s", block.Name, inputStr))
			}
		}
		return strings.Join(parts, "\n")

	case "user":
		if msg.Message == nil {
			return ""
		}
		for _, block := range msg.Message.Content {
			if block.Type == "tool_result" {
				status := "ok"
				if block.IsError {
					status = "error"
				}
				result := block.Content
				if len(result) > 200 {
					result = result[:200] + "..."
				}
				return fmt.Sprintf("[tool_result] %s (%s): %s", block.ToolUseID, status, result)
			}
		}
		return ""

	case "result":
		if msg.Result != nil {
			return fmt.Sprintf("[result] Completed in %.1fs, %d turns", msg.Result.Duration/1000, msg.Result.NumTurns)
		}
		return "[result] Completed"

	default:
		return ""
	}
}

func buildSDKEnvList(env map[string]string) []string {
	merged := map[string]string{}
	for _, item := range os.Environ() {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		merged[parts[0]] = parts[1]
	}
	for key, value := range env {
		merged[key] = value
	}

	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]string, 0, len(keys))
	for _, key := range keys {
		items = append(items, key+"="+merged[key])
	}
	return items
}
