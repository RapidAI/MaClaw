package memory

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// RecallEntriesPromptOptions controls shared prompt rendering for automatic
// memory recall sections.
type RecallEntriesPromptOptions struct {
	Header   string
	Intro    string
	Footer   string
	MaxRunes int
	// SourceNumbering enables [M1], [M2], ... prefixes on each entry and appends
	// a source-attribution instruction at the end. Implements the Dreaming V3
	// "sources" concept: LLM can cite which memories influenced its answer.
	SourceNumbering bool
}

// FormatRecallEntriesForPrompt renders recalled entries for prompt injection in
// a stable format shared by GUI, TUI, and server agents.
func FormatRecallEntriesForPrompt(entries []Entry, opts RecallEntriesPromptOptions) string {
	if len(entries) == 0 {
		return ""
	}
	maxRunes := opts.MaxRunes
	if maxRunes <= 0 {
		maxRunes = 200
	}
	var b strings.Builder
	writePromptLine := func(value string) {
		if value == "" {
			return
		}
		b.WriteString(value)
		if !strings.HasSuffix(value, "\n") {
			b.WriteByte('\n')
		}
	}
	writePromptLine(opts.Header)
	writePromptLine(opts.Intro)
	for i, entry := range entries {
		line := FormatRecallEntryForPrompt(entry, maxRunes)
		if opts.SourceNumbering {
			line = "[M" + strconv.Itoa(i+1) + "] " + line
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	writePromptLine(opts.Footer)
	if opts.SourceNumbering && len(entries) > 0 {
		b.WriteString("（如果你的回答使用了上述记忆信息，请在末尾用 📌 来源：[MX] 标注使用了哪些条目。）\n")
	}
	return b.String()
}

func FormatExperienceCandidatesForPrompt(candidates []lifecycle.Candidate, opts RecallEntriesPromptOptions) string {
	if len(candidates) == 0 {
		return ""
	}
	maxRunes := opts.MaxRunes
	if maxRunes <= 0 {
		maxRunes = 200
	}
	var b strings.Builder
	writePromptLine := func(value string) {
		if value == "" {
			return
		}
		b.WriteString(value)
		if !strings.HasSuffix(value, "\n") {
			b.WriteByte('\n')
		}
	}
	writePromptLine(opts.Header)
	writePromptLine(opts.Intro)
	for i, candidate := range candidates {
		line := FormatExperienceCandidateForPrompt(candidate, maxRunes)
		if opts.SourceNumbering {
			line = "[M" + strconv.Itoa(i+1) + "] " + line
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	writePromptLine(opts.Footer)
	if opts.SourceNumbering && len(candidates) > 0 {
		b.WriteString("（如果你的回答使用了上述记忆信息，请在末尾用 📌 来源：[MX] 标注使用了哪些条目。）\n")
	}
	return b.String()
}

func FormatExperienceCandidateForPrompt(candidate lifecycle.Candidate, maxRunes int) string {
	entry := candidate.Entry
	text := firstNonEmptyString(entry.Content, entry.WhenToUse)
	if maxRunes > 0 {
		runes := []rune(text)
		if len(runes) > maxRunes {
			text = string(runes[:maxRunes]) + "..."
		}
	}
	entryType := string(entry.EntryType)
	if entryType == "" {
		entryType = "experience"
	}
	line := "- [" + entryType + "] " + text
	if entry.WhenToUse != "" && entry.WhenToUse != text {
		line += " (use: " + entry.WhenToUse + ")"
	}
	if entry.SourceURL == "" {
		return line
	}
	line += " (source: " + entry.SourceURL
	if LooksLikeFilePath(entry.SourceURL) {
		line += "; full: read_file"
	}
	line += ")"
	return line
}

// ProactivePromptOptions controls the complete dynamic memory context section
// injected into prompts by GUI, TUI, VE, and server agents.
type ProactivePromptOptions struct {
	Recall       ProactiveRecallOptions
	EventContext lifecycle.EventContext
	Policy       lifecycle.RetrievalPolicy
	TokenBudget  int

	IncludeMemoryIndex bool
	MemoryIndexLabel   string
	MemoryIndexUnit    string

	IncludeSceneIndex bool
	SceneIndexLabel   string
	SceneLimit        int
	MaxScenes         int
	MaxArtifacts      int

	RecallEntries RecallEntriesPromptOptions

	IncludeDerivedFacts bool
	DerivedFactLimit    int

	// PageIndexEnabled enables cross-page recall integration via the PageIndex.
	// When true, ProactiveContextForPrompt queries the PageIndex and includes
	// matching page-indexed items in the recall results. Zero value (false)
	// maintains backward compatibility.
	PageIndexEnabled bool

	// PageIndexMaxTokens is the dedicated sub-budget (in tokens) for page
	// context injected from the PageIndex. Default 0 means use the internal
	// default of 800 tokens.
	PageIndexMaxTokens int

	// PartialResultsEnabled enables staged recall with partial results on
	// timeout. When true, ProactiveContextForPrompt uses the StagedRecallPipeline
	// and returns the best results available within the deadline even if not all
	// stages completed. Zero value (false) maintains backward compatibility.
	PartialResultsEnabled bool
}

// ProactiveContextForPrompt builds the shared dynamic memory prompt context and
// returns both the rendered section and the recalled entries for host logging.
func (s *Store) ProactiveContextForPrompt(query string, opts ProactivePromptOptions) (string, []Entry) {
	if s == nil {
		return "", nil
	}
	var b strings.Builder
	if opts.IncludeMemoryIndex {
		if index := s.MemoryIndexForPrompt(opts.Recall.StrictProject, opts.Recall.ProjectPath, opts.MemoryIndexUnit); index != "" {
			label := opts.MemoryIndexLabel
			if label == "" {
				label = "[Memory Index] "
			}
			b.WriteByte('\n')
			b.WriteString(label)
			b.WriteString(index)
			b.WriteByte('\n')
		}
	}

	if opts.IncludeSceneIndex {
		sceneLimit := opts.SceneLimit
		if sceneLimit <= 0 {
			sceneLimit = 5
		}
		maxScenes := opts.MaxScenes
		if maxScenes <= 0 {
			maxScenes = 3
		}
		maxArtifacts := opts.MaxArtifacts
		if maxArtifacts <= 0 {
			maxArtifacts = 2
		}
		if sceneNav := s.SceneIndexForPrompt(opts.Recall.StrictProject, opts.Recall.ProjectPath, sceneLimit, maxScenes, maxArtifacts); sceneNav != "" {
			label := opts.SceneIndexLabel
			if label == "" {
				label = "[Scene Index]"
			}
			b.WriteByte('\n')
			b.WriteString(label)
			if !strings.HasSuffix(label, "\n") {
				b.WriteByte('\n')
			}
			b.WriteString(sceneNav)
			b.WriteByte('\n')
		}
	}

	var recalled []Entry
	if strings.TrimSpace(query) != "" {
		opts.Recall.EventContext = opts.EventContext
		recallStart := time.Now()

		// --- Adaptive Budget: compute topic density and expand budget if > 0.15 ---
		budgetResult := s.computeAdaptiveBudget(query, opts)
		if budgetResult.Expanded {
			opts.Recall.MaxEntries = budgetResult.MaxEntries
		}

		// --- Staged Recall: use StagedRecallPipeline when PartialResultsEnabled ---
		if opts.PartialResultsEnabled {
			recalled = s.proactiveRecallStaged(query, opts, budgetResult)
		} else {
			// Default path: policy-driven retrieval.
			decision := decideProactivePromptRetrieval(query, opts)
			s.recordRetrievalDecisionEvent(decision, opts.EventContext)
			candidates := s.RecallProactiveCandidatesWithDecision(decision, opts.Recall)
			recalled = s.entriesForExperienceCandidates(candidates)
			recallSection := FormatExperienceCandidatesForPrompt(candidates, opts.RecallEntries)
			b.WriteString(recallSection)
			if recallSection != "" {
				s.recordCandidateExperienceEvent(lifecycle.EventExperienceInjected, "proactive_prompt:"+string(decision.Mode), decision.Query, candidates, EstimateTextTokens(recallSection), opts.EventContext)
			}
		}

		// --- Page Index: query and integrate with dedicated sub-budget ---
		if opts.PageIndexEnabled {
			pageEntries := s.queryPageIndexForPrompt(query, opts)
			// Deduplicate page-indexed entries vs long-term memory entries.
			pageEntries = deduplicatePageEntries(pageEntries, recalled)
			if len(pageEntries) > 0 {
				recalled = append(recalled, pageEntries...)
			}
		}

		// For staged recall path, format the recalled entries now.
		if opts.PartialResultsEnabled && len(recalled) > 0 {
			candidates := s.entriesToExperienceCandidates(recalled)
			recallSection := FormatExperienceCandidatesForPrompt(candidates, opts.RecallEntries)
			b.WriteString(recallSection)
			if recallSection != "" {
				decision := decideProactivePromptRetrieval(query, opts)
				s.recordCandidateExperienceEvent(lifecycle.EventExperienceInjected, "proactive_prompt:staged:"+string(decision.Mode), decision.Query, candidates, EstimateTextTokens(recallSection), opts.EventContext)
			}
		}

		// Log proactive recall to the dedicated recall log file so the
		// "记忆召回记录" toggle in settings produces observable output.
		s.logProactiveRecallIfEnabled(query, opts.Recall, recalled, time.Since(recallStart))
	}

	if opts.IncludeDerivedFacts {
		limit := opts.DerivedFactLimit
		if limit <= 0 {
			limit = 5
		}
		b.WriteString(FormatDerivedFactsForPrompt(s.LastDerivedFacts(), limit))
	}
	return b.String(), recalled
}

// ---------------------------------------------------------------------------
// Adaptive Budget integration
// ---------------------------------------------------------------------------

// computeAdaptiveBudget uses the AdaptiveBudgetCalculator to determine if
// budget expansion is needed based on topic density.
func (s *Store) computeAdaptiveBudget(query string, opts ProactivePromptOptions) AdaptiveBudgetResult {
	s.mu.RLock()
	totalActive := s.activeCountLocked()
	s.mu.RUnlock()

	// Count matching entries using BM25 scores as a proxy for relevance.
	matching := s.countMatchingEntries(query, opts.Recall.OwnerID, opts.Recall.ProjectPath)

	calc := AdaptiveBudgetCalculator{}
	return calc.Calculate(matching, totalActive)
}

// countMatchingEntries counts entries with non-zero BM25 score for the query,
// respecting owner and project filters.
func (s *Store) countMatchingEntries(query string, ownerID, projectPath string) int {
	bm25Scores := s.multiQueryBM25(query, nil)
	projectLower := semanticNormalizeProjectPath(projectPath)

	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, e := range s.entries {
		if !e.IsActive() {
			continue
		}
		if !stagedRecallEntryAllowed(e, ownerID, projectLower) {
			continue
		}
		if bm25Scores[e.ID] > 0 {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// Staged Recall integration
// ---------------------------------------------------------------------------

// proactiveRecallStaged uses StagedRecallPipeline with a 2-second deadline.
// Falls back to default 12 entries if the pipeline cannot complete.
func (s *Store) proactiveRecallStaged(query string, opts ProactivePromptOptions, budget AdaptiveBudgetResult) []Entry {
	ctx := context.Background()
	deadline := time.Now().Add(2 * time.Second)

	maxEntries := opts.Recall.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}

	pipeline := StagedRecallPipeline{}
	result := pipeline.Recall(ctx, s, query, opts.Recall, deadline)

	// Annotate with partial recall marker if not all stages completed.
	if result.Partial {
		for i := range result.Entries {
			if !strings.HasPrefix(result.Entries[i].Content, "[partial recall - deep search skipped]") {
				result.Entries[i].Content = "[partial recall - deep search skipped] " + result.Entries[i].Content
			}
		}
	}

	// Fall back to default 12 entries if expansion cannot complete within budget.
	if budget.Expanded && result.Partial {
		// Expansion was requested but pipeline timed out; cap at default.
		if len(result.Entries) > defaultMaxEntries {
			result.Entries = result.Entries[:defaultMaxEntries]
		}
	}

	return result.Entries
}

// ---------------------------------------------------------------------------
// Page Index integration
// ---------------------------------------------------------------------------

// pageIndexDefaultMaxTokens is the default sub-budget for page context.
const pageIndexDefaultMaxTokens = 800

// queryPageIndexForPrompt queries the PageIndex and returns matching entries
// within the dedicated sub-budget.
func (s *Store) queryPageIndexForPrompt(query string, opts ProactivePromptOptions) []Entry {
	pi := s.PageIdx()
	if pi == nil {
		return nil
	}

	ownerID := opts.Recall.OwnerID
	if ownerID == "" {
		ownerID = "default"
	}

	expanded := ExpandQuery(query)
	candidates := pi.Query(ownerID, query, expanded.QueryTokens)
	if len(candidates) == 0 {
		return nil
	}

	maxTokens := opts.PageIndexMaxTokens
	if maxTokens <= 0 {
		maxTokens = pageIndexDefaultMaxTokens
	}

	// Convert page index candidates to entries within the token sub-budget.
	var entries []Entry
	tokenBudget := maxTokens
	for _, c := range candidates {
		tokens := EstimateTextTokens(c.Content)
		if tokens > tokenBudget {
			continue
		}
		tokenBudget -= tokens
		entries = append(entries, Entry{
			Content:  c.Content,
			Category: "page_index",
			Tags:     []string{c.Kind, c.PageID},
		})
	}
	return entries
}

// ---------------------------------------------------------------------------
// Deduplication: page-indexed entries vs long-term memory entries
// ---------------------------------------------------------------------------

// minDeduplicationLen is the minimum content length for substring containment check.
const minDeduplicationLen = 20

// deduplicatePageEntries removes page-indexed entries that are substring-contained
// within any long-term memory entry (or vice versa), using a minimum threshold
// of 20 characters to avoid spurious matches.
func deduplicatePageEntries(pageEntries, memoryEntries []Entry) []Entry {
	if len(pageEntries) == 0 || len(memoryEntries) == 0 {
		return pageEntries
	}

	var deduped []Entry
	for _, pe := range pageEntries {
		if isSubstringDuplicate(pe.Content, memoryEntries) {
			continue
		}
		deduped = append(deduped, pe)
	}
	return deduped
}

// isSubstringDuplicate checks if content is a substring of (or contains) any
// memory entry's content, requiring at least minDeduplicationLen characters of
// overlap.
func isSubstringDuplicate(content string, memoryEntries []Entry) bool {
	if len([]rune(content)) < minDeduplicationLen {
		return false
	}
	contentLower := strings.ToLower(content)
	for _, me := range memoryEntries {
		meLower := strings.ToLower(me.Content)
		if len([]rune(meLower)) < minDeduplicationLen {
			continue
		}
		// Check both directions: page content in memory, or memory in page content.
		if strings.Contains(meLower, contentLower) || strings.Contains(contentLower, meLower) {
			return true
		}
	}
	return false
}

// entriesToExperienceCandidates wraps recalled entries into lifecycle.Candidate
// for formatting with FormatExperienceCandidatesForPrompt.
func (s *Store) entriesToExperienceCandidates(entries []Entry) []lifecycle.Candidate {
	candidates := make([]lifecycle.Candidate, 0, len(entries))
	for _, e := range entries {
		candidates = append(candidates, lifecycle.Candidate{
			Entry: lifecycle.Entry{
				ID:        e.ID,
				Content:   e.Content,
				EntryType: lifecycle.EntryType(MapToCanonical(e.Category)),
				SourceURL: e.SourceURL,
			},
			Relevance: 1.0,
		})
	}
	return candidates
}

func decideProactivePromptRetrieval(query string, opts ProactivePromptOptions) lifecycle.RetrievalDecision {
	policy := opts.Policy
	if policy == nil {
		policy = lifecycle.DefaultRetrievalPolicy{}
	}
	return policy.Decide(context.Background(), lifecycle.RetrievalPolicyInput{
		TraceID:        opts.EventContext.TraceID,
		TaskID:         opts.EventContext.TaskID,
		CurrentGoal:    query,
		TokenBudget:    opts.TokenBudget,
		Boundary:       proactiveRecallBoundary(opts.Recall),
		MissingSignals: []string{"max_entries:" + strconv.Itoa(defaultPositive(opts.Recall.MaxEntries, 12))},
	})
}
