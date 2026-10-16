package toolresult

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Encrypted spills (for example database query results, which may carry
// sensitive column values) must never sit on disk in plaintext. Encryption
// uses AES-256-GCM with a per-install store key kept next to the store root;
// the key file itself is 0600 and never leaves the host. MaClawSrv
// deployments are expected to swap this for a KMS-backed key provider before
// enabling multi-tenant persistent handles.

// storeKeyFile is the name of the key file inside the store root.
const storeKeyFile = ".store.key"

// encryptedSuffix marks spilled handle files encrypted at rest.
const encryptedSuffix = ".enc"

// maxEncryptedFileBytes bounds the whole-file decrypt path. Database results
// are already capped well below this; the guard exists so a hostile or
// corrupt .enc file cannot force an unbounded allocation. It is a package
// variable (not a constant) so tests can lower the limit.
var maxEncryptedFileBytes int64 = 64 << 20

// isEncryptedToolResult reports whether a tool's spilled output must be
// encrypted at rest. This is the single choke point so every projection path
// (agent loop, checkpoints, host adapters) inherits the same rule.
//
// read_tool_result and context_checkpoint are included even though they are
// not inherently sensitive: their output can BE the decrypted plaintext of an
// encrypted handle, and without this rule the agent loop would spill that
// plaintext back to disk as a .txt file. Encrypting a public page is harmless;
// a single plaintext leak is not.
func isEncryptedToolResult(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "database", "read_tool_result", "context_checkpoint":
		return true
	}
	return false
}

// errInvalidStoreKey marks a present-but-undecodable key file. It is a
// sentinel so the create path can distinguish "corrupt key, fail loudly" from
// "key still being written by a concurrent creator, retry".
var errInvalidStoreKey = errors.New("toolresult: invalid store key")

// storeKeyForRoot resolves the store root (honoring an optional override) the
// same way Resolve does, so key lookups land beside the handles they protect.
func storeRootForKey(root string) (string, error) {
	return storeRoot(root)
}
func storeKey(root string) ([]byte, error) {
	path := filepath.Join(root, storeKeyFile)
	key, err := readStoreKeyWait(path)
	if err == nil {
		return key, nil
	}
	if !os.IsNotExist(err) {
		if errors.Is(err, errInvalidStoreKey) {
			// A corrupt key must never be silently replaced: handles sealed
			// with the original key would become undecryptable.
			return nil, err
		}
		return nil, fmt.Errorf("toolresult: read store key: %w", err)
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("toolresult: mkdir for store key: %w", err)
	}
	// O_EXCL closes the first-creation race: a concurrent creator fails with
	// EEXIST and adopts the winner's key instead of overwriting it (which
	// would orphan every handle sealed with the overwritten key).
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return readStoreKeyWait(path)
		}
		return nil, fmt.Errorf("toolresult: create store key: %w", err)
	}
	_, werr := f.WriteString(hex.EncodeToString(key))
	cerr := f.Close()
	if werr != nil {
		return nil, fmt.Errorf("toolresult: write store key: %w", werr)
	}
	if cerr != nil {
		return nil, fmt.Errorf("toolresult: write store key: %w", cerr)
	}
	return key, nil
}

// readStoreKey loads and validates the hex-encoded 32-byte store key.
func readStoreKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, decErr := hex.DecodeString(strings.TrimSpace(string(data)))
	if decErr != nil || len(key) != 32 {
		return nil, errInvalidStoreKey
	}
	return key, nil
}

// readStoreKeyWait is readStoreKey with a short retry window for the invalid
// case. With O_EXCL creation a concurrent reader can observe the key file
// after create but before the writer's Write lands; poll briefly instead of
// reporting a corrupt key. A genuinely corrupt key still errors (after the
// window) and is never overwritten.
func readStoreKeyWait(path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 50; attempt++ {
		key, err := readStoreKey(path)
		if !errors.Is(err, errInvalidStoreKey) {
			return key, err
		}
		lastErr = err
		time.Sleep(2 * time.Millisecond)
	}
	return nil, lastErr
}

// encryptPayload seals plaintext as nonce || AES-GCM ciphertext.
func encryptPayload(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptPayload opens a nonce || AES-GCM ciphertext payload.
func decryptPayload(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize()+gcm.Overhead() {
		return nil, fmt.Errorf("toolresult: encrypted handle is truncated")
	}
	nonce := data[:gcm.NonceSize()]
	return gcm.Open(nil, nonce, data[gcm.NonceSize():], nil)
}

// readStoreFile returns the plaintext content of a spilled handle file,
// transparently decrypting .enc payloads with the store key. root must be the
// resolved store root that owns the key file; use storeRootForKey to compute
// it from a caller-provided root override.
func readStoreFile(root, path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return readStorePayload(root, path, f, info.Size())
}

// readStorePayload reads r (a spilled handle file of the given total size)
// and returns its plaintext, decrypting .enc payloads with the store key.
// For .enc files the size is checked against maxEncryptedFileBytes BEFORE any
// content is read, so an oversized file can never force a full-file
// allocation. Read passes its already-validated descriptor and size here so
// the earlier SameFile check stays authoritative for the bytes actually read.
func readStorePayload(root, path string, r io.Reader, size int64) ([]byte, error) {
	if !strings.HasSuffix(path, encryptedSuffix) {
		return io.ReadAll(r)
	}
	if size < 0 || size > maxEncryptedFileBytes {
		return nil, fmt.Errorf("toolresult: encrypted handle exceeds the %d byte limit", maxEncryptedFileBytes)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	key, err := storeKey(root)
	if err != nil {
		return nil, err
	}
	plain, err := decryptPayload(key, data)
	if err != nil {
		return nil, fmt.Errorf("toolresult: decrypt handle: %w", err)
	}
	return plain, nil
}
