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
	for _, segment := range []string{" tenant", "tenant ", "tenant.", "CON", "NUL.txt", "tenant:ads", "tenant\rvalue"} {
		if _, err := bs.ObjectsDir(segment, "u1", "cws_one"); err != ErrInvalidBlobKey {
			t.Fatalf("unsafe tenant segment %q err=%v", segment, err)
		}
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
	if err := os.WriteFile(path, bytes.Repeat([]byte("a"), 128), 0o600); err != nil {
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

func TestBlobStoreGetLegacyZstdWithoutCodecMetadata(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	plain := bytes.Repeat([]byte("legacy-compressed-object\n"), 80)
	stored, compression, _ := compressObject(plain)
	if compression != "zstd" || !hasZstdMagic(stored) {
		t.Fatalf("compression=%s magic=%v", compression, hasZstdMagic(stored))
	}
	key, err := bs.keyProvider().ActiveKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := seal(deriveDEK(key.Bytes, "t1", "u1", "cws_legacy"), objectAAD("t1", "u1", "cws_legacy"), stored)
	if err != nil {
		t.Fatal(err)
	}
	sum := plaintextSHA256(plain)
	path, err := bs.ObjectPath("t1", "u1", "cws_legacy", sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_objects (
		workspace_id, sha256, size_bytes, plain_size_bytes, stored_size_bytes,
		compression, compression_level, encryption_version, ref_count, created_at, object_state
	) VALUES (?, ?, ?, 1, 7, 'none', 0, 'aes-gcm-v1', 0, ?, 'ready')`,
		"cws_legacy", sum, len(plain), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	out, err := bs.Get(ctx, "t1", "u1", "cws_legacy", sum)
	if err != nil || !bytes.Equal(out, plain) {
		t.Fatalf("legacy get len=%d err=%v", len(out), err)
	}
	var healedCompression string
	var healedPlain, healedStored int64
	if err := st.db.QueryRow(`SELECT compression, plain_size_bytes, stored_size_bytes FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, "cws_legacy", sum).Scan(&healedCompression, &healedPlain, &healedStored); err != nil {
		t.Fatal(err)
	}
	if healedCompression != "zstd" || healedPlain != int64(len(plain)) || healedStored != int64(len(sealed)) {
		t.Fatalf("healed compression=%s plain=%d stored=%d", healedCompression, healedPlain, healedStored)
	}
	out, err = bs.Get(ctx, "t1", "u1", "cws_legacy", sum)
	if err != nil || !bytes.Equal(out, plain) {
		t.Fatalf("healed get len=%d err=%v", len(out), err)
	}

	rawPlain := []byte("legacy plaintext that is not compressed!!")
	rawSum := plaintextSHA256(rawPlain)
	rawSealed, err := seal(deriveDEK(key.Bytes, "t1", "u1", "cws_legacy"), objectAAD("t1", "u1", "cws_legacy"), rawPlain)
	if err != nil {
		t.Fatal(err)
	}
	rawPath, err := bs.ObjectPath("t1", "u1", "cws_legacy", rawSum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, rawSealed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_objects (
		workspace_id, sha256, size_bytes, plain_size_bytes, stored_size_bytes,
		compression, compression_level, encryption_version, ref_count, created_at, object_state
	) VALUES (?, ?, ?, 0, 0, 'none', 0, 'aes-gcm-v1', 0, ?, 'ready')`,
		"cws_legacy", rawSum, len(rawPlain), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	out, err = bs.Get(ctx, "t1", "u1", "cws_legacy", rawSum)
	if err != nil || !bytes.Equal(out, rawPlain) {
		t.Fatalf("uncompressed legacy get len=%d err=%v", len(out), err)
	}
	var rawCompression string
	var rawPlainSize int64
	if err := st.db.QueryRow(`SELECT compression, plain_size_bytes FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, "cws_legacy", rawSum).Scan(&rawCompression, &rawPlainSize); err != nil {
		t.Fatal(err)
	}
	if rawCompression != "none" || rawPlainSize != 0 {
		t.Fatalf("uncompressed row compression=%s plain=%d", rawCompression, rawPlainSize)
	}
}

func TestBlobStoreGetLegacyZstdMagicPrefixStaysPlain(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	plain := append(append([]byte{}, zstdMagic...), []byte("not-a-zstd-frame-just-plaintext")...)
	key, err := bs.keyProvider().ActiveKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := seal(deriveDEK(key.Bytes, "t1", "u1", "cws_magic"), objectAAD("t1", "u1", "cws_magic"), plain)
	if err != nil {
		t.Fatal(err)
	}
	sum := plaintextSHA256(plain)
	path, err := bs.ObjectPath("t1", "u1", "cws_magic", sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_objects (
		workspace_id, sha256, size_bytes, plain_size_bytes, stored_size_bytes,
		compression, compression_level, encryption_version, ref_count, created_at, object_state
	) VALUES (?, ?, ?, 0, 0, 'none', 0, 'aes-gcm-v1', 0, ?, 'ready')`,
		"cws_magic", sum, len(plain), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	out, err := bs.Get(ctx, "t1", "u1", "cws_magic", sum)
	if err != nil || !bytes.Equal(out, plain) {
		t.Fatalf("magic-prefix get len=%d err=%v", len(out), err)
	}
	var compression string
	if err := st.db.QueryRow(`SELECT compression FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, "cws_magic", sum).Scan(&compression); err != nil {
		t.Fatal(err)
	}
	if compression != "none" {
		t.Fatalf("magic-prefix compression=%s", compression)
	}
}

func TestBlobStoreGetCorruptLegacyZstdFrame(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	bogus := append(append([]byte{}, zstdMagic...), bytes.Repeat([]byte{0x00}, 32)...)
	key, err := bs.keyProvider().ActiveKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := seal(deriveDEK(key.Bytes, "t1", "u1", "cws_bad"), objectAAD("t1", "u1", "cws_bad"), bogus)
	if err != nil {
		t.Fatal(err)
	}
	sum := plaintextSHA256([]byte("not-the-frame"))
	path, err := bs.ObjectPath("t1", "u1", "cws_bad", sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_objects (
		workspace_id, sha256, size_bytes, plain_size_bytes, stored_size_bytes,
		compression, compression_level, encryption_version, ref_count, created_at, object_state
	) VALUES (?, ?, 4096, 0, 0, 'none', 0, 'aes-gcm-v1', 0, ?, 'ready')`,
		"cws_bad", sum, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := bs.Get(ctx, "t1", "u1", "cws_bad", sum); err != ErrBlobCorrupt {
		t.Fatalf("corrupt frame err=%v", err)
	}
	var compression string
	var plainSize int64
	if err := st.db.QueryRow(`SELECT compression, plain_size_bytes FROM cloud_workspace_objects WHERE workspace_id = ? AND sha256 = ?`, "cws_bad", sum).Scan(&compression, &plainSize); err != nil {
		t.Fatal(err)
	}
	if compression != "none" || plainSize != 0 {
		t.Fatalf("corrupt row compression=%s plain=%d", compression, plainSize)
	}
}

func TestBlobStoreGetUsesSizeBytesWhenPlainSizeMissing(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	ctx := context.Background()
	plain := bytes.Repeat([]byte("labeled-zstd-without-plain-size\n"), 80)
	stored, compression, _ := compressObject(plain)
	if compression != "zstd" {
		t.Fatalf("compression=%s", compression)
	}
	key, err := bs.keyProvider().ActiveKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := seal(deriveDEK(key.Bytes, "t1", "u1", "cws_size"), objectAAD("t1", "u1", "cws_size"), stored)
	if err != nil {
		t.Fatal(err)
	}
	sum := plaintextSHA256(plain)
	path, err := bs.ObjectPath("t1", "u1", "cws_size", sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO cloud_workspace_objects (
		workspace_id, sha256, size_bytes, plain_size_bytes, stored_size_bytes,
		compression, compression_level, encryption_version, ref_count, created_at, object_state
	) VALUES (?, ?, ?, 0, 0, 'ZSTD', 0, 'aes-gcm-v1', 0, ?, 'ready')`,
		"cws_size", sum, len(plain), time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	out, err := bs.Get(ctx, "t1", "u1", "cws_size", sum)
	if err != nil || !bytes.Equal(out, plain) {
		t.Fatalf("size-bytes get len=%d err=%v", len(out), err)
	}
}

func TestBlobStoreGetMetadataErrorIsNotCorrupt(t *testing.T) {
	st, _ := newTestWorkspaceStore(t)
	root := t.TempDir()
	t.Setenv(masterKeyEnv, "")
	bs := &BlobStore{Root: root, KeyDir: filepath.Join(root, "keys"), DB: st.db}
	plain := []byte("hello cloud workspace")
	got, err := bs.Put(context.Background(), "t1", "u1", "cws_one", plain)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = bs.Get(ctx, "t1", "u1", "cws_one", got.SHA256)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
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
