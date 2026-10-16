package main

import (
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
	if !found || !validAuthMode(AuthModeOpenCode) || openCodeBaseURL() == "" || openCodeDefaultModel() == "" {
		t.Fatal("OpenCode mode is not ready")
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
	if !regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`).MatchString(sessions[0]) {
		t.Fatalf("session id = %q, want ses_ + 12 hex + 14 base62", sessions[0])
	}
	if !sawEmptyAssistant {
		t.Fatal("empty assistant content was not normalized")
	}
}

func TestOpenCodeUpstreamSendsOfficialClientIdentity(t *testing.T) {
	var ua, session string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		session = r.Header.Get(corelib.OpenCodeSessionHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c","choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
	}))
	defer srv.Close()
	upstream := newOpenCodeUpstream()
	upstream.client = srv.Client()
	resp, err := upstream.DoChatCompletions(context.Background(), srv.URL+"/v1/chat/completions", "oc-test-key-123456", "opencode", []byte(`{"model":"big-pickle","messages":[{"role":"user","content":"hello"}]}`), "application/json", false)
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
