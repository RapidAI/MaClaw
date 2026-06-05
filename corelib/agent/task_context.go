package agent

// task_context.go - Unified task context management layer.
//
// TaskContextManager is the single decision point for "is this message a
// continuation of the current task, a new task, or a recall of a past task?"
//
// It replaces the scattered decision logic that was spread across:
//   - legacy fresh-task heuristics
//   - shouldAutoClearIncompleteTaskContext (incomplete task marker)
//   - TopicSwitchDetector (BM25 + embedding + LLM voting)
//   - shouldRequireExecutionConfirmation (confirmation gate)
//
// All of these now defer to TaskContextManager.Resolve() for the primary
// decision, then apply their own logic only within the resolved action.

import (
	"context"
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
	ID          string             `json:"id"`
	UserID      string             `json:"user_id"`
	Summary     string             `json:"summary"`      // one-line task description
	LastRequest string             `json:"last_request"` // original user request text
	FilePaths   []string           `json:"file_paths"`   // key files involved
	ProjectPath string             `json:"project_path"` // workspace directory
	Status      ArchivedTaskStatus `json:"status"`
	CreatedAt   time.Time          `json:"created_at"`
	ArchivedAt  time.Time          `json:"archived_at"`

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
		LLMTimeout:               30 * time.Second,
	}
}

// TaskLLMClassifier is the interface for making lightweight LLM calls.
// Implemented by the GUI layer's LLMClassify wrapper.
type TaskLLMClassifier interface {
	// Classify sends a system+user prompt to the LLM and returns the raw
	// response text. Returns error on timeout or LLM failure.
	Classify(systemPrompt, userMessage string, timeoutSec int) (string, error)
}

// TaskLLMContextClassifier is implemented by classifiers that can bind an
// auxiliary LLM call to the owning agent loop's context and trace metadata.
type TaskLLMContextClassifier interface {
	ClassifyWithContext(ctx context.Context, systemPrompt, userMessage string, timeoutSec int) (string, error)
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
	// Context scopes optional LLM work to the owning agent loop. If nil, a
	// bounded background context is used for compatibility.
	Context context.Context

	// UserMessage is the new message from the user.
	UserMessage string

	// OwnerID identifies the logical conversation owner for diagnostics.
	OwnerID string

	// History is the current conversation history (may be empty).
	History []ConversationEntry

	// LastAccess is when the conversation was last active.
	LastAccess time.Time

	// ArchivedTasks are the user's recent archived tasks (for recall matching).
	ArchivedTasks []ArchivedTask

	// HasPendingAskUser is true if the previous assistant message was an
	// ask_user question; the current message is almost certainly a response.
	HasPendingAskUser bool

	// IsConfirmedResume is true if this message is a confirmation approval
	// (user clicked confirm on the confirmation card). The message text has
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
//  4. Non-destructive fallback when the classifier is unavailable
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

	// --- Layer 4: Non-destructive fallback ---
	// Ambiguous task switching is semantic. Without a classifier, preserve current
	// context; only an explicit classifier result may destructively switch tasks.
	return fallbackTaskContextDecision("history exists, no LLM available")
}

// classifyWithLLM uses a lightweight LLM call to determine the task action.
// It is the only path for ambiguous messages with existing history; recency
// and message length must not decide task continuity by themselves.
//
// The LLM call runs in a goroutine with a bounded deadline. If the call
// completes before the deadline, the result is used immediately. If it times
// out, fallback preserves context rather than destructively clearing history.
func (m *TaskContextManager) classifyWithLLM(input ResolveInput) TaskContextDecision {
	// Build a compact context summary for the LLM.
	currentTaskSummary := buildCurrentTaskSummary(input.History)
	archivedSummaries := buildArchivedTaskSummaries(input.ArchivedTasks)

	systemPrompt := taskContextClassifierPrompt

	var userMsg strings.Builder
	fmt.Fprintf(&userMsg, "Current task summary: %s\n", currentTaskSummary)
	if input.HasIncompleteTaskMarker {
		userMsg.WriteString("Current task status: previous turn may be incomplete or interrupted. If user asks to continue/resume, classify as continue; if unrelated, classify as new.\n")
	}
	if archivedSummaries != "" {
		fmt.Fprintf(&userMsg, "\nArchived tasks:\n%s\n", archivedSummaries)
	}
	fmt.Fprintf(&userMsg, "\nNew user message: %s", strings.TrimSpace(input.UserMessage))

	// Run LLM call in a goroutine to allow parallel work by the caller.
	type llmResult struct {
		resp string
		err  error
	}
	resultCh := make(chan llmResult, 1)
	go func() {
		callCtx := input.Context
		if callCtx == nil {
			callCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(callCtx, m.config.LLMTimeout)
		defer cancel()
		if classifier, ok := m.llm.(TaskLLMContextClassifier); ok {
			resp, err := classifier.ClassifyWithContext(ctx, systemPrompt, userMsg.String(), int(m.config.LLMTimeout.Seconds()))
			resultCh <- llmResult{resp: resp, err: err}
			return
		}
		resp, err := m.llm.Classify(systemPrompt, userMsg.String(), int(m.config.LLMTimeout.Seconds()))
		resultCh <- llmResult{resp: resp, err: err}
	}()

	// Wait for result with timeout (defensive; the LLM.Classify already
	// has its own timeout, but this ensures we never block longer than LLMTimeout + 500ms).
	deadline := m.config.LLMTimeout + 500*time.Millisecond
	var done <-chan struct{}
	if input.Context != nil {
		done = input.Context.Done()
	}
	select {
	case r := <-resultCh:
		if r.err != nil {
			log.Printf("[TaskContext] LLM classification failed: %v, preserving current task context", r.err)
			return fallbackTaskContextDecision(fmt.Sprintf("LLM failed: %v", r.err))
		}
		return m.parseLLMResponse(r.resp, input.ArchivedTasks)
	case <-time.After(deadline):
		log.Printf("[TaskContext] LLM classification timed out after %v, preserving current task context", deadline)
		return fallbackTaskContextDecision(fmt.Sprintf("LLM timed out after %v", deadline))
	case <-done:
		log.Printf("[TaskContext] LLM classification canceled: %v, preserving current task context", input.Context.Err())
		return fallbackTaskContextDecision(fmt.Sprintf("LLM canceled: %v", input.Context.Err()))
	}
}

func fallbackTaskContextDecision(cause string) TaskContextDecision {
	return TaskContextDecision{Action: TaskContinue, Reason: cause + ", preserving current task context", Source: "fallback"}
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

	if strings.HasPrefix(resp, "continue") {
		return TaskContextDecision{
			Action: TaskContinue,
			Reason: "LLM classified as continuation",
			Source: "llm",
		}
	}

	// Invalid classifier output is classifier failure, not evidence of a new task.
	// Keep current context unless the classifier explicitly says "new".
	return TaskContextDecision{
		Action: TaskContinue,
		Reason: fmt.Sprintf("unrecognized LLM response %q; preserving current task context", TruncateRunes(resp, 50)),
		Source: "llm",
	}
}

// --- Prompt ---

const taskContextClassifierPrompt = `You are a task context classifier. Decide whether the user's new message is:

1. continue - continuation of the current task.
2. new - a new unrelated task.
3. recall:<task_id> - resume a specific archived task.

Rules:
- If the message modifies, converts, supplements, or refers to current task output, choose continue.
- If the message is unrelated to the current task, choose new.
- If the message asks to resume an archived task, choose recall:<task_id>.
- When unsure, choose continue. Only a clear unrelated request should be classified as new.

Reply only with continue, new, or recall:<task_id>. Do not explain.`

// --- Helper functions ---

// buildCurrentTaskSummary extracts a compact summary of the current conversation.
func buildCurrentTaskSummary(history []ConversationEntry) string {
	if len(history) == 0 {
		return "(none)"
	}

	var firstUserMsg string
	for _, e := range history {
		if e.Role == "user" {
			if text, ok := e.Content.(string); ok && strings.TrimSpace(text) != "" {
				firstUserMsg = TruncateRunes(strings.TrimSpace(text), 150)
				break
			}
		}
	}

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
		fmt.Fprintf(&sb, "Initial user request: %s", firstUserMsg)
	}
	if lastAssistantMsg != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "Recent assistant reply: %s", lastAssistantMsg)
	}
	if sb.Len() == 0 {
		return "(history exists but has no text content)"
	}
	return sb.String()
}

// buildArchivedTaskSummaries formats archived tasks for the LLM prompt.
func buildArchivedTaskSummaries(tasks []ArchivedTask) string {
	if len(tasks) == 0 {
		return ""
	}
	var sb strings.Builder
	limit := 5
	if len(tasks) < limit {
		limit = len(tasks)
	}
	for _, t := range tasks[:limit] {
		summary := t.Summary
		if summary == "" {
			summary = TruncateRunes(t.LastRequest, 80)
		}
		fmt.Fprintf(&sb, "- [%s] %s (status: %s)\n", t.ID, summary, t.Status)
	}
	return sb.String()
}
