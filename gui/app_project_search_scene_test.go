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
