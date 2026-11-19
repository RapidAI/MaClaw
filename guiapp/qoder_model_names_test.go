package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/qoder"
)

func TestRewriteQoderAssignedModelsUsesDisplayNames(t *testing.T) {
	qoder.RememberModels([]qoder.Model{
		{Key: "auto", DisplayName: "Fast", Enabled: true},
		{Key: "dfmodel", DisplayName: "DeepSeek-Flash", Enabled: true},
		{Key: "qmodel_38max", DisplayName: "Qwen3.8-Max", Enabled: true},
		{Key: "gmodel", DisplayName: "GLM-5.3", Enabled: true},
	})
	providers := []corelib.MaclawLLMProvider{
		{
			ID:           "qoder-cn",
			Name:         qoder.NameCN,
			URL:          qoder.ChatBase,
			Model:        "auto",
			Models:       []string{"auto", "dfmodel", "qmodel_38max"},
			VisionModels: []string{"dfmodel"},
		},
		{
			ID:     "other",
			Name:   "OpenAI",
			Model:  "auto",
			Models: []string{"auto", "gpt-5"},
		},
	}
	profiles := &corelib.MaclawLLMProfiles{
		Assistant: corelib.MaclawLLMProfile{ProviderID: "qoder-cn", Model: "dfmodel"},
		Coding:    corelib.MaclawLLMProfile{ProviderID: "other", Model: "auto"},
		Horizon: corelib.MaclawLLMHorizonProfiles{
			Executor: corelib.MaclawLLMProfile{ProviderID: "qoder-cn", Model: "gmodel"},
		},
	}
	rewriteQoderAssignedModels(providers, profiles)
	if providers[0].Model != "Fast" {
		t.Fatalf("qoder model = %q, want Fast", providers[0].Model)
	}
	want := []string{"Fast", "DeepSeek-Flash", "Qwen3.8-Max"}
	if len(providers[0].Models) != len(want) {
		t.Fatalf("qoder catalog = %#v", providers[0].Models)
	}
	for i := range want {
		if providers[0].Models[i] != want[i] {
			t.Fatalf("qoder catalog = %#v", providers[0].Models)
		}
	}
	if len(providers[0].VisionModels) != 1 || providers[0].VisionModels[0] != "DeepSeek-Flash" {
		t.Fatalf("vision models = %#v", providers[0].VisionModels)
	}
	if providers[1].Model != "auto" || len(providers[1].Models) != 2 || providers[1].Models[0] != "auto" {
		t.Fatalf("unrelated provider changed: %#v", providers[1])
	}
	if profiles.Assistant.Model != "DeepSeek-Flash" {
		t.Fatalf("assistant model = %q", profiles.Assistant.Model)
	}
	if profiles.Coding.Model != "auto" {
		t.Fatalf("coding model = %q", profiles.Coding.Model)
	}
	if profiles.Horizon.Executor.Model != "GLM-5.3" {
		t.Fatalf("executor model = %q", profiles.Horizon.Executor.Model)
	}
}

func TestQoderCatalogProfilesStayOnNamedEdition(t *testing.T) {
	named := qoderCatalogProfiles(qoder.NameGlobal)
	if len(named) != 1 || named[0].Name != qoder.NameGlobal {
		t.Fatalf("global profiles = %#v", named)
	}
	either := qoderCatalogProfiles("")
	if len(either) != 2 || either[0].Name != qoder.NameCN || either[1].Name != qoder.NameGlobal {
		t.Fatalf("unnamed profiles = %#v", either)
	}
}

func TestQoderConnectionTreatsDisplayNameAsSameModel(t *testing.T) {
	qoder.RememberModels([]qoder.Model{
		{Key: "dfmodel", DisplayName: "DeepSeek-Flash", Enabled: true},
		{Key: "auto", DisplayName: "Fast", Enabled: true},
	})
	short := corelib.MaclawLLMProvider{
		Name: qoder.NameCN, URL: qoder.ChatBase, Model: "dfmodel",
		Protocol: "openai", AuthType: "oauth",
	}
	named := short
	named.Model = "DeepSeek-Flash"
	if !sameMaclawLLMProviderConnectionShape(short, named) {
		t.Fatal("display name was treated as a different Qoder connection")
	}
	other := corelib.MaclawLLMProvider{
		Name: "OpenAI", URL: "https://api.openai.com/v1", Model: "auto",
		Protocol: "openai", AuthType: "api_key",
	}
	renamed := other
	renamed.Model = "Fast"
	if sameMaclawLLMProviderConnectionShape(other, renamed) {
		t.Fatal("non-qoder model comparison used a Qoder alias")
	}
}
