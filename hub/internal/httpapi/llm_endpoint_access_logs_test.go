package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestGetLLMEndpointAccessLogsHandlerMergesPendingEntries(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointAccessLogAccumulator.mu.Lock()
	savedPending := globalLLMEndpointAccessLogAccumulator.pending
	globalLLMEndpointAccessLogAccumulator.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	defer func() {
		globalLLMEndpointAccessLogAccumulator.mu.Lock()
		globalLLMEndpointAccessLogAccumulator.pending = savedPending
		globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	}()

	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{Email: "a@example.com", ClientIP: "10.0.0.1", StatusCode: http.StatusOK, TotalCostRMB: 1.25, RequestBody: `{"model":"auto"}`})
	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{Email: "b@example.com", ClientIP: "10.0.0.2", StatusCode: http.StatusBadGateway, ErrorCode: "LLM_UPSTREAM_FAILED", RequestBody: `{"model":"auto"}`})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/access-logs?limit=10", nil)
	rec := httptest.NewRecorder()
	GetLLMEndpointAccessLogsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp llmEndpointAccessLogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Summary.TotalRequests != 2 {
		t.Fatalf("summary total_requests = %d, want 2", resp.Summary.TotalRequests)
	}
	if resp.Summary.UniqueIPCount != 2 {
		t.Fatalf("summary unique_ip_count = %d, want 2", resp.Summary.UniqueIPCount)
	}
	if len(resp.Logs) != 2 {
		t.Fatalf("logs len = %d, want 2", len(resp.Logs))
	}
	foundCost := false
	for _, item := range resp.Logs {
		if item.TotalCostRMB == 1.25 {
			foundCost = true
		}
	}
	if !foundCost {
		t.Fatalf("total_cost_rmb not returned in logs: %#v", resp.Logs)
	}
}

func TestGetLLMEndpointAccessLogsHandlerUsesTenantScopedSettings(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointAccessLogAccumulator.mu.Lock()
	savedPending := globalLLMEndpointAccessLogAccumulator.pending
	globalLLMEndpointAccessLogAccumulator.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	defer func() {
		globalLLMEndpointAccessLogAccumulator.mu.Lock()
		globalLLMEndpointAccessLogAccumulator.pending = savedPending
		globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	}()

	globalLogs := newLLMEndpointAccessLogStore()
	globalLogs.add(llmEndpointAccessLogEntry{Email: "global@example.com", ClientIP: "10.0.0.1", StatusCode: http.StatusOK})
	if err := saveLLMEndpointAccessLogs(context.Background(), system, globalLogs); err != nil {
		t.Fatalf("save global access logs: %v", err)
	}

	tenantLogs := newLLMEndpointAccessLogStore()
	tenantLogs.add(llmEndpointAccessLogEntry{Email: "tenant@example.com", ClientIP: "10.0.0.2", StatusCode: http.StatusOK})
	if err := saveLLMEndpointAccessLogs(context.Background(), scopedSystemSettingsForTenant("tenant_a", system), tenantLogs); err != nil {
		t.Fatalf("save tenant access logs: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/access-logs?limit=10", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{Scope: "tenant", TenantID: "tenant_a"}))
	rec := httptest.NewRecorder()
	GetLLMEndpointAccessLogsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp llmEndpointAccessLogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Total != 1 || len(resp.Logs) != 1 || resp.Logs[0].Email != "tenant@example.com" {
		t.Fatalf("tenant access logs should not read global settings: total=%d logs=%#v", resp.Total, resp.Logs)
	}
}

func TestPruneLLMEndpointAccessLogsKeepsLatestEntries(t *testing.T) {
	logs := newLLMEndpointAccessLogStore()
	for i := 0; i < llmEndpointAccessLogsKeepEntries+5; i++ {
		logs.add(llmEndpointAccessLogEntry{
			Email:      "user@example.com",
			ClientIP:   "10.0.0.1",
			StatusCode: http.StatusOK,
			CreatedAt:  time.Unix(int64(i), 0).UTC(),
		})
	}
	if len(logs.Entries) != llmEndpointAccessLogsKeepEntries {
		t.Fatalf("entries len = %d, want %d", len(logs.Entries), llmEndpointAccessLogsKeepEntries)
	}
	if got := logs.Entries[0].CreatedAt; !got.Equal(time.Unix(5, 0).UTC()) {
		t.Fatalf("oldest kept entry = %s, want unix 5", got)
	}
	if got := logs.Entries[len(logs.Entries)-1].CreatedAt; !got.Equal(time.Unix(int64(llmEndpointAccessLogsKeepEntries+4), 0).UTC()) {
		t.Fatalf("newest kept entry = %s", got)
	}
}

func TestLLMV1ChatCompletionsHandlerWritesAccessLog(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "access-log@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointAccessLogAccumulator.mu.Lock()
	savedPending := globalLLMEndpointAccessLogAccumulator.pending
	globalLLMEndpointAccessLogAccumulator.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	defer func() {
		globalLLMEndpointAccessLogAccumulator.mu.Lock()
		globalLLMEndpointAccessLogAccumulator.pending = savedPending
		globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	}()
	invalidateLLMRuntimeCaches(system)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: "access-log@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 6, "total_tokens": 18},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello access log"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	rec := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	logs, err := currentLLMEndpointAccessLogs(ctx, system)
	if err != nil {
		t.Fatalf("currentLLMEndpointAccessLogs: %v", err)
	}
	if logs.TotalRequests != 1 {
		t.Fatalf("total requests = %d, want 1", logs.TotalRequests)
	}
	if len(logs.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(logs.Entries))
	}
	entry := logs.Entries[0]
	if entry.ClientIP != "203.0.113.5" {
		t.Fatalf("client_ip = %q, want 203.0.113.5", entry.ClientIP)
	}
	if entry.Email != "access-log@example.com" {
		t.Fatalf("email = %q", entry.Email)
	}
	if entry.RequestedModel != "auto" || entry.AuthorizedModel != "auto" {
		t.Fatalf("unexpected models: requested=%q authorized=%q", entry.RequestedModel, entry.AuthorizedModel)
	}
	if entry.ProviderID != "provider-a" {
		t.Fatalf("provider_id = %q, want provider-a", entry.ProviderID)
	}
	if entry.TotalTokens != 18 || entry.InputTokens != 12 || entry.OutputTokens != 6 {
		t.Fatalf("unexpected token usage: %+v", entry)
	}
	if entry.StatusCode != http.StatusOK {
		t.Fatalf("status_code = %d, want %d", entry.StatusCode, http.StatusOK)
	}
	if entry.RequestBody == "" {
		t.Fatal("expected request body to be logged")
	}
}

func TestLLMV1ResponsesHandlerWritesAccessLog(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken := issueViewerTokenForTenant(t, identity, "tenant_a", "responses-log@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointAccessLogAccumulator.mu.Lock()
	savedPending := globalLLMEndpointAccessLogAccumulator.pending
	globalLLMEndpointAccessLogAccumulator.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	defer func() {
		globalLLMEndpointAccessLogAccumulator.mu.Lock()
		globalLLMEndpointAccessLogAccumulator.pending = savedPending
		globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	}()
	invalidateLLMRuntimeCaches(system)

	tenantSystem := scopedSystemSettingsForTenant("tenant_a", system)
	if err := llmservice.SaveRegistry(ctx, tenantSystem, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: "responses-log@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "test-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 4, "total_tokens": 11},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, tenantSystem, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "input": "hello responses access log"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec := httptest.NewRecorder()
	LLMV1ResponsesHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	logs, err := currentLLMEndpointAccessLogs(ctx, tenantSystem)
	if err != nil {
		t.Fatalf("currentLLMEndpointAccessLogs: %v", err)
	}
	if logs.TotalRequests != 1 || len(logs.Entries) != 1 {
		t.Fatalf("logs = total:%d entries:%d", logs.TotalRequests, len(logs.Entries))
	}
	entry := logs.Entries[0]
	if entry.ClientIP != "203.0.113.9" || entry.Email != "responses-log@example.com" {
		t.Fatalf("unexpected actor log: %+v", entry)
	}
	if entry.RequestedModel != "auto" || entry.AuthorizedModel != "auto" || entry.ProviderID != "provider-a" {
		t.Fatalf("unexpected model/provider log: %+v", entry)
	}
	if entry.TotalTokens != 11 || entry.InputTokens != 7 || entry.OutputTokens != 4 {
		t.Fatalf("unexpected token usage: %+v", entry)
	}
	if entry.Metadata["wire_api"] != "responses" {
		t.Fatalf("metadata wire_api = %#v, want responses", entry.Metadata["wire_api"])
	}
}

func TestLLMV1ChatCompletionsHandlerWritesOfficialFailureDiagnosticsToAccessLog(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "official-log@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointAccessLogAccumulator.mu.Lock()
	savedPending := globalLLMEndpointAccessLogAccumulator.pending
	globalLLMEndpointAccessLogAccumulator.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	defer func() {
		globalLLMEndpointAccessLogAccumulator.mu.Lock()
		globalLLMEndpointAccessLogAccumulator.pending = savedPending
		globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	}()
	invalidateLLMRuntimeCaches(system)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           llmservice.MaClawOfficialServiceGroupID,
			Name:         "MaClaw Official",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: "official-log@example.com", ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID}}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": map[string]any{"message": "deepseek timeout"}})
	}))
	defer hubCenter.Close()
	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: hubCenter.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello access log"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	logs, err := currentLLMEndpointAccessLogs(ctx, system)
	if err != nil {
		t.Fatalf("currentLLMEndpointAccessLogs: %v", err)
	}
	if logs.TotalRequests != 1 || len(logs.Entries) != 1 {
		t.Fatalf("logs = total:%d entries:%d", logs.TotalRequests, len(logs.Entries))
	}
	entry := logs.Entries[0]
	if entry.ProviderID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("provider_id = %q, want %q", entry.ProviderID, llmservice.MaClawOfficialProviderID)
	}
	if entry.StatusCode != http.StatusGatewayTimeout || entry.ErrorCode != "LLM_OFFICIAL_UNAVAILABLE" {
		t.Fatalf("unexpected status/error log: status=%d error=%q", entry.StatusCode, entry.ErrorCode)
	}
	parsed, err := url.Parse(hubCenter.URL)
	if err != nil {
		t.Fatalf("parse hubcenter URL: %v", err)
	}
	if entry.Metadata["failure_stage"] != "upstream_provider" {
		t.Fatalf("metadata failure_stage = %#v, want upstream_provider", entry.Metadata["failure_stage"])
	}
	if entry.Metadata["upstream_status"] != float64(http.StatusGatewayTimeout) && entry.Metadata["upstream_status"] != http.StatusGatewayTimeout {
		t.Fatalf("metadata upstream_status = %#v, want 504", entry.Metadata["upstream_status"])
	}
	if entry.Metadata["upstream_host"] != parsed.Host {
		t.Fatalf("metadata upstream_host = %#v, want %q", entry.Metadata["upstream_host"], parsed.Host)
	}
	if entry.Metadata["upstream_api_url"] != hubCenter.URL {
		t.Fatalf("metadata upstream_api_url = %#v, want %q", entry.Metadata["upstream_api_url"], hubCenter.URL)
	}
	if entry.Metadata["request_id"] == "" {
		t.Fatalf("metadata missing request_id: %#v", entry.Metadata)
	}
}

func TestGetLLMEndpointAccessLogsHandlerSupportsFiltering(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointAccessLogAccumulator.mu.Lock()
	savedPending := globalLLMEndpointAccessLogAccumulator.pending
	globalLLMEndpointAccessLogAccumulator.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	defer func() {
		globalLLMEndpointAccessLogAccumulator.mu.Lock()
		globalLLMEndpointAccessLogAccumulator.pending = savedPending
		globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	}()

	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{Email: "a@example.com", ClientIP: "10.0.0.1", ProviderID: "provider-a", StatusCode: http.StatusOK, RequestBody: `{"model":"auto"}`, Metadata: map[string]any{"upstream_host": "api.provider-a.example"}})
	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{Email: "b@example.com", ClientIP: "10.0.0.2", ProviderID: "provider-b", StatusCode: http.StatusOK, RequestBody: `{"model":"auto"}`, Metadata: map[string]any{"upstream_host": "api.provider-b.example"}})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/access-logs?limit=10&provider=provider-a&upstream_host=provider-a.example&client_ip=10.0.0.1&email=a@example.com", nil)
	rec := httptest.NewRecorder()
	GetLLMEndpointAccessLogsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp llmEndpointAccessLogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Total != 1 || len(resp.Logs) != 1 {
		t.Fatalf("unexpected filtered result: total=%d logs=%d", resp.Total, len(resp.Logs))
	}
	if resp.Logs[0].ProviderID != "provider-a" || resp.Logs[0].ClientIP != "10.0.0.1" || resp.Logs[0].Email != "a@example.com" {
		t.Fatalf("unexpected log entry: %+v", resp.Logs[0])
	}
}

func TestGetLLMEndpointAccessLogsHandlerSupportsCachedOnlyFiltering(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointAccessLogAccumulator.mu.Lock()
	savedPending := globalLLMEndpointAccessLogAccumulator.pending
	globalLLMEndpointAccessLogAccumulator.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	defer func() {
		globalLLMEndpointAccessLogAccumulator.mu.Lock()
		globalLLMEndpointAccessLogAccumulator.pending = savedPending
		globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	}()

	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{Email: "a@example.com", ClientIP: "10.0.0.1", ProviderID: "provider-a", StatusCode: http.StatusOK, CachedInputTokens: 128, RequestBody: `{"model":"auto"}`})
	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{Email: "b@example.com", ClientIP: "10.0.0.2", ProviderID: "provider-a", StatusCode: http.StatusOK, RequestBody: `{"model":"auto"}`})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/access-logs?limit=10&provider=provider-a&cached_only=1", nil)
	rec := httptest.NewRecorder()
	GetLLMEndpointAccessLogsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp llmEndpointAccessLogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Total != 1 || len(resp.Logs) != 1 {
		t.Fatalf("unexpected cached_only result: total=%d logs=%d", resp.Total, len(resp.Logs))
	}
	if resp.Logs[0].CachedInputTokens != 128 {
		t.Fatalf("unexpected cached log: %+v", resp.Logs[0])
	}
}

func TestGetLLMEndpointAccessLogsHandlerSupportsTimeRangeFiltering(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointAccessLogAccumulator.mu.Lock()
	savedPending := globalLLMEndpointAccessLogAccumulator.pending
	globalLLMEndpointAccessLogAccumulator.pending = map[store.SystemSettingsRepository]*llmEndpointAccessLogStore{}
	globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	defer func() {
		globalLLMEndpointAccessLogAccumulator.mu.Lock()
		globalLLMEndpointAccessLogAccumulator.pending = savedPending
		globalLLMEndpointAccessLogAccumulator.mu.Unlock()
	}()

	now := time.Now().UTC()
	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{Email: "old@example.com", ClientIP: "10.0.0.1", ProviderID: "provider-a", StatusCode: http.StatusOK, CreatedAt: now.Add(-2 * time.Hour)})
	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{Email: "new@example.com", ClientIP: "10.0.0.2", ProviderID: "provider-a", StatusCode: http.StatusOK, CreatedAt: now.Add(-10 * time.Minute)})

	startAt := now.Add(-30 * time.Minute).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/access-logs?limit=10&start_at="+url.QueryEscape(startAt), nil)
	rec := httptest.NewRecorder()
	GetLLMEndpointAccessLogsHandler(system).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp llmEndpointAccessLogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Total != 1 || len(resp.Logs) != 1 || resp.Logs[0].Email != "new@example.com" {
		t.Fatalf("unexpected time filtered result: %+v", resp)
	}
}
