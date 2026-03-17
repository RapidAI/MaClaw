package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// IFlowSDKExecutionStrategy launches iFlow in ACP WebSocket mode.
// The iFlow CLI is started with --experimental-acp --port <PORT>, and
// communication happens via a WebSocket connection to ws://localhost:<PORT>/acp.
type IFlowSDKExecutionStrategy struct{}

func NewIFlowSDKExecutionStrategy() *IFlowSDKExecutionStrategy {
	return &IFlowSDKExecutionStrategy{}
}

func (s *IFlowSDKExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	execPath, err := resolveExecutablePath(cmd.Command)
	if err != nil {
		return nil, fmt.Errorf("iflow-sdk: %w", err)
	}

	// Read the ACP port from the environment.
	portStr := cmd.Env["IFLOW_ACP_PORT"]
	if portStr == "" {
		return nil, fmt.Errorf("iflow-sdk: IFLOW_ACP_PORT not set in command env")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("iflow-sdk: invalid IFLOW_ACP_PORT: %s", portStr)
	}

	args := append([]string{}, cmd.Args...)
	c := buildExecCmd(execPath, args, cmd.Cwd, cmd.Env)

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("iflow-sdk: start: %w", err)
	}

	// Wait for the ACP WebSocket server to become ready.
	wsURL := fmt.Sprintf("ws://localhost:%d/acp", port)
	ws, err := waitForACPReady(wsURL, 5*time.Second, 500*time.Millisecond)
	if err != nil {
		// Kill the process if we can't connect.
		_ = c.Process.Kill()
		_ = c.Wait()
		return nil, fmt.Errorf("iflow-sdk: ACP server not ready: %w", err)
	}

	handle := &IFlowSDKExecutionHandle{
		cmd:      c,
		ws:       ws,
		pid:      c.Process.Pid,
		outputCh: make(chan []byte, 128),
		exitCh:   make(chan PTYExit, 1),
	}

	go handle.readWebSocket()
	go handle.waitProcess()

	return handle, nil
}

// waitForACPReady polls the WebSocket endpoint until a connection succeeds
// or the timeout expires.
func waitForACPReady(url string, timeout, interval time.Duration) (*websocket.Conn, error) {
	deadline := time.Now().Add(timeout)
	dialer := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
	}

	var lastErr error
	for time.Now().Before(deadline) {
		conn, resp, err := dialer.Dial(url, http.Header{})
		if err == nil {
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			return conn, nil
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		lastErr = err
		time.Sleep(interval)
	}
	return nil, fmt.Errorf("timeout waiting for %s: %v", url, lastErr)
}

// ACPMessage represents an ACP WebSocket message from iFlow.
type ACPMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ACPAssistantPayload is the payload for AssistantMessage.
type ACPAssistantPayload struct {
	Content string `json:"content"`
}

// ACPToolCallPayload is the payload for ToolCallMessage.
type ACPToolCallPayload struct {
	ToolName string      `json:"tool_name"`
	Input    interface{} `json:"input"`
}

// ACPPlanPayload is the payload for PlanMessage.
type ACPPlanPayload struct {
	Plan string `json:"plan"`
}

// ACPTaskFinishPayload is the payload for TaskFinishMessage.
type ACPTaskFinishPayload struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// ACPUserMessage is the ACP-formatted message sent to iFlow via WebSocket.
type ACPUserMessage struct {
	Type    string              `json:"type"`
	Payload ACPUserInputPayload `json:"payload"`
}

// ACPUserInputPayload is the payload for a user input message.
type ACPUserInputPayload struct {
	Content string `json:"content"`
}

// ACPInterruptMessage is sent to request an interrupt via WebSocket.
type ACPInterruptMessage struct {
	Type string `json:"type"`
}

// IFlowSDKExecutionHandle wraps an iFlow process communicating via ACP WebSocket.
type IFlowSDKExecutionHandle struct {
	cmd *exec.Cmd
	ws  *websocket.Conn
	pid int

	outputCh chan []byte
	exitCh   chan PTYExit

	mu     sync.Mutex
	closed bool
}

func (h *IFlowSDKExecutionHandle) PID() int {
	return h.pid
}

// Write sends a user message to iFlow via the ACP WebSocket.
func (h *IFlowSDKExecutionHandle) Write(data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("iflow session closed")
	}

	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil
	}

	msg := ACPUserMessage{
		Type:    "UserMessage",
		Payload: ACPUserInputPayload{Content: text},
	}
	return h.ws.WriteJSON(msg)
}

// Interrupt sends an interrupt message to iFlow via the ACP WebSocket.
func (h *IFlowSDKExecutionHandle) Interrupt() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("iflow session closed")
	}

	msg := ACPInterruptMessage{Type: "Interrupt"}
	return h.ws.WriteJSON(msg)
}

func (h *IFlowSDKExecutionHandle) Kill() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return fmt.Errorf("process not available")
	}
	return h.cmd.Process.Kill()
}

func (h *IFlowSDKExecutionHandle) Output() <-chan []byte {
	return h.outputCh
}

func (h *IFlowSDKExecutionHandle) Exit() <-chan PTYExit {
	return h.exitCh
}

func (h *IFlowSDKExecutionHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true

	// Close the WebSocket connection gracefully.
	_ = h.ws.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	_ = h.ws.Close()
	return nil
}

// readWebSocket reads ACP messages from the WebSocket and converts them
// to human-readable text on the output channel.
func (h *IFlowSDKExecutionHandle) readWebSocket() {
	defer close(h.outputCh)

	for {
		_, message, err := h.ws.ReadMessage()
		if err != nil {
			// WebSocket closed or error — stop reading.
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.outputCh <- []byte(fmt.Sprintf("[iflow-ws-error] %v\n", err))
			}
			return
		}

		text := acpMessageToText(message)
		if text != "" {
			h.outputCh <- []byte(text + "\n")
		}
	}
}

// waitProcess waits for the iFlow process to exit and reports via exitCh.
func (h *IFlowSDKExecutionHandle) waitProcess() {
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

// acpMessageToText parses a raw ACP WebSocket message and converts it
// to human-readable text for the output pipeline.
func acpMessageToText(data []byte) string {
	var msg ACPMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		// Not valid JSON — emit as raw text.
		trimmed := strings.TrimSpace(string(data))
		if trimmed != "" {
			return trimmed
		}
		return ""
	}

	switch msg.Type {
	case "AssistantMessage":
		var payload ACPAssistantPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil && payload.Content != "" {
			return payload.Content
		}

	case "ToolCallMessage":
		var payload ACPToolCallPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil && payload.ToolName != "" {
			return fmt.Sprintf("⚡ %s", payload.ToolName)
		}

	case "PlanMessage":
		var payload ACPPlanPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil && payload.Plan != "" {
			return payload.Plan
		}

	case "TaskFinishMessage":
		var payload ACPTaskFinishPayload
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			if payload.Summary != "" {
				return fmt.Sprintf("✓ %s: %s", payload.Status, payload.Summary)
			}
			if payload.Status != "" {
				return fmt.Sprintf("✓ %s", payload.Status)
			}
		}
	}

	return ""
}
