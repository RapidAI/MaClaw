package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func (r *llmPromptCacheRepo) Get(ctx context.Context, cacheKey string) (*store.LLMPromptCacheEntry, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT cache_key, provider_id, model, kind, input_hash, payload, payload_bytes, cached_input_tokens, cache_write_tokens, hit_count, created_at, accessed_at, expires_at FROM llm_prompt_cache WHERE cache_key = ?`, cacheKey)

	var (
		entry                          store.LLMPromptCacheEntry
		payload                        []byte
		createdAt, accessedAt, expires sql.NullString
	)
	if err := row.Scan(
		&entry.CacheKey,
		&entry.ProviderID,
		&entry.Model,
		&entry.Kind,
		&entry.InputHash,
		&payload,
		&entry.PayloadBytes,
		&entry.CachedInputTokens,
		&entry.CacheWriteTokens,
		&entry.HitCount,
		&createdAt,
		&accessedAt,
		&expires,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	entry.Payload = append([]byte(nil), payload...)
	if createdAt.Valid {
		entry.CreatedAt = mustParseTime(createdAt.String)
	}
	if accessedAt.Valid {
		entry.AccessedAt = mustParseTime(accessedAt.String)
	}
	if expires.Valid {
		t := mustParseTime(expires.String)
		entry.ExpiresAt = &t
		if !t.IsZero() && !t.After(time.Now().UTC()) {
			_ = r.Delete(ctx, cacheKey)
			return nil, nil
		}
	}

	now := time.Now().UTC()
	entry.AccessedAt = now
	entry.HitCount++
	if err := execWrite(ctx, r.batch, r.db, `UPDATE llm_prompt_cache SET accessed_at = ?, hit_count = hit_count + 1 WHERE cache_key = ?`, now.Format(time.RFC3339), cacheKey); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *llmPromptCacheRepo) Put(ctx context.Context, entry *store.LLMPromptCacheEntry) error {
	if entry == nil {
		return nil
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	if entry.AccessedAt.IsZero() {
		entry.AccessedAt = entry.CreatedAt
	}
	if entry.Kind == "" {
		entry.Kind = "metadata"
	}
	if entry.PayloadBytes <= 0 {
		entry.PayloadBytes = int64(len(entry.Payload))
	}
	var expiresAt any
	if entry.ExpiresAt != nil && !entry.ExpiresAt.IsZero() {
		expiresAt = entry.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return execWrite(ctx, r.batch, r.db, `INSERT INTO llm_prompt_cache (cache_key, provider_id, model, kind, input_hash, payload, payload_bytes, cached_input_tokens, cache_write_tokens, hit_count, created_at, accessed_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(cache_key) DO UPDATE SET
			provider_id = excluded.provider_id,
			model = excluded.model,
			kind = excluded.kind,
			input_hash = excluded.input_hash,
			payload = excluded.payload,
			payload_bytes = excluded.payload_bytes,
			cached_input_tokens = excluded.cached_input_tokens,
			cache_write_tokens = excluded.cache_write_tokens,
			accessed_at = excluded.accessed_at,
			expires_at = excluded.expires_at`,
		entry.CacheKey,
		entry.ProviderID,
		entry.Model,
		entry.Kind,
		entry.InputHash,
		entry.Payload,
		entry.PayloadBytes,
		entry.CachedInputTokens,
		entry.CacheWriteTokens,
		entry.HitCount,
		entry.CreatedAt.UTC().Format(time.RFC3339),
		entry.AccessedAt.UTC().Format(time.RFC3339),
		expiresAt,
	)
}

func (r *llmPromptCacheRepo) Delete(ctx context.Context, cacheKey string) error {
	return execWrite(ctx, r.batch, r.db, `DELETE FROM llm_prompt_cache WHERE cache_key = ?`, cacheKey)
}

func (r *llmPromptCacheRepo) Purge(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM llm_prompt_cache`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *llmPromptCacheRepo) PurgeByKeyPrefix(ctx context.Context, prefix string) (int64, error) {
	if prefix == "" {
		return 0, nil
	}
	res, err := r.db.ExecContext(ctx, `DELETE FROM llm_prompt_cache WHERE substr(cache_key, 1, ?) = ?`, len(prefix), prefix)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *llmPromptCacheRepo) PurgeDefaultTenant(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM llm_prompt_cache WHERE cache_key NOT LIKE 'tenant:%'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *llmPromptCacheRepo) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM llm_prompt_cache WHERE expires_at IS NOT NULL AND expires_at <= ?`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *llmPromptCacheRepo) TrimToBytes(ctx context.Context, maxBytes int64) (int64, error) {
	if maxBytes < 0 {
		maxBytes = 0
	}
	stats, err := r.Stats(ctx, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	if stats.TotalBytes <= maxBytes {
		return 0, nil
	}

	rows, err := r.readDB.QueryContext(ctx, `SELECT cache_key, payload_bytes FROM llm_prompt_cache ORDER BY accessed_at ASC, created_at ASC, cache_key ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	trimmed := int64(0)
	totalBytes := stats.TotalBytes
	for rows.Next() {
		var cacheKey string
		var payloadBytes int64
		if err := rows.Scan(&cacheKey, &payloadBytes); err != nil {
			return trimmed, err
		}
		if totalBytes <= maxBytes {
			break
		}
		res, err := r.db.ExecContext(ctx, `DELETE FROM llm_prompt_cache WHERE cache_key = ?`, cacheKey)
		if err != nil {
			return trimmed, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return trimmed, err
		}
		if affected > 0 {
			trimmed += affected
			totalBytes -= payloadBytes
		}
	}
	if err := rows.Err(); err != nil {
		return trimmed, err
	}
	return trimmed, nil
}

func (r *llmPromptCacheRepo) ListRecent(ctx context.Context, limit int) ([]*store.LLMPromptCacheEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.readDB.QueryContext(ctx, `SELECT cache_key, provider_id, model, kind, input_hash, payload, payload_bytes, cached_input_tokens, cache_write_tokens, hit_count, created_at, accessed_at, expires_at FROM llm_prompt_cache ORDER BY accessed_at DESC, created_at DESC, cache_key DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*store.LLMPromptCacheEntry, 0, limit)
	for rows.Next() {
		var (
			entry                          store.LLMPromptCacheEntry
			payload                        []byte
			createdAt, accessedAt, expires sql.NullString
		)
		if err := rows.Scan(
			&entry.CacheKey,
			&entry.ProviderID,
			&entry.Model,
			&entry.Kind,
			&entry.InputHash,
			&payload,
			&entry.PayloadBytes,
			&entry.CachedInputTokens,
			&entry.CacheWriteTokens,
			&entry.HitCount,
			&createdAt,
			&accessedAt,
			&expires,
		); err != nil {
			return nil, err
		}
		entry.Payload = append([]byte(nil), payload...)
		if createdAt.Valid {
			entry.CreatedAt = mustParseTime(createdAt.String)
		}
		if accessedAt.Valid {
			entry.AccessedAt = mustParseTime(accessedAt.String)
		}
		if expires.Valid {
			t := mustParseTime(expires.String)
			entry.ExpiresAt = &t
		}
		items = append(items, &entry)
	}
	return items, rows.Err()
}
func (r *llmPromptCacheRepo) Stats(ctx context.Context, now time.Time) (*store.LLMPromptCacheStats, error) {
	row := r.readDB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(payload_bytes), 0), COALESCE(SUM(CASE WHEN expires_at IS NOT NULL AND expires_at <= ? THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN expires_at IS NOT NULL AND expires_at <= ? THEN payload_bytes ELSE 0 END), 0), COALESCE(SUM(hit_count), 0) FROM llm_prompt_cache`, now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	stats := &store.LLMPromptCacheStats{}
	if err := row.Scan(&stats.Entries, &stats.TotalBytes, &stats.ExpiredEntries, &stats.ExpiredBytes, &stats.TotalHits); err != nil {
		return nil, err
	}
	return stats, nil
}
