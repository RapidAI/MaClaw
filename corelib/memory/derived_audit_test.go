package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDerivedMemoryAuditsReportsIssuesAndBoundary(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "evidence-1", Content: "raw evidence", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{
			ID:          "schema-1",
			Content:     "derived schema with one missing evidence",
			Category:    CategoryProjectKnowledge,
			DerivedKind: "schema:recurring",
			EvidenceIDs: []string{"evidence-1", "missing-evidence"},
			Boundary:    &MemoryBoundary{OwnerID: "owner-a", ProjectPath: `D:\workprj\alpha`, SourceScope: "conversation"},
			CreatedAt:   now,
			UpdatedAt:   now,
			Strength:    1,
		},
		{
			ID:          "profile-owner-b",
			Content:     "other owner derived profile",
			Category:    CategoryProfile,
			DerivedKind: "profile",
			EvidenceIDs: []string{"evidence-1"},
			Boundary:    &MemoryBoundary{OwnerID: "owner-b"},
			CreatedAt:   now,
			UpdatedAt:   now,
			Strength:    1,
		},
		{
			ID:          "schema-no-boundary",
			Content:     "derived schema without boundary",
			Category:    CategoryProjectKnowledge,
			DerivedKind: "schema:insight",
			CreatedAt:   now,
			UpdatedAt:   now,
			Strength:    1,
		},
	})
	store.mu.Unlock()

	audits := store.DerivedMemoryAudits(`D:\workprj\alpha`, "owner-a", 10)
	if len(audits) != 2 {
		t.Fatalf("expected visible owner-a and boundaryless derived audits, got %+v", audits)
	}
	var schemaAudit DerivedMemoryAudit
	for _, audit := range audits {
		if audit.EntryID == "schema-1" {
			schemaAudit = audit
		}
		if audit.EntryID == "profile-owner-b" {
			t.Fatalf("owner-b derived memory should be filtered out: %+v", audits)
		}
	}
	if schemaAudit.EntryID == "" {
		t.Fatalf("schema-1 audit missing: %+v", audits)
	}
	if len(schemaAudit.MissingEvidenceIDs) != 1 || schemaAudit.MissingEvidenceIDs[0] != "missing-evidence" {
		t.Fatalf("expected missing evidence issue, got %+v", schemaAudit)
	}
	if !derivedAuditStringSliceContains(schemaAudit.Issues, "missing_evidence_entries") {
		t.Fatalf("expected missing_evidence_entries issue, got %+v", schemaAudit.Issues)
	}
}

func TestDerivedMemoryAuditsRequireExplicitBoundaryContext(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{
			ID:          "schema-bounded",
			Content:     "owner and project bounded derived schema",
			Category:    CategoryProjectKnowledge,
			DerivedKind: "schema:recurring",
			EvidenceIDs: []string{"raw-1"},
			Boundary:    &MemoryBoundary{OwnerID: "owner-a", ProjectPath: `D:\workprj\alpha`},
			CreatedAt:   now,
			UpdatedAt:   now,
			Strength:    1,
		},
		{
			ID:          "schema-open",
			Content:     "unbounded legacy derived schema",
			Category:    CategoryProjectKnowledge,
			DerivedKind: "schema:insight",
			CreatedAt:   now,
			UpdatedAt:   now,
			Strength:    1,
		},
	})
	store.mu.Unlock()

	if audits := store.DerivedMemoryAudits("", "", 10); len(audits) != 1 || audits[0].EntryID != "schema-open" {
		t.Fatalf("bounded derived memory should require explicit context, got %+v", audits)
	}
	if audits := store.DerivedMemoryAudits(`D:\workprj\alpha`, "", 10); len(audits) != 1 || audits[0].EntryID != "schema-open" {
		t.Fatalf("owner-bounded derived memory should require owner context, got %+v", audits)
	}
	if audits := store.DerivedMemoryAudits(`D:\workprj\alpha`, "owner-a", 10); len(audits) != 2 {
		t.Fatalf("expected bounded and legacy audits with full context, got %+v", audits)
	}
}
func TestHandleToolDerivedAuditFormatsEvidenceAndIssues(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{{
		ID:          "schema-tool",
		Content:     "tool visible derived schema",
		Category:    CategoryProjectKnowledge,
		DerivedKind: "schema:recurring",
		EvidenceIDs: []string{"missing-evidence"},
		Boundary:    &MemoryBoundary{OwnerID: "owner-a"},
		CreatedAt:   now,
		UpdatedAt:   now,
		Strength:    1,
	}})
	store.mu.Unlock()

	out := HandleTool(store, map[string]interface{}{"action": "derived", "limit": 5}, ToolOptions{OwnerID: "owner-a"})
	for _, want := range []string{"Derived memories: 1", "schema-tool", "kind=schema:recurring", "evidence=missing-evidence", "missing_evidence_entries", "owner_id=owner-a"} {
		if !strings.Contains(out, want) {
			t.Fatalf("derived audit output missing %q in:\n%s", want, out)
		}
	}
}

func derivedAuditStringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSupersedeDerivedMemoryOnlyInvalidatesDerivedEntry(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{
		{ID: "raw-1", Content: "raw evidence should remain active", Category: CategoryProjectKnowledge, CreatedAt: now, UpdatedAt: now, Strength: 1},
		{
			ID:          "schema-1",
			Content:     "derived schema to supersede",
			Category:    CategoryProjectKnowledge,
			DerivedKind: "schema:recurring",
			EvidenceIDs: []string{"raw-1"},
			Boundary:    &MemoryBoundary{OwnerID: "owner-a", ProjectPath: `D:\workprj\alpha`},
			CreatedAt:   now,
			UpdatedAt:   now,
			Strength:    1,
		},
		{
			ID:          "schema-owner-only",
			OwnerID:     "owner-a",
			Content:     "legacy derived schema with entry owner but no boundary",
			Category:    CategoryProjectKnowledge,
			DerivedKind: "schema:legacy",
			CreatedAt:   now,
			UpdatedAt:   now,
			Strength:    1,
		},
	})
	store.mu.Unlock()

	if err := store.SupersedeDerivedMemory("raw-1", `D:\workprj\alpha`, "owner-a"); err == nil {
		t.Fatal("raw evidence should not be superseded by derived surgery")
	}
	if err := store.SupersedeDerivedMemory("schema-1", `D:\workprj\beta`, "owner-a"); err == nil {
		t.Fatal("derived surgery should enforce project boundary")
	}
	if err := store.SupersedeDerivedMemory("schema-1", "", "owner-a"); err == nil || !strings.Contains(err.Error(), "requires project boundary") {
		t.Fatalf("derived surgery should require explicit project boundary, got %v", err)
	}
	if err := store.SupersedeDerivedMemory("schema-1", `D:\workprj\alpha`, ""); err == nil || !strings.Contains(err.Error(), "requires owner boundary") {
		t.Fatalf("derived surgery should require explicit owner boundary, got %v", err)
	}
	if err := store.SupersedeDerivedMemory("schema-owner-only", "", ""); err == nil || !strings.Contains(err.Error(), "requires owner boundary") {
		t.Fatalf("legacy owner-scoped derived surgery should require owner context, got %v", err)
	}
	if err := store.SupersedeDerivedMemory("schema-1", `D:\workprj\alpha`, "owner-a"); err != nil {
		t.Fatalf("SupersedeDerivedMemory: %v", err)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	for _, entry := range store.entries {
		switch entry.ID {
		case "raw-1":
			if entry.Status == StatusSuperseded {
				t.Fatalf("raw evidence should remain active: %+v", entry)
			}
		case "schema-1":
			if entry.Status != StatusSuperseded || entry.InvalidAt == nil {
				t.Fatalf("derived schema should be superseded with invalid time: %+v", entry)
			}
		}
	}
}

func TestHandleToolDerivedSurgerySupersedesDerivedOnly(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	store.mu.Lock()
	store.SetEntries([]Entry{{
		ID:          "schema-tool",
		Content:     "tool visible derived schema",
		Category:    CategoryProjectKnowledge,
		DerivedKind: "schema:recurring",
		EvidenceIDs: []string{"raw-1"},
		Boundary:    &MemoryBoundary{OwnerID: "owner-a"},
		CreatedAt:   now,
		UpdatedAt:   now,
		Strength:    1,
	}})
	store.mu.Unlock()

	called := false
	out := HandleTool(store, map[string]interface{}{"action": "derived_surgery", "id": "schema-tool"}, ToolOptions{AfterWrite: func() { called = true }})
	if !strings.Contains(out, "requires owner boundary") {
		t.Fatalf("expected missing owner boundary failure, got: %s", out)
	}
	if called {
		t.Fatal("AfterWrite should not run when surgery is rejected")
	}

	out = HandleTool(store, map[string]interface{}{"action": "derived_surgery", "id": "schema-tool"}, ToolOptions{OwnerID: "owner-a", AfterWrite: func() { called = true }})
	if !strings.Contains(out, "Derived memory superseded: schema-tool") {
		t.Fatalf("unexpected surgery output: %s", out)
	}
	if !called {
		t.Fatal("expected AfterWrite callback")
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	if store.entries[0].Status != StatusSuperseded {
		t.Fatalf("expected schema-tool superseded: %+v", store.entries[0])
	}
}
