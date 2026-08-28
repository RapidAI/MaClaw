package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	mu                 sync.Mutex
	leaseID            string
	acquired           string
	conflictUntilForce bool
	forceCount         int
	leaseAcquires      int
	heartbeats         int
	heartbeatStatus    int
	deleted            bool
	failPush           bool
	revision           string
	entries            []cloudWorkspaceManifestEntry
	objects            map[string][]byte
	chunks             map[string]map[int][]byte
	sidecars           map[string][]byte
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
	if h.acquired == "" {
		h.acquired = cloudWorkspaceAcquiredGranted
	}
	if h.heartbeatStatus == 0 {
		h.heartbeatStatus = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	switch {
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
			LeaseID:   h.leaseID,
			ExpiresAt: "2099-01-01T00:00:00Z",
			Acquired:  acquired,
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
		_ = json.NewEncoder(w).Encode(map[string]any{"lease_id": h.leaseID, "expires_at": "2099-01-01T00:00:00Z"})
	case r.Method == http.MethodDelete && strings.Contains(path, "/leases/"):
		h.deleted = true
		_ = json.NewEncoder(w).Encode(map[string]any{"released": true})
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/manifest"):
		entries := h.entries
		if entries == nil {
			entries = []cloudWorkspaceManifestEntry{}
		}
		_ = json.NewEncoder(w).Encode(cloudWorkspaceManifest{Revision: h.revision, Entries: entries})
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
	default:
		http.NotFound(w, r)
	}
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
	t.Cleanup(resetCloudWorkspaceDialogMocks)
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
	hub := &fakeCloudWorkspaceHub{acquired: cloudWorkspaceAcquiredGranted}
	app := newCloudWorkspaceMountTestApp(t, hub)
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
