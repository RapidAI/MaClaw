package main

import (
	"context"
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
	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tts"
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

	mu               sync.Mutex
	conn             *websocket.Conn
	hubURL           string
	machineID        string
	machineToken     string
	legacyMachineIDs []string
	connected        bool
	lastError        string
	dial             func(urlStr string) (*websocket.Conn, error)
	reconnectCh      chan struct{}
	reconnecting     atomic.Bool
	allowReconnect   atomic.Bool

	// Completion notices from IM /startmenu must survive a transient Hub
	// disconnect. Keep a small in-memory FIFO and flush it after reconnect.
	// This is deliberately memory-only: a desktop restart cannot know whether a
	// previous WebSocket write reached Hub, so persisting would risk duplicate
	// IM completion messages.
	proactiveMu       sync.Mutex
	pendingProactive  []pendingIMProactiveMessage
	flushingProactive atomic.Bool

	// Deferred hardware speech is keyed by the concrete client/conversation.
	// A newer result cancels synthesis for the older turn before it can enqueue
	// stale audio behind the new result page.
	hubSpeechMu    sync.Mutex
	hubSpeechTurns map[string]*hubDeviceSpeechTurn

	// reconnectImmediate is set to true when the Hub sends a close(1001)
	// "going away" frame, indicating a planned shutdown (e.g., redeployment).
	// The reconnect loop uses a minimal initial backoff (100ms) instead of
	// the normal 500ms exponential backoff, enabling sub-second reconnection
	// to the new Hub instance.
	reconnectImmediate atomic.Bool

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
	// Every paired ESP32 receives an isolated Agent runtime. The desktop and
	// non-hardware IM channels continue to use imHandler above.
	hardwareAgents *hardwareAgentRuntimeRegistry

	// Digital employee discussion handler for pushed Hub discussion messages.
	veHandlerMu            sync.Mutex
	veHandler              *VEMessageHandler
	groupDispatcher        *GroupChatDispatcher
	veDetailRefresh        sync.Map // sessionID -> *veDetailRefreshState
	mobileTaskActive       atomic.Bool
	mobileBackendSSHOutput sync.Map // mobile sessionID -> last reported SSH preview
	mobileBackendSSHTasks  sync.Map // mobile taskID -> corelib SSH background taskID

	// cachedHeartbeatSec avoids LoadConfig() on every heartbeat tick.
	// Updated when config is loaded for interval computation or after connect.
	cachedHeartbeatSec atomic.Int64

	// Adaptive poll state for mobile digital-employee / backend task loops.
	mobileTaskPollState *mobileTaskPollState

	// requestWaiters turns selected WebSocket writes into Hub-confirmed
	// operations. Physical hardware playback has a separate waiter because the
	// Hub acceptance ACK arrives before the ESP32 playback receipt.
	requestWaiters    sync.Map // request ID -> chan error
	playbackWaiters   sync.Map // request ID -> chan error
	deviceListWaiters sync.Map // request ID -> chan device list result

	// IO relay for multi-device session roaming cleanup on disconnect.
	ioRelay *SessionIORelay
}

type pendingIMProactiveMessage struct {
	text        string
	platform    string
	platformUID string
}

type hubDeviceSpeechTurn struct {
	ctx      context.Context
	cancel   context.CancelFunc
	replyTo  string
	parts    []string
	expected int
	queued   int
	started  bool
}

type HardwareDeviceBinding struct {
	ClientID        string    `json:"clientId"`
	ClientName      string    `json:"clientName,omitempty"`
	ProtocolVersion string    `json:"protocolVersion,omitempty"`
	PairedAt        time.Time `json:"pairedAt,omitempty"`
	LastSeenAt      time.Time `json:"lastSeenAt,omitempty"`
	Online          bool      `json:"online"`
	LastAckStatus   string    `json:"lastAckStatus,omitempty"`
	Volume          *int      `json:"volume,omitempty"`
	PetSkin         string    `json:"petSkin,omitempty"`
}

type hardwareDeviceListResult struct {
	Devices    []HardwareDeviceBinding
	MaxDevices int
	BoundCount int
	Err        error
}

// HardwareDeviceBindings describes the Hub-owned hardware bindings and their
// fixed Hub-owned capacity (five devices per GUI).
type HardwareDeviceBindings struct {
	// Keep these names stable at the Wails boundary. The settings UI consumes
	// camel-case JSON keys; leaving this struct untagged made encoding/json
	// expose Go's exported names ("Devices" / "BoundCount"), which the UI
	// interpreted as an empty list and zero bindings.
	Devices    []HardwareDeviceBinding `json:"devices"`
	MaxDevices int                     `json:"maxDevices"`
	BoundCount int                     `json:"boundCount"`
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

const maxPendingIMProactiveMessages = 100

var errIMProactiveHubDisconnected = errors.New("hub is not connected")

func NewRemoteHubClient(app *App, manager *RemoteSessionManager) *RemoteHubClient {
	return &RemoteHubClient{
		app:              app,
		manager:          manager,
		dial:             defaultHubDial,
		reconnectCh:      make(chan struct{}, 1),
		previewPending:   make(map[string]*pendingPreviewDelta),
		previewStopCh:    make(chan struct{}),
		lastSummary:      make(map[string]string),
		pendingProactive: make([]pendingIMProactiveMessage, 0),
		hubSpeechTurns:   make(map[string]*hubDeviceSpeechTurn),
	}
}

func (c *RemoteHubClient) ensureIMHandler() *IMMessageHandler {
	c.imHandlerMu.Lock()
	defer c.imHandlerMu.Unlock()
	if c.imHandler != nil {
		return c.imHandler
	}
	// The desktop and Hub transports must share the same handler when one is
	// already running. Constructing a second handler used to overwrite
	// app.imHandler inside NewIMMessageHandler, splitting loop/interrupt state
	// between two instances and making a new Hub message appear unrelated to the
	// active desktop task.
	var h *IMMessageHandler
	if c.app != nil {
		h = c.app.imHandler
	}
	if h == nil {
		h = NewIMMessageHandler(c.app, c.manager)
		if c.app != nil {
			c.app.imHandler = h
		}
	}
	c.imHandler = h
	if c.configureIMHandler != nil {
		c.configureIMHandler(h)
	}
	h.clientToolDispatcher = c.dispatchHubClientTool
	return h
}

func (c *RemoteHubClient) hardwareAgentHandler(clientID string) (*IMMessageHandler, error) {
	if c == nil {
		return nil, fmt.Errorf("hardware agent runtime is not configured")
	}
	c.imHandlerMu.Lock()
	if c.hardwareAgents == nil {
		var configure func(*IMMessageHandler)
		if c.app != nil {
			configure = c.app.configureHardwareAgent
		}
		c.hardwareAgents = newHardwareAgentRuntimeRegistry(c.app, c.manager, configure)
	}
	runtimes := c.hardwareAgents
	c.imHandlerMu.Unlock()
	handler, err := runtimes.handler(clientID)
	if err == nil && handler != nil {
		// The registry owns the isolated runtime but not the Hub transport.
		// Bind this handler to the same client-tool relay as the primary Hub
		// handler, so a device-local tool call stays addressed to its own client.
		handler.clientToolDispatcher = c.dispatchHubClientTool
	}
	return handler, err
}

func (c *RemoteHubClient) removeHardwareAgent(clientID string) {
	if c == nil {
		return
	}
	c.imHandlerMu.Lock()
	runtimes := c.hardwareAgents
	c.imHandlerMu.Unlock()
	if runtimes != nil {
		runtimes.remove(clientID)
	}
}

func (c *RemoteHubClient) stopHardwareAgents() {
	if c == nil {
		return
	}
	c.imHandlerMu.Lock()
	runtimes := c.hardwareAgents
	c.hardwareAgents = nil
	c.imHandlerMu.Unlock()
	if runtimes != nil {
		runtimes.stopAll()
	}
}

func (c *RemoteHubClient) dispatchHubClientTool(_ context.Context, target agent.ClientToolContext, definition agent.ClientToolDefinition, callID string, arguments map[string]any) error {
	if callID == "" {
		callID = "ct_" + randomHexID(12)
	}
	call := &coreim.ThirdPartyToolCall{
		ID: callID, Name: definition.Name, Arguments: arguments, Risk: definition.Risk,
		RequiresApproval: definition.RequiresApproval, TimeoutMs: definition.TimeoutMs,
		IdempotencyKey: callID, Metadata: definition.Metadata,
	}
	if err := coreim.NormalizeThirdPartyToolCall(call); err != nil {
		return err
	}
	return c.SendDeviceGatewayToolMessage(target.ClientID, target.ConversationID, map[string]any{
		"reply_type": "tool_call", "type": "tool_call", "toolCall": call,
		"replyTo": target.ReplyToMessageID, "replyToMessageId": target.ReplyToMessageID,
	})
}

// currentIMHandler returns the Hub handler safely while it may be created by
// an incoming message concurrently with embedding activation.
func (c *RemoteHubClient) currentIMHandler() *IMMessageHandler {
	if c == nil {
		return nil
	}
	c.imHandlerMu.Lock()
	defer c.imHandlerMu.Unlock()
	return c.imHandler
}

func (c *RemoteHubClient) hardwareAgentHandlers() []*IMMessageHandler {
	if c == nil {
		return nil
	}
	c.imHandlerMu.Lock()
	runtimes := c.hardwareAgents
	c.imHandlerMu.Unlock()
	return runtimes.handlers()
}

// existingHardwareAgentHandler selects a previously-created hardware runtime
// without constructing one. Control messages must stay within an existing
// hardware boundary and must not revive an Agent after its device was unbound.
func (c *RemoteHubClient) existingHardwareAgentHandler(clientID string) *IMMessageHandler {
	if c == nil {
		return nil
	}
	c.imHandlerMu.Lock()
	runtimes := c.hardwareAgents
	c.imHandlerMu.Unlock()
	return runtimes.existingHandler(clientID)
}

func (c *RemoteHubClient) isActiveHardwareAgentHandler(clientID string, handler *IMMessageHandler) bool {
	if c == nil {
		return false
	}
	c.imHandlerMu.Lock()
	runtimes := c.hardwareAgents
	c.imHandlerMu.Unlock()
	return runtimes.isActiveHandler(clientID, handler)
}

func thirdPartyClientIDFromSessionUserID(userID string) string {
	parts := strings.SplitN(strings.TrimSpace(userID), ":", 3)
	if len(parts) != 3 || !strings.EqualFold(strings.TrimSpace(parts[0]), "thirdparty") {
		return ""
	}
	return normalizeThirdPartyID(parts[1])
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
	c.legacyMachineIDs = legacyHardwareMachineIDs(cfg)

	if c.hubURL == "" {
		return fmt.Errorf("remote hub url is empty")
	}
	if c.machineID == "" || c.machineToken == "" {
		return fmt.Errorf("remote machine identity is incomplete")
	}
	return nil
}

// legacyHardwareMachineIDs identifies only the pre-machine credential that
// this same desktop installation used before durable machine IDs were
// introduced. It must never expand to all machines owned by a user: those can
// be separate live GUIs with their own isolated hardware bindings.
func legacyHardwareMachineIDs(cfg corelib.AppConfig) []string {
	current := strings.TrimSpace(cfg.RemoteMachineID)
	legacy := strings.TrimSpace(cfg.RemoteClientID)
	if legacy == "" || legacy == current {
		return nil
	}
	return []string{legacy}
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
	go c.mobileDigitalEmployeeTaskLoop()
	if c.app.shouldAutoSyncHubManagedCapabilitiesOnConnect() {
		c.app.TriggerHubManagedCapabilitySync("hub-connect")
	}
	c.startPreviewFlusher()

	// Re-send IM gateway claims for any already-connected gateways that are
	// in hub mode. This covers both initial connect and reconnect scenarios.
	go c.syncIMGatewayClaims()
	go c.flushPendingIMProactiveMessages()

	// Pull unread notifications on (re)connect so the client syncs any
	// notifications that arrived while offline.
	go c.app.PullUnreadNotifications()
	go c.app.refreshDeviceAmbientWeatherOnce()
	go c.syncDeviceGatewayPetProfile()
	go c.syncDeviceGatewayHardwareState()

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
		"machine not registered", "device not registered",
	}
	for _, phrase := range machineUserNotFound {
		if strings.Contains(msg, phrase) || strings.Contains(reason, phrase) {
			return true
		}
	}
	if (strings.Contains(msg, "machine ") || strings.Contains(reason, "machine ")) &&
		(strings.Contains(msg, "not registered") || strings.Contains(reason, "not registered")) {
		return true
	}

	if isGenericRemoteHubRouteError(msg) || isGenericRemoteHubRouteError(reason) {
		return false
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
	if c.app.shouldAutoSyncHubManagedCapabilitiesOnConnect() {
		c.app.TriggerHubManagedCapabilitySync("hub-connect")
	}
	c.startPreviewFlusher()
	go c.syncIMGatewayClaims()
	go c.flushPendingIMProactiveMessages()

	// Pull unread notifications on (re)connect so the client syncs any
	// notifications that arrived while offline.
	go c.app.PullUnreadNotifications()
	go c.app.refreshDeviceAmbientWeatherOnce()
	go c.syncDeviceGatewayPetProfile()
	go c.syncDeviceGatewayHardwareState()

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
	// RefreshAllAsync skips fresh cached versions and only runs safe --version
	// probes (never bare "version", which would start a full Claude Code session).
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
	// Prefer cached heartbeat interval so we don't re-read config from disk
	// on every beat (can be every 5–30s for the machine lifetime).
	heartbeatSec := int(c.cachedHeartbeatSec.Load())
	if heartbeatSec <= 0 {
		cfg, _ := c.app.LoadConfig()
		heartbeatSec = normalizeRemoteHeartbeatIntervalSec(cfg.RemoteHeartbeatSec)
		c.cachedHeartbeatSec.Store(int64(heartbeatSec))
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return nil
	}

	activeSessions := len(sessions)
	profile := c.app.currentRemoteMachineProfile(heartbeatSec, activeSessions)
	adaptivePrompt := agent.AdaptivePromptHeartbeatStat()
	costOps := llm.CostOpsHeartbeatStat()

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
			"adaptive_prompt":        adaptivePrompt,
			"cost_ops":               costOps,
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
			// Detect planned Hub shutdown: close(1001, "going away").
			// Set reconnectImmediate so reconnectLoop uses minimal backoff.
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseServiceRestart) {
				log.Printf("[hub-client] readLoop: Hub sent planned close (1001/1012), enabling fast reconnect")
				c.reconnectImmediate.Store(true)
			}
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
			err := hubRequestError(msg.Payload)
			c.completeHubRequest(msg.RequestID, err)
			c.completeHubPlaybackRequest(msg.RequestID, err)
			c.completeHardwareDeviceList(msg.RequestID, nil, 0, 0, err)
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
		case hubInboundMessageNotificationPush:
			go c.app.handleNotificationPush(msg.Payload)
		case hubInboundMessageMobileDigitalEmployeeTask:
			// Hub push: claim immediately instead of waiting for the poll timer.
			go c.handleMobileDigitalEmployeeTaskPush(msg)
		case hubInboundMessageVEEvent:
			c.handleVEEvent(msg)
		case hubInboundMessageAck:
			c.completeHubRequest(msg.RequestID, nil)
			c.handleAck(msg)
		case hubInboundMessageDeviceGatewayPlaybackReceipt:
			c.handleDeviceGatewayPlaybackReceipt(msg)
		case hubInboundMessageDeviceGatewayDevices:
			c.handleDeviceGatewayDevices(msg)
		}
	}
}

func (c *RemoteHubClient) completeHubRequest(requestID string, err error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	waiter, ok := c.requestWaiters.LoadAndDelete(requestID)
	if !ok {
		return
	}
	ch, ok := waiter.(chan error)
	if !ok {
		log.Printf("[hub-client] invalid request waiter type for %s", requestID)
		return
	}
	select {
	case ch <- err:
	default:
	}
}

func hubRequestError(payload json.RawMessage) error {
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(payload, &body) == nil {
		message := strings.TrimSpace(body.Message)
		code := strings.TrimSpace(body.Code)
		if message != "" {
			if code != "" {
				return fmt.Errorf("Hub rejected request (%s): %s", code, message)
			}
			return fmt.Errorf("Hub rejected request: %s", message)
		}
	}
	return fmt.Errorf("Hub rejected request: %s", truncateLogPayload(payload))
}

func (c *RemoteHubClient) failHubRequests(err error) {
	if err == nil {
		err = fmt.Errorf("Hub connection closed before request confirmation")
	}
	c.requestWaiters.Range(func(key, _ any) bool {
		requestID, _ := key.(string)
		c.completeHubRequest(requestID, err)
		return true
	})
}

func (c *RemoteHubClient) completeHubPlaybackRequest(requestID string, err error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	waiter, ok := c.playbackWaiters.LoadAndDelete(requestID)
	if !ok {
		return
	}
	ch, ok := waiter.(chan error)
	if !ok {
		log.Printf("[hub-client] invalid playback waiter type for %s", requestID)
		return
	}
	select {
	case ch <- err:
	default:
	}
}

func (c *RemoteHubClient) failHubPlaybackRequests(err error) {
	if err == nil {
		err = fmt.Errorf("Hub connection closed before ESP32 playback confirmation")
	}
	c.playbackWaiters.Range(func(key, value any) bool {
		requestID, _ := key.(string)
		c.completeHubPlaybackRequest(requestID, err)
		return true
	})
}

func (c *RemoteHubClient) failHardwareDeviceLists(err error) {
	c.deviceListWaiters.Range(func(key, _ any) bool {
		requestID, _ := key.(string)
		c.completeHardwareDeviceList(requestID, nil, 0, 0, err)
		return true
	})
}

func (c *RemoteHubClient) handleDeviceGatewayPlaybackReceipt(msg inboundHubEnvelope) {
	var payload struct {
		ClientID string `json:"clientId"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		c.completeHubPlaybackRequest(msg.RequestID, fmt.Errorf("invalid ESP32 playback receipt: %w", err))
		return
	}
	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if status == "delivered" || status == "read" {
		c.completeHubPlaybackRequest(msg.RequestID, nil)
		return
	}
	c.completeHubPlaybackRequest(msg.RequestID, fmt.Errorf("ESP32 reported audio playback failed (device %s)", strings.TrimSpace(payload.ClientID)))
}

func (c *RemoteHubClient) handleDeviceGatewayDevices(msg inboundHubEnvelope) {
	var payload struct {
		Devices    []HardwareDeviceBinding `json:"devices"`
		MaxDevices int                     `json:"maxDevices"`
		BoundCount int                     `json:"boundCount"`
	}
	err := json.Unmarshal(msg.Payload, &payload)
	c.completeHardwareDeviceList(msg.RequestID, payload.Devices, payload.MaxDevices, payload.BoundCount, err)
}

func (c *RemoteHubClient) completeHardwareDeviceList(requestID string, devices []HardwareDeviceBinding, maxDevices, boundCount int, err error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	waiter, ok := c.deviceListWaiters.LoadAndDelete(requestID)
	if !ok {
		return
	}
	ch, ok := waiter.(chan hardwareDeviceListResult)
	if !ok {
		log.Printf("[hub-client] invalid hardware list waiter type for %s", requestID)
		return
	}
	select {
	case ch <- hardwareDeviceListResult{Devices: devices, MaxDevices: maxDevices, BoundCount: boundCount, Err: err}:
	default:
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
	c.rememberHubThirdPartyClientCapabilities(payload)
	// Preserve the Hub transport family before audit normalization expands it to
	// a device-qualified platform name ("thirdparty:<client>"). Runtime
	// selection must use the transport family, not that audit-only identity.
	isThirdPartyHardware := normalizeIMGatewayPlatformKind(payload.Platform) == imGatewayPlatformThirdParty

	requestID := msg.RequestID
	payload.RequestID = requestID
	// New direct-Hub input supersedes any deferred synthesis from the preceding
	// answer on the same physical surface. Cancel at request receipt, not when
	// the next result finally arrives, so old audio cannot leak into processing.
	c.cancelHubDeviceSpeech(payload)
	go func() {
		// Hub gateways use the family platform name ("thirdparty") while the
		// platform UID retains the ESP32 client/conversation. Recover the local
		// provenance before auditing so Hub-mode history is grouped per device.
		payload.Platform, payload.UserID = normalizeHubThirdPartyAuditIdentity(payload.Platform, payload.UserID)
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
		// A Hub third-party delivery represents one concrete ESP32 binding.
		// Route only those deliveries to a device runtime; normal Hub IM channels
		// continue to use the desktop/Hub handler and retain their existing state.
		var handler *IMMessageHandler
		var handlerErr error
		if isThirdPartyHardware {
			clientID := ""
			if payload.ClientToolContext != nil {
				clientID = strings.TrimSpace(payload.ClientToolContext.ClientID)
			}
			if clientID == "" {
				handlerErr = fmt.Errorf("third-party hardware message is missing client ID")
			} else {
				handler, handlerErr = c.hardwareAgentHandler(clientID)
			}
		} else {
			handler = c.ensureIMHandler()
		}
		if handlerErr != nil {
			c.setLastError(handlerErr.Error())
			_ = c.sendIMAgentResponse(requestID, &IMAgentResponse{Error: handlerErr.Error()})
			return
		}
		if handler == nil {
			c.setLastError("im handler not initialized")
			return
		}
		if isThirdPartyHardware && !c.isActiveHardwareAgentHandler(payload.ClientToolContext.ClientID, handler) {
			// The binding was removed after this Hub message was accepted. Never
			// revive or execute an Agent for a revoked hardware identity.
			return
		}
		if payload.StartMenu != nil {
			if !c.app.hasWailsEventsContext() {
				resp := &IMAgentResponse{Error: "当前设备未打开 AI 助手界面，无法创建任务标签页。请打开桌面端 AI 助手后重试。"}
				if err := c.sendIMAgentResponse(requestID, resp); err != nil {
					c.setLastError(fmt.Sprintf("im.agent_response send error: %s", err.Error()))
				}
				return
			}
			if err := c.openStartMenuTask(payload); err != nil {
				resp := &IMAgentResponse{Error: "无法创建 AI 助手任务: " + err.Error()}
				if sendErr := c.sendIMAgentResponse(requestID, resp); sendErr != nil {
					c.setLastError(fmt.Sprintf("im.agent_response send error: %s", sendErr.Error()))
				}
				return
			}
			// The new AI assistant tab owns the first agent turn. Do not also run
			// this Hub-delivered request in the bare IM session, otherwise a single
			// /startmenu confirmation executes the task twice in two contexts.
			resp := &IMAgentResponse{Text: startMenuCreatedReply(payload.StartMenu)}
			if err := c.sendIMAgentResponse(requestID, resp); err != nil {
				c.setLastError(fmt.Sprintf("im.agent_response send error: %s", err.Error()))
			}
			return
		}

		// Interrupt check: if the agent loop is already running (session mutex held)
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
				// to HandleIMMessageWithProgress (which will block on the session mutex
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
		if isThirdPartyHardware && !c.isActiveHardwareAgentHandler(payload.ClientToolContext.ClientID, handler) {
			// The binding was removed while the Agent was running. Do not relay the
			// old turn to a later pairing that happens to reuse this client ID.
			c.cancelHubDeviceSpeech(payload)
			return
		}
		// Downsize large screenshots before sending over WebSocket to Hub.
		// Multi-monitor captures can be several MB; Hub WebSocket may timeout.
		if resp != nil && len(resp.ImageKey) > 500_000 {
			if ds, err := remote.DownsizeScreenshotBase64(resp.ImageKey, 400_000); err == nil {
				resp.ImageKey = ds
			}
		}
		c.prepareHubDeviceSpeech(payload, requestID, resp)
		if err := c.sendIMAgentResponse(requestID, resp); err != nil {
			c.setLastError(fmt.Sprintf("im.agent_response send error: %s", err.Error()))
			c.cancelHubDeviceSpeech(payload)
			return
		}
	}()
}

// prepareHubDeviceSpeech plans a direct-Hub ESP32 response without blocking on
// Kokoro or MP3 work. Synthesis is deliberately not started here: the Agent
// response still has to travel Hub -> GUI -> Hub before it reaches the device
// queue, while speech uses the direct GUI -> Hub path. Starting both here lets
// audio overtake the result surface. startHubDeviceSpeechAfterResult is called
// only after that terminal text frame has been written back to Hub.
func (c *RemoteHubClient) prepareHubDeviceSpeech(message IMUserMessage, requestID string, resp *IMAgentResponse) bool {
	if c == nil || c.app == nil || c.app.thirdPartyGateway == nil || resp == nil ||
		resp.Deferred || resp.Error != "" || len(resp.VoiceParts) > 0 || resp.VoiceData != "" ||
		message.ClientToolContext == nil || !isThirdPartyVoicePlatform(message.Platform) {
		return false
	}
	clientID := strings.TrimSpace(message.ClientToolContext.ClientID)
	conversationID := strings.TrimSpace(message.ClientToolContext.ConversationID)
	replyTo := strings.TrimSpace(message.ClientToolContext.ReplyToMessageID)
	if clientID == "" || strings.TrimSpace(resp.Text) == "" {
		return false
	}
	resp.Text = cleanDeviceReplyText(resp.Text)
	capabilities := c.app.thirdPartyGateway.clientCapabilities(clientID)
	// A returned MP3/WAV is already the result's audio representation. Hub will
	// promote it from FileData to the voice channel; synthesizing the text as
	// well would play two audio versions for one answer.
	if responseDeviceFileAudioCount(resp, capabilities) > 0 {
		return false
	}
	parts := tts.PrepareSpeechChunks(resp.Text, 0, deviceVoiceChunkRunes)
	if len(parts) == 0 || !clientCanSynthesizeDeviceSpeech(c.app.thirdPartyGateway, capabilities) {
		return false
	}
	if replyTo == "" {
		replyTo = strings.TrimSpace(requestID)
	}
	resp.PendingVoiceParts = len(parts)
	turnKey := clientID + "\x00" + conversationID
	ctx, cancel := context.WithCancel(context.Background())
	turn := &hubDeviceSpeechTurn{
		ctx: ctx, cancel: cancel, replyTo: replyTo,
		parts: append([]string(nil), parts...), expected: len(parts),
	}
	c.hubSpeechMu.Lock()
	if c.hubSpeechTurns == nil {
		c.hubSpeechTurns = make(map[string]*hubDeviceSpeechTurn)
	}
	if previous := c.hubSpeechTurns[turnKey]; previous != nil {
		previous.cancel()
	}
	c.hubSpeechTurns[turnKey] = turn
	c.hubSpeechMu.Unlock()
	return true
}

// startHubDeviceSpeechAfterResult is the ordering barrier for direct-Hub
// hardware replies. SendDeviceGatewayReply has already written the terminal
// text frame to the same GUI -> Hub WebSocket before this method is called, so
// every subsequently streamed voice frame is enqueued after the result page.
func (c *RemoteHubClient) startHubDeviceSpeechAfterResult(clientID, conversationID, replyTo string) {
	if c == nil {
		return
	}
	clientID = strings.TrimSpace(clientID)
	conversationID = strings.TrimSpace(conversationID)
	replyTo = strings.TrimSpace(replyTo)
	turnKey := clientID + "\x00" + conversationID
	c.hubSpeechMu.Lock()
	turn := c.hubSpeechTurns[turnKey]
	if turn == nil || turn.started || (replyTo != "" && turn.replyTo != "" && replyTo != turn.replyTo) {
		c.hubSpeechMu.Unlock()
		return
	}
	turn.started = true
	ctx := turn.ctx
	parts := append([]string(nil), turn.parts...)
	c.hubSpeechMu.Unlock()
	go c.streamHubDeviceSpeech(ctx, turnKey, clientID, conversationID, turn, parts)
}

func (c *RemoteHubClient) cancelHubDeviceSpeech(message IMUserMessage) {
	if c == nil || message.ClientToolContext == nil {
		return
	}
	turnKey := strings.TrimSpace(message.ClientToolContext.ClientID) + "\x00" + strings.TrimSpace(message.ClientToolContext.ConversationID)
	c.hubSpeechMu.Lock()
	if turn := c.hubSpeechTurns[turnKey]; turn != nil {
		turn.cancel()
		delete(c.hubSpeechTurns, turnKey)
	}
	c.hubSpeechMu.Unlock()
}

func (c *RemoteHubClient) cancelAllHubDeviceSpeech() {
	if c == nil {
		return
	}
	c.hubSpeechMu.Lock()
	turns := c.hubSpeechTurns
	c.hubSpeechTurns = make(map[string]*hubDeviceSpeechTurn)
	c.hubSpeechMu.Unlock()
	for _, turn := range turns {
		if turn != nil {
			turn.cancel()
		}
	}
}

func (c *RemoteHubClient) streamHubDeviceSpeech(ctx context.Context, turnKey, clientID, conversationID string, turn *hubDeviceSpeechTurn, parts []string) {
	defer func() {
		turn.cancel()
		c.hubSpeechMu.Lock()
		if c.hubSpeechTurns[turnKey] == turn {
			delete(c.hubSpeechTurns, turnKey)
		}
		c.hubSpeechMu.Unlock()
	}()
	started := time.Now()
	ok := streamPreparedDeviceVoicePayload(c.app.ttsManager, parts, thirdPartyPlatform(clientID), func(part IMVoicePart, index, total int) bool {
		if ctx.Err() != nil {
			return false
		}
		reply := GatewayReplyPayload{
			ReplyType: gatewayReplyTypeVoice, FileData: part.Data, FileName: part.FileName, MimeType: part.MimeType,
			SourceMessageID: turn.replyTo, VoicePartIndex: index, VoicePartTotal: total,
			Metadata: map[string]any{"speech_part": index, "speech_parts": total},
		}
		if err := c.SendDeviceGatewayReply(clientID, conversationID, reply); err != nil {
			log.Printf("[hub-client] deferred device speech send failed client=%s part=%d/%d: %v", clientID, index, total, err)
			return false
		}
		c.hubSpeechMu.Lock()
		if c.hubSpeechTurns[turnKey] == turn {
			turn.queued = index
		}
		c.hubSpeechMu.Unlock()
		return true
	})
	c.hubSpeechMu.Lock()
	queued := turn.queued
	c.hubSpeechMu.Unlock()
	if !ok && ctx.Err() == nil && queued < turn.expected {
		_ = c.sendDeviceSpeechEnd(clientID, conversationID, turn.replyTo, turn.expected, queued)
	}
	log.Printf("[hub-client] deferred device speech finished client=%s replyTo=%s parts=%d ok=%t elapsed=%s",
		clientID, turn.replyTo, len(parts), ok, time.Since(started).Round(time.Millisecond))
}

func (c *RemoteHubClient) sendDeviceSpeechEnd(clientID, conversationID, replyTo string, expected, sent int) error {
	return c.SendDeviceGatewayToolMessage(clientID, conversationID, map[string]any{
		"reply_type": "speech_end", "type": "speech_end", "replyTo": replyTo, "replyToMessageId": replyTo,
		"extra": map[string]any{"speech_parts_expected": expected, "speech_parts_sent": sent},
	})
}

// rememberHubThirdPartyClientCapabilities bridges the direct-Hub hardware
// route into the local gateway's delivery registry. The ESP handshakes with
// Hub, not this GUI, so without this copy the final gateway reply is treated as
// text-only locally and the complete TTS stream is skipped even though Hub
// forwarded the concrete device capabilities with im.user_message.
func (c *RemoteHubClient) rememberHubThirdPartyClientCapabilities(message IMUserMessage) {
	if c == nil || c.app == nil || c.app.thirdPartyGateway == nil ||
		message.ClientCapabilities == nil || message.ClientToolContext == nil ||
		!strings.EqualFold(strings.TrimSpace(message.Platform), "thirdparty") {
		return
	}
	clientID := strings.TrimSpace(message.ClientToolContext.ClientID)
	if clientID == "" {
		return
	}
	c.app.thirdPartyGateway.setClientCapabilities(clientID, message.ClientCapabilities)
	capabilities := c.app.thirdPartyGateway.clientCapabilities(clientID)
	audio := capabilities.Output.Audio
	log.Printf("[hub-client] remembered thirdparty device capabilities client=%s audio=%t playback=%t mp3=%t replyTo=%s",
		clientID, audio != nil, audio != nil && audio.Playback,
		capabilities.SupportsOutputMIME("audio", "audio/mpeg"),
		strings.TrimSpace(message.ClientToolContext.ReplyToMessageID))
}

func normalizeHubThirdPartyAuditIdentity(platform, userID string) (string, string) {
	platform = strings.TrimSpace(platform)
	userID = strings.TrimSpace(userID)
	if !strings.EqualFold(platform, imAuditPlatformThirdParty.String()) || !strings.HasPrefix(strings.ToLower(userID), imAuditPlatformThirdParty.String()+":") {
		return platform, userID
	}
	parts := strings.SplitN(userID, ":", 3)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return platform, userID
	}
	return normalizeIMAuditPlatform(thirdPartyPlatform(parts[1])), userID
}

func startMenuCreatedReply(launch *agent.StartMenuLaunch) string {
	return startMenuTaskCreatedReply(launch != nil && strings.TrimSpace(launch.AgentMode) == "remote_coding_dev")
}

func (c *RemoteHubClient) openStartMenuTask(message IMUserMessage) error {
	if c == nil || c.app == nil || message.StartMenu == nil {
		return nil
	}
	launch := message.StartMenu
	title := strings.TrimSpace(launch.Title)
	if title == "" {
		title = strings.TrimSpace(message.Text)
	}
	if title == "" {
		return fmt.Errorf("任务标题为空")
	}
	mode := strings.TrimSpace(launch.AgentMode)
	var task ProjectSearchResult
	switch mode {
	case "remote_coding_dev":
		if strings.TrimSpace(launch.RemoteHost) == "" || strings.TrimSpace(launch.RemoteUser) == "" || strings.TrimSpace(launch.RemoteDir) == "" {
			return fmt.Errorf("远程开发环境信息不完整")
		}
		port := launch.RemotePort
		if port <= 0 || port > 65535 {
			port = 22
		}
		task = c.app.CreateRemoteCodingTask(title, launch.RemoteHost, launch.RemoteUser, launch.RemoteDir, port)
	case "coding_dev":
		task = c.app.CreateTaskWithMode(title, launch.WorkingDir, mode)
		if task.ProjectPath != "" {
			if err := c.app.PrepareLocalCodingEnvironment(task.ProjectPath, launch.WorkingDir); err != nil {
				return err
			}
		}
	default:
		task = c.app.CreateTask(title, "")
	}
	if strings.TrimSpace(task.ProjectPath) == "" {
		return fmt.Errorf("创建任务失败")
	}
	remoteNeedsReconnect := mode == "remote_coding_dev"
	initialMessage := strings.TrimSpace(launch.TaskText)
	if initialMessage == "" {
		initialMessage = strings.TrimSpace(message.Text)
	}
	c.app.emitEvent("im-startmenu-task-created", map[string]interface{}{
		"project_path":    task.ProjectPath,
		"task_title":      task.Name,
		"initial_message": initialMessage,
		// The frontend holds remote prompts until SSH reconnect succeeds, then
		// sends them automatically. Keep this true for parity with local IM
		// gateways and the user-facing automatic-start message.
		"auto_send":              true,
		"prepare_mode":           "new-agent",
		"agent_mode":             mode,
		"remote_host":            launch.RemoteHost,
		"remote_needs_reconnect": remoteNeedsReconnect,
		"im_platform":            launch.Platform,
		"im_target_uid":          launch.TargetUID,
	})
	return nil
}

// handleIMCancelSession handles im.cancel_session from Hub - cancels the
// currently running agent loop so the user can start a new task.
func (c *RemoteHubClient) handleIMCancelSession(msg inboundHubEnvelope) {
	log.Printf("[hub-client] im.cancel_session received")
	var payload struct {
		UserID         string `json:"user_id"`
		ClientID       string `json:"clientId"`
		ClientIDLegacy string `json:"client_id"`
		ConversationID string `json:"conversationId"`
	}
	if len(msg.Payload) == 0 || json.Unmarshal(msg.Payload, &payload) != nil {
		log.Printf("[hub-client] im.cancel_session ignored: missing or invalid payload")
		return
	}
	userID := strings.TrimSpace(payload.UserID)
	clientID := normalizeThirdPartyID(payload.ClientID)
	if clientID == "" {
		clientID = normalizeThirdPartyID(payload.ClientIDLegacy)
	}
	if clientID == "" {
		clientID = thirdPartyClientIDFromSessionUserID(userID)
	}
	if clientID != "" {
		if userID == "" {
			userID = thirdPartySessionUserID(clientID, payload.ConversationID)
		}
		handler := c.existingHardwareAgentHandler(clientID)
		if handler == nil {
			log.Printf("[hub-client] im.cancel_session ignored: hardware runtime not found client=%s", clientID)
			return
		}
		_, _ = handler.CancelSessionForUser(userID)
		return
	}
	if userID == "" {
		log.Printf("[hub-client] im.cancel_session ignored: missing user_id payload")
		return
	}
	if handler := c.currentIMHandler(); handler != nil {
		_, _ = handler.CancelSessionForUser(userID)
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
			ChatType:    reply.ChatType,
			Extra:       reply.Extra,
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
			ReplyType:       reply.ReplyType,
			PlatformUID:     reply.PlatformUID,
			Text:            reply.Text,
			ImageData:       reply.ImageData,
			Caption:         reply.Caption,
			FileData:        reply.FileData,
			FileName:        reply.FileName,
			MimeType:        reply.MimeType,
			ChatType:        reply.ChatType,
			SourceMessageID: reply.SourceMessageID,
			SenderID:        reply.SenderID,
			Extra:           reply.Extra,
		})
	case imGatewayPlatformThirdParty:
		if c.app.thirdPartyGateway == nil {
			c.app.log("[hub-client] im.gateway_reply: thirdPartyGateway is nil, ignoring")
			return
		}
		c.app.thirdPartyGateway.HandleGatewayReply(GatewayReplyPayload{
			ReplyType:       reply.ReplyType,
			PlatformUID:     reply.PlatformUID,
			Text:            reply.Text,
			ImageData:       reply.ImageData,
			Caption:         reply.Caption,
			FileData:        reply.FileData,
			FileName:        reply.FileName,
			MimeType:        reply.MimeType,
			SourceMessageID: reply.SourceMessageID,
			Progress:        reply.Progress,
			Final:           reply.Final,
			Complete:        reply.Complete,
			Metadata:        reply.Metadata,
			VoicePartIndex:  reply.VoicePartIndex,
			VoicePartTotal:  reply.VoicePartTotal,
			VoicePartFinal:  reply.VoicePartFinal,
			Extra:           reply.Extra,
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
	if resp != nil {
		log.Printf("[hub-client] im.agent_response sending request=%s text_runes=%d voice=%t voice_parts=%d error=%t deferred=%t",
			requestID, len([]rune(resp.Text)), resp.VoiceData != "", len(resp.VoiceParts), resp.Error != "", resp.Deferred)
	}
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
	return c.SendIMProactiveMessageToTarget(text, "", "")
}

// SendIMProactiveMessageToTarget delivers a completion summary to the exact
// IM conversation that initiated a /startmenu task.
func (c *RemoteHubClient) SendIMProactiveMessageToTarget(text, platform, platformUID string) error {
	pending := pendingIMProactiveMessage{
		text:        strings.TrimSpace(text),
		platform:    strings.TrimSpace(platform),
		platformUID: strings.TrimSpace(platformUID),
	}
	if pending.text == "" {
		return nil
	}
	// Serialize direct writes with the reconnect FIFO so completion notices are
	// delivered in creation order even when a reconnect races a new completion.
	c.proactiveMu.Lock()
	if len(c.pendingProactive) > 0 {
		c.enqueueIMProactiveMessageLocked(pending)
		c.proactiveMu.Unlock()
		go c.flushPendingIMProactiveMessages()
		return nil
	}
	err := c.writeIMProactiveMessage(pending)
	if err == nil {
		c.proactiveMu.Unlock()
		return nil
	}
	c.enqueueIMProactiveMessageLocked(pending)
	c.proactiveMu.Unlock()
	if !errors.Is(err, errIMProactiveHubDisconnected) {
		go c.handleConnectionLoss(err)
	}
	return nil
}

func (c *RemoteHubClient) writeIMProactiveMessage(pending pendingIMProactiveMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return errIMProactiveHubDisconnected
	}
	msg := HubEnvelope{
		Type:      "im.proactive_message",
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]string{
			"text":         pending.text,
			"platform":     pending.platform,
			"platform_uid": pending.platformUID,
		},
	}
	if err := c.conn.WriteJSON(msg); err != nil {
		log.Printf("[hub-client] IM proactive message write failed; queueing for reconnect: %v", err)
		return err
	}
	return nil
}

func (c *RemoteHubClient) enqueueIMProactiveMessageLocked(pending pendingIMProactiveMessage) {
	if len(c.pendingProactive) >= maxPendingIMProactiveMessages {
		c.pendingProactive = c.pendingProactive[1:]
		log.Printf("[hub-client] IM proactive queue full; dropped oldest completion notice")
	}
	c.pendingProactive = append(c.pendingProactive, pending)
}

func (c *RemoteHubClient) flushPendingIMProactiveMessages() {
	if c.flushingProactive.Swap(true) {
		return
	}
	defer func() {
		c.flushingProactive.Store(false)
		// A connect can race this flusher's final disconnected write attempt:
		// Connect's own flush sees flushingProactive=true and returns, then this
		// goroutine exits. Re-check after releasing the flag so that race cannot
		// leave a completion notice stranded until another reconnect.
		c.proactiveMu.Lock()
		hasPending := len(c.pendingProactive) > 0
		c.proactiveMu.Unlock()
		if hasPending && c.IsConnected() {
			go c.flushPendingIMProactiveMessages()
		}
	}()
	for {
		c.proactiveMu.Lock()
		if len(c.pendingProactive) == 0 {
			c.proactiveMu.Unlock()
			return
		}
		pending := c.pendingProactive[0]
		if err := c.writeIMProactiveMessage(pending); err != nil {
			c.proactiveMu.Unlock()
			if !errors.Is(err, errIMProactiveHubDisconnected) {
				go c.handleConnectionLoss(err)
			}
			return
		}
		c.pendingProactive = c.pendingProactive[1:]
		c.proactiveMu.Unlock()
	}
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
	return c.SendIMProactiveFileRequest(agent.IMFileDeliveryRequest{
		Data: b64Data, FileName: fileName, MIMEType: mimeType, Message: message,
	})
}

// SendIMProactiveFileRequest sends a file plus optional exact IM target.
func (c *RemoteHubClient) SendIMProactiveFileRequest(req agent.IMFileDeliveryRequest) error {
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
			"file_data": req.Data,
			"file_name": req.FileName,
			"mime_type": req.MIMEType,
			"message":   req.Message,
		},
	}
	if target := req.Target.Normalize(); target.Active() {
		msg.Payload.(map[string]interface{})["target"] = target
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

func (c *RemoteHubClient) SendDeviceGatewayPairing(pairCode string) error {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return fmt.Errorf("Hub not connected")
	}
	profile := c.app.devicePetProfileForConfig(cfg)
	return c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_pairing", TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{"pairCode": pairCode, "petSkin": profile["skin"], "motionEnabled": profile["motionEnabled"], "petAsset": profile["asset"]}})
}

func (c *RemoteHubClient) ListHardwareDeviceBindings() (HardwareDeviceBindings, error) {
	requestID := "device-list-" + randomHexID(12)
	waiter := make(chan hardwareDeviceListResult, 1)
	c.deviceListWaiters.Store(requestID, waiter)
	defer c.deviceListWaiters.Delete(requestID)
	c.mu.Lock()
	if !c.connected || c.conn == nil {
		c.mu.Unlock()
		return HardwareDeviceBindings{}, fmt.Errorf("Hub not connected")
	}
	legacyMachineIDs := append([]string(nil), c.legacyMachineIDs...)
	err := c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_devices_list", RequestID: requestID, TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{"legacyMachineIds": legacyMachineIDs}})
	c.mu.Unlock()
	if err != nil {
		return HardwareDeviceBindings{}, err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case result := <-waiter:
		if result.Err != nil {
			return HardwareDeviceBindings{}, result.Err
		}
		return HardwareDeviceBindings{Devices: result.Devices, MaxDevices: result.MaxDevices, BoundCount: result.BoundCount}, nil
	case <-timer.C:
		return HardwareDeviceBindings{}, fmt.Errorf("timed out waiting for Hub hardware list")
	}
}

func (c *RemoteHubClient) ListHardwareDevices() ([]HardwareDeviceBinding, error) {
	bindings, err := c.ListHardwareDeviceBindings()
	return bindings.Devices, err
}

func (c *RemoteHubClient) DeleteHardwareDevice(clientID string) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("hardware client ID is required")
	}
	requestID := "device-delete-" + randomHexID(12)
	waiter := make(chan error, 1)
	c.requestWaiters.Store(requestID, waiter)
	defer c.requestWaiters.Delete(requestID)
	c.mu.Lock()
	if !c.connected || c.conn == nil {
		c.mu.Unlock()
		return fmt.Errorf("Hub not connected")
	}
	err := c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_device_delete", RequestID: requestID, TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{"clientId": clientID}})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-waiter:
		if err == nil {
			c.removeHardwareAgent(clientID)
		}
		return err
	case <-timer.C:
		return fmt.Errorf("timed out waiting for Hub hardware deletion")
	}
}

func (c *RemoteHubClient) SendDeviceGatewayReply(clientID, conversationID string, reply GatewayReplyPayload) error {
	// Pet profile synchronization has its own settings event and reconnect path.
	// Rendering and attaching RGB565 frames to every assistant reply wasted CPU
	// and added roughly 90 KiB to otherwise small text messages.
	payload := map[string]any{"reply_type": reply.ReplyType.String(), "text": reply.Text, "caption": reply.Caption, "image_data": reply.ImageData, "file_data": reply.FileData, "file_name": reply.FileName, "mime_type": reply.MimeType, "extra": reply.Extra}
	metadata := make(map[string]any, len(reply.Metadata)+1)
	for key, value := range reply.Metadata {
		metadata[key] = value
	}
	if reply.VoicePartIndex > 0 {
		payload["voice_part_index"] = reply.VoicePartIndex
	}
	if reply.VoicePartTotal > 0 {
		payload["voice_part_total"] = reply.VoicePartTotal
	}
	if reply.VoicePartFinal {
		payload["voice_part_final"] = true
	}
	// Terminal classification must survive the GUI -> Hub relay.  Older Hub
	// producers sometimes left progress=true on their final envelope; making the
	// terminal turn explicit here lets constrained clients prefer completion and
	// prevents a finished answer from remaining behind a processing surface.
	if normalizeThirdPartyGatewayMessageKind(reply.ReplyType.String()) != thirdPartyGatewayMessageVoice {
		if reply.Progress && !gatewayReplyIsFinal(reply) {
			payload["progress"] = true
			metadata["acp_turn"] = "progress"
		} else {
			payload["progress"] = false
			payload["final"] = true
			metadata["acp_turn"] = "final"
		}
	}
	if len(metadata) > 0 {
		payload["metadata"] = metadata
	}
	if normalizeThirdPartyGatewayMessageKind(reply.ReplyType.String()) == thirdPartyGatewayMessageImage {
		imageCaption := deviceResponseImageCaption(reply.Caption, reply.Text)
		payload["caption"] = imageCaption
		// Device image capabilities were supplied with the originating request.
		// Convert before the Hub relay so Hub only stores a bounded display-ready
		// payload instead of an unrestricted source image.
		if c.app != nil && c.app.thirdPartyGateway != nil {
			capabilities := c.app.thirdPartyGateway.clientCapabilities(clientID)
			if !capabilities.SupportsOutput("image") {
				return fmt.Errorf("device does not support image output")
			}
			prepared, err := prepareDeviceResponseImage(reply.ImageData, capabilities)
			if err != nil {
				return fmt.Errorf("prepare device image: %w", err)
			}
			payload["image_data"], payload["mime_type"] = prepared.Data, prepared.MIMEType
			payload["data"] = prepared.Data
			payload["file_name"], payload["sizeBytes"] = prepared.FileName, prepared.Size
			payload["width"], payload["height"] = prepared.Width, prepared.Height
		} else {
			return fmt.Errorf("device image capabilities are unavailable")
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return fmt.Errorf("Hub not connected")
	}
	if sourceID := thirdPartyReplyCorrelation(reply); sourceID != "" {
		// Carry both protocol spellings while deployed clients converge. The
		// ESP32 accepts either and can therefore complete the exact command.
		payload["replyTo"] = sourceID
		payload["replyToMessageId"] = sourceID
	}
	glyphText := reply.Text
	glyphCaption := reply.Caption
	if normalizeThirdPartyGatewayMessageKind(reply.ReplyType.String()) == thirdPartyGatewayMessageImage {
		glyphText = ""
		glyphCaption, _ = payload["caption"].(string)
	}
	if glyphs := deviceGlyphsForText(glyphText, glyphCaption); len(glyphs) > 0 {
		payload["glyphs"] = glyphs
	}
	return c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{"clientId": clientID, "conversationId": conversationID, "reply": payload}})
}

// SendDeviceGatewayToolMessage relays a protocol-native client tool lifecycle
// message without coercing it through the text/media reply shape.
func (c *RemoteHubClient) SendDeviceGatewayToolMessage(clientID, conversationID string, reply map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return fmt.Errorf("Hub not connected")
	}
	return c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{
		"clientId": clientID, "conversationId": firstNonEmpty(conversationID, "default"), "reply": reply,
	}})
}

// SendDeviceGatewayPetProfile pushes a settings-only profile change. Pet
// selection is independent of an assistant reply, so waiting for the next
// conversation made an idle ESP keep showing the previously selected pack.
func (c *RemoteHubClient) SendDeviceGatewayPetProfile(cfg corelib.AppConfig) error {
	profile := c.app.devicePetProfileForConfig(cfg)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return fmt.Errorf("Hub not connected")
	}
	reply := map[string]any{
		"reply_type":         "pet_profile",
		"pet_skin":           profile["skin"],
		"pet_motion_enabled": profile["motionEnabled"],
		"pet_asset":          profile["asset"],
	}
	return c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{"clientId": "*", "conversationId": "system", "reply": reply}})
}

// SendDeviceGatewayPetProfileForClient sends an isolated pet-profile update
// to exactly one hardware binding. Hub validates the ownership boundary and
// persists the selected profile for reconnects.
func (c *RemoteHubClient) SendDeviceGatewayPetProfileForClient(clientID string, profile map[string]any) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("hardware client ID is required")
	}
	requestID := "device-pet-" + randomHexID(12)
	waiter := make(chan error, 1)
	c.requestWaiters.Store(requestID, waiter)
	defer c.requestWaiters.Delete(requestID)
	c.mu.Lock()
	if !c.connected || c.conn == nil {
		c.mu.Unlock()
		return fmt.Errorf("Hub not connected")
	}
	reply := map[string]any{
		"reply_type": "pet_profile", "pet_skin": profile["skin"],
		"pet_motion_enabled": profile["motionEnabled"], "pet_asset": profile["asset"],
	}
	err := c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", RequestID: requestID, TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{
		"clientId": clientID, "conversationId": "system", "reply": reply,
	}})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-waiter:
		return err
	case <-timer.C:
		return fmt.Errorf("timed out waiting for Hub hardware pet confirmation")
	}
}

// SendDeviceGatewayHardwareConfig relays hardware-only settings to every
// device paired with this GUI. Hub retains the client-to-machine ownership
// boundary and fans the message out to matching device queues.
func (c *RemoteHubClient) SendDeviceGatewayHardwareConfig(extra map[string]any) error {
	return c.SendDeviceGatewayHardwareReply(map[string]any{"reply_type": "hardware_config", "extra": extra})
}

// SendDeviceGatewayHardwareConfigForClient sends a settings-only command to
// one owned ESP32. Per-device volume controls must never use the wildcard
// broadcast path.
func (c *RemoteHubClient) SendDeviceGatewayHardwareConfigForClient(clientID string, extra map[string]any) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("hardware client ID is required")
	}
	requestID := "device-volume-" + randomHexID(12)
	waiter := make(chan error, 1)
	c.requestWaiters.Store(requestID, waiter)
	defer c.requestWaiters.Delete(requestID)
	c.mu.Lock()
	if !c.connected || c.conn == nil {
		c.mu.Unlock()
		return fmt.Errorf("Hub not connected")
	}
	err := c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", RequestID: requestID, TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{
		"clientId": clientID, "conversationId": "system",
		"reply": map[string]any{"reply_type": "hardware_config", "extra": extra},
	}})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-waiter:
		return err
	case <-timer.C:
		return fmt.Errorf("timed out waiting for Hub hardware volume confirmation")
	}
}

// SendDeviceGatewayHardwareEnabled publishes the durable master switch used by
// Hub's public ESP32 endpoint. Device credentials remain paired while disabled,
// but Hub rejects their traffic until the desktop enables hardware again.
func (c *RemoteHubClient) SendDeviceGatewayHardwareEnabled(enabled bool) error {
	requestID := "device-hardware-state-" + randomHexID(12)
	waiter := make(chan error, 1)
	c.requestWaiters.Store(requestID, waiter)
	defer c.requestWaiters.Delete(requestID)

	c.mu.Lock()
	if !c.connected || c.conn == nil {
		c.mu.Unlock()
		return fmt.Errorf("Hub not connected")
	}
	err := c.conn.WriteJSON(HubEnvelope{
		Type:      "im.device_gateway_reply",
		RequestID: requestID,
		TS:        time.Now().Unix(),
		MachineID: c.machineID,
		Payload: map[string]any{
			"clientId": "*", "conversationId": "system",
			"reply": map[string]any{"reply_type": "hardware_enabled", "enabled": enabled},
		},
	})
	c.mu.Unlock()
	if err != nil {
		return err
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-waiter:
		return err
	case <-timer.C:
		return fmt.Errorf("timed out waiting for Hub hardware state confirmation")
	}
}

// SendDeviceGatewayAllowCustomPets updates Hub's durable authorization gate
// for independent device-pet profiles.
func (c *RemoteHubClient) SendDeviceGatewayAllowCustomPets(enabled bool) error {
	requestID := "device-custom-pets-" + randomHexID(12)
	waiter := make(chan error, 1)
	c.requestWaiters.Store(requestID, waiter)
	defer c.requestWaiters.Delete(requestID)
	c.mu.Lock()
	if !c.connected || c.conn == nil {
		c.mu.Unlock()
		return fmt.Errorf("Hub not connected")
	}
	err := c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", RequestID: requestID, TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{
		"clientId": "*", "conversationId": "system", "reply": map[string]any{"reply_type": "hardware_custom_pets", "enabled": enabled},
	}})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case err := <-waiter:
		return err
	case <-timer.C:
		return fmt.Errorf("timed out waiting for Hub custom-pet setting confirmation")
	}
}

func (c *RemoteHubClient) SendDeviceGatewayHardwareReply(reply map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return fmt.Errorf("Hub not connected")
	}
	return c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{
		"clientId": "*", "conversationId": "system",
		"reply": reply,
	}})
}

// SendDeviceGatewayHardwareReplyConfirmed sends an audio preview to one
// concrete client and waits until that ESP32 acknowledges physical playback.
// Hub's earlier protocol ACK only confirms queue acceptance.
func (c *RemoteHubClient) SendDeviceGatewayHardwareReplyConfirmed(clientID string, reply map[string]any) error {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return fmt.Errorf("hardware client ID is required")
	}
	requestID := "device-hardware-" + randomHexID(12)
	waiter := make(chan error, 1)
	acceptance := make(chan error, 1)
	c.playbackWaiters.Store(requestID, waiter)
	c.requestWaiters.Store(requestID, acceptance)
	defer c.playbackWaiters.Delete(requestID)
	defer c.requestWaiters.Delete(requestID)

	confirmedReply := make(map[string]any, len(reply))
	for key, value := range reply {
		confirmedReply[key] = value
	}
	extra := make(map[string]any)
	if existing, ok := reply["extra"].(map[string]any); ok {
		for key, value := range existing {
			extra[key] = value
		}
	}
	extra["hardware_audio_preview"] = true
	extra["hardware_audio_preview_request_id"] = requestID
	confirmedReply["extra"] = extra

	c.mu.Lock()
	if !c.connected || c.conn == nil {
		c.mu.Unlock()
		return fmt.Errorf("Hub not connected")
	}
	err := c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", RequestID: requestID, TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{
		"clientId": clientID, "conversationId": "system", "reply": confirmedReply,
	}})
	c.mu.Unlock()
	if err != nil {
		return err
	}

	// First require Hub to accept and queue the request. This produces a fast,
	// precise error when no remote hardware can receive it, instead of making
	// the GUI wait for the longer physical-playback timeout.
	acceptanceTimer := time.NewTimer(5 * time.Second)
	select {
	case err := <-acceptance:
		acceptanceTimer.Stop()
		if err != nil {
			return err
		}
	case <-acceptanceTimer.C:
		return fmt.Errorf("timed out waiting for Hub to accept the remote playback request")
	}

	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case err := <-waiter:
		return err
	case <-timer.C:
		return fmt.Errorf("timed out waiting for ESP32 playback confirmation")
	}
}

// syncDeviceGatewayPetProfile makes reconnects self-healing. A pet can be
// changed while the GUI is offline; without this catch-up push the Hub and ESP
// would retain the old skin until another setting change or gateway reply.
func (c *RemoteHubClient) syncDeviceGatewayPetProfile() {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		log.Printf("[device-pet] load profile for reconnect sync failed: %v", err)
		return
	}
	if cfg.HardwareAllowCustomPets {
		// Per-device profiles already live durably in Hub. A reconnect must not
		// replace them with the desktop's currently selected system pet.
		return
	}
	if err := c.SendDeviceGatewayPetProfile(cfg); err != nil {
		log.Printf("[device-pet] reconnect profile sync failed: %v", err)
	}
}

// syncDeviceGatewayWelcome restores Hub's durable boot-time sound after a GUI
// reconnect. It intentionally does not trigger a test playback.
func (c *RemoteHubClient) syncDeviceGatewayWelcome() {
	if err := c.app.SyncHardwareWelcome(); err != nil {
		log.Printf("[device-welcome] reconnect sync failed: %v", err)
	}
}

// syncDeviceGatewayVolume restores the latest local speaker setting after a
// Hub reconnect, including changes made while the Hub was unavailable.
func (c *RemoteHubClient) syncDeviceGatewayVolume() {
	if err := c.app.SyncHardwareVolume(); err != nil {
		log.Printf("[device-volume] reconnect sync failed: %v", err)
	}
}

func (c *RemoteHubClient) syncDeviceGatewayHardwareState() {
	cfg, err := c.app.LoadConfig()
	if err != nil {
		log.Printf("[device-hardware] load reconnect state failed: %v", err)
		return
	}
	if err := c.SendDeviceGatewayHardwareEnabled(cfg.HardwareEnabled); err != nil {
		log.Printf("[device-hardware] reconnect state sync failed: %v", err)
		return
	}
	if !cfg.HardwareEnabled {
		return
	}
	if err := c.SendDeviceGatewayAllowCustomPets(cfg.HardwareAllowCustomPets); err != nil {
		log.Printf("[device-pet] reconnect custom-pet permission sync failed: %v", err)
	}
	c.syncDeviceGatewayWelcome()
	c.syncDeviceGatewayVolume()
}

// SendDeviceGatewayAmbient publishes a GUI-resolved weather snapshot to all
// hardware surfaces paired with this GUI. Weather lookup remains a GUI concern;
// Hub is only the authenticated relay.
func (c *RemoteHubClient) SendDeviceGatewayAmbient(summary string, temperatureC int, location string, expiresAt time.Time) error {
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(2 * time.Hour)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected || c.conn == nil {
		return fmt.Errorf("Hub not connected")
	}
	ambient := map[string]any{
		"weather": map[string]any{
			"summary": strings.TrimSpace(summary), "temperatureC": temperatureC,
			"location": strings.TrimSpace(location),
		},
		"expiresAt": expiresAt.UnixMilli(),
	}
	if glyphs := deviceGlyphsForText(summary, location); len(glyphs) > 0 {
		ambient["glyphs"] = glyphs
	}
	reply := map[string]any{"reply_type": "ambient", "ambient": ambient}
	return c.conn.WriteJSON(HubEnvelope{Type: "im.device_gateway_reply", TS: time.Now().Unix(), MachineID: c.machineID, Payload: map[string]any{"clientId": "*", "conversationId": "system", "reply": reply}})
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
	// Reuse one timer across ticks; recreate only when interval changes.
	interval := c.currentHeartbeatInterval()
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		<-timer.C
		if !c.IsConnected() {
			return
		}
		if err := c.SendHeartbeat(); err != nil {
			c.handleConnectionLoss(err)
			return
		}
		next := c.currentHeartbeatInterval()
		if next != interval {
			interval = next
		}
		timer.Reset(interval)
	}
}

func (c *RemoteHubClient) currentHeartbeatInterval() time.Duration {
	if sec := c.cachedHeartbeatSec.Load(); sec > 0 {
		return time.Duration(sec) * time.Second
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return time.Duration(corelib.DefaultRemoteHeartbeatSec) * time.Second
	}
	sec := normalizeRemoteHeartbeatIntervalSec(cfg.RemoteHeartbeatSec)
	c.cachedHeartbeatSec.Store(int64(sec))
	return time.Duration(sec) * time.Second
}

// InvalidateHeartbeatIntervalCache forces the next heartbeat to re-read config.
// Call after RemoteHeartbeatSec is changed via settings.
func (c *RemoteHubClient) InvalidateHeartbeatIntervalCache() {
	if c == nil {
		return
	}
	c.cachedHeartbeatSec.Store(0)
	// Hub URL / machine token may change with the same settings save path.
	c.invalidateMobileWorkerAuth()
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
	c.cancelAllHubDeviceSpeech()
	c.stopHardwareAgents()
	disconnectErr := fmt.Errorf("Hub disconnected before request confirmation")
	c.failHubRequests(disconnectErr)
	c.failHubPlaybackRequests(fmt.Errorf("Hub disconnected before ESP32 playback confirmation"))
	c.failHardwareDeviceLists(disconnectErr)
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
	c.cancelAllHubDeviceSpeech()
	// A broken Hub connection invalidates every device runtime just as an
	// explicit Disconnect does. Otherwise an automatic reconnect could retain
	// per-device loops and private HTTP pools from the dead transport.
	c.stopHardwareAgents()
	playbackErr := errors.New("Hub connection lost before ESP32 playback confirmation")
	if err != nil {
		playbackErr = fmt.Errorf("Hub connection lost before ESP32 playback confirmation: %w", err)
	}
	c.failHubPlaybackRequests(playbackErr)
	c.failHubRequests(playbackErr)
	c.failHardwareDeviceLists(playbackErr)
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
	maxBackoff := 30 * time.Second
	// Fast reconnect: when Hub sent close(1001) indicating planned shutdown,
	// use minimal backoff so the client reconnects within ~100-200ms of the
	// new Hub instance becoming available.
	if c.reconnectImmediate.Swap(false) {
		backoff = 100 * time.Millisecond
		maxBackoff = 2 * time.Second // cap low — new instance will be ready shortly
		log.Printf("[hub-client] reconnectLoop: fast reconnect mode (Hub planned shutdown)")
	}
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

		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
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
