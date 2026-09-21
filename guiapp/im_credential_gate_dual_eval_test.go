package guiapp

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// captureCredentialFenceDualEvalHook swaps the mismatch hook and returns a
// collector (restored via t.Cleanup).
func captureCredentialFenceDualEvalHook(t *testing.T) *[][]interface{} {
	t.Helper()
	var captured [][]interface{}
	prev := credentialFenceDualEvalMismatchHook
	credentialFenceDualEvalMismatchHook = func(tool string, legacyEffect, newEffect permission.Effect, rule *permission.Rule) {
		captured = append(captured, []interface{}{tool, legacyEffect, newEffect, rule})
	}
	t.Cleanup(func() { credentialFenceDualEvalMismatchHook = prev })
	return &captured
}

func TestCredentialFenceDualEval(t *testing.T) {
	cases := []struct {
		name      string
		snap      func(t *testing.T) *permission.Snapshot
		argsJSON  string
		triggered bool
		wantLog   bool
		wantNew   permission.Effect
	}{
		{
			name: "nil snapshot is silent",
			snap: func(t *testing.T) *permission.Snapshot { return nil },
			// clean payload, no snapshot: nothing to compare, no log.
			wantLog: false,
		},
		{
			name: "no rules: default is silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, nil)
			},
			wantLog: false,
		},
		{
			name: "legacy allow + snapshot deny logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "knowledge_save_text", Effect: permission.EffectDeny, Reason: "no unsupervised writes"},
				})
			},
			wantLog: true,
			wantNew: permission.EffectDeny,
		},
		{
			name: "legacy allow + snapshot ask logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "knowledge_save_text", Effect: permission.EffectAsk},
				})
			},
			wantLog: true,
			wantNew: permission.EffectAsk,
		},
		{
			name: "legacy ask + snapshot allow (operator When rule) logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "knowledge_save_text", Effect: permission.EffectAllow, When: &permission.ArgsPredicate{Field: "action", Equals: "save"}},
				})
			},
			argsJSON:  `{"action":"save","text":"server root sunion123"}`,
			triggered: true,
			wantLog:   true,
			wantNew:   permission.EffectAllow,
		},
		{
			name: "legacy ask + snapshot deny logs",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "knowledge_save_text", Effect: permission.EffectDeny},
				})
			},
			triggered: true,
			wantLog:   true,
			wantNew:   permission.EffectDeny,
		},
		{
			name: "agreement ask vs ask is silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "knowledge_save_text", Effect: permission.EffectAsk},
				})
			},
			triggered: true,
			wantLog:   false,
		},
		{
			name: "agreement allow vs allow is silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "knowledge_save_text", Effect: permission.EffectAllow},
				})
			},
			wantLog: false,
		},
		{
			name: "unparsable args: When rules cannot match, silent",
			snap: func(t *testing.T) *permission.Snapshot {
				return mustPermissionSnapshot(t, []permission.Rule{
					{Tool: "knowledge_save_text", Effect: permission.EffectAllow, When: &permission.ArgsPredicate{Field: "action", Equals: "save"}},
				})
			},
			argsJSON: `{not json`,
			wantLog:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			captured := captureCredentialFenceDualEvalHook(t)
			argsJSON := tc.argsJSON
			if argsJSON == "" {
				argsJSON = `{"text":"普通的知识库笔记内容"}`
			}
			credentialFenceDualEval(tc.snap(t), "knowledge_save_text", argsJSON, tc.triggered)
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
			if m[0] != "knowledge_save_text" {
				t.Fatalf("tool = %v", m[0])
			}
			wantLegacy := permission.EffectAllow
			if tc.triggered {
				wantLegacy = permission.EffectAsk
			}
			if m[1] != wantLegacy || m[2] != tc.wantNew {
				t.Fatalf("legacy/new = %v/%v, want %v/%v", m[1], m[2], wantLegacy, tc.wantNew)
			}
			if rule, ok := m[3].(*permission.Rule); !ok || rule == nil {
				t.Fatalf("rule must be the concrete matched rule, got %v", m[3])
			}
		})
	}
}

func mustPermissionSnapshot(t *testing.T, rules []permission.Rule) *permission.Snapshot {
	t.Helper()
	snap, err := permission.Load(permission.Options{ManagedRules: rules})
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	return snap
}

// Integration: the fence decision point runs dual-eval against the App's
// snapshot; the legacy outcome (clean payload proceeds) must be untouched and
// a diverging concrete rule must be logged.
func TestCredentialGateDualEvalThroughExecutionPath(t *testing.T) {
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{ManagedRules: []permission.Rule{
			{Tool: "knowledge_save_text", Effect: permission.EffectDeny, Reason: "operator deny"},
		}})
	})
	captured := captureCredentialFenceDualEvalHook(t)

	var called int32
	h := credentialGateTestHandler(t, &called)
	result := h.executeToolDetailedWithRuntimeContext(context.Background(), "u1", false, "", "knowledge_save_text", `{"text":"普通的知识库笔记内容"}`, "", nil)
	if result.Outcome != toolOutcomeSucceeded || atomic.LoadInt32(&called) != 1 {
		t.Fatalf("dual-eval must not change fence behavior: %+v called=%d", result, called)
	}
	if len(*captured) != 1 {
		t.Fatalf("divergence must be logged once, got %d", len(*captured))
	}
	m := (*captured)[0]
	if m[1] != permission.EffectAllow || m[2] != permission.EffectDeny {
		t.Fatalf("legacy/new = %v/%v, want allow/deny", m[1], m[2])
	}
}

// Integration: a nil App (TUI standalone shape) skips dual-eval silently.
func TestCredentialGateDualEvalNilAppSkips(t *testing.T) {
	captured := captureCredentialFenceDualEvalHook(t)
	h := &IMMessageHandler{}
	if snap := h.credentialGateDualEvalSnapshot(); snap != nil {
		t.Fatalf("nil-app snapshot = %v, want nil", snap)
	}
	// Directly through the block: h.app == nil fails closed for triggered
	// payloads, but dual-eval must stay silent.
	proceed, result := h.credentialGateBlock(context.Background(), "u1", "knowledge_save_text", credentialGateSecretArgs, "")
	if proceed || result.Outcome != toolOutcomeFailed {
		t.Fatalf("nil handler must fail closed, got proceed=%v result=%+v", proceed, result)
	}
	if len(*captured) != 0 {
		t.Fatalf("nil-app fence must not dual-eval, got %v", *captured)
	}
}
