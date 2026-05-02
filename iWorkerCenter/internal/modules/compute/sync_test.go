package compute

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewSyncManager(t *testing.T) {
	sm := NewSyncManager("https://cloud.example.com", "center-1", "secret-abc")
	if sm.cloudURL != "https://cloud.example.com" {
		t.Fatalf("cloudURL = %q, want trimmed URL", sm.cloudURL)
	}
	if sm.centerID != "center-1" {
		t.Fatalf("centerID = %q", sm.centerID)
	}
	if sm.centerSecret != "secret-abc" {
		t.Fatalf("centerSecret = %q", sm.centerSecret)
	}
	status := sm.GetSyncStatus()
	if status.Status != "pending" {
		t.Fatalf("initial status = %q, want pending", status.Status)
	}
}

func TestSyncNowSuccess(t *testing.T) {
	providers := []ComputeProvider{
		{ID: "p1", Name: "OpenAI", Protocol: "openai", Enabled: true},
		{ID: "p2", Name: "Anthropic", Protocol: "anthropic", Enabled: true},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request path and center authentication header
		if r.URL.Path != "/api/centers/center-1/compute-providers" {
			t.Errorf("path = %q, want /api/centers/center-1/compute-providers", r.URL.Path)
		}
		if got := r.Header.Get("X-Center-Secret"); got != "secret-abc" {
			t.Errorf("X-Center-Secret = %q, want secret-abc", got)
		}
		if r.URL.Query().Get("secret") != "" {
			t.Errorf("secret query should not be set, got %q", r.URL.Query().Get("secret"))
		}
		resp := syncResponse{
			Providers:         providers,
			ComputePermission: true,
			ForceSync:         false,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	if err := sm.SyncNow(); err != nil {
		t.Fatalf("SyncNow() error: %v", err)
	}

	got := sm.GetProviders()
	if len(got) != 2 {
		t.Fatalf("providers len = %d, want 2", len(got))
	}
	if got[0].ID != "p1" || got[1].ID != "p2" {
		t.Fatalf("providers = %+v", got)
	}

	status := sm.GetSyncStatus()
	if status.Status != "success" {
		t.Fatalf("status = %q, want success", status.Status)
	}
	if status.ProviderCount != 2 {
		t.Fatalf("provider_count = %d, want 2", status.ProviderCount)
	}
	if status.LastSyncAt == "" {
		t.Fatal("last_sync_at should be set")
	}
	if !sm.GetComputePermission() {
		t.Fatal("compute_permission should be true")
	}
}

func TestSyncNowFailurePreservesProviders(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call succeeds
			resp := syncResponse{
				Providers: []ComputeProvider{
					{ID: "p1", Name: "OpenAI", Protocol: "openai", Enabled: true},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		// Second call fails
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")

	// First sync succeeds
	if err := sm.SyncNow(); err != nil {
		t.Fatalf("first SyncNow() error: %v", err)
	}
	if len(sm.GetProviders()) != 1 {
		t.Fatalf("providers len = %d after first sync, want 1", len(sm.GetProviders()))
	}

	// Second sync fails; providers should be preserved
	if err := sm.SyncNow(); err == nil {
		t.Fatal("second SyncNow() should return error")
	}

	got := sm.GetProviders()
	if len(got) != 1 {
		t.Fatalf("providers len = %d after failed sync, want 1 (preserved)", len(got))
	}
	if got[0].ID != "p1" {
		t.Fatalf("preserved provider ID = %q, want p1", got[0].ID)
	}

	status := sm.GetSyncStatus()
	if status.Status != "failure" {
		t.Fatalf("status = %q, want failure", status.Status)
	}
	if status.Error == "" {
		t.Fatal("error should be set on failure")
	}
	if status.ProviderCount != 1 {
		t.Fatalf("provider_count = %d, want 1 (preserved)", status.ProviderCount)
	}
}

func TestSyncNowCloudUnreachable(t *testing.T) {
	// Point to a non-existent server
	sm := NewSyncManager("http://127.0.0.1:1", "center-1", "secret-abc")
	sm.client.Timeout = 100 * time.Millisecond

	err := sm.SyncNow()
	if err == nil {
		t.Fatal("SyncNow() should return error for unreachable server")
	}

	status := sm.GetSyncStatus()
	if status.Status != "failure" {
		t.Fatalf("status = %q, want failure", status.Status)
	}
}

func TestSyncForceSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := syncResponse{
			Providers:         []ComputeProvider{{ID: "p1", Name: "OpenAI"}},
			ComputePermission: false,
			ForceSync:         true,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	if err := sm.SyncNow(); err != nil {
		t.Fatalf("SyncNow() error: %v", err)
	}

	if !sm.HasForceSync() {
		t.Fatal("HasForceSync() should return true after force_sync response")
	}
	// Second call should return false (flag cleared)
	if sm.HasForceSync() {
		t.Fatal("HasForceSync() should return false after being read")
	}
}

func TestSyncComputePermission(t *testing.T) {
	permGranted := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := syncResponse{
			Providers:         []ComputeProvider{},
			ComputePermission: permGranted,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")

	// Permission granted
	sm.SyncNow()
	if !sm.GetComputePermission() {
		t.Fatal("compute_permission should be true")
	}

	// Permission revoked
	permGranted = false
	sm.SyncNow()
	if sm.GetComputePermission() {
		t.Fatal("compute_permission should be false after revocation")
	}
}

func TestStartAndStop(t *testing.T) {
	syncCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		syncCount++
		resp := syncResponse{Providers: []ComputeProvider{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	sm.interval = 50 * time.Millisecond // fast polling for test

	sm.Start()
	time.Sleep(200 * time.Millisecond)
	sm.Stop()

	// Should have synced at least once (immediate) plus a few ticks
	if syncCount < 1 {
		t.Fatalf("syncCount = %d, want >= 1", syncCount)
	}

	status := sm.GetSyncStatus()
	if status.Status != "success" {
		t.Fatalf("status = %q, want success", status.Status)
	}
}

func TestGetProvidersReturnsCopy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := syncResponse{
			Providers: []ComputeProvider{{ID: "p1", Name: "Test"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	sm.SyncNow()

	// Mutating the returned slice should not affect internal state
	got := sm.GetProviders()
	got[0].Name = "mutated"

	internal := sm.GetProviders()
	if internal[0].Name == "mutated" {
		t.Fatal("GetProviders should return a copy, not a reference to internal state")
	}
}

func TestSyncInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	err := sm.SyncNow()
	if err == nil {
		t.Fatal("SyncNow() should return error for invalid JSON")
	}

	status := sm.GetSyncStatus()
	if status.Status != "failure" {
		t.Fatalf("status = %q, want failure", status.Status)
	}
}

func TestTrailingSlashTrimmed(t *testing.T) {
	sm := NewSyncManager("https://cloud.example.com/", "c1", "s1")
	if sm.cloudURL != "https://cloud.example.com" {
		t.Fatalf("cloudURL = %q, trailing slash not trimmed", sm.cloudURL)
	}
}

func TestSyncManagerWithResolverUsesLatestCredentials(t *testing.T) {
	centerID := ""
	centerSecret := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-1/compute-providers" {
			t.Fatalf("path = %q, want /api/centers/center-1/compute-providers", r.URL.Path)
		}
		if got := r.Header.Get("X-Center-Secret"); got != "secret-abc" {
			t.Fatalf("X-Center-Secret = %q, want secret-abc", got)
		}
		_ = json.NewEncoder(w).Encode(syncResponse{Providers: []ComputeProvider{{ID: "p1", Name: "Cloud GPT"}}})
	}))
	defer srv.Close()

	sm := NewSyncManagerWithResolver(srv.URL, func() (string, string) {
		return centerID, centerSecret
	})
	if !sm.IsConfigured() {
		t.Fatal("resolver-backed sync manager should be configured when cloud URL is set")
	}
	err := sm.SyncNow()
	if !errors.Is(err, ErrWaitingForCredentials) {
		t.Fatalf("SyncNow() error = %v, want ErrWaitingForCredentials", err)
	}
	status := sm.GetSyncStatus()
	if status.Status != "waiting_for_credentials" {
		t.Fatalf("status = %q, want waiting_for_credentials", status.Status)
	}

	centerID = "center-1"
	centerSecret = "secret-abc"
	if err := sm.SyncNow(); err != nil {
		t.Fatalf("SyncNow() after credentials error: %v", err)
	}
	if got := sm.GetProviders(); len(got) != 1 || got[0].ID != "p1" {
		t.Fatalf("providers = %+v, want p1", got)
	}
}

func TestSyncManagerIsConfiguredRequiresStaticSecret(t *testing.T) {
	if NewSyncManager("https://cloud.example.com", "center-1", "").IsConfigured() {
		t.Fatal("static sync manager without center secret should not be configured")
	}
	if !NewSyncManager("https://cloud.example.com", "center-1", "secret-abc").IsConfigured() {
		t.Fatal("static sync manager with center id and secret should be configured")
	}
}

func TestSyncNowMissingStaticCredentialsWaitsForCredentials(t *testing.T) {
	sm := NewSyncManager("https://cloud.example.com", "center-1", "")
	err := sm.SyncNow()
	if !errors.Is(err, ErrWaitingForCredentials) {
		t.Fatalf("SyncNow() error = %v, want ErrWaitingForCredentials", err)
	}
	status := sm.GetSyncStatus()
	if status.Status != "waiting_for_credentials" {
		t.Fatalf("status = %q, want waiting_for_credentials", status.Status)
	}
	if status.Error == "" {
		t.Fatal("status error should explain missing credentials")
	}
}

func TestSyncNowFailureMarksNonBlockingCachedProviders(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(syncResponse{Providers: []ComputeProvider{{ID: "p1", Name: "Cloud GPT"}}})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("cloud down"))
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	if err := sm.SyncNow(); err != nil {
		t.Fatalf("first SyncNow() error: %v", err)
	}
	if err := sm.SyncNow(); err == nil {
		t.Fatal("second SyncNow() should fail")
	}
	status := sm.GetSyncStatus()
	if status.Status != "failure" || !status.NonBlocking || status.RuntimeImpact != "using_cached_cloud_providers" || status.ProviderCount != 1 {
		t.Fatalf("status = %+v, want non-blocking cached provider failure", status)
	}
}

func TestSyncNowFailureWithoutCacheMarksLocalFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	if err := sm.SyncNow(); err == nil {
		t.Fatal("SyncNow() should fail")
	}
	status := sm.GetSyncStatus()
	if status.Status != "failure" || !status.NonBlocking || status.RuntimeImpact != "local_settings_fallback" || status.ProviderCount != 0 {
		t.Fatalf("status = %+v, want non-blocking local fallback failure", status)
	}
}

func TestSyncStatusObserverReceivesUpdatedStatusWithoutDeadlock(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(syncResponse{Providers: []ComputeProvider{{ID: "p1", Name: "Cloud GPT"}}})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("cloud down"))
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	observed := make(chan ComputeSyncStatus, 2)
	sm.SetStatusObserver(func(status ComputeSyncStatus) {
		latest := sm.GetSyncStatus()
		if latest.Status != status.Status || latest.ProviderCount != status.ProviderCount || latest.RuntimeImpact != status.RuntimeImpact {
			t.Errorf("observer saw stale status: callback=%+v latest=%+v", status, latest)
		}
		observed <- status
	})

	if err := sm.SyncNow(); err != nil {
		t.Fatalf("first SyncNow() error: %v", err)
	}
	first := waitObservedStatus(t, observed)
	if first.Status != "success" || first.ProviderCount != 1 || first.RuntimeImpact != "cloud_sync_current" {
		t.Fatalf("first observed status = %+v, want cloud sync success", first)
	}

	if err := sm.SyncNow(); err == nil {
		t.Fatal("second SyncNow() should fail")
	}
	second := waitObservedStatus(t, observed)
	if second.Status != "failure" || !second.NonBlocking || second.ProviderCount != 1 || second.RuntimeImpact != "using_cached_cloud_providers" {
		t.Fatalf("second observed status = %+v, want non-blocking cached provider failure", second)
	}
}

func TestSyncStatusObserverReceivesLocalFallbackFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	sm := NewSyncManager(srv.URL, "center-1", "secret-abc")
	observed := make(chan ComputeSyncStatus, 1)
	sm.SetStatusObserver(func(status ComputeSyncStatus) {
		latest := sm.GetSyncStatus()
		if latest.Status != status.Status || latest.ProviderCount != status.ProviderCount || latest.RuntimeImpact != status.RuntimeImpact {
			t.Errorf("observer saw stale status: callback=%+v latest=%+v", status, latest)
		}
		observed <- status
	})

	if err := sm.SyncNow(); err == nil {
		t.Fatal("SyncNow() should fail")
	}
	status := waitObservedStatus(t, observed)
	if status.Status != "failure" || !status.NonBlocking || status.ProviderCount != 0 || status.RuntimeImpact != "local_settings_fallback" {
		t.Fatalf("observed status = %+v, want non-blocking local fallback failure", status)
	}
}

func waitObservedStatus(t *testing.T, observed <-chan ComputeSyncStatus) ComputeSyncStatus {
	t.Helper()
	select {
	case status := <-observed:
		return status
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sync status observer")
	}
	return ComputeSyncStatus{}
}
