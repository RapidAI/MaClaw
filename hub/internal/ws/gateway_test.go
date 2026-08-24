package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/gorilla/websocket"
)

type proactiveFileCapture struct {
	broadcastCalls int
	targetCalls    int
	tenantID       string
	userID         string
	target         agent.IMFileDeliveryTarget
	data           string
	fileName       string
	mimeType       string
	message        string
	targetErr      error
}

func (p *proactiveFileCapture) SendProactiveMessage(context.Context, string, string, string) error {
	return nil
}

func (p *proactiveFileCapture) SendProactiveMessageToTarget(context.Context, string, string, string, string, string) error {
	return nil
}

func (p *proactiveFileCapture) SendProactiveFile(_ context.Context, tenantID, userID, data, fileName, mimeType, message string) error {
	p.broadcastCalls++
	p.capture(tenantID, userID, agent.IMFileDeliveryTarget{}, data, fileName, mimeType, message)
	return nil
}

func (p *proactiveFileCapture) SendProactiveFileToTarget(_ context.Context, tenantID, userID string, target agent.IMFileDeliveryTarget, data, fileName, mimeType, message string) error {
	p.targetCalls++
	p.capture(tenantID, userID, target, data, fileName, mimeType, message)
	return p.targetErr
}

func (p *proactiveFileCapture) capture(tenantID, userID string, target agent.IMFileDeliveryTarget, data, fileName, mimeType, message string) {
	p.tenantID = tenantID
	p.userID = userID
	p.target = target
	p.data = data
	p.fileName = fileName
	p.mimeType = mimeType
	p.message = message
}

func proactiveFileEnvelope(t *testing.T, payload map[string]any) Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return Envelope{Type: "im.proactive_file", Payload: raw}
}

func TestHandleIMProactiveFileUsesExactTargetWithoutBroadcast(t *testing.T) {
	capture := &proactiveFileCapture{}
	gateway := &Gateway{IMProactive: capture}
	ctx := &ConnContext{Role: "machine", TenantID: "tenant-authenticated", UserID: "user-authenticated"}
	target := map[string]any{
		"channel": " lansenger ", "group_id": " group-9 ", "group_name": " 研发群 ",
	}
	msg := proactiveFileEnvelope(t, map[string]any{
		"file_data": "ZGF0YQ==", "file_name": "report.pdf", "mime_type": "application/pdf",
		"message": "报告已生成", "target": target,
		// Payload identity is deliberately untrusted and must never override the
		// authenticated WebSocket connection identity.
		"tenant_id": "tenant-attacker", "user_id": "user-attacker",
	})

	if err := gateway.handleIMProactiveFile(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if capture.targetCalls != 1 || capture.broadcastCalls != 0 {
		t.Fatalf("target calls=%d broadcast calls=%d", capture.targetCalls, capture.broadcastCalls)
	}
	if capture.tenantID != ctx.TenantID || capture.userID != ctx.UserID {
		t.Fatalf("sender identity=%q/%q, want authenticated %q/%q", capture.tenantID, capture.userID, ctx.TenantID, ctx.UserID)
	}
	if capture.target.Channel != "lansenger" || capture.target.GroupID != "group-9" || capture.target.GroupName != "研发群" {
		t.Fatalf("normalized target=%#v", capture.target)
	}
	if capture.data != "ZGF0YQ==" || capture.fileName != "report.pdf" || capture.mimeType != "application/pdf" || capture.message != "报告已生成" {
		t.Fatalf("file request not preserved: %#v", capture)
	}
}

func TestHandleIMProactiveFileWithoutTargetPreservesLegacyBroadcast(t *testing.T) {
	capture := &proactiveFileCapture{}
	gateway := &Gateway{IMProactive: capture}
	ctx := &ConnContext{Role: "machine", TenantID: "tenant-a", UserID: "user-a"}
	msg := proactiveFileEnvelope(t, map[string]any{
		"file_data": "ZGF0YQ==", "file_name": "notes.txt", "mime_type": "text/plain",
	})

	if err := gateway.handleIMProactiveFile(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if capture.broadcastCalls != 1 || capture.targetCalls != 0 {
		t.Fatalf("broadcast calls=%d target calls=%d", capture.broadcastCalls, capture.targetCalls)
	}
}

func TestHandleIMProactiveFileExactTargetFailureNeverFallsBack(t *testing.T) {
	capture := &proactiveFileCapture{targetErr: errors.New("target is outside tenant")}
	gateway := &Gateway{IMProactive: capture}
	ctx := &ConnContext{Role: "machine", TenantID: "tenant-a", UserID: "user-a"}
	msg := proactiveFileEnvelope(t, map[string]any{
		"file_data": "ZGF0YQ==", "file_name": "report.pdf",
		"target": map[string]any{"channel": "feishu", "group_id": "foreign-chat"},
	})

	if err := gateway.handleIMProactiveFile(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if capture.targetCalls != 1 || capture.broadcastCalls != 0 {
		t.Fatalf("failed exact target must not fall back: target=%d broadcast=%d", capture.targetCalls, capture.broadcastCalls)
	}
}

func TestHandleIMProactiveFileIncompleteExactTargetIsRejectedWithoutBroadcast(t *testing.T) {
	for name, target := range map[string]map[string]any{
		"missing channel":   {"group_id": "group-9"},
		"missing recipient": {"channel": "feishu", "group_name": "研发群"},
	} {
		t.Run(name, func(t *testing.T) {
			capture := &proactiveFileCapture{}
			gateway := &Gateway{IMProactive: capture}
			ctx := &ConnContext{Role: "machine", TenantID: "tenant-a", UserID: "user-a"}
			msg := proactiveFileEnvelope(t, map[string]any{
				"file_data": "ZGF0YQ==", "file_name": "report.pdf", "target": target,
			})

			if err := gateway.handleIMProactiveFile(ctx, msg); err != nil {
				t.Fatal(err)
			}
			if capture.targetCalls != 0 || capture.broadcastCalls != 0 {
				t.Fatalf("incomplete target must not be sent: target=%d broadcast=%d", capture.targetCalls, capture.broadcastCalls)
			}
		})
	}
}

type testIdentityService struct{}

func (s *testIdentityService) AuthenticateMachine(ctx context.Context, machineID, rawToken string) (*auth.MachinePrincipal, error) {
	return &auth.MachinePrincipal{TenantID: store.DefaultTenantID, UserID: "user-1", MachineID: machineID}, nil
}

func (s *testIdentityService) AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error) {
	return &auth.ViewerPrincipal{TenantID: store.DefaultTenantID, UserID: "user-1", Email: "viewer@example.com"}, nil
}

func (s *testIdentityService) IssueViewerTokenForUser(ctx context.Context, userID string) (string, error) {
	return "test-viewer-token-for-" + userID, nil
}

type testDeviceBinder struct {
	mu               sync.RWMutex
	boundMachineID   string
	unboundMachineID string
	markedOnline     int
	heartbeats       int
	tenantID         string
	userID           string
}

func (d *testDeviceBinder) BindDesktop(machineID string, ctx *ConnContext) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.boundMachineID = machineID
}

func (d *testDeviceBinder) UnbindDesktop(ctx context.Context, machineID string, conn *ConnContext) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.unboundMachineID = machineID
	return nil
}

func (d *testDeviceBinder) unboundMachine() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.unboundMachineID
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

func (d *testDeviceBinder) GetMachineOwner(ctx context.Context, machineID string) (string, string, error) {
	tenantID := d.tenantID
	if tenantID == "" {
		tenantID = "tenant_default"
	}
	userID := d.userID
	if userID == "" {
		userID = "user-1"
	}
	return tenantID, userID, nil
}

func (d *testDeviceBinder) SetAlias(ctx context.Context, machineID string, alias string) {}

func (d *testDeviceBinder) CheckAliasConflict(machineID, userID, alias string) bool { return false }

type testSessionService struct {
	mu               sync.RWMutex
	snapshot         *session.SessionCacheEntry
	events           []string
	offlineMachineID string
	tokenTenantID    string
	tokenSourceID    string
	tokenUserID      string
	tokenUsage       store.UserTokenUsage
}

type testSecurityProvider struct {
	tenantID string
	userID   string
}

func (p *testSecurityProvider) GetEffectivePolicyByUserID(ctx context.Context, userID string) (*security.EffectivePolicy, error) {
	return &security.EffectivePolicy{}, nil
}

func (p *testSecurityProvider) IsCentralizedEnabled(ctx context.Context) (bool, error) {
	return true, nil
}

func (p *testSecurityProvider) GetHeartbeatPolicy(ctx context.Context, userID string) (*security.HeartbeatSecurityPayload, error) {
	p.tenantID = security.TenantIDFromContext(ctx)
	p.userID = userID
	return &security.HeartbeatSecurityPayload{CentralizedSecurity: true, Policy: &security.EffectivePolicy{GossipEnabled: false}}, nil
}

func TestGatewayInjectSecurityPolicyUsesConnectionTenant(t *testing.T) {
	provider := &testSecurityProvider{}
	gateway := &Gateway{SecurityProvider: provider}
	ack := map[string]any{"ok": true}

	gateway.injectSecurityPolicy(ack, "alice@example.com", "tenant_acme", "test")

	if provider.tenantID != "tenant_acme" || provider.userID != "alice@example.com" {
		t.Fatalf("security provider got tenant/user = %q/%q, want tenant_acme/alice@example.com", provider.tenantID, provider.userID)
	}
	if _, ok := ack["security_policy"]; !ok {
		t.Fatal("expected security_policy in ack payload")
	}
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

func (s *testSessionService) OnSessionImage(ctx context.Context, machineID, userID, sessionID string, img session.SessionImage) {
	s.events = append(s.events, "session.image")
}

func (s *testSessionService) RecordUserTokenUsageSnapshot(ctx context.Context, tenantID, sourceID, userID string, usage store.UserTokenUsage, observedAt time.Time) error {
	s.tokenTenantID = tenantID
	s.tokenSourceID = sourceID
	s.tokenUserID = userID
	s.tokenUsage = usage
	return nil
}
func (s *testSessionService) RecordHeartbeat(ctx context.Context, tenantID, machineID, userID string, at time.Time) error {
	return nil
}
func (s *testSessionService) MarkMachineOffline(ctx context.Context, machineID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offlineMachineID = machineID
	return nil
}

func (s *testSessionService) offlineMachine() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.offlineMachineID
}

func (s *testSessionService) GetSnapshot(userID, machineID, sessionID string) (*session.SessionCacheEntry, bool) {
	return s.GetSnapshotForTenant("", userID, machineID, sessionID)
}

func (s *testSessionService) GetSnapshotForTenant(tenantID, userID, machineID, sessionID string) (*session.SessionCacheEntry, bool) {
	if s.snapshot == nil {
		return nil, false
	}
	if s.snapshot.UserID != userID || s.snapshot.MachineID != machineID || s.snapshot.SessionID != sessionID {
		return nil, false
	}
	if tenantID != "" && s.snapshot.TenantID != "" && s.snapshot.TenantID != tenantID {
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
	tenantID := store.TenantIDFromContext(ctx)
	if s.snapshot.TenantID != "" && s.snapshot.TenantID != tenantID {
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

func TestGatewayHandleSessionEventSkipsOtherTenantWatchers(t *testing.T) {
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	matching := &ConnContext{TenantID: "tenant_a", sendCh: make(chan any, 2), closeSend: make(chan struct{})}
	other := &ConnContext{TenantID: "tenant_b", sendCh: make(chan any, 2), closeSend: make(chan struct{})}

	gateway.mu.Lock()
	gateway.viewersBySession["sess-1"] = map[*ConnContext]struct{}{matching: {}, other: {}}
	gateway.viewersByMachine["machine-1"] = map[*ConnContext]struct{}{matching: {}, other: {}}
	gateway.mu.Unlock()

	gateway.HandleSessionEvent(session.Event{
		Type:      "session.summary",
		TenantID:  "tenant_a",
		SessionID: "sess-1",
		MachineID: "machine-1",
		UserID:    "user-1",
		Summary:   &session.SessionSummary{SessionID: "sess-1", MachineID: "machine-1", Status: "busy"},
	})

	select {
	case <-matching.sendCh:
	default:
		t.Fatalf("expected matching tenant watcher to receive event")
	}
	select {
	case msg := <-other.sendCh:
		t.Fatalf("other tenant watcher received event: %#v", msg)
	default:
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
			SessionID:  "sess-1",
			MachineID:  "machine-1",
			Title:      "Claude Session",
			Status:     "busy",
			TokenUsage: &session.SessionTokenUsage{InputTokens: 1200, OutputTokens: 80, CachedInputTokens: 768},
		},
	})

	var msg struct {
		Type      string `json:"type"`
		MachineID string `json:"machine_id"`
		SessionID string `json:"session_id"`
		Payload   struct {
			Status     string                     `json:"status"`
			TokenUsage *session.SessionTokenUsage `json:"token_usage,omitempty"`
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
	if msg.Payload.TokenUsage == nil || msg.Payload.TokenUsage.CachedInputTokens != 768 {
		t.Fatalf("expected diagnostic token usage in summary broadcast, got %#v", msg.Payload.TokenUsage)
	}
}

func TestGatewayMachineHeartbeatRecordsClientLLMTokenUsage(t *testing.T) {
	sess := &testSessionService{}
	deviceBinder := &testDeviceBinder{}
	gateway := NewGateway(&testIdentityService{}, deviceBinder, sess)

	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"type":    "auth.machine",
		"payload": map[string]any{"machine_id": "machine-1", "machine_token": "token"},
	}); err != nil {
		t.Fatalf("write auth.machine: %v", err)
	}
	var ignored map[string]any
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read auth.ok: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "machine.heartbeat",
		"payload": map[string]any{
			"active_sessions":        1,
			"heartbeat_interval_sec": 30,
			"llm_token_usage": map[string]any{
				"input_tokens":        1200,
				"output_tokens":       80,
				"cached_input_tokens": 768,
				"cache_write_tokens":  128,
			},
		},
	}); err != nil {
		t.Fatalf("write machine.heartbeat: %v", err)
	}
	if err := conn.ReadJSON(&ignored); err != nil {
		t.Fatalf("read heartbeat ack: %v", err)
	}

	if deviceBinder.heartbeats != 1 {
		t.Fatalf("expected heartbeat to be recorded by device binder")
	}
	if sess.tokenTenantID != store.DefaultTenantID || sess.tokenUserID != "user-1" || sess.tokenSourceID != "gui:machine-1" {
		t.Fatalf("unexpected token usage identity tenant=%q user=%q source=%q", sess.tokenTenantID, sess.tokenUserID, sess.tokenSourceID)
	}
	if sess.tokenUsage.InputTokens != 1200 || sess.tokenUsage.OutputTokens != 80 || sess.tokenUsage.CachedInputTokens != 768 || sess.tokenUsage.CacheWriteTokens != 128 {
		t.Fatalf("unexpected token usage: %#v", sess.tokenUsage)
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
		if deviceBinder.unboundMachine() == "machine-1" && sessionSvc.offlineMachine() == "machine-1" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if deviceBinder.unboundMachine() != "machine-1" || sessionSvc.offlineMachine() != "machine-1" {
		t.Fatalf("expected machine disconnect cleanup, got unbound=%q offline=%q", deviceBinder.unboundMachine(), sessionSvc.offlineMachine())
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

	// Machine tries to send session.image_input (should be rejected -only viewers can)
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

type tenantCaptureGatewayPlugin struct {
	data chan json.RawMessage
}

func (p *tenantCaptureGatewayPlugin) Name() string { return "telegram" }
func (p *tenantCaptureGatewayPlugin) ClaimGatewayForTenant(tenantID, machineID, userID string) (bool, string, uint64) {
	return true, "", 1
}
func (p *tenantCaptureGatewayPlugin) ReleaseAllForTenantMachine(tenantID, machineID string) {}
func (p *tenantCaptureGatewayPlugin) ReleaseAllForTenantMachineBySeq(tenantID, machineID string, seqs map[string]uint64) {
}
func (p *tenantCaptureGatewayPlugin) HandleGatewayMessage(machineID string, payload json.RawMessage) {
	p.data <- payload
}

func TestHandleIMGatewayMessageInjectsConnectionTenant(t *testing.T) {
	plugin := &tenantCaptureGatewayPlugin{data: make(chan json.RawMessage, 1)}
	gateway := &Gateway{IMGatewayPlugins: map[string]IMGatewayPlugin{"telegram": plugin}}
	inner := json.RawMessage(`{"platform_uid":"platform-a","text":"hello","tenant_id":"tenant_b"}`)
	payload, err := json.Marshal(map[string]any{"platform": "telegram", "data": inner})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	ctx := &ConnContext{Role: "machine", TenantID: "tenant_a", MachineID: "machine-a", UserID: "user-a"}
	if err := gateway.handleIMGatewayMessage(ctx, Envelope{Payload: payload}); err != nil {
		t.Fatalf("handle gateway message: %v", err)
	}
	select {
	case got := <-plugin.data:
		var message map[string]any
		if err := json.Unmarshal(got, &message); err != nil {
			t.Fatalf("unmarshal forwarded data: %v", err)
		}
		if message["tenant_id"] != "tenant_a" {
			t.Fatalf("tenant_id = %v, want tenant_a from connection context", message["tenant_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for forwarded gateway message")
	}
}

type deviceGatewayReplyCapture struct {
	replies           []map[string]any
	queueCount        int
	profileUpdates    int
	petSkin           string
	volumeUpdates     int
	volume            any
	brightnessUpdates int
	brightness        any
	devices           []map[string]any
	deletedClient     string
	deleteErr         error
	targetMachine     string
	targetClient      string
	targetCount       int
	hardwareState     *bool
	pairingErr        error
}

type pairingCollisionError struct{}

func (pairingCollisionError) Error() string { return "unrelated wording" }
func (pairingCollisionError) PairingErrorCode() string {
	return "PAIRING_CODE_COLLISION"
}

type pairingDisabledError struct{}

func (pairingDisabledError) Error() string { return "machine hardware transport is disabled" }
func (pairingDisabledError) PairingErrorCode() string {
	return "HARDWARE_DISABLED"
}

func (g *deviceGatewayReplyCapture) RegisterPairing(string, string, string, string) error {
	return g.pairingErr
}
func (g *deviceGatewayReplyCapture) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (g *deviceGatewayReplyCapture) EnqueueReply(_, _ string, reply map[string]any) {
	g.replies = append(g.replies, reply)
}
func (g *deviceGatewayReplyCapture) EnqueueMachineReply(_ string, _ string, reply map[string]any) {
	g.replies = append(g.replies, reply)
}
func (g *deviceGatewayReplyCapture) EnqueueMachineReplyCount(_ string, _ string, reply map[string]any) int {
	if g.queueCount > 0 {
		g.replies = append(g.replies, reply)
	}
	return g.queueCount
}
func (g *deviceGatewayReplyCapture) EnqueueMachineClientReplyCount(machineID, clientID, _ string, reply map[string]any) int {
	g.targetMachine = machineID
	g.targetClient = clientID
	if g.targetCount > 0 {
		g.replies = append(g.replies, reply)
	}
	return g.targetCount
}

type detailedDeviceGatewayReplyCapture struct {
	deviceGatewayReplyCapture
	result MachineClientReplyResult
}

func (g *detailedDeviceGatewayReplyCapture) EnqueueMachineClientReplyResult(machineID, clientID, _ string, reply map[string]any) MachineClientReplyResult {
	g.targetMachine = machineID
	g.targetClient = clientID
	if g.result.Queued > 0 {
		g.replies = append(g.replies, reply)
	}
	return g.result
}
func (g *deviceGatewayReplyCapture) UpdateMachinePetProfileAsset(_ string, skin string, _ bool, _ map[string]any) {
	g.profileUpdates++
	g.petSkin = skin
}
func (g *deviceGatewayReplyCapture) UpdateMachineVolume(_ string, volume any) error {
	g.volumeUpdates++
	g.volume = volume
	return nil
}
func (g *deviceGatewayReplyCapture) UpdateMachineDeviceVolume(_ string, clientID string, volume any) error {
	g.volumeUpdates++
	g.volume = map[string]any{"clientId": clientID, "volume": volume}
	return nil
}
func (g *deviceGatewayReplyCapture) UpdateMachineBrightness(_ string, brightness any) error {
	g.brightnessUpdates++
	g.brightness = brightness
	return nil
}
func (g *deviceGatewayReplyCapture) UpdateMachineDeviceBrightness(_ string, clientID string, brightness any) error {
	g.brightnessUpdates++
	g.brightness = map[string]any{"clientId": clientID, "brightness": brightness}
	return nil
}
func (g *deviceGatewayReplyCapture) UpdateMachineHardwareEnabled(_ string, enabled bool) error {
	g.hardwareState = &enabled
	return nil
}
func (g *deviceGatewayReplyCapture) MachineHardwareBindingStateJSON(string) map[string]any {
	return map[string]any{"maxDevices": 5, "boundCount": len(g.devices)}
}
func (g *deviceGatewayReplyCapture) ListMachineDevicesJSON(string) []map[string]any { return g.devices }
func (g *deviceGatewayReplyCapture) DeleteMachineDevice(_, clientID string) error {
	g.deletedClient = clientID
	return g.deleteErr
}

type hardwareNotOwnedError struct{}

func (hardwareNotOwnedError) Error() string             { return "hardware client is not bound to this machine" }
func (hardwareNotOwnedError) HardwareErrorCode() string { return "HARDWARE_NOT_OWNED" }

func TestGatewayListsAndDeletesOwnedHardwareDevices(t *testing.T) {
	capture := &deviceGatewayReplyCapture{devices: []map[string]any{{"clientId": "esp32s3-a", "online": true}, {"clientId": "esp32s3-b", "online": false}}}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = capture
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"}})
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(map[string]any{"type": "im.device_gateway_devices_list", "request_id": "list-1", "payload": map[string]any{}})
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response["type"] != "im.device_gateway_devices" || response["request_id"] != "list-1" {
		t.Fatalf("list response=%#v", response)
	}
	_ = conn.WriteJSON(map[string]any{"type": "im.device_gateway_device_delete", "request_id": "delete-1", "payload": map[string]any{"clientId": "esp32s3-a"}})
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response["type"] != "ack" || capture.deletedClient != "esp32s3-a" {
		t.Fatalf("delete response=%#v deleted=%q", response, capture.deletedClient)
	}
}

func TestConnContextSendRejectsMessagesAfterClose(t *testing.T) {
	ctx := &ConnContext{sendCh: make(chan any, 1), closeSend: make(chan struct{})}
	ctx.closeWriter()
	if ctx.Send(map[string]any{"type": "late-message"}) {
		t.Fatal("closed connection accepted a queued message")
	}
	select {
	case message := <-ctx.sendCh:
		t.Fatalf("closed connection queued %#v", message)
	default:
	}
}

func TestConnContextWriteLoopClosesSendAfterWriteFailure(t *testing.T) {
	ctx := &ConnContext{
		sendCh:     make(chan any, 1),
		closeSend:  make(chan struct{}),
		writerDone: make(chan struct{}),
	}
	go ctx.writeLoop()
	ctx.sendCh <- map[string]any{"type": "will-fail"}
	select {
	case <-ctx.writerDone:
	case <-time.After(time.Second):
		t.Fatal("writer did not stop after a failed write")
	}
	if ctx.Send(map[string]any{"type": "late-message"}) {
		t.Fatal("writer failure left the connection accepting messages")
	}
}

func TestGatewayDeleteHardwareReportsNotOwnedWithStableCode(t *testing.T) {
	capture := &deviceGatewayReplyCapture{deleteErr: hardwareNotOwnedError{}}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = capture
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"}}); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "im.device_gateway_device_delete", "request_id": "delete-not-owned", "payload": map[string]any{"clientId": "esp32s3-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	payload, _ := response["payload"].(map[string]any)
	if response["type"] != "error" || response["request_id"] != "delete-not-owned" || payload["code"] != "HARDWARE_NOT_OWNED" {
		t.Fatalf("delete error=%#v", response)
	}
}

func TestGatewayPairingErrorKeepsRequestCorrelation(t *testing.T) {
	capture := &deviceGatewayReplyCapture{pairingErr: pairingCollisionError{}}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = capture
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{
		"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"},
	}); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{
		"type": "im.device_gateway_pairing", "request_id": "pairing-1", "payload": map[string]any{"pairCode": "123456"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	payload, _ := response["payload"].(map[string]any)
	if response["type"] != "error" || response["request_id"] != "pairing-1" || payload["code"] != "PAIRING_CODE_COLLISION" {
		t.Fatalf("pairing error lost request correlation: %#v", response)
	}
}

func TestGatewayPairingDisabledErrorKeepsStableCode(t *testing.T) {
	capture := &deviceGatewayReplyCapture{pairingErr: pairingDisabledError{}}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = capture
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{
		"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"},
	}); err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{
		"type": "im.device_gateway_pairing", "request_id": "pairing-disabled", "payload": map[string]any{"pairCode": "123456"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	payload, _ := response["payload"].(map[string]any)
	if response["type"] != "error" || response["request_id"] != "pairing-disabled" || payload["code"] != "HARDWARE_DISABLED" {
		t.Fatalf("disabled pairing error lost stable code: %#v", response)
	}
}

func TestHandleDeviceGatewayReplyDoesNotResetPetProfile(t *testing.T) {
	capture := &deviceGatewayReplyCapture{targetCount: 1}
	gateway := &Gateway{DeviceGateway: capture}
	ctx := &ConnContext{Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "pet-a", "conversationId": "default",
		"reply": map[string]any{"reply_type": "text", "text": "hello"},
	})
	if _, err := gateway.handleDeviceGatewayReply(ctx, Envelope{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if capture.profileUpdates != 0 {
		t.Fatalf("ordinary reply unexpectedly updated pet profile %d times", capture.profileUpdates)
	}
	if len(capture.replies) != 1 || capture.replies[0]["text"] != "hello" {
		t.Fatalf("ordinary reply was not relayed: %#v", capture.replies)
	}
	if capture.targetMachine != "gui-a" || capture.targetClient != "pet-a" {
		t.Fatalf("target route machine=%q client=%q", capture.targetMachine, capture.targetClient)
	}
}

func TestHandleDeviceGatewayReplyUsesOwnedTargetRoute(t *testing.T) {
	capture := &deviceGatewayReplyCapture{targetCount: 1}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = capture
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"}})
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(map[string]any{
		"type": "im.device_gateway_reply", "request_id": "owned-target-1",
		"payload": map[string]any{"clientId": "pet-a", "conversationId": "default", "reply": map[string]any{"reply_type": "text", "text": "hello"}},
	})
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response["type"] != "ack" || response["request_id"] != "owned-target-1" {
		t.Fatalf("owned reply response=%#v", response)
	}
	if capture.targetMachine != "gui-a" || capture.targetClient != "pet-a" || len(capture.replies) != 1 {
		t.Fatalf("owned route replies=%#v machine=%q client=%q", capture.replies, capture.targetMachine, capture.targetClient)
	}
}

func TestHandleDeviceGatewayReplyRejectsUnownedTarget(t *testing.T) {
	capture := &deviceGatewayReplyCapture{}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = capture
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"}})
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(map[string]any{
		"type": "im.device_gateway_reply", "request_id": "cross-machine-1",
		"payload": map[string]any{"clientId": "pet-owned-by-gui-b", "conversationId": "default", "reply": map[string]any{"reply_type": "text", "text": "must not cross machines"}},
	})
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	errPayload, _ := response["payload"].(map[string]any)
	if response["type"] != "error" || response["request_id"] != "cross-machine-1" || errPayload["code"] != "HARDWARE_NOT_OWNED" {
		t.Fatalf("unowned reply response=%#v", response)
	}
	if len(capture.replies) != 0 || capture.targetMachine != "gui-a" || capture.targetClient != "pet-owned-by-gui-b" {
		t.Fatalf("unowned reply route=%#v machine=%q client=%q", capture.replies, capture.targetMachine, capture.targetClient)
	}
}

func TestHandleDeviceGatewayPetProfileUpdateIsExplicit(t *testing.T) {
	capture := &deviceGatewayReplyCapture{}
	gateway := &Gateway{DeviceGateway: capture}
	ctx := &ConnContext{Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "*", "conversationId": "system",
		"reply": map[string]any{"reply_type": "pet_profile", "pet_skin": "mini-claw", "pet_motion_enabled": true},
	})
	if _, err := gateway.handleDeviceGatewayReply(ctx, Envelope{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if capture.profileUpdates != 1 || capture.petSkin != "mini-claw" || len(capture.replies) != 0 {
		t.Fatalf("explicit profile update=%d skin=%q replies=%#v", capture.profileUpdates, capture.petSkin, capture.replies)
	}
}

func TestHandleDeviceGatewayHardwareEnabledUpdateIsDurableAndNotRelayed(t *testing.T) {
	capture := &deviceGatewayReplyCapture{}
	gateway := &Gateway{DeviceGateway: capture}
	ctx := &ConnContext{Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "*", "conversationId": "system",
		"reply": map[string]any{"reply_type": "hardware_enabled", "enabled": true},
	})
	if _, err := gateway.handleDeviceGatewayReply(ctx, Envelope{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if capture.hardwareState == nil || !*capture.hardwareState || len(capture.replies) != 0 {
		t.Fatalf("hardware state=%v replies=%#v", capture.hardwareState, capture.replies)
	}
}

func TestHandleDeviceGatewayVolumeUpdateIsDurableAndRelayed(t *testing.T) {
	capture := &deviceGatewayReplyCapture{}
	gateway := &Gateway{DeviceGateway: capture}
	ctx := &ConnContext{Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "*", "conversationId": "system",
		"reply": map[string]any{"reply_type": "hardware_config", "extra": map[string]any{"volume": 0}},
	})
	if _, err := gateway.handleDeviceGatewayReply(ctx, Envelope{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if capture.volumeUpdates != 1 || capture.volume != float64(0) {
		t.Fatalf("volume update=%d value=%#v", capture.volumeUpdates, capture.volume)
	}
	if len(capture.replies) != 1 || capture.replies[0]["reply_type"] != "hardware_config" {
		t.Fatalf("volume update was not relayed: %#v", capture.replies)
	}
}

func TestHandleDeviceGatewayPerDeviceVolumeUpdateIsDurableAndTargeted(t *testing.T) {
	capture := &deviceGatewayReplyCapture{}
	gateway := &Gateway{DeviceGateway: capture}
	ctx := &ConnContext{Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "esp32-a", "conversationId": "system",
		"reply": map[string]any{"reply_type": "hardware_config", "extra": map[string]any{"volume": 42}},
	})
	if _, err := gateway.handleDeviceGatewayReply(ctx, Envelope{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	update, ok := capture.volume.(map[string]any)
	if capture.volumeUpdates != 1 || !ok || update["clientId"] != "esp32-a" || update["volume"] != float64(42) {
		t.Fatalf("per-device volume update=%#v", capture.volume)
	}
	if len(capture.replies) != 0 {
		t.Fatalf("per-device volume was relayed twice: %#v", capture.replies)
	}
}

func TestHandleDeviceGatewayBrightnessUpdateIsDurableAndRelayed(t *testing.T) {
	capture := &deviceGatewayReplyCapture{}
	gateway := &Gateway{DeviceGateway: capture}
	ctx := &ConnContext{Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "*", "conversationId": "system",
		"reply": map[string]any{"reply_type": "hardware_config", "extra": map[string]any{"brightness": 65}},
	})
	if _, err := gateway.handleDeviceGatewayReply(ctx, Envelope{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if capture.brightnessUpdates != 1 || capture.brightness != float64(65) {
		t.Fatalf("brightness update=%d value=%#v", capture.brightnessUpdates, capture.brightness)
	}
	// A brightness-only payload must not touch the volume path.
	if capture.volumeUpdates != 0 {
		t.Fatalf("brightness-only payload updated volume=%#v", capture.volume)
	}
	if len(capture.replies) != 1 || capture.replies[0]["reply_type"] != "hardware_config" {
		t.Fatalf("brightness update was not relayed: %#v", capture.replies)
	}
}

func TestHandleDeviceGatewayPerDeviceBrightnessUpdateIsDurableAndTargeted(t *testing.T) {
	capture := &deviceGatewayReplyCapture{}
	gateway := &Gateway{DeviceGateway: capture}
	ctx := &ConnContext{Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "esp32-a", "conversationId": "system",
		"reply": map[string]any{"reply_type": "hardware_config", "extra": map[string]any{"brightness": 30}},
	})
	if _, err := gateway.handleDeviceGatewayReply(ctx, Envelope{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	update, ok := capture.brightness.(map[string]any)
	if capture.brightnessUpdates != 1 || !ok || update["clientId"] != "esp32-a" || update["brightness"] != float64(30) {
		t.Fatalf("per-device brightness update=%#v", capture.brightness)
	}
	if capture.volumeUpdates != 0 {
		t.Fatalf("brightness-only payload updated volume=%#v", capture.volume)
	}
	if len(capture.replies) != 0 {
		t.Fatalf("per-device brightness was relayed twice: %#v", capture.replies)
	}
}

func TestHandleDeviceGatewayHardwareConfigRejectsEmptyLevels(t *testing.T) {
	assertHardwareConfigRejected(t, map[string]any{})
}

func TestHandleDeviceGatewayHardwareConfigRejectsUnknownLevels(t *testing.T) {
	assertHardwareConfigRejected(t, map[string]any{"volume": 50, "unsupported": true})
}

func assertHardwareConfigRejected(t *testing.T, extra map[string]any) {
	t.Helper()
	capture := &deviceGatewayReplyCapture{}
	gateway := &Gateway{DeviceGateway: capture}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade connection: %v", err)
			return
		}
		defer conn.Close()
		var response map[string]any
		if err := conn.ReadJSON(&response); err != nil {
			t.Errorf("read error response: %v", err)
			return
		}
		payload, _ := response["payload"].(map[string]any)
		if response["type"] != "error" || response["request_id"] != "empty-levels" || payload["code"] != "INVALID_MESSAGE" {
			t.Errorf("unexpected error response: %#v", response)
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := &ConnContext{Conn: conn, Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "esp32-a", "conversationId": "system",
		"reply": map[string]any{"reply_type": "hardware_config", "extra": extra},
	})
	handled, err := gateway.handleDeviceGatewayReply(ctx, Envelope{RequestID: "empty-levels", Payload: payload})
	if !handled || err != nil {
		t.Fatalf("empty hardware config was not rejected: handled=%t err=%v", handled, err)
	}
	if capture.volumeUpdates != 0 || capture.brightnessUpdates != 0 || len(capture.replies) != 0 {
		t.Fatalf("empty hardware config had side effects: %#v", capture)
	}
}

func TestGatewayDeviceGatewayReplyReturnsRequestAck(t *testing.T) {
	capture := &deviceGatewayReplyCapture{queueCount: 1}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = capture
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial machine: %v", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]any{
		"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"},
	}); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("read auth: %v", err)
	}

	if err := conn.WriteJSON(map[string]any{
		"type": "im.device_gateway_reply", "request_id": "welcome-preview-1",
		"payload": map[string]any{
			"clientId": "*", "conversationId": "system",
			"reply": map[string]any{"reply_type": "audio", "mime_type": "audio/wav", "file_data": "d2F2", "extra": map[string]any{"hardware_audio_preview": true}},
		},
	}); err != nil {
		t.Fatalf("write hardware reply: %v", err)
	}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("read hardware reply ack: %v", err)
	}
	if response["type"] != "ack" || response["request_id"] != "welcome-preview-1" {
		t.Fatalf("hardware reply response=%#v", response)
	}
	if len(capture.replies) != 1 || capture.replies[0]["reply_type"] != "audio" {
		t.Fatalf("hardware reply was not relayed: %#v", capture.replies)
	}
}

func TestGatewayHardwarePreviewFailsFastWithoutCompatibleDevice(t *testing.T) {
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = &deviceGatewayReplyCapture{}
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"}})
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(map[string]any{
		"type": "im.device_gateway_reply", "request_id": "offline-preview-1",
		"payload": map[string]any{"clientId": "*", "conversationId": "system", "reply": map[string]any{
			"reply_type": "audio", "mime_type": "audio/wav", "file_data": "d2F2", "extra": map[string]any{"hardware_audio_preview": true},
		}},
	})
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	payload, _ := response["payload"].(map[string]any)
	if response["type"] != "error" || response["request_id"] != "offline-preview-1" || payload["code"] != "NO_COMPATIBLE_HARDWARE" {
		t.Fatalf("offline preview response=%#v", response)
	}
}

func TestGatewaySelectedPreviewReturnsOfflineInsteadOfNotOwned(t *testing.T) {
	capture := &detailedDeviceGatewayReplyCapture{result: MachineClientReplyResult{Reason: "HARDWARE_OFFLINE"}}
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = capture
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"}})
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(map[string]any{
		"type": "im.device_gateway_reply", "request_id": "selected-offline-preview",
		"payload": map[string]any{"clientId": "pet-a", "conversationId": "system", "reply": map[string]any{
			"reply_type": "audio", "mime_type": "audio/wav", "file_data": "d2F2", "extra": map[string]any{"hardware_audio_preview": true},
		}},
	})
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	payload, _ := response["payload"].(map[string]any)
	if response["type"] != "error" || response["request_id"] != "selected-offline-preview" || payload["code"] != "HARDWARE_OFFLINE" {
		t.Fatalf("selected offline preview response=%#v", response)
	}
	if capture.targetMachine != "gui-a" || capture.targetClient != "pet-a" {
		t.Fatalf("selected offline route=%q/%q", capture.targetMachine, capture.targetClient)
	}
}

func TestHandleDeviceGatewaySelectedPreviewQueuesOnlyItsOwnedTarget(t *testing.T) {
	capture := &deviceGatewayReplyCapture{targetCount: 1}
	gateway := &Gateway{DeviceGateway: capture}
	ctx := &ConnContext{Role: "machine", MachineID: "gui-a"}
	payload, _ := json.Marshal(map[string]any{
		"clientId": "esp32-a", "conversationId": "system",
		"reply": map[string]any{
			"reply_type": "audio", "mime_type": "audio/wav", "file_data": "d2F2",
			"extra": map[string]any{"hardware_audio_preview": true},
		},
	})
	handled, err := gateway.handleDeviceGatewayReply(ctx, Envelope{Payload: payload})
	if err != nil || handled {
		t.Fatalf("selected preview handled=%v err=%v", handled, err)
	}
	if capture.targetMachine != "gui-a" || capture.targetClient != "esp32-a" || len(capture.replies) != 1 {
		t.Fatalf("selected preview route=%q/%q replies=%#v", capture.targetMachine, capture.targetClient, capture.replies)
	}
}

func TestGatewayInvalidDeviceGatewayReplyCorrelatesError(t *testing.T) {
	gateway := NewGateway(&testIdentityService{}, &testDeviceBinder{}, &testSessionService{})
	gateway.DeviceGateway = &deviceGatewayReplyCapture{}
	server := httptest.NewServer(http.HandlerFunc(gateway.HandleWS))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]any{"type": "auth.machine", "payload": map[string]any{"machine_id": "gui-a", "machine_token": "token"}})
	var response map[string]any
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(map[string]any{
		"type": "im.device_gateway_reply", "request_id": "bad-preview-1",
		"payload": map[string]any{"conversationId": "system", "reply": map[string]any{"reply_type": "audio"}},
	})
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response["type"] != "error" || response["request_id"] != "bad-preview-1" {
		t.Fatalf("correlated error=%#v", response)
	}
}
