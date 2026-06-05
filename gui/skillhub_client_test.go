package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSkillHubClientInstallToDirAcceptsBundledJSONAboveOneMB(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/large-json/download" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "large-json",
			"name":        "Large JSON Skill",
			"description": strings.Repeat("x", 1100*1024),
			"version":     "1.0.0",
			"steps":       []map[string]any{{"action": "craft_tool", "params": map[string]any{"instructions": "ok"}}},
		})
	}))
	defer server.Close()

	client := NewSkillHubClient(&App{testHomeDir: t.TempDir()})
	entry, err := client.InstallToDir(context.Background(), "large-json", server.URL, t.TempDir())
	if err != nil {
		t.Fatalf("InstallToDir: %v", err)
	}
	if entry.Name != "Large JSON Skill" || entry.HubSkillID != "large-json" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestSkillHubClientInstallToDirSendsViewerTokenToConfiguredEnterpriseHub(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/enterprise-skill/download" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			http.Error(w, "missing viewer token", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "enterprise-skill",
			"name":        "Enterprise Skill",
			"description": "enterprise",
			"version":     "1.0.0",
			"steps":       []map[string]any{{"action": "craft_tool", "params": map[string]any{"instructions": "ok"}}},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	client := NewSkillHubClient(app)
	entry, err := client.InstallToDir(context.Background(), "enterprise-skill", server.URL, t.TempDir())
	if err != nil {
		t.Fatalf("InstallToDir: %v", err)
	}
	if entry.Name != "Enterprise Skill" || entry.HubSkillID != "enterprise-skill" {
		t.Fatalf("entry = %+v", entry)
	}
}
