package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

type fakeMachineSender struct {
	err      error
	messages []map[string]any
}

func (f *fakeMachineSender) SendToMachine(machineID string, msg any) error {
	_ = machineID
	if f.err != nil {
		return f.err
	}
	if typed, ok := msg.(map[string]any); ok {
		f.messages = append(f.messages, typed)
	}
	return nil
}

func newHTTPAPITestServices(t *testing.T) (*auth.IdentityService, *device.Service, *session.Service) {
	t.Helper()

	dbPath := t.TempDir() + `\hub-httpapi-test.db`
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               dbPath,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		_ = provider.Close()
	})

	st := sqlite.NewStore(provider)
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	deviceSvc := device.NewService(st.Machines, device.NewRuntime())
	sessionSvc := session.NewService(session.NewCache(), st.Sessions)
	return identity, deviceSvc, sessionSvc
}

func issueViewerToken(t *testing.T, identity *auth.IdentityService, email string) (string, *auth.EnrollmentResult) {
	t.Helper()
	ctx := context.Background()
	enroll, err := identity.StartEnrollment(ctx, email, "office-pc", "windows", "", "")
	if err != nil {
		t.Fatalf("StartEnrollment: %v", err)
	}
	req, err := identity.RequestEmailLogin(ctx, email)
	if err != nil {
		t.Fatalf("RequestEmailLogin: %v", err)
	}
	prefix := "Use this confirm URL for development: "
	if !strings.HasPrefix(req.Message, prefix) {
		t.Fatalf("unexpected confirm message: %q", req.Message)
	}
	parsed, err := url.Parse(strings.TrimPrefix(req.Message, prefix))
	if err != nil {
		t.Fatalf("parse confirm url: %v", err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatal("missing login token")
	}
	viewerToken, _, err := identity.ConfirmEmailLogin(ctx, token)
	if err != nil {
		t.Fatalf("ConfirmEmailLogin: %v", err)
	}
	return viewerToken, enroll
}

func TestListMachinesHandlerReturnsViewerMachines(t *testing.T) {
	identity, deviceSvc, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "viewer@example.com")

	deviceSvc.BindDesktop(enroll.MachineID, &ws.ConnContext{UserID: enroll.UserID, Role: "machine"})
	if err := deviceSvc.MarkOnline(context.Background(), enroll.MachineID, ws.MachineHelloPayload{
		Name:                 "office-pc",
		Platform:             "windows",
		Hostname:             "office-host",
		Arch:                 "amd64",
		AppVersion:           "1.0.0",
		HeartbeatIntervalSec: 10,
	}); err != nil {
		t.Fatalf("MarkOnline: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rr := httptest.NewRecorder()

	ListMachinesHandler(identity, deviceSvc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, enroll.MachineID) {
		t.Fatalf("expected machine id in response, body=%s", body)
	}
	if !strings.Contains(body, "office-pc") {
		t.Fatalf("expected machine name in response, body=%s", body)
	}
	if !strings.Contains(body, `"platform":"windows"`) {
		t.Fatalf("expected platform in response, body=%s", body)
	}
}

func TestListSessionsHandlerReturnsViewerSessions(t *testing.T) {
	identity, deviceSvc, sessionSvc := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "sessions@example.com")

	deviceSvc.BindDesktop(enroll.MachineID, &ws.ConnContext{UserID: enroll.UserID, Role: "machine"})
	if err := sessionSvc.OnSessionCreated(context.Background(), enroll.MachineID, enroll.UserID, "sess_list_1", map[string]any{
		"tool":         "claude",
		"title":        "demo-project",
		"project_path": "D:/workprj/demo-project",
		"status":       "starting",
	}); err != nil {
		t.Fatalf("OnSessionCreated: %v", err)
	}
	if err := sessionSvc.OnSessionSummary(context.Background(), enroll.MachineID, enroll.UserID, "sess_list_1", session.SessionSummary{
		SessionID:       "sess_list_1",
		Tool:            "claude",
		Title:           "demo-project",
		Status:          "busy",
		Severity:        "info",
		CurrentTask:     "Inspecting project files",
		ProgressSummary: "Reading relevant source files",
		UpdatedAt:       time.Now().Unix(),
	}); err != nil {
		t.Fatalf("OnSessionSummary: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?machine_id="+url.QueryEscape(enroll.MachineID), nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rr := httptest.NewRecorder()

	ListSessionsHandler(identity, sessionSvc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "sess_list_1") || !strings.Contains(body, "demo-project") {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestListMachinesHandlerRejectsMissingBearerToken(t *testing.T) {
	identity, deviceSvc, _ := newHTTPAPITestServices(t)

	req := httptest.NewRequest(http.MethodGet, "/api/machines", nil)
	rr := httptest.NewRecorder()

	ListMachinesHandler(identity, deviceSvc).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("expected unauthorized response, body=%s", rr.Body.String())
	}
}

func TestGetSessionHandlerReturnsSnapshot(t *testing.T) {
	identity, deviceSvc, sessionSvc := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "snapshot@example.com")

	deviceSvc.BindDesktop(enroll.MachineID, &ws.ConnContext{UserID: enroll.UserID, Role: "machine"})
	if err := sessionSvc.OnSessionCreated(context.Background(), enroll.MachineID, enroll.UserID, "sess_1", map[string]any{
		"tool":         "claude",
		"title":        "demo-project",
		"project_path": "D:/workprj/demo-project",
		"status":       "starting",
	}); err != nil {
		t.Fatalf("OnSessionCreated: %v", err)
	}
	if err := sessionSvc.OnSessionSummary(context.Background(), enroll.MachineID, enroll.UserID, "sess_1", session.SessionSummary{
		SessionID:       "sess_1",
		Tool:            "claude",
		Title:           "demo-project",
		Status:          "busy",
		Severity:        "info",
		CurrentTask:     "Inspecting project files",
		ProgressSummary: "Reading relevant source files",
		UpdatedAt:       time.Now().Unix(),
	}); err != nil {
		t.Fatalf("OnSessionSummary: %v", err)
	}
	if err := sessionSvc.OnSessionPreviewDelta(context.Background(), enroll.MachineID, enroll.UserID, "sess_1", session.SessionPreviewDelta{
		SessionID:   "sess_1",
		OutputSeq:   1,
		AppendLines: []string{"Reading main.go"},
		UpdatedAt:   time.Now().Unix(),
	}); err != nil {
		t.Fatalf("OnSessionPreviewDelta: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/session?machine_id="+url.QueryEscape(enroll.MachineID)+"&session_id=sess_1", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rr := httptest.NewRecorder()

	GetSessionHandler(identity, sessionSvc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "demo-project") || !strings.Contains(body, "Reading main.go") {
		t.Fatalf("unexpected body=%s", body)
	}
}

func TestSessionInputHandlerRoutesCommandToMachine(t *testing.T) {
	identity, _, sessionSvc := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "control@example.com")

	if err := sessionSvc.OnSessionCreated(context.Background(), enroll.MachineID, enroll.UserID, "sess_control_1", map[string]any{
		"tool":         "claude",
		"title":        "control-project",
		"project_path": "D:/workprj/control-project",
		"status":       "busy",
	}); err != nil {
		t.Fatalf("OnSessionCreated: %v", err)
	}

	sender := &fakeMachineSender{}
	body := strings.NewReader(`{"machine_id":"` + enroll.MachineID + `","session_id":"sess_control_1","text":"Continue."}`)
	req := httptest.NewRequest(http.MethodPost, "/api/session/input", body)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rr := httptest.NewRecorder()

	SessionInputHandler(identity, sessionSvc, sender).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if len(sender.messages) != 1 {
		t.Fatalf("expected 1 routed message, got %d", len(sender.messages))
	}
	msg := sender.messages[0]
	if msg["type"] != "session.input" {
		t.Fatalf("expected session.input, got %#v", msg["type"])
	}
	payload, ok := msg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload map, got %#v", msg["payload"])
	}
	if payload["text"] != "Continue." {
		t.Fatalf("unexpected payload text: %#v", payload["text"])
	}
}

func TestSessionInterruptHandlerReturnsConflictWhenMachineOffline(t *testing.T) {
	identity, _, sessionSvc := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "offline@example.com")

	if err := sessionSvc.OnSessionCreated(context.Background(), enroll.MachineID, enroll.UserID, "sess_control_2", map[string]any{
		"tool":         "claude",
		"title":        "control-project",
		"project_path": "D:/workprj/control-project",
		"status":       "busy",
	}); err != nil {
		t.Fatalf("OnSessionCreated: %v", err)
	}

	sender := &fakeMachineSender{err: device.ErrMachineOffline}
	body := strings.NewReader(`{"machine_id":"` + enroll.MachineID + `","session_id":"sess_control_2"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/session/interrupt", body)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rr := httptest.NewRecorder()

	SessionInterruptHandler(identity, sessionSvc, sender).ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "MACHINE_OFFLINE") {
		t.Fatalf("expected MACHINE_OFFLINE response, body=%s", rr.Body.String())
	}
}

func TestSessionKillHandlerRejectsUnknownSession(t *testing.T) {
	identity, _, sessionSvc := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "missing@example.com")

	sender := &fakeMachineSender{err: errors.New("should not be called")}
	body := strings.NewReader(`{"machine_id":"` + enroll.MachineID + `","session_id":"sess_missing"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/session/kill", body)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rr := httptest.NewRecorder()

	SessionKillHandler(identity, sessionSvc, sender).ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if len(sender.messages) != 0 {
		t.Fatalf("sender should not be called for missing session")
	}
}
