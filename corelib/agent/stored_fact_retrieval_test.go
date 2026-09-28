package agent

import (
	"strings"
	"testing"
)

func TestShouldNudgeStoredRetrieval(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "ssh"}},
	}
	if !shouldNudgeStoredRetrieval(false, tools, nil, 0) {
		t.Fatal("a full turn that has not called a tool must look up stored facts")
	}
	if shouldNudgeStoredRetrieval(true, tools, nil, 0) {
		t.Fatal("a light turn must not be forced to search storage")
	}
	if shouldNudgeStoredRetrieval(false, tools, nil, 1) {
		t.Fatal("the retrieval nudge is one-shot")
	}
	used := []ConversationEntry{{Role: "tool", ToolName: "web_search"}}
	if shouldNudgeStoredRetrieval(false, tools, used, 0) {
		t.Fatal("a turn that already called a tool must not be nudged")
	}
	onlySSH := []map[string]interface{}{{"function": map[string]interface{}{"name": "ssh"}}}
	if shouldNudgeStoredRetrieval(false, onlySSH, nil, 0) {
		t.Fatal("nothing to force when memory and knowledge_search are absent")
	}
	lookupOnly := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
	}
	if shouldNudgeStoredRetrieval(false, lookupOnly, nil, 0) {
		t.Fatal("a lookup surface must not be forced to search storage before answering")
	}
	readOnly := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "read_file"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "knowledge_search"}},
	}
	if shouldNudgeStoredRetrieval(false, readOnly, nil, 0) {
		t.Fatal("a read-only file surface must not be forced to search storage")
	}
}

func TestRetrievalToolsStillPending(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "web_search"}},
	}
	if !retrievalToolsStillPending(tools, nil) {
		t.Fatal("uncalled memory and knowledge_search are pending")
	}
	called := []ConversationEntry{{Role: "tool", ToolName: "memory"}, {Role: "tool", ToolName: "knowledge_search"}}
	if retrievalToolsStillPending(tools, called) {
		t.Fatal("both retrieval tools already ran")
	}
	onlyWeb := []map[string]interface{}{{"function": map[string]interface{}{"name": "web_search"}}}
	if retrievalToolsStillPending(onlyWeb, nil) {
		t.Fatal("no retrieval tool on the surface means nothing to force")
	}
}

func TestStoredOperationalFactNudgeOrdersMemoryThenKnowledge(t *testing.T) {
	nudge := StoredOperationalFactNudge()
	mem := strings.Index(nudge, "memory")
	kb := strings.Index(nudge, "knowledge_search")
	if mem < 0 || kb < 0 || mem > kb {
		t.Fatalf("nudge must name memory before knowledge_search: %s", nudge)
	}
	if !strings.Contains(nudge, "冲突") || !strings.Contains(nudge, "直接用") {
		t.Fatalf("nudge must use agreement and ask only on conflict: %s", nudge)
	}
}
