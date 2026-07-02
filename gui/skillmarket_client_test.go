package main

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

type failingRoundTripper struct{}

func (f failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("network disabled in test")
}

func TestSkillMarketBaseURLUsesConfiguredPublicHubCenter(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  "http://127.0.0.1:65140",
		RemoteHubCenterURLs: []string{"http://127.0.0.1:65140", "https://hubs.example.com/"},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := &SkillMarketClient{
		app:    app,
		client: &http.Client{Transport: failingRoundTripper{}},
	}
	if got, want := client.baseURL(), "https://hubs.example.com"; got != want {
		t.Fatalf("baseURL() = %q, want %q", got, want)
	}
}

func TestSkillMarketBaseURLDoesNotFallBackToHardcodedDefault(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	origDefaults := remote.DefaultRemoteHubCenterURLs
	origDefault := remote.DefaultRemoteHubCenterURL
	origGUIDefault := defaultRemoteHubCenterURL
	remote.DefaultRemoteHubCenterURLs = []string{"https://default-hubcenter.example.com"}
	remote.DefaultRemoteHubCenterURL = "https://default-hubcenter.example.com"
	defaultRemoteHubCenterURL = "https://default-hubcenter.example.com"
	defer func() {
		remote.DefaultRemoteHubCenterURLs = origDefaults
		remote.DefaultRemoteHubCenterURL = origDefault
		defaultRemoteHubCenterURL = origGUIDefault
	}()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  "http://127.0.0.1:65140",
		RemoteHubCenterURLs: []string{"http://127.0.0.1:65140"},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := &SkillMarketClient{
		app:    app,
		client: &http.Client{Transport: failingRoundTripper{}},
	}
	if got := client.baseURL(); got != "" {
		t.Fatalf("baseURL() = %q, want empty without a confirmed public HubCenter", got)
	}
}
