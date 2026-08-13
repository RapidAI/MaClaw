package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type hubLLMConfigTestSettings struct {
	values map[string]string
}

func (s hubLLMConfigTestSettings) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

func (s hubLLMConfigTestSettings) Get(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

func TestLoadGlobalHubLLMConfigIgnoresTenantScopedShadow(t *testing.T) {
	global, err := json.Marshal(im.HubLLMConfig{Enabled: true, APIURL: "https://global.example/v1", APIKey: "global-key", Model: "global-model"})
	if err != nil {
		t.Fatalf("marshal global config: %v", err)
	}
	tenant, err := json.Marshal(im.HubLLMConfig{Enabled: false, APIURL: "https://tenant.example/v1", APIKey: "tenant-key", Model: "tenant-model"})
	if err != nil {
		t.Fatalf("marshal tenant config: %v", err)
	}
	settings := hubLLMConfigTestSettings{values: map[string]string{
		"hub_llm_config":                 string(global),
		"tenant:tenant_a:hub_llm_config": string(tenant),
	}}

	cfg := loadGlobalHubLLMConfig(im.WithTenant(context.Background(), "tenant_a"), settings)
	if cfg == nil || cfg.APIURL != "https://global.example/v1" || cfg.Model != "global-model" {
		t.Fatalf("loaded config = %#v", cfg)
	}
}

func TestTenantScopedSystemSettingsExposesGlobalSettings(t *testing.T) {
	base := hubLLMConfigTestSettings{values: map[string]string{"mail_config": "global-smtp"}}
	scoped := scopedSystemSettingsForTenant("tenant_a", base)
	globalProvider, ok := scoped.(interface {
		GlobalSystemSettings() store.SystemSettingsRepository
	})
	if !ok {
		t.Fatal("tenant-scoped settings must expose their global repository")
	}
	value, err := globalProvider.GlobalSystemSettings().Get(context.Background(), "mail_config")
	if err != nil || value != "global-smtp" {
		t.Fatalf("global mail config = %q, %v", value, err)
	}
}
