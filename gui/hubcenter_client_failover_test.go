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

func TestFetchNews_FailsOverAndPersistsHubCenterList(t *testing.T) {
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
			}{OK: true, URLs: []string{backup.URL, "https://third.example"}})
		case "/api/news":
			_ = json.NewEncoder(w).Encode(struct {
				Articles []NewsArticle `json:"articles"`
			}{Articles: []NewsArticle{{ID: "n1", Title: "ok"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	articles, err := app.FetchNews()
	if err != nil {
		t.Fatalf("FetchNews() error = %v", err)
	}
	if len(articles) != 1 || articles[0].ID != "n1" {
		t.Fatalf("FetchNews() articles = %+v", articles)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubCenterURL != backup.URL {
		t.Fatalf("RemoteHubCenterURL = %q, want %q", saved.RemoteHubCenterURL, backup.URL)
	}
	if !containsString(saved.RemoteHubCenterURLs, backup.URL) || !containsString(saved.RemoteHubCenterURLs, "https://third.example") {
		t.Fatalf("RemoteHubCenterURLs = %#v", saved.RemoteHubCenterURLs)
	}
}

func TestSkillHubSearch_FailsOverAndPersistsHubCenterList(t *testing.T) {
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
			}{OK: true, URLs: []string{backup.URL, "https://fourth.example"}})
		case "/api/v1/skills/search":
			_ = json.NewEncoder(w).Encode(hubSkillSearchResult{Skills: []hubSkillItem{{ID: "s1", Name: "Skill One"}}, Total: 1, Page: 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := NewSkillHubClient(app)
	results, err := client.Search(context.Background(), "skill")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "s1" {
		t.Fatalf("Search() results = %+v", results)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubCenterURL != backup.URL {
		t.Fatalf("RemoteHubCenterURL = %q, want %q", saved.RemoteHubCenterURL, backup.URL)
	}
	if !containsString(saved.RemoteHubCenterURLs, "https://fourth.example") {
		t.Fatalf("RemoteHubCenterURLs = %#v", saved.RemoteHubCenterURLs)
	}
}

func TestGossipBrowsePosts_FailsOverAndPersistsHubCenterList(t *testing.T) {
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
			}{OK: true, URLs: []string{backup.URL, "https://fifth.example"}})
		case "/api/gossip/browse":
			_ = json.NewEncoder(w).Encode(GossipBrowseResult{OK: true, Posts: []GossipPost{{ID: "p1", Content: "hello"}}, Total: 1, Page: 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := NewGossipClient(app)
	result, err := client.BrowsePosts(context.Background(), 1)
	if err != nil {
		t.Fatalf("BrowsePosts() error = %v", err)
	}
	if len(result.Posts) != 1 || result.Posts[0].ID != "p1" {
		t.Fatalf("BrowsePosts() result = %+v", result)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubCenterURL != backup.URL {
		t.Fatalf("RemoteHubCenterURL = %q, want %q", saved.RemoteHubCenterURL, backup.URL)
	}
	if !containsString(saved.RemoteHubCenterURLs, "https://fifth.example") {
		t.Fatalf("RemoteHubCenterURLs = %#v", saved.RemoteHubCenterURLs)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
