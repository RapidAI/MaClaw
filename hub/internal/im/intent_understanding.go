package im

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// ---------------------------------------------------------------------------
// LLM system prompt for intent understanding
// ---------------------------------------------------------------------------

const intentUnderstandingSystemPrompt = `你是一个意图理解助手。用户会描述一个复杂任务，你需要：

1. 分析用户的描述，提取结构化意图
2. 用一句话复述你的理解
3. 列出需要澄清的模糊点
4. 判断信息是否足够开始执行

请以 JSON 格式返回，包含以下字段：
{
  "intent": {
    "category": "coding|product_design|innovation|business_plan",
    "summary": "一句话复述核心诉求",
    "goals": ["具体目标1", "具体目标2"],
    "constraints": ["约束条件1", "约束条件2"],
    "open_questions": ["模糊点1", "模糊点2"],
    "confidence": 0.0-1.0,
    "ready": false
  },
  "reply": "向用户展示的回复文本（复述理解+追问）",
  "ready": false
}

判断规则：
- ready=true 仅当用户明确表示可以开始（如"开工"、"开始"、"可以了"、"就这样"、"没问题了"）且你对需求理解充分（confidence >= 0.7）
- 用户说"开始我觉得还需要加个功能"这类包含"开始"但实际在补充需求的，ready=false
- 用户说"算了"、"取消"、"不做了"时，reply 中说明已取消，ready=false
- category 根据任务性质判断：编程开发=coding，产品设计=product_design，创新制定=innovation，商业计划=business_plan
- 每轮都要更新 intent 中的所有字段

仅返回 JSON，不要其他内容。`

// ---------------------------------------------------------------------------
// LLM interaction helpers
// ---------------------------------------------------------------------------

// understandingLLMResult is the parsed JSON output from the understanding LLM.
type understandingLLMResult struct {
	Intent StructuredIntent `json:"intent"`
	Reply  string           `json:"reply"`
	Ready  bool             `json:"ready"`
}

// buildUnderstandingPrompt constructs the messages array for the LLM call,
// including conversation history from the session.
func buildUnderstandingPrompt(session *UnderstandingSession, newText string) []interface{} {
	messages := []interface{}{
		map[string]string{"role": "system", "content": intentUnderstandingSystemPrompt},
	}

	// Add conversation history
	for _, round := range session.Rounds {
		messages = append(messages,
			map[string]string{"role": "user", "content": round.UserText},
		)
		if round.AssistantText != "" {
			messages = append(messages,
				map[string]string{"role": "assistant", "content": round.AssistantText},
			)
		}
	}

	// Add the new user message
	messages = append(messages,
		map[string]string{"role": "user", "content": newText},
	)

	return messages
}

// parseUnderstandingResult extracts StructuredIntent, reply text, and ready
// flag from the LLM's JSON output. Handles markdown code fences.
func parseUnderstandingResult(content string) (*StructuredIntent, string, bool, error) {
	content = strings.TrimSpace(content)

	// Strip markdown code fences if present
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			// Remove first and last lines (``` markers)
			end := len(lines) - 1
			for end > 0 && strings.TrimSpace(lines[end]) == "" {
				end--
			}
			if end > 0 && strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
				content = strings.Join(lines[1:end], "\n")
			} else {
				content = strings.Join(lines[1:], "\n")
			}
		}
	}

	var result understandingLLMResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, "", false, fmt.Errorf("JSON parse failed: %w (raw: %s)", err, truncate(content, 200))
	}

	return &result.Intent, result.Reply, result.Ready, nil
}

// callUnderstandingLLM performs the full LLM call flow for intent understanding.
// It builds the prompt, calls the LLM with a 10s timeout, and parses the result.
func callUnderstandingLLM(
	ctx context.Context,
	cfg *HubLLMConfig,
	breaker *CircuitBreaker,
	llmSem *LLMSemaphore,
	session *UnderstandingSession,
	text string,
) (*StructuredIntent, string, bool, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, "", false, fmt.Errorf("LLM not configured")
	}

	if !breaker.Allow() {
		return nil, "", false, fmt.Errorf("circuit breaker open")
	}

	messages := buildUnderstandingPrompt(session, text)
	llmCfg := cfg.ToMaclawLLMConfig()

	const understandingTimeout = 10 * time.Second
	callCtx, cancel := context.WithTimeout(ctx, understandingTimeout)
	defer cancel()

	// Acquire semaphore
	if !llmSem.Acquire(callCtx) {
		return nil, "", false, fmt.Errorf("LLM semaphore timeout")
	}
	defer llmSem.Release()

	client := &http.Client{Timeout: understandingTimeout}
	resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, client, understandingTimeout)
	if err != nil {
		breaker.RecordFailure()
		return nil, "", false, fmt.Errorf("LLM call failed: %w", err)
	}
	breaker.RecordSuccess()

	intent, reply, ready, err := parseUnderstandingResult(resp.Content)
	if err != nil {
		return nil, "", false, err
	}

	return intent, reply, ready, nil
}

// ---------------------------------------------------------------------------
// UnderstandingManager — manages understanding sessions
// ---------------------------------------------------------------------------

// UnderstandingManager manages multi-round intent understanding sessions.
type UnderstandingManager struct {
	configProvider func() *HubLLMConfig
	breaker        *CircuitBreaker
	llmSem         *LLMSemaphore
	repo           store.WorkflowRepository

	mu       sync.RWMutex
	sessions map[string]*UnderstandingSession // userID → session (in-memory cache)
}

// NewUnderstandingManager creates a new UnderstandingManager.
func NewUnderstandingManager(
	configProvider func() *HubLLMConfig,
	breaker *CircuitBreaker,
	llmSem *LLMSemaphore,
	repo store.WorkflowRepository,
) *UnderstandingManager {
	return &UnderstandingManager{
		configProvider: configProvider,
		breaker:        breaker,
		llmSem:         llmSem,
		repo:           repo,
		sessions:       make(map[string]*UnderstandingSession),
	}
}

// StartSession creates a new understanding session and performs the first
// LLM round. Returns the paraphrase + questions response.
func (um *UnderstandingManager) StartSession(
	ctx context.Context,
	userID, text string,
) (*GenericResponse, error) {
	// Check for existing session
	if existing := um.GetActiveSession(userID); existing != nil {
		// Already has a session — treat as HandleInput
		return um.HandleInput(ctx, userID, text)
	}

	now := time.Now()
	session := &UnderstandingSession{
		ID:        fmt.Sprintf("us_%d", now.UnixNano()),
		UserID:    userID,
		State:     UnderstandingActive,
		Rounds:    []UnderstandingRound{},
		CreatedAt: now,
		UpdatedAt: now,
	}

	cfg := um.configProvider()
	intent, reply, ready, err := callUnderstandingLLM(ctx, cfg, um.breaker, um.llmSem, session, text)
	if err != nil {
		log.Printf("[UnderstandingManager] LLM error for user=%s: %v", userID, err)
		return nil, err
	}

	// Record the round
	session.Intent = *intent
	session.Rounds = append(session.Rounds, UnderstandingRound{
		UserText:      text,
		AssistantText: reply,
		Timestamp:     now,
	})
	session.UpdatedAt = now

	// Store in memory
	um.mu.Lock()
	um.sessions[userID] = session
	um.mu.Unlock()

	// Persist to SQLite
	um.persistSession(ctx, session)

	if ready {
		session.State = UnderstandingConfirmed
		um.persistSession(ctx, session)
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "✅",
			Title:      "意图确认",
			Body:       reply + "\n\n🚀 意图已确认，准备开始工作流。",
			FallbackText: "__understanding_ready__",
		}, nil
	}

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "🤔",
		Title:      "意图理解",
		Body:       reply + "\n\n确定了就告诉我'开工'，或继续补充细节。",
	}, nil
}

// HandleInput processes a follow-up message in an active understanding session.
func (um *UnderstandingManager) HandleInput(
	ctx context.Context,
	userID, text string,
) (*GenericResponse, error) {
	session := um.GetActiveSession(userID)
	if session == nil {
		return nil, fmt.Errorf("no active understanding session for user %s", userID)
	}

	// Check for cancel triggers
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "算了" || lower == "取消" || lower == "cancel" || lower == "不做了" {
		um.CancelSession(userID)
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "🚫",
			Title:      "已取消",
			Body:       "好的，已取消当前意图理解会话。",
		}, nil
	}

	cfg := um.configProvider()
	intent, reply, ready, err := callUnderstandingLLM(ctx, cfg, um.breaker, um.llmSem, session, text)
	if err != nil {
		log.Printf("[UnderstandingManager] LLM error for user=%s: %v", userID, err)
		return nil, err
	}

	now := time.Now()
	session.Intent = *intent
	session.Rounds = append(session.Rounds, UnderstandingRound{
		UserText:      text,
		AssistantText: reply,
		Timestamp:     now,
	})
	session.UpdatedAt = now

	um.mu.Lock()
	um.sessions[userID] = session
	um.mu.Unlock()

	um.persistSession(ctx, session)

	if ready {
		session.State = UnderstandingConfirmed
		um.persistSession(ctx, session)
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "✅",
			Title:      "意图确认",
			Body:       reply + "\n\n🚀 意图已确认，准备开始工作流。",
			FallbackText: "__understanding_ready__",
		}, nil
	}

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "🤔",
		Title:      "意图理解",
		Body:       reply + "\n\n确定了就告诉我'开工'，或继续补充细节。",
	}, nil
}

// GetActiveSession returns the active understanding session for a user.
// Checks memory first, then falls back to SQLite.
func (um *UnderstandingManager) GetActiveSession(userID string) *UnderstandingSession {
	// Check memory cache first
	um.mu.RLock()
	session := um.sessions[userID]
	um.mu.RUnlock()
	if session != nil && session.State == UnderstandingActive {
		return session
	}

	// Fall back to SQLite
	if um.repo == nil {
		return nil
	}
	row, err := um.repo.GetActiveUnderstandingSession(context.Background(), userID)
	if err != nil || row == nil {
		return nil
	}

	// Reconstruct session from row
	s := &UnderstandingSession{
		ID:        row.ID,
		UserID:    row.UserID,
		State:     UnderstandingState(row.State),
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.IntentJSON != "" {
		_ = json.Unmarshal([]byte(row.IntentJSON), &s.Intent)
	}
	if row.RoundsJSON != "" {
		_ = json.Unmarshal([]byte(row.RoundsJSON), &s.Rounds)
	}

	if s.State != UnderstandingActive {
		return nil
	}

	// Cache in memory
	um.mu.Lock()
	um.sessions[userID] = s
	um.mu.Unlock()

	return s
}

// CancelSession marks the session as cancelled and cleans up.
func (um *UnderstandingManager) CancelSession(userID string) {
	um.mu.Lock()
	session := um.sessions[userID]
	if session != nil {
		session.State = UnderstandingCancelled
		session.UpdatedAt = time.Now()
	}
	delete(um.sessions, userID)
	um.mu.Unlock()

	if session != nil && um.repo != nil {
		um.persistSession(context.Background(), session)
	}
}

// RemoveSession removes a session from the in-memory cache (e.g. after
// workflow creation consumes it).
func (um *UnderstandingManager) RemoveSession(userID string) {
	um.mu.Lock()
	delete(um.sessions, userID)
	um.mu.Unlock()
}

// cleanupExpiredSessions removes sessions that have been inactive for 30+ minutes.
func (um *UnderstandingManager) CleanupExpiredSessions() {
	cutoff := time.Now().Add(-30 * time.Minute)

	// Collect expired sessions under lock, persist outside lock.
	var expired []*UnderstandingSession

	um.mu.Lock()
	for userID, session := range um.sessions {
		if session.UpdatedAt.Before(cutoff) && session.State == UnderstandingActive {
			session.State = UnderstandingCancelled
			session.UpdatedAt = time.Now()
			expired = append(expired, session)
			delete(um.sessions, userID)
		}
	}
	um.mu.Unlock()

	// Persist outside the lock to avoid holding it during I/O.
	for _, session := range expired {
		if um.repo != nil {
			um.persistSession(context.Background(), session)
		}
	}
}

// persistSession saves the session to SQLite.
func (um *UnderstandingManager) persistSession(ctx context.Context, session *UnderstandingSession) {
	if um.repo == nil {
		return
	}

	intentJSON, _ := json.Marshal(session.Intent)
	roundsJSON, _ := json.Marshal(session.Rounds)

	row := &store.UnderstandingSessionRow{
		ID:         session.ID,
		UserID:     session.UserID,
		IntentJSON: string(intentJSON),
		RoundsJSON: string(roundsJSON),
		State:      string(session.State),
		CreatedAt:  session.CreatedAt,
		UpdatedAt:  session.UpdatedAt,
	}

	if err := um.repo.SaveUnderstandingSession(ctx, row); err != nil {
		log.Printf("[UnderstandingManager] persist error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Off-topic detection (Task 3.3)
// ---------------------------------------------------------------------------

// OffTopicResult indicates whether a message is on-topic for the current workflow.
type OffTopicResult int

const (
	OnTopic         OffTopicResult = iota // related to current workflow
	OffTopicSimple                        // unrelated but simple (quick answer)
	OffTopicComplex                       // unrelated and complex (suggest cancel/finish first)
)

// offTopicSimplePatterns are keywords indicating simple off-topic messages.
var offTopicSimplePatterns = []string{
	"天气", "几点", "时间", "日期", "今天",
	"你好", "谢谢", "嗯", "ok", "hi", "hello",
}

// detectOffTopic uses lightweight rules (keywords + length) to determine
// if a message is off-topic relative to the current workflow type.
// No LLM call is made.
func detectOffTopic(currentWorkflowType string, text string) OffTopicResult {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Very short messages in a workflow context are likely on-topic
	// (confirmations, modifications, etc.)
	if len([]rune(lower)) < 5 {
		return OnTopic
	}

	// Check for workflow-related keywords based on type
	workflowKeywords := getWorkflowKeywords(currentWorkflowType)
	for _, kw := range workflowKeywords {
		if strings.Contains(lower, kw) {
			return OnTopic
		}
	}

	// Check for common workflow interaction patterns
	interactionPatterns := []string{
		"下一步", "确认", "继续", "next", "ok", "好的",
		"跳过", "skip", "取消", "cancel", "算了", "不做了",
		"改一下", "修改", "调整", "补充", "加上", "去掉", "删除",
		"开工", "开始", "可以了", "就这样", "没问题",
	}
	for _, p := range interactionPatterns {
		if strings.Contains(lower, p) {
			return OnTopic
		}
	}

	// Check for simple off-topic patterns
	for _, p := range offTopicSimplePatterns {
		if strings.Contains(lower, p) {
			return OffTopicSimple
		}
	}

	// Messages that don't match any workflow keywords may be off-topic
	if len([]rune(lower)) > 10 {
		// Check if it looks like a new task request
		newTaskIndicators := []string{
			"帮我做", "帮我开发", "帮我设计", "帮我写",
			"另一个", "新的项目", "换个",
		}
		for _, ind := range newTaskIndicators {
			if strings.Contains(lower, ind) {
				return OffTopicComplex
			}
		}
	}

	// Default: assume on-topic (benefit of the doubt)
	return OnTopic
}

// getWorkflowKeywords returns keywords relevant to a workflow type.
func getWorkflowKeywords(workflowType string) []string {
	switch workflowType {
	case "coding":
		return []string{
			"代码", "编码", "函数", "接口", "api", "数据库", "测试",
			"bug", "错误", "需求", "设计", "架构", "模块", "实现",
			"部署", "性能", "安全", "重构", "变量", "类", "方法",
		}
	case "product_design":
		return []string{
			"用户", "功能", "需求", "界面", "交互", "流程", "原型",
			"产品", "设计", "体验", "场景", "痛点", "竞品",
		}
	case "innovation":
		return []string{
			"创新", "市场", "机会", "趋势", "可行性", "mvp",
			"验证", "路线图", "里程碑", "方案",
		}
	case "business_plan":
		return []string{
			"商业", "市场", "收入", "成本", "客户", "竞争",
			"定价", "运营", "财务", "团队", "融资",
		}
	default:
		return nil
	}
}
