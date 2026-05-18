package memory

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildSceneIndexGroupsSourcesAndArtifacts(t *testing.T) {
	now := time.Now()
	entries := []Entry{
		{ID: "req", Title: "Requirements", Content: "# Requirements\nBuild the thing", Category: CategoryTaskArtifact, Tags: []string{"workflow", "requirements", "workflow:coding", "/home/user/app"}, SourceType: "workflow_output_ref", SourceURL: "/home/user/app/memory_refs/workflow_output/req.md", UpdatedAt: now.Add(-time.Hour)},
		{ID: "design", Title: "Design", Content: "# Design\nUse SQLite", Category: CategoryTaskArtifact, Tags: []string{"workflow", "design", "workflow:coding", "/home/user/app"}, SourceType: "workflow_output_ref", SourceURL: "/home/user/app/memory_refs/workflow_output/design.md", UpdatedAt: now},
		{ID: "fact", Content: "Project uses SQLite for local state", Category: CategoryProjectKnowledge, Tags: []string{"/home/user/app"}, UpdatedAt: now.Add(-30 * time.Minute)},
		{ID: "user", Content: "User likes compact UI", Category: CategoryUserFact, UpdatedAt: now},
	}

	scenes := BuildSceneIndex(entries, 10)
	if len(scenes) != 1 {
		t.Fatalf("expected 1 scene, got %d", len(scenes))
	}
	scene := scenes[0]
	if scene.ProjectPath != "/home/user/app" {
		t.Fatalf("ProjectPath = %q", scene.ProjectPath)
	}
	if scene.Name != "Requirements" {
		t.Fatalf("Name = %q, want Requirements", scene.Name)
	}
	if len(scene.SourceURLs) != 2 {
		t.Fatalf("expected 2 source URLs, got %#v", scene.SourceURLs)
	}
	if len(scene.RecentArtifacts) != 2 || scene.RecentArtifacts[0].Title != "Design" {
		t.Fatalf("unexpected recent artifacts: %#v", scene.RecentArtifacts)
	}
	if len(scene.WorkflowTypes) != 1 || scene.WorkflowTypes[0] != "coding" {
		t.Fatalf("WorkflowTypes = %#v", scene.WorkflowTypes)
	}
}

func TestStoreSceneIndex(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "# Scene Doc\n" + strings.Repeat("detail ", 40), Category: CategoryTaskArtifact, SourceURL: "/home/user/project/doc.md"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	scenes := store.SceneIndex(5)
	if len(scenes) != 1 {
		t.Fatalf("expected 1 scene, got %d", len(scenes))
	}
	if scenes[0].ProjectPath != "/home/user/project" || len(scenes[0].RecentArtifacts) != 1 {
		t.Fatalf("unexpected scene: %#v", scenes[0])
	}
}

func TestFormatSceneIndexForPromptIncludesReadFileHint(t *testing.T) {
	scenes := []SceneRecord{{
		ProjectPath: "/home/user/project",
		Name:        "Project",
		RecentArtifacts: []SceneArtifact{{
			Title:      "Requirements",
			SourceType: "workflow_output_ref",
			SourceURL:  "/home/user/project/memory_refs/requirements.md",
		}},
	}}
	out := FormatSceneIndexForPrompt(scenes, 3, 2)
	for _, want := range []string{"Project", "Requirements", "source: /home/user/project/memory_refs/requirements.md; full: read_file"} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted scene missing %q in:\n%s", want, out)
		}
	}
}
