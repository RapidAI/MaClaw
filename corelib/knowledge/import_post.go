package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// importTopicLinkLimit is the per-source related-link budget used after import.
// Lower than interactive refresh to keep bulk imports responsive.
const importTopicLinkLimit = 6

// skipIntraBatchLinkMin is the batch size at which we stop linking newly
// imported sources to each other (they are usually near-duplicates by title).
// Links to pre-existing sources still form.
const skipIntraBatchLinkMin = 12

// importPostProgress reports background post-import phases to the UI.
// phase is "linking" or "embedding"; progress is 0-100 within that phase.
type importPostProgress func(phase string, progress int)

// scheduleImportPostWork runs topic linking + node embedding after the import
// transaction commits. Prefer a background connection so ImportDirectory can
// return (UI shows "indexing") without waiting for the model.
//
// onPhase may be nil. onDone is called exactly once when post-work finishes
// (success, soft failure, cancel, or panic recovery); may be nil.
func (s *SQLiteStore) scheduleImportPostWork(sourceIDs []string, onPhase importPostProgress, onDone func()) {
	if s == nil || len(sourceIDs) == 0 {
		if onDone != nil {
			onDone()
		}
		return
	}
	ids := uniqueNonEmptyIDs(sourceIDs)
	if len(ids) == 0 {
		if onDone != nil {
			onDone()
		}
		return
	}
	exclude := make(map[string]struct{}, len(ids))
	if len(ids) >= skipIntraBatchLinkMin {
		for _, id := range ids {
			exclude[id] = struct{}{}
		}
	}
	// Ensure embedding column exists on the primary connection before async work
	// so concurrent readers never see "no such column".
	_ = s.EnsureNodeEmbeddingColumn()

	var doneOnce sync.Once
	finish := func() {
		if onDone == nil {
			return
		}
		doneOnce.Do(onDone)
	}

	// Throttle phase progress callbacks (linking 200 files would otherwise spam the UI).
	phaseProgress := throttleImportPostProgress(onPhase, 200*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancelGen := s.setBackgroundCancel(cancel)

	run := func(store *SQLiteStore) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[knowledge-import] post-work panic: %v", rec)
			}
			s.clearBackgroundCancel(cancelGen)
			finish()
		}()
		phaseProgress("linking", 0)
		if ctx.Err() != nil {
			return
		}
		store.linkImportedSources(ctx, ids, exclude, func(done, total int) {
			if total <= 0 {
				return
			}
			phaseProgress("linking", (done*100)/total)
		})
		if ctx.Err() != nil {
			return
		}
		phaseProgress("embedding", 0)
		// Spreadsheet rows are independently searchable evidence. Index just the
		// new sources here, after commit, so imports become semantically available
		// without a later full-table backfill.
		if err := store.BackfillTableRowEmbeddingsForSources(ctx, ids); err != nil && ctx.Err() == nil {
			log.Printf("[knowledge-import] table-row embedding failed: %v", err)
		}
		if ctx.Err() != nil {
			return
		}
		if err := store.BackfillNodeEmbeddingsForSourcesWithProgress(ctx, ids, func(processed, total int) {
			if total <= 0 {
				return
			}
			phaseProgress("embedding", (processed*100)/total)
		}); err != nil && ctx.Err() == nil {
			log.Printf("[knowledge-import] node embedding failed: %v", err)
		}
		if ctx.Err() == nil {
			phaseProgress("embedding", 100)
		}
	}

	// Background path: lightweight secondary connection (WAL; skip ensureSchema).
	if strings.TrimSpace(s.dbPath) != "" {
		path := s.dbPath
		emb := s.currentEmbedder()
		if !s.startBackground(func() {
			bg, err := openSecondarySQLiteStore(path)
			if err != nil {
				log.Printf("[knowledge-import] background store open failed: %v", err)
				s.clearBackgroundCancel(cancelGen)
				finish()
				return
			}
			if emb != nil && !embedding.IsNoop(emb) {
				// Do not call SetEmbedder on this short-lived store: that starts a
				// full-database asynchronous refresh which can race its connection
				// closing. The post-work path below explicitly backfills only the
				// just-imported sources.
				bg.embedderMu.Lock()
				bg.embedder = emb
				bg.embedderGeneration++
				bg.embedderMu.Unlock()
			}
			run(bg)
			if bg.db != nil {
				_ = bg.db.Close()
				bg.db = nil
			}
		}) {
			// Close won the race with scheduling. Do not leave import state waiting
			// for a callback that can no longer run.
			s.clearBackgroundCancel(cancelGen)
			finish()
		}
		return
	}

	// Fallback: run inline (no db path).
	run(s)
}

// openSecondarySQLiteStore opens an existing knowledge DB for background
// post-work without re-running ensureSchema (primary already migrated).
func openSecondarySQLiteStore(dbPath string) (*SQLiteStore, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("knowledge sqlite: db path is required")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge sqlite open secondary: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := applyPragmas(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Cheap liveness check; fail fast if the primary deleted the file.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("knowledge sqlite ping secondary: %w", err)
	}
	return &SQLiteStore{db: db, dbPath: dbPath}, nil
}

// throttleImportPostProgress coalesces frequent phase updates so bulk linking
// does not flood the UI event bus. Always emits 0% and 100% for a phase.
func throttleImportPostProgress(onPhase importPostProgress, minInterval time.Duration) importPostProgress {
	if onPhase == nil {
		return func(string, int) {}
	}
	if minInterval <= 0 {
		minInterval = 200 * time.Millisecond
	}
	var mu sync.Mutex
	var lastPhase string
	var lastAt time.Time
	var lastProgress int
	return func(phase string, progress int) {
		if progress < 0 {
			progress = 0
		}
		if progress > 100 {
			progress = 100
		}
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		force := phase != lastPhase || progress == 0 || progress >= 100 || progress-lastProgress >= 10
		if !force && !lastAt.IsZero() && now.Sub(lastAt) < minInterval {
			return
		}
		lastPhase = phase
		lastAt = now
		lastProgress = progress
		onPhase(phase, progress)
	}
}

func uniqueNonEmptyIDs(sourceIDs []string) []string {
	seen := make(map[string]struct{}, len(sourceIDs))
	ids := make([]string, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// linkImportedSources builds topic-related source links after an import batch.
//
// Unlike RefreshSourceTopicLinks (which runs TopicRelevance: O(sources×terms×tables)
// LIKE scans), this uses FTS Search so large markdown/PDF imports do not spend
// longer linking than ingesting.
//
// excludeIDs: when non-nil, skip linking to those sources (used to avoid
// dense intra-batch links among hundreds of similar *_知识库.md files).
func (s *SQLiteStore) linkImportedSources(ctx context.Context, sourceIDs []string, excludeIDs map[string]struct{}, onProgress func(done, total int)) {
	if s == nil || len(sourceIDs) == 0 {
		return
	}
	ids := uniqueNonEmptyIDs(sourceIDs)
	if len(ids) == 0 {
		return
	}

	// Cap work on very large batches: still link every source, but with a smaller
	// neighbor budget so wall time stays linear-ish with FTS.
	limit := importTopicLinkLimit
	if len(ids) > 100 {
		limit = 4
	}
	if len(ids) > 300 {
		limit = 3
	}

	total := len(ids)
	for i, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if onProgress != nil {
			onProgress(i, total)
		}
		_, _ = s.refreshSourceTopicLinksFast(ctx, id, limit, excludeIDs)
	}
	if onProgress != nil {
		onProgress(total, total)
	}
}

// refreshSourceTopicLinksFast links a source to related sources via FTS Search.
// Public RefreshSourceTopicLinks keeps the richer TopicRelevance scorer for
// explicit maintenance/UI actions.
func (s *SQLiteStore) refreshSourceTopicLinksFast(ctx context.Context, sourceID string, limit int, excludeIDs map[string]struct{}) (SourceTopicLinkBuildResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return SourceTopicLinkBuildResult{}, fmt.Errorf("source id is required")
	}
	if limit <= 0 {
		limit = importTopicLinkLimit
	}
	if limit > 50 {
		limit = 50
	}
	if ctx.Err() != nil {
		return SourceTopicLinkBuildResult{}, ctx.Err()
	}
	source, err := s.GetSource(ctx, sourceID)
	if err != nil {
		return SourceTopicLinkBuildResult{}, err
	}

	query := strings.TrimSpace(source.Title)
	if th := strings.TrimSpace(source.TopicHint); th != "" {
		if query == "" {
			query = th
		} else {
			query = th + " " + query
		}
	}
	// Fall back to relative path basename tokens when title is empty/generic.
	if query == "" {
		query = strings.TrimSpace(source.RelativePath)
	}
	result := SourceTopicLinkBuildResult{
		SourceID: sourceID,
		Notes:    []string{"import_fast_topic_links", "fts_search"},
	}
	if len(excludeIDs) > 0 {
		result.Notes = append(result.Notes, "skip_intra_batch")
	}
	if strings.TrimSpace(query) == "" {
		result.Notes = append(result.Notes, "empty_link_query")
		return result, nil
	}

	// Oversample then dedupe by source id (Search returns cards/facts/nodes).
	// Request more hits when excluding a large batch so external neighbors remain.
	searchLimit := limit * 4
	if len(excludeIDs) > 0 {
		searchLimit = limit * 8
		if searchLimit < 24 {
			searchLimit = 24
		}
		if searchLimit > 80 {
			searchLimit = 80
		}
	}
	hits, err := s.Search(ctx, SearchOptions{
		Query:           query,
		OwnerID:         source.OwnerID,
		TenantID:        source.TenantID,
		ProjectPath:     source.ProjectPath,
		IncludeDisabled: false,
		Limit:           searchLimit,
		// Prefer FTS-only path for speed; embeddings may not be ready yet mid-import.
		PreferEmbedding: false,
	})
	if err != nil {
		return result, err
	}

	type cand struct {
		src   Source
		score float64
		terms []string
	}
	best := make(map[string]cand)
	order := make([]string, 0, limit)
	for _, hit := range hits {
		relatedID := strings.TrimSpace(hit.Source.ID)
		if relatedID == "" || relatedID == sourceID {
			result.Skipped++
			continue
		}
		// Skip other members of the same import batch (large bulk imports).
		if excludeIDs != nil {
			if _, skip := excludeIDs[relatedID]; skip {
				result.Skipped++
				continue
			}
		}
		if prev, ok := best[relatedID]; ok {
			if hit.Score > prev.score {
				prev.score = hit.Score
				best[relatedID] = prev
			}
			continue
		}
		terms := make([]string, 0, 2)
		if hit.CardTitle != "" {
			terms = append(terms, hit.CardTitle)
		} else if hit.Claim != "" {
			terms = append(terms, truncateRunes(hit.Claim, 40))
		} else if hit.Snippet != "" {
			terms = append(terms, truncateRunes(hit.Snippet, 40))
		}
		best[relatedID] = cand{src: hit.Source, score: hit.Score, terms: terms}
		order = append(order, relatedID)
	}
	result.Scanned = len(best)
	result.Candidates = len(best)

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_source_links WHERE relation = ? AND (source_id = ? OR related_source_id = ?)`,
		SourceRelationTopicRelated, sourceID, sourceID); err != nil {
		return result, err
	}

	linked := 0
	for _, relatedID := range order {
		if linked >= limit {
			break
		}
		c := best[relatedID]
		link := SourceLink{
			SourceID:        sourceID,
			RelatedSourceID: relatedID,
			Relation:        SourceRelationTopicRelated,
			Score:           c.score,
			Terms:           c.terms,
			Evidence:        []string{"import_fts"},
			RelatedSource:   c.src,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := insertSourceLinkTx(ctx, tx, link); err != nil {
			return result, err
		}
		reverse := link
		reverse.SourceID = relatedID
		reverse.RelatedSourceID = sourceID
		reverse.RelatedSource = source
		if err := insertSourceLinkTx(ctx, tx, reverse); err != nil {
			return result, err
		}
		result.Links = append(result.Links, link)
		linked++
	}
	result.Linked = linked
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
