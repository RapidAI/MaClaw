package memory

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// TemporalTree implements the Temporal Memory Tree (TMT) structure
// inspired by TiMem (arXiv:2601.02845). It organizes memory entries
// into a five-level hierarchy (Segment → Session → Day → Week → Profile)
// with explicit temporal containment and progressive consolidation.
//
// Structural properties:
//   - Temporal Containment: parent interval ⊇ child interval
//   - Progressive Consolidation: |Lᵢ| ≤ |Lᵢ₋₁| (higher levels have fewer nodes)
//   - Semantic Consolidation: higher-level nodes are LLM-generated summaries
type TemporalTree struct {
	mu      sync.RWMutex
	nodes   map[string]*tmtNode // entryID → node
	roots   []string            // IDs of top-level nodes (no parent)
	byLevel [6][]string         // level → sorted entry IDs (index 0 unused)
}

// tmtNode is the internal tree node wrapping an entry ID with structural links.
type tmtNode struct {
	EntryID  string
	Level    TemporalLevel
	Interval TimeInterval
	ParentID string
	Children []string // sorted by interval start
}

// NewTemporalTree creates an empty TMT.
func NewTemporalTree() *TemporalTree {
	return &TemporalTree{
		nodes: make(map[string]*tmtNode),
	}
}

// Insert adds an entry to the tree at the specified level with the given
// time interval. Returns an error if constraints are violated.
func (t *TemporalTree) Insert(entryID string, level TemporalLevel, interval TimeInterval) error {
	if level < LevelSegment || level > LevelProfile {
		return fmt.Errorf("tmt: invalid level %d", level)
	}
	if interval.End.Before(interval.Start) {
		return fmt.Errorf("tmt: invalid interval: end before start")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.nodes[entryID]; exists {
		return fmt.Errorf("tmt: entry %q already in tree", entryID)
	}

	node := &tmtNode{
		EntryID:  entryID,
		Level:    level,
		Interval: interval,
	}

	t.nodes[entryID] = node
	t.byLevel[level] = append(t.byLevel[level], entryID)
	t.roots = append(t.roots, entryID)

	return nil
}

// SetParent establishes a parent-child relationship. Enforces:
//   - Parent level = child level + 1
//   - Temporal containment: parent interval ⊇ child interval
func (t *TemporalTree) SetParent(childID, parentID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	child, ok := t.nodes[childID]
	if !ok {
		return fmt.Errorf("tmt: child %q not found", childID)
	}
	parent, ok := t.nodes[parentID]
	if !ok {
		return fmt.Errorf("tmt: parent %q not found", parentID)
	}

	// Constraint: parent level must be exactly child level + 1
	if parent.Level != child.Level+1 {
		return fmt.Errorf("tmt: level mismatch: parent L%d, child L%d (require parent = child + 1)",
			parent.Level, child.Level)
	}

	// Constraint: temporal containment
	if !parent.Interval.Contains(child.Interval) {
		return fmt.Errorf("tmt: temporal containment violated: parent [%s, %s] does not contain child [%s, %s]",
			parent.Interval.Start.Format(time.RFC3339),
			parent.Interval.End.Format(time.RFC3339),
			child.Interval.Start.Format(time.RFC3339),
			child.Interval.End.Format(time.RFC3339))
	}

	// Unlink from previous parent if any.
	if child.ParentID != "" {
		if oldParent, ok := t.nodes[child.ParentID]; ok {
			oldParent.Children = removeString(oldParent.Children, childID)
		}
	}

	child.ParentID = parentID
	parent.Children = append(parent.Children, childID)

	// Sort children by interval start.
	sort.Slice(parent.Children, func(i, j int) bool {
		ci, cj := t.nodes[parent.Children[i]], t.nodes[parent.Children[j]]
		return ci.Interval.Start.Before(cj.Interval.Start)
	})

	// Remove child from roots.
	t.roots = removeString(t.roots, childID)

	return nil
}

// Ancestors returns all ancestor entry IDs from the given entry up to the
// root, filtered by the allowed levels set. The result is ordered from
// lowest to highest level.
func (t *TemporalTree) Ancestors(entryID string, allowedLevels map[TemporalLevel]bool) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []string
	current := entryID

	for {
		node, ok := t.nodes[current]
		if !ok || node.ParentID == "" {
			break
		}
		parent, ok := t.nodes[node.ParentID]
		if !ok {
			break
		}
		if allowedLevels == nil || allowedLevels[parent.Level] {
			result = append(result, parent.EntryID)
		}
		current = parent.EntryID
	}

	return result
}

// Children returns direct child entry IDs of the given entry.
func (t *TemporalTree) Children(entryID string) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node, ok := t.nodes[entryID]
	if !ok {
		return nil
	}
	out := make([]string, len(node.Children))
	copy(out, node.Children)
	return out
}

// EntriesAtLevel returns all entry IDs at the given temporal level,
// sorted by interval start time.
func (t *TemporalTree) EntriesAtLevel(level TemporalLevel) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if level < LevelSegment || level > LevelProfile {
		return nil
	}
	out := make([]string, len(t.byLevel[level]))
	copy(out, t.byLevel[level])
	return out
}

// LevelCount returns the number of nodes at each level.
func (t *TemporalTree) LevelCount() map[TemporalLevel]int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	counts := make(map[TemporalLevel]int)
	for i := LevelSegment; i <= LevelProfile; i++ {
		counts[i] = len(t.byLevel[i])
	}
	return counts
}

// Remove removes a node from the tree. Children are orphaned (become roots)
// rather than reparented to the grandparent, because reparenting would
// violate the TMT level adjacency constraint (parent = child + 1).
func (t *TemporalTree) Remove(entryID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	node, ok := t.nodes[entryID]
	if !ok {
		return
	}

	// Orphan children — they become roots.
	for _, childID := range node.Children {
		if child, ok := t.nodes[childID]; ok {
			child.ParentID = ""
			t.roots = append(t.roots, childID)
		}
	}

	// Remove from parent's children list.
	if node.ParentID != "" {
		if parent, ok := t.nodes[node.ParentID]; ok {
			parent.Children = removeString(parent.Children, entryID)
		}
	}

	// Remove from level index.
	t.byLevel[node.Level] = removeString(t.byLevel[node.Level], entryID)

	// Remove from roots.
	t.roots = removeString(t.roots, entryID)

	delete(t.nodes, entryID)
}

// NodeCount returns the total number of nodes in the tree.
func (t *TemporalTree) NodeCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.nodes)
}

// Has returns true if the entry is in the tree.
func (t *TemporalTree) Has(entryID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.nodes[entryID]
	return ok
}

// NodeInfo returns the level and interval of a node, or false if not found.
func (t *TemporalTree) NodeInfo(entryID string) (TemporalLevel, *TimeInterval, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	node, ok := t.nodes[entryID]
	if !ok {
		return LevelNone, nil, false
	}
	return node.Level, &node.Interval, true
}

// ParentOf returns the parent entry ID, or empty string if none.
func (t *TemporalTree) ParentOf(entryID string) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	node, ok := t.nodes[entryID]
	if !ok {
		return ""
	}
	return node.ParentID
}

// FindPendingConsolidation returns child entry IDs at (level-1) that have
// time intervals within the given window and have no parent yet.
// These are candidates for consolidation into a new node at `level`.
func (t *TemporalTree) FindPendingConsolidation(level TemporalLevel, window TimeInterval) []string {
	if level < LevelSession || level > LevelProfile {
		return nil
	}
	childLevel := level - 1

	t.mu.RLock()
	defer t.mu.RUnlock()

	var pending []string
	for _, id := range t.byLevel[childLevel] {
		node := t.nodes[id]
		if node.ParentID != "" {
			continue // already consolidated
		}
		if window.Contains(node.Interval) {
			pending = append(pending, id)
		}
	}
	return pending
}

// RecentAtLevel returns the N most recent entry IDs at the given level,
// ordered by interval end time descending.
func (t *TemporalTree) RecentAtLevel(level TemporalLevel, n int) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if level < LevelSegment || level > LevelProfile {
		return nil
	}

	ids := make([]string, len(t.byLevel[level]))
	copy(ids, t.byLevel[level])

	// Sort by interval end time descending.
	sort.Slice(ids, func(i, j int) bool {
		ni, nj := t.nodes[ids[i]], t.nodes[ids[j]]
		return ni.Interval.End.After(nj.Interval.End)
	})

	if n > 0 && len(ids) > n {
		ids = ids[:n]
	}
	return ids
}

// Rebuild reconstructs the tree from persisted Entry data.
// Called on Store load to restore the TMT from Entry.Level/Interval/ParentID/ChildIDs.
func (t *TemporalTree) Rebuild(entries []Entry) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nodes = make(map[string]*tmtNode)
	for i := range t.byLevel {
		t.byLevel[i] = nil
	}
	t.roots = nil

	// Build ID set for validation.
	idSet := make(map[string]bool, len(entries))
	for _, e := range entries {
		idSet[e.ID] = true
	}

	// Phase 1: Create all nodes.
	for _, e := range entries {
		if e.Level == LevelNone {
			continue
		}
		interval := TimeInterval{Start: e.CreatedAt, End: e.UpdatedAt}
		if e.Interval != nil {
			interval = *e.Interval
		}
		node := &tmtNode{
			EntryID:  e.ID,
			Level:    e.Level,
			Interval: interval,
			ParentID: e.ParentID,
		}
		// Validate children exist.
		for _, childID := range e.ChildIDs {
			if idSet[childID] {
				node.Children = append(node.Children, childID)
			}
		}
		t.nodes[e.ID] = node
		if e.Level >= LevelSegment && e.Level <= LevelProfile {
			t.byLevel[e.Level] = append(t.byLevel[e.Level], e.ID)
		}
	}

	// Phase 2: Identify roots (no parent or parent not in TMT).
	for id, node := range t.nodes {
		if node.ParentID == "" || t.nodes[node.ParentID] == nil {
			t.roots = append(t.roots, id)
			node.ParentID = "" // clean up dangling parent
		}
	}

	// Sort byLevel arrays by interval start.
	for i := LevelSegment; i <= LevelProfile; i++ {
		sort.Slice(t.byLevel[i], func(a, b int) bool {
			na, nb := t.nodes[t.byLevel[i][a]], t.nodes[t.byLevel[i][b]]
			return na.Interval.Start.Before(nb.Interval.Start)
		})
	}
}

// removeString removes the first occurrence of s from slice.
func removeString(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
