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
	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/cloud-agents":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
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
			{ID: "prov-1", Name: "DeepSeek", URL: "https://api.deepseek.com", Key: "sk-test", Model: "deepseek-chat", Protocol: "openai", AgentType: "openclaw", ContextLength: 128000, TimeoutSec: 300, MaxOutputTokens: 8192},
		},
		CloudAgentLLMProvider: "prov-1",
		CloudAgentLLMModel:    "deepseek-v3",
		MaclawLLMThinkingMode: "high",
	}); err != nil {
		t.Fatal(err)
	}

	row, err := app.CreateCloudAgent("任务", "session")
	if err != nil {
		t.Fatal(err)
	}
	// The thinking level rides BOTH calls: the create payload covers an
	// official-mode instance (no default llm applied), the llm payload a
	// custom one. Without it the cloud turn sends no reasoning field and
	// shows no 思考, unlike the same provider locally.
	if got := createBody["thinking_mode"]; got != "high" {
		t.Fatalf("create thinking_mode=%v", got)
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
	// The wire fields must ride along or the srv runs every provider as
	// openai chat/completions — an anthropic endpoint then answers a shape
	// the loop reports as "LLM returned no choices".
	if got := llmBody["protocol"]; got != "openai" {
		t.Fatalf("llm protocol=%v", got)
	}
	if got := llmBody["agent_type"]; got != "openclaw" {
		t.Fatalf("llm agent_type=%v", got)
	}
	if got := llmBody["context_length"]; got != float64(128000) {
		t.Fatalf("llm context_length=%v", got)
	}
	if got := llmBody["timeout_sec"]; got != float64(300) {
		t.Fatalf("llm timeout_sec=%v", got)
	}
	if got := llmBody["max_output_tokens"]; got != float64(8192) {
		t.Fatalf("llm max_output_tokens=%v", got)
	}
	if got := llmBody["thinking_mode"]; got != "high" {
		t.Fatalf("llm thinking_mode=%v", got)
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
