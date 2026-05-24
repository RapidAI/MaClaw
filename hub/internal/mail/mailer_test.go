package mail

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type memorySettings map[string]string

func (m memorySettings) Set(ctx context.Context, key, valueJSON string) error {
	m[key] = valueJSON
	return nil
}

func (m memorySettings) Get(ctx context.Context, key string) (string, error) {
	return m[key], nil
}

func TestConfigForSendUsesTenantSenderName(t *testing.T) {
	settings := memorySettings{
		systemKeyMailConfig: `{"enabled":true,"smtp_host":"smtp.example.com","smtp_port":587,"smtp_username":"user","smtp_password":"pass","from_name":"Global Hub","from_email":"noreply@example.com"}`,
		"tenant:tenant_acme:" + systemKeyTenantMailSenderName: `{"from_name":"Acme Mail"}`,
		systemKeyTenantMailSenderName:                         `{"from_name":"Default Tenant Mail"}`,
	}
	svc := New(config.Config{}, settings)

	cfg, err := svc.configForSend(store.WithTenant(context.Background(), "tenant_acme"))
	if err != nil {
		t.Fatalf("tenant send config: %v", err)
	}
	if cfg.FromName != "Acme Mail" {
		t.Fatalf("tenant from name = %q, want Acme Mail", cfg.FromName)
	}

	globalCfg, err := svc.configForSend(context.Background())
	if err != nil {
		t.Fatalf("global send config: %v", err)
	}
	if globalCfg.FromName != "Global Hub" {
		t.Fatalf("global from name = %q, want Global Hub", globalCfg.FromName)
	}

	defaultTenantCfg, err := svc.configForSend(store.WithTenant(context.Background(), store.DefaultTenantID))
	if err != nil {
		t.Fatalf("default tenant send config: %v", err)
	}
	if defaultTenantCfg.FromName != "Default Tenant Mail" {
		t.Fatalf("default tenant from name = %q, want Default Tenant Mail", defaultTenantCfg.FromName)
	}
}
