package knowledge

import "context"

// finalizeCommittedSource enriches the returned view after the write transaction
// has committed. These reads are deliberately best-effort: returning an error at
// this point would misreport durable data as failed and invite duplicate retries.
func (s *SQLiteStore) finalizeCommittedSource(ctx context.Context, source Source, duplicate bool) Source {
	sources := []Source{source}
	_ = s.hydrateSourceCounts(ctx, sources)
	_ = s.hydrateSourceLabels(ctx, sources)
	if duplicate {
		sources[0].SaveStatus = SaveStatusDuplicate
	} else {
		sources[0].SaveStatus = SaveStatusCreated
	}
	return sources[0]
}
