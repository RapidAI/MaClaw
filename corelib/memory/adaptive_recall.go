package memory

import (
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	adaptiveMaxThemes       = 5
	adaptiveThemeExpansion  = 3
	adaptiveDefaultMaxItems = 15
	adaptiveDefaultTokens   = 2500
)

// AdaptiveRecallPlan explains the top-down choices made by
// RecallAdaptiveHierDebug. It is intended for diagnostics, evaluation, and UI
// inspection rather than prompt injection.
type AdaptiveRecallPlan struct {
	Query            string                   `json:"query"`
	Complexity       QueryComplexity          `json:"complexity"`
	QueryFacets      []RecallQueryFacet       `json:"query_facets,omitempty"`
	Budget           AdaptiveRecallBudget     `json:"budget"`
	Diversity        AdaptiveDiversityStats   `json:"diversity,omitempty"`
	FacetCoverage    []AdaptiveFacetCoverage  `json:"facet_coverage,omitempty"`
	Fallback         bool                     `json:"fallback"`
	SelectedThemes   []AdaptiveThemeSelection `json:"selected_themes,omitempty"`
	ThemeAggregates  []AdaptiveThemeAggregate `json:"theme_aggregates,omitempty"`
	SeedEntryIDs     []string                 `json:"seed_entry_ids,omitempty"`
	ExpandedEntryIDs []string                 `json:"expanded_entry_ids,omitempty"`
	ResultEntryIDs   []string                 `json:"result_entry_ids,omitempty"`
	ExpandedEvidence []AdaptiveEntryEvidence  `json:"expanded_evidence,omitempty"`
	ResultEvidence   []AdaptiveEntryEvidence  `json:"result_evidence,omitempty"`
}

type RecallQueryFacet struct {
	Kind   string   `json:"kind"`
	Text   string   `json:"text"`
	Tokens []string `json:"tokens,omitempty"`
}

type AdaptiveRecallBudget struct {
	MaxItems    int `json:"max_items"`
	TokenBudget int `json:"token_budget"`
}

type AdaptiveDiversityStats struct {
	ThemeCap                   int `json:"theme_cap,omitempty"`
	SourceCap                  int `json:"source_cap,omitempty"`
	DeferredByThemeCap         int `json:"deferred_by_theme_cap,omitempty"`
	DeferredBySourceCap        int `json:"deferred_by_source_cap,omitempty"`
	BackfilledDeferred         int `json:"backfilled_deferred,omitempty"`
	SelectedThemeCount         int `json:"selected_theme_count,omitempty"`
	SelectedSourceCount        int `json:"selected_source_count,omitempty"`
	SelectedThemeTargets       int `json:"selected_theme_targets,omitempty"`
	SelectedThemeCoveredBefore int `json:"selected_theme_covered_before,omitempty"`
	SelectedThemeCoveredAfter  int `json:"selected_theme_covered_after,omitempty"`
	ReservedSelectedThemes     int `json:"reserved_selected_themes,omitempty"`
}

type AdaptiveFacetCoverage struct {
	Kind             string   `json:"kind"`
	Text             string   `json:"text"`
	SelectedThemeIDs []string `json:"selected_theme_ids,omitempty"`
	ExpandedEntryIDs []string `json:"expanded_entry_ids,omitempty"`
}

type AdaptiveThemeSelection struct {
	ThemeID       string                       `json:"theme_id"`
	Summary       string                       `json:"summary,omitempty"`
	EntryIDs      []string                     `json:"entry_ids,omitempty"`
	Reason        string                       `json:"reason,omitempty"`
	MatchedFacets []string                     `json:"matched_facets,omitempty"`
	MatchEvidence []AdaptiveThemeMatchEvidence `json:"match_evidence,omitempty"`
}

type AdaptiveThemeMatchEvidence struct {
	FacetKind      string `json:"facet_kind"`
	Token          string `json:"token,omitempty"`
	EntryID        string `json:"entry_id,omitempty"`
	ContentPreview string `json:"content_preview,omitempty"`
	SourceType     string `json:"source_type,omitempty"`
	SourceURL      string `json:"source_url,omitempty"`
}

type AdaptiveThemeAggregate struct {
	ThemeID          string         `json:"theme_id"`
	Summary          string         `json:"summary,omitempty"`
	MatchedFacets    []string       `json:"matched_facets,omitempty"`
	SeedEntryIDs     []string       `json:"seed_entry_ids,omitempty"`
	ExpandedEntryIDs []string       `json:"expanded_entry_ids,omitempty"`
	ResultEntryIDs   []string       `json:"result_entry_ids,omitempty"`
	ResultPreviews   []string       `json:"result_previews,omitempty"`
	SourceCounts     map[string]int `json:"source_counts,omitempty"`
	TokenEstimate    int            `json:"token_estimate,omitempty"`
}

type AdaptiveEntryEvidence struct {
	EntryID        string `json:"entry_id"`
	Reason         string `json:"reason"`
	ThemeID        string `json:"theme_id,omitempty"`
	SourceType     string `json:"source_type,omitempty"`
	SourceURL      string `json:"source_url,omitempty"`
	Rank           int    `json:"rank"`
	ExpansionScore int    `json:"expansion_score,omitempty"`
}

// ShouldUseAdaptiveRecall returns true for multi-entity or otherwise complex
// queries where top-down theme expansion is likely to recover useful adjacent
// evidence beyond the flat hybrid seed set.
func ShouldUseAdaptiveRecall(query string) bool {
	expanded := ExpandQuery(query)
	if ClassifyComplexity(query, expanded.Entities, nil) == ComplexitySimple {
		return false
	}
	lq := strings.ToLower(query)
	adaptiveSignals := []string{
		"why", "compare", "analyze", "trend", "history", "evolve", "summarize", "pattern",
		"relationship", "between", "over time", "tradeoff", "risk",
		"为什么", "对比", "分析", "趋势", "历史", "变化", "演变", "总结", "比较", "模式", "关系", "取舍", "风险",
	}
	for _, signal := range adaptiveSignals {
		if strings.Contains(lq, signal) {
			return true
		}
	}
	return len([]rune(query)) >= 60 || len(expanded.Entities) >= 3
}

// DecomposeRecallQuery creates lightweight facets for decoupled retrieval.
// It is rule-based so adaptive recall remains deterministic and offline.
func DecomposeRecallQuery(query string, expanded ExpandResult) []RecallQueryFacet {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	var facets []RecallQueryFacet
	addFacet := func(kind, text string, tokens []string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		tokens = uniqueSortedStrings(tokens)
		for _, existing := range facets {
			if existing.Kind == kind && strings.EqualFold(existing.Text, text) {
				return
			}
		}
		facets = append(facets, RecallQueryFacet{Kind: kind, Text: text, Tokens: tokens})
	}

	if len(expanded.Entities) > 0 {
		addFacet("entity", strings.Join(expanded.Entities, " / "), expanded.Entities)
	}

	lq := strings.ToLower(query)
	if containsAny(lq, []string{"compare", "vs", "versus", "between", "tradeoff", "对比", "比较", "取舍"}) {
		addFacet("comparison", query, append([]string(nil), expanded.QueryTokens...))
	}
	if containsAny(lq, []string{"why", "because", "reason", "root cause", "为什么", "原因", "根因"}) {
		addFacet("causal", query, append([]string(nil), expanded.QueryTokens...))
	}
	if containsAny(lq, []string{"over time", "history", "trend", "evolve", "changed", "timeline", "时间", "历史", "趋势", "演变"}) {
		addFacet("temporal", query, append([]string(nil), expanded.QueryTokens...))
	}
	if len(facets) == 0 {
		addFacet("keyword", query, append([]string(nil), expanded.QueryTokens...))
	}
	return facets
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func selectThemesByQueryFacets(themes []ThemeNode, facets []RecallQueryFacet, selected map[string]struct{}, limit int, entryByID map[string]Entry) []ThemeNode {
	return selectThemesByQueryFacetsCached(themes, facets, selected, limit, entryByID, nil)
}

func selectThemesByQueryFacetsCached(themes []ThemeNode, facets []RecallQueryFacet, selected map[string]struct{}, limit int, entryByID map[string]Entry, matchedByThemeID map[string][]string) []ThemeNode {
	if limit <= 0 || len(facets) == 0 || len(themes) == 0 {
		return nil
	}
	type scoredTheme struct {
		theme ThemeNode
		score int
	}
	var scored []scoredTheme
	for _, theme := range themes {
		if _, ok := selected[theme.ID]; ok {
			continue
		}
		score := 0
		for _, kind := range matchedFacetKindsForThemeCached(theme, facets, entryByID, matchedByThemeID) {
			score += facetMatchWeight(kind)
		}
		if score > 0 {
			scored = append(scored, scoredTheme{theme: theme, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].theme.MemberCount != scored[j].theme.MemberCount {
			return scored[i].theme.MemberCount > scored[j].theme.MemberCount
		}
		return scored[i].theme.ID < scored[j].theme.ID
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	out := make([]ThemeNode, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.theme)
	}
	return out
}

func matchedFacetKindsForTheme(theme ThemeNode, facets []RecallQueryFacet, entryByID ...map[string]Entry) []string {
	var entries map[string]Entry
	if len(entryByID) > 0 {
		entries = entryByID[0]
	}
	return matchedFacetKindsForThemeCached(theme, facets, entries, nil)
}

func matchedFacetKindsForThemeCached(theme ThemeNode, facets []RecallQueryFacet, entryByID map[string]Entry, matchedByThemeID map[string][]string) []string {
	if len(facets) == 0 {
		return nil
	}
	cacheKey := themeMatchCacheKey(theme)
	if matchedByThemeID != nil {
		if matched, ok := matchedByThemeID[cacheKey]; ok {
			return append([]string(nil), matched...)
		}
	}
	text := strings.ToLower(themeFacetMatchText(theme, entryByID))
	seen := make(map[string]struct{})
	var matched []string
	for _, facet := range facets {
		for _, token := range facet.Tokens {
			token = strings.ToLower(strings.TrimSpace(token))
			if token == "" || !containsFacetToken(text, token) {
				continue
			}
			if _, ok := seen[facet.Kind]; ok {
				break
			}
			seen[facet.Kind] = struct{}{}
			matched = append(matched, facet.Kind)
			break
		}
	}
	sort.Strings(matched)
	if matchedByThemeID != nil {
		matchedByThemeID[cacheKey] = append([]string(nil), matched...)
	}
	return matched
}

func themeFacetMatchEvidence(theme ThemeNode, facets []RecallQueryFacet, entryByID map[string]Entry, limit int) []AdaptiveThemeMatchEvidence {
	return themeFacetMatchEvidenceCached(theme, facets, entryByID, limit, nil)
}

func themeFacetMatchEvidenceCached(theme ThemeNode, facets []RecallQueryFacet, entryByID map[string]Entry, limit int, evidenceByThemeID map[string][]AdaptiveThemeMatchEvidence) []AdaptiveThemeMatchEvidence {
	if len(facets) == 0 || limit <= 0 {
		return nil
	}
	cacheKey := themeMatchCacheKey(theme)
	if evidenceByThemeID != nil {
		if evidence, ok := evidenceByThemeID[cacheKey]; ok {
			if len(evidence) > limit {
				return append([]AdaptiveThemeMatchEvidence(nil), evidence[:limit]...)
			}
			return append([]AdaptiveThemeMatchEvidence(nil), evidence...)
		}
	}
	var evidence []AdaptiveThemeMatchEvidence
	seen := make(map[string]struct{})
	themeText := strings.ToLower(theme.Summary + " " + strings.Join(theme.Tags, " "))
	for _, facet := range facets {
		for _, token := range facet.Tokens {
			token = strings.ToLower(strings.TrimSpace(token))
			if token == "" {
				continue
			}
			matched := false
			if containsFacetToken(themeText, token) {
				key := facet.Kind + "\x00" + token + "\x00theme"
				if _, ok := seen[key]; !ok {
					evidence = append(evidence, AdaptiveThemeMatchEvidence{
						FacetKind:      facet.Kind,
						Token:          token,
						ContentPreview: themeContentPreview(theme.Summary, 120),
					})
					seen[key] = struct{}{}
				}
				matched = true
			}
			entry, ok := firstThemeEntryContainingToken(theme, entryByID, token)
			if ok {
				key := facet.Kind + "\x00" + token + "\x00" + entry.ID
				if _, exists := seen[key]; !exists {
					evidence = append(evidence, AdaptiveThemeMatchEvidence{
						FacetKind:      facet.Kind,
						Token:          token,
						EntryID:        entry.ID,
						ContentPreview: themeContentPreview(entry.Content, 120),
						SourceType:     string(ClassifyExperienceSource(entry)),
						SourceURL:      entry.SourceURL,
					})
					seen[key] = struct{}{}
				}
				matched = true
			}
			if matched {
				break
			}
		}
		if len(evidence) >= limit {
			evidence = evidence[:limit]
			if evidenceByThemeID != nil {
				evidenceByThemeID[cacheKey] = append([]AdaptiveThemeMatchEvidence(nil), evidence...)
			}
			return evidence
		}
	}
	if evidenceByThemeID != nil {
		evidenceByThemeID[cacheKey] = append([]AdaptiveThemeMatchEvidence(nil), evidence...)
	}
	return evidence
}

func themeMatchCacheKey(theme ThemeNode) string {
	if theme.ID != "" {
		return theme.ID
	}
	return "anonymous\x00" + strings.Join(theme.EntryIDs, "\x00") + "\x00" + theme.Summary + "\x00" + strings.Join(theme.Tags, "\x00")
}

func firstThemeEntryContainingToken(theme ThemeNode, entryByID map[string]Entry, token string) (Entry, bool) {
	if len(entryByID) == 0 || token == "" {
		return Entry{}, false
	}
	for _, id := range theme.EntryIDs {
		entry, ok := entryByID[id]
		if !ok {
			continue
		}
		text := strings.ToLower(entry.Title + " " + entry.Content + " " + strings.Join(entry.Tags, " ") + " " + strings.Join(entry.Entities, " "))
		if containsFacetToken(text, token) {
			return entry, true
		}
	}
	return Entry{}, false
}

func containsFacetToken(text string, token string) bool {
	if token == "" {
		return false
	}
	if !isASCIIAlphaNumToken(token) {
		return strings.Contains(text, token)
	}
	start := 0
	for start <= len(text) {
		idx := strings.Index(text[start:], token)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isASCIIAlphaNumByte(text[idx-1])
		after := idx + len(token)
		afterOK := after >= len(text) || !isASCIIAlphaNumByte(text[after])
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
	return false
}

func isASCIIAlphaNumToken(token string) bool {
	for _, r := range token {
		if r > unicode.MaxASCII || !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return token != ""
}

func isASCIIAlphaNumByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
}

func themeFacetMatchText(theme ThemeNode, entryByID map[string]Entry) string {
	var b strings.Builder
	b.WriteString(theme.Summary)
	b.WriteByte(' ')
	b.WriteString(strings.Join(theme.Tags, " "))
	if len(entryByID) == 0 {
		return b.String()
	}
	for _, id := range theme.EntryIDs {
		entry, ok := entryByID[id]
		if !ok {
			continue
		}
		b.WriteByte(' ')
		b.WriteString(entry.Title)
		b.WriteByte(' ')
		b.WriteString(entry.Content)
		b.WriteByte(' ')
		b.WriteString(strings.Join(entry.Tags, " "))
		b.WriteByte(' ')
		b.WriteString(strings.Join(entry.Entities, " "))
	}
	return b.String()
}

func facetMatchWeight(kind string) int {
	switch kind {
	case "entity":
		return 4
	case "comparison", "causal", "temporal":
		return 2
	default:
		return 1
	}
}

type adaptiveExpansionCandidate struct {
	entry Entry
	score int
}

func adaptiveExpansionCandidateScore(entry Entry, theme ThemeNode, facets []RecallQueryFacet) int {
	text := strings.ToLower(entry.Title + " " + entry.Content + " " + strings.Join(entry.Tags, " ") + " " + strings.Join(entry.Entities, " "))
	score := 0
	for _, facet := range facets {
		matchedFacet := false
		for _, token := range facet.Tokens {
			token = strings.ToLower(strings.TrimSpace(token))
			if token == "" || !strings.Contains(text, token) {
				continue
			}
			if !matchedFacet {
				score += facetMatchWeight(facet.Kind) * 10
				matchedFacet = true
			}
			score++
		}
	}
	themeText := strings.ToLower(theme.Summary + " " + strings.Join(theme.Tags, " "))
	for _, tag := range entry.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" && strings.Contains(themeText, tag) {
			score++
		}
	}
	if entry.Pinned {
		score += 3
	}
	if entry.AccessCount > 0 {
		if entry.AccessCount > 5 {
			score += 5
		} else {
			score += entry.AccessCount
		}
	}
	return score
}

func sortAdaptiveExpansionCandidates(candidates []adaptiveExpansionCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].entry.AccessCount != candidates[j].entry.AccessCount {
			return candidates[i].entry.AccessCount > candidates[j].entry.AccessCount
		}
		if !candidates[i].entry.UpdatedAt.Equal(candidates[j].entry.UpdatedAt) {
			return candidates[i].entry.UpdatedAt.After(candidates[j].entry.UpdatedAt)
		}
		return candidates[i].entry.ID < candidates[j].entry.ID
	})
}

func themeExpansionCount(evidence []AdaptiveEntryEvidence, themeID string) int {
	count := 0
	for _, ev := range evidence {
		if ev.ThemeID == themeID {
			count++
		}
	}
	return count
}

func adaptiveRecallBudget(complexity QueryComplexity, facets []RecallQueryFacet) AdaptiveRecallBudget {
	budget := AdaptiveRecallBudget{
		MaxItems:    adaptiveDefaultMaxItems,
		TokenBudget: adaptiveDefaultTokens,
	}
	switch complexity {
	case ComplexityHybrid:
		budget.MaxItems = 18
		budget.TokenBudget = 3000
	case ComplexityComplex:
		budget.MaxItems = 24
		budget.TokenBudget = 4200
	}
	if len(facets) >= 4 {
		budget.MaxItems += 3
		budget.TokenBudget += 600
	}
	if len(facets) >= 6 {
		budget.MaxItems += 3
		budget.TokenBudget += 600
	}
	if budget.MaxItems > 30 {
		budget.MaxItems = 30
	}
	if budget.TokenBudget > 5400 {
		budget.TokenBudget = 5400
	}
	return budget
}

func firstThemeByEntryID(themes []ThemeNode) map[string]string {
	out := make(map[string]string)
	for _, theme := range themes {
		for _, id := range theme.EntryIDs {
			if _, ok := out[id]; !ok {
				out[id] = theme.ID
			}
		}
	}
	return out
}

func selectAdaptiveResults(scored []recallScored, budget AdaptiveRecallBudget, entryThemeByID map[string]string) ([]Entry, AdaptiveDiversityStats) {
	var stats AdaptiveDiversityStats
	if len(scored) == 0 || budget.MaxItems <= 0 || budget.TokenBudget <= 0 {
		return nil, stats
	}
	themeCap := budget.MaxItems / 3
	if themeCap < 3 {
		themeCap = 3
	}
	sourceCap := budget.MaxItems / 2
	if sourceCap < 4 {
		sourceCap = 4
	}
	stats.ThemeCap = themeCap
	stats.SourceCap = sourceCap

	result := make([]Entry, 0, budget.MaxItems)
	seen := make(map[string]struct{}, len(scored))
	themeCounts := make(map[string]int)
	sourceCounts := make(map[string]int)
	deferred := make([]recallScored, 0)
	tokenBudget := budget.TokenBudget

	for _, candidate := range scored {
		tokens := EstimateTextTokens(candidate.entry.Content)
		if tokens > tokenBudget || tokens > budget.TokenBudget {
			continue
		}
		if candidate.entry.ID != "" {
			if _, exists := seen[candidate.entry.ID]; exists {
				continue
			}
		}
		themeID := entryThemeByID[candidate.entry.ID]
		source := strings.TrimSpace(string(ClassifyExperienceSource(candidate.entry)))
		if source == "" {
			source = string(ExperienceSourceUnknown)
		}
		overThemeCap := themeID != "" && themeCounts[themeID] >= themeCap
		overSourceCap := sourceCounts[source] >= sourceCap
		if overThemeCap || overSourceCap {
			if overThemeCap {
				stats.DeferredByThemeCap++
			}
			if overSourceCap {
				stats.DeferredBySourceCap++
			}
			deferred = append(deferred, candidate)
			continue
		}
		result = append(result, candidate.entry)
		if candidate.entry.ID != "" {
			seen[candidate.entry.ID] = struct{}{}
		}
		tokenBudget -= tokens
		if themeID != "" {
			themeCounts[themeID]++
		}
		sourceCounts[source]++
		if len(result) >= budget.MaxItems {
			stats.SelectedThemeCount = nonZeroMapLen(themeCounts)
			stats.SelectedSourceCount = nonZeroMapLen(sourceCounts)
			return result, stats
		}
	}

	for _, candidate := range deferred {
		if candidate.entry.ID != "" {
			if _, exists := seen[candidate.entry.ID]; exists {
				continue
			}
		}
		tokens := EstimateTextTokens(candidate.entry.Content)
		if tokens > tokenBudget {
			continue
		}
		result = append(result, candidate.entry)
		if candidate.entry.ID != "" {
			seen[candidate.entry.ID] = struct{}{}
		}
		tokenBudget -= tokens
		stats.BackfilledDeferred++
		themeID := entryThemeByID[candidate.entry.ID]
		if themeID != "" {
			themeCounts[themeID]++
		}
		source := strings.TrimSpace(string(ClassifyExperienceSource(candidate.entry)))
		if source == "" {
			source = string(ExperienceSourceUnknown)
		}
		sourceCounts[source]++
		if len(result) >= budget.MaxItems {
			break
		}
	}
	stats.SelectedThemeCount = nonZeroMapLen(themeCounts)
	stats.SelectedSourceCount = nonZeroMapLen(sourceCounts)
	return result, stats
}

func nonZeroMapLen(counts map[string]int) int {
	n := 0
	for _, count := range counts {
		if count > 0 {
			n++
		}
	}
	return n
}

// RecallAdaptiveHier retrieves through the xMemory-style theme layer. It keeps
// the existing RecallDynamic pipeline as the seed stage, then expands within
// selected themes to recover related but lower-ranked evidence.
func (s *Store) RecallAdaptiveHier(query string, category Category, projectPath string, ownerID ...string) []Entry {
	result, _ := s.RecallAdaptiveHierDebug(query, category, projectPath, ownerID...)
	s.touchRecallResultsAsync(result)
	return result
}

// RecallAdaptiveHierDebug returns both results and the decision plan.
func (s *Store) RecallAdaptiveHierDebug(query string, category Category, projectPath string, ownerID ...string) ([]Entry, AdaptiveRecallPlan) {
	expanded := ExpandQuery(query)
	complexity := ClassifyComplexity(query, expanded.Entities, nil)
	queryFacets := DecomposeRecallQuery(query, expanded)
	budget := adaptiveRecallBudget(complexity, queryFacets)
	plan := AdaptiveRecallPlan{Query: query, Complexity: complexity, QueryFacets: queryFacets, Budget: budget}

	seeds := s.RecallDynamic(query, category, projectPath, ownerID...)
	plan.SeedEntryIDs = recallResultIDs(seeds)
	if !ShouldUseAdaptiveRecall(query) || s.themeManager == nil {
		plan.Fallback = true
		setAdaptivePlanResults(&plan, seeds, nil, nil)
		return seeds, plan
	}

	themes := s.themeManager.Themes()
	if len(themes) == 0 {
		plan.Fallback = true
		setAdaptivePlanResults(&plan, seeds, nil, nil)
		return seeds, plan
	}

	s.mu.RLock()
	entryByID := make(map[string]Entry, len(s.entries))
	for _, entry := range s.entries {
		entryByID[entry.ID] = entry
	}
	s.mu.RUnlock()

	entryToThemes := make(map[string][]ThemeNode)
	for _, theme := range themes {
		for _, id := range theme.EntryIDs {
			entryToThemes[id] = append(entryToThemes[id], theme)
		}
	}
	matchedFacetsByThemeID := make(map[string][]string)
	matchEvidenceByThemeID := make(map[string][]AdaptiveThemeMatchEvidence)

	selectedThemeIDs := make(map[string]struct{})
	var selectedThemes []ThemeNode
	for _, seed := range seeds {
		for _, theme := range entryToThemes[seed.ID] {
			if _, exists := selectedThemeIDs[theme.ID]; exists {
				continue
			}
			selectedThemeIDs[theme.ID] = struct{}{}
			selectedThemes = append(selectedThemes, theme)
			plan.SelectedThemes = append(plan.SelectedThemes, AdaptiveThemeSelection{
				ThemeID:       theme.ID,
				Summary:       theme.Summary,
				EntryIDs:      append([]string(nil), theme.EntryIDs...),
				Reason:        "seed_entry",
				MatchedFacets: matchedFacetKindsForThemeCached(theme, queryFacets, entryByID, matchedFacetsByThemeID),
				MatchEvidence: themeFacetMatchEvidenceCached(theme, queryFacets, entryByID, 5, matchEvidenceByThemeID),
			})
			if len(selectedThemes) >= adaptiveMaxThemes {
				break
			}
		}
		if len(selectedThemes) >= adaptiveMaxThemes {
			break
		}
	}
	if len(selectedThemes) < adaptiveMaxThemes {
		facetThemes := selectThemesByQueryFacetsCached(themes, queryFacets, selectedThemeIDs, adaptiveMaxThemes-len(selectedThemes), entryByID, matchedFacetsByThemeID)
		for _, theme := range facetThemes {
			selectedThemeIDs[theme.ID] = struct{}{}
			selectedThemes = append(selectedThemes, theme)
			plan.SelectedThemes = append(plan.SelectedThemes, AdaptiveThemeSelection{
				ThemeID:       theme.ID,
				Summary:       theme.Summary,
				EntryIDs:      append([]string(nil), theme.EntryIDs...),
				Reason:        "query_facet",
				MatchedFacets: matchedFacetKindsForThemeCached(theme, queryFacets, entryByID, matchedFacetsByThemeID),
				MatchEvidence: themeFacetMatchEvidenceCached(theme, queryFacets, entryByID, 5, matchEvidenceByThemeID),
			})
		}
	}
	if len(selectedThemes) == 0 {
		plan.Fallback = true
		setAdaptivePlanResults(&plan, seeds, nil, nil)
		return seeds, plan
	}

	projectLower := semanticNormalizeProjectPath(projectPath)
	filterOwner := firstOwnerID(ownerID...)
	seedSet := make(map[string]Entry, len(seeds))
	seedIDs := make(map[string]struct{}, len(seeds))
	for _, seed := range seeds {
		seedSet[seed.ID] = seed
		seedIDs[seed.ID] = struct{}{}
	}
	expandedThemeByEntryID := make(map[string]string)
	expansionScoreByEntryID := make(map[string]int)

	expandedEntries := make([]Entry, 0, len(seeds)+len(selectedThemes)*adaptiveThemeExpansion)
	expandedEntries = append(expandedEntries, seeds...)

	for _, theme := range selectedThemes {
		var candidates []adaptiveExpansionCandidate
		for _, id := range theme.EntryIDs {
			if _, exists := seedSet[id]; exists {
				continue
			}
			found, ok := entryByID[id]
			if !ok || !recallDynamicEntryAllowed(found, category, projectLower, filterOwner) {
				continue
			}
			candidates = append(candidates, adaptiveExpansionCandidate{
				entry: found,
				score: adaptiveExpansionCandidateScore(found, theme, queryFacets),
			})
		}
		sortAdaptiveExpansionCandidates(candidates)
		for _, candidate := range candidates {
			if _, exists := seedSet[candidate.entry.ID]; exists {
				continue
			}
			expandedEntries = append(expandedEntries, candidate.entry)
			plan.ExpandedEntryIDs = append(plan.ExpandedEntryIDs, candidate.entry.ID)
			plan.ExpandedEvidence = append(plan.ExpandedEvidence, AdaptiveEntryEvidence{
				EntryID:        candidate.entry.ID,
				Reason:         "theme_expansion",
				ThemeID:        theme.ID,
				SourceType:     string(ClassifyExperienceSource(candidate.entry)),
				SourceURL:      candidate.entry.SourceURL,
				Rank:           len(plan.ExpandedEntryIDs),
				ExpansionScore: candidate.score,
			})
			expandedThemeByEntryID[candidate.entry.ID] = theme.ID
			expansionScoreByEntryID[candidate.entry.ID] = candidate.score
			seedSet[candidate.entry.ID] = candidate.entry
			if len(plan.ExpandedEntryIDs) >= len(selectedThemes)*adaptiveThemeExpansion {
				break
			}
			if themeExpansionCount(plan.ExpandedEvidence, theme.ID) >= adaptiveThemeExpansion {
				break
			}
		}
	}

	scored := make([]recallScored, 0, len(expandedEntries))
	now := time.Now()
	for i, entry := range expandedEntries {
		score := float64(len(expandedEntries) - i)
		if i >= len(seeds) {
			score *= 0.5
			score += float64(expansionScoreByEntryID[entry.ID]) * 0.05
		}
		score += 0.01 * memoryStreamScore(entry, 0, 0, projectLower, now)
		scored = append(scored, recallScored{entry: entry, score: score})
	}
	scored = themeAwareDiversityRerank(scored, themes, adaptiveMaxThemes)

	entryThemeByID := firstThemeByEntryID(themes)
	result, diversity := selectAdaptiveResults(scored, budget, entryThemeByID)
	result = ensureAdaptiveExpansionCoverage(result, expandedEntries, expandedThemeByEntryID, budget.MaxItems, budget.TokenBudget)
	diversity.SelectedThemeTargets = countSelectedThemeTargets(plan.SelectedThemes)
	diversity.SelectedThemeCoveredBefore = countCoveredSelectedThemes(result, plan.SelectedThemes, entryThemeByID)
	result, diversity.ReservedSelectedThemes = ensureAdaptiveSelectedThemeCoverage(result, expandedEntries, plan.SelectedThemes, entryThemeByID, budget.MaxItems, budget.TokenBudget)
	diversity.SelectedThemeCoveredAfter = countCoveredSelectedThemes(result, plan.SelectedThemes, entryThemeByID)
	plan.Diversity = diversity
	planResults := setAdaptivePlanResults(&plan, result, seedIDs, expandedThemeByEntryID, entryThemeByID)
	plan.FacetCoverage = adaptiveFacetCoverage(queryFacets, plan.SelectedThemes, plan.ExpandedEvidence)
	plan.ThemeAggregates = adaptiveThemeAggregates(plan.SelectedThemes, seeds, planResults, plan.ExpandedEvidence)
	return result, plan
}

func setAdaptivePlanResults(plan *AdaptiveRecallPlan, entries []Entry, seedIDs map[string]struct{}, expandedThemeByEntryID map[string]string, entryThemeByID ...map[string]string) []Entry {
	planResults := dedupeEntriesByID(entries)
	plan.ResultEntryIDs = recallResultIDs(planResults)
	plan.ResultEvidence = adaptiveResultEvidence(planResults, seedIDs, expandedThemeByEntryID, entryThemeByID...)
	return planResults
}

func dedupeEntriesByID(entries []Entry) []Entry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]Entry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.ID != "" {
			if _, ok := seen[entry.ID]; ok {
				continue
			}
			seen[entry.ID] = struct{}{}
		}
		out = append(out, entry)
	}
	return out
}

func ensureAdaptiveExpansionCoverage(result []Entry, candidates []Entry, expandedThemeByEntryID map[string]string, maxItems int, maxTokens int) []Entry {
	if len(result) == 0 || len(expandedThemeByEntryID) == 0 {
		return result
	}
	resultIDs := make(map[string]struct{}, len(result))
	coveredThemes := make(map[string]struct{})
	for _, entry := range result {
		resultIDs[entry.ID] = struct{}{}
		if themeID := expandedThemeByEntryID[entry.ID]; themeID != "" {
			coveredThemes[themeID] = struct{}{}
		}
	}
	totalTokens := 0
	for _, entry := range result {
		totalTokens += EstimateTextTokens(entry.Content)
	}
	for _, candidate := range candidates {
		themeID := expandedThemeByEntryID[candidate.ID]
		if themeID == "" {
			continue
		}
		if _, exists := resultIDs[candidate.ID]; exists {
			continue
		}
		if _, covered := coveredThemes[themeID]; covered {
			continue
		}
		candidateTokens := EstimateTextTokens(candidate.Content)
		if candidateTokens > maxTokens {
			continue
		}
		if len(result) < maxItems && totalTokens+candidateTokens <= maxTokens {
			result = append(result, candidate)
			resultIDs[candidate.ID] = struct{}{}
			coveredThemes[themeID] = struct{}{}
			totalTokens += candidateTokens
			continue
		}
		replaceIdx := -1
		for i := len(result) - 1; i >= 0; i-- {
			if expandedThemeByEntryID[result[i].ID] == "" {
				replacementTokens := totalTokens - EstimateTextTokens(result[i].Content) + candidateTokens
				if replacementTokens <= maxTokens {
					replaceIdx = i
					totalTokens = replacementTokens
					break
				}
			}
		}
		if replaceIdx < 0 {
			continue
		}
		delete(resultIDs, result[replaceIdx].ID)
		result[replaceIdx] = candidate
		resultIDs[candidate.ID] = struct{}{}
		coveredThemes[themeID] = struct{}{}
	}
	return result
}

func ensureAdaptiveSelectedThemeCoverage(result []Entry, candidates []Entry, selectedThemes []AdaptiveThemeSelection, entryThemeByID map[string]string, maxItems int, maxTokens int) ([]Entry, int) {
	if len(result) == 0 || len(candidates) == 0 || len(selectedThemes) == 0 {
		return result, 0
	}
	requiredThemes := make(map[string]struct{}, len(selectedThemes))
	for _, theme := range selectedThemes {
		if theme.ThemeID != "" {
			requiredThemes[theme.ThemeID] = struct{}{}
		}
	}
	if len(requiredThemes) == 0 {
		return result, 0
	}
	resultIDs := make(map[string]struct{}, len(result))
	coveredThemes := make(map[string]struct{})
	totalTokens := 0
	reserved := 0
	for _, entry := range result {
		resultIDs[entry.ID] = struct{}{}
		totalTokens += EstimateTextTokens(entry.Content)
		if themeID := entryThemeByID[entry.ID]; themeID != "" {
			coveredThemes[themeID] = struct{}{}
		}
	}
	for _, candidate := range candidates {
		themeID := entryThemeByID[candidate.ID]
		if themeID == "" {
			continue
		}
		if _, required := requiredThemes[themeID]; !required {
			continue
		}
		if _, covered := coveredThemes[themeID]; covered {
			continue
		}
		if _, exists := resultIDs[candidate.ID]; exists {
			continue
		}
		candidateTokens := EstimateTextTokens(candidate.Content)
		if candidateTokens > maxTokens {
			continue
		}
		if len(result) < maxItems && totalTokens+candidateTokens <= maxTokens {
			result = append(result, candidate)
			resultIDs[candidate.ID] = struct{}{}
			coveredThemes[themeID] = struct{}{}
			totalTokens += candidateTokens
			reserved++
			continue
		}
		replaceIdx := -1
		for i := len(result) - 1; i >= 0; i-- {
			currentTheme := entryThemeByID[result[i].ID]
			if currentTheme == themeID {
				continue
			}
			if _, required := requiredThemes[currentTheme]; required {
				if countThemeResults(result, entryThemeByID, currentTheme) <= 1 {
					continue
				}
			}
			replacementTokens := totalTokens - EstimateTextTokens(result[i].Content) + candidateTokens
			if replacementTokens <= maxTokens {
				replaceIdx = i
				totalTokens = replacementTokens
				break
			}
		}
		if replaceIdx < 0 {
			continue
		}
		delete(resultIDs, result[replaceIdx].ID)
		result[replaceIdx] = candidate
		resultIDs[candidate.ID] = struct{}{}
		coveredThemes[themeID] = struct{}{}
		reserved++
	}
	return result, reserved
}

func countThemeResults(entries []Entry, entryThemeByID map[string]string, themeID string) int {
	count := 0
	for _, entry := range entries {
		if entryThemeByID[entry.ID] == themeID {
			count++
		}
	}
	return count
}

func countSelectedThemeTargets(selectedThemes []AdaptiveThemeSelection) int {
	seen := make(map[string]struct{}, len(selectedThemes))
	for _, theme := range selectedThemes {
		if theme.ThemeID != "" {
			seen[theme.ThemeID] = struct{}{}
		}
	}
	return len(seen)
}

func countCoveredSelectedThemes(entries []Entry, selectedThemes []AdaptiveThemeSelection, entryThemeByID map[string]string) int {
	if len(entries) == 0 || len(selectedThemes) == 0 {
		return 0
	}
	required := make(map[string]struct{}, len(selectedThemes))
	for _, theme := range selectedThemes {
		if theme.ThemeID != "" {
			required[theme.ThemeID] = struct{}{}
		}
	}
	covered := make(map[string]struct{}, len(required))
	for _, entry := range entries {
		themeID := entryThemeByID[entry.ID]
		if _, ok := required[themeID]; ok {
			covered[themeID] = struct{}{}
		}
	}
	return len(covered)
}

func adaptiveResultEvidence(entries []Entry, seedIDs map[string]struct{}, expandedThemeByEntryID map[string]string, entryThemeByID ...map[string]string) []AdaptiveEntryEvidence {
	if len(entries) == 0 {
		return nil
	}
	var firstThemeByEntry map[string]string
	if len(entryThemeByID) > 0 {
		firstThemeByEntry = entryThemeByID[0]
	}
	out := make([]AdaptiveEntryEvidence, 0, len(entries))
	for i, entry := range entries {
		reason := "fallback_seed"
		if seedIDs != nil {
			if _, ok := seedIDs[entry.ID]; ok {
				reason = "seed"
			} else {
				reason = "theme_expansion"
			}
		}
		themeID := expandedThemeByEntryID[entry.ID]
		if themeID == "" && firstThemeByEntry != nil {
			themeID = firstThemeByEntry[entry.ID]
		}
		out = append(out, AdaptiveEntryEvidence{
			EntryID:    entry.ID,
			Reason:     reason,
			ThemeID:    themeID,
			SourceType: string(ClassifyExperienceSource(entry)),
			SourceURL:  entry.SourceURL,
			Rank:       i + 1,
		})
	}
	return out
}

func adaptiveFacetCoverage(facets []RecallQueryFacet, themes []AdaptiveThemeSelection, expanded []AdaptiveEntryEvidence) []AdaptiveFacetCoverage {
	if len(facets) == 0 {
		return nil
	}
	expandedByTheme := make(map[string][]string)
	for _, ev := range expanded {
		if ev.ThemeID == "" || ev.EntryID == "" {
			continue
		}
		expandedByTheme[ev.ThemeID] = append(expandedByTheme[ev.ThemeID], ev.EntryID)
	}
	out := make([]AdaptiveFacetCoverage, 0, len(facets))
	for _, facet := range facets {
		coverage := AdaptiveFacetCoverage{Kind: facet.Kind, Text: facet.Text}
		seenTheme := make(map[string]struct{})
		seenEntry := make(map[string]struct{})
		for _, theme := range themes {
			if theme.ThemeID == "" {
				continue
			}
			if !stringSliceContains(theme.MatchedFacets, facet.Kind) {
				continue
			}
			if _, ok := seenTheme[theme.ThemeID]; !ok {
				coverage.SelectedThemeIDs = append(coverage.SelectedThemeIDs, theme.ThemeID)
				seenTheme[theme.ThemeID] = struct{}{}
			}
			for _, entryID := range expandedByTheme[theme.ThemeID] {
				if _, ok := seenEntry[entryID]; ok {
					continue
				}
				coverage.ExpandedEntryIDs = append(coverage.ExpandedEntryIDs, entryID)
				seenEntry[entryID] = struct{}{}
			}
		}
		out = append(out, coverage)
	}
	return out
}

func adaptiveThemeAggregates(themes []AdaptiveThemeSelection, seeds []Entry, results []Entry, expanded []AdaptiveEntryEvidence) []AdaptiveThemeAggregate {
	if len(themes) == 0 {
		return nil
	}
	seedByID := make(map[string]Entry, len(seeds))
	for _, seed := range seeds {
		seedByID[seed.ID] = seed
	}
	resultByID := make(map[string]Entry, len(results))
	for _, result := range results {
		resultByID[result.ID] = result
	}
	expandedByTheme := make(map[string][]string)
	for _, ev := range expanded {
		if ev.ThemeID == "" || ev.EntryID == "" {
			continue
		}
		expandedByTheme[ev.ThemeID] = append(expandedByTheme[ev.ThemeID], ev.EntryID)
	}

	aggregates := make([]AdaptiveThemeAggregate, 0, len(themes))
	for _, theme := range themes {
		if theme.ThemeID == "" {
			continue
		}
		aggregate := AdaptiveThemeAggregate{
			ThemeID:       theme.ThemeID,
			Summary:       theme.Summary,
			MatchedFacets: append([]string(nil), theme.MatchedFacets...),
			SourceCounts:  make(map[string]int),
		}
		seenSeed := make(map[string]struct{})
		seenResult := make(map[string]struct{})
		for _, id := range theme.EntryIDs {
			if _, ok := seedByID[id]; ok {
				if _, seen := seenSeed[id]; !seen {
					aggregate.SeedEntryIDs = append(aggregate.SeedEntryIDs, id)
					seenSeed[id] = struct{}{}
				}
			}
			if entry, ok := resultByID[id]; ok {
				if _, seen := seenResult[id]; !seen {
					aggregate.ResultEntryIDs = append(aggregate.ResultEntryIDs, id)
					aggregate.TokenEstimate += EstimateTextTokens(entry.Content)
					source := strings.TrimSpace(string(ClassifyExperienceSource(entry)))
					if source == "" {
						source = string(ExperienceSourceUnknown)
					}
					aggregate.SourceCounts[source]++
					if len(aggregate.ResultPreviews) < 3 {
						aggregate.ResultPreviews = append(aggregate.ResultPreviews, themeContentPreview(entry.Content, 120))
					}
					seenResult[id] = struct{}{}
				}
			}
		}
		seenExpanded := make(map[string]struct{})
		for _, id := range expandedByTheme[theme.ThemeID] {
			if _, seen := seenExpanded[id]; seen {
				continue
			}
			aggregate.ExpandedEntryIDs = append(aggregate.ExpandedEntryIDs, id)
			seenExpanded[id] = struct{}{}
		}
		if len(aggregate.SourceCounts) == 0 {
			aggregate.SourceCounts = nil
		}
		if len(aggregate.SeedEntryIDs) == 0 && len(aggregate.ExpandedEntryIDs) == 0 && len(aggregate.ResultEntryIDs) == 0 {
			continue
		}
		aggregates = append(aggregates, aggregate)
	}
	return aggregates
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
