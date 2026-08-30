package cloudworkspace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestSidecarCompressionRoundTrip(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	plain := bytes.Repeat([]byte("conversation turn\n"), 4096)
	if err := bs.PutSidecar(context.Background(), "t1", "u1", "cws_one", SidecarSession, plain); err != nil {
		t.Fatal(err)
	}
	got, err := bs.GetSidecar(context.Background(), "t1", "u1", "cws_one", SidecarSession)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("round trip len=%d err=%v", len(got), err)
	}
}

func TestSessionSidecarWriteMergesExistingHistory(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	first, _ := json.Marshal(map[string]any{"conversation": []any{map[string]any{"id": "a"}}})
	second, _ := json.Marshal(map[string]any{"conversation": []any{map[string]any{"id": "b"}}})
	if err := bs.PutSidecar(ctx, "t1", "u1", "cws_one", SidecarSession, first); err != nil {
		t.Fatal(err)
	}
	if err := bs.PutSidecar(ctx, "t1", "u1", "cws_one", SidecarSession, second); err != nil {
		t.Fatal(err)
	}
	got, err := bs.GetSidecar(ctx, "t1", "u1", "cws_one", SidecarSession)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Conversation []map[string]any `json:"conversation"`
	}
	if err := json.Unmarshal(got, &payload); err != nil || len(payload.Conversation) != 2 {
		t.Fatalf("merged=%s err=%v", got, err)
	}
}
