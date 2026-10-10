package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestReconcileManagedMCPRuntimeSyncOnStartupRetriesDueHTTPState(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{}
		if req.Method == "tools/list" {
			result = map[string]any{"tools": []any{}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{{
		ID: "restart-managed", Name: "Restart managed", EndpointURL: server.URL,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.resetMCPRuntimeSyncPending("restart-managed", "http"); err != nil {
		t.Fatal(err)
	}
	app.reconcileManagedMCPRuntimeSyncOnStartup(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := loadMCPRuntimeSyncState("restart-managed"); ok && state.Status == "ready" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, _ := loadMCPRuntimeSyncState("restart-managed")
	t.Fatalf("startup reconciliation did not publish ready marker: %+v", state)
}

func TestReconcileManagedMCPRuntimeSyncLocalFailureDoesNotDoubleCount(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	failed := corelib.LocalMCPServerEntry{
		ID: "startup-failed-local", Name: "Startup failed local", Command: "definitely-missing-maclaw-mcp-command",
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap-failed"}, AutoStart: true,
	}
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{failed}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.resetMCPRuntimeSyncPending(failed.ID, "stdio"); err != nil {
		t.Fatal(err)
	}
	app.reconcileManagedMCPRuntimeSyncOnStartup(context.Background())
	t.Cleanup(func() {
		if app.localMCPManager != nil {
			app.localMCPManager.StopAll()
		}
	})
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if first, ok := loadMCPRuntimeSyncState(failed.ID); ok && first.Attempts >= 1 {
			if first.Attempts != 1 {
				t.Fatalf("local runtime attempts=%d, want exactly one manager-owned failure", first.Attempts)
			}
			if first.Status != "pending" {
				t.Fatalf("local runtime status=%q, want pending after first failure", first.Status)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	first, _ := loadMCPRuntimeSyncState(failed.ID)
	t.Fatalf("failed local runtime state was not persisted: %+v", first)
}

func TestMCPRuntimeSyncFailurePersistsBoundedReviewState(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{}
	for i := 1; i <= mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure("srv", "http", "req", os.ErrDeadlineExceeded); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	state, ok := loadMCPRuntimeSyncState("srv")
	if !ok || state.Status != "needs_review" || state.Attempts != mcpRuntimeSyncMaxAttempts {
		t.Fatalf("state = %+v, ok=%v", state, ok)
	}
	if app.mcpRuntimeSyncShouldRetry("srv") {
		t.Fatal("needs_review runtime state should not retry automatically")
	}
	if !app.mcpRuntimeSyncPending("srv") {
		t.Fatal("needs_review runtime state must remain an admission blocker")
	}
	if err := app.markMCPRuntimeSyncReady("srv"); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if _, ok := loadMCPRuntimeSyncState("srv"); ok {
		t.Fatal("ready transition did not clear durable failure state")
	}
}

func TestFinalizeLocalMCPRuntimeSyncPreservesTerminalState(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{}
	for i := 0; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure("terminal-local", "stdio", "req", context.DeadlineExceeded); err != nil {
			t.Fatal(err)
		}
	}
	before, ok := loadMCPRuntimeSyncState("terminal-local")
	if !ok || before.Status != "needs_review" {
		t.Fatalf("initial state = %+v, ok=%v; want terminal needs_review", before, ok)
	}
	// A stale startup target list must not turn a newer terminal blocker into a
	// ready marker when the checked manager has no running process.
	app.finalizeLocalMCPRuntimeSync(nil, []string{"terminal-local"})
	after, ok := loadMCPRuntimeSyncState("terminal-local")
	if !ok || after != before {
		t.Fatalf("terminal state changed during finalize: before=%+v after=%+v ok=%v", before, after, ok)
	}
}

func TestMCPRuntimeSyncPersistenceFailureFenceBlocksStaleReady(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	app.mcpRegistry = NewMCPRegistry(app)
	managed := corelib.LocalMCPServerEntry{
		ID: "stale-ready", Source: corelib.MCPSourceMarket,
		Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"}, AutoStart: true,
	}
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{managed}}); err != nil {
		t.Fatal(err)
	}
	// Simulate a failed durable write after an older ready marker remained on
	// disk. The process-local fence must take precedence over that stale state.
	if err := writeMCPRuntimeSyncStates(map[string]MCPRuntimeSyncState{
		managed.ID: {ServerID: managed.ID, Status: "ready", UpdatedAt: time.Now().UTC().Format(time.RFC3339)},
	}); err != nil {
		t.Fatal(err)
	}
	key := mcpRuntimeSyncPersistenceKey(managed.ID)
	mcpRuntimeSyncStateMu.Lock()
	mcpRuntimeSyncPersistenceFailures[key] = true
	mcpRuntimeSyncStateMu.Unlock()
	t.Cleanup(func() {
		mcpRuntimeSyncStateMu.Lock()
		delete(mcpRuntimeSyncPersistenceFailures, key)
		mcpRuntimeSyncStateMu.Unlock()
	})
	if !app.mcpRuntimeSyncPending(managed.ID) {
		t.Fatal("persistence failure fence must keep runtime pending")
	}
	if app.mcpRuntimeSyncShouldRetry(managed.ID) {
		t.Fatal("persistence failure fence must suppress automatic retry")
	}
	if app.mcpRuntimeSyncAllowsAutomaticStart(managed) {
		t.Fatal("persistence failure fence must block automatic start")
	}
}

func TestMCPRuntimeSyncStateCorruptionFailsClosed(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	path := filepath.Join(base, "skill_evolution", "mcp_runtime_sync.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !(&App{}).mcpRuntimeSyncPending("srv") {
		t.Fatal("corrupt runtime state must fail closed")
	}
}

func TestMCPRuntimeSyncStateInvalidTimestampFailsClosed(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	path := filepath.Join(base, "skill_evolution", "mcp_runtime_sync.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := `{"srv":{"server_id":"srv","status":"pending","attempts":1,"next_retry_at":"not-a-time","updated_at":"2026-08-31T00:00:00Z"}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if !(&App{}).mcpRuntimeSyncPending("srv") {
		t.Fatal("invalid retry timestamp must fail closed")
	}
	if (&App{}).mcpRuntimeSyncShouldRetry("srv") {
		t.Fatal("invalid retry timestamp must not authorize an automatic retry")
	}
}

func TestMCPRuntimeSyncMissingManagedStateFailsClosed(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{{
		ID: "managed-missing", Name: "Managed", EndpointURL: "https://example.invalid/mcp",
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if !app.mcpRuntimeSyncPending("managed-missing") {
		t.Fatal("managed MCP without durable readiness state must be blocked")
	}
}

func TestMCPRuntimeSyncPendingResetStartsFreshBoundedCycle(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{}
	for i := 0; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure("srv", "stdio", "stale-request", os.ErrNotExist); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.resetMCPRuntimeSyncPending("srv", "stdio"); err != nil {
		t.Fatal(err)
	}
	state, ok := loadMCPRuntimeSyncState("srv")
	if !ok || state.Status != "pending" || state.Attempts != 0 || state.LastError != "" || state.RequestID != "" {
		t.Fatalf("reset state = %+v, ok=%v", state, ok)
	}
}

func TestMCPRuntimeSyncReadyMarkerPersistsForManagedServer(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{{
		ID:          "managed-http",
		Name:        "Managed HTTP",
		EndpointURL: "https://example.invalid/mcp",
		Source:      corelib.MCPSourceMarket,
		Capability:  &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.markMCPRuntimeSyncReady("managed-http"); err != nil {
		t.Fatal(err)
	}
	state, ok := loadMCPRuntimeSyncState("managed-http")
	if !ok || state.Status != "ready" {
		t.Fatalf("ready state = %+v, ok=%v", state, ok)
	}
	if app.mcpRuntimeSyncPending("managed-http") {
		t.Fatal("persisted ready marker must not block runtime")
	}
}

func TestAutoStartLocalMCPServersHonorsDurableRuntimeBlocker(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	entry := corelib.LocalMCPServerEntry{
		ID: "managed-autostart-blocked", Name: "Managed blocked", Command: "missing-command",
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
		AutoStart: true,
	}
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	// A failed probe schedules a future retry. Generic startup AutoStart must
	// not bypass that durable backoff and launch the process immediately.
	if err := app.recordMCPRuntimeSyncFailure(entry.ID, "stdio", "req", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	app.autoStartLocalMCPServers([]corelib.LocalMCPServerEntry{entry})
	if app.localMCPManager != nil {
		t.Fatal("AutoStart bypassed pending runtime retry backoff")
	}

	// Exhausted retries are terminal until configuration changes or an
	// explicit operator action re-arms the state; startup must remain inert.
	for i := 1; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure(entry.ID, "stdio", "req", context.DeadlineExceeded); err != nil {
			t.Fatal(err)
		}
	}
	state, ok := loadMCPRuntimeSyncState(entry.ID)
	if !ok || state.Status != "needs_review" {
		t.Fatalf("runtime state = %+v, ok=%v; want needs_review", state, ok)
	}
	app.autoStartLocalMCPServers([]corelib.LocalMCPServerEntry{entry})
	if app.localMCPManager != nil {
		t.Fatal("AutoStart bypassed terminal needs_review runtime blocker")
	}
}

func TestLocalMCPManagerSyncSkipsBlockedManagedSibling(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	blocked := corelib.LocalMCPServerEntry{
		ID: "managed-sibling-blocked", Name: "Managed sibling blocked", Command: "missing-command",
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap-blocked"},
		AutoStart: true,
	}
	healthy := newHelperLocalMCPServerEntry("manual-sibling-healthy", false, true)
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{blocked, healthy}}); err != nil {
		t.Fatal(err)
	}
	if err := app.recordMCPRuntimeSyncFailure(blocked.ID, "stdio", "req", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	app.autoStartLocalMCPServers([]corelib.LocalMCPServerEntry{blocked, healthy})
	t.Cleanup(func() {
		if app.localMCPManager != nil {
			app.localMCPManager.StopAll()
		}
	})
	waitForLocalMCPRunning(t, app.localMCPManager, healthy.ID, true)
	waitForLocalMCPRunning(t, app.localMCPManager, blocked.ID, false)
	if state, ok := loadMCPRuntimeSyncState(blocked.ID); !ok || state.Status != "pending" {
		t.Fatalf("blocked sibling runtime state = %+v, ok=%v; manager sync must not retry it", state, ok)
	}
}

func TestSyncLocalMCPServersReportsBlockedManagedRuntime(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	app.mcpRegistry = NewMCPRegistry(app)
	entry := corelib.LocalMCPServerEntry{
		ID: "managed-sync-blocked", Name: "Managed sync blocked", Command: "missing-command",
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
		AutoStart: true,
	}
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	if err := app.recordMCPRuntimeSyncFailure(entry.ID, "stdio", "req", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	err := app.SyncLocalMCPServers()
	if err == nil || !strings.Contains(err.Error(), entry.ID) {
		t.Fatalf("SyncLocalMCPServers() error = %v, want durable blocker for %s", err, entry.ID)
	}
	if state, ok := loadMCPRuntimeSyncState(entry.ID); !ok || state.Status != "pending" {
		t.Fatalf("runtime state = %+v, ok=%v; checked sync must not mark blocked runtime ready", state, ok)
	}
}

func mcpToolsListServer(t *testing.T, listHits *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "tools/list" && listHits != nil {
			listHits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{}
		if req.Method == "tools/list" {
			result = map[string]any{"tools": []any{map[string]any{
				"name":        "work_log_query",
				"description": "query",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
			}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server
}

func waitMCPObserved(t *testing.T, registry *MCPRegistry, serverID string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		status, tools, observed := registry.remoteHealthObservation(serverID)
		if status == mcpHealthStatusHealthy && observed && len(tools) == 1 && tools[0].Name == "work_log_query" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	status, tools, observed := registry.remoteHealthObservation(serverID)
	t.Fatalf("observation server=%s status=%s observed=%v tools=%#v", serverID, status, observed, tools)
}

func TestRemoteMCPDueForObservation(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	market := corelib.MCPServerEntry{
		ID: "market-due", Name: "market", EndpointURL: "https://example.invalid/mcp",
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}
	manual := corelib.MCPServerEntry{ID: "manual-due", Name: "manual", EndpointURL: "https://example.invalid/manual", Source: corelib.MCPSourceManual}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{market, manual}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if !app.remoteMCPDueForObservation(manual, false) {
		t.Fatal("manual remote is always due for a process-local observation")
	}
	if app.remoteMCPDueForObservation(corelib.MCPServerEntry{ID: "manual-due", Source: corelib.MCPSourceManual}, false) {
		t.Fatal("remote without an endpoint is not observable")
	}
	if app.remoteMCPDueForObservation(market, false) {
		t.Fatal("missing managed state is owned by the startup retry path")
	}
	if !app.remoteMCPDueForObservation(market, true) {
		t.Fatal("missing managed state is due when the health loop may retry")
	}
	if err := app.markMCPRuntimeSyncReady(market.ID); err != nil {
		t.Fatal(err)
	}
	if !app.remoteMCPDueForObservation(market, false) {
		t.Fatal("durable ready marker must be re-observed in this process")
	}
	if err := app.resetMCPRuntimeSyncPending(market.ID, "http"); err != nil {
		t.Fatal(err)
	}
	if app.remoteMCPDueForObservation(market, false) || !app.remoteMCPDueForObservation(market, true) {
		t.Fatal("pending due retry must stay on the single startup reconciler until the health loop")
	}
	if err := app.recordMCPRuntimeSyncFailure(market.ID, "http", "", context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	if app.remoteMCPDueForObservation(market, true) {
		t.Fatal("pending backoff was treated as due")
	}
	for i := 1; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure(market.ID, "http", "", context.DeadlineExceeded); err != nil {
			t.Fatal(err)
		}
	}
	if app.remoteMCPDueForObservation(market, true) || app.remoteMCPDueForObservation(market, false) {
		t.Fatal("needs_review was scheduled for another probe")
	}
}

func TestReconcileRemoteMCPObservationsProbesManualServer(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	var lists atomic.Int32
	server := mcpToolsListServer(t, &lists)
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	entry := corelib.MCPServerEntry{ID: "manual-observe", Name: "manual", EndpointURL: server.URL, Source: corelib.MCPSourceManual}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.reconcileRemoteMCPObservations(ctx, false)
	waitMCPObserved(t, app.mcpRegistry, entry.ID)
	if lists.Load() != 1 {
		t.Fatalf("tools/list calls=%d, want 1", lists.Load())
	}
	inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	coverage := inventory.coverage.ForProviderKind("mcp")
	if coverage.State != tool.CatalogCoverageComplete || coverage.ReasonCode != "" {
		t.Fatalf("coverage=%+v", coverage)
	}
	if len(inventory.mcpEntries) != 1 || inventory.mcpEntries[0].ToolName != "work_log_query" || inventory.mcpEntries[0].RuntimeBlocked {
		t.Fatalf("entries=%#v", inventory.mcpEntries)
	}
}

func TestReconcileRemoteMCPObservationsRebuildsMarketReady(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	var lists atomic.Int32
	server := mcpToolsListServer(t, &lists)
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	entry := corelib.MCPServerEntry{
		ID: "market-ready", Name: "market", EndpointURL: server.URL,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.markMCPRuntimeSyncReady(entry.ID); err != nil {
		t.Fatal(err)
	}
	before, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	if coverage := before.coverage.ForProviderKind("mcp"); coverage.State != tool.CatalogCoverageIncomplete || coverage.ReasonCode != tool.CatalogCoverageReasonNotReady || len(before.mcpEntries) != 0 {
		t.Fatalf("ready marker was treated as a tool list: coverage=%+v entries=%#v", coverage, before.mcpEntries)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.reconcileRemoteMCPObservations(ctx, false)
	waitMCPObserved(t, app.mcpRegistry, entry.ID)
	if lists.Load() != 1 {
		t.Fatalf("tools/list calls=%d, want 1", lists.Load())
	}
	state, ok := loadMCPRuntimeSyncState(entry.ID)
	if !ok || state.Status != "ready" || state.Attempts != 0 {
		t.Fatalf("ready state after rebuild = %+v ok=%v", state, ok)
	}
	after, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	if coverage := after.coverage.ForProviderKind("mcp"); coverage.State != tool.CatalogCoverageComplete || len(after.mcpEntries) != 1 || after.mcpEntries[0].RuntimeBlocked {
		t.Fatalf("rebuilt inventory coverage=%+v entries=%#v", coverage, after.mcpEntries)
	}
}

func TestReconcileRemoteMCPObservationsSkipsNeedsReview(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	var lists atomic.Int32
	server := mcpToolsListServer(t, &lists)
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	entry := corelib.MCPServerEntry{
		ID: "market-review", Name: "market", EndpointURL: server.URL,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	for i := 0; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure(entry.ID, "http", "", context.DeadlineExceeded); err != nil {
			t.Fatal(err)
		}
	}
	before, ok := loadMCPRuntimeSyncState(entry.ID)
	if !ok || before.Status != "needs_review" || before.Attempts != mcpRuntimeSyncMaxAttempts {
		t.Fatalf("state=%+v ok=%v", before, ok)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.reconcileRemoteMCPObservations(ctx, false)
	app.reconcileRemoteMCPObservations(ctx, true)
	time.Sleep(400 * time.Millisecond)
	if lists.Load() != 0 {
		t.Fatalf("needs_review was probed %d times", lists.Load())
	}
	after, ok := loadMCPRuntimeSyncState(entry.ID)
	if !ok || after != before {
		t.Fatalf("needs_review changed: before=%+v after=%+v", before, after)
	}
	inventory, err := (&IMMessageHandler{app: app}).semanticDynamicInventory(context.Background(), "desktop-user")
	if err != nil {
		t.Fatal(err)
	}
	coverage := inventory.coverage.ForProviderKind("mcp")
	if coverage.State != tool.CatalogCoverageComplete || len(inventory.mcpEntries) != 0 {
		t.Fatalf("needs_review kept the family open: coverage=%+v entries=%#v", coverage, inventory.mcpEntries)
	}
}

func TestReconcileRemoteMCPObservationsLeavesPendingToStartupReconcile(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	var lists atomic.Int32
	server := mcpToolsListServer(t, &lists)
	app := &App{testHomeDir: base}
	entry := corelib.MCPServerEntry{
		ID: "market-pending", Name: "market", EndpointURL: server.URL,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.resetMCPRuntimeSyncPending(entry.ID, "http"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.reconcileRemoteMCPObservations(ctx, false)
	time.Sleep(400 * time.Millisecond)
	if lists.Load() != 0 {
		t.Fatalf("gap pass probed a pending server %d times", lists.Load())
	}
	state, ok := loadMCPRuntimeSyncState(entry.ID)
	if !ok || state.Status != "pending" || state.Attempts != 0 {
		t.Fatalf("pending state=%+v ok=%v", state, ok)
	}
}

func TestReconcileMCPRuntimeOnStartupProbesPendingMarketOnce(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	var lists atomic.Int32
	server := mcpToolsListServer(t, &lists)
	app := &App{testHomeDir: base}
	defer app.closeSemanticInvocationStore()
	entry := corelib.MCPServerEntry{
		ID: "market-once", Name: "market", EndpointURL: server.URL,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.resetMCPRuntimeSyncPending(entry.ID, "http"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.reconcileMCPRuntimeOnStartup(ctx)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := loadMCPRuntimeSyncState(entry.ID); ok && state.Status == "ready" {
			// A second in-flight probe would still be on the local server.
			time.Sleep(300 * time.Millisecond)
			if lists.Load() != 1 {
				t.Fatalf("tools/list calls=%d, want exactly one startup probe", lists.Load())
			}
			state, ok = loadMCPRuntimeSyncState(entry.ID)
			if !ok || state.Status != "ready" || state.Attempts != 0 {
				t.Fatalf("startup probe disturbed ready state: %+v ok=%v", state, ok)
			}
			waitMCPObserved(t, app.mcpRegistry, entry.ID)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, _ := loadMCPRuntimeSyncState(entry.ID)
	t.Fatalf("startup did not publish one ready observation: state=%+v lists=%d", state, lists.Load())
}

func TestProbeRemoteMCPCancelDoesNotConsumeDurableBudget(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	entry := corelib.MCPServerEntry{
		ID: "market-cancel", Name: "market", EndpointURL: "http://127.0.0.1:1/mcp",
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.markMCPRuntimeSyncReady(entry.ID); err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	app.probeRemoteMCP(parent, entry)
	state, ok := loadMCPRuntimeSyncState(entry.ID)
	if !ok || state.Status != "ready" || state.Attempts != 0 {
		t.Fatalf("canceled probe consumed the durable budget: %+v ok=%v", state, ok)
	}
	if _, exists := app.mcpRegistry.health[entry.ID]; exists {
		t.Fatalf("canceled probe recorded in-memory health: %+v", app.mcpRegistry.health[entry.ID])
	}
}
