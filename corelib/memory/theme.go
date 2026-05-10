package memory

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultThemeAttachThreshold = 0.62
	defaultThemeMergeThreshold  = 0.90
	defaultThemeMinCohesion     = 0.72
	defaultThemeMaxSize         = 12
	defaultThemeNeighborK       = 10
)

// ThemeNode is a higher-level, embedding-aware grouping of related memory
// entries. It is the xMemory-style layer above individual semantic/episodic
// entries and below profile-level summaries.
type ThemeNode struct {
	ID           string    `json:"id"`
	Summary      string    `json:"summary"`
	Centroid     []float32 `json:"centroid,omitempty"`
	EntryIDs     []string  `json:"entry_ids"`
	MemberCount  int       `json:"member_count"`
	Tags         []string  `json:"tags,omitempty"`
	Neighbors    []string  `json:"neighbors,omitempty"`
	NeighborSims []float64 `json:"neighbor_sims,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ThemeHealth summarizes how well the theme layer covers active memory.
// It is intentionally cheap to compute so tools can expose it as diagnostics.
type ThemeHealth struct {
	ThemeCount            int     `json:"theme_count"`
	ActiveEligibleEntries int     `json:"active_eligible_entries"`
	CoveredEntries        int     `json:"covered_entries"`
	UncoveredEntries      int     `json:"uncovered_entries"`
	CoverageRate          float64 `json:"coverage_rate"`
	EntryReferences       int     `json:"entry_references"`
	DuplicateEntryRefs    int     `json:"duplicate_entry_refs"`
	AverageThemeSize      float64 `json:"average_theme_size"`
	MaxThemeSize          int     `json:"max_theme_size"`
	NeighborLinks         int     `json:"neighbor_links"`
	IsolatedThemes        int     `json:"isolated_themes"`
	ThemesWithCentroid    int     `json:"themes_with_centroid"`
	ThemesWithoutCentroid int     `json:"themes_without_centroid"`
}

// ThemeEvidence points to representative raw memories behind a theme.
type ThemeEvidence struct {
	EntryID        string    `json:"entry_id"`
	Category       Category  `json:"category,omitempty"`
	ContentPreview string    `json:"content_preview,omitempty"`
	SourceType     string    `json:"source_type,omitempty"`
	SourceURL      string    `json:"source_url,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	AccessCount    int       `json:"access_count,omitempty"`
	Similarity     float64   `json:"similarity,omitempty"`
	Reason         string    `json:"reason,omitempty"`
}

// ThemeExplanation combines a theme with representative source memories.
type ThemeExplanation struct {
	Theme    ThemeNode       `json:"theme"`
	Evidence []ThemeEvidence `json:"evidence,omitempty"`
	Cohesion float64         `json:"cohesion,omitempty"`
}

// ThemeIssue is an actionable diagnostic finding for the theme layer.
type ThemeIssue struct {
	Kind       string   `json:"kind"`
	Severity   string   `json:"severity"`
	ThemeID    string   `json:"theme_id,omitempty"`
	EntryID    string   `json:"entry_id,omitempty"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion"`
	EntryIDs   []string `json:"entry_ids,omitempty"`
}

// ThemeDiagnosticReport groups theme health with actionable issues.
type ThemeDiagnosticReport struct {
	Health ThemeHealth  `json:"health"`
	Issues []ThemeIssue `json:"issues,omitempty"`
}

// ThemeMaintenanceAction is a proposed, non-destructive maintenance step.
type ThemeMaintenanceAction struct {
	Action     string   `json:"action"`
	Priority   string   `json:"priority"`
	Reason     string   `json:"reason"`
	ThemeID    string   `json:"theme_id,omitempty"`
	EntryIDs   []string `json:"entry_ids,omitempty"`
	IssueKinds []string `json:"issue_kinds,omitempty"`
}

// ThemeMaintenancePlan summarizes recommended upkeep for the theme layer.
type ThemeMaintenancePlan struct {
	Health  ThemeHealth              `json:"health"`
	Actions []ThemeMaintenanceAction `json:"actions,omitempty"`
}

// ThemeManager maintains a compact, diverse theme layer for memory recall.
// It is intentionally dependency-light: embeddings are already stored on Entry,
// and LLM summaries are optional.
type ThemeManager struct {
	mu     sync.RWMutex
	themes []ThemeNode

	attachThreshold float64
	mergeThreshold  float64
	minCohesion     float64
	maxThemeSize    int
	neighborK       int
}

func NewThemeManager() *ThemeManager {
	return &ThemeManager{
		attachThreshold: defaultThemeAttachThreshold,
		mergeThreshold:  defaultThemeMergeThreshold,
		minCohesion:     defaultThemeMinCohesion,
		maxThemeSize:    defaultThemeMaxSize,
		neighborK:       defaultThemeNeighborK,
	}
}

// Rebuild reconstructs the theme layer from active entries. Entries with
// embeddings use centroid attachment; entries without embeddings can still
// contribute to lightweight tag-derived themes.
func (tm *ThemeManager) Rebuild(entries []Entry, llm LLMChatCaller) []ThemeNode {
	if tm == nil {
		return nil
	}
	now := time.Now()
	entryByID := make(map[string]Entry, len(entries))
	var embedded []Entry
	var fallback []Entry
	for _, e := range entries {
		if !e.IsActive() || !themeEntryAllowed(e) {
			continue
		}
		entryByID[e.ID] = e
		if len(e.Embedding) > 0 {
			embedded = append(embedded, e)
		} else {
			fallback = append(fallback, e)
		}
	}

	var themes []ThemeNode
	for _, e := range embedded {
		bestIdx := -1
		bestSim := 0.0
		for i := range themes {
			if len(themes[i].EntryIDs) >= tm.maxThemeSize {
				continue
			}
			sim := cosineFloat32(e.Embedding, themes[i].Centroid)
			if sim > bestSim {
				bestSim = sim
				bestIdx = i
			}
		}
		if bestIdx >= 0 && bestSim >= tm.attachThreshold {
			themes[bestIdx].EntryIDs = append(themes[bestIdx].EntryIDs, e.ID)
			themes[bestIdx].MemberCount = len(themes[bestIdx].EntryIDs)
			themes[bestIdx].Tags = mergeTags(themes[bestIdx].Tags, themeEntryTags(e))
			themes[bestIdx].Centroid = recomputeThemeCentroid(themes[bestIdx].EntryIDs, entryByID)
			themes[bestIdx].UpdatedAt = now
			continue
		}
		themes = append(themes, ThemeNode{
			ID:          themeIDFromEntry(e.ID),
			Summary:     themeSummaryFromEntries([]string{e.ID}, entryByID),
			Centroid:    append([]float32(nil), e.Embedding...),
			EntryIDs:    []string{e.ID},
			MemberCount: 1,
			Tags:        themeEntryTags(e),
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	themes = tm.splitOversizedOrHeterogeneous(themes, entryByID, now)
	themes = tm.mergeCloseThemes(themes, entryByID, now)
	themes = append(themes, tm.buildFallbackTagThemes(fallback, entryByID, now)...)

	for i := range themes {
		themes[i].EntryIDs = uniqueSortedStrings(themes[i].EntryIDs)
		themes[i].MemberCount = len(themes[i].EntryIDs)
		themes[i].Tags = uniqueSortedStrings(themes[i].Tags)
		themes[i].Summary = tm.summarizeTheme(themes[i], entryByID, llm)
	}
	recomputeThemeNeighbors(themes, tm.neighborK)

	tm.mu.Lock()
	tm.themes = themes
	tm.mu.Unlock()
	return themes
}

func themeEntryAllowed(e Entry) bool {
	switch MapToCanonical(e.Category) {
	case CategorySelfIdentity:
		return false
	default:
		return true
	}
}

func themeEntryTags(e Entry) []string {
	tags := append([]string(nil), e.Tags...)
	for _, ent := range e.Entities {
		if name, ok := semanticEntityTokenName(ent); ok {
			tags = append(tags, name)
		}
	}
	out := tags[:0]
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if isTrivialTag(tag) {
			continue
		}
		out = append(out, tag)
	}
	return uniqueSortedStrings(out)
}

func themeIDFromEntry(entryID string) string {
	if entryID == "" {
		return fmt.Sprintf("theme_%d", time.Now().UnixNano())
	}
	return "theme_" + entryID
}

func (tm *ThemeManager) splitOversizedOrHeterogeneous(themes []ThemeNode, entryByID map[string]Entry, now time.Time) []ThemeNode {
	var out []ThemeNode
	for _, theme := range themes {
		if len(theme.EntryIDs) <= 1 {
			out = append(out, theme)
			continue
		}
		cohesion := themeCohesion(theme.EntryIDs, entryByID)
		if len(theme.EntryIDs) <= tm.maxThemeSize && cohesion >= tm.minCohesion {
			out = append(out, theme)
			continue
		}
		for idx, ids := range splitThemeEntryIDs(theme.EntryIDs, entryByID, tm.maxThemeSize) {
			if len(ids) == 0 {
				continue
			}
			out = append(out, ThemeNode{
				ID:          fmt.Sprintf("%s_%d", theme.ID, idx),
				Summary:     themeSummaryFromEntries(ids, entryByID),
				Centroid:    recomputeThemeCentroid(ids, entryByID),
				EntryIDs:    ids,
				MemberCount: len(ids),
				Tags:        themeTagsForEntries(ids, entryByID),
				CreatedAt:   theme.CreatedAt,
				UpdatedAt:   now,
			})
		}
	}
	return out
}

func splitThemeEntryIDs(ids []string, entryByID map[string]Entry, maxSize int) [][]string {
	if len(ids) <= maxSize {
		return [][]string{ids}
	}
	remaining := append([]string(nil), ids...)
	var groups [][]string
	for len(remaining) > 0 {
		seed := remaining[0]
		group := []string{seed}
		remaining = remaining[1:]
		sort.SliceStable(remaining, func(i, j int) bool {
			return cosineFloat32(entryByID[remaining[i]].Embedding, entryByID[seed].Embedding) >
				cosineFloat32(entryByID[remaining[j]].Embedding, entryByID[seed].Embedding)
		})
		take := maxSize - 1
		if take > len(remaining) {
			take = len(remaining)
		}
		group = append(group, remaining[:take]...)
		remaining = remaining[take:]
		groups = append(groups, group)
	}
	return groups
}

func (tm *ThemeManager) mergeCloseThemes(themes []ThemeNode, entryByID map[string]Entry, now time.Time) []ThemeNode {
	merged := make([]bool, len(themes))
	var out []ThemeNode
	for i := range themes {
		if merged[i] {
			continue
		}
		base := themes[i]
		for j := i + 1; j < len(themes); j++ {
			if merged[j] {
				continue
			}
			if len(base.EntryIDs)+len(themes[j].EntryIDs) > tm.maxThemeSize {
				continue
			}
			if cosineFloat32(base.Centroid, themes[j].Centroid) < tm.mergeThreshold {
				continue
			}
			base.EntryIDs = append(base.EntryIDs, themes[j].EntryIDs...)
			base.Tags = mergeTags(base.Tags, themes[j].Tags)
			base.Centroid = recomputeThemeCentroid(base.EntryIDs, entryByID)
			base.MemberCount = len(base.EntryIDs)
			base.UpdatedAt = now
			merged[j] = true
		}
		out = append(out, base)
	}
	return out
}

func (tm *ThemeManager) buildFallbackTagThemes(entries []Entry, entryByID map[string]Entry, now time.Time) []ThemeNode {
	tagToIDs := make(map[string][]string)
	for _, e := range entries {
		for _, tag := range themeEntryTags(e) {
			tagToIDs[tag] = append(tagToIDs[tag], e.ID)
		}
	}
	var themes []ThemeNode
	for tag, ids := range tagToIDs {
		ids = uniqueSortedStrings(ids)
		if len(ids) < 3 {
			continue
		}
		themes = append(themes, ThemeNode{
			ID:          "theme_tag_" + strings.ReplaceAll(tag, " ", "_"),
			Summary:     themeSummaryFromEntries(ids, entryByID),
			EntryIDs:    ids,
			MemberCount: len(ids),
			Tags:        []string{tag},
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	sort.SliceStable(themes, func(i, j int) bool { return themes[i].ID < themes[j].ID })
	return themes
}

func (tm *ThemeManager) summarizeTheme(theme ThemeNode, entryByID map[string]Entry, llm LLMChatCaller) string {
	if llm != nil && llm.IsConfigured() && len(theme.EntryIDs) >= 3 {
		var sb strings.Builder
		limit := len(theme.EntryIDs)
		if limit > 12 {
			limit = 12
		}
		for _, id := range theme.EntryIDs[:limit] {
			content := strings.TrimSpace(entryByID[id].Content)
			if content == "" {
				continue
			}
			runes := []rune(content)
			if len(runes) > 180 {
				content = string(runes[:180])
			}
			fmt.Fprintf(&sb, "- %s\n", content)
		}
		if strings.TrimSpace(sb.String()) != "" {
			prompt := fmt.Sprintf("Summarize these related memory entries into one concise theme title or sentence. Return only the summary.\n\n%s", sb.String())
			resp, err := llm.ChatCall([]map[string]string{{"role": "user", "content": prompt}})
			if err == nil && strings.TrimSpace(resp) != "" {
				return strings.TrimSpace(resp)
			}
		}
	}
	return themeSummaryFromEntries(theme.EntryIDs, entryByID)
}

func themeSummaryFromEntries(ids []string, entryByID map[string]Entry) string {
	tags := themeTagsForEntries(ids, entryByID)
	if len(tags) > 0 {
		if len(tags) > 3 {
			tags = tags[:3]
		}
		return strings.Join(tags, " / ")
	}
	for _, id := range ids {
		if content := strings.TrimSpace(entryByID[id].Content); content != "" {
			runes := []rune(content)
			if len(runes) > 80 {
				content = string(runes[:80])
			}
			return content
		}
	}
	return "memory theme"
}

func themeTagsForEntries(ids []string, entryByID map[string]Entry) []string {
	var tags []string
	for _, id := range ids {
		tags = mergeTags(tags, themeEntryTags(entryByID[id]))
	}
	return uniqueSortedStrings(tags)
}

func recomputeThemeCentroid(ids []string, entryByID map[string]Entry) []float32 {
	var sum []float32
	count := 0
	for _, id := range ids {
		emb := entryByID[id].Embedding
		if len(emb) == 0 {
			continue
		}
		if len(sum) == 0 {
			sum = make([]float32, len(emb))
		}
		if len(sum) != len(emb) {
			continue
		}
		for i := range emb {
			sum[i] += emb[i]
		}
		count++
	}
	if count == 0 {
		return nil
	}
	for i := range sum {
		sum[i] /= float32(count)
	}
	normalizeFloat32InPlace(sum)
	return sum
}

func themeCohesion(ids []string, entryByID map[string]Entry) float64 {
	if len(ids) <= 1 {
		return 1
	}
	pairs := 0
	total := 0.0
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			a, b := entryByID[ids[i]].Embedding, entryByID[ids[j]].Embedding
			if len(a) == 0 || len(a) != len(b) {
				continue
			}
			total += cosineFloat32(a, b)
			pairs++
		}
	}
	if pairs == 0 {
		return 1
	}
	return total / float64(pairs)
}

func recomputeThemeNeighbors(themes []ThemeNode, k int) {
	for i := range themes {
		type neighbor struct {
			id  string
			sim float64
		}
		var ns []neighbor
		for j := range themes {
			if i == j || len(themes[i].Centroid) == 0 || len(themes[j].Centroid) == 0 {
				continue
			}
			sim := cosineFloat32(themes[i].Centroid, themes[j].Centroid)
			if sim <= 0 {
				continue
			}
			ns = append(ns, neighbor{id: themes[j].ID, sim: sim})
		}
		sort.SliceStable(ns, func(a, b int) bool {
			if ns[a].sim != ns[b].sim {
				return ns[a].sim > ns[b].sim
			}
			return ns[a].id < ns[b].id
		})
		if len(ns) > k {
			ns = ns[:k]
		}
		themes[i].Neighbors = themes[i].Neighbors[:0]
		themes[i].NeighborSims = themes[i].NeighborSims[:0]
		for _, n := range ns {
			themes[i].Neighbors = append(themes[i].Neighbors, n.id)
			themes[i].NeighborSims = append(themes[i].NeighborSims, n.sim)
		}
	}
}

func cosineFloat32(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	dot, na, nb := 0.0, 0.0, 0.0
	for i := range a {
		af, bf := float64(a[i]), float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func normalizeFloat32InPlace(v []float32) {
	sum := 0.0
	for _, x := range v {
		sum += float64(x * x)
	}
	if sum == 0 {
		return
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
}

func uniqueSortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Themes returns a stable snapshot of current theme nodes.
func (tm *ThemeManager) Themes() []ThemeNode {
	if tm == nil {
		return nil
	}
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	out := make([]ThemeNode, len(tm.themes))
	copy(out, tm.themes)
	return out
}

// TopThemes returns themes sorted for inspection: larger themes first, then
// most recently updated, then stable ID order. A non-positive limit returns all.
func (tm *ThemeManager) TopThemes(limit int) []ThemeNode {
	themes := tm.Themes()
	sort.SliceStable(themes, func(i, j int) bool {
		if themes[i].MemberCount != themes[j].MemberCount {
			return themes[i].MemberCount > themes[j].MemberCount
		}
		if !themes[i].UpdatedAt.Equal(themes[j].UpdatedAt) {
			return themes[i].UpdatedAt.After(themes[j].UpdatedAt)
		}
		return themes[i].ID < themes[j].ID
	})
	if limit > 0 && len(themes) > limit {
		themes = themes[:limit]
	}
	return themes
}

// ExplainThemes returns top themes plus representative raw memory evidence.
func (tm *ThemeManager) ExplainThemes(entries []Entry, themeLimit int, evidenceLimit int) []ThemeExplanation {
	if evidenceLimit <= 0 {
		evidenceLimit = 3
	}
	entryByID := make(map[string]Entry, len(entries))
	for _, e := range entries {
		entryByID[e.ID] = e
	}
	themes := tm.TopThemes(themeLimit)
	out := make([]ThemeExplanation, 0, len(themes))
	for _, theme := range themes {
		out = append(out, ThemeExplanation{
			Theme:    theme,
			Evidence: representativeThemeEvidence(theme, entryByID, evidenceLimit),
			Cohesion: themeCohesion(theme.EntryIDs, entryByID),
		})
	}
	return out
}

// DiagnoseThemes reports actionable health issues in the current theme layer.
func (tm *ThemeManager) DiagnoseThemes(entries []Entry, limit int) ThemeDiagnosticReport {
	health := tm.Health(entries)
	if limit <= 0 {
		limit = 50
	}
	themes := tm.Themes()
	entryByID := make(map[string]Entry, len(entries))
	eligible := make(map[string]Entry, len(entries))
	coveredBy := make(map[string][]string, len(entries))
	for _, e := range entries {
		entryByID[e.ID] = e
		if !e.IsActive() || !themeEntryAllowed(e) || strings.TrimSpace(e.ID) == "" {
			continue
		}
		eligible[e.ID] = e
	}

	var issues []ThemeIssue
	addIssue := func(issue ThemeIssue) {
		if len(issues) < limit {
			issues = append(issues, issue)
		}
	}

	if health.ActiveEligibleEntries > 0 && health.CoverageRate < 0.90 {
		addIssue(ThemeIssue{
			Kind:       "low_coverage",
			Severity:   severityForRatio(health.CoverageRate, 0.50, 0.80),
			Message:    fmt.Sprintf("theme coverage is %.2f (%d/%d)", health.CoverageRate, health.CoveredEntries, health.ActiveEligibleEntries),
			Suggestion: "Backfill embeddings or add meaningful tags for uncovered active memories, then rebuild the theme layer.",
		})
	}

	for _, theme := range themes {
		for _, id := range theme.EntryIDs {
			if _, ok := eligible[id]; ok {
				coveredBy[id] = append(coveredBy[id], theme.ID)
			}
		}
		cohesion := themeCohesion(theme.EntryIDs, entryByID)
		if len(theme.EntryIDs) > 2 && cohesion < tm.minCohesion {
			addIssue(ThemeIssue{
				Kind:       "low_cohesion_theme",
				Severity:   severityForRatio(cohesion, tm.minCohesion-0.25, tm.minCohesion-0.10),
				ThemeID:    theme.ID,
				Message:    fmt.Sprintf("theme cohesion is %.2f for %d members", cohesion, len(theme.EntryIDs)),
				Suggestion: "Split this theme or tune attach/split thresholds so unrelated memories stop sharing one centroid.",
				EntryIDs:   append([]string(nil), theme.EntryIDs...),
			})
		}
		if tm.maxThemeSize > 0 && len(theme.EntryIDs) >= tm.maxThemeSize {
			addIssue(ThemeIssue{
				Kind:       "theme_at_capacity",
				Severity:   "medium",
				ThemeID:    theme.ID,
				Message:    fmt.Sprintf("theme has %d members and reached the max theme size", len(theme.EntryIDs)),
				Suggestion: "Review representative evidence; if it mixes decisions or time periods, split the theme before further growth.",
				EntryIDs:   append([]string(nil), theme.EntryIDs...),
			})
		}
		if health.ThemeCount > 1 && len(theme.Neighbors) == 0 {
			addIssue(ThemeIssue{
				Kind:       "isolated_theme",
				Severity:   "low",
				ThemeID:    theme.ID,
				Message:    "theme has no neighbor links",
				Suggestion: "Check whether this is a genuine standalone topic or a missing-embedding/tag artifact.",
				EntryIDs:   append([]string(nil), theme.EntryIDs...),
			})
		}
	}

	for id, entry := range eligible {
		if len(coveredBy[id]) == 0 {
			suggestion := "Add an embedding or stronger tags so this memory can attach to a theme."
			if len(entry.Embedding) > 0 {
				suggestion = "Review theme thresholds; this embedded memory did not attach to any retained theme."
			}
			addIssue(ThemeIssue{
				Kind:       "uncovered_entry",
				Severity:   "medium",
				EntryID:    id,
				Message:    fmt.Sprintf("active memory is not represented in any theme: %s", themeContentPreview(entry.Content, 80)),
				Suggestion: suggestion,
			})
			continue
		}
		if len(coveredBy[id]) > 1 {
			addIssue(ThemeIssue{
				Kind:       "duplicate_entry_reference",
				Severity:   "medium",
				EntryID:    id,
				Message:    fmt.Sprintf("memory appears in %d themes", len(coveredBy[id])),
				Suggestion: "Deduplicate fallback tag themes or prefer the strongest centroid-backed theme for this entry.",
				EntryIDs:   append([]string(nil), coveredBy[id]...),
			})
		}
	}

	sort.SliceStable(issues, func(i, j int) bool {
		si, sj := severityRank(issues[i].Severity), severityRank(issues[j].Severity)
		if si != sj {
			return si > sj
		}
		if issues[i].Kind != issues[j].Kind {
			return issues[i].Kind < issues[j].Kind
		}
		if issues[i].ThemeID != issues[j].ThemeID {
			return issues[i].ThemeID < issues[j].ThemeID
		}
		return issues[i].EntryID < issues[j].EntryID
	})
	if len(issues) > limit {
		issues = issues[:limit]
	}
	return ThemeDiagnosticReport{Health: health, Issues: issues}
}

// PlanThemeMaintenance converts diagnostics into a compact, non-destructive
// action plan that operators or future automation can apply deliberately.
func PlanThemeMaintenance(report ThemeDiagnosticReport, limit int) ThemeMaintenancePlan {
	if limit <= 0 {
		limit = 20
	}
	type grouped struct {
		action     ThemeMaintenanceAction
		severity   int
		entrySet   map[string]struct{}
		issueKinds map[string]struct{}
	}
	groups := map[string]*grouped{}
	add := func(key string, issue ThemeIssue, action string, reason string, themeID string, entryIDs []string) {
		g, ok := groups[key]
		if !ok {
			g = &grouped{
				action: ThemeMaintenanceAction{
					Action:   action,
					Priority: issue.Severity,
					Reason:   reason,
					ThemeID:  themeID,
				},
				severity:   severityRank(issue.Severity),
				entrySet:   map[string]struct{}{},
				issueKinds: map[string]struct{}{},
			}
			groups[key] = g
		}
		if r := severityRank(issue.Severity); r > g.severity {
			g.severity = r
			g.action.Priority = issue.Severity
		}
		g.issueKinds[issue.Kind] = struct{}{}
		for _, id := range entryIDs {
			if strings.TrimSpace(id) != "" {
				g.entrySet[id] = struct{}{}
			}
		}
	}

	for _, issue := range report.Issues {
		switch issue.Kind {
		case "low_coverage":
			add("backfill_theme_inputs", issue, "backfill_theme_inputs", "Improve embeddings/tags for uncovered memories, then rebuild themes.", "", nil)
		case "uncovered_entry":
			add("backfill_theme_inputs", issue, "backfill_theme_inputs", "Improve embeddings/tags for uncovered memories, then rebuild themes.", "", []string{issue.EntryID})
		case "low_cohesion_theme":
			add("split_theme:"+issue.ThemeID, issue, "review_split_theme", "Theme members appear semantically mixed; review evidence and split if needed.", issue.ThemeID, issue.EntryIDs)
		case "theme_at_capacity":
			add("split_theme:"+issue.ThemeID, issue, "review_split_theme", "Theme is at capacity; split by decision, time period, or subtopic before further growth.", issue.ThemeID, issue.EntryIDs)
		case "isolated_theme":
			add("review_isolated:"+issue.ThemeID, issue, "review_isolated_theme", "Theme has no neighbor links; verify whether it is standalone or missing useful metadata.", issue.ThemeID, issue.EntryIDs)
		case "duplicate_entry_reference":
			add("dedupe_entry_refs", issue, "deduplicate_theme_membership", "Memory appears in multiple themes; keep the strongest theme assignment or deduplicate fallback tag themes.", "", []string{issue.EntryID})
		}
	}

	actions := make([]ThemeMaintenanceAction, 0, len(groups))
	for _, g := range groups {
		for id := range g.entrySet {
			g.action.EntryIDs = append(g.action.EntryIDs, id)
		}
		sort.Strings(g.action.EntryIDs)
		for kind := range g.issueKinds {
			g.action.IssueKinds = append(g.action.IssueKinds, kind)
		}
		sort.Strings(g.action.IssueKinds)
		actions = append(actions, g.action)
	}
	sort.SliceStable(actions, func(i, j int) bool {
		ri, rj := severityRank(actions[i].Priority), severityRank(actions[j].Priority)
		if ri != rj {
			return ri > rj
		}
		if actions[i].Action != actions[j].Action {
			return actions[i].Action < actions[j].Action
		}
		return actions[i].ThemeID < actions[j].ThemeID
	})
	if len(actions) > limit {
		actions = actions[:limit]
	}
	return ThemeMaintenancePlan{Health: report.Health, Actions: actions}
}

func severityForRatio(value, highCutoff, mediumCutoff float64) string {
	if value < highCutoff {
		return "high"
	}
	if value < mediumCutoff {
		return "medium"
	}
	return "low"
}

func severityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func representativeThemeEvidence(theme ThemeNode, entryByID map[string]Entry, limit int) []ThemeEvidence {
	type scored struct {
		entry Entry
		sim   float64
		score float64
	}
	items := make([]scored, 0, len(theme.EntryIDs))
	for _, id := range theme.EntryIDs {
		entry, ok := entryByID[id]
		if !ok {
			continue
		}
		sim := cosineFloat32(entry.Embedding, theme.Centroid)
		score := sim*2 + math.Log1p(float64(entry.AccessCount))*0.05
		if !entry.UpdatedAt.IsZero() {
			score += 0.01
		}
		if strings.TrimSpace(entry.Content) != "" {
			score += 0.01
		}
		items = append(items, scored{entry: entry, sim: sim, score: score})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		if !items[i].entry.UpdatedAt.Equal(items[j].entry.UpdatedAt) {
			return items[i].entry.UpdatedAt.After(items[j].entry.UpdatedAt)
		}
		return items[i].entry.ID < items[j].entry.ID
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]ThemeEvidence, 0, len(items))
	for _, item := range items {
		reason := "tag_or_recent_representative"
		if item.sim > 0 {
			reason = "centroid_representative"
		}
		out = append(out, ThemeEvidence{
			EntryID:        item.entry.ID,
			Category:       item.entry.Category,
			ContentPreview: themeContentPreview(item.entry.Content, 140),
			SourceType:     string(ClassifyExperienceSource(item.entry)),
			SourceURL:      item.entry.SourceURL,
			UpdatedAt:      item.entry.UpdatedAt,
			AccessCount:    item.entry.AccessCount,
			Similarity:     item.sim,
			Reason:         reason,
		})
	}
	return out
}

func themeContentPreview(content string, limit int) string {
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if content == "" || limit <= 0 {
		return content
	}
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit]) + "..."
}

// Health reports coverage and connectivity diagnostics for the current theme
// layer against active, theme-eligible entries.
func (tm *ThemeManager) Health(entries []Entry) ThemeHealth {
	themes := tm.Themes()
	eligible := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if !e.IsActive() || !themeEntryAllowed(e) {
			continue
		}
		if strings.TrimSpace(e.ID) == "" {
			continue
		}
		eligible[e.ID] = struct{}{}
	}

	covered := make(map[string]struct{}, len(eligible))
	health := ThemeHealth{
		ThemeCount:            len(themes),
		ActiveEligibleEntries: len(eligible),
	}
	for _, theme := range themes {
		size := len(theme.EntryIDs)
		health.EntryReferences += size
		if size > health.MaxThemeSize {
			health.MaxThemeSize = size
		}
		health.NeighborLinks += len(theme.Neighbors)
		if len(theme.Neighbors) == 0 {
			health.IsolatedThemes++
		}
		if len(theme.Centroid) > 0 {
			health.ThemesWithCentroid++
		} else {
			health.ThemesWithoutCentroid++
		}
		for _, id := range theme.EntryIDs {
			if _, ok := eligible[id]; ok {
				covered[id] = struct{}{}
			}
		}
	}
	health.CoveredEntries = len(covered)
	health.UncoveredEntries = health.ActiveEligibleEntries - health.CoveredEntries
	if health.UncoveredEntries < 0 {
		health.UncoveredEntries = 0
	}
	if health.ActiveEligibleEntries > 0 {
		health.CoverageRate = float64(health.CoveredEntries) / float64(health.ActiveEligibleEntries)
	}
	if health.ThemeCount > 0 {
		health.AverageThemeSize = float64(health.EntryReferences) / float64(health.ThemeCount)
	}
	if health.EntryReferences > health.CoveredEntries {
		health.DuplicateEntryRefs = health.EntryReferences - health.CoveredEntries
	}
	return health
}
