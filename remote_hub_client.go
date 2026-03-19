package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type HubEnvelope struct {
	Type      string      `json:"type"`
	RequestID string      `json:"request_id,omitempty"`
	TS        int64       `json:"ts,omitempty"`
	MachineID string      `json:"machine_id,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Payload   interface{} `json:"payload"`
}

type inboundHubEnvelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	TS        int64           `json:"ts,omitempty"`
	MachineID string          `json:"machine_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type RemoteHubClient struct {
	app     *App
	manager *RemoteSessionManager

	mu             sync.Mutex
	conn           *websocket.Conn
	hubURL         string
	machineID      string
	machineToken   string
	connected      bool
	lastError      string
	dial           func(urlStr string) (*websocket.Conn, error)
	reconnectCh    chan struct{}
	reconnecting   atomic.Bool
	allowReconnect atomic.Bool

	// Preview delta batching: accumulate lines per session and flush periodically
	// to reduce WebSocket message frequency for PWA viewers.
	previewMu      sync.Mutex
	previewPending map[string]*pendingPreviewDelta // sessionID → accumulated delta
	previewTicker  *time.Ticker
	previewStopCh  chan struct{}

	// Summary throttling: avoid sending identical summaries repeatedly.
	summaryMu   sync.Mutex
	lastSummary map[string]string // sessionID → JSON of last sent summary

	// IM message handler for Agent Passthrough.
	imHandler *IMMessageHandler

	// IO relay for multi-device session roaming cleanup on disconnect.
	ioRelay *SessionIORelay
}

// pendingPreviewDelta accumulates preview lines for a session between flushes.
type pendingPreviewDelta struct {
	SessionID string
	Lines     []string
	OutputSeq int64
	UpdatedAt int64
}

// previewFlushInterval controls how often accumulated preview deltas are sent
// to the hub. Lower values = more responsive but more network traffic.
const previewFlushInterval = 150 * time.Millisecond

// hubPongWait is the maximum time the client waits for any incoming data or
// pong from the hub before considering the connection dead. Must be greater
// than the hub's ping interval (30s). Shared between connectLocked and readLoop.
const hubPongWait = 90 * time.Second

func NewRemoteHubClient(app *App, manager *RemoteSessionManager) *RemoteHubClient {
	return &RemoteHubClient{
		app:            app,
		manager:        manager,
		dial:           defaultHubDial,
		reconnectCh:    make(chan struct{}, 1),
		previewPending: make(map[string]*pendingPreviewDelta),
		previewStopCh:  make(chan struct{}),
		lastSummary:    make(map[string]string),
		imHandler:      NewIMMessageHandler(app, manager),
	}
}

func defaultHubDial(urlStr string) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(urlStr, nil)
	return conn, err
}

func (c *RemoteHubClient) loadConfig() error {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return err
	}

	c.hubURL = strings.TrimRight(cfg.RemoteHubURL, "/")
	c.machineID = cfg.RemoteMachineID
	c.machineToken = cfg.RemoteMachineToken

	if c.hubURL == "" {
		return fmt.Errorf("remote hub url is empty")
	}
	if c.machineID == "" || c.machineToken == "" {
		return fmt.Errorf("remote machine identity is incomplete")
	}
	return nil
}

func (c *RemoteHubClient) Connect() error {
	c.mu.Lock()
	if err := c.connectLocked(); err != nil {
		c.lastError = err.Error()
		c.mu.Unlock()
		return err
	}

	c.allowReconnect.Store(true)
	c.lastError = ""
	c.mu.Unlock()

	c.app.emitRemoteStateChanged()
	go c.readLoop()
	go c.heartbeatLoop()
	go c.SyncSessions()
	go c.SyncLaunchProjects()
	c.startPreviewFlusher()

	return nil
}

// errHubAuthFailed is returned when the hub rejects machine credentials.
var errHubAuthFailed = fmt.Errorf("hub authentication failed")

func (c *RemoteHubClient) connectLocked() error {
	if err := c.loadConfig(); err != nil {
		c.lastError = err.Error()
		return err
	}

	wsURL := c.toWebSocketURL(c.hubURL) + "/ws"
	conn, err := c.dial(wsURL)
	if err != nil {
		c.lastError = err.Error()
		return err
	}

	c.conn = conn
	c.connected = true

	// gorilla/websocket automatically replies to server pings with pongs.
	// Set a generous read deadline that gets refreshed by the pong handler
	// so the client detects a dead hub connection within a bounded time.
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(hubPongWait))
		return nil
	})

	// Clear summary dedup cache on new connection so re-synced summaries
	// are always sent to the hub.
	c.summaryMu.Lock()
	c.lastSummary = make(map[string]string)
	c.summaryMu.Unlock()

	if err := c.sendMachineAuthLocked(); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		c.lastError = err.Error()
		return err
	}

	// Read auth response synchronously so we can detect credential rejection
	// before proceeding with the hello handshake.
	var authResp inboundHubEnvelope
	_ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.ReadJSON(&authResp); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		c.lastError = "failed to read auth response"
		return fmt.Errorf("read auth response: %w", err)
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(hubPongWait)) // initial deadline; refreshed by pong handler

	if authResp.Type == "error" {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		c.lastError = "Machine authentication failed"
		return errHubAuthFailed
	}

	if err := c.sendMachineHelloLocked(); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		c.lastError = err.Error()
		return err
	}
	return nil
}

func (c *RemoteHubClient) toWebSocketURL(base string) string {
	if strings.HasPrefix(base, "https://") {
		return "wss://" + strings.TrimPrefix(base, "https://")
	}
	if strings.HasPrefix(base, "http://") {
		return "ws://" + strings.TrimPrefix(base, "http://")
	}
	return "ws://" + base
}

func (c *RemoteHubClient) sendMachineAuthLocked() error {
	msg := HubEnvelope{
		Type: "auth.machine",
		TS:   time.Now().Unix(),
		Payload: map[string]string{
			"machine_id":    c.machineID,
			"machine_token": c.machineToken,
		},
	}
	return c.conn.WriteJSON(msg)
}

func (c *RemoteHubClient) sendMachineHelloLocked() error {
	cfg, _ := c.app.LoadConfig()
	profile := c.app.currentRemoteMachineProfile(cfg.RemoteHeartbeatSec, 0)
	msg := HubEnvelope{
		Type:      "machine.hello",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]interface{}{
			"name":                   profile.Name,
			"platform":               profile.Platform,
			"hostname":               profile.Hostname,
			"arch":                   profile.Arch,
			"app_version":            profile.AppVersion,
			"heartbeat_interval_sec": profile.HeartbeatSec,
			"capabilities": map[string]interface{}{
				"remote_sessions": true,
				"pty":             true,
				"tools":           []string{"claude"},
				"llm_configured":  c.app.isMaclawLLMConfigured(),
			},
		},
	}
	return c.conn.WriteJSON(msg)
}

func (c *RemoteHubClient) SendSessionCreated(s *RemoteSession) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	execMode := "sdk"
	if _, isSDK := s.Exec.(*SDKExecutionHandle); isSDK {
		execMode = "sdk"
	} else if _, isACP := s.Exec.(*GeminiACPExecutionHandle); isACP {
		execMode = "gemini-acp"
	}

	msg := HubEnvelope{
		Type:      "session.created",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		SessionID: s.ID,
		Payload: map[string]interface{}{
			"tool":           s.Tool,
			"title":          s.Title,
			"source":         string(normalizeRemoteLaunchSource(s.LaunchSource)),
			"project_path":   s.ProjectPath,
			"status":         string(s.Status),
			"execution_mode": execMode,
			"started_at":     s.CreatedAt.Unix(),
		},
	}
	err := c.conn.WriteJSON(msg)
	if err == nil {
		c.app.emitEvent("remote-session-changed", "created", s.ID)
	}
	return err
}

func (c *RemoteHubClient) SendSessionSummary(summary SessionSummary) error {
	// Throttle: skip if the summary hasn't changed since last send.
	summary.MachineID = c.machineID
	data, err := json.Marshal(summary)
	if err == nil {
		key := string(data)
		c.summaryMu.Lock()
		if c.lastSummary[summary.SessionID] == key {
			c.summaryMu.Unlock()
			return nil
		}
		c.lastSummary[summary.SessionID] = key
		c.summaryMu.Unlock()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	msg := HubEnvelope{
		Type:      "session.summary",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		SessionID: summary.SessionID,
		Payload:   summary,
	}
	err = c.conn.WriteJSON(msg)
	if err == nil {
		c.app.emitEvent("remote-session-changed", "summary", summary.SessionID)
	}
	return err
}

func (c *RemoteHubClient) SendImportantEvent(event ImportantEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	event.MachineID = c.machineID
	msg := HubEnvelope{
		Type:      "session.important_event",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		SessionID: event.SessionID,
		Payload:   event,
	}
	err := c.conn.WriteJSON(msg)
	if err == nil {
		c.app.emitEvent("remote-session-changed", "important_event", event.SessionID)
	}
	return err
}

func (c *RemoteHubClient) SendPreviewDelta(delta SessionPreviewDelta) error {
	c.previewMu.Lock()
	pending, ok := c.previewPending[delta.SessionID]
	if !ok {
		pending = &pendingPreviewDelta{SessionID: delta.SessionID}
		c.previewPending[delta.SessionID] = pending
	}
	pending.Lines = append(pending.Lines, delta.AppendLines...)
	pending.OutputSeq = delta.OutputSeq
	pending.UpdatedAt = delta.UpdatedAt
	c.previewMu.Unlock()
	return nil
}

// startPreviewFlusher starts the background goroutine that periodically
// flushes accumulated preview deltas to the hub.
func (c *RemoteHubClient) startPreviewFlusher() {
	c.previewMu.Lock()
	// Stop any existing flusher before starting a new one
	if c.previewTicker != nil {
		c.previewTicker.Stop()
		c.previewTicker = nil
	}
	// Create a fresh stop channel
	c.previewStopCh = make(chan struct{}, 1)
	stopCh := c.previewStopCh
	c.previewTicker = time.NewTicker(previewFlushInterval)
	ticker := c.previewTicker
	c.previewMu.Unlock()

	go func() {
		for {
			select {
			case <-ticker.C:
				c.flushPreviewDeltas()
			case <-stopCh:
				ticker.Stop()
				// Final flush to avoid losing buffered data
				c.flushPreviewDeltas()
				return
			}
		}
	}()
}

// stopPreviewFlusher stops the background flush goroutine.
func (c *RemoteHubClient) stopPreviewFlusher() {
	c.previewMu.Lock()
	if c.previewStopCh != nil {
		select {
		case c.previewStopCh <- struct{}{}:
		default:
		}
	}
	c.previewMu.Unlock()
}

// flushPreviewDeltas sends all accumulated preview deltas to the hub in one pass.
func (c *RemoteHubClient) flushPreviewDeltas() {
	c.previewMu.Lock()
	if len(c.previewPending) == 0 {
		c.previewMu.Unlock()
		return
	}
	// Swap out the pending map
	toSend := c.previewPending
	c.previewPending = make(map[string]*pendingPreviewDelta)
	c.previewMu.Unlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return
	}

	for _, pending := range toSend {
		if len(pending.Lines) == 0 {
			continue
		}
		delta := SessionPreviewDelta{
			SessionID:   pending.SessionID,
			OutputSeq:   pending.OutputSeq,
			AppendLines: pending.Lines,
			UpdatedAt:   pending.UpdatedAt,
		}
		msg := HubEnvelope{
			Type:      "session.preview_delta",
			TS:        time.Now().Unix(),
			MachineID: c.machineID,
			SessionID: delta.SessionID,
			Payload:   delta,
		}
		if err := c.conn.WriteJSON(msg); err == nil {
			c.app.emitEvent("remote-session-changed", "preview_delta", delta.SessionID)
		}
	}
}

func (c *RemoteHubClient) SendSessionClosed(s *RemoteSession) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	msg := HubEnvelope{
		Type:      "session.closed",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		SessionID: s.ID,
		Payload: map[string]interface{}{
			"status":    string(s.Status),
			"exit_code": s.ExitCode,
			"ended_at":  time.Now().Unix(),
		},
	}
	err := c.conn.WriteJSON(msg)
	if err == nil {
		c.app.emitEvent("remote-session-changed", "closed", s.ID)
	}
	return err
}

// SendSessionImage sends an image extracted from SDK output to the Hub for delivery to mobile clients.
func (c *RemoteHubClient) SendSessionImage(img ImageTransferMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	msg := HubEnvelope{
		Type:      "session.image",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		SessionID: img.SessionID,
		Payload:   img,
	}
	return c.conn.WriteJSON(msg)
}


func (c *RemoteHubClient) SyncSessions() {
	if c.manager == nil {
		return
	}

	for _, s := range c.manager.List() {
		if s == nil {
			continue
		}
		_ = c.SendSessionCreated(s)
		for _, event := range s.Events {
			_ = c.SendImportantEvent(event)
		}
		_ = c.SendSessionSummary(s.Summary)
		if len(s.Preview.PreviewLines) > 0 {
			_ = c.SendPreviewDelta(SessionPreviewDelta{
				SessionID:   s.ID,
				OutputSeq:   s.Preview.OutputSeq,
				AppendLines: append([]string{}, s.Preview.PreviewLines...),
				UpdatedAt:   time.Now().Unix(),
			})
		}
	}
	// Flush batched preview deltas immediately after sync so viewers
	// receive the full initial state without waiting for the next tick.
	c.flushPreviewDeltas()
}

func (c *RemoteHubClient) SyncLaunchProjects() {
	projects, err := c.app.ListRemoteLaunchProjects()
	if err != nil {
		c.setLastError(err.Error())
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return
	}

	msg := HubEnvelope{
		Type:      "machine.projects",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]interface{}{
			"projects": projects,
		},
	}
	_ = c.conn.WriteJSON(msg)
}

// SessionMetadata holds lightweight metadata about an active session,
// used for multi-device session roaming via Hub heartbeats.
type SessionMetadata struct {
	ID          string `json:"id"`
	Tool        string `json:"tool"`
	ProjectPath string `json:"project_path"`
	Status      string `json:"status"`
}

// collectSessionMetadata returns metadata for all active sessions managed
// by the RemoteSessionManager. The caller must NOT hold c.mu.
func (c *RemoteHubClient) collectSessionMetadata() []SessionMetadata {
	if c.manager == nil {
		return nil
	}
	sessions := c.manager.List()
	if len(sessions) == 0 {
		return nil
	}
	meta := make([]SessionMetadata, 0, len(sessions))
	for _, s := range sessions {
		if s == nil {
			continue
		}
		meta = append(meta, SessionMetadata{
			ID:          s.ID,
			Tool:        s.Tool,
			ProjectPath: s.ProjectPath,
			Status:      string(s.Status),
		})
	}
	return meta
}

func (c *RemoteHubClient) SendHeartbeat() error {
	// Collect session metadata before acquiring the connection lock to
	// avoid holding c.mu while iterating sessions (manager has its own lock).
	sessions := c.collectSessionMetadata()

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	activeSessions := len(sessions)
	cfg, _ := c.app.LoadConfig()
	profile := c.app.currentRemoteMachineProfile(cfg.RemoteHeartbeatSec, activeSessions)

	msg := HubEnvelope{
		Type:      "machine.heartbeat",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]interface{}{
			"active_sessions":        activeSessions,
			"heartbeat_interval_sec": profile.HeartbeatSec,
			"app_version":            profile.AppVersion,
			"llm_configured":         c.app.isMaclawLLMConfigured(),
			"sessions":               sessions,
		},
	}
	return c.conn.WriteJSON(msg)
}

func (c *RemoteHubClient) readLoop() {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}

		var msg inboundHubEnvelope
		if err := conn.ReadJSON(&msg); err != nil {
			c.handleConnectionLoss(err)
			return
		}

		// Refresh read deadline on every incoming message so the connection
		// stays alive as long as the hub is actively sending data, even if
		// WebSocket-level pongs are slightly delayed.
		_ = conn.SetReadDeadline(time.Now().Add(hubPongWait))

		switch msg.Type {
		case "error":
			c.storeHubError(msg.Payload)
		case "session.start":
			// Run in a goroutine to avoid blocking the read loop during
			// potentially slow session creation (e.g. full-disk scans).
			go c.handleSessionStart(msg)
		case "session.input":
			c.handleSessionInput(msg)
		case "session.interrupt":
			c.handleSessionInterrupt(msg)
		case "session.kill":
			c.handleSessionKill(msg)
		case "session.image_input":
			c.handleSessionImageInput(msg)
		case "session.screenshot":
			c.handleSessionScreenshot(msg)
		case "im.user_message":
			go c.handleIMUserMessage(msg)
		}
	}
}

func (c *RemoteHubClient) handleSessionStart(msg inboundHubEnvelope) {
	var payload RemoteStartSessionRequest
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.replyStartError(msg.RequestID, err)
		return
	}
	if strings.TrimSpace(payload.Tool) == "" {
		c.replyStartError(msg.RequestID, fmt.Errorf("tool is required"))
		return
	}

	session, err := c.app.StartRemoteSessionForProject(payload)
	if err != nil {
		c.replyStartError(msg.RequestID, err)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return
	}
	_ = c.conn.WriteJSON(HubEnvelope{
		Type:      "session.start.result",
		RequestID: msg.RequestID,
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		SessionID: session.ID,
		Payload: map[string]interface{}{
			"status":       "ok",
			"session_id":   session.ID,
			"tool":         session.Tool,
			"title":        session.Title,
			"project_path": session.ProjectPath,
		},
	})
}

func (c *RemoteHubClient) handleSessionInput(msg inboundHubEnvelope) {
	if c.manager == nil || msg.SessionID == "" {
		return
	}
	var payload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.setLastError(err.Error())
		return
	}
	if payload.Text == "" {
		return
	}
	if err := c.manager.WriteInput(msg.SessionID, payload.Text); err != nil {
		c.setLastError(err.Error())
	}
	c.app.emitEvent("remote-session-changed", "input", msg.SessionID)
}

func (c *RemoteHubClient) handleSessionInterrupt(msg inboundHubEnvelope) {
	if c.manager == nil || msg.SessionID == "" {
		return
	}
	if err := c.manager.Interrupt(msg.SessionID); err != nil {
		c.setLastError(err.Error())
	}
	c.app.emitEvent("remote-session-changed", "interrupt", msg.SessionID)
}

func (c *RemoteHubClient) handleSessionKill(msg inboundHubEnvelope) {
	if c.manager == nil || msg.SessionID == "" {
		return
	}
	if err := c.manager.Kill(msg.SessionID); err != nil {
		c.setLastError(err.Error())
	}
	c.app.emitEvent("remote-session-changed", "kill", msg.SessionID)
}

func (c *RemoteHubClient) handleSessionImageInput(msg inboundHubEnvelope) {
	if c.manager == nil || msg.SessionID == "" {
		return
	}
	var img ImageTransferMessage
	if err := json.Unmarshal(msg.Payload, &img); err != nil {
		c.setLastError(err.Error())
		_ = c.SendSessionImageError(msg.SessionID, err.Error())
		return
	}
	// Ensure the session ID from the envelope is used.
	img.SessionID = msg.SessionID
	if err := c.manager.WriteImageInput(msg.SessionID, img); err != nil {
		c.setLastError(err.Error())
		_ = c.SendSessionImageError(msg.SessionID, err.Error())
		return
	}
	c.app.emitEvent("remote-session-changed", "image_input", msg.SessionID)
}

func (c *RemoteHubClient) handleSessionScreenshot(msg inboundHubEnvelope) {
	if c.manager == nil || msg.SessionID == "" {
		return
	}
	var payload struct {
		WindowTitle string `json:"window_title"`
	}
	_ = json.Unmarshal(msg.Payload, &payload)

	// Run screenshot capture in a goroutine to avoid blocking the WebSocket
	// read loop — screenshot commands can take several seconds.
	sessionID := msg.SessionID
	windowTitle := payload.WindowTitle
	go func() {
		var err error
		if windowTitle != "" {
			err = c.manager.CaptureWindowScreenshot(sessionID, windowTitle)
		} else {
			err = c.manager.CaptureScreenshot(sessionID)
		}
		if err != nil {
			c.setLastError(err.Error())
			c.app.log(fmt.Sprintf("[hub-screenshot] session=%s error: %v", sessionID, err))
			// Send error back to viewers so the PWA can display feedback.
			_ = c.SendSessionImageError(sessionID, "screenshot failed: "+err.Error())
		}
	}()
}

// handleIMUserMessage processes an IM user message forwarded from Hub.
// The Agent processing runs in a goroutine to avoid blocking the readLoop.
func (c *RemoteHubClient) handleIMUserMessage(msg inboundHubEnvelope) {
	var payload IMUserMessage
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.setLastError(fmt.Sprintf("im.user_message parse error: %s", err.Error()))
		return
	}

	requestID := msg.RequestID
	go func() {
		// Create a progress callback that sends intermediate updates to Hub.
		// Hub will relay these to the user via IM and reset the response timeout.
		onProgress := func(text string) {
			if err := c.sendIMAgentProgress(requestID, text); err != nil {
				c.app.log(fmt.Sprintf("[im-progress] send error for request=%s: %s", requestID, err.Error()))
			}
		}
		resp := c.imHandler.HandleIMMessageWithProgress(payload, onProgress)
		if err := c.sendIMAgentResponse(requestID, resp); err != nil {
			c.setLastError(fmt.Sprintf("im.agent_response send error: %s", err.Error()))
		}
	}()
}

// sendIMAgentResponse sends the Agent's reply back to Hub.
func (c *RemoteHubClient) sendIMAgentResponse(requestID string, resp *IMAgentResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	msg := HubEnvelope{
		Type:      "im.agent_response",
		RequestID: requestID,
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]interface{}{
			"response": resp,
		},
	}
	return c.conn.WriteJSON(msg)
}

// sendIMAgentProgress sends an intermediate progress update to Hub while the
// Agent is still working. Hub uses this to (a) deliver a status message to the
// user via IM and (b) reset the response timeout so long-running tasks don't
// trigger a 504.
func (c *RemoteHubClient) sendIMAgentProgress(requestID string, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	msg := HubEnvelope{
		Type:      "im.agent_progress",
		RequestID: requestID,
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]interface{}{
			"text": text,
		},
	}
	return c.conn.WriteJSON(msg)
}

// SendSessionImageError sends an error response to the Hub when image input injection fails.
func (c *RemoteHubClient) SendSessionImageError(sessionID, errorMsg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	msg := HubEnvelope{
		Type:      "session.image_input.error",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		SessionID: sessionID,
		Payload: map[string]string{
			"message": errorMsg,
		},
	}
	return c.conn.WriteJSON(msg)
}

// SendIMProactiveMessage sends a proactive (non-request-based) message to the
// Hub for delivery to the user's IM channels. Used for scheduled task results.
func (c *RemoteHubClient) SendIMProactiveMessage(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil // silently drop when not connected, consistent with other Send* methods
	}

	msg := HubEnvelope{
		Type:      "im.proactive_message",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]string{
			"text": text,
		},
	}
	return c.conn.WriteJSON(msg)
}

// SendIMProactiveFile sends a proactive file (non-request-based) to the Hub
// for delivery to the user's IM channels. Used for Swarm PDF document delivery.
func (c *RemoteHubClient) SendIMProactiveFile(b64Data, fileName, mimeType, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	msg := HubEnvelope{
		Type:      "im.proactive_file",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]interface{}{
			"file_data": b64Data,
			"file_name": fileName,
			"mime_type": mimeType,
			"message":   message,
		},
	}
	return c.conn.WriteJSON(msg)
}

func (c *RemoteHubClient) storeHubError(payload json.RawMessage) {
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		c.setLastError(err.Error())
		return
	}
	if body.Message != "" {
		c.setLastError(body.Message)
	}
}

func (c *RemoteHubClient) replyStartError(requestID string, err error) {
	if err == nil {
		return
	}
	c.setLastError(err.Error())

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return
	}
	_ = c.conn.WriteJSON(HubEnvelope{
		Type:      "error",
		RequestID: requestID,
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]string{
			"message": err.Error(),
		},
	})
}

func (c *RemoteHubClient) setLastError(message string) {
	c.mu.Lock()
	c.lastError = message
	c.mu.Unlock()

	c.app.emitRemoteStateChanged()
}

func (c *RemoteHubClient) heartbeatLoop() {
	for {
		interval := c.currentHeartbeatInterval()
		timer := time.NewTimer(interval)
		<-timer.C
		if !c.IsConnected() {
			timer.Stop()
			return
		}
		if err := c.SendHeartbeat(); err != nil {
			timer.Stop()
			c.handleConnectionLoss(err)
			return
		}
		timer.Stop()
	}
}

func (c *RemoteHubClient) currentHeartbeatInterval() time.Duration {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return time.Duration(defaultRemoteHeartbeatSec) * time.Second
	}
	return time.Duration(normalizeRemoteHeartbeatIntervalSec(cfg.RemoteHeartbeatSec)) * time.Second
}

func (c *RemoteHubClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected && c.conn != nil
}

func (c *RemoteHubClient) LastError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastError
}

func (c *RemoteHubClient) Disconnect() error {
	c.stopPreviewFlusher()
	c.mu.Lock()
	c.allowReconnect.Store(false)
	c.connected = false
	if c.conn == nil {
		c.mu.Unlock()
		c.app.emitRemoteStateChanged()
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	c.mu.Unlock()

	c.app.emitRemoteStateChanged()
	return err
}

func (c *RemoteHubClient) handleConnectionLoss(err error) {
	c.stopPreviewFlusher()
	c.cleanupIORelay()
	c.mu.Lock()
	if err != nil {
		c.lastError = err.Error()
	}
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.connected = false
	c.mu.Unlock()

	c.app.emitRemoteStateChanged()

	c.triggerReconnect()
}

func (c *RemoteHubClient) triggerReconnect() {
	if !c.allowReconnect.Load() {
		return
	}
	if c.reconnecting.Swap(true) {
		return
	}

	select {
	case c.reconnectCh <- struct{}{}:
	default:
	}

	go c.reconnectLoop()
}

func (c *RemoteHubClient) reconnectLoop() {
	defer c.reconnecting.Store(false)

	backoff := 500 * time.Millisecond
	for c.allowReconnect.Load() {
		if c.IsConnected() {
			return
		}

		err := c.Connect()
		if err == nil {
			return
		}

		// If the hub rejected our credentials, attempt re-enrollment so the
		// machine obtains fresh machine_id / machine_token before retrying.
		if errors.Is(err, errHubAuthFailed) {
			if c.tryReEnroll() {
				// Re-enrollment succeeded; retry connect immediately with new creds.
				continue
			}
			// Re-enrollment also failed — the server may have unbound this user.
			// Verify activation status and clear local state if needed.
			if !c.app.VerifyRemoteActivation() {
				return // activation was invalidated, stop reconnecting
			}
		}

		time.Sleep(backoff)
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

// tryReEnroll attempts to re-enroll with the hub using the saved email and
// client_id. Returns true if new credentials were obtained and persisted.
func (c *RemoteHubClient) tryReEnroll() bool {
	cfg, err := c.app.LoadConfig()
	if err != nil || cfg.RemoteEmail == "" {
		return false
	}
	result, err := c.app.ActivateRemote(cfg.RemoteEmail, "", "")
	if err != nil {
		return false
	}
	return result.MachineID != "" && result.MachineToken != ""
}

func (c *RemoteHubClient) appVersion() string {
	return remoteAppVersion()
}

// SetIORelay sets the IO relay used for multi-device session roaming.
func (c *RemoteHubClient) SetIORelay(relay *SessionIORelay) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ioRelay = relay
}

// cleanupIORelay unsubscribes this device from all active sessions in the
// IO relay. This ensures a disconnected device stops receiving output while
// sessions and other devices' subscriptions remain unaffected.
func (c *RemoteHubClient) cleanupIORelay() {
	if c.ioRelay == nil {
		return
	}
	if c.manager == nil {
		return
	}

	deviceID := c.machineID
	for _, s := range c.manager.List() {
		if s == nil {
			continue
		}
		c.ioRelay.Unsubscribe(s.ID, deviceID)
	}
}
