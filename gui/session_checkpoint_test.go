package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestSessionCheckpointer_SaveAndRecall(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	defer ms.Stop()

	cb := NewContextBridge()
	cp := NewSessionCheckpointer(ms, cb)
	if cp == nil {
		t.Fatal("NewSessionCheckpointer returned nil")
	}

	session := &RemoteSession{
		ID:          "sess_test_1",
		Tool:        "claude",
		ProjectPath: "/home/user/myproject",
		Status:      SessionExited,
		Summary: SessionSummary{
			CurrentTask:     "修复登录 bug",
			ProgressSummary: "已修改 auth.go，待测试",
		},
		Events: []ImportantEvent{
			{Type: "tool_use", Summary: "修改了 auth.go 的验证逻辑"},
			{Type: "command.execute", Summary: "go test ./..."},
		},
	}

	if err := cp.SaveCheckpoint(session); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	// Recall should find the checkpoint.
	result := cp.RecallCheckpoint("/home/user/myproject")
	if result == "" {
		t.Fatal("RecallCheckpoint returned empty string")
	}
	if !strings.Contains(result, "claude") {
		t.Error("checkpoint should contain tool name")
	}
	if !strings.Contains(result, "修复登录 bug") {
		t.Error("checkpoint should contain last task")
	}
	if !strings.Contains(result, "已修改 auth.go") {
		t.Error("checkpoint should contain progress summary")
	}
}

func TestSessionCheckpointer_RecallEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	defer ms.Stop()

	cp := NewSessionCheckpointer(ms, nil)
	result := cp.RecallCheckpoint("/nonexistent/project")
	if result != "" {
		t.Errorf("expected empty string for nonexistent project, got: %s", result)
	}
}

func TestSessionCheckpointer_BuildResumePrompt(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	defer ms.Stop()

	cp := NewSessionCheckpointer(ms, nil)

	session := &RemoteSession{
		ID:          "sess_test_2",
		Tool:        "codex",
		ProjectPath: "/home/user/webapp",
		Status:      SessionExited,
		Summary: SessionSummary{
			CurrentTask:     "添加用户注册功能",
			ProgressSummary: "已完成数据库模型，待实现 API",
		},
	}
	_ = cp.SaveCheckpoint(session)

	prompt := cp.BuildResumePrompt("/home/user/webapp")
	if prompt == "" {
		t.Fatal("BuildResumePrompt returned empty string")
	}
	if !strings.Contains(prompt, "Previous Session Progress") {
		t.Error("resume prompt should contain header")
	}
	if !strings.Contains(prompt, "添加用户注册功能") {
		t.Error("resume prompt should contain task info")
	}
}

func TestSessionCheckpointer_NilMemoryStore(t *testing.T) {
	cp := NewSessionCheckpointer(nil, nil)
	if cp != nil {
		t.Error("expected nil checkpointer when memory store is nil")
	}
}

func TestSessionCheckpointer_NilSession(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	defer ms.Stop()

	cp := NewSessionCheckpointer(ms, nil)
	if err := cp.SaveCheckpoint(nil); err != nil {
		t.Errorf("SaveCheckpoint(nil) should not error, got: %v", err)
	}
}

func TestSessionCheckpointer_WithContextBridge(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	defer ms.Stop()

	cb := NewContextBridge()
	// Add some file changes to the context bridge.
	cb.ExtractFromEvents("/home/user/project", []ImportantEvent{
		{
			SessionID: "sess_ctx_1",
			Type:      "file.create",
			Summary:   "models/user.go",
			CreatedAt: time.Now().Unix(),
		},
		{
			SessionID: "sess_ctx_1",
			Type:      "command.execute",
			Summary:   "go mod tidy",
			CreatedAt: time.Now().Unix(),
		},
	})

	cp := NewSessionCheckpointer(ms, cb)
	session := &RemoteSession{
		ID:          "sess_ctx_1",
		Tool:        "claude",
		ProjectPath: "/home/user/project",
		Status:      SessionExited,
		Summary: SessionSummary{
			CurrentTask: "初始化项目",
		},
	}
	if err := cp.SaveCheckpoint(session); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}

	result := cp.RecallCheckpoint("/home/user/project")
	if !strings.Contains(result, "models/user.go") {
		t.Error("checkpoint should contain file changes from context bridge")
	}
}

func TestSessionCheckpointer_BuildResumePromptForSlot(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	defer ms.Stop()

	cp := NewSessionCheckpointer(ms, nil)
	session := &RemoteSession{
		ID:          "sess_test_slot",
		Tool:        "codex",
		ProjectPath: "/home/user/webapp",
		Status:      SessionExited,
		Summary: SessionSummary{
			CurrentTask:     "添加用户注册功能",
			ProgressSummary: "已完成数据库模型，待实现 API",
		},
	}
	_ = cp.SaveCheckpoint(session)

	slot := &agent.UnfinishedTaskSlot{ProjectPath: "/home/user/webapp"}
	prompt := cp.BuildResumePromptForSlot(slot)
	if prompt == "" {
		t.Fatal("BuildResumePromptForSlot returned empty string")
	}
	if !strings.Contains(prompt, "添加用户注册功能") {
		t.Fatal("slot resume prompt should contain task info")
	}
}

func TestMemoryStore_RecallForProject(t *testing.T) {
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	defer ms.Stop()

	// Save entries for different projects.
	_ = ms.Save(memory.Entry{
		Content:  "项目A的配置信息",
		Category: memory.CategoryProjectKnowledge,
		Tags:     []string{"/home/user/projectA"},
	})
	_ = ms.Save(memory.Entry{
		Content:  "项目B的配置信息",
		Category: memory.CategoryProjectKnowledge,
		Tags:     []string{"/home/user/projectB"},
	})

	// RecallForProject should boost entries matching the project path.
	results := ms.RecallForProject("配置", "/home/user/projectA")
	if len(results) == 0 {
		t.Fatal("RecallForProject returned no results")
	}
	// The first non-user_fact result should be projectA's entry.
	if !strings.Contains(results[0].Content, "项目A") {
		t.Errorf("expected projectA entry first, got: %s", results[0].Content)
	}
}

func TestTaskOrchestrator2_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	planPath := filepath.Join(tmpDir, "plans.json")

	o := NewTaskOrchestrator2WithPersist(nil, nil, nil, planPath)
	plan, err := o.CreatePlan("persistent plan", []PlanSubTask{
		{Description: "task 1", Tool: "claude"},
		{Description: "task 2", Tool: "codex"},
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Verify file was created.
	if _, err := os.Stat(planPath); os.IsNotExist(err) {
		t.Fatal("plan file was not created")
	}

	// Create a new orchestrator and verify it loads the plan.
	o2 := NewTaskOrchestrator2WithPersist(nil, nil, nil, planPath)
	got, err := o2.GetStatus(plan.ID)
	if err != nil {
		t.Fatalf("GetStatus after reload: %v", err)
	}
	if got.Description != "persistent plan" {
		t.Errorf("description = %s, want 'persistent plan'", got.Description)
	}
	if len(got.SubTasks) != 2 {
		t.Errorf("subtasks = %d, want 2", len(got.SubTasks))
	}
}

func TestTaskOrchestrator2_Resume(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	plan, _ := o.CreatePlan("resume test", []PlanSubTask{
		{ID: "a", Description: "first"},
		{ID: "b", Description: "second", DependsOn: []string{"a"}},
	})

	// Simulate: first task completed, second was running (interrupted).
	o.mu.Lock()
	plan.SubTasks[0].Status = "completed"
	plan.SubTasks[1].Status = "running"
	plan.Status = "failed"
	o.mu.Unlock()

	// Resume resets "running" subtasks to "pending" before calling Execute.
	// With nil manager, Execute will fail on the pending subtask, but we
	// can verify the status reset happened.
	_ = o.Resume(plan.ID)

	got, _ := o.GetStatus(plan.ID)
	// First task should still be completed.
	if got.SubTasks[0].Status != "completed" {
		t.Errorf("subtask[0] status = %s, want completed", got.SubTasks[0].Status)
	}
	// Second task should have been attempted (failed due to nil manager).
	if got.SubTasks[1].Status != "failed" {
		t.Errorf("subtask[1] status = %s, want failed (nil manager)", got.SubTasks[1].Status)
	}
}

func TestTaskOrchestrator2_ListResumable(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	plan, _ := o.CreatePlan("resumable", []PlanSubTask{
		{ID: "a", Description: "first"},
	})

	// Mark as failed with pending subtask.
	o.mu.Lock()
	o.plans[plan.ID].Status = "failed"
	o.mu.Unlock()

	resumable := o.ListResumable()
	if len(resumable) != 1 {
		t.Errorf("expected 1 resumable plan, got %d", len(resumable))
	}
}

func TestTaskOrchestrator2_Resume_NotFound(t *testing.T) {
	o := NewTaskOrchestrator2(nil, nil, nil)
	err := o.Resume("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent plan")
	}
}

// fakeRemoteSession creates a minimal RemoteSession for testing.
func fakeRemoteSession(id, tool, project string) *RemoteSession {
	return &RemoteSession{
		mu:          sync.RWMutex{},
		ID:          id,
		Tool:        tool,
		ProjectPath: project,
		Status:      SessionExited,
		Summary: SessionSummary{
			CurrentTask:     "test task",
			ProgressSummary: "test progress",
		},
	}
}
