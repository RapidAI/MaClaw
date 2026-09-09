package toolresult

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEncryptedSpillRoundTrip(t *testing.T) {
	root := t.TempDir()
	secret := "customer phone=13800001234 token=abc123"
	proj, err := Project(ProjectOptions{
		ToolName:   "database",
		SessionKey: "owner-a",
		Content:    secret,
		Limit:      16,
		Root:       root,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if proj.Handle == nil || !proj.Handle.Encrypted {
		t.Fatalf("database spill must be encrypted: %+v", proj.Handle)
	}
	if !strings.HasSuffix(proj.Handle.Path, encryptedSuffix) {
		t.Fatalf("encrypted handle path = %q", proj.Handle.Path)
	}
	raw, err := os.ReadFile(proj.Handle.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "13800001234") {
		t.Fatal("encrypted handle leaked plaintext on disk")
	}

	// Model-facing read-back by id decrypts transparently and pages.
	res, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-a", Root: root, Limit: 12})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if res.TotalBytes != len(secret) || !res.Truncated || !strings.HasPrefix(secret, res.Content) {
		t.Fatalf("read page = %+v", res)
	}
	next, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-a", Root: root, Offset: res.NextOffset, Limit: 4096})
	if err != nil {
		t.Fatalf("Read next: %v", err)
	}
	if !strings.HasSuffix(secret, next.Content) {
		t.Fatalf("paged read lost content: %q", next.Content)
	}

	// The .enc suffix a model might echo back is tolerated.
	if _, err := Resolve(proj.Handle.ID+encryptedSuffix, "", "owner-a", root); err != nil {
		t.Fatalf("resolve with suffix: %v", err)
	}
	// Another session must not resolve the handle.
	if _, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-b", Root: root}); err == nil {
		t.Fatal("cross-session read of encrypted handle succeeded")
	}
	// Full read-back in one page returns the complete plaintext.
	full, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-a", Root: root, Limit: MaxReadLimit})
	if err != nil || full.Content != secret {
		t.Fatalf("full read-back = %q %v", full.Content, err)
	}
}

func TestEncryptedHandleRejectsTampering(t *testing.T) {
	root := t.TempDir()
	handle, err := Spill(SpillOptions{ToolName: "database", SessionKey: "s", Content: "sensitive", Root: root, Encrypt: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(handle.Path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(handle.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ReadOptions{ID: handle.ID, SessionKey: "s", Root: root}); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
}

func TestPlaintextSpillUnaffected(t *testing.T) {
	root := t.TempDir()
	proj, err := Project(ProjectOptions{ToolName: "bash", SessionKey: "s", Content: "plain log output", Limit: 4, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Handle == nil || proj.Handle.Encrypted || !strings.HasSuffix(proj.Handle.Path, ".txt") {
		t.Fatalf("non-database tool must keep plaintext spill: %+v", proj.Handle)
	}
	res, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "s", Root: root})
	if err != nil || res.Content != "plain log output" {
		t.Fatalf("plaintext read = %q %v", res.Content, err)
	}
}

func TestStoreKeyCreatedOnce(t *testing.T) {
	root := t.TempDir()
	key1, err := storeKey(root)
	if err != nil {
		t.Fatal(err)
	}
	key2, err := storeKey(root)
	if err != nil {
		t.Fatal(err)
	}
	if string(key1) != string(key2) || len(key1) != 32 {
		t.Fatal("store key not stable")
	}
	if _, err := os.Stat(filepath.Join(root, storeKeyFile)); err != nil {
		t.Fatal("key file missing")
	}
}

func TestEncryptedHandlesCountedInStatsAndPruned(t *testing.T) {
	root := t.TempDir()
	if _, err := Spill(SpillOptions{ToolName: "database", SessionKey: "s", Content: "secret", Root: root, Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	stats := GetStoreStats(root)
	if stats.Files != 1 {
		t.Fatalf("encrypted handle missing from stats: %+v", stats)
	}
	result, err := PruneOlderThan(root, 0)
	_ = result
	if err != nil {
		t.Fatal(err)
	}
}

func TestStoreKeyConcurrentFirstCreation(t *testing.T) {
	root := t.TempDir()
	const workers = 16
	keys := make([][]byte, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			keys[i], errs[i] = storeKey(root)
		}(i)
	}
	wg.Wait()
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("storeKey worker %d: %v", i, errs[i])
		}
		if len(keys[i]) != 32 {
			t.Fatalf("worker %d key length = %d", i, len(keys[i]))
		}
		if !bytes.Equal(keys[i], keys[0]) {
			t.Fatalf("worker %d got a different key; a loser key would orphan its handles", i)
		}
	}
	// The persisted key matches what every caller received.
	persisted, err := readStoreKey(filepath.Join(root, storeKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(persisted, keys[0]) {
		t.Fatal("persisted key differs from returned key")
	}
}

func TestStoreKeyRejectsCorruptFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, storeKeyFile)
	if err := os.WriteFile(path, []byte("not-a-valid-hex-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := storeKey(root); err == nil {
		t.Fatal("corrupt store key must error, not be silently replaced")
	}
	// The corrupt file must not be overwritten: an operator can still recover
	// it, and handles sealed with the real key are not orphaned.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "not-a-valid-hex-key" {
		t.Fatal("corrupt key file was overwritten")
	}
}

func TestDecryptedContentRespillsEncrypted(t *testing.T) {
	// read_tool_result pages of an encrypted handle, and context_checkpoint
	// snapshots, contain decrypted plaintext of sensitive results. Spilling
	// those tool results must stay encrypted or the plaintext lands on disk
	// as a .txt under a different tool name.
	for _, tool := range []string{"read_tool_result", "context_checkpoint"} {
		t.Run(tool, func(t *testing.T) {
			root := t.TempDir()
			secret := "decrypted secret page phone=13900005678"
			proj, err := Project(ProjectOptions{
				ToolName:   tool,
				SessionKey: "owner",
				Content:    secret,
				Limit:      8,
				Root:       root,
			})
			if err != nil {
				t.Fatalf("Project: %v", err)
			}
			if proj.Handle == nil || !proj.Handle.Encrypted {
				t.Fatalf("%s spill must be encrypted: %+v", tool, proj.Handle)
			}
			raw, err := os.ReadFile(proj.Handle.Path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "13900005678") {
				t.Fatalf("%s spill leaked plaintext on disk", tool)
			}
			res, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner", Root: root, Limit: MaxReadLimit})
			if err != nil || res.Content != secret {
				t.Fatalf("read-back = %q %v", res.Content, err)
			}
		})
	}
}

func TestOversizedEncryptedHandleRejectedBeforeRead(t *testing.T) {
	root := t.TempDir()
	old := maxEncryptedFileBytes
	maxEncryptedFileBytes = 64
	t.Cleanup(func() { maxEncryptedFileBytes = old })

	dir := filepath.Join(root, "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Content need not be valid ciphertext: the size guard must trip before
	// any read or decrypt attempt.
	id := "oversized_handle"
	path := filepath.Join(dir, id+encryptedSuffix)
	if err := os.WriteFile(path, make([]byte, maxEncryptedFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Read(ReadOptions{ID: id, Root: root})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized .enc read error = %v", err)
	}

	// Directly prove nothing is read from an oversized payload: a reader
	// that fails on any Read must not be touched.
	_, err = readStorePayload(root, "x"+encryptedSuffix, failOnReadReader{}, maxEncryptedFileBytes+1)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("readStorePayload error = %v", err)
	}
}

type failOnReadReader struct{}

func (failOnReadReader) Read([]byte) (int, error) {
	return 0, errors.New("reader must not be read for oversized payloads")
}
