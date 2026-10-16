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

func (r *llmBillingLedgerRepo) RecordSettlement(ctx context.Context, settlement *store.LLMBillingSettlement, events ...store.LLMBillingOutboxEvent) (bool, error) {
	if r == nil || r.db == nil || settlement == nil || strings.TrimSpace(settlement.RequestID) == "" {
		return false, nil
	}
	createdAt := settlement.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO llm_billing_ledger (
		tenant_id, request_id, user_id, email, provider_id, service_group_ids_json,
		input_tokens, output_tokens, requested_microcredits, deducted_microcredits,
		provider_multiplier, billing_group_multiplier, pricing_json,
		cached_input_tokens, cache_write_tokens,
		normal_input_credits, cache_read_credits, cache_write_credits, output_credits,
		normal_input_cost_rmb, cache_read_cost_rmb, cache_write_cost_rmb, output_cost_rmb,
		created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		settlement.CachedInputTokens,
		settlement.CacheWriteTokens,
		settlement.NormalInputCredits,
		settlement.CacheReadCredits,
		settlement.CacheWriteCredits,
		settlement.OutputCredits,
		settlement.NormalInputCostRMB,
		settlement.CacheReadCostRMB,
		settlement.CacheWriteCostRMB,
		settlement.OutputCostRMB,
		createdAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, err
	}
	if err := insertLLMBillingOutboxEvents(ctx, tx, settlement.TenantID, settlement.RequestID, createdAt, events); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return err == nil && rows > 0, err
}

// RecordBillingEvents appends outbox events that have no settlement row of
// their own (currently reservation releases).
func (r *llmBillingLedgerRepo) RecordBillingEvents(ctx context.Context, events ...store.LLMBillingOutboxEvent) error {
	if r == nil || r.db == nil || len(events) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertLLMBillingOutboxEvents(ctx, tx, "", "", time.Now().UTC(), events); err != nil {
		return err
	}
	return tx.Commit()
}

// insertLLMBillingOutboxEvents writes the outbox rows inside tx. The
// (tenant_id, request_id, event_type) unique key makes a replayed settlement
// or release a no-op, matching the consumer idempotency contract. An event
// with an explicit tenant/request wins over the settlement defaults.
func insertLLMBillingOutboxEvents(ctx context.Context, tx *sql.Tx, defaultTenantID, defaultRequestID string, defaultCreatedAt time.Time, events []store.LLMBillingOutboxEvent) error {
	for _, event := range events {
		requestID := strings.TrimSpace(event.RequestID)
		if requestID == "" {
			requestID = strings.TrimSpace(defaultRequestID)
		}
		eventType := strings.TrimSpace(event.EventType)
		if requestID == "" || eventType == "" {
			continue
		}
		createdAt := event.CreatedAt
		if createdAt.IsZero() {
			createdAt = defaultCreatedAt
		}
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		payload := strings.TrimSpace(event.Payload)
		if payload == "" {
			payload = "{}"
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO llm_billing_outbox (
			tenant_id, request_id, event_type, payload_json, created_at
		) VALUES (?, ?, ?, ?, ?)`,
			normalizeTenantID(firstNonEmptyString(event.TenantID, defaultTenantID)),
			requestID,
			eventType,
			payload,
			createdAt.UTC().Format(time.RFC3339),
		); err != nil {
			return err
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
