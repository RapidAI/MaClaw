package memory

import (
	"path/filepath"
	"testing"
	"time"
)

func TestProjectContextForHostUsesStrictProjectRecallAndScene(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	projectPath := filepath.Join(t.TempDir(), "project-a")
	otherProject := filepath.Join(t.TempDir(), "project-b")
	artifactPath := filepath.Join(projectPath, "design.md")
	entries := []Entry{
		{ID: "artifact-a", Title: "Design", Content: "task artifact progress for alpha", Category: CategoryTaskArtifact, Tags: []string{projectPath}, SourceURL: artifactPath, Scope: ScopeProject},
		{ID: "knowledge-a", Title: "Knowledge", Content: "project knowledge alpha", Category: CategoryProjectKnowledge, Tags: []string{projectPath}, Scope: ScopeProject},
		{ID: "artifact-b", Title: "Other", Content: "task artifact progress for beta", Category: CategoryTaskArtifact, Tags: []string{otherProject}, Scope: ScopeProject},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	got := store.ProjectContextForHost(projectPath, 10)
	if len(got.TaskArtifacts) != 1 || got.TaskArtifacts[0].ID != "artifact-a" {
		t.Fatalf("TaskArtifacts = %#v", got.TaskArtifacts)
	}
	if len(got.ProjectKnowledge) != 1 || got.ProjectKnowledge[0].ID != "knowledge-a" {
		t.Fatalf("ProjectKnowledge = %#v", got.ProjectKnowledge)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("Entries = %#v", got.Entries)
	}
	if !got.HasScene || got.Scene.ProjectPath != projectPath || len(got.Scene.RecentArtifacts) == 0 {
		t.Fatalf("Scene = %#v, HasScene=%v", got.Scene, got.HasScene)
	}
	if nilStore := (*Store)(nil); len(nilStore.ProjectContextForHost(projectPath, 10).Entries) != 0 {
		t.Fatalf("nil store should return empty project context")
	}
}

func TestProjectContextForHostFastScanIsProjectScopedAndRecentFirst(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	projectPath := `D:\workprj\alpha`
	otherProject := `D:\workprj\beta`
	now := time.Now()
	entries := make([]Entry, 0, 64)
	for i := 0; i < 50; i++ {
		entries = append(entries, Entry{
			ID:        "other-" + string(rune('a'+i%26)),
			Title:     "Other",
			Content:   "unrelated project memory",
			Category:  CategoryTaskArtifact,
			Tags:      []string{otherProject},
			Scope:     ScopeProject,
			UpdatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}
	entries = append(entries,
		Entry{ID: "old-artifact", Title: "Old", Content: "old project progress", Category: CategoryTaskArtifact, Tags: []string{projectPath}, SourceURL: `D:\workprj\alpha\old.md`, Scope: ScopeProject, UpdatedAt: now.Add(time.Minute)},
		Entry{ID: "new-artifact", Title: "New", Content: "new project progress", Category: CategoryTaskArtifact, Tags: []string{projectPath}, SourceURL: `D:\workprj\alpha\new.md`, Scope: ScopeProject, UpdatedAt: now.Add(2 * time.Minute)},
		Entry{ID: "knowledge", Title: "Knowledge", Content: "project knowledge", Category: CategoryProjectKnowledge, Tags: []string{projectPath}, Scope: ScopeProject, UpdatedAt: now.Add(3 * time.Minute)},
	)

	store.Lock()
	store.SetEntries(entries)
	store.Unlock()

	got := store.ProjectContextForHost(projectPath, 1)
	if len(got.TaskArtifacts) != 2 || got.TaskArtifacts[0].ID != "new-artifact" || got.TaskArtifacts[1].ID != "old-artifact" {
		t.Fatalf("TaskArtifacts = %#v", got.TaskArtifacts)
	}
	if len(got.ProjectKnowledge) != 1 || got.ProjectKnowledge[0].ID != "knowledge" {
		t.Fatalf("ProjectKnowledge = %#v", got.ProjectKnowledge)
	}
	if !got.HasScene || got.Scene.ProjectPath != projectPath || got.Scene.EntryCount != 3 {
		t.Fatalf("Scene = %#v HasScene=%v", got.Scene, got.HasScene)
	}
	for _, entry := range got.Entries {
		if entry.ID == "" || entry.Tags[0] != projectPath {
			t.Fatalf("unrelated entry leaked into context: %#v", entry)
		}
	}
}

func TestSceneHostProjectionsReturnProjectScenes(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	projectPath := filepath.Join(t.TempDir(), "scene-host")
	if err := store.Save(Entry{ID: "scene-artifact", Title: "Scene Host", Content: "scene artifact", Category: CategoryTaskArtifact, Tags: []string{projectPath}, SourceURL: filepath.Join(projectPath, "scene.md"), Scope: ScopeProject}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	scene, ok := store.SceneForProjectForHost(projectPath, 10)
	if !ok || scene.ProjectPath != projectPath || len(scene.RecentArtifacts) != 1 {
		t.Fatalf("SceneForProjectForHost = %#v, ok=%v", scene, ok)
	}
	scenes := store.ScenesByProjectForHost(10)
	if scenes[projectPath].ProjectPath != projectPath {
		t.Fatalf("ScenesByProjectForHost = %#v", scenes)
	}
	if _, ok := store.SceneForProjectForHost("missing", 10); ok {
		t.Fatalf("missing scene should not be found")
	}
	if nilStore := (*Store)(nil); nilStore.ScenesByProjectForHost(10) != nil {
		t.Fatalf("nil store scenes should be nil")
	}
}

func TestSearchProjectsForHostJoinsPrefsAndScenes(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	projectPath := filepath.Join(t.TempDir(), "project-search")
	if err := store.Save(Entry{ID: "search-artifact", Title: "Original Name", Content: "project search task artifact", Category: CategoryTaskArtifact, Tags: []string{projectPath}, SourceURL: filepath.Join(projectPath, "artifact.md"), Scope: ScopeProject}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	pi := store.ProjectIndex()
	if pi == nil {
		t.Fatal("ProjectIndex is nil")
	}
	pi.SetCustomName(projectPath, "Custom Name")
	pi.SetPinned(projectPath, true)

	items := store.SearchProjectsForHost("project search", 10)
	if len(items) != 1 {
		t.Fatalf("SearchProjectsForHost = %#v", items)
	}
	item := items[0]
	if item.Record.ProjectPath != projectPath || item.DisplayName != "Custom Name" || !item.Pinned {
		t.Fatalf("unexpected item = %#v", item)
	}
	if !item.HasScene || len(item.Scene.RecentArtifacts) != 1 {
		t.Fatalf("scene not joined: %#v", item)
	}
	single, ok := store.ProjectRecordForHost(projectPath)
	if !ok || single.DisplayName != "Custom Name" || !single.HasScene {
		t.Fatalf("ProjectRecordForHost = %#v, ok=%v", single, ok)
	}
}

func TestProjectPreferenceFacadesForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	projectPath := filepath.Join(t.TempDir(), "prefs")
	if err := store.Save(Entry{ID: "prefs-artifact", Title: "Prefs Original", Content: "prefs artifact", Category: CategoryTaskArtifact, Tags: []string{projectPath}, Scope: ScopeProject}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := store.ProjectDisplayNameForHost(projectPath); got != "Prefs Original" {
		t.Fatalf("ProjectDisplayNameForHost = %q", got)
	}
	if got := store.RenameProjectForHost(projectPath, "Prefs Custom"); got != "Prefs Custom" {
		t.Fatalf("RenameProjectForHost = %q", got)
	}
	store.PinProjectForHost(projectPath, true)
	item, ok := store.ProjectRecordForHost(projectPath)
	if !ok || !item.Pinned || item.DisplayName != "Prefs Custom" {
		t.Fatalf("ProjectRecordForHost after pin/rename = %#v, ok=%v", item, ok)
	}
	store.HideProjectForHost(projectPath)
	store.ArchiveProjectPreferenceForHost(projectPath, true)
	if !store.ProjectArchivedForHost(projectPath) {
		t.Fatalf("ProjectArchivedForHost should be true")
	}

	changedProject := ""
	store.SetProjectChangeHandlerForHost(func(projectPath string) {
		changedProject = projectPath
	})
	if err := store.Save(Entry{ID: "prefs-artifact-2", Title: "Prefs Followup", Content: "prefs followup", Category: CategoryTaskArtifact, Tags: []string{projectPath}, Scope: ScopeProject}); err != nil {
		t.Fatalf("Save followup: %v", err)
	}
	if changedProject != projectPath {
		t.Fatalf("SetProjectChangeHandlerForHost changed project = %q, want %q", changedProject, projectPath)
	}
}
