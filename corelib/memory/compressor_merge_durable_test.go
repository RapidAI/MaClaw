package memory

import (
	"context"
	"testing"
	"time"
)

type mergeEverythingChatLLM struct{}

func (mergeEverythingChatLLM) IsConfigured() bool { return true }
func (mergeEverythingChatLLM) ChatCall(messages []map[string]string) (string, error) {
	return `[{"keep":0,"remove":[1],"merged":"merged content"}]`, nil
}

// Durable task-management entries are 1:1 task identities, not compressible
// facts: merging them unions task-path tags and erases tasks from the
// sidebar. The compressor must leave them out of merge batches while still
// merging ordinary entries.
func TestMergeSemanticDuplicatesSkipsDurableTaskManagementEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir + "/memories.json")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()
	now := time.Now()
	save := func(e Entry) {
		if err := store.Save(e); err != nil {
			t.Fatalf("Save %s: %v", e.ID, err)
		}
	}
	save(Entry{ID: "durable-task-a", Title: "任务甲", Content: "# 任务甲\n\nCreated from task management.", Category: CategoryTaskArtifact, Tags: []string{"task_management", "C:/tasks/alpha"}, CreatedAt: now, UpdatedAt: now})
	save(Entry{ID: "durable-task-b", Title: "任务乙", Content: "# 任务乙\n\nCreated from task management.", Category: CategoryTaskArtifact, Tags: []string{"task_management", "C:/tasks/beta"}, CreatedAt: now, UpdatedAt: now})
	save(Entry{ID: "fact-a", Content: "用户喜欢深色主题", Category: CategoryPreference, CreatedAt: now, UpdatedAt: now})
	save(Entry{ID: "fact-b", Content: "用户偏好深色界面", Category: CategoryPreference, CreatedAt: now, UpdatedAt: now})

	compressor := NewCompressor(store, mergeEverythingChatLLM{}, nil)
	merged, err := compressor.mergeSemanticDuplicates(context.Background())
	if err != nil {
		t.Fatalf("mergeSemanticDuplicates: %v", err)
	}
	if merged != 1 {
		t.Fatalf("merged = %d, want 1 (only the preference pair)", merged)
	}
	tasks := store.List(CategoryTaskArtifact, "")
	if len(tasks) != 2 {
		t.Fatalf("expected both task identity entries to survive, got %d", len(tasks))
	}
	for _, e := range tasks {
		if len(e.Tags) != 2 {
			t.Fatalf("entry %s tags unioned: %v", e.ID, e.Tags)
		}
	}
}
