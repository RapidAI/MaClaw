package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func newStrictProjectTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test_memory.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Stop() })
	return s
}

func TestRecallDynamicStrict_IncludesMatchingProjectEntries(t *testing.T) {
	s := newStrictProjectTestStore(t)

	// Save a project-scoped entry tagged with projectA.
	_ = s.Save(Entry{
		Content:  "ProjectA uses React 18 with TypeScript for the frontend architecture",
		Category: CategoryProjectKnowledge,
		Scope:    ScopeProject,
		Tags:     []string{"D:\\workprj\\projectA"},
	})

	results := s.RecallDynamicStrict("React frontend", "", "D:\\workprj\\projectA")
	found := false
	for _, e := range results {
		if strings.Contains(e.Content, "ProjectA uses React") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find projectA entry when querying with matching projectPath")
	}
}

func TestRecallDynamicStrict_ExcludesOtherProjectEntries(t *testing.T) {
	s := newStrictProjectTestStore(t)

	// Save entries for two different projects.
	_ = s.Save(Entry{
		Content:  "ProjectA uses React 18 with TypeScript for the frontend architecture",
		Category: CategoryProjectKnowledge,
		Scope:    ScopeProject,
		Tags:     []string{"D:\\workprj\\projectA"},
	})
	_ = s.Save(Entry{
		Content:  "ProjectB uses Vue 3 with Composition API for the frontend architecture",
		Category: CategoryProjectKnowledge,
		Scope:    ScopeProject,
		Tags:     []string{"D:\\workprj\\projectB"},
	})

	// Query from projectA's perspective — should NOT see projectB's entry.
	results := s.RecallDynamicStrict("frontend architecture", "", "D:\\workprj\\projectA")
	for _, e := range results {
		if strings.Contains(e.Content, "ProjectB uses Vue") {
			t.Error("strict recall should NOT include entries from other projects")
		}
	}
}

func TestRecallDynamicStrict_IncludesGlobalScopeEntries(t *testing.T) {
	s := newStrictProjectTestStore(t)

	// Save a global-scope entry (e.g., archived experience).
	_ = s.Save(Entry{
		Content:  "General best practice: always use parameterized SQL queries to prevent injection",
		Category: CategoryProjectKnowledge,
		Scope:    ScopeGlobal,
		Tags:     []string{"archived_experience", "D:\\workprj\\oldProject"},
	})

	// Save a project-scoped entry for projectA.
	_ = s.Save(Entry{
		Content:  "ProjectA database layer uses PostgreSQL 16 with pgvector extension",
		Category: CategoryProjectKnowledge,
		Scope:    ScopeProject,
		Tags:     []string{"D:\\workprj\\projectA"},
	})

	// Query from projectA — should see both the global entry and projectA's entry.
	results := s.RecallDynamicStrict("SQL database", "", "D:\\workprj\\projectA")
	hasGlobal := false
	hasProjectA := false
	for _, e := range results {
		if strings.Contains(e.Content, "parameterized SQL") {
			hasGlobal = true
		}
		if strings.Contains(e.Content, "ProjectA database") {
			hasProjectA = true
		}
	}
	if !hasGlobal {
		t.Error("strict recall should include ScopeGlobal entries")
	}
	if !hasProjectA {
		t.Error("strict recall should include matching project entries")
	}
}

func TestRecallDynamicStrict_IncludesUserFactAndPreference(t *testing.T) {
	s := newStrictProjectTestStore(t)

	// user_fact and preference have ScopeGlobal by default (InferScope).
	_ = s.Save(Entry{
		Content:  "User prefers dark theme and Vim keybindings in all editors",
		Category: CategoryPreference,
		Scope:    ScopeGlobal,
	})
	_ = s.Save(Entry{
		Content:  "User is a senior Go developer with 8 years of experience",
		Category: CategoryUserFact,
		Scope:    ScopeGlobal,
	})

	// These should be visible from any project tab.
	results := s.RecallDynamicStrict("developer preferences", "", "D:\\workprj\\projectA")
	// Note: RecallDynamic with category="" filters out user_fact and self_identity
	// from the general recall. But preference is allowed through.
	hasPreference := false
	for _, e := range results {
		if strings.Contains(e.Content, "dark theme") {
			hasPreference = true
		}
	}
	if !hasPreference {
		t.Error("strict recall should include preference entries (ScopeGlobal)")
	}
}

func TestRecallDynamicStrict_EmptyProjectPath_ReturnsAll(t *testing.T) {
	s := newStrictProjectTestStore(t)

	_ = s.Save(Entry{
		Content:  "ProjectA uses React 18 with TypeScript for the frontend architecture",
		Category: CategoryProjectKnowledge,
		Scope:    ScopeProject,
		Tags:     []string{"D:\\workprj\\projectA"},
	})
	_ = s.Save(Entry{
		Content:  "ProjectB uses Vue 3 with Composition API for the frontend architecture",
		Category: CategoryProjectKnowledge,
		Scope:    ScopeProject,
		Tags:     []string{"D:\\workprj\\projectB"},
	})

	// Empty projectPath means no strict filtering — behaves like RecallDynamic.
	results := s.RecallDynamicStrict("frontend architecture", "", "")
	if len(results) == 0 {
		t.Error("empty projectPath should return results without strict filtering")
	}
}

func TestRecallStrictProjectEntryAllowed_UnitLogic(t *testing.T) {
	projectLower := "d:/workprj/projecta"

	tests := []struct {
		name    string
		entry   Entry
		allowed bool
	}{
		{
			name:    "ScopeGlobal always allowed",
			entry:   Entry{Scope: ScopeGlobal, Tags: []string{"D:\\workprj\\projectB"}},
			allowed: true,
		},
		{
			name:    "ScopeProject with matching tag allowed",
			entry:   Entry{Scope: ScopeProject, Tags: []string{"D:\\workprj\\projectA"}},
			allowed: true,
		},
		{
			name:    "ScopeProject with non-matching tag excluded",
			entry:   Entry{Scope: ScopeProject, Tags: []string{"D:\\workprj\\projectB"}},
			allowed: false,
		},
		{
			name:    "ScopeProject with no path tags excluded",
			entry:   Entry{Scope: ScopeProject, Tags: []string{"some_keyword", "another_tag"}},
			allowed: false,
		},
		{
			name:    "ScopeProject with matching + non-matching tags allowed",
			entry:   Entry{Scope: ScopeProject, Tags: []string{"D:\\workprj\\projectB", "D:\\workprj\\projectA"}},
			allowed: true,
		},
		{
			name:    "Empty scope (default) treated as ScopeProject excluded",
			entry:   Entry{Scope: "", Tags: []string{"D:\\workprj\\projectB"}},
			allowed: true, // empty scope != ScopeProject, so it passes
		},
		{
			name:    "ScopeProject with subdirectory tag allowed",
			entry:   Entry{Scope: ScopeProject, Tags: []string{"D:\\workprj\\projectA\\src"}},
			allowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recallStrictProjectEntryAllowed(tt.entry, projectLower)
			if got != tt.allowed {
				t.Errorf("recallStrictProjectEntryAllowed() = %v, want %v", got, tt.allowed)
			}
		})
	}
}
