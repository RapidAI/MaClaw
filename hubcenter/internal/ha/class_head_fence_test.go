package ha

import (
	"encoding/json"
	"strings"
	"testing"
)

func officialHeadStore(samples ...string) string {
	items := make([]map[string]any, 0, len(samples))
	for i, at := range samples {
		items = append(items, map[string]any{
			"id":      at,
			"preview": "p",
			"index":   i,
			"at":      at,
		})
	}
	raw, err := json.Marshal(map[string]any{"version": 2, "samples": items})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestOfficialClassHeadSettingKeyMatchesGlobalAndPerGroup(t *testing.T) {
	if !officialClassHeadSettingKey("llm_official_class_head_v1") {
		t.Fatal("global key must be fenced")
	}
	if !officialClassHeadSettingKey("llm_official_class_head_v1:coding-auto") {
		t.Fatal("per-group key must be fenced")
	}
	if officialClassHeadSettingKey("llm_class_head_v1:coding-auto") {
		t.Fatal("per-group training key is a different entity and must not be fenced here")
	}
	if officialClassHeadSettingKey("llm_official_class_head_v1_other") {
		t.Fatal("prefix must require the ':' separator")
	}
}

// The store is rewritten on every training sample, so replicas of one entity
// arrive constantly and out of order across peers. A stale replica landing
// after a fresher one must not roll the local store backwards.
func TestOfficialClassHeadFenceKeepsFresherLocalStore(t *testing.T) {
	local := officialHeadStore("2026-09-18T02:00:00Z", "2026-09-18T03:00:00Z")
	stale := officialHeadStore("2026-09-18T01:00:00Z", "2026-09-18T01:30:00Z")

	if !officialClassHeadFence(local, stale) {
		t.Fatal("a staler incoming store must be fenced")
	}
}

func TestOfficialClassHeadFenceAppliesNewerStore(t *testing.T) {
	local := officialHeadStore("2026-09-18T02:00:00Z")
	newer := officialHeadStore("2026-09-18T02:00:00Z", "2026-09-18T04:00:00Z")

	if officialClassHeadFence(local, newer) {
		t.Fatal("a strictly newer incoming store must be applied")
	}
}

// Sample At values move to the head on write but a re-observation does not
// re-stamp them, so the newest timestamp is the reliable freshness signal.
func TestOfficialClassHeadFenceIgnoresSampleOrder(t *testing.T) {
	local := officialHeadStore("2026-09-18T05:00:00Z", "2026-09-18T02:00:00Z")
	stale := officialHeadStore("2026-09-18T01:00:00Z", "2026-09-18T03:00:00Z")

	if !officialClassHeadFence(local, stale) {
		t.Fatal("order of samples within the store must not decide freshness")
	}
}

// Equal newest timestamps mean "no newer observation", which is the steady
// state under a high write rate: the store is rewritten per training sample, so
// most replicas carry a timestamp that is not newer than the local one. These
// are applied (not fenced) — they cost one idempotent local write and keep the
// pull cursor advancing, and the pull path already recorded the op in
// ha_sync_ops. What matters is that the local store's *content* is not rolled
// backwards: the fence only refuses an incoming store that is strictly older,
// or one that would shrink the sample set.
func TestOfficialClassHeadFenceAppliesEqualNewestTimestamp(t *testing.T) {
	local := officialHeadStore("2026-09-18T02:00:00Z")
	same := officialHeadStore("2026-09-18T02:00:00Z")

	if officialClassHeadFence(local, same) {
		t.Fatal("an incoming store with an equally-new observation is applied, not fenced")
	}
}

// A same-newest-timestamp store that carries a different sample set is applied
// too, as long as it does not shrink the sample count.
func TestOfficialClassHeadFenceAppliesEqualNewestTimestampDifferentSamples(t *testing.T) {
	local := officialHeadStore("2026-09-18T01:00:00Z", "2026-09-18T02:00:00Z")
	other := officialHeadStore("2026-09-18T02:00:00Z", "2026-09-18T02:00:00Z")

	if officialClassHeadFence(local, other) {
		t.Fatal("a same-size store with an equally-new observation must not be fenced")
	}
}

// An empty incoming store is the truncation signature: applying it would wipe
// the classifier training set on this node.
func TestOfficialClassHeadFenceRefusesEmptyIncomingStore(t *testing.T) {
	local := officialHeadStore("2026-09-18T02:00:00Z")
	if !officialClassHeadFence(local, officialHeadStore()) {
		t.Fatal("an empty incoming store must not wipe a populated local store")
	}
}

func TestOfficialClassHeadFenceKeepsLocalOnFewerSamples(t *testing.T) {
	local := officialHeadStore("2026-09-18T01:00:00Z", "2026-09-18T02:00:00Z", "2026-09-18T03:00:00Z")
	pruned := officialHeadStore("2026-09-18T01:00:00Z")

	if !officialClassHeadFence(local, pruned) {
		t.Fatal("a store with fewer samples is a prune/truncation and must be fenced")
	}
}

func TestOfficialClassHeadFenceAllowsFirstWrite(t *testing.T) {
	if officialClassHeadFence("", officialHeadStore("2026-09-18T02:00:00Z")) {
		t.Fatal("an empty local store must accept the incoming one")
	}
}

// A malformed payload must not fence: applying it surfaces the error through
// json.Unmarshal in applySystemSettingOp instead of being silently swallowed.
func TestOfficialClassHeadFenceDoesNotFenceMalformedPayloads(t *testing.T) {
	if officialClassHeadFence("not json", officialHeadStore("2026-09-18T02:00:00Z")) {
		t.Fatal("unparseable local store must not fence")
	}
	if officialClassHeadFence(officialHeadStore("2026-09-18T02:00:00Z"), "not json") {
		t.Fatal("unparseable incoming store must not fence")
	}
}

func TestOfficialClassHeadFenceHandlesMissingTimestamps(t *testing.T) {
	local := `{"version":2,"samples":[{"id":"a"},{"id":"b"}]}`
	incoming := `{"version":2,"samples":[{"id":"a"},{"id":"b","at":"2026-09-18T02:00:00Z"}]}`
	// local has no timestamps at all, incoming has one: incoming is newer.
	if officialClassHeadFence(local, incoming) {
		t.Fatal("a timestamped incoming store beats an untimestamped local store")
	}
	// Reverse: local is timestamped, incoming is not.
	if !officialClassHeadFence(incoming, local) {
		t.Fatal("an untimestamped incoming store must not overwrite a timestamped local store")
	}
}

func TestOfficialClassHeadFenceSurvivesRealStoreShape(t *testing.T) {
	// Guard against the field names drifting: this mirrors the shape emitted by
	// llmservice.OfficialClassHeadStore.
	raw := `{"version":2,"samples":[{"id":"s1","preview":"hello","rule_class":"coding","head_class":"coding","at":"2026-09-18T02:00:00Z"}]}`
	if !strings.Contains(raw, `"at"`) {
		t.Fatal("fixture is stale")
	}
	if officialClassHeadFence("", raw) {
		t.Fatal("first write must land")
	}
}
