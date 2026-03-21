package im

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ReplyMerger uses LLM to merge multiple device replies into a single
// coherent response. Falls back to structured formatting when LLM is
// unavailable.
type ReplyMerger struct {
	configProvider func() *HubLLMConfig
	breaker        *CircuitBreaker
	client         *http.Client
}

// NewReplyMerger creates a new merger.
func NewReplyMerger(configProvider func() *HubLLMConfig, breaker *CircuitBreaker) *ReplyMerger {
	return &ReplyMerger{
		configProvider: configProvider,
		breaker:        breaker,
		client:         &http.Client{Timeout: 15 * time.Second},
	}
}

// DeviceReply holds one device's response in a broadcast.
type DeviceReply struct {
	Name      string
	MachineID string
	Response  *GenericResponse
	Err       error
}

// MergeReplies merges multiple device replies.
// Strategy:
//  1. Single reply → return as-is
//  2. All similar (first 100 chars match) → return first + "其他设备观点一致"
//  3. Multiple different + LLM available → LLM merge
//  4. Multiple different + no LLM → structured format
func (rm *ReplyMerger) MergeReplies(ctx context.Context, replies []DeviceReply) (*GenericResponse, error) {
	// Filter successful text replies.
	var good []DeviceReply
	var errors []DeviceReply
	for _, r := range replies {
		if r.Err != nil || r.Response == nil {
			errors = append(errors, r)
		} else {
			good = append(good, r)
		}
	}

	if len(good) == 0 {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "❌",
			Title:      "全部失败",
			Body:       "所有设备均未返回有效回复。",
		}, nil
	}

	if len(good) == 1 {
		resp := good[0].Response
		if len(errors) > 0 {
			resp.Body += fmt.Sprintf("\n\n⚠️ %d 台设备未回复", len(errors))
		}
		return resp, nil
	}

	// Check similarity.
	if rm.areSimilar(good) {
		resp := good[0].Response
		resp.Body += fmt.Sprintf("\n\n✅ 其他 %d 台设备观点一致", len(good)-1)
		if len(errors) > 0 {
			resp.Body += fmt.Sprintf("\n⚠️ %d 台设备未回复", len(errors))
		}
		return resp, nil
	}

	// Try LLM merge.
	cfg := rm.configProvider()
	if cfg != nil && cfg.Enabled && rm.breaker.Allow() {
		merged := rm.llmMerge(ctx, good, cfg)
		if merged != "" {
			body := merged
			body += fmt.Sprintf("\n\n📊 统计: %d 台设备回复", len(good))
			if len(errors) > 0 {
				body += fmt.Sprintf(", %d 台未回复", len(errors))
			}
			return &GenericResponse{
				StatusCode: 200,
				StatusIcon: "📢",
				Title:      "合并回复",
				Body:       body,
			}, nil
		}
	}

	// Fallback: structured format.
	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "📢",
		Title:      "群聊回复",
		Body:       FormatBroadcastReply(replies),
	}, nil
}

func (rm *ReplyMerger) areSimilar(replies []DeviceReply) bool {
	return areSimilarReplies(replies)
}

func (rm *ReplyMerger) llmMerge(ctx context.Context, replies []DeviceReply, cfg *HubLLMConfig) string {
	var b strings.Builder
	b.WriteString("以下是多台设备对同一问题的回复，请合并为一份整合回复：\n")
	b.WriteString("要求：去除重复内容、保留各设备独特观点、标注观点来源设备名称、末尾列出关键差异。\n\n")
	for _, r := range replies {
		fmt.Fprintf(&b, "[%s]:\n%s\n\n", r.Name, r.Response.Body)
	}

	messages := []interface{}{
		map[string]string{"role": "user", "content": b.String()},
	}

	llmCfg := cfg.ToMaclawLLMConfig()
	resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, rm.client, 10*time.Second)
	if err != nil {
		log.Printf("[ReplyMerger] LLM merge error: %v", err)
		rm.breaker.RecordFailure()
		return ""
	}
	rm.breaker.RecordSuccess()
	return resp.Content
}
