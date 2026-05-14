package ve

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testKeyMat() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}

// encryptPayloadForTest encrypts a quotaPayload using the same algorithm as QuotaStore.
func encryptPayloadForTest(t *testing.T, store *QuotaStore, payload quotaPayload) []byte {
	t.Helper()
	plaintext, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	aesKey := store.deriveAESKey()
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	file := encryptedQuotaFile{Ciphertext: ciphertext, Nonce: nonce, Version: quotaFileVersion}
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestQuotaStore_SaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-test-001"

	store := NewQuotaStore(key, hubID, fp)
	if err := store.SaveQuota(42); err != nil {
		t.Fatalf("SaveQuota() error = %v", err)
	}

	// Load with fresh store (no cache)
	store2 := NewQuotaStore(key, hubID, fp)
	quota, err := store2.LoadQuota()
	if err != nil {
		t.Fatalf("LoadQuota() error = %v", err)
	}
	if quota != 42 {
		t.Errorf("LoadQuota() = %d, want 42", quota)
	}
}

func TestQuotaStore_GetEffectiveQuota_CachesValue(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-test-002"

	store := NewQuotaStore(key, hubID, fp)
	if err := store.SaveQuota(100); err != nil {
		t.Fatal(err)
	}

	// First call loads from disk
	q := store.GetEffectiveQuota()
	if q != 100 {
		t.Errorf("GetEffectiveQuota() = %d, want 100", q)
	}

	// Delete file — cached value should still work
	os.Remove(fp)
	q = store.GetEffectiveQuota()
	if q != 100 {
		t.Errorf("GetEffectiveQuota() after file delete = %d, want 100 (cached)", q)
	}

	// Invalidate cache — should return 0 (file missing)
	store.InvalidateCache()
	q = store.GetEffectiveQuota()
	if q != 0 {
		t.Errorf("GetEffectiveQuota() after invalidate = %d, want 0 (file missing)", q)
	}
}

func TestQuotaStore_WrongKey_DecryptionFails(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key1 := testKeyMat()
	key2 := testKeyMat()
	hubID := "hub-test-003"

	store1 := NewQuotaStore(key1, hubID, fp)
	if err := store1.SaveQuota(50); err != nil {
		t.Fatal(err)
	}

	store2 := NewQuotaStore(key2, hubID, fp)
	_, err := store2.LoadQuota()
	if err == nil {
		t.Fatal("LoadQuota() with wrong key should fail")
	}
}

func TestQuotaStore_HubIDMismatch(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()

	store1 := NewQuotaStore(key, "hub-A", fp)
	if err := store1.SaveQuota(30); err != nil {
		t.Fatal(err)
	}

	store2 := NewQuotaStore(key, "hub-B", fp)
	_, err := store2.LoadQuota()
	if err == nil {
		t.Fatal("LoadQuota() with different hub ID should fail")
	}
	if q := store2.GetEffectiveQuota(); q != 0 {
		t.Errorf("GetEffectiveQuota() with hub ID mismatch = %d, want 0", q)
	}
}

func TestQuotaStore_TimestampExpired(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-test-004"

	store := NewQuotaStore(key, hubID, fp)

	// Construct payload with expired timestamp
	oldTime := time.Now().UTC().Add(-25 * time.Hour)
	mac := store.computeMAC(25, hubID, oldTime)
	payload := quotaPayload{Quota: 25, HubID: hubID, Timestamp: oldTime, MAC: mac}
	data := encryptPayloadForTest(t, store, payload)
	if err := os.WriteFile(fp, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store2 := NewQuotaStore(key, hubID, fp)
	_, err := store2.LoadQuota()
	if err == nil {
		t.Fatal("LoadQuota() with expired timestamp should fail")
	}
}

func TestQuotaStore_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-test-005"

	os.WriteFile(fp, []byte("not json at all"), 0o600)

	store := NewQuotaStore(key, hubID, fp)
	_, err := store.LoadQuota()
	if err == nil {
		t.Fatal("LoadQuota() with corrupted file should fail")
	}
	if q := store.GetEffectiveQuota(); q != 0 {
		t.Errorf("GetEffectiveQuota() with corrupted file = %d, want 0", q)
	}
}

func TestQuotaStore_MACTampered(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-test-006"

	store := NewQuotaStore(key, hubID, fp)

	// Construct payload with wrong MAC (MAC computed for quota=999, but actual quota=50)
	now := time.Now().UTC()
	wrongMAC := store.computeMAC(999, hubID, now)
	payload := quotaPayload{Quota: 50, HubID: hubID, Timestamp: now, MAC: wrongMAC}
	data := encryptPayloadForTest(t, store, payload)
	if err := os.WriteFile(fp, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store2 := NewQuotaStore(key, hubID, fp)
	_, err := store2.LoadQuota()
	if err == nil {
		t.Fatal("LoadQuota() with tampered MAC should fail")
	}
}

func TestQuotaStore_FileMissing_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "nonexistent.enc")
	key := testKeyMat()
	hubID := "hub-test-007"

	store := NewQuotaStore(key, hubID, fp)
	if q := store.GetEffectiveQuota(); q != 0 {
		t.Errorf("GetEffectiveQuota() with missing file = %d, want 0", q)
	}
}

func TestQuotaStore_InvalidRange(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-test-008"

	store := NewQuotaStore(key, hubID, fp)

	if err := store.SaveQuota(-1); err == nil {
		t.Error("SaveQuota(-1) should fail")
	}
	if err := store.SaveQuota(10001); err == nil {
		t.Error("SaveQuota(10001) should fail")
	}
	if err := store.SaveQuota(0); err != nil {
		t.Errorf("SaveQuota(0) should succeed: %v", err)
	}
	if err := store.SaveQuota(10000); err != nil {
		t.Errorf("SaveQuota(10000) should succeed: %v", err)
	}
}

func TestQuotaStore_PlaintextNotInFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-test-009"

	store := NewQuotaStore(key, hubID, fp)
	if err := store.SaveQuota(7777); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatal(err)
	}

	// The string "7777" should not appear in the encrypted file
	content := string(data)
	for i := 0; i <= len(content)-4; i++ {
		if content[i:i+4] == "7777" {
			t.Error("quota value 7777 found in plaintext in encrypted file")
			break
		}
	}
}

func TestQuotaStore_OverwriteQuota(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "quota.enc")
	key := testKeyMat()
	hubID := "hub-test-010"

	store := NewQuotaStore(key, hubID, fp)
	if err := store.SaveQuota(10); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveQuota(20); err != nil {
		t.Fatal(err)
	}

	store2 := NewQuotaStore(key, hubID, fp)
	q, err := store2.LoadQuota()
	if err != nil {
		t.Fatal(err)
	}
	if q != 20 {
		t.Errorf("LoadQuota() after overwrite = %d, want 20", q)
	}
}
