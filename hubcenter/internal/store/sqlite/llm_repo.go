package sqlite

import (
	"context"
	"database/sql"
	"fmt"
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
			model TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			credits_deducted REAL NOT NULL DEFAULT 0,
			cache_hit INTEGER NOT NULL DEFAULT 0,
			auth_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_llm_usage_hub_tenant_time ON llm_usage_records(hub_id, tenant_id, created_at)`,

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
		time.Now().UTC().Format(time.RFC3339), auth.ID,
	)
	return err
}

func (r *llmAuthRepo) DeductCredits(ctx context.Context, id string, credits float64, now time.Time) error {
	// Only deduct if there's remaining balance (prevents snowball over-deduction)
	result, err := r.write.ExecContext(ctx,
		`UPDATE llm_tenant_authorizations SET credits_used = credits_used + ?, updated_at = ? WHERE id = ? AND (credits_total - credits_used) >= ?`,
		credits, now.Format(time.RFC3339), id, credits,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		// Insufficient balance - deduct anyway but mark as exhausted
		_, err = r.write.ExecContext(ctx,
			`UPDATE llm_tenant_authorizations SET credits_used = credits_used + ?, status = 'exhausted', updated_at = ? WHERE id = ?`,
			credits, now.Format(time.RFC3339), id,
		)
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// UsageRepository implementation
// ---------------------------------------------------------------------------

type llmUsageRepo struct {
	write *sql.DB
	read  *sql.DB
}

func NewLLMUsageRepo(p *Provider) *llmUsageRepo {
	return &llmUsageRepo{write: p.Write, read: p.Read}
}

func (r *llmUsageRepo) Insert(ctx context.Context, record *llmservice.TenantUsageRecord) error {
	_, err := r.write.ExecContext(ctx,
		`INSERT INTO llm_usage_records (hub_id, tenant_id, model, provider_id, input_tokens, output_tokens, credits_deducted, cache_hit, auth_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.HubID, record.TenantID, record.Model, record.ProviderID,
		record.InputTokens, record.OutputTokens, record.Credits,
		boolToInt(record.CacheHit), record.AuthID, record.CreatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *llmUsageRepo) QuerySummary(ctx context.Context, filter llmservice.UsageFilter) ([]llmservice.TenantUsageSummary, error) {
	// Determine grouping expression based on period
	periodExpr := "DATE(created_at)" // default: daily
	periodLabel := "daily"
	switch filter.Period {
	case "weekly":
		periodExpr = "DATE(created_at, 'weekday 0', '-6 days')" // SQLite: start of week (Monday)
		periodLabel = "weekly"
	case "monthly":
		periodExpr = "SUBSTR(created_at, 1, 7)" // "2026-01"
		periodLabel = "monthly"
	default:
		periodLabel = "daily"
	}

	query := `SELECT hub_id, tenant_id, ` + periodExpr + ` as period_start, SUM(input_tokens), SUM(output_tokens), SUM(credits_deducted), COUNT(*), SUM(CASE WHEN cache_hit=1 THEN 1 ELSE 0 END) FROM llm_usage_records WHERE 1=1`
	var args []any
	if filter.HubID != "" {
		query += ` AND hub_id = ?`
		args = append(args, filter.HubID)
	}
	if filter.TenantID != "" {
		query += ` AND tenant_id = ?`
		args = append(args, filter.TenantID)
	}
	if filter.StartDate != "" {
		query += ` AND created_at >= ?`
		args = append(args, filter.StartDate)
	}
	if filter.EndDate != "" {
		query += ` AND created_at <= ?`
		args = append(args, filter.EndDate+"T23:59:59Z")
	}
	query += ` GROUP BY hub_id, tenant_id, ` + periodExpr + ` ORDER BY period_start DESC`
	if filter.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, filter.Limit)
	}

	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []llmservice.TenantUsageSummary
	for rows.Next() {
		var s llmservice.TenantUsageSummary
		var cacheHits int64
		if err := rows.Scan(&s.HubID, &s.TenantID, &s.PeriodStart, &s.InputTokens, &s.OutputTokens, &s.TotalCredits, &s.TotalRequests, &cacheHits); err != nil {
			return nil, err
		}
		s.Period = periodLabel
		s.CacheHits = cacheHits
		if s.TotalRequests > 0 {
			s.CacheHitRate = float64(cacheHits) / float64(s.TotalRequests)
		}
		results = append(results, s)
	}
	return results, nil
}

func (r *llmUsageRepo) QueryRecent(ctx context.Context, hubID, tenantID string, limit int) ([]*llmservice.TenantUsageRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.read.QueryContext(ctx, `SELECT id, hub_id, tenant_id, model, provider_id, input_tokens, output_tokens, credits_deducted, cache_hit, auth_id, created_at FROM llm_usage_records WHERE hub_id=? AND tenant_id=? ORDER BY created_at DESC LIMIT ?`, hubID, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []*llmservice.TenantUsageRecord
	for rows.Next() {
		var rec llmservice.TenantUsageRecord
		var cacheHit int
		var createdAt string
		if err := rows.Scan(&rec.ID, &rec.HubID, &rec.TenantID, &rec.Model, &rec.ProviderID, &rec.InputTokens, &rec.OutputTokens, &rec.Credits, &cacheHit, &rec.AuthID, &createdAt); err != nil {
			return nil, err
		}
		rec.CacheHit = cacheHit == 1
		rec.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		records = append(records, &rec)
	}
	return records, nil
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
