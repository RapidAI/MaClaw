package cloudworkspace

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestApplyOperationIdempotentAndPerFileConflict(t *testing.T) {
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "ops.db"), WAL: true, BusyTimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatal(err)
	}
	st := NewStore(provider.Write)
	ws, err := st.Create(context.Background(), CreateParams{TenantID: "t1", UserID: "u1", Name: "ops", Quota: 2, TenantMaxTotalBytes: 1 << 30}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := provider.Write.Exec(`INSERT INTO cloud_workspace_objects(workspace_id,sha256,size_bytes,plain_size_bytes,stored_size_bytes,compression,created_at) VALUES(?,?,?,?,?,?,?)`, ws.ID, sha, 3, 3, 3, "none", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := st.ApplyOperation(ctx, "t1", "u1", ws.ID, Operation{OpID: "op-1", Path: "a.txt", Kind: "put", ObjectSHA256: sha, PlainSize: 3, ClientInstanceID: "m1"}, time.Now().UTC())
	if err != nil || !first.Accepted {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	dup, err := st.ApplyOperation(ctx, "t1", "u1", ws.ID, Operation{OpID: "op-1", Path: "a.txt", Kind: "put", ObjectSHA256: sha, PlainSize: 3, ClientInstanceID: "m1"}, time.Now().UTC())
	if err != nil || !dup.Accepted || dup.Merge != "duplicate" || dup.WorkspaceSeq != first.WorkspaceSeq {
		t.Fatalf("duplicate=%+v err=%v", dup, err)
	}
	conflict, err := st.ApplyOperation(ctx, "t1", "u1", ws.ID, Operation{OpID: "op-2", Path: "a.txt", Kind: "delete", BaseFileRevision: "stale", ClientInstanceID: "m2"}, time.Now().UTC())
	if err != nil || conflict.Accepted || conflict.ConflictSeq != first.WorkspaceSeq {
		t.Fatalf("conflict=%+v err=%v", conflict, err)
	}
	other, err := st.ApplyOperation(ctx, "t1", "u1", ws.ID, Operation{OpID: "op-3", Path: "b.txt", Kind: "delete", BaseFileRevision: "", ClientInstanceID: "m2"}, time.Now().UTC())
	if err != nil || !other.Accepted {
		t.Fatalf("independent op=%+v err=%v", other, err)
	}
}
