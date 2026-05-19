package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type tenantScopedSystemSettings struct {
	tenantID string
	base     store.SystemSettingsRepository
}

func scopedSystemSettingsForRequest(r *http.Request, base store.SystemSettingsRepository) store.SystemSettingsRepository {
	if base == nil || r == nil || !isTenantScopedAdminRequest(r) {
		return base
	}
	return scopedSystemSettingsForTenant(RequestTenantID(r), base)
}

func shouldReloadSharedRuntimeForRequest(r *http.Request) bool {
	if r == nil {
		return true
	}
	return !isTenantScopedAdminRequest(r)
}

func scopedSystemSettingsForTenant(tenantID string, base store.SystemSettingsRepository) store.SystemSettingsRepository {
	return ScopedSystemSettingsForTenant(tenantID, base)
}

func ScopedSystemSettingsForTenant(tenantID string, base store.SystemSettingsRepository) store.SystemSettingsRepository {
	tenantID = strings.TrimSpace(tenantID)
	if base == nil || tenantID == "" || tenantID == store.DefaultTenantID {
		return base
	}
	return tenantScopedSystemSettings{tenantID: tenantID, base: base}
}

func (s tenantScopedSystemSettings) Set(ctx context.Context, key, valueJSON string) error {
	return s.base.Set(ctx, s.key(key), valueJSON)
}

func (s tenantScopedSystemSettings) Get(ctx context.Context, key string) (string, error) {
	return s.base.Get(ctx, s.key(key))
}

func (s tenantScopedSystemSettings) TenantID() string {
	return s.tenantID
}

func (s tenantScopedSystemSettings) key(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "tenant:") || isGlobalSystemSettingsKey(key) {
		return key
	}
	return "tenant:" + s.tenantID + ":" + key
}

func isGlobalSystemSettingsKey(key string) bool {
	switch key {
	case "center_base_url",
		"center_registration",
		"admin_email",
		"hub_installation_id",
		"server_public_base_url":
		return true
	default:
		return false
	}
}
