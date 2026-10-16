package tool

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// isolatePermissionDualEvalStats redirects the durable stats file into
// t.TempDir() and resets the process-global holder, so the test neither
// reads nor deletes the real
// maclawpath.DataDir()/stats/permission_dual_eval.json.
func isolatePermissionDualEvalStats(t *testing.T) {
	t.Helper()
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() {
		_ = ResetPermissionDualEvalStats()
		maclawpath.SetBaseDir(oldBase)
	})
	if err := ResetPermissionDualEvalStats(); err != nil {
		t.Fatalf("reset: %v", err)
	}
}

func TestPermissionDualEvalStatsRecordGetReset(t *testing.T) {
	isolatePermissionDualEvalStats(t)

	if line := FormatPermissionDualEvalLine(); line != "" {
		t.Fatalf("line must be empty when zero, got %q", line)
	}

	RecordPermissionDualEvalMismatch("expert_whitelist", "bash", "allow", "deny")
	RecordPermissionDualEvalMismatch("expert_whitelist", "ssh", "allow", "ask")
	RecordPermissionDualEvalMismatch("credential_fence", "write_file", "ask", "allow")

	st := GetPermissionDualEvalStats()
	if st.Total != 3 {
		t.Fatalf("total=%d", st.Total)
	}
	if st.Gates["expert_whitelist"] != 2 || st.Gates["credential_fence"] != 1 {
		t.Fatalf("gates=%+v", st.Gates)
	}
	if st.LastGate != "credential_fence" || st.LastTool != "write_file" ||
		st.LastLegacy != "ask" || st.LastNew != "allow" || st.LastAt == 0 {
		t.Fatalf("last=%+v", st)
	}

	line := FormatPermissionDualEvalLine()
	if !strings.Contains(line, "mismatches=3") ||
		!strings.Contains(line, "expert_whitelist=2") ||
		!strings.Contains(line, "credential_fence=1") ||
		!strings.Contains(line, "last=credential_fence/write_file ask->allow") {
		t.Fatalf("line=%q", line)
	}

	if err := ResetPermissionDualEvalStats(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	st = GetPermissionDualEvalStats()
	if st.Total != 0 || len(st.Gates) != 0 || st.LastGate != "" || st.LastAt != 0 {
		t.Fatalf("after reset: %+v", st)
	}
	if FormatPermissionDualEvalLine() != "" {
		t.Fatalf("line after reset: %q", FormatPermissionDualEvalLine())
	}
}

func TestPermissionDualEvalStatsGateOverflow(t *testing.T) {
	isolatePermissionDualEvalStats(t)

	// Fill the map to the bound with distinct gates.
	for i := 0; i < permissionDualEvalMaxGates; i++ {
		RecordPermissionDualEvalMismatch("gate_"+string(rune('a'+i)), "tool", "allow", "deny")
	}
	// Two more distinct gates must fold into "(other)".
	RecordPermissionDualEvalMismatch("overflow_x", "tool", "allow", "deny")
	RecordPermissionDualEvalMismatch("overflow_y", "tool", "deny", "allow")

	st := GetPermissionDualEvalStats()
	if st.Total != int64(permissionDualEvalMaxGates)+2 {
		t.Fatalf("total=%d", st.Total)
	}
	if len(st.Gates) != permissionDualEvalMaxGates+1 {
		t.Fatalf("gate keys=%d, want %d (16 named + overflow): %+v", len(st.Gates), permissionDualEvalMaxGates+1, st.Gates)
	}
	if st.Gates[permissionDualEvalOtherGate] != 2 {
		t.Fatalf("overflow bucket=%d, want 2: %+v", st.Gates[permissionDualEvalOtherGate], st.Gates)
	}
}

func TestPermissionDualEvalStatsPersistRestore(t *testing.T) {
	isolatePermissionDualEvalStats(t)

	RecordPermissionDualEvalMismatch("planner_surface", "edit_file", "allow", "deny")
	if err := FlushPermissionDualEvalStats(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	data, err := os.ReadFile(PermissionDualEvalStatsPath())
	if err != nil {
		t.Fatalf("durable file missing: %v", err)
	}
	var snap permissionDualEvalStatsDiskSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if snap.Total != 1 || snap.Gates["planner_surface"] != 1 || snap.LastTool != "edit_file" {
		t.Fatalf("snapshot=%+v", snap)
	}

	// Simulate process restart: reset the holder (keeps the file) and
	// record through a fresh load path.
	processPermissionDualEvalStats.total.Store(0)
	processPermissionDualEvalStats.mu.Lock()
	processPermissionDualEvalStats.loaded = false
	processPermissionDualEvalStats.gates = make(map[string]int64)
	processPermissionDualEvalStats.mu.Unlock()

	RecordPermissionDualEvalMismatch("acp_permission", "bash", "allow", "ask")
	st := GetPermissionDualEvalStats()
	if st.Total != 2 {
		t.Fatalf("total after restore=%d, want 2", st.Total)
	}
	if st.Gates["planner_surface"] != 1 || st.Gates["acp_permission"] != 1 {
		t.Fatalf("gates after restore=%+v", st.Gates)
	}
	if st.LastGate != "acp_permission" {
		t.Fatalf("last gate after restore=%q", st.LastGate)
	}
}

// TestPermissionDualEvalStatsTestIsolation verifies the durable file landed
// under the redirected base dir and the real data dir was untouched.
func TestPermissionDualEvalStatsTestIsolation(t *testing.T) {
	isolatePermissionDualEvalStats(t)

	RecordPermissionDualEvalMismatch("gate", "tool", "allow", "deny")
	if err := FlushPermissionDualEvalStats(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if !strings.HasPrefix(PermissionDualEvalStatsPath(), maclawpath.BaseDir()) {
		t.Fatalf("stats path %q not under redirected base %q", PermissionDualEvalStatsPath(), maclawpath.BaseDir())
	}
}
