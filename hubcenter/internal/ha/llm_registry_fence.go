package ha

import (
	"encoding/json"
	"strings"
	"time"
)

// llmRegistryUpdatedAt is the subset of the LLM service registry blob the HA
// apply path needs in order to order two replicated snapshots. It is parsed
// structurally so the fence stays independent of llmservice.Registry's
// evolving schema.
type llmRegistryUpdatedAt struct {
	UpdatedAt time.Time `json:"updated_at"`
}

// llmRegistryFence reports whether an incoming replicated write to the LLM
// service registry should be skipped in favor of the local copy.
//
// The registry is a single system-setting blob (providers + arrays + service
// groups) that every mutation rewrites whole: admin provider CRUD, token-bank
// share publish/withdraw, autopause, default-group changes. MutateRegistry
// serializes writers per process only; cross-node there is no coordination,
// and applySystemSettingOp is last-applied-wins. Without a fence, a lagging
// replica of an older snapshot can land after a fresher local one and roll
// the registry backwards — the failure that silently unpublishes a token-bank
// share from every node while the owning node's share row still says
// published (R2-d in docs/design/token-bank-design-zh.md).
//
// The fence is deliberately narrow, mirroring llmProviderMonitorLeaseFence:
// it refuses only an incoming snapshot strictly older than the local one
// (persistRegistry stamps UpdatedAt on every save). It does NOT solve true
// concurrency — two nodes writing from the same base blob still lose one
// side's change — it only removes the arbitrary apply-order winner and the
// lagging-replica rollback.
//
// Deliberate non-fences:
//   - empty or unparsable local value: nothing to protect;
//   - unparsable incoming payload: a corrupted registry must not block
//     recovery, so it is applied as usual;
//   - incoming snapshot without a timestamp: a legacy writer must converge;
//   - equal timestamps: the same write echoed back, applying it is idempotent.
//
// Clock-skew caveat: nodes compare wall-clock stamps, so a skewed node's
// genuinely-later write can lose to an earlier-stamped local value. The fence
// trades that for determinism; without it the winner is chosen by apply
// order, which is no better.
func llmRegistryFence(localRaw, incomingRaw string) bool {
	var incoming llmRegistryUpdatedAt
	if err := json.Unmarshal([]byte(incomingRaw), &incoming); err != nil {
		return false
	}
	if incoming.UpdatedAt.IsZero() {
		return false
	}
	if strings.TrimSpace(localRaw) == "" {
		return false
	}
	var local llmRegistryUpdatedAt
	if err := json.Unmarshal([]byte(localRaw), &local); err != nil {
		return false
	}
	return !local.UpdatedAt.IsZero() && local.UpdatedAt.After(incoming.UpdatedAt)
}
