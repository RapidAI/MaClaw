package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// cancelWords are Chinese phrases that indicate the user wants to cancel
// the current intent understanding session.
var cancelWords = []string{"算了", "取消", "不做了"}

// sessionExpiryDuration is the maximum inactivity time before a session
// is considered expired and eligible for cleanup.
const sessionExpiryDuration = 30 * time.Minute

// llmIntentTimeout is the timeout for LLM calls during intent understanding.
const llmIntentTimeout = 10 * time.Second

// IntentUnderstandingManager manages multi-round intent clarification sessions.
// It uses an independent LLM conversation (no tools) to understand user intent
// before starting a workflow.
type IntentUnderstandingManager struct {
	mu       sync.RWMutex
	sessions map[string]*UnderstandingSession // userID → session
	store    PersistenceStore
	llm      LLMCaller
	registry *WorkflowRegistry
}

// NewIntentUnderstandingManager creates a new manager with the given dependencies.
func NewIntentUnderstandingManager(store PersistenceStore, llm LLMCaller, registry *WorkflowRegistry) *IntentUnderstandingManager {
	return &IntentUnderstandingManager{
		sessions: make(map[string]*UnderstandingSession),
		store:    store,
		llm:      llm,
		registry: registry,
	}
}

// Start creates a new intent understanding session for the user and sends
// the first message to the LLM for analysis. Returns the LLM's reply text.
func (m *IntentUnderstandingManager) Start(userID, text string) (string, error) {
	now := time.Now()
	sess := &UnderstandingSession{
		ID:        fmt.Sprintf("iu-%s-%d", userID, now.UnixMilli()),
		UserID:    userID,
		Intent:    StructuredIntent{},
		Rounds:    nil,
		State:     UnderstandingActive,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Build LLM messages: system prompt + user message
	messages := m.buildInitialMessages(text)

	// Call LLM
	raw, err := m.llm.DoSimpleLLMRequest(messages, llmIntentTimeout)
	if err != nil {
		return "", fmt.Errorf("intent understanding LLM call: %w", err)
	}

	// Parse LLM response
	reply, intent, _ := parseLLMIntentResponse(raw)

	// Update session
	sess.Intent = intent
	sess.Rounds = append(sess.Rounds, UnderstandingRound{
		UserText:      text,
		AssistantText: reply,
		Timestamp:     now,
	})

	// Store in memory and persist
	m.mu.Lock()
	m.sessions[userID] = sess
	m.mu.Unlock()

	if m.store != nil {
		_ = m.store.SaveUnderstandingSession(sess)
	}

	return reply, nil
}

// HandleInput processes user input within an active intent understanding session.
// Returns: reply text, whether the user is ready to start the workflow,
// whether the user cancelled, the confirmed intent (when ready=true), and any error.
func (m *IntentUnderstandingManager) HandleInput(userID, text string) (reply string, ready bool, cancelled bool, intent *StructuredIntent, err error) {
	// Check for cancel words first
	trimmed := strings.TrimSpace(text)
	if isCancelMessage(trimmed) {
		m.cleanupSession(userID)
		return "", false, true, nil, nil
	}

	m.mu.RLock()
	sess := m.sessions[userID]
	m.mu.RUnlock()

	if sess == nil {
		return "", false, false, nil, fmt.Errorf("no active understanding session for user %s", userID)
	}

	// Build conversation messages from session history
	messages := m.buildConversationMessages(sess, text)

	// Call LLM
	raw, llmErr := m.llm.DoSimpleLLMRequest(messages, llmIntentTimeout)
	if llmErr != nil {
		return "", false, false, nil, fmt.Errorf("intent understanding LLM call: %w", llmErr)
	}

	// Parse response
	replyText, parsedIntent, isReady := parseLLMIntentResponse(raw)

	// Update session
	now := time.Now()
	m.mu.Lock()
	sess.Intent = parsedIntent
	sess.Rounds = append(sess.Rounds, UnderstandingRound{
		UserText:      text,
		AssistantText: replyText,
		Timestamp:     now,
	})
	sess.UpdatedAt = now
	if isReady {
		sess.State = UnderstandingConfirmed
	}
	// Capture intent before potential deletion
	confirmedIntent := sess.Intent
	m.mu.Unlock()

	if m.store != nil {
		_ = m.store.SaveUnderstandingSession(sess)
	}

	// If ready, clean up the in-memory session (persistence already saved as confirmed)
	if isReady {
		m.mu.Lock()
		delete(m.sessions, userID)
		m.mu.Unlock()
		return replyText, true, false, &confirmedIntent, nil
	}

	return replyText, false, false, nil, nil
}

// GetSession returns the active understanding session for the user, or nil.
func (m *IntentUnderstandingManager) GetSession(userID string) *UnderstandingSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[userID]
}

// HasActiveSession returns true if the user has an active understanding session.
func (m *IntentUnderstandingManager) HasActiveSession(userID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sess, ok := m.sessions[userID]
	return ok && sess != nil && sess.State == UnderstandingActive
}

// CleanupExpired removes sessions that have been inactive for more than 30 minutes.
func (m *IntentUnderstandingManager) CleanupExpired() {
	cutoff := time.Now().Add(-sessionExpiryDuration)

	m.mu.Lock()
	var expired []string
	for uid, sess := range m.sessions {
		if sess.UpdatedAt.Before(cutoff) {
			expired = append(expired, uid)
		}
	}
	for _, uid := range expired {
		if sess, ok := m.sessions[uid]; ok {
			sess.State = UnderstandingExpired
			if m.store != nil {
				_ = m.store.SaveUnderstandingSession(sess)
			}
		}
		delete(m.sessions, uid)
	}
	m.mu.Unlock()
}

// RestoreSession loads a session from the persistence store into memory.
// Called during application startup to recover active sessions.
func (m *IntentUnderstandingManager) RestoreSession(userID string) error {
	if m.store == nil {
		return nil
	}
	sess, err := m.store.LoadUnderstandingSession(userID)
	if err != nil {
		return err
	}
	if sess == nil || sess.State != UnderstandingActive {
		return nil
	}
	m.mu.Lock()
	m.sessions[userID] = sess
	m.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// cleanupSession removes the session from memory and persistence.
func (m *IntentUnderstandingManager) cleanupSession(userID string) {
	m.mu.Lock()
	if sess, ok := m.sessions[userID]; ok {
		sess.State = UnderstandingCancelled
		if m.store != nil {
			_ = m.store.SaveUnderstandingSession(sess)
		}
	}
	delete(m.sessions, userID)
	m.mu.Unlock()

	if m.store != nil {
		_ = m.store.DeleteUnderstandingSession(userID)
	}
}

// isCancelMessage checks if the message contains any cancel words.
func isCancelMessage(text string) bool {
	for _, w := range cancelWords {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// buildInitialMessages constructs the LLM message array for the first round.
func (m *IntentUnderstandingManager) buildInitialMessages(userText string) []interface{} {
	systemPrompt := m.buildSystemPrompt()
	return []interface{}{
		map[string]interface{}{"role": "system", "content": systemPrompt},
		map[string]interface{}{"role": "user", "content": userText},
	}
}

// buildConversationMessages constructs the full conversation history for the LLM.
func (m *IntentUnderstandingManager) buildConversationMessages(sess *UnderstandingSession, newUserText string) []interface{} {
	systemPrompt := m.buildSystemPrompt()
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": systemPrompt},
	}

	// Add conversation history
	for _, round := range sess.Rounds {
		messages = append(messages,
			map[string]interface{}{"role": "user", "content": round.UserText},
			map[string]interface{}{"role": "assistant", "content": round.AssistantText},
		)
	}

	// Add the new user message
	messages = append(messages,
		map[string]interface{}{"role": "user", "content": newUserText},
	)

	return messages
}

// buildSystemPrompt constructs the system prompt for intent understanding.
// Includes all registered template descriptions so the LLM can classify intent.
func (m *IntentUnderstandingManager) buildSystemPrompt() string {
	var b strings.Builder

	b.WriteString("你是一个智能助手的意图理解模块。你的任务是通过对话理解用户想要完成的任务，并将其分类到合适的工作流类型。\n\n")

	// Include all registered template descriptions
	if m.registry != nil {
		descs := m.registry.AllDescriptions()
		if descs != "" {
			b.WriteString("## 可用的工作流类型\n\n")
			b.WriteString(descs)
			b.WriteString("\n")
		}
	}

	b.WriteString("## 你的职责\n\n")
	b.WriteString("1. 分析用户的意图，判断属于哪种工作流类型\n")
	b.WriteString("2. 提取用户的目标、约束条件和开放性问题\n")
	b.WriteString("3. 如果信息不足，通过追问澄清需求\n")
	b.WriteString("4. 通过语义分析判断用户是否已经准备好开始执行（ready），不要仅依赖关键词匹配\n")
	b.WriteString("5. 每轮回复末尾加上提示：确定了就告诉我\"开工\"\n\n")

	b.WriteString("## 输出格式\n\n")
	b.WriteString("请严格以 JSON 格式输出，不要包含其他文本：\n")
	b.WriteString("```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"intent\": {\n")
	b.WriteString("    \"category\": \"工作流类型（如 coding, product_design, innovation, business_plan, testing）\",\n")
	b.WriteString("    \"summary\": \"用户意图的一句话摘要\",\n")
	b.WriteString("    \"goals\": [\"目标1\", \"目标2\"],\n")
	b.WriteString("    \"constraints\": [\"约束1\", \"约束2\"],\n")
	b.WriteString("    \"open_questions\": [\"待澄清问题1\"],\n")
	b.WriteString("    \"confidence\": 0.8,\n")
	b.WriteString("    \"ready\": false\n")
	b.WriteString("  },\n")
	b.WriteString("  \"reply\": \"你对用户说的话（自然语言回复，末尾提示确定了就告诉我'开工'）\",\n")
	b.WriteString("  \"ready\": false\n")
	b.WriteString("}\n")
	b.WriteString("```\n\n")

	b.WriteString("## ready 判断规则\n\n")
	b.WriteString("- ready=true：用户明确表示可以开始了（如\"开工\"、\"开始吧\"、\"可以了\"、\"没问题了\"、\"就这样\"），且你对意图的理解已经足够清晰\n")
	b.WriteString("- ready=false：用户还在补充信息、提出新需求、或者虽然说了类似\"开始\"但实际在补充需求（如\"开始我觉得还需要加个功能\"）\n")
	b.WriteString("- 通过语义分析综合判断，不要仅匹配关键词\n")

	return b.String()
}

// llmIntentResult is the expected JSON structure from the LLM response.
type llmIntentResult struct {
	Intent StructuredIntent `json:"intent"`
	Reply  string           `json:"reply"`
	Ready  bool             `json:"ready"`
}

// parseLLMIntentResponse parses the LLM's JSON response.
// Returns the reply text, structured intent, and ready flag.
// On parse failure, uses the raw text as the reply with a zero intent.
func parseLLMIntentResponse(raw string) (reply string, intent StructuredIntent, ready bool) {
	// Try to extract JSON from the response (may be wrapped in markdown code block)
	jsonStr := extractJSON(raw)

	var result llmIntentResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Fallback: use raw text as reply
		return strings.TrimSpace(raw), StructuredIntent{}, false
	}

	return result.Reply, result.Intent, result.Ready
}

// extractJSON attempts to extract a JSON object from text that may be
// wrapped in markdown code blocks or contain surrounding text.
func extractJSON(text string) string {
	trimmed := strings.TrimSpace(text)

	// Try to find JSON within ```json ... ``` blocks
	if idx := strings.Index(trimmed, "```json"); idx >= 0 {
		start := idx + len("```json")
		if end := strings.Index(trimmed[start:], "```"); end >= 0 {
			return strings.TrimSpace(trimmed[start : start+end])
		}
	}

	// Try to find JSON within ``` ... ``` blocks
	if idx := strings.Index(trimmed, "```"); idx >= 0 {
		start := idx + len("```")
		if end := strings.Index(trimmed[start:], "```"); end >= 0 {
			candidate := strings.TrimSpace(trimmed[start : start+end])
			if strings.HasPrefix(candidate, "{") {
				return candidate
			}
		}
	}

	// Try to find a top-level JSON object
	if braceStart := strings.Index(trimmed, "{"); braceStart >= 0 {
		// Find the matching closing brace
		depth := 0
		for i := braceStart; i < len(trimmed); i++ {
			switch trimmed[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return trimmed[braceStart : i+1]
				}
			}
		}
	}

	return trimmed
}
