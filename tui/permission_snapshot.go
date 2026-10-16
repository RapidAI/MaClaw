package main

// Permission snapshot publishing for the core-loop dual-eval harness
// (docs/design/tool-routing-improvement-plan-zh.md Phase 1, R3) on the TUI
// host. Mirrors the guiapp (App.permissionSnapshot) and agentservice
// (CoreAgentExecutor.permissionSnapshot) slices, but scoped process-level:
// the TUI is a single-process host without the guiapp App object, and all
// seven TUI loop-callback types delegate to this one holder, giving them
// uniform policy observability. The snapshot is built at most once per
// process and is immutable afterwards; the core loop's dual-eval harness can
// hold the returned pointer without further synchronization.

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// permissionDualEvalEnvKey is the kill switch: MACLAW_PERMISSION_DUAL_EVAL=off
// (or 0/false/no) makes the TUI publish no snapshot, disabling dual-eval.
const permissionDualEvalEnvKey = "MACLAW_PERMISSION_DUAL_EVAL"

// permissionLoadFunc is the snapshot loader seam; tests swap it to stub the
// rule sources without touching the real ~/.maclaw/config.json.
var permissionLoadFunc = permission.Load

// permissionDualEvalDisabled reports whether the kill switch disables
// dual-eval. Same semantics as the guiapp/agentservice slices: explicit off
// values win, anything else (including unset) keeps the harness on.
func permissionDualEvalDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(permissionDualEvalEnvKey))) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

// tuiPermissionSnapshotState is the process-level lazy holder.
type tuiPermissionSnapshotState struct {
	once sync.Once
	snap *permission.Snapshot
	err  error
}

// tuiPermissionSnapshotHolder is the single process-level holder instance.
// Tests replace the pointer to get a fresh once.
var tuiPermissionSnapshotHolder = &tuiPermissionSnapshotState{}

// tuiPermissionSnapshot returns the process-level merged permission snapshot
// for the dual-eval harness, or nil when dual-eval is disabled or the load
// failed (skip-over-noise: a transient loader error must not turn into a
// permanent deny-all plus per-call divergence spam — see the guiapp slice for
// the full policy note; the flip slice must revisit deny-all vs skip).
// The snapshot is built at most once per process and is immutable afterwards.
func tuiPermissionSnapshot() *permission.Snapshot {
	if permissionDualEvalDisabled() {
		return nil
	}
	tuiPermissionSnapshotHolder.once.Do(func() {
		tuiPermissionSnapshotHolder.snap, tuiPermissionSnapshotHolder.err = buildPermissionSnapshot()
		if tuiPermissionSnapshotHolder.err != nil {
			log.Printf("[tui] dual-eval permission snapshot load failed, dual-eval disabled for this process (skip-over-noise): %v", tuiPermissionSnapshotHolder.err)
			tuiPermissionSnapshotHolder.snap = nil
		}
	})
	return tuiPermissionSnapshotHolder.snap
}

// buildPermissionSnapshot merges the managed/user-config/project rule
// sources into one snapshot. A managed rule source does not exist in this
// repo yet, so ManagedRules stays nil and ManagedYoloPin stays false.
func buildPermissionSnapshot() (*permission.Snapshot, error) {
	return permissionLoadFunc(permission.Options{
		// config.json stays under the default base dir (~/.maclaw) even when a
		// custom data_dir is configured (see corelib.MaclawBaseDir docs).
		UserConfigPath: filepath.Join(corelib.MaclawDefaultBaseDir(), "config.json"),
		ProjectDir:     corelib.EffectiveWorkspaceDir(),
		// TrustProject is hardcoded false: no workspace-trust signal exists
		// yet (plan: 不信任目录不贡献规则), so project-level rule files
		// contribute nothing until a trust gate lands.
		TrustProject: false,
	})
}

// PermissionSnapshot implements agent.PermissionSnapshotProvider on every
// TUI loop-callback type. All seven delegate to the process-level holder; a
// nil callback publishes no snapshot, so dual-eval stays off there.

func (c *tuiCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil {
		return nil
	}
	return tuiPermissionSnapshot()
}

func (c *tuiBtwCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil {
		return nil
	}
	return tuiPermissionSnapshot()
}

func (c *tuiLoopCycleCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil {
		return nil
	}
	return tuiPermissionSnapshot()
}

func (c *pipeCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil {
		return nil
	}
	return tuiPermissionSnapshot()
}

func (c *rpcCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil {
		return nil
	}
	return tuiPermissionSnapshot()
}

func (c *tuiWeixinCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil {
		return nil
	}
	return tuiPermissionSnapshot()
}

func (c *tuiSchedulerCallbacks) PermissionSnapshot() *permission.Snapshot {
	if c == nil {
		return nil
	}
	return tuiPermissionSnapshot()
}

var (
	_ agent.PermissionSnapshotProvider = (*tuiCallbacks)(nil)
	_ agent.PermissionSnapshotProvider = (*tuiBtwCallbacks)(nil)
	_ agent.PermissionSnapshotProvider = (*tuiLoopCycleCallbacks)(nil)
	_ agent.PermissionSnapshotProvider = (*pipeCallbacks)(nil)
	_ agent.PermissionSnapshotProvider = (*rpcCallbacks)(nil)
	_ agent.PermissionSnapshotProvider = (*tuiWeixinCallbacks)(nil)
	_ agent.PermissionSnapshotProvider = (*tuiSchedulerCallbacks)(nil)
)
