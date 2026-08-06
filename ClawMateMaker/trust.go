package main

import (
	"crypto/ed25519"
	"encoding/base64"

	"clawmatemaker/internal/firmware"
)

// releaseTrustStore is compiled into the desktop application during official
// packaging. The placeholder intentionally trusts no real release; it makes
// unsigned/misconfigured CI output fail closed until a release public key is
// set in the build environment/package definition.
//
// Production packaging replaces CLAWMATE_RELEASE_PUBLIC_KEY_BASE64 with the
// public half matching the protected GitHub Actions signing key and uses the
// same key ID exposed by CLAWMATE_FIRMWARE_SIGNING_KEY_ID.
// These are variables so official desktop builds can inject the public key
// using Go's -ldflags -X. They are deliberately empty in developer builds;
// an empty or malformed key means zero trusted releases, never a bypass.
func releaseTrustStore() firmware.TrustStore {
	raw, err := base64.StdEncoding.DecodeString(releasePublicKeyBase64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return firmware.TrustStore{}
	}
	return firmware.TrustStore{releaseKeyID: ed25519.PublicKey(raw)}
}
