// gui/im_pending_media.go — Pending media buffer for IM message handler.
//
// When a user sends an image/file without accompanying text, the buffer
// holds the attachments for up to 10 seconds, waiting for a follow-up
// text message that describes the user's intent. If no text arrives
// within the timeout, a prompt is sent via the onProgress callback
// asking the user what they want to do with the media.
//
// Multiple attachments arriving within the window are merged; each new
// arrival resets the 10-second timer.
package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

const pendingMediaTimeout = 10 * time.Second

// pendingMediaStaleTimeout is the maximum time an entry can remain in the
// buffer after the timer has fired. If the user never sends a follow-up
// text, the entry is garbage-collected to prevent memory leaks.
const pendingMediaStaleTimeout = 5 * time.Minute

// pendingMediaEntry holds buffered attachments for a single user.
type pendingMediaEntry struct {
	attachments []MessageAttachment
	timer       *time.Timer
	onProgress  ProgressCallback // stored so the timeout goroutine can notify the user
	createdAt   time.Time        // for stale entry cleanup
}

// pendingMediaBuffer is a per-user buffer for media-only messages.
type pendingMediaBuffer struct {
	mu      sync.Mutex
	entries map[string]*pendingMediaEntry // keyed by userID
}

func newPendingMediaBuffer() *pendingMediaBuffer {
	b := &pendingMediaBuffer{
		entries: make(map[string]*pendingMediaEntry),
	}
	go b.cleanupLoop()
	return b
}

// Add appends attachments for userID and (re)starts the timeout timer.
// Returns true if the media was buffered (caller should return Deferred).
func (b *pendingMediaBuffer) Add(userID string, attachments []MessageAttachment, onProgress ProgressCallback) bool {
	if len(attachments) == 0 {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, exists := b.entries[userID]
	if exists {
		// Accumulate and reset timer.
		entry.attachments = append(entry.attachments, attachments...)
		entry.createdAt = time.Now() // reset stale clock on new arrivals
		if entry.onProgress == nil && onProgress != nil {
			entry.onProgress = onProgress
		}
		if entry.timer != nil {
			entry.timer.Reset(pendingMediaTimeout)
		} else {
			// Timer already fired; restart it for the new batch.
			entry.timer = time.AfterFunc(pendingMediaTimeout, func() {
				b.onTimeout(userID)
			})
		}
		log.Printf("[PendingMedia] user=%s accumulated %d attachments (total %d)", userID, len(attachments), len(entry.attachments))
		return true
	}

	// New entry.
	entry = &pendingMediaEntry{
		attachments: append([]MessageAttachment(nil), attachments...),
		onProgress:  onProgress,
		createdAt:   time.Now(),
	}
	entry.timer = time.AfterFunc(pendingMediaTimeout, func() {
		b.onTimeout(userID)
	})
	b.entries[userID] = entry
	log.Printf("[PendingMedia] user=%s buffered %d attachments, waiting %v for intent", userID, len(attachments), pendingMediaTimeout)
	return true
}

// Drain removes and returns any pending attachments for userID.
// Returns nil if nothing is pending.
func (b *pendingMediaBuffer) Drain(userID string) []MessageAttachment {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, exists := b.entries[userID]
	if !exists {
		return nil
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	atts := entry.attachments
	delete(b.entries, userID)
	log.Printf("[PendingMedia] user=%s drained %d attachments", userID, len(atts))
	return atts
}

// cleanupLoop periodically removes stale entries that were never drained.
func (b *pendingMediaBuffer) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		b.mu.Lock()
		now := time.Now()
		for uid, entry := range b.entries {
			if now.Sub(entry.createdAt) > pendingMediaStaleTimeout {
				if entry.timer != nil {
					entry.timer.Stop()
				}
				delete(b.entries, uid)
				log.Printf("[PendingMedia] user=%s stale entry removed (%d attachments)", uid, len(entry.attachments))
			}
		}
		b.mu.Unlock()
	}
}

// onTimeout fires when the user hasn't sent a follow-up text within the window.
// The attachments are kept in the buffer so a subsequent Drain can still retrieve
// them — only the timer reference is cleared. A background cleanup goroutine
// removes truly stale entries after a generous grace period.
func (b *pendingMediaBuffer) onTimeout(userID string) {
	b.mu.Lock()
	entry, exists := b.entries[userID]
	if !exists {
		b.mu.Unlock()
		return
	}
	// Keep attachments for a follow-up Drain; just record that the timer fired.
	onProgress := entry.onProgress
	count := len(entry.attachments)
	entry.onProgress = nil // avoid double-notify
	entry.timer = nil      // mark as timed-out
	b.mu.Unlock()

	log.Printf("[PendingMedia] user=%s timeout after %v, %d attachments still buffered", userID, pendingMediaTimeout, count)

	if onProgress == nil {
		return
	}

	// Build a friendly prompt — NOT added to conversation history.
	prompt := buildMediaPrompt(count)
	onProgress(prompt)
}

// buildMediaPrompt creates the timeout prompt shown to the user.
func buildMediaPrompt(count int) string {
	if count == 1 {
		return i18n.T(i18n.MsgMediaSingle, "zh")
	}
	return i18n.Tf(i18n.MsgMediaMultiple, "zh", count)
}

// canInferIntentFromHistory checks the last few conversation entries to see
// if there's enough context to guess what the user wants to do with the media.
// Returns true if the recent conversation suggests a clear intent (e.g. the
// user was discussing code bugs, UI review, etc.).
func canInferIntentFromHistory(entries []conversationEntry) bool {
	// Look at the last 3 user messages for intent signals.
	const lookback = 3
	checked := 0
	for i := len(entries) - 1; i >= 0 && checked < lookback; i-- {
		if entries[i].Role != "user" {
			continue
		}
		checked++
		text, ok := entries[i].Content.(string)
		if !ok {
			continue
		}
		lower := strings.ToLower(text)
		// Pad with spaces for word-boundary matching on short keywords.
		padded := " " + lower + " "
		for _, kw := range intentKeywords {
			if len(kw) <= 3 {
				// Short keywords need word boundaries to avoid false positives
				// (e.g. "log" matching "dialog").
				if strings.Contains(padded, " "+kw+" ") {
					return true
				}
			} else if strings.Contains(lower, kw) {
				return true
			}
		}
	}
	return false
}

// intentKeywords are phrases that suggest the user has an active task context
// where media intent can be inferred (e.g. debugging, UI review, testing).
var intentKeywords = []string{
	// 编程/调试
	"bug", "报错", "error", "错误", "异常", "crash", "日志", "log",
	"代码", "code", "函数", "function", "接口", "api",
	// UI/设计
	"界面", "ui", "设计", "design", "样式", "layout", "页面",
	// 测试
	"测试", "test", "截图", "screenshot",
	// 文件处理
	"分析", "解析", "提取", "ocr", "识别", "翻译",
	// 通用指令上下文
	"帮我看", "看看这个", "检查", "review",
}

// isSynthesizedMediaText returns true if the text was auto-generated by the
// webhook layer for a media-only message (e.g. "[用户发送了文件]").
// These messages should be buffered rather than processed immediately.
func isSynthesizedMediaText(text string) bool {
	return strings.HasPrefix(text, "[用户发送了")
}
