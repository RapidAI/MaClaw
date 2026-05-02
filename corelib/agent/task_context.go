package agent

// task_context.go — Unified task context management layer.
//
// TaskContextManager is the single decision point for "is this message a
// continuation of the current task, a new task, or a recall of a past task?"
//
// It replaces the scattered decision logic that was spread across:
//   - looksLikeFreshTaskRequest (keyword matching)
//   - shouldAutoClearIncompleteTaskContext (incomplete task marker)
//   - TopicSwitchDetector (BM25 + embedding + LLM voting)
//   - shouldRequireExecutionConfirmation (confirmation gate)
//
// All of these now defer to TaskContextManager.Resolve() for the primary
// decision, then apply their own logic only within the resolved action.

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// TaskAction is the resolved action for a new user message.
type TaskAction int

const (
	// TaskContinue means the message continues the current active task.
	// Conversation history is preserved. Confirmation gate is skipped.
	TaskContinue TaskAction = iota

	// TaskNew means the message starts a brand-new task.
	// Current task is archived. Conversation history is cleared.
	// Confirmation gate may apply.
	TaskNew

	// TaskRecall means the user wants to resume a specific past task.
	// The recalled task's context is restored into conversation history.
	TaskRecall
)

func (a TaskAction) String() string {
	switch a {
	case TaskContinue:
		return "continue"
	case TaskNew:
		return "new"
	case TaskRecall:
		return "recall"
	default:
		return fmt.Sprintf("unknown(%d)", int(a))
	}
}

// TaskContextDecision is the output of TaskContextManager.Resolve().
type TaskContextDecision struct {
	Action TaskAction

	// RecallTaskID is set when Action == TaskRecall. It identifies which
	// archived task to restore.
	RecallTaskID string

	// Reason is a short human-readable explanation for logging/debugging.
	Reason string

	// Source indicates which signal drove the decision (e.g. "structural",
	// "llm", "explicit").
	Source string
}

// ArchivedTask is a compact snapshot of a completed or abandoned task,
// stored for potential recall.
type ArchivedTask struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Summary     string    `json:"summary"`      // one-line task description
	LastRequest string    `json:"last_request"` // original user request text
	FilePaths   []string  `json:"file_paths"`   // key files involved
	ProjectPath string    `json:"project_path"` // workspace directory
	Status      string    `json:"status"`       // "completed", "abandoned", "interrupted"
	CreatedAt   time.Time `json:"created_at"`
	ArchivedAt  time.Time `json:"archived_at"`

	// CompressedHistory is a condensed version of the conversation that
	// can be injected back into the context when the task is recalled.
	// Kept short (~500-1000 tokens) to leave room for the new conversation.
	CompressedHistory []ConversationEntry `json:"compressed_history,omitempty"`
}

// TaskContextConfig holds tunable parameters for the TaskContextManager.
type TaskContextConfig struct {
	// MaxArchivedTasks is the maximum number of archived tasks to keep per user.
	MaxArchivedTasks int

	// ActiveConversationWindow is retained for config compatibility. Task
	// continuity is resolved by explicit state or the classifier, not by recency.
	ActiveConversationWindow time.Duration

	// LLMTimeout is the maximum time to wait for the LLM classification call.
	LLMTimeout time.Duration
}

// DefaultTaskContextConfig returns sensible defaults.
func DefaultTaskContextConfig() TaskContextConfig {
	return TaskContextConfig{
		MaxArchivedTasks:         10,
		ActiveConversationWindow: 5 * time.Minute,
		LLMTimeout:               8 * time.Second,
	}
}

// TaskLLMClassifier is the interface for making lightweight LLM calls.
// Implemented by the GUI layer's LLMClassify wrapper.
type TaskLLMClassifier interface {
	// Classify sends a system+user prompt to the LLM and returns the raw
	// response text. Returns error on timeout or LLM failure.
	Classify(systemPrompt, userMessage string, timeoutSec int) (string, error)
}

// TaskContextManager is the unified decision point for task switching.
type TaskContextManager struct {
	config TaskContextConfig
	llm    TaskLLMClassifier // nil = structural-only mode (no LLM calls)
}

// NewTaskContextManager creates a TaskContextManager.
func NewTaskContextManager(config TaskContextConfig, llm TaskLLMClassifier) *TaskContextManager {
	return &TaskContextManager{
		config: config,
		llm:    llm,
	}
}

// ResolveInput bundles the signals available at decision time.
type ResolveInput struct {
	// UserMessage is the new message from the user.
	UserMessage string

	// History is the current conversation history (may be empty).
	History []ConversationEntry

	// LastAccess is when the conversation was last active.
	LastAccess time.Time

	// ArchivedTasks are the user's recent archived tasks (for recall matching).
	ArchivedTasks []ArchivedTask

	// HasPendingAskUser is true if the previous assistant message was an
	// ask_user question — the current message is almost certainly a response.
	HasPendingAskUser bool

	// IsConfirmedResume is true if this message is a confirmation approval
	// (user clicked "确认" on the confirmation card). The message text has
	// been replaced with the enhanced instruction.
	IsConfirmedResume bool

	// HasActiveUnderstandingSession is true while a workflow clarification
	// session owns the user's next replies. In that state, the message is bound
	// to the active clarification unless the frontend explicitly starts a new
	// task.
	HasActiveUnderstandingSession bool

	// HasIncompleteTaskMarker is true if the conversation history contains
	// markers indicating the previous task was interrupted (max rounds
	// reached, session still running, etc.).
	HasIncompleteTaskMarker bool

	// ExplicitNewTask is true if the frontend explicitly flagged this as a new task.
	ExplicitNewTask bool
}

// Resolve determines whether the user's message continues the current task,
// starts a new task, or recalls a past task.
//
// Decision hierarchy (highest priority first):
//  1. Explicit bound-state signals (frontend flags, ask_user response, confirmed resume)
//  2. Structural signals that need no interpretation (empty history = new task)
//  3. LLM classification for every ambiguous message with history
//  4. Conservative fallback when the classifier is unavailable
func (m *TaskContextManager) Resolve(input ResolveInput) TaskContextDecision {
	trimmed := strings.TrimSpace(input.UserMessage)

	// --- Layer 1: Explicit signals (no ambiguity) ---

	if input.ExplicitNewTask {
		return TaskContextDecision{
			Action: TaskNew,
			Reason: "frontend explicitly flagged as new task",
			Source: "explicit",
		}
	}

	if input.HasPendingAskUser {
		return TaskContextDecision{
			Action: TaskContinue,
			Reason: "responding to ask_user question",
			Source: "explicit",
		}
	}

	if input.IsConfirmedResume {
		return TaskContextDecision{
			Action: TaskContinue,
			Reason: "user confirmed pending task execution",
			Source: "explicit",
		}
	}

	if input.HasActiveUnderstandingSession {
		return TaskContextDecision{
			Action: TaskContinue,
			Reason: "active workflow understanding session",
			Source: "explicit",
		}
	}

	// --- Layer 2: Structural signals ---

	// No history = definitely a new task.
	if len(input.History) == 0 {
		return TaskContextDecision{
			Action: TaskNew,
			Reason: "no conversation history",
			Source: "structural",
		}
	}

	if trimmed == "" {
		return TaskContextDecision{
			Action: TaskContinue,
			Reason: "empty message with existing history",
			Source: "structural",
		}
	}

	// --- Layer 3: LLM classification ---

	if m.llm != nil {
		return m.classifyWithLLM(input)
	}

	// --- Layer 4: Conservative fallback ---
	// History exists but no LLM available. Default to continue to avoid
	// the amnesia bug — losing context is worse than keeping stale context.
	return TaskContextDecision{
		Action: TaskContinue,
		Reason: "history exists, no LLM available, defaulting to continue",
		Source: "fallback",
	}
}

// classifyWithLLM uses a lightweight LLM call to determine the task action.
// It is the only path for ambiguous messages with existing history; recency
// and message length must not decide task continuity by themselves.
func (m *TaskContextManager) classifyWithLLM(input ResolveInput) TaskContextDecision {
	// Build a compact context summary for the LLM.
	currentTaskSummary := buildCurrentTaskSummary(input.History)
	archivedSummaries := buildArchivedTaskSummaries(input.ArchivedTasks)

	systemPrompt := taskContextClassifierPrompt

	var userMsg strings.Builder
	fmt.Fprintf(&userMsg, "当前任务摘要：%s\n", currentTaskSummary)
	if input.HasIncompleteTaskMarker {
		userMsg.WriteString("当前任务状态：上一轮可能未完成或被中断。若用户明确要求继续/恢复则判为 continue；若用户提出无关新请求则判为 new。\n")
	}
	if archivedSummaries != "" {
		fmt.Fprintf(&userMsg, "\n历史任务：\n%s\n", archivedSummaries)
	}
	fmt.Fprintf(&userMsg, "\n用户新消息：%s", strings.TrimSpace(input.UserMessage))

	resp, err := m.llm.Classify(systemPrompt, userMsg.String(), int(m.config.LLMTimeout.Seconds()))
	if err != nil {
		log.Printf("[TaskContext] LLM classification failed: %v, falling back to continue", err)
		return TaskContextDecision{
			Action: TaskContinue,
			Reason: fmt.Sprintf("LLM failed: %v, defaulting to continue", err),
			Source: "fallback",
		}
	}

	return m.parseLLMResponse(resp, input.ArchivedTasks)
}

// parseLLMResponse interprets the LLM's classification response.
func (m *TaskContextManager) parseLLMResponse(resp string, archived []ArchivedTask) TaskContextDecision {
	resp = strings.TrimSpace(strings.ToLower(resp))

	// Strip markdown fences if present.
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	// The LLM should respond with one of: "continue", "new", "recall:<id>"
	if strings.HasPrefix(resp, "recall:") {
		taskID := strings.TrimSpace(strings.TrimPrefix(resp, "recall:"))
		// Validate the task ID exists.
		for _, t := range archived {
			if t.ID == taskID {
				return TaskContextDecision{
					Action:       TaskRecall,
					RecallTaskID: taskID,
					Reason:       fmt.Sprintf("LLM identified recall of task %q", taskID),
					Source:       "llm",
				}
			}
		}
		log.Printf("[TaskContext] LLM returned recall:%s but task not found in archive, treating as new", taskID)
		return TaskContextDecision{
			Action: TaskNew,
			Reason: fmt.Sprintf("LLM said recall:%s but task not found", taskID),
			Source: "llm",
		}
	}

	if strings.HasPrefix(resp, "new") {
		return TaskContextDecision{
			Action: TaskNew,
			Reason: "LLM classified as new task",
			Source: "llm",
		}
	}

	// Default: continue (conservative).
	return TaskContextDecision{
		Action: TaskContinue,
		Reason: fmt.Sprintf("LLM response: %q, defaulting to continue", TruncateRunes(resp, 50)),
		Source: "llm",
	}
}

// --- Prompt ---

const taskContextClassifierPrompt = `你是一个任务上下文分类器。判断用户的新消息属于以下哪种情况：

1. continue — 接续当前任务（对当前任务的后续请求、修改、补充、格式转换等）
2. new — 开启一个全新的、与当前任务无关的任务
3. recall:<task_id> — 想要恢复/继续某个历史任务

判断规则：
- 如果新消息引用、修改、转换、补充当前任务的内容或结果，选 continue
- 如果新消息与当前任务完全无关，选 new
- 如果新消息提到要继续/恢复某个历史任务（不是当前任务），选 recall:<对应的task_id>
- 当不确定时，优先选 continue（保留上下文比丢失上下文代价更小）

只回答 continue、new 或 recall:<task_id>，不要解释。`

// --- Helper functions ---

// buildCurrentTaskSummary extracts a compact summary of the current conversation.
func buildCurrentTaskSummary(history []ConversationEntry) string {
	if len(history) == 0 {
		return "(无)"
	}

	// Extract the first user message (original task request).
	var firstUserMsg string
	for _, e := range history {
		if e.Role == "user" {
			if text, ok := e.Content.(string); ok && strings.TrimSpace(text) != "" {
				firstUserMsg = TruncateRunes(strings.TrimSpace(text), 150)
				break
			}
		}
	}

	// Extract the last assistant message (most recent progress).
	var lastAssistantMsg string
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			if text, ok := history[i].Content.(string); ok && strings.TrimSpace(text) != "" {
				lastAssistantMsg = TruncateRunes(strings.TrimSpace(text), 200)
				break
			}
		}
	}

	var sb strings.Builder
	if firstUserMsg != "" {
		fmt.Fprintf(&sb, "用户原始请求：%s", firstUserMsg)
	}
	if lastAssistantMsg != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "最近助手回复：%s", lastAssistantMsg)
	}
	if sb.Len() == 0 {
		return "(对话历史存在但无文本内容)"
	}
	return sb.String()
}

// buildArchivedTaskSummaries formats archived tasks for the LLM prompt.
func buildArchivedTaskSummaries(tasks []ArchivedTask) string {
	if len(tasks) == 0 {
		return ""
	}
	var sb strings.Builder
	// Show at most 5 recent archived tasks to keep prompt compact.
	limit := 5
	if len(tasks) < limit {
		limit = len(tasks)
	}
	for _, t := range tasks[:limit] {
		summary := t.Summary
		if summary == "" {
			summary = TruncateRunes(t.LastRequest, 80)
		}
		fmt.Fprintf(&sb, "- [%s] %s (状态: %s)\n", t.ID, summary, t.Status)
	}
	return sb.String()
}
