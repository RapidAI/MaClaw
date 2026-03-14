package ws

import (
	"context"
	"encoding/json"
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
	MarkMachineOffline(ctx context.Context, machineID string) error
	GetSnapshot(userID, machineID, sessionID string) (*session.SessionCacheEntry, bool)
	ListByMachine(ctx context.Context, userID, machineID string) ([]*session.SessionCacheEntry, error)
}

type identityService interface {
	AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error)
	AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error)
}

type Gateway struct {
	Identity identityService
	Devices  DeviceBinder
	Sessions SessionService

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

func (g *Gateway) HandleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx := &ConnContext{Conn: conn}
	defer g.cleanupConnection(ctx)

	for {
		var msg Envelope
		if err := conn.ReadJSON(&msg); err != nil {
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
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid auth.machine payload")
	}
	principal, err := g.Identity.AuthenticateMachine(context.Background(), payload.MachineID, payload.MachineToken)
	if err != nil {
		return writeWSError(ctx.Conn, "UNAUTHORIZED", "Machine authentication failed")
	}
	ctx.Role = "machine"
	ctx.UserID = principal.UserID
	ctx.MachineID = principal.MachineID
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
			"summary":       entry.Summary,
			"preview":       entry.Preview,
			"recent_events": entry.RecentEvents,
			"host_online":   entry.HostOnline,
			"updated_at":    entry.UpdatedAt.Unix(),
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
			"session_id":  entry.SessionID,
			"machine_id":  entry.MachineID,
			"user_id":     entry.UserID,
			"summary":     entry.Summary,
			"preview":     entry.Preview,
			"host_online": entry.HostOnline,
			"updated_at":  entry.UpdatedAt.Unix(),
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
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid machine.hello payload")
	}
	if err := g.Devices.MarkOnline(context.Background(), ctx.MachineID, payload); err != nil {
		return writeWSError(ctx.Conn, "INTERNAL_ERROR", err.Error())
	}
	return writeAck(ctx.Conn, msg.RequestID)
}

func (g *Gateway) handleMachineHeartbeat(ctx *ConnContext, msg Envelope) error {
	var payload MachineHeartbeatPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return writeWSError(ctx.Conn, "INVALID_MESSAGE", "Invalid machine.heartbeat payload")
	}
	if err := g.Devices.Heartbeat(context.Background(), ctx.MachineID, payload); err != nil {
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
	return writeAck(ctx.Conn, msg.RequestID)
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

func (g *Gateway) cleanupConnection(ctx *ConnContext) {
	if ctx == nil {
		return
	}
	if ctx.Role == "machine" && ctx.MachineID != "" {
		_ = g.Devices.UnbindDesktop(context.Background(), ctx.MachineID, ctx)
		_ = g.Sessions.MarkMachineOffline(context.Background(), ctx.MachineID)
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
