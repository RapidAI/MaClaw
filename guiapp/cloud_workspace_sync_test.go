package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type cloudWorkspaceMutationTransport struct {
	root       string
	entries    []cloudWorkspaceManifestEntry
	object     []byte
	mutateRead bool
	mutateGet  bool
	putCalls   int
}

func (t *cloudWorkspaceMutationTransport) GetManifest(context.Context) (*cloudWorkspaceManifest, error) {
	if t.mutateRead {
		_ = os.WriteFile(filepath.Join(t.root, "a.txt"), []byte("edited-after-scan"), 0o600)
	}
	return &cloudWorkspaceManifest{Revision: "rev-1", Entries: t.entries}, nil
}

func (t *cloudWorkspaceMutationTransport) PutManifest(context.Context, string, []cloudWorkspaceManifestEntry) (*cloudWorkspaceManifest, error) {
	return nil, nil
}

func (t *cloudWorkspaceMutationTransport) GetObject(context.Context, string, int64) ([]byte, error) {
	if t.mutateGet {
		_ = os.WriteFile(filepath.Join(t.root, "a.txt"), []byte("edited-during-download"), 0o600)
	}
	return append([]byte(nil), t.object...), nil
}

func (t *cloudWorkspaceMutationTransport) PutObject(context.Context, string, []byte) error {
	t.putCalls++
	return nil
}

func (t *cloudWorkspaceMutationTransport) PutChunk(context.Context, string, int, []byte) error {
	t.putCalls++
	return nil
}

func (t *cloudWorkspaceMutationTransport) CompleteObject(context.Context, string) error { return nil }

func TestCloudWorkspacePushRejectsFileChangedAfterScan(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("before"))
	transport := &cloudWorkspaceMutationTransport{root: root, mutateRead: true, entries: []cloudWorkspaceManifestEntry{{Path: "a.txt", SHA256: hex.EncodeToString(sum[:]), Size: int64(len("before"))}}}
	_, err := (&cloudWorkspaceProtocol{Transport: transport}).Push(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "changed while uploading") {
		t.Fatalf("Push err=%v, want changed-file rejection", err)
	}
	if transport.putCalls != 0 {
		t.Fatalf("changed file must not be uploaded, put calls=%d", transport.putCalls)
	}
}

func TestCloudWorkspacePullRejectsFileChangedDuringDownload(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	remote := []byte("remote")
	sum := sha256.Sum256(remote)
	transport := &cloudWorkspaceMutationTransport{
		root: root, mutateGet: true,
		entries: []cloudWorkspaceManifestEntry{{Path: "a.txt", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(remote))}},
		object:  remote,
	}
	_, err := (&cloudWorkspaceProtocol{Transport: transport}).Pull(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "changed while downloading") {
		t.Fatalf("Pull err=%v, want changed-file rejection", err)
	}
	got, readErr := os.ReadFile(filepath.Join(root, "a.txt"))
	if readErr != nil || string(got) != "edited-during-download" {
		t.Fatalf("concurrent local edit was overwritten: %q err=%v", got, readErr)
	}
}

func TestCloudWorkspacePullRejectsSymlinkedParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "sub")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	remote := []byte("remote")
	sum := sha256.Sum256(remote)
	transport := &cloudWorkspaceMutationTransport{
		entries: []cloudWorkspaceManifestEntry{{Path: "sub/a.txt", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(remote))}},
		object:  remote,
	}
	if _, err := (&cloudWorkspaceProtocol{Transport: transport}).Pull(context.Background(), root); err == nil {
		t.Fatal("Pull must reject a symlinked parent directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("Pull wrote through symlink into outside directory: err=%v", err)
	}
}

func TestCloudWorkspaceHashIndexReusesUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := scanCloudWorkspaceLocalContextWithOptions(context.Background(), root, cloudWorkspaceScanOptions{})
	if err != nil || len(first) != 1 {
		t.Fatalf("first scan entries=%v err=%v", first, err)
	}
	secondProgress := cloudWorkspaceScanProgress{}
	second, err := scanCloudWorkspaceLocalContextWithOptions(context.Background(), root, cloudWorkspaceScanOptions{Progress: func(p cloudWorkspaceScanProgress) { secondProgress = p }})
	if err != nil || len(second) != 1 {
		t.Fatalf("second scan entries=%v err=%v", second, err)
	}
	if secondProgress.FilesReused != 1 || secondProgress.FilesScanned != 0 {
		t.Fatalf("progress=%+v, want one reused file and no rehash", secondProgress)
	}
	if _, err := os.Stat(cloudWorkspaceHashIndexPath(root)); err != nil {
		t.Fatalf("hash index missing: %v", err)
	}
}

func TestCloudWorkspaceReadStateDoesNotCreateMissingCache(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "missing-cache")
	if _, err := readCloudWorkspaceLocalState(root); err != nil {
		t.Fatalf("read missing state: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("read-only state inspection created cache root: err=%v", err)
	}
}

func TestCloudWorkspaceHashIndexInvalidatesOnWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanCloudWorkspaceLocal(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	progress := cloudWorkspaceScanProgress{}
	entries, err := scanCloudWorkspaceLocalContextWithOptions(context.Background(), root, cloudWorkspaceScanOptions{Progress: func(p cloudWorkspaceScanProgress) { progress = p }})
	if err != nil || len(entries) != 1 {
		t.Fatalf("scan entries=%v err=%v", entries, err)
	}
	if progress.FilesScanned != 1 || progress.FilesReused != 0 {
		t.Fatalf("progress=%+v, want rehash after write", progress)
	}
	if entries[0].SHA256 == "" {
		t.Fatal("updated entry has empty hash")
	}
}

func TestCloudWorkspaceHashIndexCorruptFallsBackToHash(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(cloudWorkspaceHashIndexPath(root)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cloudWorkspaceHashIndexPath(root), []byte(`{"version":999,"entries":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	progress := cloudWorkspaceScanProgress{}
	if _, err := scanCloudWorkspaceLocalContextWithOptions(context.Background(), root, cloudWorkspaceScanOptions{Progress: func(p cloudWorkspaceScanProgress) { progress = p }}); err != nil {
		t.Fatal(err)
	}
	if progress.FilesScanned != 1 || progress.FilesReused != 0 {
		t.Fatalf("progress=%+v, corrupt index should force hash", progress)
	}
	var idx cloudWorkspaceHashIndex
	raw, err := os.ReadFile(cloudWorkspaceHashIndexPath(root))
	if err != nil || json.Unmarshal(raw, &idx) != nil || idx.Version != cloudWorkspaceHashIndexVersion {
		t.Fatalf("index not repaired: raw=%s err=%v", raw, err)
	}
}

func TestCloudWorkspaceAuditIsRedactedAndRotates(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	for i := 0; i < 2; i++ {
		app.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: "cws-audit", Operation: "download", Outcome: "failed", Detail: `open C:\\secret\\token.txt: access denied`, Bytes: 42})
	}
	path := cloudWorkspaceAuditPath(app)
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `"workspace_id":"cws-audit"`) {
		t.Fatalf("audit log missing: err=%v raw=%s", err, raw)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "token.txt") || strings.Contains(string(raw), "access denied") {
		t.Fatal("audit log should not contain path or free-form error detail")
	}
	if !strings.Contains(string(raw), `"detail":"permission_denied"`) {
		t.Fatalf("audit log should use a stable reason code: %s", raw)
	}
	app.recordCloudWorkspaceAudit(cloudWorkspaceAuditEvent{WorkspaceID: `C:\\secret\\workspace`, Operation: "download"})
	raw, err = os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), `C:\\secret`) || !strings.Contains(string(raw), `"workspace_id":"sha256:`) {
		t.Fatalf("audit workspace id should be hashed when unsafe: err=%v raw=%s", err, raw)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("audit log permissions too broad: err=%v info=%v", err, info)
		}
	}
}

func TestCancelCloudWorkspaceSyncCancelsRunningPass(t *testing.T) {
	resetCloudWorkspaceMounts()
	ctx, cancel := context.WithCancel(context.Background())
	mount := &cloudWorkspaceHeldMount{WorkspaceID: "cws-cancel", LocalPath: t.TempDir(), syncCancel: cancel, syncRunning: true}
	storeCloudWorkspaceMount(mount)
	if err := (&App{}).CancelCloudWorkspaceSync("cws-cancel"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("sync context was not canceled")
	}
	resetCloudWorkspaceMounts()
}

func TestCloudWorkspaceLocalScanHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanCloudWorkspaceLocalContext(ctx, root); err != context.Canceled {
		t.Fatalf("scan error=%v, want context.Canceled", err)
	}
}

func TestCloudWorkspaceSafeRelPath(t *testing.T) {
	okCases := []string{"a.txt", "src/main.go", "docs/readme.md"}
	for _, p := range okCases {
		got, ok := cloudWorkspaceSafeRelPath(p)
		if !ok || got != p {
			t.Fatalf("path %q ok=%v got=%q", p, ok, got)
		}
	}
	bad := []string{"", "../secret", "foo/../bar", "/abs", `C:\windows`, `a\b`, "foo//bar", "CON.txt", "trailing.", "trailing ", " leading"}
	for _, p := range bad {
		if _, ok := cloudWorkspaceSafeRelPath(p); ok {
			t.Fatalf("path %q should be rejected", p)
		}
	}
}

func TestCloudWorkspaceEmptyManifestBaselinePreservesLaterLocalChanges(t *testing.T) {
	root := t.TempDir()
	remote := &cloudWorkspaceManifest{Revision: "", Entries: []cloudWorkspaceManifestEntry{}}
	if err := writeCloudWorkspaceManifestState(root, remote); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "offline.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	kind, err := cloudWorkspaceCacheSyncPlan(root, remote, false)
	if err != nil {
		t.Fatal(err)
	}
	if kind != cloudWorkspaceSyncPush {
		t.Fatalf("sync plan=%s, want push for a known empty baseline", kind)
	}
}

func TestCloudWorkspaceUnknownCacheStillPullsOnFirstOpen(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "leftover.txt"), []byte("unknown"), 0o600); err != nil {
		t.Fatal(err)
	}
	kind, err := cloudWorkspaceCacheSyncPlan(root, &cloudWorkspaceManifest{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if kind != cloudWorkspaceSyncPull {
		t.Fatalf("sync plan=%s, want pull for a cache with no durable baseline", kind)
	}
}
