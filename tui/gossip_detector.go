package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

const (
	tuiChatBufferMax     = 10
	tuiChatTriggerRounds = 3
	tuiCooldownDuration  = 10 * time.Minute
)

const tuiGossipDetectPrompt = `你是一个八卦嗅探器。以下是一段人机对话记录，请判断其中是否有有趣、好玩、值得分享的八卦内容。

规则：
1. 只关注真正有趣、搞笑、吐槽、八卦的内容，普通技术问答不算
2. 如果有趣，提取精华写成一条适合发到社区的帖子（100-300字，口语化，可以加emoji）
3. 帖子内容不要包含任何文件路径、邮箱、IP地址等敏感信息
4. 用 JSON 格式回复：{"interesting": true, "post": "帖子内容"} 或 {"interesting": false}
5. 只输出 JSON，不要输出其他内容

对话记录：
`

var (
	tuiReUnixPath  = regexp.MustCompile(`/[a-zA-Z0-9_.\-]+(/[a-zA-Z0-9_.\-]+)+`)
	tuiReWinPath   = regexp.MustCompile(`[A-Za-z]:\\[^\s]+`)
	tuiReEmail     = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	tuiReIP        = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	tuiReMultiSpace = regexp.MustCompile(`\s{2,}`)
)

func tuiSanitize(content string) string {
	content = tuiReWinPath.ReplaceAllString(content, "")
	content = tuiReUnixPath.ReplaceAllString(content, "")
	content = tuiReEmail.ReplaceAllString(content, "")
	content = tuiReIP.ReplaceAllString(content, "")
	content = tuiReMultiSpace.ReplaceAllString(content, " ")
	return strings.TrimSpace(content)
}

type tuiChatExchange struct {
	User      string
	Assistant string
}

// TUIGossipDetector 是 TUI 端的聊天八卦检测器。
type TUIGossipDetector struct {
	mu          sync.Mutex
	buffer      []tuiChatExchange
	roundsSince int
	lastPublish time.Time
	httpClient  *http.Client
	// 缓存配置，避免每轮都读磁盘
	cachedCfg     *corelib.AppConfig
	cfgCachedAt   time.Time
}

func NewTUIGossipDetector() *TUIGossipDetector {
	return &TUIGossipDetector{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// OnChatCompleted 每轮对话完成后调用。
func (d *TUIGossipDetector) OnChatCompleted(userMsg, assistantReply string) {
	// 检查是否启用（缓存配置 1 分钟，避免每轮读磁盘）
	d.mu.Lock()
	var cfg corelib.AppConfig
	if d.cachedCfg != nil && time.Since(d.cfgCachedAt) < time.Minute {
		cfg = *d.cachedCfg
	} else {
		d.mu.Unlock()
		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		loaded, err := store.LoadConfig()
		if err != nil {
			return
		}
		d.mu.Lock()
		d.cachedCfg = &loaded
		d.cfgCachedAt = time.Now()
		cfg = loaded
	}

	if !cfg.GossipAutoPublish {
		d.mu.Unlock()
		return
	}
	d.buffer = append(d.buffer, tuiChatExchange{User: userMsg, Assistant: assistantReply})
	if len(d.buffer) > tuiChatBufferMax {
		d.buffer = d.buffer[len(d.buffer)-tuiChatBufferMax:]
	}
	d.roundsSince++

	if d.roundsSince < tuiChatTriggerRounds {
		d.mu.Unlock()
		return
	}

	if time.Since(d.lastPublish) < tuiCooldownDuration {
		d.roundsSince = 0
		d.mu.Unlock()
		return
	}

	bufCopy := make([]tuiChatExchange, len(d.buffer))
	copy(bufCopy, d.buffer)
	d.roundsSince = 0
	d.mu.Unlock()

	go d.detectAndPublish(bufCopy, cfg)
}

// ClearBuffer 清空对话缓冲区（用于清除聊天历史时同步清空）。
func (d *TUIGossipDetector) ClearBuffer() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.buffer = nil
	d.roundsSince = 0
}

func (d *TUIGossipDetector) detectAndPublish(exchanges []tuiChatExchange, appCfg corelib.AppConfig) {
	llmCfg, err := commands.LoadLLMConfig()
	if err != nil || llmCfg.URL == "" || llmCfg.Model == "" {
		return
	}

	var sb strings.Builder
	for i, ex := range exchanges {
		sb.WriteString("用户: ")
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
		map[string]string{"role": "user", "content": tuiGossipDetectPrompt + sb.String()},
	}

	resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, d.httpClient, 30*time.Second)
	if err != nil {
		log.Printf("[tui-gossip] LLM detection failed: %v", err)
		return
	}

	content := strings.TrimSpace(resp.Content)
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
		log.Printf("[tui-gossip] parse LLM response failed: %v", err)
		return
	}

	if !result.Interesting || strings.TrimSpace(result.Post) == "" {
		return
	}

	post := tuiSanitize(result.Post)
	if post == "" {
		return
	}

	d.mu.Lock()
	d.lastPublish = time.Now()
	d.buffer = nil
	d.mu.Unlock()

	// 直接 HTTP 调用 HubCenter 发布
	if err := d.publishToHubCenter(appCfg, post); err != nil {
		log.Printf("[tui-gossip] publish failed: %v", err)
	} else {
		log.Printf("[tui-gossip] published gossip: %s", truncateRunes(post, 100))
	}
}

func (d *TUIGossipDetector) publishToHubCenter(cfg corelib.AppConfig, content string) error {
	baseURL := strings.TrimSpace(cfg.RemoteHubCenterURL)
	if baseURL == "" {
		baseURL = "http://hubs.mypapers.top:9388"
	}
	machineID := strings.TrimSpace(cfg.RemoteMachineID)
	if machineID == "" {
		return fmt.Errorf("machine_id not configured")
	}

	payload, _ := json.Marshal(map[string]string{
		"machine_id": machineID,
		"user_email": strings.TrimSpace(cfg.RemoteEmail),
		"content":    content,
		"category":   "gossip",
	})

	reqURL := strings.TrimRight(baseURL, "/") + "/api/gossip/publish"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx, "POST",
		reqURL, bytes.NewReader(payload),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Machine-ID", url.QueryEscape(machineID))

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
