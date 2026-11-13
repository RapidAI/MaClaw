package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func resetVisionMemos(t *testing.T) {
	t.Helper()
	prev := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	visionMemoMu.Lock()
	visionMemos = map[string]visionMemo{}
	visionMemoMu.Unlock()
	t.Cleanup(func() {
		maclawpath.SetBaseDir(prev)
		visionMemoMu.Lock()
		visionMemos = map[string]visionMemo{}
		visionMemoMu.Unlock()
	})
}

func TestDesktopModelLabelNamesTheCalledModel(t *testing.T) {
	req := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Config: corelib.AppConfig{
		MaclawLLMCurrentProvider: "chat",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "chat", URL: "https://chat.example/v1", Key: "k", Model: "foreground-model"},
			{Name: "eyes", URL: "https://eyes.example/v1", Key: "k", Model: "qwen4-max-preview"},
		},
		MaclawHubBotRoles: corelib.MaclawHubBotRoles{
			ComputerUse: corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "eyes"},
		},
	}}}
	if got := desktopModelLabel(req); got != "chat/foreground-model" {
		t.Fatalf("label = %q, want the foreground model the loop calls", got)
	}
}

func TestDesktopVisionProbeOverrideAndDowngrade(t *testing.T) {
	resetVisionMemos(t)
	probes := 0
	previous := desktopProbeVision
	desktopProbeVision = func(cfg corelib.MaclawLLMConfig) (bool, bool) {
		probes++
		if cfg.Model == "inconclusive" {
			return false, false
		}
		return true, true
	}
	t.Cleanup(func() { desktopProbeVision = previous })

	unknown := visionTurn("see", "auto", false)
	if !desktopTurnSeesImages(unknown) || probes != 1 {
		t.Fatalf("unprobed auto should probe once, probes=%d", probes)
	}
	if !desktopTurnSeesImages(unknown) || probes != 1 {
		t.Fatalf("a cached probe must not run again, probes=%d", probes)
	}

	off := visionTurn("see", "off", true)
	if desktopTurnSeesImages(off) {
		t.Fatal("override off wins over a detected vision model")
	}
	on := visionTurn("text-only", "on", false)
	if !desktopTurnSeesImages(on) {
		t.Fatal("override on sends the screenshot without a probe")
	}
	if probes != 1 {
		t.Fatalf("overrides must not probe, probes=%d", probes)
	}

	miss := visionTurn("inconclusive", "auto", false)
	if desktopTurnSeesImages(miss) || desktopTurnSeesImages(miss) || probes != 3 {
		t.Fatalf("an inconclusive probe stays unknown, probes=%d", probes)
	}

	key := desktopVisionKey(unknown)
	ctx := context.WithValue(context.Background(), desktopLLMSeesImagesKey{}, true)
	ctx = context.WithValue(ctx, desktopVisionCacheKey{}, key)
	if !desktopContextSeesImages(ctx) {
		t.Fatal("the turn started on a vision model")
	}
	desktopNoteVisionRejected(corelib.MaclawLLMConfig{URL: "https://llm.example/v1", Model: "see", ProviderName: "eyes"})
	if desktopContextSeesImages(ctx) || desktopTurnSeesImages(unknown) {
		t.Fatal("an image refusal downgrades this turn and the next one onto OCR")
	}
}

func visionTurn(model, override string, detected bool) agentruntime.TurnRequest {
	provider := corelib.MaclawLLMProvider{
		Name: "eyes", URL: "https://llm.example/v1", Key: "k", Model: model,
		VisionByModel: []corelib.ModelVision{{Model: model, Override: override}},
	}
	if detected {
		provider.VisionModels = []string{model}
		provider.VisionTestedModels = []string{model}
		provider.SupportsVision = true
		provider.VisionByModel[0].DetectedAt = "2026-01-01T00:00:00Z"
	}
	return agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Config: corelib.AppConfig{
		MaclawLLMCurrentProvider: "eyes",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{provider},
	}}}
}

func TestModelServiceLogPath(t *testing.T) {
	if got := modelServiceLogPath("/data"); !strings.HasSuffix(got, "/logs/maclaw-models.log") {
		t.Fatalf("log path = %s", got)
	}
}
