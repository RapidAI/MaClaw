package agentservice

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

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
