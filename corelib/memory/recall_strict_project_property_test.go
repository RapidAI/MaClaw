package memory

import (
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 3.2, 3.3**
//
// Property 8: Strict project recall filtering
// RecallDynamicStrict only returns entries matching the current project or universal knowledge.

// genProjectPath generates a random Windows-style project path.
func genProjectPath() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		drive := rapid.SampledFrom([]string{"C", "D", "E"}).Draw(t, "drive")
		depth := rapid.IntRange(1, 3).Draw(t, "depth")
		parts := make([]string, depth)
		for i := range parts {
			parts[i] = rapid.StringMatching(`[a-z]{3,10}`).Draw(t, "part")
		}
		return drive + ":\\" + strings.Join(parts, "\\")
	})
}

// genEntryContent generates a non-empty content string with enough substance for BM25 matching.
func genEntryContent() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		words := []string{"project", "code", "architecture", "design", "module", "service",
			"database", "frontend", "backend", "testing", "deployment", "config",
			"build", "release", "feature", "component", "library", "framework"}
		n := rapid.IntRange(3, 8).Draw(t, "wordCount")
		selected := make([]string, n)
		for i := range selected {
			selected[i] = rapid.SampledFrom(words).Draw(t, "word")
		}
		// Add a unique suffix to avoid substring dedup
		suffix := rapid.StringMatching(`[a-z0-9]{8}`).Draw(t, "suffix")
		return strings.Join(selected, " ") + " " + suffix
	})
}

// TestProperty_StrictRecall_ProjectScopeMatchingIncluded verifies that
// entries with ScopeProject + matching projectPath tags are always included
// in RecallDynamicStrict results.
func TestProperty_StrictRecall_ProjectScopeMatchingIncluded(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		s, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer s.Stop()

		projectPath := genProjectPath().Draw(rt, "projectPath")
		content := genEntryContent().Draw(rt, "content")

		// Save a ScopeProject entry tagged with the target project.
		err = s.Save(Entry{
			Content:  content,
			Category: CategoryProjectKnowledge,
			Scope:    ScopeProject,
			Tags:     []string{projectPath},
		})
		if err != nil {
			rt.Fatal(err)
		}

		// Use a query that matches the content.
		query := strings.Fields(content)[0]
		results := s.RecallDynamicStrict(query, "", projectPath)

		// The entry must be in the results.
		found := false
		for _, e := range results {
			if e.Content == content {
				found = true
				break
			}
		}
		if !found {
			rt.Fatalf("ScopeProject entry with matching projectPath tag should be included in strict recall results")
		}
	})
}

// TestProperty_StrictRecall_ProjectScopeNonMatchingExcluded verifies that
// entries with ScopeProject + non-matching projectPath tags are always excluded
// from RecallDynamicStrict results.
func TestProperty_StrictRecall_ProjectScopeNonMatchingExcluded(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		s, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer s.Stop()

		projectA := genProjectPath().Draw(rt, "projectA")
		projectB := genProjectPath().Draw(rt, "projectB")

		// Ensure the two paths are different.
		if strings.EqualFold(projectA, projectB) {
			rt.Skip("generated identical paths")
		}

		content := genEntryContent().Draw(rt, "content")

		// Save a ScopeProject entry tagged with projectB.
		err = s.Save(Entry{
			Content:  content,
			Category: CategoryProjectKnowledge,
			Scope:    ScopeProject,
			Tags:     []string{projectB},
		})
		if err != nil {
			rt.Fatal(err)
		}

		// Query from projectA's perspective.
		query := strings.Fields(content)[0]
		results := s.RecallDynamicStrict(query, "", projectA)

		// The entry must NOT be in the results.
		for _, e := range results {
			if e.Content == content {
				rt.Fatalf("ScopeProject entry with non-matching projectPath tag should be excluded from strict recall results")
			}
		}
	})
}

// TestProperty_StrictRecall_GlobalScopeAlwaysIncluded verifies that
// entries with ScopeGlobal are always included regardless of their tags.
func TestProperty_StrictRecall_GlobalScopeAlwaysIncluded(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		s, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer s.Stop()

		projectPath := genProjectPath().Draw(rt, "projectPath")
		otherPath := genProjectPath().Draw(rt, "otherPath")
		content := genEntryContent().Draw(rt, "content")

		// Save a ScopeGlobal entry — may have tags from another project.
		err = s.Save(Entry{
			Content:  content,
			Category: CategoryProjectKnowledge,
			Scope:    ScopeGlobal,
			Tags:     []string{otherPath, "archived_experience"},
		})
		if err != nil {
			rt.Fatal(err)
		}

		// Query from projectPath's perspective.
		query := strings.Fields(content)[0]
		results := s.RecallDynamicStrict(query, "", projectPath)

		// The ScopeGlobal entry must be in the results.
		found := false
		for _, e := range results {
			if e.Content == content {
				found = true
				break
			}
		}
		if !found {
			rt.Fatalf("ScopeGlobal entry should always be included in strict recall results regardless of tags")
		}
	})
}

// TestProperty_StrictRecall_UserFactPreferenceAlwaysIncluded verifies that
// entries with user_fact/preference categories (which have ScopeGlobal) are
// always included in strict recall results.
func TestProperty_StrictRecall_UserFactPreferenceAlwaysIncluded(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		s, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer s.Stop()

		projectPath := genProjectPath().Draw(rt, "projectPath")
		content := genEntryContent().Draw(rt, "content")

		// Save a preference entry with ScopeGlobal (no project tags).
		err = s.Save(Entry{
			Content:  content,
			Category: CategoryPreference,
			Scope:    ScopeGlobal,
		})
		if err != nil {
			rt.Fatal(err)
		}

		// Query from any project.
		query := strings.Fields(content)[0]
		results := s.RecallDynamicStrict(query, "", projectPath)

		// The entry must be in the results.
		found := false
		for _, e := range results {
			if e.Content == content {
				found = true
				break
			}
		}
		if !found {
			rt.Fatalf("preference entries (ScopeGlobal) should always be included in strict recall")
		}
	})
}

// TestProperty_StrictRecall_EmptyProjectPathReturnsAll verifies that
// when projectPath is empty, RecallDynamicStrict returns all entries
// without strict filtering (behaves like RecallDynamic).
func TestProperty_StrictRecall_EmptyProjectPathReturnsAll(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		s, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer s.Stop()

		projectA := genProjectPath().Draw(rt, "projectA")
		projectB := genProjectPath().Draw(rt, "projectB")

		// Ensure different paths.
		if strings.EqualFold(projectA, projectB) {
			rt.Skip("generated identical paths")
		}

		contentA := genEntryContent().Draw(rt, "contentA")
		contentB := genEntryContent().Draw(rt, "contentB")

		// Save entries for two different projects.
		_ = s.Save(Entry{
			Content:  contentA,
			Category: CategoryProjectKnowledge,
			Scope:    ScopeProject,
			Tags:     []string{projectA},
		})
		_ = s.Save(Entry{
			Content:  contentB,
			Category: CategoryProjectKnowledge,
			Scope:    ScopeProject,
			Tags:     []string{projectB},
		})

		// Query with empty projectPath — should not apply strict filtering.
		query := "project code architecture design module service database"
		results := s.RecallDynamicStrict(query, "", "")

		// With empty projectPath, strict filtering is disabled.
		// Results should be at least as large as either strict-filtered result.
		strictA := s.RecallDynamicStrict(query, "", projectA)
		strictB := s.RecallDynamicStrict(query, "", projectB)

		if len(results) < len(strictA) || len(results) < len(strictB) {
			rt.Fatalf("empty projectPath should return at least as many results as strict filtering: "+
				"empty=%d, strictA=%d, strictB=%d", len(results), len(strictA), len(strictB))
		}
	})
}
