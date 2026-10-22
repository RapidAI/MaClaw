package corelib

import (
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
