package memory

import "context"

// UpsertProjectKnowledgeForHost stores generated project knowledge through the
// shared corelib write path. Host packages provide source metadata, but identity
// tags, repair, and dedup semantics stay centralized here.
func (s *Store) UpsertProjectKnowledgeForHost(opts ProjectKnowledgeUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	return s.UpsertProjectKnowledge(opts)
}

// UpsertTaskArtifactForHost stores host-visible task artifacts through the
// shared corelib upsert path.
func (s *Store) UpsertTaskArtifactForHost(opts TaskArtifactUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	return s.UpsertTaskArtifact(opts)
}

// UpsertConversationSummaryForHost stores conversation summaries through the
// shared corelib upsert path.
func (s *Store) UpsertConversationSummaryForHost(opts ConversationSummaryUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	return s.UpsertConversationSummary(opts)
}

// UpsertGeneratedInsightForHost stores generated insight memory through the
// shared corelib upsert path.
func (s *Store) UpsertGeneratedInsightForHost(opts GeneratedInsightUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	return s.UpsertGeneratedInsight(opts)
}

// UpsertSessionCheckpointForHost stores generated session checkpoints through
// the shared corelib upsert path.
func (s *Store) UpsertSessionCheckpointForHost(opts SessionCheckpointUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	return s.UpsertSessionCheckpoint(opts)
}

// StorePathForHost exposes the store path only for host-owned memory reference
// files that deliberately avoid stuffing large blobs into memory content.
func (s *Store) StorePathForHost() string {
	if s == nil {
		return ""
	}
	return s.Path()
}

// PendingDedupCountForHost returns the count of deferred dedup work visible to
// host persistence loops.
func (s *Store) PendingDedupCountForHost() int {
	if s == nil {
		return 0
	}
	return s.PendingDedupCount()
}

// ProcessPendingDedupForHost runs deferred dedup work from host persistence
// loops without exposing the lower-level queue mechanics.
func (s *Store) ProcessPendingDedupForHost(ctx context.Context) int {
	if s == nil {
		return 0
	}
	return s.ProcessPendingDedup(ctx)
}
