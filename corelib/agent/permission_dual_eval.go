package agent

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/permission"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// PermissionSnapshotProvider lets a host publish the merged permission rule
// snapshot (docs/design/tool-routing-improvement-plan-zh.md Phase 1, R3) to
// the core loop. When implemented, authorizeLoopTool dual-evaluates every
// admission decision against the snapshot and logs divergences — the
// old-vs-new "对拍" harness. Divergence logging never changes the legacy
// outcome; behavior flips only come after a gate's migration slice.
type PermissionSnapshotProvider interface {
	PermissionSnapshot() *permission.Snapshot
}

// dualEvalMismatchHook is replaceable in tests to capture mismatch reports.
var dualEvalMismatchHook = func(gate, tool string, legacyEffect, newEffect permission.Effect, rule *permission.Rule) {
	coretool.RecordPermissionDualEvalMismatch(gate, tool, string(legacyEffect), string(newEffect))
	src := ""
	if rule != nil {
		src = rule.Source
	}
	log.Printf("[permission-dual-eval] gate=%s tool=%q legacy=%s new=%s rule_source=%s reason=%q",
		gate, tool, legacyEffect, newEffect, src, ruleReason(rule))
}

func ruleReason(r *permission.Rule) string {
	if r == nil {
		return ""
	}
	return r.Reason
}

// dualEvalPermissionDecision compares one legacy gate outcome with the
// permission snapshot decision for the same call. Only concrete rule
// matches (dec.Rule != nil) are reported — an unmatched decision
// (Decision.Default) carries no policy intent and must not spam the log.
// ask-vs-deny/allow differences are reported too: they mark spots where the
// future gate will pause for confirmation instead of the legacy flat outcome.
func dualEvalPermissionDecision(gate, tool string, dec permission.Decision, legacyEffect permission.Effect) {
	if dec.Rule == nil {
		return
	}
	if dec.Effect != legacyEffect {
		dualEvalMismatchHook(gate, tool, legacyEffect, dec.Effect, dec.Rule)
	}
}

// resolveDualEvalDecision returns the snapshot decision for one call, or
// Decision{Default:true} when the host publishes no snapshot or args parsing
// fails. The args payload is parsed only when the snapshot actually contains
// When-predicate rules; a name/kind-only snapshot decides identically with
// nil args, so the per-call JSON parse is skipped on the hot path. Kind
// falls back to the builtin table; unknown tools keep kind "" so only
// unqualified rules can match them (fail-closed by construction).
func resolveDualEvalDecision(cb LoopCallbacks, name, argsJSON string) permission.Decision {
	provider, ok := cb.(PermissionSnapshotProvider)
	if !ok || provider == nil {
		return permission.Decision{Default: true}
	}
	snap := provider.PermissionSnapshot()
	if snap == nil {
		return permission.Decision{Default: true}
	}
	var args map[string]interface{}
	if snap.HasArgsRules() && strings.TrimSpace(argsJSON) != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			args = nil
		}
	}
	kind := permission.BuiltinKind(name)
	return snap.Decide(name, kind, args)
}
