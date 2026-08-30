package main

import (
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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
