package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const promptStableCacheKeyPrefix = "sha256:"
const promptStableCacheKeyLength = 12

// StableCacheKey returns a short hash of the stable prompt segment. It is meant
// for logs and metadata when MACLAW_DEBUG_PROMPT_BUNDLE=1. It is not the
// prefix pin: alerts use PromptPrefixHash on the four sent segments.
func (p PromptBundle) StableCacheKey() string {
	stable := strings.TrimSpace(p.StableSystemPrompt)
	if stable == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(stable))
	encoded := hex.EncodeToString(sum[:])
	if len(encoded) > promptStableCacheKeyLength {
		encoded = encoded[:promptStableCacheKeyLength]
	}
	return promptStableCacheKeyPrefix + encoded
}
