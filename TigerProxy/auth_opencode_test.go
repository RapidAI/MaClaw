package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
)

func TestOpenCodeAuthModeIsListed(t *testing.T) {
	var found bool
	for _, info := range authModeCatalog() {
		if info.ID != AuthModeOpenCode {
			continue
		}
		found = true
		if info.Name != "OpenCode 登录" || info.Kind != "opencode" {
			t.Fatalf("catalog entry = %+v", info)
		}
	}
	if !found || !validAuthMode(AuthModeOpenCode) || openCodeBaseURL() != "https://opencode.ai/zen/go/v1" || openCodeDefaultModel() != "glm-5.3-flash-free" {
		t.Fatalf("OpenCode mode is not ready: base=%s model=%s", openCodeBaseURL(), openCodeDefaultModel())
	}
	if configfile.OpenCodeZenBaseURL != "https://opencode.ai/zen/v1" {
		t.Fatalf("GUI zen url = %s", configfile.OpenCodeZenBaseURL)
	}
	if !openCodeAuthURLAllowed("https://opencode.ai/auth") {
		t.Fatal("official auth page rejected")
	}
	if openCodeAuthURLAllowed("http://opencode.ai/auth") || openCodeAuthURLAllowed("https://opencode.ai.evil.example/auth") || openCodeAuthURLAllowed("https://user@opencode.ai/auth") {
		t.Fatal("untrusted auth page accepted")
	}
}

func TestCompleteOpenCodeLoginPinsZen(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeOpenCode,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CompleteOpenCodeLogin("short"); err == nil {
		t.Fatal("short key accepted")
	}
	status, err := app.CompleteOpenCodeLogin("oc-test-key-123456")
	if err != nil {
		t.Fatal(err)
	}
	if !status.LoggedIn {
		t.Fatal("not logged in")
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != openCodeBaseURL() || got.ModelID != openCodeDefaultModel() || got.AccessToken != "oc-test-key-123456" {
		t.Fatal("OpenCode login was not saved on the Zen endpoint")
	}
}

func TestUseLocalOpenCodeLogin(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpHome, "xdg"))
	t.Setenv("APPDATA", filepath.Join(tmpHome, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmpHome, "local"))
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
	path := configfile.OpencodeAuthPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"opencode":{"key":"oc-local-key-123456"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := configfile.OpencodeConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"model":"opencode/gpt-5.5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeOpenCode,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UseLocalOpenCodeLogin(); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "oc-local-key-123456" || got.BaseURL != openCodeBaseURL() || got.ModelID != "gpt-5.5" {
		t.Fatal("local OpenCode key was not imported")
	}
	if err := os.WriteFile(configPath, []byte(`{"model":"opencode/big-pickle"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UseLocalOpenCodeLogin(); err != nil {
		t.Fatal(err)
	}
	got, err = loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelID != "gpt-5.5" {
		t.Fatalf("reimport replaced the selected model with %s", got.ModelID)
	}
}

func TestUseLocalOpenCodeLoginReplacesPlaceholderDefault(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpHome, "xdg"))
	t.Setenv("APPDATA", filepath.Join(tmpHome, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmpHome, "local"))
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
	path := configfile.OpencodeAuthPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"opencode":{"key":"oc-local-key-123456"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := configfile.OpencodeConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"model":"opencode/gpt-5.5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeOpenCode,
		ModelID:        openCodeDefaultModel(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UseLocalOpenCodeLogin(); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelID != "gpt-5.5" {
		t.Fatalf("placeholder default stayed in place: %s", got.ModelID)
	}
}

func TestUseLocalOpenCodeLoginReadsJSONCModel(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpHome, "xdg"))
	t.Setenv("APPDATA", filepath.Join(tmpHome, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmpHome, "local"))
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")
	path := configfile.OpencodeAuthPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"opencode":{"key":"oc-local-key-123456"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := configfile.OpencodeConfigJSONCPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("{\n  // local preference\n  \"model\": \"opencode/kimi-k2.6\",\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeOpenCode,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.UseLocalOpenCodeLogin(); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelID != "kimi-k2.6" {
		t.Fatalf("jsonc model = %s", got.ModelID)
	}
}

func TestUseLocalOpenCodeLoginRejectsUnusableKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpHome, "xdg"))
	t.Setenv("APPDATA", filepath.Join(tmpHome, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(tmpHome, "local"))
	t.Setenv("OPENCODE_API_KEY", "")
	path := configfile.OpencodeAuthPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"opencode":{"key":"short"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewApp().UseLocalOpenCodeLogin()
	if err == nil || !strings.Contains(err.Error(), "无法使用") || strings.Contains(err.Error(), "未找到") {
		t.Fatal("short local key was treated as missing")
	}
}

func TestOpenCodeUpstreamDoesNotCapStreamBody(t *testing.T) {
	client := newOpenCodeUpstream().client
	if client == nil || client.Timeout != 0 {
		t.Fatal("client timeout would cut off a long stream")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil || !transport.DisableCompression || transport.ResponseHeaderTimeout != 10*time.Minute || transport.Proxy == nil {
		t.Fatal("upstream transport is not ready for streaming")
	}
}

func TestNormalizeOpenCodeKeyAcceptsBearerAndJSON(t *testing.T) {
	for _, raw := range []string{
		"Bearer oc-test-key-123456",
		`{"opencode":{"type":"api","key":"oc-test-key-123456"}}`,
		`{"key":"oc-test-key-123456"}`,
	} {
		got, err := normalizeOpenCodeKey(raw)
		if err != nil || got != "oc-test-key-123456" {
			t.Fatalf("normalize %q failed", raw)
		}
	}
	if _, err := normalizeOpenCodeKey("short"); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestOpenCodeChatSessionFollowsConversation(t *testing.T) {
	var sessions []string
	var sawEmptyAssistant bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessions = append(sessions, r.Header.Get(corelib.OpenCodeSessionHeader))
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), `"content":""`) {
			sawEmptyAssistant = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	first := []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":null}]}`)
	second := []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"other"}]}`)
	for _, body := range [][]byte{first, first, second} {
		resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex", body, "application/json", false)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	if len(sessions) != 3 || sessions[0] == "" || sessions[0] != sessions[1] || sessions[0] == sessions[2] {
		t.Fatal("session header was not stable per conversation")
	}
	if !regexp.MustCompile(`^ses_[0-9a-f]{9}ffe[0-9A-Za-z]{14}$`).MatchString(sessions[0]) {
		t.Fatalf("session id = %q, want descending counter 1", sessions[0])
	}
	if !sawEmptyAssistant {
		t.Fatal("empty assistant content was not normalized")
	}
}

func TestOpenCodeModelsUseOfficialClientIdentity(t *testing.T) {
	var ua, session string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		session = r.Header.Get(corelib.OpenCodeSessionHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	resp, err := upstream.DoModels(context.Background(), srv.URL+"/v1/models", "oc-test-key-123456", "codex_cli_rs/0.130.0")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if ua != "opencode/"+corelib.OpenCodeClientVersion {
		t.Fatalf("User-Agent = %q", ua)
	}
	if !regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`).MatchString(session) {
		t.Fatalf("session = %q", session)
	}
}

func TestOpenCodeUpstreamSendsOfficialClientIdentity(t *testing.T) {
	var ua, session, originator, raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		session = r.Header.Get(corelib.OpenCodeSessionHeader)
		originator = r.Header.Get("originator")
		body, _ := io.ReadAll(r.Body)
		raw = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	payload := []byte(`{"model":"big-pickle","messages":[` +
		`{"role":"system","content":"You are a coding agent running in the Codex CLI, a terminal-based coding assistant. Codex CLI is an open source project led by OpenAI. Keep going."},` +
		`{"role":"user","content":"hello"}]}`)
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex_cli_rs/0.130.0", payload, "application/json", false)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if ua != "opencode/"+corelib.OpenCodeClientVersion {
		t.Fatalf("User-Agent = %q", ua)
	}
	if originator != "" {
		t.Fatalf("originator = %q, want it removed", originator)
	}
	if strings.Contains(raw, "Codex CLI") {
		t.Fatalf("Codex identity was forwarded: %s", raw)
	}
	if !strings.Contains(raw, "Keep going.") || !strings.Contains(raw, "hello") {
		t.Fatalf("prompt was dropped: %s", raw)
	}
	if !regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`).MatchString(session) {
		t.Fatalf("session = %q", session)
	}
}

func TestOpenCodeFreeTierBodyIncludesStreamAndTools(t *testing.T) {
	var raw, accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		body, _ := io.ReadAll(r.Body)
		raw = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	payload := []byte(`{"model":"big-pickle","stream":false,"messages":[{"role":"user","content":"<file>"}],"tools":[{"type":"function","function":{"name":"shell","parameters":{"type":"object","properties":{}}}}]}`)
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex_cli_rs/0.130.0", payload, "application/json", false)
	if err != nil {
		t.Fatal(err)
	}
	clientBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(clientBody), `"content":"ok"`) {
		t.Fatalf("json upstream was rewritten: %s", clientBody)
	}
	if accept != "text/event-stream" {
		t.Fatalf("Accept = %q", accept)
	}
	for _, want := range []string{`"stream":true`, `"name":"shell"`, `"name":"bash"`, `"name":"read"`, "<file>"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("upstream body missing %s: %s", want, raw)
		}
	}
	if strings.Contains(raw, `\u003c`) {
		t.Fatalf("markup was escaped: %s", raw)
	}
}

func TestOpenCodeNonStreamCollectsUpstreamSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-1\",\"model\":\"big-pickle\",\"choices\":[{\"delta\":{\"content\":\"<ok>\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"shell\",\"arguments\":\"{\\\"cmd\\\":\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"ls\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex", []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}`), "application/json", false)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	got := string(body)
	if strings.HasPrefix(strings.TrimSpace(got), "data:") {
		t.Fatalf("client received the raw stream: %s", got)
	}
	for _, want := range []string{`"content":"<ok>"`, `"name":"shell"`, `"arguments":"{\"cmd\":\"ls\"}"`, `"finish_reason":"tool_calls"`, `"prompt_tokens":1`} {
		if !strings.Contains(got, want) {
			t.Fatalf("collected body missing %s: %s", want, got)
		}
	}
}

func TestOpenCodeStreamPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"live\"}}]}\n\n")
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex", []byte(`{"model":"big-pickle","stream":true,"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"bash"}},{"type":"function","function":{"name":"read"}}]}`), "text/event-stream", true)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "data:") || !strings.Contains(string(body), "live") {
		t.Fatalf("stream was collected: %s", body)
	}
}

func TestPrepareOpenCodeFreeTierBodyLeavesQualifiedRequest(t *testing.T) {
	body := []byte(`{"model":"big-pickle","stream":true,"temperature":0.2,"messages":[{"role":"user","content":"<file>"}],"tools":[{"type":"function","function":{"name":"bash","parameters":{"type":"object"}}},{"type":"function","function":{"name":"Read","parameters":{"type":"object"}}}]}`)
	if got := prepareOpenCodeFreeTierBody(body); string(got) != string(body) {
		t.Fatalf("qualified body was rewritten: %s", got)
	}
	rewritten := []byte(`{"model":"big-pickle","temperature":0.2,"messages":[{"role":"user","content":"<file>"}],"stream":false}`)
	got := string(prepareOpenCodeFreeTierBody(rewritten))
	for _, want := range []string{`"temperature":0.2`, `"messages":[{"role":"user","content":"<file>"}]`, `"stream":true`, `"name":"bash"`, `"name":"read"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("rewritten body missing %s: %s", want, got)
		}
	}
	if strings.Index(got, `"model"`) > strings.Index(got, `"temperature"`) {
		t.Fatalf("field order changed: %s", got)
	}
	foreign := []byte(`{"model":"big-pickle","tools":[{"type":"web_search","name":"bash"},{"type":"function","function":{"name":"shell"}}]}`)
	got = string(prepareOpenCodeFreeTierBody(foreign))
	if strings.Count(got, `"name":"bash"`) != 2 || !strings.Contains(got, `"type":"web_search"`) || !strings.Contains(got, `"name":"read"`) || !strings.Contains(got, `"name":"shell"`) {
		t.Fatalf("function tools = %s", got)
	}
	if !strings.Contains(got, `"command"`) || !strings.Contains(got, `"filePath"`) {
		t.Fatalf("tool schema = %s", got)
	}
}

func TestOpenCodeNonStreamSurfacesStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"OpenCode's free tier can only be used from within OpenCode\"}}\n\n")
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex", []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}`), "application/json", false)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "free tier") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestOpenCodeNonStreamMergesResentToolDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_a\",\"function\":{\"name\":\"shell\",\"arguments\":\"{\\\"cmd\\\":\\\"ls\\\"}\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_a\",\"function\":{\"name\":\"shell\",\"arguments\":\"{\\\"cmd\\\":\\\"ls\\\"}\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call_b\",\"function\":{\"name\":\"apply_patch\",\"arguments\":\"{}\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex", []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}`), "application/json", false)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	got := string(body)
	if strings.Count(got, `"name":"shell"`) != 1 || strings.Count(got, `"name":"apply_patch"`) != 1 {
		t.Fatalf("tool calls merged wrong: %s", got)
	}
	if strings.Count(got, `cmd`) != 1 {
		t.Fatalf("arguments were appended twice: %s", got)
	}
}

func TestOpenCodeNonStreamRejectsTruncatedStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"this stream is longer than the collect limit\"}}]}\n\n")
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	upstream.collectLimit = 32
	_, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex", []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}`), "application/json", false)
	if err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenCodeStreamWrapsJSONCompletion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","model":"big-pickle","choices":[{"message":{"role":"assistant","content":"<ok>","tool_calls":[{"id":"call_1","type":"function","function":{"name":"shell","arguments":{"cmd":"ls"}}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex", []byte(`{"model":"big-pickle","stream":true,"messages":[{"role":"user","content":"hi"}]}`), "text/event-stream", true)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	got := string(body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(got), "data:") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, got)
	}
	for _, want := range []string{`"<ok>"`, `"name":"shell"`, `"index":0`, `cmd\":\"ls`, `"prompt_tokens":1`, "data: [DONE]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("wrapped stream missing %s: %s", want, got)
		}
	}
	kept, ok := chatCompletionJSONToSSE([]byte(`{"choices":[{"delta":null,"message":{"role":"assistant","content":"kept"}}]}`))
	if !ok || !bytes.Contains(kept, []byte("kept")) || bytes.Contains(kept, []byte(`"delta":null`)) {
		t.Fatalf("message was dropped: %s", kept)
	}
}

func TestOpenCodeGoGatewayRewritesZenRequests(t *testing.T) {
	if got := openCodeGoEndpoint("https://opencode.ai/zen/v1/chat/completions"); got != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("chat endpoint = %s", got)
	}
	if got := openCodeGoEndpoint("https://opencode.ai/zen/v1/models?x=1"); got != "https://opencode.ai/zen/go/v1/models?x=1" {
		t.Fatalf("models endpoint = %s", got)
	}
	if got := openCodeGoEndpoint("https://opencode.ai/zen/go/v1/chat/completions"); got != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("go endpoint changed: %s", got)
	}
	if got := openCodeGoEndpoint("http://127.0.0.1:9/v1/chat/completions"); got != "http://127.0.0.1:9/v1/chat/completions" {
		t.Fatalf("local endpoint changed: %s", got)
	}
	rewritten := rewriteOpenCodeZenFreeModel([]byte(`{"model":"big-pickle","temperature":0.2,"messages":[{"role":"user","content":"<file>"}]}`))
	if !bytes.Contains(rewritten, []byte(`"model":"glm-5.3-flash-free"`)) || !bytes.Contains(rewritten, []byte(`"temperature":0.2`)) || !bytes.Contains(rewritten, []byte(`"<file>"`)) {
		t.Fatalf("model rewrite = %s", rewritten)
	}
	for _, id := range []string{"glm-5.3-flash-free", "deepseek-v4.1-flash-free"} {
		endpoint, forwarded, upstreamStream := finishOpenCodeChat("https://opencode.ai/zen/go/v1/chat/completions", []byte(`{"model":"`+id+`","stream":false,"messages":[{"role":"user","content":"hi"}]}`), false)
		if endpoint != "https://opencode.ai/zen/v1/chat/completions" || !upstreamStream || !bytes.Contains(forwarded, []byte(`"model":"`+id+`"`)) || !bytes.Contains(forwarded, []byte(`"name":"bash"`)) || !bytes.Contains(forwarded, []byte(`"name":"read"`)) {
			t.Fatalf("%s endpoint=%s stream=%v body=%s", id, endpoint, upstreamStream, forwarded)
		}
	}
	kept := rewriteOpenCodeGoModel("https://opencode.ai/zen/go/v1/chat/completions", []byte(`{"model":"deepseek-v4-pro"}`))
	if string(kept) != `{"model":"deepseek-v4-pro"}` {
		t.Fatalf("paid model changed: %s", kept)
	}
	for _, id := range []string{"cat-jam-free", "longcat-2.5-preview-free", "glm-5.3-flash", "deepseek-v4.1-flash"} {
		got := rewriteOpenCodeGoModel("https://opencode.ai/zen/go/v1/chat/completions", []byte(`{"model":"`+id+`"}`))
		if string(got) != `{"model":"`+id+`"}` {
			t.Fatalf("model %s changed: %s", id, got)
		}
	}
	endpoint, forwarded, upstreamStream := finishOpenCodeChat("https://opencode.ai/zen/go/v1/chat/completions", []byte(`{"model":"deepseek-v4.1-flash","stream":false,"messages":[{"role":"user","content":"hi"}]}`), false)
	if endpoint != "https://opencode.ai/zen/go/v1/chat/completions" || upstreamStream {
		t.Fatalf("endpoint=%s stream=%v", endpoint, upstreamStream)
	}
	if bytes.Contains(forwarded, []byte(`"name":"bash"`)) || !bytes.Contains(forwarded, []byte(`"stream":false`)) || !bytes.Contains(forwarded, []byte(`"model":"deepseek-v4.1-flash"`)) {
		t.Fatalf("go request was reshaped: %s", forwarded)
	}
	local := rewriteOpenCodeGoModel("http://127.0.0.1:9/v1/chat/completions", []byte(`{"model":"big-pickle"}`))
	if string(local) != `{"model":"big-pickle"}` {
		t.Fatalf("local model changed: %s", local)
	}
	settings := normalizeSettings(Settings{
		ActiveAuthMode: AuthModeOpenCode,
		BaseURL:        "https://opencode.ai/zen/v1",
		ModelID:        "big-pickle",
		Models:         []ModelOption{{ID: "big-pickle"}},
	})
	if settings.BaseURL != openCodeBaseURL() || settings.ModelID != "glm-5.3-flash-free" || len(settings.Models) != 0 {
		t.Fatalf("settings = %+v", settings)
	}
	paid := normalizeSettings(Settings{
		ActiveAuthMode: AuthModeOpenCode,
		ModelID:        "deepseek-v4-pro",
		Models:         []ModelOption{{ID: "deepseek-v4-pro"}},
	})
	if paid.ModelID != "deepseek-v4-pro" || len(paid.Models) != 1 {
		t.Fatalf("paid settings = %+v", paid)
	}
	served := normalizeSettings(Settings{
		ActiveAuthMode: AuthModeOpenCode,
		ModelID:        "cat-jam-free",
		Models:         []ModelOption{{ID: "cat-jam-free"}},
	})
	if served.ModelID != "cat-jam-free" || len(served.Models) != 1 {
		t.Fatalf("served free model = %+v", served)
	}
}

func TestOpenCodeZenFreeModelsLive(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("OPENCODE_LIVE=1 is not set")
	}
	key := strings.TrimSpace(os.Getenv("OPENCODE_KEY"))
	if key == "" {
		t.Skip("OPENCODE_KEY not set")
	}
	upstream := newOpenCodeUpstream()
	for _, model := range []string{"glm-5.3-flash-free", "deepseek-v4.1-flash-free"} {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		resp, err := upstream.DoChatCompletions(ctx, "https://opencode.ai/zen/go/v1/chat/completions", key, "codex", []byte(`{"model":"`+model+`","messages":[{"role":"user","content":"Reply with exactly PING"}]}`), "application/json", false)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), model) || !strings.Contains(string(body), "PING") {
			t.Fatalf("%s status=%d body=%s", model, resp.StatusCode, body)
		}
	}
}

func TestOpenCodeJSONErrorIsNotAnEmptySuccess(t *testing.T) {
	const payload = `{"error":{"message":"OpenCode's free tier can only be used from within OpenCode"}}`
	for _, stream := range []bool{false, true} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, payload)
		}))
		upstream := newOpenCodeUpstream()
		upstream.client = srv.Client()
		resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "codex", []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hi"}]}`), "application/json", stream)
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "free tier") || strings.Contains(string(body), `"choices"`) {
			t.Fatalf("stream=%v status=%d body=%s", stream, resp.StatusCode, body)
		}
	}
}
