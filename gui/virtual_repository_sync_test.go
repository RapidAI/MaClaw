package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestApplyVirtualRepositorySyncKeepsLocalDefinitionsUnbound(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	pkg := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{
		repo.ID: {Repository: repo, Location: "local"},
	}}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Unbound || items[0].RootPath != "" || items[0].Definition == nil || items[0].Definition.ID != repo.ID {
		t.Fatalf("synced index item = %#v", items)
	}
	if _, err := os.Stat(filepath.Join(app.getMaclawBaseDir(), "virtual-repository-sync", repo.ID)); !os.IsNotExist(err) {
		t.Fatalf("sync must not create an implicit local root, stat error = %v", err)
	}

	listedRaw, err := app.ListVirtualRepositories()
	if err != nil {
		t.Fatal(err)
	}
	var listed []map[string]any
	if err := json.Unmarshal([]byte(listedRaw), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0]["unbound"] != true || listed[0]["error_code"] != "location_unavailable" {
		t.Fatalf("listed synchronized repository = %#v", listed)
	}
}

func TestApplyVirtualRepositorySyncRepositoryTombstoneWinsOverStaleDefinition(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_deleted", Name: "Deleted workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	pkg := virtualRepositorySyncPackage{
		Version:      virtualRepositorySyncVersion,
		Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}},
		Tombstones:   map[string]time.Time{"repo:" + repo.ID: time.Now().UTC()},
	}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("repository tombstone was overridden by a stale definition: %#v", items)
	}
}

func TestVirtualRepositoryIndexRejectsInvalidPortableDefinition(t *testing.T) {
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{{
		ID: "vrepo_synced", Name: "Synced workspace", Unbound: true,
		Definition: &VirtualRepository{Version: 1, ID: "vrepo_other", Name: "Synced workspace"},
	}}}
	if err := validateVirtualRepositoryIndex(&index); err == nil {
		t.Fatal("mismatched portable definition was accepted")
	}
}

func TestVirtualRepositorySyncRejectsRepositoryMapKeyMismatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_embedded", Name: "Synced workspace"}
	pkg := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{
		"vrepo_map_key": {Repository: repo, Location: "local"},
	}}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err == nil {
		t.Fatal("sync package with mismatched repository map key was accepted")
	}
	if _, conflicts := mergeVirtualRepositorySyncPackages(pkg, virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion}, nil, nil); len(conflicts) != 1 {
		t.Fatalf("merge conflicts = %#v, want one invalid-package conflict", conflicts)
	}
}

func TestVirtualRepositorySyncRejectsMalformedSensitivePackageBeforeApply(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	now := time.Now().UTC()
	validCredential := virtualRepositorySyncCred{Metadata: RepositoryCredentialMetadata{
		ID: "credential-1", Name: "Git", Kind: "git", Username: "alice", CreatedAt: now, UpdatedAt: now,
	}}
	for name, pkg := range map[string]virtualRepositorySyncPackage{
		"credential key mismatch": {Version: virtualRepositorySyncVersion, Credentials: map[string]virtualRepositorySyncCred{"other-id": validCredential}},
		"credential secret contains newline": {Version: virtualRepositorySyncVersion, Credentials: map[string]virtualRepositorySyncCred{"credential-1": {
			Metadata: validCredential.Metadata, Secret: "secret\nvalue",
		}}},
		"malformed binding":                     {Version: virtualRepositorySyncVersion, Bindings: map[string]string{"repository-only": "credential-1"}},
		"binding references missing credential": {Version: virtualRepositorySyncVersion, Bindings: map[string]string{"repo-1:node-1": "credential-missing"}},
		"malformed SSH secret":                  {Version: virtualRepositorySyncVersion, SSHSecrets: map[string]string{"repo-1": strings.Repeat("x", virtualRepositoryFieldMaxLength+1)}},
		"unknown tombstone kind":                {Version: virtualRepositorySyncVersion, Tombstones: map[string]time.Time{"unknown:item": now}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateVirtualRepositorySyncPackage(pkg); err == nil {
				t.Fatal("malformed sync package was accepted")
			}
			if err := app.applyVirtualRepositorySyncPackage(pkg); err == nil {
				t.Fatal("malformed sync package was applied")
			}
		})
	}
	if _, err := os.Stat(app.repositoryCredentialPath()); !os.IsNotExist(err) {
		t.Fatalf("malformed package wrote a credential file: %v", err)
	}
	if _, err := os.Stat(app.repositoryCredentialBindingsPath()); !os.IsNotExist(err) {
		t.Fatalf("malformed package wrote a binding file: %v", err)
	}
}

func TestApplyVirtualRepositorySyncRejectsCorruptLocalIndexBeforeWritingCredentials(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), virtualRepositoryIndex{Version: 2}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	pkg := virtualRepositorySyncPackage{
		Version: virtualRepositorySyncVersion,
		Credentials: map[string]virtualRepositorySyncCred{
			"credential-1": {Metadata: RepositoryCredentialMetadata{ID: "credential-1", Name: "Git", Kind: "git", Username: "alice", CreatedAt: now, UpdatedAt: now}},
		},
	}
	if err := app.applyVirtualRepositorySyncPackage(pkg); err == nil || !strings.Contains(err.Error(), "load local virtual repository index") {
		t.Fatalf("apply error = %v, want corrupt-index rejection before local writes", err)
	}
	if _, err := os.Stat(app.repositoryCredentialPath()); !os.IsNotExist(err) {
		t.Fatalf("corrupt local index still allowed a credential write: %v", err)
	}
}

func TestPortableDefinitionIsCanonicalizedBeforeSync(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{
		ID: "source", Name: "Source", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: `legacy\\source`, RemoteURL: "https://example.com/source.git", Enabled: true},
	}}}
	if err := app.applyVirtualRepositorySyncPackage(virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}}}); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Definition == nil {
		t.Fatalf("portable definition = %#v", items)
	}
	if got := items[0].Definition.Nodes[0].Repository.RelativePath; got != "Source" {
		t.Fatalf("portable mapping path = %q, want %q", got, "Source")
	}
	pkg, err := app.snapshotVirtualRepositorySyncPackage()
	if err != nil {
		t.Fatal(err)
	}
	if got := pkg.Repositories[repo.ID].Repository.Nodes[0].Repository.RelativePath; got != "Source" {
		t.Fatalf("snapshotted portable mapping path = %q, want %q", got, "Source")
	}
}

func TestBindVirtualRepositoryRootInitializesAnEmptySelectedDirectory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	if err := app.applyVirtualRepositorySyncPackage(virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	raw, err := app.BindVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"root_path":%q}`, repo.ID, root))
	if err != nil {
		t.Fatal(err)
	}
	var bound VirtualRepository
	if err := json.Unmarshal([]byte(raw), &bound); err != nil {
		t.Fatal(err)
	}
	if bound.ID != repo.ID || bound.RootPath != root {
		t.Fatalf("bound repository = %#v", bound)
	}
	if _, err := os.Stat(virtualRepositoryManifestPath(root)); err != nil {
		t.Fatalf("binding did not initialize manifest: %v", err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Unbound || items[0].RootPath != root {
		t.Fatalf("bound index item = %#v", items)
	}
}

func TestBindVirtualRepositoryRootAcceptsMatchingManifest(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	if err := app.applyVirtualRepositorySyncPackage(virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}}}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	existing := repo
	existing.RootPath = root
	if err := writeVirtualRepository(&existing); err != nil {
		t.Fatal(err)
	}
	if _, err := app.BindVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"root_path":%q}`, repo.ID, root)); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Unbound || items[0].RootPath != root {
		t.Fatalf("matching manifest was not bound: %#v", items)
	}
}

func TestBindVirtualRepositoryRootRejectsDifferentManifestAndNonEmptyDirectory(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	if err := app.applyVirtualRepositorySyncPackage(virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}}}); err != nil {
		t.Fatal(err)
	}
	differentRoot := t.TempDir()
	different := repo
	different.ID, different.RootPath = "vrepo_other", differentRoot
	if err := writeVirtualRepository(&different); err != nil {
		t.Fatal(err)
	}
	if _, err := app.BindVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"root_path":%q}`, repo.ID, differentRoot)); err == nil {
		t.Fatal("binding a different manifest succeeded")
	}
	nonEmptyRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonEmptyRoot, "notes.txt"), []byte("not empty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.BindVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"root_path":%q}`, repo.ID, nonEmptyRoot)); err == nil {
		t.Fatal("binding a non-empty directory succeeded")
	}
}

func TestBindVirtualRepositoryRootReconnectsUnavailableExistingRoot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	root := t.TempDir()
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Workspace", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	if err := writeVirtualRepository(&repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(&repo); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-offline"); err != nil {
		t.Fatal(err)
	}
	newRoot := root + "-offline"
	listedRaw, err := app.ListVirtualRepositories()
	if err != nil {
		t.Fatal(err)
	}
	var listed []map[string]any
	if err := json.Unmarshal([]byte(listedRaw), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0]["root_repair"] != true || listed[0]["error_code"] != "location_unavailable" {
		t.Fatalf("unavailable local root was not marked for repair: %#v", listed)
	}
	raw, err := app.BindVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"root_path":%q}`, repo.ID, newRoot))
	if err != nil {
		t.Fatal(err)
	}
	var bound VirtualRepository
	if err := json.Unmarshal([]byte(raw), &bound); err != nil {
		t.Fatal(err)
	}
	if bound.RootPath != newRoot {
		t.Fatalf("reconnected root = %q, want %q", bound.RootPath, newRoot)
	}
	if app.IsVirtualRepositoryBackgroundSyncPending() {
		t.Fatal("reconnecting an unavailable local root scheduled a portable-data sync")
	}
}

func TestBindVirtualRepositoryRootDoesNotInitializeUnavailableExistingRoot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	missingRoot := filepath.Join(t.TempDir(), "missing")
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Workspace", RootPath: missingRoot, Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{{ID: repo.ID, Name: repo.Name, RootPath: repo.RootPath}}}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), index); err != nil {
		t.Fatal(err)
	}
	emptyRoot := t.TempDir()
	if _, err := app.BindVirtualRepositoryRoot(fmt.Sprintf(`{"repository_id":%q,"root_path":%q}`, repo.ID, emptyRoot)); err == nil {
		t.Fatal("reconnecting a missing local root initialized an empty directory")
	}
}

func TestSnapshotVirtualRepositorySyncPackageRetainsUnboundDefinition(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Synced workspace", Nodes: []VirtualRepositoryNode{{ID: "source", Name: "Source", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "source", RemoteURL: "https://example.com/source.git", Enabled: true}}}}
	if err := app.applyVirtualRepositorySyncPackage(virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: repo, Location: "local"}}}); err != nil {
		t.Fatal(err)
	}
	pkg, err := app.snapshotVirtualRepositorySyncPackage()
	if err != nil {
		t.Fatal(err)
	}
	synced, exists := pkg.Repositories[repo.ID]
	if !exists || synced.Location != "local" || synced.Repository.RootPath != "" || len(synced.Repository.Nodes) != 1 || synced.Repository.Nodes[0].Repository == nil {
		t.Fatalf("unbound definition was not preserved in snapshot: %#v", synced)
	}
}

func TestApplyVirtualRepositorySyncPreservesBoundRoot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Local workspace", Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	root := t.TempDir()
	repo.RootPath = root
	if err := writeVirtualRepository(&repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(&repo); err != nil {
		t.Fatal(err)
	}
	cloud := repo
	cloud.Name, cloud.RootPath = "Cloud workspace", ""
	if err := app.applyVirtualRepositorySyncPackage(virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: cloud, Location: "local"}}}); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Unbound || items[0].RootPath != root || items[0].Name != repo.Name {
		t.Fatalf("bound local repository was replaced by portable sync data: %#v", items)
	}
}

func TestApplyVirtualRepositorySyncReplacesUnavailableBoundRootWithPortableDefinition(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	root := t.TempDir()
	repo := VirtualRepository{Version: 1, ID: "vrepo_synced", Name: "Local workspace", RootPath: root, Nodes: []VirtualRepositoryNode{{ID: "docs", Name: "Docs"}}}
	if err := writeVirtualRepository(&repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(&repo); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	cloud := repo
	cloud.Name, cloud.RootPath = "Cloud workspace", ""
	if err := app.applyVirtualRepositorySyncPackage(virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Repositories: map[string]virtualRepositorySyncRepo{repo.ID: {Repository: cloud, Location: "local"}}}); err != nil {
		t.Fatal(err)
	}
	items, err := app.loadVirtualRepositoryIndexItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Unbound || items[0].Definition == nil || items[0].Name != cloud.Name || items[0].RootPath != "" {
		t.Fatalf("unavailable bound root was not replaced with a portable definition: %#v", items)
	}
	pkg, err := app.snapshotVirtualRepositorySyncPackage()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := pkg.Repositories[repo.ID]; !exists {
		t.Fatal("portable replacement no longer snapshots for synchronization")
	}
}

func TestVirtualRepositoryBackgroundSyncPendingTracksQueuedAndCancelledRuns(t *testing.T) {
	app := &App{}
	app.scheduleVirtualRepositorySyncAfter(time.Hour, 0)
	if !app.IsVirtualRepositoryBackgroundSyncPending() {
		t.Fatal("scheduled sync should disable manual sync")
	}
	app.scheduleVirtualRepositorySyncAfter(time.Hour, 0)
	if !app.IsVirtualRepositoryBackgroundSyncPending() {
		t.Fatal("replaced scheduled sync should remain pending")
	}
	app.virtualRepositorySyncScheduleMu.Lock()
	app.cancelVirtualRepositorySyncTimerLocked()
	app.virtualRepositorySyncScheduleMu.Unlock()
	if app.IsVirtualRepositoryBackgroundSyncPending() {
		t.Fatal("cancelled scheduled sync should re-enable manual sync")
	}
}

func TestVirtualRepositoryBackgroundSyncIgnoresSupersededTimer(t *testing.T) {
	app := &App{}
	first := time.NewTimer(time.Hour)
	second := time.NewTimer(time.Hour)
	defer first.Stop()
	defer second.Stop()
	app.virtualRepositorySyncTimer = second
	// The first timer fired while a replacement was being scheduled. Both
	// scheduled jobs had claimed a pending slot; the stale callback must release
	// only its own slot and leave the replacement pending.
	app.virtualRepositoryBackgroundSyncs.Store(2)
	app.runScheduledVirtualRepositorySync(first, 0, true)
	if got := app.virtualRepositoryBackgroundSyncs.Load(); got != 1 {
		t.Fatalf("superseded timer left pending count at %d, want 1", got)
	}
	if app.virtualRepositorySyncTimer != second {
		t.Fatal("superseded timer cleared the active timer")
	}
}

func TestVirtualRepositoryBackgroundSyncRetryPreservesNewerScheduledSync(t *testing.T) {
	app := &App{}
	newer := time.NewTimer(time.Hour)
	defer newer.Stop()
	app.virtualRepositorySyncTimer = newer
	app.virtualRepositoryBackgroundSyncs.Store(1)

	app.scheduleVirtualRepositorySyncRetryAfter(time.Hour, 3, errors.New("hub offline"))

	if app.virtualRepositorySyncTimer != newer {
		t.Fatal("failed retry replaced the newer sync scheduled by a local edit")
	}
	if got := app.virtualRepositoryBackgroundSyncs.Load(); got != 1 {
		t.Fatalf("failed retry changed pending count to %d, want 1", got)
	}
}

func TestVirtualRepositoryBackgroundSyncRetryWaitDoesNotBlockManualSync(t *testing.T) {
	app := &App{}
	app.scheduleVirtualRepositorySyncRetryAfter(time.Hour, 1, errors.New("read virtual repository \"vrepo-test\" for sync: connect SSH"))
	if app.IsVirtualRepositoryBackgroundSyncPending() {
		t.Fatal("retry wait must not disable manual sync")
	}
	status := app.virtualRepositoryBackgroundSyncStatusSnapshot()
	if status.Phase != virtualRepositorySyncPhaseRetryWait {
		t.Fatalf("phase = %q, want %q", status.Phase, virtualRepositorySyncPhaseRetryWait)
	}
	if !strings.Contains(status.Message, "vrepo-test") {
		t.Fatalf("status message = %q, want remote failure details", status.Message)
	}
	if status.NextRetryAt == "" {
		t.Fatal("retry wait should advertise next_retry_at")
	}
}

func TestVirtualRepositoryBackgroundSyncStopsAfterMaxAttempts(t *testing.T) {
	app := &App{}
	app.scheduleVirtualRepositorySyncRetryAfter(time.Hour, virtualRepositorySyncMaxAutoAttempts, errors.New("hub returned 503"))
	if app.IsVirtualRepositoryBackgroundSyncPending() {
		t.Fatal("permanent failure must not leave pending true")
	}
	if app.virtualRepositorySyncTimer != nil {
		t.Fatal("permanent failure must not schedule another timer")
	}
	status := app.virtualRepositoryBackgroundSyncStatusSnapshot()
	if status.Phase != virtualRepositorySyncPhaseFailed {
		t.Fatalf("phase = %q, want %q", status.Phase, virtualRepositorySyncPhaseFailed)
	}
	if !strings.Contains(status.Message, "503") {
		t.Fatalf("failed message = %q", status.Message)
	}
}

func TestVirtualRepositoryManualSyncCancelsRetryWait(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	// No Hub credentials: manual sync should fail quickly after cancelling the wait,
	// rather than returning busy.
	app.scheduleVirtualRepositorySyncRetryAfter(time.Hour, 1, errors.New("previous failure"))
	if app.virtualRepositorySyncTimer == nil {
		t.Fatal("expected retry timer")
	}
	_, err := app.SyncVirtualRepositories("")
	if err == nil {
		t.Fatal("expected hub configuration error after cancelling retry wait")
	}
	if app.virtualRepositorySyncTimer != nil {
		t.Fatal("manual sync should cancel the retry timer")
	}
	if app.IsVirtualRepositoryBackgroundSyncPending() {
		t.Fatal("manual sync left pending true")
	}
	if status := app.virtualRepositoryBackgroundSyncStatusSnapshot(); status.Phase != virtualRepositorySyncPhaseIdle {
		t.Fatalf("manual sync phase = %q, want idle after cancelling retry wait", status.Phase)
	}
}

func TestScheduledVirtualRepositorySyncDoesNotSelfBusy(t *testing.T) {
	// Regression: runScheduled claims the pending slot before entering the shared
	// path. That path must not treat its own slot as "busy" or automatic sync
	// spins forever on "synchronization is in progress".
	app := &App{testHomeDir: t.TempDir()}
	app.virtualRepositoryBackgroundSyncs.Store(1)
	app.virtualRepositorySyncPhase = virtualRepositorySyncPhaseRunning

	raw, err := app.syncScheduledVirtualRepositories()
	if err == nil {
		var result VirtualRepositorySyncResult
		if json.Unmarshal([]byte(raw), &result) != nil {
			t.Fatalf("decode scheduled sync result: %v", raw)
		}
		if result.Status == "busy" {
			t.Fatal("scheduled sync treated its own pending slot as busy")
		}
	}
	// Missing Hub credentials is the expected outcome; anything other than busy.
	if err != nil && strings.Contains(err.Error(), "in progress") {
		t.Fatalf("scheduled sync returned self-busy error: %v", err)
	}
}

func TestVirtualRepositoryBackgroundSyncSuccessDoesNotClobberQueuedEdit(t *testing.T) {
	// Contract of the success-path critical section: when a local edit queues a
	// timer while a run is in flight, finishing that run must not force phase=idle.
	app := &App{}
	queued := time.AfterFunc(time.Hour, func() {})
	defer queued.Stop()
	app.virtualRepositorySyncTimer = queued
	app.virtualRepositorySyncTimerBlocksUI = true
	app.virtualRepositoryBackgroundSyncs.Store(2) // active run + queued edit
	app.virtualRepositorySyncPhase = virtualRepositorySyncPhaseQueued

	app.virtualRepositorySyncScheduleMu.Lock()
	if app.virtualRepositorySyncTimer == nil {
		app.setVirtualRepositoryBackgroundSyncPhaseLocked(virtualRepositorySyncPhaseIdle, "", 0, time.Time{})
	}
	app.virtualRepositorySyncScheduleMu.Unlock()
	app.adjustVirtualRepositoryBackgroundSyncPending(-1)

	if app.virtualRepositorySyncTimer != queued {
		t.Fatal("success path cleared the queued edit timer")
	}
	if app.virtualRepositorySyncPhase != virtualRepositorySyncPhaseQueued {
		t.Fatalf("phase = %q, want queued preserved over success", app.virtualRepositorySyncPhase)
	}
	if got := app.virtualRepositoryBackgroundSyncs.Load(); got != 1 {
		t.Fatalf("pending after run release = %d, want 1 for queued edit", got)
	}
}

func TestVirtualRepositoryBackgroundSyncPermanentConfigErrorSkipsBackoff(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.virtualRepositoryBackgroundSyncs.Store(1)
	app.virtualRepositorySyncPhase = virtualRepositorySyncPhaseRunning
	// No Hub credentials → permanent error on first automatic attempt.
	timer := time.NewTimer(time.Hour)
	// Use the real run entry with a timer identity that matches.
	app.virtualRepositorySyncTimer = timer
	app.virtualRepositorySyncTimerBlocksUI = true
	app.runScheduledVirtualRepositorySync(timer, 0, true)

	if app.IsVirtualRepositoryBackgroundSyncPending() {
		t.Fatal("permanent config failure left pending true")
	}
	if app.virtualRepositorySyncTimer != nil {
		app.virtualRepositorySyncTimer.Stop()
		t.Fatal("permanent config failure scheduled a retry timer")
	}
	status := app.virtualRepositoryBackgroundSyncStatusSnapshot()
	if status.Phase != virtualRepositorySyncPhaseFailed {
		t.Fatalf("phase = %q, want failed", status.Phase)
	}
	if !strings.Contains(strings.ToLower(status.Message), "hub") {
		t.Fatalf("failed message = %q, want hub configuration detail", status.Message)
	}
}

func TestIsVirtualRepositorySyncPermanentError(t *testing.T) {
	if !isVirtualRepositorySyncPermanentError(errors.New("Hub URL not configured")) {
		t.Fatal("expected hub url error to be permanent")
	}
	if !isVirtualRepositorySyncPermanentError(errors.New("machine id missing; register to Hub first")) {
		t.Fatal("expected machine id error to be permanent")
	}
	if isVirtualRepositorySyncPermanentError(errors.New("hub returned 503")) {
		t.Fatal("transient hub HTTP error must remain retriable")
	}
	if isVirtualRepositorySyncPermanentError(errors.New("connect SSH example.com:22")) {
		t.Fatal("SSH connectivity must remain retriable")
	}
}

func TestVirtualRepositoryBackgroundSyncStatusSnapshotCoherentWithPhase(t *testing.T) {
	app := &App{}
	// Counter still "running" while phase already advanced to retry_wait (or the
	// reverse): public snapshot must not advertise a contradictory pair.
	app.virtualRepositoryBackgroundSyncs.Store(1)
	app.virtualRepositorySyncPhase = virtualRepositorySyncPhaseRetryWait
	app.virtualRepositorySyncStatusMessage = "hub offline"
	status := app.virtualRepositoryBackgroundSyncStatusSnapshot()
	if status.Pending || status.Phase != virtualRepositorySyncPhaseRetryWait {
		t.Fatalf("retry_wait snapshot = %+v, want pending=false", status)
	}

	app.virtualRepositoryBackgroundSyncs.Store(0)
	app.virtualRepositorySyncPhase = virtualRepositorySyncPhaseRunning
	status = app.virtualRepositoryBackgroundSyncStatusSnapshot()
	if !status.Pending || status.Phase != virtualRepositorySyncPhaseRunning {
		t.Fatalf("running snapshot = %+v, want pending=true", status)
	}
}

func TestVirtualRepositoryBackgroundSyncPendingDoesNotUnderflow(t *testing.T) {
	app := &App{}
	app.setVirtualRepositoryBackgroundSyncPending(-1)
	if got := app.virtualRepositoryBackgroundSyncs.Load(); got != 0 {
		t.Fatalf("pending count underflowed to %d, want 0", got)
	}
}

func TestVirtualRepositoryManualSyncDefersToQueuedBackgroundSync(t *testing.T) {
	app := &App{}
	// Simulate an already-running automatic job (pending without a cancellable
	// timer). Manual sync must still return busy rather than stacking work.
	app.virtualRepositoryBackgroundSyncs.Store(1)
	app.virtualRepositorySyncPhase = virtualRepositorySyncPhaseRunning

	raw, err := app.SyncVirtualRepositories("")
	if err != nil {
		t.Fatalf("manual sync returned an error: %v", err)
	}
	var result VirtualRepositorySyncResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode manual sync result: %v", err)
	}
	if result.Status != "busy" {
		t.Fatalf("manual sync status = %q, want busy", result.Status)
	}
}

func TestSnapshotVirtualRepositorySyncPackageUsesCachedRemoteDefinition(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	remote := &VirtualRepositoryRemote{Host: "unreachable.example", User: "deploy", Port: 22}
	repo := VirtualRepository{
		Version: 1, ID: "vrepo_remote", Name: "Remote workspace", RootPath: "/srv/workspace", Remote: remote,
		Nodes: []VirtualRepositoryNode{{ID: "src", Name: "src", Repository: &VirtualRepositoryBinding{Kind: "git", RelativePath: "src", RemoteURL: "https://example.com/src.git", Enabled: true}}},
	}
	app.rememberVirtualRepositoryDefinition(&repo)
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{{
		ID: repo.ID, Name: repo.Name, RootPath: repo.RootPath, Remote: cloneVirtualRepositoryRemote(remote), LastOpened: time.Now().UTC(),
	}}}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), index); err != nil {
		t.Fatal(err)
	}
	pkg, err := app.snapshotVirtualRepositorySyncPackage()
	if err != nil {
		t.Fatalf("snapshot with unreachable remote should use cache: %v", err)
	}
	synced, ok := pkg.Repositories[repo.ID]
	if !ok || synced.Location != "remote" || len(synced.Repository.Nodes) != 1 {
		t.Fatalf("cached remote package = %#v", pkg.Repositories)
	}
}

func TestVirtualRepositoryDefinitionCachePrunesRemovedRemotes(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	remote := &VirtualRepositoryRemote{Host: "example.com", User: "deploy", Port: 22}
	keep := VirtualRepository{Version: 1, ID: "vrepo_keep", Name: "Keep", RootPath: "/srv/keep", Remote: remote}
	drop := VirtualRepository{Version: 1, ID: "vrepo_drop", Name: "Drop", RootPath: "/srv/drop", Remote: remote}
	app.rememberVirtualRepositoryDefinition(&keep)
	app.rememberVirtualRepositoryDefinition(&drop)

	cache := virtualRepositoryDefinitionCache{Version: 1, Items: map[string]VirtualRepository{
		keep.ID: keep, drop.ID: drop,
	}}
	if !pruneVirtualRepositoryDefinitionCache(&cache, map[string]struct{}{keep.ID: {}}) {
		t.Fatal("expected prune to report a change")
	}
	if _, ok := cache.Items[drop.ID]; ok {
		t.Fatal("removed remote stayed in definition cache")
	}
	if _, ok := cache.Items[keep.ID]; !ok {
		t.Fatal("live remote was pruned")
	}
	if pruneVirtualRepositoryDefinitionCache(&cache, map[string]struct{}{keep.ID: {}}) {
		t.Fatal("second prune should be a no-op")
	}
}

func TestStoreVirtualRepositoryDefinitionCacheIgnoresLocalRepos(t *testing.T) {
	cache := virtualRepositoryDefinitionCache{Version: 1, Items: map[string]VirtualRepository{}}
	local := VirtualRepository{Version: 1, ID: "vrepo_local", Name: "Local", Nodes: nil}
	if storeVirtualRepositoryDefinitionInCache(&cache, &local) {
		t.Fatal("local repositories must not enter the remote definition cache")
	}
	if len(cache.Items) != 0 {
		t.Fatalf("cache items = %#v", cache.Items)
	}
}

func emptyVirtualRepositorySyncDoc() []byte {
	return []byte(`{"version":1,"repositories":{},"credentials":{},"bindings":{},"ssh_secrets":{}}`)
}

func configureVirtualRepositorySyncTestApp(t *testing.T, hubURL string) *App {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hubURL,
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "machine-test",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return app
}

func TestVirtualRepositorySyncRequestRejectsTruncatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "32")
		_, _ = w.Write([]byte(`{"version":1}`))
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, _, err := app.virtualRepositorySyncRequest(ctx, server.URL, "machine-token", "machine-test", http.MethodGet, nil)
	if err == nil || !strings.Contains(err.Error(), "read Hub response") {
		t.Fatalf("request error = %v, want response-read failure", err)
	}
}

func TestVirtualRepositorySyncRequestRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", virtualRepositorySyncResponseMaxSize+1)))
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, _, err := app.virtualRepositorySyncRequest(ctx, server.URL, "machine-token", "machine-test", http.MethodGet, nil)
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("request error = %v, want response-size failure", err)
	}
}

func TestSyncVirtualRepositoriesRetriesStaleRevisionThenSucceeds(t *testing.T) {
	var puts atomic.Int32
	cloud := []byte(`{"version":1,"repositories":{"repo-cloud":{"repository":{"version":1,"id":"repo-cloud","name":"Cloud"},"location":"local"}},"credentials":{},"bindings":{},"ssh_secrets":{}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/virtual-repositories/sync" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("X-Virtual-Repository-Sync-Revision", "rev-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(cloud)
		case http.MethodPut:
			n := puts.Add(1)
			if n == 1 {
				http.Error(w, `{"error":{"code":"VREPO_SYNC_CONFLICT"}}`, http.StatusConflict)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"has_document":true,"revision":"rev-2","updated_at":"2026-01-01T00:00:00Z","limit_bytes":2097152}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	raw, err := app.SyncVirtualRepositories("")
	if err != nil {
		t.Fatalf("SyncVirtualRepositories: %v", err)
	}
	var result VirtualRepositorySyncResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("status = %q message=%q, want success", result.Status, result.Message)
	}
	if got := puts.Load(); got != 2 {
		t.Fatalf("PUT count = %d, want 2 (one conflict then success)", got)
	}
}

func TestSyncVirtualRepositoriesConvergesStaleCheckpointWithoutPut(t *testing.T) {
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/virtual-repositories/sync" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("X-Virtual-Repository-Sync-Revision", "cloud-rev")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(emptyVirtualRepositorySyncDoc())
		case http.MethodPut:
			puts.Add(1)
			http.Error(w, "a converged sync must not write", http.StatusInternalServerError)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	if err := writeJSONFile(app.virtualRepositorySyncStatePath(), virtualRepositorySyncState{
		Version:       virtualRepositorySyncVersion,
		CloudRevision: "stale-rev",
		ItemHashes:    map[string]string{"repo:removed": "old"},
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := app.SyncVirtualRepositories("")
	if err != nil {
		t.Fatalf("SyncVirtualRepositories: %v", err)
	}
	var result VirtualRepositorySyncResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "success" || result.CloudRevision != "cloud-rev" {
		t.Fatalf("result = %+v, want successful no-op convergence", result)
	}
	if got := puts.Load(); got != 0 {
		t.Fatalf("PUT count = %d, want 0 for an already-converged document", got)
	}
	state, err := app.loadVirtualRepositorySyncState()
	if err != nil {
		t.Fatal(err)
	}
	if state.CloudRevision != "cloud-rev" || len(state.ItemHashes) != 0 || state.LastSyncedAt.IsZero() {
		t.Fatalf("checkpoint was not advanced after no-op convergence: %#v", state)
	}
	if _, err := os.Stat(app.repositoryCredentialPath()); !os.IsNotExist(err) {
		t.Fatalf("no-op convergence rewrote local credentials: %v", err)
	}
	if _, err := os.Stat(app.repositoryCredentialBindingsPath()); !os.IsNotExist(err) {
		t.Fatalf("no-op convergence rewrote local credential bindings: %v", err)
	}
	if _, err := os.Stat(app.virtualRepositoryStatePath("virtual-repositories-index.json")); !os.IsNotExist(err) {
		t.Fatalf("no-op convergence rewrote the local repository index: %v", err)
	}
}

func TestVirtualRepositorySyncPackagesEqualNormalizesOptionalEmptyMaps(t *testing.T) {
	withoutOptionalMaps := virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion}
	withEmptyMaps := virtualRepositorySyncPackage{
		Version:      virtualRepositorySyncVersion,
		Repositories: map[string]virtualRepositorySyncRepo{},
		Credentials:  map[string]virtualRepositorySyncCred{},
		Bindings:     map[string]string{},
		SSHSecrets:   map[string]string{},
		Tombstones:   map[string]time.Time{},
	}
	if !virtualRepositorySyncPackagesEqual(withoutOptionalMaps, withEmptyMaps) {
		t.Fatal("absent optional maps and empty maps must converge without a PUT")
	}
	withEmptyMaps.Tombstones["repo:repo-1"] = time.Now().UTC()
	if virtualRepositorySyncPackagesEqual(withoutOptionalMaps, withEmptyMaps) {
		t.Fatal("a real tombstone change was treated as equal")
	}
}

func TestAdvanceVirtualRepositorySyncCheckpointDoesNotMutateAcceptedPackage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	original := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	newer := original.Add(time.Minute)
	pkg := virtualRepositorySyncPackage{
		Version:    virtualRepositorySyncVersion,
		Tombstones: map[string]time.Time{"repo:accepted": original},
	}
	if err := writeJSONFile(app.virtualRepositorySyncStatePath(), virtualRepositorySyncState{
		Version:    virtualRepositorySyncVersion,
		ItemHashes: map[string]string{},
		Tombstones: map[string]time.Time{"repo:concurrent": newer},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := app.advanceVirtualRepositorySyncCheckpoint(pkg, "cloud-rev")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := pkg.Tombstones["repo:concurrent"]; exists {
		t.Fatalf("checkpoint reconciliation mutated the accepted package: %#v", pkg.Tombstones)
	}
	if got := state.Tombstones["repo:concurrent"]; !got.Equal(newer) {
		t.Fatalf("concurrent tombstone = %v, want %v", got, newer)
	}
	if _, exists := state.ItemHashes["tombstone:repo:concurrent"]; !exists {
		t.Fatalf("checkpoint hashes omitted concurrent tombstone: %#v", state.ItemHashes)
	}
}

func TestFinishVirtualRepositorySyncAdvancesGeneration(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	before := app.virtualRepositorySyncGeneration.Load()
	pkg := virtualRepositorySyncPackage{
		Version:      virtualRepositorySyncVersion,
		Repositories: map[string]virtualRepositorySyncRepo{},
		Credentials:  map[string]virtualRepositorySyncCred{},
		Bindings:     map[string]string{},
		SSHSecrets:   map[string]string{},
		Tombstones:   map[string]time.Time{},
	}
	if _, err := app.finishVirtualRepositorySync(pkg, "cloud-rev"); err != nil {
		t.Fatal(err)
	}
	if got := app.virtualRepositorySyncGeneration.Load(); got != before+1 {
		t.Fatalf("generation = %d, want %d after applying cloud state", got, before+1)
	}
}

func TestSyncVirtualRepositoriesRejectsCloudDocumentWithoutRevision(t *testing.T) {
	var puts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(emptyVirtualRepositorySyncDoc())
		case http.MethodPut:
			puts.Add(1)
			http.Error(w, "must not write without a cloud revision", http.StatusInternalServerError)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	if _, err := app.SyncVirtualRepositories(""); err == nil || !strings.Contains(err.Error(), "without a revision") {
		t.Fatalf("sync error = %v, want missing-revision error", err)
	}
	if got := puts.Load(); got != 0 {
		t.Fatalf("PUT count = %d, want no unsafe unconditional write", got)
	}
}

func TestSyncVirtualRepositoriesRejectsSuccessfulPutWithoutRevision(t *testing.T) {
	var puts atomic.Int32
	cloud := []byte(`{"version":1,"repositories":{"repo-cloud":{"repository":{"version":1,"id":"repo-cloud","name":"Cloud"},"location":"local"}},"credentials":{},"bindings":{},"ssh_secrets":{}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("X-Virtual-Repository-Sync-Revision", "cloud-rev")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(cloud)
		case http.MethodPut:
			puts.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"has_document":true}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	if _, err := app.SyncVirtualRepositories(""); err == nil || !strings.Contains(err.Error(), "without returning a revision") {
		t.Fatalf("sync error = %v, want missing PUT revision error", err)
	}
	if got := puts.Load(); got != 1 {
		t.Fatalf("PUT count = %d, want one attempted optimistic write", got)
	}
}

func TestSyncVirtualRepositoriesRejectsPutThatDidNotStoreDocument(t *testing.T) {
	cloud := []byte(`{"version":1,"repositories":{"repo-cloud":{"repository":{"version":1,"id":"repo-cloud","name":"Cloud"},"location":"local"}},"credentials":{},"bindings":{},"ssh_secrets":{}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("X-Virtual-Repository-Sync-Revision", "cloud-rev")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(cloud)
		case http.MethodPut:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"has_document":false,"revision":"unexpected"}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	if _, err := app.SyncVirtualRepositories(""); err == nil || !strings.Contains(err.Error(), "without confirming the stored document") {
		t.Fatalf("sync error = %v, want rejected non-persisted PUT response", err)
	}
	if _, err := os.Stat(app.virtualRepositoryStatePath("virtual-repositories-index.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected PUT response applied local repository state: %v", err)
	}
}

func TestSyncVirtualRepositoriesRejectsPutWithoutStoredDocumentConfirmation(t *testing.T) {
	cloud := []byte(`{"version":1,"repositories":{"repo-cloud":{"repository":{"version":1,"id":"repo-cloud","name":"Cloud"},"location":"local"}},"credentials":{},"bindings":{},"ssh_secrets":{}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("X-Virtual-Repository-Sync-Revision", "cloud-rev")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(cloud)
		case http.MethodPut:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"revision":"new-rev"}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	if _, err := app.SyncVirtualRepositories(""); err == nil || !strings.Contains(err.Error(), "without confirming the stored document") {
		t.Fatalf("sync error = %v, want missing stored-document acknowledgement", err)
	}
	if _, err := os.Stat(app.virtualRepositoryStatePath("virtual-repositories-index.json")); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed PUT response applied local repository state: %v", err)
	}
}

func TestIsVirtualRepositorySyncRevisionRace(t *testing.T) {
	if !isVirtualRepositorySyncRevisionRace(VirtualRepositorySyncResult{Status: "conflict", Reason: virtualRepositorySyncReasonRevisionRace}) {
		t.Fatal("structured revision_race should match")
	}
	if !isVirtualRepositorySyncRevisionRace(VirtualRepositorySyncResult{Status: "conflict"}) {
		t.Fatal("legacy empty-conflicts conflict should match")
	}
	if isVirtualRepositorySyncRevisionRace(VirtualRepositorySyncResult{Status: "conflict", Conflicts: []VirtualRepositorySyncConflict{{ID: "repo:x", Kind: "repository", Name: "x"}}}) {
		t.Fatal("item-level conflicts should not match")
	}
	if isVirtualRepositorySyncRevisionRace(VirtualRepositorySyncResult{Status: "success"}) {
		t.Fatal("success should not match")
	}
	if virtualRepositorySyncStaleBackoff(0) != 50*time.Millisecond || virtualRepositorySyncStaleBackoff(3) != 400*time.Millisecond {
		t.Fatalf("unexpected backoff schedule: 0=%s 3=%s", virtualRepositorySyncStaleBackoff(0), virtualRepositorySyncStaleBackoff(3))
	}
}

func TestSyncVirtualRepositoriesReportsRevisionRaceAfterBudget(t *testing.T) {
	var puts atomic.Int32
	cloud := []byte(`{"version":1,"repositories":{"repo-cloud":{"repository":{"version":1,"id":"repo-cloud","name":"Cloud"},"location":"local"}},"credentials":{},"bindings":{},"ssh_secrets":{}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/virtual-repositories/sync" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("X-Virtual-Repository-Sync-Revision", fmt.Sprintf("rev-%d", puts.Load()+1))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(cloud)
		case http.MethodPut:
			puts.Add(1)
			http.Error(w, `{"error":{"code":"VREPO_SYNC_CONFLICT"}}`, http.StatusConflict)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	app := configureVirtualRepositorySyncTestApp(t, server.URL)
	raw, err := app.SyncVirtualRepositories("")
	if err != nil {
		t.Fatalf("SyncVirtualRepositories: %v", err)
	}
	var result VirtualRepositorySyncResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "conflict" || result.Reason != virtualRepositorySyncReasonRevisionRace {
		t.Fatalf("result = %+v, want conflict/revision_race", result)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("revision race should not include item conflicts: %+v", result.Conflicts)
	}
	if got := puts.Load(); got != int32(virtualRepositorySyncMaxStaleAttempts) {
		t.Fatalf("PUT count = %d, want %d attempts", got, virtualRepositorySyncMaxStaleAttempts)
	}
}

func TestVirtualRepositorySyncTombstoneUpdatesDoNotLoseConcurrentDeletes(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	const count = 32
	var group sync.WaitGroup
	for i := 0; i < count; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			app.recordVirtualRepositorySyncTombstone("repo", fmt.Sprintf("repo-%d", i))
		}(i)
	}
	group.Wait()
	state, err := app.loadVirtualRepositorySyncState()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("repo:repo-%d", i)
		if _, exists := state.Tombstones[key]; !exists {
			t.Fatalf("missing concurrent tombstone %q", key)
		}
	}
}

func TestMarkVirtualRepositorySyncMutationAdvancesGeneration(t *testing.T) {
	app := &App{}
	before := app.virtualRepositorySyncGeneration.Load()
	app.markVirtualRepositorySyncMutation()
	if got := app.virtualRepositorySyncGeneration.Load(); got != before+1 {
		t.Fatalf("generation = %d, want %d after durable local mutation", got, before+1)
	}
	var nilApp *App
	nilApp.markVirtualRepositorySyncMutation()
}

func TestMergeVirtualRepositorySyncDeletionConflictCanUseCloudDeletion(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 1, 0, 0, 0, time.UTC)
	deleted := updated.Add(time.Hour)
	base := virtualRepositorySyncRepo{Repository: VirtualRepository{ID: "repo-1", Name: "Workspace", Version: 1, UpdatedAt: updated}, Location: "local"}
	local := base
	local.Repository.Name = "Workspace changed locally"
	merged, conflicts := mergeVirtualRepositorySyncPackages(
		virtualRepositorySyncPackage{Version: 1, Repositories: map[string]virtualRepositorySyncRepo{"repo-1": local}},
		virtualRepositorySyncPackage{Version: 1, Repositories: map[string]virtualRepositorySyncRepo{}, Tombstones: map[string]time.Time{"repo:repo-1": deleted}},
		map[string]string{"repo:repo-1": virtualRepositorySyncHash(base)},
		map[string]string{"repo:repo-1": "cloud"},
	)
	if len(conflicts) != 0 {
		t.Fatalf("cloud deletion should resolve conflict, got %#v", conflicts)
	}
	if _, exists := merged.Repositories["repo-1"]; exists {
		t.Fatal("cloud deletion left the local repository in the merged package")
	}
	if _, exists := merged.Tombstones["repo:repo-1"]; !exists {
		t.Fatal("cloud deletion did not retain the repository tombstone")
	}
}

func TestMergeVirtualRepositorySyncDeletionConflictCanKeepLocal(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 1, 0, 0, 0, time.UTC)
	deleted := updated.Add(time.Hour)
	base := virtualRepositorySyncRepo{Repository: VirtualRepository{ID: "repo-1", Name: "Workspace", Version: 1, UpdatedAt: updated}, Location: "local"}
	local := base
	local.Repository.Name = "Workspace changed locally"
	merged, conflicts := mergeVirtualRepositorySyncPackages(
		virtualRepositorySyncPackage{Version: 1, Repositories: map[string]virtualRepositorySyncRepo{"repo-1": local}},
		virtualRepositorySyncPackage{Version: 1, Repositories: map[string]virtualRepositorySyncRepo{}, Tombstones: map[string]time.Time{"repo:repo-1": deleted}},
		map[string]string{"repo:repo-1": virtualRepositorySyncHash(base)},
		map[string]string{"repo:repo-1": "local"},
	)
	if len(conflicts) != 0 {
		t.Fatalf("local choice should resolve conflict, got %#v", conflicts)
	}
	if got := merged.Repositories["repo-1"].Repository.Name; got != "Workspace changed locally" {
		t.Fatalf("merged repository name = %q", got)
	}
	if _, exists := merged.Tombstones["repo:repo-1"]; exists {
		t.Fatal("keeping the local repository retained the deletion tombstone")
	}
}

func TestMergeVirtualRepositorySyncRepositoryDeletionRemovesSSHSecret(t *testing.T) {
	deleted := time.Date(2026, time.July, 23, 3, 0, 0, 0, time.UTC)
	merged, conflicts := mergeVirtualRepositorySyncPackages(
		virtualRepositorySyncPackage{Version: 1, Tombstones: map[string]time.Time{"repo:repo-1": deleted}},
		virtualRepositorySyncPackage{Version: 1, SSHSecrets: map[string]string{"repo-1": "cloud-password"}, Bindings: map[string]string{"repo-1:node-1": "credential-1"}},
		map[string]string{"ssh:repo-1": virtualRepositorySyncHash("cloud-password"), "binding:repo-1:node-1": virtualRepositorySyncHash("credential-1")},
		nil,
	)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected deletion conflicts: %#v", conflicts)
	}
	if _, exists := merged.SSHSecrets["repo-1"]; exists {
		t.Fatal("deleted repository retained its SSH password in the sync package")
	}
	if _, exists := merged.Tombstones["ssh:repo-1"]; !exists {
		t.Fatal("SSH deletion tombstone was not retained")
	}
	if _, exists := merged.Bindings["repo-1:node-1"]; exists {
		t.Fatal("deleted repository retained a credential binding")
	}
	if _, exists := merged.Tombstones["binding:repo-1:node-1"]; !exists {
		t.Fatal("repository deletion did not retain the binding tombstone")
	}
}

func TestMergeVirtualRepositorySyncCredentialDeletionRemovesDanglingBinding(t *testing.T) {
	deletedAt := time.Date(2026, time.July, 24, 9, 0, 0, 0, time.UTC)
	merged, conflicts := mergeVirtualRepositorySyncPackages(
		virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Tombstones: map[string]time.Time{"cred:credential-1": deletedAt}},
		virtualRepositorySyncPackage{Version: virtualRepositorySyncVersion, Bindings: map[string]string{"repo-1:node-1": "credential-1"}},
		map[string]string{"binding:repo-1:node-1": virtualRepositorySyncHash("credential-1")},
		nil,
	)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", conflicts)
	}
	if _, exists := merged.Bindings["repo-1:node-1"]; exists {
		t.Fatalf("deleted credential retained a dangling binding: %#v", merged.Bindings)
	}
	if got := merged.Tombstones["binding:repo-1:node-1"]; !got.Equal(deletedAt) {
		t.Fatalf("binding tombstone = %v, want credential deletion time %v", got, deletedAt)
	}
	if err := validateVirtualRepositorySyncPackage(merged); err != nil {
		t.Fatalf("cleaned merged package must be valid: %v", err)
	}
}

func TestMergeVirtualRepositorySyncCredentialDeletionConflictChoices(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 4, 0, 0, 0, time.UTC)
	base := virtualRepositorySyncCred{Metadata: RepositoryCredentialMetadata{ID: "credential-1", Name: "Git", Kind: "git", Username: "old", CreatedAt: updated, UpdatedAt: updated}, Secret: "old-secret"}
	local := base
	local.Metadata.Username = "new"
	local.Metadata.UpdatedAt = updated.Add(time.Minute)

	for _, choice := range []struct {
		name       string
		resolution string
		wantExists bool
	}{
		{name: "keep local", resolution: "local", wantExists: true},
		{name: "keep cloud deletion", resolution: "cloud", wantExists: false},
	} {
		t.Run(choice.name, func(t *testing.T) {
			merged, conflicts := mergeVirtualRepositorySyncPackages(
				virtualRepositorySyncPackage{Version: 1, Credentials: map[string]virtualRepositorySyncCred{"credential-1": local}},
				virtualRepositorySyncPackage{Version: 1, Tombstones: map[string]time.Time{"cred:credential-1": updated.Add(2 * time.Minute)}},
				map[string]string{"cred:credential-1": virtualRepositorySyncHash(base)},
				map[string]string{"cred:credential-1": choice.resolution},
			)
			if len(conflicts) != 0 {
				t.Fatalf("unexpected conflicts: %#v", conflicts)
			}
			_, exists := merged.Credentials["credential-1"]
			if exists != choice.wantExists {
				t.Fatalf("credential exists = %t, want %t", exists, choice.wantExists)
			}
			_, tombstoned := merged.Tombstones["cred:credential-1"]
			if tombstoned == choice.wantExists {
				t.Fatalf("credential tombstone = %t, want %t", tombstoned, !choice.wantExists)
			}
		})
	}
}

func TestMergeVirtualRepositorySyncBindingAndSSHDeletionConflictChoices(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 5, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		name       string
		prefix     string
		local      virtualRepositorySyncPackage
		cloud      virtualRepositorySyncPackage
		baseHash   string
		resolution string
		present    func(virtualRepositorySyncPackage) bool
	}{
		{
			name: "binding keeps local", prefix: "binding:repo-1:node-1", resolution: "local", baseHash: virtualRepositorySyncHash("credential-old"),
			local:   virtualRepositorySyncPackage{Version: 1, Bindings: map[string]string{"repo-1:node-1": "credential-new"}},
			cloud:   virtualRepositorySyncPackage{Version: 1, Tombstones: map[string]time.Time{"binding:repo-1:node-1": updated.Add(time.Minute)}},
			present: func(pkg virtualRepositorySyncPackage) bool { return pkg.Bindings["repo-1:node-1"] == "credential-new" },
		},
		{
			name: "SSH keeps cloud deletion", prefix: "ssh:repo-1", resolution: "cloud", baseHash: virtualRepositorySyncHash("old-password"),
			local:   virtualRepositorySyncPackage{Version: 1, SSHSecrets: map[string]string{"repo-1": "new-password"}},
			cloud:   virtualRepositorySyncPackage{Version: 1, Tombstones: map[string]time.Time{"ssh:repo-1": updated.Add(time.Minute)}},
			present: func(pkg virtualRepositorySyncPackage) bool { _, exists := pkg.SSHSecrets["repo-1"]; return exists },
		},
	} {
		t.Run(item.name, func(t *testing.T) {
			merged, conflicts := mergeVirtualRepositorySyncPackages(item.local, item.cloud, map[string]string{item.prefix: item.baseHash}, map[string]string{item.prefix: item.resolution})
			if len(conflicts) != 0 {
				t.Fatalf("unexpected conflicts: %#v", conflicts)
			}
			if got := item.present(merged); got != (item.resolution == "local") {
				t.Fatalf("item exists = %t, want %t", got, item.resolution == "local")
			}
		})
	}
}

func TestMergeVirtualRepositorySyncPackageDoesNotMutateInputs(t *testing.T) {
	deleted := time.Date(2026, time.July, 23, 6, 0, 0, 0, time.UTC)
	local := virtualRepositorySyncPackage{
		Version:    1,
		Tombstones: map[string]time.Time{"repo:repo-1": deleted},
		Bindings:   map[string]string{"repo-1:node-1": "credential-1"},
	}
	cloud := virtualRepositorySyncPackage{
		Version:    1,
		SSHSecrets: map[string]string{"repo-1": "password"},
	}
	_, conflicts := mergeVirtualRepositorySyncPackages(local, cloud, nil, nil)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", conflicts)
	}
	if _, exists := local.Tombstones["ssh:repo-1"]; exists {
		t.Fatal("merge mutated local tombstones")
	}
	if _, exists := cloud.Tombstones["ssh:repo-1"]; exists {
		t.Fatal("merge mutated cloud tombstones")
	}
}

func TestMergeVirtualRepositorySyncRepositoryCopyKeepsScopedSecretsAndBindings(t *testing.T) {
	updated := time.Date(2026, time.July, 23, 7, 0, 0, 0, time.UTC)
	base := virtualRepositorySyncRepo{Repository: VirtualRepository{ID: "repo-1", Name: "Workspace", Version: 1, UpdatedAt: updated}, Location: "remote"}
	localRepo := base
	localRepo.Repository.Name = "Workspace on this computer"
	cloudRepo := base
	cloudRepo.Repository.Name = "Workspace on the Hub"
	credential := virtualRepositorySyncCred{Metadata: RepositoryCredentialMetadata{ID: "credential-1", Name: "Git", Kind: "git", Username: "user", CreatedAt: updated, UpdatedAt: updated}, Secret: "token"}
	local := virtualRepositorySyncPackage{
		Version:      1,
		Repositories: map[string]virtualRepositorySyncRepo{"repo-1": localRepo},
		Credentials:  map[string]virtualRepositorySyncCred{"credential-1": credential},
		Bindings:     map[string]string{"repo-1:node-1": "credential-1"},
		SSHSecrets:   map[string]string{"repo-1": "ssh-password"},
	}
	cloud := virtualRepositorySyncPackage{
		Version:      1,
		Repositories: map[string]virtualRepositorySyncRepo{"repo-1": cloudRepo},
		Credentials:  map[string]virtualRepositorySyncCred{"credential-1": credential},
		Bindings:     map[string]string{"repo-1:node-1": "credential-1"},
		SSHSecrets:   map[string]string{"repo-1": "ssh-password"},
	}
	merged, conflicts := mergeVirtualRepositorySyncPackages(local, cloud, map[string]string{"repo:repo-1": virtualRepositorySyncHash(base)}, map[string]string{"repo:repo-1": "copy"})
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %#v", conflicts)
	}
	var copyID string
	for id, repo := range merged.Repositories {
		if id != "repo-1" && repo.Repository.Name == "Workspace on this computer (local copy)" {
			copyID = id
		}
	}
	if copyID == "" {
		t.Fatalf("repository copy missing from %#v", merged.Repositories)
	}
	if merged.SSHSecrets[copyID] != "ssh-password" {
		t.Fatalf("copied repository SSH password = %q", merged.SSHSecrets[copyID])
	}
	if merged.Bindings[copyID+":node-1"] != "credential-1" {
		t.Fatalf("copied repository binding = %q", merged.Bindings[copyID+":node-1"])
	}
}
