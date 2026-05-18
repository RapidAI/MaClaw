package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleToolSupportsLightMemRecall(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "Project snake game uses ebiten and test command is go test ./...", Category: CategoryProjectKnowledge, Tags: []string{"snake", "ebiten"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	out := HandleTool(store, map[string]interface{}{"action": "recall", "mode": "lightmem", "query": "continue snake game tests", "debug": true}, ToolOptions{})
	if !strings.Contains(out, "Recalled 1 relevant memories") || !strings.Contains(out, "LightMem plan") {
		t.Fatalf("unexpected lightmem recall output:\n%s", out)
	}
}

func TestHandleToolReportsMemoryCandidates(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "I just ran the current task and it is probably okay", Category: CategoryProjectKnowledge, Tags: []string{memoryCandidateTag}, Status: StatusDormant}); err != nil {
		t.Fatal(err)
	}

	out := HandleTool(store, map[string]interface{}{"action": "memory_candidates"}, ToolOptions{})
	if !strings.Contains(out, "Memory candidates: 1") || !strings.Contains(out, "action=quarantine") {
		t.Fatalf("unexpected candidates output:\n%s", out)
	}
}

func TestStoreStatsAndFormatterIncludeCandidates(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "Active project memory", Category: CategoryProjectKnowledge, Status: StatusActive, Tags: []string{"project"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Content: "thanks", Category: CategoryProjectKnowledge, Status: StatusDormant, Tags: []string{memoryCandidateTag}, Stale: true}); err != nil {
		t.Fatal(err)
	}

	stats := store.Stats()
	if stats.Total != 2 || stats.Active != 1 || stats.Dormant != 1 || stats.Candidates.Total != 1 || stats.Candidates.Stale != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	formatted := FormatStoreStatsForTool(stats)
	if !strings.Contains(formatted, "Candidates:       1") || !strings.Contains(formatted, "Memory Store Stats") {
		t.Fatalf("unexpected formatted stats:\n%s", formatted)
	}
}

func TestHandleToolSaveUsesGovernance(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	out := HandleTool(store, map[string]interface{}{
		"action":   "save",
		"content":  "I just ran the current task and it is probably okay",
		"category": "project_knowledge",
	}, ToolOptions{})
	if !strings.Contains(out, "Memory saved as candidate") {
		t.Fatalf("expected weak tool save to be quarantined, got: %s", out)
	}
	candidates := store.ListMemoryCandidates("current task", 10)
	if len(candidates) != 1 {
		t.Fatalf("expected one quarantined candidate, got %d", len(candidates))
	}

	out = HandleTool(store, map[string]interface{}{
		"action":   "save",
		"content":  "Project API endpoint is https://api.example.com and test command is pnpm test",
		"category": "project_knowledge",
	}, ToolOptions{})
	if !strings.Contains(out, "Memory saved:") {
		t.Fatalf("expected durable project memory to be accepted, got: %s", out)
	}
}

func TestMemoryToolActionNormalizationAndSchema(t *testing.T) {
	if got := NormalizeMemoryToolAction("memory_candidates"); got != MemoryToolActionCandidates {
		t.Fatalf("NormalizeMemoryToolAction(memory_candidates) = %q", got)
	}
	if got := NormalizeMemoryToolAction("scene_index"); got != MemoryToolActionScenes {
		t.Fatalf("NormalizeMemoryToolAction(scene_index) = %q", got)
	}
	if !MemoryToolActionTrace.IsRecallOnlyAllowed() || MemoryToolActionSave.IsRecallOnlyAllowed() {
		t.Fatal("unexpected recall-only action policy")
	}

	def := ToolDefinitionSchema()
	if def.Properties["mode"] == nil || def.Properties["project_path"] == nil || def.Properties["diagnose"] == nil {
		t.Fatalf("memory tool schema missing shared properties: %#v", def.Properties)
	}
	if !strings.Contains(def.Description, "lightmem") || !strings.Contains(def.Description, "candidates") {
		t.Fatalf("memory tool schema description missing shared capabilities: %q", def.Description)
	}
}
