package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RebuildSourceDerived rewrites one source's claims and body indexes.
// Model calls and text segmentation happen before the write transaction.
// Full-text and vectors for the body are rebuilt from stored node text and
// table-row text. Claim vectors are not written on this path.
func (s *SQLiteStore) RebuildSourceDerived(ctx context.Context, id string, distillMode string) (Source, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Source{}, fmt.Errorf("source id is required")
	}
	source, err := s.GetSource(ctx, id)
	if err != nil {
		return Source{}, err
	}
	nodes, err := s.ListNodesBySource(ctx, id, 10000)
	if err != nil {
		return Source{}, err
	}
	hasRows, err := s.sourceHasTableRows(ctx, id)
	if err != nil {
		return Source{}, err
	}
	if len(nodes) == 0 && !hasRows {
		return Source{}, fmt.Errorf("source %s has no parsed document nodes or table rows to rebuild from", id)
	}
	var prepared *distilledCardWrite
	if len(nodes) > 0 {
		prepared, err = s.prepareDistilledCardsEmbed(ctx, source, nodes, distillMode, false)
		if err != nil {
			return Source{}, err
		}
	}
	wasDisabled := source.Status == StatusDisabled
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Source{}, err
	}
	defer tx.Rollback()
	if len(nodes) > 0 {
		if err := deleteSourceCardsAndFacts(ctx, tx, id); err != nil {
			return Source{}, err
		}
	}
	source.Status = StatusParsed
	source.ErrorMessage = ""
	source.UpdatedAt = time.Now().UTC()
	// Rebuilding indexes is not a new save. An empty owner stays empty, including
	// on a desktop store that stamps owners when a source is first written.
	if err := insertSource(ctx, tx, source); err != nil {
		return Source{}, err
	}
	if prepared != nil {
		source, err = s.writeDistilledCards(ctx, tx, source, prepared)
		if err != nil {
			return Source{}, err
		}
	}
	if wasDisabled {
		source.Status = StatusDisabled
		source.UpdatedAt = time.Now().UTC()
		if err := insertSource(ctx, tx, source); err != nil {
			return Source{}, err
		}
	}
	if err := insertSourceVersionTx(ctx, tx, source, "rebuild_derived"); err != nil {
		return Source{}, err
	}
	if err := tx.Commit(); err != nil {
		return Source{}, err
	}
	if err := s.rebuildSourceBodyFTS(ctx, id); err != nil {
		return Source{}, err
	}
	if err := s.BackfillNodeEmbeddingsForSources(ctx, []string{id}); err != nil {
		return Source{}, err
	}
	if err := s.BackfillTableRowEmbeddingsForSources(ctx, []string{id}); err != nil {
		return Source{}, err
	}
	sources := []Source{source}
	if err := s.hydrateSourceCounts(ctx, sources); err != nil {
		return Source{}, err
	}
	return sources[0], nil
}

func (s *SQLiteStore) sourceHasTableRows(ctx context.Context, sourceID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("knowledge store is nil")
	}
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM kb_rows WHERE source_id = ? LIMIT 1`, sourceID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) RebuildSourcesDerived(ctx context.Context, ids []string, distillMode string) SourceRebuildResult {
	result := SourceRebuildResult{}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result.Requested++
		source, err := s.RebuildSourceDerived(ctx, id, distillMode)
		if err != nil {
			result.Failed++
			appendSourceRebuildFailure(&result, id, err)
			continue
		}
		result.Rebuilt++
		result.Sources = append(result.Sources, source)
	}
	return result
}

func appendSourceRebuildFailure(result *SourceRebuildResult, sourceID string, err error) {
	if result == nil || err == nil {
		return
	}
	result.Failures = append(result.Failures, SourceRebuildFailure{SourceID: sourceID, Error: err.Error()})
	if IsSQLiteLockedError(err) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s: transient sqlite lock during rebuild; retry later", sourceID))
	}
}

func (s *SQLiteStore) RebuildSourcesDerivedByFilter(ctx context.Context, opts ListSourcesOptions, distillMode string) (SourceRebuildResult, error) {
	opts.Limit = sourceFilterLimit(opts, 100, 500, 5000)
	if opts.Status == "" {
		opts.IncludeDisabled = true
	}
	sources, err := s.ListSources(ctx, opts)
	if err != nil {
		return SourceRebuildResult{}, err
	}
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ID)
	}
	return s.RebuildSourcesDerived(ctx, ids, distillMode), nil
}
