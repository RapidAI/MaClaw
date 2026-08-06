package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunCreatesSignedManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "esptool"), []byte("sidecar"), 0700); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(dir, "esptool", "esptool 5.3.1", "test-key", base64.StdEncoding.EncodeToString(priv)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "sidecar-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var signed struct {
		SchemaVersion int       `json:"schemaVersion"`
		Tools         []record  `json:"tools"`
		Signature     signature `json:"signature"`
	}
	if err := json.Unmarshal(raw, &signed); err != nil {
		t.Fatal(err)
	}
	if signed.Signature.Algorithm != "ed25519" || signed.Signature.KeyID != "test-key" {
		t.Fatalf("signature=%+v", signed.Signature)
	}
	payload, err := json.Marshal(unsignedManifest{SchemaVersion: signed.SchemaVersion, Tools: signed.Tools})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := base64.StdEncoding.DecodeString(signed.Signature.Signature)
	if err != nil || !ed25519.Verify(pub, payload, sig) {
		t.Fatalf("manifest signature invalid: %v", err)
	}
}
