package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectIndex_ReplacePrefixedTags(t *testing.T) {
	pi := NewProjectIndex()
	path := `D:\work\tasks\remote-1`
	pi.IndexEntry(&Entry{
		ID:        "e1",
		Content:   "# remote task",
		Category:  CategoryTaskArtifact,
		Tags:      []string{"manual_task", "recent_task", path, "remote_coding_dev", "remote_host:10.0.0.1", "remote_workdir:/old"},
		SourceURL: filepath.Join(path, "task.md"),
		UpdatedAt: time.Now(),
	})
	pi.ReplacePrefixedTags(path, []string{"remote_host:", "remote_workdir:"}, []string{
		"remote_host:10.0.0.9",
		"remote_workdir:/new",
	})
	rec := pi.Get(path)
	if rec == nil {
		t.Fatal("record missing")
	}
	has := func(target string) bool {
		for _, tag := range rec.Tags {
			if tag == target {
				return true
			}
		}
		return false
	}
	if !has("remote_host:10.0.0.9") || has("remote_host:10.0.0.1") {
		t.Fatalf("host tags: %#v", rec.Tags)
	}
	if !has("remote_workdir:/new") || has("remote_workdir:/old") {
		t.Fatalf("workdir tags: %#v", rec.Tags)
	}
	if !has("remote_coding_dev") {
		t.Fatalf("non-prefixed tags should remain: %#v", rec.Tags)
	}
}

func TestProjectIndex_Rebuild(t *testing.T) {
	pi := NewProjectIndex()

	entries := []Entry{
		{
			ID:        "e1",
			Content:   "# Snake Game Requirements\nA classic snake game with keyboard controls.",
			Category:  CategoryTaskArtifact,
			Tags:      []string{"snake", "game", "workflow:coding"},
			SourceURL: "D:\\workprj\\snake\\requirements.md",
			UpdatedAt: time.Now().Add(-2 * time.Hour),
		},
		{
			ID:        "e2",
			Content:   "Technical design: use HTML5 Canvas for rendering.",
			Category:  CategoryProjectKnowledge,
			Tags:      []string{"snake", "canvas"},
			SourceURL: "D:\\workprj\\snake\\design.md",
			UpdatedAt: time.Now().Add(-1 * time.Hour),
		},
		{
			ID:        "e3",
			Content:   "# PPT Presentation Design\nProduct launch presentation.",
			Category:  CategoryTaskArtifact,
			Tags:      []string{"ppt", "presentation", "workflow:presentation_design"},
			SourceURL: "/home/user/projects/launch-ppt/slides.md",
			UpdatedAt: time.Now(),
		},
		{
			// Non-project entry (user_fact) — should be ignored.
			ID:       "e4",
			Content:  "User prefers dark mode.",
			Category: CategoryUserFact,
		},
	}

	pi.Rebuild(entries)

	if pi.Count() != 2 {
		t.Fatalf("expected 2 projects, got %d", pi.Count())
	}

	// Check snake project.
	snake := pi.Get("D:\\workprj\\snake")
	if snake == nil {
		t.Fatal("expected snake project to be indexed")
	}
	if snake.Name != "Snake Game Requirements" {
		t.Errorf("expected name 'Snake Game Requirements', got %q", snake.Name)
	}
	if snake.WorkflowType != "coding" {
		t.Errorf("expected workflow type 'coding', got %q", snake.WorkflowType)
	}
	if snake.EntryCount != 2 {
		t.Errorf("expected 2 entries, got %d", snake.EntryCount)
	}
}

func TestProjectIndex_Search(t *testing.T) {
	pi := NewProjectIndex()

	now := time.Now()
	entries := []Entry{
		{
			ID: "e1", Content: "# Snake Game\nClassic snake.", Category: CategoryTaskArtifact,
			Tags: []string{"snake", "game"}, SourceURL: "D:\\workprj\\snake\\req.md", UpdatedAt: now.Add(-3 * time.Hour),
		},
		{
			ID: "e2", Content: "# Chat App\nReal-time messaging.", Category: CategoryTaskArtifact,
			Tags: []string{"chat", "websocket"}, SourceURL: "D:\\workprj\\chat\\req.md", UpdatedAt: now.Add(-1 * time.Hour),
		},
		{
			ID: "e3", Content: "# Todo List\nSimple task manager.", Category: CategoryProjectKnowledge,
			Tags: []string{"todo", "task"}, SourceURL: "D:\\workprj\\todo\\notes.md", UpdatedAt: now,
		},
	}

	pi.Rebuild(entries)

	// Search for "snake".
	results := pi.Search("snake", 10)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'snake'")
	}
	if results[0].Name != "Snake Game" {
		t.Errorf("expected first result to be 'Snake Game', got %q", results[0].Name)
	}

	// Search for "chat".
	results = pi.Search("chat", 10)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'chat'")
	}
	if results[0].Name != "Chat App" {
		t.Errorf("expected first result to be 'Chat App', got %q", results[0].Name)
	}

	// Empty query returns output-backed projects sorted by recency.
	results = pi.Search("", 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 output results for empty query, got %d", len(results))
	}
	if results[0].Name != "Chat App" {
		t.Errorf("expected most recent project first, got %q", results[0].Name)
	}
}

func TestProjectIndex_RecentRequiresTangibleOutput(t *testing.T) {
	pi := NewProjectIndex()
	now := time.Now()

	pi.Rebuild([]Entry{
		{
			ID:         "chat",
			Title:      "Continue",
			Content:    "Task: continue\nResult: ok",
			Category:   CategoryProjectKnowledge,
			SourceType: "task_sediment",
			Tags:       []string{"task_sediment", "D:\\workprj\\chatty"},
			UpdatedAt:  now,
		},
		{
			ID:         "output",
			Title:      "Improve Recent Tasks Filtering",
			Content:    "Task: filter recent tasks\nResult: Added has_output filter and tests",
			Category:   CategoryProjectKnowledge,
			SourceType: "task_sediment",
			Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", "D:\\workprj\\maclaw"},
			UpdatedAt:  now.Add(-time.Minute),
		},
	})

	recent := pi.ListRecent(10)
	if len(recent) != 1 {
		t.Fatalf("ListRecent returned %d records, want 1", len(recent))
	}
	if recent[0].Name != "Improve Recent Tasks Filtering" || !recent[0].HasOutput {
		t.Fatalf("recent[0] = %+v, want output-backed task", recent[0])
	}
	if got := pi.Search("continue", 10); len(got) != 0 {
		t.Fatalf("Search returned non-output small talk: %+v", got)
	}
}

func TestProjectIndex_ListRecentMatchingFiltersBeforeSorting(t *testing.T) {
	pi := NewProjectIndex()
	now := time.Now()
	pi.Rebuild([]Entry{
		{
			ID: "automatic", Title: "Automatic", Content: "automatic", Category: CategoryTaskArtifact,
			Tags: []string{"tangible_output", "automatic", "C:/tasks/automatic"}, CreatedAt: now, UpdatedAt: now.Add(2 * time.Minute),
		},
		{
			ID: "older-task", Title: "Older task", Content: "older", Category: CategoryTaskArtifact,
			Tags: []string{"tangible_output", "task_management", "C:/tasks/older"}, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "newer-task", Title: "Newer task", Content: "newer", Category: CategoryTaskArtifact,
			Tags: []string{"tangible_output", "task_management", "C:/tasks/newer"}, CreatedAt: now, UpdatedAt: now.Add(time.Minute),
		},
	})

	matching := pi.ListRecentMatching(1, func(rec ProjectRecord) bool {
		for _, tag := range rec.Tags {
			if tag == "task_management" {
				return true
			}
		}
		return false
	})
	if len(matching) != 1 || matching[0].Name != "Newer task" {
		t.Fatalf("ListRecentMatching = %+v, want newest matching task", matching)
	}
}

func TestProjectIndex_SearchMatchingFiltersBeforeLimit(t *testing.T) {
	pi := NewProjectIndex()
	now := time.Now()
	pi.Rebuild([]Entry{
		{
			ID: "automatic", Title: "Automatic build", Content: "automatic", Category: CategoryTaskArtifact,
			Tags: []string{"tangible_output", "automatic", "C:/tasks/automatic"}, CreatedAt: now, UpdatedAt: now.Add(2 * time.Minute),
		},
		{
			ID: "managed", Title: "Managed build", Content: "managed", Category: CategoryTaskArtifact,
			Tags: []string{"tangible_output", "task_management", "C:/tasks/managed"}, CreatedAt: now, UpdatedAt: now,
		},
	})

	matching := pi.SearchMatching("build", 1, func(rec ProjectRecord) bool {
		for _, tag := range rec.Tags {
			if tag == "task_management" {
				return true
			}
		}
		return false
	})
	if len(matching) != 1 || matching[0].Name != "Managed build" {
		t.Fatalf("SearchMatching = %+v, want matching managed task", matching)
	}
}

func TestProjectIndex_OutputTitleAndPreviewWin(t *testing.T) {
	pi := NewProjectIndex()
	now := time.Now()
	projectPath := "D:\\workprj\\maclaw"

	pi.IndexEntry(&Entry{
		ID:        "note",
		Title:     "Review/Fix/Optimize",
		Content:   "Review/Fix/Optimize",
		Category:  CategoryProjectKnowledge,
		Tags:      []string{projectPath},
		UpdatedAt: now.Add(-2 * time.Hour),
	})
	pi.IndexEntry(&Entry{
		ID:         "older-output",
		Title:      "Filter Recent Tasks By Output",
		Content:    "Task: recent tasks\nResult: Hidden small talk from the sidebar",
		Category:   CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", projectPath},
		UpdatedAt:  now.Add(-time.Hour),
	})
	pi.IndexEntry(&Entry{
		ID:         "newer-output",
		Title:      "Improve Recent Task Titles",
		Content:    "Task: title quality\nResult: Generated descriptive titles from task results",
		Category:   CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", projectPath},
		UpdatedAt:  now,
	})

	recent := pi.ListRecent(1)
	if len(recent) != 1 {
		t.Fatalf("ListRecent len = %d, want 1", len(recent))
	}
	if recent[0].Name != "Improve Recent Task Titles" {
		t.Fatalf("Name = %q, want latest output title", recent[0].Name)
	}
	if recent[0].Preview != "Generated descriptive titles from task results" {
		t.Fatalf("Preview = %q, want output result preview", recent[0].Preview)
	}
}

func TestProjectIndex_SearchArchivedByEnglishAndChineseKeywords(t *testing.T) {
	pi := NewProjectIndex()
	projectPath := "D:\\workprj\\archived-task"
	pi.IndexEntry(&Entry{
		ID:         "archived-output",
		Title:      "Archived Task Notes",
		Content:    "Task: archive search\nResult: Saved archive summary",
		Category:   CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", projectPath},
		UpdatedAt:  time.Now(),
	})
	pi.SetArchived(projectPath, true)

	if got := pi.Search("Task Notes", 10); len(got) != 0 {
		t.Fatalf("Search without archive keyword returned archived task: %+v", got)
	}
	for _, query := range []string{"archive", "archived", "\u5f52\u6863", "\u6b78\u6a94"} {
		got := pi.Search(query, 10)
		if len(got) != 1 || got[0].ProjectPath != projectPath || !got[0].Archived {
			t.Fatalf("Search(%q) = %+v, want archived project", query, got)
		}
	}
}

func TestProjectIndex_TaskPrefsNormalizeSlashVariants(t *testing.T) {
	pi := NewProjectIndex()
	projectPath := "D:\\workprj\\slash-pref-task"
	slashPath := "D:/workprj/slash-pref-task"
	pi.IndexEntry(&Entry{
		ID:         "slash-pref-output",
		Title:      "Slash Pref Task",
		Content:    "Task: slash prefs\nResult: Saved prefs across path slash styles",
		Category:   CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", projectPath},
		UpdatedAt:  time.Now(),
	})

	pi.SetCustomName(slashPath, "Custom Slash Name")
	pi.SetPinned(slashPath, true)
	if got := pi.CustomName(projectPath); got != "Custom Slash Name" {
		t.Fatalf("CustomName(%q) = %q, want slash-variant custom name", projectPath, got)
	}
	if !pi.IsPinned(projectPath) {
		t.Fatalf("IsPinned(%q) = false, want true from slash-variant pref", projectPath)
	}

	pi.SetHidden(slashPath, true)
	if !pi.IsHidden(projectPath) {
		t.Fatalf("IsHidden(%q) = false, want true from slash-variant pref", projectPath)
	}
	if got := pi.ListRecent(10); len(got) != 0 {
		t.Fatalf("ListRecent after slash-variant hide = %+v, want hidden task omitted", got)
	}
}

func TestProjectIndex_LoadPrefsNormalizesSlashVariants(t *testing.T) {
	tempDir := t.TempDir()
	prefsPath := filepath.Join(tempDir, "task_prefs.json")
	if err := os.WriteFile(prefsPath, []byte(`{
  "prefs": {
    "D:/workprj/persisted-pref-task": { "name": "Persisted Slash Name", "hidden": true }
  }
}`), 0o644); err != nil {
		t.Fatalf("WriteFile(task_prefs.json): %v", err)
	}
	projectPath := "D:\\workprj\\persisted-pref-task"
	pi := NewProjectIndex(tempDir)
	pi.IndexEntry(&Entry{
		ID:         "persisted-pref-output",
		Title:      "Persisted Pref Task",
		Content:    "Task: persisted prefs\nResult: Loaded prefs across path slash styles",
		Category:   CategoryProjectKnowledge,
		SourceType: "task_sediment",
		Tags:       []string{"task_sediment", "tangible_output", "output_tool:edit_file", projectPath},
		UpdatedAt:  time.Now(),
	})

	if got := pi.CustomName(projectPath); got != "Persisted Slash Name" {
		t.Fatalf("CustomName(%q) = %q, want persisted slash-variant custom name", projectPath, got)
	}
	if !pi.IsHidden(projectPath) {
		t.Fatalf("IsHidden(%q) = false, want true from persisted slash-variant pref", projectPath)
	}
	if got := pi.ListRecent(10); len(got) != 0 {
		t.Fatalf("ListRecent after loading slash-variant hidden pref = %+v, want hidden task omitted", got)
	}
}

func TestProjectIndex_PathlessOutputArtifactIgnored(t *testing.T) {
	pi := NewProjectIndex()
	pi.IndexEntry(&Entry{
		ID:         "pathless",
		Title:      "Generated Report",
		Content:    "Result: Generated report",
		Category:   CategoryTaskArtifact,
		SourceType: "workflow_output",
		UpdatedAt:  time.Now(),
	})
	if pi.Count() != 0 {
		t.Fatalf("pathless output should not create project, count=%d", pi.Count())
	}
}

func TestProjectIndex_IncrementalUpdate(t *testing.T) {
	pi := NewProjectIndex()

	// Start with one entry.
	e1 := Entry{
		ID: "e1", Content: "# My Project\nInitial setup.", Category: CategoryProjectKnowledge,
		Tags: []string{"myproj"}, SourceURL: "D:\\dev\\myproj\\readme.md", UpdatedAt: time.Now(),
	}
	pi.IndexEntry(&e1)

	if pi.Count() != 1 {
		t.Fatalf("expected 1 project, got %d", pi.Count())
	}

	// Add another entry to the same project.
	e2 := Entry{
		ID: "e2", Content: "Added authentication module.", Category: CategoryProjectKnowledge,
		Tags: []string{"auth"}, SourceURL: "D:\\dev\\myproj\\auth.md", UpdatedAt: time.Now().Add(time.Hour),
	}
	pi.IndexEntry(&e2)

	if pi.Count() != 1 {
		t.Fatalf("expected still 1 project, got %d", pi.Count())
	}

	rec := pi.Get("D:\\dev\\myproj")
	if rec == nil {
		t.Fatal("expected project to exist")
	}
	if rec.EntryCount != 2 {
		t.Errorf("expected 2 entries, got %d", rec.EntryCount)
	}
}

func TestProjectIndex_DormantEntriesIgnored(t *testing.T) {
	pi := NewProjectIndex()

	e := Entry{
		ID: "e1", Content: "Old project.", Category: CategoryProjectKnowledge,
		SourceURL: "D:\\old\\proj\\readme.md", UpdatedAt: time.Now(),
		Status: StatusDormant,
	}
	pi.IndexEntry(&e)

	if pi.Count() != 0 {
		t.Errorf("expected dormant entries to be ignored, got %d projects", pi.Count())
	}
}

func TestProjectIndex_ReindexSameEntryNoDuplicateCount(t *testing.T) {
	pi := NewProjectIndex()

	e := Entry{
		ID: "e1", Content: "# My Project\nSome content.", Category: CategoryProjectKnowledge,
		Tags: []string{"myproj"}, SourceURL: "D:\\dev\\myproj\\readme.md", UpdatedAt: time.Now(),
	}

	// Index the same entry three times (simulates hash dedup + substring dedup
	// paths calling IndexEntry after tag merge).
	pi.IndexEntry(&e)
	pi.IndexEntry(&e)
	pi.IndexEntry(&e)

	rec := pi.Get("D:\\dev\\myproj")
	if rec == nil {
		t.Fatal("expected project to exist")
	}
	if rec.EntryCount != 1 {
		t.Errorf("expected EntryCount=1 after re-indexing same entry, got %d", rec.EntryCount)
	}
}

func TestInferProjectPath(t *testing.T) {
	tests := []struct {
		name     string
		entry    Entry
		expected string
	}{
		{
			name:     "Windows file path in SourceURL",
			entry:    Entry{SourceURL: "D:\\workprj\\snake\\requirements.md"},
			expected: "D:\\workprj\\snake",
		},
		{
			name:     "Unix file path in SourceURL",
			entry:    Entry{SourceURL: "/home/user/projects/app/design.md"},
			expected: "/home/user/projects/app",
		},
		{
			name:     "Directory path in tags",
			entry:    Entry{Tags: []string{"D:\\workprj\\myapp"}},
			expected: "D:\\workprj\\myapp",
		},
		{
			name:     "memory_refs SourceURL prefers project tag",
			entry:    Entry{SourceURL: "/home/user/.maclaw/memory_refs/workflow_output/u/2026-05/out.md", Tags: []string{"/home/user/project"}},
			expected: "/home/user/project",
		},
		{
			name:     "No path info",
			entry:    Entry{Tags: []string{"snake", "game"}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferProjectPath(&tt.entry)
			if got != tt.expected {
				t.Errorf("inferProjectPath() = %q, want %q", got, tt.expected)
			}
		})
	}
}
