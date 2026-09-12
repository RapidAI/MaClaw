package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// ---------------------------------------------------------------------------
// LLM Tables Migration
// ---------------------------------------------------------------------------

// EnsureLLMTables creates all LLM-related tables if they don't exist.
func EnsureLLMTables(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS llm_tenant_authorizations (
			id TEXT PRIMARY KEY,
			hub_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			admin_email TEXT NOT NULL,
			service_group_id TEXT NOT NULL,
			credits_total REAL NOT NULL DEFAULT 0,
			credits_used REAL NOT NULL DEFAULT 0,
			starts_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			allow_external_providers INTEGER NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'card',
			card_order_id TEXT NOT NULL DEFAULT '',
			bound_node_id TEXT NOT NULL DEFAULT '',
			bound_at TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_auth_hub_tenant ON llm_tenant_authorizations(hub_id, tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_auth_service_group ON llm_tenant_authorizations(service_group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_auth_status ON llm_tenant_authorizations(status, expires_at)`,
		`CREATE TABLE IF NOT EXISTS llm_usage_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			hub_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			request_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cached_input_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			cache_usage_source TEXT NOT NULL DEFAULT '',
			usage_anomaly TEXT NOT NULL DEFAULT '',
			pricing_source TEXT NOT NULL DEFAULT '',
			credits_deducted REAL NOT NULL DEFAULT 0,
			cache_hit INTEGER NOT NULL DEFAULT 0,
			auth_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_usage_hub_tenant_time ON llm_usage_records(hub_id, tenant_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_usage_created_at ON llm_usage_records(created_at)`,

		// Idempotency ledger for HA-replicated usage batches: a batch is applied
		// at most once per node even if the op log redelivers it.
		`CREATE TABLE IF NOT EXISTS llm_usage_sync_batches (
			batch_id TEXT PRIMARY KEY,
			source_node_id TEXT NOT NULL DEFAULT '',
			record_count INTEGER NOT NULL DEFAULT 0,
			applied_at TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS llm_card_types (
			id TEXT PRIMARY KEY,
			service_group_id TEXT NOT NULL,
			label TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			credits REAL NOT NULL,
			period TEXT NOT NULL,
			price_rmb REAL NOT NULL,
			template TEXT NOT NULL DEFAULT 'gradient_blue',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS llm_card_orders (
			id TEXT PRIMARY KEY,
			order_no TEXT NOT NULL UNIQUE,
			card_type_id TEXT NOT NULL,
			admin_email TEXT NOT NULL,
			hub_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			service_group_id TEXT NOT NULL,
			agent_id TEXT NOT NULL DEFAULT '',
			agent_name TEXT NOT NULL DEFAULT '',
			credits REAL NOT NULL,
			period TEXT NOT NULL,
			amount REAL NOT NULL,
			payment_mode TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			pay_channel TEXT NOT NULL DEFAULT '',
			pay_qr_url TEXT NOT NULL DEFAULT '',
			pay_deep_link TEXT NOT NULL DEFAULT '',
			pay_instruction TEXT NOT NULL DEFAULT '',
			pay_url TEXT NOT NULL DEFAULT '',
			payment_id TEXT NOT NULL DEFAULT '',
			payment_msg TEXT NOT NULL DEFAULT '',
			reviewed_by TEXT NOT NULL DEFAULT '',
			reviewed_at TEXT NOT NULL DEFAULT '',
			paid_at TEXT NOT NULL DEFAULT '',
			archived_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS llm_node_bindings (
			hub_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			bound_at TEXT NOT NULL,
			last_active TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			PRIMARY KEY (hub_id, tenant_id)
		)`,
		// Reconciliation facts outlive the process-local proxy cache. Do not add
		// automatic retention here: Hub keeps sent reservations until it can
		// retrieve this exact first-write-wins record.
		`CREATE TABLE IF NOT EXISTS llm_proxy_billing_attempts (
			hub_id TEXT NOT NULL COLLATE NOCASE,
			tenant_id TEXT NOT NULL COLLATE NOCASE,
			request_id TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			provider_id TEXT NOT NULL,
			pricing_snapshot_json TEXT NOT NULL,
			completed_at TEXT NOT NULL,
			PRIMARY KEY (hub_id, tenant_id, request_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_proxy_billing_attempts_completed_at ON llm_proxy_billing_attempts(completed_at)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("ensure llm table: %w", err)
		}
	}
	if err := ensureLLMCardTypeDescriptionColumn(db); err != nil {
		return err
	}
	if err := ensureLLMCardOrderAgentColumns(db); err != nil {
		return err
	}
	if err := ensureLLMCardOrderArchivedColumn(db); err != nil {
		return err
	}
	if err := ensureLLMCardOrderPaymentDetailColumns(db); err != nil {
		return err
	}
	if err := ensureLLMCardOrderAuthorizationIndex(db); err != nil {
		return err
	}
	if err := ensureLLMCardOrderIndexes(db); err != nil {
		return err
	}
	if err := ensureLLMUsageClassColumns(db); err != nil {
		return err
	}
	if err := ensureLLMUsageSyncColumns(db); err != nil {
		return err
	}
	return nil
}

func ensureLLMUsageSyncColumns(db *sql.DB) error {
	columns, err := tableColumns(db, "llm_usage_records")
	if err != nil {
		return err
	}
	if !columns["origin_node_id"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN origin_node_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.origin_node_id: %w", err)
		}
	}
	if !columns["sync_id"] {
		// Nullable on purpose: legacy local rows stay NULL, and the partial
		// unique index below only constrains replicated rows (which always
		// carry an ID), letting INSERT OR IGNORE deduplicate them.
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN sync_id TEXT`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.sync_id: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_usage_sync_id ON llm_usage_records(sync_id) WHERE sync_id IS NOT NULL`); err != nil {
		return fmt.Errorf("ensure llm usage sync id index: %w", err)
	}
	return nil
}

func ensureLLMUsageClassColumns(db *sql.DB) error {
	columns, err := tableColumns(db, "llm_usage_records")
	if err != nil {
		return err
	}
	if !columns["service_group_id"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN service_group_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.service_group_id: %w", err)
		}
	}
	if !columns["request_id"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN request_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.request_id: %w", err)
		}
	}
	if !columns["workload_class"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN workload_class TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.workload_class: %w", err)
		}
	}
	if !columns["class_source"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN class_source TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.class_source: %w", err)
		}
	}
	if !columns["request_preview"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN request_preview TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.request_preview: %w", err)
		}
	}
	if !columns["cached_input_tokens"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN cached_input_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.cached_input_tokens: %w", err)
		}
	}
	if !columns["cache_write_tokens"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN cache_write_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.cache_write_tokens: %w", err)
		}
	}
	if !columns["cache_usage_source"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN cache_usage_source TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.cache_usage_source: %w", err)
		}
	}
	if !columns["usage_anomaly"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN usage_anomaly TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.usage_anomaly: %w", err)
		}
	}
	if !columns["pricing_source"] {
		if _, err := db.Exec(`ALTER TABLE llm_usage_records ADD COLUMN pricing_source TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_usage_records.pricing_source: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_llm_usage_group_class_time ON llm_usage_records(service_group_id, workload_class, created_at)`); err != nil {
		return fmt.Errorf("ensure llm usage group/class index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_llm_usage_time_group ON llm_usage_records(created_at, service_group_id)`); err != nil {
		return fmt.Errorf("ensure llm usage time/group index: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_llm_usage_hub_tenant_request ON llm_usage_records(hub_id, tenant_id, request_id)`); err != nil {
		return fmt.Errorf("ensure llm usage hub/tenant/request index: %w", err)
	}
	return nil
}

func ensureLLMCardOrderAuthorizationIndex(db *sql.DB) error {
	// Early builds did not prevent duplicated rows. Consolidate them before
	// adding the uniqueness guard so an upgrade keeps the canonical (earliest)
	// authorization instead of making HubCenter fail to start. Keep dependent
	// usage and order records pointing at that canonical authorization rather
	// than leaving dangling audit/history references after the cleanup.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin llm authorization deduplication: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
		UPDATE llm_usage_records
		SET auth_id = (
			SELECT canonical.id
			FROM llm_tenant_authorizations AS duplicate
			JOIN llm_tenant_authorizations AS canonical
			  ON canonical.card_order_id = duplicate.card_order_id
			WHERE duplicate.id = llm_usage_records.auth_id
			  AND duplicate.card_order_id <> ''
			ORDER BY canonical.rowid ASC
			LIMIT 1
		)
		WHERE auth_id IN (
			SELECT duplicate.id
			FROM llm_tenant_authorizations AS duplicate
			WHERE duplicate.card_order_id <> ''
			  AND duplicate.rowid <> (
				SELECT MIN(canonical.rowid)
				FROM llm_tenant_authorizations AS canonical
				WHERE canonical.card_order_id = duplicate.card_order_id
			  )
		)
	`); err != nil {
		return fmt.Errorf("repair llm usage authorization references: %w", err)
	}
	if _, err := tx.Exec(`
		UPDATE llm_card_orders
		SET payment_id = (
			SELECT canonical.id
			FROM llm_tenant_authorizations AS duplicate
			JOIN llm_tenant_authorizations AS canonical
			  ON canonical.card_order_id = duplicate.card_order_id
			WHERE duplicate.id = llm_card_orders.payment_id
			  AND duplicate.card_order_id <> ''
			ORDER BY canonical.rowid ASC
			LIMIT 1
		)
		WHERE payment_id IN (
			SELECT duplicate.id
			FROM llm_tenant_authorizations AS duplicate
			WHERE duplicate.card_order_id <> ''
			  AND duplicate.rowid <> (
				SELECT MIN(canonical.rowid)
				FROM llm_tenant_authorizations AS canonical
				WHERE canonical.card_order_id = duplicate.card_order_id
			  )
		)
	`); err != nil {
		return fmt.Errorf("repair llm order authorization references: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM llm_tenant_authorizations
		WHERE card_order_id <> ''
		  AND rowid NOT IN (
			SELECT MIN(rowid)
			FROM llm_tenant_authorizations
			WHERE card_order_id <> ''
			GROUP BY card_order_id
		  )
	`); err != nil {
		return fmt.Errorf("deduplicate llm_tenant_authorizations.card_order_id: %w", err)
	}
	if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_auth_card_order ON llm_tenant_authorizations(card_order_id) WHERE card_order_id <> ''`); err != nil {
		return fmt.Errorf("ensure llm_tenant_authorizations.card_order_id unique index: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit llm authorization deduplication: %w", err)
	}
	return nil
}

func ensureLLMCardTypeDescriptionColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(llm_card_types)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == "description" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE llm_card_types ADD COLUMN description TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("ensure llm_card_types.description: %w", err)
	}
	return nil
}

func ensureLLMCardOrderAgentColumns(db *sql.DB) error {
	columns, err := tableColumns(db, "llm_card_orders")
	if err != nil {
		return err
	}
	if !columns["agent_id"] {
		if _, err := db.Exec(`ALTER TABLE llm_card_orders ADD COLUMN agent_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_card_orders.agent_id: %w", err)
		}
	}
	if !columns["agent_name"] {
		if _, err := db.Exec(`ALTER TABLE llm_card_orders ADD COLUMN agent_name TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_card_orders.agent_name: %w", err)
		}
	}
	return nil
}

func ensureLLMCardOrderArchivedColumn(db *sql.DB) error {
	columns, err := tableColumns(db, "llm_card_orders")
	if err != nil {
		return err
	}
	if !columns["archived_at"] {
		if _, err := db.Exec(`ALTER TABLE llm_card_orders ADD COLUMN archived_at TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_card_orders.archived_at: %w", err)
		}
	}
	return nil
}

func ensureLLMCardOrderPaymentDetailColumns(db *sql.DB) error {
	columns, err := tableColumns(db, "llm_card_orders")
	if err != nil {
		return err
	}
	if !columns["pay_deep_link"] {
		if _, err := db.Exec(`ALTER TABLE llm_card_orders ADD COLUMN pay_deep_link TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_card_orders.pay_deep_link: %w", err)
		}
	}
	if !columns["pay_instruction"] {
		if _, err := db.Exec(`ALTER TABLE llm_card_orders ADD COLUMN pay_instruction TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("ensure llm_card_orders.pay_instruction: %w", err)
		}
	}
	return nil
}

func ensureLLMCardOrderIndexes(db *sql.DB) error {
	// Legacy databases may still miss columns added by later migrations; only
	// index columns that actually exist so EnsureLLMTables stays idempotent.
	columns, err := tableColumns(db, "llm_card_orders")
	if err != nil {
		return err
	}
	if columns["hub_id"] && columns["tenant_id"] {
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_llm_orders_hub_tenant ON llm_card_orders(hub_id, tenant_id)`); err != nil {
			return fmt.Errorf("ensure llm orders hub/tenant index: %w", err)
		}
	}
	if columns["status"] && columns["archived_at"] && columns["created_at"] {
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_llm_orders_status_archived_created ON llm_card_orders(status, archived_at, created_at)`); err != nil {
			return fmt.Errorf("ensure llm orders status index: %w", err)
		}
	}
	if columns["service_group_id"] {
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_llm_orders_service_group ON llm_card_orders(service_group_id)`); err != nil {
			return fmt.Errorf("ensure llm orders service group index: %w", err)
		}
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

// ---------------------------------------------------------------------------
// TenantAuthorizationRepository implementation
// ---------------------------------------------------------------------------

type llmAuthRepo struct {
	write *sql.DB
	read  *sql.DB
}

func NewLLMAuthRepo(p *Provider) *llmAuthRepo {
	return &llmAuthRepo{write: p.Write, read: p.Read}
}

func (r *llmAuthRepo) Create(ctx context.Context, auth *llmservice.TenantAuthorization) error {
	_, err := r.write.ExecContext(ctx,
		`INSERT INTO llm_tenant_authorizations (id, hub_id, tenant_id, admin_email, service_group_id, credits_total, credits_used, starts_at, expires_at, allow_external_providers, source, card_order_id, bound_node_id, bound_at, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		auth.ID, auth.HubID, auth.TenantID, auth.AdminEmail, auth.ServiceGroupID,
		auth.CreditsTotal, auth.CreditsUsed,
		auth.StartsAt.Format(time.RFC3339), auth.ExpiresAt.Format(time.RFC3339),
		boolToInt(auth.AllowExternalProviders), auth.Source, auth.CardOrderID,
		auth.BoundNodeID, formatTimeOrEmpty(auth.BoundAt),
		auth.Status, auth.CreatedAt.Format(time.RFC3339), auth.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *llmAuthRepo) GetByID(ctx context.Context, id string) (*llmservice.TenantAuthorization, error) {
	row := r.read.QueryRowContext(ctx, `SELECT id, hub_id, tenant_id, admin_email, service_group_id, credits_total, credits_used, starts_at, expires_at, allow_external_providers, source, card_order_id, bound_node_id, bound_at, status, created_at, updated_at FROM llm_tenant_authorizations WHERE id = ?`, id)
	return scanAuth(row)
}

func (r *llmAuthRepo) GetByCardOrderID(ctx context.Context, orderNo string) (*llmservice.TenantAuthorization, error) {
	row := r.read.QueryRowContext(ctx, `SELECT id, hub_id, tenant_id, admin_email, service_group_id, credits_total, credits_used, starts_at, expires_at, allow_external_providers, source, card_order_id, bound_node_id, bound_at, status, created_at, updated_at FROM llm_tenant_authorizations WHERE card_order_id = ?`, strings.TrimSpace(orderNo))
	return scanAuth(row)
}

func (r *llmAuthRepo) ListByHubTenant(ctx context.Context, hubID, tenantID string) ([]*llmservice.TenantAuthorization, error) {
	rows, err := r.read.QueryContext(ctx, `SELECT id, hub_id, tenant_id, admin_email, service_group_id, credits_total, credits_used, starts_at, expires_at, allow_external_providers, source, card_order_id, bound_node_id, bound_at, status, created_at, updated_at FROM llm_tenant_authorizations WHERE hub_id = ? AND tenant_id = ?`, hubID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuthRows(rows)
}

func (r *llmAuthRepo) ListByHub(ctx context.Context, hubID string) ([]*llmservice.TenantAuthorization, error) {
	rows, err := r.read.QueryContext(ctx, `SELECT id, hub_id, tenant_id, admin_email, service_group_id, credits_total, credits_used, starts_at, expires_at, allow_external_providers, source, card_order_id, bound_node_id, bound_at, status, created_at, updated_at FROM llm_tenant_authorizations WHERE hub_id = ? ORDER BY created_at DESC`, hubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuthRows(rows)
}

func (r *llmAuthRepo) ListByServiceGroup(ctx context.Context, serviceGroupID string) ([]*llmservice.TenantAuthorization, error) {
	rows, err := r.read.QueryContext(ctx, `SELECT id, hub_id, tenant_id, admin_email, service_group_id, credits_total, credits_used, starts_at, expires_at, allow_external_providers, source, card_order_id, bound_node_id, bound_at, status, created_at, updated_at FROM llm_tenant_authorizations WHERE service_group_id = ? ORDER BY created_at DESC`, serviceGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuthRows(rows)
}

func (r *llmAuthRepo) ListAll(ctx context.Context) ([]*llmservice.TenantAuthorization, error) {
	rows, err := r.read.QueryContext(ctx, `SELECT id, hub_id, tenant_id, admin_email, service_group_id, credits_total, credits_used, starts_at, expires_at, allow_external_providers, source, card_order_id, bound_node_id, bound_at, status, created_at, updated_at FROM llm_tenant_authorizations ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuthRows(rows)
}

func (r *llmAuthRepo) Update(ctx context.Context, auth *llmservice.TenantAuthorization) error {
	updatedAt := auth.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
		auth.UpdatedAt = updatedAt
	}
	_, err := r.write.ExecContext(ctx,
		`UPDATE llm_tenant_authorizations SET
			hub_id=?, tenant_id=?, admin_email=?, service_group_id=?,
			credits_total=?, credits_used=?, starts_at=?, expires_at=?,
			allow_external_providers=?, source=?, card_order_id=?,
			bound_node_id=?, bound_at=?, status=?, updated_at=?
		 WHERE id=?`,
		auth.HubID, auth.TenantID, auth.AdminEmail, auth.ServiceGroupID,
		auth.CreditsTotal, auth.CreditsUsed,
		auth.StartsAt.Format(time.RFC3339), auth.ExpiresAt.Format(time.RFC3339),
		boolToInt(auth.AllowExternalProviders), auth.Source, auth.CardOrderID,
		auth.BoundNodeID, formatTimeOrEmpty(auth.BoundAt), auth.Status,
		updatedAt.Format(time.RFC3339), auth.ID,
	)
	return err
}

func (r *llmAuthRepo) DeductCredits(ctx context.Context, id string, credits float64, now time.Time) (float64, error) {
	if credits <= 0 {
		return 0, nil
	}
	conn, err := r.write.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	var remaining float64
	err = conn.QueryRowContext(ctx, `SELECT credits_total - credits_used FROM llm_tenant_authorizations WHERE id = ? AND credits_total > credits_used`, id).Scan(&remaining)
	if err == sql.ErrNoRows {
		if _, commitErr := conn.ExecContext(ctx, `COMMIT`); commitErr != nil {
			return 0, commitErr
		}
		committed = true
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	actual := credits
	if remaining < actual {
		actual = remaining
	}
	if actual <= 0 {
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			return 0, err
		}
		committed = true
		return 0, nil
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE llm_tenant_authorizations
		    SET credits_used = credits_used + ?,
		        status = CASE WHEN credits_used + ? >= credits_total THEN 'exhausted' ELSE status END,
		        updated_at = ?
		  WHERE id = ?`,
		actual, actual, now.Format(time.RFC3339), id,
	); err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return 0, err
	}
	committed = true
	return actual, nil
}

// ---------------------------------------------------------------------------
// UsageRepository implementation
// ---------------------------------------------------------------------------

type llmUsageRepo struct {
	write *sql.DB
	read  *sql.DB
}

type llmProxyBillingAttemptRepo struct {
	write *sql.DB
	read  *sql.DB
}

func NewProxyBillingAttemptRepo(p *Provider) *llmProxyBillingAttemptRepo {
	return &llmProxyBillingAttemptRepo{write: p.Write, read: p.Read}
}

func (r *llmProxyBillingAttemptRepo) RecordProxyBillingAttempt(ctx context.Context, attempt llmservice.ProxyBillingAttempt) (bool, error) {
	if r == nil || r.write == nil {
		return false, fmt.Errorf("proxy billing attempt repository is not configured")
	}
	completedAt := attempt.CompletedAt.UTC()
	if completedAt.IsZero() {
		return false, fmt.Errorf("proxy billing attempt completed_at is required")
	}
	snapshot, err := json.Marshal(attempt.PricingSnapshot)
	if err != nil {
		return false, fmt.Errorf("encode proxy billing pricing snapshot: %w", err)
	}
	result, err := r.write.ExecContext(ctx, `
		INSERT OR IGNORE INTO llm_proxy_billing_attempts
			(hub_id, tenant_id, request_id, status_code, provider_id, pricing_snapshot_json, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(attempt.HubID), strings.TrimSpace(attempt.TenantID), strings.TrimSpace(attempt.RequestID),
		attempt.StatusCode, strings.TrimSpace(attempt.ProviderID), string(snapshot), completedAt.Format(time.RFC3339Nano))
	if err != nil {
		return false, fmt.Errorf("insert proxy billing attempt: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect proxy billing attempt insert: %w", err)
	}
	return rows > 0, nil
}

func (r *llmProxyBillingAttemptRepo) GetProxyBillingAttempt(ctx context.Context, hubID, tenantID, requestID string) (llmservice.ProxyBillingAttempt, bool, error) {
	if r == nil || r.read == nil {
		return llmservice.ProxyBillingAttempt{}, false, fmt.Errorf("proxy billing attempt repository is not configured")
	}
	var attempt llmservice.ProxyBillingAttempt
	var snapshotJSON, completedAt string
	err := r.read.QueryRowContext(ctx, `
		SELECT hub_id, tenant_id, request_id, status_code, provider_id, pricing_snapshot_json, completed_at
		  FROM llm_proxy_billing_attempts
		 WHERE hub_id = ? COLLATE NOCASE AND tenant_id = ? COLLATE NOCASE AND request_id = ?`,
		strings.TrimSpace(hubID), strings.TrimSpace(tenantID), strings.TrimSpace(requestID)).Scan(
		&attempt.HubID, &attempt.TenantID, &attempt.RequestID, &attempt.StatusCode, &attempt.ProviderID, &snapshotJSON, &completedAt)
	if err == sql.ErrNoRows {
		return llmservice.ProxyBillingAttempt{}, false, nil
	}
	if err != nil {
		return llmservice.ProxyBillingAttempt{}, false, fmt.Errorf("query proxy billing attempt: %w", err)
	}
	if err := json.Unmarshal([]byte(snapshotJSON), &attempt.PricingSnapshot); err != nil {
		return llmservice.ProxyBillingAttempt{}, false, fmt.Errorf("decode proxy billing pricing snapshot: %w", err)
	}
	completed, err := time.Parse(time.RFC3339Nano, completedAt)
	if err != nil {
		return llmservice.ProxyBillingAttempt{}, false, fmt.Errorf("parse proxy billing completed_at: %w", err)
	}
	attempt.CompletedAt = completed.UTC()
	return attempt, true, nil
}

func NewLLMUsageRepo(p *Provider) *llmUsageRepo {
	return &llmUsageRepo{write: p.Write, read: p.Read}
}

func (r *llmUsageRepo) Insert(ctx context.Context, record *llmservice.TenantUsageRecord) error {
	return r.insert(ctx, record, "")
}

// InsertLocal persists a usage record originated by this HA node so a later
// sync seed pass can tell locally-originated rows apart from replicated ones.
func (r *llmUsageRepo) InsertLocal(ctx context.Context, record *llmservice.TenantUsageRecord, nodeID string) error {
	return r.insert(ctx, record, strings.TrimSpace(nodeID))
}

func (r *llmUsageRepo) insert(ctx context.Context, record *llmservice.TenantUsageRecord, originNodeID string) error {
	if record == nil {
		return nil
	}
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	_, err := r.write.ExecContext(ctx,
		`INSERT INTO llm_usage_records (hub_id, tenant_id, request_id, model, provider_id, service_group_id, workload_class, class_source, request_preview, input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, cache_usage_source, usage_anomaly, pricing_source, credits_deducted, cache_hit, auth_id, created_at, origin_node_id, sync_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(record.HubID), strings.TrimSpace(record.TenantID),
		strings.TrimSpace(record.RequestID),
		strings.TrimSpace(record.Model), strings.TrimSpace(record.ProviderID),
		strings.TrimSpace(record.ServiceGroupID), strings.TrimSpace(record.WorkloadClass),
		strings.TrimSpace(record.ClassSource), record.Preview,
		record.InputTokens, record.OutputTokens, record.CachedInputTokens, record.CacheWriteTokens, strings.TrimSpace(record.CacheUsageSource), strings.TrimSpace(record.UsageAnomaly), strings.TrimSpace(record.PricingSource), record.Credits,
		boolToInt(record.CacheHit), strings.TrimSpace(record.AuthID), createdAt.UTC().Format(time.RFC3339),
		strings.TrimSpace(originNodeID), usageSyncIDValue(record.SyncID),
	)
	return err
}

// usageSyncIDValue stores an empty sync ID as NULL so the partial unique
// index on sync_id only constrains rows that actually carry one.
func usageSyncIDValue(syncID string) any {
	if s := strings.TrimSpace(syncID); s != "" {
		return s
	}
	return nil
}

// InsertBatch applies one HA-replicated usage batch. The batch ledger makes
// the apply idempotent: a redelivered batch ID is acknowledged without
// inserting the records a second time. applied reports whether the records
// were actually inserted by this call.
func (r *llmUsageRepo) InsertBatch(ctx context.Context, batchID, sourceNodeID string, records []*llmservice.TenantUsageRecord) (applied bool, err error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || len(records) == 0 {
		return false, nil
	}
	tx, err := r.write.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin llm usage batch apply: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO llm_usage_sync_batches (batch_id, source_node_id, record_count, applied_at) VALUES (?, ?, ?, ?)`,
		batchID, strings.TrimSpace(sourceNodeID), len(records), time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return false, fmt.Errorf("record llm usage sync batch: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return false, nil
	}
	for _, record := range records {
		if record == nil {
			continue
		}
		createdAt := record.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		// OR IGNORE plus the partial unique index on sync_id deduplicates at
		// record level: a row republished under a different batch ID (e.g. a
		// seed pass after an origin-node restart) is skipped, not counted.
		if _, err = tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO llm_usage_records (hub_id, tenant_id, request_id, model, provider_id, service_group_id, workload_class, class_source, request_preview, input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, cache_usage_source, usage_anomaly, pricing_source, credits_deducted, cache_hit, auth_id, created_at, origin_node_id, sync_id)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			strings.TrimSpace(record.HubID), strings.TrimSpace(record.TenantID),
			strings.TrimSpace(record.RequestID),
			strings.TrimSpace(record.Model), strings.TrimSpace(record.ProviderID),
			strings.TrimSpace(record.ServiceGroupID), strings.TrimSpace(record.WorkloadClass),
			strings.TrimSpace(record.ClassSource), record.Preview,
			record.InputTokens, record.OutputTokens, record.CachedInputTokens, record.CacheWriteTokens, strings.TrimSpace(record.CacheUsageSource), strings.TrimSpace(record.UsageAnomaly), strings.TrimSpace(record.PricingSource), record.Credits,
			boolToInt(record.CacheHit), strings.TrimSpace(record.AuthID), createdAt.UTC().Format(time.RFC3339),
			strings.TrimSpace(sourceNodeID), usageSyncIDValue(record.SyncID),
		); err != nil {
			return false, fmt.Errorf("insert replicated llm usage record: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("commit llm usage batch apply: %w", err)
	}
	return true, nil
}

// MaxUsageID returns the largest llm_usage_records primary key, or 0 on an
// empty table. The HA sync seed uses it as an upper bound so rows recorded
// after the live replicator started are not published twice.
func (r *llmUsageRepo) MaxUsageID(ctx context.Context) (int64, error) {
	var id int64
	if err := r.read.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM llm_usage_records`).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// ListLocalForSync pages locally-originated usage records by primary key for
// the HA sync seed pass. Rows with an empty origin predate the origin_node_id
// column and are necessarily local; replicated rows always carry a non-empty
// origin and are never echoed back. maxID bounds the scan (<= 0 means no
// bound): rows above it belong to the live replicator and must not be
// published a second time by the seed.
func (r *llmUsageRepo) ListLocalForSync(ctx context.Context, nodeID string, afterID, maxID int64, limit int) ([]*llmservice.TenantUsageRecord, error) {
	if limit <= 0 {
		limit = 300
	}
	query := `SELECT id, hub_id, tenant_id, request_id, model, provider_id, service_group_id, workload_class, class_source, request_preview, input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, cache_usage_source, usage_anomaly, pricing_source, credits_deducted, cache_hit, auth_id, created_at, sync_id
	   FROM llm_usage_records
	  WHERE id > ? AND (origin_node_id = '' OR origin_node_id = ?)`
	args := []any{afterID, strings.TrimSpace(nodeID)}
	if maxID > 0 {
		query += ` AND id <= ?`
		args = append(args, maxID)
	}
	query += ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]*llmservice.TenantUsageRecord, 0)
	for rows.Next() {
		var rec llmservice.TenantUsageRecord
		var cacheHit int
		var createdAt string
		var syncID sql.NullString
		if err := rows.Scan(&rec.ID, &rec.HubID, &rec.TenantID, &rec.RequestID, &rec.Model, &rec.ProviderID, &rec.ServiceGroupID, &rec.WorkloadClass, &rec.ClassSource, &rec.Preview, &rec.InputTokens, &rec.OutputTokens, &rec.CachedInputTokens, &rec.CacheWriteTokens, &rec.CacheUsageSource, &rec.UsageAnomaly, &rec.PricingSource, &rec.Credits, &cacheHit, &rec.AuthID, &createdAt, &syncID); err != nil {
			return nil, err
		}
		rec.CacheHit = cacheHit == 1
		rec.CreatedAt, _ = parseStoredUsageTime(createdAt)
		rec.SyncID = syncID.String
		records = append(records, &rec)
	}
	return records, rows.Err()
}

func (r *llmUsageRepo) QuerySummary(ctx context.Context, filter llmservice.UsageFilter) ([]llmservice.TenantUsageSummary, error) {
	loc := llmservice.TrafficLocation(filter.Timezone)
	start, hasStart := parseUsageLocalDate(filter.StartDate, loc)
	end, hasEnd := parseUsageLocalDate(filter.EndDate, loc)
	offsetAt := time.Now()
	if hasStart {
		offsetAt = start
	}
	periodExpr, periodLabel := usagePeriodSQL(filter.Period, loc, offsetAt)

	aggs := periodExpr + ` as period_start, SUM(input_tokens), SUM(output_tokens), SUM(cached_input_tokens), SUM(cache_write_tokens), SUM(credits_deducted), COUNT(*), SUM(CASE WHEN cache_hit=1 THEN 1 ELSE 0 END)`
	selectCols := `TRIM(hub_id), TRIM(tenant_id), ` + aggs
	groupBy := `TRIM(hub_id), TRIM(tenant_id), ` + periodExpr
	if filter.GroupByServiceGroup {
		selectCols = `TRIM(hub_id), TRIM(tenant_id), TRIM(service_group_id), ` + aggs
		groupBy += `, TRIM(service_group_id)`
	}
	query := `SELECT ` + selectCols + ` FROM llm_usage_records WHERE 1=1`
	var args []any
	if hubID := strings.TrimSpace(filter.HubID); hubID != "" {
		query += ` AND hub_id = ?`
		args = append(args, hubID)
	}
	if tenantID := strings.TrimSpace(filter.TenantID); tenantID != "" {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		query += ` AND TRIM(model) = ?`
		args = append(args, model)
	}
	if groupID := strings.TrimSpace(filter.ServiceGroupID); groupID != "" {
		query += ` AND TRIM(service_group_id) = ?`
		args = append(args, groupID)
	}
	if class := strings.TrimSpace(filter.WorkloadClass); class != "" {
		query += ` AND TRIM(workload_class) = ?`
		args = append(args, class)
	}
	if hasStart {
		query += ` AND ` + usageCreatedAtWindowPred
		args = append(args,
			usageCreatedAtScanBound(start),
			sqliteUTCDateTime(start.UTC().Add(-24*time.Hour)),
			sqliteUTCDateTime(start),
		)
	}
	if hasEnd {
		endExclusive := end.AddDate(0, 0, 1)
		// Compare the normalized value so legacy space-separated timestamps are
		// included alongside the current RFC3339 representation.
		query += ` AND datetime(created_at) < ?`
		args = append(args, sqliteUTCDateTime(endExclusive))
	}
	query += ` GROUP BY ` + groupBy + ` ORDER BY period_start DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]llmservice.TenantUsageSummary, 0)
	for rows.Next() {
		var s llmservice.TenantUsageSummary
		var cacheHits int64
		var err error
		if filter.GroupByServiceGroup {
			err = rows.Scan(&s.HubID, &s.TenantID, &s.ServiceGroupID, &s.PeriodStart, &s.InputTokens, &s.OutputTokens, &s.CachedInputTokens, &s.CacheWriteTokens, &s.TotalCredits, &s.TotalRequests, &cacheHits)
		} else {
			err = rows.Scan(&s.HubID, &s.TenantID, &s.PeriodStart, &s.InputTokens, &s.OutputTokens, &s.CachedInputTokens, &s.CacheWriteTokens, &s.TotalCredits, &s.TotalRequests, &cacheHits)
		}
		if err != nil {
			return nil, err
		}
		s.Period = periodLabel
		s.CacheHits = cacheHits
		if s.TotalRequests > 0 {
			s.CacheHitRate = float64(cacheHits) / float64(s.TotalRequests)
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

func (r *llmUsageRepo) QueryRecent(ctx context.Context, hubID, tenantID string, limit int) ([]*llmservice.TenantUsageRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.read.QueryContext(ctx, `SELECT id, hub_id, tenant_id, request_id, model, provider_id, input_tokens, output_tokens, cached_input_tokens, cache_write_tokens, cache_usage_source, usage_anomaly, pricing_source, credits_deducted, cache_hit, auth_id, created_at FROM llm_usage_records WHERE hub_id=? AND tenant_id=? ORDER BY datetime(created_at) DESC, id DESC LIMIT ?`, strings.TrimSpace(hubID), strings.TrimSpace(tenantID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]*llmservice.TenantUsageRecord, 0)
	for rows.Next() {
		var rec llmservice.TenantUsageRecord
		var cacheHit int
		var createdAt string
		if err := rows.Scan(&rec.ID, &rec.HubID, &rec.TenantID, &rec.RequestID, &rec.Model, &rec.ProviderID, &rec.InputTokens, &rec.OutputTokens, &rec.CachedInputTokens, &rec.CacheWriteTokens, &rec.CacheUsageSource, &rec.UsageAnomaly, &rec.PricingSource, &rec.Credits, &cacheHit, &rec.AuthID, &createdAt); err != nil {
			return nil, err
		}
		rec.CacheHit = cacheHit == 1
		rec.CreatedAt, _ = parseStoredUsageTime(createdAt)
		records = append(records, &rec)
	}
	return records, rows.Err()
}

func addProviderPeriodTraffic(a, b llmservice.ProviderPeriodTraffic) llmservice.ProviderPeriodTraffic {
	return llmservice.ProviderPeriodTraffic{
		Day:   addTokenTraffic(a.Day, b.Day),
		Week:  addTokenTraffic(a.Week, b.Week),
		Month: addTokenTraffic(a.Month, b.Month),
	}
}

func addTokenTraffic(a, b llmservice.TokenTraffic) llmservice.TokenTraffic {
	return llmservice.TokenTraffic{
		InputTokens:  a.InputTokens + b.InputTokens,
		OutputTokens: a.OutputTokens + b.OutputTokens,
		TotalTokens:  a.TotalTokens + b.TotalTokens,
	}
}

// usageCreatedAtWindowPred keeps the indexed lexical lower bound for current
// RFC3339 rows while also admitting legacy space-separated timestamps. The
// normalized datetime check remains authoritative for the actual window.
const usageCreatedAtWindowPred = `(created_at >= ? OR datetime(created_at) >= ?) AND datetime(created_at) >= ?`

func sqliteUTCDateTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// parseStoredUsageTime accepts both the current RFC3339 representation and
// the space-separated SQLite datetime format emitted by older installations.
// Legacy values without an explicit offset are interpreted as UTC, matching
// the normalized storage contract used for new records.
func parseStoredUsageTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty usage timestamp")
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported usage timestamp %q", value)
}

func usageCreatedAtScanBound(since time.Time) string {
	return since.UTC().Add(-24 * time.Hour).Format(time.RFC3339)
}

func parseUsageLocalDate(value string, loc *time.Location) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if loc == nil {
		loc = time.UTC
	}
	t, err := time.ParseInLocation("2006-01-02", value, loc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func sqliteLocalTimeModifiers(loc *time.Location, at time.Time) string {
	if loc == nil {
		loc = time.UTC
	}
	if at.IsZero() {
		at = time.Now()
	}
	_, offset := at.In(loc).Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	mins := (offset % 3600) / 60
	if hours == 0 && mins == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	if hours != 0 {
		parts = append(parts, fmt.Sprintf("'%s%d hours'", sign, hours))
	}
	if mins != 0 {
		parts = append(parts, fmt.Sprintf("'%s%d minutes'", sign, mins))
	}
	return strings.Join(parts, ", ")
}

func usagePeriodSQL(period string, loc *time.Location, now time.Time) (expr, label string) {
	localDT := "datetime(created_at)"
	if mods := sqliteLocalTimeModifiers(loc, now); mods != "" {
		localDT = "datetime(created_at, " + mods + ")"
	}
	switch period {
	case "weekly":
		return "DATE(" + localDT + ", 'weekday 0', '-6 days')", "weekly"
	case "monthly":
		return "strftime('%Y-%m', " + localDT + ")", "monthly"
	default:
		return "DATE(" + localDT + ")", "daily"
	}
}

func (r *llmUsageRepo) QueryProviderTraffic(ctx context.Context, dayStart, weekStart, monthStart time.Time) (map[string]llmservice.ProviderPeriodTraffic, error) {
	return r.queryTokenPeriodTraffic(ctx, "TRIM(provider_id)", dayStart, weekStart, monthStart)
}

func (r *llmUsageRepo) queryTokenPeriodTraffic(ctx context.Context, keyExpr string, dayStart, weekStart, monthStart time.Time) (map[string]llmservice.ProviderPeriodTraffic, error) {
	var extraWhere string
	switch keyExpr {
	case "TRIM(provider_id)":
		extraWhere = ` AND TRIM(provider_id) != ''`
	case "TRIM(service_group_id)":
		extraWhere = ` AND TRIM(service_group_id) != ''`
	default:
		return nil, fmt.Errorf("unsupported traffic key %q", keyExpr)
	}
	dayBound := sqliteUTCDateTime(dayStart)
	weekBound := sqliteUTCDateTime(weekStart)
	monthBound := sqliteUTCDateTime(monthStart)
	// Widen the indexed scan by a day so offset-formatted legacy rows still enter the
	// datetime() filter. Some older databases also contain SQLite's
	// space-separated timestamp form ("YYYY-MM-DD HH:MM:SS"). A lexical
	// comparison against RFC3339 would exclude those rows before datetime() can
	// normalize them, which made provider traffic appear to be permanently zero.
	scanBound := usageCreatedAtScanBound(monthStart)
	scanDateTimeBound := sqliteUTCDateTime(monthStart.UTC().Add(-24 * time.Hour))
	rows, err := r.read.QueryContext(ctx, `
		SELECT traffic_key,
		       SUM(CASE WHEN ts >= ? THEN input_tokens ELSE 0 END),
		       SUM(CASE WHEN ts >= ? THEN output_tokens ELSE 0 END),
		       SUM(CASE WHEN ts >= ? THEN input_tokens ELSE 0 END),
		       SUM(CASE WHEN ts >= ? THEN output_tokens ELSE 0 END),
		       SUM(CASE WHEN ts >= ? THEN input_tokens ELSE 0 END),
		       SUM(CASE WHEN ts >= ? THEN output_tokens ELSE 0 END)
		  FROM (
		        SELECT `+keyExpr+` AS traffic_key, input_tokens, output_tokens, datetime(created_at) AS ts
		          FROM llm_usage_records
		         WHERE (created_at >= ? OR datetime(created_at) >= ?)`+extraWhere+`
		       ) AS usage
		 GROUP BY traffic_key`,
		dayBound, dayBound, weekBound, weekBound, monthBound, monthBound, scanBound, scanDateTimeBound,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := map[string]llmservice.ProviderPeriodTraffic{}
	for rows.Next() {
		var key string
		var dayIn, dayOut, weekIn, weekOut, monthIn, monthOut int64
		if err := rows.Scan(&key, &dayIn, &dayOut, &weekIn, &weekOut, &monthIn, &monthOut); err != nil {
			return nil, err
		}
		key = strings.TrimSpace(key)
		if key == "" || (dayIn == 0 && dayOut == 0 && weekIn == 0 && weekOut == 0 && monthIn == 0 && monthOut == 0) {
			continue
		}
		results[key] = addProviderPeriodTraffic(results[key], llmservice.ProviderPeriodTraffic{
			Day:   llmservice.TokenTraffic{InputTokens: dayIn, OutputTokens: dayOut, TotalTokens: dayIn + dayOut},
			Week:  llmservice.TokenTraffic{InputTokens: weekIn, OutputTokens: weekOut, TotalTokens: weekIn + weekOut},
			Month: llmservice.TokenTraffic{InputTokens: monthIn, OutputTokens: monthOut, TotalTokens: monthIn + monthOut},
		})
	}
	return results, rows.Err()
}

func (r *llmUsageRepo) QueryClassTraffic(ctx context.Context, serviceGroupID string, since time.Time) ([]llmservice.ClassTrafficRow, map[string]int64, []llmservice.ClassTrafficSample, error) {
	groupID := strings.TrimSpace(serviceGroupID)
	if groupID == "" {
		return nil, map[string]int64{}, nil, nil
	}
	rows, err := r.read.QueryContext(ctx, `
		SELECT TRIM(workload_class), TRIM(class_source), COUNT(*), SUM(input_tokens), SUM(output_tokens)
		  FROM llm_usage_records
		 WHERE `+usageCreatedAtWindowPred+`
		   AND TRIM(service_group_id) = ?
		 GROUP BY TRIM(workload_class), TRIM(class_source)`,
		usageCreatedAtScanBound(since), sqliteUTCDateTime(since.UTC().Add(-24*time.Hour)), sqliteUTCDateTime(since), groupID,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	byClass := map[string]llmservice.ClassTrafficRow{}
	sources := map[string]int64{}
	for rows.Next() {
		var class, source string
		var requests, inTok, outTok int64
		if err := rows.Scan(&class, &source, &requests, &inTok, &outTok); err != nil {
			return nil, nil, nil, err
		}
		if class == "" {
			class = "unclassified"
		}
		curr := byClass[class]
		curr.Class = class
		curr.Requests += requests
		curr.InputTokens += inTok
		curr.OutputTokens += outTok
		curr.TotalTokens += inTok + outTok
		byClass[class] = curr
		if source != "" {
			sources[source] += requests
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	out := make([]llmservice.ClassTrafficRow, 0, len(byClass))
	for _, row := range byClass {
		out = append(out, row)
	}
	samples, err := r.queryClassTrafficSamples(ctx, groupID, since)
	if err != nil {
		return out, sources, nil, err
	}
	return out, sources, samples, nil
}

func (r *llmUsageRepo) QueryServiceGroupTraffic(ctx context.Context, dayStart, weekStart, monthStart time.Time) (map[string]llmservice.ProviderPeriodTraffic, error) {
	return r.queryTokenPeriodTraffic(ctx, "TRIM(service_group_id)", dayStart, weekStart, monthStart)
}

func (r *llmUsageRepo) queryClassTrafficSamples(ctx context.Context, serviceGroupID string, since time.Time) ([]llmservice.ClassTrafficSample, error) {
	sampleRows, err := r.read.QueryContext(ctx, `
		SELECT created_at, TRIM(workload_class), TRIM(class_source), request_preview
		  FROM llm_usage_records
		 WHERE `+usageCreatedAtWindowPred+`
		   AND TRIM(service_group_id) = ?
		   AND TRIM(class_source) != ?
		   AND TRIM(request_preview) != ''
		 ORDER BY datetime(created_at) DESC, id DESC
		 LIMIT 20`,
		usageCreatedAtScanBound(since), sqliteUTCDateTime(since.UTC().Add(-24*time.Hour)), sqliteUTCDateTime(since), strings.TrimSpace(serviceGroupID), "hint",
	)
	if err != nil {
		return nil, err
	}
	defer sampleRows.Close()
	var samples []llmservice.ClassTrafficSample
	for sampleRows.Next() {
		var createdAt, class, source, preview string
		if err := sampleRows.Scan(&createdAt, &class, &source, &preview); err != nil {
			return nil, err
		}
		if class == "" {
			class = "unclassified"
		}
		at, _ := parseStoredUsageTime(createdAt)
		samples = append(samples, llmservice.ClassTrafficSample{
			At:      at,
			Class:   class,
			Source:  source,
			Preview: preview,
		})
	}
	return samples, sampleRows.Err()
}

// ---------------------------------------------------------------------------
// CardTypeRepository implementation
// ---------------------------------------------------------------------------

type llmCardTypeRepo struct {
	write *sql.DB
	read  *sql.DB
}

func NewLLMCardTypeRepo(p *Provider) *llmCardTypeRepo {
	return &llmCardTypeRepo{write: p.Write, read: p.Read}
}

func (r *llmCardTypeRepo) Create(ctx context.Context, ct *cardstore.CardType) error {
	_, err := r.write.ExecContext(ctx,
		`INSERT INTO llm_card_types (id, service_group_id, label, description, credits, period, price_rmb, template, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ct.ID, ct.ServiceGroupID, ct.Label, ct.Description, ct.Credits, ct.Period, ct.PriceRMB, ct.Template, boolToInt(ct.Enabled), ct.CreatedAt.Format(time.RFC3339), ct.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *llmCardTypeRepo) Update(ctx context.Context, ct *cardstore.CardType) error {
	_, err := r.write.ExecContext(ctx,
		`UPDATE llm_card_types SET service_group_id=?, label=?, description=?, credits=?, period=?, price_rmb=?, template=?, enabled=?, updated_at=? WHERE id=?`,
		ct.ServiceGroupID, ct.Label, ct.Description, ct.Credits, ct.Period, ct.PriceRMB, ct.Template, boolToInt(ct.Enabled), ct.UpdatedAt.Format(time.RFC3339), ct.ID,
	)
	return err
}

func (r *llmCardTypeRepo) GetByID(ctx context.Context, id string) (*cardstore.CardType, error) {
	row := r.read.QueryRowContext(ctx, `SELECT id, service_group_id, label, description, credits, period, price_rmb, template, enabled, created_at, updated_at FROM llm_card_types WHERE id=?`, id)
	ct := &cardstore.CardType{}
	var enabled int
	var createdAt, updatedAt string
	if err := row.Scan(&ct.ID, &ct.ServiceGroupID, &ct.Label, &ct.Description, &ct.Credits, &ct.Period, &ct.PriceRMB, &ct.Template, &enabled, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ct.Enabled = enabled == 1
	ct.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	ct.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return ct, nil
}

func (r *llmCardTypeRepo) ListEnabled(ctx context.Context) ([]*cardstore.CardType, error) {
	return r.listWhere(ctx, "WHERE enabled=1")
}

func (r *llmCardTypeRepo) ListAll(ctx context.Context) ([]*cardstore.CardType, error) {
	return r.listWhere(ctx, "")
}

func (r *llmCardTypeRepo) Delete(ctx context.Context, id string) error {
	_, err := r.write.ExecContext(ctx, `DELETE FROM llm_card_types WHERE id=?`, id)
	return err
}

func (r *llmCardTypeRepo) listWhere(ctx context.Context, where string) ([]*cardstore.CardType, error) {
	rows, err := r.read.QueryContext(ctx, `SELECT id, service_group_id, label, description, credits, period, price_rmb, template, enabled, created_at, updated_at FROM llm_card_types `+where+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var types []*cardstore.CardType
	for rows.Next() {
		ct := &cardstore.CardType{}
		var enabled int
		var createdAt, updatedAt string
		if err := rows.Scan(&ct.ID, &ct.ServiceGroupID, &ct.Label, &ct.Description, &ct.Credits, &ct.Period, &ct.PriceRMB, &ct.Template, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		ct.Enabled = enabled == 1
		ct.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		ct.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		types = append(types, ct)
	}
	return types, nil
}

// ---------------------------------------------------------------------------
// LLMBindingRepository implementation
// ---------------------------------------------------------------------------

type llmBindingRepo struct {
	write *sql.DB
	read  *sql.DB
}

func NewLLMBindingRepo(p *Provider) *llmBindingRepo {
	return &llmBindingRepo{write: p.Write, read: p.Read}
}

func (r *llmBindingRepo) Upsert(ctx context.Context, b *store.LLMNodeBinding) error {
	_, err := r.write.ExecContext(ctx,
		`INSERT OR REPLACE INTO llm_node_bindings (hub_id, tenant_id, node_id, bound_at, last_active, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		b.HubID, b.TenantID, b.NodeID, b.BoundAt.Format(time.RFC3339), b.LastActive.Format(time.RFC3339), b.ExpiresAt.Format(time.RFC3339),
	)
	return err
}

func (r *llmBindingRepo) Get(ctx context.Context, hubID, tenantID string) (*store.LLMNodeBinding, error) {
	row := r.read.QueryRowContext(ctx, `SELECT hub_id, tenant_id, node_id, bound_at, last_active, expires_at FROM llm_node_bindings WHERE hub_id=? AND tenant_id=?`, hubID, tenantID)
	b := &store.LLMNodeBinding{}
	var boundAt, lastActive, expiresAt string
	if err := row.Scan(&b.HubID, &b.TenantID, &b.NodeID, &boundAt, &lastActive, &expiresAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	b.BoundAt, _ = time.Parse(time.RFC3339, boundAt)
	b.LastActive, _ = time.Parse(time.RFC3339, lastActive)
	b.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	return b, nil
}

func (r *llmBindingRepo) Delete(ctx context.Context, hubID, tenantID string) error {
	_, err := r.write.ExecContext(ctx, `DELETE FROM llm_node_bindings WHERE hub_id=? AND tenant_id=?`, hubID, tenantID)
	return err
}

func (r *llmBindingRepo) ListByNode(ctx context.Context, nodeID string) ([]*store.LLMNodeBinding, error) {
	rows, err := r.read.QueryContext(ctx, `SELECT hub_id, tenant_id, node_id, bound_at, last_active, expires_at FROM llm_node_bindings WHERE node_id=?`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBindingRows(rows)
}

func (r *llmBindingRepo) ListAll(ctx context.Context) ([]*store.LLMNodeBinding, error) {
	rows, err := r.read.QueryContext(ctx, `SELECT hub_id, tenant_id, node_id, bound_at, last_active, expires_at FROM llm_node_bindings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBindingRows(rows)
}

func (r *llmBindingRepo) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	res, err := r.write.ExecContext(ctx, `DELETE FROM llm_node_bindings WHERE expires_at < ?`, now.Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------------------
// scan helpers
// ---------------------------------------------------------------------------

func scanAuth(row *sql.Row) (*llmservice.TenantAuthorization, error) {
	a := &llmservice.TenantAuthorization{}
	var allowExt int
	var startsAt, expiresAt, boundAt, createdAt, updatedAt string
	if err := row.Scan(&a.ID, &a.HubID, &a.TenantID, &a.AdminEmail, &a.ServiceGroupID,
		&a.CreditsTotal, &a.CreditsUsed, &startsAt, &expiresAt,
		&allowExt, &a.Source, &a.CardOrderID, &a.BoundNodeID, &boundAt, &a.Status, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	a.AllowExternalProviders = allowExt == 1
	a.StartsAt, _ = time.Parse(time.RFC3339, startsAt)
	a.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	a.BoundAt, _ = time.Parse(time.RFC3339, boundAt)
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return a, nil
}

func scanAuthRows(rows *sql.Rows) ([]*llmservice.TenantAuthorization, error) {
	var results []*llmservice.TenantAuthorization
	for rows.Next() {
		a := &llmservice.TenantAuthorization{}
		var allowExt int
		var startsAt, expiresAt, boundAt, createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.HubID, &a.TenantID, &a.AdminEmail, &a.ServiceGroupID,
			&a.CreditsTotal, &a.CreditsUsed, &startsAt, &expiresAt,
			&allowExt, &a.Source, &a.CardOrderID, &a.BoundNodeID, &boundAt, &a.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		a.AllowExternalProviders = allowExt == 1
		a.StartsAt, _ = time.Parse(time.RFC3339, startsAt)
		a.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
		a.BoundAt, _ = time.Parse(time.RFC3339, boundAt)
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		results = append(results, a)
	}
	return results, nil
}

func scanBindingRows(rows *sql.Rows) ([]*store.LLMNodeBinding, error) {
	var results []*store.LLMNodeBinding
	for rows.Next() {
		b := &store.LLMNodeBinding{}
		var boundAt, lastActive, expiresAt string
		if err := rows.Scan(&b.HubID, &b.TenantID, &b.NodeID, &boundAt, &lastActive, &expiresAt); err != nil {
			return nil, err
		}
		b.BoundAt, _ = time.Parse(time.RFC3339, boundAt)
		b.LastActive, _ = time.Parse(time.RFC3339, lastActive)
		b.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
		results = append(results, b)
	}
	return results, nil
}

func formatTimeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
