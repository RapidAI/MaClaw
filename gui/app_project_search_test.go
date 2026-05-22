package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func newProjectSearchTestApp(t *testing.T) *App {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))
	app := &App{testHomeDir: tempHome}
	t.Cleanup(func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})
	return app
}

func TestEnsureMemoryStoreDefaultsToSQLite(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	if app.memoryStore == nil {
		t.Fatal("memory store was not initialized")
	}
	dbPath := filepath.Join(memory.DataDirStoreDir(app.getMaclawBaseDir()), "memory.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected SQLite memory database at %s: %v", dbPath, err)
	}
}

func TestProjectIndexChangeTriggersDebouncedMemoryPipeline(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.memoryPipelineDebounce = 10 * time.Millisecond
	app.ensureMemoryStore()
	if app.memPipeline == nil {
		t.Fatal("memory pipeline was not initialized")
	}
	app.memPipeline.Stop()
	app.memPipeline = memory.NewMaintenance(app.memoryStore, nil, nil).Pipeline()
	app.memPipeline.Start()

	_, lastRun, _ := app.memPipeline.Status()
	if lastRun.IsZero() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			_, lastRun, _ = app.memPipeline.Status()
			if !lastRun.IsZero() {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if lastRun.IsZero() {
		t.Fatal("memory pipeline did not complete its initial run")
	}

	taskDir := filepath.Join(app.GetDataDir(), "tasks", "pipeline-trigger-smoke")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", taskDir, err)
	}
	taskFile := filepath.Join(taskDir, "task.md")
	if err := os.WriteFile(taskFile, []byte("# Pipeline trigger smoke\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", taskFile, err)
	}
	now := time.Now()
	if err := app.memoryStore.Save(memory.Entry{
		Title:      "Pipeline trigger smoke",
		Content:    "# Pipeline trigger smoke\n",
		Category:   memory.CategoryTaskArtifact,
		Tags:       []string{"manual_task", "recent_task"},
		SourceURL:  taskFile,
		SourceType: "manual",
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Save(memory.Entry): %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, nextRun, _ := app.memPipeline.Status()
		if nextRun.After(lastRun) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("project index change did not trigger a debounced memory pipeline run")
}

func TestCreateRecentTaskAddsSearchableRecentTask(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask("  \u65b0\u4efb\u52a1  ")
	if created.Name != "\u65b0\u4efb\u52a1" {
		t.Fatalf("Name = %q, want new task title", created.Name)
	}
	if created.ProjectPath == "" || created.ID == "" {
		t.Fatalf("expected project identifiers, got %#v", created)
	}
	if !strings.Contains(filepath.Clean(created.ProjectPath), filepath.Clean(filepath.Join(".maclaw", "data", "tasks"))) {
		t.Fatalf("ProjectPath = %q, want a synthetic task path under data/tasks", created.ProjectPath)
	}
	if created.EntryCount != 1 {
		t.Fatalf("EntryCount = %d, want 1", created.EntryCount)
	}
	if !created.HasOutput {
		t.Fatalf("HasOutput = false, want true for manual recent task")
	}
	taskFile := filepath.Join(created.ProjectPath, "task.md")
	content, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", taskFile, err)
	}
	if !strings.Contains(string(content), "# \u65b0\u4efb\u52a1") {
		t.Fatalf("task file content = %q, want task title", content)
	}

	found := false
	for _, result := range app.SearchProjects("\u65b0\u4efb\u52a1", 10) {
		if result.ProjectPath == created.ProjectPath && result.Name == "\u65b0\u4efb\u52a1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created task was not returned by SearchProjects")
	}
}

func TestSearchProjectsFiltersNonOutputRecords(t *testing.T) {
	app := newProjectSearchTestApp(t)
	app.ensureMemoryStore()
	now := time.Now()

	if err := app.memoryStore.Save(memory.Entry{
		Title:      "Small Talk Continue",
		Content:    "Task: continue\nResult: ok",
		Category:   memory.CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", filepath.Join(app.GetDataDir(), "tasks", "small-talk")},
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("Save non-output: %v", err)
	}
	if err := app.memoryStore.Save(memory.Entry{
		Title:      "Improve Recent Tasks Filtering",
		Content:    "Task: recent tasks\nResult: Added has_output filtering",
		Category:   memory.CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", filepath.Join(app.GetDataDir(), "tasks", "output-task")},
		CreatedAt:  now,
		UpdatedAt:  now.Add(time.Second),
	}); err != nil {
		t.Fatalf("Save output: %v", err)
	}

	results := app.SearchProjects("", 10)
	if len(results) != 1 {
		t.Fatalf("SearchProjects returned %d records, want 1: %+v", len(results), results)
	}
	if results[0].Name != "Improve Recent Tasks Filtering" || !results[0].HasOutput {
		t.Fatalf("result = %+v, want output-backed task", results[0])
	}
}

func TestCreateRecentTaskUsesTaskNamePreview(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask("Draft recent task filtering implementation")
	if created.Name != "Draft recent task filtering implementation" {
		t.Fatalf("Name = %q, want task name", created.Name)
	}
	if created.Preview == "Manual task placeholder." {
		t.Fatalf("Preview = %q, want a user-facing task preview", created.Preview)
	}
}

func TestCreateRecentTaskRejectsGenericCommandName(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask("Review/Fix/Optimize")
	if created.ID != "" || created.Name != "" || created.ProjectPath != "" || created.HasOutput {
		t.Fatalf("generic CreateRecentTask = %#v, want zero result", created)
	}
	if got := app.SearchProjects("", 10); len(got) != 0 {
		t.Fatalf("SearchProjects returned %d records for generic task", len(got))
	}
}

func TestCreateRecentTaskIgnoresBlankName(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask(" \t\n ")
	if created.ID != "" || created.Name != "" || created.ProjectPath != "" || len(created.Tags) != 0 {
		t.Fatalf("blank CreateRecentTask = %#v, want zero result", created)
	}
	if got := app.SearchProjects("", 10); len(got) != 0 {
		t.Fatalf("SearchProjects returned %d records for blank task", len(got))
	}
}

func TestRecentTaskSlug(t *testing.T) {
	tests := map[string]string{
		"Hello World!":             "hello-world",
		"---Alpha_Beta---":         "alpha_beta",
		"\u4e2d\u6587\u4efb\u52a1": "task",
		"  A   B   C  ":            "a-b-c",
		"01234567890123456789012345678901234567890123456789": "0123456789012345678901234567890123456789",
	}
	for input, want := range tests {
		if got := recentTaskSlug(input); got != want {
			t.Fatalf("recentTaskSlug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeRecentTaskName(t *testing.T) {
	if got := normalizeRecentTaskName("  Alpha\n\tBeta   Gamma  "); got != "Alpha Beta Gamma" {
		t.Fatalf("normalizeRecentTaskName whitespace = %q", got)
	}
	long := strings.Repeat("\u6d4b", 130)
	if got := normalizeRecentTaskName(long); len([]rune(got)) != 120 {
		t.Fatalf("normalizeRecentTaskName length = %d, want 120", len([]rune(got)))
	}
}
