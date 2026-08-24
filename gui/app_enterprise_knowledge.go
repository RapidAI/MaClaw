package main

// Enterprise digital assets: thin GUI adapter over corelib/enterpriseknowledge.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/enterpriseknowledge"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

var enterpriseKnowledgeMu sync.Mutex

// EnterpriseLibraryView is exposed to Wails UI.
type EnterpriseLibraryView struct {
	LibraryID       string `json:"library_id"`
	Name            string `json:"name"`
	LastRev         int64  `json:"last_rev"`
	AccessState     string `json:"access_state"`
	ACLFingerprint  string `json:"acl_fingerprint"`
	LastSyncAt      string `json:"last_sync_at"`
	LastError       string `json:"last_error"`
	UserSyncEnabled bool   `json:"user_sync_enabled"`
	HubSyncEnabled  bool   `json:"hub_sync_enabled"`
}

// EnterpriseSyncStatus is UI status for the sync agent.
type EnterpriseSyncStatus struct {
	Running      bool   `json:"running"`
	Paused       bool   `json:"paused"`
	LastRunAt    string `json:"last_run_at"`
	LastError    string `json:"last_error"`
	LastOutcome  string `json:"last_outcome"`
	LibraryCount int    `json:"library_count"`
}

func (a *App) enterpriseDataDir() string {
	if a == nil {
		return ""
	}
	return a.GetDataDir()
}

// ensureEnterpriseClient opens (or returns) the long-lived enterprise client for this App.
func (a *App) ensureEnterpriseClient() *enterpriseknowledge.Client {
	if a == nil {
		return nil
	}
	enterpriseKnowledgeMu.Lock()
	defer enterpriseKnowledgeMu.Unlock()
	if a.enterpriseClient != nil {
		return a.enterpriseClient
	}
	c, err := enterpriseknowledge.Open(a.enterpriseDataDir())
	if err != nil {
		log.Printf("[enterprise-knowledge] open failed: %v", err)
		return nil
	}
	a.enterpriseClient = c
	// Keep legacy pointer for any code that still reads the store field.
	a.enterpriseKnowledgeStore = c.Store()
	log.Printf("[enterprise-knowledge] client opened at %s", enterpriseknowledge.KnowledgeDBPath(a.enterpriseDataDir()))
	return a.enterpriseClient
}

// ensureEnterpriseKnowledgeStore returns the knowledge SQLite store (Wails/legacy).
func (a *App) ensureEnterpriseKnowledgeStore() *knowledge.SQLiteStore {
	c := a.ensureEnterpriseClient()
	if c == nil {
		return nil
	}
	store, err := c.EnsureStore()
	if err != nil {
		log.Printf("[enterprise-knowledge] ensure store: %v", err)
		return nil
	}
	enterpriseKnowledgeMu.Lock()
	a.enterpriseKnowledgeStore = store
	enterpriseKnowledgeMu.Unlock()
	return store
}

// EnterpriseKnowledgeListLibraries returns local library state (active + sync_disabled visible).
func (a *App) EnterpriseKnowledgeListLibraries() ([]EnterpriseLibraryView, error) {
	c := a.ensureEnterpriseClient()
	if c == nil {
		return []EnterpriseLibraryView{}, fmt.Errorf("enterprise knowledge unavailable")
	}
	libs, err := c.ListLibraries()
	if err != nil {
		return []EnterpriseLibraryView{}, err
	}
	out := make([]EnterpriseLibraryView, 0, len(libs))
	for _, lib := range libs {
		out = append(out, EnterpriseLibraryView{
			LibraryID:       lib.LibraryID,
			Name:            lib.Name,
			LastRev:         lib.LastRev,
			AccessState:     lib.AccessState,
			ACLFingerprint:  lib.ACLFingerprint,
			LastSyncAt:      lib.LastSyncAt,
			LastError:       lib.LastError,
			UserSyncEnabled: lib.UserSyncEnabled,
			HubSyncEnabled:  lib.HubSyncEnabled,
		})
	}
	return out, nil
}

// EnterpriseKnowledgeSetLibraryUserSync enables or disables Hub→local pull for one library.
func (a *App) EnterpriseKnowledgeSetLibraryUserSync(libraryID string, enabled bool) error {
	c := a.ensureEnterpriseClient()
	if c == nil {
		return fmt.Errorf("enterprise knowledge unavailable")
	}
	return c.SetUserSync(libraryID, enabled)
}

// EnterpriseKnowledgeSearch searches only access_state=active libraries.
func (a *App) EnterpriseKnowledgeSearch(q, libraryID string) ([]knowledge.SearchResult, error) {
	c := a.ensureEnterpriseClient()
	if c == nil {
		return nil, fmt.Errorf("enterprise knowledge store not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return c.SearchActive(ctx, q, libraryID)
}

// EnterpriseSyncStatus returns agent status.
func (a *App) EnterpriseSyncStatus() (EnterpriseSyncStatus, error) {
	st := EnterpriseSyncStatus{}
	if a.enterpriseSync != nil {
		s := a.enterpriseSync.Status()
		st.Running = s.Running
		st.Paused = s.Paused
		st.LastRunAt = s.LastRunAt
		st.LastError = s.LastError
		st.LastOutcome = s.LastOutcome
		st.LibraryCount = s.LibraryCount
	} else {
		libs, _ := a.EnterpriseKnowledgeListLibraries()
		st.LibraryCount = len(libs)
	}
	return st, nil
}

// EnterpriseSetSyncPaused pauses/resumes local scheduler only.
func (a *App) EnterpriseSetSyncPaused(paused bool) error {
	a.ensureEnterpriseSyncAgent()
	if a.enterpriseSync != nil {
		a.enterpriseSync.SetPaused(paused)
	}
	return nil
}

// EnterpriseSyncNow triggers one sync cycle.
func (a *App) EnterpriseSyncNow() (EnterpriseSyncStatus, error) {
	ag := a.ensureEnterpriseSyncAgent()
	var err error
	if ag != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err = ag.RunOnce(ctx)
		cancel()
		// Concurrent background tick is not a hard failure for the UI.
		if errors.Is(err, enterpriseknowledge.ErrSyncInProgress) {
			err = nil
		}
	}
	st, _ := a.EnterpriseSyncStatus()
	return st, err
}

// EnterprisePurgeRevokedLibrary deletes local sources for a library.
// Refuses while a Hub sync cycle is running (same client would race writers).
// Pauses the background agent for the duration so a new cycle cannot start mid-purge.
func (a *App) EnterprisePurgeRevokedLibrary(libraryID string) error {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library_id required")
	}
	enterpriseKnowledgeMu.Lock()
	defer enterpriseKnowledgeMu.Unlock()
	if a.enterpriseSync != nil {
		st := a.enterpriseSync.Status()
		if st.Running {
			return fmt.Errorf("%w: wait for sync to finish or pause sync", enterpriseknowledge.ErrSyncInProgress)
		}
		// Pause background loop for the purge; always restore prior pause flag
		// (do not force-unpause if the user already paused sync).
		wasPaused := st.Paused
		a.enterpriseSync.SetPaused(true)
		defer a.enterpriseSync.SetPaused(wasPaused)
		// Re-check after pause: a cycle may have started between Status and SetPaused.
		if a.enterpriseSync.Status().Running {
			return fmt.Errorf("%w: wait for sync to finish or pause sync", enterpriseknowledge.ErrSyncInProgress)
		}
	}
	c := a.enterpriseClient
	if c == nil {
		opened, err := enterpriseknowledge.Open(a.enterpriseDataDir())
		if err != nil {
			return fmt.Errorf("store unavailable: %w", err)
		}
		a.enterpriseClient = opened
		a.enterpriseKnowledgeStore = opened.Store()
		c = opened
	}
	if err := c.PurgeLibrary(libraryID); err != nil {
		return err
	}
	// Drop pooled meta/search leases so auto-recall does not keep a pre-purge handle.
	enterpriseknowledge.InvalidateCache(a.enterpriseDataDir())
	return nil
}

func (a *App) ensureEnterpriseSyncAgent() *enterpriseknowledge.SyncAgent {
	if a == nil {
		return nil
	}
	enterpriseKnowledgeMu.Lock()
	defer enterpriseKnowledgeMu.Unlock()
	if a.enterpriseSync != nil {
		return a.enterpriseSync
	}
	c := a.enterpriseClient
	if c == nil {
		// open without re-taking lock (already held)
		opened, err := enterpriseknowledge.Open(a.enterpriseDataDir())
		if err != nil {
			log.Printf("[enterprise-knowledge] open for sync: %v", err)
			return nil
		}
		a.enterpriseClient = opened
		a.enterpriseKnowledgeStore = opened.Store()
		c = opened
	}
	host, _ := os.Hostname()
	app := a
	ag := enterpriseknowledge.NewSyncAgent(c, func() (string, string, error) {
		return app.resolveKnowledgeHubAuth("", "")
	}, "gui-"+host)
	if ag == nil {
		log.Printf("[enterprise-knowledge] sync agent unavailable (client not open)")
		return nil
	}
	a.enterpriseSync = ag
	a.enterpriseSync.StartBackground()
	return a.enterpriseSync
}

// StartEnterpriseDigitalAssetSync starts the background sync agent.
func (a *App) StartEnterpriseDigitalAssetSync() {
	a.ensureEnterpriseSyncAgent()
}
