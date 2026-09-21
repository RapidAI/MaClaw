package agent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/permission"
)

type dualEvalTestCallbacks struct {
	LoopCallbacks
	snap             *permission.Snapshot
	profile          PromptProfile
	toolAllowed      bool
	toolCallAllowed  bool
	authorizerDeny   bool
	toolCallDeny     bool
	capturedMismatch []dualEvalMismatch
}

type dualEvalMismatch struct {
	gate         string
	tool         string
	legacyEffect permission.Effect
	newEffect    permission.Effect
}

func (c *dualEvalTestCallbacks) PermissionSnapshot() *permission.Snapshot { return c.snap }
func (c *dualEvalTestCallbacks) CurrentPromptProfile() PromptProfile      { return c.profile }
func (c *dualEvalTestCallbacks) IsToolAllowed(name string) bool           { return c.toolAllowed }
func (c *dualEvalTestCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	return c.toolCallAllowed, ""
}

func withMismatchCapture(t *testing.T, cb *dualEvalTestCallbacks) {
	t.Helper()
	orig := dualEvalMismatchHook
	dualEvalMismatchHook = func(gate, tool string, legacyEffect, newEffect permission.Effect, rule *permission.Rule) {
		cb.capturedMismatch = append(cb.capturedMismatch, dualEvalMismatch{gate, tool, legacyEffect, newEffect})
	}
	t.Cleanup(func() { dualEvalMismatchHook = orig })
}

func TestDualEvalNoProviderIsSilent(t *testing.T) {
	cb := &dualEvalTestCallbacks{toolAllowed: true, toolCallAllowed: true}
	res, denied := authorizeLoopTool(cb, "bash", "{}")
	if denied {
		t.Fatalf("unexpected deny: %+v", res)
	}
}

func TestDualEvalMismatchReportedOnAllowPath(t *testing.T) {
	snap := permission.DenyAllSnapshot("test")
	cb := &dualEvalTestCallbacks{snap: snap, toolAllowed: true, toolCallAllowed: true}
	withMismatchCapture(t, cb)
	// Legacy allows (fall-through); permission denies with a concrete rule.
	res, denied := authorizeLoopTool(cb, "bash", "{}")
	if denied {
		t.Fatalf("legacy behavior must not change during dual-eval: %+v", res)
	}
	if len(cb.capturedMismatch) != 1 {
		t.Fatalf("mismatches=%v", cb.capturedMismatch)
	}
	m := cb.capturedMismatch[0]
	if m.gate != "authorize_fallthrough" || m.legacyEffect != permission.EffectAllow || m.newEffect != permission.EffectDeny {
		t.Fatalf("mismatch=%+v", m)
	}
}

func TestDualEvalNoMismatchWhenRuleAgrees(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "database", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "action", Equals: "execute"}, Source: "test"},
	}
	snap := testSnapshotWithRules(t, rules)
	cb := &dualEvalTestCallbacks{snap: snap, profile: PromptProfileLight, toolAllowed: true, toolCallAllowed: true}
	withMismatchCapture(t, cb)
	// Legacy light path also denies database writes.
	_, denied := authorizeLoopTool(cb, "database", `{"action":"execute"}`)
	if !denied {
		t.Fatalf("legacy light deny expected")
	}
	if len(cb.capturedMismatch) != 0 {
		t.Fatalf("agreeing rules must not be reported: %v", cb.capturedMismatch)
	}
}

func TestDualEvalMismatchOnLightDenyVsRuleAllow(t *testing.T) {
	rules := []permission.Rule{
		{Tool: "bash", Effect: permission.EffectAllow, Source: "test"},
	}
	snap := testSnapshotWithRules(t, rules)
	cb := &dualEvalTestCallbacks{snap: snap, profile: PromptProfileLight, toolAllowed: true, toolCallAllowed: true}
	withMismatchCapture(t, cb)
	// Light allowlist denies bash; permission rule allows it.
	_, denied := authorizeLoopTool(cb, "bash", "{}")
	if !denied {
		t.Fatalf("legacy light deny expected")
	}
	if len(cb.capturedMismatch) != 1 || cb.capturedMismatch[0].gate != "light_allowlist" {
		t.Fatalf("mismatches=%v", cb.capturedMismatch)
	}
}

func TestDualEvalDefaultDecisionIsSilent(t *testing.T) {
	// Snapshot with no matching rules: Decide returns Default → no report.
	snap := testSnapshotWithRules(t, []permission.Rule{{Tool: "web_search", Effect: permission.EffectAllow, Source: "test"}})
	cb := &dualEvalTestCallbacks{snap: snap, toolAllowed: true, toolCallAllowed: true}
	withMismatchCapture(t, cb)
	if _, denied := authorizeLoopTool(cb, "bash", "{}"); denied {
		t.Fatalf("unexpected deny")
	}
	if len(cb.capturedMismatch) != 0 {
		t.Fatalf("default decisions must be silent: %v", cb.capturedMismatch)
	}
}

func testSnapshotWithRules(t *testing.T, rules []permission.Rule) *permission.Snapshot {
	t.Helper()
	// Build a snapshot through Load with managed rules only (no file sources).
	snap, err := permission.Load(permission.Options{ManagedRules: rules})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return snap
}

// TestResolveDualEvalSkipsArgsParseWithoutWhenRules: a snapshot with only
// name/kind rules decides identically for nil, valid, and unparseable args —
// the per-call JSON parse is skipped, so no parse path can change the
// outcome.
func TestResolveDualEvalSkipsArgsParseWithoutWhenRules(t *testing.T) {
	snap := testSnapshotWithRules(t, []permission.Rule{
		{Tool: "bash", Effect: permission.EffectAllow, Source: "test"},
	})
	cb := &dualEvalTestCallbacks{snap: snap}
	for _, argsJSON := range []string{"", "{}", `{"path":"/tmp/x"}`, "{unparseable", "not json at all"} {
		dec := resolveDualEvalDecision(cb, "bash", argsJSON)
		if dec.Default || dec.Effect != permission.EffectAllow {
			t.Fatalf("argsJSON=%q: want name-rule match, got %+v", argsJSON, dec)
		}
	}
}

// TestResolveDualEvalParsesWhenRulesPresent: when the snapshot carries a
// When predicate, args parsing still happens and drives the decision.
func TestResolveDualEvalParsesWhenRulesPresent(t *testing.T) {
	snap := testSnapshotWithRules(t, []permission.Rule{
		{Tool: "database", Effect: permission.EffectDeny, When: &permission.ArgsPredicate{Field: "action", Equals: "execute"}, Source: "test"},
	})
	cb := &dualEvalTestCallbacks{snap: snap}
	if dec := resolveDualEvalDecision(cb, "database", `{"action":"execute"}`); dec.Default || dec.Effect != permission.EffectDeny {
		t.Fatalf("matching args must hit the When rule: %+v", dec)
	}
	if dec := resolveDualEvalDecision(cb, "database", `{"action":"select"}`); !dec.Default {
		t.Fatalf("non-matching args must default: %+v", dec)
	}
}
