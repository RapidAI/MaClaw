package ha

import (
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

// officialHeadSampleTimestamp is the subset of a training sample this package
// needs in order to order two replicated stores. It is deliberately parsed
// structurally rather than by importing llmservice's full sample type so the
// fence stays independent of that package's evolving schema.
type officialHeadSampleTimestamp struct {
	At string `json:"at"`
}

type officialHeadTimestampEnvelope struct {
	Samples []officialHeadSampleTimestamp `json:"samples"`
}

// officialClassHeadSettingKey reports whether key belongs to the official
// classifier training store. Both the global key and its per-group variants
// carry the same versioned store, so both need the same fence.
func officialClassHeadSettingKey(key string) bool {
	key = strings.TrimSpace(key)
	return key == llmservice.OfficialClassHeadKey || strings.HasPrefix(key, llmservice.OfficialClassHeadKey+":")
}

// officialClassHeadFence reports whether the local store should win over the
// incoming replicated one.
//
// The classifier training store is rewritten on every recorded training sample,
// so peers exchange a high volume of full-store snapshots for a single entity.
// Ops are replicated in sequence order per peer, but the applied state is only
// "latest wins". Since a pull batch can straddle two peers' histories, a
// snapshot that is already thousands of samples stale can land after a fresher
// one and roll the local store backwards, silently discarding training data.
//
// Entity version cannot arbitrate this. Store Version is a format constant
// (officialClassHeadStoreVer), not a revision counter, so every snapshot of this
// entity looks like revision 1 and shouldApplyRemoteVersion has nothing to
// compare. The fence therefore compares content instead.
//
// The fence is deliberately narrow. It refuses only two things:
//
//  1. An incoming store whose newest sample is strictly older than the local
//     newest sample (the roll-back case).
//  2. An incoming store that would shrink the sample set (a prune or truncation
//     signature; also covers an empty store wiping a populated one).
//
// Everything else is applied, including a store that is equally-new but
// different: that costs one idempotent local write, keeps the pull cursor moving,
// and lets ordinary replication converge. Malformed payloads never fence, so a
// corrupted store cannot block recovery.
func officialClassHeadFence(localRaw, incomingRaw string) bool {
	if strings.TrimSpace(localRaw) == "" {
		return false
	}
	var local, incoming officialHeadTimestampEnvelope
	if err := json.Unmarshal([]byte(incomingRaw), &incoming); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(localRaw), &local); err != nil {
		return false
	}
	if len(incoming.Samples) == 0 {
		return len(local.Samples) > 0
	}
	// Fewer samples is the prune or truncation signature: keep the local store.
	if len(incoming.Samples) < len(local.Samples) {
		return true
	}
	return newestOfficialHeadSampleAt(incoming.Samples) < newestOfficialHeadSampleAt(local.Samples)
}

// newestOfficialHeadSampleAt returns the lexicographically greatest sample
// timestamp. timestamps are RFC3339 UTC strings, so string order is time order.
func newestOfficialHeadSampleAt(samples []officialHeadSampleTimestamp) string {
	newest := ""
	for _, sample := range samples {
		if at := strings.TrimSpace(sample.At); at > newest {
			newest = at
		}
	}
	return newest
}
