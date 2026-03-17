package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GeminiACPExecutionStrategy launches Gemini CLI with --experimental-acp,
// which exposes a JSON-RPC protocol on stdin/stdout for structured
// bidirectional communication.  This is analogous to Claude Code's
// --input-format stream-json but uses the ACP (Agent Communication Protocol).
//
// Protocol flow:
//  1. Client sends "initialize" request → server responds with protocolVersion
//  2. Client sends "session/new" request → server responds with sessionId
//  3. Client sends "session/prompt" request → server streams session/update
//     notifications and responds when the prompt completes
//  4. Repeat step 3 for each user message
type GeminiACPExecutionStrategy struct{}

func NewGeminiACPExecutionStrategy() *GeminiACPExecutionStrategy {
	return &GeminiACPExecutionStrategy{}
}

func (s *GeminiACPExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	execPath, err := resolveExecutablePath(cmd.Command)
	if err != nil {
		return nil, fmt.Errorf("gemini-acp: %w", err)
	}

	args := append([]string{}, cmd.Args...)
	c := buildExecCmd(execPath, args, cmd.Cwd, cmd.Env)

	pipes, err := createProcessPipes(c)
	if err != nil {
		return nil, fmt.Errorf("gemini-acp: %w", err)
	}

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("gemini-acp: start: %w", err)
	}

	rc := NewReaderCoordinator(128)
	handle := &GeminiACPExecutionHandle{
		cmd:      c,
		stdin:    pipes.Stdin,
		stdout:   pipes.Stdout,
		stderr:   pipes.Stderr,
		pid:      c.Process.Pid,
		cwd:      cmd.Cwd,
		outputCh: rc.Output(),
		exitCh:   make(chan PTYExit, 1),
		pending:  map[interface{}]acpPendingRequest{},
		readerRC: rc,
	}

	rc.Add(2)
	go handle.readStdout()
	go handle.readStderr()
	rc.CloseWhenDone()
	go handle.waitProcess()

	// Perform ACP handshake: initialize + session/new
	if err := handle.acpInitialize(); err != nil {
		_ = c.Process.Kill()
		return nil, fmt.Errorf("gemini-acp: initialize failed: %w", err)
	}
	if err := handle.acpNewSession(); err != nil {
		_ = c.Process.Kill()
		return nil, fmt.Errorf("gemini-acp: session/new failed: %w", err)
	}

	handle.outputCh <- []byte(fmt.Sprintf("[gemini-acp] session=%s pid=%d\n", handle.SessionID(), handle.pid))

	return handle, nil
}

// acpPendingRequest tracks a pending JSON-RPC request awaiting a response.
type acpPendingRequest struct {
	ch chan acpResponse
}

type acpResponse struct {
	Result json.RawMessage
	Error  *acpRPCError
}

type acpRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"` // validation details from Gemini CLI
}

// GeminiACPExecutionHandle wraps a Gemini CLI process running in ACP mode.
type GeminiACPExecutionHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	pid    int
	cwd    string

	outputCh chan []byte
	exitCh   chan PTYExit
	readerRC *ReaderCoordinator

	mu        sync.Mutex
	closed    bool
	nextID    int
	pending   map[interface{}]acpPendingRequest
	sessionID string

	writeMu sync.Mutex // serializes writes to stdin

	// Permissions is set by the session manager to delegate permission
	// decisions to the session-level PermissionHandler.
	Permissions *PermissionHandler

	// promptActive tracks whether a prompt is in progress.
	promptActive atomic.Bool
}

func (h *GeminiACPExecutionHandle) PID() int { return h.pid }

func (h *GeminiACPExecutionHandle) SessionID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionID
}

// Write sends a user message to Gemini via the ACP session/prompt request.
// The prompt runs asynchronously — streaming output arrives via session/update
// notifications on the outputCh.
func (h *GeminiACPExecutionHandle) Write(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	h.mu.Lock()
	sid := h.sessionID
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("gemini-acp session closed")
	}
	h.mu.Unlock()

	if sid == "" {
		return fmt.Errorf("gemini-acp: no active session")
	}

	// Echo user input
	h.outputCh <- []byte(fmt.Sprintf("\n❯ %s\n", text))

	// Send prompt asynchronously so Write doesn't block until completion.
	go h.sendPrompt(sid, text)
	return nil
}

func (h *GeminiACPExecutionHandle) sendPrompt(sessionID, text string) {
	h.promptActive.Store(true)
	defer h.promptActive.Store(false)

	params := map[string]interface{}{
		"sessionId": sessionID,
		"prompt": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}

	resp, err := h.sendRequest("session/prompt", params, 0) // no timeout — prompts can be long
	if err != nil {
		h.outputCh <- []byte(fmt.Sprintf("[gemini-acp] prompt error: %v\n", err))
		return
	}

	// Parse stop reason from response
	var result map[string]interface{}
	if json.Unmarshal(resp, &result) == nil {
		if reason, ok := result["stopReason"].(string); ok && reason != "" {
			h.outputCh <- []byte(fmt.Sprintf("[gemini-acp] turn complete: %s\n", reason))
		}
	}
}

func (h *GeminiACPExecutionHandle) Interrupt() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("gemini-acp session closed")
	}
	sid := h.sessionID
	h.mu.Unlock()

	if sid != "" {
		// Send cancel outside the lock to avoid deadlock if stdin pipe
		// buffer is full while readStdout needs h.mu to dispatch.
		h.sendNotification("session/cancel", map[string]interface{}{
			"sessionId": sid,
		})
	}
	return nil
}

func (h *GeminiACPExecutionHandle) Kill() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *GeminiACPExecutionHandle) Output() <-chan []byte { return h.outputCh }
func (h *GeminiACPExecutionHandle) Exit() <-chan PTYExit  { return h.exitCh }

func (h *GeminiACPExecutionHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	_ = h.stdin.Close()
	return nil
}

// --- JSON-RPC transport layer ---

type acpJSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type acpJSONRPCNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type acpJSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *acpRPCError    `json:"error,omitempty"`
	// For incoming requests from the server
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

func (h *GeminiACPExecutionHandle) writeJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("gemini-acp: marshal: %w", err)
	}
	data = append(data, '\n')
	h.writeMu.Lock()
	_, err = h.stdin.Write(data)
	h.writeMu.Unlock()
	return err
}

func (h *GeminiACPExecutionHandle) sendRequest(method string, params interface{}, timeout time.Duration) (json.RawMessage, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, fmt.Errorf("gemini-acp session closed")
	}
	h.nextID++
	id := h.nextID
	ch := make(chan acpResponse, 1)
	h.pending[id] = acpPendingRequest{ch: ch}
	h.mu.Unlock()

	req := acpJSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	if err := h.writeJSON(req); err != nil {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return nil, err
	}

	if timeout <= 0 {
		// No timeout — wait indefinitely
		resp, ok := <-ch
		if !ok {
			return nil, fmt.Errorf("gemini-acp: channel closed waiting for %s", method)
		}
		if resp.Error != nil {
			return nil, fmtACPError(method, resp.Error)
		}
		return resp.Result, nil
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("gemini-acp: channel closed waiting for %s", method)
		}
		if resp.Error != nil {
			return nil, fmtACPError(method, resp.Error)
		}
		return resp.Result, nil
	case <-time.After(timeout):
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
		return nil, fmt.Errorf("gemini-acp: %s timed out after %v", method, timeout)
	}
}

func (h *GeminiACPExecutionHandle) sendNotification(method string, params interface{}) {
	notif := acpJSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	_ = h.writeJSON(notif)
}

// fmtACPError formats an ACP JSON-RPC error into a descriptive Go error.
// When the error includes a Data field (e.g. validation details), it is
// appended so the caller sees the full diagnostic information.
func fmtACPError(method string, e *acpRPCError) error {
	if len(e.Data) > 0 {
		return fmt.Errorf("gemini-acp: %s error: %s (code %d, data: %s)", method, e.Message, e.Code, string(e.Data))
	}
	return fmt.Errorf("gemini-acp: %s error: %s (code %d)", method, e.Message, e.Code)
}

// --- ACP handshake ---

func (h *GeminiACPExecutionHandle) acpInitialize() error {
	params := map[string]interface{}{
		"protocolVersion": 1,
		"clientCapabilities": map[string]interface{}{
			"fs":       map[string]interface{}{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
		"clientInfo": map[string]interface{}{
			"name":    "cceasy",
			"version": "1.0.0",
		},
	}

	resp, err := h.sendRequest("initialize", params, 30*time.Second)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("invalid initialize response: %w", err)
	}
	if _, ok := result["protocolVersion"]; !ok {
		return fmt.Errorf("initialize response missing protocolVersion")
	}
	return nil
}

func (h *GeminiACPExecutionHandle) acpNewSession() error {
	params := map[string]interface{}{
		"cwd":        h.cwd,
		"mcpServers": []interface{}{}, // Required: empty array if no MCP servers
	}

	resp, err := h.sendRequest("session/new", params, 30*time.Second)
	if err != nil {
		return err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("invalid session/new response: %w", err)
	}
	sid, _ := result["sessionId"].(string)
	if sid == "" {
		return fmt.Errorf("session/new response missing sessionId")
	}

	h.mu.Lock()
	h.sessionID = sid
	h.mu.Unlock()
	return nil
}

// --- stdout reader: JSON-RPC message dispatcher ---

func (h *GeminiACPExecutionHandle) readStdout() {
	defer h.readerRC.Done()

	scanner := bufio.NewScanner(h.stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		var msg acpJSONRPCResponse
		if err := json.Unmarshal([]byte(trimmed), &msg); err != nil {
			// Not valid JSON — emit as raw output
			h.outputCh <- []byte(trimmed + "\n")
			continue
		}

		// Response to a pending request
		if msg.ID != nil && msg.Method == "" {
			h.handleResponse(msg)
			continue
		}

		// Incoming request from server (e.g. session/request_permission)
		if msg.Method != "" && msg.ID != nil {
			h.handleServerRequest(msg)
			continue
		}

		// Notification from server (e.g. session/update)
		if msg.Method != "" {
			h.handleNotification(msg.Method, msg.Params)
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		h.outputCh <- []byte(fmt.Sprintf("[gemini-acp-read-error] %v\n", err))
	}

	// Reject all pending requests
	h.mu.Lock()
	for id, p := range h.pending {
		close(p.ch)
		delete(h.pending, id)
	}
	h.mu.Unlock()
}

func (h *GeminiACPExecutionHandle) handleResponse(msg acpJSONRPCResponse) {
	h.mu.Lock()
	// Normalize ID to int for matching (JSON numbers decode as float64)
	key := normalizeJSONRPCID(msg.ID)
	p, ok := h.pending[key]
	if ok {
		delete(h.pending, key)
	}
	h.mu.Unlock()

	if !ok {
		return
	}

	p.ch <- acpResponse{
		Result: msg.Result,
		Error:  msg.Error,
	}
}

func normalizeJSONRPCID(id interface{}) interface{} {
	switch v := id.(type) {
	case float64:
		return int(v)
	default:
		return id
	}
}

func (h *GeminiACPExecutionHandle) handleServerRequest(msg acpJSONRPCResponse) {
	switch msg.Method {
	case "session/request_permission":
		// Extract tool info for display.
		var params map[string]interface{}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			// Can't parse params — respond with error so the server doesn't hang.
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"error": map[string]interface{}{
					"code":    -32602,
					"message": fmt.Sprintf("invalid params: %v", err),
				},
			}
			_ = h.writeJSON(resp)
			break
		}

		toolName := ""
		if toolCall, ok := params["toolCall"].(map[string]interface{}); ok {
			toolName, _ = toolCall["title"].(string)
		}
		if toolName != "" {
			h.outputCh <- []byte(fmt.Sprintf("⚡ %s\n", toolName))
		}

		// Determine the default allow option ID from the options list.
		optionID := "allow_once"
		denyOptionID := ""
		if options, ok := params["options"].([]interface{}); ok {
			for _, opt := range options {
				if optMap, ok := opt.(map[string]interface{}); ok {
					kind, _ := optMap["kind"].(string)
					oid, _ := optMap["optionId"].(string)
					if (kind == "allow_once" || kind == "allow_always") && oid != "" {
						optionID = oid
					}
					if kind == "deny" && oid != "" {
						denyOptionID = oid
					}
				}
			}
		}

		// Use the permission handler if available, otherwise auto-approve.
		approved := true
		if h.Permissions != nil {
			reqID := fmt.Sprintf("acp_%v", msg.ID)
			permReq := PermissionRequest{
				RequestID: reqID,
				SessionID: h.SessionID(),
				ToolName:  toolName,
				CreatedAt: time.Now(),
			}
			comp := h.Permissions.HandleRequest(permReq)
			approved = comp.Decision == PermissionApproved || comp.Decision == PermissionApprovedForSession
		}

		if approved {
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"result": map[string]interface{}{
					"outcome": map[string]interface{}{
						"outcome":  "selected",
						"optionId": optionID,
					},
				},
			}
			_ = h.writeJSON(resp)
		} else {
			// Deny — use deny option if available, otherwise use allow_once
			// with a deny outcome.
			selectedID := denyOptionID
			if selectedID == "" {
				selectedID = optionID
			}
			resp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      msg.ID,
				"result": map[string]interface{}{
					"outcome": map[string]interface{}{
						"outcome":  "denied",
						"optionId": selectedID,
					},
				},
			}
			_ = h.writeJSON(resp)
		}
	default:
		// Unknown server request — respond with method not found
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      msg.ID,
			"error": map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("Method not found: %s", msg.Method),
			},
		}
		_ = h.writeJSON(resp)
	}
}

func (h *GeminiACPExecutionHandle) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "session/update":
		h.handleSessionUpdate(params)
	case "session/error":
		var p map[string]interface{}
		if json.Unmarshal(params, &p) == nil {
			msg, _ := p["message"].(string)
			if msg == "" {
				msg = "unknown session error"
			}
			h.outputCh <- []byte(fmt.Sprintf("[gemini-acp] session error: %s\n", msg))
		}
	default:
		// Unknown notifications are silently ignored per JSON-RPC spec.
	}
}

func (h *GeminiACPExecutionHandle) handleSessionUpdate(params json.RawMessage) {
	var p map[string]interface{}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}

	update, ok := p["update"].(map[string]interface{})
	if !ok {
		return
	}

	updateType, _ := update["sessionUpdate"].(string)
	switch updateType {
	case "agent_message_chunk", "agentMessageChunk":
		text := extractACPTextContent(update["content"])
		if text != "" {
			h.outputCh <- []byte(text)
		}
	case "tool_call", "toolCall":
		title, _ := update["title"].(string)
		if title == "" {
			title = "tool call"
		}
		status, _ := update["status"].(string)
		h.outputCh <- []byte(fmt.Sprintf("⚡ %s [%s]\n", title, status))
	case "tool_call_update", "toolCallUpdate":
		status, _ := update["status"].(string)
		if status == "completed" || status == "failed" {
			title, _ := update["title"].(string)
			if title == "" {
				title = "tool"
			}
			icon := "✓"
			if status == "failed" {
				icon = "✗"
			}
			h.outputCh <- []byte(fmt.Sprintf("%s %s %s\n", icon, title, status))
		}
	case "available_commands_update":
		// Gemini sends available commands after session creation — ignore silently
	}
}

// extractACPTextContent extracts text from an ACP content block.
func extractACPTextContent(content interface{}) string {
	block, ok := content.(map[string]interface{})
	if !ok {
		return ""
	}
	if blockType, _ := block["type"].(string); blockType != "text" {
		return ""
	}
	text, _ := block["text"].(string)
	return text
}

func (h *GeminiACPExecutionHandle) readStderr() {
	defer h.readerRC.Done()
	scanner := bufio.NewScanner(h.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			h.outputCh <- []byte("[stderr] " + line + "\n")
		}
	}
}

func (h *GeminiACPExecutionHandle) waitProcess() {
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
