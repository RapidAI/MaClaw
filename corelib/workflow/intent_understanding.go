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
// Set to 30s to accommodate third-party API providers (e.g. Zhipu GLM at
// open.bigmodel.cn) which can be significantly slower than direct Anthropic.
const llmIntentTimeout = 30 * time.Second

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

// StartResult holds the outcome of Start(). When Rejected is true, the LLM
// determined this is NOT a workflow task — the caller should fall through to
// the normal agent loop without creating a session.
type StartResult struct {
	Reply    string // LLM's reply text (empty when rejected)
	Rejected bool   // true = LLM says "not a workflow", no session created
}

// Start creates a new intent understanding session for the user and sends
// the first message to the LLM for analysis.
//
// The LLM performs a one-shot classification: if it determines the message
// is NOT a workflow task (category="none"), it returns Rejected=true and no
// session is created. This avoids multi-round overhead for simple directives
// like "翻译这段话" or "什么是微服务".
//
// If the LLM determines it IS a workflow task, a session is created and the
// LLM's reply (with intent summary and clarification questions) is returned.
func (m *IntentUnderstandingManager) Start(userID, text string) (*StartResult, error) {
	now := time.Now()

	// Build LLM messages: system prompt + user message
	messages := m.buildInitialMessages(text)

	// Call LLM
	raw, err := m.llm.DoSimpleLLMRequest(messages, llmIntentTimeout)
	if err != nil {
		return nil, fmt.Errorf("intent understanding LLM call: %w", err)
	}

	// Parse LLM response
	reply, intent, _, parseOK := parseLLMIntentResponse(raw)

	// JSON parse failure — LLM returned malformed output. Don't silently
	// reject as "not a workflow". Fall through to normal agent loop with
	// the raw text so the user at least sees something, and log the issue.
	if !parseOK {
		return &StartResult{Rejected: true}, nil
	}

	// One-shot rejection: if LLM says category="none", this is not a workflow.
	// Don't create a session — let the caller fall through to normal agent loop.
	if intent.Category == WorkflowNone {
		return &StartResult{Rejected: true}, nil
	}

	// Empty category with successful parse means the LLM omitted the field.
	// Treat as rejection rather than creating a broken session.
	if intent.Category == "" {
		return &StartResult{Rejected: true}, nil
	}

	// It's a workflow task — create session
	sess := &UnderstandingSession{
		ID:        fmt.Sprintf("iu-%s-%d", userID, now.UnixMilli()),
		UserID:    userID,
		Intent:    intent,
		Rounds:    nil,
		State:     UnderstandingActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
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

	return &StartResult{Reply: reply}, nil
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
	replyText, parsedIntent, isReady, _ := parseLLMIntentResponse(raw)

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
// The LLM must decide TWO things in one shot:
//   1. Is this a workflow task at all? (category="none" if not)
//   2. If yes, which workflow template? (category=template type)
func (m *IntentUnderstandingManager) buildSystemPrompt() string {
	var b strings.Builder
	b.Grow(4096) // pre-allocate to avoid repeated buffer growth

	b.WriteString("你是一个智能助手的意图理解模块。你的任务是判断用户消息是否需要启动一个多阶段工作流，如果需要，将其分类到合适的工作流类型。\n\n")

	b.WriteString("## 核心判断：是否需要工作流\n\n")
	b.WriteString("工作流适用于需要多阶段、多文档产出的复杂任务。以下情况 **不需要** 工作流（category 设为 \"none\"）：\n")
	b.WriteString("- 简单指令：翻译、格式化、总结、搜索、计算、纠错、润色等一步完成的任务\n")
	b.WriteString("- 知识查询：什么是X、怎么理解X、X和Y的区别、推荐/建议类问题\n")
	b.WriteString("- 闲聊/确认：打招呼、感谢、确认、简短回复\n")
	b.WriteString("- 单文件操作：写一段代码片段、生成一个配置文件、写一封邮件\n")
	b.WriteString("- 文件操作：打开、查看、预览、截图、转换、导出文件（即使文件类型是PPT/PDF/Word，操作已有文件不是创建新内容）\n\n")
	b.WriteString("以下情况 **需要** 工作流：\n")
	b.WriteString("- 软件开发：开发系统/应用/游戏/工具，需要需求→设计→编码的完整流程\n")
	b.WriteString("- 文档创作：PRD、商业计划书、论文、研报、测试方案等需要多阶段迭代的文档\n")
	b.WriteString("- 分析项目：竞品分析、尽职调查、合规审计、专利分析等需要系统性分析的任务\n")
	b.WriteString("- 策划项目：活动策划、创新方案、项目立项等需要多维度规划的任务\n\n")

	b.WriteString("## 内容处理任务 vs 工作流任务\n\n")
	b.WriteString("这是最容易混淆的区分。请用以下语义测试判断：**用户是要「处理已有内容」还是「创建新的结构化产物」？**\n\n")
	b.WriteString("**内容处理任务（category=\"none\"）**：一次性读取、加工、转换已有内容的任务。输入内容已经存在，任务是将其处理成另一种形式。\n")
	b.WriteString("- 典型动作：翻译、摘要、解读、整理、格式转换、字幕处理、文档梳理、资料收集\n")
	b.WriteString("- 关键特征：输入已存在 → 单次处理 → 输出结果\n")
	b.WriteString("- 例：\"看HF论文做摘要\" = 读取已有论文 → 生成摘要（内容处理）\n")
	b.WriteString("- 例：\"把报告翻译成英文\" = 读取已有报告 → 翻译输出（内容处理）\n\n")
	b.WriteString("**工作流任务（需要工作流）**：多阶段项目，创建全新的结构化文档，需要经过规划、起草、迭代修改等多个阶段。\n")
	b.WriteString("- 典型动作：写文献综述、开发系统、做商业计划书、写研究报告\n")
	b.WriteString("- 关键特征：从零创建 → 多阶段迭代 → 产出新的结构化文档\n")
	b.WriteString("- 例：\"帮我写一篇文献综述\" = 从零创建学术文档 → 多阶段迭代（工作流）\n")
	b.WriteString("- 例：\"开发一个贪吃蛇游戏\" = 从零创建软件 → 需求→设计→编码（工作流）\n\n")
	b.WriteString("**判断口诀**：\"看/读/翻译/整理/摘要/解读\" 已有内容 → category=\"none\"；\"写/做/开发/创建\" 新产物 → 对应工作流类型\n")
	b.WriteString("**PPT 特别注意**：\"打开/查看/转换/截图 PPT\" → category=\"none\"（文件操作）；\"设计/制作/生成 PPT\" → category=\"presentation_design\"（创建新PPT）\n\n")

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
	b.WriteString("1. 首先判断用户消息是否需要工作流（不需要则 category=\"none\"）\n")
	b.WriteString("2. 如果需要，分析属于哪种工作流类型\n")
	b.WriteString("3. 提取用户的目标、约束条件和开放性问题\n")
	b.WriteString("4. 如果信息不足，通过追问澄清需求\n")
	b.WriteString("5. 通过语义分析判断用户是否已经准备好开始执行（ready）\n")
	b.WriteString("6. 每轮回复末尾加上提示：确定了就告诉我\"开工\"\n\n")

	b.WriteString("## 输出格式\n\n")
	b.WriteString("请严格以 JSON 格式输出，不要包含其他文本：\n")
	b.WriteString("```json\n")
	b.WriteString("{\n")
	b.WriteString("  \"intent\": {\n")
	b.WriteString("    \"category\": \"工作流类型（如 coding, product_design, none 等）\",\n")
	b.WriteString("    \"summary\": \"用户意图的一句话摘要\",\n")
	b.WriteString("    \"goals\": [\"目标1\", \"目标2\"],\n")
	b.WriteString("    \"constraints\": [\"约束1\", \"约束2\"],\n")
	b.WriteString("    \"open_questions\": [\"待澄清问题1\"],\n")
	b.WriteString("    \"confidence\": 0.8,\n")
	b.WriteString("    \"ready\": false\n")
	b.WriteString("  },\n")
	b.WriteString("  \"reply\": \"你对用户说的话\",\n")
	b.WriteString("  \"ready\": false\n")
	b.WriteString("}\n")
	b.WriteString("```\n\n")

	b.WriteString("## category 判断规则\n\n")
	b.WriteString("- category=\"none\"：不需要工作流的简单任务（翻译/查询/闲聊/单步操作）。reply 可留空或写简短说明。\n")
	b.WriteString("- category=\"coding\"：软件开发任务\n")
	b.WriteString("- category=\"product_design\"：产品设计/PRD 任务\n")
	b.WriteString("- category=\"business_plan\"：商业计划任务\n")
	b.WriteString("- category=\"bid_response\"：招投标/投标响应任务（需要用户上传招标文件）\n")
	b.WriteString("- category=\"contract_review\"：合同审查任务（需要用户上传合同）\n")
	b.WriteString("- category=\"due_diligence\"：尽职调查任务（需要用户提供公司资料）\n")
	b.WriteString("- category=\"compliance_audit\"：合规审计任务（需要用户提供审计资料）\n")
	b.WriteString("- category=\"patent_analysis\"：专利分析任务（需要用户上传技术方案或专利文献）\n")
	b.WriteString("- 其他类型参见上方「可用的工作流类型」列表，category 值使用列表中加粗的英文标识\n\n")

	b.WriteString("## 易混淆示例（重要）\n\n")
	b.WriteString("- \"翻译这段英文\" → category=\"none\"（一步完成的简单指令）\n")
	b.WriteString("- \"什么是微服务架构\" → category=\"none\"（知识查询）\n")
	b.WriteString("- \"帮我写一段Python排序代码\" → category=\"none\"（单文件代码片段，不需要完整开发流程）\n")
	b.WriteString("- \"怎么配置nginx\" → category=\"none\"（操作指导类问题）\n")
	b.WriteString("- \"生成一个UUID\" → category=\"none\"（简单生成任务）\n")
	b.WriteString("- \"打开桌面上的PPT文件并截图\" → category=\"none\"（文件操作，不是创建PPT）\n")
	b.WriteString("- \"打开这个PPT\" → category=\"none\"（文件操作）\n")
	b.WriteString("- \"把PPT转换成PDF\" → category=\"none\"（文件格式转换，不是创建PPT）\n")
	b.WriteString("- \"查看这个PPT的内容\" → category=\"none\"（文件查看操作）\n")
	b.WriteString("- \"截图当前PPT页面\" → category=\"none\"（截图操作）\n")
	b.WriteString("- \"开发一个贪吃蛇游戏\" → category=\"coding\"（需要完整开发流程）\n")
	b.WriteString("- \"帮我做一个CRM系统\" → category=\"coding\"（需要完整开发流程）\n")
	b.WriteString("- \"怎么做一个电商系统\" → category=\"coding\"（虽然用了疑问句式，但实际是开发请求）\n")
	b.WriteString("- \"生成网络安全产品的PRD文档\" → category=\"product_design\"（多阶段文档任务）\n")
	b.WriteString("- \"帮我做一份商业计划书\" → category=\"business_plan\"（多阶段文档任务）\n")
	b.WriteString("- \"做一份竞品分析\" → category=\"competitive_analysis\"（系统性分析任务）\n")
	b.WriteString("- \"帮我分析这个招标文件，准备投标\" → category=\"bid_response\"（需要上传招标文件的投标响应任务）\n")
	b.WriteString("- \"审查一下这个合同\" → category=\"contract_review\"（需要上传合同的审查任务）\n")
	b.WriteString("- \"对这家公司做个尽调\" → category=\"due_diligence\"（需要提供公司资料的尽职调查）\n")
	b.WriteString("- \"检查一下我们的数据合规情况\" → category=\"compliance_audit\"（需要提供审计资料的合规审计）\n")
	b.WriteString("- \"分析一下这个专利的侵权风险\" → category=\"patent_analysis\"（需要上传专利/技术方案的专利分析）\n")
	b.WriteString("- \"看HF论文做摘要\" → category=\"none\"（读取已有论文并生成摘要 = 内容处理）\n")
	b.WriteString("- \"帮我写一篇文献综述\" → category=\"literature_review\"（从零创建学术文档 = 工作流）\n")
	b.WriteString("- \"把这份报告翻译成英文\" → category=\"none\"（翻译已有内容 = 内容处理）\n")
	b.WriteString("- \"帮我写一份研究报告\" → category=\"research_report\"（从零创建研究报告 = 工作流）\n")
	b.WriteString("- \"整理这些会议纪要\" → category=\"none\"（整理已有内容 = 内容处理）\n")
	b.WriteString("- \"解读这篇论文的核心观点\" → category=\"none\"（解读已有内容 = 内容处理）\n")
	b.WriteString("- \"帮我写一篇论文\" → category=\"paper_writing\"（从零创建学术论文 = 工作流）\n\n")

	b.WriteString("## ready 判断规则\n\n")
	b.WriteString("- ready=true：用户明确表示可以开始了（如\"开工\"、\"开始吧\"、\"可以了\"、\"没问题了\"、\"就这样\"），且你对意图的理解已经足够清晰\n")
	b.WriteString("- ready=false：用户还在补充信息、提出新需求、或者虽然说了类似\"开始\"但实际在补充需求\n")
	b.WriteString("- 通过语义分析综合判断，不要仅匹配关键词\n\n")

	b.WriteString("## 易混淆 ready 示例\n\n")
	b.WriteString("- \"开始吧\" → ready=true\n")
	b.WriteString("- \"开工\" → ready=true\n")
	b.WriteString("- \"就这样吧\" → ready=true\n")
	b.WriteString("- \"开始我觉得还需要加个登录功能\" → ready=false（在补充需求）\n")
	b.WriteString("- \"可以了，不过能不能再加个导出功能\" → ready=false（有追加需求）\n")
	b.WriteString("- \"差不多了，但是安全方面还要考虑一下\" → ready=false（有未解决的顾虑）\n")

	return b.String()
}

// llmIntentResult is the expected JSON structure from the LLM response.
type llmIntentResult struct {
	Intent StructuredIntent `json:"intent"`
	Reply  string           `json:"reply"`
	Ready  bool             `json:"ready"`
}

// parseLLMIntentResponse parses the LLM's JSON response.
// Returns the reply text, structured intent, top-level ready flag, and
// whether JSON parsing succeeded.
func parseLLMIntentResponse(raw string) (reply string, intent StructuredIntent, ready bool, parseOK bool) {
	// Try to extract JSON from the response (may be wrapped in markdown code block)
	jsonStr := extractJSON(raw)

	var result llmIntentResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Parse failed — return raw text as reply but signal failure
		return strings.TrimSpace(raw), StructuredIntent{}, false, false
	}

	// Normalize category: trim whitespace and lowercase to prevent LLM
	// output variations like "None", " none ", "NONE" from bypassing guards.
	result.Intent.Category = WorkflowType(strings.ToLower(strings.TrimSpace(string(result.Intent.Category))))

	return result.Reply, result.Intent, result.Ready, true
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
