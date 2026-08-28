package cloudworkspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func newTestSyncEnv(t *testing.T) (*Service, *Workspace, auth.MachinePrincipal) {
	t.Helper()
	st, _ := newTestWorkspaceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	insertTestMachine(t, st, "m1", "u1", "HOST-M1")
	ws, err := st.Create(ctx, CreateParams{TenantID: "t1", UserID: "u1", Name: "A", Quota: 5, TenantMaxTotalBytes: 1 << 30}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(ctx, acquireParams(ws.ID, "m1"), now); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	svc := &Service{
		Workspaces: st,
		Blobs:      &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db},
	}
	return svc, ws, auth.MachinePrincipal{TenantID: "t1", UserID: "u1", MachineID: "m1"}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProtocolPushPullRoundTrip(t *testing.T) {
	svc, ws, principal := newTestSyncEnv(t)
	p := &Protocol{Transport: &ServiceTransport{Service: svc, Principal: principal, WorkspaceID: ws.ID}}
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"readme.md":         "# hi",
		"src/main.go":       "package main\n",
		"node_modules/x.js": "ignored",
	})
	pushed, err := p.Push(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(pushed.Entries) != 2 {
		t.Fatalf("entries=%+v", pushed.Entries)
	}
	state, err := ReadLocalState(src)
	if err != nil || state.LastPushedRevision != pushed.Revision {
		t.Fatalf("state=%+v err=%v", state, err)
	}

	dst := t.TempDir()
	writeTree(t, dst, map[string]string{"extra.txt": "gone"})
	pulled, err := p.Pull(context.Background(), dst)
	if err != nil {
		t.Fatal(err)
	}
	if pulled.Revision != pushed.Revision {
		t.Fatalf("rev %q vs %q", pulled.Revision, pushed.Revision)
	}
	if _, err := os.Stat(filepath.Join(dst, "extra.txt")); !os.IsNotExist(err) {
		t.Fatal("pull should delete extra local file")
	}
	got, err := os.ReadFile(filepath.Join(dst, "src", "main.go"))
	if err != nil || string(got) != "package main\n" {
		t.Fatalf("main.go=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "node_modules", "x.js")); !os.IsNotExist(err) {
		t.Fatal("ignored path should not be pulled")
	}
}

func TestProtocolRenewedPushesWithoutPull(t *testing.T) {
	svc, ws, principal := newTestSyncEnv(t)
	p := &Protocol{Transport: &ServiceTransport{Service: svc, Principal: principal, WorkspaceID: ws.ID}}
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "one"})
	if _, err := p.Push(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{"b.txt": "two"})
	out, err := p.SyncAfterAcquire(context.Background(), root, AcquiredRenewed)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("renewed push entries=%+v", out.Entries)
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err != nil {
		t.Fatal("renewed must not pull-delete local dirty file")
	}

	other := t.TempDir()
	writeTree(t, other, map[string]string{"a.txt": "one", "b.txt": "stale local"})
	if _, err := p.SyncAfterAcquire(context.Background(), other, AcquiredGranted); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(other, "b.txt"))
	if err != nil || string(got) != "two" {
		t.Fatalf("granted pull should take server tree, got %q err=%v", got, err)
	}
}

type pullStubTransport struct {
	man *Manifest
	obj map[string][]byte
}

func (p pullStubTransport) GetManifest(context.Context) (*Manifest, error) { return p.man, nil }
func (p pullStubTransport) PutManifest(context.Context, string, []ManifestEntry) (*Manifest, error) {
	return nil, ErrUnavailable
}
func (p pullStubTransport) GetObject(_ context.Context, sha string) ([]byte, error) {
	return p.obj[sha], nil
}
func (p pullStubTransport) PutObject(context.Context, string, []byte) error { return ErrUnavailable }
func (p pullStubTransport) PutChunk(context.Context, string, int, []byte) error {
	return ErrUnavailable
}
func (p pullStubTransport) CompleteObject(context.Context, string) error { return ErrUnavailable }

func TestProtocolPullRejectsDotDotPath(t *testing.T) {
	p := &Protocol{Transport: pullStubTransport{
		man: &Manifest{Revision: "1", Entries: []ManifestEntry{{Path: "../secret.txt", SHA256: "abc", Size: 1}}},
	}}
	if _, err := p.Pull(context.Background(), t.TempDir()); err == nil {
		t.Fatal("pull must reject .. path")
	}
}

func TestProtocolPullRejectsHashMismatch(t *testing.T) {
	p := &Protocol{Transport: pullStubTransport{
		man: &Manifest{Revision: "1", Entries: []ManifestEntry{{Path: "a.txt", SHA256: "00", Size: 4}}},
		obj: map[string][]byte{"00": []byte("nope")},
	}}
	if _, err := p.Pull(context.Background(), t.TempDir()); err != ErrBlobHashMismatch {
		t.Fatalf("err=%v want ErrBlobHashMismatch", err)
	}
}

func TestProtocolChunkedPut(t *testing.T) {
	svc, ws, principal := newTestSyncEnv(t)
	p := &Protocol{
		Transport:    &ServiceTransport{Service: svc, Principal: principal, WorkspaceID: ws.ID},
		MaxDirectPut: 4,
		MaxChunk:     4,
	}
	root := t.TempDir()
	writeTree(t, root, map[string]string{"big.bin": "abcdefghij"})
	out, err := p.Push(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 1 {
		t.Fatalf("entries=%+v", out.Entries)
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
