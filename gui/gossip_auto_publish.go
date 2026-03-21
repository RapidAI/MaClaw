package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ── Content sanitization ────────────────────────────────────────────────

var (
	reUnixPath   = regexp.MustCompile(`/[a-zA-Z0-9_.\-]+(/[a-zA-Z0-9_.\-]+)+`)
	reWinPath    = regexp.MustCompile(`[A-Za-z]:\\[^\s]+`)
	reEmail      = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	reIP         = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	reMultiSpace = regexp.MustCompile(`\s{2,}`)
)

func sanitizeContent(content string) string {
	content = reWinPath.ReplaceAllString(content, "")
	content = reUnixPath.ReplaceAllString(content, "")
	content = reEmail.ReplaceAllString(content, "")
	content = reIP.ReplaceAllString(content, "")
	content = reMultiSpace.ReplaceAllString(content, " ")
	return strings.TrimSpace(content)
}

// ── Chat exchange buffer ────────────────────────────────────────────────

// ChatExchange 记录一轮用户-助手对话。
type ChatExchange struct {
	User      string
	Assistant string
}

const (
	chatBufferMax     = 10 // 最多保留 10 轮对话
	chatTriggerRounds = 3  // 每累积 3 轮触发一次检测
	cooldownDuration  = 10 * time.Minute
)

// gossipDetectPrompt 是用于判断对话是否有趣的 LLM prompt。
const gossipDetectPrompt = `你是一个八卦嗅探器。以下是一段人机对话记录，请判断其中是否有有趣、好玩、值得分享的八卦内容。

规则：
1. 只关注真正有趣、搞笑、吐槽、八卦的内容，普通技术问答不算
2. 如果有趣，提取精华写成一条适合发到社区的帖子（100-300字，口语化，可以加emoji）
3. 帖子内容不要包含任何文件路径、邮箱、IP地址等敏感信息
4. 用 JSON 格式回复：{"interesting": true, "post": "帖子内容"} 或 {"interesting": false}
5. 只输出 JSON，不要输出其他内容

对话记录：
`

// ── AutoPublishTrigger ──────────────────────────────────────────────────

// AutoPublishTrigger 监听聊天对话，检测有趣内容并自动发布到 Gossip。
type AutoPublishTrigger struct {
	mu          sync.Mutex
	client      *GossipClient
	llmCfgFn    func() corelib.MaclawLLMConfig
	lastPublish time.Time
	enabled     func() bool
	buffer      []ChatExchange
	roundsSince int // 自上次检测以来的轮数
	httpClient  *http.Client
}

// NewAutoPublishTrigger 创建自动发布触发器。
func NewAutoPublishTrigger(client *GossipClient, enabledFn func() bool) *AutoPublishTrigger {
	return &AutoPublishTrigger{
		client:     client,
		enabled:    enabledFn,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetLLMConfigFn 设置获取 LLM 配置的函数（延迟注入，避免初始化循环依赖）。
func (t *AutoPublishTrigger) SetLLMConfigFn(fn func() corelib.MaclawLLMConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.llmCfgFn = fn
}

// OnChatCompleted 每轮对话完成后调用。累积到阈值后触发八卦检测。
func (t *AutoPublishTrigger) OnChatCompleted(userMsg, assistantReply string) {
	t.mu.Lock()

	if !t.enabled() {
		t.mu.Unlock()
		return
	}

	// 追加到 buffer
	t.buffer = append(t.buffer, ChatExchange{
		User:      userMsg,
		Assistant: assistantReply,
	})
	// 超过上限时丢弃最早的
	if len(t.buffer) > chatBufferMax {
		t.buffer = t.buffer[len(t.buffer)-chatBufferMax:]
	}
	t.roundsSince++

	// 未达到触发阈值
	if t.roundsSince < chatTriggerRounds {
		t.mu.Unlock()
		return
	}

	// 冷却期内跳过
	if time.Since(t.lastPublish) < cooldownDuration {
		t.roundsSince = 0 // 重置计数，下次再检测
		t.mu.Unlock()
		log.Printf("[gossip-auto] cooldown active, skipping detection")
		return
	}

	// 拷贝 buffer 用于异步检测
	bufCopy := make([]ChatExchange, len(t.buffer))
	copy(bufCopy, t.buffer)
	llmCfgFn := t.llmCfgFn
	t.roundsSince = 0
	t.mu.Unlock()

	if llmCfgFn == nil {
		log.Printf("[gossip-auto] LLM config not set, skipping detection")
		return
	}

	go t.detectAndPublish(bufCopy, llmCfgFn())
}

// ClearBuffer 清空对话缓冲区（用于清除聊天历史时同步清空）。
func (t *AutoPublishTrigger) ClearBuffer() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buffer = nil
	t.roundsSince = 0
}

// detectAndPublish 用 LLM 判断对话是否有趣，有趣则发帖。
func (t *AutoPublishTrigger) detectAndPublish(exchanges []ChatExchange, cfg corelib.MaclawLLMConfig) {
	if cfg.URL == "" || cfg.Model == "" {
		log.Printf("[gossip-auto] LLM not configured, skipping detection")
		return
	}

	// 构建对话文本
	var sb strings.Builder
	for i, ex := range exchanges {
		sb.WriteString("用户: ")
		// 截断过长的单条消息
		userText := ex.User
		if len([]rune(userText)) > 500 {
			userText = string([]rune(userText)[:500]) + "..."
		}
		sb.WriteString(userText)
		sb.WriteString("\n助手: ")
		assistText := ex.Assistant
		if len([]rune(assistText)) > 500 {
			assistText = string([]rune(assistText)[:500]) + "..."
		}
		sb.WriteString(assistText)
		if i < len(exchanges)-1 {
			sb.WriteString("\n---\n")
		}
	}

	messages := []interface{}{
		map[string]string{"role": "user", "content": gossipDetectPrompt + sb.String()},
	}

	httpClient := t.httpClient
	resp, err := agent.DoSimpleLLMRequest(cfg, messages, httpClient, 30*time.Second)
	if err != nil {
		log.Printf("[gossip-auto] LLM detection failed: %v", err)
		return
	}

	// 解析 JSON 响应
	content := strings.TrimSpace(resp.Content)
	// 尝试提取 JSON（LLM 可能包裹在 markdown code block 中）
	if idx := strings.Index(content, "{"); idx >= 0 {
		if end := strings.LastIndex(content, "}"); end > idx {
			content = content[idx : end+1]
		}
	}

	var result struct {
		Interesting bool   `json:"interesting"`
		Post        string `json:"post"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		log.Printf("[gossip-auto] failed to parse LLM response: %v (raw: %s)", err, gossipTruncateStr(content, 200))
		return
	}

	if !result.Interesting || strings.TrimSpace(result.Post) == "" {
		log.Printf("[gossip-auto] conversation not interesting enough, skipping")
		return
	}

	// 脱敏并发布
	post := sanitizeContent(result.Post)
	if post == "" {
		log.Printf("[gossip-auto] post empty after sanitization, skipping")
		return
	}

	t.mu.Lock()
	t.lastPublish = time.Now()
	t.buffer = nil // 发布后清空 buffer
	t.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = t.client.PublishPost(ctx, post, "gossip")
	if err != nil {
		log.Printf("[gossip-auto] publish failed: %v", err)
		return
	}
	log.Printf("[gossip-auto] published gossip post: %s", gossipTruncateStr(post, 100))
}

// gossipTruncateStr 截断字符串到指定 rune 长度。
func gossipTruncateStr(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
