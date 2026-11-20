package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCreateTaskUnifiedStoresTaskLLMPin(t *testing.T) {
	app := newTaskLLMTestApp(t)

	pinned, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:          "整理周报",
		LLMProviderID: "local",
		LLMModel:      "custom:model",
	})
	if err != nil {
		t.Fatalf("CreateTaskUnified: %v", err)
	}
	if id, model := app.memoryStore.ProjectIndex().TaskLLM(pinned.ProjectPath); id != "local" || model != "custom:model" {
		t.Fatalf("stored pin = %q %q", id, model)
	}
	choice, err := app.GetTaskLLMChoice(pinned.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if choice.FollowsAssignment || choice.ProviderID != "local" || choice.Model != "custom:model" || choice.ProviderName != "Local" {
		t.Fatalf("choice = %+v", choice)
	}

	plain, err := app.CreateTaskUnified(TaskCreateOptions{Name: "再写一份"})
	if err != nil {
		t.Fatal(err)
	}
	if id, model := app.memoryStore.ProjectIndex().TaskLLM(plain.ProjectPath); id != "" || model != "" {
		t.Fatalf("empty options stored %q %q", id, model)
	}

	same, err := app.CreateTaskUnified(TaskCreateOptions{
		Name:          "跟随分配",
		LLMProviderID: "local",
		LLMModel:      "assistant-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id, model := app.memoryStore.ProjectIndex().TaskLLM(same.ProjectPath); id != "" || model != "" {
		t.Fatalf("assignment pair was stored as a pin: %q %q", id, model)
	}
	followed, err := app.GetTaskLLMChoice(same.ProjectPath)
	if err != nil || !followed.FollowsAssignment || followed.Model != "assistant-model" {
		t.Fatalf("followed = %+v err %v", followed, err)
	}

	if _, err := app.CreateTaskUnified(TaskCreateOptions{Name: "缺一半", LLMProviderID: "local"}); err == nil {
		t.Fatal("one-sided pin was accepted")
	}
	if _, err := app.CreateTaskUnified(TaskCreateOptions{Name: "没有这家", LLMProviderID: "missing", LLMModel: "x"}); err == nil {
		t.Fatal("unknown provider was accepted")
	}
}

func TestTaskLLMChoiceClearAndCodingAssignment(t *testing.T) {
	app := newTaskLLMTestApp(t)
	created, err := app.CreateTaskUnified(TaskCreateOptions{Name: "编程", Mode: "coding_dev", WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.SetTaskLLMChoice(created.ProjectPath, "local", "pinned-model"); err != nil {
		t.Fatal(err)
	}
	choice, err := app.GetTaskLLMChoice(created.ProjectPath)
	if err != nil || choice.FollowsAssignment || choice.Model != "pinned-model" {
		t.Fatalf("pin = %+v err %v", choice, err)
	}
	if err := app.SetTaskLLMChoice(created.ProjectPath, "", ""); err != nil {
		t.Fatal(err)
	}
	choice, err = app.GetTaskLLMChoice(created.ProjectPath)
	if err != nil || !choice.FollowsAssignment || choice.ProviderID != "local" || choice.Model != "assistant-model" {
		t.Fatalf("cleared = %+v err %v", choice, err)
	}
	if err := app.SetTaskLLMChoice(created.ProjectPath, "hub", ""); err == nil {
		t.Fatal("one-sided update was accepted")
	}
}

func TestPrepareAgentLoopConfigUsesTaskLLMPin(t *testing.T) {
	app := newTaskLLMTestApp(t)
	h := &IMMessageHandler{app: app}
	path := `D:\tasks\pin-loop`
	userID := projectSessionOwnerID(path)

	pinned := h.prepareAgentLoopConfig(&LoopContext{Kind: LoopKindChat, UserID: userID})
	if pinned.Config.Model != "assistant-model" || pinned.Config.Profile != maclawLLMRoleForeground {
		t.Fatalf("unpinned = model %q profile %q", pinned.Config.Model, pinned.Config.Profile)
	}

	app.memoryStore.ProjectIndex().SetTaskLLM(path, "local", "text-only-model")
	chat := h.prepareAgentLoopConfig(&LoopContext{
		Kind:       LoopKindChat,
		ChatTaskID: "task-1",
		UserID:     userID,
	})
	if chat.Config.Model != "text-only-model" || chat.Config.Profile != maclawLLMRoleBackground || chat.Config.ProviderID != "local" {
		t.Fatalf("chat pin = model %q profile %q provider %q", chat.Config.Model, chat.Config.Profile, chat.Config.ProviderID)
	}

	helper := h.prepareAgentLoopConfig(&LoopContext{
		Kind:                   LoopKindChat,
		UserID:                 userID,
		ComputerHelper:         true,
		ComputerUseActive:      true,
		ComputerUseGateSettled: true,
	})
	if helper.Config.Model != "assistant-model" || helper.Config.Profile != maclawLLMRoleComputerUse {
		t.Fatalf("text pin stole computer use: model %q profile %q", helper.Config.Model, helper.Config.Profile)
	}

	app.memoryStore.ProjectIndex().SetTaskLLM(path, "local", "vision-model")
	seeing := h.prepareAgentLoopConfig(&LoopContext{
		Kind:                   LoopKindChat,
		UserID:                 userID,
		ComputerHelper:         true,
		ComputerUseActive:      true,
		ComputerUseGateSettled: true,
	})
	if seeing.Config.Model != "vision-model" || !seeing.Config.SupportsVision || seeing.Config.Profile != maclawLLMRoleComputerUse {
		t.Fatalf("vision pin = model %q vision %v profile %q", seeing.Config.Model, seeing.Config.SupportsVision, seeing.Config.Profile)
	}

	app.memoryStore.ProjectIndex().SetTaskLLM(path, "missing", "gone-model")
	fallen := h.prepareAgentLoopConfig(&LoopContext{Kind: LoopKindChat, UserID: userID})
	if fallen.Config.Model != "assistant-model" || fallen.Config.Profile != maclawLLMRoleForeground {
		t.Fatalf("missing provider = model %q profile %q", fallen.Config.Model, fallen.Config.Profile)
	}

	app.memoryStore.ProjectIndex().SetTaskLLM(path, "local", "task-model")
	app.mcpRememberPin(path, "Local", "mcp-model")
	mcp := h.prepareAgentLoopConfig(&LoopContext{Kind: LoopKindChat, UserID: userID})
	if mcp.Config.Model != "mcp-model" || mcp.Config.Profile != maclawLLMRoleForeground {
		t.Fatalf("mcp pin = model %q profile %q", mcp.Config.Model, mcp.Config.Profile)
	}
	if strings.TrimSpace(mcp.Config.RouteSource) == "" && mcp.Config.Model == "" {
		t.Fatal("mcp config was empty")
	}
}

func TestTaskLLMCodingSessionUsesPin(t *testing.T) {
	app := newTaskLLMTestApp(t)
	h := &IMMessageHandler{app: app}
	path := `D:\tasks\coding-pin`
	userID := projectSessionOwnerID(path)

	base := h.codingLLMConfigForSession(userID)
	if base.Model != "assistant-model" || base.Profile != maclawLLMProfileCoding {
		t.Fatalf("unpinned coding = model %q profile %q", base.Model, base.Profile)
	}
	if got := h.getCodingLLMConfig(); got.Model != "assistant-model" || got.Profile != maclawLLMProfileCoding {
		t.Fatalf("assignment = model %q profile %q", got.Model, got.Profile)
	}

	app.memoryStore.ProjectIndex().SetTaskLLM(path, "local", "pinned-coder")
	pinned := h.codingLLMConfigForSession(userID)
	if pinned.Model != "pinned-coder" || pinned.ProviderID != "local" || pinned.Profile != maclawLLMProfileCoding {
		t.Fatalf("pinned coding = model %q provider %q profile %q", pinned.Model, pinned.ProviderID, pinned.Profile)
	}
	light := h.codingLightweightConfigForSession(userID)
	if light.Model != "pinned-coder" || light.ProviderID != "local" || light.Profile != maclawLLMProfileCoding {
		t.Fatalf("pinned lightweight = model %q provider %q profile %q", light.Model, light.ProviderID, light.Profile)
	}
	if h.codingLightweightConfigForSession("").Model != "assistant-model" {
		t.Fatal("unpinned lightweight used the pin")
	}
	invalidateCodingRouteCapabilitiesCache()
	t.Cleanup(invalidateCodingRouteCapabilitiesCache)
	if model := codingRouteModel(h.codingRouteCapabilities(userID), codingRoutePrefPrimary); model != "pinned-coder" {
		t.Fatalf("pinned route primary = %q", model)
	}
	if model := codingRouteModel(h.codingRouteCapabilities(""), codingRoutePrefPrimary); model != "assistant-model" {
		t.Fatalf("assignment route primary = %q", model)
	}
	if h.codingLLMConfigForSession("").Model != "assistant-model" {
		t.Fatal("a session without a task used the pin")
	}
	if h.getCodingLLMConfig().Model != "assistant-model" {
		t.Fatal("the coding assignment followed the task pin")
	}

	injected := corelib.MaclawLLMConfig{Model: "standalone-model", Profile: maclawLLMProfileCoding}
	h.standaloneConfig = &StandaloneConfig{LLMConfigFunc: func() corelib.MaclawLLMConfig { return injected }}
	if got := h.codingLLMConfigForSession(userID); got.Model != "standalone-model" {
		t.Fatalf("standalone injector = %q", got.Model)
	}
	h.standaloneConfig = nil

	app.memoryStore.ProjectIndex().SetTaskLLM(path, "missing", "gone")
	if got := h.codingLLMConfigForSession(userID); got.Model != "assistant-model" || got.Profile != maclawLLMProfileCoding {
		t.Fatalf("missing provider = model %q profile %q", got.Model, got.Profile)
	}
}

func codingRouteModel(caps []codingRouteCapability, pref string) string {
	for _, cap := range caps {
		if cap.Pref == pref {
			return cap.Model
		}
	}
	return ""
}

func newTaskLLMTestApp(t *testing.T) *App {
	t.Helper()
	app := newProjectSearchTestApp(t)
	if err := app.SaveConfig(corelib.AppConfig{
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
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.ensureMemoryStore()
	if app.memoryStore == nil || app.memoryStore.ProjectIndex() == nil {
		t.Fatal("memory store was not initialized")
	}
	return app
}
