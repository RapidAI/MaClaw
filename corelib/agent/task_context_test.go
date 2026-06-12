package agent

import (
	"fmt"
	"testing"
	"time"
)

// mockLLMClassifier is a test double for TaskLLMClassifier.
type mockLLMClassifier struct {
	response   string
	err        error
	calls      int
	timeoutSec int
}

func (m *mockLLMClassifier) Classify(systemPrompt, userMessage string, timeoutSec int) (string, error) {
	m.calls++
	m.timeoutSec = timeoutSec
	return m.response, m.err
}

func TestDefaultTaskContextConfig_LLMTimeoutAllowsRemoteClassifier(t *testing.T) {
	cfg := DefaultTaskContextConfig()
	if cfg.LLMTimeout < 30*time.Second {
		t.Fatalf("expected LLMTimeout >= 30s, got %s", cfg.LLMTimeout)
	}
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

func TestResolve_ActiveUnderstandingSession_IsContinue(t *testing.T) {
	llm := &mockLLMClassifier{response: "new"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage:                   "抗战胜利，中国民众，激昂",
		History:                       []ConversationEntry{{Role: "user", Content: "查看api服务器状态"}},
		HasActiveUnderstandingSession: true,
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue for active understanding session, got %s", d.Action)
	}
	if d.Source != "explicit" {
		t.Fatalf("expected source=explicit, got %s", d.Source)
	}
	if llm.calls != 0 {
		t.Fatalf("active understanding should bypass LLM classifier, got %d calls", llm.calls)
	}
}

func TestResolve_ActiveBackgroundTask_IsContinue(t *testing.T) {
	llm := &mockLLMClassifier{response: "new"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage:             "please also group these PII types",
		History:                 []ConversationEntry{{Role: "user", Content: "run GPU84 eval"}, {Role: "assistant", Content: "background task is running"}},
		HasActiveBackgroundTask: true,
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue for active background task, got %s", d.Action)
	}
	if d.Source != "runtime" {
		t.Fatalf("expected source=runtime, got %s", d.Source)
	}
	if llm.calls != 0 {
		t.Fatalf("active background task should bypass LLM classifier, got %d calls", llm.calls)
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

func TestResolve_ShortHistory_UsesLLM(t *testing.T) {
	llm := &mockLLMClassifier{response: "continue"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "帮我搜索论文",
		History: []ConversationEntry{
			{Role: "user", Content: "之前的任务"},
			{Role: "assistant", Content: "好的"},
		},
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue from classifier for short history, got %s", d.Action)
	}
	if d.Source != "llm" {
		t.Fatalf("expected source=llm, got %s", d.Source)
	}
	if llm.calls != 1 {
		t.Fatalf("expected LLM to be called for short history, got %d calls", llm.calls)
	}
}

func TestResolve_ExactlyFiveEntries_UsesLLM(t *testing.T) {
	llm := &mockLLMClassifier{response: "continue"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	// With exactly 5 entries, the LLM should be called.
	d := mgr.Resolve(ResolveInput{
		UserMessage: "继续",
		History: []ConversationEntry{
			{Role: "user", Content: "任务1"},
			{Role: "assistant", Content: "好的"},
			{Role: "user", Content: "继续"},
			{Role: "assistant", Content: "完成"},
			{Role: "user", Content: "再来"},
		},
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue, got %s", d.Action)
	}
	if llm.calls != 1 {
		t.Fatalf("expected LLM to be called for 5 entries, got %d calls", llm.calls)
	}
}

func TestResolve_ShortMessage_IsContinue(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "好的",
		History: []ConversationEntry{
			{Role: "user", Content: "之前的任务"},
			{Role: "assistant", Content: "好的"},
			{Role: "user", Content: "继续"},
			{Role: "assistant", Content: "完成了"},
			{Role: "user", Content: "再来"},
		},
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue without semantic classifier, got %s", d.Action)
	}
}

func TestResolve_ShortMessageWithClassifierUsesLLM(t *testing.T) {
	llm := &mockLLMClassifier{response: "new"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "天气",
		History: []ConversationEntry{
			{Role: "user", Content: "之前的任务"},
			{Role: "assistant", Content: "好的"},
			{Role: "user", Content: "继续"},
			{Role: "assistant", Content: "完成了"},
			{Role: "user", Content: "再来"},
		},
		LastAccess: time.Now().Add(-30 * time.Second),
	})
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew from classifier for short message, got %s", d.Action)
	}
	if d.Source != "llm" {
		t.Fatalf("expected source=llm, got %s", d.Source)
	}
	if llm.calls != 1 {
		t.Fatalf("expected classifier to be called once, got %d", llm.calls)
	}
}
func TestResolve_ActiveConversation_NoLLM_IsContinue(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "生成方便AI工具访问的markdown版本",
		History: []ConversationEntry{
			{Role: "user", Content: "安装skill并评估"},
			{Role: "assistant", Content: "好的，开始安装"},
			{Role: "user", Content: "继续"},
			{Role: "assistant", Content: "安装完成"},
			{Role: "user", Content: "评估结果呢"},
		},
		LastAccess: time.Now().Add(-30 * time.Second), // 30 seconds ago
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue for active conversation without LLM, got %s", d.Action)
	}
}

func TestResolve_ActiveConversation_NoLLM_FollowUpFallsBackToContinue(t *testing.T) {
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), nil)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "go on",
		History: []ConversationEntry{
			{Role: "user", Content: "install skills"},
			{Role: "assistant", Content: "working"},
			{Role: "user", Content: "continue"},
			{Role: "assistant", Content: "still running"},
			{Role: "user", Content: "status"},
		},
		LastAccess: time.Now().Add(-30 * time.Second),
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue without semantic classifier, got %s", d.Action)
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
			{Role: "user", Content: "好的"},
			{Role: "assistant", Content: "还需要什么帮助吗？"},
			{Role: "user", Content: "再看看"},
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
			{Role: "user", Content: "好的"},
			{Role: "assistant", Content: "还需要什么？"},
			{Role: "user", Content: "看看"},
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
			{Role: "assistant", Content: "好的"},
			{Role: "user", Content: "继续"},
			{Role: "assistant", Content: "完成了"},
			{Role: "user", Content: "再来"},
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
			{Role: "assistant", Content: "好的"},
			{Role: "user", Content: "继续"},
			{Role: "assistant", Content: "完成了"},
			{Role: "user", Content: "再来"},
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

func TestResolve_LLM_InvalidResponseFallsBackToContinue(t *testing.T) {
	llm := &mockLLMClassifier{response: "let me think about it"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "review/fix/optimize",
		History: []ConversationEntry{
			{Role: "user", Content: "old task"},
			{Role: "assistant", Content: "old answer"},
		},
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue for invalid classifier response, got %s", d.Action)
	}
	if d.Source != "llm" {
		t.Fatalf("expected source=llm, got %s", d.Source)
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
			{Role: "user", Content: "好的"},
			{Role: "assistant", Content: "还需要什么？"},
			{Role: "user", Content: "看看"},
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

func TestResolve_LLM_UsesConfiguredTimeout(t *testing.T) {
	llm := &mockLLMClassifier{response: "new"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	_ = mgr.Resolve(ResolveInput{
		UserMessage: "帮我搜索最新的AI论文",
		History: []ConversationEntry{
			{Role: "user", Content: "之前的任务"},
			{Role: "assistant", Content: "完成了"},
		},
	})
	if llm.timeoutSec < 30 {
		t.Fatalf("expected classifier timeout >= 30s, got %d", llm.timeoutSec)
	}
}

func TestResolve_ActiveConversation_UsesLLMForFollowUp(t *testing.T) {
	llm := &mockLLMClassifier{response: "continue"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "上面的内容生成markdown版本",
		History: []ConversationEntry{
			{Role: "user", Content: "安装 skill"},
			{Role: "assistant", Content: "已完成"},
			{Role: "user", Content: "好的"},
			{Role: "assistant", Content: "还需要什么？"},
			{Role: "user", Content: "看看结果"},
		},
		LastAccess: time.Now().Add(-30 * time.Second), // very recent
	})
	if d.Action != TaskContinue {
		t.Fatalf("expected TaskContinue for classifier-approved follow-up, got %s", d.Action)
	}
	if d.Source != "llm" {
		t.Fatalf("expected source=llm, got %s", d.Source)
	}
	if llm.calls != 1 {
		t.Fatalf("expected classifier to be called once, got %d", llm.calls)
	}
}
func TestResolve_ActiveConversation_SubstantiveNewTaskUsesLLM(t *testing.T) {
	llm := &mockLLMClassifier{response: "new"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "帮我查一下北京天气",
		History: []ConversationEntry{
			{Role: "user", Content: "帮我整理 Hugging Face Agent 论文并生成 PDF"},
			{Role: "assistant", Content: "PDF 已经生成并发送，重点论文包括 GLM-5V-Turbo。"},
			{Role: "user", Content: "好的"},
			{Role: "assistant", Content: "还需要什么帮助吗？"},
			{Role: "user", Content: "看看"},
		},
		LastAccess: time.Now().Add(-30 * time.Second),
	})
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew for unrelated active message, got %s", d.Action)
	}
	if d.Source != "llm" {
		t.Fatalf("expected source=llm, got %s", d.Source)
	}
}
func TestResolve_IncompleteTaskMarker_UsesLLM(t *testing.T) {
	llm := &mockLLMClassifier{response: "new"}
	mgr := NewTaskContextManager(DefaultTaskContextConfig(), llm)
	d := mgr.Resolve(ResolveInput{
		UserMessage: "帮我搜索最新的AI论文",
		History: []ConversationEntry{
			{Role: "user", Content: "之前的任务"},
			{Role: "assistant", Content: "(已达到最大推理轮次，请继续发送消息以完成任务)"},
			{Role: "user", Content: "好的"},
			{Role: "assistant", Content: "继续处理中..."},
			{Role: "user", Content: "看看进度"},
		},
		HasIncompleteTaskMarker: true,
		LastAccess:              time.Now().Add(-30 * time.Second), // even if active
	})
	if d.Action != TaskNew {
		t.Fatalf("expected TaskNew for classifier-identified new task, got %s", d.Action)
	}
	if d.Source != "llm" {
		t.Fatalf("expected source=llm, got %s", d.Source)
	}
	if llm.calls != 1 {
		t.Fatalf("expected classifier to be called once, got %d", llm.calls)
	}
}
