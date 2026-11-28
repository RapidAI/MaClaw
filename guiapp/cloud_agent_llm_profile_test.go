package guiapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// cloudLLMTestProviders is one tested desktop provider pair whose assigned
// models equal the stored provider models, so a save never fires a live
// vision probe (providerVisionTestStatus answers "unsupported" for it).
func cloudLLMTestProviders() []corelib.MaclawLLMProvider {
	return []corelib.MaclawLLMProvider{
		{ID: "provider-a", Name: "Provider A", URL: "https://a.example.test/v1", Key: "a-key", Model: "model-a", ConnectionTestPassed: true},
		{ID: "provider-b", Name: "Provider B", URL: "https://b.example.test/v1", Key: "b-key", Model: "model-b", ConnectionTestPassed: true},
	}
}

func TestCloudAgentDefaultLLMFollowsAssistantByDefault(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: cloudLLMTestProviders(),
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Never configured: the new default follows the assistant's live selection.
	def, ok := app.cloudAgentDefaultLLM()
	if !ok {
		t.Fatalf("unconfigured cloud default must follow the assistant")
	}
	if def.URL != "https://a.example.test/v1" || def.Key != "a-key" || def.Model != "model-a" || def.Label != "Provider A" {
		t.Fatalf("default = %#v", def)
	}
}

func TestCloudAgentDefaultLLMFollowFallsBackToOfficialForHubAssistant(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	providers := cloudLLMTestProviders()
	providers[0].IsHubService = true
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: providers,
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// A hub-service assistant cannot be pushed as a custom cloud endpoint;
	// the instances stay on the Hub official model.
	if _, ok := app.cloudAgentDefaultLLM(); ok {
		t.Fatalf("hub-service assistant must fall back to the official model")
	}
}

func TestCloudAgentDefaultLLMExplicitOfficialPin(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: cloudLLMTestProviders(),
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
			Cloud:     &corelib.MaclawLLMProfile{InheritAssistant: false},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// The explicit empty pin is MaClaw official, not the follow default.
	if _, ok := app.cloudAgentDefaultLLM(); ok {
		t.Fatalf("explicit official pin must keep instances official")
	}
}

func TestCloudAgentDefaultLLMSeedsLegacyKeysWithProfiles(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: cloudLLMTestProviders(),
		// A config that already carries the profiles struct plus a pre-migration
		// cloud pick: the read-time seed keeps it instead of flipping to follow.
		CloudAgentLLMProvider: "provider-b",
		CloudAgentLLMModel:    "model-b-pinned",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	def, ok := app.cloudAgentDefaultLLM()
	if !ok {
		t.Fatalf("legacy cloud pick must survive the profiles migration")
	}
	if def.Model != "model-b-pinned" || def.Label != "Provider B" {
		t.Fatalf("default = %#v", def)
	}
}

func TestCloudAgentDefaultLLMResolvesLegacyNamePicks(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	// The legacy settings panel stored the display name when the row had no
	// durable ID yet; the seeded profile must still resolve it.
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:    cloudLLMTestProviders(),
		CloudAgentLLMProvider: "Provider B",
		CloudAgentLLMModel:    "model-b",
	}); err != nil {
		t.Fatal(err)
	}

	def, ok := app.cloudAgentDefaultLLM()
	if !ok {
		t.Fatalf("legacy name pick must resolve")
	}
	if def.URL != "https://b.example.test/v1" || def.Model != "model-b" {
		t.Fatalf("default = %#v", def)
	}
}

func TestCloudAgentLegacySeedNormalizesNamePickWithProviderModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	// The old panel stored the raw display name and left the model empty
	// (= provider default). The seed must materialize a row that passes the
	// assignment contract: durable read ID plus the provider's stored model.
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:    cloudLLMTestProviders(),
		CloudAgentLLMProvider: "Provider B",
		CloudAgentLLMModel:    "",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	if state.Profiles.Cloud == nil {
		t.Fatalf("legacy name pick must seed a cloud row")
	}
	if state.Profiles.Cloud.ProviderID != "provider-b" || state.Profiles.Cloud.Model != "model-b" || state.Profiles.Cloud.InheritAssistant {
		t.Fatalf("seeded cloud = %#v, want provider-b/model-b non-inherit", state.Profiles.Cloud)
	}
}

func TestCloudAgentLegacySeedSkipsIneligiblePicks(t *testing.T) {
	seededCloud := func(pick string, providers []corelib.MaclawLLMProvider) *corelib.MaclawLLMProfile {
		app := &App{testHomeDir: t.TempDir()}
		if err := app.SaveConfig(corelib.AppConfig{
			MaclawLLMProviders:    providers,
			CloudAgentLLMProvider: pick,
			MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
				Version:   maclawLLMProfilesVersion,
				Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
				Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
			},
		}); err != nil {
			t.Fatal(err)
		}
		state, err := app.GetMaclawLLMProfilePanelState()
		if err != nil {
			t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
		}
		return state.Profiles.Cloud
	}

	// A pick that predates the connection-test contract must NOT materialize:
	// the seeded row would fail save validation and block every later save.
	if cloud := seededCloud("provider-untested", []corelib.MaclawLLMProvider{
		{ID: "provider-a", Name: "Provider A", URL: "https://a.example.test/v1", Key: "a-key", Model: "model-a", ConnectionTestPassed: true},
		{ID: "provider-untested", Name: "Untested", URL: "https://x.example.test/v1", Key: "x-key", Model: "model-x"},
	}); cloud != nil {
		t.Fatalf("untested legacy pick seeded %#v, want nil (follow default)", cloud)
	}

	// Hub-service rows can never serve a custom cloud endpoint. The loader
	// recomputes IsHubService from the canonical name, so the fixture must
	// use one of the hubServiceProviderNameAliases.
	if cloud := seededCloud("hub-row", []corelib.MaclawLLMProvider{
		{ID: "provider-a", Name: "Provider A", URL: "https://a.example.test/v1", Key: "a-key", Model: "model-a", ConnectionTestPassed: true},
		{ID: "hub-row", Name: "MaClaw Official", URL: "https://hub.example.test", Key: "k", Model: "model-hub", ConnectionTestPassed: true, IsHubService: true},
	}); cloud != nil {
		t.Fatalf("hub-service legacy pick seeded %#v, want nil (follow default)", cloud)
	}

	// Deleted provider: the pick cannot resolve, so nothing is seeded.
	if cloud := seededCloud("prov-deleted", cloudLLMTestProviders()); cloud != nil {
		t.Fatalf("deleted-provider legacy pick seeded %#v, want nil", cloud)
	}
}

func TestGetMaclawLLMProfilePanelStateCloudSummary(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	panelState := func(profiles *corelib.MaclawLLMProfiles) MaclawLLMProfilePanelState {
		app := &App{testHomeDir: t.TempDir()}
		if err := app.SaveConfig(corelib.AppConfig{
			MaclawLLMProviders: cloudLLMTestProviders(),
			MaclawLLMProfiles:  profiles,
		}); err != nil {
			t.Fatal(err)
		}
		state, err := app.GetMaclawLLMProfilePanelState()
		if err != nil {
			t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
		}
		return state
	}
	base := func() *corelib.MaclawLLMProfiles {
		return &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		}
	}

	// Never configured: follow-assistant resolves the assistant's live pick.
	if state := panelState(base()); !state.Cloud.InheritAssistant ||
		state.Cloud.ProviderName != "Provider A" || state.Cloud.Model != "model-a" || !state.Cloud.Configured {
		t.Fatalf("follow cloud summary = %#v", state.Cloud)
	}

	// Custom pick: the summary names the row's provider and pinned model.
	state := panelState(&corelib.MaclawLLMProfiles{
		Version:   maclawLLMProfilesVersion,
		Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
		Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		Cloud:     &corelib.MaclawLLMProfile{ProviderID: "provider-b", Model: "model-b"},
	})
	if state.Cloud.InheritAssistant || state.Cloud.ProviderName != "Provider B" || state.Cloud.Model != "model-b" || !state.Cloud.Configured {
		t.Fatalf("custom cloud summary = %#v", state.Cloud)
	}

	// Explicit MaClaw official pin: nothing resolvable to show.
	state = panelState(&corelib.MaclawLLMProfiles{
		Version:   maclawLLMProfilesVersion,
		Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
		Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		Cloud:     &corelib.MaclawLLMProfile{InheritAssistant: false},
	})
	if state.Cloud.InheritAssistant || state.Cloud.ProviderName != "" || state.Cloud.Configured {
		t.Fatalf("official cloud summary = %#v", state.Cloud)
	}
}

// cloudLLMFanoutHub is the stub Hub for post-save fan-out tests. It records
// every per-instance llm POST (custom push) and PATCH (official reset).
type cloudLLMFanoutHub struct {
	server   *httptest.Server
	llmSets  map[string]map[string]any
	resets   []string
	listRows []CloudAgentRow
}

func newCloudLLMFanoutHub(t *testing.T, rows []CloudAgentRow) *cloudLLMFanoutHub {
	t.Helper()
	hub := &cloudLLMFanoutHub{llmSets: map[string]map[string]any{}, listRows: rows}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cloud-agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := struct {
			Instances []CloudAgentRow `json:"instances"`
			Enabled   bool            `json:"enabled"`
		}{Instances: hub.listRows, Enabled: true}
		_ = json.NewEncoder(w).Encode(payload)
	})
	mux.HandleFunc("/api/v1/cloud-agents/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/cloud-agents/"), "/llm")
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode llm body: %v", err)
			}
			hub.llmSets[id] = body
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "llm_mode": "custom"})
		case http.MethodPatch:
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/cloud-agents/")
			hub.resets = append(hub.resets, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "llm_mode": "official"})
		default:
			http.NotFound(w, r)
		}
	})
	hub.server = httptest.NewServer(mux)
	t.Cleanup(hub.server.Close)
	return hub
}

func overrideCloudLLMFanoutHook(t *testing.T) chan struct{} {
	t.Helper()
	done := make(chan struct{}, 4)
	orig := applyCloudAgentLLMDefaultAfterProfilesChangeFn
	applyCloudAgentLLMDefaultAfterProfilesChangeFn = func(a *App) {
		a.applyCloudAgentLLMDefaultAfterProfilesChange()
		done <- struct{}{}
	}
	t.Cleanup(func() { applyCloudAgentLLMDefaultAfterProfilesChangeFn = orig })
	return done
}

func waitCloudLLMFanout(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("cloud default fan-out did not run after the save")
	}
}

func TestSaveMaclawLLMProfilesFansOutFollowedAssistantToInstances(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	hub := newCloudLLMFanoutHub(t, []CloudAgentRow{
		{ID: "a1", Name: "builder", Kind: "coding", Status: "running", LLMMode: "custom", LLMLabel: "Provider A", LLMModel: "model-a"},
		{ID: "a2", Name: "chat", Kind: "session", Status: "idle", LLMMode: "official"},
	})
	done := overrideCloudLLMFanoutHook(t)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:          hub.server.URL,
		RemoteViewerToken:     "viewer-token",
		RemoteMachineToken:    "machine-token",
		RemoteMachineID:       "machine-test",
		MaclawLLMProviders:    cloudLLMTestProviders(),
		MaclawLLMThinkingMode: "high",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	updated := state.Profiles
	updated.Assistant.ProviderID = "provider-b"
	updated.Assistant.Model = "model-b"
	if err := app.SaveMaclawLLMProfiles(updated, state.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles: %v", err)
	}
	waitCloudLLMFanout(t, done)

	// Follow is live: BOTH instances take the assistant's new model, official
	// rows included (the same rule the old settings panel applied).
	if len(hub.llmSets) != 2 {
		t.Fatalf("llm pushes = %v, want both instances", hub.llmSets)
	}
	for _, id := range []string{"a1", "a2"} {
		body := hub.llmSets[id]
		if body["model"] != "model-b" || body["label"] != "Provider B" || body["key"] != "b-key" {
			t.Fatalf("llm body for %s = %#v", id, body)
		}
		if body["thinking_mode"] != "high" {
			t.Fatalf("llm thinking_mode for %s = %#v", id, body)
		}
	}
	if len(hub.resets) != 0 {
		t.Fatalf("unexpected resets = %v", hub.resets)
	}
}

func TestSaveMaclawLLMProfilesFansOutOfficialPinResetToCustomInstances(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	hub := newCloudLLMFanoutHub(t, []CloudAgentRow{
		{ID: "a1", Name: "builder", Kind: "coding", Status: "running", LLMMode: "custom", LLMLabel: "Provider A", LLMModel: "model-a"},
		{ID: "a2", Name: "chat", Kind: "session", Status: "idle", LLMMode: "official"},
	})
	done := overrideCloudLLMFanoutHook(t)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.server.URL,
		RemoteViewerToken:  "viewer-token",
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "machine-test",
		MaclawLLMProviders: cloudLLMTestProviders(),
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
			Cloud:     &corelib.MaclawLLMProfile{ProviderID: "provider-b", Model: "model-b"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	updated := state.Profiles
	// Pin the explicit MaClaw official choice: the fingerprint flips custom →
	// official, so only the custom instance is reset.
	updated.Cloud = &corelib.MaclawLLMProfile{InheritAssistant: false}
	if err := app.SaveMaclawLLMProfiles(updated, state.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles: %v", err)
	}
	waitCloudLLMFanout(t, done)

	if len(hub.resets) != 1 || hub.resets[0] != "a1" {
		t.Fatalf("resets = %v, want only the custom instance a1", hub.resets)
	}
	if len(hub.llmSets) != 0 {
		t.Fatalf("unexpected llm pushes = %v", hub.llmSets)
	}
}

func TestSaveMaclawLLMProfilesSkipsFanOutWithoutHubSession(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	// No RemoteHubURL/RemoteMachineID: the fan-out must short-circuit instead
	// of dialing anything.
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: cloudLLMTestProviders(),
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Run the hook synchronously: it must return without touching a Hub.
	app.applyCloudAgentLLMDefaultAfterProfilesChange()

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	updated := state.Profiles
	updated.Assistant.ProviderID = "provider-b"
	updated.Assistant.Model = "model-b"
	if err := app.SaveMaclawLLMProfiles(updated, state.Revision); err != nil {
		t.Fatalf("SaveMaclawLLMProfiles: %v", err)
	}
}

func TestSaveMaclawLLMProfilesRejectsHubServiceCloudProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "provider-a", Name: "Provider A", URL: "https://a.example.test/v1", Key: "a-key", Model: "model-a", ConnectionTestPassed: true},
			{ID: "hub-row", Name: "MaClaw Official", URL: "https://hub.example.test", Key: "k", Model: "model-hub", ConnectionTestPassed: true, IsHubService: true},
			{ID: "provider-untested", Name: "Untested", URL: "https://x.example.test/v1", Key: "x-key", Model: "model-x"},
		},
		MaclawLLMCurrentProvider: "Provider A",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}

	// A hub-service row points back at this Hub's own API and cannot serve a
	// custom cloud endpoint, even with a passed test.
	updated := state.Profiles
	updated.Cloud = &corelib.MaclawLLMProfile{ProviderID: "hub-row", Model: "model-hub"}
	err = app.SaveMaclawLLMProfiles(updated, state.Revision)
	if err == nil || !strings.Contains(err.Error(), "Hub service") {
		t.Fatalf("hub-service cloud pick err = %v, want a Hub-service rejection", err)
	}

	// A provider that never passed a connection test is equally rejected.
	updated = state.Profiles
	updated.Cloud = &corelib.MaclawLLMProfile{ProviderID: "provider-untested", Model: "model-x"}
	err = app.SaveMaclawLLMProfiles(updated, state.Revision)
	if err == nil || !strings.Contains(err.Error(), "has not passed a connection test") {
		t.Fatalf("untested cloud pick err = %v, want a connection-test rejection", err)
	}

	// The explicit official pin stays valid.
	updated = state.Profiles
	updated.Cloud = &corelib.MaclawLLMProfile{InheritAssistant: false}
	if err := app.SaveMaclawLLMProfiles(updated, state.Revision); err != nil {
		t.Fatalf("official cloud pin: %v", err)
	}
}

func TestGetMaclawLLMProfilePanelStateSeedsCloudFromLegacyKeys(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders:    cloudLLMTestProviders(),
		CloudAgentLLMProvider: "provider-b",
		CloudAgentLLMModel:    "model-b",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := app.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	if state.Profiles.Cloud == nil || state.Profiles.Cloud.ProviderID != "provider-b" || state.Profiles.Cloud.InheritAssistant {
		t.Fatalf("panel cloud = %#v, want the seeded legacy pick", state.Profiles.Cloud)
	}

	// No legacy keys and no saved row: cloud stays nil — the UI renders the
	// follow default without materializing a row.
	other := &App{testHomeDir: t.TempDir()}
	if err := other.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: cloudLLMTestProviders(),
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "provider-a", Model: "model-a"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
	otherState, err := other.GetMaclawLLMProfilePanelState()
	if err != nil {
		t.Fatalf("GetMaclawLLMProfilePanelState: %v", err)
	}
	if otherState.Profiles.Cloud != nil {
		t.Fatalf("panel cloud = %#v, want nil for the follow default", otherState.Profiles.Cloud)
	}
}
