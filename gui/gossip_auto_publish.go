package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ── Content sanitization ────────────────────────────────────────────────

var (
	// Unix paths: /home/user/project, /xxx/xxx etc.
	reUnixPath = regexp.MustCompile(`/[a-zA-Z0-9_.\-]+(/[a-zA-Z0-9_.\-]+)+`)
	// Windows paths: C:\Users\xxx, D:\path\to\file etc.
	reWinPath = regexp.MustCompile(`[A-Za-z]:\\[^\s]+`)
	// Email addresses: user@example.com
	reEmail = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	// IP addresses: 192.168.1.1
	reIP = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
)

// reMultiSpace collapses runs of whitespace left after sanitization.
var reMultiSpace = regexp.MustCompile(`\s{2,}`)

// sanitizeContent 对内容进行脱敏处理，移除文件路径、邮箱地址和 IP 地址。
func sanitizeContent(content string) string {
	content = reWinPath.ReplaceAllString(content, "")
	content = reUnixPath.ReplaceAllString(content, "")
	content = reEmail.ReplaceAllString(content, "")
	content = reIP.ReplaceAllString(content, "")
	content = reMultiSpace.ReplaceAllString(content, " ")
	return strings.TrimSpace(content)
}

// ── AutoPublishTrigger ──────────────────────────────────────────────────

// AutoPublishTrigger 在特定事件发生时自动生成并发布 Gossip 内容。
type AutoPublishTrigger struct {
	mu          sync.Mutex
	client      *GossipClient
	lastPublish time.Time  // 冷却计时
	enabled     func() bool // 动态读取 gossip_auto_publish 配置
}

// NewAutoPublishTrigger 创建自动发布触发器。
func NewAutoPublishTrigger(client *GossipClient, enabledFn func() bool) *AutoPublishTrigger {
	return &AutoPublishTrigger{
		client:  client,
		enabled: enabledFn,
	}
}

// OnSkillUploaded 当 Skill 上传成功时触发，生成 category="news" 的帖子。
func (t *AutoPublishTrigger) OnSkillUploaded(skillName, description string) {
	if strings.TrimSpace(skillName) == "" {
		log.Printf("[gossip-auto] skipping publish: empty skill name")
		return
	}
	content := fmt.Sprintf("🎉 新 Skill 上架: %s — %s", skillName, description)
	t.tryPublish(content, "news")
}

// OnSessionCompleted 当编码会话完成时触发。仅当 durationMin > 5 时生成 category="project" 的帖子。
func (t *AutoPublishTrigger) OnSessionCompleted(sessionSummary string, durationMin int) {
	if durationMin <= 5 {
		return
	}
	sanitized := sanitizeContent(sessionSummary)
	if sanitized == "" {
		log.Printf("[gossip-auto] skipping publish: session summary empty after sanitization")
		return
	}
	content := fmt.Sprintf("💻 编码会话完成 (%d分钟): %s", durationMin, sanitized)
	t.tryPublish(content, "project")
}

// tryPublish 内部方法：检查启用状态和冷却间隔，然后调用 GossipClient 发布。
func (t *AutoPublishTrigger) tryPublish(content, category string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.enabled() {
		return
	}

	if time.Since(t.lastPublish) < 10*time.Minute {
		log.Printf("[gossip-auto] cooldown active, skipping publish")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 无论成功失败都更新冷却时间，避免连续失败时反复重试
	t.lastPublish = time.Now()

	_, err := t.client.PublishPost(ctx, content, category)
	if err != nil {
		log.Printf("[gossip-auto] publish failed: %v", err)
		return
	}
}
