package main

import (
	"crypto/ed25519"
	"encoding/base64"

	"clawmatemaker/internal/firmware"
)

// releaseTrustStore is compiled into the desktop app during official
// packaging. Empty or malformed developer metadata trusts no release and
// fails closed; it never enables an unsigned package path.
func releaseTrustStore() firmware.TrustStore {
	raw, err := base64.StdEncoding.DecodeString(releasePublicKeyBase64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return firmware.TrustStore{}
	}
	return firmware.TrustStore{releaseKeyID: ed25519.PublicKey(raw)}
}
