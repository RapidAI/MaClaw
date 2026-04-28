package agent

import (
	"testing"
)

// --- BuildEntryGroups tests ---

func TestBuildEntryGroups_NoToolCalls(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "bye"},
	}
	groups := BuildEntryGroups(entries)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	for i, g := range groups {
		if g.End-g.Start != 1 {
			t.Errorf("group %d: expected size 1, got %d", i, g.End-g.Start)
		}
	}
}

func TestBuildEntryGroups_SingleToolCall(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "check weather"},
		{Role: "assistant", Content: "let me check", ToolCalls: []map[string]interface{}{
			{"id": "call_1"},
		}},
		{Role: "tool", Content: "sunny", ToolCallID: "call_1"},
		{Role: "assistant", Content: "it's sunny"},
	}
	groups := BuildEntryGroups(entries)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups (user, assistant+tool, assistant), got %d", len(groups))
	}
	// The second group should contain assistant + tool = 2 entries.
	if groups[1].End-groups[1].Start != 2 {
		t.Errorf("tool_calls group: expected size 2, got %d", groups[1].End-groups[1].Start)
	}
	if groups[1].Start != 1 || groups[1].End != 3 {
		t.Errorf("tool_calls group: expected [1,3), got [%d,%d)", groups[1].Start, groups[1].End)
	}
}

func TestBuildEntryGroups_MultipleToolCalls(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "do two things"},
		{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{
			{"id": "call_a"}, {"id": "call_b"},
		}},
		{Role: "tool", Content: "result_a", ToolCallID: "call_a"},
		{Role: "tool", Content: "result_b", ToolCallID: "call_b"},
		{Role: "assistant", Content: "both done"},
	}
	groups := BuildEntryGroups(entries)
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	// assistant + 2 tools = 3 entries in one group.
	if groups[1].End-groups[1].Start != 3 {
		t.Errorf("tool_calls group: expected size 3, got %d", groups[1].End-groups[1].Start)
	}
}

func TestBuildEntryGroups_ConsecutiveToolCallGroups(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{{"id": "c1"}}},
		{Role: "tool", Content: "r1", ToolCallID: "c1"},
		{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{{"id": "c2"}}},
		{Role: "tool", Content: "r2", ToolCallID: "c2"},
		{Role: "assistant", Content: "done"},
	}
	groups := BuildEntryGroups(entries)
	if len(groups) != 4 {
		t.Fatalf("expected 4 groups, got %d", len(groups))
	}
	// Group 1: assistant+tool (c1)
	if groups[1].End-groups[1].Start != 2 {
		t.Errorf("group 1: expected size 2, got %d", groups[1].End-groups[1].Start)
	}
	// Group 2: assistant+tool (c2)
	if groups[2].End-groups[2].Start != 2 {
		t.Errorf("group 2: expected size 2, got %d", groups[2].End-groups[2].Start)
	}
}

// --- TrimHistory group-based tests ---

func TestTrimHistory_NeverSplitsToolCallGroup(t *testing.T) {
	// Build a history that exceeds MaxConversationTurns.
	// The key invariant: after trimming, every assistant(tool_calls)
	// must be immediately followed by its tool messages.
	var entries []ConversationEntry
	for i := 0; i < MaxConversationTurns+10; i++ {
		entries = append(entries, ConversationEntry{
			Role: "user", Content: "question",
		})
		entries = append(entries, ConversationEntry{
			Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{
				{"id": "call_" + string(rune('A'+i))},
			},
		})
		entries = append(entries, ConversationEntry{
			Role: "tool", Content: "result", ToolCallID: "call_" + string(rune('A'+i)),
		})
	}

	trimmed := TrimHistory(entries)

	// Verify the structural invariant: no orphaned tool_calls or tool messages.
	for i, e := range trimmed {
		if e.Role == "assistant" && e.ToolCalls != nil {
			// Must be followed by tool message(s).
			if i+1 >= len(trimmed) || trimmed[i+1].Role != "tool" {
				t.Errorf("assistant(tool_calls) at index %d has no following tool message", i)
			}
		}
		if e.Role == "tool" {
			// Must be preceded by assistant(tool_calls) or another tool.
			if i == 0 {
				t.Errorf("orphaned tool message at index 0")
			} else if trimmed[i-1].Role != "assistant" && trimmed[i-1].Role != "tool" {
				t.Errorf("tool message at index %d preceded by %q, not assistant/tool", i, trimmed[i-1].Role)
			}
		}
	}
}

func TestTrimHistory_SmallHistory_Unchanged(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{{"id": "c1"}}},
		{Role: "tool", Content: "ok", ToolCallID: "c1"},
		{Role: "assistant", Content: "done"},
	}
	result := TrimHistory(entries)
	if len(result) != 4 {
		t.Fatalf("small history should be unchanged, got %d entries", len(result))
	}
}

// --- GroupContaining tests ---

func TestGroupContaining(t *testing.T) {
	entries := []ConversationEntry{
		{Role: "user", Content: "hi"},                                                          // 0
		{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{{"id": "c1"}}},    // 1
		{Role: "tool", Content: "ok", ToolCallID: "c1"},                                        // 2
		{Role: "assistant", Content: "done"},                                                    // 3
	}
	groups := BuildEntryGroups(entries)

	// Index 0 should be in its own group.
	g := GroupContaining(groups, 0)
	if g == nil || g.Start != 0 || g.End != 1 {
		t.Errorf("index 0: expected [0,1), got %v", g)
	}

	// Index 1 (assistant with tool_calls) should be in group [1,3).
	g = GroupContaining(groups, 1)
	if g == nil || g.Start != 1 || g.End != 3 {
		t.Errorf("index 1: expected [1,3), got %v", g)
	}

	// Index 2 (tool) should be in the SAME group as index 1.
	g = GroupContaining(groups, 2)
	if g == nil || g.Start != 1 || g.End != 3 {
		t.Errorf("index 2: expected [1,3), got %v", g)
	}

	// Index 3 should be in its own group.
	g = GroupContaining(groups, 3)
	if g == nil || g.Start != 3 || g.End != 4 {
		t.Errorf("index 3: expected [3,4), got %v", g)
	}
}

// --- Structural invariant property test ---

func TestTrimHistory_StructuralInvariant_NoOrphanedPairs(t *testing.T) {
	// Property: for ANY input, TrimHistory's output never has:
	// 1. An assistant(tool_calls) without immediately following tool messages
	// 2. A tool message without a preceding assistant(tool_calls) or tool
	//
	// Test with various sizes and patterns.
	patterns := [][]ConversationEntry{
		// Pattern 1: alternating user/assistant with tool_calls
		makeToolCallHistory(MaxConversationTurns + 5),
		// Pattern 2: mixed — some turns have tool_calls, some don't
		makeMixedHistory(MaxConversationTurns + 10),
		// Pattern 3: large tool_calls groups (3 tools per call)
		makeLargeGroupHistory(MaxConversationTurns + 3),
	}

	for pi, entries := range patterns {
		trimmed := TrimHistory(entries)
		assertNoOrphanedPairs(t, trimmed, pi)
	}
}

func makeToolCallHistory(turns int) []ConversationEntry {
	var entries []ConversationEntry
	for i := 0; i < turns; i++ {
		entries = append(entries,
			ConversationEntry{Role: "user", Content: "q"},
			ConversationEntry{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{{"id": "c"}}},
			ConversationEntry{Role: "tool", Content: "r", ToolCallID: "c"},
		)
	}
	return entries
}

func makeMixedHistory(turns int) []ConversationEntry {
	var entries []ConversationEntry
	for i := 0; i < turns; i++ {
		entries = append(entries, ConversationEntry{Role: "user", Content: "q"})
		if i%3 == 0 {
			entries = append(entries,
				ConversationEntry{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{{"id": "c"}}},
				ConversationEntry{Role: "tool", Content: "r", ToolCallID: "c"},
			)
		} else {
			entries = append(entries, ConversationEntry{Role: "assistant", Content: "text"})
		}
	}
	return entries
}

func makeLargeGroupHistory(turns int) []ConversationEntry {
	var entries []ConversationEntry
	for i := 0; i < turns; i++ {
		entries = append(entries,
			ConversationEntry{Role: "user", Content: "q"},
			ConversationEntry{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{
				{"id": "a"}, {"id": "b"}, {"id": "c"},
			}},
			ConversationEntry{Role: "tool", Content: "ra", ToolCallID: "a"},
			ConversationEntry{Role: "tool", Content: "rb", ToolCallID: "b"},
			ConversationEntry{Role: "tool", Content: "rc", ToolCallID: "c"},
		)
	}
	return entries
}

func assertNoOrphanedPairs(t *testing.T, entries []ConversationEntry, patternIdx int) {
	t.Helper()
	for i, e := range entries {
		if e.Role == "assistant" && e.ToolCalls != nil {
			if i+1 >= len(entries) || entries[i+1].Role != "tool" {
				t.Errorf("pattern %d: assistant(tool_calls) at index %d has no following tool message (len=%d)",
					patternIdx, i, len(entries))
			}
		}
		if e.Role == "tool" && i == 0 {
			t.Errorf("pattern %d: orphaned tool message at index 0", patternIdx)
		}
		if e.Role == "tool" && i > 0 && entries[i-1].Role != "assistant" && entries[i-1].Role != "tool" {
			t.Errorf("pattern %d: tool at index %d preceded by %q", patternIdx, i, entries[i-1].Role)
		}
	}
}


// --- TrimHistory token-level trim test ---

func TestTrimHistory_TokenLevelTrim_NeverSplitsGroup(t *testing.T) {
	// Create entries within MaxConversationTurns but over MaxMemoryTokenEstimate.
	// Each tool result is huge to blow the token budget.
	bigContent := string(make([]byte, 10000)) // ~4000 tokens each
	var entries []ConversationEntry
	for i := 0; i < 10; i++ {
		entries = append(entries,
			ConversationEntry{Role: "user", Content: "q"},
			ConversationEntry{Role: "assistant", Content: "", ToolCalls: []map[string]interface{}{{"id": "c"}}},
			ConversationEntry{Role: "tool", Content: bigContent, ToolCallID: "c"},
		)
	}

	trimmed := TrimHistory(entries)
	assertNoOrphanedPairs(t, trimmed, 99)
}


func TestBuildEntryGroups_EmptyToolCallsSlice(t *testing.T) {
	// After JSON round-trip, an empty tool_calls array becomes a non-nil
	// []interface{}{} which is interface{}-non-nil. BuildEntryGroups must
	// treat this as "no tool calls" — not start a multi-entry group.
	entries := []ConversationEntry{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok", ToolCalls: []interface{}{}}, // empty but non-nil
		{Role: "user", Content: "bye"},
	}
	groups := BuildEntryGroups(entries)
	if len(groups) != 3 {
		t.Fatalf("expected 3 single-entry groups, got %d", len(groups))
	}
	for i, g := range groups {
		if g.End-g.Start != 1 {
			t.Errorf("group %d: expected size 1, got %d", i, g.End-g.Start)
		}
	}
}
