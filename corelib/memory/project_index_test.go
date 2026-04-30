package memory

import (
	"path/filepath"
	"testing"
	"time"
)

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

	// Empty query returns all sorted by recency.
	results = pi.Search("", 10)
	if len(results) != 3 {
		t.Fatalf("expected 3 results for empty query, got %d", len(results))
	}
	if results[0].Name != "Todo List" {
		t.Errorf("expected most recent project first, got %q", results[0].Name)
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
			expected: filepath.Clean("/home/user/projects/app"),
		},
		{
			name:     "Directory path in tags",
			entry:    Entry{Tags: []string{"D:\\workprj\\myapp"}},
			expected: "D:\\workprj\\myapp",
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
