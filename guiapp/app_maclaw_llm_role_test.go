package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func newRoleTestApp(t *testing.T, cfg corelib.AppConfig) *App {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	app := &App{testHomeDir: tmp}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return app
}

func roleTestProviders() []corelib.MaclawLLMProvider {
	return []corelib.MaclawLLMProvider{
		{
			ID: "hub", Name: hubServiceProviderName, IsHubService: true,
			URL: "https://hub.example/api/llm/v1", Key: "viewer", Model: "auto",
			Protocol: "openai", ConnectionTestPassed: true,
			VisionModels: []string{"vision-model"},
		},
		{
			ID: "local", Name: "Local", URL: "https://local.example/v1", Key: "key",
			Model: "assistant-model", Protocol: "openai", ConnectionTestPassed: true,
			SupportsVision: true, VisionModels: []string{"vision-model"},
		},
	}
}

func TestResolveMaclawLLMRoleInheritsHubOverThirdPartyAssistant(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground:  "auto",
			Background:  "worker-model",
			ComputerUse: "auto",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})

	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.Model != "auto" || fg.ProviderID != "hub" || fg.Profile != maclawLLMRoleForeground {
		t.Fatalf("foreground = %+v", fg)
	}
	bg, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil {
		t.Fatal(err)
	}
	if bg.Model != "worker-model" || bg.ProviderID != "hub" || bg.Profile != maclawLLMRoleBackground {
		t.Fatalf("background = %+v", bg)
	}
	cu, err := app.ResolveMaclawLLMRole(maclawLLMRoleComputerUse)
	if err != nil {
		t.Fatal(err)
	}
	if cu.Model != "auto" || !cu.SupportsVision || cu.Profile != maclawLLMRoleComputerUse {
		t.Fatalf("computer_use = model %q vision %v profile %q", cu.Model, cu.SupportsVision, cu.Profile)
	}
}

func TestResolveMaclawLLMRoleLocalPinAndSingleModelFallback(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:     maclawLLMProfilesVersion,
			Assistant:   corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:      corelib.MaclawLLMProfile{InheritAssistant: true},
			Background:  corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false), Model: "pinned-worker"},
			ComputerUse: corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false), Model: "vision-model"},
		},
	})

	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.Model != "assistant-model" || fg.ProviderID != "local" || fg.Profile != maclawLLMRoleForeground {
		t.Fatalf("missing hub snapshot should follow assistant, got %+v", fg)
	}
	bg, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil {
		t.Fatal(err)
	}
	if bg.Model != "pinned-worker" || bg.ProviderID != "hub" {
		t.Fatalf("local pin should use the hub provider, got %+v", bg)
	}
	cu, err := app.ResolveMaclawLLMRole(maclawLLMRoleComputerUse)
	if err != nil {
		t.Fatal(err)
	}
	if cu.Model != "vision-model" || !cu.SupportsVision {
		t.Fatalf("pinned vision model = %+v", cu)
	}
}

func TestResolveMaclawLLMRolePinnedNonVisionStaysTextPrimary(t *testing.T) {
	providers := roleTestProviders()
	providers[0].VisionModels = []string{"other-vision"}
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       providers,
		MaclawLLMCurrentProvider: "Local",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:     maclawLLMProfilesVersion,
			Assistant:   corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:      corelib.MaclawLLMProfile{InheritAssistant: true},
			ComputerUse: corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false), Model: "text-only"},
		},
	})
	cu, err := app.ResolveMaclawLLMRole(maclawLLMRoleComputerUse)
	if err != nil {
		t.Fatal(err)
	}
	if cu.SupportsVision {
		t.Fatalf("pinned non-vision model should stay text-primary: %+v", cu)
	}
}

func TestResolveMaclawLLMRolePinUsesAssistantProviderWithoutHub(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "local", Name: "Local", URL: "https://local.example/v1", Key: "key",
			Model: "assistant-model", Protocol: "openai", ConnectionTestPassed: true,
		}},
		MaclawLLMCurrentProvider: "Local",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:    maclawLLMProfilesVersion,
			Assistant:  corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:     corelib.MaclawLLMProfile{InheritAssistant: true},
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false), Model: "pinned-fg"},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.Model != "pinned-fg" || fg.ProviderID != "local" {
		t.Fatalf("pin without hub = %+v", fg)
	}
}

func TestApplyHubLLMServiceStatusStoresRoleModelsWithoutTouchingPins(t *testing.T) {
	inherit := false
	cfg := corelib.AppConfig{
		RemoteViewerToken: "viewer-token",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:    maclawLLMProfilesVersion,
			Assistant:  corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:     corelib.MaclawLLMProfile{InheritAssistant: true},
			Background: corelib.MaclawLLMRoleAssignment{InheritHub: &inherit, Model: "keep-me"},
		},
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "local", Name: "Local", URL: "https://local.example/v1", Key: "key", Model: "assistant-model",
		}},
	}
	app := &App{}
	changed := app.applyHubLLMServiceStatusToConfig(&cfg, HubLLMServiceStatus{
		Active:        true,
		HubLLMBaseURL: "https://hub.example/api/llm/v1",
		RoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground:  "auto",
			Background:  "hub-worker",
			ComputerUse: "auto",
		},
	})
	if !changed {
		t.Fatal("expected hub role snapshot to change config")
	}
	if cfg.HubRoleModels.Background != "hub-worker" || cfg.HubRoleModels.Foreground != "auto" {
		t.Fatalf("snapshot = %+v", cfg.HubRoleModels)
	}
	if cfg.MaclawLLMProfiles.Background.InheritsHub() || cfg.MaclawLLMProfiles.Background.Model != "keep-me" {
		t.Fatalf("local pin changed: %+v", cfg.MaclawLLMProfiles.Background)
	}
}

func TestSaveMaclawLLMProfilesRejectsEmptyLocalRolePin(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "local", Name: "Local", URL: "https://local.example/v1", Key: "key",
			Model: "assistant-model", ConnectionTestPassed: true,
		}},
		MaclawLLMCurrentProvider: "Local",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatal(err)
	}
	profiles := state.Profiles
	profiles.Foreground = corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false)}
	if err := app.SaveMaclawLLMProfiles(profiles, state.Revision); err == nil {
		t.Fatal("expected empty local pin to be rejected")
	}
}
