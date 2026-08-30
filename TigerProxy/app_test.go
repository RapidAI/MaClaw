package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

type tigerProxyUpstreamRequest struct {
	Path    string
	Headers http.Header
	Body    map[string]interface{}
}

func TestNormalizeListenAddressDefaultsToAllInterfaces(t *testing.T) {
	cases := map[string]string{
		"":            "0.0.0.0:18086",
		":18090":      "0.0.0.0:18090",
		"18091":       "0.0.0.0:18091",
		"localhost:7": "0.0.0.0:7",
		"*:18092":     "0.0.0.0:18092",
	}

	for in, want := range cases {
		got, err := normalizeListenAddress(in)
		if err != nil {
			t.Fatalf("normalizeListenAddress(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("normalizeListenAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeSettingsUsesCodexContextDefaultsAndPreservesOverrides(t *testing.T) {
	defaults := normalizeSettings(Settings{})
	if defaults.CodexContextWindow != 199000 || defaults.CodexAutoCompactTokenLimit != 180000 {
		t.Fatalf("Codex defaults = %d/%d, want 199000/180000", defaults.CodexContextWindow, defaults.CodexAutoCompactTokenLimit)
	}

	overrides := normalizeSettings(Settings{
		CodexContextWindow:         256000,
		CodexAutoCompactTokenLimit: 220000,
	})
	if overrides.CodexContextWindow != 256000 || overrides.CodexAutoCompactTokenLimit != 220000 {
		t.Fatalf("Codex overrides = %d/%d, want 256000/220000", overrides.CodexContextWindow, overrides.CodexAutoCompactTokenLimit)
	}
}

func TestSaveSettingsRejectsInvalidCodexCompactionThreshold(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	app := NewApp()
	_, err := app.SaveSettings(Settings{
		CodexContextWindow:         100000,
		CodexAutoCompactTokenLimit: 100000,
	})
	if err == nil || !strings.Contains(err.Error(), "必须小于上下文长度") {
		t.Fatalf("SaveSettings error = %v, want invalid Codex threshold error", err)
	}
}

func TestGenerateAPIKeySynchronizesConfiguredCodexCredential(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := writeSettings(Settings{APIKey: "old-proxy-key"}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := configfile.WriteTigerProxyCodexConfig("old-proxy-key", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatalf("write Codex config: %v", err)
	}

	result, err := NewApp().GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	key := result.APIKey
	if key == "" || key == "old-proxy-key" {
		t.Fatalf("generated key = %q, want a new non-empty key", key)
	}
	if !result.CodexCredentialSync.Configured || !result.CodexCredentialSync.Updated || result.CodexCredentialSync.Error != "" {
		t.Fatalf("Codex sync = %+v, want configured and updated without error", result.CodexCredentialSync)
	}
	auth, err := configfile.ReadCodexAuth()
	if err != nil {
		t.Fatalf("read Codex auth: %v", err)
	}
	if got, _ := auth["OPENAI_API_KEY"].(string); got != key {
		t.Fatalf("Codex key = %q, want generated key", got)
	}
}

func TestSaveSettingsSynchronizesConfiguredCodexCredentialAfterManualKeyChange(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	if err := writeSettings(Settings{APIKey: "old-proxy-key"}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	if err := configfile.WriteTigerProxyCodexConfig("old-proxy-key", "http://127.0.0.1:18086/v1", "gpt-5.5"); err != nil {
		t.Fatalf("write Codex config: %v", err)
	}

	status, err := NewApp().SaveSettings(Settings{APIKey: "manual-new-proxy-key"})
	if err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if status.CodexCredentialSync == nil || !status.CodexCredentialSync.Configured || !status.CodexCredentialSync.Updated || status.CodexCredentialSync.Error != "" {
		t.Fatalf("Codex sync = %+v, want configured and updated without error", status.CodexCredentialSync)
	}
	auth, err := configfile.ReadCodexAuth()
	if err != nil {
		t.Fatalf("read Codex auth: %v", err)
	}
	if got, _ := auth["OPENAI_API_KEY"].(string); got != "manual-new-proxy-key" {
		t.Fatalf("Codex key = %q, want manually saved key", got)
	}
}

func TestSaveSettingsRollsBackPersistedSettingsWhenRestartFails(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	if err := writeSettings(Settings{ListenAddress: "127.0.0.1:0", APIKey: "old-key"}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer occupied.Close()

	app := NewApp()
	_, err = app.SaveSettings(Settings{ListenAddress: occupied.Addr().String(), APIKey: "new-key"})
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("SaveSettings error = %v, want listener error", err)
	}
	if app.isRunning() {
		t.Fatal("proxy started despite a failed replacement listener")
	}
	saved, err := loadSettings()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if saved.ListenAddress != "127.0.0.1:0" || saved.APIKey != "old-key" {
		t.Fatalf("settings after failed restart = %+v, want original listener and key", saved)
	}
}

func TestSaveSettingsKeepsRunningProxyAndSettingsWhenReplacementBindFails(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	app := NewApp()
	if err := app.restartProxy(Settings{ListenAddress: "127.0.0.1:0", APIKey: "old-key"}); err != nil {
		t.Fatalf("start initial proxy: %v", err)
	}
	defer app.stopProxy()
	oldURL := tigerProxyTestBaseURL(t, app)
	if err := writeSettings(Settings{ListenAddress: oldURL[len("http://"):], APIKey: "old-key"}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer occupied.Close()

	if _, err := app.SaveSettings(Settings{ListenAddress: occupied.Addr().String(), APIKey: "new-key"}); err == nil {
		t.Fatal("SaveSettings succeeded with an occupied listener")
	}
	if !app.isRunning() || tigerProxyTestBaseURL(t, app) != oldURL {
		t.Fatal("running proxy changed after failed replacement")
	}
	saved, err := loadSettings()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if saved.ListenAddress != oldURL[len("http://"):] || saved.APIKey != "old-key" {
		t.Fatalf("settings after failed restart = %+v, want original settings", saved)
	}
}

func TestSaveSettingsRestartsOnSameAddressForUpstreamChanges(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	app := NewApp()
	if err := app.restartProxy(Settings{ListenAddress: "127.0.0.1:0", APIKey: "local-key"}); err != nil {
		t.Fatalf("start initial proxy: %v", err)
	}
	defer app.stopProxy()
	if err := writeSettings(Settings{ListenAddress: "127.0.0.1:0", APIKey: "local-key"}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if _, err := app.SaveSettings(Settings{
		ListenAddress: "127.0.0.1:0",
		APIKey:        "local-key",
		AccessToken:   "updated-upstream-token",
		BaseURL:       "https://example.com",
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if !app.isRunning() {
		t.Fatal("proxy did not restart successfully on the existing listener")
	}
	saved, err := loadSettings()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if saved.AccessToken != "updated-upstream-token" || saved.BaseURL != "https://example.com" {
		t.Fatalf("settings = %+v, want updated upstream credentials", saved)
	}
}

func TestUpdateModelsForCurrentTokenOnlyUpdatesModels(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	if err := writeSettings(Settings{
		APIKey:      "new-user-key",
		AccessToken: "current-token",
		BaseURL:     "https://current.example",
		ModelID:     "current-model",
		Models:      []ModelOption{{ID: "old", Name: "Old"}},
	}); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	app := NewApp()
	if !app.updateModelsForCurrentToken("current-token", []ModelOption{{ID: "fresh", Name: "Fresh"}}) {
		t.Fatal("model refresh update was not applied")
	}
	after, err := loadSettings()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if after.APIKey != "new-user-key" || after.BaseURL != "https://current.example" || after.ModelID != "current-model" {
		t.Fatalf("credential fields were overwritten: %+v", after)
	}
	if len(after.Models) != 1 || after.Models[0].ID != "fresh" {
		t.Fatalf("models = %+v, want refreshed list", after.Models)
	}
	if app.updateModelsForCurrentToken("stale-token", []ModelOption{{ID: "wrong", Name: "Wrong"}}) {
		t.Fatal("stale token unexpectedly overwrote the model list")
	}
}

func TestRestartProxyKeepsCurrentServerRunningWhenReplacementBindFails(t *testing.T) {
	app := NewApp()
	if err := app.restartProxy(Settings{ListenAddress: "127.0.0.1:0", APIKey: "old-key"}); err != nil {
		t.Fatalf("start initial proxy: %v", err)
	}
	defer app.stopProxy()
	oldURL := tigerProxyTestBaseURL(t, app)

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer occupied.Close()

	if err := app.restartProxy(Settings{ListenAddress: occupied.Addr().String(), APIKey: "new-key"}); err == nil {
		t.Fatal("restartProxy succeeded with an occupied listener")
	}
	if !app.isRunning() {
		t.Fatal("initial proxy stopped after failed replacement bind")
	}
	if got := tigerProxyTestBaseURL(t, app); got != oldURL {
		t.Fatalf("running proxy URL = %q, want %q", got, oldURL)
	}
}

func TestNormalizeModelOptionsStripsProviderPrefixAndDeduplicates(t *testing.T) {
	models := normalizeModelOptions([]ModelOption{
		{ID: "qax-codegen/Qwen-Flash", Name: "Qwen Flash", ContextWindow: 1000},
		{ID: "Qwen-Flash", Name: "duplicate", ContextWindow: 2000},
		{ID: " qax-codegen/Claude-Sonnet ", Name: "", ContextWindow: 3000},
		{ID: "", Name: "ignored"},
	})

	if len(models) != 2 {
		t.Fatalf("model count = %d, want 2: %+v", len(models), models)
	}
	if models[0].ID != "Qwen-Flash" || models[0].Name != "Qwen Flash" || models[0].ContextWindow != 1000 {
		t.Fatalf("first model = %+v, want normalized Qwen-Flash", models[0])
	}
	if models[1].ID != "Claude-Sonnet" || models[1].Name != "Claude-Sonnet" || models[1].ContextWindow != 3000 {
		t.Fatalf("second model = %+v, want fallback name Claude-Sonnet", models[1])
	}
}

func TestHasStartHiddenArg(t *testing.T) {
	if !hasStartHiddenArg([]string{"--hidden"}) {
		t.Fatal("hasStartHiddenArg should accept --hidden")
	}
	if !hasStartHiddenArg([]string{"/hidden"}) {
		t.Fatal("hasStartHiddenArg should accept /hidden")
	}
	if hasStartHiddenArg([]string{"--other"}) {
		t.Fatal("hasStartHiddenArg should ignore unrelated args")
	}
}

func TestTigerProxyUpstreamRequestsCarryTigerClawClientName(t *testing.T) {
	seen := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	defer upstream.Close()

	app := NewApp()
	err := app.restartProxy(Settings{
		ListenAddress: "127.0.0.1:0",
		APIKey:        "local-key",
		AccessToken:   "upstream-token",
		BaseURL:       upstream.URL,
	})
	if err != nil {
		t.Fatalf("restartProxy: %v", err)
	}
	defer app.stopProxy()

	req, err := http.NewRequest(http.MethodGet, tigerProxyTestBaseURL(t, app)+"/v1/models", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer local-key")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200", resp.StatusCode)
	}

	var headers http.Header
	select {
	case headers = <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not receive proxied request")
	}
	if got := headers.Get("User-Agent"); got != corelib.CodeGenClientName {
		t.Fatalf("upstream User-Agent = %q, want %q", got, corelib.CodeGenClientName)
	}
	if got := headers.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
		t.Fatalf("upstream %s = %q, want %q", corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
	}
}

func TestTigerProxyOpenAIChatUpstreamServesChatAndResponsesClients(t *testing.T) {
	seen := make(chan tigerProxyUpstreamRequest, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream body: %v", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		seen <- tigerProxyUpstreamRequest{Path: r.URL.Path, Headers: r.Header.Clone(), Body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-tigerproxy","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer upstream.Close()

	app := NewApp()
	err := app.restartProxy(Settings{
		ListenAddress: "127.0.0.1:0",
		APIKey:        "local-key",
		AccessToken:   "upstream-token",
		BaseURL:       upstream.URL,
	})
	if err != nil {
		t.Fatalf("restartProxy: %v", err)
	}
	defer app.stopProxy()

	baseURL := tigerProxyTestBaseURL(t, app)
	client := &http.Client{Timeout: 5 * time.Second}

	chatReq, err := http.NewRequest(http.MethodPost, baseURL+"/v1/chat/completions", strings.NewReader(`{"model":"qax-codegen/Auto","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatalf("new chat request: %v", err)
	}
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set("Authorization", "Bearer local-key")
	chatResp, err := client.Do(chatReq)
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}
	_ = chatResp.Body.Close()
	if chatResp.StatusCode != http.StatusOK {
		t.Fatalf("chat status = %d, want 200", chatResp.StatusCode)
	}

	responsesReq, err := http.NewRequest(http.MethodPost, baseURL+"/v1/responses", strings.NewReader(`{"model":"qax-codegen/Auto","input":"hi from codex"}`))
	if err != nil {
		t.Fatalf("new responses request: %v", err)
	}
	responsesReq.Header.Set("Content-Type", "application/json")
	responsesReq.Header.Set("Authorization", "Bearer local-key")
	responsesResp, err := client.Do(responsesReq)
	if err != nil {
		t.Fatalf("responses request: %v", err)
	}
	responsesBody, _ := io.ReadAll(responsesResp.Body)
	_ = responsesResp.Body.Close()
	if responsesResp.StatusCode != http.StatusOK {
		t.Fatalf("responses status = %d, want 200; body=%s", responsesResp.StatusCode, responsesBody)
	}
	var responsePayload map[string]interface{}
	if err := json.Unmarshal(responsesBody, &responsePayload); err != nil {
		t.Fatalf("decode responses body: %v; body=%s", err, responsesBody)
	}
	if responsePayload["object"] != "response" {
		t.Fatalf("responses object = %#v, want response; body=%s", responsePayload["object"], responsesBody)
	}

	first := tigerProxyReadUpstreamRequest(t, seen)
	second := tigerProxyReadUpstreamRequest(t, seen)
	for i, req := range []tigerProxyUpstreamRequest{first, second} {
		if req.Path != "/chat/completions" {
			t.Fatalf("upstream request %d path = %q, want /chat/completions", i+1, req.Path)
		}
		if got := req.Headers.Get(corelib.CodeGenClientNameHeader); got != corelib.CodeGenClientName {
			t.Fatalf("upstream request %d %s = %q, want %q", i+1, corelib.CodeGenClientNameHeader, got, corelib.CodeGenClientName)
		}
	}
	if second.Body["messages"] == nil {
		t.Fatalf("responses request was not converted to chat messages: %#v", second.Body)
	}
}

func TestTigerProxyResponsesToolCallIsStoredAndHit(t *testing.T) {
	var upstreamHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-tool","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
	}))
	defer upstream.Close()

	app := NewApp()
	err := app.restartProxy(Settings{
		ListenAddress: "127.0.0.1:0",
		APIKey:        "local-key",
		AccessToken:   "upstream-token",
		BaseURL:       upstream.URL,
	})
	if err != nil {
		t.Fatalf("restartProxy: %v", err)
	}
	defer app.stopProxy()

	baseURL := tigerProxyTestBaseURL(t, app)
	client := &http.Client{Timeout: 5 * time.Second}
	doReq := func() (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, baseURL+"/v1/responses", strings.NewReader(`{"model":"qax-codegen/Auto","input":"hi","temperature":0,"tools":[{"type":"function","function":{"name":"read_file","parameters":{"type":"object"}}}]}`))
		if err != nil {
			t.Fatalf("new responses request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer local-key")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("responses request: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, body
	}

	resp1, body1 := doReq()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d, want 200 body=%s", resp1.StatusCode, body1)
	}
	if resp1.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("first X-Cache = %q, want MISS", resp1.Header.Get("X-Cache"))
	}

	status, err := app.Status()
	if err != nil {
		t.Fatalf("status after store: %v", err)
	}
	if status.LastCacheProtocol != "responses" {
		t.Fatalf("LastCacheProtocol = %q, want responses", status.LastCacheProtocol)
	}
	if status.LastCacheOutcome != "store" {
		t.Fatalf("LastCacheOutcome = %q, want store (tool_calls should be cacheable for exact-match retries)", status.LastCacheOutcome)
	}
	if status.CacheEntries < 1 {
		t.Fatalf("CacheEntries = %d, want >= 1 after tool_call store", status.CacheEntries)
	}

	resp2, body2 := doReq()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d, want 200 body=%s", resp2.StatusCode, body2)
	}
	if resp2.Header.Get("X-Cache") != "HIT" {
		t.Fatalf("second X-Cache = %q, want HIT (upstreamHits=%d body=%s)", resp2.Header.Get("X-Cache"), upstreamHits.Load(), body2)
	}
	if !bytes.Contains(body2, []byte("read_file")) {
		t.Fatalf("second body missing tool name: %s", body2)
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("upstream hits=%d, want 1 (second served from cache)", upstreamHits.Load())
	}

	status2, err := app.Status()
	if err != nil {
		t.Fatalf("status after hit: %v", err)
	}
	if status2.CacheHits < 1 {
		t.Fatalf("CacheHits = %d, want >= 1", status2.CacheHits)
	}
	if status2.LastCacheOutcome != "hit" {
		t.Fatalf("LastCacheOutcome = %q, want hit", status2.LastCacheOutcome)
	}
}

func tigerProxyTestBaseURL(t *testing.T, app *App) string {
	t.Helper()
	var addr net.Addr
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if app != nil {
			app.mu.Lock()
			server := app.server
			app.mu.Unlock()
			if server != nil {
				addr = server.Addr()
			}
		}
		if addr != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == nil {
		t.Fatal("proxy listener address is nil")
	}
	return "http://" + addr.String()
}

func tigerProxyReadUpstreamRequest(t *testing.T, seen <-chan tigerProxyUpstreamRequest) tigerProxyUpstreamRequest {
	t.Helper()
	select {
	case req := <-seen:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("upstream did not receive proxied request")
	}
	return tigerProxyUpstreamRequest{}
}
