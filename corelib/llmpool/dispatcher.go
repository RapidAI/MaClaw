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
	ProviderRoutes            []DispatchProviderRoute
	CapabilityTags            []string
	Priority                  int
	ResolutionTier            int
	CreditMultiplier          float64
	ProviderCapabilityTags    map[string][]string
	ProviderPriorities        map[string]int
	ProviderResolutionTiers   map[string]int
	ProviderCreditMultipliers map[string]float64
}

type DispatchProviderRoute struct {
	ProviderID       string
	Model            string
	CapabilityTags   []string
	Priority         int
	ResolutionTier   int
	CreditMultiplier float64
	OriginalIndex    int
}

// ScoredProviderRoute is a dispatch route plus the capability-match score
// used to keep same-band load balancing from mixing failover tiers.
type ScoredProviderRoute struct {
	Route          DispatchProviderRoute
	Score          int
	ResolutionTier int
}

// OrderProviders selects and sorts providers for a given request body,
// scoring by capability match, resolution tier, credit cost, and priority.
// This is the shared dispatcher logic used by both Hub and HubCenter.
func OrderProviders(body map[string]any, model *DispatchModel) []string {
	routes := OrderProviderRoutes(body, model)
	if len(routes) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(routes))
	for _, route := range routes {
		ordered = append(ordered, route.ProviderID)
	}
	return ordered
}

func OrderProviderRoutes(body map[string]any, model *DispatchModel) []DispatchProviderRoute {
	scored := OrderScoredProviderRoutes(body, model)
	if len(scored) == 0 {
		return nil
	}
	ordered := make([]DispatchProviderRoute, 0, len(scored))
	for _, item := range scored {
		ordered = append(ordered, item.Route)
	}
	return ordered
}

func OrderScoredProviderRoutes(body map[string]any, model *DispatchModel) []ScoredProviderRoute {
	routes := dispatchRoutes(model)
	if len(routes) == 0 {
		return nil
	}

	capabilityNeeds := DetectCapabilityNeeds(body)
	scored := make([]ScoredProviderRoute, 0, len(routes))

	for _, route := range routes {
		route = normalizeDispatchRoute(model, route)
		scored = append(scored, ScoredProviderRoute{
			Route:          route,
			Score:          CapabilityMatchScore(route.CapabilityTags, capabilityNeeds),
			ResolutionTier: route.ResolutionTier,
		})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		if scored[i].ResolutionTier != scored[j].ResolutionTier {
			return scored[i].ResolutionTier < scored[j].ResolutionTier
		}
		if scored[i].Route.CreditMultiplier != scored[j].Route.CreditMultiplier {
			return scored[i].Route.CreditMultiplier < scored[j].Route.CreditMultiplier
		}
		if scored[i].Route.Priority != scored[j].Route.Priority {
			return scored[i].Route.Priority > scored[j].Route.Priority
		}
		return scored[i].Route.OriginalIndex < scored[j].Route.OriginalIndex
	})
	return scored
}

// CapabilityMatchScore is the WRR band key for a provider. It counts only
// tag overlap with the request; admin priority is a sort tie-break, not a band.
func CapabilityMatchScore(tags []string, needs map[string]int) int {
	if len(needs) == 0 {
		return 0
	}
	have := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		have[tag] = struct{}{}
	}
	score := 0
	for need, weight := range needs {
		if _, ok := have[need]; ok {
			score += weight * 100
		}
	}
	return score
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
			if textHasCapabilityKeyword(text, keyword) {
				needs[tag] += weight
				return
			}
		}
	}
	addKeywordWeight("document", 8, "document", "pdf", "docs", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "word", "excel", "spreadsheet", "markdown", "contract", "manual", "spec", "report", "summary", "summarize", "read file")
	addKeywordWeight("reasoning", 5, "reason", "analyze", "analysis", "think", "math", "proof", "deduce")
	addKeywordWeight("tools", 8, "tools", "tool", "browser", "search", "function", "call tool", "execute", "fetch")
	return needs
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func capabilityTagsForProvider(model *DispatchModel, providerID string) []string {
	if tags, ok := lookupProviderValue(model.ProviderCapabilityTags, providerID); ok && len(tags) > 0 {
		return tags
	}
	return model.CapabilityTags
}

func dispatchRoutes(model *DispatchModel) []DispatchProviderRoute {
	if model == nil {
		return nil
	}
	if len(model.ProviderRoutes) > 0 {
		routes := make([]DispatchProviderRoute, 0, len(model.ProviderRoutes))
		for idx, route := range model.ProviderRoutes {
			if strings.TrimSpace(route.ProviderID) == "" {
				continue
			}
			if route.OriginalIndex == 0 && idx != 0 {
				route.OriginalIndex = idx
			}
			routes = append(routes, route)
		}
		return routes
	}
	routes := make([]DispatchProviderRoute, 0, len(model.ProviderIDs))
	for idx, providerID := range model.ProviderIDs {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			continue
		}
		routes = append(routes, DispatchProviderRoute{ProviderID: providerID, OriginalIndex: idx})
	}
	return routes
}

func normalizeDispatchRoute(model *DispatchModel, route DispatchProviderRoute) DispatchProviderRoute {
	route.CapabilityTags = routeCapabilityTags(model, route)
	route.Priority = routePriority(model, route)
	route.ResolutionTier = NormalizedResolutionTier(routeResolutionTier(model, route))
	route.CreditMultiplier = NormalizeCreditMultiplier(routeCreditMultiplier(model, route))
	return route
}

func routeCapabilityTags(model *DispatchModel, route DispatchProviderRoute) []string {
	if len(route.CapabilityTags) > 0 {
		return route.CapabilityTags
	}
	if len(model.ProviderRoutes) > 0 {
		return model.CapabilityTags
	}
	return capabilityTagsForProvider(model, route.ProviderID)
}

func routePriority(model *DispatchModel, route DispatchProviderRoute) int {
	if len(model.ProviderRoutes) > 0 {
		return route.Priority
	}
	if route.Priority != 0 {
		return route.Priority
	}
	return priorityForProvider(model, route.ProviderID)
}

func routeResolutionTier(model *DispatchModel, route DispatchProviderRoute) int {
	if len(model.ProviderRoutes) > 0 {
		return route.ResolutionTier
	}
	if route.ResolutionTier != 0 {
		return route.ResolutionTier
	}
	return resolutionTierForProvider(model, route.ProviderID)
}

func routeCreditMultiplier(model *DispatchModel, route DispatchProviderRoute) float64 {
	if len(model.ProviderRoutes) > 0 {
		if route.CreditMultiplier > 0 {
			return route.CreditMultiplier
		}
		return model.CreditMultiplier
	}
	if route.CreditMultiplier > 0 {
		return route.CreditMultiplier
	}
	return creditMultiplierForProvider(model, route.ProviderID)
}

func priorityForProvider(model *DispatchModel, providerID string) int {
	if p, ok := lookupProviderValue(model.ProviderPriorities, providerID); ok {
		return p
	}
	return model.Priority
}

func resolutionTierForProvider(model *DispatchModel, providerID string) int {
	if t, ok := lookupProviderValue(model.ProviderResolutionTiers, providerID); ok {
		return t
	}
	return model.ResolutionTier
}

func creditMultiplierForProvider(model *DispatchModel, providerID string) float64 {
	if m, ok := lookupProviderValue(model.ProviderCreditMultipliers, providerID); ok && m > 0 {
		return m
	}
	return model.CreditMultiplier
}

func lookupProviderValue[T any](m map[string]T, providerID string) (T, bool) {
	var zero T
	if len(m) == 0 {
		return zero, false
	}
	if v, ok := m[providerID]; ok {
		return v, true
	}
	want := strings.ToLower(strings.TrimSpace(providerID))
	if want == "" || want == providerID {
		return zero, false
	}
	v, ok := m[want]
	return v, ok
}

func NormalizedResolutionTier(t int) int {
	if t <= 0 {
		return 1
	}
	return t
}

func textHasCapabilityKeyword(text, keyword string) bool {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if text == "" || keyword == "" {
		return false
	}
	if strings.Contains(keyword, " ") {
		return strings.Contains(text, keyword)
	}
	// Short tokens false-positive as substrings (doc⊂docker, search⊂research,
	// excel⊂excellent). Longer stems like "document" still use substring match.
	if len(keyword) > 6 {
		return strings.Contains(text, keyword)
	}
	for {
		i := strings.Index(text, keyword)
		if i < 0 {
			return false
		}
		left := i == 0 || !isCapabilityKeywordByte(text[i-1])
		right := i+len(keyword) == len(text) || !isCapabilityKeywordByte(text[i+len(keyword)])
		if left && right {
			return true
		}
		text = text[i+1:]
	}
}

func isCapabilityKeywordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
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
	if prompt, ok := body["prompt"].(string); ok {
		parts = append(parts, prompt)
	}
	return strings.Join(parts, " ")
}

// RequestTextPreview returns a compact, truncated view of request text for rule-board samples.
func RequestTextPreview(body map[string]any, maxRunes int) string {
	text := strings.Join(strings.Fields(extractRequestText(body)), " ")
	if maxRunes <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
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
