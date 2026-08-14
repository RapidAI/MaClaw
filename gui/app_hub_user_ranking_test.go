package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGetHubUserInvitationsNormalizesPageAndKeepsViewerScope(t *testing.T) {
	var gotPath, gotPage, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotPage = r.URL.Query().Get("page")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true,"invite_url":"https://hub.example/invite/demo","total":1,"page":1}`))
	}))
	defer server.Close()

	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL + "/",
			RemoteViewerToken: "viewer-token",
		},
	}

	resp := app.GetHubUserInvitationsPage(0)

	if resp.Error != "" {
		t.Fatalf("GetHubUserInvitationsPage error = %q", resp.Error)
	}
	if gotPath != "/api/me/invitations" || gotPage != "1" {
		t.Fatalf("request = %s?page=%s, want /api/me/invitations?page=1", gotPath, gotPage)
	}
	if gotAuth != "Bearer viewer-token" {
		t.Fatalf("authorization = %q, want bearer viewer-token", gotAuth)
	}
}

func TestGetHubUserInvitationStatusUsesLightweightEndpoint(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":false}`))
	}))
	defer server.Close()

	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL + "/",
			RemoteViewerToken: "viewer-token",
		},
	}

	resp := app.GetHubUserInvitationStatus()
	if resp.Error != "" || resp.Enabled {
		t.Fatalf("unexpected status response: %#v", resp)
	}
	if gotPath != "/api/me/invitations/status" || gotQuery != "" {
		t.Fatalf("request = %s?%s, want /api/me/invitations/status", gotPath, gotQuery)
	}
	if gotAuth != "Bearer viewer-token" {
		t.Fatalf("authorization = %q, want bearer viewer-token", gotAuth)
	}
}

func TestGetHubUserInvitationStatusFallsBackForOlderHubs(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		if r.URL.Path == "/api/me/invitations/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true,"invite_url":"https://hub.example/invite/demo"}`))
	}))
	defer server.Close()

	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}

	for attempt := 0; attempt < 2; attempt++ {
		resp := app.GetHubUserInvitationStatus()
		if resp.Error != "" || !resp.Enabled {
			t.Fatalf("fallback response = %#v", resp)
		}
	}
	if got, want := strings.Join(requestedPaths, ","), "/api/me/invitations/status,/api/me/invitations,/api/me/invitations"; got != want {
		t.Fatalf("requested paths = %q, want %q", got, want)
	}
}

func TestRotateHubUserInvitationUsesPostAndViewerScope(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true,"invite_url":"https://hub.example/invite/new"}`))
	}))
	defer server.Close()

	app := &App{
		configCacheValid: true,
		configCache: corelib.AppConfig{
			RemoteHubURL:      server.URL,
			RemoteViewerToken: "viewer-token",
		},
	}

	resp := app.RotateHubUserInvitation()

	if resp.Error != "" {
		t.Fatalf("RotateHubUserInvitation error = %q", resp.Error)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/me/invitations/rotate" {
		t.Fatalf("request = %s %s, want POST /api/me/invitations/rotate", gotMethod, gotPath)
	}
	if gotAuth != "Bearer viewer-token" {
		t.Fatalf("authorization = %q, want bearer viewer-token", gotAuth)
	}
}
