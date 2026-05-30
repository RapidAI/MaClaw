package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
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
		AuxiliaryLLM: corelib.AuxiliaryLLMConfig{URL: "https://aux.example/v1", Key: "aux-key", Model: "aux-model"},
		ModelRoutes:  map[string]corelib.ModelRouteConfig{"intent": {Model: "intent-model", Key: "intent-key"}},
	})

	if cfg.MaclawLLMUrl == "" || cfg.MaclawLLMKey == "" || cfg.MaclawLLMModel == "" {
		t.Fatalf("flat LLM fields should remain available: %#v", cfg)
	}
	if cfg.MaclawLLMProtocol != "" || cfg.MaclawLLMContextLength != 0 || cfg.MaclawLLMTimeoutSec != 0 || cfg.MaclawLLMCurrentProvider != "" || len(cfg.MaclawLLMProviders) != 0 || cfg.AuxiliaryLLM.IsConfigured() || len(cfg.ModelRoutes) != 0 {
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
	for _, want := range []string{`"memory_auto_compress":false`, `"memory_max_backups":0`, `"knowledge_skill_token_budget":0`} {
		if !strings.Contains(text, want) {
			t.Fatalf("get zero response missing %s body = %s", want, text)
		}
	}
}

func TestUserVisibleConfigCandidateStripsStoredComplexLLMFields(t *testing.T) {
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
	if candidate.MaclawLLMProtocol != "" || candidate.MaclawLLMContextLength != 0 || candidate.MaclawLLMTimeoutSec != 0 || candidate.MaclawLLMCurrentProvider != "" || len(candidate.MaclawLLMProviders) != 0 || candidate.AuxiliaryLLM.IsConfigured() || len(candidate.ModelRoutes) != 0 {
		t.Fatalf("candidate should clear stored complex user LLM fields: %#v", candidate)
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
		{Key: "auxiliary_llm"},
		{Key: "model_routes"},
	})

	for _, def := range defs {
		if _, hidden := userComplexConfigKeys[def.Key]; hidden {
			t.Fatalf("schema should not expose complex user LLM field %q", def.Key)
		}
	}
	if len(defs) != 1 || defs[0].Key != "maclaw_llm_url" {
		t.Fatalf("schema should keep simple LLM fields only, got %#v", defs)
	}
}
