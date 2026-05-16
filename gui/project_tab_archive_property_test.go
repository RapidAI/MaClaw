package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
	"pgregory.net/rapid"
)

// ============================================================================
// Feature: project-tab-isolation
// Property 10: Archive entry correctness
// After archiving, a project_knowledge entry with ScopeGlobal and
// "archived_experience" tag exists.
// **Validates: Requirements 6.3, 6.4**
//
// Property 11: Archived task hidden by default
// After archiving, the task is hidden from the default list but findable
// via search.
// **Validates: Requirements 7.2**
// ============================================================================

// --- Mock LLM caller for archive tests ---

type mockArchiveLLMCaller struct {
	mu        sync.Mutex
	callCount int
	response  string
	err       error
}

func (m *mockArchiveLLMCaller) LLMClassify(_ context.Context, req LLMClassifyRequest) (*LLMClassifyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	resp := m.response
	if resp == "" {
		resp = fmt.Sprintf("## 任务目标\n测试项目经验\n\n## 技术方案\nGo + rapid\n\n## 关键决策\n使用 property-based testing\n\n## 踩坑与解决\n无\n\n## 产出物\n测试文件\n\n## 可复用经验\n使用 rapid 库")
	}
	return &LLMClassifyResult{
		Text:         resp,
		InputTokens:  100,
		OutputTokens: 50,
		Latency:      200 * time.Millisecond,
	}, nil
}

// --- Generators ---

// genProjectPath generates a random project path (Windows or Unix style).
func genProjectPath(t *rapid.T, label string) string {
	style := rapid.IntRange(0, 1).Draw(t, label+"_style")
	name := rapid.StringMatching(`[a-z]{3,12}`).Draw(t, label+"_name")
	if style == 0 {
		// Windows style
		drive := rapid.StringMatching(`[A-Z]`).Draw(t, label+"_drive")
		return fmt.Sprintf("%s:\\workprj\\%s", drive, name)
	}
	// Unix style
	return fmt.Sprintf("/home/user/projects/%s", name)
}

// genProjectName generates a random project name.
func genProjectName(t *rapid.T, label string) string {
	return rapid.StringMatching(`[A-Za-z0-9 ]{3,20}`).Draw(t, label)
}

// --- Helper: create a memory store with project entries ---

func createStoreWithProjectEntries(t testing.TB, projectPath string, numEntries int) *memory.Store {
	t.Helper()
	dir := t.(*testing.T).TempDir()
	store, err := memory.NewStore(dir + "/test_memory.json")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < numEntries; i++ {
		cat := memory.CategoryTaskArtifact
		if i%2 == 0 {
			cat = memory.CategoryProjectKnowledge
		}
		entry := memory.Entry{
			Content:   fmt.Sprintf("Project entry %d for testing", i),
			Category:  cat,
			Scope:     memory.ScopeProject,
			Tags:      []string{projectPath, fmt.Sprintf("tag_%d", i)},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// --- Property 10: Archive entry correctness ---

func TestProperty10_ArchiveEntryCorrectness(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		projectPath := genProjectPath(rt, "path")
		projectName := genProjectName(rt, "name")
		numEntries := rapid.IntRange(1, 10).Draw(rt, "numEntries")

		// Create store with project entries.
		store := createStoreWithProjectEntries(t, projectPath, numEntries)
		defer store.Stop()

		// Create ProjectIndex and index the entries.
		projIndex := memory.NewProjectIndex()
		store.RLock()
		projIndex.Rebuild(store.Entries())
		store.RUnlock()

		// Create mock LLM caller.
		llmCaller := &mockArchiveLLMCaller{}

		// Create ArchiveService and archive.
		svc := NewArchiveService(store, llmCaller, projIndex)
		result, err := svc.Archive(context.Background(), ArchiveRequest{
			ProjectPath: projectPath,
			ProjectName: projectName,
		})

		// Archive must succeed.
		if err != nil {
			rt.Fatalf("Archive failed: %v", err)
		}
		if !result.Archived {
			rt.Fatal("Archive result.Archived should be true")
		}
		if !result.ExperienceExtracted {
			rt.Fatal("Archive result.ExperienceExtracted should be true (LLM available)")
		}

		// Property: After archiving, a project_knowledge entry with ScopeGlobal
		// and "archived_experience" tag exists in the memory store.
		store.RLock()
		entries := store.Entries()
		store.RUnlock()

		found := false
		for _, e := range entries {
			if e.Category != memory.CategoryProjectKnowledge {
				continue
			}
			if e.Scope != memory.ScopeGlobal {
				continue
			}
			hasArchivedTag := false
			hasProjectPathTag := false
			for _, tag := range e.Tags {
				if tag == "archived_experience" {
					hasArchivedTag = true
				}
				if tag == projectPath {
					hasProjectPathTag = true
				}
			}
			if hasArchivedTag && hasProjectPathTag {
				found = true
				break
			}
		}

		if !found {
			rt.Fatalf("After archiving project %q, no project_knowledge entry with ScopeGlobal and tags [archived_experience, %s] found",
				projectPath, projectPath)
		}
	})
}

// TestProperty10_ArchiveWithoutLLM verifies graceful degradation:
// when LLM is unavailable, archive still marks the project but no experience entry is created.
func TestProperty10_ArchiveWithoutLLM(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		projectPath := genProjectPath(rt, "path")
		numEntries := rapid.IntRange(1, 5).Draw(rt, "numEntries")

		store := createStoreWithProjectEntries(t, projectPath, numEntries)
		defer store.Stop()

		projIndex := memory.NewProjectIndex()
		store.RLock()
		projIndex.Rebuild(store.Entries())
		store.RUnlock()

		// No LLM caller — graceful degradation.
		svc := NewArchiveService(store, nil, projIndex)
		result, err := svc.Archive(context.Background(), ArchiveRequest{
			ProjectPath: projectPath,
		})

		if err != nil {
			rt.Fatalf("Archive without LLM failed: %v", err)
		}
		if !result.Archived {
			rt.Fatal("Archive result.Archived should be true even without LLM")
		}
		if result.ExperienceExtracted {
			rt.Fatal("ExperienceExtracted should be false when LLM is nil")
		}

		// ProjectIndex should still mark as archived.
		if !projIndex.IsArchived(projectPath) {
			rt.Fatal("ProjectIndex.IsArchived should return true after archive without LLM")
		}
	})
}

// --- Property 11: Archived task hidden by default ---

func TestProperty11_ArchivedTaskHiddenByDefault(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		projectPath := genProjectPath(rt, "path")
		projectName := genProjectName(rt, "name")
		numEntries := rapid.IntRange(1, 5).Draw(rt, "numEntries")

		store := createStoreWithProjectEntries(t, projectPath, numEntries)
		defer store.Stop()

		// Create ProjectIndex and rebuild with entries.
		projIndex := memory.NewProjectIndex()
		store.RLock()
		projIndex.Rebuild(store.Entries())
		store.RUnlock()

		// Verify project appears in ListRecent before archiving.
		recentBefore := projIndex.ListRecent(50)
		foundBefore := false
		for _, rec := range recentBefore {
			if strings.EqualFold(
				strings.ReplaceAll(rec.ProjectPath, "\\", "/"),
				strings.ReplaceAll(projectPath, "\\", "/"),
			) {
				foundBefore = true
				break
			}
		}
		if !foundBefore {
			// Project might not be indexed if path doesn't look like a project path.
			// Skip this test case — the property only applies to indexed projects.
			return
		}

		// Archive the project.
		llmCaller := &mockArchiveLLMCaller{}
		svc := NewArchiveService(store, llmCaller, projIndex)
		result, err := svc.Archive(context.Background(), ArchiveRequest{
			ProjectPath: projectPath,
			ProjectName: projectName,
		})
		if err != nil {
			rt.Fatalf("Archive failed: %v", err)
		}
		if !result.Archived {
			rt.Fatal("Archive result.Archived should be true")
		}

		// Property: After archiving, the task is hidden from ListRecent.
		recentAfter := projIndex.ListRecent(50)
		for _, rec := range recentAfter {
			normalizedRec := strings.ToLower(strings.ReplaceAll(rec.ProjectPath, "\\", "/"))
			normalizedPath := strings.ToLower(strings.ReplaceAll(projectPath, "\\", "/"))
			if normalizedRec == normalizedPath {
				rt.Fatalf("Archived project %q should NOT appear in ListRecent, but it does", projectPath)
			}
		}

		// Property: After archiving, the task is findable via Search("归档").
		searchResults := projIndex.Search("归档", 50)
		foundInSearch := false
		for _, rec := range searchResults {
			normalizedRec := strings.ToLower(strings.ReplaceAll(rec.ProjectPath, "\\", "/"))
			normalizedPath := strings.ToLower(strings.ReplaceAll(projectPath, "\\", "/"))
			if normalizedRec == normalizedPath {
				foundInSearch = true
				break
			}
		}
		if !foundInSearch {
			// Also try "archived" keyword.
			searchResults = projIndex.Search("archived", 50)
			for _, rec := range searchResults {
				normalizedRec := strings.ToLower(strings.ReplaceAll(rec.ProjectPath, "\\", "/"))
				normalizedPath := strings.ToLower(strings.ReplaceAll(projectPath, "\\", "/"))
				if normalizedRec == normalizedPath {
					foundInSearch = true
					break
				}
			}
		}
		if !foundInSearch {
			rt.Fatalf("Archived project %q should be findable via Search(\"归档\") or Search(\"archived\"), but was not found", projectPath)
		}
	})
}

// TestProperty11_ConcurrentArchiveReturnsEarly verifies that calling Archive
// twice on the same project returns early on the second call.
func TestProperty11_ConcurrentArchiveReturnsEarly(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		projectPath := genProjectPath(rt, "path")
		numEntries := rapid.IntRange(1, 5).Draw(rt, "numEntries")

		store := createStoreWithProjectEntries(t, projectPath, numEntries)
		defer store.Stop()

		projIndex := memory.NewProjectIndex()
		store.RLock()
		projIndex.Rebuild(store.Entries())
		store.RUnlock()

		llmCaller := &mockArchiveLLMCaller{}
		svc := NewArchiveService(store, llmCaller, projIndex)

		// First archive.
		result1, err := svc.Archive(context.Background(), ArchiveRequest{
			ProjectPath: projectPath,
		})
		if err != nil {
			rt.Fatalf("First archive failed: %v", err)
		}
		if !result1.Archived {
			rt.Fatal("First archive should succeed")
		}

		// Record LLM call count after first archive.
		llmCaller.mu.Lock()
		callsAfterFirst := llmCaller.callCount
		llmCaller.mu.Unlock()

		// Second archive — should return early (already archived).
		result2, err := svc.Archive(context.Background(), ArchiveRequest{
			ProjectPath: projectPath,
		})
		if err != nil {
			rt.Fatalf("Second archive failed: %v", err)
		}
		if !result2.Archived {
			rt.Fatal("Second archive should still report Archived=true")
		}

		// Property: Second archive should NOT call LLM again.
		llmCaller.mu.Lock()
		callsAfterSecond := llmCaller.callCount
		llmCaller.mu.Unlock()

		if callsAfterSecond != callsAfterFirst {
			rt.Fatalf("Second archive should not call LLM (calls before=%d, after=%d)",
				callsAfterFirst, callsAfterSecond)
		}

		// Property: Second archive message indicates already archived.
		if !strings.Contains(result2.Message, "已归档") {
			rt.Fatalf("Second archive message should indicate already archived, got: %q", result2.Message)
		}
	})
}

// TestProperty10_IsArchivedAfterArchive verifies that ProjectIndex.IsArchived
// returns true after Archive() is called.
func TestProperty10_IsArchivedAfterArchive(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		projectPath := genProjectPath(rt, "path")
		numEntries := rapid.IntRange(0, 5).Draw(rt, "numEntries")

		store := createStoreWithProjectEntries(t, projectPath, numEntries)
		defer store.Stop()

		projIndex := memory.NewProjectIndex()
		store.RLock()
		projIndex.Rebuild(store.Entries())
		store.RUnlock()

		// Before archive: IsArchived should be false.
		if projIndex.IsArchived(projectPath) {
			rt.Fatal("IsArchived should be false before archiving")
		}

		llmCaller := &mockArchiveLLMCaller{}
		svc := NewArchiveService(store, llmCaller, projIndex)

		_, err := svc.Archive(context.Background(), ArchiveRequest{
			ProjectPath: projectPath,
		})
		if err != nil {
			rt.Fatalf("Archive failed: %v", err)
		}

		// Property: After Archive(), IsArchived returns true.
		if !projIndex.IsArchived(projectPath) {
			rt.Fatalf("ProjectIndex.IsArchived(%q) should return true after Archive()", projectPath)
		}
	})
}
