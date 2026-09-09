package cloudworkspace

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealOpenRoundTripAndAAD(t *testing.T) {
	dek := deriveDEK(bytes.Repeat([]byte{7}, 32), "t1", "u1", "cws_abc")
	aad := objectAAD("t1", "u1", "cws_abc")
	plain := []byte("workspace object body")
	blob, err := seal(dek, aad, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, plain) {
		t.Fatal("ciphertext leaked plaintext")
	}
	got, err := open(dek, aad, blob)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("roundtrip=%q err=%v", got, err)
	}
	if _, err := open(dek, objectAAD("t1", "u1", "cws_other"), blob); err != ErrBlobCorrupt {
		t.Fatalf("wrong workspace aad err=%v", err)
	}
	other := deriveDEK(bytes.Repeat([]byte{7}, 32), "t1", "u2", "cws_abc")
	if _, err := open(other, aad, blob); err != ErrBlobCorrupt {
		t.Fatalf("wrong user dek err=%v", err)
	}
}

func TestDeriveDEKIsolatesWorkspace(t *testing.T) {
	master := bytes.Repeat([]byte{9}, 32)
	a := deriveDEK(master, "t", "u", "ws1")
	b := deriveDEK(master, "t", "u", "ws2")
	if bytes.Equal(a, b) {
		t.Fatal("workspace DEKs must differ")
	}
	if !bytes.Equal(a, deriveDEK(master, "t", "u", "ws1")) {
		t.Fatal("DEK must be stable")
	}
}

func TestLoadMasterKeyFromEnvAndFile(t *testing.T) {
	t.Setenv(masterKeyEnv, "")
	dir := t.TempDir()
	key, err := loadMasterKey(dir)
	if err != nil || len(key) != 32 {
		t.Fatalf("generated key len=%d err=%v", len(key), err)
	}
	again, err := loadMasterKey(dir)
	if err != nil || !bytes.Equal(key, again) {
		t.Fatalf("file key not stable err=%v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, masterKeyFile))
	if err != nil || !bytes.Equal(raw, key) {
		t.Fatalf("master.key mismatch err=%v", err)
	}

	envKey := bytes.Repeat([]byte{3}, 32)
	t.Setenv(masterKeyEnv, base64.RawStdEncoding.EncodeToString(envKey))
	got, err := loadMasterKey(dir)
	if err != nil || !bytes.Equal(got, envKey) {
		t.Fatalf("env key=%x err=%v", got, err)
	}

	t.Setenv(masterKeyEnv, "not-base64")
	if _, err := loadMasterKey(dir); err == nil {
		t.Fatal("invalid env key should fail")
	}
}

func TestPlaintextSHA256LowerHex(t *testing.T) {
	sum := plaintextSHA256([]byte("abc"))
	if len(sum) != 64 || sum != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("sha256=%s", sum)
	}
}

func TestVersionedEnvelopeRoundTripAndLegacyFallback(t *testing.T) {
	t.Setenv(masterKeyEnv, "")
	t.Setenv(keyringEnv, "")
	dir := t.TempDir()
	provider := &FileKeyProvider{Dir: dir}
	aad := objectAAD("tenant", "user", "workspace")
	envelope, version, err := sealWorkspace(context.Background(), provider, "tenant", "user", "workspace", aad, []byte("v2 payload"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, "aes-gcm-v2:") {
		t.Fatalf("version=%q", version)
	}
	plain, keyID, err := openWorkspace(context.Background(), provider, "tenant", "user", "workspace", aad, envelope)
	if err != nil || string(plain) != "v2 payload" || keyID == "" {
		t.Fatalf("v2 open=%q key=%q err=%v", plain, keyID, err)
	}
	key, err := provider.ActiveKey(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := seal(deriveDEK(key.Bytes, "tenant", "user", "workspace"), aad, []byte("legacy payload"))
	if err != nil {
		t.Fatal(err)
	}
	plain, legacyID, err := openWorkspace(context.Background(), provider, "tenant", "user", "workspace", aad, legacy)
	if err != nil || string(plain) != "legacy payload" || legacyID != key.ID {
		t.Fatalf("legacy open=%q key=%q err=%v", plain, legacyID, err)
	}
	descriptor, err := provider.Descriptor(context.Background())
	if err != nil || descriptor.Envelope != "aes-gcm-v2" || descriptor.KeyCount != 1 || descriptor.KeyID != key.ID {
		t.Fatalf("descriptor=%+v err=%v", descriptor, err)
	}
	truncated := envelope[:len(ciphertextEnvelopeMagic)]
	if _, _, err := openWorkspace(context.Background(), provider, "tenant", "user", "workspace", aad, truncated); err != ErrBlobCorrupt {
		t.Fatalf("truncated v2 envelope err=%v", err)
	}
	future := append([]byte(nil), envelope...)
	future[len(ciphertextEnvelopeMagic)-1] = 3
	if _, _, err := openWorkspace(context.Background(), provider, "tenant", "user", "workspace", aad, future); err != ErrBlobCorrupt {
		t.Fatalf("unknown envelope version err=%v", err)
	}
}

// A keyring written by a pre-11.31 binary that crashed mid-rotation carries
// rotation_key_id naming the rotation target. The provider must keep every
// key readable and continue new writes with the rotation target instead of
// refusing to load the keyring.
func TestKeyringInterruptedRotationMarkerStaysReadable(t *testing.T) {
	t.Setenv(masterKeyEnv, "")
	t.Setenv(keyringEnv, "")
	dir := t.TempDir()
	oldKey := bytes.Repeat([]byte{1}, 32)
	newKey := bytes.Repeat([]byte{2}, 32)
	oldID := encryptionKeyID(oldKey)
	newID := encryptionKeyID(newKey)
	state := keyringFile{
		Version:       keyringVersion,
		ActiveKeyID:   oldID,
		RotationKeyID: newID,
		Keys: []keyringEntry{
			{ID: oldID, Material: base64.RawStdEncoding.EncodeToString(oldKey)},
			{ID: newID, Material: base64.RawStdEncoding.EncodeToString(newKey)},
		},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, masterKeyringFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	p := &FileKeyProvider{Dir: dir}
	active, err := p.ActiveKey(context.Background())
	if err != nil {
		t.Fatalf("interrupted-rotation keyring must load: %v", err)
	}
	if active.ID != newID || !bytes.Equal(active.Bytes, newKey) {
		t.Fatalf("active=%s want rotation target %s", active.ID, newID)
	}
	if _, err := p.Key(context.Background(), oldID); err != nil {
		t.Fatalf("old key must stay readable: %v", err)
	}
	keys, err := p.AllKeys(context.Background())
	if err != nil || len(keys) != 2 || keys[0].ID != newID {
		t.Fatalf("all keys=%+v err=%v, want rotation target first", keys, err)
	}
	desc, err := p.Descriptor(context.Background())
	if err != nil || desc.KeyID != newID || desc.KeyCount != 2 || desc.RotationKeyID != newID {
		t.Fatalf("descriptor=%+v err=%v", desc, err)
	}
}

// A rotation marker pointing at an unknown key means the keyring lost the
// rotation target; fail closed instead of silently picking another key.
func TestKeyringOrphanRotationMarkerFailsClosed(t *testing.T) {
	t.Setenv(masterKeyEnv, "")
	t.Setenv(keyringEnv, "")
	dir := t.TempDir()
	key := bytes.Repeat([]byte{1}, 32)
	state := keyringFile{
		Version:       keyringVersion,
		ActiveKeyID:   encryptionKeyID(key),
		RotationKeyID: strings.Repeat("9", 64),
		Keys:          []keyringEntry{{ID: encryptionKeyID(key), Material: base64.RawStdEncoding.EncodeToString(key)}},
	}
	raw, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, masterKeyringFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&FileKeyProvider{Dir: dir}).ActiveKey(context.Background()); err == nil || !strings.Contains(err.Error(), "rotation key") {
		t.Fatalf("orphan rotation marker err=%v", err)
	}
}
