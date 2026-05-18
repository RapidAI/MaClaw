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
	dbPath := filepath.Join(app.getMaclawBaseDir(), "memory.db")
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
	app.memPipeline = memory.NewPipeline(app.memoryStore, nil, nil, nil, nil)
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

	created := app.CreateRecentTask("  新任务  ")
	if created.Name != "新任务" {
		t.Fatalf("Name = %q, want 新任务", created.Name)
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
	taskFile := filepath.Join(created.ProjectPath, "task.md")
	content, err := os.ReadFile(taskFile)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", taskFile, err)
	}
	if !strings.Contains(string(content), "# 新任务") {
		t.Fatalf("task file content = %q, want task title", content)
	}

	found := false
	for _, result := range app.SearchProjects("新任务", 10) {
		if result.ProjectPath == created.ProjectPath && result.Name == "新任务" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created task was not returned by SearchProjects")
	}
}

func TestCreateRecentTaskUsesTaskNamePreview(t *testing.T) {
	app := newProjectSearchTestApp(t)

	created := app.CreateRecentTask("Review/Fix/Optimize")
	if created.Name != "Review/Fix/Optimize" {
		t.Fatalf("Name = %q, want task name", created.Name)
	}
	if created.Preview == "Manual task placeholder." {
		t.Fatalf("Preview = %q, want a user-facing task preview", created.Preview)
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
		"Hello World!":     "hello-world",
		"---Alpha_Beta---": "alpha_beta",
		"中文任务":             "task",
		"  A   B   C  ":    "a-b-c",
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
	long := strings.Repeat("测", 130)
	if got := normalizeRecentTaskName(long); len([]rune(got)) != 120 {
		t.Fatalf("normalizeRecentTaskName length = %d, want 120", len([]rune(got)))
	}
}
