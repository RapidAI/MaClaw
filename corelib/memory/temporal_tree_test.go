package memory

import (
	"testing"
	"time"
)

func TestTemporalTreeInsertAndQuery(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	// Insert L1 segments.
	seg1 := TimeInterval{Start: now, End: now.Add(1 * time.Minute)}
	seg2 := TimeInterval{Start: now.Add(5 * time.Minute), End: now.Add(6 * time.Minute)}

	if err := tree.Insert("s1", LevelSegment, seg1); err != nil {
		t.Fatalf("Insert s1: %v", err)
	}
	if err := tree.Insert("s2", LevelSegment, seg2); err != nil {
		t.Fatalf("Insert s2: %v", err)
	}

	if tree.NodeCount() != 2 {
		t.Errorf("expected 2 nodes, got %d", tree.NodeCount())
	}
	if !tree.Has("s1") || !tree.Has("s2") {
		t.Error("expected s1 and s2 to exist")
	}
}

func TestTemporalTreeSetParent(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	segInterval := TimeInterval{Start: now, End: now.Add(10 * time.Minute)}
	sessionInterval := TimeInterval{Start: now.Add(-1 * time.Minute), End: now.Add(15 * time.Minute)}

	tree.Insert("seg", LevelSegment, segInterval)
	tree.Insert("sess", LevelSession, sessionInterval)

	if err := tree.SetParent("seg", "sess"); err != nil {
		t.Fatalf("SetParent: %v", err)
	}

	// Check parent link.
	parentID := tree.ParentOf("seg")
	if parentID != "sess" {
		t.Errorf("expected parent sess, got %q", parentID)
	}

	// Check children list.
	children := tree.Children("sess")
	if len(children) != 1 || children[0] != "seg" {
		t.Errorf("expected children [seg], got %v", children)
	}
}

func TestTemporalTreeSetParentConstraints(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	seg := TimeInterval{Start: now, End: now.Add(5 * time.Minute)}
	sess := TimeInterval{Start: now, End: now.Add(5 * time.Minute)}

	tree.Insert("seg", LevelSegment, seg)
	tree.Insert("sess", LevelSession, sess)

	// Same level should fail.
	tree.Insert("seg2", LevelSegment, seg)
	err := tree.SetParent("seg", "seg2")
	if err == nil {
		t.Error("expected error for same-level parent")
	}

	// Non-adjacent level should fail.
	dayInterval := TimeInterval{Start: now.Add(-1 * time.Hour), End: now.Add(24 * time.Hour)}
	tree.Insert("day", LevelDay, dayInterval)
	err = tree.SetParent("seg", "day")
	if err == nil {
		t.Error("expected error for non-adjacent level parent (L1 -> L3)")
	}

	// Temporal containment violation should fail.
	narrowSession := TimeInterval{Start: now.Add(10 * time.Minute), End: now.Add(20 * time.Minute)}
	tree.Insert("narrow_sess", LevelSession, narrowSession)
	err = tree.SetParent("seg", "narrow_sess")
	if err == nil {
		t.Error("expected error for temporal containment violation")
	}
}

func TestTemporalTreeAncestors(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	seg := TimeInterval{Start: now, End: now.Add(5 * time.Minute)}
	sess := TimeInterval{Start: now.Add(-1 * time.Minute), End: now.Add(10 * time.Minute)}
	day := TimeInterval{Start: now.Add(-1 * time.Hour), End: now.Add(24 * time.Hour)}

	tree.Insert("seg", LevelSegment, seg)
	tree.Insert("sess", LevelSession, sess)
	tree.Insert("day", LevelDay, day)

	tree.SetParent("seg", "sess")
	tree.SetParent("sess", "day")

	// Get all ancestors.
	ancestors := tree.Ancestors("seg", nil)
	if len(ancestors) != 2 {
		t.Errorf("expected 2 ancestors, got %d: %v", len(ancestors), ancestors)
	}

	// Filtered ancestors: only session level.
	filtered := tree.Ancestors("seg", map[TemporalLevel]bool{LevelSession: true})
	if len(filtered) != 1 || filtered[0] != "sess" {
		t.Errorf("expected [sess], got %v", filtered)
	}
}

func TestTemporalTreeFindPendingConsolidation(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	// Insert 3 segments within a window.
	window := TimeInterval{Start: now, End: now.Add(1 * time.Hour)}
	tree.Insert("s1", LevelSegment, TimeInterval{Start: now, End: now.Add(5 * time.Minute)})
	tree.Insert("s2", LevelSegment, TimeInterval{Start: now.Add(10 * time.Minute), End: now.Add(15 * time.Minute)})
	tree.Insert("s3", LevelSegment, TimeInterval{Start: now.Add(30 * time.Minute), End: now.Add(35 * time.Minute)})

	// Find pending consolidation at L2 (session).
	pending := tree.FindPendingConsolidation(LevelSession, window)
	if len(pending) != 3 {
		t.Errorf("expected 3 pending L1 nodes, got %d", len(pending))
	}

	// Now assign a parent to s1 — it should no longer be pending.
	sessInterval := TimeInterval{Start: now.Add(-1 * time.Minute), End: now.Add(1 * time.Hour)}
	tree.Insert("sess1", LevelSession, sessInterval)
	tree.SetParent("s1", "sess1")

	pending = tree.FindPendingConsolidation(LevelSession, window)
	if len(pending) != 2 {
		t.Errorf("expected 2 pending L1 nodes, got %d", len(pending))
	}
}

func TestTemporalTreeRemove(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	tree.Insert("a", LevelSegment, TimeInterval{Start: now, End: now.Add(5 * time.Minute)})
	tree.Insert("b", LevelSegment, TimeInterval{Start: now.Add(10 * time.Minute), End: now.Add(15 * time.Minute)})

	tree.Remove("a")

	if tree.Has("a") {
		t.Error("expected a to be removed")
	}
	if tree.NodeCount() != 1 {
		t.Errorf("expected 1 node, got %d", tree.NodeCount())
	}
}

func TestTemporalTreeRebuild(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{
			ID:       "e1",
			Level:    LevelSegment,
			Interval: &TimeInterval{Start: now, End: now.Add(5 * time.Minute)},
			ParentID: "e2",
		},
		{
			ID:       "e2",
			Level:    LevelSession,
			Interval: &TimeInterval{Start: now.Add(-1 * time.Minute), End: now.Add(1 * time.Hour)},
			ChildIDs: []string{"e1"},
		},
		{
			ID:    "e3",
			Level: LevelNone, // should be skipped
		},
	}

	tree := NewTemporalTree()
	tree.Rebuild(entries)

	if tree.NodeCount() != 2 {
		t.Errorf("expected 2 nodes after rebuild, got %d", tree.NodeCount())
	}
	if tree.ParentOf("e1") != "e2" {
		t.Errorf("expected e1 parent to be e2, got %q", tree.ParentOf("e1"))
	}
}

func TestTemporalTreeLevelCount(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	tree.Insert("s1", LevelSegment, TimeInterval{Start: now, End: now.Add(5 * time.Minute)})
	tree.Insert("s2", LevelSegment, TimeInterval{Start: now.Add(10 * time.Minute), End: now.Add(15 * time.Minute)})
	tree.Insert("sess1", LevelSession, TimeInterval{Start: now, End: now.Add(30 * time.Minute)})

	counts := tree.LevelCount()
	if counts[LevelSegment] != 2 {
		t.Errorf("expected 2 segments, got %d", counts[LevelSegment])
	}
	if counts[LevelSession] != 1 {
		t.Errorf("expected 1 session, got %d", counts[LevelSession])
	}
	if counts[LevelDay] != 0 {
		t.Errorf("expected 0 days, got %d", counts[LevelDay])
	}
}

func TestTemporalTreeRecentAtLevel(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	tree.Insert("s1", LevelSegment, TimeInterval{Start: now, End: now.Add(1 * time.Minute)})
	tree.Insert("s2", LevelSegment, TimeInterval{Start: now.Add(5 * time.Minute), End: now.Add(6 * time.Minute)})
	tree.Insert("s3", LevelSegment, TimeInterval{Start: now.Add(10 * time.Minute), End: now.Add(11 * time.Minute)})

	recent := tree.RecentAtLevel(LevelSegment, 2)
	if len(recent) != 2 {
		t.Errorf("expected 2 recent, got %d", len(recent))
	}
	// Should be most recently inserted first (s3, s2).
	if recent[0] != "s3" || recent[1] != "s2" {
		t.Errorf("expected [s3, s2], got %v", recent)
	}
}

func TestTemporalTreeDuplicateInsert(t *testing.T) {
	tree := NewTemporalTree()
	now := time.Now()

	seg := TimeInterval{Start: now, End: now.Add(5 * time.Minute)}
	tree.Insert("s1", LevelSegment, seg)
	err := tree.Insert("s1", LevelSegment, seg)
	if err == nil {
		t.Error("expected error on duplicate insert")
	}
}
