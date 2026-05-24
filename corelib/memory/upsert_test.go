package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpsertEntryByTagsDoesNotEnqueueSemanticDedup(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	store.SetEmbedder(&fakeEmbedderForDedup{
		dim: 2,
		vectors: map[string][]float32{
			"first generated memory":  {1, 0},
			"second generated memory": {1, 0},
		},
	})
	if _, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "first generated memory",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"generated", "one"},
		IdentityTagCount: 2,
		Scope:            ScopeProject,
	}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if _, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "second generated memory",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"generated", "two"},
		IdentityTagCount: 2,
		Scope:            ScopeProject,
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if got := store.PendingDedupCount(); got != 0 {
		t.Fatalf("generated upsert should preserve stable identity and skip async semantic dedup, pending=%d", got)
	}
}

func TestUpsertEntryByTagsCreatesUpdatesAndTouches(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	result, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "Tool routing hint for react migration",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"routing", "react", "react"},
		IdentityTagCount: 2,
		SourceType:       "tool_usage",
	})
	if err != nil || !result.Created || result.EntryID == "" {
		t.Fatalf("expected create with entry ID, result=%+v err=%v", result, err)
	}

	result, err = store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "Tool routing hint for react migration",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"routing", "react"},
		IdentityTagCount: 2,
	})
	if err != nil || !result.Touched {
		t.Fatalf("expected touch, result=%+v err=%v", result, err)
	}

	result, err = store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "Updated routing hint for react migration",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"routing", "react", "updated"},
		IdentityTagCount: 2,
		MergeExistingTags: func(existing, desired []string) []string {
			return append(existing, desired...)
		},
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected update, result=%+v err=%v", result, err)
	}
	entries := store.List(CategoryProjectKnowledge, "Updated routing")
	if len(entries) != 1 || !hasTag(entries[0].Tags, "updated") || !hasTag(entries[0].Tags, "routing") {
		t.Fatalf("unexpected updated entry: %+v", entries)
	}
}

func TestUpsertEntryByTagsUpdatePersistsRelatedEdgesAndRefreshesGraph(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewSQLiteBackend(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("NewSQLiteBackend: %v", err)
	}
	store.SetBackend(backend, SyncConfig{Enabled: false})
	defer store.Stop()

	now := time.Now().UTC()
	created, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "generated profile edge left",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"generated", "edge-left"},
		IdentityTagCount: 2,
	})
	if err != nil || !created.Created {
		t.Fatalf("create left: result=%+v err=%v", created, err)
	}
	if err := store.Save(Entry{ID: "edge-right", Content: "generated profile edge right", Category: CategoryProjectKnowledge}); err != nil {
		t.Fatalf("save right: %v", err)
	}

	updated, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "generated profile edge left updated",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"generated", "edge-left", "updated"},
		IdentityTagCount: 2,
		RelatedIDs:       []string{"edge-right"},
		RelatedEdges:     []RelatedEdge{{ID: "edge-right", Strength: 0.6, LinkType: LinkDerivedFrom, UpdatedAt: now}},
	})
	if err != nil || !updated.Updated || updated.EntryID != created.EntryID {
		t.Fatalf("update left: result=%+v err=%v", updated, err)
	}

	neighbors := store.graph.neighborsTypedOf(created.EntryID)
	if edge, ok := neighbors["edge-right"]; !ok || edge.LinkType != LinkDerivedFrom || edge.Strength != 0.6 {
		t.Fatalf("upsert update did not refresh graph edges: %#v", neighbors)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for _, entry := range loaded {
		if entry.ID == created.EntryID {
			if len(entry.RelatedEdges) != 1 || entry.RelatedEdges[0].ID != "edge-right" || entry.RelatedEdges[0].LinkType != LinkDerivedFrom {
				t.Fatalf("related edge was not persisted: %+v", entry.RelatedEdges)
			}
			return
		}
	}
	t.Fatalf("updated entry %s not loaded", created.EntryID)
}

func TestUpsertEntryByTagsRepairsExistingDuplicateContent(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "legacy-generated",
		Content:  "same generated summary content",
		Category: CategoryTaskArtifact,
		Tags:     []string{"legacy"},
	}); err != nil {
		t.Fatalf("seed duplicate: %v", err)
	}

	result, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:            "same generated summary content",
		Category:           CategoryTaskArtifact,
		Tags:               []string{"workflow", "summary"},
		IdentityTagCount:   2,
		Scope:              ScopeProject,
		SourceType:         "workflow_output_ref",
		DefaultDerivedKind: "artifact:summary",
	})
	if err != nil || !result.Updated || result.EntryID != "legacy-generated" {
		t.Fatalf("expected duplicate content repair, result=%+v err=%v", result, err)
	}
	entries := store.List(CategoryTaskArtifact, "same generated summary")
	if len(entries) != 1 || entries[0].ID != "legacy-generated" || entries[0].Scope != ScopeProject || entries[0].SourceType != "workflow_output_ref" || entries[0].DerivedKind != "artifact:summary" {
		t.Fatalf("duplicate generated entry was not repaired: %+v", entries)
	}
	if !hasTag(entries[0].Tags, "legacy") || !hasTag(entries[0].Tags, "workflow") || !hasTag(entries[0].Tags, "summary") {
		t.Fatalf("duplicate repair should preserve existing and desired tags: %+v", entries[0].Tags)
	}

	result, err = store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:            "same generated summary content",
		Category:           CategoryTaskArtifact,
		Tags:               []string{"workflow", "summary"},
		IdentityTagCount:   2,
		Scope:              ScopeProject,
		SourceType:         "workflow_output_ref",
		DefaultDerivedKind: "artifact:summary",
	})
	if err != nil || !result.Touched || result.EntryID != "legacy-generated" {
		t.Fatalf("expected repeated upsert to touch preserved generated entry, result=%+v err=%v", result, err)
	}
	entries = store.List(CategoryTaskArtifact, "same generated summary")
	if len(entries) != 1 || !hasTag(entries[0].Tags, "legacy") || !hasTag(entries[0].Tags, "workflow") || !hasTag(entries[0].Tags, "summary") {
		t.Fatalf("repeated upsert should keep repaired tags: %+v", entries)
	}
}

func TestUpsertEntryByTagsDuplicateRepairPreservesSharedOwner(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "shared-generated",
		Content:  "shared generated summary content",
		Category: CategoryTaskArtifact,
		Tags:     []string{"shared"},
	}); err != nil {
		t.Fatalf("seed shared duplicate: %v", err)
	}

	result, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:            "shared generated summary content",
		Category:           CategoryTaskArtifact,
		Tags:               []string{"workflow", "summary"},
		IdentityTagCount:   2,
		Scope:              ScopeProject,
		OwnerID:            "user-1",
		SourceType:         "workflow_output_ref",
		DefaultDerivedKind: "artifact:summary",
	})
	if err != nil || !result.Updated || result.EntryID != "shared-generated" {
		t.Fatalf("expected shared duplicate repair, result=%+v err=%v", result, err)
	}
	entries := store.SearchDirectByID("shared-generated")
	if len(entries) != 1 || entries[0].OwnerID != "" || entries[0].Scope != ScopeProject || entries[0].DerivedKind != "artifact:summary" {
		t.Fatalf("shared duplicate should stay shared while repairing metadata: %+v", entries)
	}
}

func TestUpsertEntryByTagsSharedWriteDoesNotRepairPrivateDuplicate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "private-generated",
		Content:  "owner private generated summary content",
		Category: CategoryTaskArtifact,
		Tags:     []string{"private"},
		OwnerID:  "user-1",
	}); err != nil {
		t.Fatalf("seed private duplicate: %v", err)
	}

	result, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "owner private generated summary content",
		Category:         CategoryTaskArtifact,
		Tags:             []string{"workflow", "shared"},
		IdentityTagCount: 2,
		Scope:            ScopeProject,
		SourceType:       "workflow_output_ref",
	})
	if err != nil || !result.Created || result.EntryID == "private-generated" {
		t.Fatalf("shared generated write should create shared entry instead of repairing private duplicate, result=%+v err=%v", result, err)
	}
	private := store.SearchDirectByID("private-generated")
	if len(private) != 1 || private[0].OwnerID != "user-1" || hasTag(private[0].Tags, "workflow") {
		t.Fatalf("private duplicate should remain private and unchanged: %+v", private)
	}
	shared := store.SearchDirectByID(result.EntryID)
	if len(shared) != 1 || shared[0].OwnerID != "" || !hasTag(shared[0].Tags, "shared") {
		t.Fatalf("expected separate shared generated entry: %+v", shared)
	}
}
func TestUpsertEntryByTagsKeepsOwnersIsolated(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	_, err = store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "user one task",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"task_sediment", "auto", "task-path"},
		IdentityTagCount: 3,
		Scope:            ScopeProject,
		OwnerID:          "user-1",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.UpsertEntryByTags(UpsertByTagsOptions{
		Content:          "user two task",
		Category:         CategoryProjectKnowledge,
		Tags:             []string{"task_sediment", "auto", "task-path"},
		IdentityTagCount: 3,
		Scope:            ScopeProject,
		OwnerID:          "user-2",
	})
	if err != nil || !result.Created {
		t.Fatalf("expected separate create for second owner, result=%+v err=%v", result, err)
	}

	entries := store.List(CategoryProjectKnowledge, "task")
	if len(entries) != 2 {
		t.Fatalf("expected two owner-isolated entries, got %d: %+v", len(entries), entries)
	}
	owners := map[string]bool{}
	for _, entry := range entries {
		owners[entry.OwnerID] = true
		if entry.Scope != ScopeProject {
			t.Fatalf("expected project scope, got %+v", entry)
		}
	}
	if !owners["user-1"] || !owners["user-2"] {
		t.Fatalf("missing owner-specific entries: %+v", entries)
	}
}

func TestUpsertEntryByID(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entry := Entry{ID: "retry_tool_timeout", Content: "retry once", Category: CategoryProjectKnowledge, Tags: []string{"retry"}}
	result, err := store.UpsertEntryByID(entry)
	if err != nil || !result.Created {
		t.Fatalf("expected create, result=%+v err=%v", result, err)
	}
	entry.Content = "retry twice"
	entry.Tags = []string{"retry-updated"}
	result, err = store.UpsertEntryByID(entry)
	if err != nil || !result.Updated {
		t.Fatalf("expected update, result=%+v err=%v", result, err)
	}
	entries := store.SearchDirectByID("retry_tool_timeout")
	if len(entries) != 1 || hasTag(entries[0].Tags, "retry") || !hasTag(entries[0].Tags, "retry-updated") {
		t.Fatalf("generic fixed-id upsert should replace tags exactly: %+v", entries)
	}
	result, err = store.UpsertEntryByID(entry)
	if err != nil || !result.Touched {
		t.Fatalf("expected touch, result=%+v err=%v", result, err)
	}
}

func TestUpsertEntryByIDAllowsSameContentAcrossCategories(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "existing-user-fact",
		Content:  "same wording across categories",
		Category: CategoryUserFact,
		Tags:     []string{"user"},
	}); err != nil {
		t.Fatalf("seed user fact: %v", err)
	}
	result, err := store.UpsertEntryByID(Entry{
		ID:       "project-same-content",
		Content:  "initial project content",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"project"},
	})
	if err != nil || !result.Created {
		t.Fatalf("create project entry: result=%+v err=%v", result, err)
	}
	result, err = store.UpsertEntryByID(Entry{
		ID:       "project-same-content",
		Content:  "same wording across categories",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"project"},
	})
	if err != nil || !result.Updated {
		t.Fatalf("same content in different category should update independently, result=%+v err=%v", result, err)
	}
	if entries := store.SearchDirectByID("project-same-content"); len(entries) != 1 || entries[0].Category != CategoryProjectKnowledge || entries[0].Content != "same wording across categories" {
		t.Fatalf("project entry not updated independently: %+v", entries)
	}
}

func TestUpsertEntryByIDSharedWriteDoesNotRepairPrivateDuplicate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "private-fixed-duplicate",
		Content:  "private fixed-id generated insight",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"private"},
		OwnerID:  "user-1",
	}); err != nil {
		t.Fatalf("seed private duplicate: %v", err)
	}

	result, err := store.UpsertEntryByID(Entry{
		ID:          "shared-fixed-entry",
		Content:     "private fixed-id generated insight",
		Category:    CategoryProjectKnowledge,
		Tags:        []string{"shared"},
		Scope:       ScopeProject,
		SourceType:  "generated_test",
		DerivedKind: "generated_test",
	})
	if err != nil || !result.Created || result.EntryID != "shared-fixed-entry" {
		t.Fatalf("shared fixed-id write should create shared entry instead of repairing private duplicate, result=%+v err=%v", result, err)
	}
	private := store.SearchDirectByID("private-fixed-duplicate")
	if len(private) != 1 || private[0].OwnerID != "user-1" || hasTag(private[0].Tags, "shared") {
		t.Fatalf("private duplicate should remain private and unchanged: %+v", private)
	}
	shared := store.SearchDirectByID("shared-fixed-entry")
	if len(shared) != 1 || shared[0].OwnerID != "" || !hasTag(shared[0].Tags, "shared") {
		t.Fatalf("expected separate shared fixed-id entry: %+v", shared)
	}
}
func TestUpsertEntryByIDGeneratedIDReturnsExistingDuplicate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "existing-generated-id-duplicate",
		Content:  "same generated-id durable insight",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"old"},
		Scope:    ScopeGlobal,
	}); err != nil {
		t.Fatalf("seed duplicate: %v", err)
	}

	result, err := store.UpsertEntryByID(Entry{
		Content:     "same generated-id durable insight",
		Category:    CategoryProjectKnowledge,
		Tags:        []string{"new"},
		Scope:       ScopeProject,
		SourceType:  "generated_test",
		DerivedKind: "generated_test",
	})
	if err != nil || !result.Updated || result.EntryID != "existing-generated-id-duplicate" {
		t.Fatalf("expected generated-id duplicate repair, result=%+v err=%v", result, err)
	}
	entries := store.SearchDirectByID("existing-generated-id-duplicate")
	if len(entries) != 1 || entries[0].Scope != ScopeProject || entries[0].SourceType != "generated_test" || entries[0].DerivedKind != "generated_test" || !hasTag(entries[0].Tags, "old") || !hasTag(entries[0].Tags, "new") {
		t.Fatalf("generated-id duplicate was not repaired: %+v", entries)
	}
	if all := store.List(CategoryProjectKnowledge, "same generated-id durable insight"); len(all) != 1 {
		t.Fatalf("generated-id duplicate should not create another entry: %+v", all)
	}
}

func TestUpsertEntryByIDRepairsExistingDuplicateContent(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "existing-duplicate",
		Content:  "same durable generated insight",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"old"},
		Scope:    ScopeGlobal,
	}); err != nil {
		t.Fatalf("seed duplicate: %v", err)
	}

	result, err := store.UpsertEntryByID(Entry{
		ID:          "fixed-duplicate",
		Content:     "same durable generated insight",
		Category:    CategoryProjectKnowledge,
		Tags:        []string{"new"},
		Scope:       ScopeProject,
		SourceType:  "generated_test",
		DerivedKind: "generated_test",
	})
	if err != nil || !result.Updated || result.EntryID != "existing-duplicate" {
		t.Fatalf("expected duplicate repair through existing entry, result=%+v err=%v", result, err)
	}
	if entries := store.SearchDirectByID("fixed-duplicate"); len(entries) != 0 {
		t.Fatalf("fixed-id duplicate should not create a second entry: %+v", entries)
	}
	entries := store.SearchDirectByID("existing-duplicate")
	if len(entries) != 1 || entries[0].Scope != ScopeProject || entries[0].SourceType != "generated_test" || entries[0].DerivedKind != "generated_test" || !hasTag(entries[0].Tags, "old") || !hasTag(entries[0].Tags, "new") {
		t.Fatalf("duplicate entry was not repaired with generated metadata: %+v", entries)
	}
}

func TestUpsertEntryByIDUpdatePersistsThroughSQLiteBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	backend := newTestSQLiteBackend(t)
	store.SetBackend(backend, SyncConfig{Enabled: false})
	seed := Entry{ID: "upsert-sqlite", Content: "old generated memory", Category: CategoryProjectKnowledge, Tags: []string{"old"}, AccessCount: 1, Strength: 1, CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour)}
	if err := store.UpsertEntriesByID([]Entry{seed}); err != nil {
		t.Fatalf("seed UpsertEntriesByID: %v", err)
	}

	result, err := store.UpsertEntryByID(Entry{
		ID:           "upsert-sqlite",
		Content:      "new generated memory",
		Category:     CategoryProjectKnowledge,
		Tags:         []string{"new"},
		Scope:        ScopeProject,
		SourceType:   "generated_test",
		SourceURL:    "file://generated",
		EvidenceIDs:  []string{"evidence-1"},
		RelatedIDs:   []string{"evidence-1"},
		RelatedEdges: []RelatedEdge{{ID: "evidence-1", Strength: 0.8, LinkType: LinkReferences}},
		DerivedKind:  "generated_test",
	})
	if err != nil || !result.Updated || result.EntryID != "upsert-sqlite" {
		t.Fatalf("expected sqlite upsert update, result=%+v err=%v", result, err)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one backend entry, got %+v", loaded)
	}
	got := loaded[0]
	if got.Content != "new generated memory" || got.Scope != ScopeProject || got.SourceType != "generated_test" || got.DerivedKind != "generated_test" {
		t.Fatalf("backend upsert update not persisted: %+v", got)
	}
	if len(got.Versions) != 1 || got.Versions[0].Content != "old generated memory" {
		t.Fatalf("content update should preserve version history, got %+v", got.Versions)
	}
	if !hasTag(got.Tags, "new") || len(got.RelatedEdges) != 1 || got.RelatedEdges[0].ID != "evidence-1" {
		t.Fatalf("metadata update not persisted: %+v", got)
	}
}

func TestUpsertEntryByIDDuplicateRepairPreservesSharedOwner(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "shared-fixed-duplicate",
		Content:  "shared fixed-id generated insight",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"shared"},
	}); err != nil {
		t.Fatalf("seed shared duplicate: %v", err)
	}

	result, err := store.UpsertEntryByID(Entry{
		ID:          "owner-fixed-duplicate",
		Content:     "shared fixed-id generated insight",
		Category:    CategoryProjectKnowledge,
		Tags:        []string{"owner"},
		Scope:       ScopeProject,
		OwnerID:     "user-1",
		SourceType:  "generated_test",
		DerivedKind: "generated_test",
	})
	if err != nil || !result.Updated || result.EntryID != "shared-fixed-duplicate" {
		t.Fatalf("expected fixed-id shared duplicate repair, result=%+v err=%v", result, err)
	}
	entries := store.SearchDirectByID("shared-fixed-duplicate")
	if len(entries) != 1 || entries[0].OwnerID != "" || entries[0].Scope != ScopeProject || entries[0].DerivedKind != "generated_test" {
		t.Fatalf("fixed-id duplicate repair should keep shared owner: %+v", entries)
	}
}

func TestUpsertProjectKnowledgeByIDAppliesDefaults(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	result, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{
		ID:         "fixed-project-knowledge",
		Title:      "Fixed project knowledge",
		Content:    "Generated project insight",
		Tags:       []string{"D:/repo", "insight"},
		SourceType: "experience_learning",
		SourceURL:  "experience://fixed",
	})
	if err != nil || !result.Created || result.EntryID != "fixed-project-knowledge" {
		t.Fatalf("expected fixed-id create, result=%+v err=%v", result, err)
	}
	entries := store.SearchDirectByID("fixed-project-knowledge")
	if len(entries) != 1 {
		t.Fatalf("expected fixed-id entry, got %+v", entries)
	}
	if entries[0].Category != CategoryProjectKnowledge || entries[0].Scope != ScopeProject || entries[0].DerivedKind != "experience_learning" || entries[0].Boundary == nil || entries[0].Boundary.ProjectPath != "D:/repo" {
		t.Fatalf("fixed-id project knowledge missing defaults: %+v", entries[0])
	}
}

func TestUpsertProjectKnowledgeByIDRepairsDefaultScope(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{ID: "legacy-scope", Content: "legacy generated project insight", Category: CategoryProjectKnowledge, Scope: ScopeGlobal}); err != nil {
		t.Fatalf("seed legacy entry: %v", err)
	}
	result, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{
		ID:         "legacy-scope",
		Content:    "updated generated project insight",
		Tags:       []string{"D:/repo", "insight"},
		SourceType: "experience_learning",
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected fixed-id update, result=%+v err=%v", result, err)
	}
	entries := store.SearchDirectByID("legacy-scope")
	if len(entries) != 1 || entries[0].Scope != ScopeProject || entries[0].DerivedKind != "experience_learning" {
		t.Fatalf("project knowledge upsert should repair generated defaults: %+v", entries)
	}
}

func TestUpsertProjectKnowledgeByIDRepairsMetadataWithoutContentChange(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "legacy-metadata",
		Content:  "same generated project insight",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"D:/repo", "insight"},
		Scope:    ScopeGlobal,
	}); err != nil {
		t.Fatalf("seed legacy entry: %v", err)
	}
	result, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{
		ID:         "legacy-metadata",
		Content:    "same generated project insight",
		Tags:       []string{"D:/repo", "insight"},
		SourceType: "experience_learning",
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected fixed-id metadata repair, result=%+v err=%v", result, err)
	}
	entries := store.SearchDirectByID("legacy-metadata")
	if len(entries) != 1 || entries[0].Scope != ScopeProject || entries[0].DerivedKind != "experience_learning" || entries[0].Boundary == nil || entries[0].Boundary.ProjectPath != "D:/repo" {
		t.Fatalf("project knowledge upsert should repair generated metadata: %+v", entries)
	}
}
func TestUpsertProjectKnowledgeByIDPreservesExistingBoundary(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{
		ID:       "fixed-boundary",
		Content:  "legacy generated project boundary insight",
		Category: CategoryProjectKnowledge,
		Tags:     []string{"D:/repo", "legacy"},
		Scope:    ScopeProject,
		Boundary: &MemoryBoundary{OwnerID: "user-1", ProjectPath: "D:/repo/richer", SourceScope: "review"},
	}); err != nil {
		t.Fatalf("seed legacy entry: %v", err)
	}

	result, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{
		ID:         "fixed-boundary",
		Content:    "updated generated project boundary insight",
		Tags:       []string{"D:/repo", "insight"},
		OwnerID:    "user-1",
		SourceType: "experience_learning",
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected fixed-id boundary-preserving update, result=%+v err=%v", result, err)
	}

	entries := store.SearchDirectByID("fixed-boundary")
	if len(entries) != 1 || entries[0].Boundary == nil || entries[0].Boundary.ProjectPath != "D:/repo/richer" || entries[0].Boundary.SourceScope != "review" {
		t.Fatalf("fixed-id project knowledge should preserve existing boundary unless caller supplies one: %+v", entries)
	}
}
func TestUpsertProjectKnowledgeByIDMergesExistingTags(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	result, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{
		ID:         "fixed-merge",
		Content:    "fixed generated project knowledge",
		Tags:       []string{"legacy", "D:/repo"},
		SourceType: "experience_learning",
	})
	if err != nil || !result.Created {
		t.Fatalf("expected fixed-id create, result=%+v err=%v", result, err)
	}

	result, err = store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{
		ID:         "fixed-merge",
		Content:    "fixed generated project knowledge",
		Tags:       []string{"D:/repo", "insight"},
		SourceType: "experience_learning",
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected fixed-id metadata update, result=%+v err=%v", result, err)
	}

	entries := store.SearchDirectByID("fixed-merge")
	if len(entries) != 1 || !hasTag(entries[0].Tags, "legacy") || !hasTag(entries[0].Tags, "D:/repo") || !hasTag(entries[0].Tags, "insight") {
		t.Fatalf("fixed-id project knowledge should merge existing and desired tags: %+v", entries)
	}
}
func TestManualMemoryHelpers(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.SaveManualMemory("manual note", CategoryInstruction, []string{"manual-tag"}); err != nil {
		t.Fatalf("SaveManualMemory: %v", err)
	}
	entries := store.List(CategoryInstruction, "manual note")
	if len(entries) != 1 {
		t.Fatalf("expected one manual entry, got %d", len(entries))
	}
	if entries[0].SourceType != ManualMemorySourceType {
		t.Fatalf("SourceType = %q, want manual", entries[0].SourceType)
	}

	if err := store.UpdateManualMemory(entries[0].ID, "manual note updated", CategoryPreference, []string{"updated"}); err != nil {
		t.Fatalf("UpdateManualMemory: %v", err)
	}
	updated := store.SearchDirectByID(entries[0].ID)
	if len(updated) != 1 || updated[0].Category != CategoryPreference || updated[0].Content != "manual note updated" || !hasTag(updated[0].Tags, "updated") {
		t.Fatalf("unexpected updated manual entry: %+v", updated)
	}
	if updated[0].SourceType != ManualMemorySourceType {
		t.Fatalf("updated SourceType = %q, want manual", updated[0].SourceType)
	}
}

func TestUpsertTaskArtifactAppliesProjectArtifactDefaults(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	result, err := store.UpsertTaskArtifact(TaskArtifactUpsertOptions{
		Title:            "Workflow phase",
		Content:          "artifact v1",
		Tags:             []string{"workflow", "phase-a"},
		IdentityTagCount: 2,
		OwnerID:          "user-1",
		SourceType:       "workflow_output_ref",
		SourceURL:        "D:/tmp/phase-a.md",
	})
	if err != nil || !result.Created {
		t.Fatalf("expected task artifact create, result=%+v err=%v", result, err)
	}

	result, err = store.UpsertTaskArtifact(TaskArtifactUpsertOptions{
		Title:            "Workflow phase",
		Content:          "artifact v2",
		Tags:             []string{"workflow", "phase-a"},
		IdentityTagCount: 2,
		OwnerID:          "user-1",
		SourceType:       "workflow_output_ref",
		SourceURL:        "D:/tmp/phase-a-v2.md",
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected task artifact update, result=%+v err=%v", result, err)
	}

	entries := store.List(CategoryTaskArtifact, "artifact v2")
	if len(entries) != 1 {
		t.Fatalf("expected one task artifact, got %d: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Category != CategoryTaskArtifact || entry.Scope != ScopeProject || entry.OwnerID != "user-1" || entry.SourceURL != "D:/tmp/phase-a-v2.md" {
		t.Fatalf("unexpected task artifact defaults: %+v", entry)
	}
}

func TestUpsertConversationSummaryAppliesDefaults(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	result, err := store.UpsertConversationSummary(ConversationSummaryUpsertOptions{
		Title:            "Conversation summary",
		Content:          "summary v1",
		Tags:             []string{"conversation_summary", "user-1", "2026-05-19"},
		IdentityTagCount: 3,
		OwnerID:          "user-1",
	})
	if err != nil || !result.Created {
		t.Fatalf("expected create, result=%+v err=%v", result, err)
	}

	result, err = store.UpsertConversationSummary(ConversationSummaryUpsertOptions{
		Title:            "Conversation summary",
		Content:          "summary v2",
		Tags:             []string{"conversation_summary", "user-1", "2026-05-19"},
		IdentityTagCount: 3,
		OwnerID:          "user-1",
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected update, result=%+v err=%v", result, err)
	}

	entries := store.List(CategoryConversationSummary, "summary v2")
	if len(entries) != 1 {
		t.Fatalf("expected one conversation summary, got %d: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Category != CategoryConversationSummary || entry.Scope != ScopeProject || entry.OwnerID != "user-1" || entry.SourceType != "conversation_summary" {
		t.Fatalf("unexpected conversation summary defaults: %+v", entry)
	}
	if entry.DerivedKind != "summary" || entry.Boundary == nil || entry.Boundary.OwnerID != "user-1" || entry.Boundary.SourceScope != "conversation_summary" {
		t.Fatalf("unexpected conversation summary derived metadata: %+v", entry)
	}
}

func TestUpsertTaskArtifactPreservesOptionalDerivedMetadata(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	boundary := &MemoryBoundary{OwnerID: "user-1", ProjectPath: "D:/workprj/aicoder", SourceScope: "workflow"}
	_, err = store.UpsertTaskArtifact(TaskArtifactUpsertOptions{
		Title:            "Workflow summary",
		Content:          "artifact derived from workflow evidence",
		Tags:             []string{"workflow", "summary"},
		IdentityTagCount: 2,
		OwnerID:          "user-1",
		SourceType:       "workflow_output_ref",
		EvidenceIDs:      []string{"raw-a", "raw-b"},
		RelatedIDs:       []string{"raw-a", "raw-b"},
		DerivedKind:      "artifact:summary",
		Boundary:         boundary,
	})
	if err != nil {
		t.Fatalf("UpsertTaskArtifact: %v", err)
	}

	entries := store.List(CategoryTaskArtifact, "workflow evidence")
	if len(entries) != 1 {
		t.Fatalf("expected one task artifact, got %+v", entries)
	}
	entry := entries[0]
	if entry.DerivedKind != "artifact:summary" || strings.Join(entry.EvidenceIDs, ",") != "raw-a,raw-b" || strings.Join(entry.RelatedIDs, ",") != "raw-a,raw-b" {
		t.Fatalf("optional derived metadata not preserved: %+v", entry)
	}
	if entry.Boundary == nil || entry.Boundary.OwnerID != "user-1" || entry.Boundary.ProjectPath != "D:/workprj/aicoder" || entry.Boundary.SourceScope != "workflow" {
		t.Fatalf("optional boundary not preserved: %+v", entry.Boundary)
	}
}
func TestUpsertProjectKnowledgeAppliesCategory(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	boundary := &MemoryBoundary{SourceScope: "tool_usage"}
	result, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{
		Title:            "Routing hint",
		Content:          "Prefer ripgrep for file search",
		Tags:             []string{"usage", "search"},
		IdentityTagCount: 2,
		SourceType:       "tool_usage",
		EvidenceIDs:      []string{"trace-1"},
		DerivedKind:      "usage_pattern",
		Boundary:         boundary,
	})
	if err != nil || !result.Created {
		t.Fatalf("expected project knowledge create, result=%+v err=%v", result, err)
	}

	result, err = store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{
		Title:            "Routing hint",
		Content:          "Prefer ripgrep for codebase search",
		Tags:             []string{"usage", "search", "rg"},
		IdentityTagCount: 2,
		SourceType:       "tool_usage",
		MergeExistingTags: func(existing, desired []string) []string {
			return append(existing, desired...)
		},
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected project knowledge update, result=%+v err=%v", result, err)
	}

	entries := store.List(CategoryProjectKnowledge, "ripgrep")
	if len(entries) != 1 {
		t.Fatalf("expected one project knowledge entry, got %d: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Category != CategoryProjectKnowledge || entry.SourceType != "tool_usage" || !hasTag(entry.Tags, "rg") {
		t.Fatalf("unexpected project knowledge defaults: %+v", entry)
	}
	if entry.DerivedKind != "usage_pattern" || strings.Join(entry.EvidenceIDs, ",") != "trace-1" || entry.Boundary == nil || entry.Boundary.SourceScope != "tool_usage" {
		t.Fatalf("unexpected project knowledge derived metadata: %+v", entry)
	}
}

func TestUpsertSessionCheckpointAppliesDefaults(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	result, err := store.UpsertSessionCheckpoint(SessionCheckpointUpsertOptions{
		Title:            "Session checkpoint",
		Content:          "checkpoint v1",
		Tags:             []string{"session_checkpoint", "D:/workprj/aicoder", "codex", "session-1"},
		IdentityTagCount: 4,
		OwnerID:          "user-1",
	})
	if err != nil || !result.Created {
		t.Fatalf("expected checkpoint create, result=%+v err=%v", result, err)
	}

	result, err = store.UpsertSessionCheckpoint(SessionCheckpointUpsertOptions{
		Title:            "Session checkpoint",
		Content:          "checkpoint v2",
		Tags:             []string{"session_checkpoint", "D:/workprj/aicoder", "codex", "session-1"},
		IdentityTagCount: 4,
		OwnerID:          "user-1",
	})
	if err != nil || !result.Updated {
		t.Fatalf("expected checkpoint update, result=%+v err=%v", result, err)
	}

	entries := store.List(CategorySessionCheckpoint, "checkpoint v2")
	if len(entries) != 1 {
		t.Fatalf("expected one checkpoint, got %d: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Category != CategorySessionCheckpoint || entry.Scope != ScopeProject || entry.OwnerID != "user-1" || entry.SourceType != "session_checkpoint" {
		t.Fatalf("unexpected checkpoint defaults: %+v", entry)
	}
	if entry.DerivedKind != "session_checkpoint" || entry.Boundary == nil || entry.Boundary.OwnerID != "user-1" || entry.Boundary.SourceScope != "session_checkpoint" || entry.Boundary.ProjectPath == "" {
		t.Fatalf("unexpected checkpoint derived metadata: %+v", entry)
	}
}

func TestUpsertGeneratedInsightAppliesDerivedDefaults(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	result, err := store.UpsertGeneratedInsight(GeneratedInsightUpsertOptions{
		Title:            "Self review insight",
		Content:          "Prefer checking memory write helpers before adding new GUI saves",
		Category:         CategoryInstruction,
		Tags:             []string{"self_review", "proactive"},
		IdentityTagCount: 2,
		SourceType:       "self_review",
	})
	if err != nil || !result.Created {
		t.Fatalf("expected generated insight create, result=%+v err=%v", result, err)
	}

	entries := store.List(CategoryInstruction, "memory write helpers")
	if len(entries) != 1 {
		t.Fatalf("expected one generated insight, got %d: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.Category != CategoryInstruction || entry.Scope != ScopeGlobal || entry.SourceType != "self_review" {
		t.Fatalf("unexpected generated insight defaults: %+v", entry)
	}
	if entry.DerivedKind != "self_review" || entry.Boundary == nil || entry.Boundary.SourceScope != "self_review" {
		t.Fatalf("unexpected generated insight derived metadata: %+v", entry)
	}
}
