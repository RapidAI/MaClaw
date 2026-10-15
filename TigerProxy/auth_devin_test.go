package main

import (
	"testing"
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
	t.Setenv("AICODER_SKIP_CODEXPROXY_RELAUNCH", "1")
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
