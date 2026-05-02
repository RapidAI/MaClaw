package compute

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestSyncManager creates a SyncManager backed by a test server that
// returns the given providers, permission, and forceSync flags.
func newTestSyncManager(providers []ComputeProvider, permission, forceSync bool) (*SyncManager, *httptest.Server) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := syncResponse{
			Providers:         providers,
			ComputePermission: permission,
			ForceSync:         forceSync,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	sm := NewSyncManager(srv.URL, "center-1", "secret")
	sm.SyncNow()
	return sm, srv
}

func TestNewSourceManagerDefaultsToCloud(t *testing.T) {
	sm, srv := newTestSyncManager(nil, false, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	if src.GetSource() != "cloud" {
		t.Fatalf("default source = %q, want cloud", src.GetSource())
	}
}

func TestSetSourceToLocalRequiresPermission(t *testing.T) {
	sm, srv := newTestSyncManager(nil, false, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	err := src.SetSource("local")
	if err != ErrNoPermission {
		t.Fatalf("SetSource(local) without permission: err = %v, want ErrNoPermission", err)
	}
	if src.GetSource() != "cloud" {
		t.Fatalf("source should remain cloud after denied switch")
	}
}

func TestSetSourceToLocalWithPermission(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	if err := src.SetSource("local"); err != nil {
		t.Fatalf("SetSource(local) with permission: %v", err)
	}
	if src.GetSource() != "local" {
		t.Fatalf("source = %q, want local", src.GetSource())
	}
}

func TestSetSourceInvalidValue(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	err := src.SetSource("hybrid")
	if err != ErrInvalidSource {
		t.Fatalf("SetSource(hybrid): err = %v, want ErrInvalidSource", err)
	}
}

func TestSetSourceBackToCloud(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")
	if err := src.SetSource("cloud"); err != nil {
		t.Fatalf("SetSource(cloud): %v", err)
	}
	if src.GetSource() != "cloud" {
		t.Fatalf("source = %q, want cloud", src.GetSource())
	}
}

func TestGetActiveProvidersCloudMode(t *testing.T) {
	cloudProviders := []ComputeProvider{
		{ID: "cp1", Name: "CloudProvider1"},
		{ID: "cp2", Name: "CloudProvider2"},
	}
	sm, srv := newTestSyncManager(cloudProviders, false, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	got := src.GetActiveProviders()
	if len(got) != 2 {
		t.Fatalf("active providers len = %d, want 2", len(got))
	}
	if got[0].ID != "cp1" || got[1].ID != "cp2" {
		t.Fatalf("unexpected providers: %+v", got)
	}
}

func TestGetActiveProvidersLocalMode(t *testing.T) {
	cloudProviders := []ComputeProvider{{ID: "cp1", Name: "CloudOnly"}}
	sm, srv := newTestSyncManager(cloudProviders, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")

	localProviders := []ComputeProvider{
		{ID: "lp1", Name: "LocalProvider1"},
	}
	src.SetLocalProviders(localProviders)

	got := src.GetActiveProviders()
	if len(got) != 1 {
		t.Fatalf("active providers len = %d, want 1", len(got))
	}
	if got[0].ID != "lp1" {
		t.Fatalf("expected local provider lp1, got %q", got[0].ID)
	}
}

func TestIsLocalEditAllowed(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)

	// cloud mode — not allowed
	if src.IsLocalEditAllowed() {
		t.Fatal("IsLocalEditAllowed should be false in cloud mode")
	}

	// local mode with permission — allowed
	src.SetSource("local")
	if !src.IsLocalEditAllowed() {
		t.Fatal("IsLocalEditAllowed should be true in local mode with permission")
	}
}

func TestIsLocalEditAllowedNoPermission(t *testing.T) {
	// Start with permission to switch to local, then revoke via a new sync
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")

	// Simulate permission revocation by syncing with permission=false
	srv.Close()
	noPermSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := syncResponse{ComputePermission: false}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer noPermSrv.Close()

	sm2 := NewSyncManager(noPermSrv.URL, "center-1", "secret")
	sm2.SyncNow()

	src2 := &SourceManager{source: "local", syncMgr: sm2}
	if src2.IsLocalEditAllowed() {
		t.Fatal("IsLocalEditAllowed should be false when permission is revoked")
	}
}

func TestHandleForceSync(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")
	src.SetLocalProviders([]ComputeProvider{{ID: "lp1", Name: "Local"}})

	src.HandleForceSync()

	if src.GetSource() != "cloud" {
		t.Fatalf("source = %q after HandleForceSync, want cloud", src.GetSource())
	}
	if len(src.GetLocalProviders()) != 0 {
		t.Fatalf("local providers should be cleared after HandleForceSync")
	}
}

func TestCheckForceSyncTriggersRevert(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, true) // force_sync = true
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")
	src.SetLocalProviders([]ComputeProvider{{ID: "lp1"}})

	src.CheckForceSync()

	if src.GetSource() != "cloud" {
		t.Fatalf("source = %q after CheckForceSync, want cloud", src.GetSource())
	}
	if len(src.GetLocalProviders()) != 0 {
		t.Fatal("local providers should be cleared after CheckForceSync")
	}
}

func TestCheckForceSyncNoOp(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false) // force_sync = false
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")
	src.SetLocalProviders([]ComputeProvider{{ID: "lp1"}})

	src.CheckForceSync()

	if src.GetSource() != "local" {
		t.Fatalf("source = %q, should remain local when no force_sync", src.GetSource())
	}
	if len(src.GetLocalProviders()) != 1 {
		t.Fatal("local providers should be preserved when no force_sync")
	}
}

func TestSetLocalProvidersInCloudMode(t *testing.T) {
	sm, srv := newTestSyncManager(nil, false, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	err := src.SetLocalProviders([]ComputeProvider{{ID: "lp1"}})
	if err != ErrNotLocalMode {
		t.Fatalf("SetLocalProviders in cloud mode: err = %v, want ErrNotLocalMode", err)
	}
}

func TestSetLocalProvidersMakesCopy(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")

	original := []ComputeProvider{{ID: "lp1", Name: "Original"}}
	src.SetLocalProviders(original)

	// Mutate the original slice
	original[0].Name = "Mutated"

	got := src.GetLocalProviders()
	if got[0].Name == "Mutated" {
		t.Fatal("SetLocalProviders should copy the slice, not reference it")
	}
}

func TestGetLocalProvidersReturnsCopy(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")
	src.SetLocalProviders([]ComputeProvider{{ID: "lp1", Name: "Original"}})

	got := src.GetLocalProviders()
	got[0].Name = "Mutated"

	internal := src.GetLocalProviders()
	if internal[0].Name == "Mutated" {
		t.Fatal("GetLocalProviders should return a copy")
	}
}

func TestGetActiveProvidersReturnsCopyInLocalMode(t *testing.T) {
	sm, srv := newTestSyncManager(nil, true, false)
	defer srv.Close()

	src := NewSourceManager(sm)
	src.SetSource("local")
	src.SetLocalProviders([]ComputeProvider{{ID: "lp1", Name: "Original"}})

	got := src.GetActiveProviders()
	got[0].Name = "Mutated"

	internal := src.GetActiveProviders()
	if internal[0].Name == "Mutated" {
		t.Fatal("GetActiveProviders should return a copy in local mode")
	}
}

func TestGetActiveProvidersFallsBackToLocalWhenCloudUnavailable(t *testing.T) {
	sm := NewSyncManager("http://127.0.0.1:1", "center-1", "secret")
	src := NewSourceManager(sm)
	src.SetFallbackProvidersResolver(func() []ComputeProvider {
		return []ComputeProvider{{ID: "local-1", Name: "Local Backup"}}
	})

	if err := sm.SyncNow(); err == nil {
		t.Fatal("SyncNow should fail for unreachable cloud")
	}
	got := src.GetActiveProviders()
	if len(got) != 1 || got[0].ID != "local-1" {
		t.Fatalf("active providers = %+v, want local fallback", got)
	}
}

func TestGetActiveProvidersUsesCloudWhenSyncCurrent(t *testing.T) {
	sm, srv := newTestSyncManager([]ComputeProvider{{ID: "cloud-1", Name: "Cloud"}}, false, false)
	defer srv.Close()
	src := NewSourceManager(sm)
	src.SetFallbackProvidersResolver(func() []ComputeProvider {
		return []ComputeProvider{{ID: "local-1", Name: "Local Backup"}}
	})

	got := src.GetActiveProviders()
	if len(got) != 1 || got[0].ID != "cloud-1" {
		t.Fatalf("active providers = %+v, want cloud provider", got)
	}
}

func TestGetActiveProvidersDoesNotFallbackOnSuccessfulEmptyCloudSync(t *testing.T) {
	sm, srv := newTestSyncManager([]ComputeProvider{}, false, false)
	defer srv.Close()
	src := NewSourceManager(sm)
	src.SetFallbackProvidersResolver(func() []ComputeProvider {
		return []ComputeProvider{{ID: "local-1", Name: "Local Backup"}}
	})

	got := src.GetActiveProviders()
	if len(got) != 0 {
		t.Fatalf("active providers = %+v, want successful empty cloud list", got)
	}
}
