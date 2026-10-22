package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type memberUpstreamSettings struct {
	values map[string]string
}

func (m *memberUpstreamSettings) Set(_ context.Context, key, valueJSON string) error {
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[key] = valueJSON
	return nil
}

func (m *memberUpstreamSettings) Get(_ context.Context, key string) (string, error) {
	if m.values == nil {
		return "", nil
	}
	return m.values[key], nil
}

func TestToCoreAndRegistryResponseCarryUpstreamOverrides(t *testing.T) {
	provider := &im.LLMProvider{
		ID:                "p",
		APIURL:            "https://example.com/v1",
		APIKey:            "secret",
		Model:             "m",
		ExtraBody:         map[string]any{"reasoning_effort": "none"},
		ExtraBodyPriority: "downstream",
		DropFields:        []string{"thinking"},
	}
	cfg := toCoreLLMEndpointProvider(provider).MaclawLLMConfig()
	if cfg.MemberExtraBody["reasoning_effort"] != "none" || cfg.MemberExtraBodyPriority != "downstream" || len(cfg.MemberDropFields) != 1 {
		t.Fatalf("cfg extra %#v priority %q drop %#v", cfg.MemberExtraBody, cfg.MemberExtraBodyPriority, cfg.MemberDropFields)
	}

	resp := registryResponse(httptest.NewRequest(http.MethodGet, "http://hub.example/", nil), &im.LLMProviderRegistry{
		Providers: []im.LLMProvider{*provider},
	}, &llmservice.Registry{}, nil)
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"reasoning_effort":"none"`) || !strings.Contains(text, `"extra_body_priority":"downstream"`) || !strings.Contains(text, `"thinking"`) {
		t.Fatalf("response = %s", text)
	}
	if strings.Contains(text, "secret") {
		t.Fatalf("response leaked the key: %s", text)
	}
}

func TestUpsertProviderPlanKeepsOmittedUpstreamOverrides(t *testing.T) {
	settings := &memberUpstreamSettings{}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/config-agent/execute", nil)
	_, err := upsertProviderFromPlanArgs(req, settings, map[string]any{
		"id": "p", "name": "P", "api_url": "https://example.com/v1", "api_key": "secret", "model": "m",
		"extra_body":          map[string]any{"reasoning_effort": "none"},
		"extra_body_priority": "downstream",
		"drop_fields":         "thinking, thinking",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = upsertProviderFromPlanArgs(req, settings, map[string]any{
		"id": "p", "name": "P", "api_url": "https://example.com/v1", "api_key": "secret", "model": "m2",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := im.LoadLLMProviderRegistry(req.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	got := reg.Providers[0]
	if got.Model != "m2" || got.ExtraBody["reasoning_effort"] != "none" || got.ExtraBodyPriority != "downstream" || len(got.DropFields) != 1 {
		t.Fatalf("kept = model %q extra %#v priority %q drop %#v", got.Model, got.ExtraBody, got.ExtraBodyPriority, got.DropFields)
	}
	view := providerUpstreamOverrideView(got)
	if _, ok := view["api_key"]; ok {
		t.Fatalf("view included a secret: %#v", view)
	}

	_, err = upsertProviderFromPlanArgs(req, settings, map[string]any{
		"id": "p", "name": "P", "api_url": "https://example.com/v1", "api_key": "secret", "model": "m2",
		"extra_body": map[string]any{}, "drop_fields": []any{}, "extra_body_priority": "member",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err = im.LoadLLMProviderRegistry(req.Context(), settings)
	if err != nil {
		t.Fatal(err)
	}
	got = reg.Providers[0]
	if got.ExtraBody != nil || got.DropFields != nil || got.ExtraBodyPriority != "" {
		t.Fatalf("clear failed: %#v %q %#v", got.ExtraBody, got.ExtraBodyPriority, got.DropFields)
	}
	_, err = upsertProviderFromPlanArgs(req, settings, map[string]any{
		"id": "p", "name": "P", "api_url": "https://example.com/v1", "api_key": "secret", "model": "m2",
		"extra_body": map[string]any{"messages": []any{}},
	})
	if err == nil {
		t.Fatal("expected protected extra_body to be rejected")
	}
}

func TestConfigAgentToolCatalogDocumentsUpstreamOverrides(t *testing.T) {
	raw := configAgentToolCatalogJSON(nil)
	for _, want := range []string{"extra_body", "extra_body_priority", "drop_fields", "Omit any of the three"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("catalog missing %s", want)
		}
	}
}

func TestLLMProviderTestRejectsInvalidUpstreamOverrides(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/test", strings.NewReader(`{"api_url":"https://example.com/v1","api_key":"k","model":"m","extra_body":{"model":"hijack"}}`))
	rec := httptest.NewRecorder()
	TestLLMProviderHandler(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "extra_body cannot set model") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

var _ store.SystemSettingsRepository = (*memberUpstreamSettings)(nil)
