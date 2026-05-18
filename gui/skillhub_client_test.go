package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
