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

// userReferralMetricSystemSettings carries the durable referral repository
// alongside the existing settings dependency. It keeps the metering API
// unchanged while letting its periodic flush record aggregate-only referral
// reward metrics without a database lookup on every request.
type userReferralMetricSystemSettings struct {
	store.SystemSettingsRepository
	repo    store.UserReferralRepository
	usage   store.LLMUsageRepository
	billing store.LLMBillingLedgerRepository
}

func (s userReferralMetricSystemSettings) UserReferralMetricRepository() store.UserReferralRepository {
	return s.repo
}

func (s userReferralMetricSystemSettings) LLMUsageRepository() store.LLMUsageRepository {
	return s.usage
}

func (s userReferralMetricSystemSettings) LLMBillingLedgerRepository() store.LLMBillingLedgerRepository {
	return s.billing
}

func (s userReferralMetricSystemSettings) TenantID() string {
	if scoped, ok := s.SystemSettingsRepository.(interface{ TenantID() string }); ok {
		return scoped.TenantID()
	}
	return store.DefaultTenantID
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
	if metrics, ok := base.(userReferralMetricSystemSettings); ok {
		metrics.SystemSettingsRepository = tenantScopedSystemSettings{tenantID: tenantID, base: metrics.SystemSettingsRepository}
		return metrics
	}
	return tenantScopedSystemSettings{tenantID: tenantID, base: base}
}

func globalSystemSettings(base store.SystemSettingsRepository) store.SystemSettingsRepository {
	if metrics, ok := base.(userReferralMetricSystemSettings); ok {
		return globalSystemSettings(metrics.SystemSettingsRepository)
	}
	if scoped, ok := base.(tenantScopedSystemSettings); ok {
		return scoped.base
	}
	return base
}

func (s tenantScopedSystemSettings) GlobalSystemSettings() store.SystemSettingsRepository {
	return globalSystemSettings(s.base)
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
