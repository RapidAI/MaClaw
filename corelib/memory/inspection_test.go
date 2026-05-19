package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbedStatusForToolFormatsCounts(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{Content: "with embedding", Embedding: []float32{1}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Content: "without embedding", Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	status := store.EmbedStatusForTool()
	if status.TotalEntries != 2 || status.WithEmbeddings != 1 || status.WithoutEmbeddings != 1 {
		t.Fatalf("unexpected embed status: %+v", status)
	}
	out := FormatEmbedStatusForTool(status)
	if !strings.Contains(out, "Embedding Status:") || !strings.Contains(out, "With embeddings:         1") {
		t.Fatalf("unexpected embed status output:\n%s", out)
	}
}

func TestStrengthForToolSortsAndFormats(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "strong", Content: "strong entry", Strength: 1, UpdatedAt: now, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{ID: "weak", Content: "weak entry", Strength: 0.01, UpdatedAt: now, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	items := store.StrengthForTool(now)
	if len(items) != 2 || items[0].Entry.ID != "weak" || !items[0].Dormant {
		t.Fatalf("unexpected strength snapshots: %+v", items)
	}
	out := FormatStrengthForTool(items)
	if !strings.Contains(out, "STRENGTH") || !strings.Contains(out, "weak") || !strings.Contains(out, "* ") {
		t.Fatalf("unexpected strength output:\n%s", out)
	}
}

func TestGraphAndInferenceFormattersForTool(t *testing.T) {
	neighbors := []GraphNeighborSnapshot{{ID: "b", Strength: 0.75, Content: "neighbor content"}}
	graphOut := FormatGraphNeighborsForTool("a", neighbors)
	if !strings.Contains(graphOut, "Graph neighbors for a") || !strings.Contains(graphOut, "0.7500") {
		t.Fatalf("unexpected graph output:\n%s", graphOut)
	}
	if out := FormatGraphNeighborsForTool("missing", nil); !strings.Contains(out, "No graph neighbors") {
		t.Fatalf("unexpected empty graph output: %s", out)
	}

	infer := InferenceResult{QueryEntities: []string{"react"}, Derived: []DerivedFact{{Subject: "react", Predicate: "relates_to", Object: "migration", Confidence: 0.9, RuleName: "test_rule", Explanation: "because"}}, GraphEntities: 2, GraphFacts: 1, RuleCount: 1}
	inferOut := FormatInferenceResultForTool("react migration", infer)
	if !strings.Contains(inferOut, "Query entities: [react]") || !strings.Contains(inferOut, "Derived facts (1)") || !strings.Contains(inferOut, "Rules: 1") {
		t.Fatalf("unexpected inference output:\n%s", inferOut)
	}
}
