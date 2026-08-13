package mail

import (
	"context"
	"errors"
	"strings"
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
		"tenant:tenant_acme:" + TenantSenderNameSettingKey: `{"from_name":"Acme Mail"}`,
		TenantSenderNameSettingKey:                         `{"from_name":"Default Tenant Mail"}`,
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

func TestConfigForSendIgnoresInvalidTenantSenderName(t *testing.T) {
	settings := memorySettings{
		systemKeyMailConfig: `{"enabled":true,"smtp_host":"smtp.example.com","smtp_port":587,"smtp_username":"user","smtp_password":"pass","from_name":"Global Hub","from_email":"noreply@example.com"}`,
		"tenant:tenant_acme:" + TenantSenderNameSettingKey: `{bad-json`,
	}
	svc := New(config.Config{}, settings)

	cfg, err := svc.configForSend(store.WithTenant(context.Background(), "tenant_acme"))
	if err != nil {
		t.Fatalf("tenant send config: %v", err)
	}
	if cfg.FromName != "Global Hub" {
		t.Fatalf("tenant from name = %q, want Global Hub fallback", cfg.FromName)
	}
}

func TestConfigForSendTruncatesLegacyLongTenantSenderName(t *testing.T) {
	settings := memorySettings{
		systemKeyMailConfig: `{"enabled":true,"smtp_host":"smtp.example.com","smtp_port":587,"smtp_username":"user","smtp_password":"pass","from_name":"Global Hub","from_email":"noreply@example.com"}`,
		"tenant:tenant_acme:" + TenantSenderNameSettingKey: `{"from_name":"` + strings.Repeat("界", TenantSenderNameMaxRunes+1) + `"}`,
	}
	svc := New(config.Config{}, settings)

	cfg, err := svc.configForSend(store.WithTenant(context.Background(), "tenant_acme"))
	if err != nil {
		t.Fatalf("tenant send config: %v", err)
	}
	if len([]rune(cfg.FromName)) != TenantSenderNameMaxRunes {
		t.Fatalf("tenant from name length = %d, want %d", len([]rune(cfg.FromName)), TenantSenderNameMaxRunes)
	}
}

type tenantScopedSettings struct {
	tenantID string
	base     store.SystemSettingsRepository
}

func (s tenantScopedSettings) Set(ctx context.Context, key, valueJSON string) error {
	return s.base.Set(ctx, "tenant:"+s.tenantID+":"+key, valueJSON)
}

func (s tenantScopedSettings) Get(ctx context.Context, key string) (string, error) {
	return s.base.Get(ctx, "tenant:"+s.tenantID+":"+key)
}

func (s tenantScopedSettings) GlobalSystemSettings() store.SystemSettingsRepository {
	return s.base
}

func TestCurrentConfigUsesGlobalCredentialsForTenantScopedSettings(t *testing.T) {
	base := memorySettings{
		systemKeyMailConfig: `{"enabled":true,"smtp_host":"smtp.example.com","smtp_port":587,"smtp_username":"user","smtp_password":"pass","from_name":"Global Hub","from_email":"noreply@example.com"}`,
		"tenant:tenant_acme:" + TenantSenderNameSettingKey: `{"from_name":"Acme Mail"}`,
	}
	svc := New(config.Config{}, tenantScopedSettings{tenantID: "tenant_acme", base: base})

	cfg, err := svc.CurrentConfig(store.WithTenant(context.Background(), "tenant_acme"))
	if err != nil {
		t.Fatalf("CurrentConfig: %v", err)
	}
	if !cfg.Enabled || cfg.SMTPHost != "smtp.example.com" || cfg.FromEmail != "noreply@example.com" {
		t.Fatalf("tenant send config = %#v", cfg)
	}
	// The actual delivery path must combine global credentials with this
	// tenant's branding without asking the scoped wrapper to scope the key a
	// second time.
	cfg, err = svc.configForSend(store.WithTenant(context.Background(), "tenant_acme"))
	if err != nil {
		t.Fatalf("configForSend: %v", err)
	}
	if cfg.FromName != "Acme Mail" {
		t.Fatalf("tenant sender name = %q, want Acme Mail", cfg.FromName)
	}
}

func TestSendReturnsNotConfiguredErrorForIncompleteDeliveryConfig(t *testing.T) {
	for _, rawConfig := range []string{
		``,
		`{"enabled":true}`,
		`{"enabled":true,"smtp_host":"smtp.example.com"}`,
	} {
		t.Run(rawConfig, func(t *testing.T) {
			settings := memorySettings{}
			if rawConfig != "" {
				settings[systemKeyMailConfig] = rawConfig
			}
			svc := New(config.Config{}, settings)
			if err := svc.Send(context.Background(), []string{"user@example.com"}, "Login code", "123456"); !errors.Is(err, ErrDeliveryNotConfigured) {
				t.Fatalf("Send error = %v, want ErrDeliveryNotConfigured", err)
			}
		})
	}
}
