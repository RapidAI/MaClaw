package guiapp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// BR-20261120-BD8D23C968F7 — 任务继续时失败.
//
// A coding task keeps its transcript in the coding-agent resume record, not in
// the chat memory and not in the tab snapshot: the workbench runs the turns and
// the tab only renders them. The continuation guard only looked at the chat
// memory and the tab snapshot, so pressing 继续 on a coding task that had 41
// recorded turns was refused with "没有找到这个任务之前的对话". The guard is right
// to fail closed on a task with no history at all; it must not fail closed on a
// task whose history lives in the coding transcript.
func TestContinuationUsesCodingResumeTranscript(t *testing.T) {
	app := newProjectSearchTestApp(t)
	projectPath := filepath.Join(t.TempDir(), "coding-task")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	owner := projectSessionOwnerID(projectPath)
	req := AIAssistantSendRequest{RequestID: "req-continue", Text: "继续", Lang: "zh-Hans"}

	memory := agent.NewConversationMemory()
	defer memory.Stop()
	handler := &IMMessageHandler{memory: memory}

	// Nothing recorded anywhere: the guard still fails closed.
	app.restoreProjectAgentSession(handler, owner, projectPath)
	if !projectContinuationHistoryMissing(req, handler, owner, projectPath) {
		t.Fatal("continuation with no history anywhere must still be refused")
	}

	// The workbench records the task transcript the way a real coding turn does.
	recordCodingAgentResume(owner, projectPath, []agent.ConversationEntry{
		{Role: "user", Content: "修掉表格公式被竖线切断的问题"},
		{Role: "assistant", Content: "根因是单元格切分没保护公式里的竖线"},
	}, nil)
	if got := loadCodingAgentResume(owner, projectPath); len(got.History) == 0 {
		t.Fatal("coding resume transcript was not recorded")
	}

	// 繼續 now works: the guard sees the coding transcript through the restore.
	app.restoreProjectAgentSession(handler, owner, projectPath)
	if projectContinuationHistoryMissing(req, handler, owner, projectPath) {
		t.Fatal("continuation was blocked although the coding task has a transcript")
	}
	if entries := memory.Load(owner); len(entries) == 0 {
		t.Fatalf("coding transcript was not restored into the session: %#v", entries)
	}
}

// A coding transcript belonging to another project must not satisfy the guard:
// the fallback is scoped to the task it was recorded for.
func TestCodingResumeTranscriptDoesNotLeakAcrossProjects(t *testing.T) {
	app := newProjectSearchTestApp(t)
	root := t.TempDir()
	recorded := filepath.Join(root, "recorded-task")
	other := filepath.Join(root, "other-task")
	for _, dir := range []string{recorded, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	owner := projectSessionOwnerID(recorded)
	recordCodingAgentResume(owner, recorded, []agent.ConversationEntry{
		{Role: "user", Content: "另一件事"},
		{Role: "assistant", Content: "好的"},
	}, nil)

	memory := agent.NewConversationMemory()
	defer memory.Stop()
	handler := &IMMessageHandler{memory: memory}
	app.restoreProjectAgentSession(handler, projectSessionOwnerID(other), other)
	if got := memory.Load(projectSessionOwnerID(other)); len(got) != 0 {
		t.Fatalf("another task's coding transcript leaked in: %#v", got)
	}
}
