package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func NewLLMBillingLedgerRepository(db, readDB *sql.DB) store.LLMBillingLedgerRepository {
	if readDB == nil {
		readDB = db
	}
	return &llmBillingLedgerRepo{db: db, readDB: readDB}
}

func (r *llmBillingLedgerRepo) RecordSettlement(ctx context.Context, settlement *store.LLMBillingSettlement) (bool, error) {
	if r == nil || r.db == nil || settlement == nil || strings.TrimSpace(settlement.RequestID) == "" {
		return false, nil
	}
	createdAt := settlement.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	result, err := r.db.ExecContext(ctx, `INSERT OR IGNORE INTO llm_billing_ledger (
		tenant_id, request_id, user_id, email, provider_id, service_group_ids_json,
		input_tokens, output_tokens, requested_microcredits, deducted_microcredits,
		provider_multiplier, billing_group_multiplier, pricing_json, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalizeTenantID(settlement.TenantID),
		strings.TrimSpace(settlement.RequestID),
		strings.TrimSpace(settlement.UserID),
		strings.ToLower(strings.TrimSpace(settlement.Email)),
		strings.TrimSpace(settlement.ProviderID),
		strings.TrimSpace(settlement.ServiceGroupIDsJSON),
		settlement.InputTokens,
		settlement.OutputTokens,
		settlement.RequestedMicrocredits,
		settlement.DeductedMicrocredits,
		settlement.ProviderMultiplier,
		settlement.BillingGroupMultiplier,
		strings.TrimSpace(settlement.PricingJSON),
		createdAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return err == nil && rows > 0, err
}
