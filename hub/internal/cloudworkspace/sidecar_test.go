package cloudworkspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestBlobStoreSidecarEncryptedAtRest(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	plain := []byte(`{"name":"标书","mode":"coding_dev","tag":"cloud_workspace:cws_one"}`)
	if err := bs.PutSidecar(ctx, "t1", "u1", "cws_one", SidecarTask, plain); err != nil {
		t.Fatal(err)
	}
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
	if err := bs.PutSidecar(ctx, "t1", "u1", "cws_one", "nope.json", plain); err != ErrInvalidSidecarName {
		t.Fatalf("invalid name err=%v", err)
	}
}
