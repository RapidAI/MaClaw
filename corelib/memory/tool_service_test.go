package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestRecallByModeDefaultsToAuto(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	result, err := store.RecallByMode("compare api design tradeoffs over time", "", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.NormalizedMode != "auto" {
		t.Fatalf("empty mode should normalize to auto, got %q", result.NormalizedMode)
	}
	if result.AdaptivePlan == nil {
		t.Fatalf("auto mode should use adaptive path for complex queries")
	}
}

func TestRecallByModeAutoRespectsOwnerBoundary(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{
			ID:          "owner-a-derived",
			Content:     "auto boundary target prefers careful evidence gates",
			Category:    CategoryPreference,
			Scope:       ScopeGlobal,
			CreatedAt:   now,
			UpdatedAt:   now,
			Strength:    1,
			DerivedKind: "profile",
			Boundary:    &MemoryBoundary{OwnerID: "owner-a"},
		},
		{
			ID:        "shared-derived",
			Content:   "auto boundary target keeps shared recall behavior",
			Category:  CategoryPreference,
			Scope:     ScopeGlobal,
			CreatedAt: now,
			UpdatedAt: now,
			Strength:  1,
		},
	})
	store.mu.Unlock()

	result, err := store.RecallByMode("auto boundary target", "", "auto", "", 0, "owner-b")
	if err != nil {
		t.Fatal(err)
	}
	if containsEntryID(result.Entries, "owner-a-derived") {
		t.Fatalf("auto recall should enforce owner boundary: %+v", result.Entries)
	}
	if !containsEntryID(result.Entries, "shared-derived") {
		t.Fatalf("auto recall should preserve shared entries: %+v", result.Entries)
	}
}

func TestMemoryToolActionNormalizationAndSchema(t *testing.T) {
	if got := NormalizeMemoryToolAction("memory_candidates"); got != MemoryToolActionCandidates {
		t.Fatalf("NormalizeMemoryToolAction(memory_candidates) = %q", got)
	}
	if got := NormalizeMemoryToolAction("scene_index"); got != MemoryToolActionScenes {
		t.Fatalf("NormalizeMemoryToolAction(scene_index) = %q", got)
	}
	if !MemoryToolActionTrace.IsRecallOnlyAllowed() || !MemoryToolActionDerived.IsRecallOnlyAllowed() || MemoryToolActionDerivedSurgery.IsRecallOnlyAllowed() || MemoryToolActionSave.IsRecallOnlyAllowed() {
		t.Fatal("unexpected recall-only action policy")
	}

	def := ToolDefinitionSchema()
	if def.Properties["mode"] == nil || def.Properties["project_path"] == nil || def.Properties["diagnose"] == nil {
		t.Fatalf("memory tool schema missing shared properties: %#v", def.Properties)
	}
	if !strings.Contains(def.Description, "lightmem") || !strings.Contains(def.Description, "candidates") || !strings.Contains(def.Description, "derived") || !strings.Contains(def.Description, "derived_surgery") {
		t.Fatalf("memory tool schema description missing shared capabilities: %q", def.Description)
	}
}

func TestMemoryCandidatesForToolFormatsConsolidation(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		Content:    "Project API endpoint is https://api.example.com and build command is pnpm test",
		Category:   CategoryProjectKnowledge,
		Tags:       []string{memoryCandidateTag, "api"},
		Status:     StatusDormant,
		SourceType: "memory_candidate",
	}); err != nil {
		t.Fatal(err)
	}

	result := store.MemoryCandidatesForTool(context.Background(), "api", 10, true)
	out := FormatMemoryCandidatesResultForTool(result)
	if result.Consolidation == nil || result.Consolidation.Promoted != 1 {
		t.Fatalf("expected promoted candidate consolidation, got %+v", result.Consolidation)
	}
	if !strings.Contains(out, "Candidate consolidation: scanned=1 promoted=1") || !strings.Contains(out, "No memory candidates found.") {
		t.Fatalf("unexpected candidate result format:\n%s", out)
	}
}

func TestMemoryThemesForToolSharedRenderAndJSONPayload(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	for _, entry := range []Entry{
		{Content: "React migration decision", Category: CategoryProjectKnowledge, Tags: []string{"react", "migration"}, Embedding: []float32{1, 0}, Status: StatusActive},
		{Content: "Vue migration decision", Category: CategoryProjectKnowledge, Tags: []string{"vue", "migration"}, Embedding: []float32{0.98, 0.02}, Status: StatusActive},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	opts := ToolThemesOptions{Limit: 5, Stats: true, EvidenceLimit: 1, Diagnose: true, IssueLimit: 10}
	result := store.MemoryThemesForTool(opts)
	out := FormatMemoryThemesResultForTool(result, opts)
	if !strings.Contains(out, "theme_health:") || !strings.Contains(out, "theme_diagnostics:") || !strings.Contains(out, "Memory themes:") {
		t.Fatalf("unexpected theme result format:\n%s", out)
	}
	payload, ok := MemoryThemesJSONPayloadForTool(result, opts).(map[string]interface{})
	if !ok {
		t.Fatalf("expected diagnostic payload map")
	}
	if payload["diagnostics"] == nil || payload["explanations"] == nil {
		t.Fatalf("payload should preserve diagnostics and evidence: %+v", payload)
	}
}
