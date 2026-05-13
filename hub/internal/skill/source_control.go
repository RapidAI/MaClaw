// Package skill provides skill source access control HTTP handlers for maclawsrv.
//
// The core logic (SourceControlService, SourceControlConfig, ResolveForUser)
// lives in corelib/skill — this package only provides the HTTP handler layer
// and adapts hub/internal/store.SystemSettingsRepository to corelib/skill.KVStore.
package skill

import (
	"context"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// AllSources re-exports the canonical list from corelib/skill.
var AllSources = cskill.AllSkillSources

// SourceControlConfig re-exports for handler use.
type SourceControlConfig = cskill.SourceControlConfig

// SourceControlService re-exports for handler use.
type SourceControlService = cskill.SourceControlService

// NewSourceControlService creates a SourceControlService backed by the hub's
// SystemSettingsRepository (adapted to corelib/skill.KVStore).
func NewSourceControlService(system store.SystemSettingsRepository) *SourceControlService {
	return cskill.NewSourceControlService(&systemSettingsAdapter{system: system})
}

// systemSettingsAdapter adapts hub/internal/store.SystemSettingsRepository
// to corelib/skill.KVStore.
type systemSettingsAdapter struct {
	system store.SystemSettingsRepository
}

func (a *systemSettingsAdapter) Set(ctx context.Context, key, value string) error {
	return a.system.Set(ctx, key, value)
}

func (a *systemSettingsAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.system.Get(ctx, key)
}
