package guiapp

import (
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// captureScopeApprovalDualEvalHook swaps the mismatch hook and returns a
// collector (restored via t.Cleanup).
func captureScopeApprovalDualEvalHook(t *testing.T) *[][]interface{} {
	t.Helper()
	var captured [][]interface{}
	prev := scopeApprovalDualEvalMismatchHook
	scopeApprovalDualEvalMismatchHook = func(tool string, legacyEffect, newEffect permission.Effect, rule *permission.Rule) {
		captured = append(captured, []interface{}{tool, legacyEffect, newEffect, rule})
	}
	t.Cleanup(func() { scopeApprovalDualEvalMismatchHook = prev })
	return &captured
}

func TestScopeApprovalDualEval(t *testing.T) {
	fileArgs := map[string]interface{}{"path": "/etc/ssl/key.pem", "project_path": "/workspace/proj"}
	bashArgs := map[string]interface{}{"command": "rm -rf /data", "working_dir": "/workspace/proj", "project_path": "/workspace/proj"}
	cases := []struct {
		name    string
		snap    func(t *testing.T) *permission.Snapshot
		tool    string
		args    map[string]interface{}
		legacy  permission.Effect
		wantLog bool
		wantNew permission.Effect
	}{
		{
			name: "nil snapshot is silent",
			snap: func(t *testing.T) *permission.Snapshot { return nil },
			tool: "read_file", args: fileArgs, legacy: permission.EffectAsk,
		},
		{
			name: "no rules: default is silent",
			snap: func(t *testing.T) *permission.Snapshot { return mustPermissionSnapshot(t, nil) },
			tool: "read_file", args: fileArgs, legacy: permission.EffectAsk,
		},
		{
			name: "legacy ask + Prefix deny on path logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "read_file", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "path", Prefix: "/etc/"}, Reason: "protected root"},
				})
			},
			tool: "read_file", args: fileArgs, legacy: permission.EffectAsk,
			wantLog: true, wantNew: permission.EffectDeny,
		},
		{
			name: "legacy ask + allow rule on bash logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "bash", Effect: permission.EffectAllow},
				})
			},
			tool: "bash", args: bashArgs, legacy: permission.EffectAsk,
			wantLog: true, wantNew: permission.EffectAllow,
		},
		{
			name: "legacy ask + ask rule: agreement is silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "read_file", Effect: permission.EffectAsk},
				})
			},
			tool: "read_file", args: fileArgs, legacy: permission.EffectAsk,
		},
		{
			name: "legacy allow + Prefix deny on path logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "read_file", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "path", Prefix: "/etc/"}},
				})
			},
			tool: "read_file", args: fileArgs, legacy: permission.EffectAllow,
			wantLog: true, wantNew: permission.EffectDeny,
		},
		{
			name: "prompt-point ask maps through unchanged",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "read_file", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "path", Prefix: "/etc/"}},
				})
			},
			tool: "read_file", args: fileArgs, legacy: permission.EffectAsk,
			wantLog: true, wantNew: permission.EffectDeny,
		},
		{
			name: "nil args: Prefix rule cannot match, silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "read_file", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "path", Prefix: "/etc/"}},
				})
			},
			tool: "read_file", args: nil, legacy: permission.EffectAsk,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captured := captureScopeApprovalDualEvalHook(t)
			scopeApprovalDualEval(tc.snap(t), tc.tool, tc.args, tc.legacy)
			if !tc.wantLog {
				if len(*captured) != 0 {
					t.Fatalf("want no mismatch log, got %v", *captured)
				}
				return
			}
			if len(*captured) != 1 {
				t.Fatalf("want 1 mismatch log, got %d", len(*captured))
			}
			m := (*captured)[0]
			if m[0] != tc.tool || m[1] != tc.legacy || m[2] != tc.wantNew {
				t.Fatalf("tool/legacy/new = %v/%v/%v, want %v/%v/%v", m[0], m[1], m[2], tc.tool, tc.legacy, tc.wantNew)
			}
			if rule, ok := m[3].(*permission.Rule); !ok || rule == nil {
				t.Fatalf("rule must be the concrete matched rule, got %v", m[3])
			}
		})
	}
}

// Integration: the real check() flow maps its decision points correctly and
// the legacy outcome is untouched by dual-eval.
func TestScopeApprovalCheckDualEvalIntegration(t *testing.T) {
	// User-prompt point with user deny: the snapshot allows the tool, so the
	// deny branch (user deny after the prompt) diverges as legacy=deny vs
	// new=allow; user deny still wins (legacy).
	snap := mustPermissionSnapshot(t, []permission.Rule{
		{Tool: "read_file", Effect: permission.EffectAllow, Reason: "operator allow"},
	})
	captured := captureScopeApprovalDualEvalHook(t)
	state := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		return ScopeApprovalDeny
	}, false)
	state.setDualEvalSnapshot(func() *permission.Snapshot { return snap })
	rejection := state.check("read_file", "/etc/ssl/key.pem", "/workspace/proj")
	if rejection == "" {
		t.Fatal("user deny must still reject (legacy behavior unchanged)")
	}
	if len(*captured) != 1 {
		t.Fatalf("want 1 mismatch log, got %d", len(*captured))
	}
	m := (*captured)[0]
	if m[1] != permission.EffectDeny || m[2] != permission.EffectAllow {
		t.Fatalf("legacy/new = %v/%v, want deny/allow (user deny maps to deny)", m[1], m[2])
	}

	// User deny against a snapshot deny: agreement, silent.
	*captured = nil
	denySnap := mustPermissionSnapshot(t, []permission.Rule{
		{Tool: "read_file", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "path", Prefix: "/etc/"}, Reason: "protected root"},
	})
	state.setDualEvalSnapshot(func() *permission.Snapshot { return denySnap })
	if rejection := state.check("read_file", "/etc/ssl/key.pem", "/workspace/proj"); rejection == "" {
		t.Fatal("user deny must still reject (legacy behavior unchanged)")
	}
	if len(*captured) != 0 {
		t.Fatalf("agreeing denies must not log, got %v", *captured)
	}

	// Full-access pass → legacy allow; the protected-root deny diverges.
	*captured = nil
	full := newScopeApprovalState(nil, true)
	full.setDualEvalSnapshot(func() *permission.Snapshot { return denySnap })
	if got := full.check("read_file", "/etc/ssl/key.pem", "/workspace/proj"); got != "" {
		t.Fatalf("full access must pass (legacy behavior unchanged), got %q", got)
	}
	if len(*captured) != 1 {
		t.Fatalf("want 1 mismatch log for full-access pass, got %d", len(*captured))
	}
	if m := (*captured)[0]; m[1] != permission.EffectAllow || m[2] != permission.EffectDeny {
		t.Fatalf("legacy/new = %v/%v, want allow/deny", m[1], m[2])
	}

	// Hard reject without callback → legacy deny; agreement with the deny
	// rule → silent.
	*captured = nil
	blocked := newScopeApprovalState(nil, false)
	blocked.setDualEvalSnapshot(func() *permission.Snapshot { return denySnap })
	if got := blocked.check("read_file", "/etc/ssl/key.pem", "/workspace/proj"); got == "" || !strings.Contains(got, "/etc/ssl/key.pem") {
		t.Fatalf("nil callback must hard-reject (legacy behavior unchanged), got %q", got)
	}
	if len(*captured) != 0 {
		t.Fatalf("agreeing deny must not log, got %v", *captured)
	}

	// No resolver installed (unit-test/TUI shape): silent, behavior unchanged.
	*captured = nil
	plain := newScopeApprovalState(func(ScopeApprovalRequest) ScopeApprovalDecision { return ScopeApprovalAllowOnce }, false)
	if got := plain.check("read_file", "/etc/ssl/key.pem", "/workspace/proj"); got != "" {
		t.Fatalf("allow_once must pass (legacy behavior unchanged), got %q", got)
	}
	if len(*captured) != 0 {
		t.Fatalf("nil resolver must skip dual-eval, got %v", *captured)
	}
}

// TestScopeApprovalDualEvalSnapshotInstallRace exercises the resolver
// install/read path from many goroutines; run with -race to prove the
// s.mu guard on dualEvalSnapshot is sufficient (install via
// setDualEvalSnapshot races with in-flight check() dual-eval reads).
func TestScopeApprovalDualEvalSnapshotInstallRace(t *testing.T) {
	snap := mustPermissionSnapshot(t, []permission.Rule{
		{Tool: "read_file", Effect: permission.EffectAllow},
	})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			state := newScopeApprovalState(func(ScopeApprovalRequest) ScopeApprovalDecision {
				return ScopeApprovalDeny
			}, false)
			for j := 0; j < 50; j++ {
				if i%2 == 0 {
					state.setDualEvalSnapshot(func() *permission.Snapshot { return snap })
				}
				_ = state.check("read_file", "/etc/ssl/key.pem", "/workspace/proj")
				if i%2 == 1 {
					state.setDualEvalSnapshot(nil)
				}
			}
		}(i)
	}
	wg.Wait()
}

// Integration: the high-risk bash path (checkHighRisk) dual-evaluates with
// the command/working_dir args convention and keeps its outcome.
func TestScopeApprovalCheckHighRiskDualEvalIntegration(t *testing.T) {
	snap := mustPermissionSnapshot(t, []permission.Rule{
		{Tool: "bash", Effect: permission.EffectAllow, Reason: "operator allow"},
	})
	captured := captureScopeApprovalDualEvalHook(t)
	rejection := "high-risk command"
	state := newScopeApprovalState(func(req ScopeApprovalRequest) ScopeApprovalDecision {
		if req.Kind != localHighRiskApprovalKind {
			t.Errorf("request kind = %q, want %q", req.Kind, localHighRiskApprovalKind)
		}
		return ScopeApprovalDeny
	}, false)
	state.setDualEvalSnapshot(func() *permission.Snapshot { return snap })
	if got := state.checkHighRisk("bash", "rm -rf /data", "/workspace/proj", "/workspace/proj", rejection); got != rejection {
		t.Fatalf("user deny must return the original rejection (legacy unchanged), got %q", got)
	}
	if len(*captured) != 1 {
		t.Fatalf("want 1 mismatch log, got %d", len(*captured))
	}
	m := (*captured)[0]
	if m[0] != "bash" || m[1] != permission.EffectDeny || m[2] != permission.EffectAllow {
		t.Fatalf("tool/legacy/new = %v/%v/%v, want bash/deny/allow (user deny maps to deny)", m[0], m[1], m[2])
	}

	// checkTaskModeGuard uses the same args convention and mapping.
	*captured = nil
	if got := state.checkTaskModeGuard("bash", "make install", "/workspace/proj", "/workspace/proj", rejection); got != rejection {
		t.Fatalf("task-mode guard deny must return the original rejection, got %q", got)
	}
	if len(*captured) != 1 {
		t.Fatalf("want 1 mismatch log from checkTaskModeGuard, got %d", len(*captured))
	}
	if m := (*captured)[0]; m[1] != permission.EffectDeny || m[2] != permission.EffectAllow {
		t.Fatalf("legacy/new = %v/%v, want deny/allow", m[1], m[2])
	}
}
