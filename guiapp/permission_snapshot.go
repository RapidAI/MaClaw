package guiapp

// Permission snapshot publishing for the shared agent loop's dual-eval
// harness (docs/design/tool-routing-improvement-plan-zh.md Phase 1, R3):
// the App builds one merged permission.Snapshot lazily and the IM host's
// loop callbacks publish it via agent.PermissionSnapshotProvider, so
// corelib/agent dual-evaluates every authorizeLoopTool decision against the
// snapshot and logs divergences. Behavior never changes here; only the
// old-vs-new comparison runs.

import (
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// permissionDualEvalEnvKey is the kill switch: MACLAW_PERMISSION_DUAL_EVAL=off
// (or 0/false/no) makes the App publish no snapshot, disabling dual-eval.
const permissionDualEvalEnvKey = "MACLAW_PERMISSION_DUAL_EVAL"

// permissionLoadFunc is the snapshot loader seam; tests swap it to stub the
// rule sources without touching the real ~/.maclaw/config.json.
var permissionLoadFunc = permission.Load

// permissionDualEvalDisabled reports whether the kill switch disables
// dual-eval. Env-flag convention matches the shared agent-loop flags
// (resolveSharedAgentLoopModeLive): explicit off values win, anything else
// (including unset) keeps the harness on.
func permissionDualEvalDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(permissionDualEvalEnvKey))) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

// permissionSnapshot returns the App's merged permission snapshot for the
// dual-eval harness, or nil when dual-eval is disabled. The snapshot is built
// at most once per App and is immutable afterwards.
//
// POLICY (dual-eval phase): on loader error a NIL snapshot is published, so
// dual-eval silently skips for the rest of the process, and the failure is
// logged ONCE here (at build time) instead of re-logging on every tool call.
// The engine-level contract is unchanged: permission.Load still fails closed
// with DenyAllSnapshot+err (see corelib/permission). Deny-all vs skip is a
// policy decision that the flip slice MUST revisit before behavior flips —
// during observation, skip-over-noise beats a permanent deny-all that turns
// one transient loader error into endless divergence spam.
func (a *App) permissionSnapshot() *permission.Snapshot {
	if a == nil || permissionDualEvalDisabled() {
		return nil
	}
	a.permissionSnapshotOnce.Do(func() {
		a.permissionSnapshotVal, a.permissionSnapshotErr = a.buildPermissionSnapshot()
		if a.permissionSnapshotErr != nil {
			log.Printf("[permission] dual-eval snapshot load failed, dual-eval disabled for this process (skip-over-noise; engine Load still fails closed): %v", a.permissionSnapshotErr)
			a.permissionSnapshotVal = nil
		}
	})
	return a.permissionSnapshotVal
}

// buildPermissionSnapshot merges the managed/user-config/project rule
// sources into one snapshot. A managed rule source does not exist in this
// repo yet, so ManagedRules stays nil and ManagedYoloPin stays false.
//
// After Load succeeds, expert definitions are translated into subject-scoped
// rules and appended (see expert_permission_rules.go). A translation failure
// is logged and the snapshot is published WITHOUT expert rules: dual-eval
// degradation must never break the snapshot.
//
// NOTE(staleness): the snapshot is built once per process (sync.Once), so
// experts edited at runtime do not re-translate until restart. Acceptable
// for the dual-eval observation phase; MUST be revisited before the
// behavior-flip slice.
func (a *App) buildPermissionSnapshot() (*permission.Snapshot, error) {
	userConfigPath, err := a.getConfigPath()
	if err != nil {
		log.Printf("[permission] config path unresolved, user-config rules skipped: %v", err)
		userConfigPath = ""
	}
	snap, err := permissionLoadFunc(permission.Options{
		UserConfigPath: userConfigPath,
		ProjectDir:     corelib.EffectiveWorkspaceDir(),
		// TrustProject is hardcoded false: the repo has no workspace-trust
		// signal yet (searched guiapp/corelib for trust-project concepts;
		// only unrelated signing/evidence "untrusted" handling exists). Per
		// the plan's 不信任目录不贡献规则, project-level rule files
		// (.maclaw/permission.json, .claude/settings.json) contribute nothing
		// until a trust gate lands.
		TrustProject: false,
	})
	if err != nil || snap == nil {
		// Load already fails closed (DenyAllSnapshot); expert rules would be
		// pointless on top of it.
		return snap, err
	}
	expertRules, err := loadExpertPermissionRules()
	if err != nil {
		log.Printf("[permission] expert rule translation failed, expert rules skipped: %v", err)
		return snap, nil
	}
	return snap.WithAdditionalRules(expertRules), nil
}

// PermissionSnapshot implements agent.PermissionSnapshotProvider on the IM
// host's loop callbacks. It delegates to the App-level holder; a nil handler
// or App (TUI standalone mode) publishes no snapshot, so dual-eval stays off
// on those hosts.
func (c *sharedAgentLoopCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil || c.handler == nil || c.handler.app == nil {
		return nil
	}
	return c.handler.app.permissionSnapshot()
}

var _ agent.PermissionSnapshotProvider = (*sharedAgentLoopCallbacks)(nil)
