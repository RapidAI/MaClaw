package agent

import (
	"path/filepath"
	"testing"
)

func TestTaskArchive_ArchiveAndList(t *testing.T) {
	ta := NewTaskArchive("", 3) // in-memory only
	ta.Archive(ArchivedTask{ID: "t1", UserID: "u1", Summary: "task 1", Status: "completed"})
	ta.Archive(ArchivedTask{ID: "t2", UserID: "u1", Summary: "task 2", Status: "switched"})

	tasks := ta.List("u1")
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	// Newest first.
	if tasks[0].ID != "t2" {
		t.Fatalf("expected t2 first, got %s", tasks[0].ID)
	}
}

func TestTaskArchive_Eviction(t *testing.T) {
	ta := NewTaskArchive("", 2) // max 2
	ta.Archive(ArchivedTask{ID: "t1", UserID: "u1", Summary: "task 1"})
	ta.Archive(ArchivedTask{ID: "t2", UserID: "u1", Summary: "task 2"})
	ta.Archive(ArchivedTask{ID: "t3", UserID: "u1", Summary: "task 3"})

	tasks := ta.List("u1")
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks after eviction, got %d", len(tasks))
	}
	// t1 should be evicted (oldest).
	for _, task := range tasks {
		if task.ID == "t1" {
			t.Fatal("t1 should have been evicted")
		}
	}
}

func TestTaskArchive_Dedup(t *testing.T) {
	ta := NewTaskArchive("", 5)
	ta.Archive(ArchivedTask{ID: "t1", UserID: "u1", Summary: "v1"})
	ta.Archive(ArchivedTask{ID: "t1", UserID: "u1", Summary: "v2"})

	tasks := ta.List("u1")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after dedup, got %d", len(tasks))
	}
	if tasks[0].Summary != "v2" {
		t.Fatalf("expected updated summary v2, got %s", tasks[0].Summary)
	}
}

func TestTaskArchive_Get(t *testing.T) {
	ta := NewTaskArchive("", 5)
	ta.Archive(ArchivedTask{ID: "t1", UserID: "u1", Summary: "task 1"})

	task, ok := ta.Get("u1", "t1")
	if !ok {
		t.Fatal("expected to find t1")
	}
	if task.Summary != "task 1" {
		t.Fatalf("expected summary 'task 1', got %s", task.Summary)
	}

	_, ok = ta.Get("u1", "nonexistent")
	if ok {
		t.Fatal("expected not to find nonexistent task")
	}
}

func TestTaskArchive_Remove(t *testing.T) {
	ta := NewTaskArchive("", 5)
	ta.Archive(ArchivedTask{ID: "t1", UserID: "u1", Summary: "task 1"})
	ta.Archive(ArchivedTask{ID: "t2", UserID: "u1", Summary: "task 2"})

	ta.Remove("u1", "t1")
	tasks := ta.List("u1")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after remove, got %d", len(tasks))
	}
	if tasks[0].ID != "t2" {
		t.Fatalf("expected t2 remaining, got %s", tasks[0].ID)
	}
}

func TestTaskArchive_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.json")

	// Write.
	ta1 := NewTaskArchive(path, 5)
	ta1.Archive(ArchivedTask{ID: "t1", UserID: "u1", Summary: "persisted task"})

	// Read back.
	ta2 := NewTaskArchive(path, 5)
	tasks := ta2.List("u1")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after reload, got %d", len(tasks))
	}
	if tasks[0].Summary != "persisted task" {
		t.Fatalf("expected 'persisted task', got %s", tasks[0].Summary)
	}
}

func TestTaskArchive_UserIsolation(t *testing.T) {
	ta := NewTaskArchive("", 5)
	ta.Archive(ArchivedTask{ID: "t1", UserID: "u1", Summary: "user1 task"})
	ta.Archive(ArchivedTask{ID: "t2", UserID: "u2", Summary: "user2 task"})

	if len(ta.List("u1")) != 1 {
		t.Fatal("u1 should have 1 task")
	}
	if len(ta.List("u2")) != 1 {
		t.Fatal("u2 should have 1 task")
	}
	if len(ta.List("u3")) != 0 {
		t.Fatal("u3 should have 0 tasks")
	}
}

func TestBuildArchivedTask(t *testing.T) {
	history := []ConversationEntry{
		{Role: "user", Content: "帮我安装并评估所有 Skill"},
		{Role: "assistant", Content: "好的，我来安装..."},
		{Role: "user", Content: "继续"},
		{Role: "assistant", Content: "已完成评估，报告如下：\n文件保存到 /tmp/report.pdf"},
	}
	task := BuildArchivedTask("u1", history, "completed", "/workspace")

	if task.UserID != "u1" {
		t.Fatalf("expected userID u1, got %s", task.UserID)
	}
	if task.Status != "completed" {
		t.Fatalf("expected status completed, got %s", task.Status)
	}
	if task.LastRequest == "" {
		t.Fatal("expected non-empty LastRequest")
	}
	if len(task.CompressedHistory) == 0 {
		t.Fatal("expected non-empty CompressedHistory")
	}
	if task.ProjectPath != "/workspace" {
		t.Fatalf("expected project path /workspace, got %s", task.ProjectPath)
	}
}
