package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyEmbeddedTrustMetadataRequiresBothExpectedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ClawMateMaker")
	if err := os.WriteFile(path, []byte("prefix key-v1 middle public-key-base64 suffix"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyEmbeddedTrustMetadata(path, "key-v1", "public-key-base64"); err != nil {
		t.Fatalf("valid metadata rejected: %v", err)
	}
	if err := verifyEmbeddedTrustMetadata(path, "key-v2", "public-key-base64"); err == nil {
		t.Fatal("missing key ID was accepted")
	}
	if err := verifyEmbeddedTrustMetadata(path, "key-v1", "public-key-other"); err == nil {
		t.Fatal("missing public key was accepted")
	}
}
