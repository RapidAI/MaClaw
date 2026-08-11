package digitalasset

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// SettingsUpdate is a partial update for tenant digital-asset settings (admin UI).
type SettingsUpdate struct {
	Enabled     *bool `json:"enabled"`
	SyncEnabled *bool `json:"sync_enabled"`
}

func tenantSettingsStorageKey(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return SettingsKey
	}
	return "tenant:" + tenantID + ":" + SettingsKey
}

// processDefaults returns hub-config / env seed settings used when no tenant row exists.
func (s *Service) processDefaults() TenantSettings {
	base := DefaultTenantSettings()
	if s == nil {
		return base
	}
	// Router seeds s.Settings from config; prefer that snapshot for limits when present.
	if s.Settings.MaxLibraries > 0 || s.Settings.MaxOpenLibraries > 0 || s.Settings.MaxLibraryBytes > 0 {
		base = s.Settings
	}
	// Process config/env seed for Enabled (system_settings row may override later).
	// s.Enabled is the authority so tests can flip the process flag without rewriting Settings.
	base.Enabled = s.Enabled
	return base
}

// LoadTenantSettings returns effective digital-asset settings for a tenant.
// When system_settings has a row for the tenant, Enabled/SyncEnabled come from that row
// (so Admin UI can toggle without editing hub config.yaml). Otherwise processDefaults apply.
func (s *Service) LoadTenantSettings(ctx context.Context, tenantID string) TenantSettings {
	base := s.processDefaults()
	if s == nil || s.System == nil {
		return base
	}
	raw, err := s.System.Get(ctx, tenantSettingsStorageKey(tenantID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return base
	}
	var stored TenantSettings
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return base
	}
	return fillTenantSettingsZeros(stored)
}

// SaveTenantSettings persists full settings for a tenant (used after merge with defaults).
func (s *Service) SaveTenantSettings(ctx context.Context, tenantID string, settings TenantSettings) error {
	if s == nil || s.System == nil {
		return ErrFeatureDisabled
	}
	settings = fillTenantSettingsZeros(settings)
	b, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return s.System.Set(ctx, tenantSettingsStorageKey(tenantID), string(b))
}

// UpdateTenantSettings applies a partial patch and persists.
func (s *Service) UpdateTenantSettings(ctx context.Context, tenantID string, patch SettingsUpdate) (TenantSettings, error) {
	if s == nil {
		return TenantSettings{}, ErrFeatureDisabled
	}
	if s.System == nil {
		return TenantSettings{}, ErrFeatureDisabled
	}
	cur := s.LoadTenantSettings(ctx, tenantID)
	if patch.Enabled != nil {
		cur.Enabled = *patch.Enabled
	}
	if patch.SyncEnabled != nil {
		cur.SyncEnabled = *patch.SyncEnabled
	}
	if err := s.SaveTenantSettings(ctx, tenantID, cur); err != nil {
		return cur, err
	}
	return cur, nil
}

// IsFeatureEnabled reports whether digital assets are on for the tenant.
func (s *Service) IsFeatureEnabled(ctx context.Context, tenantID string) bool {
	if s == nil {
		return false
	}
	return s.LoadTenantSettings(ctx, tenantID).Enabled
}

func fillTenantSettingsZeros(s TenantSettings) TenantSettings {
	d := DefaultTenantSettings()
	out := s
	// Enabled / SyncEnabled kept as stored (false is meaningful).
	// If SyncEnabled was never written in an old payload, default true when all limits are zero?
	// We always save full structs from UpdateTenantSettings; SyncEnabled false is valid.
	if out.RevokePolicy == "" {
		out.RevokePolicy = d.RevokePolicy
	}
	if out.MaxLibraryBytes <= 0 {
		out.MaxLibraryBytes = d.MaxLibraryBytes
	}
	if out.MaxLibraries <= 0 {
		out.MaxLibraries = d.MaxLibraries
	}
	if out.MaxArchiveUploadBytes <= 0 {
		out.MaxArchiveUploadBytes = d.MaxArchiveUploadBytes
	}
	if out.MaxArchiveExtractedBytes <= 0 {
		out.MaxArchiveExtractedBytes = d.MaxArchiveExtractedBytes
	}
	if out.MaxArchiveFileCount <= 0 {
		out.MaxArchiveFileCount = d.MaxArchiveFileCount
	}
	if out.MaxBrowserDirFiles <= 0 {
		out.MaxBrowserDirFiles = d.MaxBrowserDirFiles
	}
	if out.MaxBrowserDirBytes <= 0 {
		out.MaxBrowserDirBytes = d.MaxBrowserDirBytes
	}
	if out.MaxOpenLibraries <= 0 {
		out.MaxOpenLibraries = d.MaxOpenLibraries
	}
	if out.ChangelogKeepRevs <= 0 {
		out.ChangelogKeepRevs = d.ChangelogKeepRevs
	}
	if out.ChangelogKeepDays <= 0 {
		out.ChangelogKeepDays = d.ChangelogKeepDays
	}
	if out.PerTenantConcurrentPulls <= 0 {
		out.PerTenantConcurrentPulls = d.PerTenantConcurrentPulls
	}
	if out.PerUserPullRPM <= 0 {
		out.PerUserPullRPM = d.PerUserPullRPM
	}
	if len(out.ArchiveIncludeExtensions) == 0 {
		out.ArchiveIncludeExtensions = append([]string{}, d.ArchiveIncludeExtensions...)
	}
	if len(out.ArchiveDenyExtensions) == 0 {
		out.ArchiveDenyExtensions = append([]string{}, d.ArchiveDenyExtensions...)
	}
	// AllowAdminImportPrivate: keep as stored.
	return out
}
