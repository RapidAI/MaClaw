package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestSkillSearcherSearch_FailsOverAndPersistsHubCenterList(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL, "https://skillmarket-backup.example"}})
		case "/api/v1/skillmarket/search":
			_ = json.NewEncoder(w).Encode(struct {
				Results []SkillSearchResult `json:"results"`
			}{Results: []SkillSearchResult{{ID: "m1", Name: "Market Skill", Price: 0}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	searcher := NewSkillSearcher(NewSkillMarketClient(app))
	results, err := searcher.Search(context.Background(), "market", nil, 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "m1" {
		t.Fatalf("Search() results = %+v", results)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubCenterURL != backup.URL {
		t.Fatalf("RemoteHubCenterURL = %q, want %q", saved.RemoteHubCenterURL, backup.URL)
	}
	if !containsString(saved.RemoteHubCenterURLs, "https://skillmarket-backup.example") {
		t.Fatalf("RemoteHubCenterURLs = %#v", saved.RemoteHubCenterURLs)
	}
}

func TestDownloadSkillJSONFromHubCenter_FailsOver(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL}})
		case "/api/v1/skills/demo/download":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "demo",
				"name":        "Demo Skill",
				"description": "demo",
				"version":     "1.0.0",
				"steps":       []map[string]any{{"action": "craft_tool", "params": map[string]any{"instructions": "do it"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	skill, err := downloadSkillJSONFromHubCenter(context.Background(), app, "/api/v1/skills/demo/download")
	if err != nil {
		t.Fatalf("downloadSkillJSONFromHubCenter() error = %v", err)
	}
	if skill.Name != "Demo Skill" || skill.HubSkillID != "demo" {
		t.Fatalf("skill = %+v", skill)
	}
}
