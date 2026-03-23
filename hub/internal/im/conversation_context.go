package im

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	maxConversationRounds = 20
	summaryTimeout        = 3 * time.Second
	summaryMaxChars       = 100
	handoffMaxChars       = 500
)

// ConversationRound records one round of user↔agent interaction.
type ConversationRound struct {
	UserText     string
	Summary      string // LLM-generated or truncated agent reply summary
	TargetDevice string // machineID
	DeviceName   string
	Timestamp    time.Time
}

// ConversationContext is a per-user sliding window of recent conversation rounds.
type ConversationContext struct {
	mu     sync.RWMutex
	rounds []ConversationRound
}

// RecordRound appends a round and asynchronously generates a summary.
func (cc *ConversationContext) RecordRound(userText, responseText, targetDevice, deviceName string, llmCfg *HubLLMConfig, breaker *CircuitBreaker, llmSem *LLMSemaphore) {
	summary := generateSummary(responseText, llmCfg, breaker, llmSem)
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.rounds = append(cc.rounds, ConversationRound{
		UserText:     userText,
		Summary:      summary,
		TargetDevice: targetDevice,
		DeviceName:   deviceName,
		Timestamp:    time.Now(),
	})
	if len(cc.rounds) > maxConversationRounds {
		cc.rounds = cc.rounds[len(cc.rounds)-maxConversationRounds:]
	}
}

// RecordRoundAsync records a round with async summary generation (non-blocking).
func (cc *ConversationContext) RecordRoundAsync(userText, responseText, targetDevice, deviceName string, llmCfg *HubLLMConfig, breaker *CircuitBreaker, llmSem *LLMSemaphore) {
	go func() {
		cc.RecordRound(userText, responseText, targetDevice, deviceName, llmCfg, breaker, llmSem)
	}()
}

// GetRecentSummaries returns the most recent n rounds.
func (cc *ConversationContext) GetRecentSummaries(n int) []ConversationRound {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	if len(cc.rounds) == 0 {
		return nil
	}
	start := len(cc.rounds) - n
	if start < 0 {
		start = 0
	}
	out := make([]ConversationRound, len(cc.rounds)-start)
	copy(out, cc.rounds[start:])
	return out
}

// BuildHandoffContext builds a handoff context string (≤500 chars).
func (cc *ConversationContext) BuildHandoffContext(fromDeviceName, reason string) string {
	recent := cc.GetRecentSummaries(3)
	if len(recent) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "上一台设备: %s\n", fromDeviceName)
	if reason != "" {
		fmt.Fprintf(&b, "切换原因: %s\n", reason)
	}
	b.WriteString("最近对话:\n")
	for _, r := range recent {
		line := fmt.Sprintf("- [%s] %s → %s\n", r.DeviceName, truncate(r.UserText, 40), truncate(r.Summary, 60))
		if b.Len()+len(line) > handoffMaxChars {
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

// Clear removes all rounds.
func (cc *ConversationContext) Clear() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.rounds = nil
}

// Len returns the number of stored rounds.
func (cc *ConversationContext) Len() int {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return len(cc.rounds)
}

// FormatDisplay formats the context for the /context command.
func (cc *ConversationContext) FormatDisplay() string {
	recent := cc.GetRecentSummaries(5)
	if len(recent) == 0 {
		return "📭 暂无对话上下文记录。"
	}
	var b strings.Builder
	b.WriteString("📋 最近对话上下文：\n\n")
	for i, r := range recent {
		ts := r.Timestamp.Format("15:04:05")
		fmt.Fprintf(&b, "%d. [%s] → %s\n   用户: %s\n   摘要: %s\n\n",
			i+1, ts, r.DeviceName, truncate(r.UserText, 50), truncate(r.Summary, 80))
	}
	fmt.Fprintf(&b, "共 %d 轮记录。发送 /context clear 清除。", cc.Len())
	return b.String()
}

// conversationContextStore manages per-user conversation contexts.
type conversationContextStore struct {
	mu   sync.RWMutex
	data map[string]*ConversationContext
}

func newConversationContextStore() *conversationContextStore {
	return &conversationContextStore{data: make(map[string]*ConversationContext)}
}

// GetOrCreate returns the user's conversation context, creating one if absent.
func (s *conversationContextStore) GetOrCreate(userID string) *ConversationContext {
	s.mu.RLock()
	cc := s.data[userID]
	s.mu.RUnlock()
	if cc != nil {
		return cc
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cc = s.data[userID]; cc != nil {
		return cc
	}
	cc = &ConversationContext{}
	s.data[userID] = cc
	return cc
}

// Stats returns the number of active contexts and total rounds.
func (s *conversationContextStore) Stats() (contexts int, totalRounds int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	contexts = len(s.data)
	for _, cc := range s.data {
		totalRounds += cc.Len()
	}
	return
}

// generateSummary creates a short summary of the agent's response.
// Uses LLM if available (3s timeout), otherwise truncates to 100 chars.
func generateSummary(responseText string, llmCfg *HubLLMConfig, breaker *CircuitBreaker, llmSem *LLMSemaphore) string {
	if responseText == "" {
		return "(空回复)"
	}
	if llmCfg == nil || !llmCfg.Enabled || breaker == nil || !breaker.Allow() {
		return truncate(responseText, summaryMaxChars)
	}

	// Acquire LLM semaphore; fall back to truncation on timeout.
	if llmSem != nil {
		ctx, cancel := context.WithTimeout(context.Background(), defaultAcquireTimeout)
		defer cancel()
		if !llmSem.Acquire(ctx) {
			log.Printf("[ConversationContext] semaphore timeout for summary, falling back to truncation")
			return truncate(responseText, summaryMaxChars)
		}
		defer llmSem.Release()
	}

	messages := []interface{}{
		map[string]string{"role": "system", "content": "请用不超过100字概括以下回复的核心内容，只返回摘要文本。"},
		map[string]string{"role": "user", "content": responseText},
	}

	maclawCfg := llmCfg.ToMaclawLLMConfig()
	client := &http.Client{Timeout: summaryTimeout}

	resp, err := agent.DoSimpleLLMRequest(maclawCfg, messages, client, summaryTimeout)
	if err != nil {
		log.Printf("[ConversationContext] summary LLM error: %v", err)
		breaker.RecordFailure()
		return truncate(responseText, summaryMaxChars)
	}
	breaker.RecordSuccess()
	s := strings.TrimSpace(resp.Content)
	if s == "" {
		return truncate(responseText, summaryMaxChars)
	}
	return truncate(s, summaryMaxChars)
}
