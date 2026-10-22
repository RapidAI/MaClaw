package corelib

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// ApplyMemberUpstreamBody merges a member's extra_body into the request that
// will be sent upstream, then removes drop_fields. Empty extra and drop
// lists leave the body unchanged.
//
// priority "" and "member" let the member value replace the same key.
// "downstream" keeps a key the request already has and only fills the rest.
// Nested objects merge. model, messages, and stream are left alone.
// Drop runs after the merge, so a listed field is removed even when
// extra_body set it.
func ApplyMemberUpstreamBody(body map[string]any, extra map[string]any, priority string, drop []string) {
	if body == nil {
		return
	}
	if len(extra) > 0 {
		deepMergeMemberBody(body, extra, memberExtraBodyWins(priority))
	}
	for _, field := range drop {
		field = strings.TrimSpace(field)
		if field == "" || memberUpstreamBodyProtected(field) {
			continue
		}
		delete(body, field)
	}
}

func memberExtraBodyWins(priority string) bool {
	return !strings.EqualFold(strings.TrimSpace(priority), "downstream")
}

// FitAnthropicThinkingBudget keeps an enabled thinking budget strictly below
// max_tokens. The output cap stays as the request set it, including a member
// extra_body value. Call this after ApplyMemberUpstreamBody: the fit has to
// see the numbers that will actually be sent. A budget under 1024 is left
// alone so an explicit small budget is not rewritten.
func FitAnthropicThinkingBudget(body map[string]any) {
	if body == nil {
		return
	}
	thinking, _ := body["thinking"].(map[string]any)
	if thinking == nil || !strings.EqualFold(strings.TrimSpace(fmt.Sprint(thinking["type"])), "enabled") {
		return
	}
	budget, ok := anthropicTokenCount(thinking["budget_tokens"])
	if !ok || budget < 1024 {
		return
	}
	maxTokens, ok := anthropicTokenCount(body["max_tokens"])
	if !ok || maxTokens < 2 {
		return
	}
	if maxBudget := maxTokens - 1; budget > maxBudget {
		thinking["budget_tokens"] = maxBudget
	}
}

func anthropicTokenCount(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		if n > int64(math.MaxInt) || n < int64(math.MinInt) {
			return 0, false
		}
		return int(n), true
	case float32:
		return anthropicFloatTokenCount(float64(n))
	case float64:
		return anthropicFloatTokenCount(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			if i > int64(math.MaxInt) || i < int64(math.MinInt) {
				return 0, false
			}
			return int(i), true
		}
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return anthropicFloatTokenCount(f)
	default:
		return 0, false
	}
}

func anthropicFloatTokenCount(n float64) (int, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > float64(math.MaxInt) {
		return 0, false
	}
	return int(n), true
}

func memberUpstreamBodyProtected(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "model", "messages", "stream":
		return true
	default:
		return false
	}
}

func deepMergeMemberBody(dst, src map[string]any, srcWins bool) {
	for key, srcValue := range src {
		if memberUpstreamBodyProtected(key) {
			continue
		}
		dstValue, exists := dst[key]
		srcMap := memberBodyObject(srcValue)
		dstMap := memberBodyObject(dstValue)
		if srcMap != nil && dstMap != nil {
			// Clone before writing. The request map is often a shallow copy, and
			// merging into the shared nested object would leak this member's
			// fields into a later retry or another provider.
			merged, _ := cloneMemberBodyValue(dstMap).(map[string]any)
			deepMergeMemberBody(merged, srcMap, srcWins)
			dst[key] = merged
			continue
		}
		if !exists || srcWins {
			dst[key] = cloneMemberBodyValue(srcValue)
		}
	}
}

func memberBodyObject(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return nil
	}
}

func cloneMemberBodyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneMemberBodyValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneMemberBodyValue(item)
		}
		return out
	default:
		return typed
	}
}

func (cfg MaclawLLMConfig) applyMemberUpstreamBody(body map[string]any) {
	ApplyMemberUpstreamBody(body, cfg.MemberExtraBody, cfg.MemberExtraBodyPriority, cfg.MemberDropFields)
}

// MemberUpstreamOverrides copies the saved member override onto a request config.
func MemberUpstreamOverrides(cfg MaclawLLMConfig, extra map[string]any, priority string, drop []string) MaclawLLMConfig {
	cfg.MemberExtraBody = extra
	cfg.MemberExtraBodyPriority = priority
	cfg.MemberDropFields = append([]string(nil), drop...)
	return cfg
}
