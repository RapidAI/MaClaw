package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/cloudworkspaceignore"
)

type fakeCloudWorkspaceHub struct {
	mu                  sync.Mutex
	leaseID             string
	leaseExpiresAt      string
	fencingToken        int64
	acquired            string
	conflictUntilForce  bool
	forceCount          int
	leaseAcquires       int
	heartbeats          int
	heartbeatStatus     int
	deleted             bool
	failPush            bool
	failAfterOperations int
	operationCount      int
	revision            string
	entries             []cloudWorkspaceManifestEntry
	objects             map[string][]byte
	chunks              map[string]map[int][]byte
	sidecars            map[string][]byte
	sidecarGets         int
	sidecarGetsByName   map[string]int
	events              []cloudWorkspaceEvent
	failEvents          bool
	entitlement         *CloudWorkspaceEntitlement
}

func TestCloudWorkspaceProcessLockExcludesConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	const attempts = 2
	results := make(chan struct {
		path string
		err  error
	}, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := acquireCloudWorkspaceProcessLock(root)
			results <- struct {
				path string
				err  error
			}{path: path, err: err}
		}()
	}
	wg.Wait()
	close(results)
	var acquired string
	var failures int
	for result := range results {
		if result.err == nil {
			if acquired != "" {
				t.Fatal("two concurrent processes acquired the same writer lock")
			}
			acquired = result.path
		} else {
			failures++
		}
	}
	if acquired == "" || failures != 1 {
		t.Fatalf("acquired=%q failures=%d", acquired, failures)
	}
	releaseCloudWorkspaceProcessLock(acquired)
}

func TestCloudWorkspaceCacheIDRejectsTraversalAndWhitespace(t *testing.T) {
	for _, id := range []string{".", "..", " cws", "cws ", "cws/other", `cws\\other`, "cws:other", "cws\tother", "CON", "cws."} {
		if validCloudWorkspaceCacheID(id) {
			t.Fatalf("cache id %q must be rejected", id)
		}
	}
	if !validCloudWorkspaceCacheID("cws_valid") {
		t.Fatal("valid cache id rejected")
	}
}

func TestCloudWorkspaceCachePathSanitizesUntrustedTenant(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := normalizeProjectSessionPath(app.cloudWorkspaceCachePath(`..\outside`, "cws_safe"))
	root := normalizeProjectSessionPath(filepath.Join(app.GetDataDir(), "cloud-workspaces"))
	if !cloudWorkspacePathInsideRoot(root, path) {
		t.Fatalf("sanitized cache path escaped root: root=%q path=%q", root, path)
	}
	if strings.Contains(path, "outside") {
		t.Fatalf("raw tenant value leaked into cache path: %q", path)
	}
	if got := app.cloudWorkspaceTenantID(); got != cloudWorkspaceDefaultTenantID {
		t.Fatalf("missing tenant should use default, got %q", got)
	}
}

func TestCloudWorkspacePrepareRejectsUnsafeWorkspaceID(t *testing.T) {
	app := &App{}
	for _, id := range []string{"../escape", `cws\\escape`, "cws:escape", " cws"} {
		if _, err := app.PrepareCloudWorkspace(id); err == nil {
			t.Fatalf("PrepareCloudWorkspace accepted unsafe id %q", id)
		}
		if _, err := app.PrepareCloudWorkspaceReadOnly(id); err == nil {
			t.Fatalf("PrepareCloudWorkspaceReadOnly accepted unsafe id %q", id)
		}
	}
}

func TestCloudWorkspaceProcessLockHeartbeatKeepsLongLivedWriterOwned(t *testing.T) {
	root := t.TempDir()
	acquired, err := acquireCloudWorkspaceProcessLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseCloudWorkspaceProcessLock(acquired)

	// A lock created long ago would previously be reclaimed even when the
	// owning GUI was healthy. Refresh its liveness timestamp as the heartbeat
	// does, then verify a second writer is still rejected.
	stale := time.Now().Add(-3 * cloudWorkspaceHeartbeatInterval)
	if err := os.Chtimes(acquired, stale, stale); err != nil {
		t.Fatal(err)
	}
	if err := touchCloudWorkspaceProcessLock(acquired); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireCloudWorkspaceProcessLock(root); err == nil {
		t.Fatal("second writer reclaimed a lock refreshed by the active heartbeat")
	}
}

func TestCloudWorkspaceProcessLockReleaseIsOwnerScoped(t *testing.T) {
	root := t.TempDir()
	firstPath, firstOwner, err := acquireCloudWorkspaceProcessLockOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	// Make the first lock eligible for crash recovery, then let a successor
	// reclaim it. A delayed cleanup from the old owner must not remove the
	// successor's lock.
	stale := time.Now().Add(-3 * cloudWorkspaceHeartbeatInterval)
	if err := os.Chtimes(firstPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	secondPath, secondOwner, err := acquireCloudWorkspaceProcessLockOwned(root)
	if err != nil {
		t.Fatal(err)
	}
	if secondPath != firstPath || secondOwner == firstOwner {
		t.Fatalf("successor lock path/owner=%q/%q, first=%q/%q", secondPath, secondOwner, firstPath, firstOwner)
	}
	releaseCloudWorkspaceProcessLockOwned(firstPath, firstOwner)
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("old owner cleanup removed successor lock: %v", err)
	}
	releaseCloudWorkspaceProcessLockOwned(secondPath, secondOwner)
}

func TestCloudWorkspaceProcessLockGuardRecoversAfterCrash(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, ".maclaw-cloud", "writer.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	guardPath := lockPath + ".guard"
	if err := os.WriteFile(guardPath, []byte("crashed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(guardPath, time.Now().Add(-3*cloudWorkspaceHeartbeatInterval), time.Now().Add(-3*cloudWorkspaceHeartbeatInterval)); err != nil {
		t.Fatal(err)
	}
	got, err := acquireCloudWorkspaceProcessLockGuard(lockPath)
	if err != nil {
		t.Fatalf("stale guard was not reclaimed: %v", err)
	}
	if got != guardPath {
		t.Fatalf("guard path=%q want %q", got, guardPath)
	}
	releaseCloudWorkspaceProcessLockGuard(got)

	active, err := acquireCloudWorkspaceProcessLockGuard(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseCloudWorkspaceProcessLockGuard(active)
	if _, err := acquireCloudWorkspaceProcessLockGuard(lockPath); err == nil {
		t.Fatal("active guard must block a concurrent lock operation")
	}
}

func TestCloudWorkspaceWatcherErrorMarksReconcileRequired(t *testing.T) {
	mount := &cloudWorkspaceHeldMount{WorkspaceID: "cws-reconcile", ReadOnly: true}
	app := &App{}
	app.markCloudWorkspaceReconcileRequired(mount, errors.New("watch queue overflow"))
	mount.mu.Lock()
	required := mount.reconcileRequired
	mount.mu.Unlock()
	if !required {
		t.Fatal("watcher error did not mark reconciliation as required")
	}
}

func TestCloudWorkspaceWatcherReconcileMarkerPersistsAndClearsOnCommit(t *testing.T) {
	root := t.TempDir()
	mount := &cloudWorkspaceHeldMount{
		WorkspaceID: "cws-reconcile-persist",
		LocalPath:   root,
		ReadOnly:    false,
	}
	app := &App{}
	app.markCloudWorkspaceReconcileRequired(mount, errors.New("fsnotify overflow"))
	state, err := readCloudWorkspaceLocalState(root)
	if err != nil {
		t.Fatal(err)
	}
	if !state.ReconcileRequired || state.ReconcileReason != "fsnotify overflow" || state.ReconcileAt == "" {
		t.Fatalf("durable reconcile marker=%+v", state)
	}

	// A successful manifest commit establishes a complete baseline and must
	// clear the marker so a restart does not trigger an unnecessary scan.
	if err := writeCloudWorkspaceManifestState(root, &cloudWorkspaceManifest{Revision: "rev-1", Entries: []cloudWorkspaceManifestEntry{}}); err != nil {
		t.Fatal(err)
	}
	state, err = readCloudWorkspaceLocalState(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.ReconcileRequired || state.ReconcileReason != "" || state.ReconcileAt != "" {
		t.Fatalf("reconcile marker not cleared after commit=%+v", state)
	}
}

func TestCloudWorkspaceReadOnlyReconcileMarkerIsNotPersisted(t *testing.T) {
	root := t.TempDir()
	mount := &cloudWorkspaceHeldMount{
		WorkspaceID:      "cws-reconcile-readonly",
		LocalPath:        root,
		ReadOnly:         true,
		isolatedReadOnly: true,
	}
	(&App{}).markCloudWorkspaceReconcileRequired(mount, errors.New("watcher closed"))
	if _, err := os.Stat(cloudWorkspaceStatePath(root)); !os.IsNotExist(err) {
		t.Fatalf("isolated read-only marker should not persist, stat err=%v", err)
	}
}

func TestCloudWorkspaceStopReadOnlyMountDoesNotFollowTenantSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	cacheRoot := filepath.Join(root, "cloud-workspaces-readonly")
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cacheRoot, "tenant_link")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	mount := &cloudWorkspaceHeldMount{
		WorkspaceID:      "cws-stop-symlink",
		LocalPath:        filepath.Join(cacheRoot, "tenant_link", "cws-stop-symlink", "instance-1"),
		ReadOnly:         true,
		isolatedReadOnly: true,
	}
	stopCloudWorkspaceMount(mount)
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("stop followed tenant symlink and removed outside data: %v", err)
	}
}

func TestCloudWorkspaceLeaseExpiryDemotesWriterToReadOnly(t *testing.T) {
	mount := &cloudWorkspaceHeldMount{WorkspaceID: "cws-offline", ReadOnly: false, LeaseID: "cwl-1"}
	applyCloudWorkspaceLeaseExpired(mount)
	mount.mu.Lock()
	readOnly := mount.ReadOnly
	mount.mu.Unlock()
	if !readOnly {
		t.Fatal("expired lease must demote the local writer to read-only")
	}
}

func TestCloudWorkspaceStolenWaitsForInFlightSync(t *testing.T) {
	mount := &cloudWorkspaceHeldMount{WorkspaceID: "cws-stolen-wait", LeaseID: "lease", syncRunning: true}
	done := make(chan struct{})
	mount.syncDone = done
	finished := make(chan struct{})
	go func() {
		applyCloudWorkspaceStolen(mount)
		close(finished)
	}()
	select {
	case <-finished:
		t.Fatal("stolen transition returned before in-flight sync completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(done)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("stolen transition did not finish after sync completion")
	}
	mount.mu.Lock()
	readOnly, stopped := mount.ReadOnly, mount.stopped
	mount.mu.Unlock()
	if !readOnly || !stopped {
		t.Fatalf("stolen mount state readOnly=%v stopped=%v", readOnly, stopped)
	}
}

func TestCloudWorkspaceNetworkPartitionDemotesAndRecoversWithNewEpoch(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{
		acquired:        cloudWorkspaceAcquiredGranted,
		heartbeatStatus: http.StatusServiceUnavailable,
		leaseExpiresAt:  time.Now().UTC().Add(250 * time.Millisecond).Format(time.RFC3339Nano),
		fencingToken:    1,
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	oldInterval := cloudWorkspaceHeartbeatIntervalValue
	cloudWorkspaceBackgroundDisabled = false
	cloudWorkspaceHeartbeatIntervalValue = 10 * time.Millisecond
	cloudWorkspaceConfirmDiscardDirtyFn = func() bool { return true }
	t.Cleanup(func() {
		cloudWorkspaceHeartbeatIntervalValue = oldInterval
		cloudWorkspaceBackgroundDisabled = true
	})

	prepared, err := app.PrepareCloudWorkspace("cws_partition")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		mount := lookupHeldCloudWorkspace("cws_partition")
		if mount == nil {
			t.Fatal("partitioned mount disappeared")
		}
		mount.mu.Lock()
		readOnly := mount.ReadOnly
		mount.mu.Unlock()
		if readOnly {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer was not demoted after lease expiry")
		}
		time.Sleep(10 * time.Millisecond)
	}

	dirtyPath := filepath.Join(prepared.LocalPath, "offline.txt")
	if err := os.WriteFile(dirtyPath, []byte("offline change"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pushCloudWorkspace(context.Background(), "cws_partition", prepared.LocalPath); !errors.Is(err, errCloudWorkspaceFenced) {
		t.Fatalf("read-only push err=%v", err)
	}
	hub.mu.Lock()
	if _, uploaded := hub.objects[cloudWorkspaceSHA256Hex([]byte("offline change"))]; uploaded {
		hub.mu.Unlock()
		t.Fatal("offline read-only cache uploaded data")
	}
	hub.heartbeatStatus = http.StatusOK
	hub.leaseExpiresAt = time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	hub.leaseID = "cwl_recovered"
	hub.fencingToken = 2
	hub.acquired = cloudWorkspaceAcquiredGranted
	hub.mu.Unlock()

	recovered, err := app.PrepareCloudWorkspace("cws_partition")
	if err != nil {
		t.Fatalf("recover prepare: %v", err)
	}
	if recovered.LocalPath != prepared.LocalPath {
		t.Fatalf("recovered path=%q want %q", recovered.LocalPath, prepared.LocalPath)
	}
	mount := lookupHeldCloudWorkspace("cws_partition")
	if mount == nil {
		t.Fatal("recovered mount missing")
	}
	mount.mu.Lock()
	readOnly, leaseID, token := mount.ReadOnly, mount.LeaseID, mount.FencingToken
	mount.mu.Unlock()
	if readOnly || leaseID != "cwl_recovered" || token != 2 {
		t.Fatalf("recovered mount readOnly=%v lease=%q token=%d", readOnly, leaseID, token)
	}
	hub.mu.Lock()
	_, uploaded := hub.objects[cloudWorkspaceSHA256Hex([]byte("offline change"))]
	hub.mu.Unlock()
	if !uploaded {
		t.Fatal("confirmed recovery did not upload preserved local change under the new epoch")
	}
}

func cloudWorkspaceSHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (h *fakeCloudWorkspaceHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.objects == nil {
		h.objects = map[string][]byte{}
	}
	if h.chunks == nil {
		h.chunks = map[string]map[int][]byte{}
	}
	if h.sidecars == nil {
		h.sidecars = map[string][]byte{}
	}
	if h.leaseID == "" {
		h.leaseID = "cwl_testlease"
	}
	if h.leaseExpiresAt == "" {
		h.leaseExpiresAt = "2099-01-01T00:00:00Z"
	}
	if h.fencingToken <= 0 {
		h.fencingToken = 1
	}
	if h.acquired == "" {
		h.acquired = cloudWorkspaceAcquiredGranted
	}
	if h.heartbeatStatus == 0 {
		h.heartbeatStatus = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == cloudWorkspaceEntitlementPath:
		if h.entitlement == nil {
			http.NotFound(w, r)
			return
		}
		ent := *h.entitlement
		if ent.Workspaces == nil {
			ent.Workspaces = []CloudWorkspaceEntitlementWorkspace{}
		}
		if ent.Deleted == nil {
			ent.Deleted = []CloudWorkspaceDeletedWorkspace{}
		}
		_ = json.NewEncoder(w).Encode(ent)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/leases"):
		var req struct {
			Force bool `json:"force"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Force {
			h.forceCount++
			h.conflictUntilForce = false
			h.acquired = cloudWorkspaceAcquiredGranted
		}
		if h.conflictUntilForce && !req.Force {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":               "CLOUD_WORKSPACE_IN_USE",
				"holder_machine_id":   "other",
				"holder_machine_name": "DESKTOP-OTHER",
				"expires_at":          "2099-01-01T00:00:00Z",
			})
			return
		}
		acquired := h.acquired
		if h.forceCount == 0 && acquired == "" {
			acquired = cloudWorkspaceAcquiredGranted
		}
		h.leaseAcquires++
		h.deleted = false
		_ = json.NewEncoder(w).Encode(cloudWorkspaceAcquireOutcome{
			LeaseID:      h.leaseID,
			ExpiresAt:    h.leaseExpiresAt,
			Acquired:     acquired,
			FencingToken: h.fencingToken,
		})
		if h.acquired == cloudWorkspaceAcquiredGranted {
			h.acquired = cloudWorkspaceAcquiredRenewed
		}
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/heartbeat"):
		h.heartbeats++
		w.WriteHeader(h.heartbeatStatus)
		if h.heartbeatStatus == http.StatusConflict {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "CLOUD_WORKSPACE_IN_USE"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"lease_id": h.leaseID, "expires_at": h.leaseExpiresAt})
	case r.Method == http.MethodDelete && strings.Contains(path, "/leases/"):
		h.deleted = true
		_ = json.NewEncoder(w).Encode(map[string]any{"released": true})
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/manifest"):
		entries := h.entries
		if entries == nil {
			entries = []cloudWorkspaceManifestEntry{}
		}
		_ = json.NewEncoder(w).Encode(cloudWorkspaceManifest{Revision: h.revision, Entries: entries})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/operations"):
		if h.failPush {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "CLOUD_WORKSPACE_FAILED", "message": "push failed"})
			return
		}
		h.operationCount++
		if h.failAfterOperations > 0 && h.operationCount > h.failAfterOperations {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "CLOUD_WORKSPACE_FAILED", "message": "operation failed"})
			return
		}
		var op cloudWorkspaceOperation
		_ = json.NewDecoder(r.Body).Decode(&op)
		if h.entries == nil {
			h.entries = []cloudWorkspaceManifestEntry{}
		}
		switch op.Kind {
		case "delete":
			kept := h.entries[:0]
			for _, entry := range h.entries {
				if entry.Path != op.Path {
					kept = append(kept, entry)
				}
			}
			h.entries = kept
		case "put":
			updated := false
			next := cloudWorkspaceManifestEntry{Path: op.Path, SHA256: op.ObjectSHA256, Size: op.PlainSize}
			for i, entry := range h.entries {
				if entry.Path == op.Path {
					h.entries[i] = next
					updated = true
					break
				}
			}
			if !updated {
				h.entries = append(h.entries, next)
			}
		}
		if h.revision == "" {
			h.revision = "rev-op"
		}
		_ = json.NewEncoder(w).Encode(cloudWorkspaceOperationResult{
			Accepted: true, WorkspaceSeq: 1, FileRevision: "fr-" + op.OpID, Merge: "ok",
		})
	case r.Method == http.MethodGet && strings.Contains(path, "/events"):
		if h.failEvents {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "events failed"})
			return
		}
		events := h.events
		if events == nil {
			events = []cloudWorkspaceEvent{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"events": events})
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/manifest"):
		if h.failPush {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "CLOUD_WORKSPACE_FAILED", "message": "push failed"})
			return
		}
		var req struct {
			IfMatchRevision string                        `json:"if_match_revision"`
			Entries         []cloudWorkspaceManifestEntry `json:"entries"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		h.entries = req.Entries
		h.revision = "rev-" + cloudWorkspaceSHA256Hex([]byte(req.IfMatchRevision + time.Now().String()))[:12]
		if req.Entries == nil {
			h.entries = []cloudWorkspaceManifestEntry{}
		}
		_ = json.NewEncoder(w).Encode(cloudWorkspaceManifest{Revision: h.revision, Entries: h.entries})
	case strings.Contains(path, "/sidecars/"):
		name := sidecarNameFromPath(path)
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			h.sidecars[name] = append([]byte(nil), body...)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": name, "size": len(body)})
		case http.MethodGet:
			h.sidecarGets++
			if h.sidecarGetsByName == nil {
				h.sidecarGetsByName = map[string]int{}
			}
			h.sidecarGetsByName[name]++
			body, ok := h.sidecars[name]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": "NOT_FOUND", "message": "sidecar not found"})
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	case strings.Contains(path, "/objects/") && strings.Contains(path, "/chunks/"):
		sha := objectSHAFromPath(path)
		idx := chunkIndexFromPath(path)
		body, _ := io.ReadAll(r.Body)
		if h.chunks[sha] == nil {
			h.chunks[sha] = map[int][]byte{}
		}
		h.chunks[sha][idx] = append([]byte(nil), body...)
		_ = json.NewEncoder(w).Encode(map[string]any{"sha256": sha, "index": idx, "size": len(body)})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/complete"):
		sha := objectSHAFromPath(path)
		var buf []byte
		for i := 0; ; i++ {
			part, ok := h.chunks[sha][i]
			if !ok {
				break
			}
			buf = append(buf, part...)
		}
		h.objects[sha] = buf
		_ = json.NewEncoder(w).Encode(map[string]any{"sha256": sha, "size": len(buf)})
	case r.Method == http.MethodPut && strings.Contains(path, "/objects/"):
		sha := objectSHAFromPath(path)
		body, _ := io.ReadAll(r.Body)
		h.objects[sha] = append([]byte(nil), body...)
		_ = json.NewEncoder(w).Encode(map[string]any{"sha256": sha, "size": len(body), "existed": false})
	case r.Method == http.MethodGet && strings.Contains(path, "/objects/"):
		sha := objectSHAFromPath(path)
		body := h.objects[sha]
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case r.Method == http.MethodDelete && fakeCloudWorkspaceItemPath(path):
		id := strings.TrimPrefix(path, cloudWorkspaceCollectionPath+"/")
		row := h.takeEntitlementWorkspace(id)
		if row.ID == "" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": row.ID, "name": row.Name, "status": "deleted",
			"used_bytes": row.UsedBytes, "deleted_at": "2026-08-29T00:00:00Z",
		})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/restore"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, cloudWorkspaceCollectionPath+"/"), "/restore")
		row := h.returnEntitlementWorkspace(id)
		if row.ID == "" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": row.ID, "name": row.Name, "status": "active", "used_bytes": row.UsedBytes,
		})
	default:
		http.NotFound(w, r)
	}
}

func fakeCloudWorkspaceItemPath(path string) bool {
	prefix := cloudWorkspaceCollectionPath + "/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	rest := strings.TrimPrefix(path, prefix)
	return rest != "" && !strings.Contains(rest, "/")
}

func (h *fakeCloudWorkspaceHub) takeEntitlementWorkspace(id string) CloudWorkspaceEntitlementWorkspace {
	if h.entitlement == nil {
		return CloudWorkspaceEntitlementWorkspace{}
	}
	kept := h.entitlement.Workspaces[:0]
	var found CloudWorkspaceEntitlementWorkspace
	for _, ws := range h.entitlement.Workspaces {
		if ws.ID == id {
			found = ws
			continue
		}
		kept = append(kept, ws)
	}
	if found.ID == "" {
		return CloudWorkspaceEntitlementWorkspace{}
	}
	h.entitlement.Workspaces = kept
	h.entitlement.Used = len(kept)
	h.entitlement.Deleted = append(h.entitlement.Deleted, CloudWorkspaceDeletedWorkspace{
		ID: found.ID, Name: found.Name, UsedBytes: found.UsedBytes, DeletedAt: "2026-08-29T00:00:00Z",
	})
	return found
}

func (h *fakeCloudWorkspaceHub) returnEntitlementWorkspace(id string) CloudWorkspaceEntitlementWorkspace {
	if h.entitlement == nil {
		return CloudWorkspaceEntitlementWorkspace{}
	}
	kept := h.entitlement.Deleted[:0]
	var found CloudWorkspaceDeletedWorkspace
	for _, ws := range h.entitlement.Deleted {
		if ws.ID == id {
			found = ws
			continue
		}
		kept = append(kept, ws)
	}
	if found.ID == "" {
		return CloudWorkspaceEntitlementWorkspace{}
	}
	h.entitlement.Deleted = kept
	row := CloudWorkspaceEntitlementWorkspace{ID: found.ID, Name: found.Name, UsedBytes: found.UsedBytes}
	h.entitlement.Workspaces = append(h.entitlement.Workspaces, row)
	h.entitlement.Used = len(h.entitlement.Workspaces)
	return row
}

func sidecarNameFromPath(path string) string {
	const marker = "/sidecars/"
	i := strings.Index(path, marker)
	if i < 0 {
		return ""
	}
	return path[i+len(marker):]
}

func objectSHAFromPath(path string) string {
	const marker = "/objects/"
	i := strings.Index(path, marker)
	if i < 0 {
		return ""
	}
	rest := path[i+len(marker):]
	if cut := strings.Index(rest, "/"); cut >= 0 {
		rest = rest[:cut]
	}
	return rest
}

func chunkIndexFromPath(path string) int {
	const marker = "/chunks/"
	i := strings.Index(path, marker)
	if i < 0 {
		return 0
	}
	n := 0
	for _, c := range path[i+len(marker):] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func newCloudWorkspaceMountTestApp(t *testing.T, hub *fakeCloudWorkspaceHub) *App {
	t.Helper()
	resetCloudWorkspaceDialogMocks()
	resetCloudWorkspaceInstanceSessions()
	t.Cleanup(resetCloudWorkspaceDialogMocks)
	t.Cleanup(resetCloudWorkspaceInstanceSessions)
	server := httptest.NewServer(hub)
	t.Cleanup(server.Close)
	app := newProjectSearchTestApp(t)
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "machine-test",
		RemoteTenantID:     "tenant_acme",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	seedCloudWorkspaceInstanceSession(server.URL, "machine-token", "machine-test")
	return app
}

func TestCloudWorkspaceTransferTimeout(t *testing.T) {
	if got := cloudWorkspaceTransferTimeout(0); got != 60*time.Second {
		t.Fatalf("size 0: %v", got)
	}
	if got := cloudWorkspaceTransferTimeout(8 << 20); got != (30+32)*time.Second {
		t.Fatalf("8MiB: %v", got)
	}
	if cloudWorkspaceChunkTimeout != 60*time.Second {
		t.Fatalf("chunk timeout=%v", cloudWorkspaceChunkTimeout)
	}
	direct := cloudWorkspaceTransferTimeout(cloudWorkspaceObjectMaxBytes)
	if got := cloudWorkspaceSyncTimeout(); got < direct {
		t.Fatalf("sync timeout %v < direct object timeout %v", got, direct)
	}
	chunked := time.Duration(cloudWorkspaceMaxObjectChunkCount()) * cloudWorkspaceChunkTimeout
	if got := cloudWorkspaceSyncTimeout(); got < chunked {
		t.Fatalf("sync timeout %v < chunked max-object budget %v", got, chunked)
	}
	shutdown, cancel := context.WithTimeout(context.Background(), cloudWorkspaceShutdownReleaseTimeout)
	defer cancel()
	bound, stop := bindCloudWorkspaceTimeout(shutdown, 10*time.Minute)
	defer stop()
	deadline, ok := bound.Deadline()
	if !ok || time.Until(deadline) > cloudWorkspaceShutdownReleaseTimeout+time.Second {
		t.Fatalf("shutdown context must not be extended, deadline=%v ok=%v", deadline, ok)
	}
}

func TestCloudWorkspaceCacheDirUsesHeldOrOnDiskPathWithoutHub(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	t.Cleanup(resetCloudWorkspaceMounts)

	if _, err := app.CloudWorkspaceCacheDir(""); err == nil {
		t.Fatal("empty workspace id should fail")
	}
	if _, err := app.CloudWorkspaceCacheDir(`..\cws_escape`); err == nil {
		t.Fatal("path-like workspace id should fail")
	}

	missing, err := app.CloudWorkspaceCacheDir("cws_missing")
	if err != nil {
		t.Fatalf("missing cache: %v", err)
	}
	if missing.LocalPath != "" {
		t.Fatalf("missing cache path=%q", missing.LocalPath)
	}

	diskID := "cws_disk"
	diskPath := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", diskID))
	if err := os.MkdirAll(diskPath, 0o700); err != nil {
		t.Fatal(err)
	}
	fromDisk, err := app.CloudWorkspaceCacheDir(diskID)
	if err != nil {
		t.Fatalf("on-disk cache: %v", err)
	}
	if fromDisk.LocalPath != diskPath {
		t.Fatalf("on-disk path=%q want %q", fromDisk.LocalPath, diskPath)
	}

	heldID := "cws_ro"
	heldPath := normalizeProjectSessionPath(filepath.Join(t.TempDir(), "held-cache"))
	if err := os.MkdirAll(heldPath, 0o700); err != nil {
		t.Fatal(err)
	}
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID: heldID,
		LocalPath:   heldPath,
		ReadOnly:    true,
	})
	fromHeld, err := app.CloudWorkspaceCacheDir(heldID)
	if err != nil {
		t.Fatalf("held cache: %v", err)
	}
	if fromHeld.LocalPath != heldPath {
		t.Fatalf("held path=%q want %q", fromHeld.LocalPath, heldPath)
	}

	goneID := "cws_gone"
	goneTenant := filepath.Join(app.GetDataDir(), "cloud-workspaces", "other_tenant", goneID)
	if err := os.MkdirAll(goneTenant, 0o700); err != nil {
		t.Fatal(err)
	}
	storeCloudWorkspaceMount(&cloudWorkspaceHeldMount{
		WorkspaceID: goneID,
		LocalPath:   filepath.Join(t.TempDir(), "deleted-held"),
		ReadOnly:    true,
	})
	fromOther, err := app.CloudWorkspaceCacheDir(goneID)
	if err != nil {
		t.Fatalf("other tenant cache: %v", err)
	}
	wantOther := normalizeProjectSessionPath(goneTenant)
	if fromOther.LocalPath != wantOther {
		t.Fatalf("other tenant path=%q want %q", fromOther.LocalPath, wantOther)
	}

	hub.mu.Lock()
	leases := hub.leaseAcquires
	hub.mu.Unlock()
	if leases != 0 {
		t.Fatalf("cache dir lookup must not acquire a lease, got %d", leases)
	}
}

func TestCloudWorkspaceCachePathWindowsJoin(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{})
	got := app.cloudWorkspaceCachePath("tenant_acme", "cws_demo")
	want := filepath.Join(app.GetDataDir(), "cloud-workspaces", "tenant_acme", "cws_demo")
	if got != want {
		t.Fatalf("cache path=%q want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("cache path must be absolute on Windows: %q", got)
	}
	if strings.Contains(got, "cloud-workspaces/tenant_acme") && filepath.Separator == '\\' {
		t.Fatalf("Windows path should not use slash separators: %q", got)
	}
}

func TestPrepareCloudWorkspaceDoesNotParseIDsFromPath(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{revision: "rev-server", acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	decoy := filepath.Join(app.GetDataDir(), "cloud-workspaces", "other_tenant", "cws_other")
	if err := os.MkdirAll(decoy, 0o700); err != nil {
		t.Fatal(err)
	}
	prepared, err := app.PrepareCloudWorkspace("cws_demo")
	if err != nil {
		t.Fatalf("PrepareCloudWorkspace: %v", err)
	}
	if prepared.WorkspaceID != "cws_demo" {
		t.Fatalf("workspace id=%q", prepared.WorkspaceID)
	}
	want := normalizeProjectSessionPath(filepath.Join(app.GetDataDir(), "cloud-workspaces", "tenant_acme", "cws_demo"))
	if prepared.LocalPath != want {
		t.Fatalf("local=%q want %q", prepared.LocalPath, want)
	}
	if prepared.LocalPath == decoy {
		t.Fatal("must not parse workspace id from a decoy cache path")
	}
}

func TestPrepareGrantedPullsServerTreeAndDeletesLocalExtras(t *testing.T) {
	body := []byte("hello from server")
	sum := cloudWorkspaceSHA256Hex(body)
	hub := &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		revision: "rev-1",
		entries:  []cloudWorkspaceManifestEntry{{Path: "src/main.go", SHA256: sum, Size: int64(len(body))}},
		objects:  map[string][]byte{sum: body},
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	root := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_pull"))
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra.txt"), []byte("gone"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "stale-empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "x.js"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := app.PrepareCloudWorkspace("cws_pull")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(prepared.LocalPath, "src", "main.go"))
	if err != nil || string(got) != string(body) {
		t.Fatalf("main.go=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "extra.txt")); !os.IsNotExist(err) {
		t.Fatal("pull should delete unignored extra")
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "stale-empty")); !os.IsNotExist(err) {
		t.Fatal("pull should prune empty leftover dirs")
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "node_modules", "x.js")); err != nil {
		t.Fatal("ignored extras must not be deleted")
	}
	st, err := readCloudWorkspaceLocalState(prepared.LocalPath)
	if err != nil || st.LastPushedRevision != "rev-1" {
		t.Fatalf("state=%+v err=%v", st, err)
	}
}

func TestPrepareRenewedSkipsPullAndPushesLocal(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredRenewed, revision: "rev-old"}
	app := newCloudWorkspaceMountTestApp(t, hub)
	root := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_renew"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "local.txt"), []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra-local.txt"), []byte("also-keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := app.PrepareCloudWorkspace("cws_renew")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prepared.LocalPath, "extra-local.txt")); err != nil {
		t.Fatal("renewed must not pull-delete local files")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.entries) != 2 {
		t.Fatalf("pushed entries=%+v", hub.entries)
	}
	if hub.revision == "rev-old" {
		t.Fatal("push should replace revision")
	}
}

func TestPushUnchangedTreeKeepsRevision(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_stable")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "a.txt"), []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	first, err := app.pushCloudWorkspace(ctx, "cws_stable", prepared.LocalPath)
	if err != nil || first == nil || first.Revision == "" {
		t.Fatalf("first push=%+v err=%v", first, err)
	}
	second, err := app.pushCloudWorkspace(ctx, "cws_stable", prepared.LocalPath)
	if err != nil || second == nil {
		t.Fatalf("second push=%+v err=%v", second, err)
	}
	if second.Revision != first.Revision {
		t.Fatalf("noop push bumped revision %q -> %q", first.Revision, second.Revision)
	}
}

func TestPrepareReusesHeldWritableMount(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	first, err := app.PrepareCloudWorkspace("cws_reuse")
	if err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	before := hub.leaseAcquires
	hub.mu.Unlock()
	second, err := app.PrepareCloudWorkspace("cws_reuse")
	if err != nil {
		t.Fatal(err)
	}
	if second.LocalPath != first.LocalPath {
		t.Fatalf("path=%q want %q", second.LocalPath, first.LocalPath)
	}
	hub.mu.Lock()
	after := hub.leaseAcquires
	hub.mu.Unlock()
	if after != before {
		t.Fatalf("second prepare must not re-acquire, before=%d after=%d", before, after)
	}
}

func TestPrepareRenewedPushFailKeepsLease(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredRenewed, failPush: true}
	app := newCloudWorkspaceMountTestApp(t, hub)
	root := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_renew_fail"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dirty.txt"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := app.PrepareCloudWorkspace("cws_renew_fail")
	if err == nil {
		t.Fatal("expected push failure")
	}
	hub.mu.Lock()
	deleted := hub.deleted
	hub.mu.Unlock()
	if deleted {
		t.Fatal("same-machine renew must not DELETE the lease when push fails")
	}
	if lookupHeldCloudWorkspace("cws_renew_fail") != nil {
		t.Fatal("failed prepare must not store a mount")
	}
	state, stateErr := readCloudWorkspaceLocalState(root)
	if stateErr != nil || !state.ReconcileRequired {
		t.Fatalf("failed prepare must persist reconcile marker: state=%+v err=%v", state, stateErr)
	}
}

func TestPrepareStealForceOnlyAfterConfirm(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{conflictUntilForce: true, acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	cloudWorkspaceConfirmStealFn = func(string) bool { return false }
	_, err := app.PrepareCloudWorkspace("cws_busy")
	if err == nil || !strings.Contains(err.Error(), "占用") {
		t.Fatalf("err=%v", err)
	}
	hub.mu.Lock()
	if hub.forceCount != 0 {
		t.Fatalf("force sent without confirm: %d", hub.forceCount)
	}
	hub.mu.Unlock()

	cloudWorkspaceConfirmStealFn = func(holder string) bool {
		if holder != "DESKTOP-OTHER" {
			t.Fatalf("holder=%q", holder)
		}
		return true
	}
	if _, err := app.PrepareCloudWorkspace("cws_busy"); err != nil {
		t.Fatalf("force prepare: %v", err)
	}
	hub.mu.Lock()
	if hub.forceCount == 0 {
		t.Fatal("expected force=true after confirm")
	}
	hub.mu.Unlock()
}

func TestDirtyCacheCancelDeletesLeaseAndDoesNotOpen(t *testing.T) {
	body := []byte("server")
	sum := cloudWorkspaceSHA256Hex(body)
	hub := &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		revision: "rev-server",
		entries:  []cloudWorkspaceManifestEntry{{Path: "a.txt", SHA256: sum, Size: int64(len(body))}},
		objects:  map[string][]byte{sum: body},
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	root := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_dirty"))
	if err := os.MkdirAll(filepath.Join(root, ".maclaw-cloud"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeCloudWorkspaceLocalState(root, "rev-local"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stale.txt"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	cloudWorkspaceConfirmDiscardDirtyFn = func() bool { return false }
	_, err := app.PrepareCloudWorkspace("cws_dirty")
	if err == nil || !strings.Contains(err.Error(), "取消") {
		t.Fatalf("err=%v", err)
	}
	hub.mu.Lock()
	deleted := hub.deleted
	hub.mu.Unlock()
	if !deleted {
		t.Fatal("cancel must DELETE lease")
	}
	if lookupHeldCloudWorkspace("cws_dirty") != nil {
		t.Fatal("must not keep a mount after cancel")
	}
	if _, err := os.Stat(filepath.Join(root, "stale.txt")); err != nil {
		t.Fatal("cancel must not pull/delete local files")
	}
}

func TestDivergedTreesPromptAndKeepLocalOnConfirm(t *testing.T) {
	server := []byte("server")
	sum := cloudWorkspaceSHA256Hex(server)
	hub := &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		revision: "rev-server",
		entries:  []cloudWorkspaceManifestEntry{{Path: "a.txt", SHA256: sum, Size: int64(len(server))}},
		objects:  map[string][]byte{sum: server},
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	root := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_diverge"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("local-newer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeCloudWorkspaceLocalState(root, "rev-old"); err != nil {
		t.Fatal(err)
	}
	cloudWorkspaceConfirmDiscardDirtyFn = func() bool { return true }
	if _, err := app.PrepareCloudWorkspace("cws_diverge"); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(got) != "local-newer" {
		t.Fatalf("keep-local confirm must not pull, got=%q err=%v", got, err)
	}
}

func TestMatchingTreeDoesNotPromptDirtyOnRevisionMismatch(t *testing.T) {
	body := []byte("server")
	sum := cloudWorkspaceSHA256Hex(body)
	hub := &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		revision: "rev-server",
		entries:  []cloudWorkspaceManifestEntry{{Path: "a.txt", SHA256: sum, Size: int64(len(body))}},
		objects:  map[string][]byte{sum: body},
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	root := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_match"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeCloudWorkspaceLocalState(root, "rev-old"); err != nil {
		t.Fatal(err)
	}
	cloudWorkspaceConfirmDiscardDirtyFn = func() bool {
		t.Fatal("identical files must not prompt discard")
		return false
	}
	if _, err := app.PrepareCloudWorkspace("cws_match"); err != nil {
		t.Fatalf("prepare: %v", err)
	}
}

func TestLocalExtrasAtSameRevisionPushWithoutPrompt(t *testing.T) {
	body := []byte("server")
	sum := cloudWorkspaceSHA256Hex(body)
	hub := &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		revision: "rev-server",
		entries:  []cloudWorkspaceManifestEntry{{Path: "a.txt", SHA256: sum, Size: int64(len(body))}},
		objects:  map[string][]byte{sum: body},
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	root := normalizeProjectSessionPath(app.cloudWorkspaceCachePath("tenant_acme", "cws_extras"))
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra.txt"), []byte("unpushed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeCloudWorkspaceLocalState(root, "rev-server"); err != nil {
		t.Fatal(err)
	}
	cloudWorkspaceConfirmDiscardDirtyFn = func() bool {
		t.Fatal("same-revision local extras must push without a discard prompt")
		return false
	}
	if _, err := app.PrepareCloudWorkspace("cws_extras"); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "extra.txt")); err != nil {
		t.Fatal("local extras must be kept")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	found := false
	for _, entry := range hub.entries {
		if entry.Path == "extra.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("push must upload extras, entries=%+v", hub.entries)
	}
}

func TestScanCloudWorkspaceLocalWindowsPathsAndIgnore(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		rel = filepath.FromSlash(strings.ReplaceAll(rel, `\`, "/"))
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("src/main.go", "package main")
	write(`src\nested\win.txt`, "windows")
	write(".maclaw-cloud/state.json", `{"last_pushed_revision":"x"}`)
	write("node_modules/x.js", "nope")
	write("keep.exe", "bin")
	entries, err := scanCloudWorkspaceLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		if strings.Contains(e.Path, `\`) {
			t.Fatalf("manifest path must be slash-separated: %q", e.Path)
		}
		got[e.Path] = true
	}
	if !got["src/main.go"] || !got["src/nested/win.txt"] || !got["keep.exe"] {
		t.Fatalf("entries=%+v", entries)
	}
	if got[".maclaw-cloud/state.json"] || got["node_modules/x.js"] {
		t.Fatalf("ignored paths leaked: %+v", entries)
	}
	if !cloudworkspaceignore.ShouldIgnore(`src\.maclaw-cloud\x`, false, "") {
		t.Fatal("windows separators must still force-ignore .maclaw-cloud")
	}
}

func TestScanCloudWorkspaceLocalRejectsPortablePath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CON.txt"), []byte("reserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanCloudWorkspaceLocal(root); err == nil {
		t.Fatal("Windows-reserved paths should be rejected before upload")
	}
}

func TestCloudWorkspaceWatchIgnored(t *testing.T) {
	matcher := cloudworkspaceignore.NewMatcher("")
	if !cloudWorkspaceWatchIgnored("node_modules", true, matcher) {
		t.Fatal("node_modules dir should be ignored")
	}
	if !cloudWorkspaceWatchIgnored("node_modules/x.js", false, matcher) {
		t.Fatal("node_modules file should be ignored")
	}
	if !cloudWorkspaceWatchIgnored(".maclaw-cloud", true, matcher) {
		t.Fatal("cache dir should be ignored")
	}
	if cloudWorkspaceWatchIgnored("src", true, matcher) {
		t.Fatal("src should be watched")
	}
	if cloudWorkspaceWatchIgnored("src/main.go", false, matcher) {
		t.Fatal("tracked file should be watched")
	}
}

func TestAddCloudWorkspaceWatchRecursiveSkipsIgnoredDirs(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{"src", "node_modules", ".maclaw-cloud", filepath.Join("src", "nested")} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := addCloudWorkspaceWatchRecursive(w, root); err != nil {
		t.Fatal(err)
	}
	watched := map[string]bool{}
	for _, p := range w.WatchList() {
		watched[filepath.Clean(p)] = true
	}
	if !watched[filepath.Clean(root)] {
		t.Fatalf("root not watched: %v", w.WatchList())
	}
	if !watched[filepath.Clean(filepath.Join(root, "src"))] {
		t.Fatalf("src not watched: %v", w.WatchList())
	}
	if watched[filepath.Clean(filepath.Join(root, "node_modules"))] {
		t.Fatalf("node_modules must not be watched: %v", w.WatchList())
	}
	if watched[filepath.Clean(filepath.Join(root, ".maclaw-cloud"))] {
		t.Fatalf("cache dir must not be watched: %v", w.WatchList())
	}
}

func TestCreateTaskWithCloudWorkspaceTagsExplicitID(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", `D:\not\from\this\path`, "coding_dev", "cws_demo")
	if !projectRecordHasTagLike(created.Tags, cloudWorkspaceTag("cws_demo")) {
		t.Fatalf("missing cloud_workspace tag: %v", created.Tags)
	}
	want := normalizeProjectSessionPath(filepath.Join(app.GetDataDir(), "cloud-workspaces", "tenant_acme", "cws_demo"))
	if created.WorkingDir != want {
		t.Fatalf("working_dir=%q want %q", created.WorkingDir, want)
	}
	if lookupCloudWorkspaceIDByLocalPath(created.WorkingDir) != "cws_demo" {
		t.Fatal("process map localPath→workspaceID missing")
	}
	if lookupCloudWorkspaceIDByLocalPath(`D:\not\from\this\path`) != "" {
		t.Fatal("must not map caller workingDir as workspace id source")
	}
}

func TestHideTaskReleasesCloudWorkspaceLease(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_hide")
	if lookupHeldCloudWorkspace("cws_hide") == nil {
		t.Fatal("expected held mount")
	}
	app.HideTask(created.ProjectPath)
	if lookupHeldCloudWorkspace("cws_hide") != nil {
		t.Fatal("hide should release mount")
	}
	hub.mu.Lock()
	deleted := hub.deleted
	hub.mu.Unlock()
	if !deleted {
		t.Fatal("hide should DELETE lease")
	}
	if got := mustResumeCloudWorkspaceTask(t, app, "cws_hide"); got.ProjectPath != "" {
		t.Fatalf("resume after hide=%q", got.ProjectPath)
	}
}

func TestHeartbeat409MarksReadOnlyAndSkipsPush(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	prepared, err := app.PrepareCloudWorkspace("cws_stolen")
	if err != nil {
		t.Fatal(err)
	}
	mount := lookupHeldCloudWorkspace("cws_stolen")
	if mount == nil {
		t.Fatal("missing mount")
	}
	if err := os.WriteFile(filepath.Join(prepared.LocalPath, "after.txt"), []byte("no-upload"), 0o600); err != nil {
		t.Fatal(err)
	}
	applyCloudWorkspaceStolen(mount)
	if !mount.ReadOnly {
		t.Fatal("expected read-only")
	}
	if mount.watcher != nil {
		t.Fatal("watcher should stop")
	}
	hub.mu.Lock()
	before := len(hub.entries)
	hub.mu.Unlock()
	if err := app.ReleaseCloudWorkspace("cws_stolen"); err != nil {
		t.Fatalf("release: %v", err)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.entries) != before {
		t.Fatalf("stolen release must not push, entries=%+v", hub.entries)
	}
	if !hub.deleted {
		t.Fatal("release still DELETEs lease best-effort")
	}
}

func TestResumeCloudWorkspaceTaskRePreparesAfterRelease(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_resume")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.releaseCloudWorkspace(ctx, "cws_resume", false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if lookupHeldCloudWorkspace("cws_resume") != nil {
		t.Fatal("lease should be released after tab-close")
	}
	hub.mu.Lock()
	before := hub.leaseAcquires
	hub.mu.Unlock()
	resumed := mustResumeCloudWorkspaceTask(t, app, "cws_resume")
	if resumed.ProjectPath != created.ProjectPath {
		t.Fatalf("resume=%q want %q", resumed.ProjectPath, created.ProjectPath)
	}
	if lookupHeldCloudWorkspace("cws_resume") == nil {
		t.Fatal("resume must re-Prepare and hold the lease")
	}
	hub.mu.Lock()
	after := hub.leaseAcquires
	hub.mu.Unlock()
	if after <= before {
		t.Fatalf("resume must POST /leases again, before=%d after=%d", before, after)
	}
}

func TestResumeHeldMountSkipsSidecarRefresh(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{
		acquired: cloudWorkspaceAcquiredGranted,
		sidecars: map[string][]byte{cloudWorkspaceSidecarSession: []byte(`{"conversation":[]}`)},
	}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_held")
	hub.mu.Lock()
	before := hub.sidecarGets
	hub.mu.Unlock()
	if lookupHeldCloudWorkspace("cws_held") == nil {
		t.Fatal("create should hold the mount")
	}
	resumed := mustResumeCloudWorkspaceTask(t, app, "cws_held", created.ProjectPath)
	if resumed.ProjectPath != created.ProjectPath {
		t.Fatalf("resume=%q want %q", resumed.ProjectPath, created.ProjectPath)
	}
	hub.mu.Lock()
	after := hub.sidecarGets
	hub.mu.Unlock()
	if after != before {
		t.Fatalf("held resume must not re-fetch sidecars, before=%d after=%d", before, after)
	}
}

func TestResumeCloudWorkspaceTaskFindsTagAfterProcessMapClear(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_restart")
	resetCloudWorkspaceMounts()
	cloudWorkspaceDialogMu.Lock()
	cloudWorkspaceTaskByID = map[string]ProjectSearchResult{}
	cloudWorkspaceDialogMu.Unlock()
	if _, ok := lookupCloudWorkspaceTask("cws_restart"); ok {
		t.Fatal("process map should be empty after restart")
	}
	resumed := mustResumeCloudWorkspaceTask(t, app, "cws_restart")
	if resumed.ProjectPath != created.ProjectPath {
		t.Fatalf("tag resume=%q want %q", resumed.ProjectPath, created.ProjectPath)
	}
	again := mustCreateCloudWorkspaceTask(t, app, "云端任务重复", "", "coding_dev", "cws_restart")
	if again.ProjectPath != created.ProjectPath {
		t.Fatalf("create must reuse 1:1 task, got %q want %q", again.ProjectPath, created.ProjectPath)
	}
}

func TestHideTaskKeepsLeaseWhenPushFails(t *testing.T) {
	oldBackgroundDisabled := cloudWorkspaceBackgroundDisabled
	t.Cleanup(func() { cloudWorkspaceBackgroundDisabled = oldBackgroundDisabled })
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	cloudWorkspaceBackgroundDisabled = false
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_pushfail")
	if err := os.WriteFile(filepath.Join(created.WorkingDir, "dirty.txt"), []byte("unsynced"), 0o600); err != nil {
		t.Fatal(err)
	}
	hub.mu.Lock()
	hub.failPush = true
	hub.mu.Unlock()
	app.HideTask(created.ProjectPath)
	hub.mu.Lock()
	deleted := hub.deleted
	hub.mu.Unlock()
	if deleted {
		t.Fatal("failed Push must not DELETE the lease")
	}
	if lookupHeldCloudWorkspace("cws_pushfail") == nil {
		t.Fatal("failed Push must keep the held lease")
	}
	mount := lookupHeldCloudWorkspace("cws_pushfail")
	mount.mu.Lock()
	watcherResumed := mount.watcher != nil && !mount.stopped && !mount.releasing
	watcherPresent, stopped, releasing, readOnly := mount.watcher != nil, mount.stopped, mount.releasing, mount.ReadOnly
	mount.mu.Unlock()
	if !watcherResumed {
		t.Fatalf("failed release flush must resume watcher on the retained mount (present=%v stopped=%v releasing=%v readOnly=%v backgroundDisabled=%v)", watcherPresent, stopped, releasing, readOnly, cloudWorkspaceBackgroundDisabled)
	}
}

func TestCreateTaskWithCloudWorkspaceReturnsPrepareError(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{conflictUntilForce: true, acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	cloudWorkspaceConfirmStealFn = func(string) bool { return false }
	_, err := app.CreateTaskWithCloudWorkspace("云端任务", "", "coding_dev", "cws_busy")
	if err == nil || !strings.Contains(err.Error(), "占用") {
		t.Fatalf("create should surface in-use error, got %v", err)
	}
}

func TestPrepareSendsHeartbeatDuringSync(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	cloudWorkspaceBackgroundDisabled = false
	cloudWorkspaceHeartbeatIntervalValue = time.Hour
	t.Cleanup(func() {
		cloudWorkspaceBackgroundDisabled = true
		cloudWorkspaceHeartbeatIntervalValue = cloudWorkspaceHeartbeatInterval
	})
	if _, err := app.PrepareCloudWorkspace("cws_hb"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.Lock()
		n := hub.heartbeats
		hub.mu.Unlock()
		if n >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("prepare must heartbeat before Pull/Push finishes")
}

func TestResumeTaskPreparesCloudWorkspace(t *testing.T) {
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
	created := mustCreateCloudWorkspaceTask(t, app, "云端任务", "", "coding_dev", "cws_sidebar")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.releaseCloudWorkspace(ctx, "cws_sidebar", false); err != nil {
		t.Fatalf("release: %v", err)
	}
	if lookupHeldCloudWorkspace("cws_sidebar") != nil {
		t.Fatal("expected released mount")
	}
	if got := app.ResumeTask(created.ProjectPath); got == "" {
		t.Fatal("ResumeTask should succeed after re-Prepare")
	}
	if lookupHeldCloudWorkspace("cws_sidebar") == nil {
		t.Fatal("sidebar ResumeTask must re-Prepare the lease")
	}
}

func TestProtocolChunkedPutUses8MiBChunks(t *testing.T) {
	store := &memCloudWorkspaceTransport{objects: map[string][]byte{}}
	p := &cloudWorkspaceProtocol{Transport: store, MaxDirectPut: 4, MaxChunk: 4}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "big.bin"), []byte("abcdefghij"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := p.Push(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("entries=%+v", out.Entries)
	}
	if store.chunkCalls < 3 {
		t.Fatalf("chunkCalls=%d", store.chunkCalls)
	}
	dst := t.TempDir()
	if _, err := p.Pull(context.Background(), dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "big.bin"))
	if err != nil || string(got) != "abcdefghij" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

type memCloudWorkspaceTransport struct {
	revision   string
	entries    []cloudWorkspaceManifestEntry
	objects    map[string][]byte
	chunks     map[string]map[int][]byte
	chunkCalls int
}

func (m *memCloudWorkspaceTransport) GetManifest(ctx context.Context) (*cloudWorkspaceManifest, error) {
	entries := m.entries
	if entries == nil {
		entries = []cloudWorkspaceManifestEntry{}
	}
	return &cloudWorkspaceManifest{Revision: m.revision, Entries: entries}, nil
}

func (m *memCloudWorkspaceTransport) PutManifest(ctx context.Context, ifMatch string, entries []cloudWorkspaceManifestEntry) (*cloudWorkspaceManifest, error) {
	m.entries = entries
	m.revision = "rev-mem"
	return &cloudWorkspaceManifest{Revision: m.revision, Entries: entries}, nil
}

func (m *memCloudWorkspaceTransport) GetObject(ctx context.Context, sha string, size int64) ([]byte, error) {
	return m.objects[sha], nil
}

func (m *memCloudWorkspaceTransport) PutObject(ctx context.Context, sha string, data []byte) error {
	m.objects[sha] = append([]byte(nil), data...)
	return nil
}

func (m *memCloudWorkspaceTransport) PutChunk(ctx context.Context, sha string, index int, data []byte) error {
	m.chunkCalls++
	if m.chunks == nil {
		m.chunks = map[string]map[int][]byte{}
	}
	if m.chunks[sha] == nil {
		m.chunks[sha] = map[int][]byte{}
	}
	m.chunks[sha][index] = append([]byte(nil), data...)
	return nil
}

func (m *memCloudWorkspaceTransport) CompleteObject(ctx context.Context, sha string) error {
	var buf []byte
	for i := 0; ; i++ {
		part, ok := m.chunks[sha][i]
		if !ok {
			break
		}
		buf = append(buf, part...)
	}
	m.objects[sha] = buf
	return nil
}

func TestIsCloudWorkspaceCachePath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	cache := filepath.Join(dataDir, "cloud-workspaces", "tenant_acme", "cws_demo")
	if !isCloudWorkspaceCachePath(dataDir, cache) {
		t.Fatal("cache root")
	}
	if !isCloudWorkspaceCachePath(dataDir, filepath.Join(cache, "out.pdf")) {
		t.Fatal("cache file")
	}
	if isCloudWorkspaceCachePath(dataDir, filepath.Join(t.TempDir(), "maclaw", "workspace", "book")) {
		t.Fatal("default workspace is not a cloud cache")
	}
	decoy := filepath.Join(t.TempDir(), "cloud-workspaces", "tenant_acme", "cws_demo")
	if isCloudWorkspaceCachePath(dataDir, decoy) {
		t.Fatal("unrelated cloud-workspaces folder must not count")
	}
	if isCloudWorkspaceCachePath(dataDir, "") {
		t.Fatal("empty")
	}
}

func TestCloudWorkspaceExecutionDirDoesNotFollowDesktopDefault(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	desktop := filepath.Join(t.TempDir(), "desktop-default")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := app.SetTabWorkingDir("", desktop); err != nil {
		t.Fatalf("SetTabWorkingDir desktop: %v", err)
	}
	created := mustCreateCloudWorkspaceTask(t, app, "数学基础", "", "", "cws_math")
	owner := projectSessionOwnerID(created.ProjectPath)
	if got := app.EffectiveWorkingDirForOwner(owner); got != created.WorkingDir {
		t.Fatalf("unbound owner dir = %q, want cloud cache %q (desktop=%q)", got, created.WorkingDir, desktop)
	}
	if !isCloudWorkspaceCachePath(app.GetDataDir(), created.WorkingDir) {
		t.Fatalf("created working dir is not a cache path: %q", created.WorkingDir)
	}

	tabID := "proj-cloud-math"
	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); strings.TrimSpace(msg) == "" {
		t.Fatal("CreateProjectTabSession returned empty")
	}
	if got := app.GetTabWorkingDir(tabID)["path"]; got != created.WorkingDir {
		t.Fatalf("tab working dir = %q, want cache %q", got, created.WorkingDir)
	}

	pdfName := "人工智能数学基础-v1.1.0.pdf"
	if err := os.WriteFile(filepath.Join(created.WorkingDir, pdfName), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	listed, err := app.GetCodingWorkbenchDirectory(created.ProjectPath, "")
	if err != nil {
		t.Fatalf("GetCodingWorkbenchDirectory after write: %v", err)
	}
	found := false
	for _, entry := range listed.Entries {
		if entry.Name == pdfName && !entry.IsDir {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("preview listing missing %q: %+v (root=%q)", pdfName, listed.Entries, listed.Root)
	}

	h := &IMMessageHandler{app: app}
	if got := h.effectiveWorkingDirForUser(owner); got != created.WorkingDir {
		t.Fatalf("skill/tool working dir = %q, want cache %q", got, created.WorkingDir)
	}
}

func TestCreateProjectTabSessionRepairsStaleCloudWorkingDir(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	desktop := filepath.Join(t.TempDir(), "stale-desktop")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := app.SetTabWorkingDir("", desktop); err != nil {
		t.Fatalf("SetTabWorkingDir desktop: %v", err)
	}
	created := mustCreateCloudWorkspaceTask(t, app, "云端成果", "", "", "cws_stale")
	tabID := "proj-cloud-stale"
	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); strings.TrimSpace(msg) == "" {
		t.Fatal("CreateProjectTabSession returned empty")
	}

	persist := app.ensureProjectTabSessionPersist()
	session, err := persist.LoadSession(tabID)
	if err != nil || session == nil {
		t.Fatalf("LoadSession: %v %#v", err, session)
	}
	session.WorkingDir = desktop
	if err := persist.SaveSession(session); err != nil {
		t.Fatalf("SaveSession stale dir: %v", err)
	}
	app.tabWorkingDirOverrides.Store(tabID, desktop)
	app.assistantSessionWorkingDirs.Store(projectSessionOwnerID(created.ProjectPath), desktop)

	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); strings.TrimSpace(msg) == "" {
		t.Fatal("reopen CreateProjectTabSession returned empty")
	}
	if got := app.GetTabWorkingDir(tabID)["path"]; got != created.WorkingDir {
		t.Fatalf("repaired tab dir = %q, want cache %q", got, created.WorkingDir)
	}
	if got := app.EffectiveWorkingDirForOwner(projectSessionOwnerID(created.ProjectPath)); got != created.WorkingDir {
		t.Fatalf("repaired owner dir = %q, want cache %q", got, created.WorkingDir)
	}
	reloaded, err := persist.LoadSession(tabID)
	if err != nil || reloaded == nil {
		t.Fatalf("reloaded session: %v %#v", err, reloaded)
	}
	if normalizeProjectSessionPath(reloaded.WorkingDir) != created.WorkingDir {
		t.Fatalf("persisted session working dir = %q, want cache %q", reloaded.WorkingDir, created.WorkingDir)
	}
}

func TestCloudWorkspacePreviewAndToolsShareCacheAfterStaleWorkingDirTag(t *testing.T) {
	app := newCloudWorkspaceMountTestApp(t, &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted})
	desktop := filepath.Join(t.TempDir(), "stale-tag-desktop")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(desktop, "local-only.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := mustCreateCloudWorkspaceTask(t, app, "stale tag", "", "", "cws_tag")
	if err := app.persistTaskWorkingDir(created.ProjectPath, desktop); err != nil {
		t.Fatalf("persistTaskWorkingDir: %v", err)
	}
	pdfName := "book.pdf"
	if err := os.WriteFile(filepath.Join(created.WorkingDir, pdfName), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	owner := projectSessionOwnerID(created.ProjectPath)
	if got := app.EffectiveWorkingDirForOwner(owner); got != created.WorkingDir {
		t.Fatalf("owner dir = %q, want cache %q after stale tag", got, created.WorkingDir)
	}
	listed, err := app.GetCodingWorkbenchDirectory(created.ProjectPath, "")
	if err != nil {
		t.Fatalf("GetCodingWorkbenchDirectory: %v", err)
	}
	foundPDF, foundLocal := false, false
	for _, entry := range listed.Entries {
		if entry.Name == pdfName {
			foundPDF = true
		}
		if entry.Name == "local-only.txt" {
			foundLocal = true
		}
	}
	if !foundPDF || foundLocal {
		t.Fatalf("listing should be cache (pdf=%v local=%v entries=%+v)", foundPDF, foundLocal, listed.Entries)
	}

	tabID := "proj-cloud-outside-dir"
	if msg := app.CreateProjectTabSession(tabID, created.ProjectPath); strings.TrimSpace(msg) == "" {
		t.Fatal("CreateProjectTabSession returned empty")
	}
	outside := filepath.Join(t.TempDir(), "user-picked")
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("precondition: outside path should not exist, err=%v", err)
	}
	if err := app.SetTabWorkingDir(tabID, outside); err != nil {
		t.Fatalf("SetTabWorkingDir: %v", err)
	}
	if got := app.GetTabWorkingDir(tabID)["path"]; got != created.WorkingDir {
		t.Fatalf("SetTabWorkingDir outside cache = %q, want cache %q", got, created.WorkingDir)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("rejected out-of-cache pick must not be created, err=%v", err)
	}
}
