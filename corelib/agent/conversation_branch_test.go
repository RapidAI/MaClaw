package agent

import (
	"encoding/json"
	"testing"
)

func TestConversationTree_PreservesExplicitRoots(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "math ppt", ID: "math", Timestamp: 50},
		{Role: "assistant", Content: "ok", ID: "math-a", ParentID: "math", Timestamp: 51},
		{Role: "user", Content: "maclaw ppt", ID: "maclaw", Timestamp: 100},
		{Role: "assistant", Content: "ok", ID: "maclaw-a", ParentID: "maclaw", Timestamp: 101},
	}
	tree := NewConversationTreeWithTip(entries, "maclaw-a")
	branch := tree.ActiveBranch()
	if len(branch) != 2 {
		t.Fatalf("active branch = %d, want 2 (maclaw root only), contents=%v", len(branch), branchContents(branch))
	}
	if branch[0].ID != "maclaw" || branch[1].ID != "maclaw-a" {
		t.Fatalf("active branch IDs = %s -> %s, want maclaw -> maclaw-a", branch[0].ID, branch[1].ID)
	}
}

func branchContents(entries []ConversationEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID + ":" + e.Role
	}
	return out
}

func TestConversationTree_LinearHistory(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: "write code"},
		{Role: "assistant", Content: "here's the code"},
	}

	tree := NewConversationTree(entries)

	// Active branch should be all entries in order.
	branch := tree.ActiveBranch()
	if len(branch) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(branch))
	}
	if branch[0].Content != "hello" || branch[3].Content != "here's the code" {
		t.Fatalf("wrong order: first=%v last=%v", branch[0].Content, branch[3].Content)
	}

	// No branch points in linear history.
	if bp := tree.BranchPoints(); len(bp) != 0 {
		t.Fatalf("expected 0 branch points, got %d", len(bp))
	}
}

func TestConversationTree_BranchAndActiveBranch(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "design a game"},
		{Role: "assistant", Content: "here's the design"},
		{Role: "user", Content: "confirm design"},
		{Role: "assistant", Content: "starting implementation"},
	}

	tree := NewConversationTree(entries)

	// Branch at "here's the design" (index 1 → entry after user message).
	allEntries := tree.AllEntries()
	designEntryID := allEntries[1].ID // "here's the design"

	// Branch back to the design step.
	if !tree.BranchAt(designEntryID) {
		t.Fatal("BranchAt should succeed")
	}

	// Append a different path.
	tree.Append(ConversationEntry{Role: "user", Content: "change to 2D"})
	tree.Append(ConversationEntry{Role: "assistant", Content: "ok, redesigning for 2D"})

	// Active branch should be: design a game → design → change to 2D → redesigning
	branch := tree.ActiveBranch()
	if len(branch) != 4 {
		t.Fatalf("expected 4 entries on new branch, got %d", len(branch))
	}
	if branch[2].Content != "change to 2D" {
		t.Fatalf("branch[2] = %v, want 'change to 2D'", branch[2].Content)
	}
	if branch[3].Content != "ok, redesigning for 2D" {
		t.Fatalf("branch[3] = %v, want 'ok, redesigning for 2D'", branch[3].Content)
	}

	// Tree should have 6 total entries (4 original + 2 new branch).
	if tree.Size() != 6 {
		t.Fatalf("tree size = %d, want 6", tree.Size())
	}

	// Branch point at the design entry.
	bps := tree.BranchPoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 branch point, got %d", len(bps))
	}
	if bps[0].EntryID != designEntryID {
		t.Fatalf("branch point entry = %s, want %s", bps[0].EntryID, designEntryID)
	}
	if len(bps[0].BranchIDs) != 2 {
		t.Fatalf("expected 2 branches from design, got %d", len(bps[0].BranchIDs))
	}
}

func TestConversationTree_SwitchBranch(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "A"},
		{Role: "assistant", Content: "B"},
	}

	tree := NewConversationTree(entries)
	allEntries := tree.AllEntries()
	entryA := allEntries[0].ID
	entryB := allEntries[1].ID

	// Create branch 1 from A.
	tree.BranchAt(entryA)
	tree.Append(ConversationEntry{Role: "user", Content: "C1"})
	branch1Tip := tree.TipID()

	// Create branch 2 from A.
	tree.BranchAt(entryA)
	tree.Append(ConversationEntry{Role: "user", Content: "C2"})

	// Active branch: A → C2
	branch := tree.ActiveBranch()
	if len(branch) != 2 || branch[1].Content != "C2" {
		t.Fatalf("expected A → C2, got %v", branch)
	}

	// Switch back to branch 1.
	tree.BranchAt(branch1Tip)
	branch = tree.ActiveBranch()
	if len(branch) != 2 || branch[1].Content != "C1" {
		t.Fatalf("expected A → C1 after switch, got %v", branch)
	}

	// Switch to original path (A → B).
	tree.BranchAt(entryB)
	branch = tree.ActiveBranch()
	if len(branch) != 2 || branch[1].Content != "B" {
		t.Fatalf("expected A → B after switch, got %v", branch)
	}
}

func TestConversationTree_BranchAtInvalidID(t *testing.T) {
	tree := NewConversationTree([]ConversationEntry{{Role: "user", Content: "hi"}})
	if tree.BranchAt("nonexistent") {
		t.Fatal("BranchAt with invalid ID should return false")
	}
}

func TestConversationTree_Empty(t *testing.T) {
	tree := NewConversationTree(nil)
	if branch := tree.ActiveBranch(); len(branch) != 0 {
		t.Fatalf("empty tree should have empty branch, got %d", len(branch))
	}
	if tree.Size() != 0 {
		t.Fatalf("empty tree size = %d", tree.Size())
	}
}

func TestConversationTree_AllEntriesPreservesAllBranches(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "root"},
	}
	tree := NewConversationTree(entries)
	rootID := tree.AllEntries()[0].ID

	// Create 3 branches from root.
	for i := 0; i < 3; i++ {
		tree.BranchAt(rootID)
		tree.Append(ConversationEntry{Role: "user", Content: "branch"})
	}

	// AllEntries should have 4 total (1 root + 3 children).
	all := tree.AllEntries()
	if len(all) != 4 {
		t.Fatalf("AllEntries len = %d, want 4", len(all))
	}
}

func TestConversationTree_IDsAreUnique(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "a"},
		{Role: "user", Content: "b"},
		{Role: "user", Content: "c"},
	}
	tree := NewConversationTree(entries)
	all := tree.AllEntries()
	seen := make(map[string]bool)
	for _, e := range all {
		if seen[e.ID] {
			t.Fatalf("duplicate ID: %s", e.ID)
		}
		seen[e.ID] = true
	}
}

func TestConversationTree_MetadataRoundTripThroughConversationEntry(t *testing.T) {
	original := NewConversationTree([]ConversationEntry{
		{Role: "user", Content: "A"},
		{Role: "assistant", Content: "B"},
	})
	rootID := original.AllEntries()[0].ID
	original.BranchAt(rootID)
	original.Append(ConversationEntry{Role: "user", Content: "C"})

	data, err := json.Marshal(original.AllConversationEntries())
	if err != nil {
		t.Fatal(err)
	}
	var restoredEntries []ConversationEntry
	if err := json.Unmarshal(data, &restoredEntries); err != nil {
		t.Fatal(err)
	}
	restored := NewConversationTreeWithTip(restoredEntries, original.TipID())

	if restored.Size() != 3 {
		t.Fatalf("restored tree size = %d, want 3", restored.Size())
	}
	branch := restored.ActiveBranch()
	if len(branch) != 2 || branch[1].Content != "C" {
		t.Fatalf("restored active branch = %#v, want A -> C", branch)
	}
	if bps := restored.BranchPoints(); len(bps) != 1 {
		t.Fatalf("restored branch points = %d, want 1", len(bps))
	}
}

func TestConversationMemory_BranchKeepsInactiveNodes(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	userID := "branch-user"

	cm.Save(userID, []ConversationEntry{
		{Role: "user", Content: "A"},
		{Role: "assistant", Content: "B"},
	})
	active := cm.Load(userID)
	if len(active) != 2 {
		t.Fatalf("active len = %d, want 2", len(active))
	}
	rootID := active[0].ID
	if !cm.SetActiveBranchTip(userID, rootID) {
		t.Fatal("SetActiveBranchTip should succeed")
	}
	cm.Append(userID, ConversationEntry{Role: "user", Content: "C"})

	active = cm.Load(userID)
	if len(active) != 2 || active[1].Content != "C" {
		t.Fatalf("active branch = %#v, want A -> C", active)
	}
	all := cm.LoadAll(userID)
	if len(all) != 3 {
		t.Fatalf("all entries len = %d, want 3", len(all))
	}
	tree := NewConversationTreeWithTip(all, cm.ActiveBranchTipID(userID))
	if bps := tree.BranchPoints(); len(bps) != 1 || len(bps[0].BranchIDs) != 2 {
		t.Fatalf("branch points = %#v, want root with two branches", bps)
	}
}

func TestConversationMemory_SaveLinearActiveBranchAfterBranchPreservesInactiveNodes(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	userID := "branch-save-user"

	cm.Save(userID, []ConversationEntry{
		{Role: "user", Content: "A"},
		{Role: "assistant", Content: "B"},
		{Role: "user", Content: "C"},
	})
	active := cm.Load(userID)
	if len(active) != 3 {
		t.Fatalf("active len = %d, want 3", len(active))
	}
	if !cm.SetActiveBranchTip(userID, active[1].ID) {
		t.Fatal("SetActiveBranchTip should succeed")
	}
	cm.Save(userID, []ConversationEntry{
		{Role: "user", Content: "A"},
		{Role: "assistant", Content: "B"},
		{Role: "user", Content: "D"},
	})

	active = cm.Load(userID)
	if len(active) != 3 || active[2].Content != "D" {
		t.Fatalf("active branch = %#v, want A -> B -> D", active)
	}
	all := cm.LoadAll(userID)
	if len(all) != 4 {
		t.Fatalf("all entries len = %d, want 4", len(all))
	}
	tree := NewConversationTreeWithTip(all, cm.ActiveBranchTipID(userID))
	if bps := tree.BranchPoints(); len(bps) != 1 || len(bps[0].BranchIDs) != 2 {
		t.Fatalf("branch points = %#v, want B with two branches", bps)
	}
}

func TestConversationMemory_DoesNotDeduplicateEqualTextAcrossBranches(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	userID := "branch-dedup-user"

	cm.Save(userID, []ConversationEntry{
		{Role: "user", Content: "A"},
		{Role: "assistant", Content: "B"},
		{Role: "assistant", Content: "same"},
	})
	active := cm.Load(userID)
	if len(active) != 3 {
		t.Fatalf("active len = %d, want 3", len(active))
	}
	if !cm.SetActiveBranchTip(userID, active[1].ID) {
		t.Fatal("SetActiveBranchTip should succeed")
	}
	cm.Append(userID, ConversationEntry{Role: "assistant", Content: "same"})

	all := cm.LoadAll(userID)
	if len(all) != 4 {
		t.Fatalf("all entries len = %d, want 4", len(all))
	}
	tree := NewConversationTreeWithTip(all, cm.ActiveBranchTipID(userID))
	if bps := tree.BranchPoints(); len(bps) != 1 || len(bps[0].BranchIDs) != 2 {
		t.Fatalf("branch points = %#v, want B with two equal-text branches", bps)
	}
}

func TestConversationMemory_ClearConversationMethodsResetActiveBranchTip(t *testing.T) {
	tests := []struct {
		name  string
		clear func(*ConversationMemory, string)
	}{
		{name: "keep-slot", clear: func(cm *ConversationMemory, userID string) { cm.ClearConversationButKeepSlot(userID) }},
		{name: "dismiss-slot", clear: func(cm *ConversationMemory, userID string) { cm.ClearConversationAndDismissSlot(userID) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewConversationMemory()
			defer cm.Stop()
			userID := "branch-clear-user-" + tt.name

			cm.Save(userID, []ConversationEntry{
				{Role: "user", Content: "A"},
				{Role: "assistant", Content: "B"},
			})
			if cm.ActiveBranchTipID(userID) == "" {
				t.Fatal("expected active branch tip before clear")
			}
			tt.clear(cm, userID)
			if tip := cm.ActiveBranchTipID(userID); tip != "" {
				t.Fatalf("active branch tip after clear = %q, want empty", tip)
			}
			if all := cm.LoadAll(userID); len(all) != 0 {
				t.Fatalf("LoadAll after clear len = %d, want 0", len(all))
			}
		})
	}
}
