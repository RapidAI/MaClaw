package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	remote.ResetFailureMemory()
	t.Cleanup(remote.ResetFailureMemory)

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

func TestResolveHubCenterCandidatesDeprioritizesRecentlyFailedPreferred(t *testing.T) {
	remote.ResetFailureMemory()
	t.Cleanup(remote.ResetFailureMemory)

	origDefaults := remote.DefaultRemoteHubCenterURLs
	remote.DefaultRemoteHubCenterURLs = []string{
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
	}
	defer func() { remote.DefaultRemoteHubCenterURLs = origDefaults }()

	app := &App{hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set("https://hubs2.maclaw.top", []string{"https://hubs2.maclaw.top"})
	remote.RecordProbeResult("https://hubs2.maclaw.top", false)

	got, err := app.resolveHubCenterCandidates(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolveHubCenterCandidates: %v", err)
	}
	if len(got) < 3 {
		t.Fatalf("candidates = %#v", got)
	}
	if got[0] == "https://hubs2.maclaw.top" {
		t.Fatalf("recently failed preferred must not stay first: %#v", got)
	}
	if got[len(got)-1] != "https://hubs2.maclaw.top" {
		t.Fatalf("recently failed preferred should be last for recovery: %#v", got)
	}
}

func TestShouldDemoteHubCenterCandidate(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	if shouldDemoteHubCenterCandidate(parent, context.Canceled, 0) {
		t.Fatal("parent cancellation must not demote nodes")
	}
	if shouldDemoteHubCenterCandidate(context.Background(), nil, http.StatusNotFound) {
		t.Fatal("404 must not demote healthy nodes")
	}
	if shouldDemoteHubCenterCandidate(context.Background(), nil, http.StatusForbidden) {
		t.Fatal("403 must not demote healthy nodes")
	}
	if !shouldDemoteHubCenterCandidate(context.Background(), nil, http.StatusBadGateway) {
		t.Fatal("5xx should demote")
	}
	if !shouldDemoteHubCenterCandidate(context.Background(), context.DeadlineExceeded, 0) {
		t.Fatal("per-node deadline should demote")
	}
	if !shouldDemoteHubCenterCandidate(context.Background(), errors.New("dial tcp: connection refused"), 0) {
		t.Fatal("connection refused should demote")
	}
	if !shouldDemoteHubCenterCandidate(context.Background(), io.ErrUnexpectedEOF, http.StatusOK) {
		t.Fatal("truncated 2xx body should demote")
	}
	if shouldDemoteHubCenterCandidate(context.Background(), errors.New("checksum mismatch"), http.StatusOK) {
		t.Fatal("content/integrity errors on 2xx must not demote")
	}
}

func TestGetHubCenterBytesFromCandidatesFailsOverDeadPreferred(t *testing.T) {
	liveHits := 0
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/demo/download" {
			http.NotFound(w, r)
			return
		}
		liveHits++
		_, _ = w.Write([]byte(`{"ok":true,"id":"demo"}`))
	}))
	defer live.Close()

	app := &App{}
	client := &http.Client{Timeout: 5 * time.Second}
	bases := []string{"http://127.0.0.1:1", live.URL}
	used, gotBases, data, err := app.getHubCenterBytesFromCandidates(context.Background(), client, bases, "/api/v1/skills/demo/download", 0)
	if err != nil {
		t.Fatalf("getHubCenterBytesFromCandidates: %v", err)
	}
	if used != live.URL {
		t.Fatalf("used = %q, want live %q", used, live.URL)
	}
	if liveHits != 1 {
		t.Fatalf("liveHits = %d, want 1", liveHits)
	}
	if !strings.Contains(string(data), `"demo"`) {
		t.Fatalf("data = %s", data)
	}
	if len(gotBases) < 2 {
		t.Fatalf("bases = %#v", gotBases)
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
	// Successful loopback endpoint must remain preferred. Public default seeds
	// may appear in the discovered list for future failover, but must not
	// replace the working local preferred URL.
	if base != server.URL {
		t.Fatalf("preferred cache base = %q, want loopback %q (all=%#v)", base, server.URL, all)
	}
	if remote.IsLoopbackURL(base) == false {
		t.Fatalf("preferred base %q must stay loopback", base)
	}
	_ = all
}
