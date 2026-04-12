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

	"github.com/RapidAI/CodeClaw/corelib"
)

// sdkDiagLog writes diagnostic messages to ~/.maclaw/logs/sdk_diag.log.
// Gated by corelib.IsLogDetailEnabled() — only writes when the user
// enables "日志详情" in settings.
var sdkDiagLog = func() *os.File {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".maclaw", "logs")
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "sdk_diag.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return f
}()

func sdkDiag(format string, args ...interface{}) {
	if !corelib.IsLogDetailEnabled() {
		return
	}
	msg := fmt.Sprintf(time.Now().Format("15:04:05.000")+" "+format+"\n", args...)
	if sdkDiagLog != nil {
		_, _ = sdkDiagLog.WriteString(msg)
	}
}

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

	sdkDiag("process started: pid=%d, cmd=%s, args=%v, cwd=%s",
		c.Process.Pid, cmd.Command, cmd.Args, cmd.Cwd)
	// Log key env vars for debugging custom provider issues
	for _, key := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL"} {
		if v, ok := cmd.Env[key]; ok {
			masked := v
			if len(masked) > 8 && strings.Contains(strings.ToLower(key), "token") {
				masked = masked[:4] + "..." + masked[len(masked)-4:]
			}
			sdkDiag("  env %s=%s", key, masked)
		} else {
			sdkDiag("  env %s=(not set)", key)
		}
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
		msgCh:     make(chan SDKMessage, 256),
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

	// hasStreamEvents tracks whether stream_event messages have been
	// received. When true, tool_use blocks in assistant messages are
	// skipped by sdkMessageToText to avoid duplication (they were
	// already emitted by extractStreamEventText). When false (e.g.
	// CodeBuddy/Cursor without --include-partial-messages), tool_use
	// blocks are still rendered by sdkMessageToText.
	hasStreamEvents atomic.Bool
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
		SessionID:       "default",
		ParentToolUseID: nil,
	}
	sdkDiag("Write() pid=%d, text_len=%d, text=%.100s", h.pid, len(text), text)
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

func (h *SDKExecutionHandle) WriteAskUserQuestionAnswer(pending *PendingToolUse, text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("sdk session closed")
	}
	if pending == nil || strings.TrimSpace(pending.ToolUseID) == "" {
		return fmt.Errorf("sdk: missing pending AskUserQuestion tool_use_id")
	}
	resultMsg := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type":        "tool_result",
					"tool_use_id": pending.ToolUseID,
					"content":     buildAskUserQuestionAnswerContent(pending, text),
				},
			},
		},
		"session_id":         "default",
		"parent_tool_use_id": nil,
	}
	return h.writeJSON(resultMsg)
}

// Interrupt sends an interrupt control request to Claude Code.
// It first attempts a graceful interrupt via stdin JSON.  If the mutex
// cannot be acquired within 2 seconds (indicating a blocked writeJSON),
// it falls back to killing the process directly.
func (h *SDKExecutionHandle) Interrupt() error {
	// Try to acquire the mutex with a short timeout.  If writeJSON is
	// blocked on a stuck stdin pipe, we must not wait forever.
	acquired := make(chan struct{}, 1)
	go func() {
		h.mu.Lock()
		select {
		case acquired <- struct{}{}:
			// Caller will unlock via defer.
		default:
			// Timeout already fired — caller won't read from acquired,
			// so we must release the mutex ourselves to avoid a leak.
			h.mu.Unlock()
		}
	}()

	select {
	case <-acquired:
		// Got the lock — try graceful interrupt via stdin.
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

	case <-time.After(2 * time.Second):
		// Signal the goroutine that we're not waiting anymore.
		// If it later acquires the mutex, the default branch above
		// will release it.

		// Mutex blocked — stdin pipe is likely stuck.  Fall back to
		// process kill so the user isn't left with a frozen session.
		if h.cmd != nil && h.cmd.Process != nil {
			return h.cmd.Process.Kill()
		}
		return fmt.Errorf("sdk: interrupt timed out and process not available for kill")
	}
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
	// Try to acquire the mutex with a timeout.  If writeJSON is blocked
	// on a stuck stdin pipe, we still need to close stdin to unblock it.
	acquired := make(chan struct{}, 1)
	go func() {
		h.mu.Lock()
		select {
		case acquired <- struct{}{}:
			// Caller will unlock via defer.
		default:
			// Timeout already fired — release the mutex ourselves.
			h.mu.Unlock()
		}
	}()

	select {
	case <-acquired:
		defer h.mu.Unlock()
		if h.closed {
			return nil
		}
		h.closed = true
		_ = h.stdin.Close()
		return nil

	case <-time.After(2 * time.Second):
		// Mutex blocked — force-close stdin without the lock.
		// This is safe because stdin.Close() will unblock any pending
		// Write() call in the stuck goroutine, which will then release
		// the mutex.
		_ = h.stdin.Close()
		return nil
	}
}

// sdkStdinWriteTimeout is the maximum time to wait for a stdin write to
// complete.  If the Claude Code process is not reading stdin (e.g. stuck
// in a tool execution or crashed), the write will block indefinitely
// without this timeout, holding the mutex and freezing the entire session.
const sdkStdinWriteTimeout = 10 * time.Second

func (h *SDKExecutionHandle) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sdk: marshal: %w", err)
	}
	data = append(data, '\n')

	sdkDiag("writeJSON pid=%d, len=%d, data=%.200s", h.pid, len(data), string(data))

	// Use a goroutine + timer to avoid blocking indefinitely when the
	// child process is not reading stdin.  Without this, a stuck process
	// causes the mutex to be held forever, making the session completely
	// unresponsive (no input, no interrupt, no kill via SDK path).
	type writeResult struct {
		n   int
		err error
	}
	ch := make(chan writeResult, 1)
	go func() {
		n, err := h.stdin.Write(data)
		ch <- writeResult{n, err}
	}()

	select {
	case res := <-ch:
		sdkDiag("writeJSON completed pid=%d, n=%d, err=%v", h.pid, res.n, res.err)
		return res.err
	case <-time.After(sdkStdinWriteTimeout):
		sdkDiag("writeJSON TIMEOUT pid=%d after %v", h.pid, sdkStdinWriteTimeout)
		return fmt.Errorf("sdk: stdin write timed out after %v (process may be stuck)", sdkStdinWriteTimeout)
	}
}

func (h *SDKExecutionHandle) readStdout() {
	defer h.readerRC.Done()
	defer close(h.msgCh)

	sdkDiag("readStdout started for pid=%d", h.pid)

	scanner := bufio.NewScanner(h.stdout)
	// Allow large lines (Claude can produce big JSON)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineCount := 0
	for scanner.Scan() {
		lineCount++
		if lineCount <= 5 {
			sdkDiag("stdout line #%d (pid=%d): %.500s", lineCount, h.pid, scanner.Text())
		}
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
			// Surface api_retry messages before parsing into SDKMessage
			// (SDKMessage struct doesn't capture the extra fields).
			if msgType == "system" {
				if sub, _ := raw["subtype"].(string); sub == "api_retry" {
					attempt, _ := raw["attempt"].(float64)
					errStr, _ := raw["error"].(string)
					line := fmt.Sprintf("⚠️ API retry (attempt %.0f): %s", attempt, errStr)
					h.outputCh <- []byte(line + "\n")
				}
			}

			// Surface API errors from result messages so the user sees
			// what went wrong instead of a silent busy→waiting transition.
			if msgType == "result" {
				if isErr, _ := raw["is_error"].(bool); isErr {
					if errText, _ := raw["result"].(string); errText != "" {
						if len(errText) > 500 {
							errText = errText[:500] + "..."
						}
						h.outputCh <- []byte(fmt.Sprintf("❌ %s\n", errText))
					}
				}
			}

			// Parse as SDK message
			var msg SDKMessage
			if err := json.Unmarshal([]byte(trimmed), &msg); err == nil {
				// Check for session init
				if msg.Type == "system" && msg.Subtype == "init" && msg.SessionID != "" {
					h.claudeSessionID.Store(msg.SessionID)
				}

				// Handle stream_event: extract streaming text and emit immediately
				if msg.Type == "stream_event" && msg.Event != nil {
					h.hasStreamEvents.Store(true)
					text := extractStreamEventText(msg.Event)
					if text != "" {
						h.outputCh <- []byte(text)
					}
					// Don't send stream_events to msgCh — they're too noisy
					continue
				}

				// Send to structured channel reliably — dropping assistant/user/result
				// messages can leave the session stuck in busy forever.
				h.msgCh <- msg

				// Also convert to human-readable text for the output pipeline
				text := sdkMessageToText(msg, h.hasStreamEvents.Load())
				if text != "" {
					h.outputCh <- []byte(text + "\n")
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		h.outputCh <- []byte(fmt.Sprintf("[sdk-read-error] %v\n", err))
	}
	sdkDiag("readStdout ended for pid=%d, total lines=%d, scanErr=%v", h.pid, lineCount, scanner.Err())
}

func (h *SDKExecutionHandle) readStderr() {
	defer h.readerRC.Done()
	scanner := bufio.NewScanner(h.stderr)
	stderrLineCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		stderrLineCount++
		if stderrLineCount <= 3 {
			sdkDiag("stderr line #%d (pid=%d): %.300s", stderrLineCount, h.pid, line)
		}
		if strings.TrimSpace(line) != "" {
			h.outputCh <- []byte("[stderr] " + line + "\n")
		}
	}
	sdkDiag("readStderr ended for pid=%d, total lines=%d", h.pid, stderrLineCount)
}

func (h *SDKExecutionHandle) waitProcess() {
	defer close(h.exitCh)

	err := h.cmd.Wait()
	sdkDiag("process exited: pid=%d, err=%v, exitCode=%d",
		h.pid, err, h.cmd.ProcessState.ExitCode())

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
		h.outputCh <- []byte(fmt.Sprintf("[auto-approved] Tool: %s", toolName))
		return
	}

	// Send to control request channel for external handling
	select {
	case h.ctrlReqCh <- req:
	default:
		// Channel full — auto-approve to avoid blocking Claude
		_ = h.RespondToControlRequest(req.RequestID, true, req.Request.Input)
		h.outputCh <- []byte(fmt.Sprintf("[auto-approved-overflow] Tool: %s", req.Request.ToolName))
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
func sdkMessageToText(msg SDKMessage, hasStreamEvents bool) string {
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
		//
		// NOTE: When hasStreamEvents is true, tool_use block names were
		// already emitted by extractStreamEventText on content_block_start,
		// so we skip them to avoid duplicate lines like "⏺ Bash ⏺ Bash ls ...".
		// When hasStreamEvents is false (e.g. CodeBuddy/Cursor without
		// --include-partial-messages), we still render tool_use blocks here.
		// Exception: AskUserQuestion always needs its full details rendered.
		if msg.Message == nil {
			return ""
		}
		var parts []string
		for _, block := range msg.Message.Content {
			switch block.Type {
			case "text":
				// Skip — already streamed incrementally via stream_event
			case "tool_use":
				if block.Name == "AskUserQuestion" {
					if details := formatAskUserQuestionBlock(block); details != "" {
						parts = append(parts, details)
						continue
					}
				}
				if hasStreamEvents {
					// Already shown by extractStreamEventText — skip.
					continue
				}
				// No stream_events available — render tool_use here.
				summary := block.Name
				if input, ok := block.Input.(map[string]interface{}); ok {
					if file, ok := input["file_path"].(string); ok {
						summary += " " + file
					} else if cmd, ok := input["command"].(string); ok {
						if len(cmd) > 80 {
							cmd = cmd[:80] + "..."
						}
						summary += " " + cmd
					}
				}
				parts = append(parts, fmt.Sprintf("⏺ %s", summary))
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
					return fmt.Sprintf("⏺ %s", result)
				}
				// Suppress successful tool results — they're verbose
				return ""
			}
		}
		return ""

	case "result":
		// Error results are surfaced directly in readStdout from the raw
		// JSON (SDKResultPayload doesn't capture is_error/result fields).
		return ""

	default:
		return ""
	}
}

func formatAskUserQuestionBlock(block SDKContentBlock) string {
	view := buildAskUserQuestionView(block.ID, block.Name, block.Input)
	if view == nil {
		return "⏺ AskUserQuestion"
	}
	parts := []string{"⏺ AskUserQuestion"}
	if view.Header != "" {
		parts = append(parts, view.Header)
	}
	if view.Question != "" {
		parts = append(parts, view.Question)
	}
	for _, option := range view.Options {
		line := "- " + option.Label
		if option.Description != "" {
			line += ": " + option.Description
		}
		parts = append(parts, line)
	}
	if view.Hint != "" {
		parts = append(parts, "Hint: "+view.Hint)
	}
	return strings.Join(parts, "\n")
}

// extractStreamEventText extracts displayable text from a raw Claude API
// streaming event (delivered via stream_event messages when
// --include-partial-messages is enabled).
//
// Supported event types:
//   - content_block_delta with delta.type == "text_delta" — streaming text
//   - content_block_start with content_block.type == "tool_use" — tool start indicator
func extractStreamEventText(event map[string]interface{}) string {
	eventType, _ := event["type"].(string)

	switch eventType {
	case "message_start":
		// Emit a marker when the LLM starts generating a response.
		// This ensures RawOutputLines grows immediately, preventing
		// the session observer from timing out during the gap between
		// message_start and the first content_block_start (which can
		// be tens of seconds with slow API proxies like GLM).
		return "\n⏳ LLM responding...\n"

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
		if deltaType == "thinking_delta" {
			// Don't emit individual thinking tokens — they would flood
			// RawOutputLines with hundreds of "💭" markers. The single
			// "💭 Thinking..." line from content_block_start is enough
			// to signal the session observer that the LLM is alive.
			return ""
		}
		// input_json_delta for tool inputs — skip (too noisy)
		return ""

	case "content_block_start":
		block, ok := event["content_block"].(map[string]interface{})
		if !ok {
			return ""
		}
		blockType, _ := block["type"].(string)
		if blockType == "thinking" {
			return "\n💭 Thinking...\n"
		}
		if blockType == "tool_use" {
			name, _ := block["name"].(string)
			if name != "" {
				return fmt.Sprintf("\n⏺ %s", name)
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
