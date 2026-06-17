package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	hcCardstore "github.com/RapidAI/CodeClaw/hubcenter/internal/cardstore"
)

type llmOrderRepo struct {
	write *sql.DB
	read  *sql.DB
}

const llmOrderSelectColumns = "order_no, card_type_id, admin_email, hub_id, tenant_id, service_group_id, agent_id, agent_name, credits, period, amount, payment_mode, status, pay_channel, pay_qr_url, pay_deep_link, pay_instruction, pay_url, payment_id, payment_msg, reviewed_by, reviewed_at, paid_at, archived_at, created_at, updated_at"

// NewLLMOrderRepo creates a PurchaseOrderRepository backed by SQLite.
func NewLLMOrderRepo(p *Provider) *llmOrderRepo {
	return &llmOrderRepo{write: p.Write, read: p.Read}
}

func (r *llmOrderRepo) Create(ctx context.Context, order *hcCardstore.PurchaseOrder) error {
	_, err := r.write.ExecContext(ctx,
		`INSERT INTO llm_card_orders (id, order_no, card_type_id, admin_email, hub_id, tenant_id, service_group_id, agent_id, agent_name, credits, period, amount, payment_mode, status, pay_channel, pay_qr_url, pay_deep_link, pay_instruction, pay_url, payment_id, payment_msg, reviewed_by, reviewed_at, paid_at, archived_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		order.OrderNo, order.OrderNo, order.CardTypeID, order.Email, order.HubID, order.TenantID,
		order.ServiceGroupID, order.AgentID, order.AgentName, order.Credits, order.Period, order.Amount,
		order.PaymentMode, order.Status, order.PayChannel, order.PayQRURL, order.PayDeepLink, order.PayInstruction, order.PayURL,
		order.PaymentID, order.PaymentMsg, order.ReviewedBy,
		formatTimeOrEmpty(order.ReviewedAt), formatTimeOrEmpty(order.PaidAt), order.ArchivedAt,
		order.CreatedAt.Format(time.RFC3339), order.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *llmOrderRepo) GetByOrderNo(ctx context.Context, orderNo string) (*hcCardstore.PurchaseOrder, error) {
	row := r.read.QueryRowContext(ctx,
		`SELECT `+llmOrderSelectColumns+` FROM llm_card_orders WHERE order_no = ?`, orderNo)
	return scanOrder(row)
}

func (r *llmOrderRepo) List(ctx context.Context, filter hcCardstore.OrderFilter) ([]*hcCardstore.PurchaseOrder, int, error) {
	where := "WHERE 1=1"
	var args []any
	if filter.HubID != "" {
		where += " AND hub_id = ?"
		args = append(args, filter.HubID)
	}
	if filter.TenantID != "" {
		tenantID := strings.TrimSpace(filter.TenantID)
		if tenantID == "default" || tenantID == "tenant_default" {
			where += " AND (tenant_id = ? OR tenant_id = ? OR tenant_id = '')"
			args = append(args, tenantID, alternateDefaultTenantID(tenantID))
		} else {
			where += " AND tenant_id = ?"
			args = append(args, tenantID)
		}
	}
	if filter.Email != "" {
		where += " AND admin_email = ?"
		args = append(args, filter.Email)
	}
	if filter.ServiceGroupID != "" {
		where += " AND service_group_id = ?"
		args = append(args, filter.ServiceGroupID)
	}
	statuses := normalizeOrderStatuses(filter.Statuses)
	if len(statuses) == 0 && strings.TrimSpace(filter.Status) != "" {
		statuses = normalizeOrderStatuses([]string{filter.Status})
	}
	if len(statuses) == 1 {
		where += " AND status = ?"
		args = append(args, statuses[0])
	} else if len(statuses) > 1 {
		placeholders := make([]string, len(statuses))
		for i, status := range statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}
		where += " AND status IN (" + strings.Join(placeholders, ",") + ")"
	}
	if filter.ArchivedOnly {
		where += " AND archived_at <> ''"
	} else if !filter.IncludeArchived {
		where += " AND archived_at = ''"
	}

	// Count
	var total int
	countQuery := "SELECT COUNT(*) FROM llm_card_orders " + where
	if err := r.read.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Query
	query := "SELECT " + llmOrderSelectColumns + " FROM llm_card_orders " + where + " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := r.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var orders []*hcCardstore.PurchaseOrder
	for rows.Next() {
		o, err := scanOrderFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, o)
	}
	return orders, total, nil
}

func alternateDefaultTenantID(tenantID string) string {
	if tenantID == "default" {
		return "tenant_default"
	}
	return "default"
}

func normalizeOrderStatuses(values []string) []string {
	seen := make(map[string]bool)
	var statuses []string
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			status := strings.TrimSpace(part)
			if status == "" || seen[status] {
				continue
			}
			seen[status] = true
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func (r *llmOrderRepo) UpdateStatus(ctx context.Context, orderNo, status string, now time.Time) error {
	_, err := r.write.ExecContext(ctx,
		`UPDATE llm_card_orders SET status = ?, updated_at = ? WHERE order_no = ?`,
		status, now.Format(time.RFC3339), orderNo)
	return err
}

func (r *llmOrderRepo) Update(ctx context.Context, order *hcCardstore.PurchaseOrder) error {
	_, err := r.write.ExecContext(ctx,
		`UPDATE llm_card_orders SET status=?, payment_mode=?, pay_channel=?, pay_qr_url=?, pay_deep_link=?, pay_instruction=?, pay_url=?, payment_id=?, payment_msg=?, reviewed_by=?, reviewed_at=?, paid_at=?, updated_at=? WHERE order_no=?`,
		order.Status, order.PaymentMode, order.PayChannel, order.PayQRURL, order.PayDeepLink, order.PayInstruction, order.PayURL,
		order.PaymentID, order.PaymentMsg, order.ReviewedBy,
		formatTimeOrEmpty(order.ReviewedAt), formatTimeOrEmpty(order.PaidAt),
		order.UpdatedAt.Format(time.RFC3339), order.OrderNo,
	)
	return err
}

func (r *llmOrderRepo) Delete(ctx context.Context, orderNo string) error {
	_, err := r.write.ExecContext(ctx, `DELETE FROM llm_card_orders WHERE order_no = ?`, orderNo)
	return err
}

func (r *llmOrderRepo) Archive(ctx context.Context, orderNo string, archivedAt time.Time) error {
	_, err := r.write.ExecContext(ctx,
		`UPDATE llm_card_orders SET archived_at = ?, updated_at = ? WHERE order_no = ?`,
		archivedAt.Format(time.RFC3339), archivedAt.Format(time.RFC3339), orderNo)
	return err
}

// ---------------------------------------------------------------------------
// scan helpers
// ---------------------------------------------------------------------------

func scanOrder(row *sql.Row) (*hcCardstore.PurchaseOrder, error) {
	o := &hcCardstore.PurchaseOrder{}
	var reviewedAt, paidAt, createdAt, updatedAt string
	if err := row.Scan(
		&o.OrderNo, &o.CardTypeID, &o.Order.Email, &o.HubID, &o.TenantID,
		&o.ServiceGroupID, &o.AgentID, &o.AgentName, &o.Credits, &o.Period, &o.Order.Amount,
		&o.Order.PaymentMode, &o.Order.Status, &o.Order.PayChannel, &o.Order.PayQRURL, &o.Order.PayDeepLink, &o.Order.PayInstruction, &o.Order.PayURL,
		&o.Order.PaymentID, &o.Order.PaymentMsg, &o.Order.ReviewedBy,
		&reviewedAt, &paidAt, &o.ArchivedAt, &createdAt, &updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	o.Order.OrderNo = o.OrderNo
	o.Order.ProductID = o.CardTypeID
	o.Order.ReviewedAt, _ = time.Parse(time.RFC3339, reviewedAt)
	o.Order.PaidAt, _ = time.Parse(time.RFC3339, paidAt)
	o.Order.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	o.Order.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return o, nil
}

func scanOrderFromRows(rows *sql.Rows) (*hcCardstore.PurchaseOrder, error) {
	o := &hcCardstore.PurchaseOrder{}
	var reviewedAt, paidAt, createdAt, updatedAt string
	if err := rows.Scan(
		&o.OrderNo, &o.CardTypeID, &o.Order.Email, &o.HubID, &o.TenantID,
		&o.ServiceGroupID, &o.AgentID, &o.AgentName, &o.Credits, &o.Period, &o.Order.Amount,
		&o.Order.PaymentMode, &o.Order.Status, &o.Order.PayChannel, &o.Order.PayQRURL, &o.Order.PayDeepLink, &o.Order.PayInstruction, &o.Order.PayURL,
		&o.Order.PaymentID, &o.Order.PaymentMsg, &o.Order.ReviewedBy,
		&reviewedAt, &paidAt, &o.ArchivedAt, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	o.Order.OrderNo = o.OrderNo
	o.Order.ProductID = o.CardTypeID
	o.Order.ProductLabel = "" // filled by caller if needed
	o.Order.ReviewedAt, _ = time.Parse(time.RFC3339, reviewedAt)
	o.Order.PaidAt, _ = time.Parse(time.RFC3339, paidAt)
	o.Order.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	o.Order.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return o, nil
}

// Ensure this repo satisfies the interface at compile time.
var _ hcCardstore.PurchaseOrderRepository = (*llmOrderRepo)(nil)
