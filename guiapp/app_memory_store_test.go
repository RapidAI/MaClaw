package guiapp

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestEnsureMemoryStoreConcurrentCallsShareOneStore(t *testing.T) {
	// ensureMemoryStore → applyDataDirFromConfigLocked pushes this test's
	// TempDir into the process-global corelib base dir; restore it so later
	// tests never resolve paths under a deleted TempDir.
	prevBaseDir := corelib.MaclawBaseDir()
	t.Cleanup(func() {
		corelib.SetMaclawBaseDir(prevBaseDir)
	})
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})

	const callers = 8
	stores := make(chan any, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			app.ensureMemoryStore()
			app.memoryStoreMu.Lock()
			store := app.memoryStore
			app.memoryStoreMu.Unlock()
			stores <- store
		}()
	}
	wg.Wait()
	close(stores)

	var first any
	for store := range stores {
		if store == nil {
			t.Fatal("memoryStore is nil")
		}
		if first == nil {
			first = store
			continue
		}
		if store != first {
			t.Fatal("concurrent ensureMemoryStore calls produced different stores")
		}
	}
}

func TestMemoryRuntimeSwitchWaitsForActiveTurnAndRewiresHandler(t *testing.T) {
	oldStore, err := memory.NewStore(filepath.Join(t.TempDir(), "old.json"))
	if err != nil {
		t.Fatal(err)
	}
	newStore, err := memory.NewStore(filepath.Join(t.TempDir(), "new.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(newStore.Stop)

	app := &App{memoryStore: oldStore, testHomeDir: t.TempDir()}
	oldRuntime := app.installMemoryRuntime(oldStore, nil, "old")
	handler := &IMMessageHandler{app: app, memoryStore: oldStore}

	releaseOld := handler.acquireMemoryRuntimeForTurn()
	if handler.currentMemoryStore() != oldStore {
		t.Fatal("initial turn did not acquire the old runtime")
	}

	app.resetPathBoundStateForDataDirChange()
	if app.memRuntime.Load() != nil || app.memoryStore != nil {
		t.Fatal("data-directory reset retained the old app runtime")
	}
	app.memoryStoreMu.Lock()
	app.memoryStore = newStore
	app.memoryStoreMu.Unlock()
	newRuntime := app.installMemoryRuntime(newStore, nil, "new")

	acquired := make(chan func(), 1)
	go func() {
		acquired <- handler.acquireMemoryRuntimeForTurn()
	}()
	select {
	case release := <-acquired:
		release()
		t.Fatal("new-runtime turn started while the old-runtime turn was active")
	case <-time.After(50 * time.Millisecond):
	}

	releaseOld()
	var releaseNew func()
	select {
	case releaseNew = <-acquired:
	case <-time.After(3 * time.Second):
		t.Fatal("new-runtime turn remained blocked after the old turn ended")
	}
	defer releaseNew()
	if handler.currentMemoryStore() != newStore {
		t.Fatal("handler retained the old store after the runtime switch")
	}
	if got := handler.memoryRuntimeBinding.runtime; got != newRuntime {
		t.Fatalf("handler runtime = %p, want %p", got, newRuntime)
	}

	stopped := make(chan struct{})
	go func() {
		oldRuntime.waitStopped()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("old store was not stopped after its last active turn ended")
	}
}

func TestPrivateMemoryStoreDoesNotFollowAppRuntime(t *testing.T) {
	shared, err := memory.NewStore(filepath.Join(t.TempDir(), "shared.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Stop()
	private, err := memory.NewStore(filepath.Join(t.TempDir(), "private.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer private.Stop()

	app := &App{memoryStore: shared, testHomeDir: t.TempDir()}
	app.installMemoryRuntime(shared, nil, "shared")
	handler := &IMMessageHandler{app: app}
	handler.SetPrivateMemoryStore(private)
	release := handler.acquireMemoryRuntimeForTurn()
	release()
	if handler.currentMemoryStore() != private {
		t.Fatal("private memory store was replaced by the app runtime")
	}
}

func TestUnavailableRuntimeDoesNotReuseRetiredStore(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "retired.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}
	runtime := app.installMemoryRuntime(store, nil, "old")
	handler := &IMMessageHandler{app: app, memoryStore: store}
	release := handler.acquireMemoryRuntimeForTurn()
	release()
	runtime.stopWhenIdle()
	runtime.waitStopped()
	app.memoryStore = nil

	releaseUnavailable := handler.acquireMemoryRuntimeForTurn()
	releaseUnavailable()
	if handler.currentMemoryStore() != nil || handler.memoryRuntimeBinding.runtime != nil {
		t.Fatal("unavailable runtime reused the retired store")
	}
}
