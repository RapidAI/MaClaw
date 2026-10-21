package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	cllm "github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestPrepareAgentLoopConfigSelectsRole(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground:  "fg-model",
			Background:  "bg-model",
			ComputerUse: "cu-model",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	h := &IMMessageHandler{app: app}
	// Empty roles follow the person's custom (vision) assistant; the Hub
	// snapshots apply only to an official assistant. The profile still
	// names the role each loop selected.

	fg := h.prepareAgentLoopConfig(&LoopContext{Kind: LoopKindChat})
	if fg.Config.Model != "assistant-model" || fg.Config.Profile != maclawLLMRoleForeground || fg.Config.ProviderID != "local" {
		t.Fatalf("foreground config = model %q profile %q provider %q", fg.Config.Model, fg.Config.Profile, fg.Config.ProviderID)
	}

	bg := h.prepareAgentLoopConfig(&LoopContext{Kind: LoopKindBackground})
	if bg.Config.Model != "assistant-model" || bg.Config.Profile != maclawLLMRoleBackground {
		t.Fatalf("background config = model %q profile %q", bg.Config.Model, bg.Config.Profile)
	}

	bgVision := h.prepareAgentLoopConfig(&LoopContext{
		Kind:                   LoopKindBackground,
		ComputerUseActive:      true,
		ComputerUseGateSettled: true,
	})
	if bgVision.Config.Model != "assistant-model" || bgVision.Config.Profile != maclawLLMRoleBackground {
		t.Fatalf("a background loop does not drive the computer: model %q profile %q", bgVision.Config.Model, bgVision.Config.Profile)
	}

	cu := h.prepareAgentLoopConfig(&LoopContext{
		Kind:                   LoopKindChat,
		ComputerUseActive:      true,
		ComputerUseGateSettled: true,
	})
	if cu.Config.Model != "assistant-model" || cu.Config.Profile != maclawLLMRoleForeground || cu.Config.ProviderID != "local" {
		t.Fatalf("the chat loop does not drive the computer: model %q profile %q provider %q", cu.Config.Model, cu.Config.Profile, cu.Config.ProviderID)
	}

	helper := h.prepareAgentLoopConfig(&LoopContext{
		Kind:                   LoopKindChat,
		ComputerHelper:         true,
		ComputerUseActive:      true,
		ComputerUseGateSettled: true,
	})
	if helper.Config.Model != "assistant-model" || helper.Config.Profile != maclawLLMRoleComputerUse {
		t.Fatalf("computer helper config = model %q profile %q", helper.Config.Model, helper.Config.Profile)
	}

	// Chat tasks keep LoopKindChat (iteration limits, no ContinueC pause).
	// Ordinary task execution uses the background role. Only the computer
	// helper uses the computer_use role.
	chatTask := h.prepareAgentLoopConfig(&LoopContext{
		Kind:       LoopKindChat,
		ChatTaskID: "task-1",
	})
	if chatTask.Config.Model != "assistant-model" || chatTask.Config.Profile != maclawLLMRoleBackground {
		t.Fatalf("chat task config = model %q profile %q", chatTask.Config.Model, chatTask.Config.Profile)
	}
	chatTaskVision := h.prepareAgentLoopConfig(&LoopContext{
		Kind:                   LoopKindChat,
		ChatTaskID:             "task-1",
		ComputerHelper:         true,
		ComputerUseActive:      true,
		ComputerUseGateSettled: true,
	})
	if chatTaskVision.Config.Model != "assistant-model" || chatTaskVision.Config.Profile != maclawLLMRoleComputerUse {
		t.Fatalf("computer helper task config = model %q profile %q", chatTaskVision.Config.Model, chatTaskVision.Config.Profile)
	}
}

func TestApplyTurnModelRouteKeepsBackgroundAndComputerUseModels(t *testing.T) {
	t.Setenv(cllm.CostRouteEnvKey, "on")
	app := &App{}
	app.ohModules.cachedAuxLLM = corelib.AuxiliaryLLMConfig{
		URL: "https://aux.example", Key: "ak", Model: "aux-flash",
	}
	h := &IMMessageHandler{app: app}

	background := corelib.MaclawLLMConfig{URL: "https://p.example/v1", Key: "pk", Model: "bg-model", Profile: maclawLLMRoleBackground}
	cfg, d := h.applyTurnModelRoute(background, "你好", &LoopContext{Kind: LoopKindBackground}, nil)
	if cfg.Model != "bg-model" || d.CostRouteApplied {
		t.Fatalf("background route cfg=%s applied=%v decision=%+v", cfg.Model, d.CostRouteApplied, d)
	}

	computerUse := corelib.MaclawLLMConfig{
		URL: "https://hub.example/api/llm/v1", Model: "auto", Profile: maclawLLMRoleComputerUse, SupportsVision: true,
	}
	cfg, d = h.applyTurnModelRoute(computerUse, "你好", &LoopContext{Kind: LoopKindChat}, nil)
	if cfg.Model != "auto" || d.CostRouteApplied || !cfg.HubManaged || cfg.TaskTypeHint == "" {
		t.Fatalf("computer use hub route cfg=%+v decision=%+v", cfg, d)
	}
	if !strings.Contains(d.Reason, "role model kept") {
		t.Fatalf("reason=%q", d.Reason)
	}

	foreground := corelib.MaclawLLMConfig{URL: "https://p.example/v1", Key: "pk", Model: "primary-m", Profile: maclawLLMRoleForeground}
	cfg, d = h.applyTurnModelRoute(foreground, "你好", &LoopContext{Kind: LoopKindChat}, nil)
	if cfg.Model != "aux-flash" || !d.CostRouteApplied {
		t.Fatalf("foreground should still cost-route, cfg=%s decision=%+v", cfg.Model, d)
	}
}
