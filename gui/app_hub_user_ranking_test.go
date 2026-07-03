package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestGetHubUserRankingIncludesTenantIDAndNormalizesHubURL(t *testing.T) {
	var gotPath, gotTenantID, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenantID = r.URL.Query().Get("tenant_id")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_tokens":42,"duration_seconds":60,"token_rank":1,"duration_rank":2,"total_users":3,"period":"monthly"}`))
	}))
	defer server.Close()

	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL + "/",
			RemoteViewerToken: "viewer-token",
			RemoteTenantID:    "tenant acme",
		},
	}

	resp := app.GetHubUserRanking()

	if resp.Error != "" {
		t.Fatalf("GetHubUserRanking error = %q", resp.Error)
	}
	if gotPath != "/api/my-ranking" {
		t.Fatalf("request path = %q, want /api/my-ranking", gotPath)
	}
	if gotTenantID != "tenant acme" {
		t.Fatalf("tenant_id = %q, want tenant acme", gotTenantID)
	}
	if gotAuth != "Bearer viewer-token" {
		t.Fatalf("authorization = %q, want bearer viewer-token", gotAuth)
	}
	if resp.TokenRank != 1 || resp.DurationRank != 2 || resp.TotalUsers != 3 {
		t.Fatalf("unexpected ranking response: %#v", resp)
	}
}
