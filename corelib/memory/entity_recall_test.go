package memory

import (
	"strings"
	"testing"
	"time"
)

func TestRecallDynamicDoesNotBindSharedHanFragments(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()
	if err := store.Save(Entry{
		Content:   "马勇是中国人，就职于奇安信。",
		Category:  CategoryProjectKnowledge,
		Status:    StatusActive,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{
		Content:   "中国人奇强只是一个测试称呼。",
		Category:  CategoryProjectKnowledge,
		Status:    StatusActive,
		UpdatedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	results := store.RecallDynamicForTool("中国人奇强", "", "")
	found := false
	for _, entry := range results {
		if strings.Contains(entry.Content, "奇安信") {
			t.Fatalf("recalled a different person from shared characters: %s", entry.Content)
		}
		if strings.Contains(entry.Content, "中国人奇强") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missed the memory that contains the queried name: %d results", len(results))
	}

	byName := store.RecallDynamicForTool("马勇", "", "")
	foundBio := false
	for _, entry := range byName {
		if strings.Contains(entry.Content, "奇安信") {
			foundBio = true
		}
	}
	if !foundBio {
		t.Fatal("querying the real name should still find that biography")
	}
}

func TestEntryMentionsAnchorsStayInsideOneField(t *testing.T) {
	anchors := strictRecallAnchors("中国人奇强")
	if len(anchors) == 0 {
		t.Fatal("expected strict anchors")
	}
	split := Entry{Content: "国籍为中国人，单位奇", Title: "强相关的其他说明"}
	if entryMentionsAnchors(split, anchors) {
		t.Fatalf("anchors formed across fields: %v", anchors)
	}
	whole := Entry{Content: "中国人奇强只是一个测试称呼。"}
	if !entryMentionsAnchors(whole, anchors) {
		t.Fatalf("true mention missed anchors %v", anchors)
	}
}
