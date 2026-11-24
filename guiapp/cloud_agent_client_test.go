package guiapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCreateCloudAgentAppliesDesktopDefaultLLM(t *testing.T) {
	var llmBody map[string]any
	var llmCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/cloud-agents":
			_, _ = w.Write([]byte(`{"id":"cag_new","name":"任务","kind":"session","status":"idle","llm_mode":"official"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/llm"):
			llmCalled = true
			if err := json.NewDecoder(r.Body).Decode(&llmBody); err != nil {
				t.Fatalf("decode llm body: %v", err)
			}
			_, _ = w.Write([]byte(`{"id":"cag_new","name":"任务","kind":"session","status":"idle","llm_mode":"custom","llm_label":"DeepSeek","llm_model":"deepseek-v3"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token", RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "prov-1", Name: "DeepSeek", URL: "https://api.deepseek.com", Key: "sk-test", Model: "deepseek-chat"},
		},
		CloudAgentLLMProvider: "prov-1",
		CloudAgentLLMModel:    "deepseek-v3",
	}); err != nil {
		t.Fatal(err)
	}

	row, err := app.CreateCloudAgent("任务", "session")
	if err != nil {
		t.Fatal(err)
	}
	if !llmCalled {
		t.Fatalf("default llm was not applied to the new instance")
	}
	if got := llmBody["url"]; got != "https://api.deepseek.com" {
		t.Fatalf("llm url=%v", got)
	}
	if got := llmBody["key"]; got != "sk-test" {
		t.Fatalf("llm key=%v", got)
	}
	if got := llmBody["model"]; got != "deepseek-v3" {
		t.Fatalf("llm model=%v", got)
	}
	if got := llmBody["label"]; got != "DeepSeek" {
		t.Fatalf("llm label=%v", got)
	}
	if row.LLMMode != "custom" || row.LLMModel != "deepseek-v3" {
		t.Fatalf("row mode/model=%q/%q, want custom/deepseek-v3", row.LLMMode, row.LLMModel)
	}
}

func TestCreateCloudAgentKeepsOfficialWithoutUsableDefault(t *testing.T) {
	// The stored provider id matches no row: the default cannot resolve, so
	// the new instance must stay official with no llm call at all.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/cloud-agents" {
			_, _ = w.Write([]byte(`{"id":"cag_new","name":"任务","kind":"session","status":"idle","llm_mode":"official"}`))
			return
		}
		t.Errorf("unexpected hub call %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token", RemoteMachineToken: "machine-token", RemoteMachineID: "machine-test",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "prov-1", Name: "DeepSeek", URL: "https://api.deepseek.com", Key: "sk-test", Model: "deepseek-chat"},
		},
		CloudAgentLLMProvider: "prov-deleted",
	}); err != nil {
		t.Fatal(err)
	}

	row, err := app.CreateCloudAgent("任务", "session")
	if err != nil {
		t.Fatal(err)
	}
	if row.LLMMode != "official" {
		t.Fatalf("row mode=%q, want official", row.LLMMode)
	}
}

func TestCloudAgentDefaultLLMSkipsHubServiceAndKeylessRows(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "hub-row", Name: "Hub 内置服务", URL: "https://hub.example", Key: "k", Model: "m", IsHubService: true},
			{ID: "keyless", Name: "NoKey", URL: "https://api.example.com", Key: "", Model: "m"},
		},
		CloudAgentLLMProvider: "hub-row",
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := app.cloudAgentDefaultLLM(); ok {
		t.Fatalf("hub-service provider must not become the default")
	}

	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{ID: "keyless", Name: "NoKey", URL: "https://api.example.com", Key: "", Model: "m"},
		},
		CloudAgentLLMProvider: "keyless",
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := app.cloudAgentDefaultLLM(); ok {
		t.Fatalf("keyless provider must not become the default")
	}
}
