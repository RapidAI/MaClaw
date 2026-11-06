package guiapp

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// chromeExtensionIDFromManifestKey maps a manifest "key" (base64 SPKI) to the
// Chrome extension ID: SHA-256, first 16 bytes, each nibble as 'a' + nibble.
func chromeExtensionIDFromManifestKey(key string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	var b strings.Builder
	b.Grow(32)
	for _, by := range sum[:16] {
		b.WriteByte('a' + (by >> 4))
		b.WriteByte('a' + (by & 0x0f))
	}
	return b.String(), nil
}

// browserExtensionID is the Chrome extension ID derived from the committed
// manifest public key in extensions/companion-paper/manifest.json. The private
// key that signs a CRX stays out of the repository. The Windows installer and
// the macOS postinstall write this same literal.
const browserExtensionID = "iidkjocaelnacgnadidhbedomegjndjk"

// browserExtensionVersion matches extensions/companion-paper/manifest.json.
const browserExtensionVersion = "1.0.0"

const companionBrowserHostName = "com.maclaw.companion"

const companionChromeStoreUpdateURL = "https://clients2.google.com/service/update2/crx"
const companionEdgeStoreUpdateURL = "https://edge.microsoft.com/extensionwebstorebase/v1/crx"

// Set with -X at package time. Empty means the installer registers the native
// host only and tells the user the extension itself is not auto-installed.
var browserExtensionChromeUpdateURL string
var browserExtensionEdgeUpdateURL string
