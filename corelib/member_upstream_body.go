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
	FinishMemberUpstreamBody(cfg, body)
}

// FinishMemberUpstreamBody merges extra_body, applies drop_fields, then caps
// output fields that are still present. The cap does not put a field back
// after drop_fields removed it, and it does not invent a field the merge
// left absent. A cap of 0 leaves the merged body unchanged.
func FinishMemberUpstreamBody(cfg MaclawLLMConfig, body map[string]any) {
	ApplyMemberUpstreamBody(body, cfg.MemberExtraBody, cfg.MemberExtraBodyPriority, cfg.MemberDropFields)
	ClampMemberOutputFields(body, cfg.MemberOutputTokenCap)
}

// ClampMemberOutputFields lowers max_tokens, max_completion_tokens, and
// max_output_tokens that are above cap. Each present field is judged alone.
// A usable value at or below the cap keeps its original Go type, including
// 0. A value above the cap, or a value that is not a non-negative number,
// is replaced with the cap. Absent fields stay absent. cap <= 0 does nothing.
func ClampMemberOutputFields(body map[string]any, cap int64) {
	if body == nil || cap <= 0 {
		return
	}
	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		raw, ok := body[key]
		if !ok {
			continue
		}
		n, usable := OutputBudgetInt(raw)
		if !usable || n < 0 || n > cap || outputBudgetExceeds(raw, cap) {
			body[key] = cap
		}
	}
}

// OutputBudgetInt reads a whole output budget. A fractional float yields the
// truncated integer and true so the caller can keep the original value when
// that magnitude is still inside the cap. Negative, NaN, and overflow are
// not usable.
func OutputBudgetInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float32:
		return outputBudgetFloat(float64(n))
	case float64:
		return outputBudgetFloat(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i, true
		}
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return outputBudgetFloat(f)
	default:
		return 0, false
	}
}

func outputBudgetFloat(n float64) (int64, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n >= float64(math.MaxInt64) {
		return 0, false
	}
	return int64(n), true
}

func outputBudgetExceeds(raw any, cap int64) bool {
	limit := float64(cap)
	switch n := raw.(type) {
	case float32:
		return float64(n) > limit
	case float64:
		return n > limit
	case json.Number:
		f, err := n.Float64()
		return err == nil && f > limit
	default:
		return false
	}
}

// MemberUpstreamOverrides copies the saved member override onto a request config.
func MemberUpstreamOverrides(cfg MaclawLLMConfig, extra map[string]any, priority string, drop []string) MaclawLLMConfig {
	cfg.MemberExtraBody = extra
	cfg.MemberExtraBodyPriority = priority
	cfg.MemberDropFields = append([]string(nil), drop...)
	return cfg
}
