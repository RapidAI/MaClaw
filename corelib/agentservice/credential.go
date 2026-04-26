package agentservice

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

func randomURLToken(prefix string, bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func generateCredentialAPIKey() (string, error) {
	return randomURLToken("mck_", 24)
}

func generateCredentialAPISecret() (string, error) {
	return randomURLToken("mcs_", 32)
}

func hashAPIKey(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func deriveAPIKeyPrefix(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 6 {
		return v
	}
	return v[:6]
}

func credentialLookupKey(v Credential) string {
	if hash := strings.TrimSpace(v.APIKeyHash); hash != "" {
		return hash
	}
	return hashAPIKey(v.APIKey)
}

func credentialTokenVersion(v Credential) int {
	if v.TokenVersion <= 0 {
		return 1
	}
	return v.TokenVersion
}
