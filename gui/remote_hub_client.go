package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
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
	previewPending map[string]*pendingPreviewDelta // sessionID -> accumulated delta
	previewTicker  *time.Ticker
	previewStopCh  chan struct{}

	// Summary throttling: avoid sending identical summaries repeatedly.
	summaryMu   sync.Mutex
	lastSummary map[string]string // sessionID -> JSON of last sent summary

	// IM message handler for Agent Passthrough.
	imHandlerMu        sync.Mutex
	imHandler          *IMMessageHandler
	configureIMHandler func(*IMMessageHandler)

	// Digital employee discussion handler for pushed Hub discussion messages.
	veHandlerMu     sync.Mutex
	veHandler       *VEMessageHandler
	groupDispatcher *GroupChatDispatcher
	veDetailRefresh sync.Map // sessionID -> *veDetailRefreshState

	// IO relay for multi-device session roaming cleanup on disconnect.
	ioRelay *SessionIORelay
}

type veDetailRefreshState struct {
	mu        sync.Mutex
	dirty     bool
	saturated int
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
	}
}

func (c *RemoteHubClient) ensureIMHandler() *IMMessageHandler {
	if c.imHandler != nil {
		return c.imHandler
	}
	c.imHandlerMu.Lock()
	defer c.imHandlerMu.Unlock()
	if c.imHandler != nil {
		return c.imHandler
	}
	h := NewIMMessageHandler(c.app, c.manager)
	c.imHandler = h
	if c.configureIMHandler != nil {
		c.configureIMHandler(h)
	}
	return h
}

func defaultHubDial(urlStr string) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer
	// Support wss:// with self-signed certificates (Hub TLS mode).
	if strings.HasPrefix(urlStr, "wss://") {
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	conn, _, err := dialer.Dial(urlStr, nil)
	return conn, err
}

func (c *RemoteHubClient) loadConfig() error {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return err
	}
	return c.applyConfig(cfg)
}

// persistViewerToken saves a viewer token received from the hub auth.ok
// response into the local config. Called synchronously from connectLocked
// so the token is available immediately after Connect() returns.
// Uses PatchConfig for atomic read-modify-write to avoid overwriting
// concurrent config changes.
func (c *RemoteHubClient) persistViewerToken(token string) {
	if err := c.app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteViewerToken = token
	}); err != nil {
		log.Printf("[hub-client] persistViewerToken: PatchConfig failed: %v", err)
		return
	}
	log.Printf("[hub-client] viewer token persisted from auth.ok response")
}

func (c *RemoteHubClient) applyConfig(cfg corelib.AppConfig) error {
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
	start := time.Now()
	c.mu.Lock()
	if err := c.connectLocked(); err != nil {
		c.lastError = err.Error()
		c.mu.Unlock()
		log.Printf("[onboarding] RemoteHubClient.Connect failed total=%s err=%v", time.Since(start), err)
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
	go c.SyncTools()
	c.app.TriggerHubManagedCapabilitySync("hub-connect")
	c.startPreviewFlusher()

	// Re-send IM gateway claims for any already-connected gateways that are
	// in hub mode. This covers both initial connect and reconnect scenarios.
	go c.syncIMGatewayClaims()
	log.Printf("[onboarding] RemoteHubClient.Connect total=%s", time.Since(start))

	return nil
}

// syncIMGatewayClaims sends gateway claims for all IM gateways that are
// currently connected and operating in hub (non-local) mode.
func (c *RemoteHubClient) syncIMGatewayClaims() {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return
	}
	// WeChat
	if !cfg.IsWeixinLocalMode() && c.app.weixinGateway != nil && normalizeGatewayConnectionStatus(c.app.weixinGateway.Status()).IsConnected() {
		if err := c.SendIMGatewayClaim(imGatewayPlatformWeixin); err == nil {
			log.Printf("[hub-client] re-sent weixin gateway claim on connect")
		}
	}
	// Lansenger
	if !cfg.IsLansengerLocalMode() && c.app.lansengerGateway != nil && normalizeGatewayConnectionStatus(c.app.lansengerGateway.Status()).IsConnected() {
		if err := c.SendIMGatewayClaim(imGatewayPlatformLansenger); err == nil {
			log.Printf("[hub-client] re-sent lansenger gateway claim on connect")
		}
	}
	// Telegram
	if !cfg.IsTelegramLocalMode() && c.app.telegramGateway != nil && normalizeGatewayConnectionStatus(c.app.telegramGateway.Status()).IsConnected() {
		if err := c.SendIMGatewayClaim(imGatewayPlatformTelegram); err == nil {
			log.Printf("[hub-client] re-sent telegram gateway claim on connect")
		}
	}
	// QQ Bot
	if !cfg.IsQQBotLocalMode() && c.app.qqBotGateway != nil && normalizeGatewayConnectionStatus(c.app.qqBotGateway.Status()).IsConnected() {
		if err := c.SendIMGatewayClaim(imGatewayPlatformQQBotRemote); err == nil {
			log.Printf("[hub-client] re-sent qqbot gateway claim on connect")
		}
	}
	// Third-party local HTTP gateway
	if !cfg.IsThirdPartyGatewayLocalMode() && c.app.thirdPartyGateway != nil && normalizeGatewayConnectionStatus(c.app.thirdPartyGateway.Status()).IsConnected() {
		if err := c.SendIMGatewayClaim(imGatewayPlatformThirdParty); err == nil {
			log.Printf("[hub-client] re-sent thirdparty gateway claim on connect")
		}
	}
}

// errHubAuthFailed is returned when the hub explicitly rejects machine credentials
// (e.g., machine unbound, user deleted, token revoked). Transient server errors
// during auth MUST NOT return this - use errHubTransientAuthError instead.
var errHubAuthFailed = fmt.Errorf("hub authentication failed")

// errHubTransientAuthError is returned when the hub returns an error during the
// auth handshake that is NOT a definitive credential rejection. This includes
// generic server errors, rate limiting, maintenance messages, etc.
// The reconnect loop should retry on this error without clearing credentials.
var errHubTransientAuthError = fmt.Errorf("hub auth response error (transient)")

// isDefinitiveAuthRejection inspects the error payload from a Hub "error" type
// message and determines whether it's a definitive credential rejection (machine
// unbound, token invalid, user deleted) vs a transient server error (overloaded,
// maintenance, rate limited). Only definitive rejections should trigger credential
// clearing. When in doubt, we assume transient - clearing credentials is irreversible.
func isDefinitiveAuthRejection(payload json.RawMessage) bool {
	if len(payload) == 0 {
		// No payload - cannot confirm it's a real auth rejection. Assume transient.
		return false
	}
	var errInfo struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	if json.Unmarshal(payload, &errInfo) != nil {
		return false // can't parse - assume transient
	}

	// Definitive rejection codes/messages from Hub.
	code := strings.ToLower(strings.TrimSpace(errInfo.Code))
	msg := strings.ToLower(strings.TrimSpace(errInfo.Message))
	reason := strings.ToLower(strings.TrimSpace(errInfo.Reason))

	definitiveKeywords := []string{
		"auth_failed", "authentication_failed", "invalid_token", "invalid token",
		"machine_not_found", "unbound", "revoked",
		"unauthorized", "invalid_credentials", "invalid credentials",
		"user_deleted", "machine_deleted", "token_expired", "token expired",
		"credential_rejected", "machine_revoked",
	}

	for _, kw := range definitiveKeywords {
		if strings.Contains(code, kw) || strings.Contains(msg, kw) || strings.Contains(reason, kw) {
			return true
		}
	}

	// "not_found" and "forbidden" require tighter matching to avoid false
	// positives with generic "endpoint not found" / "access forbidden" responses.
	// Only match when the subject is clearly about the machine or user.
	machineUserNotFound := []string{
		"machine not found", "user not found", "device not found",
		"token not found", "registration not found",
	}
	for _, phrase := range machineUserNotFound {
		if strings.Contains(msg, phrase) || strings.Contains(reason, phrase) {
			return true
		}
	}

	if isGenericRemoteHubRouteError(msg) || isGenericRemoteHubRouteError(reason) {
		return false
	}

	// Code-level exact matches for known Hub rejection codes with no route-shaped
	// error message attached.
	exactCodes := []string{"not_found", "forbidden"}
	for _, ec := range exactCodes {
		if code == ec {
			return true
		}
	}

	return false
}

func isGenericRemoteHubRouteError(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	// Match patterns that clearly indicate a routing/endpoint/HTTP-level issue,
	// not application-level messages about machine/user entities.
	routePhrases := []string{
		"endpoint not found",
		"route not found",
		"path not found",
		"api not found",
		"no route",
		"404 not found",
	}
	for _, phrase := range routePhrases {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	// Heuristic: message contains both "not found" and a URL-path-like segment
	// (e.g., "/api/v3/hello not found", "route /ws/auth not found").
	// This indicates the Hub/proxy couldn't route the request, not that the
	// machine entity doesn't exist.
	if strings.Contains(value, "not found") && (strings.Contains(value, "/api") || strings.Contains(value, "/ws") || strings.Contains(value, "/v1") || strings.Contains(value, "/v2") || strings.Contains(value, "/v3")) {
		return true
	}
	return false
}

// truncateLogPayload returns a string representation of a JSON payload suitable
// for logging, truncated to avoid dumping overly large or sensitive content.
func truncateLogPayload(payload json.RawMessage) string {
	if len(payload) == 0 {
		return "(empty)"
	}
	s := string(payload)
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

func (c *RemoteHubClient) connectLocked() error {
	start := time.Now()
	loadCfgStart := time.Now()
	if err := c.loadConfig(); err != nil {
		c.lastError = err.Error()
		return err
	}
	log.Printf("[onboarding] RemoteHubClient.connectLocked load_config=%s", time.Since(loadCfgStart))

	wsURL := c.toWebSocketURL(c.hubURL) + "/ws"
	dialStart := time.Now()
	conn, err := c.dial(wsURL)
	if err != nil {
		c.lastError = err.Error()
		return err
	}
	log.Printf("[onboarding] RemoteHubClient.connectLocked dial_ws=%s url=%s", time.Since(dialStart), wsURL)

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

	authSendStart := time.Now()
	if err := c.sendMachineAuthLocked(); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		c.lastError = err.Error()
		return err
	}
	log.Printf("[onboarding] RemoteHubClient.connectLocked send_auth=%s", time.Since(authSendStart))

	// Read auth response synchronously so we can detect credential rejection
	// before proceeding with the hello handshake.
	var authResp inboundHubEnvelope
	authReadStart := time.Now()
	_ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.ReadJSON(&authResp); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		c.lastError = "failed to read auth response"
		return fmt.Errorf("read auth response: %w", err)
	}
	log.Printf("[onboarding] RemoteHubClient.connectLocked read_auth=%s auth_type=%s", time.Since(authReadStart), authResp.Type)
	_ = c.conn.SetReadDeadline(time.Now().Add(hubPongWait)) // initial deadline; refreshed by pong handler

	authRespType := normalizeHubInboundMessageType(authResp.Type)
	if authRespType.IsError() {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		if isDefinitiveAuthRejection(authResp.Payload) {
			c.lastError = "Machine authentication failed"
			log.Printf("[hub-client] connectLocked: definitive auth rejection, payload=%s", truncateLogPayload(authResp.Payload))
			return errHubAuthFailed
		}
		c.lastError = "Hub returned error during auth (possibly transient)"
		log.Printf("[hub-client] connectLocked: transient auth error, payload=%s", truncateLogPayload(authResp.Payload))
		return errHubTransientAuthError
	}

	// Extract viewer_token from auth.ok payload if present.
	// This allows existing clients (which only have machine_token) to
	// obtain a viewer_token for LLM service APIs without re-enrolling.
	if authRespType.IsAuthOK() && len(authResp.Payload) > 0 {
		var authPayload struct {
			ViewerToken string `json:"viewer_token"`
		}
		if json.Unmarshal(authResp.Payload, &authPayload) == nil && authPayload.ViewerToken != "" {
			// Persist synchronously so the token is available immediately
			// after Connect() returns. Previously this was async (goroutine)
			// which caused a race: callers reading config right after Connect()
			// would see an empty RemoteViewerToken.
			c.persistViewerToken(authPayload.ViewerToken)
		}
	}

	helloStart := time.Now()
	if err := c.sendMachineHelloLocked(); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		c.lastError = err.Error()
		return err
	}
	log.Printf("[onboarding] RemoteHubClient.connectLocked send_hello=%s total=%s", time.Since(helloStart), time.Since(start))
	return nil
}

// connectAuthOnlyLocked performs WebSocket dial + machine auth + reads auth response,
// but does NOT call sendMachineHelloLocked(). This allows the caller to emit the
// "ready" event immediately after auth succeeds and run sendMachineHelloLocked()
// in a separate goroutine without blocking the ready signal.
// Caller must hold c.mu.
func (c *RemoteHubClient) connectAuthOnlyLocked() error {
	start := time.Now()
	if err := c.loadConfig(); err != nil {
		c.lastError = err.Error()
		return err
	}

	wsURL := c.toWebSocketURL(c.hubURL) + "/ws"
	dialStart := time.Now()
	conn, err := c.dial(wsURL)
	if err != nil {
		c.lastError = err.Error()
		return err
	}
	log.Printf("[asyncHubConnect] dial_ws=%s url=%s", time.Since(dialStart), wsURL)

	c.conn = conn
	c.connected = true

	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(hubPongWait))
		return nil
	})

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

	// Read auth response with 10s timeout.
	var authResp inboundHubEnvelope
	_ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := c.conn.ReadJSON(&authResp); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		c.lastError = "failed to read auth response"
		return fmt.Errorf("read auth response: %w", err)
	}
	_ = c.conn.SetReadDeadline(time.Now().Add(hubPongWait))

	authRespType := normalizeHubInboundMessageType(authResp.Type)
	if authRespType.IsError() {
		_ = c.conn.Close()
		c.conn = nil
		c.connected = false
		if isDefinitiveAuthRejection(authResp.Payload) {
			c.lastError = "Machine authentication failed"
			log.Printf("[hub-client] connectAuthOnlyLocked: definitive auth rejection, payload=%s", truncateLogPayload(authResp.Payload))
			return errHubAuthFailed
		}
		c.lastError = "Hub returned error during auth (possibly transient)"
		log.Printf("[hub-client] connectAuthOnlyLocked: transient auth error, payload=%s", truncateLogPayload(authResp.Payload))
		return errHubTransientAuthError
	}

	// Extract viewer_token from auth.ok payload if present.
	if authRespType.IsAuthOK() && len(authResp.Payload) > 0 {
		var authPayload struct {
			ViewerToken string `json:"viewer_token"`
		}
		if json.Unmarshal(authResp.Payload, &authPayload) == nil && authPayload.ViewerToken != "" {
			c.persistViewerToken(authPayload.ViewerToken)
		}
	}

	log.Printf("[asyncHubConnect] auth completed in %s", time.Since(start))
	return nil
}

// ConnectAuthOnly performs WebSocket dial + auth without sendMachineHelloLocked.
// After success, it starts the read loop, heartbeat, and sync goroutines.
// The caller is responsible for calling sendMachineHelloLocked() separately.
func (c *RemoteHubClient) ConnectAuthOnly() error {
	start := time.Now()
	c.mu.Lock()
	if err := c.connectAuthOnlyLocked(); err != nil {
		// lastError already set by connectAuthOnlyLocked
		c.mu.Unlock()
		log.Printf("[asyncHubConnect] ConnectAuthOnly failed total=%s err=%v", time.Since(start), err)
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
	go c.SyncTools()
	c.app.TriggerHubManagedCapabilitySync("hub-connect")
	c.startPreviewFlusher()
	go c.syncIMGatewayClaims()
	log.Printf("[asyncHubConnect] ConnectAuthOnly total=%s", time.Since(start))

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
	_ = c.applyConfig(cfg)
	profile := c.app.currentRemoteMachineProfile(cfg.RemoteHeartbeatSec, 0)

	// Use ToolVersionCache to get tool names without executing external processes.
	// This avoids the slow listRemoteToolMetadataForApp which calls GetToolStatus
	// (which executes tool binaries to get version info).
	var toolNames []string
	if c.app.toolVersionCache != nil {
		toolNames = c.app.toolVersionCache.GetCachedToolNames()
	}

	// If cache is empty (first startup or no prior cache), use install-status-only
	// detection via GetInstallStatus (exec.LookPath / file stat, no process execution).
	if len(toolNames) == 0 {
		defaultTools := []string{"claude", "codex", "opencode", "codebuddy", "iflow", "kilo", "browser"}
		for _, name := range defaultTools {
			if name == "browser" {
				// browser is always a builtin capability
				toolNames = append(toolNames, name)
				continue
			}
			if c.app.toolVersionCache == nil {
				// No cache available - skip install detection for this tool.
				// This can happen if toolVersionCache initialization failed.
				continue
			}
			if installed, _ := c.app.toolVersionCache.GetInstallStatus(name); installed {
				toolNames = append(toolNames, name)
			}
		}
	}

	if len(toolNames) == 0 {
		toolNames = []string{"claude"}
	}

	msg := HubEnvelope{
		Type:      "machine.hello",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]interface{}{
			"name":                   profile.Name,
			"nickname":               profile.Nickname,
			"platform":               profile.Platform,
			"hostname":               profile.Hostname,
			"arch":                   profile.Arch,
			"app_version":            profile.AppVersion,
			"heartbeat_interval_sec": profile.HeartbeatSec,
			"capabilities": map[string]interface{}{
				"remote_sessions": true,
				"pty":             true,
				"tools":           toolNames,
				"llm_configured":  c.app.isMaclawLLMConfigured(),
			},
		},
	}
	err := c.conn.WriteJSON(msg)

	// After sending hello, asynchronously refresh version cache for all tools.
	// This runs external processes in background goroutines with a 10s combined timeout.
	if c.app.toolVersionCache != nil {
		allTools := []string{"claude", "codex", "opencode", "codebuddy", "iflow", "kilo"}
		c.app.toolVersionCache.RefreshAllAsync(allTools, 10*time.Second)
	}

	return err
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

func (c *RemoteHubClient) summaryWithSessionTokenUsage(summary SessionSummary) SessionSummary {
	if c == nil || c.manager == nil || summary.TokenUsage != nil || strings.TrimSpace(summary.SessionID) == "" {
		return summary
	}
	session, ok := c.manager.Get(summary.SessionID)
	if !ok || session == nil {
		return summary
	}
	session.mu.RLock()
	usage := session.TokenUsage
	session.mu.RUnlock()
	if usage.IsZero() {
		return summary
	}
	copy := usage
	summary.TokenUsage = &copy
	return summary
}

func (c *RemoteHubClient) SendSessionSummary(summary SessionSummary) error {
	// Throttle: skip if the summary hasn't changed since last send.
	summary.MachineID = c.machineID
	summary = c.summaryWithSessionTokenUsage(summary)
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

// SyncTools sends the machine's available tools and their provider configs to Hub.
func (c *RemoteHubClient) SyncTools() {
	tools := listRemoteToolMetadataForApp(c.app)
	cfg, _ := c.app.LoadConfig()

	type toolProviderInfo struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		ModelName   string `json:"model_name"`
		HasKey      bool   `json:"has_key"`
		IsBuiltin   bool   `json:"is_builtin"`
	}
	type toolInfo struct {
		Name        string             `json:"name"`
		DisplayName string             `json:"display_name"`
		Installed   bool               `json:"installed"`
		CanStart    bool               `json:"can_start"`
		Current     string             `json:"current_provider"`
		Providers   []toolProviderInfo `json:"providers"`
	}

	items := make([]toolInfo, 0, len(tools))
	for _, t := range tools {
		if !t.Visible {
			continue
		}
		tc, err := remoteToolConfig(cfg, t.Name)
		if err != nil {
			items = append(items, toolInfo{
				Name: t.Name, DisplayName: t.DisplayName,
				Installed: t.Installed, CanStart: t.CanStart,
			})
			continue
		}
		providers := make([]toolProviderInfo, 0, len(tc.Models))
		for _, m := range tc.Models {
			providers = append(providers, toolProviderInfo{
				Name:        m.ModelName,
				DisplayName: m.ModelName,
				ModelName:   m.ModelId,
				HasKey:      strings.TrimSpace(m.ApiKey) != "" || m.IsBuiltin || m.HasSubscription,
				IsBuiltin:   m.IsBuiltin,
			})
		}
		items = append(items, toolInfo{
			Name: t.Name, DisplayName: t.DisplayName,
			Installed: t.Installed, CanStart: t.CanStart,
			Current:   tc.CurrentModel,
			Providers: providers,
		})
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return
	}

	_ = c.conn.WriteJSON(HubEnvelope{
		Type:      "machine.tools",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]interface{}{
			"tools": items,
		},
	})
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

func (c *RemoteHubClient) collectLLMTokenUsage() *corelib.TokenUsageStat {
	all := c.app.GetAllLLMTokenUsage()
	if len(all) == 0 {
		return nil
	}
	total := &corelib.TokenUsageStat{}
	for _, stat := range all {
		if stat == nil {
			continue
		}
		total.InputTokens += stat.InputTokens
		total.OutputTokens += stat.OutputTokens
		total.CachedInputTokens += stat.CachedInputTokens
		total.CacheWriteTokens += stat.CacheWriteTokens
	}
	total.TotalTokens = total.InputTokens + total.OutputTokens
	if total.TotalTokens <= 0 {
		return nil
	}
	return total
}

func (c *RemoteHubClient) SendHeartbeat() error {
	// Collect session metadata before acquiring the connection lock to
	// avoid holding c.mu while iterating sessions (manager has its own lock).
	sessions := c.collectSessionMetadata()
	llmTokenUsage := c.collectLLMTokenUsage()
	cfg, _ := c.app.LoadConfig()

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	activeSessions := len(sessions)
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
			"llm_token_usage":        llmTokenUsage,
			"sessions":               sessions,
		},
	}
	return c.conn.WriteJSON(msg)
}

// handleAck processes heartbeat ack messages from the Hub.
// It extracts Hub-pushed policy/config fields and updates local state.
func (c *RemoteHubClient) handleAck(msg inboundHubEnvelope) {
	if len(msg.Payload) == 0 {
		return
	}
	c.app.updateHubSecurityPolicy(msg.Payload)
	configChanged := c.app.updateHubHeartbeatConfig(msg.Payload)
	if hubAckHasConfig(msg.Payload) {
		reason := "hub-heartbeat"
		if configChanged {
			reason = "hub-config-update"
		}
		c.app.TriggerHubManagedCapabilitySync(reason)
	}
}

func hubAckHasConfig(payload json.RawMessage) bool {
	var wrapper struct {
		HubConfig json.RawMessage `json:"hub_config"`
	}
	if err := json.Unmarshal(payload, &wrapper); err != nil {
		return false
	}
	return len(wrapper.HubConfig) > 0 && string(wrapper.HubConfig) != "null"
}

func (c *RemoteHubClient) readLoop() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[hub-client] readLoop panic recovered: %v", r)
			c.handleConnectionLoss(fmt.Errorf("readLoop panic: %v", r))
		}
	}()
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

		switch normalizeHubInboundMessageType(msg.Type) {
		case hubInboundMessageError:
			c.storeHubError(msg.Payload)
		case hubInboundMessageSessionStart:
			// Run in a goroutine to avoid blocking the read loop during
			// potentially slow session creation (e.g. full-disk scans).
			go c.handleSessionStart(msg)
		case hubInboundMessageSessionInput:
			c.handleSessionInput(msg)
		case hubInboundMessageSessionInterrupt:
			c.handleSessionInterrupt(msg)
		case hubInboundMessageSessionKill:
			c.handleSessionKill(msg)
		case hubInboundMessageSessionImageInput:
			c.handleSessionImageInput(msg)
		case hubInboundMessageSessionScreenshot:
			c.handleSessionScreenshot(msg)
		case hubInboundMessageIMUserMessage:
			go c.handleIMUserMessage(msg)
		case hubInboundMessageIMCancelSession:
			go c.handleIMCancelSession(msg)
		case hubInboundMessageIMGatewayReply:
			go c.handleIMGatewayReply(msg)
		case hubInboundMessageGatewayClaimResult:
			c.handleIMGatewayClaimResult(msg)
		case hubInboundMessageNicknameAssigned:
			c.handleNicknameAssigned(msg)
		case hubInboundMessageVEEvent:
			c.handleVEEvent(msg)
		case hubInboundMessageAck:
			c.handleAck(msg)
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
	// read loop - screenshot commands can take several seconds.
	sessionID := msg.SessionID
	windowTitle := payload.WindowTitle
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[hub-screenshot] panic recovered for session=%s: %v", sessionID, r)
			}
		}()
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
	payload.RequestID = requestID
	go func() {
		// Create a progress callback that sends intermediate updates to Hub.
		// Hub will relay these to the user via IM and reset the response timeout.
		progressFilter := newIMProgressVisibilityFilter(c.app)
		onProgress := func(text string) {
			forwardText, ok := progressFilter.ForwardProgressOrHeartbeat(text)
			if !ok {
				return
			}
			if err := c.sendIMAgentProgress(requestID, forwardText); err != nil {
				c.app.log(fmt.Sprintf("[im-progress] send error for request=%s: %s", requestID, err.Error()))
			}
		}
		handler := c.ensureIMHandler()
		if handler == nil {
			c.setLastError("im handler not initialized")
			return
		}

		// Interrupt check: if the agent loop is already running (chatLoopMu held)
		// and this message is a cancel/merge/status signal, handle it immediately
		// without waiting for the lock.
		if handler.shouldTryInlineInterrupt(payload) {
			result := handler.interruptHandler.TryInterrupt(payload.UserID, payload.Text)
			if result.PendingConfirm {
				// Scheduler uncertain - send confirmation with corrections.
				// Hub frontend renders buttons; user clicks one to resolve.
				// TODO: Hub-side correction store + fallback timer.
				if result.Reply != "" {
					resp := &IMAgentResponse{
						Text:        result.Reply,
						Corrections: result.Corrections,
					}
					if err := c.sendIMAgentResponse(requestID, resp); err != nil {
						c.setLastError(fmt.Sprintf("im.agent_response send error: %s", err.Error()))
					}
				}
				return // Message held - not consumed, not queued.
			}
			if result.Handled {
				if result.Reply != "" {
					resp := &IMAgentResponse{
						Text:        result.Reply,
						Corrections: result.Corrections,
					}
					if err := c.sendIMAgentResponse(requestID, resp); err != nil {
						c.setLastError(fmt.Sprintf("im.agent_response send error: %s", err.Error()))
					}
				}
				return // Fully handled - message was a control signal, not a new task.
			}
			if result.Queued && result.Reply != "" {
				// Queue - send instant feedback, then fall through
				// to HandleIMMessageWithProgress (which will block on chatLoopMu
				// until the current loop finishes, then process normally).
				resp := &IMAgentResponse{
					Text:        result.Reply,
					Corrections: result.Corrections,
				}
				if err := c.sendIMAgentResponse(requestID, resp); err != nil {
					c.setLastError(fmt.Sprintf("im.agent_response send error: %s", err.Error()))
				}
				// Don't return - let the message continue to normal processing below.
			}
		}

		resp := handler.HandleIMMessageWithProgress(payload, onProgress)
		// Downsize large screenshots before sending over WebSocket to Hub.
		// Multi-monitor captures can be several MB; Hub WebSocket may timeout.
		if resp != nil && len(resp.ImageKey) > 500_000 {
			if ds, err := remote.DownsizeScreenshotBase64(resp.ImageKey, 400_000); err == nil {
				resp.ImageKey = ds
			}
		}
		if err := c.sendIMAgentResponse(requestID, resp); err != nil {
			c.setLastError(fmt.Sprintf("im.agent_response send error: %s", err.Error()))
		}
	}()
}

// handleIMCancelSession handles im.cancel_session from Hub - cancels the
// currently running agent loop so the user can start a new task.
func (c *RemoteHubClient) handleIMCancelSession(msg inboundHubEnvelope) {
	log.Printf("[hub-client] im.cancel_session received")
	if c.imHandler != nil {
		var payload struct {
			UserID string `json:"user_id"`
		}
		if len(msg.Payload) > 0 && json.Unmarshal(msg.Payload, &payload) == nil && strings.TrimSpace(payload.UserID) != "" {
			_, _ = c.imHandler.CancelSessionForUser(payload.UserID)
			return
		}
		log.Printf("[hub-client] im.cancel_session ignored: missing user_id payload")
	}
}

// handleIMGatewayReply handles im.gateway_reply from Hub - delivers the
// reply to the appropriate client-side IM gateway (QQ Bot or Telegram).
func (c *RemoteHubClient) handleIMGatewayReply(msg inboundHubEnvelope) {
	var payload struct {
		Platform string          `json:"platform"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[hub-client] im.gateway_reply parse error: %v", err)
		return
	}

	var reply GatewayReplyPayload
	if err := json.Unmarshal(payload.Payload, &reply); err != nil {
		log.Printf("[hub-client] im.gateway_reply payload parse error: %v", err)
		return
	}

	c.app.log(fmt.Sprintf("[hub-client] im.gateway_reply received: platform=%s reply_type=%s uid=%s", payload.Platform, reply.ReplyType, reply.PlatformUID))

	switch normalizeIMGatewayPlatformKind(payload.Platform) {
	case imGatewayPlatformQQBotRemote:
		if c.app.qqBotGateway == nil {
			c.app.log("[hub-client] im.gateway_reply: qqBotGateway is nil, ignoring")
			return
		}
		switch normalizeGatewayReplyTypeKind(reply.ReplyType) {
		case gatewayReplyTypeText:
			_ = c.app.qqBotGateway.SendQQBotReply(reply.PlatformUID, reply.Text)
		case gatewayReplyTypeImage:
			_ = c.app.qqBotGateway.SendQQBotMedia(qqbot.OutgoingMedia{
				OpenID:   reply.PlatformUID,
				FileType: 1,
				FileData: reply.ImageData,
				MimeType: "image/png",
			})
		case gatewayReplyTypeFile:
			_ = c.app.qqBotGateway.SendQQBotMedia(qqbot.OutgoingMedia{
				OpenID:   reply.PlatformUID,
				FileType: 4,
				FileData: reply.FileData,
				FileName: reply.FileName,
				MimeType: reply.MimeType,
			})
		case gatewayReplyTypeVoice:
			_ = c.app.qqBotGateway.SendQQBotMedia(qqbot.OutgoingMedia{
				OpenID:   reply.PlatformUID,
				FileType: 3,
				FileData: reply.FileData,
				FileName: reply.FileName,
				MimeType: reply.MimeType,
			})
		}
	case imGatewayPlatformTelegram:
		if c.app.telegramGateway == nil {
			return
		}
		c.app.telegramGateway.HandleGatewayReply(GatewayReplyPayload{
			ReplyType:   reply.ReplyType,
			PlatformUID: reply.PlatformUID,
			Text:        reply.Text,
			ImageData:   reply.ImageData,
			Caption:     reply.Caption,
			FileData:    reply.FileData,
			FileName:    reply.FileName,
			MimeType:    reply.MimeType,
		})
	case imGatewayPlatformWeixin:
		wl := weixin.GetWxLog()
		if c.app.weixinGateway == nil {
			wl.Log("hubClient.reply", "IN", reply.PlatformUID, "ERR weixinGateway is nil, dropping")
			c.app.log("[hub-client] im.gateway_reply: weixinGateway is nil, ignoring")
			return
		}
		wl.Log("hubClient.reply", "IN", reply.PlatformUID, "dispatching type=%s text_len=%d ctx_token_len=%d", reply.ReplyType, len(reply.Text), len(reply.ContextToken))
		c.app.log(fmt.Sprintf("[hub-client] im.gateway_reply: dispatching to weixinGateway, text_len=%d ctx_token_len=%d", len([]rune(reply.Text)), len(reply.ContextToken)))
		c.app.weixinGateway.HandleGatewayReply(GatewayReplyPayload{
			ReplyType:    reply.ReplyType,
			PlatformUID:  reply.PlatformUID,
			Text:         reply.Text,
			ImageData:    reply.ImageData,
			Caption:      reply.Caption,
			FileData:     reply.FileData,
			FileName:     reply.FileName,
			MimeType:     reply.MimeType,
			ContextToken: reply.ContextToken,
			Extra:        reply.Extra,
		})
	case imGatewayPlatformLansenger:
		if c.app.lansengerGateway == nil {
			c.app.log("[hub-client] im.gateway_reply: lansengerGateway is nil, ignoring")
			return
		}
		c.app.lansengerGateway.HandleGatewayReply(GatewayReplyPayload{
			ReplyType:   reply.ReplyType,
			PlatformUID: reply.PlatformUID,
			Text:        reply.Text,
			ImageData:   reply.ImageData,
			Caption:     reply.Caption,
			FileData:    reply.FileData,
			FileName:    reply.FileName,
			MimeType:    reply.MimeType,
		})
	case imGatewayPlatformThirdParty:
		if c.app.thirdPartyGateway == nil {
			c.app.log("[hub-client] im.gateway_reply: thirdPartyGateway is nil, ignoring")
			return
		}
		c.app.thirdPartyGateway.HandleGatewayReply(GatewayReplyPayload{
			ReplyType:   reply.ReplyType,
			PlatformUID: reply.PlatformUID,
			Text:        reply.Text,
			ImageData:   reply.ImageData,
			Caption:     reply.Caption,
			FileData:    reply.FileData,
			FileName:    reply.FileName,
			MimeType:    reply.MimeType,
			Extra:       reply.Extra,
		})
	}
}

// handleIMGatewayClaimResult handles im.gateway_claim_result from Hub.
func (c *RemoteHubClient) handleIMGatewayClaimResult(msg inboundHubEnvelope) {
	var payload struct {
		Platform string `json:"platform"`
		OK       bool   `json:"ok"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return
	}
	if payload.OK {
		log.Printf("[hub-client] gateway claim OK for platform=%s", payload.Platform)
	} else {
		log.Printf("[hub-client] gateway claim DENIED for platform=%s: %s", payload.Platform, payload.Reason)
	}
}

func (c *RemoteHubClient) handleNicknameAssigned(msg inboundHubEnvelope) {
	var payload struct {
		Nickname string `json:"nickname"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return
	}
	nickname := strings.TrimSpace(payload.Nickname)
	if nickname == "" {
		return
	}
	log.Printf("[hub-client] nickname assigned by Hub: %q", nickname)
	// Always accept hub-assigned nickname - the hub only sends this when
	// auto-assigning (first time) or resolving a conflict with another
	// online device, so it should always take effect.
	_ = c.app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteNickname = nickname
	})
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

// SendNicknameUpdate sends a runtime nickname change to the Hub.
func (c *RemoteHubClient) SendNicknameUpdate(nickname string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}
	msg := HubEnvelope{
		Type:      "machine.nickname_update",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]string{
			"nickname": nickname,
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
		return fmt.Errorf("not connected to Hub")
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

// SendIMGatewayClaim sends im.gateway_claim to Hub to register this machine
// as the gateway owner for the given IM platform.
func (c *RemoteHubClient) SendIMGatewayClaim(platform imGatewayPlatformKind) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}
	return c.conn.WriteJSON(HubEnvelope{
		Type:      "im.gateway_claim",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload:   map[string]string{"platform": platform.String()},
	})
}

// SendIMGatewayUnclaim sends im.gateway_unclaim to Hub to release this machine's
// gateway ownership for the given IM platform. Called when the user disables
// an IM plugin so Hub stops routing messages to this client.
func (c *RemoteHubClient) SendIMGatewayUnclaim(platform imGatewayPlatformKind) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}
	return c.conn.WriteJSON(HubEnvelope{
		Type:      "im.gateway_unclaim",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload:   map[string]string{"platform": platform.String()},
	})
}

// SendIMGatewayMessage sends im.gateway_message to Hub, forwarding an incoming
// IM message from a client-side gateway (QQ Bot, Telegram) for processing
// through the Hub's IM Adapter pipeline.
func (c *RemoteHubClient) SendIMGatewayMessage(platform imGatewayPlatformKind, data map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		if platform == "weixin" {
			weixin.GetWxLog().Log("hubClient.send", "OUT", "-", "ERR not connected, dropping gateway_message")
		}
		return fmt.Errorf("hub not connected")
	}
	err := c.conn.WriteJSON(HubEnvelope{
		Type:      "im.gateway_message",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]any{
			"platform": platform.String(),
			"data":     data,
		},
	})
	if platform == "weixin" {
		wl := weixin.GetWxLog()
		uid, _ := data["platform_uid"].(string)
		if err != nil {
			wl.Log("hubClient.send", "OUT", uid, "ERR WriteJSON: %v", err)
		} else {
			wl.Log("hubClient.send", "OUT", uid, "OK im.gateway_message sent to hub")
		}
	}
	return err
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[hub-client] heartbeatLoop panic recovered: %v", r)
			c.handleConnectionLoss(fmt.Errorf("heartbeatLoop panic: %v", r))
		}
	}()
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
		return time.Duration(corelib.DefaultRemoteHeartbeatSec) * time.Second
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[hub-client] reconnectLoop panic recovered: %v", r)
		}
	}()
	defer c.reconnecting.Store(false)

	backoff := 500 * time.Millisecond
	consecutiveAuthFailures := 0
	const maxAuthFailuresBeforeClear = 3 // require 3 consecutive definitive rejections before clearing

	// Cap total reconnect duration. After this, stop retrying and let the next
	// user-initiated action (message send, UI interaction) trigger a fresh connect.
	// This prevents an idle client from burning CPU/network forever on a down Hub.
	reconnectStart := time.Now()
	const maxReconnectDuration = 10 * time.Minute

	for c.allowReconnect.Load() {
		if c.IsConnected() {
			return
		}

		// Give up after maxReconnectDuration. The next user action will trigger
		// a fresh Connect() attempt. Credentials remain intact.
		if time.Since(reconnectStart) > maxReconnectDuration {
			log.Printf("[hub-client] reconnectLoop giving up after %s (credentials preserved, will retry on next action)", time.Since(reconnectStart))
			c.mu.Lock()
			c.lastError = "Hub reconnection timed out, will retry on next action"
			c.mu.Unlock()
			c.app.emitRemoteStateChanged()
			return
		}

		err := c.Connect()
		if err == nil {
			return
		}

		// Definitive auth rejection: Hub explicitly says credentials are invalid.
		// Require multiple consecutive rejections to rule out transient server issues
		// (e.g., Hub returning generic "error" during maintenance/deploy).
		// NEVER clear credentials on the first failure - this is irreversible.
		if errors.Is(err, errHubAuthFailed) {
			consecutiveAuthFailures++
			log.Printf("[hub-client] definitive auth rejection (%d/%d)", consecutiveAuthFailures, maxAuthFailuresBeforeClear)
			if consecutiveAuthFailures >= maxAuthFailuresBeforeClear {
				log.Printf("[hub-client] %d consecutive auth rejections - credentials permanently rejected by hub, clearing local credentials", maxAuthFailuresBeforeClear)
				c.app.clearMachineCredentials()
				c.app.emitEvent("hub-auth-rejected")
				return // stop reconnecting - user must manually re-register
			}
			// Wait longer before retrying auth to give server time to recover.
			// Use chunked sleep for early exit responsiveness.
			for waited := time.Duration(0); waited < 5*time.Second && c.allowReconnect.Load(); waited += 500 * time.Millisecond {
				time.Sleep(500 * time.Millisecond)
			}
			continue
		}

		// Transient auth error: Hub returned error but NOT a definitive rejection.
		// Do NOT clear credentials. Just retry with normal backoff.
		if errors.Is(err, errHubTransientAuthError) {
			log.Printf("[hub-client] transient auth error during reconnect, will retry (not clearing credentials)")
			// Reset auth failure counter - transient errors break the streak
			consecutiveAuthFailures = 0
		} else {
			// Network/dial/other errors - also reset auth failure counter
			consecutiveAuthFailures = 0
		}

		// Sleep with early-exit check: break the backoff into 500ms chunks so
		// we can respond quickly when allowReconnect is set to false (e.g.,
		// user clicks "Clear" / ClearRemoteActivation).
		remaining := backoff
		for remaining > 0 && c.allowReconnect.Load() {
			chunk := 500 * time.Millisecond
			if chunk > remaining {
				chunk = remaining
			}
			time.Sleep(chunk)
			remaining -= chunk
		}

		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
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
