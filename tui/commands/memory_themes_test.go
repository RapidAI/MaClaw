package commands

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestMemoryThemesCommandRuns(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.Entry{Content: "React migration decision", Category: memory.CategoryProjectKnowledge, Tags: []string{"react", "migration"}, Embedding: []float32{1, 0}, Status: memory.StatusActive, SourceType: "manual", SourceURL: "decision.md"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.Entry{Content: "Vue migration decision", Category: memory.CategoryProjectKnowledge, Tags: []string{"vue", "migration"}, Embedding: []float32{0.98, 0.02}, Status: memory.StatusActive}); err != nil {
		t.Fatal(err)
	}
	store.Stop()

	if err := memoryThemes(dir, []string{"--limit", "5", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := memoryThemes(dir, []string{"--limit", "5", "--stats", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := memoryThemes(dir, []string{"--limit", "5", "--evidence", "2", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := memoryThemes(dir, []string{"--limit", "5", "--diagnose", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := memoryThemes(dir, []string{"--limit", "5", "--plan", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := memoryThemes(dir, []string{"--limit", "5", "--apply", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := memoryStats(dir); err != nil {
		t.Fatal(err)
	}
}
