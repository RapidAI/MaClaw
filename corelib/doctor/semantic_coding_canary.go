package doctor

import (
	"os"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Rollback dial for the managed semantic coding family.
//
// Every other migrated capability family can be withdrawn by dialing
// MACLAW_SHARED_AGENT_LOOP_PERCENT down, because a family that stops taking
// the shared loop stops being served by the managed surface. Coding cannot: a
// capability-managed turn has a grant-bound executor only on the shared loop,
// so the dispatcher deliberately ignores the strangler for it. Until this dial
// existed, withdrawing the family meant deleting its rule and shipping a
// build.
//
// Withdrawal returns coding turns to their pre-migration behavior, which is
// refusal (semantic_capability_unmet) rather than legacy service. An unmapped
// capability label has always failed closed instead of reopening the name
// router, and the coding subagent that does serve coding work is reached
// through its own entry points that never consult the intent rule set. This is
// a safety valve for a family that is misbehaving, not a feature toggle.

// SemanticCodingPercentFromEnv reads MACLAW_SEMANTIC_CODING_PERCENT (0..100).
// fromEnv is false when unset, so the caller falls back to config or default.
// An unparsable value is treated as set-but-full rather than silently zero: a
// typo must not withdraw a family.
func SemanticCodingPercentFromEnv() (percent int, fromEnv bool) {
	raw := strings.TrimSpace(os.Getenv("MACLAW_SEMANTIC_CODING_PERCENT"))
	if raw == "" {
		return 100, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 100, true
	}
	return clampPercent(n), true
}

// ResolveSemanticCodingPercent prefers env, then config, then 100.
func ResolveSemanticCodingPercent(cfg corelib.AppConfig) (percent int, fromEnv bool) {
	if n, ok := SemanticCodingPercentFromEnv(); ok {
		return n, true
	}
	if cfg.SemanticCodingCanaryPercent != nil {
		return clampPercent(*cfg.SemanticCodingCanaryPercent), false
	}
	return 100, false
}

// semanticCodingCanarySalt keeps this dial's buckets independent of the
// shared-loop canary. Without it both dials hash the same userID the same way,
// so the first users withdrawn from coding would be exactly the users already
// held back from the shared loop — the population where a coding problem is
// least likely to have been observed.
const semanticCodingCanarySalt = "semantic-coding:"

// SemanticCodingCanaryBucket returns the sticky FNV-1a bucket 0..99 for userID.
// An empty userID hashes the bare salt, which lands in one fixed bucket: the
// anonymous/desktop-local path is either in or out for a given percent, and
// never flaps between turns.
func SemanticCodingCanaryBucket(userID string) int {
	key := semanticCodingCanarySalt + strings.TrimSpace(userID)
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % 100)
}

// SemanticCodingCanaryAllows reports whether the managed coding family applies
// to userID at percent. 0 withdraws it from everyone, including the empty
// userID: a safety valve that cannot reach every caller is not a safety valve.
func SemanticCodingCanaryAllows(userID string, percent int) bool {
	if percent >= 100 {
		return true
	}
	if percent <= 0 {
		return false
	}
	return SemanticCodingCanaryBucket(userID) < percent
}

// SemanticCodingCanaryPreview is a JSON-friendly membership preview for
// CLI/GUI, mirroring SharedLoopCanaryPreview.
type SemanticCodingCanaryPreview struct {
	UserID  string `json:"user_id"`
	Percent int    `json:"percent"`
	Bucket  int    `json:"bucket"`
	Allows  bool   `json:"allows"`
}

// PreviewSemanticCodingCanary builds a membership preview. percent<0 resolves
// from env and cfg.
func PreviewSemanticCodingCanary(userID string, percent int, cfg corelib.AppConfig) SemanticCodingCanaryPreview {
	if percent < 0 {
		percent, _ = ResolveSemanticCodingPercent(cfg)
	} else {
		percent = clampPercent(percent)
	}
	return SemanticCodingCanaryPreview{
		UserID:  userID,
		Percent: percent,
		Bucket:  SemanticCodingCanaryBucket(userID),
		Allows:  SemanticCodingCanaryAllows(userID, percent),
	}
}
