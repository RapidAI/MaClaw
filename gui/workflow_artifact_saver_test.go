package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func newTestMemoryStore(t *testing.T) *memory.Store {
	t.Helper()
	tmpDir := t.TempDir()
	memPath := filepath.Join(tmpDir, "memories.json")
	ms, err := memory.NewStore(memPath)
	if err != nil {
		t.Fatalf("failed to create memory store: %v", err)
	}
	t.Cleanup(func() { ms.Stop() })
	return ms
}

func TestArtifactSaver_SavesTaskArtifact(t *testing.T) {
	ms := newTestMemoryStore(t)
	saver := &workflowArtifactSaver{store: ms}

	content := "# 需求文档\n\n## 功能需求\n1. 贪吃蛇游戏\n2. 方向键控制\n3. 计分系统"
	tags := []string{"workflow", "requirements", "coding"}

	err := saver.SaveArtifact("", content, tags, "")
	if err != nil {
		t.Fatalf("SaveArtifact failed: %v", err)
	}

	// Verify the entry was saved with correct category.
	entries := ms.List(memory.CategoryTaskArtifact, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 task_artifact entry, got %d", len(entries))
	}
	if entries[0].Category != memory.CategoryTaskArtifact {
		t.Errorf("expected category task_artifact, got %s", entries[0].Category)
	}
	if !strings.Contains(entries[0].Content, "贪吃蛇游戏") {
		t.Errorf("expected content to contain '贪吃蛇游戏', got: %s", entries[0].Content)
	}
	if entries[0].Scope != memory.ScopeProject {
		t.Errorf("expected scope project, got %s", entries[0].Scope)
	}
}

func TestArtifactSaver_DeduplicatesByContentHash(t *testing.T) {
	ms := newTestMemoryStore(t)
	saver := &workflowArtifactSaver{store: ms}

	content := "# 技术设计文档\n\n## 架构设计\nMVC 模式"
	tags := []string{"workflow", "tech_design", "coding"}

	// Save twice with identical content.
	_ = saver.SaveArtifact("", content, tags, "")
	_ = saver.SaveArtifact("", content, tags, "")

	entries := ms.List(memory.CategoryTaskArtifact, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d", len(entries))
	}
}

func TestArtifactSaver_UpdatesExistingByPhaseTag(t *testing.T) {
	ms := newTestMemoryStore(t)
	saver := &workflowArtifactSaver{store: ms}

	// Save initial version.
	_ = saver.SaveArtifact("", "需求 v1: 基本功能", []string{"workflow", "requirements", "coding"}, "")

	// Save updated version with same phase tag.
	_ = saver.SaveArtifact("", "需求 v2: 基本功能 + 排行榜", []string{"workflow", "requirements", "coding"}, "")

	entries := ms.List(memory.CategoryTaskArtifact, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after update, got %d", len(entries))
	}
	if !strings.Contains(entries[0].Content, "排行榜") {
		t.Errorf("expected updated content with '排行榜', got: %s", entries[0].Content)
	}
}

func TestArtifactSaver_SkipsEmptyContent(t *testing.T) {
	ms := newTestMemoryStore(t)
	saver := &workflowArtifactSaver{store: ms}

	err := saver.SaveArtifact("", "", []string{"workflow"}, "")
	if err != nil {
		t.Fatalf("SaveArtifact with empty content should not error: %v", err)
	}

	entries := ms.List(memory.CategoryTaskArtifact, "")
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for empty content, got %d", len(entries))
	}
}

func TestArtifactSaver_NilStoreNoError(t *testing.T) {
	saver := &workflowArtifactSaver{store: nil}
	err := saver.SaveArtifact("", "some content", []string{"workflow"}, "")
	if err != nil {
		t.Fatalf("SaveArtifact with nil store should not error: %v", err)
	}
}

func TestArtifactSaver_ProactiveRecallIncludesTaskArtifact(t *testing.T) {
	// Verify that task_artifact entries are returned by RecallDynamic
	// (they should NOT be filtered like session_checkpoint).
	ms := newTestMemoryStore(t)
	saver := &workflowArtifactSaver{store: ms}

	_ = saver.SaveArtifact("", "贪吃蛇游戏需求文档: 方向键控制蛇的移动, 吃到食物增长, 碰到墙壁或自身游戏结束",
		[]string{"workflow", "requirements", "coding"}, "")

	// RecallDynamic should return the task_artifact.
	results := ms.RecallDynamic("贪吃蛇游戏", "", "")
	found := false
	for _, e := range results {
		if e.Category == memory.CategoryTaskArtifact {
			found = true
			break
		}
	}
	if !found {
		t.Error("RecallDynamic should return task_artifact entries, but none found")
		for _, e := range results {
			t.Logf("  recalled: [%s] %s", e.Category, e.Content[:min(50, len(e.Content))])
		}
	}
}

func TestArtifactSaver_SavePhaseOutputIntegration(t *testing.T) {
	// Integration test: verify that SavePhaseOutput in WorkflowEngine
	// triggers artifact saving when ArtifactSaver is wired.
	// This test uses the workflow engine directly.

	// We can't easily test the full WorkflowEngine here without setting up
	// the entire registry/store/etc. Instead, verify the ArtifactSaver
	// interface contract is correct by testing the adapter directly.
	ms := newTestMemoryStore(t)
	saver := &workflowArtifactSaver{store: ms}

	// Simulate what SavePhaseOutput does: truncate to 800 runes and save.
	content := strings.Repeat("这是一段很长的需求文档内容。", 100) // ~1200 chars
	runes := []rune(content)
	if len(runes) > 800 {
		content = string(runes[:800]) + "\n…(摘要截断)"
	}

	tags := []string{"workflow", "requirements", "coding"}
	err := saver.SaveArtifact("", content, tags, "/path/to/full/output.md")
	if err != nil {
		t.Fatalf("SaveArtifact failed: %v", err)
	}

	entries := ms.List(memory.CategoryTaskArtifact, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].SourceURL != "/path/to/full/output.md" {
		t.Errorf("expected SourceURL to be set, got: %s", entries[0].SourceURL)
	}
	if entries[0].SourceType != "workflow_output" {
		t.Errorf("expected SourceType 'workflow_output', got: %s", entries[0].SourceType)
	}
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Ensure the test file compiles even without the full App context.
var _ = os.DevNull
