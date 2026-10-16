package agentservice

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/permission"
)

func TestPermissionDualEvalDisabledKillSwitch(t *testing.T) {
	for _, v := range []string{"off", "0", "OFF", " false "} {
		t.Run("value="+v, func(t *testing.T) {
			t.Setenv(permissionDualEvalEnvKey, v)
			if !permissionDualEvalDisabled() {
				t.Fatalf("permissionDualEvalDisabled() = false, want true for %q", v)
			}
			e := &CoreAgentExecutor{}
			if snap := e.permissionSnapshot(); snap != nil {
				t.Fatalf("permissionSnapshot() = %v, want nil under kill switch", snap)
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

func TestPermissionSnapshotNilExecutor(t *testing.T) {
	var e *CoreAgentExecutor
	if snap := e.permissionSnapshot(); snap != nil {
		t.Fatalf("nil executor permissionSnapshot() = %v, want nil", snap)
	}
}

// swapPermissionLoadFunc stubs the loader seam for the duration of a test.
func swapPermissionLoadFunc(t *testing.T, fn func(permission.Options) (*permission.Snapshot, error)) {
	t.Helper()
	prev := permissionLoadFunc
	permissionLoadFunc = fn
	t.Cleanup(func() {
		permissionLoadFunc = prev
	})
}

func TestCoreAgentCallbacksPermissionSnapshotNilSafe(t *testing.T) {
	var nilCallbacks *coreAgentCallbacks
	if snap := nilCallbacks.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil callbacks PermissionSnapshot() = %v, want nil", snap)
	}
	if snap := (&coreAgentCallbacks{}).PermissionSnapshot(); snap != nil {
		t.Fatalf("nil-executor callbacks PermissionSnapshot() = %v, want nil", snap)
	}
}

func TestExecutorPermissionSnapshotBuildsOnceAndPublishes(t *testing.T) {
	var mu sync.Mutex
	var gotOpts []permission.Options
	swapPermissionLoadFunc(t, func(opts permission.Options) (*permission.Snapshot, error) {
		mu.Lock()
		gotOpts = append(gotOpts, opts)
		mu.Unlock()
		return permission.Load(permission.Options{})
	})
	e := &CoreAgentExecutor{}
	snap := e.permissionSnapshot()
	if snap == nil {
		t.Fatal("permissionSnapshot() = nil, want non-nil snapshot")
	}
	if again := e.permissionSnapshot(); again != snap {
		t.Fatal("second permissionSnapshot() returned a different snapshot; want cached immutable value")
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
	if opts.ProjectDir != "" {
		t.Fatalf("ProjectDir = %q, want empty (executor is shared across sessions; workspace is per-request)", opts.ProjectDir)
	}
}

// TestExecutorPermissionSnapshotPublishesNilOnLoadError pins the
// dual-eval-phase skip-over-noise policy: a loader error publishes a NIL
// snapshot (dual-eval silently skips for the process) and the failure is
// logged exactly once at build time. The engine-level DenyAllSnapshot+err
// contract is covered by corelib/permission's tests.
func TestExecutorPermissionSnapshotPublishesNilOnLoadError(t *testing.T) {
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.DenyAllSnapshot("rule source unreadable: test"), os.ErrPermission
	})
	var buf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	e := &CoreAgentExecutor{}
	if snap := e.permissionSnapshot(); snap != nil {
		t.Fatalf("permissionSnapshot() = %v on load error, want nil (dual-eval skips)", snap)
	}
	if snap := e.permissionSnapshot(); snap != nil {
		t.Fatalf("second permissionSnapshot() = %v on load error, want nil", snap)
	}
	if n := strings.Count(buf.String(), "dual-eval permission snapshot load failed"); n != 1 {
		t.Fatalf("load failure logged %d times, want exactly 1 (logged once at build time): %q", n, buf.String())
	}
}

func TestCoreAgentCallbacksPermissionSnapshotDelegatesToExecutor(t *testing.T) {
	t.Setenv(permissionDualEvalEnvKey, "")
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{})
	})
	e := &CoreAgentExecutor{}
	cb := &coreAgentCallbacks{executor: e}
	if snap := cb.PermissionSnapshot(); snap == nil {
		t.Fatal("PermissionSnapshot() = nil, want non-nil snapshot via executor holder")
	}
}
