package cloudworkspace

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
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
