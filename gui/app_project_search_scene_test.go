package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestSearchProjectsIncludesSceneSourceArtifacts(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store was not initialized")
	}

	projectDir := filepath.Join(t.TempDir(), "scene-project")
	refPath := filepath.Join(app.GetDataDir(), "memory_refs", "workflow_output", "desktop-user", "2026-05", "requirements.md")
	entry := memory.Entry{
		Title:      "Scene Requirements",
		Content:    "# Scene Requirements\nPersist source refs in task search results.",
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"workflow", "workflow:coding", projectDir},
		SourceType: "workflow_output_ref",
		SourceURL:  refPath,
	}
	if err := app.memoryStore.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	results := app.SearchProjects("Scene Requirements", 10)
	if len(results) == 0 {
		t.Fatal("SearchProjects returned no results")
	}
	var found ProjectSearchResult
	for _, result := range results {
		if filepath.Clean(result.ProjectPath) == filepath.Clean(projectDir) {
			found = result
			break
		}
	}
	if found.ProjectPath == "" {
		t.Fatalf("project %q was not returned: %#v", projectDir, results)
	}
	if len(found.SourceURLs) == 0 || found.SourceURLs[0] != refPath {
		t.Fatalf("SourceURLs = %#v, want first %q", found.SourceURLs, refPath)
	}
	if len(found.RecentArtifacts) == 0 {
		t.Fatalf("RecentArtifacts is empty for result %#v", found)
	}
	artifact := found.RecentArtifacts[0]
	if artifact.SourceType != "workflow_output_ref" || artifact.SourceURL != refPath {
		t.Fatalf("artifact source = (%q, %q), want workflow_output_ref and %q", artifact.SourceType, artifact.SourceURL, refPath)
	}
	if artifact.SourceHint != "full: read_file" {
		t.Fatalf("artifact source hint = %q, want full: read_file", artifact.SourceHint)
	}
	if !strings.Contains(artifact.Title, "Scene Requirements") {
		t.Fatalf("artifact title = %q, want Scene Requirements", artifact.Title)
	}
}

func TestBuildProjectTabContextMessageIncludesSceneSources(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store was not initialized")
	}

	projectDir := filepath.Join(t.TempDir(), "context-project")
	refPath := filepath.Join(app.GetDataDir(), "memory_refs", "workflow_output", "desktop-user", "2026-05", "design.md")
	entry := memory.Entry{
		Title:      "Context Design",
		Content:    "# Context Design\nPreserve full workflow design as a source ref.",
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"workflow", "workflow:coding", projectDir},
		SourceType: "workflow_output_ref",
		SourceURL:  refPath,
	}
	if err := app.memoryStore.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	message := app.buildProjectTabContextMessage(projectDir)
	if !strings.Contains(message, "最近产物来源") {
		t.Fatalf("context message did not include recent artifact source section: %s", message)
	}
	if !strings.Contains(message, "Context Design") || !strings.Contains(message, refPath) {
		t.Fatalf("context message = %q, want artifact title and source ref %q", message, refPath)
	}
	if !strings.Contains(message, "full: read_file") {
		t.Fatalf("context message = %q, want read_file drill-down hint", message)
	}
}

func TestGetProjectSceneReturnsSourceBackedArtifacts(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store was not initialized")
	}

	projectDir := filepath.Join(t.TempDir(), "detail-project")
	refPath := filepath.Join(app.GetDataDir(), "memory_refs", "workflow_output", "desktop-user", "2026-05", "implementation.md")
	entry := memory.Entry{
		Title:      "Implementation Notes",
		Content:    "# Implementation Notes\nUse source-backed scene details.",
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"workflow", "workflow:coding", projectDir},
		SourceType: "workflow_output_ref",
		SourceURL:  refPath,
	}
	if err := app.memoryStore.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	detail, err := app.GetProjectScene(projectDir)
	if err != nil {
		t.Fatalf("GetProjectScene: %v", err)
	}
	if detail.ProjectPath != projectDir || detail.EntryCount == 0 {
		t.Fatalf("detail = %#v, want project path and entries", detail)
	}
	if len(detail.RecentArtifacts) == 0 {
		t.Fatalf("RecentArtifacts is empty: %#v", detail)
	}
	artifact := detail.RecentArtifacts[0]
	if artifact.Title != "Implementation Notes" || artifact.SourceURL != refPath || artifact.SourceHint != "full: read_file" {
		t.Fatalf("artifact = %#v, want source-backed artifact with read_file hint", artifact)
	}
}

func TestRecentTaskSearchAndSceneUseWorkingDirArtifacts(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store was not initialized")
	}

	workingDir := filepath.Join(t.TempDir(), "task-workdir")
	task := app.CreateRecentTaskWithWorkingDir("Working dir scene task", workingDir)
	if task.ProjectPath == "" {
		t.Fatalf("CreateRecentTaskWithWorkingDir returned empty task: %#v", task)
	}
	refPath := filepath.Join(app.GetDataDir(), "memory_refs", "workflow_output", "desktop-user", "2026-05", "working-dir-output.md")
	entry := memory.Entry{
		Title:      "Working Dir Output",
		Content:    "# Working Dir Output\nUse execution directory scene artifacts.",
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"workflow", "workflow:coding", filepath.Clean(workingDir)},
		SourceType: "workflow_output_ref",
		SourceURL:  refPath,
	}
	if err := app.memoryStore.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	results := app.SearchProjects("Working dir scene task", 10)
	if len(results) == 0 {
		t.Fatal("SearchProjects returned no results")
	}
	foundSearchArtifact := false
	for _, artifact := range results[0].RecentArtifacts {
		if artifact.SourceURL == refPath {
			foundSearchArtifact = true
			break
		}
	}
	if !foundSearchArtifact {
		t.Fatalf("SearchProjects artifacts = %#v, want working dir artifact %q", results[0].RecentArtifacts, refPath)
	}

	detail, err := app.GetProjectScene(task.ProjectPath)
	if err != nil {
		t.Fatalf("GetProjectScene: %v", err)
	}
	if detail.ProjectPath != task.ProjectPath {
		t.Fatalf("detail.ProjectPath = %q, want task path %q", detail.ProjectPath, task.ProjectPath)
	}
	foundSceneArtifact := false
	for _, artifact := range detail.RecentArtifacts {
		if artifact.SourceURL == refPath {
			foundSceneArtifact = true
			break
		}
	}
	if !foundSceneArtifact {
		t.Fatalf("GetProjectScene artifacts = %#v, want working dir artifact %q", detail.RecentArtifacts, refPath)
	}
}

func TestBuildProjectTabContextMessageUsesWorkingDirContext(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store was not initialized")
	}

	workingDir := filepath.Join(t.TempDir(), "task-context-workdir")
	task := app.CreateRecentTaskWithWorkingDir("Working dir context task", workingDir)
	if task.ProjectPath == "" {
		t.Fatalf("CreateRecentTaskWithWorkingDir returned empty task: %#v", task)
	}
	refPath := filepath.Join(app.GetDataDir(), "memory_refs", "workflow_output", "desktop-user", "2026-05", "working-dir-context.md")
	entry := memory.Entry{
		Title:      "Working Dir Context",
		Content:    "# Working Dir Context\nUse execution directory for tab bootstrap context.",
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"workflow", "workflow:coding", filepath.Clean(workingDir)},
		SourceType: "workflow_output_ref",
		SourceURL:  refPath,
	}
	if err := app.memoryStore.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	message := app.buildProjectTabContextMessage(task.ProjectPath)
	if !strings.Contains(message, "Working Dir Context") || !strings.Contains(message, refPath) {
		t.Fatalf("context message = %q, want working dir artifact title and source ref %q", message, refPath)
	}
}
