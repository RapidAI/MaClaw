package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCapabilitySyncImmediateReason(t *testing.T) {
	for _, reason := range []string{"hub-connect", "hub-config-update", "manual", "startup", " install "} {
		if !isCapabilitySyncImmediateReason(reason) {
			t.Fatalf("reason %q should run immediately", reason)
		}
	}
	if isCapabilitySyncImmediateReason("hub-heartbeat") {
		t.Fatal("heartbeat sync should be throttleable")
	}
}

func TestCapabilityManagedSyncRetryDelayThrottlesHeartbeatNoise(t *testing.T) {
	if got := capabilityManagedSyncRetryDelay([]string{"download skill failed: unexpected EOF"}); got < capabilityManagedSyncMinRetry {
		t.Fatalf("retry delay = %s, want at least %s", got, capabilityManagedSyncMinRetry)
	}
}

func TestCapabilityManagedSyncSuccessDelayThrottlesHeartbeatNoise(t *testing.T) {
	if got := capabilityManagedSyncSuccessDelay("hub-heartbeat"); got != capabilityManagedSyncMinInterval {
		t.Fatalf("heartbeat success delay = %s, want %s", got, capabilityManagedSyncMinInterval)
	}
	if got := capabilityManagedSyncSuccessDelay("hub-config-update"); got != 0 {
		t.Fatalf("config update success delay = %s, want 0", got)
	}
}

func TestCapabilityMarketplaceUnsupportedErrorIgnoresInventory404(t *testing.T) {
	if !isCapabilityMarketplaceUnsupportedError("managed deployments request failed: hub marketplace request failed: status=404 body=map[]") {
		t.Fatal("marketplace endpoint 404 should mark marketplace unsupported")
	}
	if isCapabilityMarketplaceUnsupportedError("inventory report failed: hub marketplace request failed: status=404 body=map[]") {
		t.Fatal("inventory report 404 should not disable marketplace sync")
	}
	if isCapabilityMarketplaceUnsupportedError("hub marketplace request failed: status=404 body=map[]") {
		t.Fatal("capability detail 404 should not disable marketplace sync")
	}
}

func TestAllErrorsAreMarketplace404(t *testing.T) {
	emptyStatus := CapabilitySyncStatus{}

	// All errors are 404, no successes — should trigger circuit breaker
	if !allErrorsAreMarketplace404([]string{
		"hub marketplace request failed: status=404 body_fields=3",
		"hub marketplace request failed: status=404 body_fields=2",
	}, emptyStatus) {
		t.Fatal("all-404 errors with no successes should trigger circuit breaker")
	}
	// Single 404 error, no successes — should trigger
	if !allErrorsAreMarketplace404([]string{"hub marketplace request failed: status=404 body_fields=3"}, emptyStatus) {
		t.Fatal("single 404 error with no successes should trigger circuit breaker")
	}
	// All 404 but some operations succeeded — should NOT trigger (API partially works)
	partialSuccess := CapabilitySyncStatus{RecommendedCount: 3}
	if allErrorsAreMarketplace404([]string{
		"hub marketplace request failed: status=404 body_fields=3",
	}, partialSuccess) {
		t.Fatal("404 with partial success should not trigger circuit breaker")
	}
	// Mixed errors (some 404, some not) — should NOT trigger
	if allErrorsAreMarketplace404([]string{
		"hub marketplace request failed: status=404 body_fields=3",
		"download skill failed: unexpected EOF",
	}, emptyStatus) {
		t.Fatal("mixed errors should not trigger circuit breaker")
	}
	// Non-404 errors — should NOT trigger
	if allErrorsAreMarketplace404([]string{"hub marketplace request failed: status=500 body_fields=1"}, emptyStatus) {
		t.Fatal("non-404 errors should not trigger circuit breaker")
	}
	// Empty errors — should NOT trigger
	if allErrorsAreMarketplace404([]string{}, emptyStatus) {
		t.Fatal("empty error list should not trigger circuit breaker")
	}
	// Only inventory report 404 — should NOT trigger (inventory 404 is expected)
	if allErrorsAreMarketplace404([]string{
		"inventory report failed: hub marketplace request failed: status=404 body_fields=3",
	}, emptyStatus) {
		t.Fatal("inventory-only 404 should not trigger circuit breaker")
	}
	// Capability 404 + inventory report 404 — should trigger (capability 404 is relevant)
	if !allErrorsAreMarketplace404([]string{
		"hub marketplace request failed: status=404 body_fields=3",
		"inventory report failed: hub marketplace request failed: status=404 body_fields=2",
	}, emptyStatus) {
		t.Fatal("capability 404 with inventory 404 should still trigger circuit breaker")
	}
}

func TestCapabilityManagedSyncRetryDelayEscalatesFor404(t *testing.T) {
	// Non-404 errors get the minimum retry delay
	if got := capabilityManagedSyncRetryDelay([]string{"download skill failed: unexpected EOF"}); got != capabilityManagedSyncMinRetry {
		t.Fatalf("non-404 retry delay = %s, want %s", got, capabilityManagedSyncMinRetry)
	}
	// All-404 errors get an escalated 30-minute delay
	if got := capabilityManagedSyncRetryDelay([]string{"hub marketplace request failed: status=404 body_fields=3"}); got != 30*time.Minute {
		t.Fatalf("all-404 retry delay = %s, want 30m", got)
	}
	// Mixed errors (not all 404) get the minimum delay
	if got := capabilityManagedSyncRetryDelay([]string{
		"hub marketplace request failed: status=404 body_fields=3",
		"network timeout",
	}); got != capabilityManagedSyncMinRetry {
		t.Fatalf("mixed retry delay = %s, want %s", got, capabilityManagedSyncMinRetry)
	}
}

func TestIsManagedSkillPackageNotFoundError(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf(`download skill "missing" failed: request failed (404): {"code":"SKILL_NOT_FOUND","message":"skill package not found"}`),
		fmt.Errorf("download skill missing failed: skill package not found"),
	} {
		if !isManagedSkillPackageNotFoundError(err) {
			t.Fatalf("expected permanent package-not-found error to be recognized: %v", err)
		}
	}
	for _, err := range []error{
		fmt.Errorf("download skill failed: unexpected EOF"),
		fmt.Errorf("download skill failed: request failed (404): gateway route not found"),
	} {
		if isManagedSkillPackageNotFoundError(err) {
			t.Fatalf("unexpected permanent package-not-found classification: %v", err)
		}
	}
}

func TestCapabilityMarketplaceUnsupportedCacheResetsWhenHubURLChanges(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: "https://hub-a.example/"}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app.hubMarketplace404URL.Store("https://hub-a.example")
	app.hubMarketplaceUnsupported.Store(true)
	if !app.capabilityMarketplaceUnsupportedForCurrentHub() {
		t.Fatal("same hub URL should keep unsupported cache")
	}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: "https://hub-b.example"}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if app.capabilityMarketplaceUnsupportedForCurrentHub() {
		t.Fatal("new hub URL should clear unsupported cache and allow re-probe")
	}
	if app.hubMarketplaceUnsupported.Load() {
		t.Fatal("unsupported flag should be cleared after hub URL changes")
	}
}

func TestShouldTrackManagedCapabilityDeploymentOnlyRequiredReinstall(t *testing.T) {
	tests := []struct {
		name string
		dep  HubCapabilityDeployment
		want bool
	}{
		{name: "required reinstall", dep: HubCapabilityDeployment{CapabilityRef: "cap-1", DeploymentPolicy: "required", ReinstallIfRemoved: true}, want: true},
		{name: "empty policy defaults required", dep: HubCapabilityDeployment{CapabilityRef: "cap-1", ReinstallIfRemoved: true}, want: true},
		{name: "required no reinstall", dep: HubCapabilityDeployment{CapabilityRef: "cap-1", DeploymentPolicy: "required"}, want: false},
		{name: "blocked reinstall", dep: HubCapabilityDeployment{CapabilityRef: "cap-1", DeploymentPolicy: "blocked", ReinstallIfRemoved: true}, want: false},
		{name: "recommended reinstall", dep: HubCapabilityDeployment{CapabilityRef: "cap-1", DeploymentPolicy: "recommended", ReinstallIfRemoved: true}, want: false},
		{name: "empty ref", dep: HubCapabilityDeployment{DeploymentPolicy: "required", ReinstallIfRemoved: true}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldTrackManagedCapabilityDeployment(tt.dep); got != tt.want {
				t.Fatalf("shouldTrackManagedCapabilityDeployment() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHubSkillCapabilityInstalledChecksCapabilityRef(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	// Status is preset to "active" so loadSkills does not schedule an async
	// persistSkillStatusOverlays write that would race t.TempDir cleanup.
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{Name: "Existing Skill", Status: "active", HubSkillID: "skill-1", Capability: &corelib.SkillCapabilityRef{CapabilityID: "cap-1", VersionKey: "v1"}}}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	installed := HubCapabilitySummary{ID: "cap-1", CapabilityType: corelib.CapabilityTypeSkill, CapabilityID: "skill-1", CurrentVersionKey: "v1"}
	missing := HubCapabilitySummary{ID: "cap-2", CapabilityType: corelib.CapabilityTypeSkill, CapabilityID: "skill-2", CurrentVersionKey: "v1"}
	if !app.isHubSkillCapabilityInstalled(installed) {
		t.Fatal("expected installed capability skill to be detected")
	}
	if app.isHubSkillCapabilityInstalled(missing) {
		t.Fatal("unexpected missing capability skill detected as installed")
	}
	if skillInstallStatus(true) != "installed" || skillInstallStatus(false) != "missing" {
		t.Fatal("unexpected skill install status mapping")
	}
}

func TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSHA256(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillBody := fmt.Sprintf(`{
		"id": "cap-skill-id",
		"name": "Capability Skill",
		"description": "from enterprise hub",
		"version": "1.0.0",
		"trust_level": "trusted",
		"triggers": ["capability skill"],
		"steps": [{"action": "noop", "params": {}, "on_error": "stop"}],
		"files": {
			"skill.yaml": %q,
			"skill.md": %q
		}
	}`,
		base64.StdEncoding.EncodeToString([]byte("name: Capability Skill\ndescription: from enterprise hub\n")),
		base64.StdEncoding.EncodeToString([]byte("# Capability Skill\n")),
	)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer token for %s: %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/capabilities/cap-skill":
			_ = json.NewEncoder(w).Encode(HubCapabilitySummary{
				ID:                "cap-skill",
				CapabilityID:      "cap-skill-id",
				CapabilityType:    corelib.CapabilityTypeSkill,
				Source:            corelib.CapabilitySourceEnterpriseHub,
				Status:            "published",
				CurrentVersionKey: "1.0.0",
				MetadataJSON:      fmt.Sprintf(`{"skill_id":"cap-skill-id","hub_url":%q}`, server.URL),
				PackageSHA256:     strings.Repeat("0", 64),
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/skills/cap-skill-id/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(skillBody))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "token"}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillHubClient = NewSkillHubClient(app)

	status := app.InstallHubCapability("cap-skill")
	if len(status.Errors) == 0 {
		t.Fatalf("InstallHubCapability() errors = none, want checksum mismatch")
	}
	if !strings.Contains(status.Errors[0], "checksum mismatch") {
		t.Fatalf("InstallHubCapability() error = %q, want checksum mismatch", status.Errors[0])
	}
	if status.ManagedInstalled != 0 {
		t.Fatalf("ManagedInstalled = %d, want 0 on checksum mismatch", status.ManagedInstalled)
	}
	if app.isHubSkillCapabilityInstalled(HubCapabilitySummary{ID: "cap-skill", CapabilityID: "cap-skill-id"}) {
		t.Fatal("skill should not be registered after checksum mismatch")
	}
}
func TestInstallHubCapabilityVerifiesEnterpriseSkillPackageSignature(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillBody := fmt.Sprintf(`{
		"id": "cap-signed-skill-id",
		"name": "Signed Capability Skill",
		"description": "from enterprise hub",
		"version": "1.0.0",
		"trust_level": "trusted",
		"triggers": ["signed capability skill"],
		"steps": [{"action": "noop", "params": {}, "on_error": "stop"}],
		"files": {
			"skill.yaml": %q,
			"skill.md": %q
		}
	}`,
		base64.StdEncoding.EncodeToString([]byte("name: Signed Capability Skill\ndescription: from enterprise hub\n")),
		base64.StdEncoding.EncodeToString([]byte("# Signed Capability Skill\n")),
	)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	badSignature := "ed25519:" + base64.StdEncoding.EncodeToString(publicKey) + ":" + base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte("tampered")))

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer token for %s: %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/capabilities/cap-signed-skill":
			_ = json.NewEncoder(w).Encode(HubCapabilitySummary{
				ID:                "cap-signed-skill",
				CapabilityID:      "cap-signed-skill-id",
				CapabilityType:    corelib.CapabilityTypeSkill,
				Source:            corelib.CapabilitySourceEnterpriseHub,
				Status:            "published",
				CurrentVersionKey: "1.0.0",
				MetadataJSON:      fmt.Sprintf(`{"skill_id":"cap-signed-skill-id","hub_url":%q}`, server.URL),
				PackageSignature:  badSignature,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/skills/cap-signed-skill-id/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(skillBody))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "token"}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillHubClient = NewSkillHubClient(app)

	status := app.InstallHubCapability("cap-signed-skill")
	if len(status.Errors) == 0 {
		t.Fatalf("InstallHubCapability() errors = none, want signature verification failure")
	}
	if !strings.Contains(status.Errors[0], "signature verification failed") {
		t.Fatalf("InstallHubCapability() error = %q, want signature verification failure", status.Errors[0])
	}
	if status.ManagedInstalled != 0 {
		t.Fatalf("ManagedInstalled = %d, want 0 on signature mismatch", status.ManagedInstalled)
	}
	if app.isHubSkillCapabilityInstalled(HubCapabilitySummary{ID: "cap-signed-skill", CapabilityID: "cap-signed-skill-id"}) {
		t.Fatal("skill should not be registered after signature verification failure")
	}
}

func TestEmitHubManagedCapabilitySyncEventIncludesErrors(t *testing.T) {
	app := &App{}
	if shouldEmitHubManagedCapabilitySyncEvent(CapabilitySyncStatus{}) {
		t.Fatal("empty sync status should not emit")
	}
	if !shouldEmitHubManagedCapabilitySyncEvent(CapabilitySyncStatus{Errors: []string{"install failed"}}) {
		t.Fatal("sync errors should emit")
	}
	if !shouldEmitHubManagedCapabilitySyncEvent(CapabilitySyncStatus{NeedsUserConfig: []string{"cap-1"}}) {
		t.Fatal("needs config should emit")
	}
	app.emitHubManagedCapabilitySyncEvent(CapabilitySyncStatus{})
}

func TestCapabilityInventoryReportUsesSnapshotEndpoint(t *testing.T) {
	var got struct {
		Items        []HubCapabilityInventoryItem `json:"items"`
		FullSnapshot bool                         `json:"full_snapshot"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/capabilities/inventory" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer token: %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := &capabilityMarketClient{baseURL: server.URL, token: "token", http: server.Client()}
	err := client.reportInventory(context.Background(), HubCapabilityInventoryReport{FullSnapshot: true, Items: []HubCapabilityInventoryItem{{CapabilityRef: "cap-1", CapabilityType: "skill", CapabilityVersionKey: "v1", InstallStatus: "installed", Installed: true}}})
	if err != nil {
		t.Fatalf("report inventory: %v", err)
	}
	if !got.FullSnapshot || len(got.Items) != 1 || got.Items[0].CapabilityRef != "cap-1" || !got.Items[0].Installed {
		t.Fatalf("unexpected inventory payload: %+v", got)
	}
}

func TestCapabilityInventorySnapshotPreservesMCPNeedsConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var got struct {
		Items        []HubCapabilityInventoryItem `json:"items"`
		FullSnapshot bool                         `json:"full_snapshot"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/capabilities/cap-mcp/mcp-secret-requirements":
			_, _ = w.Write([]byte(`{"items":[{"name":"api_key","scope":"user","storage_policy":"hub","required":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/capabilities/mcp-secret-bindings":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/capabilities/mcp-hub-secrets":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/capabilities/inventory":
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{{ID: "srv-1", Name: "Remote MCP", Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap-mcp", VersionKey: "v1"}}}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	client := &capabilityMarketClient{baseURL: server.URL, token: "token", http: server.Client()}
	reported, err := app.reportHubCapabilityInventorySnapshot(context.Background(), client)
	if err != nil {
		t.Fatalf("report snapshot: %v", err)
	}
	if reported != 1 || !got.FullSnapshot || len(got.Items) != 1 {
		t.Fatalf("unexpected snapshot: reported=%d payload=%+v", reported, got)
	}
	item := got.Items[0]
	if item.CapabilityRef != "cap-mcp" || item.InstallStatus != "needs_config" || item.Installed {
		t.Fatalf("snapshot should preserve needs_config MCP status: %+v", item)
	}
}
