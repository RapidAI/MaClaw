package guiapp

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// syncBuffer is a goroutine-safe log sink for counting emitted lines.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestPermissionSnapshotKillSwitchReturnsNil(t *testing.T) {
	for _, v := range []string{"off", "0", "OFF", " false "} {
		t.Run("value="+v, func(t *testing.T) {
			t.Setenv(permissionDualEvalEnvKey, v)
			if permissionDualEvalDisabled() != true {
				t.Fatalf("permissionDualEvalDisabled() = false, want true for %q", v)
			}
			app := &App{}
			if snap := app.permissionSnapshot(); snap != nil {
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

func TestPermissionSnapshotNilApp(t *testing.T) {
	var app *App
	if snap := app.permissionSnapshot(); snap != nil {
		t.Fatalf("nil App permissionSnapshot() = %v, want nil", snap)
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

func TestSharedAgentLoopCallbacksPermissionSnapshotNilSafe(t *testing.T) {
	var nilCallbacks *sharedAgentLoopCallbacks
	if snap := nilCallbacks.PermissionSnapshot(); snap != nil {
		t.Fatalf("nil callbacks PermissionSnapshot() = %v, want nil", snap)
	}
	if snap := (&sharedAgentLoopCallbacks{}).PermissionSnapshot(); snap != nil {
		t.Fatalf("nil-handler callbacks PermissionSnapshot() = %v, want nil", snap)
	}
	if snap := (&sharedAgentLoopCallbacks{handler: &IMMessageHandler{}}).PermissionSnapshot(); snap != nil {
		t.Fatalf("nil-app callbacks PermissionSnapshot() = %v, want nil", snap)
	}
}

func TestAppPermissionSnapshotBuildsOnceAndPublishes(t *testing.T) {
	tmpHome := t.TempDir()
	var mu sync.Mutex
	var gotOpts []permission.Options
	swapPermissionLoadFunc(t, func(opts permission.Options) (*permission.Snapshot, error) {
		mu.Lock()
		gotOpts = append(gotOpts, opts)
		mu.Unlock()
		return permission.Load(permission.Options{})
	})
	app := &App{testHomeDir: tmpHome}
	snap := app.permissionSnapshot()
	if snap == nil {
		t.Fatal("permissionSnapshot() = nil, want non-nil snapshot")
	}
	if again := app.permissionSnapshot(); again != snap {
		t.Fatal("second permissionSnapshot() returned a different snapshot; want cached immutable value")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gotOpts) != 1 {
		t.Fatalf("loader called %d times, want 1 (sync.Once)", len(gotOpts))
	}
	opts := gotOpts[0]
	wantPath := filepath.Join(tmpHome, ".maclaw", "config.json")
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

// TestAppPermissionSnapshotPublishesNilOnLoadError pins the dual-eval-phase
// skip-over-noise policy: a loader error publishes a NIL snapshot (dual-eval
// silently skips for the process) and the failure is logged exactly once at
// build time, not re-logged per call. The engine-level contract — Load still
// returns DenyAllSnapshot+err — is covered by corelib/permission's
// TestLoadMalformedJSONFailsClosed.
func TestAppPermissionSnapshotPublishesNilOnLoadError(t *testing.T) {
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.DenyAllSnapshot("rule source unreadable: test"), os.ErrPermission
	})
	var buf syncBuffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	app := &App{}
	if snap := app.permissionSnapshot(); snap != nil {
		t.Fatalf("permissionSnapshot() = %v on load error, want nil (dual-eval skips)", snap)
	}
	// Second call must stay nil and must not re-log (once-guarded build).
	if snap := app.permissionSnapshot(); snap != nil {
		t.Fatalf("second permissionSnapshot() = %v on load error, want nil", snap)
	}
	if n := strings.Count(buf.String(), "dual-eval snapshot load failed"); n != 1 {
		t.Fatalf("load failure logged %d times, want exactly 1 (logged once at build time): %q", n, buf.String())
	}
}

func TestSharedAgentLoopCallbacksPermissionSnapshotDelegatesToApp(t *testing.T) {
	t.Setenv(permissionDualEvalEnvKey, "")
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{})
	})
	app := &App{testHomeDir: t.TempDir()}
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{app: app}}
	if snap := cb.PermissionSnapshot(); snap == nil {
		t.Fatal("PermissionSnapshot() = nil, want non-nil snapshot via App holder")
	}
}
