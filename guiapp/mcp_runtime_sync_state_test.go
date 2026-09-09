package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
	app.reconcileManagedMCPRuntimeSyncOnStartup()
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
	app.reconcileManagedMCPRuntimeSyncOnStartup()
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
