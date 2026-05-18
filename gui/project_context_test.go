package main

import (
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// ---------------------------------------------------------------------------
// Property-based tests for LoadProjectContext (project-tab-isolation feature).
//
// **Validates: Requirements 4.3**
//
// Property 9: Project context summary completeness
// LoadProjectContext returns a summary with non-empty ProjectName for any
// valid projectPath.
// ---------------------------------------------------------------------------

// projectPathGen generates random valid project paths (Windows-style).
type projectPathGen struct {
	Path string
}

func (projectPathGen) Generate(r *rand.Rand, size int) reflect.Value {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	drives := []string{"C", "D", "E"}
	drive := drives[r.Intn(len(drives))]

	// Generate 1-3 path segments
	segments := r.Intn(3) + 1
	parts := make([]string, segments)
	for i := range parts {
		n := r.Intn(12) + 3
		b := make([]byte, n)
		for j := range b {
			b[j] = letters[r.Intn(len(letters))]
		}
		parts[i] = string(b)
	}

	path := drive + `:\` + strings.Join(parts, `\`)
	return reflect.ValueOf(projectPathGen{Path: path})
}

// newProjectContextTestApp creates a minimal App with a memory store for testing.
func newProjectContextTestApp(t *testing.T) *App {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	app := &App{testHomeDir: tempHome}

	memPath := filepath.Join(tempHome, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	app.memoryStore = ms
	t.Cleanup(func() { ms.Stop() })
	return app
}

// TestProperty9_LoadProjectContext_NonEmptyProjectName verifies that
// LoadProjectContext with any valid projectPath returns a non-nil summary
// with a non-empty ProjectName derived from the last path segment.
func TestProperty9_LoadProjectContext_NonEmptyProjectName(t *testing.T) {
	app := newProjectContextTestApp(t)

	f := func(gen projectPathGen) bool {
		summary, err := app.LoadProjectContext(gen.Path)
		if err != nil {
			return false
		}
		if summary == nil {
			return false
		}
		if summary.ProjectName == "" {
			return false
		}
		// ProjectName should be derived from the last path segment.
		expected := filepath.Base(gen.Path)
		return summary.ProjectName == expected
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("Property 9 failed: %v", err)
	}
}

// TestProperty9_LoadProjectContext_EmptyPathReturnsError verifies that
// LoadProjectContext with an empty projectPath returns an error.
func TestProperty9_LoadProjectContext_EmptyPathReturnsError(t *testing.T) {
	app := newProjectContextTestApp(t)

	emptyPaths := []string{"", "   ", "\t", "\n", "  \t\n  "}
	for _, p := range emptyPaths {
		summary, err := app.LoadProjectContext(p)
		if err == nil {
			t.Errorf("LoadProjectContext(%q) should return error, got summary=%+v", p, summary)
		}
		if summary != nil {
			t.Errorf("LoadProjectContext(%q) should return nil summary, got %+v", p, summary)
		}
	}
}

// TestProperty9_LoadProjectContext_ProjectNameFromLastSegment verifies that
// the ProjectName is always derived from the last path segment.
func TestProperty9_LoadProjectContext_ProjectNameFromLastSegment(t *testing.T) {
	app := newProjectContextTestApp(t)

	cases := []struct {
		path     string
		wantName string
	}{
		{`D:\workprj\aicoder`, "aicoder"},
		{`C:\Users\dev\projects\my-game`, "my-game"},
		{`E:\single`, "single"},
		{`D:\a\b\c\deep-nested`, "deep-nested"},
	}

	for _, tc := range cases {
		summary, err := app.LoadProjectContext(tc.path)
		if err != nil {
			t.Fatalf("LoadProjectContext(%q) error: %v", tc.path, err)
		}
		if summary.ProjectName != tc.wantName {
			t.Errorf("LoadProjectContext(%q).ProjectName = %q, want %q", tc.path, summary.ProjectName, tc.wantName)
		}
	}
}

// TestProperty9_LoadProjectContext_TaskArtifactPopulatesRecentProgress verifies
// that when memory store has task_artifact entries for the project,
// RecentProgress is populated.
func TestProperty9_LoadProjectContext_TaskArtifactPopulatesRecentProgress(t *testing.T) {
	app := newProjectContextTestApp(t)

	projectPath := `D:\workprj\test-project`

	// Save a task_artifact entry tagged with the project path.
	entry := memory.Entry{
		Content:  "Completed requirements analysis for the game engine module.",
		Category: memory.CategoryTaskArtifact,
		Tags:     []string{projectPath},
		Scope:    memory.ScopeProject,
	}
	app.memoryStore.Save(entry)

	summary, err := app.LoadProjectContext(projectPath)
	if err != nil {
		t.Fatalf("LoadProjectContext error: %v", err)
	}
	if summary.RecentProgress == "" {
		t.Error("RecentProgress should be populated when task_artifact entries exist for the project")
	}
	if !strings.Contains(summary.RecentProgress, "Completed requirements") {
		t.Errorf("RecentProgress = %q, want to contain task artifact content", summary.RecentProgress)
	}
}

// TestProperty9_LoadProjectContext_FilePathTagsPopulateKeyArtifacts verifies
// that when memory store has entries with file path tags, KeyArtifacts is populated.
func TestProperty9_LoadProjectContext_FilePathTagsPopulateKeyArtifacts(t *testing.T) {
	app := newProjectContextTestApp(t)

	projectPath := `D:\workprj\artifact-project`
	filePath := `D:\workprj\artifact-project\src\main.go`

	// Save a project_knowledge entry with a file path tag.
	entry := memory.Entry{
		Content:   "Main entry point for the application.",
		Category:  memory.CategoryProjectKnowledge,
		Tags:      []string{projectPath, filePath},
		Scope:     memory.ScopeProject,
		SourceURL: `D:\workprj\artifact-project\docs\design.md`,
	}
	app.memoryStore.Save(entry)

	summary, err := app.LoadProjectContext(projectPath)
	if err != nil {
		t.Fatalf("LoadProjectContext error: %v", err)
	}
	if len(summary.KeyArtifacts) == 0 {
		t.Error("KeyArtifacts should be populated when entries have file path tags")
	}

	// Check that at least one of the file paths is in KeyArtifacts.
	found := false
	for _, artifact := range summary.KeyArtifacts {
		if artifact == filePath || artifact == `D:\workprj\artifact-project\docs\design.md` {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("KeyArtifacts = %v, want to contain file path %q or SourceURL", summary.KeyArtifacts, filePath)
	}
}

func TestLoadProjectContextIncludesSceneRecentArtifacts(t *testing.T) {
	app := newProjectContextTestApp(t)

	projectPath := filepath.Join(t.TempDir(), "scene-context")
	refPath := filepath.Join(app.GetDataDir(), "memory_refs", "workflow_output", "desktop-user", "2026-05", "design.md")
	entry := memory.Entry{
		Title:      "Scene Context Design",
		Content:    "# Scene Context Design\nKeep full design content behind a source ref.",
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"workflow", "workflow:coding", projectPath},
		SourceType: "workflow_output_ref",
		SourceURL:  refPath,
		Scope:      memory.ScopeProject,
	}
	if err := app.memoryStore.Save(entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	summary, err := app.LoadProjectContext(projectPath)
	if err != nil {
		t.Fatalf("LoadProjectContext error: %v", err)
	}
	if len(summary.RecentArtifacts) == 0 {
		t.Fatalf("RecentArtifacts is empty: %#v", summary)
	}
	artifact := summary.RecentArtifacts[0]
	if artifact.Title != "Scene Context Design" || artifact.SourceType != "workflow_output_ref" || artifact.SourceURL != refPath {
		t.Fatalf("RecentArtifacts[0] = %#v, want source-backed scene artifact", artifact)
	}
	if artifact.SourceHint != "full: read_file" {
		t.Fatalf("RecentArtifacts[0].SourceHint = %q, want full: read_file", artifact.SourceHint)
	}
	foundSource := false
	for _, path := range summary.KeyArtifacts {
		if path == refPath {
			foundSource = true
			break
		}
	}
	if !foundSource {
		t.Fatalf("KeyArtifacts = %#v, want source ref %q", summary.KeyArtifacts, refPath)
	}
}
