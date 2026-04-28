package compute

import (
	"encoding/json"
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
