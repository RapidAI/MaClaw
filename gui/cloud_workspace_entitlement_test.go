package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func configureCloudWorkspaceEntitlementTestApp(t *testing.T, hubURL string) *App {
	t.Helper()
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hubURL,
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "machine-test",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return app
}

func TestCloudWorkspaceEntitlementDisabledDoesNotMarkHubUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cloudWorkspaceEntitlementPath {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer machine-token" {
			t.Errorf("Authorization=%q", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "machine-test" {
			t.Errorf("X-Machine-ID=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":false,"quota":5,"used":0,"max_workspace_bytes":2147483648,"workspaces":[],"deleted":[]}`))
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	ent := app.CloudWorkspaceEntitlement()
	if ent.Enabled {
		t.Fatalf("enabled=%v, want false from Hub", ent.Enabled)
	}
	if ent.HubUnavailable {
		t.Fatalf("hub unavailable on successful disabled entitlement")
	}
	if ent.Banner != "" {
		t.Fatalf("banner=%q, want empty", ent.Banner)
	}
	if ent.Quota != 5 || ent.MaxWorkspaceBytes != 2147483648 {
		t.Fatalf("quota/bytes=%d/%d", ent.Quota, ent.MaxWorkspaceBytes)
	}
	if ent.Workspaces == nil || ent.Deleted == nil {
		t.Fatalf("nil slices: workspaces=%v deleted=%v", ent.Workspaces, ent.Deleted)
	}
}

func TestCloudWorkspaceEntitlementEnabledCachesWorkspaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"enabled":true,
			"quota":5,
			"used":1,
			"max_workspace_bytes":2147483648,
			"workspaces":[{"id":"cws_0123456789abcdef0123456789abcdef","name":"标书项目","used_bytes":10,"updated_at":"2026-08-28T10:00:00Z"}],
			"deleted":[{"id":"cws_dead","name":"旧项目","deleted_at":"2026-08-27T10:00:00Z","purge_after":"2026-09-03T10:00:00Z"}]
		}`))
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	ent := app.CloudWorkspaceEntitlement()
	if !ent.Enabled || ent.Used != 1 || len(ent.Workspaces) != 1 || ent.Workspaces[0].Name != "标书项目" {
		t.Fatalf("ent=%+v", ent)
	}
	if len(ent.Deleted) != 1 || ent.Deleted[0].Name != "旧项目" {
		t.Fatalf("deleted=%+v", ent.Deleted)
	}
	if ent.HubUnavailable || ent.Banner != "" {
		t.Fatalf("unexpected hub-down flags: %+v", ent)
	}
}

func TestCloudWorkspaceEntitlementHub5xxKeepsCacheAndDoesNotFakeDisabled(t *testing.T) {
	var mu sync.Mutex
	enabled := false
	status := http.StatusInternalServerError
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		curStatus := status
		curEnabled := enabled
		mu.Unlock()
		if curStatus != http.StatusOK {
			w.WriteHeader(curStatus)
			_, _ = w.Write([]byte(`{"code":"CLOUD_WORKSPACE_FAILED"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{
			"enabled":             curEnabled,
			"quota":               5,
			"used":                1,
			"max_workspace_bytes": 2147483648,
			"workspaces":          []any{},
			"deleted":             []any{},
		})
		_, _ = w.Write(body)
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)

	down := app.CloudWorkspaceEntitlement()
	if down.Enabled {
		t.Fatalf("no-cache 5xx must not report enabled=true: %+v", down)
	}
	if !down.HubUnavailable || down.Banner != cloudWorkspaceHubUnavailableBanner {
		t.Fatalf("no-cache 5xx should flag hub down: %+v", down)
	}

	mu.Lock()
	status = http.StatusOK
	enabled = true
	mu.Unlock()
	ok := app.CloudWorkspaceEntitlement()
	if !ok.Enabled || ok.HubUnavailable {
		t.Fatalf("successful grant: %+v", ok)
	}

	mu.Lock()
	status = http.StatusInternalServerError
	mu.Unlock()
	cached := app.CloudWorkspaceEntitlement()
	if !cached.Enabled {
		t.Fatalf("5xx after grant must keep enabled=true, got %+v", cached)
	}
	if !cached.HubUnavailable || cached.Banner != cloudWorkspaceHubUnavailableBanner {
		t.Fatalf("5xx after grant should keep cache and show banner: %+v", cached)
	}
}

func TestCloudWorkspaceEntitlementNetworkErrorKeepsLastGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"enabled":true,"quota":3,"used":0,"max_workspace_bytes":1,"workspaces":[],"deleted":[]}`))
	}))
	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	first := app.CloudWorkspaceEntitlement()
	if !first.Enabled {
		t.Fatalf("first=%+v", first)
	}
	server.Close()

	down := app.CloudWorkspaceEntitlement()
	if !down.Enabled {
		t.Fatalf("network error faked enabled=false: %+v", down)
	}
	if !down.HubUnavailable || down.Quota != 3 {
		t.Fatalf("want cached grant + banner, got %+v", down)
	}
}

func TestCloudWorkspaceEntitlementMapsHubNestedLease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"enabled":true,
			"quota":5,
			"used":1,
			"max_workspace_bytes":2147483648,
			"workspaces":[{
				"id":"cws_0123456789abcdef0123456789abcdef",
				"name":"标书项目",
				"used_bytes":10,
				"updated_at":"2026-08-28T10:00:00Z",
				"lease":{"held":true,"machine_id":"m-other","machine_name":"other-pc","is_self":false,"expires_at":"2026-08-28T10:01:30Z"}
			}],
			"deleted":[]
		}`))
	}))
	defer server.Close()

	app := configureCloudWorkspaceEntitlementTestApp(t, server.URL)
	ent := app.CloudWorkspaceEntitlement()
	if len(ent.Workspaces) != 1 {
		t.Fatalf("workspaces=%+v", ent.Workspaces)
	}
	ws := ent.Workspaces[0]
	if !ws.LeaseInUse || ws.LeaseHolder != "other-pc" {
		t.Fatalf("hub nested lease should map to occupied: %+v", ws)
	}
}

func TestCloudWorkspaceEntitlementSelfLeaseIsNotInUse(t *testing.T) {
	raw := []byte(`{"id":"cws_a","name":"A","used_bytes":1,"updated_at":"t","lease":{"held":true,"machine_id":"m1","machine_name":"HOST-M1","is_self":true}}`)
	var ws CloudWorkspaceEntitlementWorkspace
	if err := json.Unmarshal(raw, &ws); err != nil {
		t.Fatal(err)
	}
	if ws.LeaseInUse || ws.LeaseHolder != "" {
		t.Fatalf("self lease must not occupy: %+v", ws)
	}
}

func TestCloudWorkspaceEntitlementLeaseFallsBackToMachineID(t *testing.T) {
	raw := []byte(`{"id":"cws_a","name":"A","used_bytes":1,"updated_at":"t","lease":{"held":true,"machine_id":"mid","machine_name":"","is_self":false}}`)
	var ws CloudWorkspaceEntitlementWorkspace
	if err := json.Unmarshal(raw, &ws); err != nil {
		t.Fatal(err)
	}
	if !ws.LeaseInUse || ws.LeaseHolder != "mid" {
		t.Fatalf("empty machine_name should fall back to machine_id: %+v", ws)
	}
}

func TestCloudWorkspaceEntitlementMissingHubConfigIsUnavailableNotDisabled(t *testing.T) {
	resetCloudWorkspaceEntitlementCache()
	t.Cleanup(resetCloudWorkspaceEntitlementCache)
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	ent := app.CloudWorkspaceEntitlement()
	if ent.Enabled {
		t.Fatalf("unconfigured hub must not report enabled: %+v", ent)
	}
	if !ent.HubUnavailable {
		t.Fatalf("unconfigured hub should be HubUnavailable: %+v", ent)
	}
	if !strings.Contains(ent.Banner, "Hub") {
		t.Fatalf("banner=%q", ent.Banner)
	}
}
