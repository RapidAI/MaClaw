package messenger

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifySignature checks the X-Hub-Signature-256 header against the raw body.
// Meta signs with the app secret: "sha256=" plus a hex HMAC-SHA256.
func VerifySignature(body []byte, appSecret, header string) bool {
	appSecret = strings.TrimSpace(appSecret)
	header = strings.TrimSpace(header)
	if appSecret == "" {
		return false
	}
	const prefix = "sha256="
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	receivedHex := header[len(prefix):]
	if len(receivedHex) != sha256.Size*2 {
		return false
	}
	received, err := hex.DecodeString(receivedHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(received, expected)
}
