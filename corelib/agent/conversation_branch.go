package agent

// ─────────────────────────────────────────────────────────────────────────────
// Conversation Branching — Session tree structure (Pi-aligned)
//
// Adds tree branching to conversation history. Each entry gets an ID and
// ParentID, forming a tree. The active branch is the path from root to
// ActiveBranchTip. Branching allows users to "go back to a point and try
// a different approach" without losing history.
//
// Design (non-breaking extension):
// - ID/ParentID are optional. Empty ID = legacy linear entry (backward compat).
// - Load() returns only the active branch path.
// - Save() automatically assigns IDs to new entries.
// - BranchAt() creates a new branch from a given entry.
// - All entries are persisted; branches are never deleted.
// ─────────────────────────────────────────────────────────────────────────────

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// BranchableEntry extends ConversationEntry with tree structure fields.
// These fields are added to ConversationEntry via JSON tags (optional, omitempty).
type BranchableEntry struct {
	ConversationEntry
	ID        string `json:"_id,omitempty"`
	ParentID  string `json:"_parent_id,omitempty"`
	Timestamp int64  `json:"_ts,omitempty"` // Unix millis when entry was created
}

// BranchInfo describes a branch point in the conversation tree.
type BranchInfo struct {
	EntryID   string   // The entry where the branch diverges
	BranchIDs []string // IDs of child entries (each starts a different branch)
	Labels    []string // First user message content of each branch (preview)
}

// ConversationTree provides tree operations on top of a linear entry list.
// It is an overlay that can be constructed from []ConversationEntry when
// entries have _id/_parent_id fields.
type ConversationTree struct {
	entries map[string]*BranchableEntry // all entries by ID
	rootIDs []string                    // entries with no parent (roots)
	tipID   string                      // current active branch tip
}

// NewConversationTree builds a tree from a list of entries.
// Entries without IDs are treated as a linear chain (legacy compat).
func NewConversationTree(entries []ConversationEntry) *ConversationTree {
	tree := &ConversationTree{
		entries: make(map[string]*BranchableEntry, len(entries)),
	}

	// Assign IDs to entries that don't have them (legacy linear history).
	var lastID string
	for i := range entries {
		be := entryToBranchable(entries[i])
		if be.ID == "" {
			be.ID = generateEntryID()
		}
		if be.ParentID == "" && i > 0 && lastID != "" {
			be.ParentID = lastID
		}
		if be.Timestamp == 0 {
			be.Timestamp = time.Now().UnixMilli()
		}
		tree.entries[be.ID] = be
		if be.ParentID == "" {
			tree.rootIDs = append(tree.rootIDs, be.ID)
		}
		lastID = be.ID
	}

	// Tip is the last entry by default.
	if lastID != "" {
		tree.tipID = lastID
	}

	return tree
}

// NewConversationTreeWithTip builds a tree and restores the selected active tip
// when it is present. Unknown tips fall back to the last entry for compatibility.
func NewConversationTreeWithTip(entries []ConversationEntry, tipID string) *ConversationTree {
	tree := NewConversationTree(entries)
	if tipID != "" {
		_ = tree.BranchAt(tipID)
	}
	return tree
}

// ActiveBranch returns the entries on the path from root to the current tip.
// This is what gets sent to the LLM.
func (t *ConversationTree) ActiveBranch() []ConversationEntry {
	if t.tipID == "" {
		return nil
	}

	// Walk from tip to root with cycle detection.
	var path []*BranchableEntry
	visited := make(map[string]bool, len(t.entries))
	current := t.tipID
	for current != "" {
		if visited[current] {
			break // cycle detected — stop to avoid infinite loop
		}
		visited[current] = true
		entry, ok := t.entries[current]
		if !ok {
			break
		}
		path = append(path, entry)
		current = entry.ParentID
	}

	// Reverse to get root-first order.
	result := make([]ConversationEntry, len(path))
	for i, entry := range path {
		result[len(path)-1-i] = entry.ConversationEntryWithBranchMetadata()
	}
	return result
}

// BranchAt creates a new branch point: sets the tip to the given entryID.
// The next Save() will append new entries as children of this entry,
// creating a divergent branch.
func (t *ConversationTree) BranchAt(entryID string) bool {
	if _, ok := t.entries[entryID]; !ok {
		return false
	}
	t.tipID = entryID
	return true
}

// Append adds a new entry as a child of the current tip and advances the tip.
func (t *ConversationTree) Append(entry ConversationEntry) string {
	be := entryToBranchable(entry)
	be.ID = generateEntryID()
	be.ParentID = t.tipID
	be.Timestamp = time.Now().UnixMilli()
	t.entries[be.ID] = be
	if t.tipID == "" {
		t.rootIDs = append(t.rootIDs, be.ID)
	}
	t.tipID = be.ID
	return be.ID
}

// TipID returns the current branch tip entry ID.
func (t *ConversationTree) TipID() string {
	return t.tipID
}

// AllEntries returns all entries in the tree (for persistence).
func (t *ConversationTree) AllEntries() []BranchableEntry {
	result := make([]BranchableEntry, 0, len(t.entries))
	// Pre-compute children map for O(N) traversal.
	children := make(map[string][]string, len(t.entries))
	for _, e := range t.entries {
		if e.ParentID != "" {
			children[e.ParentID] = append(children[e.ParentID], e.ID)
		}
	}
	// BFS from roots.
	visited := make(map[string]bool, len(t.entries))
	queue := append([]string{}, t.rootIDs...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		if entry, ok := t.entries[id]; ok {
			result = append(result, *entry)
		}
		queue = append(queue, children[id]...)
	}
	// Append any orphans (shouldn't happen, but defensive).
	for id, entry := range t.entries {
		if !visited[id] {
			result = append(result, *entry)
		}
	}
	return result
}

// AllConversationEntries returns all tree entries with branch metadata embedded
// in ConversationEntry so callers can persist the full tree without wrappers.
func (t *ConversationTree) AllConversationEntries() []ConversationEntry {
	all := t.AllEntries()
	result := make([]ConversationEntry, len(all))
	for i := range all {
		result[i] = all[i].ConversationEntryWithBranchMetadata()
	}
	return result
}

// BranchPoints returns all entries that have more than one child.
func (t *ConversationTree) BranchPoints() []BranchInfo {
	// Count children per parent.
	children := make(map[string][]string)
	for _, entry := range t.entries {
		if entry.ParentID != "" {
			children[entry.ParentID] = append(children[entry.ParentID], entry.ID)
		}
	}

	var branches []BranchInfo
	for parentID, childIDs := range children {
		if len(childIDs) > 1 {
			labels := make([]string, len(childIDs))
			for i, cid := range childIDs {
				if e, ok := t.entries[cid]; ok {
					labels[i] = entryPreviewLabel(e)
				}
			}
			branches = append(branches, BranchInfo{
				EntryID:   parentID,
				BranchIDs: childIDs,
				Labels:    labels,
			})
		}
	}
	return branches
}

// Size returns the total number of entries across all branches.
func (t *ConversationTree) Size() int {
	return len(t.entries)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func generateEntryID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func entryToBranchable(e ConversationEntry) *BranchableEntry {
	be := &BranchableEntry{
		ConversationEntry: e,
		ID:                e.ID,
		ParentID:          e.ParentID,
		Timestamp:         e.Timestamp,
	}
	be.ConversationEntry.ID = ""
	be.ConversationEntry.ParentID = ""
	be.ConversationEntry.Timestamp = 0
	return be
}

func (e BranchableEntry) ConversationEntryWithBranchMetadata() ConversationEntry {
	out := e.ConversationEntry
	out.ID = e.ID
	out.ParentID = e.ParentID
	out.Timestamp = e.Timestamp
	return out
}

func entryPreviewLabel(e *BranchableEntry) string {
	if e == nil {
		return ""
	}
	switch content := e.Content.(type) {
	case string:
		runes := []rune(content)
		if len(runes) > 60 {
			return string(runes[:60]) + "…"
		}
		return content
	default:
		return "[" + e.Role + "]"
	}
}
