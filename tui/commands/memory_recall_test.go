package commands

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestMemoryRecallByModeAdaptiveReturnsPlan(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	for i := 0; i < 16; i++ {
		entry := memory.Entry{
			Content:   "migration decision evidence",
			Category:  memory.CategoryProjectKnowledge,
			Tags:      []string{"migration"},
			Embedding: []float32{1, float32(i) / 1000},
			Status:    memory.StatusActive,
		}
		if i == 0 {
			entry.Content = "React migration decision"
			entry.Tags = []string{"react", "migration"}
		}
		if i == 1 {
			entry.Content = "Vue migration decision"
			entry.Tags = []string{"vue", "migration"}
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	entries, plan, err := memoryRecallByMode(store, "why compare react and vue migration decisions over time", "", "adaptive", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("expected adaptive plan")
	}
	if len(entries) == 0 || len(entries) > 5 {
		t.Fatalf("unexpected entries length %d", len(entries))
	}
	if len(plan.SelectedThemes) == 0 {
		t.Fatalf("expected selected themes in plan: %+v", plan)
	}
}

func TestMemoryRecallByModeRejectsUnknownMode(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if _, _, err := memoryRecallByMode(store, "query", "", "bogus", "", 10); err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestMemoryRecallByModeAutoUsesAdaptiveForComplexQuery(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	for i := 0; i < 16; i++ {
		entry := memory.Entry{
			Content:   "migration decision evidence",
			Category:  memory.CategoryProjectKnowledge,
			Tags:      []string{"migration"},
			Embedding: []float32{1, float32(i) / 1000},
			Status:    memory.StatusActive,
		}
		if i == 0 {
			entry.Content = "React migration decision"
			entry.Tags = []string{"react", "migration"}
		}
		if i == 1 {
			entry.Content = "Vue migration decision"
			entry.Tags = []string{"vue", "migration"}
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	_, plan, err := memoryRecallByMode(store, "compare react and vue migration decisions over time", "", "auto", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("expected auto mode to choose adaptive plan for complex query")
	}
}
