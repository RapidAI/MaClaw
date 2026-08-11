package enterpriseknowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestRunOnceInProgress(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ag := NewSyncAgent(c, func() (string, string, error) {
		// Block until cancelled so a second RunOnce hits in-progress.
		<-context.Background().Done()
		return "", "", nil
	}, "dev")
	if ag == nil {
		t.Fatal("agent nil")
	}
	// Force running flag.
	ag.mu.Lock()
	ag.running = true
	ag.mu.Unlock()
	err = ag.RunOnce(context.Background())
	if !errors.Is(err, ErrSyncInProgress) {
		t.Fatalf("want ErrSyncInProgress, got %v", err)
	}
}

func TestNewSyncAgentNilClient(t *testing.T) {
	if NewSyncAgent(nil, nil, "x") != nil {
		t.Fatal("expected nil agent for nil client")
	}
}

func TestRunOnceRecordsCredentialsSkippedOutcome(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return "", "", nil }, "dev")
	if err := ag.RunOnce(context.Background()); !errors.Is(err, ErrSyncCredentialsMissing) {
		t.Fatalf("RunOnce error = %v, want ErrSyncCredentialsMissing", err)
	}
	status := ag.Status()
	if status.LastOutcome != "skipped_no_credentials" {
		t.Fatalf("outcome = %q, want skipped_no_credentials", status.LastOutcome)
	}
	if status.LastError == "" {
		t.Fatal("missing credentials should be visible in last error")
	}
}

func TestRunOnceRecordsFailedOutcomeForHubError(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "bad-token", nil }, "dev")
	if err := ag.RunOnce(context.Background()); err == nil {
		t.Fatal("expected Hub authentication error")
	}
	status := ag.Status()
	if status.LastOutcome != "failed" {
		t.Fatalf("outcome = %q, want failed", status.LastOutcome)
	}
	if !strings.Contains(status.LastError, "manifest status 401") {
		t.Fatalf("last error = %q", status.LastError)
	}
}

func TestRunOnceRecordsTenantSyncDisabledOutcome(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := SeedLibraryForTest(c, "lib_disabled", "Disabled", "active", true); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/digital-assets/sync/manifest" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tenant_sync_enabled":false,"libraries":[]}`))
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "token", nil }, "dev")
	if err := ag.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ag.Status().LastOutcome; got != "skipped_tenant_sync_disabled" {
		t.Fatalf("outcome = %q, want skipped_tenant_sync_disabled", got)
	}
	var access string
	if err := c.meta.QueryRow(`SELECT access_state FROM enterprise_library_state WHERE library_id = ?`, "lib_disabled").Scan(&access); err != nil {
		t.Fatal(err)
	}
	if access != "sync_disabled" {
		t.Fatalf("access state = %q, want sync_disabled", access)
	}
}

func TestSyncLibraryReturnsMetadataWriteError(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ops":[]}`))
	}))
	defer srv.Close()
	if _, err := c.meta.Exec(`DROP TABLE enterprise_library_state`); err != nil {
		t.Fatal(err)
	}
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "token", nil }, "dev")
	err = ag.syncLibrary(context.Background(), srv.URL, "token", "lib_metadata", "Metadata", 0, "", "fp", true)
	if err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Fatalf("expected metadata write error, got %v", err)
	}
}

func TestSyncLibraryPollsHubWhenAtTip(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := SeedLibraryForTest(c, "lib_tip", "Tip", "active", true); err != nil {
		t.Fatal(err)
	}
	// Set last_rev high.
	if _, err := c.meta.Exec(`UPDATE enterprise_library_state SET last_rev = 10 WHERE library_id = 'lib_tip'`); err != nil {
		t.Fatal(err)
	}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/digital-assets/sync/pull" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ops":[]}`))
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "tok", nil }, "dev")
	if err := ag.syncLibrary(context.Background(), srv.URL, "tok", "lib_tip", "Tip", 10, "hash-tip", "fp", true); err != nil {
		t.Fatalf("tip poll should be nil: %v", err)
	}
	if requests != 1 {
		t.Fatalf("want one authoritative pull, got %d", requests)
	}
	var contentHash string
	if err := c.meta.QueryRow(`SELECT content_hash FROM enterprise_library_state WHERE library_id = 'lib_tip'`).Scan(&contentHash); err != nil {
		t.Fatal(err)
	}
	if contentHash != "hash-tip" {
		t.Fatalf("content hash = %q, want manifest hash", contentHash)
	}
}

func TestSyncLibraryRejectsUnexpectedEmptyPullBehindTip(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ops":[]}`))
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "tok", nil }, "dev")
	err = ag.syncLibrary(context.Background(), srv.URL, "tok", "lib_behind", "Behind", 2, "hash-behind", "fp", true)
	if err == nil || !strings.Contains(err.Error(), "no package operations") {
		t.Fatalf("expected inconsistent empty pull error, got %v", err)
	}
}

func TestSyncLibraryResetsAheadCursorForHubBootstrap(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := SeedLibraryForTest(c, "lib_reset", "Reset", "active", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.meta.Exec(`UPDATE enterprise_library_state SET last_rev = 9 WHERE library_id = 'lib_reset'`); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			SinceRev int64 `json:"since_rev"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.SinceRev != 0 {
			t.Fatalf("since_rev = %d, want bootstrap cursor 0", request.SinceRev)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ops":[]}`))
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "tok", nil }, "dev")
	err = ag.syncLibrary(context.Background(), srv.URL, "tok", "lib_reset", "Reset", 2, "hash-reset", "fp", true)
	if err == nil || !strings.Contains(err.Error(), "local revision 0") {
		t.Fatalf("expected failed bootstrap to retain an uncommitted reset, got %v", err)
	}
	var lastRev int64
	if err := c.meta.QueryRow(`SELECT last_rev FROM enterprise_library_state WHERE library_id = 'lib_reset'`).Scan(&lastRev); err != nil {
		t.Fatal(err)
	}
	if lastRev != 9 {
		t.Fatalf("failed bootstrap changed stored revision to %d", lastRev)
	}
}

func TestSyncLibraryReportsHubDeferredPull(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"reason":"tenant_busy","ops":[]}`))
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "tok", nil }, "dev")
	err = ag.syncLibrary(context.Background(), srv.URL, "tok", "lib_busy", "Busy", 2, "hash-busy", "fp", true)
	if err == nil || !strings.Contains(err.Error(), "hub deferred library sync: tenant_busy") {
		t.Fatalf("expected deferred-pull error, got %v", err)
	}
	if strings.Contains(err.Error(), "no package operations") {
		t.Fatalf("deferred pull was misreported as a manifest inconsistency: %v", err)
	}
}

func TestSyncLibraryFailurePreservesLastSuccessfulSyncTime(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := SeedLibraryForTest(c, "lib_failure", "Failure", "active", true); err != nil {
		t.Fatal(err)
	}
	const previousSync = "2026-01-02T03:04:05Z"
	if _, err := c.meta.Exec(`UPDATE enterprise_library_state SET last_sync_at=? WHERE library_id=?`, previousSync, "lib_failure"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Hub unavailable"))
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "tok", nil }, "dev")
	err = ag.syncLibrary(context.Background(), srv.URL, "tok", "lib_failure", "Failure", 1, "hash", "fp", true)
	if err == nil || !strings.Contains(err.Error(), "pull 500") {
		t.Fatalf("expected pull failure, got %v", err)
	}
	var lastSyncAt, lastError string
	if err := c.meta.QueryRow(`SELECT IFNULL(last_sync_at,''), last_error FROM enterprise_library_state WHERE library_id=?`, "lib_failure").Scan(&lastSyncAt, &lastError); err != nil {
		t.Fatal(err)
	}
	if lastSyncAt != previousSync {
		t.Fatalf("last_sync_at = %q, want previous successful time %q", lastSyncAt, previousSync)
	}
	if !strings.Contains(lastError, "pull 500") {
		t.Fatalf("last_error = %q", lastError)
	}
}

func TestSyncLibraryCatchesUpAcrossPullPages(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pkg := []byte(`{"type":"source","data":{"id":"asset","kind":"text","uri":"knowledge://text/asset","title":"Asset","content_hash":"asset","status":"parsed"}}` + "\n")
	sum := sha256.Sum256(pkg)
	digest := hex.EncodeToString(sum[:])
	pullCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/digital-assets/sync/pull":
			pullCount++
			var request struct {
				SinceRev int64 `json:"since_rev"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			switch request.SinceRev {
			case 0:
				_, _ = w.Write([]byte(`{"ops":[{"rev":1,"op":"replace_snapshot","package_url":"/pkg/1","package_sha256":"` + digest + `","content_hash":"one"}]}`))
			case 1:
				_, _ = w.Write([]byte(`{"ops":[{"rev":2,"op":"replace_snapshot","package_url":"/pkg/2","package_sha256":"` + digest + `","content_hash":"two"}]}`))
			default:
				t.Fatalf("unexpected since_rev %d", request.SinceRev)
			}
		case "/pkg/1", "/pkg/2":
			_, _ = w.Write(pkg)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "tok", nil }, "dev")
	if err := ag.syncLibrary(ctx, srv.URL, "tok", "lib_pages", "Pages", 2, "two", "fp", true); err != nil {
		t.Fatal(err)
	}
	if pullCount != 2 {
		t.Fatalf("pulls = %d, want 2", pullCount)
	}
	var lastRev int64
	if err := c.meta.QueryRow(`SELECT last_rev FROM enterprise_library_state WHERE library_id = 'lib_pages'`).Scan(&lastRev); err != nil {
		t.Fatal(err)
	}
	if lastRev != 2 {
		t.Fatalf("last_rev = %d, want 2", lastRev)
	}
}

func TestSyncLibraryAppliesOnlyLatestSnapshotFromPullPage(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	oldPkg := []byte(`{"type":"source","data":{"id":"old","kind":"text","uri":"knowledge://text/old","title":"Old","content_hash":"old","status":"parsed"}}` + "\n")
	latestPkg := []byte(`{"type":"source","data":{"id":"latest","kind":"text","uri":"knowledge://text/latest","title":"Latest","content_hash":"latest","status":"parsed"}}` + "\n")
	oldSum := sha256.Sum256(oldPkg)
	latestSum := sha256.Sum256(latestPkg)
	downloads := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/digital-assets/sync/pull":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ops":[` +
				`{"rev":1,"op":"replace_snapshot","package_url":"/pkg/1","package_sha256":"` + hex.EncodeToString(oldSum[:]) + `","package_bytes":` + strconv.Itoa(len(oldPkg)) + `},` +
				`{"rev":2,"op":"replace_snapshot","package_url":"/pkg/2","package_sha256":"` + hex.EncodeToString(latestSum[:]) + `","package_bytes":` + strconv.Itoa(len(latestPkg)) + `,"content_hash":"latest"}` +
				`]}`))
		case "/pkg/1":
			downloads[r.URL.Path]++
			_, _ = w.Write(oldPkg)
		case "/pkg/2":
			downloads[r.URL.Path]++
			_, _ = w.Write(latestPkg)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "tok", nil }, "dev")
	if err := ag.syncLibrary(ctx, srv.URL, "tok", "lib_latest", "Latest", 2, "latest", "fp", true); err != nil {
		t.Fatal(err)
	}
	if downloads["/pkg/1"] != 0 || downloads["/pkg/2"] != 1 {
		t.Fatalf("downloads = %#v, want only latest package", downloads)
	}
	var lastRev int64
	if err := c.meta.QueryRow(`SELECT last_rev FROM enterprise_library_state WHERE library_id = ?`, "lib_latest").Scan(&lastRev); err != nil {
		t.Fatal(err)
	}
	if lastRev != 2 {
		t.Fatalf("last_rev = %d, want 2", lastRev)
	}
}

func TestSyncLibraryAppliesTombstoneWithoutPackage(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := SeedLibraryForTest(c, "lib_tomb", "Tomb", "active", true); err != nil {
		t.Fatal(err)
	}
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: "dal_lib_tomb_asset", Kind: knowledge.SourceKindText, URI: "knowledge://text/tomb",
		Title: "Tomb asset", ContentHash: "tomb", Status: knowledge.StatusParsed,
		FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/digital-assets/sync/pull" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ops":[{"rev":1,"op":"tombstone_library"}]}`))
	}))
	defer srv.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return srv.URL, "tok", nil }, "dev")
	if err := ag.syncLibrary(ctx, srv.URL, "tok", "lib_tomb", "Tomb", 1, "hash-tomb", "fp", true); err != nil {
		t.Fatal(err)
	}
	libs, err := c.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 0 {
		t.Fatalf("library should be purged after tombstone: %#v", libs)
	}
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 10, IncludeDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 0 {
		t.Fatalf("sources should be purged after tombstone: %#v", sources)
	}
}

func TestValidateHubURL(t *testing.T) {
	if err := validateHubURL("https://hub.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := validateHubURL("ftp://hub.example.com"); err == nil {
		t.Fatal("expected scheme reject")
	}
	if err := validateHubURL("https://user:pass@hub.example.com"); err == nil {
		t.Fatal("expected userinfo reject")
	}
}

func TestHubScopedHTTPClientBlocksCrossHostRedirect(t *testing.T) {
	// Evil target that should never be followed.
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("leaked"))
	}))
	defer evil.Close()

	// Hub-looking server that redirects off-host.
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/steal", http.StatusFound)
	}))
	defer hub.Close()

	client := hubScopedHTTPClient(hub.URL)
	req, err := http.NewRequest(http.MethodGet, hub.URL+"/pkg", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
		// Client may return the 302 without following when CheckRedirect errors.
		// Go's default: if CheckRedirect returns error, Do returns that error
		// (except ErrUseLastResponse). We reject with a non-nil error.
		t.Fatal("expected redirect to be blocked")
	}
	if !strings.Contains(err.Error(), "redirect blocked") && !strings.Contains(err.Error(), "does not match hub host") {
		// Accept either our wrap message or the underlying validate error.
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartBackgroundAfterStop(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ag := NewSyncAgent(c, func() (string, string, error) { return "", "", nil }, "dev")
	if ag == nil {
		t.Fatal("agent nil")
	}
	ag.StartBackground()
	ag.Stop()
	// Allow loop to observe closed stopCh.
	time.Sleep(20 * time.Millisecond)
	ag.StartBackground()
	// Must not panic; stop again for cleanup.
	ag.Stop()
}

func TestRunOnceStopCancelsContext(t *testing.T) {
	dir := t.TempDir()
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Slow hub that hangs until client cancels.
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until request context is done.
		<-r.Context().Done()
	}))
	defer hub.Close()

	ag := NewSyncAgent(c, func() (string, string, error) {
		return hub.URL, "token", nil
	}, "dev-cancel")
	if ag == nil {
		t.Fatal("agent nil")
	}
	done := make(chan error, 1)
	go func() {
		done <- ag.RunOnce(context.Background())
	}()
	// Give RunOnce time to start HTTP.
	time.Sleep(50 * time.Millisecond)
	ag.Stop()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancel error")
		}
		if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "cancel") {
			// http client may wrap cancel.
			t.Logf("got err=%v (acceptable if canceled)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunOnce did not return after Stop")
	}
}

func TestValidatePackageURL(t *testing.T) {
	hub := "https://hub.example.com"
	if err := validatePackageURL(hub, "https://hub.example.com/api/digital-assets/x"); err != nil {
		t.Fatalf("same host: %v", err)
	}
	// Default HTTPS port equivalence.
	if err := validatePackageURL(hub, "https://hub.example.com:443/api/x"); err != nil {
		t.Fatalf("port 443 normalize: %v", err)
	}
	if err := validatePackageURL(hub, "https://evil.example.com/steal"); err == nil {
		t.Fatal("expected host mismatch")
	}
	if err := validatePackageURL(hub, "file:///etc/passwd"); err == nil {
		t.Fatal("expected scheme reject")
	}
	if err := validatePackageURL(hub, ""); err == nil {
		t.Fatal("expected empty reject")
	}
	if err := validatePackageURL(hub, "https://user:pass@hub.example.com/x"); err == nil {
		t.Fatal("expected userinfo reject")
	}
}

func TestNamespaceJSONLMixedPackage(t *testing.T) {
	// Package already contains some namespaced ids alongside bare ones.
	in := `{"id":"dal_libx_old","type":"source"}
{"id":"bare_new","type":"source","source_id":"bare_new"}
`
	out, err := namespaceJSONLSourceIDs([]byte(in), "dal_libx_")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"id":"dal_libx_old"`) {
		t.Fatalf("already-prefixed id changed: %s", s)
	}
	if !strings.Contains(s, `"id":"dal_libx_bare_new"`) {
		t.Fatalf("bare id not prefixed: %s", s)
	}
	if strings.Contains(s, "dal_libx_dal_libx_") {
		t.Fatalf("double prefix: %s", s)
	}
}

func TestNamespaceJSONLSourceIDs(t *testing.T) {
	in := `{"type":"source","id":"src1","source_id":"src1"}
{"type":"card","id":"c1","source_id":"src1","title":"t"}
`
	out, err := namespaceJSONLSourceIDs([]byte(in), "dal_libx_")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"id":"dal_libx_src1"`) {
		t.Fatalf("missing namespaced id: %s", s)
	}
	if !strings.Contains(s, `"source_id":"dal_libx_src1"`) {
		t.Fatalf("missing namespaced source_id: %s", s)
	}
	// Idempotent: already prefixed values stay.
	out2, err := namespaceJSONLSourceIDs(out, "dal_libx_")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out2), "dal_libx_dal_libx_") > 0 {
		t.Fatalf("double prefix: %s", out2)
	}
}

func TestNamespaceJSONLSourceLinkReferences(t *testing.T) {
	in := `{"type":"source_link","data":{"source_id":"src1","related_source_id":"src2","relation":"topic_related"}}
{"type":"source_link_event","data":{"id":"event1","source_id":"src2","related_source_id":"src1","relation":"topic_related"}}
`
	out, err := namespaceJSONLSourceIDs([]byte(in), "dal_libx_")
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		`"source_id":"dal_libx_src1"`,
		`"related_source_id":"dal_libx_src2"`,
		`"source_id":"dal_libx_src2"`,
		`"related_source_id":"dal_libx_src1"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing namespaced source link reference %s: %s", want, s)
		}
	}
}

func TestNamespaceJSONLSourceIDsOrdersSourcesBeforeLinks(t *testing.T) {
	in := `{"type":"source_link","data":{"source_id":"src1","related_source_id":"src2","relation":"topic_related"}}
{"type":"source","data":{"id":"src2","kind":"text","uri":"knowledge://text/src2","title":"Second","content_hash":"two","status":"parsed"}}
{"type":"source","data":{"id":"src1","kind":"text","uri":"knowledge://text/src1","title":"First","content_hash":"one","status":"parsed"}}
`
	out, err := namespaceJSONLSourceIDs([]byte(in), "dal_libx_")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"type":"source"`) || !strings.Contains(lines[1], `"type":"source"`) || !strings.Contains(lines[2], `"type":"source_link"`) {
		t.Fatalf("records were not ordered source-first: %s", out)
	}
	path := filepath.Join(t.TempDir(), "out-of-order.jsonl")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateSnapshotPackage(context.Background(), path); err != nil {
		t.Fatalf("source-first package should validate: %v", err)
	}
}

func TestNamespaceJSONLSourceIDsOrdersAllDependentRecords(t *testing.T) {
	in := `{"type":"fact","data":{"id":"fact1","source_id":"src1","card_id":"card1","subject":"A","predicate":"is","object":"B"}}
{"type":"card","data":{"id":"card1","source_id":"src1","node_id":"node1","title":"Card","claim":"Claim"}}
{"type":"node","data":{"id":"node1","source_id":"src1","type":"paragraph","text":"Node"}}
{"type":"source_link","data":{"source_id":"src1","related_source_id":"src2","relation":"topic_related"}}
{"type":"source","data":{"id":"src2","kind":"text","uri":"knowledge://text/src2","title":"Second","content_hash":"two","status":"parsed"}}
{"type":"source","data":{"id":"src1","kind":"text","uri":"knowledge://text/src1","title":"First","content_hash":"one","status":"parsed"}}
`
	out, err := namespaceJSONLSourceIDs([]byte(in), "dal_libx_")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "all-out-of-order.jsonl")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateSnapshotPackage(context.Background(), path); err != nil {
		t.Fatalf("dependency-ordered package should validate: %v\n%s", err, out)
	}
}

func TestApplyReplaceSnapshotValidatesBeforeDeletingLocalCache(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	oldID := "dal_lib_guard_old"
	now := time.Now().UTC()
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: oldID, Kind: knowledge.SourceKindText, URI: "knowledge://text/old",
		Title: "Old cached asset", ContentHash: "old", Status: knowledge.StatusParsed,
		FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "broken.jsonl")
	data := `{"type":"source","data":{"id":"new","kind":"text","uri":"knowledge://text/new","title":"New asset","content_hash":"new","status":"parsed"}}
{"type":"source_link","data":{"source_id":"new","related_source_id":"missing","relation":"topic_related"}}
`
	if err := os.WriteFile(pkg, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	err = applyPackage(ctx, store, c, "lib_guard", "replace_snapshot", pkg)
	if err == nil || !strings.Contains(err.Error(), "missing related source reference") {
		t.Fatalf("expected isolated snapshot validation error, got %v", err)
	}
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 10, IncludeDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.ID == oldID {
			return
		}
	}
	t.Fatalf("invalid replacement package deleted existing source %q", oldID)
}

func TestApplyReplaceSnapshotRemovesStaleNamespacedSources(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	oldID := "dal_lib_replace_old"
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: oldID, Kind: knowledge.SourceKindText, URI: "knowledge://text/old",
		Title: "Stale asset", ContentHash: "old", Status: knowledge.StatusParsed,
		FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "replacement.jsonl")
	if err := os.WriteFile(pkg, []byte(`{"type":"source","data":{"id":"new","kind":"text","uri":"knowledge://text/new","title":"New asset","content_hash":"new","status":"parsed"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPackage(ctx, store, c, "lib_replace", "replace_snapshot", pkg); err != nil {
		t.Fatal(err)
	}
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 10, IncludeDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, source := range sources {
		seen[source.ID] = true
	}
	if seen[oldID] {
		t.Fatalf("stale source %q survived replacement", oldID)
	}
	if !seen["dal_lib_replace_new"] {
		t.Fatalf("replacement source missing: %#v", seen)
	}
}

func TestApplyPackageRestoresSourcesAndMapWhenReplacementImportFails(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	const libraryID = "lib_rollback"
	const oldRemoteID = "old_remote"
	const oldLocalID = "dal_lib_rollback_old_remote"
	now := time.Now().UTC()
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: oldLocalID, Kind: knowledge.SourceKindText, URI: "knowledge://text/old",
		Title: "Old cached asset", ContentHash: "old", Status: knowledge.StatusParsed,
		FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.meta.Exec(`INSERT INTO enterprise_source_map (library_id, remote_source_id, local_source_id) VALUES (?, ?, ?)`, libraryID, oldRemoteID, oldLocalID); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "replacement.jsonl")
	if err := os.WriteFile(pkg, []byte(`{"type":"source","data":{"id":"new","kind":"text","uri":"knowledge://text/new","title":"New asset","content_hash":"new","status":"parsed"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failingImporter := func(context.Context, knowledge.SnapshotImportOptions) (knowledge.SnapshotImportResult, error) {
		return knowledge.SnapshotImportResult{}, errors.New("injected replacement import failure")
	}
	err = applyPackageWithImporter(ctx, store, c, libraryID, "replace_snapshot", pkg, failingImporter)
	if err == nil || !strings.Contains(err.Error(), "injected replacement import failure") {
		t.Fatalf("applyPackage error = %v", err)
	}
	if _, err := store.GetSource(ctx, oldLocalID); err != nil {
		t.Fatalf("previous source was not restored: %v", err)
	}
	var mappedID string
	if err := c.meta.QueryRow(`SELECT local_source_id FROM enterprise_source_map WHERE library_id=? AND remote_source_id=?`, libraryID, oldRemoteID).Scan(&mappedID); err != nil {
		t.Fatalf("previous source map was not restored: %v", err)
	}
	if mappedID != oldLocalID {
		t.Fatalf("restored map = %q, want %q", mappedID, oldLocalID)
	}
	var newMappings int
	if err := c.meta.QueryRow(`SELECT COUNT(*) FROM enterprise_source_map WHERE library_id=? AND remote_source_id='new'`, libraryID).Scan(&newMappings); err != nil {
		t.Fatal(err)
	}
	if newMappings != 0 {
		t.Fatalf("failed replacement advanced source map with %d new rows", newMappings)
	}
}

func TestApplyUpsertSourcesRemovesStaleNamespacedSources(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	oldID := "dal_lib_upsert_old"
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: oldID, Kind: knowledge.SourceKindText, URI: "knowledge://text/old",
		Title: "Stale asset", ContentHash: "old", Status: knowledge.StatusParsed,
		FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "upsert.jsonl")
	if err := os.WriteFile(pkg, []byte(`{"type":"source","data":{"id":"new","kind":"text","uri":"knowledge://text/new","title":"New asset","content_hash":"new","status":"parsed"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPackage(ctx, store, c, "lib_upsert", "upsert_sources", pkg); err != nil {
		t.Fatal(err)
	}
	sources, err := store.ListSources(ctx, knowledge.ListSourcesOptions{Limit: 10, IncludeDisabled: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, source := range sources {
		seen[source.ID] = true
	}
	if seen[oldID] {
		t.Fatalf("stale source %q survived upsert package", oldID)
	}
	if !seen["dal_lib_upsert_new"] {
		t.Fatalf("upsert source missing: %#v", seen)
	}
}

func TestApplyPackageRebuildsSourceMapWithForeignDALPrefix(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "mapped.jsonl")
	if err := os.WriteFile(pkg, []byte(`{"type":"source","data":{"id":"dal_previous_remote","kind":"text","uri":"knowledge://text/mapped","title":"Mapped asset","content_hash":"mapped","status":"parsed"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPackage(ctx, store, c, "lib_map", "replace_snapshot", pkg); err != nil {
		t.Fatal(err)
	}
	var localID string
	if err := c.meta.QueryRow(`SELECT local_source_id FROM enterprise_source_map WHERE library_id=? AND remote_source_id=?`, "lib_map", "dal_previous_remote").Scan(&localID); err != nil {
		t.Fatal(err)
	}
	if localID != "dal_lib_map_dal_previous_remote" {
		t.Fatalf("mapped local source = %q", localID)
	}
	if _, err := store.GetSource(ctx, localID); err != nil {
		t.Fatalf("mapped source missing from knowledge store: %v", err)
	}
}

func TestApplyPackageRestoresSourceMapAfterImportFailure(t *testing.T) {
	ctx := context.Background()
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	store, err := c.EnsureStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.meta.Exec(`INSERT INTO enterprise_source_map (library_id, remote_source_id, local_source_id) VALUES (?, ?, ?)`, "lib_rollback", "old", "dal_lib_rollback_old"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveSource(ctx, knowledge.Source{
		ID: "dal_lib_rollback_old", Kind: knowledge.SourceKindText, URI: "knowledge://text/old",
		Title: "Old", ContentHash: "old", Status: knowledge.StatusParsed,
		FetchedAt: now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "invalid-after-delete.jsonl")
	data := `{"type":"source","data":{"id":"new","kind":"text","uri":"knowledge://text/new","title":"New","content_hash":"new","status":"parsed"}}
{"type":"source_link","data":{"source_id":"new","related_source_id":"missing","relation":"topic_related"}}
`
	if err := os.WriteFile(pkg, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPackage(ctx, store, c, "lib_rollback", "replace_snapshot", pkg); err == nil {
		t.Fatal("expected invalid package failure")
	}
	var localID string
	if err := c.meta.QueryRow(`SELECT local_source_id FROM enterprise_source_map WHERE library_id=? AND remote_source_id=?`, "lib_rollback", "old").Scan(&localID); err != nil {
		t.Fatalf("previous source map was not restored: %v", err)
	}
	if localID != "dal_lib_rollback_old" {
		t.Fatalf("restored source map = %q", localID)
	}
}

func TestVerifyPackageSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package.jsonl")
	content := []byte("snapshot payload\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if err := verifyPackageSHA256(path, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("expected digest to verify: %v", err)
	}
	if err := verifyPackageSHA256(path, strings.Repeat("0", sha256.Size*2)); err == nil {
		t.Fatal("expected mismatch")
	}
	if err := verifyPackageSHA256(path, "not-a-digest"); err == nil {
		t.Fatal("expected malformed digest rejection")
	}
}

func TestDownloadPackageRejectsHubDeclaredSizeMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("actual package"))
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "package.jsonl")
	err := downloadPackage(context.Background(), srv.Client(), srv.URL, "token", dest, int64(len("actual package")+1))
	if err == nil || !strings.Contains(err.Error(), "package size mismatch") {
		t.Fatalf("expected declared-size mismatch, got %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("mismatched package should be removed, stat error = %v", statErr)
	}
}

func TestDownloadPackageRejectsOversizedHubDeclarationBeforeRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte("unexpected"))
	}))
	defer srv.Close()
	err := downloadPackage(context.Background(), srv.Client(), srv.URL, "token", filepath.Join(t.TempDir(), "package.jsonl"), maxPackageBytes+1)
	if err == nil || !strings.Contains(err.Error(), "declared size") {
		t.Fatalf("expected oversized declared-size rejection, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("oversized package should be rejected before requesting it, got %d requests", requests)
	}
}
