package structureddata

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

var businessViews = []BusinessViewDefinition{
	{
		ID:           "company.department_directory",
		Domain:       "company",
		Title:        "Department directory",
		Description:  "Company department directory for organization, cost center, and manager lookup.",
		DatasetID:    "company.departments",
		Fields:       []string{"department_code", "name", "manager", "cost_center", "status"},
		DefaultSort:  []SortSpec{{Field: "department_code", Direction: "asc"}},
		DefaultLimit: 200,
	},
	{
		ID:           "sales.order_overview",
		Domain:       "sales",
		Title:        "Sales order overview",
		Description:  "Operational view for sales order tracking and customer follow-up.",
		DatasetID:    "sales.orders",
		Fields:       []string{"order_no", "customer", "amount", "currency", "owner", "stage", "payment_status", "order_date"},
		DefaultSort:  []SortSpec{{Field: "order_date", Direction: "desc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "sales.customer_directory",
		Domain:       "sales",
		Title:        "Customer directory",
		Description:  "Customer master data view for service and sales operations.",
		DatasetID:    "sales.customers",
		Fields:       []string{"customer_no", "name", "industry", "region", "contact", "phone", "owner", "status"},
		DefaultSort:  []SortSpec{{Field: "name", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "sales.contact_directory",
		Domain:       "sales",
		Title:        "Sales contact directory",
		Description:  "Customer contact view for account ownership, communication, and opportunity follow-up.",
		DatasetID:    "sales.contacts",
		Fields:       []string{"contact_no", "name", "customer", "role", "email", "phone", "owner", "status"},
		DefaultSort:  []SortSpec{{Field: "name", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "sales.opportunity_pipeline",
		Domain:       "sales",
		Title:        "Sales opportunity pipeline",
		Description:  "Opportunity pipeline view for stage, expected amount, probability, owner, and close date tracking.",
		DatasetID:    "sales.opportunities",
		Fields:       []string{"opportunity_no", "name", "customer", "amount", "currency", "probability", "owner", "stage", "expected_close_date"},
		DefaultSort:  []SortSpec{{Field: "expected_close_date", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "finance.expense_review",
		Domain:       "finance",
		Title:        "Expense review",
		Description:  "Expense reimbursement review queue for finance operations.",
		DatasetID:    "finance.expenses",
		Fields:       []string{"expense_no", "applicant", "department", "category", "amount", "currency", "status", "expense_date"},
		DefaultSort:  []SortSpec{{Field: "expense_date", Direction: "desc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "finance.invoice_status",
		Domain:       "finance",
		Title:        "Invoice status",
		Description:  "Invoice lifecycle view for accounts receivable and payable checks.",
		DatasetID:    "finance.invoices",
		Fields:       []string{"invoice_no", "counterparty", "invoice_type", "amount", "tax_amount", "currency", "status", "issue_date"},
		DefaultSort:  []SortSpec{{Field: "issue_date", Direction: "desc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "finance.payment_tracker",
		Domain:       "finance",
		Title:        "Payment tracker",
		Description:  "Payment operations view for accounts payable, receivable, refunds, and transfers.",
		DatasetID:    "finance.payments",
		Fields:       []string{"payment_no", "counterparty", "payment_type", "amount", "currency", "method", "status", "payment_date"},
		DefaultSort:  []SortSpec{{Field: "payment_date", Direction: "desc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "finance.account_directory",
		Domain:       "finance",
		Title:        "Chart of accounts",
		Description:  "Finance account directory for lightweight voucher classification.",
		DatasetID:    "finance.accounts",
		Fields:       []string{"account_code", "account_name", "account_type", "parent_code", "currency", "status"},
		DefaultSort:  []SortSpec{{Field: "account_code", Direction: "asc"}},
		DefaultLimit: 200,
	},
	{
		ID:           "finance.budget_control",
		Domain:       "finance",
		Title:        "Budget control",
		Description:  "Budget control view for department, category, period, budget, committed, actual, owner, and status checks.",
		DatasetID:    "finance.budgets",
		Fields:       []string{"budget_no", "period", "department", "category", "budget_amount", "committed_amount", "actual_amount", "currency", "owner", "status"},
		DefaultSort:  []SortSpec{{Field: "period", Direction: "desc"}, {Field: "department", Direction: "asc"}, {Field: "category", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "finance.voucher_register",
		Domain:       "finance",
		Title:        "Voucher register",
		Description:  "Accounting voucher register for review, posting, reversal, and audit checks.",
		DatasetID:    "finance.vouchers",
		Fields:       []string{"voucher_no", "period", "voucher_type", "summary", "debit_total", "credit_total", "currency", "status", "posted_at"},
		DefaultSort:  []SortSpec{{Field: "period", Direction: "desc"}, {Field: "voucher_no", Direction: "desc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "hr.employee_roster",
		Domain:       "hr",
		Title:        "Employee roster",
		Description:  "HR roster view for organization, role, and employment status checks.",
		DatasetID:    "hr.employees",
		Fields:       []string{"employee_no", "name", "department", "position", "manager", "employment_status", "hire_date", "mobile"},
		DefaultSort:  []SortSpec{{Field: "employee_no", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "hr.payroll_review",
		Domain:       "hr",
		Title:        "Payroll review",
		Description:  "Sensitive payroll review view for monthly payroll preparation, approval, and payment checks.",
		DatasetID:    "hr.payroll",
		Fields:       []string{"payroll_month", "employee_no", "employee_name", "department", "gross_pay", "tax", "net_pay", "status"},
		DefaultSort:  []SortSpec{{Field: "payroll_month", Direction: "desc"}, {Field: "employee_no", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "hr.leave_request_review",
		Domain:       "hr",
		Title:        "Leave request review",
		Description:  "Leave and absence review queue for employee, department, date range, days, approver, and status checks.",
		DatasetID:    "hr.leave_requests",
		Fields:       []string{"leave_no", "employee_no", "employee_name", "department", "leave_type", "start_date", "end_date", "days", "approver", "status"},
		DefaultSort:  []SortSpec{{Field: "start_date", Direction: "desc"}, {Field: "leave_no", Direction: "desc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "legal.contract_register",
		Domain:       "legal",
		Title:        "Contract register",
		Description:  "Contract lifecycle view for legal and business owners.",
		DatasetID:    "legal.contracts",
		Fields:       []string{"contract_no", "counterparty", "contract_type", "amount", "owner", "status", "signed_date", "expires_at"},
		DefaultSort:  []SortSpec{{Field: "expires_at", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "procurement.purchase_order_tracker",
		Domain:       "procurement",
		Title:        "Purchase order tracker",
		Description:  "Procurement order tracking view for approval, receiving, and supplier follow-up.",
		DatasetID:    "procurement.purchase_orders",
		Fields:       []string{"po_no", "supplier", "requester", "department", "category", "amount", "currency", "status", "order_date", "expected_date"},
		DefaultSort:  []SortSpec{{Field: "expected_date", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "procurement.supplier_directory",
		Domain:       "procurement",
		Title:        "Supplier directory",
		Description:  "Supplier master data view for procurement, finance, contracts, and payment workflows.",
		DatasetID:    "procurement.suppliers",
		Fields:       []string{"supplier_no", "name", "category", "region", "contact", "phone", "payment_terms", "status"},
		DefaultSort:  []SortSpec{{Field: "name", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "inventory.stock_overview",
		Domain:       "inventory",
		Title:        "Inventory stock overview",
		Description:  "Inventory stock view for warehouse, quantity, status, and replenishment checks.",
		DatasetID:    "inventory.items",
		Fields:       []string{"item_no", "name", "category", "warehouse", "quantity", "unit", "reorder_level", "status", "last_movement_at"},
		DefaultSort:  []SortSpec{{Field: "status", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "inventory.warehouse_directory",
		Domain:       "inventory",
		Title:        "Warehouse directory",
		Description:  "Warehouse master data view for location, responsible person, and status checks.",
		DatasetID:    "inventory.warehouses",
		Fields:       []string{"warehouse_code", "name", "location", "manager", "status"},
		DefaultSort:  []SortSpec{{Field: "warehouse_code", Direction: "asc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "inventory.movement_ledger",
		Domain:       "inventory",
		Title:        "Inventory movement ledger",
		Description:  "Inventory movement view for receipt, issue, transfer, adjustment, reservation, and posting traceability.",
		DatasetID:    "inventory.movements",
		Fields:       []string{"movement_no", "item_no", "movement_type", "quantity", "unit", "from_warehouse", "to_warehouse", "status", "occurred_at"},
		DefaultSort:  []SortSpec{{Field: "occurred_at", Direction: "desc"}},
		DefaultLimit: 100,
	},
	{
		ID:           "assets.asset_register",
		Domain:       "assets",
		Title:        "Fixed asset register",
		Description:  "Company asset register view for ownership, location, cost, and lifecycle status.",
		DatasetID:    "assets.fixed_assets",
		Fields:       []string{"asset_no", "name", "asset_type", "department", "custodian", "purchase_cost", "currency", "status", "purchase_date", "location"},
		DefaultSort:  []SortSpec{{Field: "asset_no", Direction: "asc"}},
		DefaultLimit: 100,
	},
}

func (s *Service) ListBusinessViews(ctx context.Context, p Principal, query ...QueryBusinessViewsInput) ([]BusinessViewDefinition, error) {
	_ = ctx
	out := cloneBusinessViews(businessViews)
	if p.Policy != nil {
		out = filterCapabilityViews(p, out)
	}
	if len(query) == 0 {
		return out, nil
	}
	return paginateBusinessViews(out, query[0]), nil
}

func paginateBusinessViews(items []BusinessViewDefinition, in QueryBusinessViewsInput) []BusinessViewDefinition {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := cloneBusinessViews(items)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, view := range out {
			if view.ID < beforeID {
				filtered = append(filtered, view)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) GetBusinessView(ctx context.Context, p Principal, viewID string) (*BusinessViewDefinition, error) {
	_ = ctx
	_ = p
	id := strings.TrimSpace(viewID)
	for _, view := range businessViews {
		if view.ID == id {
			clone := cloneBusinessView(view)
			return &clone, nil
		}
	}
	return nil, ErrDatasetNotFound
}

func (s *Service) QueryBusinessView(ctx context.Context, p Principal, viewID string, in QueryBusinessViewInput) (*BusinessViewResult, error) {
	view, err := s.GetBusinessView(ctx, p, viewID)
	if err != nil {
		return nil, err
	}
	query := QueryRecordsInput{
		Q:        in.Q,
		Tag:      in.Tag,
		Filter:   mergeJSONMaps(view.DefaultFilter, in.Filter),
		Sort:     append([]SortSpec(nil), in.Sort...),
		Limit:    in.Limit,
		Before:   in.Before,
		BeforeID: in.BeforeID,
	}
	if len(query.Sort) == 0 {
		query.Sort = append([]SortSpec(nil), view.DefaultSort...)
	}
	if query.Limit <= 0 {
		query.Limit = view.DefaultLimit
	}
	if query.Limit <= 0 {
		query.Limit = 100
	}
	if query.Limit > 500 {
		return nil, fmt.Errorf("%w: business view limit must be less than or equal to 500", ErrInvalidInput)
	}
	pageLimit := query.Limit
	query.Limit = 500
	query.Before = ""
	query.BeforeID = ""
	records, err := s.QueryRecords(ctx, p, view.DatasetID, query)
	if err != nil {
		return nil, err
	}
	if beforeID := strings.TrimSpace(in.BeforeID); beforeID != "" {
		for idx, record := range records {
			if record.ID == beforeID {
				records = records[idx+1:]
				break
			}
		}
	}
	hasMore := len(records) > pageLimit
	if hasMore {
		records = records[:pageLimit]
	}
	result := &BusinessViewResult{View: *view, Records: projectRecords(records, view.Fields), Limit: pageLimit, HasMore: hasMore}
	if result.HasMore {
		result.NextBefore, result.NextBeforeID = recordPageCursor(records, query.Sort)
		if result.NextBeforeID == "" && len(records) > 0 {
			result.NextBeforeID = records[len(records)-1].ID
		}
	}
	return result, nil
}

func projectRecords(records []Record, fields []string) []Record {
	allowed := map[string]struct{}{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			allowed[field] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return records
	}
	out := make([]Record, len(records))
	for i := range records {
		out[i] = records[i]
		out[i].Data = map[string]any{}
		for _, field := range fields {
			if value, ok := records[i].Data[field]; ok {
				out[i].Data[field] = value
			}
		}
	}
	return out
}

func mergeJSONMaps(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := cloneJSONMap(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func cloneBusinessViews(in []BusinessViewDefinition) []BusinessViewDefinition {
	out := make([]BusinessViewDefinition, 0, len(in))
	for _, view := range in {
		out = append(out, cloneBusinessView(view))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func cloneBusinessView(view BusinessViewDefinition) BusinessViewDefinition {
	clone := view
	clone.Fields = append([]string(nil), view.Fields...)
	clone.DefaultFilter = cloneJSONMap(view.DefaultFilter)
	clone.DefaultSort = append([]SortSpec(nil), view.DefaultSort...)
	return clone
}
