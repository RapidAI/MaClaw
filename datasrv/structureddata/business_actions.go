package structureddata

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var businessActions = []BusinessAction{
	{
		ID:             "company.department_upsert",
		Domain:         "company",
		Title:          "Create or update department",
		Description:    "Use when company organization, department hierarchy, manager, cost center, or department status changes.",
		DatasetID:      "company.departments",
		EventType:      "company.department.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"department_code", "name"},
		SuggestedTags:  []string{"company", "department", "organization"},
		InputFields:    templateFields("company.departments"),
	},
	{
		ID:             "sales.order_upsert",
		Domain:         "sales",
		Title:          "Create or update sales order",
		Description:    "Use when a CRM or sales workflow creates or changes an order.",
		DatasetID:      "sales.orders",
		EventType:      "sales.order.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"order_no", "customer", "amount"},
		SuggestedTags:  []string{"sales", "crm"},
		InputFields:    templateFields("sales.orders"),
	},
	{
		ID:             "sales.order_status_update",
		Domain:         "sales",
		Title:          "Update sales order status",
		Description:    "Use for incremental sales order workflow changes such as stage, payment status, owner, or delivery status without replacing the whole order.",
		DatasetID:      "sales.orders",
		EventType:      "sales.order.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"stage"},
		SuggestedTags:  []string{"sales", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "stage", Type: "string", Title: "Stage", Required: true, Config: enumFieldConfig("draft", "confirmed", "won", "lost", "cancelled", "fulfilled")},
			{Key: "payment_status", Type: "string", Title: "Payment Status", Config: enumFieldConfig("unpaid", "partial", "paid", "refunded", "overdue")},
			{Key: "owner", Type: "string", Title: "Owner"},
			{Key: "delivery_status", Type: "string", Title: "Delivery Status"},
		},
	},
	{
		ID:             "sales.customer_upsert",
		Domain:         "sales",
		Title:          "Create or update customer",
		Description:    "Use when customer master data changes.",
		DatasetID:      "sales.customers",
		EventType:      "sales.customer.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"customer_no", "name"},
		SuggestedTags:  []string{"sales", "customer"},
		InputFields:    templateFields("sales.customers"),
	},
	{
		ID:             "sales.contact_upsert",
		Domain:         "sales",
		Title:          "Create or update sales contact",
		Description:    "Use when customer contact person, role, phone, email, owner, or lifecycle status changes.",
		DatasetID:      "sales.contacts",
		EventType:      "sales.contact.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"contact_no", "name"},
		SuggestedTags:  []string{"sales", "customer", "contact"},
		InputFields:    templateFields("sales.contacts"),
	},
	{
		ID:             "sales.opportunity_upsert",
		Domain:         "sales",
		Title:          "Create or update sales opportunity",
		Description:    "Use when a lead, opportunity, expected amount, probability, owner, stage, or close date changes.",
		DatasetID:      "sales.opportunities",
		EventType:      "sales.opportunity.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"opportunity_no", "name"},
		SuggestedTags:  []string{"sales", "opportunity", "pipeline"},
		InputFields:    templateFields("sales.opportunities"),
	},
	{
		ID:             "sales.opportunity_stage_update",
		Domain:         "sales",
		Title:          "Update sales opportunity stage",
		Description:    "Use for lead qualification, proposal, negotiation, win, loss, or cancellation changes without replacing the opportunity.",
		DatasetID:      "sales.opportunities",
		EventType:      "sales.opportunity.stage_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"stage"},
		SuggestedTags:  []string{"sales", "opportunity", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "stage", Type: "string", Title: "Stage", Required: true, Config: enumFieldConfig("lead", "qualified", "proposal", "negotiation", "won", "lost", "cancelled")},
			{Key: "probability", Type: "number", Title: "Probability"},
			{Key: "expected_close_date", Type: "date", Title: "Expected Close Date"},
			{Key: "reason", Type: "string", Title: "Reason"},
		},
	},
	{
		ID:             "finance.expense_submit",
		Domain:         "finance",
		Title:          "Submit expense",
		Description:    "Use when an employee submits or updates an expense reimbursement.",
		DatasetID:      "finance.expenses",
		EventType:      "finance.expense.submitted",
		Operation:      "upsert_record",
		RequiredFields: []string{"expense_no", "applicant", "amount"},
		SuggestedTags:  []string{"finance", "expense"},
		InputFields:    templateFields("finance.expenses"),
	},
	{
		ID:             "finance.expense_status_update",
		Domain:         "finance",
		Title:          "Update expense status",
		Description:    "Use for reimbursement review, approval, rejection, payment, or settlement status changes without replacing the original expense.",
		DatasetID:      "finance.expenses",
		EventType:      "finance.expense.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"finance", "approval"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("submitted", "approved", "rejected", "paid", "cancelled")},
			{Key: "approved_by", Type: "string", Title: "Approved By"},
			{Key: "approved_at", Type: "datetime", Title: "Approved At"},
			{Key: "rejected_reason", Type: "string", Title: "Rejected Reason"},
			{Key: "paid_at", Type: "datetime", Title: "Paid At"},
		},
	},
	{
		ID:             "finance.invoice_upsert",
		Domain:         "finance",
		Title:          "Create or update invoice",
		Description:    "Use when an invoice is issued, received, or status changes.",
		DatasetID:      "finance.invoices",
		EventType:      "finance.invoice.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"invoice_no", "counterparty", "amount"},
		SuggestedTags:  []string{"finance", "invoice"},
		InputFields:    templateFields("finance.invoices"),
	},
	{
		ID:             "finance.invoice_status_update",
		Domain:         "finance",
		Title:          "Update invoice status",
		Description:    "Use for invoice lifecycle changes such as issued, received, verified, paid, voided, or overdue without replacing invoice master fields.",
		DatasetID:      "finance.invoices",
		EventType:      "finance.invoice.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"finance", "invoice"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("draft", "issued", "received", "verified", "paid", "voided", "overdue")},
			{Key: "verified_by", Type: "string", Title: "Verified By"},
			{Key: "verified_at", Type: "datetime", Title: "Verified At"},
			{Key: "paid_at", Type: "datetime", Title: "Paid At"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "finance.payment_upsert",
		Domain:         "finance",
		Title:          "Create or update payment",
		Description:    "Use when accounts payable, receivable, refund, or transfer payment records are prepared or changed.",
		DatasetID:      "finance.payments",
		EventType:      "finance.payment.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"payment_no", "counterparty", "payment_type", "amount"},
		SuggestedTags:  []string{"finance", "payment"},
		InputFields:    templateFields("finance.payments"),
	},
	{
		ID:             "finance.payment_status_update",
		Domain:         "finance",
		Title:          "Update payment status",
		Description:    "Use for payment approval, paid, received, failure, or cancellation changes without replacing payment master fields.",
		DatasetID:      "finance.payments",
		EventType:      "finance.payment.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"finance", "payment", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("planned", "approved", "paid", "received", "failed", "cancelled")},
			{Key: "approved_by", Type: "string", Title: "Approved By"},
			{Key: "approved_at", Type: "datetime", Title: "Approved At"},
			{Key: "paid_at", Type: "datetime", Title: "Paid At"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "finance.account_upsert",
		Domain:         "finance",
		Title:          "Create or update account",
		Description:    "Use when finance maintains a lightweight chart of accounts for voucher classification.",
		DatasetID:      "finance.accounts",
		EventType:      "finance.account.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"account_code", "account_name", "account_type"},
		SuggestedTags:  []string{"finance", "accounting", "chart_of_accounts"},
		InputFields:    templateFields("finance.accounts"),
	},
	{
		ID:             "finance.budget_upsert",
		Domain:         "finance",
		Title:          "Create or update budget",
		Description:    "Use when finance plans or adjusts department/category budget amount, committed amount, actual amount, owner, period, or status.",
		DatasetID:      "finance.budgets",
		EventType:      "finance.budget.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"budget_no", "period", "department", "category", "budget_amount"},
		SuggestedTags:  []string{"finance", "budget"},
		InputFields:    templateFields("finance.budgets"),
	},
	{
		ID:             "finance.budget_status_update",
		Domain:         "finance",
		Title:          "Update budget status",
		Description:    "Use for budget submission, approval, activation, freeze, closure, or cancellation without replacing the whole budget.",
		DatasetID:      "finance.budgets",
		EventType:      "finance.budget.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"finance", "budget", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("draft", "submitted", "approved", "active", "frozen", "closed", "cancelled")},
			{Key: "approved_by", Type: "string", Title: "Approved By"},
			{Key: "approved_at", Type: "datetime", Title: "Approved At"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "finance.voucher_upsert",
		Domain:         "finance",
		Title:          "Create or update accounting voucher",
		Description:    "Use when finance prepares, adjusts, approves, posts, reverses, or voids a lightweight accounting voucher.",
		DatasetID:      "finance.vouchers",
		EventType:      "finance.voucher.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"voucher_no", "period", "voucher_type", "debit_total", "credit_total"},
		SuggestedTags:  []string{"finance", "accounting", "voucher"},
		InputFields:    templateFields("finance.vouchers"),
	},
	{
		ID:             "finance.voucher_status_update",
		Domain:         "finance",
		Title:          "Update accounting voucher status",
		Description:    "Use for voucher review, approval, posting, reversal, or voiding without replacing voucher lines.",
		DatasetID:      "finance.vouchers",
		EventType:      "finance.voucher.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"finance", "accounting", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("draft", "reviewing", "approved", "posted", "reversed", "voided")},
			{Key: "reviewed_by", Type: "string", Title: "Reviewed By"},
			{Key: "posted_at", Type: "datetime", Title: "Posted At"},
			{Key: "reversal_reason", Type: "string", Title: "Reversal Reason"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "hr.employee_upsert",
		Domain:         "hr",
		Title:          "Create or update employee",
		Description:    "Use for onboarding, transfer, status, or employee profile changes.",
		DatasetID:      "hr.employees",
		EventType:      "hr.employee.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"employee_no", "name"},
		SuggestedTags:  []string{"hr", "employee"},
		InputFields:    templateFields("hr.employees"),
	},
	{
		ID:             "hr.employee_status_update",
		Domain:         "hr",
		Title:          "Update employee status",
		Description:    "Use for onboarding, probation, transfer, leave, resignation, or termination status changes without replacing the employee profile.",
		DatasetID:      "hr.employees",
		EventType:      "hr.employee.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"employment_status"},
		SuggestedTags:  []string{"hr", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "employment_status", Type: "string", Title: "Employment Status", Required: true, Config: enumFieldConfig("onboarding", "probation", "active", "leave", "transferred", "resigned", "terminated")},
			{Key: "department", Type: "string", Title: "Department"},
			{Key: "position", Type: "string", Title: "Position"},
			{Key: "manager", Type: "string", Title: "Manager"},
			{Key: "effective_date", Type: "date", Title: "Effective Date"},
		},
	},
	{
		ID:             "hr.payroll_upsert",
		Domain:         "hr",
		Title:          "Create or update payroll item",
		Description:    "Use when a monthly employee payroll line is prepared, adjusted, approved, paid, or cancelled.",
		DatasetID:      "hr.payroll",
		EventType:      "hr.payroll.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"payroll_month", "employee_no", "employee_name"},
		SuggestedTags:  []string{"hr", "payroll"},
		InputFields:    templateFields("hr.payroll"),
	},
	{
		ID:             "hr.payroll_status_update",
		Domain:         "hr",
		Title:          "Update payroll status",
		Description:    "Use for payroll lifecycle changes such as draft, active, suspended, paid, or cancelled without replacing the payroll line.",
		DatasetID:      "hr.payroll",
		EventType:      "hr.payroll.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"hr", "payroll", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("draft", "active", "suspended", "paid", "cancelled")},
			{Key: "approved_by", Type: "string", Title: "Approved By"},
			{Key: "approved_at", Type: "datetime", Title: "Approved At"},
			{Key: "paid_at", Type: "datetime", Title: "Paid At"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "hr.leave_request_upsert",
		Domain:         "hr",
		Title:          "Create or update leave request",
		Description:    "Use when an employee submits or updates leave, vacation, sick leave, business trip, remote, or absence records.",
		DatasetID:      "hr.leave_requests",
		EventType:      "hr.leave_request.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"leave_no", "employee_no", "employee_name", "leave_type", "start_date", "end_date", "days"},
		SuggestedTags:  []string{"hr", "leave", "approval"},
		InputFields:    templateFields("hr.leave_requests"),
	},
	{
		ID:             "hr.leave_request_status_update",
		Domain:         "hr",
		Title:          "Update leave request status",
		Description:    "Use for leave approval, rejection, cancellation, or closure without replacing the leave request.",
		DatasetID:      "hr.leave_requests",
		EventType:      "hr.leave_request.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"hr", "leave", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("draft", "submitted", "approved", "rejected", "cancelled", "closed")},
			{Key: "approver", Type: "string", Title: "Approver"},
			{Key: "approved_at", Type: "datetime", Title: "Approved At"},
			{Key: "rejected_reason", Type: "string", Title: "Rejected Reason"},
		},
	},
	{
		ID:             "legal.contract_upsert",
		Domain:         "legal",
		Title:          "Create or update contract",
		Description:    "Use when a contract is drafted, signed, renewed, or status changes.",
		DatasetID:      "legal.contracts",
		EventType:      "legal.contract.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"contract_no", "counterparty"},
		SuggestedTags:  []string{"legal", "contract"},
		InputFields:    templateFields("legal.contracts"),
	},
	{
		ID:             "legal.contract_status_update",
		Domain:         "legal",
		Title:          "Update contract status",
		Description:    "Use for contract review, signing, renewal, fulfillment, expiration, or termination status changes without replacing contract master fields.",
		DatasetID:      "legal.contracts",
		EventType:      "legal.contract.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"legal", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("draft", "reviewing", "signed", "active", "fulfilled", "expired", "terminated")},
			{Key: "reviewed_by", Type: "string", Title: "Reviewed By"},
			{Key: "signed_date", Type: "date", Title: "Signed Date"},
			{Key: "expires_at", Type: "date", Title: "Expires At"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "procurement.purchase_order_upsert",
		Domain:         "procurement",
		Title:          "Create or update purchase order",
		Description:    "Use when a procurement workflow creates or changes a supplier purchase order.",
		DatasetID:      "procurement.purchase_orders",
		EventType:      "procurement.purchase_order.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"po_no", "supplier", "amount"},
		SuggestedTags:  []string{"procurement", "purchase_order"},
		InputFields:    templateFields("procurement.purchase_orders"),
	},
	{
		ID:             "procurement.supplier_upsert",
		Domain:         "procurement",
		Title:          "Create or update supplier",
		Description:    "Use when supplier master data, payment terms, category, contact, tax number, or status changes.",
		DatasetID:      "procurement.suppliers",
		EventType:      "procurement.supplier.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"supplier_no", "name"},
		SuggestedTags:  []string{"procurement", "supplier"},
		InputFields:    templateFields("procurement.suppliers"),
	},
	{
		ID:             "procurement.purchase_order_status_update",
		Domain:         "procurement",
		Title:          "Update purchase order status",
		Description:    "Use for approval, ordering, receiving, cancellation, or closing changes without replacing the whole purchase order.",
		DatasetID:      "procurement.purchase_orders",
		EventType:      "procurement.purchase_order.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"procurement", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("draft", "submitted", "approved", "ordered", "partially_received", "received", "cancelled", "closed")},
			{Key: "approved_by", Type: "string", Title: "Approved By"},
			{Key: "received_at", Type: "datetime", Title: "Received At"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "inventory.item_upsert",
		Domain:         "inventory",
		Title:          "Create or update inventory item",
		Description:    "Use when an item master record or stock level changes.",
		DatasetID:      "inventory.items",
		EventType:      "inventory.item.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"item_no", "name"},
		SuggestedTags:  []string{"inventory", "item"},
		InputFields:    templateFields("inventory.items"),
	},
	{
		ID:             "inventory.warehouse_upsert",
		Domain:         "inventory",
		Title:          "Create or update warehouse",
		Description:    "Use when warehouse master data, location, manager, or lifecycle status changes.",
		DatasetID:      "inventory.warehouses",
		EventType:      "inventory.warehouse.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"warehouse_code", "name"},
		SuggestedTags:  []string{"inventory", "warehouse"},
		InputFields:    templateFields("inventory.warehouses"),
	},
	{
		ID:             "inventory.stock_update",
		Domain:         "inventory",
		Title:          "Update inventory stock",
		Description:    "Use for stock count, reservation, transfer, low stock, or movement updates without replacing item master fields.",
		DatasetID:      "inventory.items",
		EventType:      "inventory.stock.updated",
		Operation:      "merge_record",
		RequiredFields: []string{"quantity"},
		SuggestedTags:  []string{"inventory", "stock"},
		InputFields: []DatasetTemplateField{
			{Key: "quantity", Type: "number", Title: "Quantity", Required: true},
			{Key: "warehouse", Type: "string", Title: "Warehouse"},
			{Key: "status", Type: "string", Title: "Status", Config: enumFieldConfig("active", "low_stock", "reserved", "inactive", "obsolete")},
			{Key: "last_movement_at", Type: "datetime", Title: "Last Movement At"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "inventory.movement_record",
		Domain:         "inventory",
		Title:          "Record inventory movement",
		Description:    "Use for inventory receipts, issues, transfers, adjustments, reservations, and releases with movement-level traceability.",
		DatasetID:      "inventory.movements",
		EventType:      "inventory.movement.recorded",
		Operation:      "upsert_record",
		RequiredFields: []string{"movement_no", "item_no", "movement_type", "quantity"},
		SuggestedTags:  []string{"inventory", "movement"},
		InputFields:    templateFields("inventory.movements"),
	},
	{
		ID:             "inventory.movement_status_update",
		Domain:         "inventory",
		Title:          "Update inventory movement status",
		Description:    "Use for confirming, posting, or cancelling an inventory movement without replacing movement details.",
		DatasetID:      "inventory.movements",
		EventType:      "inventory.movement.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"inventory", "movement", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("draft", "confirmed", "posted", "cancelled")},
			{Key: "posted_by", Type: "string", Title: "Posted By"},
			{Key: "posted_at", Type: "datetime", Title: "Posted At"},
			{Key: "note", Type: "string", Title: "Note"},
		},
	},
	{
		ID:             "assets.fixed_asset_upsert",
		Domain:         "assets",
		Title:          "Create or update fixed asset",
		Description:    "Use when a company fixed asset is acquired, transferred, maintained, or disposed.",
		DatasetID:      "assets.fixed_assets",
		EventType:      "assets.fixed_asset.updated",
		Operation:      "upsert_record",
		RequiredFields: []string{"asset_no", "name"},
		SuggestedTags:  []string{"assets", "fixed_asset"},
		InputFields:    templateFields("assets.fixed_assets"),
	},
	{
		ID:             "assets.fixed_asset_status_update",
		Domain:         "assets",
		Title:          "Update fixed asset status",
		Description:    "Use for asset transfer, maintenance, disposal, loss, or custodian changes without replacing asset master fields.",
		DatasetID:      "assets.fixed_assets",
		EventType:      "assets.fixed_asset.status_updated",
		Operation:      "merge_record",
		RequiredFields: []string{"status"},
		SuggestedTags:  []string{"assets", "workflow"},
		InputFields: []DatasetTemplateField{
			{Key: "status", Type: "string", Title: "Status", Required: true, Config: enumFieldConfig("planned", "in_use", "idle", "maintenance", "transferred", "disposed", "lost")},
			{Key: "department", Type: "string", Title: "Department"},
			{Key: "custodian", Type: "string", Title: "Custodian"},
			{Key: "location", Type: "string", Title: "Location"},
			{Key: "effective_date", Type: "date", Title: "Effective Date"},
		},
	},
}

func (s *Service) ListBusinessActions(ctx context.Context, p Principal, query ...QueryBusinessActionsInput) ([]BusinessAction, error) {
	_ = ctx
	out := append([]BusinessAction(nil), businessActions...)
	if p.Policy != nil {
		out = filterCapabilityActions(p, out)
	}
	if len(query) == 0 {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	return paginateBusinessActions(out, query[0]), nil
}

func paginateBusinessActions(items []BusinessAction, in QueryBusinessActionsInput) []BusinessAction {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]BusinessAction(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, action := range out {
			if action.ID < beforeID {
				filtered = append(filtered, action)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) GetBusinessAction(ctx context.Context, p Principal, actionID string) (*BusinessAction, error) {
	_ = ctx
	_ = p
	id := strings.TrimSpace(actionID)
	for _, action := range businessActions {
		if action.ID == id {
			clone := action
			clone.RequiredFields = append([]string(nil), action.RequiredFields...)
			clone.SuggestedTags = append([]string(nil), action.SuggestedTags...)
			clone.InputFields = append([]DatasetTemplateField(nil), action.InputFields...)
			return &clone, nil
		}
	}
	return nil, ErrDatasetNotFound
}

func (s *Service) ExecuteBusinessAction(ctx context.Context, p Principal, actionID string, in ExecuteBusinessActionInput) (*ExecuteBusinessActionResult, error) {
	action, err := s.GetBusinessAction(ctx, p, actionID)
	if err != nil {
		return nil, err
	}
	if len(in.Data) == 0 {
		return nil, fmt.Errorf("%w: data is required", ErrInvalidInput)
	}
	for _, key := range action.RequiredFields {
		if _, ok := in.Data[key]; !ok {
			return nil, fmt.Errorf("%w: missing required business field %s", ErrInvalidInput, key)
		}
	}
	if _, err := s.GetDataset(ctx, p, action.DatasetID); err != nil {
		if errors.Is(err, ErrDatasetNotFound) {
			if in.DryRun {
				validation := validateBusinessActionTemplateData(*action, in.Data)
				result := &ExecuteBusinessActionResult{Action: *action, DryRun: true, Valid: validation.Valid, Validation: validation, Preview: cloneJSONMap(in.Data), Rules: s.businessActionRuleEvaluation(ctx, p, *action, in)}
				applyBusinessActionResultPackage(result, *action, in)
				return result, nil
			}
			if _, createErr := s.CreateDatasetFromTemplate(ctx, p, action.DatasetID, CreateFromTemplateInput{}); createErr != nil {
				return nil, createErr
			}
		} else {
			return nil, err
		}
	}
	tags := normalizeTags(append(append([]string{}, action.SuggestedTags...), in.Tags...))
	if in.DryRun {
		return s.dryRunBusinessAction(ctx, p, *action, in)
	}
	if errors := businessActionDataErrors(action.ID, in.Data); len(errors) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidInput, errors[0])
	}
	event, err := s.ingestRawEvent(ctx, p, DataEventInput{Source: "maclaw_business_action", EventType: action.EventType, Operation: action.Operation, DatasetID: action.DatasetID, RecordID: in.RecordID, IdempotencyKey: in.IdempotencyKey, Title: in.Title, Tags: tags, Data: in.Data, OccurredAt: in.OccurredAt}, action.ID)
	if err != nil {
		return nil, err
	}
	result := &ExecuteBusinessActionResult{Action: *action, Valid: true, Event: event, Rules: s.businessActionRuleEvaluation(ctx, p, *action, in)}
	applyBusinessActionResultPackage(result, *action, in)
	return result, nil
}

func (s *Service) dryRunBusinessAction(ctx context.Context, p Principal, action BusinessAction, in ExecuteBusinessActionInput) (*ExecuteBusinessActionResult, error) {
	datasetID := strings.TrimSpace(action.DatasetID)
	fields, err := s.store.ListFields(ctx, p.TenantID, datasetID)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		fields = businessActionFieldDefinitions(action)
	}
	recordID := strings.TrimSpace(in.RecordID)
	preview := cloneJSONMap(in.Data)
	if existing, err := s.store.GetRecord(ctx, p.TenantID, datasetID, recordID); err == nil && isMergeOperation(action.Operation) {
		preview = cloneJSONMap(existing.Data)
		for key, value := range in.Data {
			preview[key] = value
		}
	} else if err != nil && !errors.Is(err, ErrRecordNotFound) {
		return nil, err
	}
	validation := validateRecordDataResult(datasetID, fields, preview)
	if uniqueErrors, err := s.uniqueConstraintErrors(ctx, p, datasetID, fields, preview, recordID); err != nil {
		return nil, err
	} else if len(uniqueErrors) > 0 {
		validation.Valid = false
		validation.Errors = append(validation.Errors, uniqueErrors...)
	}
	appendBusinessActionDataErrors(&validation, action.ID, preview)
	result := &ExecuteBusinessActionResult{Action: action, DryRun: true, Valid: validation.Valid, Validation: &validation, Preview: preview, Rules: s.businessActionRuleEvaluation(ctx, p, action, in)}
	applyBusinessActionResultPackage(result, action, in)
	return result, nil
}

func applyBusinessActionResultPackage(result *ExecuteBusinessActionResult, action BusinessAction, in ExecuteBusinessActionInput) {
	if result == nil {
		return
	}
	result.PrimaryResult = "business_record"
	result.Artifacts = []map[string]any{}
	recordID := strings.TrimSpace(in.RecordID)
	businessRecord := map[string]any{}
	if result.Event != nil && result.Event.Record != nil {
		if recordID == "" {
			recordID = strings.TrimSpace(result.Event.Record.ID)
		}
		businessRecord = cloneJSONMap(result.Event.Record.Data)
		businessRecord["id"] = result.Event.Record.ID
		businessRecord["dataset_id"] = result.Event.Record.DatasetID
		if result.Event.Record.Title != "" {
			businessRecord["title"] = result.Event.Record.Title
		}
	} else {
		businessRecord = cloneJSONMap(result.Preview)
	}
	if recordID != "" {
		businessRecord["id"] = recordID
	}
	status := "completed"
	if result.Event != nil && strings.TrimSpace(result.Event.Status) != "" {
		status = strings.TrimSpace(result.Event.Status)
	}
	if result.DryRun {
		status = "preview"
	}
	businessStatus := status
	if result.DryRun {
		if result.Valid {
			businessStatus = "dry_run_valid"
		} else {
			businessStatus = "dry_run_invalid"
		}
	}
	result.BusinessStatus = businessStatus
	result.ResultStatus = status
	result.ResultPayload = map[string]any{
		"business_status":    businessStatus,
		"result_status":      status,
		"business_action_id": action.ID,
		"dataset_id":         action.DatasetID,
		"domain":             action.Domain,
		"operation":          action.Operation,
		"business_record":    businessRecord,
	}
	if recordID != "" {
		result.ResultPayload["record_id"] = recordID
	}
	if result.DryRun {
		result.ResultPayload["dry_run"] = true
		result.ResultPayload["valid"] = result.Valid
	}
	title := action.Title
	if title == "" {
		title = action.ID
	}
	output := map[string]any{
		"kind":   "business_record",
		"title":  title,
		"status": status,
		"data":   cloneJSONMap(businessRecord),
	}
	if result.DryRun {
		output["text"] = "Business action preview"
	} else {
		output["text"] = "Business action executed"
	}
	result.Outputs = []map[string]any{output}
}

func (s *Service) businessActionRuleEvaluation(ctx context.Context, p Principal, action BusinessAction, in ExecuteBusinessActionInput) *BusinessRuleEvaluation {
	out, err := s.EvaluateBusinessRules(ctx, p, EvaluateBusinessRulesInput{
		Domain:         action.Domain,
		DatasetID:      action.DatasetID,
		BusinessAction: action.ID,
		RecordID:       in.RecordID,
		DryRun:         in.DryRun,
		Data:           cloneJSONMap(in.Data),
	})
	if err != nil {
		return nil
	}
	return out
}

func validateBusinessActionTemplateData(action BusinessAction, data map[string]any) *ValidateRecordResult {
	fields := businessActionFieldDefinitions(action)
	result := validateRecordDataResult(action.DatasetID, fields, data)
	appendBusinessActionDataErrors(&result, action.ID, data)
	return &result
}

func appendBusinessActionDataErrors(result *ValidateRecordResult, actionID string, data map[string]any) {
	if result == nil {
		return
	}
	if errors := businessActionDataErrors(actionID, data); len(errors) > 0 {
		result.Valid = false
		result.Errors = append(result.Errors, errors...)
	}
}

func businessActionDataErrors(actionID string, data map[string]any) []string {
	switch strings.TrimSpace(actionID) {
	default:
		return nil
	}
}

func financeVoucherValidationErrors(data map[string]any) []string {
	errors := []string{}
	debitTotal, debitOK := numberFromAny(data["debit_total"])
	creditTotal, creditOK := numberFromAny(data["credit_total"])
	if debitOK && creditOK && !numbersClose(debitTotal, creditTotal) {
		errors = append(errors, "voucher debit_total must equal credit_total")
	}
	lines, ok := data["lines"].([]any)
	if !ok || len(lines) == 0 {
		return errors
	}
	lineDebit := 0.0
	lineCredit := 0.0
	for index, raw := range lines {
		line, ok := raw.(map[string]any)
		if !ok {
			errors = append(errors, fmt.Sprintf("voucher line %d must be object", index))
			continue
		}
		if debit, ok := numberFromAny(line["debit"]); ok {
			lineDebit += debit
		}
		if credit, ok := numberFromAny(line["credit"]); ok {
			lineCredit += credit
		}
	}
	if !numbersClose(lineDebit, lineCredit) {
		errors = append(errors, "voucher line debit sum must equal line credit sum")
	}
	if debitOK && !numbersClose(lineDebit, debitTotal) {
		errors = append(errors, "voucher line debit sum must equal debit_total")
	}
	if creditOK && !numbersClose(lineCredit, creditTotal) {
		errors = append(errors, "voucher line credit sum must equal credit_total")
	}
	return errors
}

func numbersClose(left, right float64) bool {
	diff := left - right
	return diff >= -0.000001 && diff <= 0.000001
}

func businessActionFieldDefinitions(action BusinessAction) []FieldDefinition {
	fields := make([]FieldDefinition, 0, len(action.InputFields))
	for _, field := range action.InputFields {
		fields = append(fields, FieldDefinition{Key: field.Key, Type: field.Type, Title: field.Title, Required: field.Required, Sensitive: field.Sensitive, Config: cloneJSONMap(field.Config)})
	}
	return fields
}

func isMergeOperation(operation string) bool {
	switch strings.TrimSpace(operation) {
	case "merge", "merge_record", "patch_record":
		return true
	default:
		return false
	}
}

func templateFields(templateID string) []DatasetTemplateField {
	for _, tmpl := range datasetTemplates {
		if tmpl.ID == templateID {
			return append([]DatasetTemplateField(nil), tmpl.Fields...)
		}
	}
	return nil
}
