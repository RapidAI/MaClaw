package memory

import (
	"context"
	"log"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// StagedRecallPipeline — progressive recall with timeout resilience.
//
// Executes recall in three additive stages, returning the best available
// results within the deadline. Each stage's results are independently usable
// if subsequent stages timeout.
//
//   Stage 1 (BM25): guaranteed within 200ms
//   Stage 2 (+Vector): target within 500ms
//   Stage 3 (+Semantic Graph + Page Index + Alias expansion): target within 1500ms
//
// Requirements: 9.1, 9.2, 9.3, 9.4, 9.5
// ---------------------------------------------------------------------------

// StagedRecallPipeline executes recall stages progressively, returning the
// best results available within the deadline.
type StagedRecallPipeline struct{}

// stage timing budgets
const (
	stageBM25Budget  = 200 * time.Millisecond
	stageVecBudget   = 500 * time.Millisecond
	stageFullBudget  = 1500 * time.Millisecond
)

// stage labels for StagedRecallResult.StageReached
const (
	StageBM25Only = "bm25_only"
	StageBM25Vec  = "bm25_vec"
	StageFull     = "full"
)

// Recall executes staged retrieval within the given deadline.
// Stage 1 (BM25): guaranteed within 200ms
// Stage 2 (+Vector): target within 500ms
// Stage 3 (+Semantic Graph + Page Index + Alias expansion): target within 1500ms
func (p *StagedRecallPipeline) Recall(ctx context.Context, store *Store, query string, opts ProactiveRecallOptions, deadline time.Time) StagedRecallResult {
	start := time.Now()

	if store == nil || query == "" {
		return StagedRecallResult{
			StageReached: StageBM25Only,
			Elapsed:      time.Since(start),
			Partial:      true,
		}
	}

	// === Query understanding (shared across all stages) ===
	expanded := ExpandQuery(query)

	// Determine filtering parameters.
	ownerID := opts.OwnerID
	projectPath := opts.ProjectPath
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}

	// === Stage 1: BM25 (guaranteed within 200ms) ===
	bm25Scores := store.multiQueryBM25(query, expanded.Entities)
	bm25Entries := p.rankBM25(store, bm25Scores, ownerID, projectPath, expanded.QueryTokens, maxEntries)

	if deadlineExceeded(ctx, deadline, stageVecBudget) {
		elapsed := time.Since(start)
		log.Printf("[perf] staged_recall stage=%s elapsed=%v", StageBM25Only, elapsed)
		return StagedRecallResult{
			Entries:      bm25Entries,
			StageReached: StageBM25Only,
			Elapsed:      elapsed,
			Partial:      true,
		}
	}

	// === Stage 2: +Vector (target within 500ms) ===
	vecScores := store.vecIndex.score(store.queryEmbeddingCached(query))
	stage2Entries := p.rankBM25Vec(store, bm25Scores, vecScores, ownerID, projectPath, expanded.QueryTokens, maxEntries)

	if deadlineExceeded(ctx, deadline, stageFullBudget) {
		elapsed := time.Since(start)
		log.Printf("[perf] staged_recall stage=%s elapsed=%v", StageBM25Vec, elapsed)
		return StagedRecallResult{
			Entries:      stage2Entries,
			StageReached: StageBM25Vec,
			Elapsed:      elapsed,
			Partial:      true,
		}
	}

	// === Stage 3: +Semantic Graph + Page Index + Alias expansion (target within 1500ms) ===
	stage3Entries := p.rankFull(store, bm25Scores, vecScores, expanded, ownerID, projectPath, maxEntries, query)

	elapsed := time.Since(start)
	log.Printf("[perf] staged_recall stage=%s elapsed=%v", StageFull, elapsed)
	return StagedRecallResult{
		Entries:      stage3Entries,
		StageReached: StageFull,
		Elapsed:      elapsed,
		Partial:      false,
	}
}

// deadlineExceeded checks whether the remaining time until the deadline is
// insufficient for the next stage budget, or if the context is cancelled.
func deadlineExceeded(ctx context.Context, deadline time.Time, nextStageBudget time.Duration) bool {
	select {
	case <-ctx.Done():
		return true
	default:
	}
	remaining := time.Until(deadline)
	return remaining < nextStageBudget
}

// rankBM25 produces a ranked list of entries using only BM25 scores.
func (p *StagedRecallPipeline) rankBM25(store *Store, bm25Scores map[string]float64, ownerID, projectPath string, queryTokens []string, maxEntries int) []Entry {
	store.mu.RLock()
	defer store.mu.RUnlock()

	projectLower := semanticNormalizeProjectPath(projectPath)
	now := time.Now()

	var candidates []recallScored
	for _, e := range store.entries {
		if !e.IsActive() {
			continue
		}
		if !stagedRecallEntryAllowed(e, ownerID, projectLower) {
			continue
		}
		score := bm25Scores[e.ID]
		if score <= 0 {
			continue
		}
		// Use a simplified memoryStreamScore with BM25 as the sole relevance signal.
		ms := memoryStreamScore(e, score, score, projectLower, now)
		candidates = append(candidates, recallScored{entry: e, score: ms})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Temporal demotion: stale/invalidated entries rank lower.
	applyTemporalDemotion(candidates, now)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	return topNEntries(candidates, maxEntries)
}

// rankBM25Vec produces a ranked list fusing BM25 + Vector scores via RRF.
func (p *StagedRecallPipeline) rankBM25Vec(store *Store, bm25Scores, vecScores map[string]float64, ownerID, projectPath string, queryTokens []string, maxEntries int) []Entry {
	store.mu.RLock()
	defer store.mu.RUnlock()

	projectLower := semanticNormalizeProjectPath(projectPath)
	now := time.Now()

	type candidate struct {
		entry Entry
		bm25  float64
		vec   float64
	}
	var candidates []candidate
	for _, e := range store.entries {
		if !e.IsActive() {
			continue
		}
		if !stagedRecallEntryAllowed(e, ownerID, projectLower) {
			continue
		}
		b := bm25Scores[e.ID]
		v := vecScores[e.ID]
		if b <= 0 && v <= 0 {
			continue
		}
		candidates = append(candidates, candidate{entry: e, bm25: b, vec: v})
	}

	if len(candidates) == 0 {
		return nil
	}

	// RRF fusion
	bm25Arr := make([]float64, len(candidates))
	vecArr := make([]float64, len(candidates))
	entryArr := make([]Entry, len(candidates))
	for i, c := range candidates {
		bm25Arr[i] = c.bm25
		vecArr[i] = c.vec
		entryArr[i] = c.entry
	}
	rrfScores := rrfFuseScores(bm25Arr, vecArr, entryArr, projectLower, queryTokens)

	var scored []recallScored
	for i, c := range candidates {
		ms := memoryStreamScore(c.entry, rrfScores[i], c.bm25, projectLower, now)
		scored = append(scored, recallScored{entry: c.entry, score: ms})
	}

	// Temporal demotion: stale/invalidated entries rank lower.
	applyTemporalDemotion(scored, now)

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	return topNEntries(scored, maxEntries)
}

// rankFull produces a ranked list using all available signals:
// BM25 + Vector + Semantic Graph + Alias expansion.
func (p *StagedRecallPipeline) rankFull(store *Store, bm25Scores, vecScores map[string]float64, expanded ExpandResult, ownerID, projectPath string, maxEntries int, query string) []Entry {
	store.mu.RLock()
	defer store.mu.RUnlock()

	projectLower := semanticNormalizeProjectPath(projectPath)
	now := time.Now()

	// Semantic graph expansion scores.
	semanticScores := map[string]float64{}
	if store.semanticGraph != nil {
		temporalMode, asOf := semanticTemporalOptionsFromQuery(query)
		for _, hit := range store.semanticGraph.SearchWithOptions(expanded.Entities, SemanticSearchOptions{
			Now:          now,
			AsOf:         asOf,
			TemporalMode: temporalMode,
			MaxHits:      30,
			OwnerID:      ownerID,
			ProjectPath:  projectLower,
		}) {
			semanticScores[hit.EntryID] = hit.Score
		}
	}

	type candidate struct {
		entry    Entry
		bm25     float64
		vec      float64
		semantic float64
	}
	var candidates []candidate
	for _, e := range store.entries {
		if !e.IsActive() {
			continue
		}
		if !stagedRecallEntryAllowed(e, ownerID, projectLower) {
			continue
		}
		b := bm25Scores[e.ID]
		v := vecScores[e.ID]
		sem := semanticScores[e.ID]
		if b <= 0 && v <= 0 && sem <= 0 {
			continue
		}
		candidates = append(candidates, candidate{entry: e, bm25: b, vec: v, semantic: sem})
	}

	if len(candidates) == 0 {
		return nil
	}

	// RRF fusion (BM25 + Vec)
	bm25Arr := make([]float64, len(candidates))
	vecArr := make([]float64, len(candidates))
	entryArr := make([]Entry, len(candidates))
	for i, c := range candidates {
		bm25Arr[i] = c.bm25
		vecArr[i] = c.vec
		entryArr[i] = c.entry
	}
	rrfScores := rrfFuseScores(bm25Arr, vecArr, entryArr, projectLower, expanded.QueryTokens)

	var scored []recallScored
	for i, c := range candidates {
		fusedRelevance := rrfScores[i]
		// Add semantic graph signal as an additive boost.
		if c.semantic > 0 {
			fusedRelevance += c.semantic * 0.3
		}
		ms := memoryStreamScore(c.entry, fusedRelevance, c.bm25, projectLower, now)
		ms += c.entry.Stability.StabilityBoost()
		scored = append(scored, recallScored{entry: c.entry, score: ms})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Graph expansion for top candidates.
	scored = store.graphExpand(scored, graphExpandSeeds)
	scored = filterRecallProjectOthers(scored, projectLower)

	// Temporal demotion: stale/invalidated entries rank lower.
	applyTemporalDemotion(scored, now)
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	return topNEntries(scored, maxEntries)
}

// stagedRecallEntryAllowed checks whether an entry passes OwnerID isolation
// and project scope filters for staged recall.
func stagedRecallEntryAllowed(e Entry, ownerID, projectLower string) bool {
	// OwnerID isolation: skip entries owned by a different user.
	if ownerID != "" && e.OwnerID != "" && e.OwnerID != ownerID {
		return false
	}
	// Skip categories not relevant to proactive recall.
	canonical := MapToCanonical(e.Category)
	if canonical == CategoryUserFact || canonical == CategorySelfIdentity ||
		canonical == CategorySessionCheckpoint || canonical == CategoryConversationSummary {
		return false
	}
	return true
}

// topNEntries extracts at most maxEntries entries from a scored candidate list,
// respecting the per-page token budget.
func topNEntries(scored []recallScored, maxEntries int) []Entry {
	if len(scored) == 0 {
		return nil
	}
	var result []Entry
	tokenBudget := perPageTokenBudget
	for _, s := range scored {
		if len(result) >= maxEntries {
			break
		}
		tokens := EstimateTextTokens(s.entry.Content)
		if tokens > tokenBudget {
			continue
		}
		tokenBudget -= tokens
		result = append(result, s.entry)
	}
	return result
}
