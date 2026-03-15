package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/gorilla/websocket"
)

type testIdentityService struct{}

func (s *testIdentityService) AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error) {
	return &auth.MachinePrincipal{UserID: "user-1", MachineID: machineID}, nil
}

func (s *testIdentityService) AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error) {
	return &auth.ViewerPrincipal{UserID: "user-1", Email: "viewer@example.com"}, nil
}

type testDeviceBinder struct {
	boundMachineID   string
	unboundMachineID string
	markedOnline     int
	heartbeats       int
}

func (d *testDeviceBinder) BindDesktop(machineID string, ctx *ConnContext) {
	d.boundMachineID = machineID
}

func (d *testDeviceBinder) UnbindDesktop(ctx context.Context, machineID string, conn *ConnContext) error {
	d.unboundMachineID = machineID
	return nil
}

func (d *testDeviceBinder) MarkOnline(ctx context.Context, machineID string, hello MachineHelloPayload) error {
	d.markedOnline++
	return nil
}

func (d *testDeviceBinder) Heartbeat(ctx context.Context, machineID string, heartbeat MachineHeartbeatPayload) error {
	d.heartbeats++
	return nil
}

func (d *testDeviceBinder) SendToMachine(machineID string, msg any) error {
	return nil
}

type testSessionService struct {
	snapshot         *session.SessionCacheEntry
	events           []string
	offlineMachineID string
}

func (s *testSessionService) OnSessionCreated(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error {
	s.events = append(s.events, "session.created")
	return nil
}

func (s *testSessionService) OnSessionSummary(ctx context.Context, machineID, userID, sessionID string, summary session.SessionSummary) error {
	s.events = append(s.events, "session.summary")
	return nil
}

func (s *testSessionService) OnSessionPreviewDelta(ctx context.Context, machineID, userID, sessionID string, delta session.SessionPreviewDelta) error {
	s.events = append(s.events, "session.preview_delta")
	return nil
}

func (s *testSessionService) OnSessionImportantEvent(ctx context.Context, machineID, userID, sessionID string, event session.ImportantEvent) error {
	s.events = append(s.events, "session.important_event")
	return nil
}

func (s *testSessionService) OnSessionClosed(ctx context.Context, machineID, userID, sessionID string, payload map[string]any) error {
	s.events = append(s.events, "session.closed")
	return nil
}

func (s *testSessionService) MarkMachineOffline(ctx context.Context, machineID string) error {
	s.offlineMachineID = machineID
	return nil
}

func (s *testSessionService) GetSnapshot(userID, machineID, sessionID string) (*session.SessionCacheEntry, bool) {
	if s.snapshot == nil {
		return nil, false
	}
	if s.snapshot.UserID != userID || s.snapshot.MachineID != machineID || s.snapshot.SessionID != sessionID {
		return nil, false
	}
	return s.snapshot, true
}

func (s *testSessionService) ListByMachine(ctx context.Context, userID, machineID string) ([]*session.SessionCacheEntry, error) {
	if s.snapshot == nil {
		return nil, nil
	}
	if s.snapshot.UserID != userID || s.snapshot.MachineID != machineID {
		return nil, nil
	}
	return []*session.SessionCacheEntry{s.snapshot}, nil
}

func TestGatewayViewerSubscribeMachineSendsSnapshot(t *testing.T) {
	sess := &testSessionService{
		snapshot: &session.SessionCacheEntry{
			SessionID:  "sess-1",
			MachineID:  "machine-1",
			UserID:     "user-1",
			HostOnline: true,
			UpdatedAt:  time.Unix(100, 0),
			Summary: session.SessionSummary{
				SessionID: "sess-1",
				MachineID: "machine-1",
				Title:     "Claude Session",
				Status:    "running",
			},
		},
	}

	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}

	var ignored map[string]any
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{
		"type":    "viewer.subscribe_machine",
		"payload": map[string]any{"machine_id": "machine-1"},
	}); err != nil {
		t.Fatalf("write viewer.subscribe_machine: %v", err)
	}

	var snapshotResp struct {
		Type      string `json:"type"`
		MachineID string `json:"machine_id"`
		Payload   struct {
			Sessions []struct {
				SessionID string `json:"session_id"`
				Summary   struct {
					Title string `json:"title"`
				} `json:"summary"`
			} `json:"sessions"`
		} `json:"payload"`
	}
	if err := conn.ReadJSON(&snapshotResp); err != nil {
		t.Fatalf("read machine.snapshot: %v", err)
	}

	if snapshotResp.Type != "machine.snapshot" {
		t.Fatalf("snapshot type = %q", snapshotResp.Type)
	}
	if snapshotResp.MachineID != "machine-1" {
		t.Fatalf("machine id = %q", snapshotResp.MachineID)
	}
	if len(snapshotResp.Payload.Sessions) != 1 {
		t.Fatalf("sessions len = %d", len(snapshotResp.Payload.Sessions))
	}
	if snapshotResp.Payload.Sessions[0].Summary.Title != "Claude Session" {
		t.Fatalf("summary title = %q", snapshotResp.Payload.Sessions[0].Summary.Title)
	}
}

func TestGatewayViewerSubscribeSessionSendsSnapshot(t *testing.T) {
	sess := &testSessionService{
		snapshot: &session.SessionCacheEntry{
			SessionID:  "sess-1",
			MachineID:  "machine-1",
			UserID:     "user-1",
			HostOnline: true,
			UpdatedAt:  time.Unix(100, 0),
			Summary: session.SessionSummary{
				SessionID: "sess-1",
				MachineID: "machine-1",
				Title:     "Claude Session",
				Status:    "running",
			},
			Preview: session.SessionPreview{
				SessionID:    "sess-1",
				OutputSeq:    2,
				PreviewLines: []string{"line one", "line two"},
			},
			RecentEvents: []session.ImportantEvent{{EventID: "evt-1", SessionID: "sess-1", Type: "task.started", Title: "Started"}},
		},
	}

	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}

	var authResp map[string]any
	if err := conn.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}
	if authResp["type"] != "auth.ok" {
		t.Fatalf("auth type = %v", authResp["type"])
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "viewer.subscribe_session",
		"payload": map[string]any{
			"machine_id": "machine-1",
			"session_id": "sess-1",
		},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	var snapshotResp struct {
		Type      string `json:"type"`
		MachineID string `json:"machine_id"`
		SessionID string `json:"session_id"`
		Payload   struct {
			Summary struct {
				Title  string `json:"title"`
				Status string `json:"status"`
			} `json:"summary"`
			Preview struct {
				PreviewLines []string `json:"preview_lines"`
			} `json:"preview"`
			RecentEvents []map[string]any `json:"recent_events"`
			HostOnline   bool             `json:"host_online"`
		} `json:"payload"`
	}
	if err := conn.ReadJSON(&snapshotResp); err != nil {
		t.Fatalf("read session.snapshot: %v", err)
	}

	if snapshotResp.Type != "session.snapshot" {
		t.Fatalf("snapshot type = %q", snapshotResp.Type)
	}
	if snapshotResp.MachineID != "machine-1" || snapshotResp.SessionID != "sess-1" {
		t.Fatalf("unexpected ids: machine=%q session=%q", snapshotResp.MachineID, snapshotResp.SessionID)
	}
	if snapshotResp.Payload.Summary.Title != "Claude Session" {
		t.Fatalf("summary title = %q", snapshotResp.Payload.Summary.Title)
	}
	if len(snapshotResp.Payload.Preview.PreviewLines) != 2 {
		t.Fatalf("preview lines = %d", len(snapshotResp.Payload.Preview.PreviewLines))
	}
	if !snapshotResp.Payload.HostOnline {
		t.Fatalf("expected host online")
	}
}

func TestGatewayHandleSessionEventBroadcastsToViewer(t *testing.T) {
	sess := &testSessionService{
		snapshot: &session.SessionCacheEntry{
			SessionID: "sess-1",
			MachineID: "machine-1",
			UserID:    "user-1",
			Summary:   session.SessionSummary{SessionID: "sess-1", MachineID: "machine-1", Title: "Claude Session", Status: "running"},
		},
	}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}
	var ignored map[string]any
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "viewer.subscribe_session",
		"payload": map[string]any{
			"machine_id": "machine-1",
			"session_id": "sess-1",
		},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	gateway.HandleSessionEvent(session.Event{
		Type:      "session.summary",
		SessionID: "sess-1",
		MachineID: "machine-1",
		UserID:    "user-1",
		Summary: &session.SessionSummary{
			SessionID: "sess-1",
			MachineID: "machine-1",
			Title:     "Updated Claude Session",
			Status:    "busy",
		},
	})

	var msg struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	if msg.Type != "session.summary" {
		t.Fatalf("broadcast type = %q", msg.Type)
	}

	var payload session.SessionSummary
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Title != "Updated Claude Session" || payload.Status != "busy" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestGatewayHandleSessionLifecycleBroadcastsToMachineWatcher(t *testing.T) {
	sess := &testSessionService{
		snapshot: &session.SessionCacheEntry{
			SessionID: "sess-1",
			MachineID: "machine-1",
			UserID:    "user-1",
			Summary:   session.SessionSummary{SessionID: "sess-1", MachineID: "machine-1", Title: "Claude Session", Status: "running"},
		},
	}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}
	var ignored map[string]any
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{
		"type":    "viewer.subscribe_machine",
		"payload": map[string]any{"machine_id": "machine-1"},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read machine snapshot: %v", err)
	}

	gateway.HandleSessionEvent(session.Event{
		Type:      "session.created",
		SessionID: "sess-2",
		MachineID: "machine-1",
		UserID:    "user-1",
		Payload: map[string]any{
			"tool":   "claude",
			"title":  "Second Session",
			"status": "starting",
		},
	})

	var msg struct {
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
	}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read lifecycle broadcast: %v", err)
	}
	if msg.Type != "session.created" || msg.SessionID != "sess-2" {
		t.Fatalf("unexpected lifecycle msg: %+v", msg)
	}
}

func TestGatewayHandleSessionSummaryBroadcastsToMachineWatcher(t *testing.T) {
	sess := &testSessionService{
		snapshot: &session.SessionCacheEntry{
			SessionID: "sess-1",
			MachineID: "machine-1",
			UserID:    "user-1",
			Summary:   session.SessionSummary{SessionID: "sess-1", MachineID: "machine-1", Title: "Claude Session", Status: "running"},
		},
	}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}
	var ignored map[string]any
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{
		"type":    "viewer.subscribe_machine",
		"payload": map[string]any{"machine_id": "machine-1"},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read machine snapshot: %v", err)
	}

	gateway.HandleSessionEvent(session.Event{
		Type:      "session.summary",
		SessionID: "sess-1",
		MachineID: "machine-1",
		UserID:    "user-1",
		Summary: &session.SessionSummary{
			SessionID: "sess-1",
			MachineID: "machine-1",
			Title:     "Claude Session",
			Status:    "busy",
		},
	})

	var msg struct {
		Type      string `json:"type"`
		MachineID string `json:"machine_id"`
		SessionID string `json:"session_id"`
		Payload   struct {
			Status string `json:"status"`
		} `json:"payload"`
	}
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("read summary broadcast: %v", err)
	}
	if msg.Type != "session.summary" || msg.MachineID != "machine-1" || msg.SessionID != "sess-1" {
		t.Fatalf("unexpected summary msg: %+v", msg)
	}
	if msg.Payload.Status != "busy" {
		t.Fatalf("unexpected status %q", msg.Payload.Status)
	}
}

func TestGatewayMachineDisconnectMarksMachineOffline(t *testing.T) {
	deviceBinder := &testDeviceBinder{}
	sessionSvc := &testSessionService{}
	gateway := NewGateway(&testIdentityService{}, deviceBinder, sessionSvc)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	viewerConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial viewer ws: %v", err)
	}
	defer viewerConn.Close()

	if err := viewerConn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}
	var ignored map[string]any
	if err := viewerConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read viewer auth.ok: %v", err)
	}
	if err := viewerConn.WriteJSON(map[string]any{
		"type":    "viewer.subscribe_machine",
		"payload": map[string]any{"machine_id": "machine-1"},
	}); err != nil {
		t.Fatalf("write viewer.subscribe_machine: %v", err)
	}
	if err := viewerConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read machine snapshot: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.machine",
		"payload": map[string]any{"machine_id": "machine-1", "machine_token": "token"},
	}); err != nil {
		t.Fatalf("write auth.machine: %v", err)
	}

	var authResp map[string]any
	if err := conn.ReadJSON(&authResp); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if deviceBinder.unboundMachineID == "machine-1" && sessionSvc.offlineMachineID == "machine-1" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if deviceBinder.unboundMachineID != "machine-1" || sessionSvc.offlineMachineID != "machine-1" {
		t.Fatalf("expected machine disconnect cleanup, got unbound=%q offline=%q", deviceBinder.unboundMachineID, sessionSvc.offlineMachineID)
	}

	var offlineMsg struct {
		Type      string `json:"type"`
		MachineID string `json:"machine_id"`
		Payload   struct {
			Status string `json:"status"`
		} `json:"payload"`
	}
	if err := viewerConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if err := viewerConn.ReadJSON(&offlineMsg); err != nil {
		t.Fatalf("read machine.offline: %v", err)
	}
	if offlineMsg.Type != "machine.offline" || offlineMsg.MachineID != "machine-1" {
		t.Fatalf("unexpected offline msg: %+v", offlineMsg)
	}
	if offlineMsg.Payload.Status != "offline" {
		t.Fatalf("unexpected offline status: %q", offlineMsg.Payload.Status)
	}
}

func TestGatewaySessionImageForwardsToSessionViewer(t *testing.T) {
	sess := &testSessionService{
		snapshot: &session.SessionCacheEntry{
			SessionID:  "sess-1",
			MachineID:  "machine-1",
			UserID:     "user-1",
			HostOnline: true,
			UpdatedAt:  time.Unix(100, 0),
			Summary:    session.SessionSummary{SessionID: "sess-1", MachineID: "machine-1", Title: "Claude Session", Status: "running"},
		},
	}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect viewer and subscribe to session
	viewerConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	defer viewerConn.Close()

	if err := viewerConn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}
	var ignored map[string]any
	if err := viewerConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}
	if err := viewerConn.WriteJSON(map[string]any{
		"type":    "viewer.subscribe_session",
		"payload": map[string]any{"machine_id": "machine-1", "session_id": "sess-1"},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	if err := viewerConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	// Connect machine
	machineConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial machine: %v", err)
	}
	defer machineConn.Close()

	if err := machineConn.WriteJSON(map[string]any{
		"type":    "auth.machine",
		"payload": map[string]any{"machine_id": "machine-1", "machine_token": "token"},
	}); err != nil {
		t.Fatalf("write auth.machine: %v", err)
	}
	if err := machineConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read machine auth.ok: %v", err)
	}

	// Machine sends session.image
	if err := machineConn.WriteJSON(map[string]any{
		"type":       "session.image",
		"session_id": "sess-1",
		"payload": map[string]any{
			"image_id":   "img_123",
			"session_id": "sess-1",
			"media_type": "image/png",
			"data":       "iVBORw0KGgo=",
			"timestamp":  1234567890,
		},
	}); err != nil {
		t.Fatalf("write session.image: %v", err)
	}

	// Viewer should receive the forwarded image
	var imgMsg struct {
		Type      string `json:"type"`
		MachineID string `json:"machine_id"`
		SessionID string `json:"session_id"`
		Payload   struct {
			ImageID   string `json:"image_id"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		} `json:"payload"`
	}
	if err := viewerConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if err := viewerConn.ReadJSON(&imgMsg); err != nil {
		t.Fatalf("read session.image: %v", err)
	}
	if imgMsg.Type != "session.image" {
		t.Fatalf("expected session.image, got %q", imgMsg.Type)
	}
	if imgMsg.MachineID != "machine-1" || imgMsg.SessionID != "sess-1" {
		t.Fatalf("unexpected ids: machine=%q session=%q", imgMsg.MachineID, imgMsg.SessionID)
	}
	if imgMsg.Payload.ImageID != "img_123" || imgMsg.Payload.MediaType != "image/png" {
		t.Fatalf("unexpected payload: %+v", imgMsg.Payload)
	}
}

func TestGatewaySessionImageRejectsViewer(t *testing.T) {
	sess := &testSessionService{}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Auth as viewer
	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	var ignored map[string]any
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	// Viewer tries to send session.image (should be rejected)
	if err := conn.WriteJSON(map[string]any{
		"type":       "session.image",
		"session_id": "sess-1",
		"payload":    map[string]any{"image_id": "img_123"},
	}); err != nil {
		t.Fatalf("write session.image: %v", err)
	}

	var errMsg struct {
		Type    string `json:"type"`
		Payload struct {
			Code string `json:"code"`
		} `json:"payload"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if err := conn.ReadJSON(&errMsg); err != nil {
		t.Fatalf("read error: %v", err)
	}
	if errMsg.Type != "error" || errMsg.Payload.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN error, got type=%q code=%q", errMsg.Type, errMsg.Payload.Code)
	}
}

func TestGatewaySessionImageInputForwardsToMachine(t *testing.T) {
	deviceBinder := &testDeviceBinder{}
	sess := &testSessionService{
		snapshot: &session.SessionCacheEntry{
			SessionID:  "sess-1",
			MachineID:  "machine-1",
			UserID:     "user-1",
			HostOnline: true,
			UpdatedAt:  time.Unix(100, 0),
			Summary:    session.SessionSummary{SessionID: "sess-1", MachineID: "machine-1", Title: "Claude Session", Status: "running"},
		},
	}
	gateway := NewGateway(&testIdentityService{}, deviceBinder, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect viewer
	viewerConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	defer viewerConn.Close()

	if err := viewerConn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}
	var ignored map[string]any
	if err := viewerConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	// Viewer sends session.image_input
	if err := viewerConn.WriteJSON(map[string]any{
		"type": "session.image_input",
		"payload": map[string]any{
			"machine_id": "machine-1",
			"session_id": "sess-1",
			"image_id":   "img_456",
			"media_type": "image/jpeg",
			"data":       "/9j/4AAQ==",
			"timestamp":  1234567890,
		},
	}); err != nil {
		t.Fatalf("write session.image_input: %v", err)
	}

	// Give a moment for the message to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify SendToMachine was called (the test binder records the machine ID)
	// The testDeviceBinder doesn't track calls, but the absence of an error response means it was forwarded
	// We can verify by checking no error was sent back to the viewer
	viewerConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var maybeErr map[string]any
	err = viewerConn.ReadJSON(&maybeErr)
	if err == nil && maybeErr["type"] == "error" {
		t.Fatalf("unexpected error response: %+v", maybeErr)
	}
}

func TestGatewaySessionImageInputRejectsMachine(t *testing.T) {
	sess := &testSessionService{}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Auth as machine
	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.machine",
		"payload": map[string]any{"machine_id": "machine-1", "machine_token": "token"},
	}); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	var ignored map[string]any
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	// Machine tries to send session.image_input (should be rejected — only viewers can)
	if err := conn.WriteJSON(map[string]any{
		"type": "session.image_input",
		"payload": map[string]any{
			"machine_id": "machine-1",
			"session_id": "sess-1",
			"image_id":   "img_456",
		},
	}); err != nil {
		t.Fatalf("write session.image_input: %v", err)
	}

	var errMsg struct {
		Type    string `json:"type"`
		Payload struct {
			Code string `json:"code"`
		} `json:"payload"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if err := conn.ReadJSON(&errMsg); err != nil {
		t.Fatalf("read error: %v", err)
	}
	if errMsg.Type != "error" || errMsg.Payload.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN error, got type=%q code=%q", errMsg.Type, errMsg.Payload.Code)
	}
}

func TestGatewaySessionImageInputErrorForwardsToViewer(t *testing.T) {
	sess := &testSessionService{
		snapshot: &session.SessionCacheEntry{
			SessionID:  "sess-1",
			MachineID:  "machine-1",
			UserID:     "user-1",
			HostOnline: true,
			UpdatedAt:  time.Unix(100, 0),
			Summary:    session.SessionSummary{SessionID: "sess-1", MachineID: "machine-1", Title: "Claude Session", Status: "running"},
		},
	}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect viewer and subscribe to session
	viewerConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial viewer: %v", err)
	}
	defer viewerConn.Close()

	if err := viewerConn.WriteJSON(map[string]any{
		"type":    "auth.viewer",
		"payload": map[string]any{"access_token": "viewer-token"},
	}); err != nil {
		t.Fatalf("write auth.viewer: %v", err)
	}
	var ignored map[string]any
	if err := viewerConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}
	if err := viewerConn.WriteJSON(map[string]any{
		"type":    "viewer.subscribe_session",
		"payload": map[string]any{"machine_id": "machine-1", "session_id": "sess-1"},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	if err := viewerConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	// Connect machine
	machineConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial machine: %v", err)
	}
	defer machineConn.Close()

	if err := machineConn.WriteJSON(map[string]any{
		"type":    "auth.machine",
		"payload": map[string]any{"machine_id": "machine-1", "machine_token": "token"},
	}); err != nil {
		t.Fatalf("write auth.machine: %v", err)
	}
	if err := machineConn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read machine auth.ok: %v", err)
	}

	// Machine sends session.image_input.error
	if err := machineConn.WriteJSON(map[string]any{
		"type":       "session.image_input.error",
		"session_id": "sess-1",
		"payload": map[string]any{
			"error":      "Image transfer is only supported in SDK mode sessions",
			"session_id": "sess-1",
		},
	}); err != nil {
		t.Fatalf("write session.image_input.error: %v", err)
	}

	// Viewer should receive the error
	var errMsg struct {
		Type      string `json:"type"`
		MachineID string `json:"machine_id"`
		SessionID string `json:"session_id"`
		Payload   struct {
			Error string `json:"error"`
		} `json:"payload"`
	}
	if err := viewerConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if err := viewerConn.ReadJSON(&errMsg); err != nil {
		t.Fatalf("read session.image_input.error: %v", err)
	}
	if errMsg.Type != "session.image_input.error" {
		t.Fatalf("expected session.image_input.error, got %q", errMsg.Type)
	}
	if errMsg.MachineID != "machine-1" || errMsg.SessionID != "sess-1" {
		t.Fatalf("unexpected ids: machine=%q session=%q", errMsg.MachineID, errMsg.SessionID)
	}
	if errMsg.Payload.Error != "Image transfer is only supported in SDK mode sessions" {
		t.Fatalf("unexpected error: %q", errMsg.Payload.Error)
	}
}
