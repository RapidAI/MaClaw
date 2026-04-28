package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// mockLLMCaller is a test double for LLMChatCaller that tracks calls.
type mockLLMCaller struct {
	configured bool
	calls      int
	response   string
	err        error
}

func (m *mockLLMCaller) ChatCall(messages []map[string]string) (string, error) {
	m.calls++
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func (m *mockLLMCaller) IsConfigured() bool {
	return m.configured
}

// genRole generates a random message role from a realistic set.
func genRole() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{"user", "assistant", "tool", "system", "function"})
}

// Feature: memory-claude-style-upgrade, Property 2: Conversation filter retains only user and assistant messages
// **Validates: Requirements 2.1**
//
// For any conversation history with mixed roles, filterMessages keeps
// user, assistant, and tool messages (tool results truncated) while
// filtering out system and developer messages. Order is preserved.
func TestProperty_ConversationFilter(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random conversation with mixed roles.
		n := rapid.IntRange(0, 50).Draw(rt, "numMessages")
		messages := make([]ConversationMessage, n)
		for i := 0; i < n; i++ {
			messages[i] = ConversationMessage{
				Role:    genRole().Draw(rt, "role"),
				Content: rapid.StringMatching(`[a-zA-Z0-9 ]{1,50}`).Draw(rt, "content"),
			}
		}

		ke := &KnowledgeExtractor{}
		filtered := ke.filterMessages(messages)

		// Property 1: All filtered messages have an allowed role.
		allowedRoles := map[string]bool{"user": true, "assistant": true, "tool": true}
		for i, m := range filtered {
			if !allowedRoles[m.Role] {
				rt.Fatalf("filtered[%d] has unexpected role %q", i, m.Role)
			}
		}

		// Property 2: Order is preserved — filtered is a subsequence of messages.
		// Tool content may be truncated, so check that original starts with filtered.
		j := 0
		for i := 0; i < len(messages) && j < len(filtered); i++ {
			if messages[i].Role == filtered[j].Role &&
				(messages[i].Content == filtered[j].Content || strings.HasPrefix(messages[i].Content, filtered[j].Content)) {
				j++
			}
		}
		if j != len(filtered) {
			rt.Fatalf("filtered messages are not a subsequence of original messages")
		}

		// Property 3: system and developer messages are excluded.
		for _, m := range filtered {
			if m.Role == "system" || m.Role == "developer" {
				rt.Fatalf("system/developer message leaked through filter: role=%q", m.Role)
			}
		}

		// Property 4: Count matches expected (user + assistant + tool).
		expected := 0
		for _, m := range messages {
			switch m.Role {
			case "user":
				if !isMemoryExcludedUserContent(m.Content) {
					expected++
				}
			case "assistant", "tool":
				expected++
			}
		}
		if len(filtered) != expected {
			rt.Fatalf("expected %d filtered messages, got %d", expected, len(filtered))
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 3: Cooldown enforcement
// **Validates: Requirements 2.4**
//
// Two Extract calls within the cooldown period: the second is a no-op
// (no new entries saved to the store).
func TestProperty_CooldownEnforcement(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{6}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		llm := &mockLLMCaller{
			configured: true,
			response:   `[{"content":"test knowledge point","category":"project_knowledge"}]`,
		}

		ke := NewKnowledgeExtractor(store, llm)
		// Use a long cooldown to ensure second call is within it.
		ke.cooldown = 1 * time.Hour

		// Generate random conversation messages (at least 1 user + 1 assistant).
		msgs := []ConversationMessage{
			{Role: "user", Content: rapid.StringMatching(`[a-zA-Z ]{5,30}`).Draw(rt, "userMsg")},
			{Role: "assistant", Content: rapid.StringMatching(`[a-zA-Z ]{5,30}`).Draw(rt, "assistMsg")},
		}

		// First call should succeed and save entries.
		if err := ke.Extract("testuser", msgs); err != nil {
			rt.Fatalf("first Extract failed: %v", err)
		}

		store.mu.RLock()
		countAfterFirst := len(store.entries)
		store.mu.RUnlock()
		callsAfterFirst := llm.calls

		// Second call within cooldown should be a no-op.
		if err := ke.Extract("testuser", msgs); err != nil {
			rt.Fatalf("second Extract failed: %v", err)
		}

		store.mu.RLock()
		countAfterSecond := len(store.entries)
		store.mu.RUnlock()

		if countAfterSecond != countAfterFirst {
			rt.Fatalf("cooldown violated: entries changed from %d to %d", countAfterFirst, countAfterSecond)
		}
		if llm.calls != callsAfterFirst {
			rt.Fatalf("cooldown violated: LLM called %d times after first, %d after second", callsAfterFirst, llm.calls)
		}
	})
}

// Feature: memory-claude-style-upgrade, Property 4: Deduplication of extracted knowledge
// **Validates: Requirements 2.7**
//
// Extracted knowledge with content identical to existing entries does not
// create duplicates. Store entry count increases only by genuinely new points.
func TestProperty_ExtractedDedup(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		dir := t.TempDir()
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{6}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Pre-populate store with some existing entries.
		nExisting := rapid.IntRange(1, 5).Draw(rt, "nExisting")
		existingContents := make([]string, nExisting)
		for i := 0; i < nExisting; i++ {
			content := rapid.StringMatching(`[a-zA-Z]{20,40}`).Draw(rt, "existContent")
			existingContents[i] = content
			if err := store.Save(Entry{
				Content:  content,
				Category: CategoryProjectKnowledge,
				Tags:     []string{"existing"},
			}); err != nil {
				rt.Fatal(err)
			}
		}

		store.mu.RLock()
		countBefore := len(store.entries)
		store.mu.RUnlock()

		// Build LLM response: mix of duplicates and new content.
		nNew := rapid.IntRange(0, 3).Draw(rt, "nNew")
		nDup := rapid.IntRange(0, nExisting).Draw(rt, "nDup")

		var points []string
		newContents := make(map[string]bool)
		for i := 0; i < nNew; i++ {
			c := "new_knowledge_" + rapid.StringMatching(`[a-zA-Z]{20,30}`).Draw(rt, "newContent")
			points = append(points, `{"content":"`+c+`","category":"project_knowledge"}`)
			newContents[c] = true
		}
		for i := 0; i < nDup; i++ {
			// Pick an existing content to duplicate.
			idx := rapid.IntRange(0, len(existingContents)-1).Draw(rt, "dupIdx")
			c := existingContents[idx]
			points = append(points, `{"content":"`+c+`","category":"project_knowledge"}`)
		}

		llmResp := "[" + joinStrings(points, ",") + "]"

		llm := &mockLLMCaller{
			configured: true,
			response:   llmResp,
		}

		ke := NewKnowledgeExtractor(store, llm)
		ke.cooldown = 0 // Disable cooldown for testing.

		msgs := []ConversationMessage{
			{Role: "user", Content: "test question"},
			{Role: "assistant", Content: "test answer"},
		}

		if err := ke.Extract("testuser", msgs); err != nil {
			rt.Fatalf("Extract failed: %v", err)
		}

		store.mu.RLock()
		countAfter := len(store.entries)
		store.mu.RUnlock()

		// Only genuinely new entries should be added.
		added := countAfter - countBefore
		if added > nNew {
			rt.Fatalf("dedup failed: added %d entries but only %d were new (had %d dups)", added, nNew, nDup)
		}
	})
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}
