package agentservice

// Permission snapshot publishing for the core-loop dual-eval harness
// (docs/design/tool-routing-improvement-plan-zh.md Phase 1, R3): the
// CoreAgentExecutor builds one merged permission.Snapshot lazily and the
// per-request coreAgentCallbacks publish it via
// agent.PermissionSnapshotProvider, so corelib/agent dual-evaluates every
// authorizeLoopTool decision on service turns against the snapshot and logs
// divergences. Behavior never changes here; only the old-vs-new comparison
// runs. Mirrors the guiapp IM-host slice (guiapp/permission_snapshot.go).

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// permissionDualEvalEnvKey is the kill switch: MACLAW_PERMISSION_DUAL_EVAL=off
// (or 0/false/no) makes the executor publish no snapshot, disabling dual-eval.
const permissionDualEvalEnvKey = "MACLAW_PERMISSION_DUAL_EVAL"

// permissionLoadFunc is the snapshot loader seam; tests swap it to stub the
// rule sources without touching the real ~/.maclaw/config.json.
var permissionLoadFunc = permission.Load

// permissionDualEvalDisabled reports whether the kill switch disables
// dual-eval. Same semantics as the guiapp slice: explicit off values win,
// anything else (including unset) keeps the harness on.
func permissionDualEvalDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(permissionDualEvalEnvKey))) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

// permissionSnapshot returns the executor's merged permission snapshot for
// the dual-eval harness, or nil when dual-eval is disabled or the load failed
// (skip-over-noise: a transient loader error must not turn into a permanent
// deny-all plus per-call divergence spam — see the guiapp slice for the full
// policy note; the flip slice must revisit deny-all vs skip). The snapshot is
// built at most once per executor and is immutable afterwards.
func (e *CoreAgentExecutor) permissionSnapshot() *permission.Snapshot {
	if e == nil || permissionDualEvalDisabled() {
		return nil
	}
	e.permissionSnapshotOnce.Do(func() {
		e.permissionSnapshotVal, e.permissionSnapshotErr = buildPermissionSnapshot()
		if e.permissionSnapshotErr != nil {
			log.Printf("[agentservice] dual-eval permission snapshot load failed, dual-eval disabled for this process (skip-over-noise): %v", e.permissionSnapshotErr)
			e.permissionSnapshotVal = nil
		}
	})
	return e.permissionSnapshotVal
}

// buildPermissionSnapshot merges the managed/user-config/project rule
// sources into one snapshot. A managed rule source does not exist in this
// repo yet, so ManagedRules stays nil and ManagedYoloPin stays false.
func buildPermissionSnapshot() (*permission.Snapshot, error) {
	// config.json stays under the default base dir (~/.maclaw) even when a
	// custom data_dir is configured (see corelib.MaclawBaseDir docs), so the
	// default-base helper is the canonical user-config path here.
	return permissionLoadFunc(permission.Options{
		UserConfigPath: filepath.Join(maclawpath.DefaultBaseDir(), "config.json"),
		// ProjectDir stays empty: the executor is shared across tenants and
		// sessions, and the per-request workspace (req.Instance.Workspace)
		// cannot seed a once-per-executor snapshot. TrustProject is false
		// anyway, so project-level rule files contribute nothing until a
		// trust gate lands (plan: 不信任目录不贡献规则).
		TrustProject: false,
	})
}

// PermissionSnapshot implements agent.PermissionSnapshotProvider on the
// service-side loop callbacks. It delegates to the executor-level holder; a
// nil callback or executor publishes no snapshot, so dual-eval stays off
// there.
func (c *coreAgentCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil || c.executor == nil {
		return nil
	}
	return c.executor.permissionSnapshot()
}

var _ agent.PermissionSnapshotProvider = (*coreAgentCallbacks)(nil)
