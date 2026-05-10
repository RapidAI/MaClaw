package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestToolMemoryRecallAdaptiveDebug(t *testing.T) {
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
			entry.SourceType = "manual"
		}
		if i == 1 {
			entry.Content = "Vue migration decision"
			entry.Tags = []string{"vue", "migration"}
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolMemory(store, map[string]interface{}{
		"action": "recall",
		"query":  "why compare react and vue migration decisions over time",
		"mode":   "adaptive",
		"debug":  true,
	})
	if !strings.Contains(out, "Adaptive plan:") {
		t.Fatalf("expected adaptive debug plan, got:\n%s", out)
	}
	if !strings.Contains(out, "source=") {
		t.Fatalf("expected adaptive debug provenance, got:\n%s", out)
	}
	if !strings.Contains(out, "React migration") || !strings.Contains(out, "Vue migration") {
		t.Fatalf("expected migration memories in output, got:\n%s", out)
	}
}

func TestToolMemoryThemes(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(memory.Entry{Content: "React migration decision", Category: memory.CategoryProjectKnowledge, Tags: []string{"react", "migration"}, Embedding: []float32{1, 0}, Status: memory.StatusActive, SourceType: "manual", SourceURL: "decision.md"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(memory.Entry{Content: "Vue migration decision", Category: memory.CategoryProjectKnowledge, Tags: []string{"vue", "migration"}, Embedding: []float32{0.98, 0.02}, Status: memory.StatusActive}); err != nil {
		t.Fatal(err)
	}

	out := ToolMemory(store, map[string]interface{}{"action": "themes", "limit": 5})
	if !strings.Contains(out, "members=") || !strings.Contains(out, "migration") {
		t.Fatalf("expected theme summary, got:\n%s", out)
	}

	out = ToolMemory(store, map[string]interface{}{"action": "themes", "limit": 5, "stats": true})
	if !strings.Contains(out, "theme_health:") || !strings.Contains(out, "coverage=") {
		t.Fatalf("expected theme health stats, got:\n%s", out)
	}

	out = ToolMemory(store, map[string]interface{}{"action": "themes", "limit": 5, "evidence": true, "evidence_limit": 2})
	if !strings.Contains(out, "evidence") || !strings.Contains(out, "decision.md") || !strings.Contains(out, "sim=") {
		t.Fatalf("expected theme evidence, got:\n%s", out)
	}

	if err := store.Save(memory.Entry{Content: "Loose note without embedding", Category: memory.CategoryProjectKnowledge, Tags: []string{"loose"}, Status: memory.StatusActive}); err != nil {
		t.Fatal(err)
	}
	out = ToolMemory(store, map[string]interface{}{"action": "themes", "diagnose": true, "issue_limit": 10})
	if !strings.Contains(out, "theme_diagnostics:") || !strings.Contains(out, "uncovered_entry") {
		t.Fatalf("expected theme diagnostics, got:\n%s", out)
	}
	out = ToolMemory(store, map[string]interface{}{"action": "themes", "plan": true, "issue_limit": 10})
	if !strings.Contains(out, "theme_maintenance_plan:") || !strings.Contains(out, "backfill_theme_inputs") {
		t.Fatalf("expected theme maintenance plan, got:\n%s", out)
	}
	out = ToolMemory(store, map[string]interface{}{"action": "themes", "apply": true, "issue_limit": 10})
	if !strings.Contains(out, "theme_maintenance_result:") || !strings.Contains(out, "rebuild_theme_layer") {
		t.Fatalf("expected theme maintenance result, got:\n%s", out)
	}
}

func TestToolMemoryRecallAutoUsesAdaptiveForComplexQuery(t *testing.T) {
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
			entry.SourceType = "manual"
		}
		if i == 1 {
			entry.Content = "Vue migration decision"
			entry.Tags = []string{"vue", "migration"}
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	out := ToolMemory(store, map[string]interface{}{
		"action": "recall",
		"query":  "compare react and vue migration decisions over time",
		"mode":   "auto",
		"debug":  true,
	})
	if !strings.Contains(out, "Adaptive plan:") {
		t.Fatalf("expected auto mode to use adaptive debug plan, got:\n%s", out)
	}
}

func TestToolMemoryRecallRejectsUnknownMode(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	out := ToolMemory(store, map[string]interface{}{
		"action": "recall",
		"query":  "anything",
		"mode":   "bogus",
	})
	if !strings.Contains(out, "unknown") && !strings.Contains(out, "未知") {
		t.Fatalf("expected unknown mode response, got %q", out)
	}
}
