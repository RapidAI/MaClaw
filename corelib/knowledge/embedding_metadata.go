package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

const (
	embeddingEntityCard = "card"
	embeddingEntityNode = "node"
	embeddingEntityRow  = "table_row"
)

// embeddingModelIdentifier uses an explicit ID when an embedder exposes one.
// The fallback is deliberately conservative: vectors from different concrete
// implementations or dimensions never share a retrieval space.
func embeddingModelIdentifier(emb embedding.Embedder) string {
	if emb == nil || embedding.IsNoop(emb) {
		return ""
	}
	if identified, ok := emb.(interface{ ModelID() string }); ok {
		if id := strings.TrimSpace(identified.ModelID()); id != "" {
			return id
		}
	}
	return fmt.Sprintf("%T:%d", emb, emb.Dim())
}

func ensureEmbeddingMetadataSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS knowledge_embedding_metadata (
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		model_id TEXT NOT NULL,
		dimension INTEGER NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(entity_type, entity_id)
	)`)
	return err
}

func upsertEmbeddingMetadataTx(ctx context.Context, tx *sql.Tx, entityType, entityID, modelID string, dimension int) error {
	if strings.TrimSpace(entityID) == "" || strings.TrimSpace(modelID) == "" || dimension <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO knowledge_embedding_metadata(entity_type, entity_id, model_id, dimension, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET model_id = excluded.model_id, dimension = excluded.dimension, updated_at = excluded.updated_at`,
		entityType, entityID, modelID, dimension, formatTime(time.Now().UTC()))
	return err
}

func (s *SQLiteStore) upsertEmbeddingMetadata(ctx context.Context, entityType, entityID, modelID string, dimension int) error {
	if s == nil || s.db == nil || strings.TrimSpace(entityID) == "" || strings.TrimSpace(modelID) == "" || dimension <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO knowledge_embedding_metadata(entity_type, entity_id, model_id, dimension, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET model_id = excluded.model_id, dimension = excluded.dimension, updated_at = excluded.updated_at`,
		entityType, entityID, modelID, dimension, formatTime(time.Now().UTC()))
	return err
}

// persistEmbeddingIfCurrent atomically persists a conditionally-matched vector
// and its model metadata. The caller's UPDATE must include a content/version
// predicate. Doing both writes in one transaction prevents a concurrent edit
// from landing between them and leaving metadata attached to different content.
func (s *SQLiteStore) persistEmbeddingIfCurrent(ctx context.Context, entityType, entityID, modelID string, vector []float32, generation uint64, updateSQL string, updateArgs ...interface{}) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("knowledge store is nil")
	}
	if strings.TrimSpace(entityID) == "" || strings.TrimSpace(modelID) == "" || !validEmbeddingVector(vector, 0) || strings.TrimSpace(updateSQL) == "" {
		return false, nil
	}
	// Hold the model read lock until commit. SetEmbedder takes the write lock, so
	// it cannot advance generation after this validation and before the vector +
	// metadata transaction becomes durable.
	s.embedderMu.RLock()
	defer s.embedderMu.RUnlock()
	if generation == 0 || s.embedderGeneration != generation {
		return false, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, updateSQL, updateArgs...)
	if err != nil {
		return false, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if updated == 0 {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := upsertEmbeddingMetadataTx(ctx, tx, entityType, entityID, modelID, len(vector)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// recordEmbeddingMetadataWithEmbedder is used by low-level write paths which
// receive a vector but not an explicit model identifier (for example direct
// SaveCard). Refuse to invent an identity for externally supplied vectors:
// an unlabelled vector must be backfilled by the configured model before it can
// enter semantic retrieval.
