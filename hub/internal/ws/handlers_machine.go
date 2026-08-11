package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/gorilla/websocket"
)

type HeartbeatConfigProvider interface {
	GetHeartbeatConfig(ctx context.Context, userID string, tenantID string) (*HeartbeatConfigPayload, error)
}

type HeartbeatConfigPayload struct {
	CapabilityMarketPolicy       corelib.CapabilityMarketPolicy        `json:"capability_market_policy,omitempty"`
	DigitalEmployeeAuthorization *corelib.DigitalEmployeeAuthorization `json:"digital_employee_authorization,omitempty"`
}

type ConnContext struct {
	Conn        *websocket.Conn
	Role        string
	TenantID    string
	UserID      string
	MachineID   string
	MachineName string // populated by handleMachineHello
	ViewerID    string

	// lastLLMTokenTotal tracks the last reported cumulative token count.
	// Used to detect actual LLM activity (delta > 0) for duration recording.
	lastLLMTokenTotal int64

	// Heartbeat log rate-limit state (per connection).
	lastHBLogAt       time.Time
	lastHBLogSessions int
	lastHBLogInterval int

	// gwClaimSeqs tracks the claim sequence for each IM gateway platform
	// claimed by this connection. Used during cleanup to only release claims
	// that belong to this specific connection (prevents race where a stale
	// connection cleanup releases a newer claim from a reconnected client).
	gwClaimSeqs map[string]uint64

	// sendCh is a buffered channel for async writes. Messages are enqueued
	// here and drained by a dedicated writer goroutine, preventing slow
	// clients from blocking broadcast loops.
	sendCh chan any
	// closeSend is closed when the connection is torn down to stop the
	// writer goroutine.
	closeSend chan struct{}
}

const machineHeartbeatLogMinInterval = time.Minute

// shouldLogMachineHeartbeat returns true when this heartbeat is worth logging.
// First heartbeat, session/interval changes, and ≥1min silence always log.
func shouldLogMachineHeartbeat(ctx *ConnContext, sessions, intervalSec int) bool {
	if ctx == nil {
		return true
	}
	now := time.Now()
	if ctx.lastHBLogAt.IsZero() ||
		sessions != ctx.lastHBLogSessions ||
		intervalSec != ctx.lastHBLogInterval ||
		now.Sub(ctx.lastHBLogAt) >= machineHeartbeatLogMinInterval {
		ctx.lastHBLogAt = now
		ctx.lastHBLogSessions = sessions
		ctx.lastHBLogInterval = intervalSec
		return true
	}
	return false
}

const (
	// sendChSize is the per-connection outbound message buffer. If a client
	// can't keep up and the buffer fills, the connection is dropped.
	sendChSize = 256
	// batchFlushInterval is the maximum time the writer goroutine waits to
	// accumulate messages before flushing them in a single write cycle.
	batchFlushInterval = 50 * time.Millisecond
)

// initSendCh initialises the async send channel and starts the writer goroutine.
func (c *ConnContext) initSendCh() {
	c.sendCh = make(chan any, sendChSize)
	c.closeSend = make(chan struct{})
	go c.writeLoop()
}

// Send enqueues a message for async delivery. Returns false if the buffer is
// full (slow client), in which case the connection is closed.
func (c *ConnContext) Send(msg any) bool {
	select {
	case c.sendCh <- msg:
		return true
	default:
		// Buffer full - slow client. Close the writer and the underlying
		// WebSocket so the read loop also terminates.
		log.Printf("[ws] Send: buffer full for role=%s machine_id=%s, dropping connection", c.Role, c.MachineID)
		c.closeWriter()
		_ = c.Conn.Close()
		return false
	}
}

// SendChDiag returns the current length of the send channel for diagnostics.
func (c *ConnContext) SendChDiag() chan any {
	return c.sendCh
}

// closeWriter signals the writer goroutine to stop. Safe to call multiple times.
func (c *ConnContext) closeWriter() {
	select {
	case <-c.closeSend:
	default:
		close(c.closeSend)
	}
}

// writeLoop drains sendCh and writes messages to the WebSocket. It batches
// messages that arrive within batchFlushInterval into a single write cycle
// to reduce syscall overhead for viewers receiving many events.
func (c *ConnContext) writeLoop() {
	batch := make([]any, 0, 16)
	timer := time.NewTimer(batchFlushInterval)
	defer timer.Stop()

	flush := func() bool {
		n := len(batch)
		for _, msg := range batch {
			if err := c.Conn.WriteJSON(msg); err != nil {
				log.Printf("[ws] writeLoop: write error role=%s machine_id=%s: %v", c.Role, c.MachineID, err)
				batch = batch[:0]
				return false // connection broken
			}
		}
		if n > 0 {
			log.Printf("[ws] writeLoop: flushed %d msg(s) to role=%s machine_id=%s", n, c.Role, c.MachineID)
		}
		batch = batch[:0]
		return true
	}

	for {
		select {
		case msg, ok := <-c.sendCh:
			if !ok {
				return
			}
			batch = append(batch, msg)
			// Drain any additional queued messages without blocking.
		drain:
			for {
				select {
				case m, ok := <-c.sendCh:
					if !ok {
						flush()
						return
					}
					batch = append(batch, m)
					if len(batch) >= 32 {
						if !flush() {
							return
						}
					}
				default:
					break drain
				}
			}
			// Reset timer; if nothing else arrives within the interval, flush.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(batchFlushInterval)
			// If batch is non-trivial, flush immediately.
			if len(batch) >= 4 {
				if !flush() {
					return
				}
			}
		case <-timer.C:
			if len(batch) > 0 {
				if !flush() {
					return
				}
			}
			timer.Reset(batchFlushInterval)
		case <-c.closeSend:
			flush()
			return
		}
	}
}

type MachineHelloPayload struct {
	Name                 string         `json:"name"`
	Nickname             string         `json:"nickname,omitempty"`
	Platform             string         `json:"platform"`
	Hostname             string         `json:"hostname,omitempty"`
	Arch                 string         `json:"arch,omitempty"`
	AppVersion           string         `json:"app_version,omitempty"`
	HeartbeatIntervalSec int            `json:"heartbeat_interval_sec,omitempty"`
	Capabilities         map[string]any `json:"capabilities,omitempty"`
}

type MachineHeartbeatPayload struct {
	ActiveSessions       int                     `json:"active_sessions,omitempty"`
	HeartbeatIntervalSec int                     `json:"heartbeat_interval_sec,omitempty"`
	AppVersion           string                  `json:"app_version,omitempty"`
	LLMConfigured        *bool                   `json:"llm_configured,omitempty"`
	LLMTokenUsage        *corelib.TokenUsageStat `json:"llm_token_usage,omitempty"`
	// AdaptivePrompt is optional process-level light/full prompt cost stats.
	AdaptivePrompt *corelib.AdaptivePromptStat `json:"adaptive_prompt,omitempty"`
	// CostOps is optional cost-route + local daily fleet snapshot.
	CostOps *corelib.CostOpsStat `json:"cost_ops,omitempty"`
}

type DeviceBinder interface {
	BindDesktop(machineID string, ctx *ConnContext)
	UnbindDesktop(ctx context.Context, machineID string, conn *ConnContext) error
	MarkOnline(ctx context.Context, machineID string, hello MachineHelloPayload) error
	Heartbeat(ctx context.Context, machineID string, heartbeat MachineHeartbeatPayload) error
	GetMachineOwner(ctx context.Context, machineID string) (tenantID string, userID string, err error)
	SendToMachine(machineID string, msg any) error
	SetAlias(ctx context.Context, machineID string, alias string)
	// CheckAliasConflict returns true if another same-user online machine
	// already uses the given alias.
	CheckAliasConflict(machineID, userID, alias string) bool
}

type SessionService interface {
	OnSessionCreated(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error
	OnSessionSummary(ctx context.Context, machineID, userID, sessionID string, summary session.SessionSummary) error
	OnSessionPreviewDelta(ctx context.Context, machineID, userID, sessionID string, delta session.SessionPreviewDelta) error
	OnSessionImportantEvent(ctx context.Context, machineID, userID, sessionID string, event session.ImportantEvent) error
	OnSessionClosed(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error
	OnSessionImage(ctx context.Context, machineID, userID, sessionID string, img session.SessionImage)
	RecordUserTokenUsageSnapshot(ctx context.Context, tenantID, sourceID, userID string, usage store.UserTokenUsage, observedAt time.Time) error
	RecordHeartbeat(ctx context.Context, tenantID, machineID, userID string, at time.Time) error
	MarkMachineOffline(ctx context.Context, machineID string) error
	GetSnapshot(userID, machineID, sessionID string) (*session.SessionCacheEntry, bool)
	GetSnapshotForTenant(tenantID, userID, machineID, sessionID string) (*session.SessionCacheEntry, bool)
	ListByMachine(ctx context.Context, userID, machineID string) ([]*session.SessionCacheEntry, error)
}

type identityService interface {
	AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error)
	AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error)
	IssueViewerTokenForUser(ctx context.Context, userID string) (string, error)
}

// IMAgentResponseHandler handles agent responses routed back from MaClaw clients.
type IMAgentResponseHandler interface {
	HandleAgentResponse(requestID string, resp json.RawMessage)
	HandleAgentProgress(requestID string, text string)
}

// IMProactiveSender sends proactive messages to a user's IM channels.
// Used for scheduled task notifications and other non-request-based messages.
type IMProactiveSender interface {
	SendProactiveMessage(ctx context.Context, tenantID, userID, text string) error
	SendProactiveMessageToTarget(ctx context.Context, tenantID, userID, platformName, platformUID, text string) error
	// SendProactiveFile sends a file to the user's IM channels (e.g. Swarm PDF documents).
	SendProactiveFile(ctx context.Context, tenantID, userID, b64Data, fileName, mimeType, message string) error
	// SendProactiveFileToTarget is exact and must not fall back to another channel.
	SendProactiveFileToTarget(ctx context.Context, tenantID, userID string, target agent.IMFileDeliveryTarget, b64Data, fileName, mimeType, message string) error
}

// IMGatewayPlugin handles gateway claim/release and message forwarding for
// client-side IM gateways (QQ Bot, Telegram). Each platform registers one.
type IMGatewayPlugin interface {
	Name() string
	ClaimGatewayForTenant(tenantID, machineID, userID string) (ok bool, reason string, seq uint64)
	ReleaseAllForTenantMachine(tenantID, machineID string)
	ReleaseAllForTenantMachineBySeq(tenantID, machineID string, seqs map[string]uint64)
	HandleGatewayMessage(machineID string, payload json.RawMessage)
}

// DeviceGateway is the public HTTP endpoint used by low-power hardware
// clients.  It remains owned by Hub while the paired GUI stays behind NAT on
// its existing outbound WebSocket connection.
type DeviceGateway interface {
	RegisterPairing(machineID, tenantID, userID, code string) error
	ServeHTTP(http.ResponseWriter, *http.Request)
}

// DeviceProfileUpdaterFunc is called when a machine sends device.profile_update.
type DeviceProfileUpdaterFunc func(tenantID, userID string, profile json.RawMessage)

// DeviceNotifyHook is called on machine connect/disconnect for IM notifications.
type DeviceNotifyHook struct {
	OnConnect    func(userID, machineID, name string)
	OnDisconnect func(userID, machineID, name string)
}

// NotificationMarkReader handles notification.ack messages from clients.
// Implemented by notification.Service to avoid circular package dependencies.
type NotificationMarkReader interface {
	MarkRead(ctx context.Context, machineID, notificationID string) error
}

type Gateway struct {
	Identity identityService
	Devices  DeviceBinder
	Sessions SessionService

	// IMResponder handles im.agent_response and im.agent_progress messages
	// from MaClaw clients. Set via SetIMResponder after construction to
	// avoid circular deps.
	IMResponder IMAgentResponseHandler

	// IMProactive handles im.proactive_message from MaClaw clients.
	// Set via SetIMProactiveSender after construction.
	IMProactive IMProactiveSender

	// IMGatewayPlugins maps platform name -> gateway plugin for client-side
	// IM gateways (QQ Bot, Telegram). Set via RegisterIMGatewayPlugin.
	IMGatewayPlugins map[string]IMGatewayPlugin
	DeviceGateway    DeviceGateway

	// DeviceProfileUpdater is called when a machine sends device.profile_update.
	// Set via SetDeviceProfileUpdater after construction.
	DeviceProfileUpdater DeviceProfileUpdaterFunc

	// DeviceNotifyFunc is called on machine connect/disconnect for IM notifications.
	// Set via SetDeviceNotifyFunc after construction.
	DeviceNotifyFunc DeviceNotifyHook

	// SecurityProvider provides security policy data for heartbeat ack injection.
	// Set via SetSecurityProvider after construction.
	SecurityProvider security.SecurityPolicyProvider

	// ConfigProvider provides Hub-managed client config options for heartbeat ack injection.
	ConfigProvider HeartbeatConfigProvider

	// NotificationService handles notification.ack messages from clients.
	// Set via SetNotificationService after construction.
	NotificationService NotificationMarkReader

	mu                sync.RWMutex
	viewersByMachine  map[string]map[*ConnContext]struct{}
	viewersBySession  map[string]map[*ConnContext]struct{}
	projectsByMachine map[string][]map[string]any
	toolsByMachine    map[string][]any // machine_id -> tool info array
}

func NewGateway(identity identityService, devices DeviceBinder, sessions SessionService) *Gateway {
	return &Gateway{
		Identity:          identity,
		Devices:           devices,
		Sessions:          sessions,
		viewersByMachine:  map[string]map[*ConnContext]struct{}{},
		viewersBySession:  map[string]map[*ConnContext]struct{}{},
		projectsByMachine: map[string][]map[string]any{},
		toolsByMachine:    map[string][]any{},
	}
}

// SetIMResponder wires the handler for im.agent_response messages.
func (g *Gateway) SetIMResponder(h IMAgentResponseHandler) {
	g.IMResponder = h
}

// SetIMProactiveSender wires the handler for im.proactive_message messages.
func (g *Gateway) SetIMProactiveSender(s IMProactiveSender) {
	g.IMProactive = s
}

// RegisterIMGatewayPlugin registers a client-side IM gateway plugin (e.g.
// "qqbot_remote", "telegram") so the WebSocket gateway can route
// im.gateway_claim and im.gateway_message to it.
func (g *Gateway) RegisterIMGatewayPlugin(plugin IMGatewayPlugin) {
	if g.IMGatewayPlugins == nil {
		g.IMGatewayPlugins = make(map[string]IMGatewayPlugin)
	}
	g.IMGatewayPlugins[plugin.Name()] = plugin
}

func (g *Gateway) SetDeviceGateway(deviceGateway DeviceGateway) {
	g.DeviceGateway = deviceGateway
}

// SetDeviceProfileUpdater wires the device profile update handler.
func (g *Gateway) SetDeviceProfileUpdater(fn DeviceProfileUpdaterFunc) {
	g.DeviceProfileUpdater = fn
}

// SetDeviceNotifyHook wires the device connect/disconnect notification hooks.
func (g *Gateway) SetDeviceNotifyHook(hook DeviceNotifyHook) {
	g.DeviceNotifyFunc = hook
}

// SetNotificationService wires the notification service for handling
// notification.ack messages from clients.
func (g *Gateway) SetNotificationService(svc NotificationMarkReader) {
	g.NotificationService = svc
}

func (g *Gateway) HandleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin:       func(r *http.Request) bool { return true },
		EnableCompression: true, // permessage-deflate - reduces bandwidth 50-70% for text-heavy preview deltas
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] HandleWS: upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	// Enable compression on outbound messages.
	conn.EnableWriteCompression(true)
	conn.SetCompressionLevel(6) // balanced speed/ratio

	log.Printf("[ws] HandleWS: new WebSocket connection from %s (compression=%v)", r.RemoteAddr, true)

	// Configure WebSocket-level ping-pong to keep the connection alive even
	// when the application-level heartbeat is delayed by heavy workloads
	// (e.g. full-disk scans). The read deadline is refreshed on every pong
	// and on every normal message, so a busy machine that sends data but
	// misses a pong still stays connected.
	const (
		pongWait   = 90 * time.Second // must be > client heartbeat interval
		pingPeriod = 30 * time.Second // must be < pongWait
	)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Start a goroutine that sends periodic WebSocket pings.
	pingDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
					return
				}
			case <-pingDone:
				return
			}
		}
	}()

	ctx := &ConnContext{Conn: conn}
	ctx.initSendCh()
	defer func() {
		close(pingDone)
		ctx.closeWriter()
		g.cleanupConnection(ctx)
	}()

	for {
		// Refresh read deadline on every incoming message so that machines
		// sending frequent data (summaries, preview deltas) don't time out
		// even if the pong is slightly delayed.
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))

		var msg Envelope
		if err := conn.ReadJSON(&msg); err != nil {
			logWebSocketReadError(ctx, err)
			return
		}

		switch msg.Type {
		case "auth.machine":
			if err := g.handleMachineAuth(ctx, msg); err != nil {
				return
			}
		case "auth.viewer":
			if err := g.handleViewerAuth(ctx, msg); err != nil {
				return
			}
		case "viewer.subscribe_machine":
			if err := g.handleViewerSubscribeMachine(ctx, msg); err != nil {
				return
			}
		case "viewer.start_session":
			if err := g.handleViewerStartSession(ctx, msg); err != nil {
				return
			}
		case "viewer.unsubscribe_machine":
			if err := g.handleViewerUnsubscribeMachine(ctx, msg); err != nil {
				return
			}
		case "viewer.subscribe_session":
			if err := g.handleViewerSubscribeSession(ctx, msg); err != nil {
				return
			}
		case "viewer.unsubscribe_session":
			if err := g.handleViewerUnsubscribeSession(ctx, msg); err != nil {
				return
			}
		case "machine.hello":
			if err := g.handleMachineHello(ctx, msg); err != nil {
				return
			}
		case "machine.heartbeat":
			if err := g.handleMachineHeartbeat(ctx, msg); err != nil {
				return
			}
		case "machine.projects":
			if err := g.handleMachineProjects(ctx, msg); err != nil {
				return
			}
		case "machine.tools":
			if err := g.handleMachineTools(ctx, msg); err != nil {
				return
			}
		case "session.created":
			if err := g.handleSessionCreated(ctx, msg); err != nil {
				return
			}
		case "session.summary":
			if err := g.handleSessionSummary(ctx, msg); err != nil {
				return
			}
		case "session.preview_delta":
			if err := g.handleSessionPreviewDelta(ctx, msg); err != nil {
				return
			}
		case "session.important_event":
			if err := g.handleSessionImportantEvent(ctx, msg); err != nil {
				return
			}
		case "session.closed":
			if err := g.handleSessionClosed(ctx, msg); err != nil {
				return
			}
		case "session.image":
			if err := g.handleSessionImage(ctx, msg); err != nil {
				return
			}
		case "session.image_input.error":
			if err := g.handleSessionImageInputError(ctx, msg); err != nil {
				return
			}
		case "session.image_input":
			if err := g.handleSessionImageInput(ctx, msg); err != nil {
				return
			}
		case "session.screenshot":
			if err := g.handleSessionScreenshot(ctx, msg); err != nil {
				return
			}
		case "im.agent_response":
			if err := g.handleIMAgentResponse(ctx, msg); err != nil {
				return
			}
		case "im.agent_progress":
			if err := g.handleIMAgentProgress(ctx, msg); err != nil {
				return
			}
		case "im.proactive_message":
			if err := g.handleIMProactiveMessage(ctx, msg); err != nil {
				return
			}
		case "im.proactive_file":
			if err := g.handleIMProactiveFile(ctx, msg); err != nil {
				return
			}
		case "im.gateway_claim":
			if err := g.handleIMGatewayClaim(ctx, msg); err != nil {
				return
			}
		case "im.gateway_unclaim":
			if err := g.handleIMGatewayUnclaim(ctx, msg); err != nil {
				return
			}
		case "im.gateway_message":
			if err := g.handleIMGatewayMessage(ctx, msg); err != nil {
				return
			}
		case "im.device_gateway_pairing":
			if err := g.handleDeviceGatewayPairing(ctx, msg); err != nil {
				return
			}
		case "im.device_gateway_devices_list":
			if err := g.handleDeviceGatewayDevicesList(ctx, msg); err != nil {
				return
			}
		case "im.device_gateway_device_delete":
			if err := g.handleDeviceGatewayDeviceDelete(ctx, msg); err != nil {
				return
			}
		case "im.device_gateway_reply":
			handled, err := g.handleDeviceGatewayReply(ctx, msg)
			if err != nil {
				return
			}
			if !handled && strings.TrimSpace(msg.RequestID) != "" {
				if err := writeAck(ctx.Conn, msg.RequestID); err != nil {
					return
				}
			}
		case "machine.nickname_update":
			if err := g.handleMachineNicknameUpdate(ctx, msg); err != nil {
				return
			}
		case "device.profile_update":
			if err := g.handleDeviceProfileUpdate(ctx, msg); err != nil {
				return
			}
		case MessageTypeNotificationAck:
			g.handleNotificationAck(ctx, msg)
		default:
			_ = writeWSError(conn, "UNKNOWN_MESSAGE", "Unsupported message type")
		}
	}
}

// logWebSocketReadError keeps normal WebSocket disconnects and idle timeouts
// distinguishable from protocol failures. A timeout still closes the connection
// (so the client can reconnect), but it is an expected outcome for an
// unavailable machine rather than evidence of a Hub application error.
func logWebSocketReadError(ctx *ConnContext, err error) {
	role, machineID := "", ""
	if ctx != nil {
		role = ctx.Role
		machineID = ctx.MachineID
	}

	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		log.Printf("[ws] HandleWS: connection closed (role=%s machine_id=%s): %v", role, machineID, err)
		return
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		log.Printf("[ws] HandleWS: connection idle timeout; closing for reconnect (role=%s machine_id=%s): %v", role, machineID, err)
		return
	}

	log.Printf("[ws] HandleWS: ReadJSON error (role=%s machine_id=%s): %v", role, machineID, err)
}

func (g *Gateway) HandleSessionEvent(event session.Event) {
	eventTenantID := store.NormalizeTenantID(event.TenantID)
	g.mu.RLock()
	machineWatchers := make([]*ConnContext, 0, len(g.viewersByMachine[event.MachineID]))
	for watcher := range g.viewersByMachine[event.MachineID] {
		if eventTenantID != "" && store.NormalizeTenantID(watcher.TenantID) != eventTenantID {
			continue
		}
		machineWatchers = append(machineWatchers, watcher)
	}
	watchers := make([]*ConnContext, 0, len(g.viewersBySession[event.SessionID]))
	for watcher := range g.viewersBySession[event.SessionID] {
		if eventTenantID != "" && store.NormalizeTenantID(watcher.TenantID) != eventTenantID {
			continue
		}
		watchers = append(watchers, watcher)
	}
	g.mu.RUnlock()

	var payload any
	switch event.Type {
	case "session.summary":
		payload = event.Summary
	case "session.preview_delta":
		payload = event.PreviewDelta
	case "session.important_event":
		payload = event.Important
	case "session.closed", "session.created":
		payload = event.Payload
	default:
		payload = event.Payload
	}

	msg := map[string]any{
		"type":       event.Type,
		"ts":         time.Now().Unix(),
		"machine_id": event.MachineID,
		"session_id": event.SessionID,
		"payload":    payload,
	}

	for _, watcher := range watchers {
		watcher.Send(msg)
	}

	if event.Type != "session.created" && event.Type != "session.closed" && event.Type != "session.summary" {
		return
	}

	for _, watcher := range machineWatchers {
		watcher.Send(msg)
	}
}

func (g *Gateway) broadcastMachineEvent(machineID string, payload map[string]any) {
	tenantID, _ := payload["tenant_id"].(string)
	normalizedTenantID := store.NormalizeTenantID(tenantID)
	g.mu.RLock()
	machineWatchers := make([]*ConnContext, 0, len(g.viewersByMachine[machineID]))
	for watcher := range g.viewersByMachine[machineID] {
		if normalizedTenantID != "" && store.NormalizeTenantID(watcher.TenantID) != normalizedTenantID {
			continue
		}
		machineWatchers = append(machineWatchers, watcher)
	}
	g.mu.RUnlock()

	for _, watcher := range machineWatchers {
		watcher.Send(payload)
	}
}

func (g *Gateway) handleMachineAuth(ctx *ConnContext, msg Envelope) error {
	var payload struct {
		MachineID    string `json:"machine_id"`
		MachineToken string `json:"machine_token"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleMachineAuth: invalid payload: %v", err)
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid auth.machine payload")
	}
	log.Printf("[ws] handleMachineAuth: authenticating machine_id=%s", payload.MachineID)
	principal, err := g.Identity.AuthenticateMachine(context.Background(), payload.MachineID, payload.MachineToken)
	if err != nil {
		log.Printf("[ws] handleMachineAuth: auth FAILED for machine_id=%s: %v", payload.MachineID, err)
		_ = writeWSError(ctx.Conn, "UNAUTHORIZED", "Machine authentication failed")
		return fmt.Errorf("machine auth failed: %w", err)
	}
	ctx.Role = "machine"
	ctx.TenantID = principal.TenantID
	ctx.UserID = principal.UserID
	ctx.MachineID = principal.MachineID
	log.Printf("[ws] handleMachineAuth: auth OK machine_id=%s user_id=%s, calling BindDesktop", principal.MachineID, principal.UserID)
	g.Devices.BindDesktop(principal.MachineID, ctx)

	authPayload := map[string]any{"role": "machine", "machine_id": principal.MachineID, "tenant_id": principal.TenantID}

	// Always issue a fresh viewer token on connect. The client persists it
	// and uses it for LLM service API calls (redeem, status, chat completions).
	// Issuing on every connect ensures the client always has a valid token
	// (viewer tokens expire after 30 days; reconnects keep it fresh).
	// Old tokens expire naturally - no accumulation concern for SQLite.
	if viewerToken, err := g.Identity.IssueViewerTokenForUser(context.Background(), principal.UserID); err == nil {
		authPayload["viewer_token"] = viewerToken
	} else {
		log.Printf("[ws] handleMachineAuth: failed to issue viewer token for user_id=%s: %v", principal.UserID, err)
	}

	return writeWSJSON(ctx.Conn, map[string]any{"type": "auth.ok", "payload": authPayload})
}

func (g *Gateway) handleViewerAuth(ctx *ConnContext, msg Envelope) error {
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid auth.viewer payload")
	}
	principal, err := g.Identity.AuthenticateViewer(context.Background(), payload.AccessToken)
	if err != nil {
		return writeWSError(ctx.Conn, "UNAUTHORIZED", "Viewer authentication failed")
	}
	ctx.Role = "viewer"
	ctx.TenantID = principal.TenantID
	ctx.UserID = principal.UserID
	ctx.ViewerID = principal.Email
	return writeWSJSON(ctx.Conn, map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "viewer", "email": principal.Email, "tenant_id": principal.TenantID}})
}

func (g *Gateway) handleViewerSubscribeSession(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "viewer" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Viewer role required")
	}
	var payload struct {
		MachineID string `json:"machine_id"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid viewer.subscribe_session payload")
	}
	entry, ok := g.Sessions.GetSnapshotForTenant(ctx.TenantID, ctx.UserID, payload.MachineID, payload.SessionID)
	if !ok || entry == nil {
		return writeWSError(ctx.Conn, "NOT_FOUND", "Session not found")
	}

	g.mu.Lock()
	if g.viewersBySession[payload.SessionID] == nil {
		g.viewersBySession[payload.SessionID] = map[*ConnContext]struct{}{}
	}
	g.viewersBySession[payload.SessionID][ctx] = struct{}{}
	ctx.MachineID = payload.MachineID
	g.mu.Unlock()

	return writeWSJSON(ctx.Conn, map[string]any{
		"type":       "session.snapshot",
		"machine_id": payload.MachineID,
		"session_id": payload.SessionID,
		"payload": map[string]any{
			"execution_mode": entry.ExecutionMode,
			"summary":        entry.Summary,
			"preview":        entry.Preview,
			"recent_events":  entry.RecentEvents,
			"host_online":    entry.HostOnline,
			"updated_at":     entry.UpdatedAt.Unix(),
		},
	})
}

func (g *Gateway) handleViewerSubscribeMachine(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "viewer" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Viewer role required")
	}
	var payload struct {
		MachineID string `json:"machine_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid viewer.subscribe_machine payload")
	}

	entries, err := g.Sessions.ListByMachine(store.WithTenant(context.Background(), ctx.TenantID), ctx.UserID, payload.MachineID)
	if err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}

	g.mu.Lock()
	if g.viewersByMachine[payload.MachineID] == nil {
		g.viewersByMachine[payload.MachineID] = map[*ConnContext]struct{}{}
	}
	g.viewersByMachine[payload.MachineID][ctx] = struct{}{}
	g.mu.Unlock()

	sessionsPayload := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		sessionsPayload = append(sessionsPayload, map[string]any{
			"session_id":     entry.SessionID,
			"machine_id":     entry.MachineID,
			"user_id":        entry.UserID,
			"execution_mode": entry.ExecutionMode,
			"summary":        entry.Summary,
			"preview":        entry.Preview,
			"host_online":    entry.HostOnline,
			"updated_at":     entry.UpdatedAt.Unix(),
		})
	}

	return writeWSJSON(ctx.Conn, map[string]any{
		"type":       "machine.snapshot",
		"machine_id": payload.MachineID,
		"payload": map[string]any{
			"sessions": sessionsPayload,
			"projects": g.getProjectsForMachine(payload.MachineID),
			"tools":    g.getToolsForMachine(payload.MachineID),
		},
	})
}

func (g *Gateway) handleViewerStartSession(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "viewer" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Viewer role required")
	}
	var payload struct {
		MachineID   string `json:"machine_id"`
		Tool        string `json:"tool"`
		ProjectID   string `json:"project_id,omitempty"`
		ProjectPath string `json:"project_path,omitempty"`
		Provider    string `json:"provider,omitempty"`
		UseProxy    *bool  `json:"use_proxy,omitempty"`
		YoloMode    *bool  `json:"yolo_mode,omitempty"`
		AdminMode   *bool  `json:"admin_mode,omitempty"`
		PythonEnv   string `json:"python_env,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid viewer.start_session payload")
	}
	if payload.MachineID == "" || payload.Tool == "" {
		return writeWSError(ctx.Conn, "INVALID_INPUT", "machine_id and tool are required")
	}
	if !g.viewerCanAccessMachine(ctx, payload.MachineID) {
		return writeWSError(ctx.Conn, "FORBIDDEN", "machine is outside current tenant")
	}

	command := map[string]any{
		"type":       "session.start",
		"request_id": msg.RequestID,
		"ts":         time.Now().Unix(),
		"machine_id": payload.MachineID,
		"payload": map[string]any{
			"tool":         payload.Tool,
			"project_id":   payload.ProjectID,
			"project_path": payload.ProjectPath,
			"provider":     payload.Provider,
			"python_env":   payload.PythonEnv,
		},
	}
	commandPayload := command["payload"].(map[string]any)
	if payload.UseProxy != nil {
		commandPayload["use_proxy"] = *payload.UseProxy
	}
	if payload.YoloMode != nil {
		commandPayload["yolo_mode"] = *payload.YoloMode
	}
	if payload.AdminMode != nil {
		commandPayload["admin_mode"] = *payload.AdminMode
	}
	if err := g.Devices.SendToMachine(payload.MachineID, command); err != nil {
		return writeWSError(ctx.Conn, "MACHINE_OFFLINE", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) viewerCanAccessMachine(ctx *ConnContext, machineID string) bool {
	if g == nil || g.Devices == nil || ctx == nil {
		return false
	}
	tenantID, userID, err := g.Devices.GetMachineOwner(context.Background(), machineID)
	if err != nil {
		return false
	}
	return strings.TrimSpace(userID) == strings.TrimSpace(ctx.UserID) && store.NormalizeTenantID(tenantID) == store.NormalizeTenantID(ctx.TenantID)
}

func (g *Gateway) handleViewerUnsubscribeMachine(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "viewer" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Viewer role required")
	}
	var payload struct {
		MachineID string `json:"machine_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid viewer.unsubscribe_machine payload")
	}

	g.mu.Lock()
	if watchers := g.viewersByMachine[payload.MachineID]; watchers != nil {
		delete(watchers, ctx)
		if len(watchers) == 0 {
			delete(g.viewersByMachine, payload.MachineID)
		}
	}
	g.mu.Unlock()

	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleViewerUnsubscribeSession(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "viewer" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Viewer role required")
	}
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid viewer.unsubscribe_session payload")
	}
	g.mu.Lock()
	if watchers := g.viewersBySession[payload.SessionID]; watchers != nil {
		delete(watchers, ctx)
		if len(watchers) == 0 {
			delete(g.viewersBySession, payload.SessionID)
		}
	}
	g.mu.Unlock()
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleMachineHello(ctx *ConnContext, msg Envelope) error {
	var payload MachineHelloPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleMachineHello: invalid payload for machine_id=%s: %v", ctx.MachineID, err)
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid machine.hello payload")
	}
	log.Printf("[ws] handleMachineHello: machine_id=%s name=%s platform=%s hostname=%s", ctx.MachineID, payload.Name, payload.Platform, payload.Hostname)
	if err := g.Devices.MarkOnline(context.Background(), ctx.MachineID, payload); err != nil {
		log.Printf("[ws] handleMachineHello: MarkOnline FAILED for machine_id=%s: %v", ctx.MachineID, err)
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	log.Printf("[ws] handleMachineHello: machine_id=%s marked online successfully", ctx.MachineID)
	ctx.MachineName = payload.Name
	if g.DeviceNotifyFunc.OnConnect != nil {
		go g.DeviceNotifyFunc.OnConnect(ctx.UserID, ctx.MachineID, payload.Name)
	}

	// Include hub_config in hello ack so the client gets digital_employee_authorization
	ackPayload := map[string]any{"ok": true}
	g.injectSecurityPolicy(ackPayload, ctx.UserID, ctx.TenantID, "handleMachineHello")
	g.injectHubConfig(ackPayload, ctx.UserID, ctx.TenantID, "handleMachineHello")
	return writeAckPayload(ctx.Conn, msg.RequestID, ackPayload)
}

func (g *Gateway) injectSecurityPolicy(ackPayload map[string]any, userID, tenantID, source string) {
	if g.SecurityProvider == nil {
		return
	}
	policyCtx := security.WithTenant(context.Background(), tenantID)
	policy, err := g.SecurityProvider.GetHeartbeatPolicy(policyCtx, userID)
	if err != nil {
		log.Printf("[ws] %s: security policy unavailable for tenant_id=%s user_id=%s: %v", source, tenantID, userID, err)
		return
	}
	if policy != nil {
		ackPayload["security_policy"] = policy
	}
}

func (g *Gateway) handleMachineHeartbeat(ctx *ConnContext, msg Envelope) error {
	var payload MachineHeartbeatPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleMachineHeartbeat: invalid payload for machine_id=%s: %v", ctx.MachineID, err)
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid machine.heartbeat payload")
	}
	// Heartbeats are high-frequency (often every 5–30s per machine). Log at most
	// once per minute per connection unless session count or interval changes —
	// otherwise hub logs become multi-GB noise that hides real failures.
	if shouldLogMachineHeartbeat(ctx, payload.ActiveSessions, payload.HeartbeatIntervalSec) {
		log.Printf("[ws] handleMachineHeartbeat: machine_id=%s sessions=%d interval=%d", ctx.MachineID, payload.ActiveSessions, payload.HeartbeatIntervalSec)
	}
	if err := g.Devices.Heartbeat(context.Background(), ctx.MachineID, payload); err != nil {
		log.Printf("[ws] handleMachineHeartbeat: Heartbeat FAILED for machine_id=%s: %v", ctx.MachineID, err)
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	if payload.LLMTokenUsage != nil && g.Sessions != nil {
		usage := store.UserTokenUsage{
			InputTokens:       payload.LLMTokenUsage.InputTokens,
			OutputTokens:      payload.LLMTokenUsage.OutputTokens,
			CachedInputTokens: payload.LLMTokenUsage.CachedInputTokens,
			CacheWriteTokens:  payload.LLMTokenUsage.CacheWriteTokens,
		}
		if err := g.Sessions.RecordUserTokenUsageSnapshot(store.WithTenant(context.Background(), ctx.TenantID), ctx.TenantID, "gui:"+ctx.MachineID, ctx.UserID, usage, time.Now()); err != nil {
			log.Printf("[ws] handleMachineHeartbeat: record llm token usage FAILED for machine_id=%s: %v", ctx.MachineID, err)
		}
		// Record heartbeat to duration log only when LLM token total has
		// increased since the last heartbeat. This ensures "usage duration"
		// measures actual AI activity, not idle client uptime.
		// On fresh connection lastLLMTokenTotal=0, so the first heartbeat with
		// any accumulated tokens triggers one recording (~60s over-count on
		// reconnect, acceptable vs missing the user's first AI session entirely).
		currentTotal := usage.TotalTokens()
		if currentTotal > ctx.lastLLMTokenTotal {
			ctx.lastLLMTokenTotal = currentTotal
			if ctx.TenantID != "" && ctx.UserID != "" {
				if err := g.Sessions.RecordHeartbeat(store.WithTenant(context.Background(), ctx.TenantID), ctx.TenantID, ctx.MachineID, ctx.UserID, time.Now()); err != nil {
					log.Printf("[ws] handleMachineHeartbeat: record heartbeat log FAILED for machine_id=%s: %v", ctx.MachineID, err)
				}
			}
		}
	}
	ackPayload := map[string]any{"ok": true}
	g.injectSecurityPolicy(ackPayload, ctx.UserID, ctx.TenantID, "handleMachineHeartbeat")
	g.injectHubConfig(ackPayload, ctx.UserID, ctx.TenantID, "handleMachineHeartbeat")
	return writeAckPayload(ctx.Conn, msg.RequestID, ackPayload)
}

func (g *Gateway) handleMachineProjects(ctx *ConnContext, msg Envelope) error {
	var payload struct {
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid machine.projects payload")
	}

	g.mu.Lock()
	g.projectsByMachine[ctx.MachineID] = cloneProjects(payload.Projects)
	g.mu.Unlock()

	g.broadcastMachineEvent(ctx.MachineID, map[string]any{
		"type":       "machine.projects",
		"tenant_id":  ctx.TenantID,
		"machine_id": ctx.MachineID,
		"ts":         time.Now().Unix(),
		"payload": map[string]any{
			"projects": cloneProjects(payload.Projects),
		},
	})
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleMachineTools(ctx *ConnContext, msg Envelope) error {
	var payload struct {
		Tools []any `json:"tools"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid machine.tools payload")
	}

	// Deep-copy the slice to avoid shared references.
	stored := make([]any, len(payload.Tools))
	copy(stored, payload.Tools)

	g.mu.Lock()
	g.toolsByMachine[ctx.MachineID] = stored
	g.mu.Unlock()

	g.broadcastMachineEvent(ctx.MachineID, map[string]any{
		"type":       "machine.tools",
		"tenant_id":  ctx.TenantID,
		"machine_id": ctx.MachineID,
		"ts":         time.Now().Unix(),
		"payload": map[string]any{
			"tools": stored,
		},
	})
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) getToolsForMachine(machineID string) []any {
	g.mu.RLock()
	defer g.mu.RUnlock()
	stored := g.toolsByMachine[machineID]
	if stored == nil {
		return nil
	}
	cp := make([]any, len(stored))
	copy(cp, stored)
	return cp
}

func (g *Gateway) handleSessionCreated(ctx *ConnContext, msg Envelope) error {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.created payload")
	}
	if ctx.TenantID != "" {
		payload["tenant_id"] = ctx.TenantID
	}
	if err := g.Sessions.OnSessionCreated(store.WithTenant(context.Background(), ctx.TenantID), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleSessionSummary(ctx *ConnContext, msg Envelope) error {
	var payload session.SessionSummary
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.summary payload")
	}
	if err := g.Sessions.OnSessionSummary(store.WithTenant(context.Background(), ctx.TenantID), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleSessionPreviewDelta(ctx *ConnContext, msg Envelope) error {
	var payload session.SessionPreviewDelta
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.preview_delta payload")
	}
	if err := g.Sessions.OnSessionPreviewDelta(store.WithTenant(context.Background(), ctx.TenantID), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	// Skip ack for preview deltas - they are high-frequency fire-and-forget
	// messages. Omitting the ack reduces round-trip overhead and frees the
	// WebSocket write buffer for the next incoming delta.
	return nil
}

func (g *Gateway) handleSessionImportantEvent(ctx *ConnContext, msg Envelope) error {
	var payload session.ImportantEvent
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.important_event payload")
	}
	if err := g.Sessions.OnSessionImportantEvent(store.WithTenant(context.Background(), ctx.TenantID), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleSessionClosed(ctx *ConnContext, msg Envelope) error {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.closed payload")
	}
	if err := g.Sessions.OnSessionClosed(store.WithTenant(context.Background(), ctx.TenantID), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

// handleSessionImage handles session.image from a machine and forwards to viewers subscribed to that session.
func (g *Gateway) handleSessionImage(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	if msg.SessionID == "" {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "session_id is required")
	}

	g.mu.RLock()
	watchers := make([]*ConnContext, 0, len(g.viewersBySession[msg.SessionID]))
	for watcher := range g.viewersBySession[msg.SessionID] {
		watchers = append(watchers, watcher)
	}
	g.mu.RUnlock()

	fwd := map[string]any{
		"type":       "session.image",
		"ts":         time.Now().Unix(),
		"machine_id": ctx.MachineID,
		"session_id": msg.SessionID,
		"payload":    json.RawMessage(msg.Payload),
	}
	for _, watcher := range watchers {
		watcher.Send(fwd)
	}

	// Dispatch to session listeners (e.g. Feishu notifier) so they can
	// forward the image to users who are watching via chat.
	var imgPayload session.SessionImage
	if err := json.Unmarshal(msg.Payload, &imgPayload); err == nil && imgPayload.Data != "" {
		g.Sessions.OnSessionImage(store.WithTenant(context.Background(), ctx.TenantID), ctx.MachineID, ctx.UserID, msg.SessionID, imgPayload)
	}

	return nil
}

// handleSessionImageInputError handles session.image_input.error from a machine and forwards to viewers subscribed to that session.
func (g *Gateway) handleSessionImageInputError(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	if msg.SessionID == "" {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "session_id is required")
	}

	g.mu.RLock()
	watchers := make([]*ConnContext, 0, len(g.viewersBySession[msg.SessionID]))
	for watcher := range g.viewersBySession[msg.SessionID] {
		watchers = append(watchers, watcher)
	}
	g.mu.RUnlock()

	fwd := map[string]any{
		"type":       "session.image_input.error",
		"ts":         time.Now().Unix(),
		"machine_id": ctx.MachineID,
		"session_id": msg.SessionID,
		"payload":    json.RawMessage(msg.Payload),
	}
	for _, watcher := range watchers {
		watcher.Send(fwd)
	}
	return nil
}

// handleSessionImageInput handles session.image_input from a viewer and forwards to the machine that owns the session.
func (g *Gateway) handleSessionImageInput(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "viewer" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Viewer role required")
	}

	var payload struct {
		SessionID string `json:"session_id"`
		MachineID string `json:"machine_id"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.image_input payload")
	}
	if payload.MachineID == "" {
		payload.MachineID = msg.MachineID
	}
	if payload.SessionID == "" {
		payload.SessionID = msg.SessionID
	}
	if payload.MachineID == "" || payload.SessionID == "" {
		return writeWSError(ctx.Conn, "INVALID_INPUT", "machine_id and session_id are required")
	}

	command := map[string]any{
		"type":       "session.image_input",
		"ts":         time.Now().Unix(),
		"machine_id": payload.MachineID,
		"session_id": payload.SessionID,
		"payload":    json.RawMessage(msg.Payload),
	}
	if err := g.Devices.SendToMachine(payload.MachineID, command); err != nil {
		return writeWSError(ctx.Conn, "MACHINE_OFFLINE", err.Error())
	}
	return nil
}

// handleSessionScreenshot handles session.screenshot from a viewer and forwards to the machine.
// The machine will capture a screenshot and send it back via session.image.
func (g *Gateway) handleSessionScreenshot(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "viewer" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Viewer role required")
	}

	var payload struct {
		SessionID   string `json:"session_id"`
		MachineID   string `json:"machine_id"`
		WindowTitle string `json:"window_title"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.screenshot payload")
	}
	if payload.MachineID == "" {
		payload.MachineID = msg.MachineID
	}
	if payload.SessionID == "" {
		payload.SessionID = msg.SessionID
	}
	if payload.MachineID == "" || payload.SessionID == "" {
		return writeWSError(ctx.Conn, "INVALID_INPUT", "machine_id and session_id are required")
	}

	command := map[string]any{
		"type":       "session.screenshot",
		"ts":         time.Now().Unix(),
		"machine_id": payload.MachineID,
		"session_id": payload.SessionID,
		"payload":    json.RawMessage(msg.Payload),
	}
	if err := g.Devices.SendToMachine(payload.MachineID, command); err != nil {
		return writeWSError(ctx.Conn, "MACHINE_OFFLINE", err.Error())
	}
	return nil
}

// handleIMAgentResponse handles im.agent_response from a MaClaw client and
// routes it to the MessageRouter so the waiting IM request can be fulfilled.
func (g *Gateway) handleIMAgentResponse(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	if g.IMResponder == nil {
		log.Printf("[ws] handleIMAgentResponse: no IMResponder configured, dropping message")
		return nil
	}
	if msg.RequestID == "" {
		log.Printf("[ws] handleIMAgentResponse: missing request_id, dropping message")
		return nil
	}
	g.IMResponder.HandleAgentResponse(msg.RequestID, msg.Payload)
	return nil
}

// handleIMAgentProgress handles im.agent_progress from a MaClaw client.
// It resets the pending request timeout and optionally delivers the progress
// text to the user via IM.
func (g *Gateway) handleIMAgentProgress(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	if g.IMResponder == nil {
		return nil
	}
	if msg.RequestID == "" {
		return nil
	}
	var payload struct {
		Text        string `json:"text"`
		Platform    string `json:"platform,omitempty"`
		PlatformUID string `json:"platform_uid,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleIMAgentProgress: parse error for request_id=%s: %v", msg.RequestID, err)
		return nil
	}
	g.IMResponder.HandleAgentProgress(msg.RequestID, payload.Text)
	return nil
}

// handleIMProactiveMessage handles im.proactive_message from a MaClaw client.
// Used for scheduled task results and other non-request-based notifications
// that need to be pushed to the user's IM channels.
func (g *Gateway) handleIMProactiveMessage(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	if g.IMProactive == nil {
		log.Printf("[ws] handleIMProactiveMessage: no IMProactiveSender configured, dropping message")
		return nil
	}
	var payload struct {
		Text        string `json:"text"`
		Platform    string `json:"platform,omitempty"`
		PlatformUID string `json:"platform_uid,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleIMProactiveMessage: parse error: %v", err)
		return nil
	}
	if payload.Text == "" {
		return nil
	}
	var err error
	if strings.TrimSpace(payload.Platform) != "" || strings.TrimSpace(payload.PlatformUID) != "" {
		err = g.IMProactive.SendProactiveMessageToTarget(context.Background(), ctx.TenantID, ctx.UserID, payload.Platform, payload.PlatformUID, payload.Text)
	} else {
		err = g.IMProactive.SendProactiveMessage(context.Background(), ctx.TenantID, ctx.UserID, payload.Text)
	}
	if err != nil {
		log.Printf("[ws] handleIMProactiveMessage: send failed for user_id=%s: %v", ctx.UserID, err)
	}
	return nil
}

// handleIMProactiveFile handles im.proactive_file from a MaClaw client.
// Used for Swarm PDF document delivery to the user's IM channels.
func (g *Gateway) handleIMProactiveFile(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	if g.IMProactive == nil {
		log.Printf("[ws] handleIMProactiveFile: no IMProactiveSender configured, dropping message")
		return nil
	}
	var payload struct {
		FileData string                     `json:"file_data"`
		FileName string                     `json:"file_name"`
		MimeType string                     `json:"mime_type"`
		Message  string                     `json:"message"`
		Target   agent.IMFileDeliveryTarget `json:"target,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleIMProactiveFile: parse error: %v", err)
		return nil
	}
	if payload.FileData == "" || payload.FileName == "" {
		return nil
	}
	var err error
	if target := payload.Target.Normalize(); target.Active() {
		// Any target field changes the operation from broadcast to exact routing.
		// Reject incomplete target objects instead of letting a downstream
		// canonicalizer guess a channel or recipient.
		if target.Channel == "" || (target.GroupID == "" && target.UserID == "") {
			log.Printf("[ws] handleIMProactiveFile: invalid exact target for user_id=%s: %#v", ctx.UserID, target)
			return nil
		}
		err = g.IMProactive.SendProactiveFileToTarget(context.Background(), ctx.TenantID, ctx.UserID, target, payload.FileData, payload.FileName, payload.MimeType, payload.Message)
	} else {
		err = g.IMProactive.SendProactiveFile(context.Background(), ctx.TenantID, ctx.UserID, payload.FileData, payload.FileName, payload.MimeType, payload.Message)
	}
	if err != nil {
		log.Printf("[ws] handleIMProactiveFile: send failed for user_id=%s: %v", ctx.UserID, err)
	}
	return nil
}

// handleIMGatewayClaim handles im.gateway_claim from a client that wants to
// register as the gateway owner for a given IM platform (QQ Bot, Telegram).
func (g *Gateway) handleIMGatewayClaim(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	var payload struct {
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleIMGatewayClaim: parse error: %v", err)
		return nil
	}
	if payload.Platform == "" {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "platform is required")
	}
	plugin, ok := g.IMGatewayPlugins[payload.Platform]
	if !ok {
		_ = writeWSJSON(ctx.Conn, map[string]any{
			"type": "im.gateway_claim_result",
			"payload": map[string]any{
				"platform": payload.Platform,
				"ok":       false,
				"reason":   fmt.Sprintf("unknown platform: %s", payload.Platform),
			},
		})
		return nil
	}
	ok, reason, seq := plugin.ClaimGatewayForTenant(ctx.TenantID, ctx.MachineID, ctx.UserID)
	if ok {
		// Record the claim seq on this connection so cleanup releases the
		// correct generation.
		if ctx.gwClaimSeqs == nil {
			ctx.gwClaimSeqs = make(map[string]uint64)
		}
		ctx.gwClaimSeqs[payload.Platform] = seq
	}
	_ = writeWSJSON(ctx.Conn, map[string]any{
		"type": "im.gateway_claim_result",
		"payload": map[string]any{
			"platform": payload.Platform,
			"ok":       ok,
			"reason":   reason,
		},
	})
	return nil
}

// handleIMGatewayUnclaim handles im.gateway_unclaim from a client that wants
// to release its gateway ownership for a given IM platform (e.g. when the user
// unchecks the "enable" checkbox). This ensures Hub stops routing IM messages
// to the disconnected client.
func (g *Gateway) handleIMGatewayUnclaim(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	var payload struct {
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleIMGatewayUnclaim: parse error: %v", err)
		return nil
	}
	if payload.Platform == "" {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "platform is required")
	}
	plugin, ok := g.IMGatewayPlugins[payload.Platform]
	if !ok {
		log.Printf("[ws] handleIMGatewayUnclaim: unknown platform %s", payload.Platform)
		return nil
	}
	if ctx.gwClaimSeqs != nil {
		if seq, ok := ctx.gwClaimSeqs[payload.Platform]; ok {
			plugin.ReleaseAllForTenantMachineBySeq(ctx.TenantID, ctx.MachineID, map[string]uint64{payload.Platform: seq})
			delete(ctx.gwClaimSeqs, payload.Platform)
		} else {
			plugin.ReleaseAllForTenantMachine(ctx.TenantID, ctx.MachineID)
		}
	} else {
		plugin.ReleaseAllForTenantMachine(ctx.TenantID, ctx.MachineID)
	}
	log.Printf("[ws] im.gateway_unclaim: platform=%s machine=%s", payload.Platform, ctx.MachineID)
	_ = writeWSJSON(ctx.Conn, map[string]any{
		"type": "im.gateway_unclaim_result",
		"payload": map[string]any{
			"platform": payload.Platform,
			"ok":       true,
		},
	})
	return nil
}

// handleIMGatewayMessage handles im.gateway_message from a client-side IM
// gateway. The client forwards incoming QQ/TG messages here so Hub can route
// them through the standard IM Adapter pipeline.
func (g *Gateway) handleIMGatewayMessage(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	var payload struct {
		Platform string          `json:"platform"`
		Data     json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleIMGatewayMessage: parse error: %v", err)
		return nil
	}
	plugin, ok := g.IMGatewayPlugins[payload.Platform]
	if !ok {
		log.Printf("[ws] handleIMGatewayMessage: unknown platform %s", payload.Platform)
		return nil
	}
	data := payload.Data
	if ctx.TenantID != "" && len(data) > 0 {
		var message map[string]any
		if err := json.Unmarshal(data, &message); err == nil {
			// The authenticated WebSocket connection is the authority for tenant
			// isolation. Older clients omit tenant_id, and newer clients must not
			// be able to spoof a different tenant in the nested payload.
			message["tenant_id"] = ctx.TenantID
			if patched, marshalErr := json.Marshal(message); marshalErr == nil {
				data = patched
			}
		}
	}

	// Run in a goroutine to avoid blocking the WS read loop.
	// HandleGatewayMessage -> IM Adapter -> routeToSingleMachine blocks until
	// the Agent replies (up to 180s). If we block here, the read loop cannot
	// receive the im.agent_response from the same connection -> deadlock.
	machineID := ctx.MachineID
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[ws] handleIMGatewayMessage: panic in HandleGatewayMessage (platform=%s machine=%s): %v", payload.Platform, machineID, r)
			}
		}()
		plugin.HandleGatewayMessage(machineID, data)
	}()
	return nil
}

func (g *Gateway) handleDeviceGatewayPairing(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "FORBIDDEN", "Machine role required")
	}
	if g.DeviceGateway == nil {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "UNAVAILABLE", "device gateway is not enabled")
	}
	var payload struct {
		PairCode string `json:"pairCode"`
		// Code is accepted from old GUI releases while pairCode is the
		// canonical WebSocket field.
		Code          string         `json:"code"`
		PetSkin       string         `json:"petSkin"`
		MotionEnabled bool           `json:"motionEnabled"`
		PetAsset      map[string]any `json:"petAsset"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "invalid pairing payload")
	}
	pairCode := strings.TrimSpace(payload.PairCode)
	if pairCode == "" {
		pairCode = strings.TrimSpace(payload.Code)
	}
	if len(pairCode) != 6 || strings.Trim(pairCode, "0123456789") != "" {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_PAIRING_CODE", "a six-digit pairing code is required")
	}
	if profiler, ok := g.DeviceGateway.(interface {
		RegisterPairingWithPetProfileAsset(string, string, string, string, string, bool, map[string]any) error
	}); ok {
		if err := profiler.RegisterPairingWithPetProfileAsset(ctx.MachineID, ctx.TenantID, ctx.UserID, pairCode, payload.PetSkin, payload.MotionEnabled, payload.PetAsset); err != nil {
			return writeWSRequestError(ctx.Conn, msg.RequestID, deviceGatewayPairingErrorCode(err), err.Error())
		}
	} else if err := g.DeviceGateway.RegisterPairing(ctx.MachineID, ctx.TenantID, ctx.UserID, pairCode); err != nil {
		return writeWSRequestError(ctx.Conn, msg.RequestID, deviceGatewayPairingErrorCode(err), err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func deviceGatewayPairingErrorCode(err error) string {
	var coded interface{ PairingErrorCode() string }
	if errors.As(err, &coded) {
		switch coded.PairingErrorCode() {
		case "PAIRING_CODE_COLLISION", "HARDWARE_DISABLED":
			return coded.PairingErrorCode()
		}
	}
	return "INVALID_PAIRING_CODE"
}

func (g *Gateway) handleDeviceGatewayDevicesList(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" || g.DeviceGateway == nil {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "FORBIDDEN", "Machine role required")
	}
	lister, ok := g.DeviceGateway.(interface {
		ListMachineDevicesJSON(string) []map[string]any
	})
	if !ok {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "UNAVAILABLE", "hardware device registry is unavailable")
	}
	var request struct {
		LegacyMachineIDs []string `json:"legacyMachineIds"`
	}
	if len(msg.Payload) > 0 && json.Unmarshal(msg.Payload, &request) != nil {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "invalid hardware list payload")
	}
	if migrator, ok := g.DeviceGateway.(interface {
		MigrateMachineHardwareBindings(string, string, string, []string) error
	}); ok {
		if err := migrator.MigrateMachineHardwareBindings(ctx.MachineID, ctx.TenantID, ctx.UserID, request.LegacyMachineIDs); err != nil {
			return writeWSRequestError(ctx.Conn, msg.RequestID, "HARDWARE_MIGRATION_FAILED", err.Error())
		}
	}
	payload := map[string]any{"devices": lister.ListMachineDevicesJSON(ctx.MachineID)}
	if state, ok := g.DeviceGateway.(interface{ MachineHardwareBindingStateJSON(string) map[string]any }); ok {
		for key, value := range state.MachineHardwareBindingStateJSON(ctx.MachineID) {
			payload[key] = value
		}
	}
	return ctx.Conn.WriteJSON(map[string]any{"type": "im.device_gateway_devices", "request_id": msg.RequestID, "payload": payload})
}

func (g *Gateway) handleDeviceGatewayDeviceDelete(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" || g.DeviceGateway == nil {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "FORBIDDEN", "Machine role required")
	}
	var payload struct {
		ClientID string `json:"clientId"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || strings.TrimSpace(payload.ClientID) == "" {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "clientId is required")
	}
	deleter, ok := g.DeviceGateway.(interface{ DeleteMachineDevice(string, string) error })
	if !ok {
		return writeWSRequestError(ctx.Conn, msg.RequestID, "UNAVAILABLE", "hardware device registry is unavailable")
	}
	if err := deleter.DeleteMachineDevice(ctx.MachineID, payload.ClientID); err != nil {
		return writeWSRequestError(ctx.Conn, msg.RequestID, deviceGatewayDeleteErrorCode(err), err.Error())
	}
	return writeAckPayload(ctx.Conn, msg.RequestID, map[string]any{"ok": true, "clientId": strings.TrimSpace(payload.ClientID)})
}

func deviceGatewayDeleteErrorCode(err error) string {
	var coded interface{ HardwareErrorCode() string }
	if errors.As(err, &coded) && coded.HardwareErrorCode() == "HARDWARE_NOT_OWNED" {
		return "HARDWARE_NOT_OWNED"
	}
	return "DEVICE_DELETE_FAILED"
}

func (g *Gateway) handleDeviceGatewayReply(ctx *ConnContext, msg Envelope) (bool, error) {
	if ctx.Role != "machine" || g.DeviceGateway == nil {
		return false, nil
	}
	var payload struct {
		ClientID       string         `json:"clientId"`
		ConversationID string         `json:"conversationId"`
		Reply          map[string]any `json:"reply"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || strings.TrimSpace(payload.ClientID) == "" {
		return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "invalid device gateway reply")
	}
	if ambient, ok := payload.Reply["ambient"].(map[string]any); ok {
		if updater, ok := g.DeviceGateway.(interface {
			UpdateMachineAmbient(string, map[string]any)
		}); ok {
			updater.UpdateMachineAmbient(ctx.MachineID, ambient)
		}
		if payload.ClientID == "*" {
			return false, nil
		}
	}
	if replyType, _ := payload.Reply["reply_type"].(string); strings.EqualFold(strings.TrimSpace(replyType), "pet_profile") {
		petSkin, _ := payload.Reply["pet_skin"].(string)
		motionEnabled, _ := payload.Reply["pet_motion_enabled"].(bool)
		asset, _ := payload.Reply["pet_asset"].(map[string]any)
		if payload.ClientID == "*" {
			if profiler, ok := g.DeviceGateway.(interface {
				UpdateMachinePetProfileAsset(string, string, bool, map[string]any)
			}); ok {
				profiler.UpdateMachinePetProfileAsset(ctx.MachineID, petSkin, motionEnabled, asset)
			}
		} else if profiler, ok := g.DeviceGateway.(interface {
			UpdateMachineDevicePetProfileAsset(string, string, string, bool, map[string]any) error
		}); ok {
			if err := profiler.UpdateMachineDevicePetProfileAsset(ctx.MachineID, payload.ClientID, petSkin, motionEnabled, asset); err != nil {
				return true, writeWSRequestError(ctx.Conn, msg.RequestID, "HARDWARE_PET_UPDATE_FAILED", err.Error())
			}
		} else {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "UNAVAILABLE", "per-device pet settings are unavailable")
		}
		return false, nil
	}
	if replyType, _ := payload.Reply["reply_type"].(string); strings.EqualFold(strings.TrimSpace(replyType), "hardware_enabled") {
		updater, ok := g.DeviceGateway.(interface {
			UpdateMachineHardwareEnabled(string, bool) error
		})
		if !ok {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "UNAVAILABLE", "hardware state routing is unavailable")
		}
		enabled, ok := payload.Reply["enabled"].(bool)
		if !ok {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "hardware enabled state is required")
		}
		if err := updater.UpdateMachineHardwareEnabled(ctx.MachineID, enabled); err != nil {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", err.Error())
		}
		return false, nil
	}
	if replyType, _ := payload.Reply["reply_type"].(string); strings.EqualFold(strings.TrimSpace(replyType), "hardware_custom_pets") {
		if payload.ClientID != "*" {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "hardware custom-pet permission is machine-scoped")
		}
		updater, ok := g.DeviceGateway.(interface {
			UpdateMachineAllowCustomPets(string, bool) error
		})
		if !ok {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "UNAVAILABLE", "hardware custom-pet settings are unavailable")
		}
		enabled, ok := payload.Reply["enabled"].(bool)
		if !ok {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "custom-pet enabled state is required")
		}
		if err := updater.UpdateMachineAllowCustomPets(ctx.MachineID, enabled); err != nil {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", err.Error())
		}
		return false, nil
	}
	if replyType, _ := payload.Reply["reply_type"].(string); strings.EqualFold(strings.TrimSpace(replyType), "hardware_welcome_config") {
		if updater, ok := g.DeviceGateway.(interface {
			UpdateMachineWelcome(string, bool, string, bool) error
		}); ok {
			enabled, _ := payload.Reply["welcome_enabled"].(bool)
			audio, _ := payload.Reply["file_data"].(string)
			replaceAudio, _ := payload.Reply["replace_audio"].(bool)
			if err := updater.UpdateMachineWelcome(ctx.MachineID, enabled, audio, replaceAudio); err != nil {
				return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", err.Error())
			}
		}
		return false, nil
	}
	if replyType, _ := payload.Reply["reply_type"].(string); strings.EqualFold(strings.TrimSpace(replyType), "hardware_config") {
		extra, _ := payload.Reply["extra"].(map[string]any)
		_, hasVolume := extra["volume"]
		_, hasBrightness := extra["brightness"]
		_, hasScreenSleepTimeout := extra["screenSleepSeconds"]
		if !hasVolume && !hasBrightness && !hasScreenSleepTimeout {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "hardware config requires volume, brightness, or screen sleep timeout")
		}
		for key := range extra {
			if key != "volume" && key != "brightness" && key != "screenSleepSeconds" {
				return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "hardware config contains an unsupported setting")
			}
		}
		if payload.ClientID == "*" {
			if hasVolume {
				if updater, ok := g.DeviceGateway.(interface {
					UpdateMachineVolume(string, any) error
				}); ok {
					if err := updater.UpdateMachineVolume(ctx.MachineID, extra["volume"]); err != nil {
						return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", err.Error())
					}
				}
			}
			if hasBrightness {
				if updater, ok := g.DeviceGateway.(interface {
					UpdateMachineBrightness(string, any) error
				}); ok {
					if err := updater.UpdateMachineBrightness(ctx.MachineID, extra["brightness"]); err != nil {
						return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", err.Error())
					}
				}
			}
			if hasScreenSleepTimeout {
				return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", "screen sleep timeout must target one hardware device")
			}
		} else {
			if hasVolume {
				if updater, ok := g.DeviceGateway.(interface {
					UpdateMachineDeviceVolume(string, string, any) error
				}); ok {
					if err := updater.UpdateMachineDeviceVolume(ctx.MachineID, payload.ClientID, extra["volume"]); err != nil {
						return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", err.Error())
					}
				}
			}
			if hasBrightness {
				if updater, ok := g.DeviceGateway.(interface {
					UpdateMachineDeviceBrightness(string, string, any) error
				}); ok {
					if err := updater.UpdateMachineDeviceBrightness(ctx.MachineID, payload.ClientID, extra["brightness"]); err != nil {
						return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", err.Error())
					}
				}
			}
			if hasScreenSleepTimeout {
				if updater, ok := g.DeviceGateway.(interface {
					UpdateMachineDeviceScreenSleepTimeout(string, string, any) error
				}); ok {
					if err := updater.UpdateMachineDeviceScreenSleepTimeout(ctx.MachineID, payload.ClientID, extra["screenSleepSeconds"]); err != nil {
						return true, writeWSRequestError(ctx.Conn, msg.RequestID, "INVALID_MESSAGE", err.Error())
					}
				}
			}
			if hasVolume || hasBrightness || hasScreenSleepTimeout {
				// The per-device updaters validate ownership, persist the setting
				// and queue it for a live compatible device. Do not route a
				// second time: an offline binding is still a successful durable
				// update and receives its level during the next handshake.
				return false, nil
			}
		}
	}
	extra, _ := payload.Reply["extra"].(map[string]any)
	hardwareAudioPreview, _ := extra["hardware_audio_preview"].(bool)
	if hardwareAudioPreview {
		if payload.ClientID == "*" {
			relay, ok := g.DeviceGateway.(interface {
				EnqueueMachineReplyCount(string, string, map[string]any) int
			})
			if !ok {
				return true, writeWSRequestError(ctx.Conn, msg.RequestID, "UNAVAILABLE", "hardware preview routing is unavailable")
			}
			if relay.EnqueueMachineReplyCount(ctx.MachineID, payload.ConversationID, payload.Reply) == 0 {
				return true, writeWSRequestError(ctx.Conn, msg.RequestID, "NO_COMPATIBLE_HARDWARE", "no online remote ESP32 supports welcome audio playback")
			}
			return false, nil
		}
		// A selected-device preview must be routed through the ownership-aware
		// path below.  Do not fall through to a generic reply where an offline
		// target is indistinguishable from a successful queue acceptance.
	}
	if payload.ClientID == "*" {
		if relay, ok := g.DeviceGateway.(interface {
			EnqueueMachineReply(string, string, map[string]any)
		}); ok {
			relay.EnqueueMachineReply(ctx.MachineID, payload.ConversationID, payload.Reply)
		}
		return false, nil
	}
	if relay, ok := g.DeviceGateway.(interface {
		EnqueueMachineClientReplyResult(string, string, string, map[string]any) MachineClientReplyResult
	}); ok {
		if replyType, ok := payload.Reply["reply_type"].(string); ok && strings.TrimSpace(replyType) != "" {
			payload.Reply["type"] = replyType
		}
		result := relay.EnqueueMachineClientReplyResult(ctx.MachineID, payload.ClientID, payload.ConversationID, payload.Reply)
		if result.Queued == 0 {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, machineClientReplyErrorCode(result), machineClientReplyErrorMessage(result))
		}
		return false, nil
	}
	if relay, ok := g.DeviceGateway.(interface {
		EnqueueMachineClientReplyCount(string, string, string, map[string]any) int
	}); ok {
		if replyType, ok := payload.Reply["reply_type"].(string); ok && strings.TrimSpace(replyType) != "" {
			payload.Reply["type"] = replyType
		}
		if relay.EnqueueMachineClientReplyCount(ctx.MachineID, payload.ClientID, payload.ConversationID, payload.Reply) == 0 {
			return true, writeWSRequestError(ctx.Conn, msg.RequestID, "HARDWARE_NOT_OWNED", "hardware client is not bound to this machine or cannot accept the reply")
		}
		return false, nil
	}
	return true, writeWSRequestError(ctx.Conn, msg.RequestID, "UNAVAILABLE", "hardware reply routing is unavailable")
}

// MachineClientReplyResult is the selected-client routing result shared by
// the websocket transport and its hardware gateway implementation. Keeping
// this interface-shaped value here avoids a package import cycle: device
// runtime code already depends on ws.
type MachineClientReplyResult struct {
	Queued int
	Reason string
}

// machineClientReplyErrorCode maps the device gateway's selected-client
// result to its stable WebSocket request error. The fallback Count-only
// interface remains for test and extension compatibility.
func machineClientReplyErrorCode(r MachineClientReplyResult) string {
	switch r.Reason {
	case "HARDWARE_DISABLED", "HARDWARE_OFFLINE", "HARDWARE_UNSUPPORTED", "HARDWARE_STALE_REPLY", "HARDWARE_NOT_OWNED":
		return r.Reason
	default:
		return "HARDWARE_UNAVAILABLE"
	}
}

func machineClientReplyErrorMessage(r MachineClientReplyResult) string {
	switch machineClientReplyErrorCode(r) {
	case "HARDWARE_DISABLED":
		return "hardware is disabled for this machine"
	case "HARDWARE_OFFLINE":
		return "hardware is offline; wait for it to reconnect before remote playback"
	case "HARDWARE_UNSUPPORTED":
		return "hardware cannot play this audio with its current capabilities"
	case "HARDWARE_STALE_REPLY":
		return "hardware session changed; retry after the device reconnects"
	case "HARDWARE_NOT_OWNED":
		return "hardware client is not bound to this machine"
	default:
		return "hardware cannot accept the reply right now"
	}
}

func (g *Gateway) cleanupConnection(ctx *ConnContext) {
	if ctx == nil {
		return
	}
	log.Printf("[ws] cleanupConnection: role=%s machine_id=%s user_id=%s", ctx.Role, ctx.MachineID, ctx.UserID)
	if ctx.Role == "machine" && ctx.MachineID != "" {
		log.Printf("[ws] cleanupConnection: unbinding machine_id=%s and marking offline", ctx.MachineID)
		_ = g.Devices.UnbindDesktop(context.Background(), ctx.MachineID, ctx)
		_ = g.Sessions.MarkMachineOffline(context.Background(), ctx.MachineID)
		// Release any IM gateway locks held by this connection, using the
		// claim seq to avoid releasing a newer claim from a reconnected client.
		if len(ctx.gwClaimSeqs) > 0 {
			for _, plugin := range g.IMGatewayPlugins {
				plugin.ReleaseAllForTenantMachineBySeq(ctx.TenantID, ctx.MachineID, ctx.gwClaimSeqs)
			}
		} else {
			// Fallback for connections that never claimed any gateway.
			for _, plugin := range g.IMGatewayPlugins {
				plugin.ReleaseAllForTenantMachine(ctx.TenantID, ctx.MachineID)
			}
		}
		// Clean up cached machine data.
		g.mu.Lock()
		delete(g.toolsByMachine, ctx.MachineID)
		g.mu.Unlock()
		g.broadcastMachineEvent(ctx.MachineID, map[string]any{
			"type":       "machine.offline",
			"tenant_id":  ctx.TenantID,
			"machine_id": ctx.MachineID,
			"ts":         time.Now().Unix(),
			"payload": map[string]any{
				"status": "offline",
			},
		})
		if g.DeviceNotifyFunc.OnDisconnect != nil {
			go g.DeviceNotifyFunc.OnDisconnect(ctx.UserID, ctx.MachineID, ctx.MachineName)
		}
		return
	}
	g.removeViewer(ctx)
}

func (g *Gateway) removeViewer(ctx *ConnContext) {
	if ctx == nil || ctx.Role != "viewer" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for machineID, watchers := range g.viewersByMachine {
		delete(watchers, ctx)
		if len(watchers) == 0 {
			delete(g.viewersByMachine, machineID)
		}
	}
	for sessionID, watchers := range g.viewersBySession {
		delete(watchers, ctx)
		if len(watchers) == 0 {
			delete(g.viewersBySession, sessionID)
		}
	}
}

func (g *Gateway) getProjectsForMachine(machineID string) []map[string]any {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return cloneProjects(g.projectsByMachine[machineID])
}

func cloneProjects(items []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		cloned := make(map[string]any, len(item))
		for k, v := range item {
			cloned[k] = v
		}
		out = append(out, cloned)
	}
	return out
}

func writeWSJSON(conn *websocket.Conn, v any) error { return conn.WriteJSON(v) }

func writeWSError(conn *websocket.Conn, code, message string) error {
	return conn.WriteJSON(map[string]any{"type": "error", "payload": map[string]any{"code": code, "message": message, "ts": time.Now().Unix()}})
}

func writeWSRequestError(conn *websocket.Conn, requestID, code, message string) error {
	return conn.WriteJSON(map[string]any{"type": "error", "request_id": requestID, "payload": map[string]any{"code": code, "message": message, "ts": time.Now().Unix()}})
}

func writeAck(conn *websocket.Conn, requestID string) error {
	return writeAckPayload(conn, requestID, map[string]any{"ok": true})
}

func writeAckPayload(conn *websocket.Conn, requestID string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{"ok": true}
	}
	if _, ok := payload["ok"]; !ok {
		payload["ok"] = true
	}
	return conn.WriteJSON(map[string]any{"type": "ack", "request_id": requestID, "payload": payload})
}

// injectHubConfig adds hub_config (including digital_employee_authorization) to
// an ack payload. Used by both handleMachineHello and handleMachineHeartbeat.
func (g *Gateway) injectHubConfig(ackPayload map[string]any, userID, tenantID, caller string) {
	if g.ConfigProvider == nil {
		return
	}
	cfg, err := g.ConfigProvider.GetHeartbeatConfig(context.Background(), userID, tenantID)
	if err != nil {
		log.Printf("[ws] %s: hub config unavailable for user_id=%s tenant_id=%s: %v", caller, userID, tenantID, err)
		return
	}
	if cfg != nil {
		ackPayload["hub_config"] = cfg
	}
}

// handleDeviceProfileUpdate processes a device.profile_update message from a
// MaClaw client, forwarding the profile data to the Coordinator's cache.
func (g *Gateway) handleDeviceProfileUpdate(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	if g.DeviceProfileUpdater != nil {
		g.DeviceProfileUpdater(ctx.TenantID, ctx.UserID, msg.Payload)
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

// handleNotificationAck processes a notification.ack message from a client.
// The payload is expected to be {"notification_id": "...", "action": "read"}.
// It delegates to NotificationService.MarkRead to persist the read status.
func (g *Gateway) handleNotificationAck(ctx *ConnContext, msg Envelope) {
	if ctx.MachineID == "" {
		log.Printf("[ws] handleNotificationAck: no machine_id in context, ignoring")
		return
	}
	if g.NotificationService == nil {
		log.Printf("[ws] handleNotificationAck: NotificationService not configured, ignoring")
		return
	}

	var payload struct {
		NotificationID string `json:"notification_id"`
		Action         string `json:"action"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleNotificationAck: invalid payload: %v", err)
		return
	}

	if payload.NotificationID == "" {
		log.Printf("[ws] handleNotificationAck: empty notification_id, ignoring")
		return
	}
	if payload.Action != "read" {
		log.Printf("[ws] handleNotificationAck: unsupported action %q, ignoring", payload.Action)
		return
	}

	bgCtx := context.Background()
	if err := g.NotificationService.MarkRead(bgCtx, ctx.MachineID, payload.NotificationID); err != nil {
		log.Printf("[ws] handleNotificationAck: MarkRead failed (machine=%s notif=%s): %v",
			ctx.MachineID, payload.NotificationID, err)
	}
}

// handleMachineNicknameUpdate processes a runtime nickname change from a machine.
// It checks for Alias conflicts with other same-user online devices before
// accepting the nickname. On conflict the request is rejected with an error.
func (g *Gateway) handleMachineNicknameUpdate(ctx *ConnContext, msg Envelope) error {
	if ctx.Role != "machine" {
		return writeWSError(ctx.Conn, "FORBIDDEN", "Machine role required")
	}
	var payload struct {
		Nickname string `json:"nickname"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid machine.nickname_update payload")
	}
	nickname := strings.TrimSpace(payload.Nickname)
	if nickname == "" {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "nickname must not be empty")
	}
	log.Printf("[ws] handleMachineNicknameUpdate: machine_id=%s nickname=%q", ctx.MachineID, nickname)

	// Check for Alias conflict with other same-user online machines.
	if conflict := g.Devices.CheckAliasConflict(ctx.MachineID, ctx.UserID, nickname); conflict {
		log.Printf("[ws] handleMachineNicknameUpdate: nickname=%q conflicts for machine_id=%s, rejecting", nickname, ctx.MachineID)
		return writeWSError(ctx.Conn, "NICKNAME_CONFLICT", fmt.Sprintf("昵称 %q 已被您的另一台在线设备使用", nickname))
	}

	g.Devices.SetAlias(context.Background(), ctx.MachineID, nickname)
	return writeAck(ctx.Conn, msg.RequestID)
}
