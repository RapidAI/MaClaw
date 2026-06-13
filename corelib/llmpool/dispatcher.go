package llmpool

import (
	"sort"
	"strings"
)

// DispatchModel holds the resolved model with per-provider metadata,
// mirroring the essential fields needed for provider ordering.
type DispatchModel struct {
	Name                      string
	ProviderIDs               []string
	CapabilityTags            []string
	Priority                  int
	ResolutionTier            int
	CreditMultiplier          float64
	ProviderCapabilityTags    map[string][]string
	ProviderPriorities        map[string]int
	ProviderResolutionTiers   map[string]int
	ProviderCreditMultipliers map[string]float64
}

// OrderProviders selects and sorts providers for a given request body,
// scoring by capability match, resolution tier, credit cost, and priority.
// This is the shared dispatcher logic used by both Hub and HubCenter.
func OrderProviders(body map[string]any, model *DispatchModel) []string {
	if model == nil || len(model.ProviderIDs) == 0 {
		return nil
	}

	type scoredProvider struct {
		providerID       string
		originalIndex    int
		score            int
		resolutionTier   int
		priority         int
		creditMultiplier float64
	}

	capabilityNeeds := DetectCapabilityNeeds(body)
	scored := make([]scoredProvider, 0, len(model.ProviderIDs))

	for idx, providerID := range model.ProviderIDs {
		score := 0
		tags := map[string]struct{}{}
		for _, tag := range capabilityTagsForProvider(model, providerID) {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			tags[tag] = struct{}{}
		}
		for need, weight := range capabilityNeeds {
			if _, ok := tags[need]; ok {
				score += weight * 100
			}
		}
		priority := priorityForProvider(model, providerID)
		score += priority

		scored = append(scored, scoredProvider{
			providerID:       providerID,
			originalIndex:    idx,
			score:            score,
			resolutionTier:   normalizedResolutionTier(resolutionTierForProvider(model, providerID)),
			priority:         priority,
			creditMultiplier: normalizeCreditMultiplier(creditMultiplierForProvider(model, providerID)),
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].resolutionTier != scored[j].resolutionTier {
			return scored[i].resolutionTier < scored[j].resolutionTier
		}
		if scored[i].creditMultiplier != scored[j].creditMultiplier {
			return scored[i].creditMultiplier < scored[j].creditMultiplier
		}
		if scored[i].priority != scored[j].priority {
			return scored[i].priority > scored[j].priority
		}
		return scored[i].originalIndex < scored[j].originalIndex
	})

	ordered := make([]string, 0, len(scored))
	for _, item := range scored {
		ordered = append(ordered, item.providerID)
	}
	return ordered
}

// DetectCapabilityNeeds analyzes an OpenAI-compatible request body and
// returns capability tags with weights indicating how strongly the request
// requires each capability.
func DetectCapabilityNeeds(body map[string]any) map[string]int {
	needs := map[string]int{}
	if body == nil {
		return needs
	}
	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		needs["tools"] += 8
	}
	if toolChoice := strings.TrimSpace(strings.ToLower(stringValue(body["tool_choice"]))); toolChoice != "" && toolChoice != "none" {
		needs["tools"] += 4
	}
	text := strings.ToLower(extractRequestText(body))
	addKeywordWeight := func(tag string, weight int, keywords ...string) {
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				needs[tag] += weight
				return
			}
		}
	}
	addKeywordWeight("document", 8, "document", "pdf", "docx", "markdown", "contract", "manual", "spec", "report", "summary", "summarize", "read file")
	addKeywordWeight("reasoning", 5, "reason", "analyze", "analysis", "think", "math", "proof", "deduce")
	addKeywordWeight("tools", 8, "tool", "browser", "search", "function", "call tool", "execute", "fetch")
	return needs
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func capabilityTagsForProvider(model *DispatchModel, providerID string) []string {
	if model.ProviderCapabilityTags != nil {
		if tags, ok := model.ProviderCapabilityTags[providerID]; ok && len(tags) > 0 {
			return tags
		}
	}
	return model.CapabilityTags
}

func priorityForProvider(model *DispatchModel, providerID string) int {
	if model.ProviderPriorities != nil {
		if p, ok := model.ProviderPriorities[providerID]; ok {
			return p
		}
	}
	return model.Priority
}

func resolutionTierForProvider(model *DispatchModel, providerID string) int {
	if model.ProviderResolutionTiers != nil {
		if t, ok := model.ProviderResolutionTiers[providerID]; ok {
			return t
		}
	}
	return model.ResolutionTier
}

func creditMultiplierForProvider(model *DispatchModel, providerID string) float64 {
	if model.ProviderCreditMultipliers != nil {
		if m, ok := model.ProviderCreditMultipliers[providerID]; ok && m > 0 {
			return m
		}
	}
	return model.CreditMultiplier
}

func normalizedResolutionTier(t int) int {
	if t <= 0 {
		return 1
	}
	return t
}

func normalizeCreditMultiplier(v float64) float64 {
	if v <= 0 {
		return 1
	}
	return v
}

func extractRequestText(body map[string]any) string {
	if body == nil {
		return ""
	}
	var parts []string
	if messages, ok := body["messages"].([]any); ok {
		for _, item := range messages {
			parts = append(parts, flattenAnyText(item))
		}
	}
	if input, ok := body["input"]; ok {
		parts = append(parts, flattenAnyText(input))
	}
	return strings.Join(parts, " ")
}

func flattenAnyText(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, flattenAnyText(item))
		}
		return strings.Join(parts, " ")
	case map[string]any:
		parts := make([]string, 0, len(val))
		for _, key := range []string{"content", "text", "input", "name", "description", "arguments"} {
			if sub, ok := val[key]; ok {
				parts = append(parts, flattenAnyText(sub))
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
