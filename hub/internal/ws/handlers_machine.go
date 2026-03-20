package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/gorilla/websocket"
)

type ConnContext struct {
	Conn      *websocket.Conn
	Role      string
	UserID    string
	MachineID string
	ViewerID  string
}

type MachineHelloPayload struct {
	Name                 string         `json:"name"`
	Platform             string         `json:"platform"`
	Hostname             string         `json:"hostname,omitempty"`
	Arch                 string         `json:"arch,omitempty"`
	AppVersion           string         `json:"app_version,omitempty"`
	HeartbeatIntervalSec int            `json:"heartbeat_interval_sec,omitempty"`
	Capabilities         map[string]any `json:"capabilities,omitempty"`
}

type MachineHeartbeatPayload struct {
	ActiveSessions       int    `json:"active_sessions,omitempty"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec,omitempty"`
	AppVersion           string `json:"app_version,omitempty"`
	LLMConfigured        *bool  `json:"llm_configured,omitempty"`
}

type DeviceBinder interface {
	BindDesktop(machineID string, ctx *ConnContext)
	UnbindDesktop(ctx context.Context, machineID string, conn *ConnContext) error
	MarkOnline(ctx context.Context, machineID string, hello MachineHelloPayload) error
	Heartbeat(ctx context.Context, machineID string, heartbeat MachineHeartbeatPayload) error
	SendToMachine(machineID string, msg any) error
}

type SessionService interface {
	OnSessionCreated(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error
	OnSessionSummary(ctx context.Context, machineID, userID, sessionID string, summary session.SessionSummary) error
	OnSessionPreviewDelta(ctx context.Context, machineID, userID, sessionID string, delta session.SessionPreviewDelta) error
	OnSessionImportantEvent(ctx context.Context, machineID, userID, sessionID string, event session.ImportantEvent) error
	OnSessionClosed(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error
	OnSessionImage(ctx context.Context, machineID, userID, sessionID string, img session.SessionImage)
	MarkMachineOffline(ctx context.Context, machineID string) error
	GetSnapshot(userID, machineID, sessionID string) (*session.SessionCacheEntry, bool)
	ListByMachine(ctx context.Context, userID, machineID string) ([]*session.SessionCacheEntry, error)
}

type identityService interface {
	AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error)
	AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error)
}

// IMAgentResponseHandler handles agent responses routed back from MaClaw clients.
type IMAgentResponseHandler interface {
	HandleAgentResponse(requestID string, resp json.RawMessage)
	HandleAgentProgress(requestID string, text string)
}

// IMProactiveSender sends proactive messages to a user's IM channels.
// Used for scheduled task notifications and other non-request-based messages.
type IMProactiveSender interface {
	SendProactiveMessage(ctx context.Context, userID, text string) error
	// SendProactiveFile sends a file to the user's IM channels (e.g. Swarm PDF documents).
	SendProactiveFile(ctx context.Context, userID, b64Data, fileName, mimeType, message string) error
}

// IMGatewayPlugin handles gateway claim/release and message forwarding for
// client-side IM gateways (QQ Bot, Telegram). Each platform registers one.
type IMGatewayPlugin interface {
	Name() string
	ClaimGateway(machineID, userID string) (bool, string)
	ReleaseAllForMachine(machineID string)
	HandleGatewayMessage(machineID string, payload json.RawMessage)
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

	// IMGatewayPlugins maps platform name → gateway plugin for client-side
	// IM gateways (QQ Bot, Telegram). Set via RegisterIMGatewayPlugin.
	IMGatewayPlugins map[string]IMGatewayPlugin

	mu                sync.RWMutex
	viewersByMachine  map[string]map[*ConnContext]struct{}
	viewersBySession  map[string]map[*ConnContext]struct{}
	projectsByMachine map[string][]map[string]any
}

func NewGateway(identity identityService, devices DeviceBinder, sessions SessionService) *Gateway {
	return &Gateway{
		Identity:          identity,
		Devices:           devices,
		Sessions:          sessions,
		viewersByMachine:  map[string]map[*ConnContext]struct{}{},
		viewersBySession:  map[string]map[*ConnContext]struct{}{},
		projectsByMachine: map[string][]map[string]any{},
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

func (g *Gateway) HandleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] HandleWS: upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("[ws] HandleWS: new WebSocket connection from %s", r.RemoteAddr)

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
	defer func() {
		close(pingDone)
		g.cleanupConnection(ctx)
	}()

	for {
		// Refresh read deadline on every incoming message so that machines
		// sending frequent data (summaries, preview deltas) don't time out
		// even if the pong is slightly delayed.
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))

		var msg Envelope
		if err := conn.ReadJSON(&msg); err != nil {
			log.Printf("[ws] HandleWS: ReadJSON error (role=%s machine_id=%s): %v", ctx.Role, ctx.MachineID, err)
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
		case "im.gateway_message":
			if err := g.handleIMGatewayMessage(ctx, msg); err != nil {
				return
			}
		default:
			_ = writeWSError(conn, "UNKNOWN_MESSAGE", "Unsupported message type")
		}
	}
}

func (g *Gateway) HandleSessionEvent(event session.Event) {
	g.mu.RLock()
	machineWatchers := make([]*ConnContext, 0, len(g.viewersByMachine[event.MachineID]))
	for watcher := range g.viewersByMachine[event.MachineID] {
		machineWatchers = append(machineWatchers, watcher)
	}
	watchers := make([]*ConnContext, 0, len(g.viewersBySession[event.SessionID]))
	for watcher := range g.viewersBySession[event.SessionID] {
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
		_ = writeWSJSON(watcher.Conn, msg)
	}

	if event.Type != "session.created" && event.Type != "session.closed" && event.Type != "session.summary" {
		return
	}

	for _, watcher := range machineWatchers {
		_ = writeWSJSON(watcher.Conn, msg)
	}
}

func (g *Gateway) broadcastMachineEvent(machineID string, payload map[string]any) {
	g.mu.RLock()
	machineWatchers := make([]*ConnContext, 0, len(g.viewersByMachine[machineID]))
	for watcher := range g.viewersByMachine[machineID] {
		machineWatchers = append(machineWatchers, watcher)
	}
	g.mu.RUnlock()

	for _, watcher := range machineWatchers {
		_ = writeWSJSON(watcher.Conn, payload)
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
	ctx.UserID = principal.UserID
	ctx.MachineID = principal.MachineID
	log.Printf("[ws] handleMachineAuth: auth OK machine_id=%s user_id=%s, calling BindDesktop", principal.MachineID, principal.UserID)
	g.Devices.BindDesktop(principal.MachineID, ctx)
	return writeWSJSON(ctx.Conn, map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine", "machine_id": principal.MachineID}})
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
	ctx.UserID = principal.UserID
	ctx.ViewerID = principal.Email
	return writeWSJSON(ctx.Conn, map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "viewer", "email": principal.Email}})
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
	entry, ok := g.Sessions.GetSnapshot(ctx.UserID, payload.MachineID, payload.SessionID)
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

	entries, err := g.Sessions.ListByMachine(context.Background(), ctx.UserID, payload.MachineID)
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

	command := map[string]any{
		"type":       "session.start",
		"request_id": msg.RequestID,
		"ts":         time.Now().Unix(),
		"machine_id": payload.MachineID,
		"payload": map[string]any{
			"tool":         payload.Tool,
			"project_id":   payload.ProjectID,
			"project_path": payload.ProjectPath,
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
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleMachineHeartbeat(ctx *ConnContext, msg Envelope) error {
	var payload MachineHeartbeatPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleMachineHeartbeat: invalid payload for machine_id=%s: %v", ctx.MachineID, err)
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid machine.heartbeat payload")
	}
	log.Printf("[ws] handleMachineHeartbeat: machine_id=%s sessions=%d interval=%d", ctx.MachineID, payload.ActiveSessions, payload.HeartbeatIntervalSec)
	if err := g.Devices.Heartbeat(context.Background(), ctx.MachineID, payload); err != nil {
		log.Printf("[ws] handleMachineHeartbeat: Heartbeat FAILED for machine_id=%s: %v", ctx.MachineID, err)
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
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
		"machine_id": ctx.MachineID,
		"ts":         time.Now().Unix(),
		"payload": map[string]any{
			"projects": cloneProjects(payload.Projects),
		},
	})
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleSessionCreated(ctx *ConnContext, msg Envelope) error {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.created payload")
	}
	if err := g.Sessions.OnSessionCreated(context.Background(), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleSessionSummary(ctx *ConnContext, msg Envelope) error {
	var payload session.SessionSummary
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.summary payload")
	}
	if err := g.Sessions.OnSessionSummary(context.Background(), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleSessionPreviewDelta(ctx *ConnContext, msg Envelope) error {
	var payload session.SessionPreviewDelta
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.preview_delta payload")
	}
	if err := g.Sessions.OnSessionPreviewDelta(context.Background(), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	// Skip ack for preview deltas — they are high-frequency fire-and-forget
	// messages. Omitting the ack reduces round-trip overhead and frees the
	// WebSocket write buffer for the next incoming delta.
	return nil
}

func (g *Gateway) handleSessionImportantEvent(ctx *ConnContext, msg Envelope) error {
	var payload session.ImportantEvent
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.important_event payload")
	}
	if err := g.Sessions.OnSessionImportantEvent(context.Background(), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleSessionClosed(ctx *ConnContext, msg Envelope) error {
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid session.closed payload")
	}
	if err := g.Sessions.OnSessionClosed(context.Background(), ctx.MachineID, ctx.UserID, msg.SessionID, payload); err != nil {
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
		_ = writeWSJSON(watcher.Conn, fwd)
	}

	// Dispatch to session listeners (e.g. Feishu notifier) so they can
	// forward the image to users who are watching via chat.
	var imgPayload session.SessionImage
	if err := json.Unmarshal(msg.Payload, &imgPayload); err == nil && imgPayload.Data != "" {
		g.Sessions.OnSessionImage(context.Background(), ctx.MachineID, ctx.UserID, msg.SessionID, imgPayload)
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
		_ = writeWSJSON(watcher.Conn, fwd)
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
		Text string `json:"text"`
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
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleIMProactiveMessage: parse error: %v", err)
		return nil
	}
	if payload.Text == "" {
		return nil
	}
	if err := g.IMProactive.SendProactiveMessage(context.Background(), ctx.UserID, payload.Text); err != nil {
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
		FileData string `json:"file_data"`
		FileName string `json:"file_name"`
		MimeType string `json:"mime_type"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[ws] handleIMProactiveFile: parse error: %v", err)
		return nil
	}
	if payload.FileData == "" || payload.FileName == "" {
		return nil
	}
	if err := g.IMProactive.SendProactiveFile(context.Background(), ctx.UserID, payload.FileData, payload.FileName, payload.MimeType, payload.Message); err != nil {
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
	ok, reason := plugin.ClaimGateway(ctx.MachineID, ctx.UserID)
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
	plugin.HandleGatewayMessage(ctx.MachineID, payload.Data)
	return nil
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
		// Release any IM gateway locks held by this machine.
		for _, plugin := range g.IMGatewayPlugins {
			plugin.ReleaseAllForMachine(ctx.MachineID)
		}
		g.broadcastMachineEvent(ctx.MachineID, map[string]any{
			"type":       "machine.offline",
			"machine_id": ctx.MachineID,
			"ts":         time.Now().Unix(),
			"payload": map[string]any{
				"status": "offline",
			},
		})
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

func writeAck(conn *websocket.Conn, requestID string) error {
	return conn.WriteJSON(map[string]any{"type": "ack", "request_id": requestID, "payload": map[string]any{"ok": true}})
}
