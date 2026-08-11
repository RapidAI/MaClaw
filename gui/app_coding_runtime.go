package main

import (
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// ensureCodingRuntimeStore opens the app-level execution ledger once. Keeping
// it application-scoped gives startup a chance to mark abandoned leases as
// interrupted instead of silently losing their attempt history.
func (a *App) ensureCodingRuntimeStore() *codingruntime.SQLiteStore {
	if a == nil {
		return nil
	}
	a.codingRuntimeStoreMu.Lock()
	defer a.codingRuntimeStoreMu.Unlock()
	if a.codingRuntimeStore != nil {
		return a.codingRuntimeStore
	}
	dir := a.GetDataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[coding-runtime] create data directory failed: %v", err)
		return nil
	}
	store, err := codingruntime.NewSQLiteStore(filepath.Join(dir, "coding_runtime.db"))
	if err != nil {
		log.Printf("[coding-runtime] open ledger failed: %v", err)
		return nil
	}
	if expired, err := store.ExpireLeases(time.Now().UTC()); err != nil {
		log.Printf("[coding-runtime] expire stale leases failed: %v", err)
	} else if len(expired) > 0 {
		log.Printf("[coding-runtime] marked %d stale attempt(s) interrupted; recovery requires read-only probe", len(expired))
	}
	if interrupted, err := store.InterruptUnstartedChildren(time.Now().UTC()); err != nil {
		log.Printf("[coding-runtime] reconcile unstarted child tasks failed: %v", err)
	} else if len(interrupted) > 0 {
		log.Printf("[coding-runtime] marked %d parent attempt(s) interrupted because child dispatch cannot survive restart", len(interrupted))
	}
	a.codingRuntimeStore = store
	return store
}

func (a *App) closeCodingRuntimeStore() {
	if a == nil {
		return
	}
	a.codingRuntimeStoreMu.Lock()
	store := a.codingRuntimeStore
	a.codingRuntimeStore = nil
	a.codingRuntimeStoreMu.Unlock()
	if store != nil {
		_ = store.Close()
	}
}
