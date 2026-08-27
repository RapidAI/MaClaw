package cloudworkspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlobStorePutGetEncryptedContentAddress(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	plain := []byte("hello cloud workspace")
	got, err := bs.Put(ctx, "t1", "u1", "cws_one", plain)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != plaintextSHA256(plain) || got.Existed || got.SizeBytes != int64(len(plain)) {
		t.Fatalf("put=%+v", got)
	}
	path, err := bs.ObjectPath("t1", "u1", "cws_one", got.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, got.SHA256+".enc") {
		t.Fatalf("object path=%s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, plain) {
		t.Fatal("disk blob leaked plaintext")
	}
	out, err := bs.Get(ctx, "t1", "u1", "cws_one", got.SHA256)
	if err != nil || !bytes.Equal(out, plain) {
		t.Fatalf("get=%q err=%v", out, err)
	}

	again, err := bs.Put(ctx, "t1", "u1", "cws_one", plain)
	if err != nil || !again.Existed {
		t.Fatalf("idempotent put=%+v err=%v", again, err)
	}

	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, "cws_one", got.SHA256).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("objects rows=%d", n)
	}

	if _, err := bs.Get(ctx, "t1", "u1", "cws_other", got.SHA256); err != ErrBlobNotFound {
		t.Fatalf("other workspace err=%v", err)
	}
	has, err := bs.Has(ctx, "t1", "u1", "cws_one", got.SHA256)
	if err != nil || !has {
		t.Fatalf("has=%v err=%v", has, err)
	}
}

func TestBlobStoreRejectsTraversalAndUpperHash(t *testing.T) {
	bs := &BlobStore{Root: t.TempDir()}
	if _, err := bs.ObjectPath("t1", "u1", "cws_one", strings.Repeat("A", 64)); err != ErrInvalidBlobKey {
		t.Fatalf("uppercase hash err=%v", err)
	}
	if _, err := bs.Put(context.Background(), "../t", "u1", "cws_one", []byte("x")); err != ErrInvalidBlobKey {
		t.Fatalf("tenant traversal err=%v", err)
	}
	if _, err := bs.StagingDir("t1", "u1/../x", "cws_one"); err != ErrInvalidBlobKey {
		t.Fatalf("user traversal err=%v", err)
	}
}

func TestBlobStoreStagingDir(t *testing.T) {
	bs := &BlobStore{Root: t.TempDir()}
	dir, err := bs.PrepareStaging("t1", "u1", "cws_one")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("staging stat=%v err=%v", info, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := bs.RemoveStaging("t1", "u1", "cws_one"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("staging still present err=%v", err)
	}
}

func TestBlobStoreGetCorruptAndTooLarge(t *testing.T) {
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), MaxObjectBytes: 4}
	if _, err := bs.Put(context.Background(), "t1", "u1", "cws_one", []byte("hello")); err != ErrBlobTooLarge {
		t.Fatalf("too large err=%v", err)
	}
	bs.MaxObjectBytes = 0
	if bs.maxObjectBytes() != defaultMaxObjectBytes {
		t.Fatalf("default max=%d want %d", bs.maxObjectBytes(), defaultMaxObjectBytes)
	}
	got, err := bs.Put(context.Background(), "t1", "u1", "cws_one", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	path, err := bs.ObjectPath("t1", "u1", "cws_one", got.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-a-gcm-blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.Get(context.Background(), "t1", "u1", "cws_one", got.SHA256); err != ErrBlobCorrupt {
		t.Fatalf("corrupt err=%v", err)
	}
}

func TestBlobStoreGetHasRefuseOversizedCiphertext(t *testing.T) {
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), MaxObjectBytes: 4}
	sum := plaintextSHA256([]byte("x"))
	path, err := bs.ObjectPath("t1", "u1", "cws_one", sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// Larger than plaintext cap + GCM overhead, so Get must not ReadFile it.
	if err := os.WriteFile(path, bytes.Repeat([]byte("a"), 64), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.Get(context.Background(), "t1", "u1", "cws_one", sum); err != ErrBlobTooLarge {
		t.Fatalf("get oversized err=%v", err)
	}
	has, err := bs.Has(context.Background(), "t1", "u1", "cws_one", sum)
	if has || err != ErrBlobTooLarge {
		t.Fatalf("has oversized has=%v err=%v", has, err)
	}
}

func TestBlobStoreWrongAADCannotOpenCopiedBlob(t *testing.T) {
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys")}
	plain := []byte("secret-bytes")
	got, err := bs.Put(context.Background(), "t1", "u1", "cws_one", plain)
	if err != nil {
		t.Fatal(err)
	}
	src, err := bs.ObjectPath("t1", "u1", "cws_one", got.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := bs.ObjectPath("t1", "u1", "cws_two", got.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.Get(context.Background(), "t1", "u1", "cws_two", got.SHA256); err != ErrBlobCorrupt {
		t.Fatalf("cross-workspace copy err=%v", err)
	}
}

func TestBlobStorePutExpectedAndChunks(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	plain := []byte("0123456789abcdef")
	sum := plaintextSHA256(plain)
	if _, err := bs.PutExpected(ctx, "t1", "u1", "cws_one", sum, []byte("nope")); err != ErrBlobHashMismatch {
		t.Fatalf("mismatch err=%v", err)
	}
	if err := bs.PutChunk(ctx, "t1", "u1", "cws_one", sum, 0, plain[:8]); err != nil {
		t.Fatal(err)
	}
	if err := bs.PutChunk(ctx, "t1", "u1", "cws_one", sum, 1, plain[8:]); err != nil {
		t.Fatal(err)
	}
	got, err := bs.AssembleChunks("t1", "u1", "cws_one", sum)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("assemble=%q err=%v", got, err)
	}
	res, err := bs.PutExpected(ctx, "t1", "u1", "cws_one", sum, got)
	if err != nil || res.SHA256 != sum {
		t.Fatalf("put=%+v err=%v", res, err)
	}
	if err := bs.RemovePart("t1", "u1", "cws_one", sum); err != nil {
		t.Fatal(err)
	}
	wrong := plaintextSHA256([]byte("other"))
	if err := bs.PutChunk(ctx, "t1", "u1", "cws_one", wrong, 0, []byte("aaaa")); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.AssembleChunks("t1", "u1", "cws_one", wrong); err != ErrBlobHashMismatch {
		t.Fatalf("bad complete err=%v", err)
	}
}
