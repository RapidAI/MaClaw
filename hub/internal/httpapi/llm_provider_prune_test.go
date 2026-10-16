package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestUpdateLLMProvidersHandlerPrunesRemovedProviderFromServiceGroups(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID: "provider-a", Name: "A", APIURL: "https://example.com/v1", Model: "gpt-a",
	}, {
		ID: "provider-b", Name: "B", APIURL: "https://example.com/v1", Model: "gpt-b",
	}}}); err != nil {
		t.Fatalf("seed provider registry: %v", err)
	}
	serviceReg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID:   "group-a",
		Name: "Group A",
		Models: []llmservice.ModelServiceModel{{
			Name:             "model-x",
			ProviderIDs:      []string{"provider-a", "provider-b"},
			CreditMultiplier: 1,
		}},
	}, {
		ID:   "group-b",
		Name: "Group B",
		Models: []llmservice.ModelServiceModel{{
			Name:        "model-y",
			ProviderIDs: []string{"provider-a"},
		}},
	}}}
	if err := llmservice.SaveRegistry(ctx, system, serviceReg); err != nil {
		t.Fatalf("seed service registry: %v", err)
	}

	request := im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID: "provider-a", Name: "A", APIURL: "https://example.com/v1", Model: "gpt-a",
	}}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", rr.Code, rr.Body.String())
	}

	storedProviders, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("reload provider registry: %v", err)
	}
	if storedProviders.FindProvider("provider-b") != nil {
		t.Fatal("provider-b should be removed")
	}
	storedServices, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("reload service registry: %v", err)
	}
	groupA := storedServices.FindModelServiceGroup("group-a")
	if groupA == nil || len(groupA.Models) != 1 {
		t.Fatalf("group-a = %#v", groupA)
	}
	if got := groupA.Models[0].ProviderIDs; len(got) != 1 || got[0] != "provider-a" {
		t.Fatalf("group-a model provider ids = %#v", got)
	}
	for _, cfg := range groupA.Models[0].ProviderConfigs {
		if cfg.ProviderID == "provider-b" {
			t.Fatalf("provider-b config still present: %#v", groupA.Models[0].ProviderConfigs)
		}
	}
	groupB := storedServices.FindModelServiceGroup("group-b")
	if groupB == nil || len(groupB.Models) != 1 || len(groupB.Models[0].ProviderIDs) != 1 || groupB.Models[0].ProviderIDs[0] != "provider-a" {
		t.Fatalf("group-b must stay untouched: %#v", groupB)
	}
}

func TestUpdateLLMProvidersHandlerStillRejectsUnknownProviderReferences(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID: "provider-a", Name: "A", APIURL: "https://example.com/v1", Model: "gpt-a",
	}}}); err != nil {
		t.Fatalf("seed provider registry: %v", err)
	}
	serviceReg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID:   "group-a",
		Name: "Group A",
		Models: []llmservice.ModelServiceModel{{
			Name:        "model-x",
			ProviderIDs: []string{"provider-ghost"},
		}},
	}}}
	if err := llmservice.SaveRegistry(ctx, system, serviceReg); err != nil {
		t.Fatalf("seed service registry: %v", err)
	}

	// provider-ghost exists in neither the old nor the new registry, so the
	// save must still be rejected instead of silently pruning a typo.
	request := im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID: "provider-a", Name: "A", APIURL: "https://example.com/v1", Model: "gpt-a",
	}}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400, body=%s", rr.Code, rr.Body.String())
	}
	storedProviders, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("reload provider registry: %v", err)
	}
	if storedProviders.FindProvider("provider-a") == nil {
		t.Fatal("rejected save must not modify the provider registry")
	}
}

func TestUpdateLLMProvidersHandlerUnchangedProvidersKeepServiceGroups(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID: "provider-a", Name: "A", APIURL: "https://example.com/v1", Model: "gpt-a",
	}}}); err != nil {
		t.Fatalf("seed provider registry: %v", err)
	}
	serviceReg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID:   "group-a",
		Name: "Group A",
		Models: []llmservice.ModelServiceModel{{
			Name:        "model-x",
			ProviderIDs: []string{"provider-a"},
		}},
	}}}
	if err := llmservice.SaveRegistry(ctx, system, serviceReg); err != nil {
		t.Fatalf("seed service registry: %v", err)
	}

	request := im.LLMProviderRegistry{Enabled: true, Providers: []im.LLMProvider{{
		ID: "provider-a", Name: "A renamed", APIURL: "https://example.com/v1", Model: "gpt-a",
	}}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", rr.Code, rr.Body.String())
	}

	storedServices, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("reload service registry: %v", err)
	}
	groupA := storedServices.FindModelServiceGroup("group-a")
	if groupA == nil || len(groupA.Models) != 1 || len(groupA.Models[0].ProviderIDs) != 1 || groupA.Models[0].ProviderIDs[0] != "provider-a" {
		t.Fatalf("group-a must stay untouched: %#v", groupA)
	}
}
