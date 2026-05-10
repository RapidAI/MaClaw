package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
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

func TestSubmitSkill_FailsOverWhenSessionExpiredOnSelectedHub(t *testing.T) {
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

	var primaryHits int32
	var backupHits int32
	var primary *httptest.Server
	var backup *httptest.Server
	discovery := func(w http.ResponseWriter) {
		_ = json.NewEncoder(w).Encode(struct {
			OK   bool     `json:"ok"`
			URLs []string `json:"urls"`
		}{OK: true, URLs: []string{primary.URL, backup.URL}})
	}
	primary = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			discovery(w)
		case "/api/v1/skills/submit":
			atomic.AddInt32(&primaryHits, 1)
			http.Error(w, `{"error":"session expired or invalid"}`, http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer primary.Close()
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			discovery(w)
		case "/api/v1/skills/submit":
			atomic.AddInt32(&backupHits, 1)
			if got := r.Header.Get("Authorization"); got != "Bearer session-token" {
				t.Errorf("Authorization = %q, want bearer token", got)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("ParseMultipartForm() error = %v", err)
			}
			if got := r.FormValue("email"); got != "uploader@example.com" {
				t.Errorf("email = %q", got)
			}
			if _, _, err := r.FormFile("zip"); err != nil {
				t.Errorf("zip form file missing: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "sub-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  primary.URL,
		RemoteHubCenterURLs: []string{backup.URL},
		RemoteViewerToken:   "session-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	id, err := NewSkillMarketClient(app).SubmitSkill(context.Background(), zipPath, "uploader@example.com")
	if err != nil {
		t.Fatalf("SubmitSkill() error = %v", err)
	}
	if id != "sub-ok" {
		t.Fatalf("submission id = %q", id)
	}
	if atomic.LoadInt32(&primaryHits) == 0 || atomic.LoadInt32(&backupHits) == 0 {
		t.Fatalf("primary hits = %d, backup hits = %d", primaryHits, backupHits)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubCenterURL != backup.URL {
		t.Fatalf("RemoteHubCenterURL = %q, want %q", saved.RemoteHubCenterURL, backup.URL)
	}
}

func TestSubmitSkill_PrefersSkillMarketSessionToken(t *testing.T) {
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

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{server.URL}})
		case "/api/v1/skills/submit":
			if got := r.Header.Get("Authorization"); got != "Bearer skillmarket-session" {
				t.Errorf("Authorization = %q, want SkillMarket session token", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "sub-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      server.URL,
		RemoteViewerToken:       "viewer-token",
		SkillMarketSessionToken: "skillmarket-session",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	id, err := NewSkillMarketClient(app).SubmitSkill(context.Background(), zipPath, "uploader@example.com")
	if err != nil {
		t.Fatalf("SubmitSkill() error = %v", err)
	}
	if id != "sub-ok" {
		t.Fatalf("submission id = %q", id)
	}
}

func TestSubmitSkill_AllAuthFailuresReturnAuthExpiredMessage(t *testing.T) {
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

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{server.URL}})
		case "/api/v1/skills/submit":
			http.Error(w, `{"error":"session expired or invalid"}`, http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      server.URL,
		SkillMarketSessionToken: "expired-session",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := NewSkillMarketClient(app).SubmitSkill(context.Background(), zipPath, "uploader@example.com")
	if err == nil {
		t.Fatal("SubmitSkill() succeeded with expired auth")
	}
	if !strings.Contains(err.Error(), "SkillMarket 认证失败或已过期") {
		t.Fatalf("SubmitSkill() error = %v, want auth expired message", err)
	}
}
