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

	"github.com/RapidAI/CodeClaw/corelib"
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

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	// Unregistered: empty public registration so official defaults remain valid seeds.
	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
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

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
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

func TestResolveHubCenterCandidatesDropsUnregisteredPeersWhenEnrolled(t *testing.T) {
	remote.ResetFailureMemory()
	t.Cleanup(remote.ResetFailureMemory)

	origDefaults := remote.DefaultRemoteHubCenterURLs
	remote.DefaultRemoteHubCenterURLs = []string{
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
	}
	defer func() { remote.DefaultRemoteHubCenterURLs = origDefaults }()

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  "https://hubs.maclaw.top",
		RemoteHubCenterURLs: []string{"http://127.0.0.1:1", "https://hubs.maclaw.top"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	// Poisoned cache prefers unregistered HA peer.
	app.hubCenterCache.Set("https://hubs2.maclaw.top", []string{
		"https://hubs2.maclaw.top",
		"https://hubs.maclaw.top",
		"https://hubs.mypapers.top",
	})

	got, err := app.resolveHubCenterCandidates(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolveHubCenterCandidates: %v", err)
	}
	want := []string{"https://hubs.maclaw.top"}
	if !remote.StringSliceEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v (registered only)", got, want)
	}
}

func TestResolveHubCenterSubmitCandidatesUsesRegisteredCenterOnly(t *testing.T) {
	remote.ResetFailureMemory()
	t.Cleanup(remote.ResetFailureMemory)

	origDefaults := remote.DefaultRemoteHubCenterURLs
	remote.DefaultRemoteHubCenterURLs = []string{
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
	}
	defer func() { remote.DefaultRemoteHubCenterURLs = origDefaults }()

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{
		testHomeDir:    tmpHome,
		hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute),
	}
	// Cache may still prefer a broken HA peer from earlier discovery; submit must ignore it.
	app.hubCenterCache.Set("https://hubs2.maclaw.top", []string{"https://hubs2.maclaw.top", "https://hubs.maclaw.top"})
	if err := app.SaveConfig(corelib.AppConfig{
		// Mirrors real About registration: loopback discovery entry + public registered center.
		RemoteHubCenterURL:  "https://hubs.maclaw.top",
		RemoteHubCenterURLs: []string{"http://127.0.0.1:61729", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := app.resolveHubCenterSubmitCandidates(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolveHubCenterSubmitCandidates: %v", err)
	}
	want := []string{"https://hubs.maclaw.top"}
	if !remote.StringSliceEqual(got, want) {
		t.Fatalf("submit candidates = %#v, want %#v (must match About, no hubs2/defaults)", got, want)
	}
}

func TestResolveHubCenterSubmitCandidatesLoopbackDoesNotUsePublicPollution(t *testing.T) {
	remote.ResetFailureMemory()
	t.Cleanup(remote.ResetFailureMemory)

	origDefaults := remote.DefaultRemoteHubCenterURLs
	remote.DefaultRemoteHubCenterURLs = []string{
		"https://hubs.mypapers.top",
		"https://hubs.maclaw.top",
		"https://hubs2.maclaw.top",
	}
	defer func() { remote.DefaultRemoteHubCenterURLs = origDefaults }()

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  "http://127.0.0.1:9",
		RemoteHubCenterURLs: []string{"http://127.0.0.1:9", "https://hubs2.maclaw.top", "https://hubs.maclaw.top"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got, err := app.resolveHubCenterSubmitCandidates(context.Background(), nil)
	if err != nil {
		t.Fatalf("resolveHubCenterSubmitCandidates: %v", err)
	}
	// Loopback preferred: seeds = loopback + official defaults (ordered), not pollution-first.
	if len(got) == 0 || got[0] != "http://127.0.0.1:9" {
		t.Fatalf("submit candidates = %#v, want loopback first", got)
	}
	// Pollution must not appear before official defaults list position; hubs2 only via defaults.
	for i, u := range got {
		if u == "https://hubs2.maclaw.top" && i < 1 {
			t.Fatalf("hubs2 must not outrank loopback: %#v", got)
		}
	}
}

func TestConfiguredHubCenterSubmitURLsSkipsLoopbackUnlessAllowed(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteHubCenterURL:  "http://127.0.0.1:9",
		RemoteHubCenterURLs: []string{"http://127.0.0.1:9", "https://hubs.maclaw.top/", "https://custom.example"},
	}
	got := remote.RegisteredPublicHubCenterURLs(cfg.RemoteHubCenterURL, cfg.RemoteHubCenterURLs)
	// Loopback preferred → no public enrollment identity.
	if len(got) != 0 {
		t.Fatalf("public submit URLs = %#v, want empty for loopback preferred", got)
	}
	gotLoopback := remote.ConfiguredHubCenterURLs("http://127.0.0.1:9", []string{"http://127.0.0.1:9"}, true)
	if !remote.StringSliceEqual(gotLoopback, []string{"http://127.0.0.1:9"}) {
		t.Fatalf("loopback-allowed = %#v", gotLoopback)
	}
	// Public preferred keeps customs and strips non-preferred official defaults.
	gotPublic := remote.RegisteredPublicHubCenterURLs("https://hubs.maclaw.top", []string{
		"https://hubs.maclaw.top", "https://hubs2.maclaw.top", "https://custom.example",
	})
	wantPublic := []string{"https://hubs.maclaw.top", "https://custom.example"}
	if !remote.StringSliceEqual(gotPublic, wantPublic) {
		t.Fatalf("public preferred = %#v, want %#v", gotPublic, wantPublic)
	}
}

func TestAppConfigHubCenterBaseURLsMatchesEffectiveHubCenterSeeds(t *testing.T) {
	// Guard against drift between corelib AppConfig and remote.EffectiveHubCenterSeeds
	// (cannot share code due to import cycle).
	defaults := []string{"https://hubs.mypapers.top", "https://hubs.maclaw.top", "https://hubs2.maclaw.top"}
	cases := []corelib.AppConfig{
		{RemoteHubCenterURL: "https://hubs.maclaw.top", RemoteHubCenterURLs: []string{"http://127.0.0.1:1", "https://hubs.maclaw.top"}},
		{RemoteHubCenterURL: "http://127.0.0.1:9", RemoteHubCenterURLs: []string{"http://127.0.0.1:9"}},
		{RemoteHubCenterURL: "hubs.maclaw.top"},
		{},
	}
	for i, cfg := range cases {
		got := cfg.HubCenterBaseURLs(defaults[0], defaults)
		want := remote.EffectiveHubCenterSeeds(cfg.RemoteHubCenterURL, cfg.RemoteHubCenterURLs, defaults)
		if !remote.StringSliceEqual(got, want) {
			t.Fatalf("case %d HubCenterBaseURLs=%#v EffectiveHubCenterSeeds=%#v", i, got, want)
		}
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

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	// Unregistered local/dev: only loopback preferred; defaults are seeds only.
	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL, RemoteHubCenterURLs: []string{server.URL}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
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
