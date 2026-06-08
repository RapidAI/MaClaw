package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

const testPlatformAvatarDataURL = "data:image/png;base64,iVBORw0KGgo="

func TestPlatformVirtualEmployeeProvisionCreatesRuntimeInstance(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)

	tenant, user, inst := platformRuntimeForTest(t, svc, "emp-001")
	if tenant.ID == "" || user.ID == "" || inst.ID == "" {
		t.Fatalf("missing runtime binding: tenant=%#v user=%#v instance=%#v", tenant, user, inst)
	}
	if inst.Metadata["ve_employee_id"] != "emp-001" || inst.Metadata["llm_service_group_id"] != "group-legal" {
		t.Fatalf("unexpected instance metadata: %#v", inst.Metadata)
	}
	if inst.Metadata["ve_avatar_data_url"] != testPlatformAvatarDataURL {
		t.Fatalf("expected avatar metadata, got %#v", inst.Metadata)
	}
	if inst.Metadata["ve_name"] != "Contract Reviewer" || inst.Metadata["ve_skill_description"] != "Review contract risks" || inst.Metadata["ve_skill_tags"] != "contract, review" {
		t.Fatalf("expected platform identity metadata for system prompt, got %#v", inst.Metadata)
	}
	cfg, err := svc.GetUserConfig(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMCurrentProvider != "hub-llm" || len(cfg.AppConfig.MaclawLLMProviders) != 1 || cfg.AppConfig.MaclawLLMProviders[0].URL != "https://hub.example.test/llm" || cfg.AppConfig.MaclawLLMProviders[0].Key == "" || cfg.AppConfig.MaclawLLMProviders[0].Key == "managed-by-hub" || cfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("expected Hub LLM provider config, got %#v", cfg.AppConfig)
	}
}

func TestPlatformVirtualEmployeeConfigUpdatesIMSettingsAndClearsAutoMode(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, _ := platformRuntimeForTest(t, svc, "emp-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}

	postPlatformJSONForTest(t, server, "/api/platform/virtual-employees/emp-001/config", map[string]any{
		"tenant_id":     "hub-tenant-001",
		"virtual_email": "contract_reviewer@example.test",
		"maclawsrv_config": map[string]any{
			"telegram_bot_enabled": true,
			"telegram_bot_token":   "telegram-secret",
			"telegram_local_mode":  true,
		},
	}, http.StatusOK)
	cfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	maskedToken := cfg.AppConfig.TelegramBotToken
	if !cfg.AppConfig.TelegramBotEnabled || maskedToken == "" || maskedToken == "telegram-secret" || cfg.AppConfig.TelegramLocalMode == nil || !*cfg.AppConfig.TelegramLocalMode {
		t.Fatalf("expected telegram settings applied, got %#v", cfg.AppConfig)
	}

	postPlatformJSONForTest(t, server, "/api/platform/virtual-employees/emp-001/config", map[string]any{
		"tenant_id":     "hub-tenant-001",
		"virtual_email": "contract_reviewer@example.test",
		"maclawsrv_config": map[string]any{
			"telegram_bot_token":  "********",
			"telegram_local_mode": nil,
		},
	}, http.StatusOK)
	cfg, err = svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig after clear: %v", err)
	}
	if cfg.AppConfig.TelegramBotToken != maskedToken || cfg.AppConfig.TelegramLocalMode != nil {
		t.Fatalf("expected masked token preserved and local mode cleared to auto, got %#v", cfg.AppConfig)
	}

	postPlatformJSONForTest(t, server, "/api/platform/virtual-employees/emp-001/config", map[string]any{
		"tenant_id":     "hub-tenant-001",
		"virtual_email": "contract_reviewer@example.test",
		"maclawsrv_config": map[string]any{
			"telegram_bot_token": "******",
		},
	}, http.StatusOK)
	cfg, err = svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig after alternate mask: %v", err)
	}
	if cfg.AppConfig.TelegramBotToken != maskedToken {
		t.Fatalf("expected alternate masked token preserved, got %#v", cfg.AppConfig)
	}
	badPort := 70000
	if err := server.updatePlatformUserMaclawSrvConfig(httptest.NewRequest(http.MethodPost, "/", nil), principal, platformMaclawSrvConfig{ThirdPartyGatewayPort: &badPort}); err == nil {
		t.Fatalf("expected invalid gateway port error")
	}
}

func TestPlatformVirtualEmployeeConfigUpdatesAvatarMetadata(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)

	postPlatformJSONForTest(t, server, "/api/platform/virtual-employees/emp-001/config", map[string]any{
		"tenant_id":       "hub-tenant-001",
		"virtual_email":   "contract_reviewer@example.test",
		"avatar_data_url": "",
	}, http.StatusOK)
	_, _, inst := platformRuntimeForTest(t, svc, "emp-001")
	if _, ok := inst.Metadata["ve_avatar_data_url"]; ok {
		t.Fatalf("expected avatar metadata cleared, got %#v", inst.Metadata)
	}
}

func TestPlatformVirtualEmployeeProvisionRejectsInvalidAvatarDataURL(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	postPlatformJSONForTest(t, server, "/api/platform/virtual-employees", map[string]any{
		"employee_id":      "emp-bad-avatar",
		"tenant_id":        "hub-tenant-001",
		"name":             "Bad Avatar",
		"virtual_email":    "bad_avatar@example.test",
		"avatar_data_url":  "data:image/png;base64,QUJD",
		"hub_llm_endpoint": "https://hub.example.test/llm",
		"hub_llm_api_key":  "test-hub-key",
	}, http.StatusBadRequest)
}

func TestNormalizePlatformAvatarAcceptsOneMiBImageDataURL(t *testing.T) {
	payload := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, platformAvatarImageMaxBytes-8)...)
	avatar := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	if len(avatar) <= 1024*1024 {
		t.Fatalf("test avatar should exceed old encoded limit")
	}
	if _, err := normalizePlatformAvatarDataURL(avatar); err != nil {
		t.Fatalf("normalize one MiB avatar: %v", err)
	}
}

func TestPlatformVirtualEmployeeProvisionAcceptsOneMiBAvatarDataURL(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, platformAvatarImageMaxBytes-8)...)
	avatar := "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload)
	postPlatformJSONForTest(t, server, "/api/platform/virtual-employees", map[string]any{
		"employee_id":      "emp-big-avatar",
		"tenant_id":        "hub-tenant-001",
		"name":             "Big Avatar",
		"virtual_email":    "big_avatar@example.test",
		"avatar_data_url":  avatar,
		"hub_llm_endpoint": "https://hub.example.test/llm",
		"hub_llm_api_key":  "test-hub-key",
	}, http.StatusOK)
}

func TestRuntimeVirtualEmployeeDiscussionMessageRunsBoundRuntime(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	payload := map[string]any{"employee_id": "emp-001", "tenant_id": "hub-tenant-001", "hub_discussion_id": "discussion-1", "hub_message_id": "message-1", "request_id": "request-1", "content": "hello from Hub"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/emp-001/discussion-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime discussion message status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		EmployeeID string `json:"employee_id"`
		Message    struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode message response: %v", err)
	}
	if out.EmployeeID != "emp-001" || !strings.Contains(out.Message.Content, "hello from Hub") {
		t.Fatalf("unexpected message response: %#v", out)
	}
}

func TestPlatformRuntimeLogIDRedactsEmployeeID(t *testing.T) {
	redacted := platformRuntimeLogID("platform-employee-1")
	if redacted == "" || strings.Contains(redacted, "platform-employee-1") || !strings.HasPrefix(redacted, "sha256:") {
		t.Fatalf("platform runtime log id was not redacted: %q", redacted)
	}
}

func TestRuntimeVirtualEmployeeDiscussionMessageMatchesRuntimeBindingCaseInsensitive(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	body, _ := json.Marshal(map[string]any{"hub_discussion_id": "discussion-case", "hub_message_id": "message-case", "request_id": "request-case", "content": "hello mixed case"})
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/EMP-001/discussion-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("X-VE-Hub-Tenant-ID", "HUB-TENANT-001")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime discussion mixed-case binding status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode message response: %v", err)
	}
	if !strings.Contains(out.Message.Content, "hello mixed case") {
		t.Fatalf("unexpected message response: %#v", out)
	}
}

func TestRuntimeVirtualEmployeeDiscussionMessageAcceptsPayloadEnvelope(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	payload := map[string]any{"payload": map[string]any{"envelope": map[string]any{"id": "env-1", "session_id": "discussion-1", "message": map[string]any{"id": "message-1", "content": "hello from envelope"}}}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/emp-001/discussion-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime envelope message status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Session struct {
			Metadata map[string]string `json:"metadata"`
		} `json:"session"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode message response: %v", err)
	}
	if !strings.Contains(out.Message.Content, "hello from envelope") {
		t.Fatalf("unexpected message response: %#v", out)
	}
	if out.Session.Metadata["client_session_key"] != "discussion-1" {
		t.Fatalf("runtime session not bound to envelope discussion: %#v", out.Session.Metadata)
	}
}

func TestRuntimeVirtualEmployeeDiscussionMessageIncludesAttachmentContext(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/download/file-1" {
			t.Fatalf("download path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("session_id"); got != "discussion-file-1" {
			t.Fatalf("session_id = %q", got)
		}
		_, _ = w.Write([]byte("pdf bytes"))
	}))
	defer fileServer.Close()
	tenant, user, _ := platformRuntimeForTest(t, svc, "emp-001")
	if _, err := svc.UpdateUserConfig(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, corelib.AppConfig{RemoteHubURL: fileServer.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1", MaclawLLMUrl: "http://127.0.0.1/unused", MaclawLLMKey: "unused", MaclawLLMModel: "unused"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	envelope := map[string]any{
		"id":         "env-file-1",
		"session_id": "discussion-file-1",
		"message": map[string]any{
			"id":      "message-file-1",
			"content": "文件中有什么？",
			"file_attachments": []map[string]any{{
				"file_url":   fileServer.URL + "/api/ve/files/download/file-1",
				"filename":   "2602.06052v3.pdf",
				"mime_type":  "application/pdf",
				"size_bytes": 4096,
			}},
		},
	}
	body, _ := json.Marshal(map[string]any{"payload": map[string]any{"envelope": envelope}})
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/emp-001/discussion-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime attachment message status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Message struct {
			Content  string            `json:"content"`
			Metadata map[string]string `json:"metadata"`
		} `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode message response: %v", err)
	}
	if !strings.Contains(out.Message.Content, "2602.06052v3.pdf") || !strings.Contains(out.Message.Content, "[Hub attachments received]") {
		t.Fatalf("attachment context missing from runtime message: %s", out.Message.Content)
	}
	if !strings.Contains(out.Message.Content, "local_path=") || !strings.Contains(out.Message.Content, ".hub-attachments") {
		t.Fatalf("downloaded local path missing from runtime message: %s", out.Message.Content)
	}
}

func TestRuntimeVirtualEmployeeDiscussionMessageAllowsAttachmentOnlyMessage(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	envelope := map[string]any{
		"id":         "env-file-only",
		"session_id": "discussion-file-only",
		"message": map[string]any{
			"id": "message-file-only",
			"file_attachments": []map[string]any{{
				"file_url":  "/api/ve/files/download/file-1",
				"filename":  "brief.pdf",
				"mime_type": "application/pdf",
			}},
		},
	}
	body, _ := json.Marshal(map[string]any{"payload": map[string]any{"envelope": envelope}})
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/emp-001/discussion-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime attachment-only message status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode message response: %v", err)
	}
	if !strings.Contains(out.Message.Content, "Please inspect the attached file(s).") || !strings.Contains(out.Message.Content, "brief.pdf") {
		t.Fatalf("attachment-only context missing: %s", out.Message.Content)
	}
}

func TestLimitPlatformMessageAttachmentsCapsTotal(t *testing.T) {
	attachments := platformMessageAttachments{}
	for i := 0; i < platformAttachmentMaxCount+5; i++ {
		attachments.File = append(attachments.File, platformFileAttachment{Filename: fmt.Sprintf("%d.pdf", i)})
	}
	limited := limitPlatformMessageAttachments(attachments)
	if got := platformMessageAttachmentCount(limited); got != platformAttachmentMaxCount {
		t.Fatalf("limited attachment count = %d, want %d", got, platformAttachmentMaxCount)
	}
}

func TestPlatformAttachmentDownloadURLRejectsUnsafeOrigins(t *testing.T) {
	if _, _, err := platformAttachmentDownloadURL("", "https://hub.example/api/ve/files/download/file-1", "discussion-1", "machine-1"); err == nil {
		t.Fatal("expected empty remote_hub_url to be rejected")
	}
	if _, _, err := platformAttachmentDownloadURL("https://hub.example", "https://evil.example/api/ve/files/download/file-1", "discussion-1", "machine-1"); err == nil {
		t.Fatal("expected cross-origin attachment URL to be rejected")
	}
	got, id, err := platformAttachmentDownloadURL("https://hub.example", "/api/ve/files/file-1", "discussion-1", "machine-1")
	if err != nil {
		t.Fatalf("platformAttachmentDownloadURL: %v", err)
	}
	if id != "file-1" || got != "https://hub.example/api/ve/files/download/file-1?participant_id=machine-1&session_id=discussion-1" {
		t.Fatalf("download URL = %q id=%q", got, id)
	}
}

func TestMaterializePlatformTextAttachmentUsesUniquePaths(t *testing.T) {
	workspace := t.TempDir()
	att := platformTextAttachment{Filename: "note.txt", Content: base64.StdEncoding.EncodeToString([]byte("first"))}
	first, err := materializePlatformTextAttachment(workspace, "discussion-1", att)
	if err != nil {
		t.Fatalf("materialize first: %v", err)
	}
	att.Content = base64.StdEncoding.EncodeToString([]byte("second"))
	second, err := materializePlatformTextAttachment(workspace, "discussion-1", att)
	if err != nil {
		t.Fatalf("materialize second: %v", err)
	}
	if first == second {
		t.Fatalf("expected unique paths, got %q", first)
	}
}

func TestSafePlatformAttachmentFilenameCapsLengthAndKeepsExtension(t *testing.T) {
	name := strings.Repeat("a", 220) + ".pdf"
	got := safePlatformAttachmentFilename(name)
	if len(got) > platformAttachmentNameMaxLen {
		t.Fatalf("filename length = %d, want <= %d", len(got), platformAttachmentNameMaxLen)
	}
	if !strings.HasSuffix(got, ".pdf") {
		t.Fatalf("filename extension not preserved: %q", got)
	}
}

func TestRuntimeVirtualEmployeeDiscussionMessageDedupesByHubMessageID(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	post := func(requestID string) {
		t.Helper()
		payload := map[string]any{"employee_id": "emp-001", "tenant_id": "hub-tenant-001", "hub_discussion_id": "discussion-1", "hub_message_id": "message-stable-1", "request_id": requestID, "content": "hello once"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/emp-001/discussion-messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("runtime discussion message status=%d body=%s", w.Code, w.Body.String())
		}
	}
	post("env-retry-1")
	post("env-retry-2")
	tenant, user, inst := platformRuntimeForTest(t, svc, "emp-001")
	sessions, err := svc.ListSessions(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, inst.ID, agentservice.ListSessionsInput{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one runtime session, got %#v", sessions)
	}
	messages, err := svc.ListMessages(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, inst.ID, sessions[0].ID, agentservice.ListMessagesInput{})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("same Hub message should not create duplicate runtime turns, got %#v", messages)
	}
}

func TestRuntimeVirtualEmployeeDiscussionMessageHonorsHubTenantHeader(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provision := func(hubTenantID, platformTenantID, name string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"employee_id":          "emp-shared",
			"tenant_id":            hubTenantID,
			"platform_tenant_id":   platformTenantID,
			"name":                 name,
			"virtual_email":        platformTenantID + "@example.test",
			"hub_llm_endpoint":     "https://hub.example.test/llm",
			"hub_llm_api_key":      "test-hub-key",
			"llm_service_group_id": "group-legal",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("provision %s status=%d body=%s", hubTenantID, w.Code, w.Body.String())
		}
	}
	provision("hub-tenant-a", "tenant-a", "Shared A")
	provision("hub-tenant-b", "tenant-b", "Shared B")
	wantTenantID := platformRuntimeTenantIDForTest(t, svc, "hub-tenant-b")

	body, _ := json.Marshal(map[string]any{"hub_discussion_id": "discussion-b", "hub_message_id": "message-b", "request_id": "request-b", "content": "hello tenant b"})
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/emp-shared/discussion-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("X-VE-Hub-Tenant-ID", "hub-tenant-b")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime discussion message status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Session struct {
			TenantID string `json:"tenant_id"`
		} `json:"session"`
		Message struct {
			TenantID string `json:"tenant_id"`
			Content  string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Session.TenantID != wantTenantID || out.Message.TenantID != wantTenantID || !strings.Contains(out.Message.Content, "hello tenant b") {
		t.Fatalf("runtime message routed to wrong tenant, want=%s got=%#v", wantTenantID, out)
	}
}

func TestRuntimeVirtualEmployeeDiscussionMessageHonorsBodyTenantID(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	for _, item := range []struct{ hubTenantID, platformTenantID, name string }{{"hub-tenant-a", "tenant-a", "Shared A"}, {"hub-tenant-b", "tenant-b", "Shared B"}} {
		body, _ := json.Marshal(map[string]any{"employee_id": "emp-shared", "tenant_id": item.hubTenantID, "platform_tenant_id": item.platformTenantID, "name": item.name, "virtual_email": item.platformTenantID + "@example.test", "hub_llm_endpoint": "https://hub.example.test/llm", "hub_llm_api_key": "test-hub-key", "llm_service_group_id": "group-legal"})
		req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("provision %s status=%d body=%s", item.hubTenantID, w.Code, w.Body.String())
		}
	}
	wantTenantID := platformRuntimeTenantIDForTest(t, svc, "hub-tenant-b")

	body, _ := json.Marshal(map[string]any{"tenant_id": "hub-tenant-b", "hub_discussion_id": "discussion-b", "hub_message_id": "message-b", "request_id": "request-b", "content": "hello body tenant"})
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/emp-shared/discussion-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime discussion message status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Session struct {
			TenantID string `json:"tenant_id"`
		} `json:"session"`
		Message struct {
			TenantID string `json:"tenant_id"`
		} `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Session.TenantID != wantTenantID || out.Message.TenantID != wantTenantID {
		t.Fatalf("runtime message routed to wrong tenant from body tenant_id, want=%s got=%#v", wantTenantID, out)
	}
}

func TestRuntimeVirtualEmployeeDiscussionMessageRunsSourceUserRuntime(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	tenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "Runtime Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(t.Context(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Runtime User", Email: "runtime@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, corelib.AppConfig{MaclawLLMUrl: "https://llm.example.test", MaclawLLMKey: "key", MaclawLLMModel: "auto"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(t.Context(), principal, agentservice.CreateInstanceInput{Name: "Source user VE", Metadata: map[string]string{"ve_source_user_id": "src-ve-001"}})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	payload := map[string]any{"employee_id": "src-ve-001", "tenant_id": "hub-tenant-001", "hub_discussion_id": "discussion-1", "hub_message_id": "message-1", "request_id": "request-1", "content": "hello source runtime"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/runtime/virtual-employees/src-ve-001/discussion-messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime source-user message status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		EmployeeID string `json:"employee_id"`
		Session    struct {
			Metadata map[string]string `json:"metadata"`
		} `json:"session"`
		Message struct {
			InstanceID string `json:"instance_id"`
			Content    string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode message response: %v", err)
	}
	if out.EmployeeID != "src-ve-001" || out.Message.InstanceID != inst.ID || !strings.Contains(out.Message.Content, "hello source runtime") {
		t.Fatalf("unexpected source-user message response: %#v", out)
	}
	if out.Session.Metadata["client_session_key"] != "discussion-1" {
		t.Fatalf("source-user runtime session not bound to Hub discussion: %#v", out.Session.Metadata)
	}
}

func TestPlatformVirtualEmployeeProvisionAllowsMissingHubLLMKeyAsAttention(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-missing-key",
		"tenant_id":            "hub-tenant-missing-key",
		"platform_tenant_id":   "tenant-missing-key",
		"name":                 "Missing Key Employee",
		"virtual_email":        "missing_key@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"llm_service_group_id": "group-missing-key",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"attention"`) {
		t.Fatalf("provision missing key status=%d body=%s", w.Code, w.Body.String())
	}
	_, _, inst := platformRuntimeForTest(t, svc, "emp-missing-key")
	if inst.Readiness.Ready || inst.Readiness.ConfigValid {
		t.Fatalf("missing key instance should exist but need config attention: %#v", inst.Readiness)
	}
}

func TestPlatformVirtualEmployeeProvisionUsesAutoModelForHubLLM(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)

	tenant, user, _ := platformRuntimeForTest(t, svc, "emp-001")
	cfg, err := svc.GetUserConfig(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMModel != "auto" {
		t.Fatalf("Hub LLM model should be auto, got %#v", cfg.AppConfig.MaclawLLMModel)
	}
	if len(cfg.AppConfig.MaclawLLMProviders) != 1 || cfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("Hub provider model should be auto, got %#v", cfg.AppConfig.MaclawLLMProviders)
	}
}

func TestPlatformLLMModelDoesNotTreatServiceGroupAsModel(t *testing.T) {
	if got := platformLLMModelFromRequest(platformVirtualEmployeeRequest{DefaultLLM: "group-legal", LLMServiceGroupID: "group-legal", HubLLMEndpoint: "https://hub.example/api/llm/v1"}); got != "auto" {
		t.Fatalf("Hub service group must not become virtual employee model, got %q", got)
	}
	if got := platformLLMModelFromRequest(platformVirtualEmployeeRequest{LLMServiceGroupID: "group-legal"}); got != "auto" {
		t.Fatalf("service group without explicit model must not become virtual employee model, got %q", got)
	}
	if got := platformSourceUserLLMModelFromRequest(platformSourceUserRequest{DefaultLLM: "group-display", LLMServiceGroupID: "group-display", HubLLMEndpoint: "https://hub.example/api/llm/v1"}); got != "auto" {
		t.Fatalf("Hub service group must not become source-user model, got %q", got)
	}
	if got := platformSourceUserLLMModelFromRequest(platformSourceUserRequest{LLMModel: "gpt-local", LLMServiceGroupID: "group-display"}); got != "gpt-local" {
		t.Fatalf("explicit model should win, got %q", got)
	}
}

func TestPlatformVirtualEmployeeSourceUserRepairsLegacyServiceGroupModelOnProvisioningPath(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, _ := platformRuntimeForTest(t, svc, "emp-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	cfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	legacy := cfg.AppConfig
	legacy.MaclawLLMModel = "group-legal"
	for i := range legacy.MaclawLLMProviders {
		legacy.MaclawLLMProviders[i].Model = "group-legal"
	}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, legacy); err != nil {
		t.Fatalf("UpdateUserConfig legacy: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/emp-001/runtime-status?tenant_id=tenant-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	cfg, err = svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig after read-only status: %v", err)
	}
	if cfg.AppConfig.MaclawLLMModel != "group-legal" {
		t.Fatalf("read-only runtime status should not repair legacy model, got %#v", cfg.AppConfig.MaclawLLMModel)
	}

	payload := map[string]any{"tenant_id": "tenant-001", "settings_tab": "Channels", "source_user": map[string]any{"id": "emp-001", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}}
	body, _ := json.Marshal(payload)
	req = httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/settings-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("settings link status=%d body=%s", w.Code, w.Body.String())
	}
	var linkOut struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&linkOut); err != nil {
		t.Fatalf("decode settings link: %v", err)
	}
	launchURL, err := url.Parse(linkOut.URL)
	if err != nil {
		t.Fatalf("parse settings link URL %q: %v", linkOut.URL, err)
	}
	if launchURL.Query().Get("settings_tab") != "im" {
		t.Fatalf("settings link should target IM tab: %s", linkOut.URL)
	}
	blockedPayload := map[string]any{"tenant_id": "tenant-001", "settings_tab": "advanced", "source_user": map[string]any{"id": "emp-001", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}}
	blockedBody, _ := json.Marshal(blockedPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/settings-link", bytes.NewReader(blockedBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("advanced settings link status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&linkOut); err != nil {
		t.Fatalf("decode advanced settings link: %v", err)
	}
	launchURL, err = url.Parse(linkOut.URL)
	if err != nil {
		t.Fatalf("parse advanced settings link URL %q: %v", linkOut.URL, err)
	}
	if launchURL.Query().Get("settings_tab") != "" {
		t.Fatalf("advanced settings tab should be hidden from launch URL: %s", linkOut.URL)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/knowledge-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("knowledge link status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&linkOut); err != nil {
		t.Fatalf("decode knowledge link: %v", err)
	}
	launchURL, err = url.Parse(linkOut.URL)
	if err != nil {
		t.Fatalf("parse knowledge link URL %q: %v", linkOut.URL, err)
	}
	if launchURL.Query().Get("view") != "knowledge" {
		t.Fatalf("knowledge link should target knowledge view: %s", linkOut.URL)
	}
	if launchURL.Query().Get("settings_tab") != "" {
		t.Fatalf("knowledge link should not carry settings_tab: %s", linkOut.URL)
	}
	cfg, err = svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig repaired: %v", err)
	}
	if cfg.AppConfig.MaclawLLMModel != "auto" {
		t.Fatalf("legacy service-group model should be repaired to auto, got %#v", cfg.AppConfig.MaclawLLMModel)
	}
	if len(cfg.AppConfig.MaclawLLMProviders) != 1 || cfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("legacy provider service-group model should be repaired to auto, got %#v", cfg.AppConfig.MaclawLLMProviders)
	}
}

func TestNormalizePlatformSettingsTab(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{in: "Channels", want: "im"},
		{in: "channels_more", want: "im"},
		{in: "IM", want: "im"},
		{in: "security", want: "security"},
		{in: "advanced", want: ""},
		{in: "unknown", want: ""},
		{in: " ../im ", want: ""},
	} {
		if got := normalizePlatformSettingsTab(tc.in); got != tc.want {
			t.Fatalf("normalizePlatformSettingsTab(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPlatformVirtualEmployeeSourceUserReusesProvisionedRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, provisionedInstance := platformRuntimeForTest(t, svc, "emp-001")

	payload := map[string]any{"tenant_id": "tenant-001", "source_user": map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Updated Contract Reviewer", "title": "Review updated contract risks", "skill_tags": "contract, compliance", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		TenantID   string `json:"tenant_id"`
		UserID     string `json:"user_id"`
		InstanceID string `json:"instance_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.TenantID != provisionedTenant.ID || out.UserID != provisionedUser.ID || out.InstanceID != provisionedInstance.ID {
		t.Fatalf("source-user launch should reuse provisioned runtime binding, got=%#v tenant=%s user=%s inst=%s", out, provisionedTenant.ID, provisionedUser.ID, provisionedInstance.ID)
	}
	users, err := svc.ListUsers(t.Context(), provisionedTenant.ID, agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected no duplicate runtime user, got %#v", users)
	}
	updated, err := svc.GetInstance(t.Context(), agentservice.Principal{TenantID: provisionedTenant.ID, UserID: provisionedUser.ID}, provisionedInstance.ID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if updated.Metadata["ve_name"] != "Updated Contract Reviewer" || updated.Metadata["ve_skill_description"] != "Review updated contract risks" || updated.Metadata["ve_skill_tags"] != "contract, compliance" {
		t.Fatalf("source-user launch should sync latest virtual employee profile metadata, got %#v", updated.Metadata)
	}
}

func TestPlatformVirtualEmployeeSourceUserMinimalPayloadPreservesProfileMetadata(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, provisionedInstance := platformRuntimeForTest(t, svc, "emp-001")

	payload := map[string]any{"tenant_id": "tenant-001", "source_user": map[string]any{"id": "emp-001", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/settings-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("settings link status=%d body=%s", w.Code, w.Body.String())
	}
	updated, err := svc.GetInstance(t.Context(), agentservice.Principal{TenantID: provisionedTenant.ID, UserID: provisionedUser.ID}, provisionedInstance.ID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if updated.Metadata["ve_name"] != "Contract Reviewer" || updated.Metadata["ve_skill_description"] != "Review contract risks" || updated.Metadata["ve_skill_tags"] != "contract, review" {
		t.Fatalf("minimal source-user payload should preserve existing profile metadata, got %#v", updated.Metadata)
	}
}

func TestPlatformVirtualEmployeeSourceUserExplicitEmptySkillTagsClearsMetadata(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, provisionedInstance := platformRuntimeForTest(t, svc, "emp-001")

	payload := map[string]any{"tenant_id": "tenant-001", "source_user": map[string]any{"id": "emp-001", "display_name": "Contract Reviewer", "external_id": "contract_reviewer", "title": "Review contract risks", "skill_tags": "", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/settings-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("settings link status=%d body=%s", w.Code, w.Body.String())
	}
	updated, err := svc.GetInstance(t.Context(), agentservice.Principal{TenantID: provisionedTenant.ID, UserID: provisionedUser.ID}, provisionedInstance.ID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if _, ok := updated.Metadata["ve_skill_tags"]; ok {
		t.Fatalf("explicit empty source-user skill_tags should clear metadata, got %#v", updated.Metadata)
	}
}

func TestPlatformVirtualEmployeeSourceUserGetEndpointsReuseProvisionedRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, provisionedInstance := platformRuntimeForTest(t, svc, "emp-001")

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/emp-001/assistant-instances?tenant_id=tenant-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant instances status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		TenantID string                  `json:"tenant_id"`
		UserID   string                  `json:"user_id"`
		Items    []agentservice.Instance `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.TenantID != provisionedTenant.ID || out.UserID != provisionedUser.ID || len(out.Items) != 1 || out.Items[0].ID != provisionedInstance.ID {
		t.Fatalf("GET should reuse provisioned binding, got=%#v tenant=%s user=%s inst=%s", out, provisionedTenant.ID, provisionedUser.ID, provisionedInstance.ID)
	}
	users, err := svc.ListUsers(t.Context(), provisionedTenant.ID, agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected no duplicate runtime user, got %#v", users)
	}
}

func TestPlatformVirtualEmployeeSourceUserReusesLegacyProvisionedRuntimeWithoutTenantMetadata(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, provisionedInstance := platformRuntimeForTest(t, svc, "emp-001")
	legacyMetadata := map[string]string{}
	for key, value := range provisionedInstance.Metadata {
		if strings.HasPrefix(key, "ve_") && strings.Contains(key, "tenant") {
			continue
		}
		legacyMetadata[key] = value
	}
	if _, err := svc.UpdateInstance(t.Context(), agentservice.Principal{TenantID: provisionedTenant.ID, UserID: provisionedUser.ID}, provisionedInstance.ID, agentservice.UpdateInstanceInput{Metadata: legacyMetadata}); err != nil {
		t.Fatalf("UpdateInstance: %v", err)
	}

	payload := map[string]any{"tenant_id": "tenant-001", "source_user": map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "title": "Review contract risks", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		TenantID   string `json:"tenant_id"`
		UserID     string `json:"user_id"`
		InstanceID string `json:"instance_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.TenantID != provisionedTenant.ID || out.UserID != provisionedUser.ID || out.InstanceID != provisionedInstance.ID {
		t.Fatalf("legacy source-user launch should reuse provisioned runtime binding, got=%#v tenant=%s user=%s inst=%s", out, provisionedTenant.ID, provisionedUser.ID, provisionedInstance.ID)
	}
}

func TestPlatformSourceUserLinkRefreshesHubLLMViewerToken(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, _ := platformRuntimeForTest(t, svc, "emp-001")

	payload := map[string]any{
		"tenant_id":            "tenant-001",
		"hub_llm_endpoint":     "https://hub.example.test/api/llm/v1",
		"hub_llm_viewer_token": "viewer-token",
		"llm_service_group_id": "ve-service",
		"source_user":          map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	cfg, err := svc.GetUserConfig(t.Context(), agentservice.Principal{TenantID: provisionedTenant.ID, UserID: provisionedUser.ID})
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMUrl != "https://hub.example.test/api/llm/v1" || cfg.AppConfig.MaclawLLMKey == "" || cfg.AppConfig.MaclawLLMKey == "managed-by-hub" || cfg.AppConfig.MaclawLLMModel != "auto" {
		t.Fatalf("source-user LLM config not refreshed: %#v", cfg.AppConfig)
	}
	if cfg.AppConfig.MaclawLLMCurrentProvider != "hub-llm" || len(cfg.AppConfig.MaclawLLMProviders) != 1 || cfg.AppConfig.MaclawLLMProviders[0].URL != "https://hub.example.test/api/llm/v1" || cfg.AppConfig.MaclawLLMProviders[0].Key == "" || cfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("source-user LLM provider config not refreshed: %#v", cfg.AppConfig)
	}
}

func TestPlatformVirtualEmployeeSourceUserInstanceCanBeProvisionedLater(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "title": "Review contract risks", "skill_tags": "contract, review", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/emp-001/assistant-instances", map[string]any{"tenant_id": "tenant-001", "source_user": sourceUser, "name": "Early assistant"}, http.StatusCreated)
	_, _, early := platformRuntimeForTest(t, svc, "emp-001")
	if early.Metadata["ve_source_user_id"] != "emp-001" || early.Metadata["ve_employee_id"] != "emp-001" {
		t.Fatalf("early virtual source-user instance missing dual identity metadata: %#v", early.Metadata)
	}
	if early.Metadata["ve_name"] != "Contract Reviewer" || early.Metadata["ve_handle"] != "contract_reviewer" || early.Metadata["ve_skill_description"] != "Review contract risks" || early.Metadata["ve_skill_tags"] != "contract, review" {
		t.Fatalf("early virtual source-user instance missing profile metadata: %#v", early.Metadata)
	}

	provisionPlatformEmployeeForTest(t, server)
	_, _, provisioned := platformRuntimeForTest(t, svc, "emp-001")
	if provisioned.ID != early.ID {
		t.Fatalf("provision should reuse early virtual source-user instance, early=%s provisioned=%s", early.ID, provisioned.ID)
	}
	if provisioned.Metadata["llm_service_group_id"] != "group-legal" || provisioned.Metadata["ve_source_user_id"] != "emp-001" {
		t.Fatalf("provision should merge metadata, got %#v", provisioned.Metadata)
	}
}

func TestPlatformDeleteVirtualEmployeeDeletesAllAssistantInstances(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, _ := platformRuntimeForTest(t, svc, "emp-001")
	sourceUser := map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "title": "Review contract risks", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/emp-001/assistant-instances", map[string]any{"tenant_id": "tenant-001", "source_user": sourceUser, "name": "Second assistant"}, http.StatusCreated)
	if _, err := svc.CreateInstance(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, agentservice.CreateInstanceInput{Name: "Legacy local instance", Metadata: map[string]string{"legacy": "true"}}); err != nil {
		t.Fatalf("CreateInstance legacy: %v", err)
	}
	instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 3 {
		t.Fatalf("expected provisioned, assistant, and legacy instance before delete, got %#v", instances)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		DeletedInstances   int  `json:"deleted_instances"`
		RemainingInstances int  `json:"remaining_instances"`
		UserDeleted        bool `json:"user_deleted"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if out.DeletedInstances != 3 || out.RemainingInstances != 0 || !out.UserDeleted {
		t.Fatalf("delete should remove all employee assistant instances and user, got %#v", out)
	}
	if _, err := svc.GetUser(t.Context(), tenant.ID, user.ID); err == nil {
		t.Fatal("managed user should be deleted after all employee instances are removed")
	}
}

func TestPlatformDeleteVirtualEmployeeHonorsBodyTenantID(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	for _, item := range []struct{ hubTenantID, platformTenantID, name string }{{"hub-tenant-a", "tenant-a", "Shared A"}, {"hub-tenant-b", "tenant-b", "Shared B"}} {
		body, _ := json.Marshal(map[string]any{"employee_id": "emp-shared", "tenant_id": item.hubTenantID, "platform_tenant_id": item.platformTenantID, "name": item.name, "virtual_email": item.platformTenantID + "@example.test", "hub_llm_endpoint": "https://hub.example.test/llm", "hub_llm_api_key": "test-hub-key", "llm_service_group_id": "group-legal"})
		req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("provision %s status=%d body=%s", item.hubTenantID, w.Code, w.Body.String())
		}
	}
	tenantAID := platformRuntimeTenantIDForTest(t, svc, "hub-tenant-a")
	tenantBID := platformRuntimeTenantIDForTest(t, svc, "hub-tenant-b")
	body, _ := json.Marshal(map[string]any{"employee_id": "emp-shared", "tenant_id": "hub-tenant-b", "platform_tenant_id": "tenant-b", "virtual_email": "tenant-b@example.test"})
	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-shared", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete tenant b status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		TenantID    string `json:"tenant_id"`
		UserDeleted bool   `json:"user_deleted"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if out.TenantID != tenantBID || !out.UserDeleted {
		t.Fatalf("delete should target tenant b, want=%s got=%#v", tenantBID, out)
	}
	usersA, err := svc.ListUsers(t.Context(), tenantAID, agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers tenant A: %v", err)
	}
	usersB, err := svc.ListUsers(t.Context(), tenantBID, agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers tenant B: %v", err)
	}
	if len(usersA) != 1 || len(usersB) != 0 {
		t.Fatalf("delete should keep tenant A and remove tenant B, usersA=%#v usersB=%#v", usersA, usersB)
	}
}

func TestPlatformDeleteVirtualEmployeeDeletesManagedUserWhenInstanceAlreadyMissing(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, inst := platformRuntimeForTest(t, svc, "emp-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if err := svc.DeleteInstance(t.Context(), principal, inst.ID); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"employee_id": "emp-001", "tenant_id": "tenant-001", "platform_tenant_id": "tenant-001", "virtual_email": user.Email})
	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-001", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete orphan status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		UserDeleted bool `json:"user_deleted"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if !out.UserDeleted {
		t.Fatalf("managed user should be deleted when runtime instance is already missing: %#v", out)
	}
	if _, err := svc.GetUser(t.Context(), tenant.ID, user.ID); err == nil {
		t.Fatal("managed user should be deleted")
	}
}

func TestPlatformDeleteVirtualEmployeeDeletesLegacyUnprotectedRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform Legacy"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(t.Context(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Legacy VE User", Email: "legacy-ve@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, err := svc.CreateInstance(t.Context(), principal, agentservice.CreateInstanceInput{Name: "Legacy VE Instance", Metadata: map[string]string{"ve_employee_id": "emp-legacy", "ve_platform_tenant_id": "tenant-legacy"}}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-legacy", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		DeletedInstances   int  `json:"deleted_instances"`
		RemainingInstances int  `json:"remaining_instances"`
		UserDeleted        bool `json:"user_deleted"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if out.DeletedInstances != 1 || out.RemainingInstances != 0 || !out.UserDeleted {
		t.Fatalf("legacy unprotected platform-only user should be deleted, got %#v", out)
	}
	if _, err := svc.GetUser(t.Context(), tenant.ID, user.ID); err == nil {
		t.Fatal("legacy unprotected platform-only user should be deleted")
	}
}
func TestPlatformRuntimeReportAcceptsBearerAdminSecret(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/platform/runtime/report", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Status  string         `json:"status"`
		Summary map[string]any `json:"summary"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status != "ok" || out.Summary == nil {
		t.Fatalf("unexpected report: %#v", out)
	}
}

func TestPlatformSourceUserAssistantLinkCreatesScopedLaunch(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{"tenant_id": "ve-tenant-a", "source_user": map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}}
	seedPlatformSourceUserConfigForTest(t, server, "/api/platform/source-users/src-001/assistant-link", payload)
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		URL         string `json:"url"`
		AccessToken string `json:"access_token"`
		TenantID    string `json:"tenant_id"`
		UserID      string `json:"user_id"`
		InstanceID  string `json:"instance_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.URL == "" || out.TenantID == "" || out.UserID == "" || out.InstanceID == "" {
		t.Fatalf("incomplete launch response: %#v", out)
	}
	if out.AccessToken != "" {
		t.Fatalf("access_token should only be carried inside launch url")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("launch response should be no-store, got %q", w.Header().Get("Cache-Control"))
	}
	launch, err := url.Parse(out.URL)
	if err != nil {
		t.Fatalf("parse launch URL: %v", err)
	}
	if launch.Query().Get("token") != "" || launch.Query().Get("launch_token") == "" {
		t.Fatalf("launch URL should use one-time launch_token only: %s", out.URL)
	}
	exchangeBody, _ := json.Marshal(map[string]any{"launch_token": launch.Query().Get("launch_token")})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/web/exchange", bytes.NewReader(exchangeBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", w.Code, w.Body.String())
	}
	var exchanged struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(w.Body).Decode(&exchanged); err != nil {
		t.Fatalf("decode exchange: %v", err)
	}
	principal, err := svc.Authenticate(exchanged.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate exchanged token: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/web/exchange", bytes.NewReader(exchangeBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("one-time launch token should not exchange twice, status=%d body=%s", w.Code, w.Body.String())
	}
	createdEvents, err := svc.ListAuditEvents(t.Context(), agentservice.ListAuditEventsInput{Action: "web.launch_token.created", ResourceType: "web_launch_token"})
	if err != nil {
		t.Fatalf("ListAuditEvents created: %v", err)
	}
	if len(createdEvents) != 1 || createdEvents[0].TenantID != out.TenantID || createdEvents[0].UserID != out.UserID || createdEvents[0].Metadata["launch_token_hash_prefix"] == "" {
		t.Fatalf("unexpected launch token created audit: %#v", createdEvents)
	}
	exchangedEvents, err := svc.ListAuditEvents(t.Context(), agentservice.ListAuditEventsInput{Action: "web.launch_token.exchanged", ResourceType: "web_launch_token"})
	if err != nil {
		t.Fatalf("ListAuditEvents exchanged: %v", err)
	}
	if len(exchangedEvents) != 1 || exchangedEvents[0].TenantID != out.TenantID || exchangedEvents[0].UserID != out.UserID || exchangedEvents[0].Metadata["source_user_id"] != "src-001" {
		t.Fatalf("unexpected launch token exchanged audit: %#v", exchangedEvents)
	}
	rejectedEvents, err := svc.ListAuditEvents(t.Context(), agentservice.ListAuditEventsInput{Action: "web.launch_token.rejected", ResourceType: "web_launch_token"})
	if err != nil {
		t.Fatalf("ListAuditEvents rejected: %v", err)
	}
	if len(rejectedEvents) != 1 || rejectedEvents[0].Metadata["reason"] == "" || rejectedEvents[0].Metadata["launch_token_hash_prefix"] == "" {
		t.Fatalf("unexpected launch token rejected audit: %#v", rejectedEvents)
	}
	if principal.TenantID != out.TenantID || principal.UserID != out.UserID {
		t.Fatalf("token principal mismatch: %#v out=%#v", principal, out)
	}
	instances, err := svc.ListInstances(t.Context(), *principal)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 1 || instances[0].Metadata["ve_source_user_id"] != "src-001" {
		t.Fatalf("unexpected source user instances: %#v", instances)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/assistant-instances?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(out.InstanceID)) {
		t.Fatalf("assistant instances status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformSourceUserAssistantLinkSanitizesForwardedLaunchURL(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{"tenant_id": "ve-tenant-a", "source_user": map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}}
	seedPlatformSourceUserConfigForTest(t, server, "/api/platform/source-users/src-001/assistant-link", payload)
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("X-Forwarded-Proto", "javascript")
	req.Header.Set("X-Forwarded-Host", "evil.example\\@bad")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	launch, err := url.Parse(out.URL)
	if err != nil {
		t.Fatalf("parse launch URL: %v", err)
	}
	if launch.Scheme != "http" || launch.Host != "127.0.0.1" || launch.Query().Get("launch_token") == "" {
		t.Fatalf("forwarded launch URL was not sanitized: %s", out.URL)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "maclaw.example.test:18443")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link with safe forwarded host status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode safe response: %v", err)
	}
	launch, err = url.Parse(out.URL)
	if err != nil {
		t.Fatalf("parse safe launch URL: %v", err)
	}
	if launch.Scheme != "https" || launch.Host != "maclaw.example.test:18443" {
		t.Fatalf("safe forwarded launch URL was not preserved: %s", out.URL)
	}
}

func TestPlatformSourceUserInstancesShareAndPreserveUserConfig(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{"tenant_id": "ve-tenant-a", "source_user": map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": payload["source_user"], "name": "One"}, http.StatusCreated)
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": payload["source_user"], "name": "Two"}, http.StatusCreated)

	tenant, user := platformSourceRuntimeUserForTest(t, svc, "src-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}

	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-link", payload, http.StatusOK)
	cfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMUrl != testLLMConfig().MaclawLLMUrl || cfg.AppConfig.MaclawLLMModel != testLLMConfig().MaclawLLMModel {
		t.Fatalf("source user launch should preserve shared user config, got %#v", cfg.AppConfig)
	}
	instances, err := svc.ListInstances(t.Context(), principal)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 2 || instances[0].UserID != instances[1].UserID {
		t.Fatalf("expected multiple instances under one shared user, got %#v", instances)
	}
}

func TestPlatformSourceUserDefaultConfigPreservesExistingNonLLMConfig(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	payload := map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "One"}
	body, _ := json.Marshal(payload)
	var in platformSourceUserRequest
	if err := json.Unmarshal(body, &in); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	binding, found, err := server.platformSourceUserBindingFromRequest(httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-instances", bytes.NewReader(body)), in, true)
	if err != nil {
		t.Fatalf("source binding: %v", err)
	}
	if !found {
		t.Fatal("source binding was not created")
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	preserved := corelib.AppConfig{
		MCPServers:               []corelib.MCPServerEntry{{ID: "mcp-a", Name: "MCP A", EndpointURL: "https://mcp.example.test/sse", AuthType: "none", Source: corelib.MCPSourceManual}},
		MaclawAgentMaxIterations: 7,
	}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, preserved); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}

	if err := server.ensurePlatformSourceUserDefaultConfig(httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-instances", nil), principal); err != nil {
		t.Fatalf("ensure default config: %v", err)
	}
	cfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if len(cfg.AppConfig.MCPServers) != 1 || cfg.AppConfig.MCPServers[0].Name != "MCP A" || cfg.AppConfig.MaclawAgentMaxIterations != 7 {
		t.Fatalf("source user default LLM config should preserve non-LLM config, got %#v", cfg.AppConfig)
	}
	if cfg.AppConfig.MaclawLLMUrl == "" || cfg.AppConfig.MaclawLLMKey == "" || cfg.AppConfig.MaclawLLMModel == "" {
		t.Fatalf("expected source user default LLM placeholders, got %#v", cfg.AppConfig)
	}
}

func TestPlatformSourceUserProvisioningPersistsSSHHosts(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-ssh", "external_id": "real-ssh", "email": "ssh@example.test", "display_name": "SSH User"}
	payload := map[string]any{
		"tenant_id":            "ve-tenant-a",
		"source_user":          sourceUser,
		"name":                 "SSH assistant",
		"hub_llm_endpoint":     "https://hub.example.test/api/llm/v1",
		"hub_llm_viewer_token": "viewer-token",
		"llm_service_group_id": "default-group",
		"ssh_hosts": []map[string]any{
			{"label": "prod-web", "host": "10.0.0.10", "port": 22, "user": "deploy", "auth_method": "agent"},
			{"label": "prod-web", "host": "ignored.example.test", "user": "root"},
			{"label": "broken", "host": "", "user": "deploy"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-ssh/assistant-instances", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create source-user instance status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	cfg, err := svc.GetUserConfig(t.Context(), agentservice.Principal{TenantID: out.TenantID, UserID: out.UserID})
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if len(cfg.AppConfig.SSHHosts) != 1 || cfg.AppConfig.SSHHosts[0].Label != "prod-web" || cfg.AppConfig.SSHHosts[0].Host != "10.0.0.10" || cfg.AppConfig.SSHHosts[0].User != "deploy" {
		t.Fatalf("expected normalized ssh host config, got %#v", cfg.AppConfig.SSHHosts)
	}
}

func TestPlatformSourceUserProvisioningTrimsSourceUserID(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": " src-trim ", "external_id": "real-trim", "email": "trim@example.test", "display_name": "Trim User"}
	body, _ := json.Marshal(map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "Trim assistant", "hub_llm_endpoint": "https://hub.example.test/api/llm/v1", "hub_llm_viewer_token": "viewer-token", "llm_service_group_id": "default-group"})
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-trim/assistant-instances", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create source-user instance status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Instance struct {
			Metadata map[string]string `json:"metadata"`
		} `json:"instance"`
		SourceUserID string `json:"source_user_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if out.SourceUserID != "src-trim" || out.Instance.Metadata["ve_source_user_id"] != "src-trim" {
		t.Fatalf("source user id should be trimmed before provisioning, got %#v", out)
	}
}

func TestPlatformUserSSHHostsCanBeClearedExplicitly(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	tenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(t.Context(), agentservice.CreateUserInput{TenantID: tenant.ID, Email: "clear@example.test", Name: "Clear"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, corelib.AppConfig{SSHHosts: []corelib.SSHHostEntry{{Label: "prod", Host: "10.0.0.10", User: "deploy"}}}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	if err := server.updatePlatformUserSSHHosts(req, principal, []corelib.SSHHostEntry{}); err != nil {
		t.Fatalf("updatePlatformUserSSHHosts: %v", err)
	}
	cfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if len(cfg.AppConfig.SSHHosts) != 0 {
		t.Fatalf("expected ssh hosts cleared, got %#v", cfg.AppConfig.SSHHosts)
	}
}

func TestPlatformSourceUserAssistantLinkRejectsUnknownInstance(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "One"}, http.StatusCreated)

	body, _ := json.Marshal(map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "instance_id": "missing-instance"})
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || !bytes.Contains(w.Body.Bytes(), []byte("assistant instance not found")) {
		t.Fatalf("unknown instance link status=%d body=%s", w.Code, w.Body.String())
	}
	createdEvents, err := svc.ListAuditEvents(t.Context(), agentservice.ListAuditEventsInput{Action: "web.launch_token.created", ResourceType: "web_launch_token"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(createdEvents) != 0 {
		t.Fatalf("unknown instance should not mint launch token, got %#v", createdEvents)
	}
}

func TestPlatformSourceUserRuntimeStatusSummarizesInstancesAndConfig(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "One"}, http.StatusCreated)
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "Two"}, http.StatusCreated)

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/runtime-status?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		SourceUserID  string `json:"source_user_id"`
		InstanceCount int    `json:"instance_count"`
		ConfigStatus  struct {
			Valid bool `json:"valid"`
		} `json:"config_status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if out.SourceUserID != "src-001" || out.InstanceCount != 2 || !out.ConfigStatus.Valid {
		t.Fatalf("unexpected runtime status: %#v", out)
	}
}

func TestPlatformSourceUserRuntimeStatusDoesNotProvisionRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-ghost/runtime-status?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Status       string `json:"status"`
		SourceUserID string `json:"source_user_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if out.Status != "not_provisioned" || out.SourceUserID != "src-ghost" {
		t.Fatalf("unexpected runtime status: %#v", out)
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("runtime status should not create tenants/users, got tenants=%#v", tenants)
	}
}

func TestPlatformSourceUserRuntimeStatusDoesNotCreateUserInExistingTenant(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	tenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: platformTenantDisplayName("ve-tenant-a", platformVirtualEmployeeRequest{TenantID: "ve-tenant-a", PlatformTenantID: "ve-tenant-a", TenantName: "ve-tenant-a"})})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-ghost/runtime-status?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	users, err := svc.ListUsers(t.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("runtime status should not create users in existing tenant, got users=%#v", users)
	}
}

func TestPlatformSourceUserAssistantInstancesDoesNotProvisionRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-ghost/assistant-instances?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant instances=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Status       string                  `json:"status"`
		SourceUserID string                  `json:"source_user_id"`
		Items        []agentservice.Instance `json:"items"`
		ConfigStatus struct {
			Valid  bool   `json:"valid"`
			Reason string `json:"reason"`
		} `json:"config_status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode assistant instances: %v", err)
	}
	if out.Status != "not_provisioned" || out.SourceUserID != "src-ghost" || len(out.Items) != 0 || out.ConfigStatus.Valid || out.ConfigStatus.Reason != "not_provisioned" {
		t.Fatalf("unexpected assistant instances response: %#v", out)
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("assistant instances should not create tenants/users, got tenants=%#v", tenants)
	}
}

func TestPlatformSourceUserExplicitAssistantLinkStillProvisionsAfterReadOnlyStatus(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/runtime-status?tenant_id=ve-tenant-a", nil)
	statusReq.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	statusRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK || !strings.Contains(statusRec.Body.String(), `"status":"not_provisioned"`) {
		t.Fatalf("read-only runtime status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}

	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-link", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser}, http.StatusOK)
	tenant, user := platformSourceRuntimeUserForTest(t, svc, "src-001")
	instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 1 || instances[0].Metadata["ve_source_user_id"] != "src-001" {
		t.Fatalf("explicit assistant link should provision source runtime, got instances=%#v", instances)
	}
}

func TestPlatformSourceUserRuntimeStatusIgnoresOrphanRuntimeUserWithoutBinding(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := platformSourceUser{ID: "src-orphan", ExternalID: "real-orphan", Email: "orphan@example.test", DisplayName: "Orphan User"}
	seedReq := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-orphan/assistant-instances", nil)
	if _, found, err := server.platformSourceUserBindingFromRequest(seedReq, platformSourceUserRequest{TenantID: "ve-tenant-a", SourceUser: sourceUser}, true); err != nil {
		t.Fatalf("seed orphan runtime user: %v", err)
	} else if !found {
		t.Fatal("seed orphan runtime user was not created")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-orphan/runtime-status?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Status       string `json:"status"`
		SourceUserID string `json:"source_user_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if out.Status != "not_provisioned" || out.SourceUserID != "src-orphan" {
		t.Fatalf("orphan runtime user should not count as provisioned source binding: %#v", out)
	}
}

func TestPlatformSourceUserRuntimeStatusIgnoresOtherSourceInstancesOnSameUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "One"}, http.StatusCreated)

	tenant, user := platformSourceRuntimeUserForTest(t, svc, "src-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.CreateInstance(t.Context(), principal, agentservice.CreateInstanceInput{Name: "Other source", Metadata: map[string]string{"ve_source_user_id": "src-other", "ve_platform_tenant_id": "ve-tenant-a"}}); err != nil {
		t.Fatalf("CreateInstance other source: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/runtime-status?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		InstanceCount int `json:"instance_count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if out.InstanceCount != 1 {
		t.Fatalf("expected only src-001 instances, got %#v", out)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/assistant-instances?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant instances=%d body=%s", w.Code, w.Body.String())
	}
	var list struct {
		Items []agentservice.Instance `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode assistant instances: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Metadata["ve_source_user_id"] != "src-001" {
		t.Fatalf("expected scoped assistant instances, got %#v", list.Items)
	}
}

func TestPlatformSourceUsersRuntimeStatusBatch(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sources := []map[string]any{
		{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"},
		{"id": "src-002", "external_id": "real-b", "email": "real-b@example.test", "display_name": "Real B"},
	}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sources[0], "name": "One"}, http.StatusCreated)
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-002/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sources[1], "name": "Two"}, http.StatusCreated)

	body, _ := json.Marshal(map[string]any{"tenant_id": "ve-tenant-a", "source_users": sources})
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/runtime-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	items, ok := out["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("unexpected batch response: %#v", out)
	}
	first, _ := items[0].(map[string]any)
	second, _ := items[1].(map[string]any)
	if first["source_user_id"] != "src-001" || second["source_user_id"] != "src-002" || first["instance_count"].(float64) != 1 || second["instance_count"].(float64) != 1 {
		t.Fatalf("unexpected batch items: %#v", items)
	}
}

func TestPlatformSourceUsersRuntimeStatusBatchDoesNotProvisionRuntimeUsers(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sources := []map[string]any{{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}}
	body, _ := json.Marshal(map[string]any{"tenant_id": "ve-tenant-a", "source_users": sources})
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/runtime-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch runtime status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"status":"not_provisioned"`) {
		t.Fatalf("expected not_provisioned response, body=%s", w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 0 {
		t.Fatalf("batch runtime status should not create tenants/users, got tenants=%#v", tenants)
	}
}

func TestPlatformSourceUsersRuntimeStatusBatchDoesNotProvisionMixedUnboundUsers(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sources := []map[string]any{
		{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"},
		{"id": "src-ghost", "external_id": "real-ghost", "email": "ghost@example.test", "display_name": "Ghost"},
	}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sources[0], "name": "One"}, http.StatusCreated)
	tenant, _ := platformSourceRuntimeUserForTest(t, svc, "src-001")

	body, _ := json.Marshal(map[string]any{"tenant_id": "ve-tenant-a", "source_users": sources})
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/runtime-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Items []struct {
			Status       string `json:"status"`
			SourceUserID string `json:"source_user_id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if len(out.Items) != 2 || out.Items[0].SourceUserID != "src-001" || out.Items[1].SourceUserID != "src-ghost" || out.Items[1].Status != "not_provisioned" {
		t.Fatalf("unexpected mixed batch response: %#v", out.Items)
	}
	users, err := svc.ListUsers(t.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("batch runtime status should not create user for unbound source user, got users=%#v", users)
	}
}

func TestPlatformRuntimeReportUsesPlatformEmployeeIDs(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)

	req := httptest.NewRequest(http.MethodGet, "/api/platform/runtime/report", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Users []struct {
			EmployeeID    string `json:"employee_id"`
			RuntimeUserID string `json:"runtime_user_id"`
			RuntimeStatus string `json:"runtime_status"`
			VirtualEmail  string `json:"virtual_email"`
		} `json:"users"`
		Instances []struct {
			EmployeeID    string `json:"employee_id"`
			RuntimeUserID string `json:"runtime_user_id"`
		} `json:"instances"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Users) != 1 || out.Users[0].EmployeeID != "emp-001" || out.Users[0].RuntimeUserID == "" || out.Users[0].RuntimeUserID == "emp-001" || out.Users[0].RuntimeStatus != "ready" {
		t.Fatalf("unexpected platform user report: %#v", out.Users)
	}
	if len(out.Instances) != 1 || out.Instances[0].EmployeeID != "emp-001" || out.Instances[0].RuntimeUserID != out.Users[0].RuntimeUserID {
		t.Fatalf("unexpected platform instance report: %#v", out.Instances)
	}
}

func TestPlatformRuntimeReportScopesByHubTenantID(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	for _, payload := range []map[string]any{
		{"employee_id": "emp-a", "tenant_id": "hub-tenant-a", "platform_tenant_id": "tenant-a", "name": "Employee A", "virtual_email": "employee-a@example.test", "hub_llm_endpoint": "https://hub.example.test/llm", "hub_llm_api_key": "test-hub-key"},
		{"employee_id": "emp-b", "tenant_id": "hub-tenant-b", "platform_tenant_id": "tenant-b", "name": "Employee B", "virtual_email": "employee-b@example.test", "hub_llm_endpoint": "https://hub.example.test/llm", "hub_llm_api_key": "test-hub-key"},
	} {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/runtime/report", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("X-Hub-Tenant-ID", "hub-tenant-a")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Users []struct {
			EmployeeID    string `json:"employee_id"`
			RuntimeStatus string `json:"runtime_status"`
		} `json:"users"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Users) != 1 || out.Users[0].EmployeeID != "emp-a" || out.Users[0].RuntimeStatus != "ready" {
		t.Fatalf("unexpected scoped report: %#v body=%s", out.Users, w.Body.String())
	}
}

func TestPlatformVirtualEmployeeProvisionIgnoresUnknownFields(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":           "emp-extra",
		"tenant_id":             "hub-tenant-extra",
		"platform_tenant_id":    "tenant-extra",
		"name":                  "Extra Field Employee",
		"handle":                "extra_field_employee",
		"virtual_email":         "extra_field_employee@example.test",
		"skill_tags":            []string{"extra"},
		"hub_llm_endpoint":      "https://hub.example.test/llm",
		"hub_llm_api_key":       "test-hub-key",
		"llm_service_group_id":  "group-extra",
		"future_platform_field": "kept-compatible",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformVirtualEmployeeProvisionUsesTenantDisplayName(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-display",
		"tenant_id":            "hub-tenant-display",
		"platform_tenant_id":   "tenant-display",
		"tenant_name":          "Tenant Display",
		"tenant_code":          "sample-local",
		"hub_tenant_code":      "sample-hub",
		"name":                 "Display Employee",
		"handle":               "display_employee",
		"virtual_email":        "display_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].Name != "VE Platform Tenant Display (sample-hub)" {
		t.Fatalf("expected readable VE Platform tenant name, got %#v", tenants)
	}
}

func TestPlatformVirtualEmployeeProvisionRenamesLegacyTenant(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	legacy, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform hub-tenant-display", DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-display",
		"tenant_id":            "hub-tenant-display",
		"platform_tenant_id":   "tenant-display",
		"tenant_name":          "Tenant Display",
		"hub_tenant_code":      "sample-hub",
		"name":                 "Display Employee",
		"handle":               "display_employee",
		"virtual_email":        "display_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].ID != legacy.ID || tenants[0].Name != "VE Platform Tenant Display (sample-hub)" {
		t.Fatalf("expected legacy tenant to be renamed and reused, got %#v", tenants)
	}
}

func TestPlatformVirtualEmployeeProvisionRenamesTenantWhenHubNameChanges(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	legacy, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform Old Tenant (sample-hub)", DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-display",
		"tenant_id":            "hub-tenant-display",
		"platform_tenant_id":   "tenant-display",
		"tenant_name":          "New Tenant",
		"hub_tenant_code":      "sample-hub",
		"name":                 "Display Employee",
		"handle":               "display_employee",
		"virtual_email":        "display_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].ID != legacy.ID || tenants[0].Name != "VE Platform New Tenant (sample-hub)" {
		t.Fatalf("expected managed tenant to be renamed without duplicate, got %#v", tenants)
	}
}

func TestPlatformVirtualEmployeeProvisionStoresTenantIdentityMetadata(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	_, _, inst := platformRuntimeForTest(t, svc, "emp-001")
	if inst.Metadata["ve_hub_tenant_id"] != "hub-tenant-001" || inst.Metadata["ve_platform_tenant_id"] != "tenant-001" {
		t.Fatalf("missing tenant identity metadata: %#v", inst.Metadata)
	}
	if _, ok := inst.Metadata["ve_hub_tenant_code"]; ok {
		t.Fatalf("empty metadata values should be omitted: %#v", inst.Metadata)
	}
}

func TestPlatformVirtualEmployeeProvisionFallsBackToEmployeeIDEmail(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	for _, id := range []string{"emp-fallback-one", "emp fallback two"} {
		payload := map[string]any{
			"employee_id":          id,
			"tenant_id":            "hub-tenant-fallback",
			"platform_tenant_id":   "tenant-fallback",
			"tenant_name":          "Fallback Tenant",
			"name":                 "Fallback Employee",
			"hub_llm_endpoint":     "https://hub.example.test/llm",
			"hub_llm_api_key":      "test-hub-key",
			"llm_service_group_id": "group-fallback",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("provision %s status=%d body=%s", id, w.Code, w.Body.String())
		}
	}
	users, err := svc.ListUsers(t.Context(), platformRuntimeTenantIDForTest(t, svc, "hub-tenant-fallback"), agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 || users[0].Email == "@ve-platform.local" || users[1].Email == "@ve-platform.local" || users[0].Email == users[1].Email {
		t.Fatalf("expected stable distinct fallback emails, got %#v", users)
	}
}

func TestPlatformVirtualEmployeeProvisionFallbackEmailAvoidsSanitizedCollisions(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	for _, id := range []string{"emp/a", "emp a"} {
		payload := map[string]any{
			"employee_id":          id,
			"tenant_id":            "hub-tenant-collision",
			"platform_tenant_id":   "tenant-collision",
			"tenant_name":          "Collision Tenant",
			"name":                 "Collision Employee",
			"handle":               "emp a",
			"hub_llm_endpoint":     "https://hub.example.test/llm",
			"hub_llm_api_key":      "test-hub-key",
			"llm_service_group_id": "group-collision",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("provision %s status=%d body=%s", id, w.Code, w.Body.String())
		}
	}
	users, err := svc.ListUsers(t.Context(), platformRuntimeTenantIDForTest(t, svc, "hub-tenant-collision"), agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 || users[0].Email == users[1].Email {
		t.Fatalf("expected collision-safe fallback emails, got %#v", users)
	}
}

func TestPlatformVirtualEmployeeProvisionRefreshesExistingRuntimeIdentity(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, inst := platformRuntimeForTest(t, svc, "emp-001")
	if inst.Metadata["ve_hub_tenant_code"] != "" {
		t.Fatalf("test setup expected empty hub tenant code: %#v", inst.Metadata)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	currentCfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	preservedApp := currentCfg.AppConfig
	preservedApp.MCPServers = []corelib.MCPServerEntry{{ID: "mcp-a", Name: "MCP A", EndpointURL: "https://mcp.example.test/sse", AuthType: "none", Source: corelib.MCPSourceManual}}
	preservedApp.MaclawAgentMaxIterations = 9
	if _, err := svc.UpdateUserConfig(t.Context(), principal, preservedApp); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	payload := map[string]any{
		"employee_id":           "emp-001",
		"tenant_id":             "hub-tenant-001",
		"platform_tenant_id":    "tenant-001",
		"tenant_name":           "Tenant Display",
		"tenant_code":           "local-code",
		"hub_tenant_code":       "hub-code",
		"name":                  "Updated Reviewer",
		"handle":                "contract_reviewer",
		"virtual_email":         "contract_reviewer@example.test",
		"skill_description":     "Updated contract risk review",
		"hub_llm_endpoint":      "https://hub.example.test/llm",
		"hub_llm_api_key":       "test-hub-key",
		"llm_service_group_id":  "group-updated",
		"custom_runtime_marker": "ignored",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reprovision status=%d body=%s", w.Code, w.Body.String())
	}
	_, updatedUser, updatedInst := platformRuntimeForTest(t, svc, "emp-001")
	if updatedUser.ID != user.ID || updatedUser.Name != "Updated Reviewer" {
		t.Fatalf("expected managed user to be refreshed, got %#v", updatedUser)
	}
	if updatedInst.ID != inst.ID || updatedInst.Name != "Updated Reviewer" || updatedInst.Description != "Updated contract risk review" {
		t.Fatalf("expected instance profile refresh, got %#v", updatedInst)
	}
	if updatedInst.Metadata["ve_hub_tenant_code"] != "hub-code" || updatedInst.Metadata["ve_tenant_code"] != "local-code" || updatedInst.Metadata["llm_service_group_id"] != "group-updated" {
		t.Fatalf("expected refreshed metadata, got %#v", updatedInst.Metadata)
	}
	updatedCfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig after reprovision: %v", err)
	}
	if len(updatedCfg.AppConfig.MCPServers) != 1 || updatedCfg.AppConfig.MCPServers[0].Name != "MCP A" || updatedCfg.AppConfig.MaclawAgentMaxIterations != 9 {
		t.Fatalf("expected non-LLM user config to be preserved, got %#v", updatedCfg.AppConfig)
	}
	if updatedCfg.AppConfig.MaclawLLMCurrentProvider != "hub-llm" || len(updatedCfg.AppConfig.MaclawLLMProviders) != 1 || updatedCfg.AppConfig.MaclawLLMProviders[0].URL != "https://hub.example.test/llm" || updatedCfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("expected refreshed Hub LLM provider config, got %#v", updatedCfg.AppConfig)
	}
}

func TestPlatformVirtualEmployeeProvisionDoesNotMatchEmptyTenantIdentity(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	oldTenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform Old", DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	oldUser, err := svc.CreateUser(t.Context(), agentservice.CreateUserInput{TenantID: oldTenant.ID, Name: "Old User", Email: "old@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: oldTenant.ID, UserID: oldUser.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, err := svc.CreateInstance(t.Context(), principal, agentservice.CreateInstanceInput{Name: "Old Instance", Metadata: map[string]string{"ve_employee_id": "old-emp"}}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-no-identity",
		"tenant_id":            "hub-tenant-new",
		"tenant_name":          "New Tenant",
		"name":                 "No Identity Employee",
		"handle":               "no_identity_employee",
		"virtual_email":        "no_identity_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("expected a new tenant instead of empty-identity match, got %#v", tenants)
	}
}

func TestPlatformVirtualEmployeeProvisionDoesNotReuseUnmanagedTenantByName(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	manual, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform Tenant Display (sample-hub)"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-display",
		"tenant_id":            "hub-tenant-display",
		"platform_tenant_id":   "tenant-display",
		"tenant_name":          "Tenant Display",
		"hub_tenant_code":      "sample-hub",
		"name":                 "Display Employee",
		"handle":               "display_employee",
		"virtual_email":        "display_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("expected unmanaged tenant not to be reused, got %#v", tenants)
	}
	if got, err := svc.GetTenant(t.Context(), manual.ID); err != nil || got.DeleteProtected {
		t.Fatalf("manual tenant should remain unmanaged, got %#v err=%v", got, err)
	}
}

func TestPlatformVirtualEmployeeProvisionAcceptsStringSkillTags(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-string-tags",
		"tenant_id":            "hub-tenant-tags",
		"platform_tenant_id":   "tenant-tags",
		"name":                 "String Tags Employee",
		"handle":               "string_tags_employee",
		"virtual_email":        "string_tags_employee@example.test",
		"skill_tags":           "contract, review; redact",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-tags",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformRuntimeFollowupEndpoints(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	paths := []string{
		"/api/platform/virtual-employees/emp-001/knowledge/imports",
		"/api/platform/virtual-employees/emp-001/migrations/imports",
		"/api/platform/sync/jobs/job-001/run",
		"/api/platform/sync/conflicts/conflict-001/resolve",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"id":"x","employee_id":"emp-001"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code < 200 || w.Code >= 300 {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestPlatformRuntimeFollowupRejectsMissingEmployee(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees/missing/knowledge/imports", bytes.NewBufferString(`{"id":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing employee status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformVirtualEmployeeDeleteRemovesRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	bindingTenant, bindingUser, _ := platformRuntimeForTest(t, svc, "emp-001")

	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(t.Context(), bindingTenant.ID, bindingUser.ID); err == nil {
		t.Fatalf("expected runtime user to be deleted")
	}
	missing := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees/emp-001/knowledge/imports", bytes.NewBufferString(`{"id":"x"}`))
	missing.Header.Set("Content-Type", "application/json")
	missing.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, missing)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected followup missing after delete, status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformVirtualEmployeeDeleteKeepsSharedRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform hub-tenant-001"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(t.Context(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Shared User", Email: "contract_reviewer@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	extra, err := svc.CreateInstance(t.Context(), principal, agentservice.CreateInstanceInput{Name: "Manual Instance"})
	if err != nil {
		t.Fatalf("CreateInstance extra: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	_, _, veInst := platformRuntimeForTest(t, svc, "emp-001")
	if veInst.UserID != user.ID {
		t.Fatalf("expected platform provision to reuse shared user, got %s want %s", veInst.UserID, user.ID)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(t.Context(), tenant.ID, user.ID); err != nil {
		t.Fatalf("shared user should be kept: %v", err)
	}
	instances, err := svc.ListInstances(t.Context(), principal)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != extra.ID {
		t.Fatalf("expected only extra instance to remain: %#v", instances)
	}
}

func provisionPlatformEmployeeForTest(t *testing.T, server *HTTPServer) {
	t.Helper()
	payload := map[string]any{
		"employee_id":          "emp-001",
		"tenant_id":            "hub-tenant-001",
		"platform_tenant_id":   "tenant-001",
		"name":                 "Contract Reviewer",
		"handle":               "contract_reviewer",
		"virtual_email":        "contract_reviewer@example.test",
		"skill_description":    "Review contract risks",
		"skill_tags":           []string{"contract", "review"},
		"avatar_data_url":      testPlatformAvatarDataURL,
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-legal",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
}

func postPlatformJSONForTest(t *testing.T, server *HTTPServer, path string, payload map[string]any, want int) {
	t.Helper()
	seedPlatformSourceUserConfigForTest(t, server, path, payload)
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("%s status=%d want=%d body=%s", path, w.Code, want, w.Body.String())
	}
}

func seedPlatformSourceUserConfigForTest(t *testing.T, server *HTTPServer, path string, payload map[string]any) {
	t.Helper()
	if !strings.Contains(path, "/api/platform/source-users/") || !strings.Contains(path, "/assistant") {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal source-user payload: %v", err)
	}
	var in platformSourceUserRequest
	if err := json.Unmarshal(body, &in); err != nil || strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.SourceUser.ID) == "" {
		return
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	binding, found, err := server.platformSourceUserBindingFromRequest(req, in, true)
	if err != nil {
		t.Fatalf("seed source-user binding: %v", err)
	}
	if !found {
		t.Fatal("seed source-user binding was not created")
	}
	if _, err := server.svc.UpdateUserConfig(t.Context(), agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}, testLLMConfig()); err != nil {
		t.Fatalf("seed source-user config: %v", err)
	}
}

func platformSourceRuntimeUserForTest(t *testing.T, svc *agentservice.Service, sourceUserID string) (agentservice.Tenant, agentservice.User) {
	t.Helper()
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	for _, tenant := range tenants {
		users, err := svc.ListUsers(t.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		for _, user := range users {
			instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
			if err != nil {
				t.Fatalf("ListInstances: %v", err)
			}
			for _, inst := range instances {
				if inst.Metadata["ve_source_user_id"] == sourceUserID {
					return tenant, user
				}
			}
		}
	}
	t.Fatalf("runtime source user %s not found", sourceUserID)
	return agentservice.Tenant{}, agentservice.User{}
}

func platformRuntimeForTest(t *testing.T, svc *agentservice.Service, employeeID string) (agentservice.Tenant, agentservice.User, agentservice.Instance) {
	t.Helper()
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	for _, tenant := range tenants {
		users, err := svc.ListUsers(t.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		for _, user := range users {
			instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
			if err != nil {
				t.Fatalf("ListInstances: %v", err)
			}
			for _, inst := range instances {
				if inst.Metadata["ve_employee_id"] == employeeID {
					return tenant, user, inst
				}
			}
		}
	}
	t.Fatalf("runtime for employee %s not found", employeeID)
	return agentservice.Tenant{}, agentservice.User{}, agentservice.Instance{}
}

func platformRuntimeTenantIDForTest(t *testing.T, svc *agentservice.Service, hubTenantID string) string {
	t.Helper()
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	for _, tenant := range tenants {
		users, err := svc.ListUsers(t.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		for _, user := range users {
			instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
			if err != nil {
				t.Fatalf("ListInstances: %v", err)
			}
			for _, inst := range instances {
				if inst.Metadata["ve_hub_tenant_id"] == hubTenantID {
					return tenant.ID
				}
			}
		}
	}
	t.Fatalf("runtime tenant for hub tenant %s not found", hubTenantID)
	return ""
}
