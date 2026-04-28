package agent

import (
	"fmt"
	"testing"
	"time"
)

// mockLLMClassifier is a test double for TaskLLMClassifier.
type mockLLMClassifier struct {
	response string
	err      error
}

func (m *mockLLMClassifier) Classify(systemPrompt, userMessage string, timeoutSec int) (string, error) {
	return m.response, m.err
}

func TestResolve_ExplicitNewTask(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage:     "帮我搜索最新的AI论文",
		History:         []ConversationEntry{{Role: "user", Content: "之前的任务"}},
		ExplicitNewTask: true,
	})
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew, got %s", d.Action)
	}
	if d.Source != "explicit" {
		t.Fatalf("expected source=explicit, got %s", d.Source)
	}
}

func TestResolve_PendingAskUser(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage:       "好的，用这个方案",
		History:           []ConversationEntry{{Role: "user", Content: "之前的任务"}},
		HasPendingAskUser: true,
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue, got %s", d.Action)
	}
}

func TestResolve_ConfirmedResume(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage:       "enhanced instruction text",
		History:           []ConversationEntry{{Role: "user", Content: "之前的任务"}},
		IsConfirmedResume: true,
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue, got %s", d.Action)
	}
}

func TestResolve_EmptyHistory_IsNewTask(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "帮我搜索最新的AI论文",
		History:     nil,
	})
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew, got %s", d.Action)
	}
	if d.Source != "structural" {
		t.Fatalf("expected source=structural, got %s", d.Source)
	}
}

func TestResolve_EmptyHistory_ShortMsg_StillNewTask(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	// Short message with empty history is still a new task — the memory
	// bridge directive is handled in the system prompt layer, not here.
	d := mgr.Resolve(ResolveInput{
		UserMessage: "开工",
		History:     nil,
	})
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew for short msg with empty history, got %s", d.Action)
	}
}

func TestResolve_ShortMessage_IsContinue(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "好的",
		History:     []ConversationEntry{{Role: "user", Content: "之前的任务"}},
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue for short message, got %s", d.Action)
	}
}

func TestResolve_ActiveConversation_NoLLM_IsContinue(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "生成方便AI工具访问的markdown版本",
		History:     []ConversationEntry{{Role: "user", Content: "安装skill并评估"}},
		LastAccess:  time.Now().Add(-30 * time.Second), // 30 seconds ago
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue for active conversation without LLM, got %s", d.Action)
	}
}

func TestResolve_LLM_Continue(t *testing.T) {
	llm := &mockLLMClassifier{response: "continue"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "把上面的内容转成markdown",
		History: []ConversationEntry{
			{Role: "user", Content: "帮我安装并评估所有skill"},
			{Role: "assistant", Content: "已完成skill评估，以下是报告..."},
		},
		LastAccess: time.Now().Add(-10 * time.Minute), // 10 minutes ago
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue, got %s", d.Action)
	}
}

func TestResolve_LLM_New(t *testing.T) {
	llm := &mockLLMClassifier{response: "new"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "帮我搜索最新的AI论文",
		History: []ConversationEntry{
			{Role: "user", Content: "帮我安装并评估所有skill"},
			{Role: "assistant", Content: "已完成skill评估"},
		},
		LastAccess: time.Now().Add(-10 * time.Minute),
	})
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew, got %s", d.Action)
	}
}

func TestResolve_LLM_Recall(t *testing.T) {
	llm := &mockLLMClassifier{response: "recall:task-123"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "继续处理上次那个部署的事",
		History: []ConversationEntry{
			{Role: "user", Content: "当前任务"},
		},
		ArchivedTasks: []ArchivedTask{
			{ID: "task-123", Summary: "部署到生产环境", Status: "abandoned"},
		},
		LastAccess: time.Now().Add(-10 * time.Minute),
	})
	if d.Action != TaskRecall {
		t.Fatalf("expected TaskRecall, got %s", d.Action)
	}
	if d.RecallTaskID != "task-123" {
		t.Fatalf("expected RecallTaskID=task-123, got %s", d.RecallTaskID)
	}
}

func TestResolve_LLM_RecallInvalidID(t *testing.T) {
	llm := &mockLLMClassifier{response: "recall:nonexistent"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "继续处理上次那个部署的事",
		History: []ConversationEntry{
			{Role: "user", Content: "当前任务"},
		},
		ArchivedTasks: []ArchivedTask{
			{ID: "task-123", Summary: "部署到生产环境", Status: "abandoned"},
		},
		LastAccess: time.Now().Add(-10 * time.Minute),
	})
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew for invalid recall ID, got %s", d.Action)
	}
}

func TestResolve_LLM_Failure_FallbackToContinue(t *testing.T) {
	llm := &mockLLMClassifier{err: fmt.Errorf("timeout")}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "生成markdown版本",
		History: []ConversationEntry{
			{Role: "user", Content: "安装skill"},
			{Role: "assistant", Content: "已完成"},
		},
		LastAccess: time.Now().Add(-10 * time.Minute),
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue on LLM failure, got %s", d.Action)
	}
	if d.Source != "fallback" {
		t.Fatalf("expected source=fallback, got %s", d.Source)
	}
}

func TestResolve_ActiveConversation_LLM_AmbiguousNew_FallbackToContinue(t *testing.T) {
	// When conversation is active, TCM returns continue structurally
	// without calling LLM at all.
	llm := &mockLLMClassifier{response: "new"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "生成markdown版本",
		History: []ConversationEntry{
			{Role: "user", Content: "安装skill"},
			{Role: "assistant", Content: "已完成"},
		},
		LastAccess: time.Now().Add(-30 * time.Second), // very recent
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue for active conversation, got %s", d.Action)
	}
	if d.Source != "structural" {
		t.Fatalf("expected source=structural, got %s", d.Source)
	}
}


func TestResolve_IncompleteTaskMarker_IsNewTask(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "帮我搜索最新的AI论文",
		History: []ConversationEntry{
			{Role: "user", Content: "之前的任务"},
			{Role: "assistant", Content: "(已达到最大推理轮次，请继续发送消息以完成任务)"},
		},
		HasIncompleteTaskMarker: true,
		LastAccess:              time.Now().Add(-30 * time.Second), // even if active
	})
	// Incomplete task marker overrides active conversation protection.
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew for incomplete task marker, got %s", d.Action)
	}
	if d.Source != "structural" {
		t.Fatalf("expected source=structural, got %s", d.Source)
	}
}
