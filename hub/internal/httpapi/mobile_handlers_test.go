package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
)

type mobileLLMTestSystemSettings struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *mobileLLMTestSystemSettings) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	return nil
}

func (s *mobileLLMTestSystemSettings) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return "", errors.New("setting not found")
	}
	return value, nil
}

func TestMobileRealtimeHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/realtime", nil)
	rec := httptest.NewRecorder()

	MobileRealtimeHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

type mobileRealtimeFakeWriter struct {
	err      error
	messages []map[string]any
}

func (w *mobileRealtimeFakeWriter) WriteJSON(v any) error {
	if w.err != nil {
		return w.err
	}
	msg, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	copied := make(map[string]any, len(msg))
	for k, value := range msg {
		copied[k] = value
	}
	w.messages = append(w.messages, copied)
	return nil
}

func TestMobileRealtimeDocumentTaskEventShape(t *testing.T) {
	event := mobileRealtimeDocumentTaskEvent("document_task", map[string]any{
		"job_id":   "mobexp_1",
		"status":   "ready",
		"draft_id": "draft_1",
	})

	if event["type"] != "document_task" || event["job_id"] != "mobexp_1" || event["status"] != "ready" {
		t.Fatalf("event = %#v, want document task fields", event)
	}
	if task, ok := event["task"].(map[string]any); !ok || task["draft_id"] != "draft_1" {
		t.Fatalf("task payload = %#v, want nested task", event["task"])
	}
}

func TestMobileRealtimeBackendSSHSessionEventShape(t *testing.T) {
	event := mobileRealtimeBackendSSHSessionEvent(map[string]any{
		"session_id":        "mobssh_1",
		"status":            "input_queued",
		"server_profile_id": "prod",
		"output_chunk":      "line 1",
		"output_seq":        int64(3),
	})

	if event["type"] != "ssh_session" || event["session_id"] != "mobssh_1" || event["status"] != "input_queued" {
		t.Fatalf("event = %#v, want backend ssh session fields", event)
	}
	if event["output_chunk"] != "line 1" || event["output_seq"] != int64(3) {
		t.Fatalf("event = %#v, want top-level output delta fields", event)
	}
	if session, ok := event["session"].(map[string]any); !ok || session["server_profile_id"] != "prod" {
		t.Fatalf("session payload = %#v, want nested session", event["session"])
	}
}

func TestMobileRealtimeBackendSSHTaskAndFileOperationEventShape(t *testing.T) {
	taskEvent := mobileRealtimeBackendSSHTaskEvent(map[string]any{
		"task_id":    "task-1",
		"session_id": "mobssh_1",
		"status":     "completed",
		"log_tail":   "done",
	})
	if taskEvent["type"] != "ssh_task" || taskEvent["task_id"] != "task-1" || taskEvent["session_id"] != "mobssh_1" || taskEvent["status"] != "completed" {
		t.Fatalf("task event = %#v, want top-level task fields", taskEvent)
	}
	if task, ok := taskEvent["task"].(map[string]any); !ok || task["log_tail"] != "done" {
		t.Fatalf("task payload = %#v, want nested task", taskEvent["task"])
	}

	fileEvent := mobileRealtimeBackendSSHFileOperationEvent(map[string]any{
		"operation_id":      "file-op-1",
		"session_id":        "mobssh_1",
		"status":            "completed",
		"bytes_transferred": int64(42),
	})
	if fileEvent["type"] != "ssh_file_operation" || fileEvent["operation_id"] != "file-op-1" || fileEvent["session_id"] != "mobssh_1" || fileEvent["status"] != "completed" {
		t.Fatalf("file event = %#v, want top-level operation fields", fileEvent)
	}
	if operation, ok := fileEvent["operation"].(map[string]any); !ok || operation["bytes_transferred"] != int64(42) {
		t.Fatalf("operation payload = %#v, want nested operation", fileEvent["operation"])
	}
}

func TestMobileRealtimeBroadcastTargetsUserAndCleansFailedWriters(t *testing.T) {
	mobileRealtimeClients.Lock()
	previous := mobileRealtimeClients.clients
	mobileRealtimeClients.clients = make(map[string]map[*mobileRealtimeClient]struct{})
	mobileRealtimeClients.Unlock()
	t.Cleanup(func() {
		mobileRealtimeClients.Lock()
		mobileRealtimeClients.clients = previous
		mobileRealtimeClients.Unlock()
	})

	target := &mobileRealtimeFakeWriter{}
	other := &mobileRealtimeFakeWriter{}
	broken := &mobileRealtimeFakeWriter{err: io.ErrClosedPipe}
	_, cleanupTarget := mobileRealtimeRegister("tenant-1", "user-1", target)
	_, cleanupOther := mobileRealtimeRegister("tenant-1", "user-2", other)
	_, _ = mobileRealtimeRegister("tenant-1", "user-1", broken)
	defer cleanupTarget()
	defer cleanupOther()

	mobileRealtimeBroadcast("tenant-1", "user-1", map[string]any{
		"type":    "digital_employee_task",
		"task_id": "mobve_1",
		"status":  "done",
	})

	if len(target.messages) != 1 {
		t.Fatalf("target messages = %d, want 1", len(target.messages))
	}
	if target.messages[0]["task_id"] != "mobve_1" || target.messages[0]["server_time"] == "" {
		t.Fatalf("target message = %#v, want task id and server time", target.messages[0])
	}
	if len(other.messages) != 0 {
		t.Fatalf("other messages = %#v, want none", other.messages)
	}
	mobileRealtimeClients.Lock()
	remaining := len(mobileRealtimeClients.clients[mobileRealtimeKey("tenant-1", "user-1")])
	mobileRealtimeClients.Unlock()
	if remaining != 1 {
		t.Fatalf("remaining target clients = %d, want failed writer removed", remaining)
	}
}

func TestMobileBootstrapHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap", nil)
	rec := httptest.NewRecorder()

	MobileBootstrapHandler(nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}

func TestMobileBootstrapPayloadIncludesServiceStatuses(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	payload := mobileBootstrapPayload(&auth.ViewerPrincipal{
		UserID:   "user-1",
		Email:    "u1@example.com",
		TenantID: "tenant-1",
	})

	user, ok := payload["user"].(map[string]any)
	if !ok || user["user_id"] != "user-1" || user["tenant_id"] != "tenant-1" {
		t.Fatalf("user payload = %#v, want viewer identity", payload["user"])
	}
	services, ok := payload["services"].(map[string]any)
	if !ok {
		t.Fatalf("services payload = %#v, want map", payload["services"])
	}
	for key, want := range map[string]string{
		"hub_status":               "online",
		"llm_status":               "available",
		"search_status":            "available",
		"documents_status":         "available",
		"digital_employees_status": "available",
		"realtime_path":            "/api/mobile/realtime",
	} {
		if services[key] != want {
			t.Fatalf("services[%s] = %#v, want %q", key, services[key], want)
		}
	}
	connection, ok := payload["connection"].(map[string]any)
	if !ok {
		t.Fatalf("connection payload = %#v, want map", payload["connection"])
	}
	candidates, ok := connection["hubcenter_candidates"].([]string)
	if !ok || len(candidates) != 3 || candidates[0] != "https://hubs.mypapers.top" || candidates[1] != "https://hubs.maclaw.top" || candidates[2] != "https://hubs2.maclaw.top" {
		t.Fatalf("hubcenter candidates = %#v, want three official presets", connection["hubcenter_candidates"])
	}
	if connection["selected_hubcenter_url"] != "https://hubs.maclaw.top" || connection["tenant_id"] != "tenant-1" {
		t.Fatalf("connection = %#v, want selected official hubcenter and tenant", connection)
	}
	llmAccess, ok := payload["llm_access"].(map[string]any)
	if !ok || llmAccess["mode"] != "maclaw_official" {
		t.Fatalf("llm_access = %#v, want maclaw_official", payload["llm_access"])
	}
	features, ok := payload["features"].(map[string]any)
	if !ok {
		t.Fatalf("features payload = %#v, want map", payload["features"])
	}
	if features["backend_ssh_sessions"] != true {
		t.Fatalf("features = %#v, want backend SSH session management flag", features)
	}
	if features["tasks"] != true {
		t.Fatalf("features = %#v, want tasks tab flag", features)
	}
	if _, ok := features["local_ssh"]; ok {
		t.Fatalf("features = %#v, must not advertise phone-local SSH", features)
	}
	limits, ok := payload["limits"].(map[string]any)
	if !ok {
		t.Fatalf("limits payload = %#v, want map", payload["limits"])
	}
	// caps may store int64; compare via float64 conversion for interface{} stability.
	quota, _ := limits["document_quota_bytes"].(int64)
	if quota == 0 {
		if f, ok := limits["document_quota_bytes"].(float64); ok {
			quota = int64(f)
		} else if n, ok := limits["document_quota_bytes"].(int); ok {
			quota = int64(n)
		}
	}
	if quota != 100*1024*1024 {
		t.Fatalf("limits = %#v, want 100MiB document quota default", limits)
	}
	if _, ok := limits["hub_file_download_max_bytes"]; !ok {
		t.Fatalf("limits = %#v, want hub_file_download_max_bytes", limits)
	}
	if payload["assistant_mode"] != "official" && payload["assistant_mode"] != "digital_twin" {
		t.Fatalf("assistant_mode = %#v, want official or digital_twin", payload["assistant_mode"])
	}
	entitlements, ok := payload["entitlements"].(map[string]any)
	if !ok {
		t.Fatalf("entitlements = %#v, want map", payload["entitlements"])
	}
	if _, ok := entitlements["mobile_official"]; !ok {
		t.Fatalf("entitlements = %#v, want mobile_official", entitlements)
	}
}

func TestMobileBootstrapPayloadIncludesPhoneCreditsAccount(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	payload := mobileBootstrapPayload(&auth.ViewerPrincipal{
		UserID:   "user-phone-1",
		Email:    "phone:19900001111",
		TenantID: "tenant-1",
	})

	user, ok := payload["user"].(map[string]any)
	if !ok {
		t.Fatalf("user payload = %#v, want map", payload["user"])
	}
	if user["phone_number"] != "19900001111" || user["credits_account"] != "phone:19900001111" {
		t.Fatalf("user payload = %#v, want phone credits account", user)
	}
	llmAccess, ok := payload["llm_access"].(map[string]any)
	if !ok {
		t.Fatalf("llm_access = %#v, want map", payload["llm_access"])
	}
	if llmAccess["mode"] != "maclaw_official" || llmAccess["credits_account"] != "phone:19900001111" {
		t.Fatalf("llm_access = %#v, want official phone credits account", llmAccess)
	}
}

func TestMobileLLMDesktopQRAuthorizationUpdatesBootstrap(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-llm@example.com")
	createBody, _ := json.Marshal(map[string]any{
		"name":     "OpenAI Compatible",
		"url":      "https://llm.example.com/v1",
		"key":      "sk-test",
		"model":    "gpt-4.1-mini",
		"models":   []string{"gpt-4.1-mini"},
		"protocol": "openai",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-sessions", bytes.NewReader(createBody))
	createReq.Host = "tenant-a.maclaw.top"
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()

	MobileLLMDesktopQRSessionHandler(identity).ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s, want 201", createRec.Code, createRec.Body.String())
	}
	var createResponse map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResponse); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	qrPayload, _ := createResponse["qr_payload"].(string)
	if qrPayload == "" || strings.Contains(qrPayload, "sk-test") {
		t.Fatalf("qr_payload = %q, want non-empty payload without API key", qrPayload)
	}
	if !strings.Contains(qrPayload, "maclaw_mobile_llm_authorization") {
		t.Fatalf("qr_payload = %q, want mobile authorization session payload", qrPayload)
	}
	if !strings.Contains(qrPayload, "tenant-a.maclaw.top") {
		t.Fatalf("qr_payload = %q, want discovered Hub URL for mobile direct consumption", qrPayload)
	}
	body, _ := json.Marshal(map[string]string{"qr_payload": qrPayload})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-authorizations", bytes.NewReader(body))
	req.Host = "tenant-a.maclaw.top"
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	MobileLLMDesktopQRAuthorizationHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	bootstrap, ok := response["bootstrap"].(map[string]any)
	if !ok {
		t.Fatalf("bootstrap = %#v, want map", response["bootstrap"])
	}
	llmAccess, ok := bootstrap["llm_access"].(map[string]any)
	if !ok {
		t.Fatalf("llm_access = %#v, want map", bootstrap["llm_access"])
	}
	if llmAccess["mode"] != "desktop_qr_third_party" || llmAccess["authorized_by"] != "maclaw-gui" {
		t.Fatalf("llm_access = %#v, want desktop QR delegated mode", llmAccess)
	}
	if llmAccess["authorization_id"] == "" || llmAccess["provider_name"] != "OpenAI Compatible" || llmAccess["provider_url"] != "https://llm.example.com/v1" || llmAccess["model"] != "gpt-4.1-mini" {
		t.Fatalf("llm_access = %#v, want provider metadata without API key", llmAccess)
	}
	if _, leaked := llmAccess["key"]; leaked {
		t.Fatalf("llm_access leaked key: %#v", llmAccess)
	}
	connection, ok := bootstrap["connection"].(map[string]any)
	if !ok || connection["hub_url"] != "https://tenant-a.maclaw.top" {
		t.Fatalf("connection = %#v, want request hub URL", bootstrap["connection"])
	}

	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/mobile/llm/desktop-qr-authorizations", nil)
	revokeReq.Header.Set("Authorization", "Bearer "+viewerToken)
	revokeRec := httptest.NewRecorder()
	MobileLLMDesktopQRAuthorizationRevokeHandler(identity).ServeHTTP(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d body=%s, want 200", revokeRec.Code, revokeRec.Body.String())
	}
	var revoked map[string]any
	if err := json.Unmarshal(revokeRec.Body.Bytes(), &revoked); err != nil {
		t.Fatalf("decode revoke response: %v", err)
	}
	revokedBootstrap, _ := revoked["bootstrap"].(map[string]any)
	revokedAccess, _ := revokedBootstrap["llm_access"].(map[string]any)
	if revoked["status"] != "revoked" || revokedAccess["mode"] != "maclaw_official" {
		t.Fatalf("revoke response = %#v, want official LLM bootstrap", revoked)
	}

	reuseReq := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-authorizations", bytes.NewReader(body))
	reuseReq.Header.Set("Authorization", "Bearer "+viewerToken)
	reuseRec := httptest.NewRecorder()
	MobileLLMDesktopQRAuthorizationHandler(identity).ServeHTTP(reuseRec, reuseReq)
	if reuseRec.Code != http.StatusBadRequest || !strings.Contains(reuseRec.Body.String(), "already been used") {
		t.Fatalf("reuse status=%d body=%s, want used-session rejection", reuseRec.Code, reuseRec.Body.String())
	}
}

func TestMobileDesktopAuthQRSessionCreatesPayload(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	desktopViewerToken, _ := issueViewerToken(t, identity, "phone:19900001111")
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/desktop-qr-sessions", strings.NewReader(`{}`))
	createReq.Host = "tenant-a.maclaw.top"
	createReq.Header.Set("Authorization", "Bearer "+desktopViewerToken)
	createRec := httptest.NewRecorder()

	MobileDesktopAuthQRSessionHandler(identity).ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s, want 201", createRec.Code, createRec.Body.String())
	}
	var createResponse map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResponse); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	sessionID, _ := createResponse["session_id"].(string)
	if !strings.HasPrefix(sessionID, "maqr_") {
		t.Fatalf("session_id = %q, want maqr_ prefix", sessionID)
	}
	qrPayload, _ := createResponse["qr_payload"].(string)
	if qrPayload == "" || !strings.Contains(qrPayload, "maclaw_mobile_desktop_authorization") {
		t.Fatalf("qr_payload = %q, want mobile desktop authorization payload", qrPayload)
	}
	if !strings.Contains(qrPayload, "tenant-a.maclaw.top") || !strings.Contains(qrPayload, sessionID) {
		t.Fatalf("qr_payload = %q, want hub URL and session id", qrPayload)
	}
	if createResponse["expires_at"] == "" {
		t.Fatalf("create response = %#v, want expires_at", createResponse)
	}
}

func TestMobileDesktopAuthQRSessionRequiresViewerAuth(t *testing.T) {
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/desktop-qr-sessions", strings.NewReader(`{}`))
	createReq.Header.Set("Authorization", "Bearer missing")
	createRec := httptest.NewRecorder()
	identity, _, _ := newHTTPAPITestServices(t)
	MobileDesktopAuthQRSessionHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s, want 401", createRec.Code, createRec.Body.String())
	}
}

func TestMobileDesktopAuthQRSessionRejectsNilIdentity(t *testing.T) {
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/desktop-qr-sessions", strings.NewReader(`{}`))
	createRec := httptest.NewRecorder()
	MobileDesktopAuthQRSessionHandler(nil).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusInternalServerError || !strings.Contains(createRec.Body.String(), "IDENTITY_UNAVAILABLE") {
		t.Fatalf("status=%d body=%s, want IDENTITY_UNAVAILABLE", createRec.Code, createRec.Body.String())
	}
}

func TestMobileDesktopAuthQRSessionRequiresRequestHost(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	desktopViewerToken, _ := issueViewerToken(t, identity, "phone:19900003333")
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/desktop-qr-sessions", strings.NewReader(`{}`))
	createReq.Host = ""
	createReq.Header.Set("Authorization", "Bearer "+desktopViewerToken)
	createRec := httptest.NewRecorder()
	MobileDesktopAuthQRSessionHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusInternalServerError || !strings.Contains(createRec.Body.String(), "Hub URL") {
		t.Fatalf("status=%d body=%s, want Hub URL failure", createRec.Code, createRec.Body.String())
	}
}

func TestMobileDesktopAuthQRSessionRequiresVerifiedPhoneIdentity(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	desktopViewerToken, _ := issueViewerToken(t, identity, "desktop-owner@example.com")
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/desktop-qr-sessions", strings.NewReader(`{}`))
	createReq.Header.Set("Authorization", "Bearer "+desktopViewerToken)
	createRec := httptest.NewRecorder()

	MobileDesktopAuthQRSessionHandler(identity).ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusForbidden || !strings.Contains(createRec.Body.String(), "PHONE_IDENTITY_REQUIRED") {
		t.Fatalf("status=%d body=%s, want phone identity requirement", createRec.Code, createRec.Body.String())
	}
}

func TestMobileDesktopAuthQRSessionAllowsEmailUserWithBoundPhone(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	desktopViewerToken, enroll := issueViewerToken(t, identity, "desktop-owner-bound@example.com")
	user, err := identity.UsersRepo().GetByID(auth.WithTenant(context.Background(), enroll.TenantID), enroll.UserID)
	if err != nil || user == nil {
		t.Fatalf("GetByID user=%#v err=%v", user, err)
	}
	if err := identity.BindVerifiedPhoneToUser(auth.WithTenant(context.Background(), enroll.TenantID), user, "19900001111"); err != nil {
		t.Fatalf("BindVerifiedPhoneToUser: %v", err)
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/desktop-qr-sessions", strings.NewReader(`{}`))
	createReq.Header.Set("Authorization", "Bearer "+desktopViewerToken)
	createRec := httptest.NewRecorder()

	MobileDesktopAuthQRSessionHandler(identity).ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201", createRec.Code, createRec.Body.String())
	}
}

func TestPruneExpiredMobileQRSessionsLocked(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	now := time.Now().UTC()
	mobileLlmAuthorizations.Lock()
	mobileLlmAuthorizations.qrSessions["expired"] = mobileLlmQRSessionRecord{
		SessionID: "expired",
		ExpiresAt: now.Add(-time.Minute),
	}
	mobileLlmAuthorizations.qrSessions["consumed"] = mobileLlmQRSessionRecord{
		SessionID:  "consumed",
		ExpiresAt:  now.Add(time.Minute),
		ConsumedAt: now.Add(-time.Second),
	}
	mobileLlmAuthorizations.qrSessions["live"] = mobileLlmQRSessionRecord{
		SessionID: "live",
		ExpiresAt: now.Add(time.Minute),
	}
	pruneExpiredMobileQRSessionsLocked(now)
	_, hasExpired := mobileLlmAuthorizations.qrSessions["expired"]
	_, hasConsumed := mobileLlmAuthorizations.qrSessions["consumed"]
	_, hasLive := mobileLlmAuthorizations.qrSessions["live"]
	mobileLlmAuthorizations.Unlock()
	if hasExpired || hasConsumed || !hasLive {
		t.Fatalf("prune result expired=%v consumed=%v live=%v", hasExpired, hasConsumed, hasLive)
	}
}

func TestMobileDesktopAuthQRSessionRefreshReplacesPriorSession(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	desktopViewerToken, enroll := issueViewerToken(t, identity, "phone:19900002222")

	firstReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/desktop-qr-sessions", strings.NewReader(`{}`))
	firstReq.Header.Set("Authorization", "Bearer "+desktopViewerToken)
	firstRec := httptest.NewRecorder()
	MobileDesktopAuthQRSessionHandler(identity).ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("first create status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}
	var first map[string]any
	if err := json.Unmarshal(firstRec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	firstID, _ := first["session_id"].(string)

	secondReq := httptest.NewRequest(http.MethodPost, "/api/mobile/auth/desktop-qr-sessions", strings.NewReader(`{}`))
	secondReq.Header.Set("Authorization", "Bearer "+desktopViewerToken)
	secondRec := httptest.NewRecorder()
	MobileDesktopAuthQRSessionHandler(identity).ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusCreated {
		t.Fatalf("second create status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
	var second map[string]any
	if err := json.Unmarshal(secondRec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	secondID, _ := second["session_id"].(string)
	if firstID == "" || secondID == "" || firstID == secondID {
		t.Fatalf("session ids first=%q second=%q, want distinct non-empty", firstID, secondID)
	}

	mobileLlmAuthorizations.Lock()
	_, hasFirst := mobileLlmAuthorizations.qrSessions[firstID]
	secondSession, hasSecond := mobileLlmAuthorizations.qrSessions[secondID]
	authCount := 0
	for _, session := range mobileLlmAuthorizations.qrSessions {
		if session.OwnerID == enroll.UserID && session.Purpose == mobileQRSessionPurposeAuth {
			authCount++
		}
	}
	mobileLlmAuthorizations.Unlock()
	if hasFirst || !hasSecond || authCount != 1 {
		t.Fatalf("sessions first=%v second=%v authCount=%d owner=%s secondRec=%#v", hasFirst, hasSecond, authCount, enroll.UserID, secondSession)
	}
}

func TestMobileLLMDesktopQRAuthorizationRejectsInvalidPayload(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-llm-invalid@example.com")
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-authorizations", strings.NewReader(`{"qr_payload":"{\"v\":1,\"type\":\"maclaw_llm\",\"name\":\"OpenAI Compatible\",\"url\":\"https://llm.example.com/v1\",\"key\":\"sk-test\"}"}`))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	MobileLLMDesktopQRAuthorizationHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "INVALID_DESKTOP_QR") {
		t.Fatalf("body = %s, want INVALID_DESKTOP_QR", rec.Body.String())
	}
}

func TestMobileLLMAuthorizationEncryptedPersistenceRestoresAndRevokes(t *testing.T) {
	settings := &mobileLLMTestSystemSettings{}
	key := bytes.Repeat([]byte{0x42}, 32)
	setMobileLLMAuthorizationPersistenceForTest(t, settings, key)
	record := mobileLlmAuthorizationRecord{
		AuthorizationID: "mllm_persisted",
		OwnerID:         "user-1",
		TenantID:        "tenant-1",
		ProviderName:    "OpenAI Compatible",
		ProviderURL:     "https://llm.example.com/v1",
		APIKey:          "delegated-secret-key",
		Model:           "gpt-4.1-mini",
		Protocol:        "openai",
		AuthorizedAt:    time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
	}
	if err := persistMobileLLMAuthorization(context.Background(), record); err != nil {
		t.Fatalf("persist authorization: %v", err)
	}
	stored, err := settings.Get(context.Background(), "tenant:tenant-1:"+mobileLLMAuthorizationPersistenceKey("user-1"))
	if err != nil {
		t.Fatalf("read encrypted authorization: %v", err)
	}
	if strings.Contains(stored, record.APIKey) || strings.Contains(stored, record.ProviderURL) {
		t.Fatalf("encrypted authorization leaked provider data: %s", stored)
	}
	mobileLlmAuthorizations.Lock()
	delete(mobileLlmAuthorizations.authorizations, mobileLlmAuthorizationKey("tenant-1", "user-1"))
	mobileLlmAuthorizations.Unlock()
	access := mobileLlmAccessPayload(context.Background(), &auth.ViewerPrincipal{
		TenantID: "tenant-1",
		UserID:   "user-1",
	})
	if access["mode"] != "desktop_qr_third_party" || access["authorization_id"] != record.AuthorizationID {
		t.Fatalf("restored bootstrap LLM access = %#v", access)
	}
	restored, ok := mobilePersistedLLMAuthorization(context.Background(), "tenant-1", "user-1")
	if !ok || restored.APIKey != record.APIKey || restored.AuthorizationID != record.AuthorizationID {
		t.Fatalf("restored authorization = %#v ok=%v", restored, ok)
	}
	if err := deletePersistedMobileLLMAuthorization(context.Background(), "tenant-1", "user-1"); err != nil {
		t.Fatalf("delete authorization: %v", err)
	}
	mobileLlmAuthorizations.Lock()
	delete(mobileLlmAuthorizations.authorizations, mobileLlmAuthorizationKey("tenant-1", "user-1"))
	mobileLlmAuthorizations.Unlock()
	if _, ok := mobilePersistedLLMAuthorization(context.Background(), "tenant-1", "user-1"); ok {
		t.Fatal("revoked authorization was restored")
	}
}

func TestMobileLLMAuthorizationEncryptionKeyRequires32Bytes(t *testing.T) {
	if _, err := mobileLLMAuthorizationEncryptionKey("not-a-valid-key"); err == nil {
		t.Fatal("invalid key was accepted")
	}
	key, err := mobileLLMAuthorizationEncryptionKey(strings.Repeat("42", 32))
	if err != nil || len(key) != 32 {
		t.Fatalf("hex key len=%d err=%v, want 32 bytes", len(key), err)
	}
}

func TestMobileLLMDesktopQRAuthorizationRejectsMobileAuthPayload(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-auth-not-llm@example.com")
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-authorizations", strings.NewReader(`{"qr_payload":"{\"v\":2,\"type\":\"maclaw_mobile_desktop_authorization\",\"session_id\":\"maqr_test\",\"hub_url\":\"https://tenant-a.maclaw.top\"}"}`))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	MobileLLMDesktopQRAuthorizationHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "MaClaw GUI LLM authorization session") {
		t.Fatalf("status = %d body=%s, want non-LLM QR rejection", rec.Code, rec.Body.String())
	}
}

func TestMobileSearchHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/search", strings.NewReader(`{"query":"status"}`))
	rec := httptest.NewRecorder()

	MobileSearchHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileSearchHandlerUsesOfficialLLMAndPreservesCitations(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-search-official@example.com")
	previousSearch := mobileWebSearch
	mobileWebSearch = func(context.Context, string, int) ([]websearch.SearchResult, error) {
		return []websearch.SearchResult{{Title: "Status source", URL: "https://example.test/status", Snippet: "healthy"}}, nil
	}
	t.Cleanup(func() { mobileWebSearch = previousSearch })
	called := false
	official := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			t.Fatalf("official path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+viewerToken {
			t.Fatalf("official request did not preserve viewer token")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode official request: %v", err)
		}
		if body["model"] != "auto" {
			t.Fatalf("model = %#v, want auto", body["model"])
		}
		w.Header().Set("X-MaClaw-Request-ID", "llm-mobile-1")
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "LLM summary"}}}})
	})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/search", strings.NewReader(`{"query":"server status"}`))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	MobileSearchHandler(identity, official).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !called || response["answer"] != "LLM summary" || response["llm_mode"] != "maclaw_official" || response["llm_request_id"] != "llm-mobile-1" || response["llm_usage_record_id"] != "llm-mobile-1" {
		t.Fatalf("response = %#v, want official LLM answer and trace", response)
	}
	citations, _ := response["citations"].([]any)
	if len(citations) != 1 {
		t.Fatalf("citations = %#v, want source preserved", response["citations"])
	}
}
func TestMobileSearchHandlerUsesDesktopQRProviderWithoutLeakingKey(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-search-third-party@example.com")
	principalReq := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap", nil)
	principalReq.Header.Set("Authorization", "Bearer "+viewerToken)
	principal, err := authenticateViewerRequest(principalReq, identity)
	if err != nil {
		t.Fatalf("authenticate viewer: %v", err)
	}
	providerCalled := false
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalled = true
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("provider path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer delegated-key" {
			t.Fatalf("provider authorization was not preserved")
		}
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "Delegated answer"}}}})
	}))
	defer provider.Close()
	clearMobileLLMAuthorizationsForTest(t)
	key := mobileLlmAuthorizationKey(principal.TenantID, principal.UserID)
	mobileLlmAuthorizations.Lock()
	mobileLlmAuthorizations.authorizations[key] = mobileLlmAuthorizationRecord{ProviderURL: provider.URL + "/v1", APIKey: "delegated-key", Model: "delegated-model", Protocol: "openai"}
	mobileLlmAuthorizations.Unlock()
	previousSearch := mobileWebSearch
	mobileWebSearch = func(context.Context, string, int) ([]websearch.SearchResult, error) { return nil, nil }
	t.Cleanup(func() { mobileWebSearch = previousSearch })
	officialCalled := false
	official := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { officialCalled = true })
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/search", strings.NewReader(`{"query":"server status"}`))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	MobileSearchHandler(identity, official).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if officialCalled || !providerCalled || strings.Contains(rec.Body.String(), "delegated-key") {
		t.Fatalf("provider dispatch result = official:%v provider:%v body:%s", officialCalled, providerCalled, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["answer"] != "Delegated answer" || response["llm_mode"] != "desktop_qr_third_party" {
		t.Fatalf("response = %#v", response)
	}
}
func TestMobileSearchFormatsResultsWithCitations(t *testing.T) {
	results := []websearch.SearchResult{
		{
			Title:   "Nginx logs guide",
			URL:     "https://example.test/nginx",
			Snippet: "Check error.log and access.log first.",
		},
		{
			Title:   "",
			URL:     "https://example.test/systemd",
			Snippet: "Use journalctl for service failures.",
		},
	}

	answer := mobileSearchAnswer("nginx 502", results, nil)
	if !strings.Contains(answer, "nginx 502") {
		t.Fatalf("answer = %q, want query", answer)
	}
	if !strings.Contains(answer, "Nginx logs guide") {
		t.Fatalf("answer = %q, want result title", answer)
	}
	// Fallback must not dump raw SERP snippets as the main answer body.
	if strings.Contains(answer, "Check error.log") {
		t.Fatalf("answer = %q, must not paste search snippet body", answer)
	}
	if !strings.Contains(answer, "LLM") && !strings.Contains(answer, "来源") {
		t.Fatalf("answer = %q, want guidance about sources/LLM", answer)
	}

	citations := mobileSearchCitations(results)
	if len(citations) != 2 {
		t.Fatalf("len(citations) = %d, want 2", len(citations))
	}
	if citations[0]["url"] != "https://example.test/nginx" {
		t.Fatalf("first citation = %#v, want nginx url", citations[0])
	}
	if citations[1]["title"] != "https://example.test/systemd" {
		t.Fatalf("second citation = %#v, want URL title fallback", citations[1])
	}
}

func TestMobileSearchKeepsSharedLinksAsCitations(t *testing.T) {
	query := "\u603b\u7ed3\u8fd9\u4e2a\u94fe\u63a5 https://example.test/incident?from=mobile"
	links := mobileExtractQueryLinks(query)
	answer := mobileSearchAnswer(query, nil, links)
	citations := mobileMergeLinkCitations(nil, links)

	if !strings.Contains(answer, "\u94fe\u63a5") {
		t.Fatalf("answer = %q, want shared-link hint", answer)
	}
	if len(citations) != 1 {
		t.Fatalf("len(citations) = %d, want 1", len(citations))
	}
	if citations[0]["url"] != "https://example.test/incident?from=mobile" {
		t.Fatalf("citation = %#v, want shared URL", citations[0])
	}
}

func TestMobileCleanSearchTextUnescapesHTMLEntities(t *testing.T) {
	got := mobileCleanSearchText("1\u5929\u524d&ensp;&#0183;&ensp;hello<br>world", 0)
	if strings.Contains(got, "&ensp;") || strings.Contains(got, "&#") || strings.Contains(got, "<br>") {
		t.Fatalf("got %q, still contains raw HTML markup", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatalf("got %q, want cleaned content", got)
	}
}

func TestMobileBuildLLMMessagesDropsOnlyTrailingDuplicateUser(t *testing.T) {
	messages := mobileBuildLLMMessages(
		"same",
		nil,
		[]mobileChatMessage{
			{Role: "user", Content: "same"}, // earlier identical question must stay
			{Role: "assistant", Content: "first answer"},
			{Role: "user", Content: "same"}, // trailing current query is dropped
		},
		nil,
	)
	// system + earlier user + assistant + final user (current)
	if len(messages) != 4 {
		t.Fatalf("len(messages)=%d %#v", len(messages), messages)
	}
	if messages[1]["content"] != "same" || messages[1]["role"] != "user" {
		t.Fatalf("expected preserved earlier user turn, got %#v", messages[1])
	}
	if messages[2]["role"] != "assistant" {
		t.Fatalf("expected assistant history, got %#v", messages[2])
	}
	if messages[3]["role"] != "user" || !strings.Contains(messages[3]["content"], "same") {
		t.Fatalf("expected final user turn, got %#v", messages[3])
	}
}

func TestMobileBuildLLMMessagesCapsHistory(t *testing.T) {
	history := make([]mobileChatMessage, 0, 40)
	for i := 0; i < 40; i++ {
		history = append(history, mobileChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("turn-%d", i),
		})
	}
	messages := mobileBuildLLMMessages("now", nil, history, nil)
	// system + capped history + final user
	if len(messages) > mobileLLMMaxHistoryTurns+2 {
		t.Fatalf("len(messages)=%d, want <= %d", len(messages), mobileLLMMaxHistoryTurns+2)
	}
	// Last history turn before final user should be near the end of the input.
	if !strings.Contains(messages[len(messages)-2]["content"], "turn-") {
		t.Fatalf("expected capped history tail, got %#v", messages)
	}
}

func TestMobileSearchHandlerStreamEmitsMetaDeltaDone(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-search-stream@example.com")
	previousSearch := mobileWebSearch
	mobileWebSearch = func(context.Context, string, int) ([]websearch.SearchResult, error) {
		return []websearch.SearchResult{{Title: "Src", URL: "https://example.test/s", Snippet: "ok"}}, nil
	}
	t.Cleanup(func() { mobileWebSearch = previousSearch })
	official := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-MaClaw-Request-ID", "llm-stream-1")
		writeJSON(w, http.StatusOK, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "结论：服务正常。\n\n- 要点"}}}})
	})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/search", strings.NewReader(`{"query":"status","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	MobileSearchHandler(identity, official).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q, want event-stream", rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, needle := range []string{"event: meta", "event: delta", "event: done", "llm-stream-1"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("stream body missing %q:\n%s", needle, body)
		}
	}
	// encoding/json escapes non-ASCII; accept raw or \u form.
	if !mobileSSEBodyContainsText(body, "结论：服务正常") {
		t.Fatalf("stream body missing answer text:\n%s", body)
	}
}

func TestMobileOpenAIStreamDelta(t *testing.T) {
	delta, ok := mobileOpenAIStreamDelta(`data: {"choices":[{"delta":{"content":"hi"}}]}`)
	if !ok || delta != "hi" {
		t.Fatalf("delta=%q ok=%v", delta, ok)
	}
	if _, ok := mobileOpenAIStreamDelta("data: [DONE]"); ok {
		t.Fatal("DONE should not yield content")
	}
	if _, ok := mobileOpenAIStreamDelta("event: message"); ok {
		t.Fatal("non-data lines should be ignored")
	}
	parts, ok := mobileOpenAIStreamDelta(`data: {"choices":[{"delta":{"content":["a","b"]}}]}`)
	if !ok || parts != "ab" {
		t.Fatalf("array content delta=%q ok=%v", parts, ok)
	}
}

// mobileSSEBodyContainsText reports whether an SSE body includes text either
// literally or as encoding/json Unicode escapes.
func mobileSSEBodyContainsText(body, text string) bool {
	if strings.Contains(body, text) {
		return true
	}
	escaped, err := json.Marshal(text)
	if err != nil {
		return false
	}
	inner := string(escaped[1 : len(escaped)-1])
	return strings.Contains(body, inner)
}

func TestMobileChunkAnswerForSSEBreaksRunes(t *testing.T) {
	chunks := mobileChunkAnswerForSSE("一二三四五六七八九十", 4)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %#v, want multiple chunks", chunks)
	}
	joined := strings.Join(chunks, "")
	if joined != "一二三四五六七八九十" {
		t.Fatalf("joined = %q", joined)
	}
}

func TestMobileSearchPromptAsksForStructuredCompanionAnswer(t *testing.T) {
	prompt := mobileSearchPrompt("北京天气", []map[string]string{
		{
			"title":   "气象局",
			"url":     "https://example.test/weather",
			"snippet": "1天前&ensp;&#183;&ensp;晴转多云",
		},
	}, nil)
	for _, needle := range []string{"结论", "表格", "Markdown", "[1]", "HTML"} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q: %s", needle, prompt)
		}
	}
	if strings.Contains(prompt, "Evidence sources") && strings.Contains(prompt, "1天前&ensp;") {
		t.Fatalf("evidence section still contains raw HTML entity: %s", prompt)
	}
	if !strings.Contains(prompt, "1天前 · 晴转多云") {
		t.Fatalf("expected cleaned snippet in prompt: %s", prompt)
	}
}

func TestMobileDocumentHandlersRequireViewerToken(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		handler http.HandlerFunc
	}{
		{
			name:    "draft",
			method:  http.MethodPost,
			path:    "/api/mobile/documents/drafts",
			body:    `{"title":"notice"}`,
			handler: MobileDocumentDraftHandler(nil),
		},
		{
			name:    "upload",
			method:  http.MethodPost,
			path:    "/api/mobile/documents/upload",
			body:    "",
			handler: MobileDocumentUploadHandler(nil),
		},
		{
			name:    "draft update",
			method:  http.MethodPatch,
			path:    "/api/mobile/documents/drafts/d1",
			body:    `{"title":"notice","markdown":"body"}`,
			handler: MobileDocumentDraftUpdateHandler(nil),
		},
		{
			name:    "draft process",
			method:  http.MethodPost,
			path:    "/api/mobile/documents/drafts/d1/process",
			body:    `{"action":"summarize"}`,
			handler: MobileDocumentProcessHandler(nil),
		},
		{
			name:    "export",
			method:  http.MethodPost,
			path:    "/api/mobile/documents/export",
			body:    `{"draft_id":"d1","format":"pdf"}`,
			handler: MobileDocumentExportHandler(nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			tt.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestMobileSSHAnalyzeHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/analyze", strings.NewReader(`{"output":"panic"}`))
	rec := httptest.NewRecorder()

	MobileSSHAnalyzeHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileSSHAnalyzeHandlerEchoesBackendSessionID(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-ssh-analysis@example.com")
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/analyze", strings.NewReader(`{
		"output":"systemctl status app",
		"backend_session_id":"mobile-ssh:mobssh_1"
	}`))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	MobileSSHAnalyzeHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := body["backend_session_id"]; got != "mobile-ssh:mobssh_1" {
		t.Fatalf("backend_session_id = %#v, want mobile-ssh:mobssh_1", got)
	}
	if got := body["status"]; got != "ready" {
		t.Fatalf("status = %#v, want ready; body = %#v", got, body)
	}
}

func TestMobileServerProfilesHandlerRequiresAuth(t *testing.T) {
	tests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "list", method: http.MethodGet},
		{name: "publish", method: http.MethodPut, body: `{"profiles":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/mobile/server-profiles", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			MobileServerProfilesHandler(nil).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestMobileServerProfilesWorkerPublishesSanitizedProfiles(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-server-profile@example.com")
	clearMobileServerProfilesForTest(t)

	publishReq := httptest.NewRequest(http.MethodPut, "/api/mobile/server-profiles", strings.NewReader(`{
		"profiles": [
			{"id":"prod","name":"Prod","host":"10.0.0.10","port":2222,"username":"deploy","auth_mode":"key","tag":"ops","note":"read only metadata","password":"secret","private_key":"secret"},
			{"id":"bad","name":"Bad","host":"","port":22,"username":"root"}
		]
	}`))
	publishReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	publishReq.Header.Set("X-Machine-ID", enroll.MachineID)
	publishRec := httptest.NewRecorder()
	MobileServerProfilesHandler(identity).ServeHTTP(publishRec, publishReq)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish status = %d body=%s", publishRec.Code, publishRec.Body.String())
	}
	var published struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	if err := json.Unmarshal(publishRec.Body.Bytes(), &published); err != nil {
		t.Fatalf("decode publish response: %v", err)
	}
	if published.Status != "ok" || published.Count != 1 {
		t.Fatalf("published = %+v, want one sanitized profile", published)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/server-profiles", nil)
	listReq.Header.Set("Authorization", "Bearer "+viewerToken)
	listRec := httptest.NewRecorder()
	MobileServerProfilesHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Profiles) != 1 {
		t.Fatalf("profiles = %#v, want one", listed.Profiles)
	}
	profile := listed.Profiles[0]
	if profile["id"] != "prod" || profile["name"] != "Prod" || profile["host"] != "10.0.0.10" || profile["username"] != "deploy" || profile["auth_mode"] != "private_key" {
		t.Fatalf("profile = %#v, want sanitized prod profile", profile)
	}
	if profile["source_machine_id"] != enroll.MachineID {
		t.Fatalf("source_machine_id = %v, want %s", profile["source_machine_id"], enroll.MachineID)
	}
	if _, ok := profile["password"]; ok {
		t.Fatalf("profile leaked password: %#v", profile)
	}
	if _, ok := profile["private_key"]; ok {
		t.Fatalf("profile leaked private key: %#v", profile)
	}
}

func TestMobileServerProfilesAreOwnerScoped(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, ownerEnroll := issueViewerToken(t, identity, "mobile-server-profile-owner@example.com")
	otherToken, _ := issueViewerToken(t, identity, "mobile-server-profile-other@example.com")
	clearMobileServerProfilesForTest(t)

	publishReq := httptest.NewRequest(http.MethodPut, "/api/mobile/server-profiles", strings.NewReader(`{"profiles":[{"id":"prod","name":"Prod","host":"10.0.0.10","port":22,"username":"root"}]}`))
	publishReq.Header.Set("Authorization", "Bearer "+ownerEnroll.MachineToken)
	publishReq.Header.Set("X-Machine-ID", ownerEnroll.MachineID)
	publishRec := httptest.NewRecorder()
	MobileServerProfilesHandler(identity).ServeHTTP(publishRec, publishReq)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish status = %d body=%s", publishRec.Code, publishRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/server-profiles", nil)
	listReq.Header.Set("Authorization", "Bearer "+otherToken)
	listRec := httptest.NewRecorder()
	MobileServerProfilesHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Profiles []map[string]any `json:"profiles"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Profiles) != 0 {
		t.Fatalf("other user profiles = %#v, want none", listed.Profiles)
	}
}

func TestMobileBackendSSHSessionHandlersRequireViewerToken(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		handler http.HandlerFunc
	}{
		{
			name:    "list",
			method:  http.MethodGet,
			path:    "/api/mobile/ssh/sessions",
			handler: MobileBackendSSHSessionsHandler(nil),
		},
		{
			name:    "create",
			method:  http.MethodPost,
			path:    "/api/mobile/ssh/sessions",
			body:    `{"server_profile_id":"prod"}`,
			handler: MobileBackendSSHSessionsHandler(nil),
		},
		{
			name:    "attach",
			method:  http.MethodPost,
			path:    "/api/mobile/ssh/sessions/mobssh_1/attach",
			handler: MobileBackendSSHSessionAttachHandler(nil),
		},
		{
			name:    "input",
			method:  http.MethodPost,
			path:    "/api/mobile/ssh/sessions/mobssh_1/input",
			body:    `{"input":"uptime"}`,
			handler: MobileBackendSSHSessionInputHandler(nil),
		},
		{
			name:    "reconnect",
			method:  http.MethodPost,
			path:    "/api/mobile/ssh/sessions/mobssh_1/reconnect",
			handler: MobileBackendSSHSessionReconnectHandler(nil),
		},
		{
			name:    "interrupt",
			method:  http.MethodPost,
			path:    "/api/mobile/ssh/sessions/mobssh_1/interrupt",
			handler: MobileBackendSSHSessionInterruptHandler(nil),
		},
		{
			name:    "close",
			method:  http.MethodDelete,
			path:    "/api/mobile/ssh/sessions/mobssh_1",
			handler: MobileBackendSSHSessionCloseHandler(nil),
		},
		{
			name:    "worker claim",
			method:  http.MethodPost,
			path:    "/api/mobile/ssh/sessions/claim",
			handler: MobileBackendSSHSessionClaimHandler(nil),
		},
		{
			name:    "worker update",
			method:  http.MethodPatch,
			path:    "/api/mobile/ssh/sessions/mobssh_1/worker",
			body:    `{"status":"connected"}`,
			handler: MobileBackendSSHSessionUpdateHandler(nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.SetPathValue("sessionId", "mobssh_1")
			rec := httptest.NewRecorder()

			tt.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestMobileBackendSSHSessionLifecycleQueuesBackendControl(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-backend-ssh@example.com")
	clearMobileBackendSSHSessionsForTest(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions", strings.NewReader(`{"server_profile_id":"prod-root"}`))
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()
	MobileBackendSSHSessionsHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Session struct {
			SessionID       string `json:"session_id"`
			ServerProfileID string `json:"server_profile_id"`
			Status          string `json:"status"`
			State           string `json:"state"`
			Message         string `json:"message"`
			RecentOutput    string `json:"recent_output"`
		} `json:"session"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	sessionID := created.Session.SessionID
	if sessionID == "" || created.Session.ServerProfileID != "prod-root" {
		t.Fatalf("created session = %+v, want backend session id and profile", created.Session)
	}
	if created.Session.Status != "queued" || created.Session.State != "pending_agent" {
		t.Fatalf("created state = %+v, want queued pending_agent", created.Session)
	}

	inputReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/input", strings.NewReader(`{"input":"sudo systemctl restart app"}`))
	inputReq.SetPathValue("sessionId", sessionID)
	inputReq.Header.Set("Authorization", "Bearer "+viewerToken)
	inputRec := httptest.NewRecorder()
	MobileBackendSSHSessionInputHandler(identity).ServeHTTP(inputRec, inputReq)
	if inputRec.Code != http.StatusAccepted {
		t.Fatalf("input status = %d body=%s", inputRec.Code, inputRec.Body.String())
	}
	var inputBody struct {
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
		Message   string `json:"message"`
		Session   struct {
			PendingInputCount int `json:"pending_input_count"`
		} `json:"session"`
	}
	if err := json.Unmarshal(inputRec.Body.Bytes(), &inputBody); err != nil {
		t.Fatalf("decode input response: %v", err)
	}
	if inputBody.SessionID != sessionID || inputBody.Status != "input_queued" || inputBody.Session.PendingInputCount != 1 {
		t.Fatalf("input response = %+v, want queued backend input", inputBody)
	}
	mobileBackendSSHSessions.Lock()
	record := mobileBackendSSHSessions.sessions[sessionID]
	mobileBackendSSHSessions.Unlock()
	if len(record.PendingInput) != 1 || record.PendingInput[0] != "sudo systemctl restart app" {
		t.Fatalf("pending input = %#v, want command recorded for worker", record.PendingInput)
	}
	if !strings.Contains(record.RecentOutput, "did not execute") {
		t.Fatalf("recent output = %q, want no local execution marker", record.RecentOutput)
	}

	attachReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/attach", nil)
	attachReq.SetPathValue("sessionId", sessionID)
	attachReq.Header.Set("Authorization", "Bearer "+viewerToken)
	attachRec := httptest.NewRecorder()
	MobileBackendSSHSessionAttachHandler(identity).ServeHTTP(attachRec, attachReq)
	if attachRec.Code != http.StatusOK {
		t.Fatalf("attach status = %d body=%s", attachRec.Code, attachRec.Body.String())
	}

	reconnectReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/reconnect", nil)
	reconnectReq.SetPathValue("sessionId", sessionID)
	reconnectReq.Header.Set("Authorization", "Bearer "+viewerToken)
	reconnectRec := httptest.NewRecorder()
	MobileBackendSSHSessionReconnectHandler(identity).ServeHTTP(reconnectRec, reconnectReq)
	if reconnectRec.Code != http.StatusOK {
		t.Fatalf("reconnect status = %d body=%s", reconnectRec.Code, reconnectRec.Body.String())
	}
	var reconnected struct {
		Session struct {
			Status string `json:"status"`
			State  string `json:"state"`
		} `json:"session"`
	}
	if err := json.Unmarshal(reconnectRec.Body.Bytes(), &reconnected); err != nil {
		t.Fatalf("decode reconnect response: %v", err)
	}
	if reconnected.Session.Status != "reconnect_requested" || reconnected.Session.State != "reconnecting" {
		t.Fatalf("reconnected session = %+v, want reconnect requested", reconnected.Session)
	}

	interruptReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/interrupt", nil)
	interruptReq.SetPathValue("sessionId", sessionID)
	interruptReq.Header.Set("Authorization", "Bearer "+viewerToken)
	interruptRec := httptest.NewRecorder()
	MobileBackendSSHSessionInterruptHandler(identity).ServeHTTP(interruptRec, interruptReq)
	if interruptRec.Code != http.StatusOK {
		t.Fatalf("interrupt status = %d body=%s", interruptRec.Code, interruptRec.Body.String())
	}
	var interrupted struct {
		Session struct {
			Status string `json:"status"`
			State  string `json:"state"`
		} `json:"session"`
	}
	if err := json.Unmarshal(interruptRec.Body.Bytes(), &interrupted); err != nil {
		t.Fatalf("decode interrupt response: %v", err)
	}
	if interrupted.Session.Status != "interrupt_requested" || interrupted.Session.State != "interrupting" {
		t.Fatalf("interrupted session = %+v, want interrupt requested", interrupted.Session)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/ssh/sessions", nil)
	listReq.Header.Set("Authorization", "Bearer "+viewerToken)
	listRec := httptest.NewRecorder()
	MobileBackendSSHSessionsHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Sessions) != 1 || listed.Sessions[0].SessionID != sessionID {
		t.Fatalf("listed sessions = %+v, want owned session", listed.Sessions)
	}

	closeReq := httptest.NewRequest(http.MethodDelete, "/api/mobile/ssh/sessions/"+sessionID, nil)
	closeReq.SetPathValue("sessionId", sessionID)
	closeReq.Header.Set("Authorization", "Bearer "+viewerToken)
	closeRec := httptest.NewRecorder()
	MobileBackendSSHSessionCloseHandler(identity).ServeHTTP(closeRec, closeReq)
	if closeRec.Code != http.StatusNoContent {
		t.Fatalf("close status = %d body=%s", closeRec.Code, closeRec.Body.String())
	}
	mobileBackendSSHSessions.Lock()
	closedRecord, exists := mobileBackendSSHSessions.sessions[sessionID]
	mobileBackendSSHSessions.Unlock()
	if !exists || closedRecord.Status != "close_requested" || closedRecord.State != "closing" {
		t.Fatalf("closed record = %#v exists=%v, want close_requested queued for worker", closedRecord, exists)
	}
}

func TestMobileBackendSSHTasksAndFilesQueueControlRecords(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-backend-ssh-tasks@example.com")
	clearMobileBackendSSHSessionsForTest(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions", strings.NewReader(`{"server_profile_id":"prod-root"}`))
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()
	MobileBackendSSHSessionsHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Session struct {
			SessionID string `json:"session_id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	sessionID := created.Session.SessionID
	mobileBackendSSHSessions.Lock()
	record := mobileBackendSSHSessions.sessions[sessionID]
	record.BackendSessionID = "mobile-ssh:" + sessionID
	record.ClaimedBy = enroll.MachineID
	mobileBackendSSHSessions.sessions[sessionID] = record
	mobileBackendSSHSessions.Unlock()

	startReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/tasks", strings.NewReader(`{"action":"exec_background","command":"journalctl -u app -n 200","tail_lines":80}`))
	startReq.SetPathValue("sessionId", sessionID)
	startReq.Header.Set("Authorization", "Bearer "+viewerToken)
	startRec := httptest.NewRecorder()
	MobileBackendSSHSessionTasksHandler(identity).ServeHTTP(startRec, startReq)
	if startRec.Code != http.StatusAccepted {
		t.Fatalf("start task status = %d body=%s", startRec.Code, startRec.Body.String())
	}
	var started struct {
		Task struct {
			TaskID           string `json:"task_id"`
			SessionID        string `json:"session_id"`
			BackendSessionID string `json:"backend_session_id"`
			Command          string `json:"command"`
			Status           string `json:"status"`
			LogTail          string `json:"log_tail"`
			TailLines        int    `json:"tail_lines"`
			ClaimedBy        string `json:"claimed_by"`
		} `json:"task"`
	}
	if err := json.Unmarshal(startRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode started task: %v", err)
	}
	if started.Task.TaskID == "" || started.Task.SessionID != sessionID || started.Task.BackendSessionID != "mobile-ssh:"+sessionID {
		t.Fatalf("started task = %+v, want session-bound task", started.Task)
	}
	if started.Task.Status != "queued" || started.Task.Command != "journalctl -u app -n 200" || started.Task.TailLines != 80 || started.Task.ClaimedBy != enroll.MachineID {
		t.Fatalf("started task = %+v, want queued GUI/agent task", started.Task)
	}
	if !strings.Contains(started.Task.LogTail, "did not execute") {
		t.Fatalf("task log tail = %q, want no local execution marker", started.Task.LogTail)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/ssh/sessions/"+sessionID+"/tasks", nil)
	listReq.SetPathValue("sessionId", sessionID)
	listReq.Header.Set("Authorization", "Bearer "+viewerToken)
	listRec := httptest.NewRecorder()
	MobileBackendSSHSessionTasksHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list task status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Tasks []struct {
			TaskID string `json:"task_id"`
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode listed tasks: %v", err)
	}
	if len(listed.Tasks) != 1 || listed.Tasks[0].TaskID != started.Task.TaskID {
		t.Fatalf("listed tasks = %+v, want created task", listed.Tasks)
	}

	waitReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/tasks/"+started.Task.TaskID+"/wait", strings.NewReader(`{"timeout":30,"tail_lines":120}`))
	waitReq.SetPathValue("sessionId", sessionID)
	waitReq.SetPathValue("taskId", started.Task.TaskID)
	waitReq.Header.Set("Authorization", "Bearer "+viewerToken)
	waitRec := httptest.NewRecorder()
	MobileBackendSSHSessionTaskWaitHandler(identity).ServeHTTP(waitRec, waitReq)
	if waitRec.Code != http.StatusAccepted {
		t.Fatalf("wait task status = %d body=%s", waitRec.Code, waitRec.Body.String())
	}
	var waited struct {
		Task struct {
			Status  string `json:"status"`
			Timeout int    `json:"timeout"`
			Tail    int    `json:"tail_lines"`
		} `json:"task"`
	}
	if err := json.Unmarshal(waitRec.Body.Bytes(), &waited); err != nil {
		t.Fatalf("decode waited task: %v", err)
	}
	if waited.Task.Status != "wait_requested" || waited.Task.Timeout != 30 || waited.Task.Tail != 120 {
		t.Fatalf("waited task = %+v, want wait request persisted", waited.Task)
	}

	killReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/tasks/"+started.Task.TaskID+"/kill", nil)
	killReq.SetPathValue("sessionId", sessionID)
	killReq.SetPathValue("taskId", started.Task.TaskID)
	killReq.Header.Set("Authorization", "Bearer "+viewerToken)
	killRec := httptest.NewRecorder()
	MobileBackendSSHSessionTaskKillHandler(identity).ServeHTTP(killRec, killReq)
	if killRec.Code != http.StatusAccepted {
		t.Fatalf("kill task status = %d body=%s", killRec.Code, killRec.Body.String())
	}
	var killed struct {
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(killRec.Body.Bytes(), &killed); err != nil {
		t.Fatalf("decode killed task: %v", err)
	}
	if killed.Task.Status != "kill_requested" {
		t.Fatalf("killed task = %+v, want kill request persisted", killed.Task)
	}

	fileReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/files", strings.NewReader(`{"action":"download","remote_path":"/var/log/app.log","local_path":"mobile-downloads/app.log"}`))
	fileReq.SetPathValue("sessionId", sessionID)
	fileReq.Header.Set("Authorization", "Bearer "+viewerToken)
	fileRec := httptest.NewRecorder()
	MobileBackendSSHSessionFilesHandler(identity).ServeHTTP(fileRec, fileReq)
	if fileRec.Code != http.StatusAccepted {
		t.Fatalf("file operation status = %d body=%s", fileRec.Code, fileRec.Body.String())
	}
	var fileOp struct {
		Operation struct {
			OperationID      string `json:"operation_id"`
			SessionID        string `json:"session_id"`
			BackendSessionID string `json:"backend_session_id"`
			Action           string `json:"action"`
			RemotePath       string `json:"remote_path"`
			LocalPath        string `json:"local_path"`
			Status           string `json:"status"`
			ClaimedBy        string `json:"claimed_by"`
		} `json:"operation"`
	}
	if err := json.Unmarshal(fileRec.Body.Bytes(), &fileOp); err != nil {
		t.Fatalf("decode file operation: %v", err)
	}
	if fileOp.Operation.OperationID == "" || fileOp.Operation.SessionID != sessionID || fileOp.Operation.BackendSessionID != "mobile-ssh:"+sessionID {
		t.Fatalf("file operation = %+v, want session-bound operation", fileOp.Operation)
	}
	if fileOp.Operation.Action != "download" || fileOp.Operation.RemotePath != "/var/log/app.log" || fileOp.Operation.LocalPath != "mobile-downloads/app.log" || fileOp.Operation.Status != "queued" || fileOp.Operation.ClaimedBy != enroll.MachineID {
		t.Fatalf("file operation = %+v, want queued GUI/agent file operation", fileOp.Operation)
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "download requires desktop path", body: `{"action":"download","remote_path":"/var/log/app.log"}`},
		{name: "upload requires remote path", body: `{"action":"upload","local_path":"desktop-files/report.txt"}`},
		{name: "stat requires remote path", body: `{"action":"stat","local_path":"desktop-files/report.txt"}`},
		{name: "list requires remote path", body: `{"action":"list","local_path":"desktop-files"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/files", strings.NewReader(tc.body))
			req.SetPathValue("sessionId", sessionID)
			req.Header.Set("Authorization", "Bearer "+viewerToken)
			rec := httptest.NewRecorder()
			MobileBackendSSHSessionFilesHandler(identity).ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), http.StatusBadRequest)
			}
		})
	}
	claimTaskReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/tasks/claim", nil)
	claimTaskReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimTaskReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimTaskRec := httptest.NewRecorder()
	MobileBackendSSHTaskClaimHandler(identity).ServeHTTP(claimTaskRec, claimTaskReq)
	if claimTaskRec.Code != http.StatusOK {
		t.Fatalf("claim task status = %d body=%s", claimTaskRec.Code, claimTaskRec.Body.String())
	}
	var claimedTask struct {
		Status string `json:"status"`
		Task   struct {
			TaskID    string `json:"task_id"`
			Status    string `json:"status"`
			ClaimedBy string `json:"claimed_by"`
		} `json:"task"`
	}
	if err := json.Unmarshal(claimTaskRec.Body.Bytes(), &claimedTask); err != nil {
		t.Fatalf("decode claimed task: %v", err)
	}
	if claimedTask.Status != "claimed" || claimedTask.Task.TaskID != started.Task.TaskID || claimedTask.Task.ClaimedBy != enroll.MachineID {
		t.Fatalf("claimed task = %+v, want machine claim", claimedTask)
	}

	updateTaskReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/ssh/tasks/"+started.Task.TaskID+"/worker", strings.NewReader(`{"status":"completed","message":"done","log_tail":"task output\n","exit_code":0,"backend_session_id":"mobile-ssh:`+sessionID+`"}`))
	updateTaskReq.SetPathValue("taskId", started.Task.TaskID)
	updateTaskReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	updateTaskReq.Header.Set("X-Machine-ID", enroll.MachineID)
	updateTaskRec := httptest.NewRecorder()
	MobileBackendSSHTaskUpdateHandler(identity).ServeHTTP(updateTaskRec, updateTaskReq)
	if updateTaskRec.Code != http.StatusOK {
		t.Fatalf("update task status = %d body=%s", updateTaskRec.Code, updateTaskRec.Body.String())
	}
	var updatedTask struct {
		Task struct {
			Status   string `json:"status"`
			LogTail  string `json:"log_tail"`
			ExitCode int    `json:"exit_code"`
		} `json:"task"`
	}
	if err := json.Unmarshal(updateTaskRec.Body.Bytes(), &updatedTask); err != nil {
		t.Fatalf("decode updated task: %v", err)
	}
	if updatedTask.Task.Status != "completed" || updatedTask.Task.ExitCode != 0 || updatedTask.Task.LogTail != "task output" {
		t.Fatalf("updated task = %+v, want worker result", updatedTask.Task)
	}

	claimFileReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/files/claim", nil)
	claimFileReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimFileReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimFileRec := httptest.NewRecorder()
	MobileBackendSSHFileOperationClaimHandler(identity).ServeHTTP(claimFileRec, claimFileReq)
	if claimFileRec.Code != http.StatusOK {
		t.Fatalf("claim file status = %d body=%s", claimFileRec.Code, claimFileRec.Body.String())
	}
	var claimedFile struct {
		Status    string `json:"status"`
		Operation struct {
			OperationID string `json:"operation_id"`
			Status      string `json:"status"`
			ClaimedBy   string `json:"claimed_by"`
		} `json:"operation"`
	}
	if err := json.Unmarshal(claimFileRec.Body.Bytes(), &claimedFile); err != nil {
		t.Fatalf("decode claimed file: %v", err)
	}
	if claimedFile.Status != "claimed" || claimedFile.Operation.OperationID != fileOp.Operation.OperationID || claimedFile.Operation.ClaimedBy != enroll.MachineID {
		t.Fatalf("claimed file operation = %+v, want machine claim", claimedFile)
	}

	updateFileReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/ssh/files/"+fileOp.Operation.OperationID+"/worker", strings.NewReader(`{"status":"completed","message":"download ready","bytes_transferred":42,"download_url":"/api/mobile/ssh/files/`+fileOp.Operation.OperationID+`/download","backend_session_id":"mobile-ssh:`+sessionID+`"}`))
	updateFileReq.SetPathValue("operationId", fileOp.Operation.OperationID)
	updateFileReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	updateFileReq.Header.Set("X-Machine-ID", enroll.MachineID)
	updateFileRec := httptest.NewRecorder()
	MobileBackendSSHFileOperationUpdateHandler(identity).ServeHTTP(updateFileRec, updateFileReq)
	if updateFileRec.Code != http.StatusOK {
		t.Fatalf("update file status = %d body=%s", updateFileRec.Code, updateFileRec.Body.String())
	}
	var updatedFile struct {
		Operation struct {
			Status           string `json:"status"`
			BytesTransferred int64  `json:"bytes_transferred"`
			DownloadURL      string `json:"download_url"`
		} `json:"operation"`
	}
	if err := json.Unmarshal(updateFileRec.Body.Bytes(), &updatedFile); err != nil {
		t.Fatalf("decode updated file: %v", err)
	}
	if updatedFile.Operation.Status != "completed" || updatedFile.Operation.BytesTransferred != 42 || updatedFile.Operation.DownloadURL == "" {
		t.Fatalf("updated file operation = %+v, want worker result", updatedFile.Operation)
	}
}

func TestMobileBackendSSHSessionWorkerClaimsAndUpdatesSession(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-backend-ssh-worker@example.com")
	clearMobileBackendSSHSessionsForTest(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions", strings.NewReader(`{"server_profile_id":"prod-root"}`))
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()
	MobileBackendSSHSessionsHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Session struct {
			SessionID string `json:"session_id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	sessionID := created.Session.SessionID
	if sessionID == "" {
		t.Fatal("created session missing id")
	}

	inputReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/input", strings.NewReader(`{"input":"uptime"}`))
	inputReq.SetPathValue("sessionId", sessionID)
	inputReq.Header.Set("Authorization", "Bearer "+viewerToken)
	inputRec := httptest.NewRecorder()
	MobileBackendSSHSessionInputHandler(identity).ServeHTTP(inputRec, inputReq)
	if inputRec.Code != http.StatusAccepted {
		t.Fatalf("input status = %d body=%s", inputRec.Code, inputRec.Body.String())
	}

	claimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimRec := httptest.NewRecorder()
	MobileBackendSSHSessionClaimHandler(identity).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim status = %d body=%s", claimRec.Code, claimRec.Body.String())
	}
	var claimed struct {
		Status  string `json:"status"`
		Session struct {
			SessionID       string   `json:"session_id"`
			ServerProfileID string   `json:"server_profile_id"`
			PendingInput    []string `json:"pending_input"`
			ClaimedBy       string   `json:"claimed_by"`
		} `json:"session"`
	}
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claimed); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if claimed.Status != "claimed" || claimed.Session.SessionID != sessionID || claimed.Session.ServerProfileID != "prod-root" {
		t.Fatalf("claimed = %+v, want backend ssh session", claimed)
	}
	if len(claimed.Session.PendingInput) != 1 || claimed.Session.PendingInput[0] != "uptime" || claimed.Session.ClaimedBy != enroll.MachineID {
		t.Fatalf("claimed pending input = %+v", claimed.Session)
	}

	realtime := &mobileRealtimeFakeWriter{}
	mobileBackendSSHSessions.Lock()
	realtimeRecord := mobileBackendSSHSessions.sessions[sessionID]
	mobileBackendSSHSessions.Unlock()
	_, cleanupRealtime := mobileRealtimeRegister(realtimeRecord.TenantID, realtimeRecord.OwnerID, realtime)
	defer cleanupRealtime()

	invalidChunkReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/ssh/sessions/"+sessionID+"/worker", strings.NewReader(`{"status":"connecting","state":"connecting","recent_output":"booting","output_chunk":"booting"}`))
	invalidChunkReq.SetPathValue("sessionId", sessionID)
	invalidChunkReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	invalidChunkReq.Header.Set("X-Machine-ID", enroll.MachineID)
	invalidChunkRec := httptest.NewRecorder()
	MobileBackendSSHSessionUpdateHandler(identity).ServeHTTP(invalidChunkRec, invalidChunkReq)
	if invalidChunkRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid output chunk update status = %d body=%s", invalidChunkRec.Code, invalidChunkRec.Body.String())
	}
	if !strings.Contains(invalidChunkRec.Body.String(), "backend_session_id") {
		t.Fatalf("invalid output chunk update body = %s, want backend_session_id error", invalidChunkRec.Body.String())
	}
	if len(realtime.messages) != 0 {
		t.Fatalf("realtime messages after invalid output chunk = %#v, want none", realtime.messages)
	}
	mobileBackendSSHSessions.Lock()
	unboundRecord := mobileBackendSSHSessions.sessions[sessionID]
	mobileBackendSSHSessions.Unlock()
	if unboundRecord.BackendSessionID != "" || unboundRecord.OutputSeq != 0 || unboundRecord.OutputChunk != "" {
		t.Fatalf("unbound record mutated by invalid output chunk = %+v", unboundRecord)
	}

	invalidConnectedReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/ssh/sessions/"+sessionID+"/worker", strings.NewReader(`{"status":"connected","state":"running","recent_output":"connected","output_chunk":"connected"}`))
	invalidConnectedReq.SetPathValue("sessionId", sessionID)
	invalidConnectedReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	invalidConnectedReq.Header.Set("X-Machine-ID", enroll.MachineID)
	invalidConnectedRec := httptest.NewRecorder()
	MobileBackendSSHSessionUpdateHandler(identity).ServeHTTP(invalidConnectedRec, invalidConnectedReq)
	if invalidConnectedRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid connected update status = %d body=%s", invalidConnectedRec.Code, invalidConnectedRec.Body.String())
	}
	if !strings.Contains(invalidConnectedRec.Body.String(), "backend_session_id") {
		t.Fatalf("invalid connected update body = %s, want backend_session_id error", invalidConnectedRec.Body.String())
	}
	if len(realtime.messages) != 0 {
		t.Fatalf("realtime messages after invalid update = %#v, want none", realtime.messages)
	}

	updateReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/ssh/sessions/"+sessionID+"/worker", strings.NewReader(`{"status":"connected","state":"running","backend_session_id":"ssh-prod","recent_output":"connected\n$ uptime\n1 day","output_chunk":"\n$ uptime\n1 day","clear_pending_input":true}`))
	updateReq.SetPathValue("sessionId", sessionID)
	updateReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	updateReq.Header.Set("X-Machine-ID", enroll.MachineID)
	updateRec := httptest.NewRecorder()
	MobileBackendSSHSessionUpdateHandler(identity).ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updated struct {
		SessionID        string `json:"session_id"`
		BackendSessionID string `json:"backend_session_id"`
		Status           string `json:"status"`
		State            string `json:"state"`
		RecentOutput     string `json:"recent_output"`
		OutputChunk      string `json:"output_chunk"`
		OutputSeq        int64  `json:"output_seq"`
		PendingCount     int    `json:"pending_input_count"`
		ClaimedBy        string `json:"claimed_by"`
	}
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.SessionID != sessionID || updated.BackendSessionID != "ssh-prod" || updated.Status != "connected" || updated.State != "running" {
		t.Fatalf("updated = %+v, want connected backend session", updated)
	}
	if updated.RecentOutput != "connected\n$ uptime\n1 day" || updated.OutputChunk != "$ uptime\n1 day" || updated.OutputSeq != 1 || updated.PendingCount != 0 || updated.ClaimedBy != enroll.MachineID {
		t.Fatalf("updated output/pending = %+v", updated)
	}
	if len(realtime.messages) != 1 {
		t.Fatalf("realtime messages = %#v, want one update", realtime.messages)
	}
	if realtime.messages[0]["type"] != "ssh_session" || realtime.messages[0]["session_id"] != sessionID || realtime.messages[0]["output_seq"] != int64(1) {
		t.Fatalf("realtime update = %#v, want ssh output seq", realtime.messages[0])
	}
	nested, ok := realtime.messages[0]["session"].(map[string]any)
	if !ok || nested["output_chunk"] != "$ uptime\n1 day" {
		t.Fatalf("realtime nested session = %#v, want output chunk", realtime.messages[0]["session"])
	}

	activeClaimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/claim", nil)
	activeClaimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	activeClaimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	activeClaimRec := httptest.NewRecorder()
	MobileBackendSSHSessionClaimHandler(identity).ServeHTTP(activeClaimRec, activeClaimReq)
	if activeClaimRec.Code != http.StatusOK {
		t.Fatalf("active claim status = %d body=%s", activeClaimRec.Code, activeClaimRec.Body.String())
	}
	var activeClaimed struct {
		Status  string `json:"status"`
		Session struct {
			SessionID string `json:"session_id"`
			Status    string `json:"status"`
		} `json:"session"`
	}
	if err := json.Unmarshal(activeClaimRec.Body.Bytes(), &activeClaimed); err != nil {
		t.Fatalf("decode active claim response: %v", err)
	}
	if activeClaimed.Status != "claimed" || activeClaimed.Session.SessionID != sessionID || activeClaimed.Session.Status != "connected" {
		t.Fatalf("active claim = %+v, want same connected session for continuous output polling", activeClaimed)
	}

	interruptReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/interrupt", nil)
	interruptReq.SetPathValue("sessionId", sessionID)
	interruptReq.Header.Set("Authorization", "Bearer "+viewerToken)
	interruptRec := httptest.NewRecorder()
	MobileBackendSSHSessionInterruptHandler(identity).ServeHTTP(interruptRec, interruptReq)
	if interruptRec.Code != http.StatusOK {
		t.Fatalf("interrupt status = %d body=%s", interruptRec.Code, interruptRec.Body.String())
	}
	interruptClaimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/claim", nil)
	interruptClaimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	interruptClaimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	interruptClaimRec := httptest.NewRecorder()
	MobileBackendSSHSessionClaimHandler(identity).ServeHTTP(interruptClaimRec, interruptClaimReq)
	if interruptClaimRec.Code != http.StatusOK {
		t.Fatalf("interrupt claim status = %d body=%s", interruptClaimRec.Code, interruptClaimRec.Body.String())
	}
	var interruptClaimed struct {
		Status  string `json:"status"`
		Session struct {
			SessionID string `json:"session_id"`
			Status    string `json:"status"`
			State     string `json:"state"`
		} `json:"session"`
	}
	if err := json.Unmarshal(interruptClaimRec.Body.Bytes(), &interruptClaimed); err != nil {
		t.Fatalf("decode interrupt claim response: %v", err)
	}
	if interruptClaimed.Status != "claimed" || interruptClaimed.Session.SessionID != sessionID || interruptClaimed.Session.Status != "interrupt_requested" || interruptClaimed.Session.State != "interrupting" {
		t.Fatalf("interrupt claim = %+v, want interrupt_requested session", interruptClaimed)
	}
}

func TestMobileBackendSSHSessionListIsOwnerScoped(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	ownerToken, _ := issueViewerToken(t, identity, "mobile-ssh-owner@example.com")
	otherToken, _ := issueViewerToken(t, identity, "mobile-ssh-other@example.com")
	clearMobileBackendSSHSessionsForTest(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions", strings.NewReader(`{"server_profile_id":"owner-prod"}`))
	createReq.Header.Set("Authorization", "Bearer "+ownerToken)
	createRec := httptest.NewRecorder()
	MobileBackendSSHSessionsHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/mobile/ssh/sessions", nil)
	listReq.Header.Set("Authorization", "Bearer "+otherToken)
	listRec := httptest.NewRecorder()
	MobileBackendSSHSessionsHandler(identity).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Sessions) != 0 {
		t.Fatalf("other user sessions = %#v, want none", listed.Sessions)
	}
}

func TestMobileDigitalEmployeesHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/digital-employees", nil)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeesHandler(nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}

func TestMobileEmployeeOwnedByViewer(t *testing.T) {
	if !mobileEmployeeOwnedByViewer(digitalEmployeeEntry{OwnerUserID: "u1"}, "u1") {
		t.Fatal("want owned")
	}
	if mobileEmployeeOwnedByViewer(digitalEmployeeEntry{OwnerUserID: "u2"}, "u1") {
		t.Fatal("other owner")
	}
	if mobileEmployeeOwnedByViewer(digitalEmployeeEntry{}, "u1") {
		t.Fatal("empty owner is pool, not own")
	}
	if mobileEmployeeListScope(false) != "own" || mobileEmployeeListScope(true) != "shared" {
		t.Fatal("scope labels")
	}
}

func TestMobileEmployeeGroupVisibilityForSharedPool(t *testing.T) {
	// Owned entry with group restriction is still visible to owner.
	owned := digitalEmployeeEntry{
		OwnerUserID: "u1", Status: veStatusActive, VisibleGroupIDs: []string{"dept-legal"},
	}
	if !mobileEmployeeOwnedByViewer(owned, "u1") {
		t.Fatal("owned")
	}
	// Pool entry with groups: hidden when path not resolved.
	pool := digitalEmployeeEntry{
		OwnerUserID: "other", Status: veStatusActive, VisibleGroupIDs: []string{"dept-legal"},
	}
	if veVisibleToRequester(pool, nil, false) {
		t.Fatal("unresolved path must hide group-restricted pool entry")
	}
	// Visible when path includes group.
	if !veVisibleToRequester(pool, []string{"dept-legal", "org-root"}, true) {
		t.Fatal("want visible for matching group")
	}
	if veVisibleToRequester(pool, []string{"dept-finance"}, true) {
		t.Fatal("wrong group must hide")
	}
	// Unrestricted pool entry is visible without group path.
	open := digitalEmployeeEntry{OwnerUserID: "other", Status: veStatusActive}
	if !veVisibleToRequester(open, nil, false) {
		t.Fatal("open pool should be visible")
	}
}

func TestMobileFilterEmployeesForViewerOwnVsShared(t *testing.T) {
	entries := []digitalEmployeeEntry{
		{ID: "own1", OwnerUserID: "u1", Status: veStatusActive, Name: "Mine"},
		{ID: "pool-legal", OwnerUserID: "other", Status: veStatusActive, Name: "Legal", VisibleGroupIDs: []string{"dept-legal"}},
		{ID: "pool-open", OwnerUserID: "other", Status: veStatusActive, Name: "Open"},
		{ID: "inactive", OwnerUserID: "u1", Status: veStatusDisabled, Name: "Off"},
		{ID: "other-owner", OwnerUserID: "u2", Status: veStatusActive, Name: "OtherOwn"},
	}
	// free/own: only owned active
	ownOnly := mobileFilterEmployeesForViewer(entries, "u1", false, nil, false)
	if len(ownOnly) != 1 || ownOnly[0].ID != "own1" {
		t.Fatalf("ownOnly=%#v", ownOnly)
	}
	// shared + matching group: own + legal pool + open pool (+ unrestricted other-owner as open pool)
	sharedIn := mobileFilterEmployeesForViewer(entries, "u1", true, []string{"dept-legal"}, true)
	ids := map[string]bool{}
	for _, e := range sharedIn {
		ids[e.ID] = true
	}
	if !ids["own1"] || !ids["pool-legal"] || !ids["pool-open"] || !ids["other-owner"] {
		t.Fatalf("sharedIn=%#v", sharedIn)
	}
	if ids["inactive"] {
		t.Fatalf("inactive leaked: %#v", sharedIn)
	}
	// shared but wrong group: own + open + unrestricted other-owner; legal pool hidden
	sharedOut := mobileFilterEmployeesForViewer(entries, "u1", true, []string{"dept-finance"}, true)
	ids2 := map[string]bool{}
	for _, e := range sharedOut {
		ids2[e.ID] = true
	}
	if !ids2["own1"] || !ids2["pool-open"] || !ids2["other-owner"] || ids2["pool-legal"] {
		t.Fatalf("sharedOut=%#v", sharedOut)
	}
	// owner always sees own even with unresolved group path in shared mode
	ownedRestricted := []digitalEmployeeEntry{
		{ID: "mine-g", OwnerUserID: "u1", Status: veStatusActive, VisibleGroupIDs: []string{"dept-secret"}},
	}
	got := mobileFilterEmployeesForViewer(ownedRestricted, "u1", true, nil, false)
	if len(got) != 1 {
		t.Fatalf("owner bypass groups: %#v", got)
	}
}

func TestMobileDigitalEmployeesHandlerListsBoundRemoteMachine(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-machine-ve@example.com")
	if err := identity.MachinesRepo().UpdateStatus(context.Background(), enroll.MachineID, "online"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := identity.MachinesRepo().UpdateAlias(context.Background(), enroll.MachineID, "Office Desktop"); err != nil {
		t.Fatalf("UpdateAlias: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/digital-employees", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeesHandler(identity, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Employees []struct {
			ID               string `json:"id"`
			MachineID        string `json:"machine_id"`
			Name             string `json:"name"`
			EmployeeType     string `json:"employee_type"`
			SkillDescription string `json:"skill_description"`
			AccessPolicy     string `json:"access_policy"`
			OnlineStatus     string `json:"online_status"`
			Resident         bool   `json:"resident"`
			RuntimeMissing   bool   `json:"runtime_missing"`
		} `json:"employees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Employees) != 1 {
		t.Fatalf("employees = %+v, want one bound machine entry", body.Employees)
	}
	employee := body.Employees[0]
	if employee.ID != "ve_"+enroll.MachineID || employee.MachineID != enroll.MachineID {
		t.Fatalf("employee identity = %+v, want ve alias for bound machine", employee)
	}
	if employee.Name != "Office Desktop" || employee.EmployeeType != veEmployeeTypePhysical {
		t.Fatalf("employee profile = %+v, want physical Office Desktop", employee)
	}
	if employee.OnlineStatus != veOnlineStatusOnline || !employee.Resident || employee.RuntimeMissing {
		t.Fatalf("employee availability = %+v, want online resident runtime-ready entry", employee)
	}
	if employee.AccessPolicy != "public" || !strings.Contains(employee.SkillDescription, "远程电脑/服务器") {
		t.Fatalf("employee capability = %+v, want mobile remote machine capability", employee)
	}
}

// Online status must follow Hub live presence (same as GET /api/ve/list), not
// stale machines.status alone.
func TestMobileDigitalEmployeesHandlerUsesLivePresenceLikeHubList(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-presence-ve@example.com")
	// DB claims online, but live presence says offline — mobile must show offline.
	if err := identity.MachinesRepo().UpdateStatus(context.Background(), enroll.MachineID, "online"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := identity.MachinesRepo().UpdateAlias(context.Background(), enroll.MachineID, "Stale Online Desktop"); err != nil {
		t.Fatalf("UpdateAlias: %v", err)
	}
	presence := fakeVEMachinePresence{infos: map[string]*device.MachineRuntimeInfo{
		enroll.MachineID: {MachineID: enroll.MachineID, Online: false},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/digital-employees", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	MobileDigitalEmployeesHandler(identity, nil, nil, presence).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Employees []struct {
			MachineID    string `json:"machine_id"`
			OnlineStatus string `json:"online_status"`
		} `json:"employees"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Employees) != 1 {
		t.Fatalf("employees=%+v", body.Employees)
	}
	if body.Employees[0].OnlineStatus != veOnlineStatusOffline {
		t.Fatalf("online_status=%q, want offline from live presence (Hub VE list rule)", body.Employees[0].OnlineStatus)
	}

	// Flip presence to online; mobile must show online even if DB is offline.
	if err := identity.MachinesRepo().UpdateStatus(context.Background(), enroll.MachineID, "offline"); err != nil {
		t.Fatalf("UpdateStatus offline: %v", err)
	}
	presence.infos[enroll.MachineID] = &device.MachineRuntimeInfo{MachineID: enroll.MachineID, Online: true}
	rec2 := httptest.NewRecorder()
	MobileDigitalEmployeesHandler(identity, nil, nil, presence).ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec2.Code, rec2.Body.String())
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode2: %v", err)
	}
	if len(body.Employees) != 1 || body.Employees[0].OnlineStatus != veOnlineStatusOnline {
		t.Fatalf("employees=%+v, want online from live presence", body.Employees)
	}
}

func TestMobileDigitalEmployeeTaskHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ops/tasks", strings.NewReader(`{"prompt":"check disk"}`))
	rec := httptest.NewRecorder()

	MobileDigitalEmployeeTaskHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeeTaskClaimHandlerRequiresWorkerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ops/tasks/claim", nil)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeeTaskClaimHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeeTaskStatusHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/digital-employees/tasks/mobve_1", nil)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeeTaskStatusHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeeTaskWorkersKeepTenantsIsolated(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-ve-tenant-worker@example.com")
	clearMobileDigitalEmployeeTasksForTest(t)
	const taskID = "mobve-other-tenant"
	mobileDigitalEmployeeTasks.Lock()
	mobileDigitalEmployeeTasks.tasks[taskID] = mobileDigitalEmployeeTaskRecord{
		TaskID: taskID, EmployeeID: "ve_" + enroll.MachineID,
		OwnerID: enroll.UserID, TenantID: "other-tenant", Status: "queued",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileDigitalEmployeeTasks.Unlock()

	claimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskClaimAnyHandler(identity).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK || !strings.Contains(claimRec.Body.String(), `"status":"empty"`) {
		t.Fatalf("cross-tenant claim status=%d body=%s", claimRec.Code, claimRec.Body.String())
	}

	updateReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(`{"status":"done","result":"should not write"}`))
	updateReq.SetPathValue("taskId", taskID)
	updateReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	updateReq.Header.Set("X-Machine-ID", enroll.MachineID)
	updateRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant update status=%d body=%s, want 404", updateRec.Code, updateRec.Body.String())
	}
	mobileDigitalEmployeeTasks.Lock()
	got := mobileDigitalEmployeeTasks.tasks[taskID]
	mobileDigitalEmployeeTasks.Unlock()
	if got.Status != "queued" || got.Result != "" {
		t.Fatalf("cross-tenant worker mutated task: %#v", got)
	}
}

func TestMobileDigitalEmployeeTaskUpdateHandlerRequiresWorkerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/mobve_1", strings.NewReader(`{"status":"done","result":"ok"}`))
	rec := httptest.NewRecorder()

	MobileDigitalEmployeeTaskUpdateHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeeTaskPayloadIncludesRemoteWorkFields(t *testing.T) {
	now := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	payload := mobileDigitalEmployeeTaskPayload(mobileDigitalEmployeeTaskRecord{
		TaskID:     "mobve_1",
		EmployeeID: "ops",
		Prompt:     "check disk",
		TaskType:   "server_maintenance",
		Context:    map[string]string{"source": "maclaw_mobile"},
		Status:     "done",
		Result:     "disk ok",
		Message:    "remote task completed",
		ClaimedBy:  "machine_1",
		CreatedAt:  now,
		UpdatedAt:  now,
	})

	if payload["prompt"] != "check disk" {
		t.Fatalf("prompt = %v, want check disk", payload["prompt"])
	}
	if payload["task_type"] != "server_maintenance" {
		t.Fatalf("task_type = %v, want server_maintenance", payload["task_type"])
	}
	contextValue, ok := payload["context"].(map[string]string)
	if !ok || contextValue["source"] != "maclaw_mobile" {
		t.Fatalf("context = %#v, want mobile source", payload["context"])
	}
	if payload["claimed_by"] != "machine_1" {
		t.Fatalf("claimed_by = %v, want machine_1", payload["claimed_by"])
	}
	if payload["status"] != "done" || payload["result"] != "disk ok" {
		t.Fatalf("payload = %#v, want final task status and result", payload)
	}
	if payload["message"] != "remote task completed" {
		t.Fatalf("message = %v, want remote task completed", payload["message"])
	}
}

func TestMobileDigitalEmployeeTaskBulkClaimSameUserAnyEmployee(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-ve-bulk@example.com")
	clearMobileDigitalEmployeeTasksForTest(t)

	// Task targeted at a named/platform employee id (not ve_<machine>).
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ve_annie_shared/tasks", strings.NewReader(`{"prompt":"北京天气","task_type":"general"}`))
	createReq.SetPathValue("employeeId", "ve_annie_shared")
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}

	// Bulk claim by same user's machine — should pick it up even though employee id ≠ machine id.
	claimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskClaimAnyHandler(identity).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimRec.Code, claimRec.Body.String())
	}
	var claimed struct {
		Status string `json:"status"`
		Task   struct {
			TaskID     string `json:"task_id"`
			EmployeeID string `json:"employee_id"`
			Status     string `json:"status"`
			ClaimedBy  string `json:"claimed_by"`
		} `json:"task"`
	}
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	if claimed.Status != "claimed" || claimed.Task.EmployeeID != "ve_annie_shared" || claimed.Task.Status != "in_progress" {
		t.Fatalf("claimed=%+v body=%s", claimed, claimRec.Body.String())
	}
	if claimed.Task.ClaimedBy != enroll.MachineID {
		t.Fatalf("claimed_by=%q want machine", claimed.Task.ClaimedBy)
	}
}

func TestMobileDigitalEmployeeTaskMachineClaimsVEAliasAndUpdates(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-ve@example.com")
	clearMobileDigitalEmployeeTasksForTest(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ve_"+enroll.MachineID+"/tasks", strings.NewReader(`{"prompt":"check disk","task_type":"server_maintenance","context":{"source":"maclaw_mobile","machine_id":"desktop-1"}}`))
	createReq.SetPathValue("employeeId", "ve_"+enroll.MachineID)
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	taskID, _ := created["task_id"].(string)
	if taskID == "" {
		t.Fatalf("created response missing task_id: %#v", created)
	}
	if created["task_type"] != "server_maintenance" {
		t.Fatalf("created task_type = %v, want server_maintenance", created["task_type"])
	}

	claimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/"+enroll.MachineID+"/tasks/claim", nil)
	claimReq.SetPathValue("employeeId", enroll.MachineID)
	claimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskClaimHandler(identity).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim status = %d body=%s", claimRec.Code, claimRec.Body.String())
	}
	var claimed struct {
		Status string `json:"status"`
		Task   struct {
			TaskID    string            `json:"task_id"`
			TaskType  string            `json:"task_type"`
			Context   map[string]string `json:"context"`
			Status    string            `json:"status"`
			ClaimedBy string            `json:"claimed_by"`
		} `json:"task"`
	}
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claimed); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if claimed.Status != "claimed" || claimed.Task.TaskID != taskID || claimed.Task.Status != "in_progress" || claimed.Task.ClaimedBy != enroll.MachineID {
		t.Fatalf("claimed response = %+v, want alias-matched in_progress task", claimed)
	}
	if claimed.Task.TaskType != "server_maintenance" || claimed.Task.Context["source"] != "maclaw_mobile" {
		t.Fatalf("claimed mobile context = %+v", claimed.Task)
	}

	updateReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(`{"status":"done","result":"disk ok","message":"checked on remote host"}`))
	updateReq.SetPathValue("taskId", taskID)
	updateReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	updateReq.Header.Set("X-Machine-ID", enroll.MachineID)
	updateRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(updateRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated["status"] != "done" || updated["result"] != "disk ok" || updated["message"] != "checked on remote host" || updated["claimed_by"] != enroll.MachineID {
		t.Fatalf("updated response = %#v", updated)
	}
}

// Hosted VE: worker machine owner may differ from phone task owner. ClaimedBy
// must authorize progress/completion; realtime targets OwnerID; terminal status
// rejects later in_progress patches.
func TestMobileDigitalEmployeeTaskUpdateHostedWorkerBroadcastsToOwner(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	phoneToken, _ := issueViewerToken(t, identity, "mobile-phone-owner@example.com")
	_, hostEnroll := issueViewerToken(t, identity, "mobile-host-worker@example.com")
	clearMobileDigitalEmployeeTasksForTest(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ve_hosted_shared/tasks", strings.NewReader(`{"prompt":"status check","task_type":"general"}`))
	createReq.SetPathValue("employeeId", "ve_hosted_shared")
	createReq.Header.Set("Authorization", "Bearer "+phoneToken)
	createRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	taskID, _ := created["task_id"].(string)
	if taskID == "" {
		t.Fatalf("missing task_id: %#v", created)
	}

	mobileDigitalEmployeeTasks.Lock()
	rec := mobileDigitalEmployeeTasks.tasks[taskID]
	phoneOwnerID := rec.OwnerID
	rec.Status = "in_progress"
	rec.ClaimedBy = hostEnroll.MachineID
	rec.Result = "claimed by host"
	mobileDigitalEmployeeTasks.tasks[taskID] = rec
	mobileDigitalEmployeeTasks.Unlock()
	if phoneOwnerID == "" {
		t.Fatal("phone owner id empty")
	}

	tenantID := mobileTestTenantID(t, identity, phoneToken)

	mobileRealtimeClients.Lock()
	previous := mobileRealtimeClients.clients
	mobileRealtimeClients.clients = make(map[string]map[*mobileRealtimeClient]struct{})
	mobileRealtimeClients.Unlock()
	t.Cleanup(func() {
		mobileRealtimeClients.Lock()
		mobileRealtimeClients.clients = previous
		mobileRealtimeClients.Unlock()
	})
	phoneWriter := &mobileRealtimeFakeWriter{}
	_, cleanup := mobileRealtimeRegister(tenantID, phoneOwnerID, phoneWriter)
	defer cleanup()

	progressReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(`{"status":"in_progress","result":"正在检查主机状态…"}`))
	progressReq.SetPathValue("taskId", taskID)
	progressReq.Header.Set("Authorization", "Bearer "+hostEnroll.MachineToken)
	progressReq.Header.Set("X-Machine-ID", hostEnroll.MachineID)
	progressRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("progress update status=%d body=%s (hosted worker must update claimed task for other owner)", progressRec.Code, progressRec.Body.String())
	}
	var progress map[string]any
	if err := json.Unmarshal(progressRec.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress["status"] != "in_progress" || progress["result"] != "正在检查主机状态…" {
		t.Fatalf("progress payload = %#v", progress)
	}
	if len(phoneWriter.messages) < 1 {
		t.Fatalf("phone owner realtime messages = %d, want at least 1", len(phoneWriter.messages))
	}
	if phoneWriter.messages[0]["type"] != "digital_employee_task" || phoneWriter.messages[0]["status"] != "in_progress" {
		t.Fatalf("progress event = %#v", phoneWriter.messages[0])
	}

	doneReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(`{"status":"done","result":"主机正常"}`))
	doneReq.SetPathValue("taskId", taskID)
	doneReq.Header.Set("Authorization", "Bearer "+hostEnroll.MachineToken)
	doneReq.Header.Set("X-Machine-ID", hostEnroll.MachineID)
	doneRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(doneRec, doneReq)
	if doneRec.Code != http.StatusOK {
		t.Fatalf("done status=%d body=%s", doneRec.Code, doneRec.Body.String())
	}

	staleReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(`{"status":"in_progress","result":"late"}`))
	staleReq.SetPathValue("taskId", taskID)
	staleReq.Header.Set("Authorization", "Bearer "+hostEnroll.MachineToken)
	staleReq.Header.Set("X-Machine-ID", hostEnroll.MachineID)
	staleRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusConflict {
		t.Fatalf("stale progress status=%d want %d body=%s", staleRec.Code, http.StatusConflict, staleRec.Body.String())
	}

	// Cross-terminal flip (done → failed) must also be rejected.
	flipReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(`{"status":"failed","result":"should not overwrite"}`))
	flipReq.SetPathValue("taskId", taskID)
	flipReq.Header.Set("Authorization", "Bearer "+hostEnroll.MachineToken)
	flipReq.Header.Set("X-Machine-ID", hostEnroll.MachineID)
	flipRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(flipRec, flipReq)
	if flipRec.Code != http.StatusConflict {
		t.Fatalf("terminal flip status=%d want %d body=%s", flipRec.Code, http.StatusConflict, flipRec.Body.String())
	}

	// Idempotent same terminal re-report returns 200 without changing result.
	beforeMsgs := len(phoneWriter.messages)
	idemReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(`{"status":"done","result":"主机正常"}`))
	idemReq.SetPathValue("taskId", taskID)
	idemReq.Header.Set("Authorization", "Bearer "+hostEnroll.MachineToken)
	idemReq.Header.Set("X-Machine-ID", hostEnroll.MachineID)
	idemRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(idemRec, idemReq)
	if idemRec.Code != http.StatusOK {
		t.Fatalf("idempotent done status=%d body=%s", idemRec.Code, idemRec.Body.String())
	}
	if len(phoneWriter.messages) != beforeMsgs {
		t.Fatalf("idempotent done should not re-broadcast, msgs %d -> %d", beforeMsgs, len(phoneWriter.messages))
	}

	last := phoneWriter.messages[len(phoneWriter.messages)-1]
	if last["type"] != "digital_employee_task" || last["status"] != "done" {
		t.Fatalf("last realtime event = %#v, want done digital_employee_task", last)
	}
}

func TestMobileDigitalEmployeeTaskClaimPrefersOldestQueued(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-fifo-claim@example.com")
	clearMobileDigitalEmployeeTasksForTest(t)

	// Create two tasks; older CreatedAt should be claimed first regardless of map order.
	now := time.Now().UTC()
	mobileDigitalEmployeeTasks.Lock()
	// Resolve owner via a real create for principal consistency, then replace store.
	mobileDigitalEmployeeTasks.Unlock()
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ve_"+enroll.MachineID+"/tasks", strings.NewReader(`{"prompt":"newer","task_type":"general"}`))
	createReq.SetPathValue("employeeId", "ve_"+enroll.MachineID)
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create = %d %s", createRec.Code, createRec.Body.String())
	}

	mobileDigitalEmployeeTasks.Lock()
	var ownerID string
	for _, rec := range mobileDigitalEmployeeTasks.tasks {
		ownerID = rec.OwnerID
		break
	}
	mobileDigitalEmployeeTasks.tasks = map[string]mobileDigitalEmployeeTaskRecord{
		"mobve_new": {
			TaskID: "mobve_new", EmployeeID: "ve_" + enroll.MachineID, OwnerID: ownerID,
			TenantID: enroll.TenantID,
			Prompt:   "newer", TaskType: "general", Status: "queued",
			CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now.Add(2 * time.Minute),
		},
		"mobve_old": {
			TaskID: "mobve_old", EmployeeID: "ve_" + enroll.MachineID, OwnerID: ownerID,
			TenantID: enroll.TenantID,
			Prompt:   "older", TaskType: "general", Status: "queued",
			CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute),
		},
	}
	mobileDigitalEmployeeTasks.Unlock()

	claimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskClaimAnyHandler(identity).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim = %d %s", claimRec.Code, claimRec.Body.String())
	}
	var claimed struct {
		Status string `json:"status"`
		Task   struct {
			TaskID string `json:"task_id"`
			Prompt string `json:"prompt"`
		} `json:"task"`
	}
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	if claimed.Status != "claimed" || claimed.Task.TaskID != "mobve_old" || claimed.Task.Prompt != "older" {
		t.Fatalf("claimed=%+v, want oldest mobve_old", claimed)
	}
}

func TestMobileDigitalEmployeeTaskUpdateSkipsNoopProgressBroadcast(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-noop-progress@example.com")
	clearMobileDigitalEmployeeTasksForTest(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ve_"+enroll.MachineID+"/tasks", strings.NewReader(`{"prompt":"ping","task_type":"general"}`))
	createReq.SetPathValue("employeeId", "ve_"+enroll.MachineID)
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create = %d %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	taskID, _ := created["task_id"].(string)

	claimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskClaimAnyHandler(identity).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim = %d %s", claimRec.Code, claimRec.Body.String())
	}

	mobileDigitalEmployeeTasks.Lock()
	ownerID := mobileDigitalEmployeeTasks.tasks[taskID].OwnerID
	mobileDigitalEmployeeTasks.Unlock()
	tenantID := mobileTestTenantID(t, identity, viewerToken)

	mobileRealtimeClients.Lock()
	previous := mobileRealtimeClients.clients
	mobileRealtimeClients.clients = make(map[string]map[*mobileRealtimeClient]struct{})
	mobileRealtimeClients.Unlock()
	t.Cleanup(func() {
		mobileRealtimeClients.Lock()
		mobileRealtimeClients.clients = previous
		mobileRealtimeClients.Unlock()
	})
	writer := &mobileRealtimeFakeWriter{}
	_, cleanup := mobileRealtimeRegister(tenantID, ownerID, writer)
	defer cleanup()

	body := `{"status":"in_progress","result":"partial-1"}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(body))
		req.SetPathValue("taskId", taskID)
		req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
		req.Header.Set("X-Machine-ID", enroll.MachineID)
		rec := httptest.NewRecorder()
		MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update[%d] = %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if len(writer.messages) != 1 {
		t.Fatalf("realtime messages = %d, want 1 (second identical progress is no-op)", len(writer.messages))
	}
}

func TestMobileDigitalEmployeeTaskProgressBroadcastsToPhoneOwner(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enroll := issueViewerToken(t, identity, "mobile-progress@example.com")
	clearMobileDigitalEmployeeTasksForTest(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ve_"+enroll.MachineID+"/tasks", strings.NewReader(`{"prompt":"ping","task_type":"general"}`))
	createReq.SetPathValue("employeeId", "ve_"+enroll.MachineID)
	createReq.Header.Set("Authorization", "Bearer "+viewerToken)
	createRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create = %d %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	taskID, _ := created["task_id"].(string)

	claimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskClaimAnyHandler(identity).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim = %d %s", claimRec.Code, claimRec.Body.String())
	}

	mobileDigitalEmployeeTasks.Lock()
	ownerID := mobileDigitalEmployeeTasks.tasks[taskID].OwnerID
	mobileDigitalEmployeeTasks.Unlock()
	tenantID := mobileTestTenantID(t, identity, viewerToken)

	mobileRealtimeClients.Lock()
	previous := mobileRealtimeClients.clients
	mobileRealtimeClients.clients = make(map[string]map[*mobileRealtimeClient]struct{})
	mobileRealtimeClients.Unlock()
	t.Cleanup(func() {
		mobileRealtimeClients.Lock()
		mobileRealtimeClients.clients = previous
		mobileRealtimeClients.Unlock()
	})
	writer := &mobileRealtimeFakeWriter{}
	_, cleanup := mobileRealtimeRegister(tenantID, ownerID, writer)
	defer cleanup()

	updateReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/"+taskID, strings.NewReader(`{"status":"in_progress","result":"部分输出：磁盘 42%"}`))
	updateReq.SetPathValue("taskId", taskID)
	updateReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	updateReq.Header.Set("X-Machine-ID", enroll.MachineID)
	updateRec := httptest.NewRecorder()
	MobileDigitalEmployeeTaskUpdateHandler(identity).ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update = %d %s", updateRec.Code, updateRec.Body.String())
	}
	if len(writer.messages) != 1 {
		t.Fatalf("realtime messages = %d want 1: %#v", len(writer.messages), writer.messages)
	}
	ev := writer.messages[0]
	if ev["type"] != "digital_employee_task" || ev["status"] != "in_progress" {
		t.Fatalf("event = %#v", ev)
	}
	taskPayload, _ := ev["task"].(map[string]any)
	if taskPayload == nil || taskPayload["result"] != "部分输出：磁盘 42%" {
		t.Fatalf("task payload = %#v", taskPayload)
	}
}

// mobileTestTenantID resolves the viewer's tenant via bootstrap for realtime tests.
func mobileTestTenantID(t *testing.T, identity *auth.IdentityService, viewerToken string) string {
	t.Helper()
	bootReq := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap", nil)
	bootReq.Header.Set("Authorization", "Bearer "+viewerToken)
	bootRec := httptest.NewRecorder()
	MobileBootstrapHandler(identity, nil, nil).ServeHTTP(bootRec, bootReq)
	if bootRec.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", bootRec.Code, bootRec.Body.String())
	}
	var boot map[string]any
	if err := json.Unmarshal(bootRec.Body.Bytes(), &boot); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	user, _ := boot["user"].(map[string]any)
	tenantID, _ := user["tenant_id"].(string)
	if tenantID == "" {
		t.Fatal("bootstrap missing tenant_id")
	}
	return tenantID
}

func TestMobilePersistentStateRoundTrip(t *testing.T) {
	clearMobileStateForTest(t)
	path := filepath.Join(t.TempDir(), "mobile-state.json")
	t.Setenv(mobileStatePathEnv, path)
	mobileResetStatePersistenceForTest()
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)

	mobileDocuments.Lock()
	mobileDocuments.drafts["draft-1"] = mobileDocumentDraftRecord{
		ID:        "draft-1",
		OwnerID:   "user-1",
		TenantID:  "tenant-1",
		Title:     "\u5e94\u6025\u62a5\u544a",
		Template:  "report",
		Markdown:  "# \u5e94\u6025\u62a5\u544a\n\n\u5185\u5bb9",
		UpdatedAt: now,
	}
	mobileDocuments.exports["export-1"] = mobileDocumentExportRecord{
		JobID:     "export-1",
		DraftID:   "draft-1",
		OwnerID:   "user-1",
		TenantID:  "tenant-1",
		Format:    "markdown",
		Status:    "ready",
		CreatedAt: now,
	}
	mobileDocuments.uploads["upload-1"] = mobileDocumentUploadRecord{
		TaskID:     "upload-1",
		OwnerID:    "user-1",
		TenantID:   "tenant-1",
		Filename:   "incident.md",
		Status:     "ready",
		DraftID:    "draft-1",
		Message:    "\u6587\u4ef6\u5df2\u89e3\u6790\u4e3a\u79fb\u52a8\u7aef\u6587\u6863\u8349\u7a3f\u3002",
		UploadedAt: now,
		UpdatedAt:  now,
	}
	mobileDocuments.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	mobileDigitalEmployeeTasks.tasks["task-1"] = mobileDigitalEmployeeTaskRecord{
		TaskID:     "task-1",
		EmployeeID: "ve-machine",
		OwnerID:    "user-1",
		TenantID:   "tenant-1",
		Prompt:     "\u68c0\u67e5\u78c1\u76d8",
		Status:     "queued",
		Result:     "\u4efb\u52a1\u5df2\u63d0\u4ea4",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	mobileDigitalEmployeeTasks.Unlock()

	mobilePersistState()
	mobileDocuments.Lock()
	mobileDocuments.drafts = make(map[string]mobileDocumentDraftRecord)
	mobileDocuments.exports = make(map[string]mobileDocumentExportRecord)
	mobileDocuments.uploads = make(map[string]mobileDocumentUploadRecord)
	mobileDocuments.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	mobileDigitalEmployeeTasks.tasks = make(map[string]mobileDigitalEmployeeTaskRecord)
	mobileDigitalEmployeeTasks.Unlock()
	mobileResetStatePersistenceForTest()

	mobileEnsureStateLoaded()

	mobileDocuments.Lock()
	draft, hasDraft := mobileDocuments.drafts["draft-1"]
	exportJob, hasExport := mobileDocuments.exports["export-1"]
	upload, hasUpload := mobileDocuments.uploads["upload-1"]
	mobileDocuments.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	task, hasTask := mobileDigitalEmployeeTasks.tasks["task-1"]
	mobileDigitalEmployeeTasks.Unlock()
	if !hasDraft || draft.Title != "\u5e94\u6025\u62a5\u544a" {
		t.Fatalf("restored draft = %#v, present=%v", draft, hasDraft)
	}
	if !hasExport || exportJob.Status != "ready" {
		t.Fatalf("restored export = %#v, present=%v", exportJob, hasExport)
	}
	if !hasUpload || upload.Message != "\u6587\u4ef6\u5df2\u89e3\u6790\u4e3a\u79fb\u52a8\u7aef\u6587\u6863\u8349\u7a3f\u3002" {
		t.Fatalf("restored upload = %#v, present=%v", upload, hasUpload)
	}
	if !hasTask || task.Prompt != "\u68c0\u67e5\u78c1\u76d8" {
		t.Fatalf("restored task = %#v, present=%v", task, hasTask)
	}
}

func TestMobilePersistentStateNormalizesLegacyTenantIDs(t *testing.T) {
	clearMobileStateForTest(t)
	path := filepath.Join(t.TempDir(), "mobile-state.json")
	t.Setenv(mobileStatePathEnv, path)
	legacy := mobilePersistentState{
		Drafts:  map[string]mobileDocumentDraftRecord{"draft": {ID: "draft", OwnerID: "owner"}},
		Exports: map[string]mobileDocumentExportRecord{"export": {JobID: "export", OwnerID: "owner"}},
		Uploads: map[string]mobileDocumentUploadRecord{"upload": {TaskID: "upload", OwnerID: "owner"}},
		MeetingRecordings: map[string]mobileMeetingRecording{"recording": {
			ID: "recording", OwnerID: "owner", Status: "ready",
			RetentionUntil: time.Now().UTC().Add(time.Hour),
		}},
		DigitalEmployeeTasks: map[string]mobileDigitalEmployeeTaskRecord{"task": {TaskID: "task", OwnerID: "owner"}},
		ServerProfiles:       map[string]mobileServerProfileRecord{"profile": {ProfileID: "profile", OwnerID: "owner"}},
		SSHVaultSecrets:      map[string]mobileSSHVaultRecord{"secret": {ProfileID: "secret", OwnerID: "owner"}},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	mobileResetStatePersistenceForTest()
	mobileEnsureStateLoaded()
	mobileDocuments.Lock()
	draft, export, upload := mobileDocuments.drafts["draft"], mobileDocuments.exports["export"], mobileDocuments.uploads["upload"]
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	recording := mobileMeetingRecordings.items["recording"]
	mobileMeetingRecordings.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	task := mobileDigitalEmployeeTasks.tasks["task"]
	mobileDigitalEmployeeTasks.Unlock()
	if draft.TenantID != "default" || export.TenantID != "default" || upload.TenantID != "default" || recording.TenantID != "default" || task.TenantID != "default" {
		t.Fatalf("legacy state tenant normalization failed: draft=%q export=%q upload=%q recording=%q task=%q", draft.TenantID, export.TenantID, upload.TenantID, recording.TenantID, task.TenantID)
	}
}

func clearMobileDigitalEmployeeTasksForTest(t *testing.T) {
	t.Helper()
	mobileDigitalEmployeeTasks.Lock()
	previous := mobileDigitalEmployeeTasks.tasks
	mobileDigitalEmployeeTasks.tasks = make(map[string]mobileDigitalEmployeeTaskRecord)
	mobileDigitalEmployeeTasks.Unlock()
	t.Cleanup(func() {
		mobileDigitalEmployeeTasks.Lock()
		mobileDigitalEmployeeTasks.tasks = previous
		mobileDigitalEmployeeTasks.Unlock()
	})
}

func clearMobileBackendSSHSessionsForTest(t *testing.T) {
	t.Helper()
	mobileBackendSSHSessions.Lock()
	previous := mobileBackendSSHSessions.sessions
	mobileBackendSSHSessions.sessions = make(map[string]mobileBackendSSHSessionRecord)
	mobileBackendSSHSessions.Unlock()
	mobileBackendSSHTasks.Lock()
	previousTasks := mobileBackendSSHTasks.tasks
	mobileBackendSSHTasks.tasks = make(map[string]mobileBackendSSHTaskRecord)
	mobileBackendSSHTasks.Unlock()
	mobileBackendSSHFileOperations.Lock()
	previousFileOperations := mobileBackendSSHFileOperations.operations
	mobileBackendSSHFileOperations.operations = make(map[string]mobileBackendSSHFileOperationRecord)
	mobileBackendSSHFileOperations.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		mobileBackendSSHSessions.sessions = previous
		mobileBackendSSHSessions.Unlock()
		mobileBackendSSHTasks.Lock()
		mobileBackendSSHTasks.tasks = previousTasks
		mobileBackendSSHTasks.Unlock()
		mobileBackendSSHFileOperations.Lock()
		mobileBackendSSHFileOperations.operations = previousFileOperations
		mobileBackendSSHFileOperations.Unlock()
	})
}

func clearMobileServerProfilesForTest(t *testing.T) {
	t.Helper()
	mobileServerProfiles.Lock()
	previous := mobileServerProfiles.profiles
	mobileServerProfiles.profiles = make(map[string]mobileServerProfileRecord)
	mobileServerProfiles.Unlock()
	t.Cleanup(func() {
		mobileServerProfiles.Lock()
		mobileServerProfiles.profiles = previous
		mobileServerProfiles.Unlock()
	})
}

func clearMobileLLMAuthorizationsForTest(t *testing.T) {
	t.Helper()
	mobileLlmAuthorizations.Lock()
	previous := mobileLlmAuthorizations.authorizations
	previousQRSessions := mobileLlmAuthorizations.qrSessions
	mobileLlmAuthorizations.authorizations = make(map[string]mobileLlmAuthorizationRecord)
	mobileLlmAuthorizations.qrSessions = make(map[string]mobileLlmQRSessionRecord)
	mobileLlmAuthorizations.Unlock()
	t.Cleanup(func() {
		mobileLlmAuthorizations.Lock()
		mobileLlmAuthorizations.authorizations = previous
		mobileLlmAuthorizations.qrSessions = previousQRSessions
		mobileLlmAuthorizations.Unlock()
	})
}

func setMobileLLMAuthorizationPersistenceForTest(t *testing.T, system *mobileLLMTestSystemSettings, key []byte) {
	t.Helper()
	mobileLLMAuthorizationPersistence.Lock()
	previousSystem := mobileLLMAuthorizationPersistence.system
	previousKey := append([]byte(nil), mobileLLMAuthorizationPersistence.key...)
	mobileLLMAuthorizationPersistence.system = system
	mobileLLMAuthorizationPersistence.key = append([]byte(nil), key...)
	mobileLLMAuthorizationPersistence.Unlock()
	t.Cleanup(func() {
		mobileLLMAuthorizationPersistence.Lock()
		mobileLLMAuthorizationPersistence.system = previousSystem
		mobileLLMAuthorizationPersistence.key = previousKey
		mobileLLMAuthorizationPersistence.Unlock()
	})
}

func clearMobileStateForTest(t *testing.T) {
	t.Helper()
	mobileDocuments.Lock()
	previousDrafts := mobileDocuments.drafts
	previousExports := mobileDocuments.exports
	previousUploads := mobileDocuments.uploads
	mobileDocuments.drafts = make(map[string]mobileDocumentDraftRecord)
	mobileDocuments.exports = make(map[string]mobileDocumentExportRecord)
	mobileDocuments.uploads = make(map[string]mobileDocumentUploadRecord)
	mobileDocuments.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	previousTasks := mobileDigitalEmployeeTasks.tasks
	mobileDigitalEmployeeTasks.tasks = make(map[string]mobileDigitalEmployeeTaskRecord)
	mobileDigitalEmployeeTasks.Unlock()
	mobileLlmAuthorizations.Lock()
	previousLLMAuthorizations := mobileLlmAuthorizations.authorizations
	previousLLMQRSessions := mobileLlmAuthorizations.qrSessions
	mobileLlmAuthorizations.authorizations = make(map[string]mobileLlmAuthorizationRecord)
	mobileLlmAuthorizations.qrSessions = make(map[string]mobileLlmQRSessionRecord)
	mobileLlmAuthorizations.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		mobileDocuments.drafts = previousDrafts
		mobileDocuments.exports = previousExports
		mobileDocuments.uploads = previousUploads
		mobileDocuments.Unlock()
		mobileDigitalEmployeeTasks.Lock()
		mobileDigitalEmployeeTasks.tasks = previousTasks
		mobileDigitalEmployeeTasks.Unlock()
		mobileLlmAuthorizations.Lock()
		mobileLlmAuthorizations.authorizations = previousLLMAuthorizations
		mobileLlmAuthorizations.qrSessions = previousLLMQRSessions
		mobileLlmAuthorizations.Unlock()
		mobileResetStatePersistenceForTest()
	})
}

func TestMobileProcessDocumentMarkdown(t *testing.T) {
	markdown := "# Incident\n\nService returned 502 for 10 minutes.\n\nNginx was restarted."

	summary := mobileProcessDocumentMarkdown("summarize", markdown)
	if !strings.Contains(summary, "# Incident \u6458\u8981") {
		t.Fatalf("summary = %q, want summary title", summary)
	}
	if !strings.Contains(summary, "Service returned 502") {
		t.Fatalf("summary = %q, want first point", summary)
	}

	formatted := mobileProcessDocumentMarkdown("format", markdown)
	if !strings.Contains(formatted, "- Service returned 502") {
		t.Fatalf("formatted = %q, want bullet formatting", formatted)
	}
}

func TestMobileDocumentUploadHandlerRejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload", nil)
	rec := httptest.NewRecorder()

	MobileDocumentUploadHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestMobileDocumentUploadStatusHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload/mobparse_1", nil)
	rec := httptest.NewRecorder()

	MobileDocumentUploadStatusHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDocumentUploadClaimHandlerClaimsPendingTask(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-claim@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 9, 50, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-claim"] = mobileDocumentUploadRecord{
		TaskID:      "upload-claim",
		OwnerID:     enroll.UserID,
		TenantID:    enroll.TenantID,
		Filename:    "screenshot.png",
		ContentType: "image/png",
		Status:      "needs_ocr",
		Message:     "\u56fe\u7247\u5df2\u5bfc\u5165\u4e3a\u79fb\u52a8\u7aef\u8349\u7a3f\uff0c\u7b49\u5f85 OCR/\u89c6\u89c9\u6a21\u578b\u8bc6\u522b\u3002",
		SourceBytes: []byte("png bytes"),
		UploadedAt:  now,
		UpdatedAt:   now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload/claim", nil)
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadClaimHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "claimed" {
		t.Fatalf("payload = %#v, want claimed", payload)
	}
	task, ok := payload["task"].(map[string]any)
	if !ok || task["task_id"] != "upload-claim" || task["status"] != "in_progress" || task["claimed_by"] != enroll.MachineID {
		t.Fatalf("task = %#v, want claimed in_progress task", payload["task"])
	}
	if task["source_download_url"] != "/api/mobile/documents/upload/upload-claim/source" {
		t.Fatalf("source_download_url = %v, want source URL", task["source_download_url"])
	}
}

func TestMobileDocumentUploadClaimHandlerFailsGhostNeedsOCR(t *testing.T) {
	// Online store + missing original: claim must terminal-fail instead of leaving
	// needs_ocr forever (Mobile would poll indefinitely).
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-claim-ghost@example.com")
	clearMobileStateForTest(t)
	t.Setenv(mobileBlobDirEnv, t.TempDir())

	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-ghost"] = mobileDocumentUploadRecord{
		TaskID:     "upload-ghost",
		OwnerID:    enroll.UserID,
		TenantID:   enroll.TenantID,
		Filename:   "gone.png",
		Status:     "needs_ocr",
		SourcePath: "missing/ghost.bin",
		SourceSize: 9999,
		UploadedAt: now,
		UpdatedAt:  now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload/claim", nil)
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadClaimHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["status"] != "no_task" {
		t.Fatalf("payload = %#v, want no_task", payload)
	}
	mobileDocuments.Lock()
	up := mobileDocuments.uploads["upload-ghost"]
	mobileDocuments.Unlock()
	if up.Status != "failed" {
		t.Fatalf("status=%q want failed", up.Status)
	}
	if up.SourcePath != "" || up.SourceSize != 0 {
		t.Fatalf("ghost meta not cleared: path=%q size=%d", up.SourcePath, up.SourceSize)
	}
	if !strings.Contains(up.Message, "原件不可用") {
		t.Fatalf("message=%q", up.Message)
	}
}

func TestMobileDocumentUploadClaimHandlerDocumentKindSkipsOCRTasks(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-claim-document@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 9, 52, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-ocr"] = mobileDocumentUploadRecord{
		TaskID:     "upload-ocr",
		OwnerID:    enroll.UserID,
		TenantID:   enroll.TenantID,
		Filename:   "screenshot.png",
		Status:     "needs_ocr",
		UploadedAt: now,
		UpdatedAt:  now,
	}
	mobileDocuments.uploads["upload-doc"] = mobileDocumentUploadRecord{
		TaskID:      "upload-doc",
		OwnerID:     enroll.UserID,
		TenantID:    enroll.TenantID,
		Filename:    "legacy.doc",
		Status:      "queued",
		SourceBytes: []byte("doc"),
		UploadedAt:  now,
		UpdatedAt:   now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload/claim?kind=document", nil)
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadClaimHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	task, ok := payload["task"].(map[string]any)
	if !ok || task["task_id"] != "upload-doc" {
		t.Fatalf("task = %#v, want queued document task", payload["task"])
	}
}

func TestMobileDocumentUploadClaimHandlerRequiresMachineToken(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-claim-viewer@example.com")
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload/claim", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	MobileDocumentUploadClaimHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s, want unauthorized", rec.Code, rec.Body.String())
	}
}

func TestMobileDocumentUploadWorkerOperationsKeepTenantsIsolated(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-worker-tenant@example.com")
	clearMobileStateForTest(t)
	now := time.Now().UTC()
	const taskID = "upload-worker-other-tenant"
	mobileDocuments.Lock()
	mobileDocuments.uploads[taskID] = mobileDocumentUploadRecord{
		TaskID: taskID, OwnerID: enroll.UserID, TenantID: "other-tenant",
		Filename: "private.png", Status: "needs_ocr", SourceBytes: []byte("source"),
		UploadedAt: now, UpdatedAt: now,
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.uploads, taskID)
		mobileDocuments.Unlock()
	})

	claimReq := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	claimReq.Header.Set("X-Machine-ID", enroll.MachineID)
	claimRec := httptest.NewRecorder()
	MobileDocumentUploadClaimHandler(identity).ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK || !strings.Contains(claimRec.Body.String(), `"status":"no_task"`) {
		t.Fatalf("cross-tenant claim status=%d body=%s", claimRec.Code, claimRec.Body.String())
	}

	resultReq := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/upload/"+taskID+"/result", strings.NewReader(`{"status":"ready","markdown":"# should not write"}`))
	resultReq.SetPathValue("taskId", taskID)
	resultReq.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	resultReq.Header.Set("X-Machine-ID", enroll.MachineID)
	resultRec := httptest.NewRecorder()
	MobileDocumentUploadResultHandler(identity).ServeHTTP(resultRec, resultReq)
	if resultRec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant result status=%d body=%s, want 404", resultRec.Code, resultRec.Body.String())
	}
	mobileDocuments.Lock()
	got := mobileDocuments.uploads[taskID]
	mobileDocuments.Unlock()
	if got.Status != "needs_ocr" || got.OCRMarkdown != "" {
		t.Fatalf("cross-tenant worker mutated task: %#v", got)
	}
}

func TestMobileDocumentUploadClaimReclaimsStaleInProgress(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-claim-stale@example.com")
	clearMobileStateForTest(t)
	staleAt := time.Now().UTC().Add(-mobileDocumentUploadClaimTimeout - time.Minute)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-stale"] = mobileDocumentUploadRecord{
		TaskID:      "upload-stale",
		OwnerID:     enroll.UserID,
		TenantID:    enroll.TenantID,
		Filename:    "shot.png",
		Status:      "in_progress",
		ClaimedBy:   "dead-machine",
		DraftID:     "mobdoc_stale",
		SourceBytes: []byte{0x89, 'P', 'N', 'G'},
		UploadedAt:  staleAt,
		UpdatedAt:   staleAt,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/documents/upload/claim?kind=ocr", nil)
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadClaimHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["status"] != "claimed" {
		t.Fatalf("status=%v want claimed", payload["status"])
	}
	task, ok := payload["task"].(map[string]any)
	if !ok || task["task_id"] != "upload-stale" {
		t.Fatalf("task=%#v", payload["task"])
	}
	if task["claimed_by"] != enroll.MachineID {
		t.Fatalf("claimed_by=%v want %s", task["claimed_by"], enroll.MachineID)
	}
}

func TestMobileDocumentUploadStatusReclaimsStaleInProgress(t *testing.T) {
	// Status polling (Mobile) must reclaim timed-out in_progress without waiting
	// for the next worker claim pass.
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-status-stale@example.com")
	clearMobileStateForTest(t)
	staleAt := time.Now().UTC().Add(-mobileDocumentUploadClaimTimeout - time.Minute)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-status-stale"] = mobileDocumentUploadRecord{
		TaskID:      "upload-status-stale",
		OwnerID:     enroll.UserID,
		TenantID:    enroll.TenantID,
		Filename:    "shot.png",
		Status:      "in_progress",
		ClaimedBy:   "dead-machine",
		DraftID:     "mobdoc_status_stale",
		SourceBytes: []byte{0x89, 'P', 'N', 'G'},
		UploadedAt:  staleAt,
		UpdatedAt:   staleAt,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload/upload-status-stale", nil)
	req.SetPathValue("taskId", "upload-status-stale")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileDocumentUploadStatusHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["status"] != "needs_ocr" {
		t.Fatalf("status=%v want needs_ocr after reclaim", payload["status"])
	}
	if claimed, _ := payload["claimed_by"].(string); claimed != "" {
		t.Fatalf("claimed_by=%q want empty after reclaim", claimed)
	}
	mobileDocuments.Lock()
	up := mobileDocuments.uploads["upload-status-stale"]
	mobileDocuments.Unlock()
	if up.Status != "needs_ocr" || up.ClaimedBy != "" {
		t.Fatalf("map state status=%q claimed_by=%q", up.Status, up.ClaimedBy)
	}
}

func TestMobileReclaimStaleDocumentUploadClaimsHelper(t *testing.T) {
	clearMobileStateForTest(t)
	now := time.Now().UTC()
	mobileDocuments.Lock()
	mobileDocuments.uploads["fresh"] = mobileDocumentUploadRecord{
		TaskID: "fresh", Status: "in_progress", UpdatedAt: now.Add(-time.Minute),
	}
	mobileDocuments.uploads["stale-img"] = mobileDocumentUploadRecord{
		TaskID: "stale-img", Filename: "a.png", Status: "in_progress",
		UpdatedAt: now.Add(-mobileDocumentUploadClaimTimeout - time.Second),
		ClaimedBy: "x",
	}
	n := mobileReclaimStaleDocumentUploadClaims(now)
	if n != 1 {
		t.Fatalf("reclaimed=%d want 1", n)
	}
	if mobileDocuments.uploads["stale-img"].Status != "needs_ocr" {
		t.Fatalf("stale status=%q", mobileDocuments.uploads["stale-img"].Status)
	}
	if mobileDocuments.uploads["fresh"].Status != "in_progress" {
		t.Fatalf("fresh should remain in_progress")
	}
	mobileDocuments.Unlock()
}

func TestMobileDocumentUploadSourceHandlerDownloadsClaimedSource(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-source@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 9, 55, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-source"] = mobileDocumentUploadRecord{
		TaskID:      "upload-source",
		OwnerID:     enroll.UserID,
		TenantID:    enroll.TenantID,
		Filename:    "incident.pdf",
		ContentType: "application/pdf",
		Status:      "in_progress",
		ClaimedBy:   enroll.MachineID,
		SourceBytes: []byte("%PDF mobile"),
		UpdatedAt:   now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload/upload-source/source", nil)
	req.SetPathValue("taskId", "upload-source")
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadSourceHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("content-type = %q, want application/pdf", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "%PDF mobile" {
		t.Fatalf("body = %q, want source bytes", rec.Body.String())
	}
}

func TestMobileDocumentUploadSourceHandlerRejectsOtherClaimedWorker(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-source-other@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 9, 58, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-source-other"] = mobileDocumentUploadRecord{
		TaskID:      "upload-source-other",
		OwnerID:     enroll.UserID,
		TenantID:    enroll.TenantID,
		Filename:    "incident.pdf",
		Status:      "in_progress",
		ClaimedBy:   "different-machine",
		SourceBytes: []byte("source"),
		UpdatedAt:   now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload/upload-source-other/source", nil)
	req.SetPathValue("taskId", "upload-source-other")
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadSourceHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want forbidden", rec.Code, rec.Body.String())
	}
}

func TestMobileDocumentUploadResultHandlerCompletesQueuedTask(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-ocr@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-queued"] = mobileDocumentUploadRecord{
		TaskID:     "upload-queued",
		OwnerID:    enroll.UserID,
		TenantID:   enroll.TenantID,
		Filename:   "incident.pdf",
		Status:     "queued",
		Message:    "\u5df2\u4e0a\u4f20\uff0c\u7b49\u5f85\u6587\u6863\u89e3\u6790\u7ba1\u7ebf\u5904\u7406\u3002",
		UploadedAt: now,
		UpdatedAt:  now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/upload/upload-queued/result", strings.NewReader(`{"status":"ready","markdown":"# Incident\n\nOCR text","message":"\u89e3\u6790\u5b8c\u6210"}`))
	req.SetPathValue("taskId", "upload-queued")
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadResultHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ready" || payload["message"] != "\u89e3\u6790\u5b8c\u6210" {
		t.Fatalf("payload = %#v, want ready parsed result", payload)
	}
	draft, ok := payload["draft"].(map[string]any)
	if !ok || !strings.Contains(draft["markdown"].(string), "OCR text") {
		t.Fatalf("payload draft = %#v, want OCR markdown draft", payload["draft"])
	}
}

func TestMobileDocumentUploadResultHandlerCompletesClaimedTask(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-claimed-result@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 10, 3, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-claimed"] = mobileDocumentUploadRecord{
		TaskID:    "upload-claimed",
		OwnerID:   enroll.UserID,
		TenantID:  enroll.TenantID,
		Filename:  "screenshot.png",
		Status:    "in_progress",
		ClaimedBy: enroll.MachineID,
		UpdatedAt: now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/upload/upload-claimed/result", strings.NewReader(`{"status":"ready","markdown":"# Screenshot\n\nOCR done"}`))
	req.SetPathValue("taskId", "upload-claimed")
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadResultHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ready" {
		t.Fatalf("payload = %#v, want ready", payload)
	}
}

func TestMobileDocumentUploadResultHandlerRejectsOtherClaimedWorker(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, ownerEnroll := issueViewerToken(t, identity, "mobile-claim-owner@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 10, 4, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-other-worker"] = mobileDocumentUploadRecord{
		TaskID:    "upload-other-worker",
		OwnerID:   ownerEnroll.UserID,
		TenantID:  ownerEnroll.TenantID,
		Filename:  "screenshot.png",
		Status:    "in_progress",
		ClaimedBy: "different-machine",
		UpdatedAt: now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/upload/upload-other-worker/result", strings.NewReader(`{"status":"ready","markdown":"# no"}`))
	req.SetPathValue("taskId", "upload-other-worker")
	req.Header.Set("Authorization", "Bearer "+ownerEnroll.MachineToken)
	req.Header.Set("X-Machine-ID", ownerEnroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadResultHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want forbidden", rec.Code, rec.Body.String())
	}
}

func TestMobileDocumentUploadResultHandlerFailsOCRTask(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-ocr-fail@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 10, 5, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-ocr"] = mobileDocumentUploadRecord{
		TaskID:     "upload-ocr",
		OwnerID:    enroll.UserID,
		TenantID:   enroll.TenantID,
		Filename:   "screenshot.png",
		Status:     "needs_ocr",
		DraftID:    "draft-ocr",
		UploadedAt: now,
		UpdatedAt:  now,
	}
	mobileDocuments.Unlock()

	req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/upload/upload-ocr/result", strings.NewReader(`{"status":"failed","error":"OCR \u670d\u52a1\u6682\u4e0d\u53ef\u7528\u3002"}`))
	req.SetPathValue("taskId", "upload-ocr")
	req.Header.Set("Authorization", "Bearer "+enroll.MachineToken)
	req.Header.Set("X-Machine-ID", enroll.MachineID)
	rec := httptest.NewRecorder()
	MobileDocumentUploadResultHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "failed" || payload["message"] != "OCR \u670d\u52a1\u6682\u4e0d\u53ef\u7528\u3002" {
		t.Fatalf("payload = %#v, want failed OCR result", payload)
	}
}

func TestMobileDocumentUploadResultHandlerRequiresMachineToken(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "mobile-ocr-viewer@example.com")
	req := httptest.NewRequest(http.MethodPatch, "/api/mobile/documents/upload/upload-1/result", strings.NewReader(`{"status":"ready","markdown":"# ok"}`))
	req.SetPathValue("taskId", "upload-1")
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()

	MobileDocumentUploadResultHandler(identity).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s, want unauthorized", rec.Code, rec.Body.String())
	}
}

func TestMobileSSHAnalysisPayloadDetectsDiskFull(t *testing.T) {
	payload := mobileSSHAnalysisPayload("write failed: no space left on device")

	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}
	if !strings.Contains(payload["summary"].(string), "\u78c1\u76d8\u7a7a\u95f4\u4e0d\u8db3") {
		t.Fatalf("summary = %v, want disk full summary", payload["summary"])
	}
	if !strings.Contains(payload["command_draft"].(string), "df -h") {
		t.Fatalf("command_draft = %v, want df -h", payload["command_draft"])
	}
}

func TestMobileUploadedTextDraftMarkdown(t *testing.T) {
	markdown, ok := mobileDraftMarkdownFromUpload("incident.log", []byte("panic: disk full"))
	if !ok {
		t.Fatal("mobileDraftMarkdownFromUpload returned ok=false")
	}
	if !strings.Contains(markdown, "# incident") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	if !strings.Contains(markdown, "```text") {
		t.Fatalf("markdown = %q, want text fence", markdown)
	}
}

func TestMobileUploadedEmptyTextDraftMarkdownUsesChineseFallback(t *testing.T) {
	markdown, ok := mobileDraftMarkdownFromUpload("empty.txt", []byte("  \n"))
	if !ok {
		t.Fatal("mobileDraftMarkdownFromUpload returned ok=false")
	}
	if !strings.Contains(markdown, "\u5bfc\u5165\u6587\u4ef6\u4e3a\u7a7a") {
		t.Fatalf("markdown = %q, want Chinese empty-file fallback", markdown)
	}
}

func TestMobileUploadTitleUsesChineseFallback(t *testing.T) {
	if title := mobileUploadTitle(".txt"); title != "\u5bfc\u5165\u6587\u6863" {
		t.Fatalf("title = %q, want \u5bfc\u5165\u6587\u6863", title)
	}
}

func TestMobileUploadedDOCXDraftMarkdown(t *testing.T) {
	data := mobileTestZip(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Incident report</w:t></w:r></w:p><w:p><w:r><w:t>Service recovered</w:t></w:r></w:p></w:body></w:document>`,
	})

	markdown, ok := mobileDraftMarkdownFromUpload("incident.docx", data)
	if !ok {
		t.Fatal("docx upload was not parsed")
	}
	if !strings.Contains(markdown, "# incident") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	if !strings.Contains(markdown, "Service recovered") {
		t.Fatalf("markdown = %q, want body text", markdown)
	}
}

func TestMobileSniffImageContentType(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0}
	if got := mobileSniffImageContentType(png); got != "image/png" {
		t.Fatalf("png sniff = %q", got)
	}
	jpg := []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0}
	if got := mobileSniffImageContentType(jpg); got != "image/jpeg" {
		t.Fatalf("jpeg sniff = %q", got)
	}
	if got := mobileSniffImageContentType([]byte("not-an-image")); got != "" {
		t.Fatalf("want empty sniff, got %q", got)
	}
}

func TestMobileDraftAppendImageMarkdownSkipsDuplicates(t *testing.T) {
	body := "# note\n\nhello\n\n![图1](/api/mobile/documents/drafts/d1/images/img1)\n"
	images := []mobileDocumentDraftImage{
		{ID: "img1", Filename: "a.png"},
		{ID: "img2", Filename: "b.png"},
	}
	got := mobileDraftAppendImageMarkdown(body, "d1", images)
	if strings.Count(got, "/images/img1") != 1 {
		t.Fatalf("img1 should appear once: %q", got)
	}
	if !strings.Contains(got, "/images/img2") {
		t.Fatalf("img2 missing: %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("body text lost: %q", got)
	}
	// All already linked → no empty 附图 section.
	onlyLinked := mobileDraftAppendImageMarkdown(body, "d1", []mobileDocumentDraftImage{
		{ID: "img1", Filename: "a.png"},
	})
	if strings.Contains(onlyLinked, "## 附图") {
		t.Fatalf("should not add empty 附图 section: %q", onlyLinked)
	}
}

func TestMobileDOCXExtractsEmbeddedPNG(t *testing.T) {
	// Durable blob store required so illustrations get SourcePath + markdown links.
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	t.Setenv(mobileStatePathEnv, statePath)
	mobileStatePathOverride = statePath
	t.Cleanup(func() { mobileStatePathOverride = "" })
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o700); err != nil {
		t.Fatal(err)
	}

	// Minimal 1x1 PNG
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe, 0xd4, 0xef, 0x00, 0x00,
		0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name string, body []byte) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	write("word/document.xml", []byte(`<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
 xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<w:body>
  <w:p><w:r><w:t>Before figure</w:t></w:r></w:p>
  <w:p><w:r><w:drawing><a:blip r:embed="rId5"/></w:drawing></w:r></w:p>
  <w:p><w:r><w:t>After figure</w:t></w:r></w:p>
</w:body></w:document>`))
	write("word/_rels/document.xml.rels", []byte(`<?xml version="1.0"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>
</Relationships>`))
	write("word/media/image1.png", png)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()

	md, images, ok := mobileDraftMarkdownFromDOCXEx("user1", "mobdoc_test1", "report.docx", data)
	if !ok {
		t.Fatal("docx+image not parsed")
	}
	if !strings.Contains(md, "Before figure") || !strings.Contains(md, "After figure") {
		t.Fatalf("markdown missing text: %q", md)
	}
	if !strings.Contains(md, "/api/mobile/documents/drafts/mobdoc_test1/images/img1") {
		t.Fatalf("markdown missing image url: %q", md)
	}
	if len(images) != 1 || images[0].ID != "img1" {
		t.Fatalf("images = %+v", images)
	}
	if images[0].SourceSize != len(png) {
		t.Fatalf("image size = %d want %d", images[0].SourceSize, len(png))
	}
	if strings.TrimSpace(images[0].SourcePath) == "" {
		t.Fatal("image SourcePath empty")
	}
}

func TestMobileUploadedXLSXDraftMarkdown(t *testing.T) {
	data := mobileTestZip(t, map[string]string{
		"xl/sharedStrings.xml":     `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Host</t></si><si><t>Status</t></si><si><t>api-1</t></si><si><t>ok</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row><row><c t="s"><v>2</v></c><c t="s"><v>3</v></c></row></sheetData></worksheet>`,
	})

	markdown, ok := mobileDraftMarkdownFromUpload("servers.xlsx", data)
	if !ok {
		t.Fatal("xlsx upload was not parsed")
	}
	if !strings.Contains(markdown, "Host | Status") {
		t.Fatalf("markdown = %q, want header row", markdown)
	}
	if !strings.Contains(markdown, "api-1 | ok") {
		t.Fatalf("markdown = %q, want data row", markdown)
	}
}

func TestMobileUploadedPDFDraftMarkdown(t *testing.T) {
	data := mobileRenderDraftPDF(mobileDocumentDraftRecord{
		Title:    "Incident PDF",
		Markdown: "# Incident PDF\n\nService recovered after restart.",
	})

	markdown, ok := mobileDraftMarkdownFromUpload("incident.pdf", data)
	if !ok {
		t.Fatal("pdf upload was not parsed")
	}
	if !strings.Contains(markdown, "# incident") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	if !strings.Contains(markdown, "Service recovered after restart.") {
		t.Fatalf("markdown = %q, want extracted PDF text", markdown)
	}
}

func TestMobilePDFNormalizeExtractedTextCollapsesGlyphSpaces(t *testing.T) {
	// Typical Chinese academic PDF extract: every glyph separated by a space.
	raw := "。 。 。 展 例 如 ， A C E 采 用 “ 生 成 器 - 反 思 器 ” （ G e n e r a t o r - R e f l e c t o r ） 的 固 定 工 作 流"
	got := mobilePDFNormalizeExtractedText(raw)
	if strings.Contains(got, "例 如") || strings.Contains(got, "采 用") {
		t.Fatalf("CJK spaces not collapsed: %q", got)
	}
	if strings.Contains(got, "G e n e r a t o r") || strings.Contains(got, "A C E") {
		t.Fatalf("Latin glyph spaces not collapsed: %q", got)
	}
	if !strings.Contains(got, "例如") || !strings.Contains(got, "采用") {
		t.Fatalf("expected joined CJK words, got %q", got)
	}
	if !strings.Contains(got, "Generator") || !strings.Contains(got, "Reflector") || !strings.Contains(got, "ACE") {
		t.Fatalf("expected joined Latin words, got %q", got)
	}
	// NBSP / ideographic space between CJK should collapse too.
	nbsp := "智\u00a0能\u3000体"
	gotNBSP := mobilePDFNormalizeExtractedText(nbsp)
	if gotNBSP != "智能体" {
		t.Fatalf("unicode spaces not collapsed: %q", gotNBSP)
	}
	// Normal English must keep word spaces.
	normal := mobilePDFNormalizeExtractedText("Service recovered after restart.")
	if normal != "Service recovered after restart." {
		t.Fatalf("normal English changed: %q", normal)
	}
	if !mobilePDFTextLooksOverSpaced(raw) {
		t.Fatal("expected over-spaced detector to fire")
	}
	if mobilePDFTextLooksOverSpaced(normal) {
		t.Fatal("normal prose should not look over-spaced")
	}
	// Markdown title preserved; body normalized.
	md := "# AI智能体自进化文献综述\n\n" + raw
	gotMD := mobilePDFNormalizeExtractedMarkdown(md)
	if !strings.HasPrefix(gotMD, "# AI智能体自进化文献综述\n") {
		t.Fatalf("title not preserved: %q", gotMD)
	}
	if strings.Contains(gotMD, "G e n e r a t o r") {
		t.Fatalf("body not normalized: %q", gotMD)
	}
}

func TestMobilePDFExtractRejectsScrapeGarbage(t *testing.T) {
	// Mimic naive literal-string scrape noise from compressed academic PDFs.
	garbage := "*\nOS\n7ooooooooo\n0\nS@\n@\n$V\nzK\n0\nL0\n00p\n] p\nNF\n0d}\nOo\nKoooo\n"
	if mobilePDFExtractedTextIsUsable(garbage) {
		t.Fatalf("expected scrape garbage to be rejected")
	}
	md := "# paper-title\n\n" + garbage
	if !mobileDraftMarkdownLooksLikePDFGarbage(md) {
		t.Fatalf("expected draft body to be flagged as PDF garbage")
	}
	// Readable Chinese/English original-only notice must not be flagged.
	notice := mobileDraftOriginalOnlyMarkdown("paper.pdf", []byte{1, 2, 3})
	if mobileDraftBodyLooksUnreadable(notice) {
		t.Fatalf("original-only notice should be readable: %q", notice)
	}
	// Short legitimate drafts must NOT be treated as unreadable.
	for _, short := range []string{
		"收到，谢谢",
		"OK",
		"# 备注\n\n明天跟进。",
		"Done.",
		// CJK short bullets previously false-positive as scrape noise.
		"# todo\n\n- 买\n- 卖\n- 走\n- 看\n- 写\n",
	} {
		if mobileDraftBodyLooksUnreadable(short) {
			t.Fatalf("short draft wrongly flagged unreadable: %q", short)
		}
	}
	// Non-PDF original with short symbol-ish lines (e.g. code) must not be replaced.
	codeDraft := mobileDocumentDraftRecord{
		SourceFilename:    "snippet.go",
		SourceContentType: "text/plain",
		Markdown:          "# snippet\n\nfunc\nmain\n{\n}\nreturn\n",
	}
	if mobileDraftRecordBodyUnreadable(codeDraft, codeDraft.Markdown) {
		t.Fatalf("code draft wrongly flagged unreadable")
	}
}

func TestMobileDraftDisplayMarkdownHidesPDFGarbage(t *testing.T) {
	clearMobileStateForTest(t)
	// Stored body is scrape garbage; no original bytes → show original-file notice.
	draft := mobileDocumentDraftRecord{
		ID:             "draft-pdf-garbage",
		Title:          "1-s2.0-paper-main",
		SourceFilename: "1-s2.0-paper-main.pdf",
		SourceSize:     2760 * 1024,
		Markdown:       "# 1-s2.0-paper-main\n\n*\nOS\n7ooooooooo\n0\nS@\n$V\nzK\n0\nL0\n00p\n",
	}
	display := mobileDraftDisplayMarkdown(draft)
	if strings.Contains(display, "7ooooooooo") || strings.Contains(display, "S@") {
		t.Fatalf("display still shows binary scrape: %q", display)
	}
	if !strings.Contains(display, "原始文件") {
		t.Fatalf("display = %q, want original-file notice", display)
	}
	if !strings.Contains(display, fmt.Sprintf("%d bytes", 2760*1024)) {
		t.Fatalf("display = %q, want preserved SourceSize", display)
	}

	// List path must hide garbage without requiring a re-extract.
	listPreview := mobileDraftListPreviewMarkdown(draft)
	if strings.Contains(listPreview, "7ooooooooo") {
		t.Fatalf("list preview still shows scrape: %q", listPreview)
	}
	if !strings.Contains(listPreview, "原始文件") {
		t.Fatalf("list preview = %q, want original-file notice", listPreview)
	}

	// When a real extractable PDF original exists, display must use extracted text.
	pdf := mobileRenderDraftPDF(mobileDocumentDraftRecord{
		Title:    "Clean Paper",
		Markdown: "Abstract: neural networks improve accuracy after fine tuning.",
	})
	draft2 := mobileDocumentDraftRecord{
		ID:                "draft-pdf-heal",
		Title:             "Clean Paper",
		SourceFilename:    "clean.pdf",
		SourceContentType: "application/pdf",
		SourceBytes:       pdf,
		SourceSize:        len(pdf),
		Markdown:          "# Clean Paper\n\n*\nOS\n7ooooooooo\n0\nS@\n$V\nzK\n0\nL0\n",
	}
	heal2 := mobileDraftHealMarkdownOutsideLock(draft2)
	if !heal2.ShouldPersist {
		t.Fatal("expected heal to want persist")
	}
	if !strings.Contains(heal2.Display, "neural networks") {
		t.Fatalf("display2 = %q, want re-extracted PDF text", heal2.Display)
	}
	if strings.Contains(heal2.Display, "7ooooooooo") {
		t.Fatalf("display2 still contains garbage: %q", heal2.Display)
	}
	if !mobileDraftApplyHealed(&draft2, heal2) {
		t.Fatal("expected apply heal to succeed")
	}
	if draft2.Markdown != heal2.Display {
		t.Fatalf("stored markdown not healed")
	}
}

func TestMobileUploadedImageDraftMarkdown(t *testing.T) {
	data := mobileTestPNG(640, 480)

	markdown := mobileDraftMarkdownFromImage("screenshot.png", data)
	if !strings.Contains(markdown, "# screenshot") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	// Original-first placeholder until desktop OCR fills the body.
	if !strings.Contains(markdown, "OCR") || !strings.Contains(markdown, "\u539f\u4ef6") {
		t.Fatalf("markdown = %q, want original-saved + OCR placeholder", markdown)
	}
	if !strings.Contains(markdown, "640 x 480") {
		t.Fatalf("markdown = %q, want dimensions", markdown)
	}
}

func TestMobileApplyUploadPipelineResultCompletesOCRDraft(t *testing.T) {
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 9, 30, 0, 0, time.UTC)
	draft := mobileDocumentDraftRecord{
		ID:        "draft-ocr",
		OwnerID:   "user-ocr",
		Title:     "screenshot",
		Template:  "report",
		Markdown:  "# screenshot\n\n\u7b49\u5f85 OCR",
		UpdatedAt: now.Add(-time.Minute),
	}
	record := mobileDocumentUploadRecord{
		TaskID:      "upload-ocr",
		OwnerID:     "user-ocr",
		Filename:    "screenshot.png",
		Status:      "needs_ocr",
		DraftID:     draft.ID,
		Message:     "\u56fe\u7247\u5df2\u5bfc\u5165\u4e3a\u79fb\u52a8\u7aef\u8349\u7a3f\uff0c\u7b49\u5f85 OCR/\u89c6\u89c9\u6a21\u578b\u8bc6\u522b\u3002",
		OCRMarkdown: "# screenshot\n\n\u8bc6\u522b\u6587\u672c\uff1a\u670d\u52a1\u62a5\u9519 502\u3002",
		OCRMessage:  "OCR \u5df2\u5b8c\u6210\u3002",
		UploadedAt:  now.Add(-time.Minute),
		UpdatedAt:   now.Add(-time.Minute),
	}

	mobileDocuments.Lock()
	mobileDocuments.drafts[draft.ID] = draft
	updated, changed := mobileApplyUploadPipelineResult(record, now)
	updatedDraft := mobileDocuments.drafts[draft.ID]
	mobileDocuments.Unlock()

	if !changed {
		t.Fatal("expected OCR result to change upload state")
	}
	if updated.Status != "ready" || updated.Message != "OCR \u5df2\u5b8c\u6210\u3002" {
		t.Fatalf("updated upload = %#v, want ready OCR completion", updated)
	}
	if !strings.Contains(updatedDraft.Markdown, "\u670d\u52a1\u62a5\u9519 502") {
		t.Fatalf("updated draft markdown = %q, want OCR text", updatedDraft.Markdown)
	}
	if !updatedDraft.UpdatedAt.Equal(now) {
		t.Fatalf("updated draft time = %v, want %v", updatedDraft.UpdatedAt, now)
	}
}

func TestMobileApplyUploadPipelineResultFailsOCRDraft(t *testing.T) {
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 9, 35, 0, 0, time.UTC)
	record := mobileDocumentUploadRecord{
		TaskID:    "upload-ocr-fail",
		OwnerID:   "user-ocr",
		Filename:  "screenshot.png",
		Status:    "needs_ocr",
		DraftID:   "draft-ocr",
		OCRError:  "OCR \u670d\u52a1\u6682\u4e0d\u53ef\u7528\u3002",
		UpdatedAt: now.Add(-time.Minute),
	}

	mobileDocuments.Lock()
	updated, changed := mobileApplyUploadPipelineResult(record, now)
	mobileDocuments.Unlock()

	if !changed {
		t.Fatal("expected OCR error to change upload state")
	}
	if updated.Status != "failed" || updated.Message != "OCR \u670d\u52a1\u6682\u4e0d\u53ef\u7528\u3002" {
		t.Fatalf("updated upload = %#v, want failed OCR error", updated)
	}
	if !updated.UpdatedAt.Equal(now) {
		t.Fatalf("updated upload time = %v, want %v", updated.UpdatedAt, now)
	}
}

func mobileTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func mobileTestPNG(width, height int) []byte {
	data := make([]byte, 24)
	copy(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	copy(data[12:], []byte{'I', 'H', 'D', 'R'})
	data[16] = byte(width >> 24)
	data[17] = byte(width >> 16)
	data[18] = byte(width >> 8)
	data[19] = byte(width)
	data[20] = byte(height >> 24)
	data[21] = byte(height >> 16)
	data[22] = byte(height >> 8)
	data[23] = byte(height)
	return data
}

func TestMobileDocumentExportPayloadReadyForPDF(t *testing.T) {
	payload := mobileDocumentExportPayload(mobileDocumentExportRecord{
		JobID:   "mobexp_1",
		Format:  "pdf",
		Status:  "ready",
		Message: "导出文件已生成，可下载或分享。",
	})

	if payload["download_url"] != "/api/mobile/documents/export/mobexp_1/download" {
		t.Fatalf("download_url = %v, want ready download URL", payload["download_url"])
	}
	if payload["message"] != "导出文件已生成，可下载或分享。" {
		t.Fatalf("message = %v, want ready message", payload["message"])
	}
}

func TestMobileDocumentExportPayloadReadyForWord(t *testing.T) {
	payload := mobileDocumentExportPayload(mobileDocumentExportRecord{
		JobID:  "mobexp_2",
		Format: "word",
		Status: "ready",
	})

	if payload["download_url"] != "/api/mobile/documents/export/mobexp_2/download" {
		t.Fatalf("download_url = %v, want ready download URL", payload["download_url"])
	}
}

func TestMobileRenderDraftPDF(t *testing.T) {
	data := mobileRenderDraftPDF(mobileDocumentDraftRecord{
		Title:    "Incident Report",
		Markdown: "# Incident Report\n\nService recovered.\n\n- nginx restarted",
	})

	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("pdf header = %q, want %%PDF-1.4", data[:8])
	}
	if !bytes.Contains(data, []byte("xref")) {
		t.Fatal("pdf missing xref")
	}
	if !bytes.Contains(data, []byte("trailer")) {
		t.Fatal("pdf missing trailer")
	}
}

func TestMobileRenderDraftDOCX(t *testing.T) {
	data := mobileRenderDraftDOCX(mobileDocumentDraftRecord{
		Title:    "Incident Report",
		Markdown: "# Incident Report\n\nService recovered.\n\n- nginx restarted",
	})

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader returned error: %v", err)
	}
	var documentXML string
	for _, file := range zr.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open document.xml: %v", err)
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read document.xml: %v", err)
		}
		documentXML = string(raw)
		break
	}
	if documentXML == "" {
		t.Fatal("docx missing word/document.xml")
	}
	if !strings.Contains(documentXML, "Incident Report") {
		t.Fatalf("document.xml = %q, want title", documentXML)
	}
	if !strings.Contains(documentXML, "Service recovered.") {
		t.Fatalf("document.xml = %q, want body", documentXML)
	}
}

func TestMobileDocumentExportStatusHandlersRequireViewerToken(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{
			name:    "status",
			path:    "/api/mobile/documents/export/job-1",
			handler: MobileDocumentExportStatusHandler(nil),
		},
		{
			name:    "download",
			path:    "/api/mobile/documents/export/job-1/download",
			handler: MobileDocumentExportDownloadHandler(nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			tt.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}
