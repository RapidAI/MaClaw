package llmservice

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestProviderAccessScopePersistsAndNormalizes(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         "https://api.openai.com/v1",
		AllowedNodeIDs: []string{" hc-us ", "", "hc-sg", "HC-US"},
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	got, err := svc.GetProvider(ctx, "openai")
	if err != nil || got == nil {
		t.Fatalf("GetProvider: %#v err=%v", got, err)
	}
	if want := []string{"hc-us", "hc-sg"}; !reflect.DeepEqual(got.AllowedNodeIDs, want) {
		t.Fatalf("AllowedNodeIDs = %#v, want %#v", got.AllowedNodeIDs, want)
	}

	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:     "openai",
		Name:   "OpenAI US",
		APIURL: "https://api.openai.com/v1",
	}); err != nil {
		t.Fatalf("UpdateProvider without allowlist: %v", err)
	}
	got, err = svc.GetProvider(ctx, "openai")
	if err != nil || got == nil {
		t.Fatalf("reload: %#v err=%v", got, err)
	}
	if want := []string{"hc-us", "hc-sg"}; !reflect.DeepEqual(got.AllowedNodeIDs, want) {
		t.Fatalf("omitted allowlist wiped scope: %#v", got.AllowedNodeIDs)
	}

	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI US",
		APIURL:         "https://api.openai.com/v1",
		AllowedNodeIDs: []string{},
	}); err != nil {
		t.Fatalf("UpdateProvider empty allowlist: %v", err)
	}
	got, err = svc.GetProvider(ctx, "openai")
	if err != nil || got == nil {
		t.Fatalf("reload all-nodes: %#v err=%v", got, err)
	}
	if len(got.AllowedNodeIDs) != 0 {
		t.Fatalf("empty allowlist should mean all nodes, got %#v", got.AllowedNodeIDs)
	}
}

func TestProviderAccessScopeJSONRoundTripsHASettingPayload(t *testing.T) {
	ctx := context.Background()
	settings := &mockSystemSettings{}
	svc := NewService(settings)
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         "https://api.openai.com/v1",
		AllowedNodeIDs: []string{"hc-us", "hc-sg"},
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	raw := settings.data[RegistrySettingKey]
	if !strings.Contains(raw, `"allowed_node_ids"`) {
		t.Fatalf("persisted registry dropped allowed_node_ids: %s", raw)
	}
	wrapped, err := json.Marshal(struct {
		Key       string `json:"key"`
		ValueJSON string `json:"value_json"`
	}{Key: RegistrySettingKey, ValueJSON: raw})
	if err != nil {
		t.Fatalf("marshal HA payload: %v", err)
	}
	var payload struct {
		Key       string `json:"key"`
		ValueJSON string `json:"value_json"`
	}
	if err := json.Unmarshal(wrapped, &payload); err != nil {
		t.Fatalf("unmarshal HA payload: %v", err)
	}
	var stored Registry
	if err := json.Unmarshal([]byte(payload.ValueJSON), &stored); err != nil {
		t.Fatalf("unmarshal registry from HA value: %v", err)
	}
	if len(stored.Providers) != 1 {
		t.Fatalf("providers = %#v", stored.Providers)
	}
	if want := []string{"hc-us", "hc-sg"}; !reflect.DeepEqual(stored.Providers[0].AllowedNodeIDs, want) {
		t.Fatalf("HA round-trip AllowedNodeIDs = %#v, want %#v", stored.Providers[0].AllowedNodeIDs, want)
	}
}
