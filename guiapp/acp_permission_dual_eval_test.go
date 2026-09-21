package guiapp

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// captureACPPermissionDualEvalHook swaps the mismatch hook and returns a
// collector (restored via t.Cleanup).
func captureACPPermissionDualEvalHook(t *testing.T) *[][]interface{} {
	t.Helper()
	var captured [][]interface{}
	prev := acpPermissionDualEvalMismatchHook
	acpPermissionDualEvalMismatchHook = func(tool string, legacyEffect, newEffect permission.Effect, rule *permission.Rule) {
		captured = append(captured, []interface{}{tool, legacyEffect, newEffect, rule})
	}
	t.Cleanup(func() { acpPermissionDualEvalMismatchHook = prev })
	return &captured
}

func TestACPPermissionDualEval(t *testing.T) {
	cases := []struct {
		name    string
		snap    func(t *testing.T) *permission.Snapshot
		tool    string
		args    string
		allowed bool
		wantLog bool
		wantNew permission.Effect
	}{
		{
			name: "nil snapshot is silent",
			snap: func(t *testing.T) *permission.Snapshot { return nil },
			tool: "bash", args: `{"command":"ls"}`, allowed: true,
		},
		{
			name: "no rules: default is silent",
			snap: func(t *testing.T) *permission.Snapshot { return mustPermissionSnapshot(t, nil) },
			tool: "bash", args: `{"command":"ls"}`, allowed: true,
		},
		{
			name: "legacy allow + deny rule logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "bash", Effect: permission.EffectDeny, Reason: "host denies shell"},
				})
			},
			tool: "bash", args: `{"command":"ls"}`, allowed: true,
			wantLog: true, wantNew: permission.EffectDeny,
		},
		{
			name: "legacy deny + allow rule logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "bash", Effect: permission.EffectAllow},
				})
			},
			tool: "bash", args: `{"command":"ls"}`, allowed: false,
			wantLog: true, wantNew: permission.EffectAllow,
		},
		{
			name: "path-scoped Prefix rule on write_file diverges from legacy allow",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "write_file", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "path", Prefix: "C:\\secrets\\"}},
				})
			},
			tool: "write_file", args: `{"path":"C:\\secrets\\key.txt","content":"x"}`, allowed: true,
			wantLog: true, wantNew: permission.EffectDeny,
		},
		{
			name: "agreement allow is silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "bash", Effect: permission.EffectAllow},
				})
			},
			tool: "bash", args: `{"command":"ls"}`, allowed: true,
		},
		{
			name: "agreement deny is silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "bash", Effect: permission.EffectDeny},
				})
			},
			tool: "bash", args: `{"command":"ls"}`, allowed: false,
		},
		{
			name: "unparsable args: When rules cannot match, silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "write_file", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "path", Prefix: "C:\\secrets\\"}},
				})
			},
			tool: "write_file", args: `{not json`, allowed: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captured := captureACPPermissionDualEvalHook(t)
			acpPermissionDualEval(tc.snap(t), tc.tool, tc.args, tc.allowed)
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
			wantLegacy := permission.EffectDeny
			if tc.allowed {
				wantLegacy = permission.EffectAllow
			}
			if m[0] != tc.tool || m[1] != wantLegacy || m[2] != tc.wantNew {
				t.Fatalf("tool/legacy/new = %v/%v/%v, want %v/%v/%v", m[0], m[1], m[2], tc.tool, wantLegacy, tc.wantNew)
			}
			if rule, ok := m[3].(*permission.Rule); !ok || rule == nil {
				t.Fatalf("rule must be the concrete matched rule, got %v", m[3])
			}
		})
	}
}

// The registry check outcome itself must be untouched by dual-eval: a
// registered deny gate still denies, an ungated request still allows.
func TestACPPermissionCheckOutcomeUntouched(t *testing.T) {
	captureACPPermissionDualEvalHook(t) // any accidental logging would be visible here
	clear := globalACPPermission.set("acp-dual-eval-test", func(ctx context.Context, toolName, argsJSON string) (bool, string) {
		return false, "user rejected tool in VS Code"
	})
	t.Cleanup(clear)

	allowed, reason := globalACPPermission.check(context.Background(), "acp-dual-eval-test", "bash", `{"command":"ls"}`)
	if allowed || reason == "" {
		t.Fatalf("deny gate must still deny, got allowed=%v reason=%q", allowed, reason)
	}
	// Tool the gate does not cover: allow, and no dual-eval log either (the
	// registry check itself performs no dual-eval).
	allowed, _ = globalACPPermission.check(context.Background(), "acp-dual-eval-test", "read_file", `{"path":"a.go"}`)
	if !allowed {
		t.Fatal("ungated tool must still allow")
	}
	// Non-ACP requestID: allow without consulting the gate.
	allowed, _ = globalACPPermission.check(context.Background(), "other-request", "bash", `{"command":"ls"}`)
	if !allowed {
		t.Fatal("non-ACP request must allow")
	}
}

func TestACPPermissionDualEvalSnapshotResolver(t *testing.T) {
	var nilHandler *IMMessageHandler
	if snap := nilHandler.acpPermissionDualEvalSnapshot(); snap != nil {
		t.Fatalf("nil handler snapshot = %v, want nil", snap)
	}
	if snap := (&IMMessageHandler{}).acpPermissionDualEvalSnapshot(); snap != nil {
		t.Fatalf("nil-app snapshot = %v, want nil", snap)
	}
	t.Setenv(permissionDualEvalEnvKey, "off")
	if snap := (&IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}).acpPermissionDualEvalSnapshot(); snap != nil {
		t.Fatalf("kill-switched snapshot = %v, want nil", snap)
	}
	t.Setenv(permissionDualEvalEnvKey, "")
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{})
	})
	if snap := (&IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}).acpPermissionDualEvalSnapshot(); snap == nil {
		t.Fatal("app-backed snapshot = nil, want non-nil")
	}
}
