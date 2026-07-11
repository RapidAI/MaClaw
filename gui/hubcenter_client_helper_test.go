package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestGetHubCenterJSONReturnsExplicitLimitErrorInsteadOfUnexpectedEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"payload": strings.Repeat("x", 64)})
	}))
	defer server.Close()

	app := &App{}
	var dest map[string]string
	_, _, err := app.getHubCenterJSONFromCandidates(context.Background(), server.Client(), []string{server.URL}, "/download", 32, &dest)
	if err == nil {
		t.Fatal("expected response size error")
	}
	if !strings.Contains(err.Error(), "client download limit") {
		t.Fatalf("error = %v, want explicit size limit", err)
	}
}

func TestReadLimitedHubCenterBodyFailsFastOnContentLength(t *testing.T) {
	_, err := readLimitedHubCenterBodyWithLength(strings.NewReader("tiny"), 100, 32)
	if err == nil {
		t.Fatal("expected content-length rejection")
	}
	if !strings.Contains(err.Error(), "client download limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestGetHubCenterJSONAcceptsResponseAtLimit(t *testing.T) {
	body := `{"ok":true}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	app := &App{}
	var dest map[string]bool
	_, _, err := app.getHubCenterJSONFromCandidates(context.Background(), server.Client(), []string{server.URL}, "/download", int64(len(body)), &dest)
	if err != nil {
		t.Fatalf("getHubCenterJSONFromCandidates: %v", err)
	}
	if !dest["ok"] {
		t.Fatalf("dest = %+v", dest)
	}
}

func TestGetHubCenterJSONRetriesUnexpectedEOF(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			_, _ = w.Write([]byte(`{"ok":`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	app := &App{}
	var dest map[string]bool
	_, _, err := app.getHubCenterJSONFromCandidates(context.Background(), server.Client(), []string{server.URL}, "/download", 1024, &dest)
	if err != nil {
		t.Fatalf("getHubCenterJSONFromCandidates: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if !dest["ok"] {
		t.Fatalf("dest = %+v", dest)
	}
}

func TestGetHubCenterJSONUnexpectedEOFDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":`))
	}))
	defer server.Close()

	app := &App{}
	var dest map[string]bool
	_, _, err := app.getHubCenterJSONFromCandidates(context.Background(), server.Client(), []string{server.URL}, "/download", 1024, &dest)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode hubcenter JSON "+server.URL+"/download failed after 6 bytes") {
		t.Fatalf("error = %v, want diagnostic with URL/path and byte count", err)
	}
}

func TestGetHubCenterBytesReturnsExactLimitedBody(t *testing.T) {
	body := strings.Repeat("a", 32)
	data, err := readLimitedHubCenterBody(strings.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("readLimitedHubCenterBody: %v", err)
	}
	if string(data) != body {
		t.Fatalf("data = %q, want %q", string(data), body)
	}
}

func TestResolveHubCenterCandidatesKeepsDefaultFailoverWithCachedSingleNode(t *testing.T) {
	origDefaults := remote.DefaultRemoteHubCenterURLs
	remote.DefaultRemoteHubCenterURLs = []string{
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
	}
	defer func() { remote.DefaultRemoteHubCenterURLs = origDefaults }()

	app := &App{hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set("https://hubs2.maclaw.top", []string{"https://hubs2.maclaw.top"})

	got, err := app.resolveHubCenterCandidates(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolveHubCenterCandidates: %v", err)
	}
	want := []string{
		"https://hubs2.maclaw.top",
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
	}
	if !remote.StringSliceEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestGetHubCenterJSONDoesNotPromoteDefaultFailoverOverLoopback(t *testing.T) {
	origDefaults := remote.DefaultRemoteHubCenterURLs
	remote.DefaultRemoteHubCenterURLs = []string{"https://hubs.maclaw.top"}
	defer func() { remote.DefaultRemoteHubCenterURLs = origDefaults }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	app := &App{hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set(server.URL, []string{server.URL})

	var dest map[string]bool
	_, _, err := app.getHubCenterJSONFromCandidates(context.Background(), server.Client(), []string{server.URL, "https://hubs.maclaw.top"}, "/health", 0, &dest)
	if err != nil {
		t.Fatalf("getHubCenterJSONFromCandidates: %v", err)
	}
	if !dest["ok"] {
		t.Fatalf("dest = %+v", dest)
	}
	base, all := app.hubCenterCache.Get()
	if base != server.URL || !remote.StringSliceEqual(all, []string{server.URL}) {
		t.Fatalf("cache = (%q, %#v), want loopback server only", base, all)
	}
}
