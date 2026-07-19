package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testExpert(id, updatedAt string) ExpertDefinition {
	return ExpertDefinition{
		ID:           id,
		Name:         "专家" + id,
		Description:  "desc",
		Icon:         "🧪",
		SystemPrompt: "prompt",
		Tools:        []string{},
		Skills:       []string{},
		CreatedAt:    "2026-01-02T00:00:00Z",
		UpdatedAt:    updatedAt,
	}
}

func TestExpertStoreSaveLoadRoundtrip(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))

	a := testExpert("expert-a", "2026-02-01T10:00:00Z")
	b := testExpert("expert-b", "2026-02-02T10:00:00Z")
	if err := store.Save(a); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := store.Save(b); err != nil {
		t.Fatalf("save b: %v", err)
	}

	// Update a in place.
	a.Description = "updated"
	if err := store.Save(a); err != nil {
		t.Fatalf("update a: %v", err)
	}

	experts, tombstones, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(experts) != 2 {
		t.Fatalf("expected 2 experts, got %d", len(experts))
	}
	if experts[0].Description != "updated" {
		t.Fatalf("expected updated description, got %q", experts[0].Description)
	}
	if len(tombstones) != 0 {
		t.Fatalf("expected no tombstones, got %v", tombstones)
	}

	// Delete with tombstone, then reload from disk to verify persistence.
	if err := store.Delete("expert-b", true); err != nil {
		t.Fatalf("delete b: %v", err)
	}
	reloaded := newExpertStore(store.path())
	experts, tombstones, err = reloaded.List()
	if err != nil {
		t.Fatalf("reload list: %v", err)
	}
	if len(experts) != 1 || experts[0].ID != "expert-a" {
		t.Fatalf("expected only expert-a after delete, got %+v", experts)
	}
	if _, ok := tombstones["expert-b"]; !ok {
		t.Fatalf("expected tombstone for expert-b, got %v", tombstones)
	}

	// No tmp file left behind by the atomic writer.
	if _, err := os.Stat(store.path() + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file should not exist after rename: %v", err)
	}
}

func TestExpertStoreSaveClearsTombstone(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts.json"))
	e := testExpert("expert-x", "2026-02-01T10:00:00Z")
	if err := store.Save(e); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.Delete("expert-x", true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.Save(e); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	_, tombstones, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, ok := tombstones["expert-x"]; ok {
		t.Fatalf("save should clear tombstone for expert-x")
	}
}

func TestExpertStoreDeleteWithoutTombstone(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts.json"))
	e := testExpert("builtin-paper-polish", "2026-02-01T10:00:00Z")
	if err := store.Save(e); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Reset-builtin path: removal without tombstone.
	if err := store.Delete(e.ID, false); err != nil {
		t.Fatalf("delete: %v", err)
	}
	experts, tombstones, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(experts) != 0 {
		t.Fatalf("expected empty store, got %+v", experts)
	}
	if len(tombstones) != 0 {
		t.Fatalf("expected no tombstone for reset, got %v", tombstones)
	}
}

func TestMergeExpertsForSyncLWW(t *testing.T) {
	local := []ExpertDefinition{
		testExpert("expert-1", "2026-02-01T10:00:00Z"),
		testExpert("expert-2", "2026-02-03T10:00:00Z"),
	}
	hubNewer := testExpert("expert-1", "2026-02-05T10:00:00Z")
	hubNewer.Description = "hub wins"
	hubOlder := testExpert("expert-2", "2026-02-01T10:00:00Z")
	hubOlder.Description = "hub loses"
	hubOnly := testExpert("expert-3", "2026-02-04T10:00:00Z")

	merged, changedIDs, needsSave := mergeExpertsForSync(local, []ExpertDefinition{hubNewer, hubOlder, hubOnly}, nil)
	if !needsSave {
		t.Fatal("expected localNeedsSave=true")
	}
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged, got %d", len(merged))
	}
	byID := map[string]ExpertDefinition{}
	for _, e := range merged {
		byID[e.ID] = e
	}
	if byID["expert-1"].Description != "hub wins" {
		t.Fatalf("hub newer should win LWW, got %q", byID["expert-1"].Description)
	}
	if byID["expert-2"].Description != "desc" {
		t.Fatalf("local newer should be kept, got %q", byID["expert-2"].Description)
	}
	if _, ok := byID["expert-3"]; !ok {
		t.Fatal("hub-only expert should be added")
	}
	// changed ids = hub-updated + hub-added (expert-2 local-won is excluded).
	if len(changedIDs) != 2 || !containsString(changedIDs, "expert-1") || !containsString(changedIDs, "expert-3") {
		t.Fatalf("expected changedIDs [expert-1 expert-3], got %v", changedIDs)
	}

	// Same inputs again → nothing new to save.
	_, changedIDs, needsSave = mergeExpertsForSync(merged, []ExpertDefinition{hubNewer, hubOlder, hubOnly}, nil)
	if needsSave {
		t.Fatal("idempotent merge should report localNeedsSave=false")
	}
	if len(changedIDs) != 0 {
		t.Fatalf("idempotent merge should report no changed ids, got %v", changedIDs)
	}
}

func TestMergeExpertsForSyncTombstonePreventsRevival(t *testing.T) {
	local := []ExpertDefinition{
		testExpert("expert-1", "2026-02-01T10:00:00Z"),
		testExpert("expert-2", "2026-02-02T10:00:00Z"),
	}
	hub := []ExpertDefinition{
		// Hub still holds a newer copy of the locally deleted expert.
		testExpert("expert-1", "2026-02-09T10:00:00Z"),
	}
	tombstones := map[string]string{
		"expert-1": time.Now().UTC().Format(time.RFC3339),
	}
	merged, changedIDs, needsSave := mergeExpertsForSync(local, hub, tombstones)
	if !needsSave {
		t.Fatal("tombstone removal should mark localNeedsSave=true")
	}
	if len(merged) != 1 || merged[0].ID != "expert-2" {
		t.Fatalf("tombstoned expert must stay deleted, got %+v", merged)
	}
	if len(changedIDs) != 1 || changedIDs[0] != "expert-1" {
		t.Fatalf("expected changedIDs [expert-1], got %v", changedIDs)
	}
}

func TestMergeExpertsForSyncDropsBuiltinIDs(t *testing.T) {
	// A (buggy or malicious) Hub copy under a builtin id must never enter the
	// local store: builtins ship in-binary and never sync.
	local := []ExpertDefinition{}
	hub := []ExpertDefinition{
		testExpert("builtin-paper-polish", "2026-02-09T10:00:00Z"),
		testExpert("expert-legit", "2026-02-09T10:00:00Z"),
	}
	merged, changedIDs, needsSave := mergeExpertsForSync(local, hub, nil)
	if len(merged) != 1 || merged[0].ID != "expert-legit" {
		t.Fatalf("builtin hub item must be dropped, got %+v", merged)
	}
	if !needsSave || len(changedIDs) != 1 || changedIDs[0] != "expert-legit" {
		t.Fatalf("expected only expert-legit to change, got ids=%v needsSave=%v", changedIDs, needsSave)
	}
}

func TestMergeExpertsForSyncUnparseableTimestamps(t *testing.T) {
	local := []ExpertDefinition{testExpert("expert-1", "not-a-date")}
	hub := []ExpertDefinition{testExpert("expert-1", "2026-02-01T10:00:00Z")}
	merged, _, needsSave := mergeExpertsForSync(local, hub, nil)
	if !needsSave {
		t.Fatal("hub with valid newer timestamp should beat unparseable local")
	}
	if merged[0].UpdatedAt != "2026-02-01T10:00:00Z" {
		t.Fatalf("expected hub version, got %q", merged[0].UpdatedAt)
	}

	// Both unparseable → tie → keep local (fail-safe toward local-first).
	local2 := []ExpertDefinition{testExpert("expert-1", "zzz")}
	local2[0].Description = "local kept"
	hub2 := []ExpertDefinition{testExpert("expert-1", "aaa")}
	hub2[0].Description = "hub must lose"
	merged2, _, needsSave2 := mergeExpertsForSync(local2, hub2, nil)
	if needsSave2 {
		t.Fatal("both-unparseable timestamps must be a tie (keep local)")
	}
	if merged2[0].Description != "local kept" {
		t.Fatalf("tie should keep local copy, got %q", merged2[0].Description)
	}
}

func TestExpertStoreMergeAndSaveFromHub(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))
	local := testExpert("expert-1", "2026-02-01T10:00:00Z")
	if err := store.Save(local); err != nil {
		t.Fatalf("save: %v", err)
	}
	hubNewer := testExpert("expert-1", "2026-02-05T10:00:00Z")
	hubNewer.Description = "from hub"
	hubOnly := testExpert("expert-2", "2026-02-04T10:00:00Z")
	hubBuiltin := testExpert("builtin-paper-polish", "2026-02-09T10:00:00Z")

	changedIDs, err := store.MergeAndSaveFromHub([]ExpertDefinition{hubNewer, hubOnly, hubBuiltin})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(changedIDs) != 2 || !containsString(changedIDs, "expert-1") || !containsString(changedIDs, "expert-2") {
		t.Fatalf("expected changedIDs [expert-1 expert-2], got %v", changedIDs)
	}
	experts, _, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(experts) != 2 {
		t.Fatalf("expected 2 experts after merge (builtin rejected), got %+v", experts)
	}
	byID := map[string]ExpertDefinition{}
	for _, e := range experts {
		byID[e.ID] = e
	}
	if byID["expert-1"].Description != "from hub" {
		t.Fatalf("hub newer should have been persisted, got %q", byID["expert-1"].Description)
	}

	// Re-merge the same hub list: no changes, and mtime untouched (no write).
	infoBefore, err := os.Stat(store.path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	changedIDs, err = store.MergeAndSaveFromHub([]ExpertDefinition{hubNewer, hubOnly})
	if err != nil {
		t.Fatalf("re-merge: %v", err)
	}
	if len(changedIDs) != 0 {
		t.Fatalf("re-merge should be a no-op, got %v", changedIDs)
	}
	infoAfter, err := os.Stat(store.path())
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Fatal("no-op merge must not rewrite the store file")
	}
}

func TestBuiltinExpertListMerge(t *testing.T) {
	builtins := builtinExperts()
	if len(builtins) != 3 {
		t.Fatalf("expected 3 builtin experts, got %d", len(builtins))
	}
	for _, b := range builtins {
		if !b.Builtin {
			t.Fatalf("builtin expert %s must have Builtin=true", b.ID)
		}
		if b.ID == "" || b.Name == "" || b.SystemPrompt == "" || b.Icon == "" {
			t.Fatalf("builtin expert %+v has empty required field", b)
		}
	}
	if builtinExpertByID("builtin-pptx-maker") == nil {
		t.Fatal("builtin-pptx-maker missing")
	}
	if builtinExpertByID("nope") != nil {
		t.Fatal("unknown id should return nil")
	}

	// User override copy replaces the builtin entry and keeps builtin flag.
	override := testExpert("builtin-paper-polish", "2026-03-01T10:00:00Z")
	override.Builtin = false // on-disk form
	user := testExpert("expert-user-1", "2026-03-02T10:00:00Z")
	list := mergeBuiltinExpertList([]ExpertDefinition{override, user})
	if len(list) != 4 {
		t.Fatalf("expected 3 builtin + 1 user, got %d", len(list))
	}
	if list[0].ID != "builtin-paper-polish" || !list[0].Builtin {
		t.Fatalf("override copy should lead with builtin flag, got %+v", list[0])
	}
	if list[0].Description != "desc" { // testExpert's description
		t.Fatalf("override content should win over in-binary, got %q", list[0].Description)
	}
	if list[3].ID != "expert-user-1" || list[3].Builtin {
		t.Fatalf("user expert should be last with Builtin=false, got %+v", list[3])
	}
}

func TestParseExpertListJSONShapes(t *testing.T) {
	bare := `[{"id":"e1","name":"a","description":"","icon":"x","system_prompt":"p","tools":[],"skills":[],"builtin":false,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]`
	list, err := parseExpertListJSON([]byte(bare))
	if err != nil || len(list) != 1 || list[0].ID != "e1" {
		t.Fatalf("bare array: list=%+v err=%v", list, err)
	}
	wrapped := `{"experts":[{"id":"e2","name":"b","description":"","icon":"x","system_prompt":"p","tools":[],"skills":[],"builtin":false,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	list, err = parseExpertListJSON([]byte(wrapped))
	if err != nil || len(list) != 1 || list[0].ID != "e2" {
		t.Fatalf("wrapped object: list=%+v err=%v", list, err)
	}
	if list, err = parseExpertListJSON(nil); err != nil || list != nil {
		t.Fatalf("empty input: list=%+v err=%v", list, err)
	}
}

func TestParseExpertProfileResponse(t *testing.T) {
	raw := "前言废话\n```json\n{\"name\":\"翻译助手\",\"description\":\"d\",\"icon\":\"🌐\",\"system_prompt\":\"完整的提示词内容\",\"suggested_tools\":[\"bash\"],\"suggested_skills\":[]}\n```\n"
	out, err := parseExpertProfileResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if out == "" || out[0] != '{' {
		t.Fatalf("expected raw JSON object, got %q", out)
	}
	if _, err := parseExpertProfileResponse("no json here"); err == nil {
		t.Fatal("expected error for non-JSON")
	}
	if _, err := parseExpertProfileResponse(`{"name":"","system_prompt":""}`); err == nil {
		t.Fatal("expected error for empty required fields")
	}
}
