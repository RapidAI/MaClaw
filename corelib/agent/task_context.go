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

	// ConfirmedContinuation is true only when the classifier explicitly
	// identified the user message as a continuation of the current task.
	ConfirmedContinuation bool
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

	// HasActiveBackgroundTask is true when a local or SSH background command
	// spawned from the current runtime is still active. Active work is a hard
	// task boundary: a follow-up message must not destructively archive the
	// current task unless the frontend explicitly starts a new one.
	HasActiveBackgroundTask bool

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

	if input.HasActiveBackgroundTask {
		return TaskContextDecision{
			Action: TaskContinue,
			Reason: "active background task is still running",
			Source: "runtime",
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
	resp = normalizeTaskContextClassifierResponse(resp)

	// The LLM should respond with one of: "continue", "new", "recall:<id>"
	if strings.HasPrefix(strings.ToLower(resp), "recall:") {
		taskID := strings.TrimSpace(resp[len("recall:"):])
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
		log.Printf("[TaskContext] LLM returned recall:%s but task not found in archive, preserving current task context", taskID)
		return TaskContextDecision{
			Action: TaskContinue,
			Reason: fmt.Sprintf("LLM said recall:%s but task not found; preserving current task context", taskID),
			Source: "llm",
		}
	}

	if strings.EqualFold(resp, "new") {
		return TaskContextDecision{
			Action: TaskNew,
			Reason: "LLM classified as new task",
			Source: "llm",
		}
	}

	if strings.EqualFold(resp, "continue") {
		return TaskContextDecision{
			Action:                TaskContinue,
			Reason:                "LLM classified as continuation",
			Source:                "llm",
			ConfirmedContinuation: true,
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

func normalizeTaskContextClassifierResponse(resp string) string {
	resp = strings.TrimSpace(resp)
	if strings.HasPrefix(resp, "```") && strings.HasSuffix(resp, "```") {
		resp = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(resp, "```"), "```"))
		if i := strings.IndexAny(resp, "\r\n"); i >= 0 {
			firstLine := strings.TrimSpace(resp[:i])
			rest := strings.TrimSpace(resp[i+1:])
			if isTaskContextFenceLanguage(firstLine) && rest != "" {
				resp = rest
			}
		}
	}
	resp = strings.Trim(resp, "`\"' \t\r\n")
	return strings.TrimSpace(resp)
}

func isTaskContextFenceLanguage(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "text", "txt", "plain", "plaintext", "json", "md", "markdown":
		return true
	default:
		return false
	}
}

// --- Prompt ---

const taskContextClassifierPrompt = `You are a task context classifier. Decide whether the user's new message is:

1. continue - continuation of the current task.
2. new - a new unrelated task.
3. recall:<task_id> - resume a specific archived task.

Rules:
- If the message modifies, converts, supplements, or refers to current task output, choose continue.
- If the message asks to act on items/issues/suggestions from the recent assistant reply (e.g. "fix these", "do the above", "implement those 4 changes"), choose continue.
- If the message uses pronouns or demonstratives that reference the current conversation ("this", "that", "these", "those", "above", "上面的", "这些", "那几个"), choose continue.
- If the message is unrelated to the current task, choose new.
- If the message asks to resume an archived task, choose recall:<task_id>.
- When unsure, choose continue. Only a clearly unrelated request should be classified as new.

Reply only with continue, new, or recall:<task_id>. Do not explain.`

// --- Helper functions ---

// buildCurrentTaskSummary extracts a compact summary of the current conversation.
//
// The summary must preserve enough referential anchors for the classifier LLM
// to correctly determine whether the next user message ("fix these 4 issues",
// "do the above", etc.) refers to the current task's output.
//
// Strategy:
//   - firstUserMsg: the original task request (150 rune budget)
//   - lastAssistantMsg: structural skeleton of the most recent assistant output.
//     For structured outputs (numbered lists, markdown headings, bullet points),
//     all item titles/headings are preserved so that back-references like
//     "fix those 4 problems" have explicit anchors in the summary.
//     For plain prose, a generous truncation (500 runes) is used.
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
	// Scan backward through assistant messages to find the best skeleton.
	// Prefer a message with structural content (numbered list, headings,
	// bullets) because it contains the referential anchors the classifier
	// needs (e.g., "4 problems" → the 4 numbered items). If only short
	// prose is found, use that. Limit scan to 4 assistant messages.
	assistantScanned := 0
	for i := len(history) - 1; i >= 0 && assistantScanned < 4; i-- {
		if history[i].Role != "assistant" {
			continue
		}
		text, ok := history[i].Content.(string)
		if !ok || strings.TrimSpace(text) == "" {
			assistantScanned++
			continue
		}
		candidate := strings.TrimSpace(text)
		assistantScanned++

		if hasStructuralContent(candidate) {
			// Found a structured message — use its skeleton and stop.
			lastAssistantMsg = extractStructuralSkeleton(candidate)
			break
		}
		if lastAssistantMsg == "" {
			// Capture the most recent assistant message as fallback.
			lastAssistantMsg = extractStructuralSkeleton(candidate)
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

// extractStructuralSkeleton extracts the structural skeleton of a text.
//
// For structured outputs (numbered lists, markdown headings, bullet points),
// it preserves all item titles/headings so that back-references like
// "fix those 4 problems" or "do the 3rd item" can be anchored.
//
// For plain prose without structure, it falls back to a generous truncation.
//
// This is the mechanism that ensures the task-context classifier has enough
// referential anchors to correctly classify continuation messages. Without
// this, a 200-char blind truncation loses all structure beyond the first
// item, making it impossible for the classifier to match "4 problems" to
// the actual 4 items in the output.
func extractStructuralSkeleton(text string) string {
	lines := strings.Split(text, "\n")

	// Detect structured lines: numbered items, bullet points, markdown headings.
	var structured []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isStructuredLine(trimmed) {
			structured = append(structured, trimmed)
		}
	}

	// If the text has meaningful structure (>=2 structured lines), extract
	// all structured lines as the skeleton. Each line is truncated to keep
	// descriptions short, but titles/numbers are preserved in full.
	if len(structured) >= 2 {
		var sb strings.Builder
		// Include a brief context preamble: the last non-empty, non-structured,
		// non-code-fence line that appears BEFORE the first structured line.
		// This captures the introductory sentence (e.g., "发现以下 4 个问题：")
		// without accidentally picking up code block contents.
		firstStructIdx := -1
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && isStructuredLine(trimmed) {
				firstStructIdx = i
				break
			}
		}
		if firstStructIdx > 0 {
			for i := firstStructIdx - 1; i >= 0; i-- {
				trimmed := strings.TrimSpace(lines[i])
				if trimmed == "" || strings.HasPrefix(trimmed, "```") {
					continue
				}
				sb.WriteString(TruncateRunes(trimmed, 80))
				sb.WriteString("\n")
				break
			}
		}
		for _, sl := range structured {
			// Preserve the full structured line title but truncate long
			// descriptions that follow the title on the same line.
			sb.WriteString(TruncateRunes(sl, 120))
			sb.WriteString("\n")
		}
		result := strings.TrimSpace(sb.String())
		// Hard cap the total skeleton to avoid token explosion for very
		// long lists (e.g., 50-item changelogs).
		return TruncateRunes(result, 800)
	}

	// No meaningful structure detected — fall back to generous truncation.
	// 500 runes is enough for 2-3 paragraphs of prose, providing the
	// classifier with sufficient semantic context.
	return TruncateRunes(text, 500)
}

// hasStructuralContent reports whether a text contains at least 2 structured
// lines (numbered items, bullet points, or headings). Used to decide whether
// to prefer this message's skeleton over a later short prose message.
func hasStructuralContent(text string) bool {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && isStructuredLine(trimmed) {
			count++
			if count >= 2 {
				return true
			}
		}
	}
	return false
}

// isStructuredLine reports whether a line looks like a structural element:
// numbered list item, bullet point, or markdown heading.
func isStructuredLine(line string) bool {
	if len(line) == 0 {
		return false
	}
	// Markdown headings: # ... , ## ... , ### ...
	if line[0] == '#' {
		return true
	}
	// Bullet points: - ... , * ... , • ...
	if len(line) >= 2 && (line[0] == '-' || line[0] == '*') && line[1] == ' ' {
		return true
	}
	if strings.HasPrefix(line, "• ") || strings.HasPrefix(line, "· ") {
		return true
	}
	// Numbered list: 1. ... , 1) ... , (1) ...
	// Check if line starts with digit(s) followed by . or )
	i := 0
	for i < len(line) && i < 4 && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i > 0 && i < len(line) {
		if line[i] == '.' || line[i] == ')' {
			return true
		}
		// Chinese numbered list: 1、 2、
		if strings.HasPrefix(line[i:], "、") {
			return true
		}
	}
	// Parenthesized numbers: (1) ...
	if line[0] == '(' {
		j := 1
		for j < len(line) && j < 5 && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		if j > 1 && j < len(line) && line[j] == ')' {
			return true
		}
	}
	return false
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
