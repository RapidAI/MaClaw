package cloudworkspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestParseTaskSidecar(t *testing.T) {
	got := ParseTaskSidecar([]byte(`{"name":"跨设备任务","mode":"coding_dev","tag":"cloud_workspace:cws_1"}`))
	if got.Name != "跨设备任务" || got.Mode != "coding_dev" || got.Tag != "cloud_workspace:cws_1" {
		t.Fatalf("got=%+v", got)
	}
	if empty := ParseTaskSidecar(nil); empty.Name != "" || empty.Mode != "" {
		t.Fatalf("empty=%+v", empty)
	}
}

func TestValidateSidecarName(t *testing.T) {
	for _, name := range []string{SidecarSession, SidecarTask, SidecarWorkbench, SidecarCheckpoint} {
		got, err := ValidateSidecarName(name)
		if err != nil || got != name {
			t.Fatalf("name %q: got %q err=%v", name, got, err)
		}
	}
	for _, name := range []string{"", "secret.json", "../task.json", "SESSION.JSON", "sidecars/task.json", ".coding_workbench.json"} {
		if _, err := ValidateSidecarName(name); err != ErrInvalidSidecarName {
			t.Fatalf("name %q err=%v", name, err)
		}
	}
}

// writeTestSidecar stages and commits a canonical sidecar file for tests that
// only need fixture content on disk.
func writeTestSidecar(t *testing.T, bs *BlobStore, tenantID, userID, workspaceID, name string, plaintext []byte) {
	t.Helper()
	path, err := bs.SidecarPath(tenantID, userID, workspaceID, name)
	if err != nil {
		t.Fatal(err)
	}
	staged, err := bs.stageSidecarFile(context.Background(), tenantID, userID, workspaceID, name, path, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if err := fileutil.RenameAtomicFile(staged, path); err != nil {
		t.Fatal(err)
	}
}

func TestBlobStoreSidecarEncryptedAtRest(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	plain := []byte(`{"name":"标书","mode":"coding_dev","tag":"cloud_workspace:cws_one"}`)
	writeTestSidecar(t, bs, "t1", "u1", "cws_one", SidecarTask, plain)
	path, err := bs.SidecarPath("t1", "u1", "cws_one", SidecarTask)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, SidecarTask+".enc") {
		t.Fatalf("path=%s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, plain) {
		t.Fatal("disk sidecar leaked plaintext")
	}
	out, err := bs.GetSidecar(ctx, "t1", "u1", "cws_one", SidecarTask)
	if err != nil || !bytes.Equal(out, plain) {
		t.Fatalf("get=%q err=%v", out, err)
	}
	if _, err := bs.GetSidecar(ctx, "t1", "u1", "cws_one", SidecarSession); err != ErrBlobNotFound {
		t.Fatalf("missing session err=%v", err)
	}
	if _, err := bs.GetSidecar(ctx, "t1", "u1", "cws_other", SidecarTask); err != ErrBlobNotFound {
		t.Fatalf("other workspace err=%v", err)
	}
	if _, err := bs.SidecarPath("t1", "u1", "cws_one", "nope.json"); err != ErrInvalidSidecarName {
		t.Fatalf("invalid name err=%v", err)
	}
}

func TestSidecarCompressionRoundTrip(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	plain := bytes.Repeat([]byte("conversation turn\n"), 4096)
	writeTestSidecar(t, bs, "t1", "u1", "cws_one", SidecarSession, plain)
	got, err := bs.GetSidecar(context.Background(), "t1", "u1", "cws_one", SidecarSession)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip len=%d err=%v", len(got), err)
	}
}

// A second write fully replaces the previous content. v1-sequential has a
// single lease-guarded writer, so no session-history merge remains.
func TestSidecarWriteReplacesExistingContent(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	first := []byte(`{"conversation":[{"id":"a"}]}`)
	second := []byte(`{"conversation":[{"id":"b"}],"cleared_at":100}`)
	writeTestSidecar(t, bs, "t1", "u1", "cws_one", SidecarSession, first)
	writeTestSidecar(t, bs, "t1", "u1", "cws_one", SidecarSession, second)
	got, err := bs.GetSidecar(ctx, "t1", "u1", "cws_one", SidecarSession)
	if err != nil || !bytes.Equal(got, second) {
		t.Fatalf("replaced=%s err=%v", got, err)
	}
}

func TestServiceSidecarRevisionCAS(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "sidecar-cas", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	svc := &Service{Workspaces: st, Blobs: &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}}
	p := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1", FencingToken: lease.FencingToken}
	// First create requires an empty If-Match.
	if _, err := svc.PutSidecarWithRevision(ctx, p, ws.ID, SidecarTask, "not-empty", []byte(`{"name":"one"}`)); err != ErrRevisionConflict {
		t.Fatalf("first create with non-empty If-Match err=%v", err)
	}
	first, err := svc.PutSidecarWithRevision(ctx, p, ws.ID, SidecarTask, "", []byte(`{"name":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision == "" {
		t.Fatal("missing sidecar revision")
	}
	// The atomic write is immediately readable through the canonical file.
	if got, err := svc.Blobs.GetSidecar(ctx, "t1", "u1", ws.ID, SidecarTask); err != nil || string(got) != `{"name":"one"}` {
		t.Fatalf("readback=%q err=%v", got, err)
	}
	if _, err := svc.PutSidecarWithRevision(ctx, p, ws.ID, SidecarTask, first.Revision+"stale", []byte(`{"name":"two"}`)); err != ErrRevisionConflict {
		t.Fatalf("stale CAS err=%v", err)
	}
	second, err := svc.PutSidecarWithRevision(ctx, p, ws.ID, SidecarTask, first.Revision, []byte(`{"name":"two"}`))
	if err != nil || second.Revision == first.Revision {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	// The metadata row tracks the committed revision.
	var stored string
	if err := st.db.QueryRowContext(ctx, `SELECT revision FROM cloud_workspace_sidecars WHERE workspace_id = ? AND name = ?`, ws.ID, SidecarTask).Scan(&stored); err != nil || stored != second.Revision {
		t.Fatalf("stored revision=%q err=%v", stored, err)
	}
	if got, err := svc.Blobs.GetSidecar(ctx, "t1", "u1", ws.ID, SidecarTask); err != nil || string(got) != `{"name":"two"}` {
		t.Fatalf("updated=%q err=%v", got, err)
	}
}

func TestServiceSidecarPutRequiresLease(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "sidecar-lease", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	svc := &Service{Workspaces: st, Blobs: &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}}
	p := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}
	if _, err := svc.PutSidecarWithRevision(ctx, p, ws.ID, SidecarTask, "", []byte(`{"name":"one"}`)); err == nil {
		t.Fatal("write without a lease must fail")
	}
	if _, err := svc.Blobs.GetSidecar(ctx, "t1", "u1", ws.ID, SidecarTask); err != ErrBlobNotFound {
		t.Fatalf("sidecar must not exist after rejected write, err=%v", err)
	}
}

// A canonical file written before CAS metadata existed still works: its
// content hash is the revision and a CAS update replaces it in place.
func TestSidecarCASBootstrapsLegacyCanonicalFile(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "legacy-sidecar", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	legacy := []byte(`{"name":"legacy"}`)
	writeTestSidecar(t, bs, "t1", "u1", ws.ID, SidecarTask, legacy)
	got, err := bs.GetSidecarWithRevision(ctx, "t1", "u1", ws.ID, SidecarTask)
	if err != nil || string(got.Data) != string(legacy) {
		t.Fatalf("legacy get=%+v err=%v", got, err)
	}
	svc := &Service{Workspaces: st, Blobs: bs}
	p := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1", FencingToken: lease.FencingToken}
	if _, err := svc.PutSidecarWithRevision(ctx, p, ws.ID, SidecarTask, got.Revision, []byte(`{"name":"next"}`)); err != nil {
		t.Fatal(err)
	}
	updated, err := bs.GetSidecar(ctx, "t1", "u1", ws.ID, SidecarTask)
	if err != nil || string(updated) != `{"name":"next"}` {
		t.Fatalf("updated=%q err=%v", updated, err)
	}
}

// A failed metadata transaction must never destroy the previously committed
// canonical sidecar content: the new payload is staged in a sibling temp file
// and only renamed into place after the commit.
func TestSidecarFailedCommitKeepsPreviousCanonicalContent(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "sidecar-commit-fail", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	svc := &Service{Workspaces: st, Blobs: &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}}
	p := auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1", FencingToken: lease.FencingToken}

	first, err := svc.PutSidecarWithRevision(ctx, p, ws.ID, SidecarTask, "", []byte(`{"name":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Break only the sidecar metadata table so the finalize transaction fails
	// after the payload has been staged.
	if _, err := st.db.Exec(`DROP TABLE cloud_workspace_sidecars`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutSidecarWithRevision(ctx, p, ws.ID, SidecarTask, first.Revision, []byte(`{"name":"two"}`)); err == nil {
		t.Fatal("write with broken metadata table must fail")
	}
	got, err := svc.Blobs.GetSidecar(ctx, "t1", "u1", ws.ID, SidecarTask)
	if err != nil || string(got) != `{"name":"one"}` {
		t.Fatalf("canonical after failed commit=%q err=%v, want previous content", got, err)
	}
	// No staged temp file may survive the failure.
	dir, err := svc.Blobs.SidecarsDir("t1", "u1", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "stage") {
			t.Fatalf("staged temp file left behind: %s", e.Name())
		}
	}
}

// Pre-11.31 deployments kept the committed content in an immutable revision
// file while the canonical file was only a best-effort copy. When the
// canonical file is missing, reads fall back to the committed DB pointer.
func TestSidecarReadFallsBackToLegacyRevisionFile(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	content := []byte(`{"name":"committed-via-revision-file"}`)
	writeTestSidecar(t, bs, "t1", "u1", "cws_legacy", SidecarTask, content)
	revision := sidecarRevision(content)
	canonical, err := bs.SidecarPath("t1", "u1", "cws_legacy", SidecarTask)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(filepath.Dir(canonical), SidecarTask+"."+revision+".enc")
	if err := os.Rename(canonical, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_sidecars (workspace_id, name, revision, updated_at) VALUES (?, ?, ?, ?)`,
		"cws_legacy", SidecarTask, revision, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	got, err := bs.GetSidecarWithRevision(ctx, "t1", "u1", "cws_legacy", SidecarTask)
	if err != nil {
		t.Fatalf("legacy fallback read: %v", err)
	}
	if string(got.Data) != string(content) || got.Revision != revision {
		t.Fatalf("legacy fallback=%+v", got)
	}
	// A tampered pointer must not resolve to an attacker-chosen path.
	if _, err := st.db.Exec(`UPDATE cloud_workspace_sidecars SET revision = ? WHERE workspace_id = ?`, "../../evil", "cws_legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.GetSidecar(ctx, "t1", "u1", "cws_legacy", SidecarTask); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("tampered revision err=%v, want ErrBlobNotFound", err)
	}
}
