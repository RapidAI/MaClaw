package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/windsurf"
)

func TestDevinAuthModeIsListed(t *testing.T) {
	var found bool
	for _, info := range authModeCatalog() {
		if info.ID == AuthModeDevin {
			found = true
			if info.Name == "" || info.Kind != "devin" {
				t.Fatalf("catalog entry = %+v", info)
			}
		}
	}
	if !found {
		t.Fatal("Devin auth mode missing")
	}
	if !validAuthMode(AuthModeDevin) {
		t.Fatal("Devin mode rejected")
	}
	if defaultAuthBaseURL(AuthModeDevin) == "" {
		t.Fatal("Devin base URL empty")
	}
}

func TestCompleteDevinLoginWithAPIKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeDevin,
	}); err != nil {
		t.Fatal(err)
	}
	status, err := app.CompleteDevinLoginWithToken("sk-ws-local-devin")
	if err != nil {
		t.Fatal(err)
	}
	if !status.LoggedIn {
		t.Fatalf("not logged in: %+v", status)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveAuthMode != AuthModeDevin || got.AccessToken != "sk-ws-local-devin" {
		t.Fatalf("settings = %+v", got)
	}
	if got.BaseURL != "https://server.codeium.com" {
		t.Fatalf("base url = %s", got.BaseURL)
	}
	if got.AuthProfiles[AuthModeDevin].AccessToken != "sk-ws-local-devin" {
		t.Fatalf("profile = %+v", got.AuthProfiles[AuthModeDevin])
	}
}

func TestPersistDevinHostReplacesCodeium(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeDevin,
		AccessToken:    "sk-ws-01-example",
		BaseURL:        "https://server.codeium.com",
		Email:          "Ada",
	}); err != nil {
		t.Fatal(err)
	}
	windsurf.RememberDevinAPIServer("sk-ws-01-example", windsurf.SelfServeAPIServer)
	app.persistDevinHost("sk-ws-01-example")
	if storedSettingsBaseURL() != windsurf.SelfServeAPIServer {
		t.Fatalf("stored base url = %s", storedSettingsBaseURL())
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != windsurf.SelfServeAPIServer {
		t.Fatalf("base url = %s", got.BaseURL)
	}
	if got.AuthProfiles[AuthModeDevin].BaseURL != windsurf.SelfServeAPIServer {
		t.Fatalf("profile url = %s", got.AuthProfiles[AuthModeDevin].BaseURL)
	}
}

func TestPersistDevinSessionReplacesAPIKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	const session = "devin-session-token$saved"
	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeDevin,
		AccessToken:    "sk-ws-01-example",
		BaseURL:        "https://server.codeium.com",
		Email:          "Ada",
	}); err != nil {
		t.Fatal(err)
	}
	app.persistDevinCredential("sk-ws-01-example", session, windsurf.SelfServeAPIServer)
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != session || got.BaseURL != windsurf.SelfServeAPIServer {
		t.Fatalf("access host = %s", got.BaseURL)
	}
	if got.AuthProfiles[AuthModeDevin].AccessToken != session {
		t.Fatal("profile kept the api key")
	}
}

func TestCompleteDevinLoginReadsTokenFromCallbackURL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeDevin,
	}); err != nil {
		t.Fatal(err)
	}
	status, err := app.CompleteDevinLoginWithToken("http://127.0.0.1:9/auth?state=abc&token=sk-ws-from-url")
	if err != nil {
		t.Fatal(err)
	}
	if !status.LoggedIn {
		t.Fatalf("not logged in: %+v", status)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "sk-ws-from-url" {
		t.Fatalf("access token = %q", got.AccessToken)
	}
}

func TestCompleteDevinLoginRejectsURLWithoutToken(t *testing.T) {
	app := NewApp()
	_, err := app.CompleteDevinLoginWithToken("https://windsurf.com/devin/account/login?client_id=x")
	if err == nil || !strings.Contains(err.Error(), "授权令牌") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompleteDevinSessionUsesSelfServeHost(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeDevin,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CompleteDevinLoginWithToken("devin-session-token$example"); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "devin-session-token$example" || got.BaseURL != windsurf.SelfServeAPIServer {
		t.Fatalf("saved credential = %s %s", got.AccessToken, got.BaseURL)
	}
}

func TestSupersededDevinLoginDoesNotReplaceSavedToken(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeDevin,
		AccessToken:    "sk-ws-kept",
		BaseURL:        windsurf.DefaultAPIServer,
	}); err != nil {
		t.Fatal(err)
	}
	flow := &devinLoginFlow{superseded: true}
	status, saved, err := app.saveDevinLogin(flow, windsurf.Session{
		APIKey:       "sk-ws-late",
		APIServerURL: windsurf.DefaultAPIServer,
	})
	if err != nil || saved || status.LoggedIn {
		t.Fatalf("saved=%v status=%+v err=%v", saved, status, err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "sk-ws-kept" {
		t.Fatalf("late login replaced the token with %q", got.AccessToken)
	}
}

func TestSupersededDevinWaitReportsReplacement(t *testing.T) {
	listener, err := windsurf.StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(listener.Close)
	app := NewApp()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	flow := &devinLoginFlow{
		ctx:        ctx,
		listener:   listener,
		superseded: true,
		finish: func() {
			cancel()
			listener.Close()
		},
	}
	app.mu.Lock()
	app.devinLogin = flow
	app.mu.Unlock()
	go func() {
		time.Sleep(20 * time.Millisecond)
		listener.Close()
	}()
	_, err = app.WaitDevinLogin()
	if err == nil || !strings.Contains(err.Error(), "被新的登录替代") {
		t.Fatalf("error = %v", err)
	}
}

func TestDevinLoginOutsideDevinModeKeepsCurrentAccount(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AICODER_SKIP_CODEX_PROCESS_KILL", "1")

	app := NewApp()
	if err := writeSettings(Settings{
		ListenAddress:  "127.0.0.1:0",
		APIKey:         "local-key",
		ActiveAuthMode: AuthModeSSO,
		AccessToken:    "sso-token",
		BaseURL:        "https://codegen.example",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.CompleteDevinLoginWithToken("sk-ws-should-not-stick"); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "sso-token" || got.ActiveAuthMode != AuthModeSSO {
		t.Fatalf("settings changed: mode=%s token=%s", got.ActiveAuthMode, got.AccessToken)
	}
}

func TestDevinLoginStopErrorDistinguishesReplacement(t *testing.T) {
	app := NewApp()
	replaced := &devinLoginFlow{superseded: true}
	if err := app.devinLoginStopError(replaced); err == nil || !strings.Contains(err.Error(), "被新的登录替代") {
		t.Fatalf("replaced = %v", err)
	}
	if err := app.devinLoginStopError(&devinLoginFlow{}); err == nil || !strings.Contains(err.Error(), "已取消或超时") {
		t.Fatalf("cancelled = %v", err)
	}
}

func TestSupersedeLeavesLoginThatStartedAfterPaste(t *testing.T) {
	app := NewApp()
	older := &devinLoginFlow{}
	newer := &devinLoginFlow{}
	app.mu.Lock()
	app.devinLogin = newer
	app.mu.Unlock()

	app.supersedeDevinLogin(older)
	app.mu.Lock()
	current := app.devinLogin
	app.mu.Unlock()
	if current != newer || newer.superseded || older.superseded {
		t.Fatal("a login opened during token exchange was stopped")
	}
}
