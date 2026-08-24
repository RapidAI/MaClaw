package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type llmUsageRecordRepo struct {
	db, readDB *sql.DB
}

func NewLLMUsageRepository(db, readDB *sql.DB) store.LLMUsageRepository {
	if readDB == nil {
		readDB = db
	}
	return &llmUsageRecordRepo{db: db, readDB: readDB}
}

func (r *llmUsageRecordRepo) Insert(ctx context.Context, rec *store.LLMUsageRecord) error {
	if r == nil || r.db == nil || rec == nil {
		return nil
	}
	createdAt := rec.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO llm_usage_records (tenant_id, user_id, email, provider_id, model, service_group_id, workload_class, class_source, request_preview, input_tokens, output_tokens, total_tokens, credits_deducted, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalizeTenantID(rec.TenantID),
		strings.TrimSpace(rec.UserID),
		strings.ToLower(strings.TrimSpace(rec.Email)),
		strings.TrimSpace(rec.ProviderID),
		strings.TrimSpace(rec.Model),
		strings.TrimSpace(rec.ServiceGroupID),
		strings.TrimSpace(rec.WorkloadClass),
		strings.TrimSpace(rec.ClassSource),
		strings.TrimSpace(rec.Preview),
		rec.InputTokens,
		rec.OutputTokens,
		rec.TotalTokens,
		rec.Credits,
		createdAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (r *llmUsageRecordRepo) ListByGroupClass(ctx context.Context, tenantID, groupID, class string) ([]store.LLMUsageRecord, error) {
	if r == nil || r.readDB == nil {
		return nil, nil
	}
	rows, err := r.readDB.QueryContext(ctx,
		`SELECT id, tenant_id, user_id, email, provider_id, model, service_group_id, workload_class, class_source, request_preview, input_tokens, output_tokens, total_tokens, credits_deducted, created_at
		 FROM llm_usage_records
		 WHERE tenant_id = ? AND service_group_id = ? AND workload_class = ?
		 ORDER BY created_at ASC, id ASC`,
		normalizeTenantID(tenantID),
		strings.TrimSpace(groupID),
		strings.TrimSpace(class),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []store.LLMUsageRecord{}
	for rows.Next() {
		var rec store.LLMUsageRecord
		var created string
		if err := rows.Scan(&rec.ID, &rec.TenantID, &rec.UserID, &rec.Email, &rec.ProviderID, &rec.Model, &rec.ServiceGroupID, &rec.WorkloadClass, &rec.ClassSource, &rec.Preview, &rec.InputTokens, &rec.OutputTokens, &rec.TotalTokens, &rec.Credits, &created); err != nil {
			return nil, err
		}
		if ts, parseErr := time.Parse(time.RFC3339, created); parseErr == nil {
			rec.CreatedAt = ts
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
