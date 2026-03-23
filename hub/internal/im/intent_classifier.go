package im

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// IntentType is the LLM intent classification result.
type IntentType string

const (
	IntentRouteSingle       IntentType = "route_single"
	IntentBroadcast         IntentType = "broadcast"
	IntentDiscuss           IntentType = "discuss"
	IntentNeedClarification IntentType = "need_clarification"
	IntentDirectAnswer      IntentType = "direct_answer"
)

// IntentResult holds the output of intent classification.
type IntentResult struct {
	Type     IntentType `json:"type"`
	TargetID string     `json:"target_id,omitempty"`
	Topic    string     `json:"topic,omitempty"`
	Reason   string     `json:"reason"`
	Message  string     `json:"message,omitempty"`
}

// routeHistoryEntry records a recent routing decision for context continuity.
type routeHistoryEntry struct {
	Text   string
	Target string
	Reason string
}

// IntentClassifier uses an LLM to classify user messages into routing intents.
type IntentClassifier struct {
	configProvider func() *HubLLMConfig
	breaker        *CircuitBreaker
	llmSem         *LLMSemaphore
	client         *http.Client

	mu    sync.Mutex
	cache []intentCacheEntry
}

type intentCacheEntry struct {
	UserID     string
	MachineSet string // sorted machineIDs joined
	TextHash   uint64
	Result     IntentResult
	CreatedAt  time.Time
}

const (
	intentCacheMax = 10
	intentCacheTTL = 5 * time.Minute
	classifyTimeout = 5 * time.Second
)

// NewIntentClassifier creates a new classifier.
func NewIntentClassifier(configProvider func() *HubLLMConfig, breaker *CircuitBreaker, llmSem *LLMSemaphore) *IntentClassifier {
	return &IntentClassifier{
		configProvider: configProvider,
		breaker:        breaker,
		llmSem:         llmSem,
		client:         &http.Client{Timeout: 10 * time.Second},
	}
}

// Classify performs LLM intent classification on a user message.
// On timeout (5s) or error, it degrades to broadcast intent.
// convRounds provides recent conversation context for continuity.
func (ic *IntentClassifier) Classify(
	ctx context.Context,
	userID, text string,
	profiles []DeviceProfile,
	recentHistory []routeHistoryEntry,
	convRounds ...[]ConversationRound,
) (*IntentResult, error) {
	// Check cache first.
	machineSet := buildMachineSet(profiles)
	textHash := hashText(text)
	if cached := ic.lookupCache(userID, machineSet, textHash); cached != nil {
		return cached, nil
	}

	// Build the LLM prompt.
	cfg := ic.configProvider()
	if cfg == nil {
		return &IntentResult{Type: IntentBroadcast, Reason: "LLM 未配置，降级广播"}, nil
	}

	prompt := ic.buildPrompt(text, profiles, recentHistory, convRounds...)
	messages := []interface{}{
		map[string]string{"role": "system", "content": intentSystemPrompt},
		map[string]string{"role": "user", "content": prompt},
	}

	llmCfg := cfg.ToMaclawLLMConfig()

	// Use a sub-context with 5s timeout.
	classifyCtx, cancel := context.WithTimeout(ctx, classifyTimeout)
	defer cancel()

	// Acquire LLM semaphore; degrade to broadcast on timeout.
	// Semaphore is acquired inside the goroutine so Release happens only
	// after the LLM call completes, not when the caller times out.

	// Channel-based call so we can respect the context timeout.
	type llmResult struct {
		resp *agent.LLMSimpleResponse
		err  error
	}
	ch := make(chan llmResult, 1)
	go func() {
		if !ic.llmSem.Acquire(classifyCtx) {
			ch <- llmResult{nil, fmt.Errorf("LLM semaphore timeout")}
			return
		}
		defer ic.llmSem.Release()
		resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, ic.client, classifyTimeout)
		ch <- llmResult{resp, err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			log.Printf("[IntentClassifier] LLM error: %v", res.err)
			ic.breaker.RecordFailure()
			return &IntentResult{Type: IntentBroadcast, Reason: "LLM 调用失败，降级广播"}, nil
		}
		ic.breaker.RecordSuccess()
		result := ic.parseResult(res.resp.Content, profiles)
		ic.addToCache(userID, machineSet, textHash, result)
		return result, nil

	case <-classifyCtx.Done():
		log.Printf("[IntentClassifier] LLM timeout for user=%s", userID)
		ic.breaker.RecordFailure()
		return &IntentResult{Type: IntentBroadcast, Reason: "LLM 超时，降级广播"}, nil
	}
}

// parseResult extracts an IntentResult from the LLM's JSON response.
// Falls back to broadcast on parse failure.
func (ic *IntentClassifier) parseResult(content string, profiles []DeviceProfile) *IntentResult {
	// Strip markdown code fences if present.
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			content = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var result IntentResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		log.Printf("[IntentClassifier] JSON parse failed: %v, raw: %s", err, truncate(content, 200))
		return &IntentResult{Type: IntentBroadcast, Reason: "LLM 返回格式错误，降级广播"}
	}

	// Validate route_single has a valid target.
	if result.Type == IntentRouteSingle && result.TargetID == "" {
		// Try to match by name in reason.
		return &IntentResult{Type: IntentBroadcast, Reason: "route_single 缺少 target_id，降级广播"}
	}

	// For route_single, resolve target_id: LLM might return a name instead of ID.
	if result.Type == IntentRouteSingle {
		resolved := false
		for _, p := range profiles {
			if p.MachineID == result.TargetID || strings.EqualFold(p.Name, result.TargetID) {
				result.TargetID = p.MachineID
				resolved = true
				break
			}
		}
		if !resolved {
			return &IntentResult{Type: IntentBroadcast, Reason: fmt.Sprintf("目标设备 %q 未找到，降级广播", result.TargetID)}
		}
	}

	return &result
}

func (ic *IntentClassifier) buildPrompt(text string, profiles []DeviceProfile, history []routeHistoryEntry, convRounds ...[]ConversationRound) string {
	var b strings.Builder
	b.WriteString("在线设备：\n")
	for _, p := range profiles {
		fmt.Fprintf(&b, "- %s (ID=%s): 项目=%s, 语言=%s, 框架=%s",
			p.Name, p.MachineID, p.ProjectPath, p.Language, p.Framework)
		if len(p.ActiveSessions) > 0 {
			fmt.Fprintf(&b, ", 活跃Session=%s", strings.Join(p.ActiveSessions, ","))
		}
		b.WriteString("\n")
	}

	// Inject conversation context if available.
	if len(convRounds) > 0 && len(convRounds[0]) > 0 {
		b.WriteString("\n最近对话上下文：\n")
		for _, r := range convRounds[0] {
			fmt.Fprintf(&b, "- [%s] 用户: %s → 摘要: %s\n", r.DeviceName, truncate(r.UserText, 50), truncate(r.Summary, 60))
		}
	}

	if len(history) > 0 {
		b.WriteString("\n最近路由历史：\n")
		for _, h := range history {
			fmt.Fprintf(&b, "- \"%s\" → %s (%s)\n", truncate(h.Text, 50), h.Target, h.Reason)
		}
	}

	fmt.Fprintf(&b, "\n用户消息：%s", text)
	return b.String()
}

const intentSystemPrompt = `你是一个消息路由助手。用户有多台开发设备在线，你需要根据消息内容和设备信息判断消息应该发给谁。

请以 JSON 格式返回分类结果，type 为以下之一：
- "route_single": 发给指定设备（需提供 target_id 和 reason）
- "broadcast": 广播到所有设备（需提供 reason）
- "discuss": 发起多设备讨论（需提供 topic 和 reason）
- "need_clarification": 无法判断（需提供 message 提示用户）
- "direct_answer": Hub 直接回答（问题不需要访问用户设备的项目代码/文件/工具）

判断规则：
- 如果问题是通用编程知识、语法问题、正则表达式、概念解释等，选 direct_answer
- 如果问题涉及"我的项目"、"这个文件"、"帮我改"、"跑一下"等需要设备操作的，不选 direct_answer
- 如果消息明显与某台设备的项目/语言/框架相关，选 route_single
- 如果消息是通用问题或需要多视角，选 broadcast
- 如果消息包含"讨论"、"大家看看"、"对比"等协作意图，选 discuss
- 如果消息太模糊无法判断，选 need_clarification
- 如果用户消息是对上一轮对话的延续（包含指代词如"这个"、"上面的"、"继续"、"再改改"，或省略主语），优先路由到上一轮的目标设备

仅返回 JSON，不要其他内容。`

// --- cache helpers ---

func (ic *IntentClassifier) lookupCache(userID, machineSet string, textHash uint64) *IntentResult {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	now := time.Now()
	// Compact: remove expired entries while searching.
	n := 0
	var found *IntentResult
	for _, e := range ic.cache {
		if now.Sub(e.CreatedAt) >= intentCacheTTL {
			continue // expired, drop
		}
		ic.cache[n] = e
		n++
		if found == nil && e.UserID == userID && e.MachineSet == machineSet && e.TextHash == textHash {
			r := e.Result
			found = &r
		}
	}
	ic.cache = ic.cache[:n]
	return found
}

func (ic *IntentClassifier) addToCache(userID, machineSet string, textHash uint64, result *IntentResult) {
	ic.mu.Lock()
	defer ic.mu.Unlock()
	if len(ic.cache) >= intentCacheMax {
		ic.cache = ic.cache[1:]
	}
	ic.cache = append(ic.cache, intentCacheEntry{
		UserID:     userID,
		MachineSet: machineSet,
		TextHash:   textHash,
		Result:     *result,
		CreatedAt:  time.Now(),
	})
}

func buildMachineSet(profiles []DeviceProfile) string {
	ids := make([]string, len(profiles))
	for i, p := range profiles {
		ids[i] = p.MachineID
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func hashText(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}
