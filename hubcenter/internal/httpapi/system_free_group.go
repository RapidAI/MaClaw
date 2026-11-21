package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// systemFreeGroupSettingsKey selects the model service group used for gossip
// moderation and for translating admin notifications. It is separate from the
// moderation enable flag so translation still runs when moderation is off.
const systemFreeGroupSettingsKey = "llm_system_free_group"

type systemFreeGroupConfig struct {
	ServiceGroupID string `json:"service_group_id"`
}

// LoadSystemFreeGroupID returns the configured service group, or empty when
// the admin has not chosen one.
func LoadSystemFreeGroupID(ctx context.Context, settings store.SystemSettingsRepository) (string, error) {
	if settings == nil {
		return "", nil
	}
	raw, err := settings.Get(ctx, systemFreeGroupSettingsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return "", err
	}
	var cfg systemFreeGroupConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return "", nil
	}
	return strings.TrimSpace(cfg.ServiceGroupID), nil
}

// SaveSystemFreeGroupID stores the chosen service group. An empty id clears it.
func SaveSystemFreeGroupID(ctx context.Context, settings store.SystemSettingsRepository, groupID string) error {
	if settings == nil {
		return fmt.Errorf("settings repository is required")
	}
	data, err := json.Marshal(systemFreeGroupConfig{ServiceGroupID: strings.TrimSpace(groupID)})
	if err != nil {
		return err
	}
	return settings.Set(ctx, systemFreeGroupSettingsKey, string(data))
}

// validateSystemFreeGroupID accepts an empty id. A non-empty id must name a
// group already in the model-service catalog. A nil LLM service cannot check
// the catalog, so the id is stored as given (tests and a process that has not
// wired the module).
func validateSystemFreeGroupID(ctx context.Context, llm *llmservice.Service, groupID string) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || llm == nil {
		return nil
	}
	reg, err := llm.LoadRegistry(ctx)
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("unknown service group")
	}
	for _, group := range reg.ServiceGroups {
		if strings.EqualFold(strings.TrimSpace(group.ID), groupID) {
			return nil
		}
	}
	return fmt.Errorf("unknown service group")
}
