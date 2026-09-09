package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestValidateMCPJSONRPCSuccess(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{"result", `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`, false},
		{"rpc error", `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"denied"}}`, true},
		{"missing result", `{"jsonrpc":"2.0","id":1}`, true},
		{"invalid json", `{`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMCPJSONRPCSuccess([]byte(tt.payload), "tools/list")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateMCPJSONRPCSuccess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMCPToolsListResult(t *testing.T) {
	if err := validateMCPToolsListResult([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)); err != nil {
		t.Fatalf("empty tools array should be valid: %v", err)
	}
	if err := validateMCPToolsListResult([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)); err == nil {
		t.Fatal("missing tools array should be rejected")
	}
	if err := validateMCPToolsListResult([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":{}}}`)); err == nil {
		t.Fatal("non-array tools should be rejected")
	}
}

func TestHealthCheckStrictRejectsJSONRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "initialize" {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "denied"}})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	registry := NewMCPRegistry(app)
	if _, err := registry.register(corelib.MCPServerEntry{ID: "strict-error", Name: "Strict error", EndpointURL: server.URL}, false); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.HealthCheckStrictContext(context.Background(), "strict-error"); err == nil || !strings.Contains(err.Error(), "JSON-RPC error") {
		t.Fatalf("strict health check error = %v, want JSON-RPC protocol failure", err)
	}
}

func TestCheckMCPServerHealthManagedUsesStrictProbeAndPersistsFailure(t *testing.T) {
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
		if req.Method == "initialize" {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "denied"}})
	}))
	defer server.Close()

	app := &App{testHomeDir: base}
	if err := app.SaveConfig(corelib.AppConfig{MCPServers: []corelib.MCPServerEntry{{
		ID: "managed-check", Name: "Managed check", EndpointURL: server.URL,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.CheckMCPServerHealth("managed-check"); err == nil || !strings.Contains(err.Error(), "JSON-RPC error") {
		t.Fatalf("managed CheckMCPServerHealth error = %v, want strict protocol failure", err)
	}
	state, ok := loadMCPRuntimeSyncState("managed-check")
	if !ok || state.Status != "pending" || state.Attempts != 1 {
		t.Fatalf("managed health failure state = %+v, ok=%v", state, ok)
	}
}

func TestRetryMCPRuntimeSyncRequiresConfirmation(t *testing.T) {
	if err := (&App{}).RetryMCPRuntimeSync("managed", false); err == nil || !strings.Contains(err.Error(), "explicit confirmation") {
		t.Fatalf("RetryMCPRuntimeSync without confirmation = %v, want explicit confirmation error", err)
	}
}

func TestRetryMCPRuntimeSyncManagedRemoteRearmsAndProbes(t *testing.T) {
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
		ID: "managed-retry", Name: "Managed retry", EndpointURL: server.URL,
		Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	for i := 0; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure("managed-retry", "http", "old", context.DeadlineExceeded); err != nil {
			t.Fatal(err)
		}
	}
	before, ok := loadMCPRuntimeSyncState("managed-retry")
	if !ok || before.Status != "needs_review" {
		t.Fatalf("precondition state = %+v, ok=%v", before, ok)
	}
	if err := app.RetryMCPRuntimeSync("managed-retry", true); err != nil {
		t.Fatalf("RetryMCPRuntimeSync() error = %v", err)
	}
	after, ok := loadMCPRuntimeSyncState("managed-retry")
	if !ok || after.Status != "ready" || after.Attempts != 0 {
		t.Fatalf("retry state = %+v, ok=%v; want ready with reset attempts", after, ok)
	}
}

func TestRetryMCPRuntimeSyncManagedLocalRearmsCheckedSync(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", base)
	t.Setenv("USERPROFILE", base)
	t.Setenv("AppData", filepath.Join(base, "AppData", "Roaming"))
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	entry := corelib.LocalMCPServerEntry{
		ID: "managed-local-retry", Name: "Managed local retry", Command: os.Args[0],
		Args: []string{"-test.run=TestLocalMCPHelperProcess", "--", "helper-mcp"},
		Env:  map[string]string{"GO_WANT_HELPER_PROCESS": "1"}, Source: corelib.MCPSourceMarket,
		Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap-local-retry"},
	}
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry = NewMCPRegistry(app)
	for i := 0; i < mcpRuntimeSyncMaxAttempts; i++ {
		if err := app.recordMCPRuntimeSyncFailure(entry.ID, "stdio", "old", context.DeadlineExceeded); err != nil {
			t.Fatal(err)
		}
	}
	app.ensureLocalMCPManager()
	t.Cleanup(func() {
		if app.localMCPManager != nil {
			app.localMCPManager.StopAll()
		}
	})
	if err := app.RetryMCPRuntimeSync(entry.ID, true); err != nil {
		t.Fatalf("RetryMCPRuntimeSync(local) error = %v", err)
	}
	state, ok := loadMCPRuntimeSyncState(entry.ID)
	if !ok || state.Status != "ready" || !app.localMCPManager.IsRunning(entry.ID) {
		t.Fatalf("local retry state=%+v ok=%v running=%v; want ready/running", state, ok, app.localMCPManager.IsRunning(entry.ID))
	}
}

func TestProbeAllUnknownAsyncManagedUsesStrictRuntimeGate(t *testing.T) {
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
		if req.Method == "initialize" {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "denied"}})
	}))
	defer server.Close()

	app := &App{testHomeDir: base}
	registry := NewMCPRegistry(app)
	if _, err := registry.register(corelib.MCPServerEntry{ID: "managed-probe", Name: "Managed probe", EndpointURL: server.URL, Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"}}, false); err != nil {
		t.Fatal(err)
	}
	registry.ProbeAllUnknownAsync()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := loadMCPRuntimeSyncState("managed-probe"); ok && state.Attempts > 0 {
			if state.Status != "pending" {
				t.Fatalf("managed probe state = %+v, want pending after first failure", state)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("managed unknown probe did not persist runtime failure")
}

func TestHealthCheckStrictContextHonorsIndependentDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`))
	}))
	defer server.Close()
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	registry := NewMCPRegistry(app)
	if _, err := registry.register(corelib.MCPServerEntry{ID: "deadline", Name: "Deadline", EndpointURL: server.URL}, false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := registry.HealthCheckStrictContext(ctx, "deadline")
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("strict health check error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("strict health check ignored deadline: elapsed=%s", elapsed)
	}
}

func TestRegisterMCPServerImmediatelySyncsTools(t *testing.T) {
	var toolsListCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "tools/list":
			toolsListCount.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "search",
						"description": "Search enterprise content",
						"inputSchema": map[string]any{"type": "object"},
					}},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}})
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)

	if err := app.RegisterMCPServer(corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}
	if toolsListCount.Load() == 0 {
		t.Fatal("RegisterMCPServer should synchronously fetch tools/list")
	}
	tools := app.mcpRegistry.GetServerTools("remote")
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Fatalf("cached tools = %#v, want search", tools)
	}

	handler := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	out := handler.toolDiscoverTool(map[string]interface{}{"need": "search enterprise content"})
	if !strings.Contains(out, "managed replan") || !strings.Contains(out, "remote/search") || strings.Contains(out, "call call_mcp_tool") {
		t.Fatalf("discover_tool should report MCP as managed-only, got %s", out)
	}

	handlerWithoutToolRegistry := &IMMessageHandler{app: app}
	out = handlerWithoutToolRegistry.toolDiscoverTool(map[string]interface{}{"need": "query enterprise content"})
	if !strings.Contains(out, "managed replan") || !strings.Contains(out, "remote/search") || strings.Contains(out, "call call_mcp_tool") {
		t.Fatalf("discover_tool should report MCP-only inventory as managed-only, got %s", out)
	}
}

func TestGetServerToolsHidesManagedStaleCacheWhileRuntimeBlocked(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	app.mcpRegistry = NewMCPRegistry(app)
	entry := corelib.MCPServerEntry{
		ID: "managed-stale-cache", Name: "Managed stale cache", EndpointURL: "http://127.0.0.1:1",
		Source:     corelib.MCPSourceMarket,
		Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"},
	}
	if _, err := app.mcpRegistry.register(entry, false); err != nil {
		t.Fatal(err)
	}
	app.mcpRegistry.mu.Lock()
	app.mcpRegistry.toolsCache[entry.ID] = []MCPToolView{{Name: "stale"}}
	app.mcpRegistry.mu.Unlock()
	if got := app.mcpRegistry.GetServerTools(entry.ID); len(got) != 0 {
		t.Fatalf("managed stale cache leaked while runtime is blocked: %#v", got)
	}
	for _, view := range app.mcpRegistry.ListServers() {
		if view.ID == entry.ID && len(view.Tools) != 0 {
			t.Fatalf("managed stale tools leaked through ListServers: %#v", view.Tools)
		}
	}
	if err := app.markMCPRuntimeSyncReady(entry.ID); err != nil {
		t.Fatal(err)
	}
	if got := app.mcpRegistry.GetServerTools(entry.ID); len(got) != 1 || got[0].Name != "stale" {
		t.Fatalf("ready managed cache should remain inspectable: %#v", got)
	}
}

func TestUpdateLocalMCPManagedToManualClearsRuntimeBlocker(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	app.mcpRegistry = NewMCPRegistry(app)
	managed := corelib.LocalMCPServerEntry{ID: "local-transition", Name: "Managed", Command: "node", Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"}}
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{managed}}); err != nil {
		t.Fatal(err)
	}
	if err := app.resetMCPRuntimeSyncPending(managed.ID, "stdio"); err != nil {
		t.Fatal(err)
	}
	// Also cover the process-local persistence-failure fence: converting the
	// entry to manual must clear it even when the durable state file is already
	// absent, otherwise a reused server ID would remain blocked forever.
	fenceKey := mcpRuntimeSyncPersistenceKey(managed.ID)
	mcpRuntimeSyncStateMu.Lock()
	mcpRuntimeSyncPersistenceFailures[fenceKey] = true
	mcpRuntimeSyncStateMu.Unlock()
	t.Cleanup(func() {
		mcpRuntimeSyncStateMu.Lock()
		delete(mcpRuntimeSyncPersistenceFailures, fenceKey)
		mcpRuntimeSyncStateMu.Unlock()
	})
	manual := managed
	manual.Source = corelib.MCPSourceManual
	manual.Capability = nil
	if err := app.mcpRegistry.UpdateLocal(manual); err != nil {
		t.Fatalf("UpdateLocal: %v", err)
	}
	if _, ok := loadMCPRuntimeSyncState(managed.ID); ok {
		t.Fatal("managed runtime blocker must be cleared when converting to manual MCP")
	}
	mcpRuntimeSyncStateMu.Lock()
	_, fenced := mcpRuntimeSyncPersistenceFailures[fenceKey]
	mcpRuntimeSyncStateMu.Unlock()
	if fenced {
		t.Fatal("managed-to-manual conversion left stale process-local runtime fence")
	}
}

func TestUpdateMCPManagedToManualClearsRuntimeBlocker(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	app.mcpRegistry = NewMCPRegistry(app)
	managed := corelib.MCPServerEntry{ID: "remote-transition", Name: "Managed", EndpointURL: "http://127.0.0.1:1", Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"}}
	if _, err := app.mcpRegistry.register(managed, false); err != nil {
		t.Fatal(err)
	}
	if err := app.resetMCPRuntimeSyncPending(managed.ID, "http"); err != nil {
		t.Fatal(err)
	}
	manual := managed
	manual.Source = corelib.MCPSourceManual
	manual.Capability = nil
	if err := app.mcpRegistry.Update(manual); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, ok := loadMCPRuntimeSyncState(managed.ID); ok {
		t.Fatal("managed runtime blocker must be cleared when converting to manual MCP")
	}
}

func TestUpdateMCPPartialEntryPreservesManagedIdentity(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	app.mcpRegistry = NewMCPRegistry(app)
	managed := corelib.MCPServerEntry{ID: "remote-partial", Name: "Managed", EndpointURL: "http://127.0.0.1:1", AuthType: "bearer", AuthSecret: "secret", Headers: map[string]string{"X-Tenant": "tenant"}, Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"}}
	if _, err := app.mcpRegistry.register(managed, false); err != nil {
		t.Fatal(err)
	}
	if err := app.mcpRegistry.Update(corelib.MCPServerEntry{ID: managed.ID, Name: "Renamed"}); err != nil {
		t.Fatalf("partial Update: %v", err)
	}
	got, err := app.mcpRegistry.findServer(managed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != corelib.MCPSourceMarket || got.Capability == nil || got.Capability.CapabilityID != "cap" {
		t.Fatalf("partial update lost managed identity: %+v", got)
	}
	if got.AuthType != managed.AuthType || got.AuthSecret != managed.AuthSecret || !reflect.DeepEqual(got.Headers, managed.Headers) {
		t.Fatalf("partial update lost authentication contract: %+v", got)
	}
}

func TestUnregisterLocalMCPClearsRuntimeState(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	app.mcpRegistry = NewMCPRegistry(app)
	entry := corelib.LocalMCPServerEntry{ID: "local-delete", Name: "Managed", Command: "node", Source: corelib.MCPSourceMarket, Capability: &corelib.MCPServerCapabilityRef{CapabilityID: "cap"}}
	if err := app.SaveConfig(corelib.AppConfig{LocalMCPServers: []corelib.LocalMCPServerEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	if err := app.resetMCPRuntimeSyncPending(entry.ID, "stdio"); err != nil {
		t.Fatal(err)
	}
	if err := app.mcpRegistry.UnregisterLocal(entry.ID); err != nil {
		t.Fatalf("UnregisterLocal: %v", err)
	}
	if _, ok := loadMCPRuntimeSyncState(entry.ID); ok {
		t.Fatal("runtime state must be removed with local MCP config")
	}
}

func TestMCPRegistryWarmServerToolsTimesOutWithoutBlocking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	registry := NewMCPRegistry(app)
	if _, err := registry.register(corelib.MCPServerEntry{ID: "slow", Name: "Slow", EndpointURL: server.URL}, false); err != nil {
		t.Fatalf("register: %v", err)
	}

	started := time.Now()
	done := make(chan error, 1)
	err := registry.warmServerTools("slow", 10*time.Millisecond, func(err error) {
		done <- err
	})
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("warmServerTools error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("warmServerTools blocked too long: %s", elapsed)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("background warmServerTools callback error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("background warmServerTools callback did not run")
	}
}

func TestDiscoverToolDoesNotFetchUncachedRemoteMCPTools(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	if _, err := app.mcpRegistry.register(corelib.MCPServerEntry{ID: "slow", Name: "Slow", EndpointURL: server.URL}, false); err != nil {
		t.Fatalf("register: %v", err)
	}

	started := time.Now()
	out := (&IMMessageHandler{app: app}).toolDiscoverTool(map[string]interface{}{"need": "query enterprise content"})
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("discover_tool blocked on uncached MCP: elapsed=%s output=%s", elapsed, out)
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("discover_tool should not fetch uncached remote MCP tools, requests=%d", got)
	}
}

func TestUpdateMCPServerHeaderChangeInvalidatesSessionAndTools(t *testing.T) {
	base := t.TempDir()
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	app := &App{testHomeDir: base}
	registry := NewMCPRegistry(app)
	entry := corelib.MCPServerEntry{
		ID: "header-rotation", Name: "Header rotation", EndpointURL: "http://127.0.0.1:1",
		Headers: map[string]string{"X-Tenant": "old"},
	}
	if _, err := registry.register(entry, false); err != nil {
		t.Fatalf("register: %v", err)
	}
	registry.setSession(entry.ID, "stale-session")
	registry.mu.Lock()
	registry.toolsCache[entry.ID] = []MCPToolView{{Name: "stale-tool"}}
	registry.mu.Unlock()
	updated := entry
	updated.Headers = map[string]string{"X-Tenant": "new"}
	if err := registry.Update(updated); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, ok := registry.getSession(entry.ID); ok {
		t.Fatal("header change must invalidate the old MCP session")
	}
	if got := registry.GetServerTools(entry.ID); len(got) != 0 {
		t.Fatalf("header change must invalidate cached tools, got %#v", got)
	}
}

func TestImportRemoteMCPServerInvalidatesToolCacheBeforeBackgroundSync(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	handler := &IMMessageHandler{cachedTools: []map[string]interface{}{{"name": "stale"}}, toolsCacheTime: time.Now()}
	app := &App{testHomeDir: t.TempDir(), imHandler: handler}
	app.mcpRegistry = NewMCPRegistry(app)

	started := time.Now()
	if err := app.importRemoteMCPServer(corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}); err != nil {
		t.Fatalf("importRemoteMCPServer: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("importRemoteMCPServer blocked on background sync: %s", elapsed)
	}
	if handler.cachedTools != nil || !handler.toolsCacheTime.IsZero() {
		t.Fatalf("tool cache should be invalidated immediately, cached=%#v time=%s", handler.cachedTools, handler.toolsCacheTime)
	}
	if got := requestCount.Load(); got != 0 {
		t.Fatalf("importRemoteMCPServer should defer background sync, requests=%d", got)
	}

	app.warmImportedRemoteMCPServers([]string{"remote"})
	deadline := time.After(time.Second)
	for requestCount.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("warmImportedRemoteMCPServers did not start background sync")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestMCPRegistrySessionsAreScopedByOwner(t *testing.T) {
	var counter atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Session-Id") == "" {
			w.Header().Set("Mcp-Session-Id", "session-"+string(rune('a'+counter.Add(1)-1)))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	registry := NewMCPRegistry(nil)
	target := &corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}
	if err := registry.ensureSession(target, "agent-a"); err != nil {
		t.Fatalf("ensureSession(agent-a): %v", err)
	}
	if err := registry.ensureSession(target, "agent-b"); err != nil {
		t.Fatalf("ensureSession(agent-b): %v", err)
	}

	a, okA := registry.getSessionForOwner("remote", "agent-a")
	b, okB := registry.getSessionForOwner("remote", "agent-b")
	if !okA || !okB || a.SessionID == "" || b.SessionID == "" || a.SessionID == b.SessionID {
		t.Fatalf("sessions not isolated: a=%#v ok=%v b=%#v ok=%v", a, okA, b, okB)
	}
}

func TestMCPRegistryEnsureSessionSingleflightPerOwner(t *testing.T) {
	var initCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Session-Id") == "" {
			initCount.Add(1)
			w.Header().Set("Mcp-Session-Id", "session-owner-a")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	registry := NewMCPRegistry(nil)
	target := &corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- registry.ensureSession(target, "agent-a")
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("ensureSession() error = %v", err)
		}
	}
	if got := initCount.Load(); got != 1 {
		t.Fatalf("initialize count = %d, want 1 for same owner", got)
	}
}

func TestParseMCPImportConfigAutoLocalAndRemote(t *testing.T) {
	cfg := `{
		"mcpServers": {
			"playwright": {
				"command": "npx",
				"args": ["-y", "@playwright/mcp"],
				"env": {"TOKEN": "secret"},
				"auto_start": true
			},
			"wiki": {
				"url": "https://mcp.example.com",
				"headers": {"Authorization": "Bearer abc", "X-Team": "docs"}
			}
		}
	}`
	local, remote, err := parseMCPImportConfig(cfg, mcpImportTargetAuto)
	if err != nil {
		t.Fatalf("parseMCPImportConfig: %v", err)
	}
	if len(local) != 1 || local[0].Name != "playwright" || local[0].Command != "npx" || !local[0].AutoStart {
		t.Fatalf("local entries = %#v", local)
	}
	if got := strings.Join(local[0].Args, " "); got != "-y @playwright/mcp" {
		t.Fatalf("local args = %q", got)
	}
	if local[0].Env["TOKEN"] != "secret" {
		t.Fatalf("local env = %#v", local[0].Env)
	}
	if len(remote) != 1 || remote[0].Name != "wiki" || remote[0].EndpointURL != "https://mcp.example.com" {
		t.Fatalf("remote entries = %#v", remote)
	}
	if remote[0].AuthType != "bearer" || remote[0].AuthSecret != "abc" {
		t.Fatalf("remote auth = %s/%q", remote[0].AuthType, remote[0].AuthSecret)
	}
	if _, ok := remote[0].Headers["Authorization"]; ok {
		t.Fatalf("authorization header should be extracted, headers=%#v", remote[0].Headers)
	}
	if remote[0].Headers["X-Team"] != "docs" {
		t.Fatalf("custom headers = %#v", remote[0].Headers)
	}
}

func TestParseMCPImportConfigAcceptsMissingOuterBraces(t *testing.T) {
	for _, cfg := range []string{
		`"mcpServers": {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		`mcpServers: {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		`mcp_servers: {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		`mcpservers: {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		`MCPServers: {"browser": {"command": "npx", "args": ["-y", "@playwright/mcp"]}}`,
		"```json\n\"mcpServers\": {\"browser\": {\"command\": \"npx\", \"args\": [\"-y\", \"@playwright/mcp\"]}}\n```",
	} {
		local, remote, err := parseMCPImportConfig(cfg, mcpImportTargetAuto)
		if err != nil {
			t.Fatalf("parseMCPImportConfig(%q): %v", cfg, err)
		}
		if len(remote) != 0 {
			t.Fatalf("remote entries = %#v", remote)
		}
		if len(local) != 1 || local[0].Name != "browser" || local[0].Command != "npx" {
			t.Fatalf("local entries = %#v", local)
		}
	}
}

func TestParseMCPImportConfigRejectsLocalMissingCommand(t *testing.T) {
	_, _, err := parseMCPImportConfig(`{"mcpServers":{"browser":{"args":["-y","@playwright/mcp"]}}}`, mcpImportTargetAuto)
	if err == nil || !strings.Contains(err.Error(), "missing command") {
		t.Fatalf("parseMCPImportConfig error = %v", err)
	}
}

func TestParseMCPImportConfigAcceptsMCPServersAliases(t *testing.T) {
	for _, cfg := range []string{
		`{"mcpservers":{"browser":{"command":"npx"}}}`,
		`{"MCPServers":{"browser":{"command":"npx"}}}`,
	} {
		local, remote, err := parseMCPImportConfig(cfg, mcpImportTargetAuto)
		if err != nil {
			t.Fatalf("parseMCPImportConfig(%q): %v", cfg, err)
		}
		if len(remote) != 0 {
			t.Fatalf("remote entries = %#v", remote)
		}
		if len(local) != 1 || local[0].Name != "browser" || local[0].Command != "npx" {
			t.Fatalf("local entries = %#v", local)
		}
	}
}

func TestToolImportMCPServersAcceptsObjectConfig(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	handler := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	out := handler.toolImportMCPServers(map[string]interface{}{
		"json_config": map[string]interface{}{
			"mcpServers": map[string]interface{}{
				"browser": map[string]interface{}{
					"command": "npx",
					"args":    []interface{}{"-y", "@playwright/mcp"},
				},
			},
		},
	})
	if !strings.Contains(out, "Imported 1 MCP server") || !strings.Contains(out, "browser") {
		t.Fatalf("toolImportMCPServers output = %s", out)
	}
	servers := app.mcpRegistry.ListLocalServers()
	if len(servers) != 1 || strings.Join(servers[0].Args, " ") != "-y @playwright/mcp" {
		t.Fatalf("local servers = %#v", servers)
	}
}

func TestToolImportMCPServersRegistersLocalConfig(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	handler := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	out := handler.toolImportMCPServers(map[string]interface{}{
		"json_config": `{"mcpServers":{"browser":{"command":"npx","args":["-y","@playwright/mcp"],"auto_start":true}}}`,
	})
	if !strings.Contains(out, "Imported 1 MCP server") || !strings.Contains(out, "browser") {
		t.Fatalf("toolImportMCPServers output = %s", out)
	}
	servers := app.mcpRegistry.ListLocalServers()
	if len(servers) != 1 || servers[0].Name != "browser" || servers[0].Command != "npx" || !servers[0].AutoStart {
		t.Fatalf("local servers = %#v", servers)
	}
}

func TestToolImportMCPServersRejectsDuplicateLocalID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.mcpRegistry.RegisterLocal(corelib.LocalMCPServerEntry{ID: "dup", Name: "Existing", Command: "node"}); err != nil {
		t.Fatalf("RegisterLocal: %v", err)
	}
	handler := &IMMessageHandler{app: app, registry: NewToolRegistry()}
	out := handler.toolImportMCPServers(map[string]interface{}{
		"json_config": `{"mcpServers":{"browser":{"id":"dup","command":"npx"}}}`,
	})
	if !strings.Contains(out, "already exists") {
		t.Fatalf("toolImportMCPServers output = %s", out)
	}
	servers := app.mcpRegistry.ListLocalServers()
	if len(servers) != 1 || servers[0].Name != "Existing" {
		t.Fatalf("local servers = %#v", servers)
	}
}

func TestImportMCPServersRejectsDuplicateRemoteIDBeforeRegister(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	if _, err := app.mcpRegistry.register(corelib.MCPServerEntry{ID: "dup-remote", Name: "Existing", EndpointURL: "https://existing.example.com"}, false); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, err := app.ImportMCPServersFromJSON(`{"mcpServers":{"wiki":{"id":"dup-remote","url":"https://mcp.example.com"}}}`, "remote")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("ImportMCPServersFromJSON error = %v", err)
	}
	servers := app.mcpRegistry.ListServers()
	if len(servers) != 1 || servers[0].Name != "Existing" {
		t.Fatalf("remote servers = %#v", servers)
	}
}

func TestImportMCPServersStartsRemoteToolSyncInBackground(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	started := time.Now()
	summary, err := app.ImportMCPServersFromJSON(`{"mcpServers":{"slow":{"url":"`+server.URL+`"}}}`, "remote")
	if err != nil {
		t.Fatalf("ImportMCPServersFromJSON: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
		t.Fatalf("remote import should not block on tool sync: %s", elapsed)
	}
	if len(summary.Remote) != 1 || summary.Remote[0] != "slow" {
		t.Fatalf("summary = %#v", summary)
	}
	servers := app.mcpRegistry.ListServers()
	if len(servers) != 1 || servers[0].Name != "slow" {
		t.Fatalf("remote servers = %#v", servers)
	}
}

func TestImportMCPServersPreflightPreventsPartialImport(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		HubSecurityCentralized: true,
		NetworkLevel:           "none",
		FileOutboundEnabled:    true,
		ImageOutboundEnabled:   true,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.mcpRegistry = NewMCPRegistry(app)

	_, err := app.ImportMCPServersFromJSON(`{"mcpServers":{
		"local-ok":{"command":"npx","args":["-y","@playwright/mcp"]},
		"remote-blocked":{"url":"https://mcp.example.com"}
	}}`, "auto")
	if err == nil || !strings.Contains(err.Error(), "security policy") {
		t.Fatalf("ImportMCPServersFromJSON error = %v", err)
	}
	if got := app.mcpRegistry.ListLocalServers(); len(got) != 0 {
		t.Fatalf("local servers should not be partially imported: %#v", got)
	}
	if got := app.mcpRegistry.ListServers(); len(got) != 0 {
		t.Fatalf("remote servers should not be partially imported: %#v", got)
	}
}

func TestRollbackMCPImportRemovesImportedServers(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.mcpRegistry = NewMCPRegistry(app)
	if err := app.mcpRegistry.RegisterLocal(corelib.LocalMCPServerEntry{ID: "local-imported", Name: "Local", Command: "npx"}); err != nil {
		t.Fatalf("RegisterLocal: %v", err)
	}
	if _, err := app.mcpRegistry.register(corelib.MCPServerEntry{ID: "remote-imported", Name: "Remote", EndpointURL: "https://mcp.example.com"}, false); err != nil {
		t.Fatalf("register remote: %v", err)
	}

	app.rollbackMCPImport([]string{"local-imported"}, []string{"remote-imported"})
	if got := app.mcpRegistry.ListLocalServers(); len(got) != 0 {
		t.Fatalf("local servers after rollback = %#v", got)
	}
	if got := app.mcpRegistry.ListServers(); len(got) != 0 {
		t.Fatalf("remote servers after rollback = %#v", got)
	}
}

func TestPrepareLocalMCPImportIDsAssignsUniqueIDs(t *testing.T) {
	entries := []corelib.LocalMCPServerEntry{
		{Name: "One", Command: "npx"},
		{Name: "Two", Command: "node"},
	}
	prepareLocalMCPImportIDs(entries)
	if entries[0].ID == "" || entries[1].ID == "" || entries[0].ID == entries[1].ID {
		t.Fatalf("prepared IDs = %q, %q", entries[0].ID, entries[1].ID)
	}
	if !strings.HasPrefix(entries[0].ID, "local-") || !strings.HasPrefix(entries[1].ID, "local-") {
		t.Fatalf("prepared IDs should use local prefix, got %q, %q", entries[0].ID, entries[1].ID)
	}
}

func TestPrepareRemoteMCPImportIDsAssignsStableUniqueIDs(t *testing.T) {
	entries := []corelib.MCPServerEntry{
		{Name: "Wiki", EndpointURL: "https://one.example.com"},
		{Name: "Wiki", EndpointURL: "https://two.example.com"},
	}
	prepareRemoteMCPImportIDs(entries)
	if entries[0].ID == "" || entries[1].ID == "" || entries[0].ID == entries[1].ID {
		t.Fatalf("prepared IDs = %q, %q", entries[0].ID, entries[1].ID)
	}
	if !strings.HasPrefix(entries[0].ID, "wiki-") || !strings.HasPrefix(entries[1].ID, "wiki-") {
		t.Fatalf("prepared IDs should use sanitized names, got %q, %q", entries[0].ID, entries[1].ID)
	}
}

func TestParseMCPImportConfigRejectsEmptyServerName(t *testing.T) {
	_, _, err := parseMCPImportConfig(`{"mcpServers":{"":{"command":"npx"}}}`, mcpImportTargetAuto)
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("parseMCPImportConfig error = %v", err)
	}
}

func TestRegisterBuiltinToolsIncludesImportMCPServers(t *testing.T) {
	registry := NewToolRegistry()
	registerBuiltinTools(registry, &IMMessageHandler{})
	tool, ok := registry.Get("import_mcp_servers")
	if !ok {
		t.Fatal("import_mcp_servers should be registered")
	}
	if !containsStringTest(tool.Required, "json_config") {
		t.Fatalf("required = %#v", tool.Required)
	}
}

func containsStringTest(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
