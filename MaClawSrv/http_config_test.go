package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestUserConfigStripsComplexLLMFields(t *testing.T) {
	cfg := stripUserComplexConfig(corelib.AppConfig{
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMKey:             "key",
		MaclawLLMModel:           "model",
		MaclawLLMProtocol:        "anthropic",
		MaclawLLMContextLength:   200000,
		MaclawLLMTimeoutSec:      300,
		MaclawLLMCurrentProvider: "hub-llm",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:  "hub-llm",
			URL:   "https://advanced.example/v1",
			Key:   "advanced-key",
			Model: "advanced-model",
		}},
		LLMPromptCache: corelib.LLMPromptCacheConfig{Enabled: true, TTLSeconds: 777},
		AuxiliaryLLM:   corelib.AuxiliaryLLMConfig{URL: "https://aux.example/v1", Key: "aux-key", Model: "aux-model"},
		ModelRoutes:    map[string]corelib.ModelRouteConfig{"intent": {Model: "intent-model", Key: "intent-key"}},
	})

	if cfg.MaclawLLMUrl == "" || cfg.MaclawLLMKey == "" || cfg.MaclawLLMModel == "" {
		t.Fatalf("flat LLM fields should remain available: %#v", cfg)
	}
	if cfg.MaclawLLMProtocol != "" || cfg.MaclawLLMContextLength != 0 || cfg.MaclawLLMTimeoutSec != 0 || cfg.MaclawLLMCurrentProvider != "" || len(cfg.MaclawLLMProviders) != 0 || cfg.LLMPromptCache.Enabled || cfg.LLMPromptCache.TTLSeconds != 0 || cfg.AuxiliaryLLM.IsConfigured() || len(cfg.ModelRoutes) != 0 {
		t.Fatalf("complex user LLM fields should be cleared: %#v", cfg)
	}
}

func TestUserConfigMemoryFieldsRoundTripThroughUserAPI(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "llm-key", MaclawLLMModel: "llm-model", LLMPromptCache: corelib.LLMPromptCacheConfig{Enabled: true, TTLSeconds: 777}}); err != nil {
		t.Fatalf("seed LLM config: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"app_config":{"memory_auto_compress":true,"memory_max_backups":10,"knowledge_skill_token_budget":12000}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update config status = %d body = %s", w.Code, w.Body.String())
	}
	var updated agentservice.UserConfig
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update config: %v", err)
	}
	if !updated.AppConfig.MemoryAutoCompress || updated.AppConfig.MemoryMaxBackups != 10 || updated.AppConfig.KnowledgeSkillTokenBudget != 12000 {
		t.Fatalf("update response lost memory fields: %#v", updated.AppConfig)
	}
	raw, err := svc.GetRawUserConfig(context.Background(), principal)
	if err != nil {
		t.Fatalf("GetRawUserConfig after visible save: %v", err)
	}
	if raw.AppConfig.MaclawLLMUrl != "https://llm.example/v1" || raw.AppConfig.MaclawLLMKey != "llm-key" || raw.AppConfig.MaclawLLMModel != "llm-model" || !raw.AppConfig.LLMPromptCache.Enabled || raw.AppConfig.LLMPromptCache.TTLSeconds != 777 {
		t.Fatalf("visible save should preserve existing LLM fields, got %#v", raw.AppConfig)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get config status = %d body = %s", w.Code, w.Body.String())
	}
	var got agentservice.UserConfig
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode get config: %v", err)
	}
	if !got.AppConfig.MemoryAutoCompress || got.AppConfig.MemoryMaxBackups != 10 || got.AppConfig.KnowledgeSkillTokenBudget != 12000 {
		t.Fatalf("get response lost memory fields: %#v", got.AppConfig)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"app_config":{"memory_auto_compress":false,"memory_max_backups":0,"knowledge_skill_token_budget":0}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update zero config status = %d body = %s", w.Code, w.Body.String())
	}
	text := w.Body.String()
	for _, want := range []string{`"memory_auto_compress":false`, `"memory_max_backups":0`, `"knowledge_skill_token_budget":0`} {
		if !strings.Contains(text, want) {
			t.Fatalf("update zero response missing %s body = %s", want, text)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get zero config status = %d body = %s", w.Code, w.Body.String())
	}
	text = w.Body.String()
	for _, hidden := range []string{"llm_prompt_cache", "mcp_servers", "local_mcp_servers", "ssh_hosts", "claude", "codex"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("user visible config should not expose hidden config %s: %s", hidden, text)
		}
	}
	for _, want := range []string{`"memory_auto_compress":false`, `"memory_max_backups":0`, `"knowledge_skill_token_budget":0`} {
		if !strings.Contains(text, want) {
			t.Fatalf("get zero response missing %s body = %s", want, text)
		}
	}
}

func TestUserConfigLLMPromptCacheHiddenAndPreserved(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "llm-key", MaclawLLMModel: "llm-model", LLMPromptCache: corelib.LLMPromptCacheConfig{Enabled: true, TTLSeconds: 777}}); err != nil {
		t.Fatalf("seed LLM cache config: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"app_config":{"llm_prompt_cache":{"enabled":false,"ttl_seconds":900}}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("typed object save status = %d body = %s", w.Code, w.Body.String())
	}
	var updated agentservice.UserConfig
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update config: %v", err)
	}
	raw, err := svc.GetRawUserConfig(context.Background(), principal)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	if !raw.AppConfig.LLMPromptCache.Enabled || raw.AppConfig.LLMPromptCache.TTLSeconds != 777 {
		t.Fatalf("user visible save should preserve hidden llm_prompt_cache, got %#v", raw.AppConfig.LLMPromptCache)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"app_config":{"llm_prompt_cache":{"enabled":"false"}}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "llm_prompt_cache.enabled") {
		t.Fatalf("bad typed object status/body = %d %s", w.Code, w.Body.String())
	}
}

func TestUserMemoryManagementAPI(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	otherUser, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Other User"})
	if err != nil {
		t.Fatalf("CreateUser other: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	otherToken, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: otherUser.ID})
	if err != nil {
		t.Fatalf("Issue other token: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/memory", strings.NewReader(`{"content":"User prefers concise answers","category":"preference","tags":["style","concise"]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create memory status = %d body = %s", w.Code, w.Body.String())
	}
	var created struct {
		ID       string   `json:"id"`
		Content  string   `json:"content"`
		Category string   `json:"category"`
		Tags     []string `json:"tags"`
		ReadOnly bool     `json:"read_only"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created memory: %v", err)
	}
	if created.ID == "" || created.Category != "preference" || created.Content != "User prefers concise answers" || created.ReadOnly {
		t.Fatalf("unexpected created memory: %#v", created)
	}
	if len(created.Tags) != 2 || created.Tags[0] != "style" || created.Tags[1] != "concise" {
		t.Fatalf("unexpected created memory tags: %#v", created.Tags)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/memory", strings.NewReader(`{"content":"User project alpha uses PostgreSQL","category":"project_knowledge","tags":["alpha"]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create second memory status = %d body = %s", w.Code, w.Body.String())
	}
	legacyMemoryDir := filepath.Join(dataRoot, "tenants", tenant.ID, "users", user.ID, "data")
	legacyStore, err := memory.OpenDataDirStore(legacyMemoryDir, memory.StoreModeAuto, filepath.Join(legacyMemoryDir, "agent_memory.json"))
	if err != nil {
		t.Fatalf("OpenDataDirStore legacy: %v", err)
	}
	legacyEntry := memory.Entry{ID: "legacy-shared-memory", Content: "Legacy shared memory is visible but read only", Category: memory.CategoryUserFact, SourceType: "legacy"}
	if err := legacyStore.UpsertEntriesByID([]memory.Entry{legacyEntry}); err != nil {
		legacyStore.Stop()
		t.Fatalf("seed legacy memory: %v", err)
	}
	legacyStore.Stop()

	req = httptest.NewRequest(http.MethodPost, "/api/v1/memory", strings.NewReader(`{"content":"I am the assistant","category":"self_identity"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "protected memory") {
		t.Fatalf("protected memory create status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/memory", strings.NewReader(`{"content":"`+strings.Repeat("x", 20001)+`","category":"user_fact"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "too long") {
		t.Fatalf("oversized memory create status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/memory", strings.NewReader(`{"content":"bad tags","category":"user_fact","tags":["`+strings.Repeat("t", 81)+`"]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "tag is too long") {
		t.Fatalf("long tag memory create status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/memory?q=concise&category=preference", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "User prefers concise answers") {
		t.Fatalf("list memory status = %d body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(body, "owner_id") || strings.Contains(body, "content_hash") || strings.Contains(body, "embedding") {
		t.Fatalf("memory API leaked internal fields: %s", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/memory?q=Legacy%20shared", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Legacy shared memory") || !strings.Contains(w.Body.String(), `"read_only":true`) {
		t.Fatalf("legacy shared memory should be visible read-only status = %d body = %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPut, "/api/v1/memory/legacy-shared-memory", strings.NewReader(`{"content":"mutated shared memory","category":"user_fact"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("legacy shared memory update status = %d body = %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/memory/legacy-shared-memory", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("legacy shared memory delete status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/memory?q=concise", nil)
	req.Header.Set("Authorization", "Bearer "+otherToken)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "User prefers concise answers") {
		t.Fatalf("other user should not see memory status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/memory/"+created.ID, strings.NewReader(`{"content":"other overwrite","category":"preference"}`))
	req.Header.Set("Authorization", "Bearer "+otherToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("other user update status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/memory/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+otherToken)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("other user delete status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/memory?limit=1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("paginated memory status = %d body = %s", w.Code, w.Body.String())
	}
	var page struct {
		Items          []struct{ ID string } `json:"items"`
		Total          int                   `json:"total"`
		Limit          int                   `json:"limit"`
		HasMore        bool                  `json:"has_more"`
		NextOffset     int                   `json:"next_offset"`
		CategoryCounts map[string]int        `json:"category_counts"`
	}
	if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
		t.Fatalf("decode paginated memory: %v", err)
	}
	if len(page.Items) != 1 || page.Total != 3 || page.Limit != 1 || !page.HasMore || page.NextOffset != 1 || page.CategoryCounts["preference"] != 1 || page.CategoryCounts["project_knowledge"] != 1 || page.CategoryCounts["user_fact"] != 1 {
		t.Fatalf("unexpected paginated memory response: %#v", page)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/memory?limit=500", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("large limit memory status = %d body = %s", w.Code, w.Body.String())
	}
	page = struct {
		Items          []struct{ ID string } `json:"items"`
		Total          int                   `json:"total"`
		Limit          int                   `json:"limit"`
		HasMore        bool                  `json:"has_more"`
		NextOffset     int                   `json:"next_offset"`
		CategoryCounts map[string]int        `json:"category_counts"`
	}{}
	if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
		t.Fatalf("decode large limit memory: %v", err)
	}
	if page.Limit != 200 {
		t.Fatalf("memory limit should cap at 200, got %#v", page)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/memory/"+created.ID, strings.NewReader(`{"content":"User prefers short direct answers","category":"preference","tags":["style"]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "short direct") {
		t.Fatalf("update memory status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/memory/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete memory status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestUserVisibleConfigUpdateDoesNotAcceptNewHiddenComplexLLMFields(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	next, err := server.userVisibleConfigUpdate(context.Background(), principal, corelib.AppConfig{
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMKey:             "secret",
		MaclawLLMModel:           "model",
		MaclawLLMProtocol:        "anthropic",
		MaclawLLMContextLength:   200000,
		MaclawLLMTimeoutSec:      300,
		MaclawLLMCurrentProvider: "advanced",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "advanced", URL: "https://advanced.example/v1", Key: "hidden", Model: "hidden-model"}},
		AuxiliaryLLM:             corelib.AuxiliaryLLMConfig{URL: "https://aux.example/v1", Key: "aux", Model: "aux-model"},
		ModelRoutes:              map[string]corelib.ModelRouteConfig{"intent": {Model: "intent-model", Key: "intent-key"}},
	})
	if err != nil {
		t.Fatalf("userVisibleConfigUpdate: %v", err)
	}
	if next.MaclawLLMUrl == "" || next.MaclawLLMKey == "" || next.MaclawLLMModel == "" {
		t.Fatalf("visible flat LLM fields should remain: %#v", next)
	}
	if next.MaclawLLMProtocol != "" || next.MaclawLLMContextLength != 0 || next.MaclawLLMTimeoutSec != 0 || next.MaclawLLMCurrentProvider != "" || len(next.MaclawLLMProviders) != 0 || next.AuxiliaryLLM.IsConfigured() || len(next.ModelRoutes) != 0 {
		t.Fatalf("visible update should not accept new hidden complex LLM fields: %#v", next)
	}
}

func TestUserVisibleConfigCandidatePreservesHiddenComplexLLMFields(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMKey:             "key",
		MaclawLLMModel:           "model",
		MaclawLLMProtocol:        "anthropic",
		MaclawLLMContextLength:   200000,
		MaclawLLMTimeoutSec:      300,
		MaclawLLMCurrentProvider: "advanced",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "advanced", URL: "https://advanced.example/v1", Key: "advanced-key", Model: "advanced-model"}},
		AuxiliaryLLM:             corelib.AuxiliaryLLMConfig{URL: "https://aux.example/v1", Key: "aux-key", Model: "aux-model"},
		ModelRoutes:              map[string]corelib.ModelRouteConfig{"intent": {Model: "intent-model", Key: "intent-key"}},
	}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}

	server := NewHTTPServer(svc, "root-admin-secret", nil)
	candidate, err := server.userVisibleConfigCandidate(context.Background(), principal, nil)
	if err != nil {
		t.Fatalf("userVisibleConfigCandidate: %v", err)
	}
	if candidate == nil || candidate.MaclawLLMUrl == "" || candidate.MaclawLLMKey == "" || candidate.MaclawLLMModel == "" {
		t.Fatalf("candidate should preserve flat LLM config: %#v", candidate)
	}
	if candidate.MaclawLLMProtocol != "anthropic" || candidate.MaclawLLMContextLength != 200000 || candidate.MaclawLLMTimeoutSec != 300 || candidate.MaclawLLMCurrentProvider != "advanced" || len(candidate.MaclawLLMProviders) != 1 || !candidate.AuxiliaryLLM.IsConfigured() || len(candidate.ModelRoutes) != 1 {
		t.Fatalf("candidate should preserve hidden complex user LLM fields for validation: %#v", candidate)
	}

	next, err := server.userVisibleConfigUpdate(context.Background(), principal, corelib.AppConfig{MemoryMaxBackups: 7})
	if err != nil {
		t.Fatalf("userVisibleConfigUpdate: %v", err)
	}
	if next.MemoryMaxBackups != 7 {
		t.Fatalf("visible memory edit was not applied: %#v", next)
	}
	if next.MaclawLLMUrl != "https://advanced.example/v1" || next.MaclawLLMKey != "advanced-key" || next.MaclawLLMModel != "advanced-model" {
		t.Fatalf("visible config save should not drop flat LLM fields: %#v", next)
	}
	if next.MaclawLLMProtocol != "anthropic" || next.MaclawLLMCurrentProvider != "advanced" || len(next.MaclawLLMProviders) != 1 || !next.AuxiliaryLLM.IsConfigured() || len(next.ModelRoutes) != 1 {
		t.Fatalf("visible config save should not drop hidden complex LLM fields: %#v", next)
	}

	next, err = server.userVisibleConfigUpdate(context.Background(), principal, corelib.AppConfig{MaclawLLMKey: "********"})
	if err != nil {
		t.Fatalf("userVisibleConfigUpdate alternate mask: %v", err)
	}
	if next.MaclawLLMKey != "advanced-key" {
		t.Fatalf("visible config save should preserve LLM key for alternate mask placeholder: %#v", next)
	}
}

func TestUserConfigResponseProjectsStoredProviderLLMFields(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{
		MaclawLLMUrl:             "https://stale.example.test/v1",
		MaclawLLMKey:             "stale-secret",
		MaclawLLMModel:           "stale-model",
		MaclawLLMCurrentProvider: "hub-llm",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub-llm", URL: "https://hub.example.test/llm", Key: "provider-secret", Model: "auto"}},
	}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get config status = %d body = %s", w.Code, w.Body.String())
	}
	var got agentservice.UserConfig
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode get config: %v", err)
	}
	if got.AppConfig.MaclawLLMUrl != "https://hub.example.test/llm" || got.AppConfig.MaclawLLMKey != "******" || got.AppConfig.MaclawLLMModel != "auto" {
		t.Fatalf("user response should expose effective flat LLM fields, got %#v", got.AppConfig)
	}
	if got.AppConfig.MaclawLLMCurrentProvider != "" || len(got.AppConfig.MaclawLLMProviders) != 0 {
		t.Fatalf("user response should still hide complex provider fields, got %#v", got.AppConfig)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"app_config":{"memory_max_backups":9,"maclaw_llm_key":"******"}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save config status = %d body = %s", w.Code, w.Body.String())
	}
	raw, err := svc.GetRawUserConfig(context.Background(), principal)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	if raw.AppConfig.MaclawLLMUrl != "https://hub.example.test/llm" || raw.AppConfig.MaclawLLMKey != "provider-secret" || raw.AppConfig.MaclawLLMModel != "auto" || raw.AppConfig.MemoryMaxBackups != 9 {
		t.Fatalf("visible save should preserve projected LLM fields, got %#v", raw.AppConfig)
	}
	if raw.AppConfig.MaclawLLMCurrentProvider != "hub-llm" || len(raw.AppConfig.MaclawLLMProviders) != 1 || raw.AppConfig.MaclawLLMProviders[0].Key != "provider-secret" {
		t.Fatalf("visible save should preserve provider LLM fields, got %#v", raw.AppConfig)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/config", strings.NewReader(`{"app_config":{"maclaw_llm_url":"https://custom.example.test/v1","maclaw_llm_key":"custom-secret","maclaw_llm_model":"custom-model"}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save custom LLM config status = %d body = %s", w.Code, w.Body.String())
	}
	raw, err = svc.GetRawUserConfig(context.Background(), principal)
	if err != nil {
		t.Fatalf("GetRawUserConfig after custom LLM save: %v", err)
	}
	if raw.AppConfig.MaclawLLMProviders[0].URL != "https://custom.example.test/v1" || raw.AppConfig.MaclawLLMProviders[0].Key != "custom-secret" || raw.AppConfig.MaclawLLMProviders[0].Model != "custom-model" {
		t.Fatalf("visible LLM edit should update effective provider, got %#v", raw.AppConfig)
	}
}

func TestAdminConfigRoundTripPreservesMaskedProviderLLMSecrets(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{
		MaclawLLMUrl:             "https://stale.example.test/v1",
		MaclawLLMKey:             "stale-secret",
		MaclawLLMModel:           "stale-model",
		MaclawLLMCurrentProvider: "hub-llm",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "hub-llm", URL: "https://hub.example.test/llm", Key: "provider-secret", Model: "auto"}},
	}); err != nil {
		t.Fatalf("UpdateUserConfig seed: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	path := "/api/v1/admin/tenants/" + tenant.ID + "/users/" + user.ID + "/config"

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin get config status = %d body = %s", w.Code, w.Body.String())
	}
	var got agentservice.UserConfig
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode admin config: %v", err)
	}
	if got.AppConfig.MaclawLLMKey != "******" || len(got.AppConfig.MaclawLLMProviders) != 1 || got.AppConfig.MaclawLLMProviders[0].Key != "******" {
		t.Fatalf("admin config response should be masked but keep provider shape, got %#v", got.AppConfig)
	}
	got.AppConfig.MemoryMaxBackups = 11
	body, err := json.Marshal(map[string]corelib.AppConfig{"app_config": got.AppConfig})
	if err != nil {
		t.Fatalf("marshal admin update: %v", err)
	}
	req = httptest.NewRequest(http.MethodPut, path, strings.NewReader(string(body)))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin update config status = %d body = %s", w.Code, w.Body.String())
	}
	raw, err := svc.GetRawUserConfig(context.Background(), principal)
	if err != nil {
		t.Fatalf("GetRawUserConfig: %v", err)
	}
	if raw.AppConfig.MemoryMaxBackups != 11 || raw.AppConfig.MaclawLLMKey != "provider-secret" || raw.AppConfig.MaclawLLMProviders[0].Key != "provider-secret" {
		t.Fatalf("admin masked round trip should preserve provider LLM secrets, got %#v", raw.AppConfig)
	}
}

func TestUserConfigSchemaFiltersComplexLLMFields(t *testing.T) {
	defs := filterUserConfigSchema([]agentservice.ParameterDefinition{
		{Key: "maclaw_llm_url"},
		{Key: "maclaw_llm_protocol"},
		{Key: "maclaw_llm_context_length"},
		{Key: "maclaw_llm_timeout_sec"},
		{Key: "maclaw_llm_current_provider"},
		{Key: "maclaw_llm_providers"},
		{Key: "llm_prompt_cache"},
		{Key: "auxiliary_llm"},
		{Key: "model_routes"},
		{Key: "mcp_servers"},
		{Key: "local_mcp_servers"},
		{Key: "ssh_hosts"},
		{Key: "claude"},
	})

	for _, def := range defs {
		if _, hidden := userHiddenConfigKeys[def.Key]; hidden {
			t.Fatalf("schema should not expose hidden user config field %q", def.Key)
		}
	}
	if len(defs) != 1 || defs[0].Key != "maclaw_llm_url" {
		t.Fatalf("schema should keep simple LLM fields only, got %#v", defs)
	}
}
