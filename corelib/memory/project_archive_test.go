package memory

import "testing"

func TestProjectEntriesForArchiveFiltersProjectKnowledgeAndArtifacts(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if _, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{Content: "project note", Tags: []string{"D:/repo/app"}, SourceType: "test"}); err != nil {
		t.Fatalf("UpsertProjectKnowledge: %v", err)
	}
	if _, err := store.UpsertTaskArtifact(TaskArtifactUpsertOptions{Content: "artifact", Tags: []string{"D:/repo/app/sub"}, SourceType: "workflow_output", SourceURL: "D:/repo/app/out.md"}); err != nil {
		t.Fatalf("UpsertTaskArtifact: %v", err)
	}
	if _, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{Content: "other note", Tags: []string{"D:/repo/other"}, SourceType: "test"}); err != nil {
		t.Fatalf("UpsertProjectKnowledge other: %v", err)
	}
	if err := store.Save(Entry{Content: "user fact", Category: CategoryUserFact, Tags: []string{"D:/repo/app"}}); err != nil {
		t.Fatalf("Save user fact: %v", err)
	}

	got := store.ProjectEntriesForArchive("D:\\repo\\app")
	if len(got) != 2 {
		t.Fatalf("expected 2 project archive entries, got %d: %+v", len(got), got)
	}
}

func TestArchivedExperienceForProjectRequiresArchiveTagAndExactPath(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if _, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{Content: "archive summary", Tags: []string{"archived_experience", "D:/repo/app"}, SourceType: "project_archive"}); err != nil {
		t.Fatalf("UpsertProjectKnowledge archive: %v", err)
	}
	if _, err := store.UpsertProjectKnowledge(ProjectKnowledgeUpsertOptions{Content: "subdir summary", Tags: []string{"archived_experience", "D:/repo/app/sub"}, SourceType: "project_archive"}); err != nil {
		t.Fatalf("UpsertProjectKnowledge subdir: %v", err)
	}

	if got := store.ArchivedExperienceForProject("D:\\repo\\app"); got != "archive summary" {
		t.Fatalf("ArchivedExperienceForProject() = %q", got)
	}
	if got := store.ArchivedExperienceForProject("D:/repo/missing"); got != "" {
		t.Fatalf("missing project returned %q", got)
	}
}
