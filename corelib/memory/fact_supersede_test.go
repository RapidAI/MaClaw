package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSupersedeContradictingFactsMarksOldReachable(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:  "服务器 10.9.8.7 当前可达，SSH 端口 22",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "desktop-user:proj",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{
		Content:  "服务器 10.9.8.7 已经不通了",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "desktop-user:proj",
	}); err != nil {
		t.Fatal(err)
	}

	n := store.SupersedeContradictingFacts("服务器 10.9.8.7 已经不通了", "desktop-user:proj")
	if n != 1 {
		t.Fatalf("superseded=%d want 1", n)
	}

	var active, superseded int
	for _, e := range store.List(CategoryProjectKnowledge, "10.9.8.7") {
		switch e.Status {
		case StatusSuperseded:
			superseded++
			if !strings.Contains(e.Content, "可达") {
				t.Fatalf("wrong entry superseded: %s", e.Content)
			}
		case StatusActive:
			active++
			if !strings.Contains(e.Content, "不通") {
				t.Fatalf("active entry should be the update: %s", e.Content)
			}
		}
	}
	if active != 1 || superseded != 1 {
		t.Fatalf("active=%d superseded=%d", active, superseded)
	}
}

func TestSupersedeContradictingFactsIgnoresIPPrefix(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "服务器 10.0.0.81 当前可达",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "u1",
	}); err != nil {
		t.Fatal(err)
	}
	n := store.SupersedeContradictingFacts("服务器 10.0.0.8 已经不通了", "u1")
	if n != 0 {
		t.Fatalf("prefix IP must not supersede a different host, superseded=%d", n)
	}
}

func TestHandleToolSaveSupersedesAndInvalidatesCursor(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:  "跳板机 172.16.1.4 当前可达",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "u1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Paginator().FirstPage(store, "172.16.1.4", CategoryProjectKnowledge, "", "u1"); err != nil {
		t.Fatal(err)
	}
	if store.Paginator().ActiveCursorsForUser("u1") == 0 {
		t.Fatal("expected a cached cursor")
	}

	out := HandleTool(store, map[string]interface{}{
		"action":   "save",
		"content":  "跳板机 172.16.1.4 已经不通了",
		"category": "project_knowledge",
	}, ToolOptions{OwnerID: "u1"})
	if !strings.Contains(strings.ToLower(out), "saved") && !strings.Contains(out, "已保存") && !strings.Contains(out, "Memory saved") && !strings.Contains(out, "saved") {
		// FormatMemorySavedForTool wording varies; accept any non-error.
		if strings.Contains(out, "failed") || strings.Contains(out, "rejected") {
			t.Fatalf("save failed: %s", out)
		}
	}
	if store.Paginator().ActiveCursorsForUser("u1") != 0 {
		t.Fatal("cursor cache should be invalidated after a contradicting save")
	}

	recalled := store.RecallDynamicForTool("172.16.1.4", CategoryProjectKnowledge, "", "u1")
	for _, e := range recalled {
		if e.IsActive() && strings.Contains(e.Content, "可达") && !strings.Contains(e.Content, "不通") {
			t.Fatalf("stale reachable fact still recalled: %s", e.Content)
		}
	}
}

func TestApplyVerifiedFactRewritesWarehouseAndRecall(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:  "跳板机 10.9.8.7 当前可达，SSH 端口 22",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "desktop-user:proj",
	}); err != nil {
		t.Fatal(err)
	}

	got := store.ApplyVerifiedFact(VerifiedFact{
		Entity:   "ip:10.9.8.7",
		Claim:    "10.9.8.7 当前不可达",
		Evidence: "bash: 100% packet loss",
	}, "desktop-user:proj")
	if got.Superseded != 1 || !got.Saved {
		t.Fatalf("sync=%#v want superseded=1 saved=true", got)
	}

	recalled := store.RecallDynamicForTool("10.9.8.7", CategoryProjectKnowledge, "", "desktop-user:proj")
	if len(recalled) == 0 {
		t.Fatal("expected verified replacement to be recallable")
	}
	for _, e := range recalled {
		if !e.IsActive() {
			continue
		}
		if strings.Contains(e.Content, "可达") && !strings.Contains(e.Content, "不可达") {
			t.Fatalf("stale reachable fact still recalled: %s", e.Content)
		}
		if !strings.Contains(e.Content, "不可达") {
			t.Fatalf("replacement missing unreachable claim: %s", e.Content)
		}
	}
}

func TestDetectMemoryFactPolarityCannotTong(t *testing.T) {
	if got := detectMemoryFactPolarity("跳板机 10.0.0.1 不能通"); got != memoryFactPolarityUnreachable {
		t.Fatalf("不能通 polarity=%v", got)
	}
}

func TestApplyVerifiedFactStrictOwnerDoesNotTouchShared(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "跳板机 10.0.0.9 当前可达",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	got := store.ApplyVerifiedFact(VerifiedFact{
		Entity:      "ip:10.0.0.9",
		Claim:       "10.0.0.9 当前不可达",
		StrictOwner: true,
	}, "desktop-user:proj")
	if got.Superseded != 0 || got.Saved {
		t.Fatalf("isolated write must not mutate shared warehouse: %#v", got)
	}
	entries := store.List(CategoryProjectKnowledge, "10.0.0.9")
	if len(entries) != 1 || entries[0].Status != StatusActive || !strings.Contains(entries[0].Content, "可达") {
		t.Fatalf("shared entry mutated: %#v", entries)
	}
}

func TestApplyVerifiedFactEmptyOwnerDoesNotTouchOtherOwners(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "跳板机 10.0.0.9 当前可达",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "desktop-user:proj",
	}); err != nil {
		t.Fatal(err)
	}
	got := store.ApplyVerifiedFact(VerifiedFact{
		Entity: "ip:10.0.0.9",
		Claim:  "10.0.0.9 当前不可达",
	}, "")
	if got.Superseded != 0 {
		t.Fatalf("empty owner must not mutate another owner's facts: %#v", got)
	}
}

func TestApplyVerifiedFactDoesNotClobberMixedPredicateEntry(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "跳板机 10.9.8.7 当前可达。API 地址是 https://old.example.com",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "u1",
	}); err != nil {
		t.Fatal(err)
	}
	got := store.ApplyVerifiedFact(VerifiedFact{
		Entity:    "ip:10.9.8.7",
		Predicate: "reachability",
		Claim:     "10.9.8.7 当前不可达",
	}, "u1")
	if got.Superseded != 0 {
		t.Fatalf("mixed-predicate note must not be superseded wholesale: %#v", got)
	}
	entries := store.List(CategoryProjectKnowledge, "old.example.com")
	if len(entries) != 1 || !entries[0].IsActive() {
		t.Fatal("address claim must remain")
	}
}

func TestApplyVerifiedFactUpdatesAddressWithoutTouchingReachability(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "跳板机 10.9.8.7 当前可达",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "u1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{
		Content:  "API 地址是 https://old.example.com",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "u1",
	}); err != nil {
		t.Fatal(err)
	}
	got := store.ApplyVerifiedFact(VerifiedFact{
		Claim: "API 地址是 https://new.example.com",
	}, "u1")
	if got.Superseded != 1 {
		t.Fatalf("address fact not superseded: %#v", got)
	}
	for _, e := range store.List(CategoryProjectKnowledge, "10.9.8.7") {
		if e.IsActive() && strings.Contains(e.Content, "可达") && !strings.Contains(e.Content, "不可达") {
			return
		}
	}
	t.Fatal("reachability fact on the same host must stay active")
}

func TestApplyVerifiedFactAliasesSupersedeResolvedIP(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "服务器 10.1.2.3 当前可达",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "u1",
	}); err != nil {
		t.Fatal(err)
	}
	got := store.ApplyVerifiedFact(VerifiedFact{
		Entity:  "host:jump.example.com",
		Claim:   "jump.example.com 当前不可达",
		Aliases: []string{"ip:10.1.2.3"},
	}, "u1")
	if got.Superseded != 1 {
		t.Fatalf("resolved IP fact not superseded: %#v", got)
	}
}

func TestApplyVerifiedFactDoesNotInsertWhenNothingStored(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	got := store.ApplyVerifiedFact(VerifiedFact{
		Entity: "ip:10.0.0.1",
		Claim:  "10.0.0.1 当前不可达",
	}, "u1")
	if got.Superseded != 0 || got.Saved {
		t.Fatalf("empty warehouse must not be seeded: %#v", got)
	}
	if entries := store.List(CategoryProjectKnowledge, "10.0.0.1"); len(entries) != 0 {
		t.Fatalf("unexpected insert: %#v", entries)
	}
}

func TestApplyVerifiedFactMatchesHostnameEntity(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{
		Content:  "jump.example.com 当前可达",
		Category: CategoryProjectKnowledge,
		Status:   StatusActive,
		OwnerID:  "u1",
	}); err != nil {
		t.Fatal(err)
	}
	got := store.ApplyVerifiedFact(VerifiedFact{
		Entity: "host:jump.example.com",
		Claim:  "jump.example.com 当前不可达",
	}, "u1")
	if got.Superseded != 1 {
		t.Fatalf("hostname fact not superseded: %#v", got)
	}
}

func TestCategorySummarySkipsSuperseded(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{Content: "User name is Alice and she lives in Shanghai city.", Category: CategoryUserFact, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	old := store.List(CategoryUserFact, "Alice")
	if len(old) == 0 {
		t.Fatal("missing entry")
	}
	if _, err := store.SupersedeEntryByID(old[0].ID, old[0].CreatedAt.Add(1)); err != nil {
		t.Fatal(err)
	}
	if got := store.UserFactSummary(400); strings.Contains(got, "Alice") {
		t.Fatalf("superseded fact leaked into summary: %q", got)
	}
}
