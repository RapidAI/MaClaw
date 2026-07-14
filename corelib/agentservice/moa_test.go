package agentservice

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

func TestMaterializeProviderByName(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "alpha", URL: "https://a.example/v1", Key: "ka", Model: "m-a", Protocol: "openai"},
			{Name: "Beta", URL: "https://b.example/v1", Key: "kb", Model: "m-b"},
		},
	}
	got, err := materializeProviderByName(cfg, "beta")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "m-b" || got.URL != "https://b.example/v1" || got.ProviderName != "Beta" {
		t.Fatalf("%+v", got)
	}
	if _, err := materializeProviderByName(cfg, "missing"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveMoAPresetForRequest(t *testing.T) {
	t.Setenv("MACLAW_MOA", "on")
	primary := corelib.MaclawLLMConfig{URL: "https://p.example/v1", Key: "k", Model: "primary", Protocol: "openai", WireAPI: "chat"}
	cfg := corelib.AppConfig{
		MoA: corelib.MoAConfig{
			Enabled:       true,
			DefaultPreset: "review",
			Presets: map[string]corelib.MoAPresetConfig{
				"review": {
					Enabled: true,
					Aggregator: corelib.MoAModelRef{UsePrimary: true},
					ReferenceModels: []corelib.MoAModelRef{
						{Provider: "advisor"},
					},
				},
			},
		},
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "advisor", URL: "https://r.example/v1", Key: "rk", Model: "ref-m", Protocol: "openai", WireAPI: "chat"},
		},
	}
	resolved, detail, ok := resolveMoAPresetForRequest(cfg, primary, "")
	if !ok {
		t.Fatalf("resolve failed: %s", detail)
	}
	if resolved.Name != "review" || len(resolved.References) != 1 {
		t.Fatalf("%+v", resolved)
	}
	if resolved.References[0].Config.Model != "ref-m" {
		t.Fatalf("ref cfg: %+v", resolved.References[0].Config)
	}

	t.Setenv("MACLAW_MOA", "off")
	if moa.EnvAllows() {
		t.Fatal("env off should deny EnvAllows")
	}
	if _, _, ok = resolveMoAPresetForRequest(cfg, primary, "review"); ok {
		t.Fatal("should fail when env off")
	}
}

func TestMoAPresetFromMetadata(t *testing.T) {
	if got := moaPresetFromMetadata(map[string]string{"moa_preset": "Review"}); got != "review" {
		t.Fatalf("got %q", got)
	}
	if got := moaPresetFromMetadata(map[string]string{"moa": "moa:deep"}); got != "deep" {
		t.Fatalf("got %q", got)
	}
	if got := moaPresetFromMetadata(nil, map[string]string{"x": "y"}); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestExecute_ExplicitMoAPresetFailClosed(t *testing.T) {
	t.Setenv("MACLAW_MOA", "on")
	exec := &CoreAgentExecutor{}
	// Config enables MoA but has no usable references → explicit request must error.
	cfg := corelib.AppConfig{
		MaclawLLMUrl:   "https://llm.example/v1",
		MaclawLLMKey:   "k",
		MaclawLLMModel: "m",
		MoA: corelib.MoAConfig{
			Enabled:       true,
			DefaultPreset: "review",
			Presets: map[string]corelib.MoAPresetConfig{
				"review": {
					Enabled:    true,
					Aggregator: corelib.MoAModelRef{UsePrimary: true},
					// no reference_models → unusable
				},
			},
		},
	}
	_, err := exec.Execute(context.Background(), ExecuteRequest{
		Config:    cfg,
		Message:   Message{Content: "hello", Role: MessageRoleUser},
		MoAPreset: "review",
		Principal: Principal{TenantID: "t", UserID: "u"},
		Session:   Session{ID: "s"},
		Instance:  Instance{Workspace: t.TempDir()},
		DataDir:   t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected fail-closed error for explicit moa_preset")
	}
	if !strings.Contains(err.Error(), "multi-model council unavailable") {
		t.Fatalf("error: %v", err)
	}
}
