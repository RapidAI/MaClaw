package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	execPath, err := resolveExecutablePath(cmd.Command)
	if err != nil {
		return nil, fmt.Errorf("sdk: %w", err)
	}

	args := append([]string{}, cmd.Args...)
	c := buildExecCmd(execPath, args, cmd.Cwd, cmd.Env)

	pipes, err := createProcessPipes(c)
	if err != nil {
		return nil, fmt.Errorf("sdk: %w", err)
	}

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("sdk: start: %w", err)
	}

	rc := NewReaderCoordinator(128)
	handle := &SDKExecutionHandle{
		cmd:       c,
		stdin:     pipes.Stdin,
		stdout:    pipes.Stdout,
		stderr:    pipes.Stderr,
		pid:       c.Process.Pid,
		outputCh:  rc.Output(),
		exitCh:    make(chan PTYExit, 1),
		msgCh:     make(chan SDKMessage, 64),
		ctrlReqCh: make(chan SDKControlRequest, 16),
		readerRC:  rc,
	}

	rc.Add(2)
	go handle.readStdout()
	go handle.readStderr()
	rc.CloseWhenDone()
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

	// readerRC coordinates stdout/stderr reader goroutines so outputCh is
	// closed only after both finish, preventing send-on-closed-channel panics.
	readerRC *ReaderCoordinator

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

// WriteUserInput sends a pre-constructed SDKUserInput message to Claude Code
// via stdin. This is used for multi-part messages (e.g. image input) where
// the caller needs full control over the message content structure.
func (h *SDKExecutionHandle) WriteUserInput(msg SDKUserInput) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("sdk session closed")
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
func (h *SDKExecutionHandle) RespondToControlRequest(requestID string, approved bool, originalInput interface{}) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("sdk session closed")
	}

	if approved {
		// Claude Code SDK requires updatedInput when allowing a tool request.
		updatedInput, _ := originalInput.(map[string]interface{})
		resp := SDKControlResponse{
			Type: "control_response",
			Response: SDKControlResponseBody{
				Subtype:   "success",
				RequestID: requestID,
				Response: &SDKPermissionResult{
					Behavior:     "allow",
					UpdatedInput: updatedInput,
				},
			},
		}
		return h.writeJSON(resp)
	}

	resp := SDKControlResponse{
		Type: "control_response",
		Response: SDKControlResponseBody{
			Subtype:   "success",
			RequestID: requestID,
			Response: &SDKPermissionResult{
				Behavior: "deny",
				Message:  "User denied the request",
			},
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
	defer h.readerRC.Done()
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

				// Handle stream_event: extract streaming text and emit immediately
				if msg.Type == "stream_event" && msg.Event != nil {
					text := extractStreamEventText(msg.Event)
					if text != "" {
						h.outputCh <- []byte(text)
					}
					// Don't send stream_events to msgCh — they're too noisy
					continue
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

	if err := scanner.Err(); err != nil {
		h.outputCh <- []byte(fmt.Sprintf("[sdk-read-error] %v\n", err))
	}
}

func (h *SDKExecutionHandle) readStderr() {
	defer h.readerRC.Done()
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

	// Wait for readStdout and readStderr goroutines to finish so that
	// all output (including error messages on stderr) is captured before
	// the exit signal is sent.  Without this, fast-exiting processes
	// (e.g. exit code 1 due to missing config) may lose their error
	// output, making it impossible for the user to diagnose the failure.
	h.readerRC.Wait()

	var codePtr *int
	if h.cmd.ProcessState != nil {
		code := h.cmd.ProcessState.ExitCode()
		codePtr = &code
	}

	// Distinguish between a real execution error (e.g. signal, crash)
	// and a normal non-zero exit code.  Go's exec package returns an
	// *exec.ExitError for any non-zero exit, but that is not necessarily
	// an unexpected failure — the tool may simply have rejected its
	// arguments or encountered a configuration issue.  By clearing err
	// when we have a valid exit code, runExitLoop will set the status to
	// SessionExited (with a "warn" severity for non-zero codes) instead
	// of SessionError, which better reflects the situation and avoids
	// alarming "execution error" messages.
	if err != nil && codePtr != nil {
		err = nil
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
		_ = h.RespondToControlRequest(req.RequestID, true, req.Request.Input)

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
		_ = h.RespondToControlRequest(req.RequestID, true, req.Request.Input)
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
			return "" // init message is handled by runSDKOutputLoop status update
		}
		return ""

	case "assistant":
		// With --include-partial-messages, text is already streamed via
		// stream_event messages. Only emit tool_use summaries from the
		// complete assistant message to avoid duplicating text output.
		if msg.Message == nil {
			return ""
		}
		var parts []string
		for _, block := range msg.Message.Content {
			switch block.Type {
			case "text":
				// Skip — already streamed incrementally via stream_event
			case "tool_use":
				summary := block.Name
				if input, ok := block.Input.(map[string]interface{}); ok {
					// Show key details for common tools
					if file, ok := input["file_path"].(string); ok {
						summary += " " + file
					} else if cmd, ok := input["command"].(string); ok {
						if len(cmd) > 80 {
							cmd = cmd[:80] + "..."
						}
						summary += " " + cmd
					}
				}
				parts = append(parts, fmt.Sprintf("⚡ %s", summary))
			case "image":
				if block.Source != nil && block.Source.MediaType != "" {
					parts = append(parts, fmt.Sprintf("🖼 Image (%s)", block.Source.MediaType))
				} else {
					parts = append(parts, "🖼 Image")
				}
			}
		}
		return strings.Join(parts, "\n")

	case "user":
		if msg.Message == nil {
			return ""
		}
		for _, block := range msg.Message.Content {
			if block.Type == "tool_result" {
				if block.IsError {
					result := block.Content
					if len(result) > 150 {
						result = result[:150] + "..."
					}
					return fmt.Sprintf("✗ %s", result)
				}
				// Suppress successful tool results — they're verbose
				return ""
			}
		}
		return ""

	case "result":
		return "" // result status is shown via session status badge

	default:
		return ""
	}
}

// extractStreamEventText extracts displayable text from a raw Claude API
// streaming event (delivered via stream_event messages when
// --include-partial-messages is enabled).
//
// Supported event types:
//   - content_block_delta with delta.type == "text_delta" → streaming text
//   - content_block_start with content_block.type == "tool_use" → tool start indicator
func extractStreamEventText(event map[string]interface{}) string {
	eventType, _ := event["type"].(string)

	switch eventType {
	case "content_block_delta":
		delta, ok := event["delta"].(map[string]interface{})
		if !ok {
			return ""
		}
		deltaType, _ := delta["type"].(string)
		if deltaType == "text_delta" {
			text, _ := delta["text"].(string)
			return text // raw text chunk — no newline, accumulates naturally
		}
		// input_json_delta for tool inputs — skip (too noisy)
		return ""

	case "content_block_start":
		block, ok := event["content_block"].(map[string]interface{})
		if !ok {
			return ""
		}
		blockType, _ := block["type"].(string)
		if blockType == "tool_use" {
			name, _ := block["name"].(string)
			if name != "" {
				return fmt.Sprintf("\n⚡ %s", name)
			}
		}
		if blockType == "image" {
			source, _ := block["source"].(map[string]interface{})
			if source != nil {
				mediaType, _ := source["media_type"].(string)
				if mediaType != "" {
					return fmt.Sprintf("\n🖼 Image (%s)", mediaType)
				}
			}
			return "\n🖼 Image"
		}
		return ""

	default:
		return ""
	}
}

func buildSDKEnvList(env map[string]string) []string {
	base := os.Environ()
	merged := make(map[string]string, len(base)+len(env))
	for _, item := range base {
		if k, v, ok := strings.Cut(item, "="); ok {
			merged[k] = v
		}
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
