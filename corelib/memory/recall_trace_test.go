package memory

import (
	"path/filepath"
	"testing"
)

func TestLastRecallTraceCapturesSignals(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entry := Entry{ID: "artifact-1", Content: "任务侧栏支持记忆证据导航和产物来源回查", Category: CategoryTaskArtifact, SourceType: "workflow_output_ref"}
	if err := store.Save(entry); err != nil {
		t.Fatal(err)
	}

	results := store.RecallDynamic("证据导航", "", "")
	if len(results) == 0 {
		t.Fatal("expected recall result")
	}

	trace := store.LastRecallTrace()
	if trace.Query != "证据导航" {
		t.Fatalf("trace query = %q", trace.Query)
	}
	if trace.BM25Hits == 0 {
		t.Fatalf("expected BM25 hits in trace: %+v", trace)
	}
	if !containsRecallTraceToken(trace.BM25Tokens, "证据") || !containsRecallTraceToken(trace.BM25Tokens, "导航") {
		t.Fatalf("expected CJK BM25 tokens in trace: %+v", trace.BM25Tokens)
	}
	if trace.SourceCounts["workflow_output_ref"] == 0 {
		t.Fatalf("expected source count for workflow_output_ref: %+v", trace.SourceCounts)
	}
}

func TestLastRecallTraceReturnsCopy(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{Content: "alpha beta memory", Category: CategoryProjectKnowledge}); err != nil {
		t.Fatal(err)
	}
	_ = store.RecallDynamic("alpha", "", "")

	trace := store.LastRecallTrace()
	trace.BM25Tokens = append(trace.BM25Tokens, "mutated")
	trace.SourceCounts["mutated"] = 99

	fresh := store.LastRecallTrace()
	if containsRecallTraceToken(fresh.BM25Tokens, "mutated") || fresh.SourceCounts["mutated"] != 0 {
		t.Fatalf("LastRecallTrace leaked mutable internals: %+v", fresh)
	}
}

func containsRecallTraceToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}
