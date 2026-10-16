package memory

import (
	"path/filepath"
	"strings"
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

func TestLastRecallTraceForOwnerDoesNotCrossUsers(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.SaveForUser(Entry{Content: "alpha editor is vim", Category: CategoryProjectKnowledge, OwnerID: "user-a"}, "user-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveForUser(Entry{Content: "beta editor is emacs", Category: CategoryProjectKnowledge, OwnerID: "user-b"}, "user-b"); err != nil {
		t.Fatal(err)
	}
	_ = store.RecallDynamic("alpha editor", "", "", "user-a")
	_ = store.RecallDynamic("beta editor", "", "", "user-b")

	if got := store.LastRecallTraceForOwner("user-a").Query; got != "alpha editor" {
		t.Fatalf("user-a trace query = %q", got)
	}
	if got := store.LastRecallTraceForOwner("user-b").Query; got != "beta editor" {
		t.Fatalf("user-b trace query = %q", got)
	}
	if got := store.LastRecallTraceForOwner("user-c").Query; got != "" {
		t.Fatalf("missing owner trace = %q", got)
	}
}

func TestProactiveRecallExcludesCanonicalUserCategory(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{Content: "常用编辑器是 vim", Category: CategoryUser, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if got := store.RecallDynamic("编辑器", "", ""); len(got) != 0 {
		t.Fatalf("proactive recall included canonical user fact: %+v", got)
	}
	if got := store.RecallDynamicForTool("编辑器", "", ""); len(got) == 0 {
		t.Fatal("tool recall should still return a user-category fact")
	}
}

func TestQuarantineCandidateDoesNotEvictActiveMemory(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	store.mu.Lock()
	store.maxItems = 2
	store.mu.Unlock()

	for _, entry := range []Entry{
		{Content: "active memory one stays", Category: CategoryProjectKnowledge, AccessCount: 5, Status: StatusActive},
		{Content: "active memory two stays", Category: CategoryProjectKnowledge, AccessCount: 5, Status: StatusActive},
		{Content: "quarantine this weak note", Category: CategoryProjectKnowledge, Status: StatusDormant, Tags: []string{memoryCandidateTag}},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}
	active := 0
	for _, entry := range store.List("", "") {
		if entry.Status == StatusDormant {
			t.Fatalf("candidate stayed in the hot set: %+v", entry)
		}
		active++
	}
	if active != 2 {
		t.Fatalf("active count = %d", active)
	}
}

func TestPaginationRankingMatchesDynamicRecall(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	for _, content := range []string{
		"alpha editor vim configuration",
		"beta editor emacs configuration",
		"gamma database backup window",
	} {
		if err := store.Save(Entry{Content: content, Category: CategoryProjectKnowledge, Status: StatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	dynamic := store.RecallDynamic("editor configuration", "", "")
	scored := store.recallScoredForPagination("editor configuration", "", "", "")
	if len(dynamic) == 0 || len(scored) < len(dynamic) {
		t.Fatalf("dynamic=%d scored=%d", len(dynamic), len(scored))
	}
	for i, entry := range dynamic {
		if scored[i].entry.ID != entry.ID {
			t.Fatalf("rank %d dynamic %s scored %s", i, entry.ID, scored[i].entry.ID)
		}
	}
}

func TestFormatRecallResultReportsLockTimeout(t *testing.T) {
	got := FormatRecallResultForTool(nil, "editor", ToolRecallResult{
		Trace: RecallTrace{Query: "editor", LockTimedOut: true},
	}, true, false)
	if !strings.Contains(got, "timed out") || !strings.Contains(got, "lock_timed_out=true") {
		t.Fatalf("timeout result = %q", got)
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
