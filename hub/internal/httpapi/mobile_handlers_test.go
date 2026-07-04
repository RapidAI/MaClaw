package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

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

	MobileBootstrapHandler(nil).ServeHTTP(rec, req)

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

	reuseReq := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-authorizations", bytes.NewReader(body))
	reuseReq.Header.Set("Authorization", "Bearer "+viewerToken)
	reuseRec := httptest.NewRecorder()
	MobileLLMDesktopQRAuthorizationHandler(identity).ServeHTTP(reuseRec, reuseReq)
	if reuseRec.Code != http.StatusBadRequest || !strings.Contains(reuseRec.Body.String(), "already been used") {
		t.Fatalf("reuse status=%d body=%s, want used-session rejection", reuseRec.Code, reuseRec.Body.String())
	}
}

func TestMobileLLMDesktopQRSessionConsumeIssuesMobileToken(t *testing.T) {
	clearMobileLLMAuthorizationsForTest(t)
	identity, _, _ := newHTTPAPITestServices(t)
	desktopViewerToken, _ := issueViewerToken(t, identity, "mobile-first-qr@example.com")
	createBody, _ := json.Marshal(map[string]any{
		"name":     "OpenAI Compatible",
		"url":      "https://llm.example.com/v1",
		"key":      "sk-test",
		"model":    "gpt-4.1-mini",
		"protocol": "openai",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-sessions", bytes.NewReader(createBody))
	createReq.Host = "tenant-a.maclaw.top"
	createReq.Header.Set("Authorization", "Bearer "+desktopViewerToken)
	createRec := httptest.NewRecorder()
	MobileLLMDesktopQRSessionHandler(identity).ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s, want 201", createRec.Code, createRec.Body.String())
	}
	var createResponse map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResponse); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	qrPayload, _ := createResponse["qr_payload"].(string)
	body, _ := json.Marshal(map[string]string{"qr_payload": qrPayload})
	consumeReq := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-sessions/consume", bytes.NewReader(body))
	consumeReq.Host = "tenant-a.maclaw.top"
	consumeRec := httptest.NewRecorder()

	MobileLLMDesktopQRSessionConsumeHandler(identity).ServeHTTP(consumeRec, consumeReq)

	if consumeRec.Code != http.StatusOK {
		t.Fatalf("consume status=%d body=%s, want 200", consumeRec.Code, consumeRec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(consumeRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode consume response: %v", err)
	}
	if response["access_token"] == "" || response["hub_url"] != "https://tenant-a.maclaw.top" {
		t.Fatalf("response = %#v, want token and request hub url", response)
	}
	bootstrap, ok := response["bootstrap"].(map[string]any)
	if !ok {
		t.Fatalf("bootstrap = %#v, want map", response["bootstrap"])
	}
	llmAccess, ok := bootstrap["llm_access"].(map[string]any)
	if !ok || llmAccess["mode"] != "desktop_qr_third_party" || llmAccess["provider_name"] != "OpenAI Compatible" {
		t.Fatalf("llm_access = %#v, want desktop QR delegated provider", bootstrap["llm_access"])
	}
	if _, leaked := llmAccess["key"]; leaked {
		t.Fatalf("llm_access leaked key: %#v", llmAccess)
	}
	principal, err := identity.AuthenticateViewer(auth.WithTenant(context.Background(), "tenant-1"), response["access_token"].(string))
	if err != nil || principal == nil || principal.Email != "mobile-first-qr@example.com" {
		t.Fatalf("AuthenticateViewer principal=%#v err=%v, want QR owner", principal, err)
	}
	reuseReq := httptest.NewRequest(http.MethodPost, "/api/mobile/llm/desktop-qr-sessions/consume", bytes.NewReader(body))
	reuseRec := httptest.NewRecorder()
	MobileLLMDesktopQRSessionConsumeHandler(identity).ServeHTTP(reuseRec, reuseReq)
	if reuseRec.Code != http.StatusBadRequest || !strings.Contains(reuseRec.Body.String(), "already been used") {
		t.Fatalf("reuse status=%d body=%s, want used-session rejection", reuseRec.Code, reuseRec.Body.String())
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

func TestMobileSearchHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/search", strings.NewReader(`{"query":"status"}`))
	rec := httptest.NewRecorder()

	MobileSearchHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
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
	if !strings.Contains(answer, "Check error.log") {
		t.Fatalf("answer = %q, want result snippet", answer)
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

	if !strings.Contains(answer, "\u5df2\u8bc6\u522b\u5206\u4eab\u94fe\u63a5") {
		t.Fatalf("answer = %q, want shared-link hint", answer)
	}
	if len(citations) != 1 {
		t.Fatalf("len(citations) = %d, want 1", len(citations))
	}
	if citations[0]["url"] != "https://example.test/incident?from=mobile" {
		t.Fatalf("citation = %#v, want shared URL", citations[0])
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

func TestMobileDigitalEmployeesHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/digital-employees", nil)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeesHandler(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("body = %s, want UNAUTHORIZED", rec.Body.String())
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

	MobileDigitalEmployeesHandler(identity, nil).ServeHTTP(rec, req)

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
		Title:     "\u5e94\u6025\u62a5\u544a",
		Template:  "report",
		Markdown:  "# \u5e94\u6025\u62a5\u544a\n\n\u5185\u5bb9",
		UpdatedAt: now,
	}
	mobileDocuments.exports["export-1"] = mobileDocumentExportRecord{
		JobID:     "export-1",
		DraftID:   "draft-1",
		OwnerID:   "user-1",
		Format:    "markdown",
		Status:    "ready",
		CreatedAt: now,
	}
	mobileDocuments.uploads["upload-1"] = mobileDocumentUploadRecord{
		TaskID:     "upload-1",
		OwnerID:    "user-1",
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

func TestMobileDocumentUploadClaimHandlerDocumentKindSkipsOCRTasks(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-claim-document@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 9, 52, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-ocr"] = mobileDocumentUploadRecord{
		TaskID:     "upload-ocr",
		OwnerID:    enroll.UserID,
		Filename:   "screenshot.png",
		Status:     "needs_ocr",
		UploadedAt: now,
		UpdatedAt:  now,
	}
	mobileDocuments.uploads["upload-doc"] = mobileDocumentUploadRecord{
		TaskID:      "upload-doc",
		OwnerID:     enroll.UserID,
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

func TestMobileDocumentUploadSourceHandlerDownloadsClaimedSource(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	_, enroll := issueViewerToken(t, identity, "mobile-source@example.com")
	clearMobileStateForTest(t)
	now := time.Date(2026, 7, 1, 9, 55, 0, 0, time.UTC)
	mobileDocuments.Lock()
	mobileDocuments.uploads["upload-source"] = mobileDocumentUploadRecord{
		TaskID:      "upload-source",
		OwnerID:     enroll.UserID,
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

func TestMobileUploadedImageDraftMarkdown(t *testing.T) {
	data := mobileTestPNG(640, 480)

	markdown := mobileDraftMarkdownFromImage("screenshot.png", data)
	if !strings.Contains(markdown, "# screenshot") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	if !strings.Contains(markdown, "\u7b49\u5f85 OCR") {
		t.Fatalf("markdown = %q, want OCR pending text", markdown)
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
