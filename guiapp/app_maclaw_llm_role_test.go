package guiapp

import (
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
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

func TestResolveMaclawLLMRoleForegroundFollowsThirdPartyAssistant(t *testing.T) {
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
	if fg.Model != "assistant-model" || fg.ProviderID != "local" || fg.Profile != maclawLLMRoleForeground || fg.URL != "https://local.example/v1" {
		t.Fatalf("foreground should follow the switched assistant, got %+v", fg)
	}
	bg, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil {
		t.Fatal(err)
	}
	// An empty background role follows the person's current model.
	if bg.Model != "assistant-model" || bg.ProviderID != "local" || bg.Profile != maclawLLMRoleBackground {
		t.Fatalf("background = %+v", bg)
	}
	cu, err := app.ResolveMaclawLLMRole(maclawLLMRoleComputerUse)
	if err != nil {
		t.Fatal(err)
	}
	// The assistant sees images, so an empty computer-use role follows it.
	if cu.Model != "assistant-model" || !cu.SupportsVision || cu.Profile != maclawLLMRoleComputerUse {
		t.Fatalf("computer_use = model %q vision %v profile %q", cu.Model, cu.SupportsVision, cu.Profile)
	}
	hub, err := app.ResolveHubLLMRole(maclawLLMRoleForeground)
	if err != nil || hub.ProviderID != "hub" || hub.Model != "auto" {
		t.Fatalf("hub foreground = %+v err %v", hub, err)
	}
}

// A text-only assistant leaves an empty computer-use role on the Hub.
func TestResolveMaclawLLMRoleComputerUseTextAssistantKeepsHub(t *testing.T) {
	providers := roleTestProviders()
	providers[1].SupportsVision = false
	providers[1].VisionModels = nil
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       providers,
		MaclawLLMCurrentProvider: "Local",
		HubRoleModels:            corelib.MaclawLLMHubRoleModels{Foreground: "auto", Background: "auto", ComputerUse: "auto"},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	cu, err := app.ResolveMaclawLLMRole(maclawLLMRoleComputerUse)
	if err != nil || cu.ProviderID != "hub" || cu.Model != "auto" {
		t.Fatalf("computer_use = %+v err %v", cu, err)
	}
	bg, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil || bg.ProviderID != "local" {
		t.Fatalf("background = %+v err %v", bg, err)
	}
}

func TestResolveMaclawLLMRoleProxyAutoIsNotOfficial(t *testing.T) {
	providers := append(roleTestProviders(), corelib.MaclawLLMProvider{
		ID: "proxy", Name: "Proxy", URL: "https://proxy.example/api/llm/v1", Key: "key",
		Model: "auto", Protocol: "openai", ConnectionTestPassed: true,
	})
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       providers,
		MaclawLLMCurrentProvider: "Proxy",
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground: "official-high",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "proxy", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.ProviderID != "proxy" || fg.Model != "auto" || fg.URL != "https://proxy.example/api/llm/v1" || fg.SupportsVision {
		t.Fatalf("proxy auto must stay on that provider without Hub vision, got %+v", fg)
	}
}

func TestResolveMaclawLLMRoleProxyAutoWithoutSnapshotStaysText(t *testing.T) {
	providers := []corelib.MaclawLLMProvider{
		{
			ID: "hub", Name: hubServiceProviderName, IsHubService: true,
			URL: "https://hub.example/api/llm/v1", Key: "viewer", Model: "auto",
			Protocol: "openai", ConnectionTestPassed: true,
		},
		{
			ID: "proxy", Name: "Proxy", URL: "https://proxy.example/api/llm/v1", Key: "key",
			Model: "auto", Protocol: "openai", ConnectionTestPassed: true,
		},
	}
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       providers,
		MaclawLLMCurrentProvider: "Proxy",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "proxy", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	for _, role := range []string{maclawLLMRoleForeground, maclawLLMRoleBackground, maclawLLMRoleComputerUse} {
		resolved, err := app.ResolveMaclawLLMRole(role)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.ProviderID != "proxy" || resolved.Model != "auto" || resolved.SupportsVision {
			t.Fatalf("%s fallback borrowed Hub vision: %+v", role, resolved)
		}
	}
}

func TestResolveMaclawLLMRoleOfficialAutoWithoutSnapshotKeepsVision(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: hubServiceProviderName,
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.ProviderID != "hub" || fg.Model != "auto" || !fg.SupportsVision {
		t.Fatalf("official auto without a snapshot should still send images, got %+v", fg)
	}
}

func TestResolveMaclawLLMRolePinnedProxyAutoWithoutHubStaysText(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "proxy", Name: "Proxy", URL: "https://proxy.example/api/llm/v1", Key: "key",
			Model: "auto", Protocol: "openai", ConnectionTestPassed: true,
		}},
		MaclawLLMCurrentProvider: "Proxy",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:    maclawLLMProfilesVersion,
			Assistant:  corelib.MaclawLLMProfile{ProviderID: "proxy", Model: "assistant-model"},
			Coding:     corelib.MaclawLLMProfile{InheritAssistant: true},
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false), Model: "auto"},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.ProviderID != "proxy" || fg.Model != "auto" || fg.SupportsVision {
		t.Fatalf("pinned proxy auto = %+v", fg)
	}
}

func TestResolveMaclawLLMRoleOfficialAssistantKeepsHubSnapshot(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: hubServiceProviderName,
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground: "official-high",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.Model != "official-high" || fg.ProviderID != "hub" {
		t.Fatalf("official auto should keep the Hub foreground band, got %+v", fg)
	}
}

func TestSkillProxyUsesAIAssistantUpstream(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: hubServiceProviderName,
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground: "official-high",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})

	profile := app.GetMaclawLLMConfig()
	if profile.Model != "auto" {
		t.Fatalf("assistant profile model = %q, want auto", profile.Model)
	}
	proxyCfg := app.skillOpenAIProxyConfig(false)
	if proxyCfg.Model != "official-high" || proxyCfg.URL != "https://hub.example/api/llm/v1" || proxyCfg.Key != "viewer" {
		t.Fatalf("proxy scalars = url %q model %q key %q", proxyCfg.URL, proxyCfg.Model, proxyCfg.Key)
	}
	if proxyCfg.Upstream.Model != "official-high" || proxyCfg.Upstream.ProviderName != hubServiceProviderName || proxyCfg.Upstream.ProviderID != "hub" {
		t.Fatalf("proxy upstream = %+v", proxyCfg.Upstream)
	}
	if proxyCfg.DecorateUpstream == nil {
		t.Fatal("proxy is missing the assistant header decorator")
	}
	if proxyCfg.BaseTransport != skillProxyUpstreamTransport {
		t.Fatal("proxy is not using the shared assistant proxy transport")
	}

	thirdParty := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	localCfg := thirdParty.skillOpenAIProxyConfig(false)
	if localCfg.Model != "assistant-model" || localCfg.Upstream.ProviderName != "Local" || localCfg.Upstream.Key != "key" || localCfg.URL != "https://local.example/v1" {
		t.Fatalf("third-party proxy = url %q model %q upstream %+v", localCfg.URL, localCfg.Model, localCfg.Upstream)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.x.ai/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	decorateSkillProxyLikeAssistant(req, corelib.MaclawLLMConfig{ProviderName: "xAI-Grok", AuthType: "oauth"})
	if got := req.Header.Get("X-XAI-Token-Auth"); got != "xai-grok-cli" {
		t.Fatalf("X-XAI-Token-Auth = %q, want the assistant OAuth marker", got)
	}

	profileWB := workbuddy.ChinaProfile()
	workBuddy := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			ID: "wb", Name: profileWB.Name, URL: profileWB.ChatURL, Key: "wb-token",
			Model: profileWB.DefaultModel, Protocol: "openai", AuthType: "oauth",
			ConnectionTestPassed: true,
		}},
		MaclawLLMCurrentProvider: profileWB.Name,
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "wb", Model: profileWB.DefaultModel},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	wbCfg := workBuddy.skillOpenAIProxyConfig(false)
	wbTransport, ok := wbCfg.BaseTransport.(*workbuddy.Transport)
	if !ok || wbTransport == nil || wbTransport.Base != skillProxyUpstreamTransport {
		t.Fatalf("WorkBuddy proxy transport = %#v, want the assistant rewrite on the shared pool", wbCfg.BaseTransport)
	}
}

func TestResolveMaclawLLMRoleOfficialBandAliasMatchesSnapshot(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: hubServiceProviderName,
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground: "official-high",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "high"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.Model != "official-high" || fg.ProviderID != "hub" {
		t.Fatalf("short band high is the official-high snapshot, got %+v", fg)
	}
}

func TestResolveMaclawLLMRoleOfficialShortBandOverridesAuto(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: hubServiceProviderName,
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground: "auto",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "mid"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.Model != "mid" || fg.ProviderID != "hub" {
		t.Fatalf("short band mid should replace auto, got %+v", fg)
	}
}

func TestResolveMaclawLLMRoleOfficialModelSwitchWins(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: hubServiceProviderName,
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground: "auto",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "official-mid"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	if fg.Model != "official-mid" || fg.ProviderID != "hub" {
		t.Fatalf("explicit official model should replace the Hub band, got %+v", fg)
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

func TestResolveMaclawLLMRolePinHonorsProviderID(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:    maclawLLMProfilesVersion,
			Assistant:  corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:     corelib.MaclawLLMProfile{InheritAssistant: true},
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false), ProviderID: "local", Model: "vision-model"},
			Background: corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false), ProviderID: "hub", Model: "high"},
		},
	})

	fg, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	// A provider pin wins over the Hub provider a legacy pin would have used.
	if fg.Model != "vision-model" || fg.ProviderID != "local" || fg.URL != "https://local.example/v1" {
		t.Fatalf("provider-pinned foreground = %+v", fg)
	}
	if !fg.SupportsVision {
		t.Fatalf("vision-model on local should be vision-capable: %+v", fg)
	}
	bg, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil {
		t.Fatal(err)
	}
	if bg.ProviderID != "hub" || bg.Model != "high" {
		t.Fatalf("hub-pinned background = %+v", bg)
	}
}

func TestResolveMaclawLLMRolePinRejectsUnknownProvider(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:    maclawLLMProfilesVersion,
			Assistant:  corelib.MaclawLLMProfile{ProviderID: "local", Model: "assistant-model"},
			Coding:     corelib.MaclawLLMProfile{InheritAssistant: true},
			Foreground: corelib.MaclawLLMRoleAssignment{InheritHub: boolPtr(false), ProviderID: "gone", Model: "x"},
		},
	})
	if _, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground); err == nil {
		t.Fatal("expected a pin referencing a missing provider to fail")
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
