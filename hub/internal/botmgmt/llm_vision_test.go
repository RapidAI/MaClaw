package botmgmt

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSaveLLMSettingsProbesEachPinnedModel(t *testing.T) {
	svc := NewService(&memSettings{})
	var probed []string
	svc.ProbeVision = func(ctx context.Context, cfg corelib.MaclawLLMConfig) (bool, bool) {
		probed = append(probed, cfg.Model)
		return cfg.Model == "qwen4-max-preview", true
	}
	view, err := svc.SaveLLMSettings(context.Background(), "tenant-a", "maclaw_official_group", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "qwen-4-max", Protocol: "openai",
		URL: "https://token-plan.example/compatible-mode/v1", Key: "sk-test", Model: "qwen-default",
		Vision: []LLMModelVisionInput{
			{Model: "qwen-default", Override: "auto"},
			{Model: "qwen4-max-preview", Override: "off"},
		},
	}}, nil, &LLMRoles{
		ComputerUse: LLMRoleAssignment{Mode: "pin", Provider: "lp_abcdef0123456789", Model: "qwen4-max-preview"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(probed) != 2 {
		t.Fatalf("probed %v, want the provider default and the role pin", probed)
	}
	got := view.LLM.Providers[0]
	var preview, fallback llmModelVision
	for _, row := range got.Vision {
		switch row.Model {
		case "qwen4-max-preview":
			preview = row
		case "qwen-default":
			fallback = row
		}
	}
	if !preview.Known || !preview.Supported || preview.Override != "off" || preview.DetectedAt == "" {
		t.Fatalf("pinned model = %+v", preview)
	}
	if !fallback.Known || fallback.Supported || fallback.Override != "auto" {
		t.Fatalf("provider default = %+v", fallback)
	}
	wire := llmProviderWire(llmProvider{ID: got.ID, Name: got.Name, URL: got.URL, Model: got.Model, Protocol: got.Protocol, Vision: got.Vision})
	rows, _ := wire["vision_by_model"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("wire = %#v", wire)
	}
	tested, _ := wire["vision_tested_models"].([]string)
	if len(tested) != 2 {
		t.Fatalf("tested = %#v", wire["vision_tested_models"])
	}
}
