package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// swapPermissionLoadFunc stubs the loader seam for the duration of a test.
func swapPermissionLoadFunc(t *testing.T, fn func(permission.Options) (*permission.Snapshot, error)) {
	t.Helper()
	prev := permissionLoadFunc
	permissionLoadFunc = fn
	t.Cleanup(func() {
		permissionLoadFunc = prev
	})
}

// resetTuiPermissionSnapshotHolder gives the test a fresh process-level holder.
func resetTuiPermissionSnapshotHolder(t *testing.T) {
	t.Helper()
	fresh := &tuiPermissionSnapshotState{}
	prev := tuiPermissionSnapshotHolder
	tuiPermissionSnapshotHolder = fresh
	t.Cleanup(func() {
		tuiPermissionSnapshotHolder = prev
	})
}

func TestPermissionDualEvalDisabledKillSwitch(t *testing.T) {
	for _, v := range []string{"off", "0", "OFF", " false "} {
		t.Run("value="+v, func(t *testing.T) {
			t.Setenv(permissionDualEvalEnvKey, v)
			if !permissionDualEvalDisabled() {
				t.Fatalf("permissionDualEvalDisabled() = false, want true for %q", v)
			}
			resetTuiPermissionSnapshotHolder(t)
			if snap := tuiPermissionSnapshot(); snap != nil {
				t.Fatalf("tuiPermissionSnapshot() = %v, want nil under kill switch", snap)
			}
		})
	}
	t.Run("unset stays on", func(t *testing.T) {
		t.Setenv(permissionDualEvalEnvKey, "")
		if permissionDualEvalDisabled() {
			t.Fatal("permissionDualEvalDisabled() = true with env unset, want false")
		}
	})
}

func TestTuiPermissionSnapshotBuildsOnceAndPublishes(t *testing.T) {
	t.Setenv(permissionDualEvalEnvKey, "")
	resetTuiPermissionSnapshotHolder(t)
	var mu sync.Mutex
	var gotOpts []permission.Options
	swapPermissionLoadFunc(t, func(opts permission.Options) (*permission.Snapshot, error) {
		mu.Lock()
		gotOpts = append(gotOpts, opts)
		mu.Unlock()
		return permission.Load(permission.Options{})
	})
	snap := tuiPermissionSnapshot()
	if snap == nil {
		t.Fatal("tuiPermissionSnapshot() = nil, want non-nil snapshot")
	}
	if again := tuiPermissionSnapshot(); again != snap {
		t.Fatal("second tuiPermissionSnapshot() returned a different snapshot; want cached immutable value")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gotOpts) != 1 {
		t.Fatalf("loader called %d times, want 1 (sync.Once)", len(gotOpts))
	}
	opts := gotOpts[0]
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	wantPath := filepath.Join(home, ".maclaw", "config.json")
	if opts.UserConfigPath != wantPath {
		t.Fatalf("UserConfigPath = %q, want %q", opts.UserConfigPath, wantPath)
	}
	if opts.TrustProject {
		t.Fatal("TrustProject = true, want false (no workspace-trust signal exists yet)")
	}
	if len(opts.ManagedRules) != 0 {
		t.Fatalf("ManagedRules = %v, want nil/empty (no managed source in this repo)", opts.ManagedRules)
	}
	if opts.ManagedYoloPin {
		t.Fatal("ManagedYoloPin = true, want false")
	}
	if opts.ProjectDir != corelib.EffectiveWorkspaceDir() {
		t.Fatalf("ProjectDir = %q, want effective workspace %q", opts.ProjectDir, corelib.EffectiveWorkspaceDir())
	}
}

// TestTuiPermissionSnapshotPublishesNilOnLoadError pins the dual-eval-phase
// skip-over-noise policy: a loader error publishes a NIL snapshot (dual-eval
// silently skips for the process) and the failure is logged exactly once at
// build time. The engine-level DenyAllSnapshot+err contract is covered by
// corelib/permission's tests.
func TestTuiPermissionSnapshotPublishesNilOnLoadError(t *testing.T) {
	t.Setenv(permissionDualEvalEnvKey, "")
	resetTuiPermissionSnapshotHolder(t)
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.DenyAllSnapshot("rule source unreadable: test"), os.ErrPermission
	})
	var buf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	if snap := tuiPermissionSnapshot(); snap != nil {
		t.Fatalf("tuiPermissionSnapshot() = %v on load error, want nil (dual-eval skips)", snap)
	}
	if snap := tuiPermissionSnapshot(); snap != nil {
		t.Fatalf("second tuiPermissionSnapshot() = %v on load error, want nil", snap)
	}
	if n := strings.Count(buf.String(), "dual-eval permission snapshot load failed"); n != 1 {
		t.Fatalf("load failure logged %d times, want exactly 1 (logged once at build time): %q", n, buf.String())
	}
}

func TestTuiCallbacksPermissionSnapshotNilSafe(t *testing.T) {
	var nilTui *tuiCallbacks
	if snap := nilTui.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil tuiCallbacks PermissionSnapshot() = %v, want nil", snap)
	}
	var nilBtw *tuiBtwCallbacks
	if snap := nilBtw.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil tuiBtwCallbacks PermissionSnapshot() = %v, want nil", snap)
	}
	var nilCycle *tuiLoopCycleCallbacks
	if snap := nilCycle.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil tuiLoopCycleCallbacks PermissionSnapshot() = %v, want nil", snap)
	}
	var nilPipe *pipeCallbacks
	if snap := nilPipe.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil pipeCallbacks PermissionSnapshot() = %v, want nil", snap)
	}
	var nilRPC *rpcCallbacks
	if snap := nilRPC.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil rpcCallbacks PermissionSnapshot() = %v, want nil", snap)
	}
	var nilWeixin *tuiWeixinCallbacks
	if snap := nilWeixin.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil tuiWeixinCallbacks PermissionSnapshot() = %v, want nil", snap)
	}
	var nilScheduler *tuiSchedulerCallbacks
	if snap := nilScheduler.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil tuiSchedulerCallbacks PermissionSnapshot() = %v, want nil", snap)
	}
}

func TestTuiCallbacksPermissionSnapshotDelegatesToHolder(t *testing.T) {
	t.Setenv(permissionDualEvalEnvKey, "")
	resetTuiPermissionSnapshotHolder(t)
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{})
	})
	if snap := (&tuiCallbacks{}).PermissionSnapshot(); snap == nil {
		t.Fatal("tuiCallbacks.PermissionSnapshot() = nil, want non-nil snapshot")
	}
	// All seven types share the process-level holder; spot-check two more
	// structurally different variants (side-query and pipe mode).
	if snap := (&tuiBtwCallbacks{}).PermissionSnapshot(); snap == nil {
		t.Fatal("tuiBtwCallbacks.PermissionSnapshot() = nil, want non-nil snapshot")
	}
	if snap := (&pipeCallbacks{}).PermissionSnapshot(); snap == nil {
		t.Fatal("pipeCallbacks.PermissionSnapshot() = nil, want non-nil snapshot")
	}
}
