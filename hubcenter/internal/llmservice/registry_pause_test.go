package llmservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestSetProviderPausedAndUpdatePreservesPause(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	if err := svc.SetProviderPaused(ctx, "deepseek", true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || !got.Paused {
		t.Fatalf("paused provider = %#v err=%v", got, err)
	}
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "DeepSeek Chat",
		APIURL: "https://api.deepseek.com/v1",
		Paused: false,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil {
		t.Fatalf("reload: %#v err=%v", got, err)
	}
	if !got.Paused || got.Name != "DeepSeek Chat" {
		t.Fatalf("update cleared pause or lost name: %#v", got)
	}
	if err := svc.SetProviderPaused(ctx, "deepseek", false); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got, err = svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || got.Paused {
		t.Fatalf("resumed provider = %#v err=%v", got, err)
	}
}

func TestSetProviderPausedMissing(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	err := svc.SetProviderPaused(context.Background(), "missing", true)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("error = %v, want ErrProviderNotFound", err)
	}
}

func TestSetProviderPausedTrimsID(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	if err := svc.SetProviderPaused(ctx, "  deepseek  ", true); err != nil {
		t.Fatalf("pause trimmed id: %v", err)
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || !got.Paused {
		t.Fatalf("paused provider = %#v err=%v", got, err)
	}
}

type pauseFailSettings struct {
	mockSystemSettings
	failSet bool
}

func (s *pauseFailSettings) Set(ctx context.Context, key, val string) error {
	if s.failSet {
		return fmt.Errorf("disk full")
	}
	return s.mockSystemSettings.Set(ctx, key, val)
}

func TestSetProviderPausedDoesNotMutateCacheOnSaveFailure(t *testing.T) {
	ctx := context.Background()
	settings := &pauseFailSettings{mockSystemSettings: mockSystemSettings{data: map[string]string{}}}
	svc := NewService(settings)
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	settings.failSet = true
	if err := svc.SetProviderPaused(ctx, "deepseek", true); err == nil {
		t.Fatal("expected save error")
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || got.Paused {
		t.Fatalf("cache mutated after failed save: %#v err=%v", got, err)
	}
}

func TestMutateRegistryPreservesPause(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	if err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID: "g1", Name: "G1", AgentID: DefaultComputeAgentID,
		Models: []llmpool.ModelConfig{{Name: "gpt-4", ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "deepseek"}}}},
	}); err != nil {
		t.Fatalf("add group: %v", err)
	}
	if err := svc.SetProviderPaused(ctx, "deepseek", true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		if len(reg.ServiceGroups) == 0 {
			t.Fatal("missing service group")
		}
		reg.ServiceGroups[0].AccessPolicy = AccessPolicyGrantRequired
		return true, nil
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || !got.Paused {
		t.Fatalf("mutate cleared pause: %#v err=%v", got, err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil || len(reg.ServiceGroups) == 0 || reg.ServiceGroups[0].AccessPolicy != AccessPolicyGrantRequired {
		t.Fatalf("access policy not updated: %#v err=%v", reg, err)
	}
}

func TestInvalidateCacheReloadsPausedProvider(t *testing.T) {
	ctx := context.Background()
	settings := &mockSystemSettings{data: map[string]string{}}
	svc := NewService(settings)
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	got, err := svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || got.Paused {
		t.Fatalf("fresh provider = %#v err=%v", got, err)
	}

	var stored Registry
	if err := json.Unmarshal([]byte(settings.data[RegistrySettingKey]), &stored); err != nil {
		t.Fatalf("parse stored registry: %v", err)
	}
	if len(stored.Providers) == 0 {
		t.Fatal("stored registry missing provider")
	}
	stored.Providers[0].Paused = true
	raw, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal paused registry: %v", err)
	}
	settings.data[RegistrySettingKey] = string(raw)

	got, err = svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || got.Paused {
		t.Fatalf("cached provider should still be live: %#v err=%v", got, err)
	}

	svc.InvalidateCache()
	got, err = svc.GetProvider(ctx, "deepseek")
	if err != nil || got == nil || !got.Paused {
		t.Fatalf("after invalidate provider = %#v err=%v", got, err)
	}
}

func TestFailedSaveDoesNotMutateCachedServiceGroupModels(t *testing.T) {
	ctx := context.Background()
	settings := &pauseFailSettings{mockSystemSettings: mockSystemSettings{data: map[string]string{}}}
	svc := NewService(settings)
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:     "deepseek",
		Name:   "deepseek",
		APIURL: "https://api.deepseek.com/v1",
	}); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	if err := svc.AddServiceGroup(ctx, llmpool.ServiceGroup{
		ID: "g1", Name: "G1", AgentID: DefaultComputeAgentID,
		Models: []llmpool.ModelConfig{{
			Name: "gpt-4",
			ProviderConfigs: []llmpool.ModelProviderConfig{{
				ProviderID: "deepseek",
				Model:      "deepseek-chat",
			}},
		}},
	}); err != nil {
		t.Fatalf("add group: %v", err)
	}

	settings.failSet = true
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		if len(reg.ServiceGroups) == 0 || len(reg.ServiceGroups[0].Models) == 0 || len(reg.ServiceGroups[0].Models[0].ProviderConfigs) == 0 {
			t.Fatal("missing nested model route")
		}
		reg.ServiceGroups[0].Models[0].Name = "mutated"
		reg.ServiceGroups[0].Models[0].ProviderConfigs[0].Model = "mutated-upstream"
		return true, nil
	}); err == nil {
		t.Fatal("expected save error")
	}

	reg, err := svc.LoadRegistry(ctx)
	if err != nil || len(reg.ServiceGroups) == 0 || len(reg.ServiceGroups[0].Models) == 0 {
		t.Fatalf("reload after failed save: %#v err=%v", reg, err)
	}
	model := reg.ServiceGroups[0].Models[0]
	if model.Name != "gpt-4" {
		t.Fatalf("cached model name mutated: %q", model.Name)
	}
	if len(model.ProviderConfigs) == 0 || model.ProviderConfigs[0].Model != "deepseek-chat" {
		t.Fatalf("cached provider route mutated: %#v", model.ProviderConfigs)
	}
}
