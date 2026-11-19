package repomaint

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// verifyHubSignature checks X-Hub-Signature-256.
// The HMAC key is the UTF-8 of the unpadded base64url secret string GitHub
// stored as config.secret. It is not the decoded 32 random bytes.
// The MAC is hex-decoded and compared with hmac.Equal. A decoded length other
// than sha256.Size is rejected before the compare.
func verifyHubSignature(secret, header string, body []byte) bool {
	const prefix = "sha256="
	if secret == "" || !strings.HasPrefix(header, prefix) {
		return false
	}
	hexPart := header[len(prefix):]
	if hexPart == "" || hexPart != strings.ToLower(hexPart) {
		return false
	}
	mac, err := hex.DecodeString(hexPart)
	if err != nil || len(mac) != sha256.Size {
		return false
	}
	sum := hmac.New(sha256.New, []byte(secret))
	sum.Write(body)
	return hmac.Equal(mac, sum.Sum(nil))
}
