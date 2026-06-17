package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
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

func TestSkillHubClientRecommendationsPreserveMaclawAppProductFields(t *testing.T) {
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
			return
		case "/api/v1/skills/popular":
		default:
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{
			"id":                             "invoice-app",
			"name":                           "Invoice App",
			"description":                    "Invoice review app",
			"version":                        "1.0.0",
			"trust_level":                    "community",
			"product_kind":                   "maclaw_app_skill",
			"is_maclaw_app":                  true,
			"maclaw_app_id":                  "invoice-review",
			"maclaw_app_name":                "Invoice Review",
			"maclaw_app_description":         "Review invoices with a guided panel",
			"maclaw_app_category":            "finance",
			"maclaw_app_icon":                "receipt",
			"maclaw_app_input_mode":          "file",
			"maclaw_app_output_modes":        []string{"pdf", "docx"},
			"maclaw_app_definition_sha256":   "abc123",
			"maclaw_app_test_evidence":       map[string]any{"run_id": "run-ok-1", "verified_at": "2026-06-17T10:00:00Z", "definition_fingerprint": "feedbeef", "artifact_present": true, "artifact_name": "invoice.pdf"},
			"artifact_contract_required":     true,
			"artifact_contract_output_modes": []string{"pdf", "docx"},
			"artifact_contract_presentation": "preview_or_file",
		}})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{server.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	client := NewSkillHubClient(app)
	if err := client.RefreshRecommendations(context.Background()); err != nil {
		t.Fatalf("RefreshRecommendations() error = %v", err)
	}

	recs := client.GetRecommendations()
	if len(recs) != 1 {
		t.Fatalf("recommendations len = %d, want 1: %#v", len(recs), recs)
	}
	if recs[0].ProductKind != "maclaw_app_skill" || !recs[0].IsMaclawApp {
		t.Fatalf("recommendation app product fields not preserved: %#v", recs[0])
	}
	if recs[0].MaclawAppID != "invoice-review" || recs[0].MaclawAppName != "Invoice Review" || recs[0].MaclawAppDescription != "Review invoices with a guided panel" || recs[0].MaclawAppCategory != "finance" || recs[0].MaclawAppIcon != "receipt" {
		t.Fatalf("recommendation app preview fields not preserved: %#v", recs[0])
	}
	if recs[0].MaclawAppInputMode != "file" || strings.Join(recs[0].MaclawAppOutputModes, ",") != "pdf,docx" {
		t.Fatalf("recommendation app IO fields not preserved: %#v", recs[0])
	}
	if recs[0].MaclawAppDefinitionSHA256 != "abc123" {
		t.Fatalf("recommendation app definition hash not preserved: %#v", recs[0])
	}
	if recs[0].MaclawAppTestEvidence == nil || recs[0].MaclawAppTestEvidence.RunID != "run-ok-1" || recs[0].MaclawAppTestEvidence.DefinitionFingerprint != "feedbeef" || !recs[0].MaclawAppTestEvidence.ArtifactPresent || recs[0].MaclawAppTestEvidence.ArtifactName != "invoice.pdf" {
		t.Fatalf("recommendation app test evidence not preserved: %#v", recs[0])
	}
	if !recs[0].ArtifactContractRequired || strings.Join(recs[0].ArtifactContractOutputModes, ",") != "pdf,docx" || recs[0].ArtifactContractPresentation != "preview_or_file" {
		t.Fatalf("recommendation artifact contract not preserved: %#v", recs[0])
	}
}
