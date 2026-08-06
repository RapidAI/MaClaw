package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestReleaseTrustStoreFailsClosedAndAcceptsInjectedKey(t *testing.T) {
	oldID, oldKey := releaseKeyID, releasePublicKeyBase64
	t.Cleanup(func() { releaseKeyID, releasePublicKeyBase64 = oldID, oldKey })
	releasePublicKeyBase64 = "not-a-key"
	if got := releaseTrustStore(); len(got) != 0 {
		t.Fatalf("invalid key trusted: %#v", got)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	releaseKeyID = "test-release"
	releasePublicKeyBase64 = base64.StdEncoding.EncodeToString(pub)
	if got := releaseTrustStore(); !got[releaseKeyID].Equal(pub) {
		t.Fatalf("injected key unavailable")
	}
}
