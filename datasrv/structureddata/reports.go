package structureddata

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	maxAggregateGroupBy       = 5
	maxAggregateGroupMappings = 50
	maxAggregateMetrics       = 10
	maxAggregateOutputLimit   = 500
	maxAggregateScanLimit     = 5000
	maxAggregateSorts         = 5
)

var reportDefinitions = []ReportDefinition{
	{ID: "company.department_count_by_status", Domain: "company", Title: "Departments by status", Description: "Department count grouped by organization status.", DatasetID: "company.departments", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "departments", Op: "count"}}, Limit: 500}},
	{ID: "sales.order_summary_by_stage", Domain: "sales", Title: "Sales orders by stage", Description: "Count and amount grouped by order stage.", DatasetID: "sales.orders", Aggregate: AggregateInput{GroupBy: []string{"stage"}, Metrics: []AggregateMetric{{Name: "orders", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "sales.revenue_by_customer", Domain: "sales", Title: "Revenue by customer", Description: "Sales amount grouped by customer.", DatasetID: "sales.orders", Aggregate: AggregateInput{GroupBy: []string{"customer"}, Metrics: []AggregateMetric{{Name: "orders", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "sales.contacts_by_customer", Domain: "sales", Title: "Sales contacts by customer", Description: "Contact count grouped by customer.", DatasetID: "sales.contacts", Aggregate: AggregateInput{GroupBy: []string{"customer"}, Metrics: []AggregateMetric{{Name: "contacts", Op: "count"}}, Limit: 500}},
	{ID: "sales.contacts_by_status", Domain: "sales", Title: "Sales contacts by status", Description: "Contact count grouped by lifecycle status.", DatasetID: "sales.contacts", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "contacts", Op: "count"}}, Limit: 500}},
	{ID: "sales.opportunity_pipeline_by_stage", Domain: "sales", Title: "Opportunity pipeline by stage", Description: "Opportunity count and expected amount grouped by pipeline stage.", DatasetID: "sales.opportunities", Aggregate: AggregateInput{GroupBy: []string{"stage"}, Metrics: []AggregateMetric{{Name: "opportunities", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "sales.opportunity_pipeline_by_owner", Domain: "sales", Title: "Opportunity pipeline by owner", Description: "Opportunity count and expected amount grouped by owner.", DatasetID: "sales.opportunities", Aggregate: AggregateInput{GroupBy: []string{"owner"}, Metrics: []AggregateMetric{{Name: "opportunities", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "finance.expense_by_department", Domain: "finance", Title: "Expenses by department", Description: "Expense count and amount grouped by department.", DatasetID: "finance.expenses", Aggregate: AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "expenses", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "finance.invoice_status_summary", Domain: "finance", Title: "Invoice status summary", Description: "Invoice count and amount grouped by status.", DatasetID: "finance.invoices", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "invoices", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "finance.payment_status_summary", Domain: "finance", Title: "Payment status summary", Description: "Payment count and amount grouped by payment status.", DatasetID: "finance.payments", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "payments", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "finance.payment_cashflow_by_type", Domain: "finance", Title: "Payment cashflow by type", Description: "Payment count and amount grouped by payable, receivable, refund, or transfer type.", DatasetID: "finance.payments", Aggregate: AggregateInput{GroupBy: []string{"payment_type"}, Metrics: []AggregateMetric{{Name: "payments", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "finance.account_count_by_type", Domain: "finance", Title: "Accounts by type", Description: "Chart of accounts count grouped by account type.", DatasetID: "finance.accounts", Aggregate: AggregateInput{GroupBy: []string{"account_type"}, Metrics: []AggregateMetric{{Name: "accounts", Op: "count"}}, Limit: 500}},
	{ID: "finance.budget_by_department", Domain: "finance", Title: "Budget by department", Description: "Budget, committed, and actual amounts grouped by department.", DatasetID: "finance.budgets", Aggregate: AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "budgets", Op: "count"}, {Name: "budget_amount", Op: "sum", Field: "budget_amount"}, {Name: "committed_amount", Op: "sum", Field: "committed_amount"}, {Name: "actual_amount", Op: "sum", Field: "actual_amount"}}, Limit: 500}},
	{ID: "finance.budget_by_status", Domain: "finance", Title: "Budget by status", Description: "Budget count and amount grouped by lifecycle status.", DatasetID: "finance.budgets", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "budgets", Op: "count"}, {Name: "budget_amount", Op: "sum", Field: "budget_amount"}, {Name: "actual_amount", Op: "sum", Field: "actual_amount"}}, Limit: 500}},
	{ID: "finance.voucher_status_summary", Domain: "finance", Title: "Voucher status summary", Description: "Voucher count and debit/credit totals grouped by voucher status.", DatasetID: "finance.vouchers", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "vouchers", Op: "count"}, {Name: "debit_total", Op: "sum", Field: "debit_total"}, {Name: "credit_total", Op: "sum", Field: "credit_total"}}, Limit: 500}},
	{ID: "finance.voucher_by_period", Domain: "finance", Title: "Voucher by period", Description: "Voucher count and debit/credit totals grouped by accounting period.", DatasetID: "finance.vouchers", Aggregate: AggregateInput{GroupBy: []string{"period"}, Metrics: []AggregateMetric{{Name: "vouchers", Op: "count"}, {Name: "debit_total", Op: "sum", Field: "debit_total"}, {Name: "credit_total", Op: "sum", Field: "credit_total"}}, Limit: 500}},
	{ID: "hr.employee_count_by_department", Domain: "hr", Title: "Employee count by department", Description: "Headcount grouped by department.", DatasetID: "hr.employees", Aggregate: AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "employees", Op: "count"}}, Limit: 500}},
	{ID: "hr.payroll_by_department", Domain: "hr", Title: "Payroll by department", Description: "Payroll count, gross pay, tax, and net pay grouped by department.", DatasetID: "hr.payroll", Aggregate: AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "payroll_lines", Op: "count"}, {Name: "gross_pay", Op: "sum", Field: "gross_pay"}, {Name: "tax", Op: "sum", Field: "tax"}, {Name: "net_pay", Op: "sum", Field: "net_pay"}}, Limit: 500}},
	{ID: "hr.payroll_status_summary", Domain: "hr", Title: "Payroll status summary", Description: "Payroll count and net pay grouped by status.", DatasetID: "hr.payroll", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "payroll_lines", Op: "count"}, {Name: "net_pay", Op: "sum", Field: "net_pay"}}, Limit: 500}},
	{ID: "hr.leave_by_status", Domain: "hr", Title: "Leave requests by status", Description: "Leave request count and days grouped by approval status.", DatasetID: "hr.leave_requests", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "leave_requests", Op: "count"}, {Name: "days", Op: "sum", Field: "days"}}, Limit: 500}},
	{ID: "hr.leave_by_department", Domain: "hr", Title: "Leave days by department", Description: "Leave request count and days grouped by department.", DatasetID: "hr.leave_requests", Aggregate: AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "leave_requests", Op: "count"}, {Name: "days", Op: "sum", Field: "days"}}, Limit: 500}},
	{ID: "legal.contract_value_by_status", Domain: "legal", Title: "Contract value by status", Description: "Contract count and value grouped by status.", DatasetID: "legal.contracts", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "contracts", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "procurement.po_amount_by_supplier", Domain: "procurement", Title: "Purchase amount by supplier", Description: "Purchase order count and amount grouped by supplier.", DatasetID: "procurement.purchase_orders", Aggregate: AggregateInput{GroupBy: []string{"supplier"}, Metrics: []AggregateMetric{{Name: "purchase_orders", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "procurement.po_status_summary", Domain: "procurement", Title: "Purchase order status summary", Description: "Purchase order count and amount grouped by status.", DatasetID: "procurement.purchase_orders", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "purchase_orders", Op: "count"}, {Name: "amount", Op: "sum", Field: "amount"}}, Limit: 500}},
	{ID: "procurement.supplier_by_status", Domain: "procurement", Title: "Suppliers by status", Description: "Supplier count grouped by supplier lifecycle status.", DatasetID: "procurement.suppliers", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "suppliers", Op: "count"}}, Limit: 500}},
	{ID: "procurement.supplier_by_category", Domain: "procurement", Title: "Suppliers by category", Description: "Supplier count grouped by category.", DatasetID: "procurement.suppliers", Aggregate: AggregateInput{GroupBy: []string{"category"}, Metrics: []AggregateMetric{{Name: "suppliers", Op: "count"}}, Limit: 500}},
	{ID: "inventory.quantity_by_warehouse", Domain: "inventory", Title: "Inventory quantity by warehouse", Description: "Inventory item count and quantity grouped by warehouse.", DatasetID: "inventory.items", Aggregate: AggregateInput{GroupBy: []string{"warehouse"}, Metrics: []AggregateMetric{{Name: "items", Op: "count"}, {Name: "quantity", Op: "sum", Field: "quantity"}}, Limit: 500}},
	{ID: "inventory.low_stock_summary", Domain: "inventory", Title: "Low stock summary", Description: "Low-stock item count by category.", DatasetID: "inventory.items", Aggregate: AggregateInput{Filter: map[string]any{"field": "status", "op": "eq", "value": "low_stock"}, GroupBy: []string{"category"}, Metrics: []AggregateMetric{{Name: "items", Op: "count"}, {Name: "quantity", Op: "sum", Field: "quantity"}}, Limit: 500}},
	{ID: "inventory.warehouse_by_status", Domain: "inventory", Title: "Warehouse by status", Description: "Warehouse count grouped by lifecycle status.", DatasetID: "inventory.warehouses", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "warehouses", Op: "count"}}, Limit: 500}},
	{ID: "inventory.movement_by_type", Domain: "inventory", Title: "Inventory movement by type", Description: "Inventory movement count and quantity grouped by movement type.", DatasetID: "inventory.movements", Aggregate: AggregateInput{GroupBy: []string{"movement_type"}, Metrics: []AggregateMetric{{Name: "movements", Op: "count"}, {Name: "quantity", Op: "sum", Field: "quantity"}}, Limit: 500}},
	{ID: "inventory.movement_by_warehouse", Domain: "inventory", Title: "Inventory movement by warehouse", Description: "Inventory movement count and quantity grouped by destination warehouse.", DatasetID: "inventory.movements", Aggregate: AggregateInput{GroupBy: []string{"to_warehouse"}, Metrics: []AggregateMetric{{Name: "movements", Op: "count"}, {Name: "quantity", Op: "sum", Field: "quantity"}}, Limit: 500}},
	{ID: "assets.value_by_department", Domain: "assets", Title: "Asset value by department", Description: "Fixed asset count and purchase cost grouped by department.", DatasetID: "assets.fixed_assets", Aggregate: AggregateInput{GroupBy: []string{"department"}, Metrics: []AggregateMetric{{Name: "assets", Op: "count"}, {Name: "purchase_cost", Op: "sum", Field: "purchase_cost"}}, Limit: 500}},
	{ID: "assets.status_summary", Domain: "assets", Title: "Asset status summary", Description: "Fixed asset count and purchase cost grouped by status.", DatasetID: "assets.fixed_assets", Aggregate: AggregateInput{GroupBy: []string{"status"}, Metrics: []AggregateMetric{{Name: "assets", Op: "count"}, {Name: "purchase_cost", Op: "sum", Field: "purchase_cost"}}, Limit: 500}},
}

func (s *Service) ListReports(ctx context.Context, p Principal, query ...QueryReportsInput) ([]ReportDefinition, error) {
	_ = ctx
	out := append([]ReportDefinition(nil), reportDefinitions...)
	if p.Policy != nil {
		out = filterCapabilityReports(p, out)
	}
	if len(query) == 0 {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	return paginateReports(out, query[0]), nil
}

func paginateReports(items []ReportDefinition, in QueryReportsInput) []ReportDefinition {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]ReportDefinition(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, report := range out {
			if report.ID < beforeID {
				filtered = append(filtered, report)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) GetReport(ctx context.Context, p Principal, reportID string) (*ReportDefinition, error) {
	_ = ctx
	_ = p
	id := strings.TrimSpace(reportID)
	for _, report := range reportDefinitions {
		if report.ID == id {
			clone := report
			clone.Aggregate.GroupBy = append([]string(nil), report.Aggregate.GroupBy...)
			clone.Aggregate.Metrics = append([]AggregateMetric(nil), report.Aggregate.Metrics...)
			clone.Aggregate.Sort = append([]SortSpec(nil), report.Aggregate.Sort...)
			return &clone, nil
		}
	}
	return nil, ErrDatasetNotFound
}

func (s *Service) RunReport(ctx context.Context, p Principal, reportID string, in AggregateInput) (*ReportResult, error) {
	report, err := s.GetReport(ctx, p, reportID)
	if err != nil {
		return nil, err
	}
	agg := report.Aggregate
	if in.Filter != nil {
		agg.Filter = in.Filter
	}
	if in.Limit > 0 {
		agg.Limit = in.Limit
	}
	if in.ScanLimit > 0 {
		agg.ScanLimit = in.ScanLimit
	}
	if len(in.Sort) > 0 {
		agg.Sort = append([]SortSpec(nil), in.Sort...)
	}
	result, err := s.AggregateRecords(ctx, p, report.DatasetID, agg)
	if err != nil {
		return nil, err
	}
	out := &ReportResult{Report: *report, Result: *result}
	applyReportResultPackage(out)
	return out, nil
}

func (s *Service) AggregateRecords(ctx context.Context, p Principal, datasetID string, in AggregateInput) (*AggregateResult, error) {
	datasetID = strings.TrimSpace(datasetID)
	if _, err := s.store.GetDataset(ctx, p.TenantID, datasetID); err != nil {
		return nil, err
	}
	if len(in.Metrics) == 0 {
		in.Metrics = []AggregateMetric{{Name: "count", Op: "count"}}
	}
	in.GroupBy = cleanFields(in.GroupBy)
	if len(in.GroupBy) > maxAggregateGroupBy {
		return nil, fmt.Errorf("%w: too many aggregate group_by fields", ErrInvalidInput)
	}
	if err := validateAggregateMetrics(in.Metrics); err != nil {
		return nil, err
	}
	if err := validateAggregateSort(in.Sort, in.GroupBy, in.Metrics); err != nil {
		return nil, err
	}
	outputLimit := in.Limit
	if outputLimit > maxAggregateOutputLimit {
		return nil, fmt.Errorf("%w: aggregate limit must be less than or equal to %d", ErrInvalidInput, maxAggregateOutputLimit)
	}
	if outputLimit <= 0 {
		outputLimit = maxAggregateOutputLimit
	}
	scanLimit := in.ScanLimit
	if scanLimit > maxAggregateScanLimit {
		return nil, fmt.Errorf("%w: aggregate scan_limit must be less than or equal to %d", ErrInvalidInput, maxAggregateScanLimit)
	}
	if scanLimit <= 0 {
		scanLimit = maxAggregateScanLimit
	}
	records, truncated, err := s.queryAggregateRecords(ctx, p, datasetID, in.Filter, scanLimit)
	if err != nil {
		return nil, err
	}
	groups := map[string]*aggregateBucket{}
	order := []string{}
	for _, record := range records {
		assignments, err := aggregateGroupAssignments(record, in.GroupBy)
		if err != nil {
			return nil, err
		}
		for _, assignment := range assignments {
			key := strings.Join(assignment.KeyParts, "\x1f")
			if key == "" {
				key = "__all__"
			}
			bucket, ok := groups[key]
			if !ok {
				bucket = &aggregateBucket{Labels: assignment.Labels, Values: map[string]*metricState{}}
				groups[key] = bucket
				order = append(order, key)
			}
			bucket.Count++
			for _, metric := range in.Metrics {
				name := metricName(metric)
				state := bucket.Values[name]
				if state == nil {
					state = &metricState{Min: math.Inf(1), Max: math.Inf(-1)}
					bucket.Values[name] = state
				}
				applyMetric(state, metric, record, assignment.Labels)
			}
		}
	}
	rows := make([]map[string]any, 0, len(order))
	for _, key := range order {
		bucket := groups[key]
		row := map[string]any{}
		for _, field := range in.GroupBy {
			row[field] = bucket.Labels[field]
		}
		for _, metric := range in.Metrics {
			name := metricName(metric)
			row[name] = finalizeMetric(bucket.Values[name], metric)
		}
		rows = append(rows, row)
	}
	sortAggregateRows(rows, in.Sort, in.GroupBy)
	if len(rows) > outputLimit {
		rows = rows[:outputLimit]
	}
	return &AggregateResult{DatasetID: datasetID, GroupBy: in.GroupBy, Metrics: in.Metrics, Rows: rows, Scanned: len(records), Limit: outputLimit, ScanLimit: scanLimit, Truncated: truncated}, nil
}

func (s *Service) queryAggregateRecords(ctx context.Context, p Principal, datasetID string, filter map[string]any, scanLimit int) ([]Record, bool, error) {
	records := []Record{}
	before := ""
	beforeID := ""
	for len(records) < scanLimit {
		pageLimit := scanLimit - len(records)
		if pageLimit > 500 {
			pageLimit = 500
		}
		page, err := s.store.QueryRecords(ctx, p.TenantID, datasetID, QueryRecordsInput{Filter: filter, Limit: pageLimit, Before: before, BeforeID: beforeID})
		if err != nil {
			return nil, false, err
		}
		records = append(records, page...)
		if len(page) < pageLimit || len(page) == 0 {
			return records, false, nil
		}
		last := page[len(page)-1]
		before = last.CreatedAt.Format(time.RFC3339Nano)
		beforeID = last.ID
	}
	return records, true, nil
}

type aggregateBucket struct {
	Labels map[string]any
	Values map[string]*metricState
	Count  int
}

type aggregateGroupAssignment struct {
	Labels   map[string]any
	KeyParts []string
}

type metricState struct {
	Count    int
	Sum      float64
	Min      float64
	Max      float64
	Distinct map[string]struct{}
}

func aggregateGroupAssignments(record Record, groupBy []string) ([]aggregateGroupAssignment, error) {
	if len(groupBy) == 0 {
		return []aggregateGroupAssignment{{Labels: map[string]any{}, KeyParts: nil}}, nil
	}
	assignments := []aggregateGroupAssignment{{Labels: map[string]any{}, KeyParts: []string{}}}
	for _, field := range groupBy {
		values := aggregateFieldValues(record.Data, field)
		next := make([]aggregateGroupAssignment, 0, len(assignments)*len(values))
		for _, assignment := range assignments {
			for _, value := range values {
				labels := cloneAnyMap(assignment.Labels)
				labels[field] = value
				keyParts := append(append([]string(nil), assignment.KeyParts...), aggregateGroupKey(value))
				next = append(next, aggregateGroupAssignment{Labels: labels, KeyParts: keyParts})
				if len(next) > maxAggregateGroupMappings {
					return nil, fmt.Errorf("%w: too many aggregate group values", ErrInvalidInput)
				}
			}
		}
		assignments = next
	}
	return assignments, nil
}

func aggregateFieldValues(data map[string]any, field string) []any {
	value, ok := recordDataPathValue(data, field)
	if !ok {
		return []any{nil}
	}
	if isArrayValue(value) {
		if values := scalarArrayValues(value); len(values) > 0 {
			return values
		}
		return []any{nil}
	}
	return []any{value}
}

func aggregateGroupKey(value any) string {
	switch value.(type) {
	case map[string]any, []any:
		return jsonString(value)
	default:
		return fmt.Sprint(value)
	}
}

func recordDataPathValue(data map[string]any, field string) (any, bool) {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil, false
	}
	parts := strings.Split(field, ".")
	var current any = data
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func cloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func applyMetric(state *metricState, metric AggregateMetric, record Record, labels map[string]any) {
	switch strings.ToLower(strings.TrimSpace(metric.Op)) {
	case "count", "":
		state.Count++
	case "count_distinct":
		if state.Distinct == nil {
			state.Distinct = map[string]struct{}{}
		}
		for _, raw := range aggregateMetricValues(record, metric, labels) {
			if raw == nil {
				continue
			}
			state.Distinct[aggregateGroupKey(raw)] = struct{}{}
		}
	case "sum", "avg", "min", "max":
		for _, raw := range aggregateMetricValues(record, metric, labels) {
			value, ok := numberFromAny(raw)
			if !ok {
				continue
			}
			state.Count++
			state.Sum += value
			if value < state.Min {
				state.Min = value
			}
			if value > state.Max {
				state.Max = value
			}
		}
	}
}

func aggregateMetricValues(record Record, metric AggregateMetric, labels map[string]any) []any {
	field := strings.TrimSpace(metric.Field)
	if field == "" {
		return []any{nil}
	}
	if value, ok := labels[field]; ok {
		return []any{value}
	}
	return aggregateFieldValues(record.Data, field)
}

func finalizeMetric(state *metricState, metric AggregateMetric) any {
	if state == nil {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(metric.Op)) {
	case "count_distinct":
		return len(state.Distinct)
	case "sum":
		return state.Sum
	case "avg":
		if state.Count == 0 {
			return 0
		}
		return state.Sum / float64(state.Count)
	case "min":
		if state.Count == 0 {
			return 0
		}
		return state.Min
	case "max":
		if state.Count == 0 {
			return 0
		}
		return state.Max
	default:
		return state.Count
	}
}

func metricName(metric AggregateMetric) string {
	if strings.TrimSpace(metric.Name) != "" {
		return strings.TrimSpace(metric.Name)
	}
	if strings.TrimSpace(metric.As) != "" {
		return strings.TrimSpace(metric.As)
	}
	if strings.TrimSpace(metric.Field) != "" {
		return strings.ToLower(strings.TrimSpace(metric.Op)) + "_" + strings.TrimSpace(metric.Field)
	}
	return strings.ToLower(firstNonEmpty(metric.Op, "count"))
}

func validateAggregateMetrics(metrics []AggregateMetric) error {
	if len(metrics) > maxAggregateMetrics {
		return fmt.Errorf("%w: too many aggregate metrics", ErrInvalidInput)
	}
	for _, metric := range metrics {
		op := strings.ToLower(strings.TrimSpace(metric.Op))
		switch op {
		case "", "count":
		case "count_distinct", "sum", "avg", "min", "max":
			if strings.TrimSpace(metric.Field) == "" {
				return fmt.Errorf("%w: aggregate metric %q requires field", ErrInvalidInput, firstNonEmpty(metric.Name, metric.As, metric.Op))
			}
		default:
			return fmt.Errorf("%w: unsupported aggregate metric op %q", ErrInvalidInput, metric.Op)
		}
	}
	return nil
}

func validateAggregateSort(specs []SortSpec, groupBy []string, metrics []AggregateMetric) error {
	if len(specs) == 0 {
		return nil
	}
	if len(specs) > maxAggregateSorts {
		return fmt.Errorf("%w: too many aggregate sort fields", ErrInvalidInput)
	}
	allowed := map[string]struct{}{}
	for _, field := range groupBy {
		allowed[strings.TrimSpace(field)] = struct{}{}
	}
	for _, metric := range metrics {
		allowed[metricName(metric)] = struct{}{}
	}
	for _, spec := range specs {
		field := strings.TrimSpace(spec.Field)
		if field == "" {
			return fmt.Errorf("%w: aggregate sort field is required", ErrInvalidInput)
		}
		if _, ok := allowed[field]; !ok {
			return fmt.Errorf("%w: aggregate sort field %q is not a group or metric field", ErrInvalidInput, field)
		}
		switch strings.ToLower(strings.TrimSpace(spec.Direction)) {
		case "", "asc", "desc":
		default:
			return fmt.Errorf("%w: unsupported aggregate sort direction", ErrInvalidInput)
		}
	}
	return nil
}

func sortAggregateRows(rows []map[string]any, specs []SortSpec, fallback []string) {
	sortFields := append([]SortSpec(nil), specs...)
	if len(sortFields) == 0 {
		for _, field := range fallback {
			sortFields = append(sortFields, SortSpec{Field: field, Direction: "asc"})
		}
	}
	if len(sortFields) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		for _, spec := range sortFields {
			field := strings.TrimSpace(spec.Field)
			if field == "" {
				continue
			}
			cmp := compareAggregateValues(rows[i][field], rows[j][field])
			if cmp == 0 {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(spec.Direction), "desc") {
				return cmp > 0
			}
			return cmp < 0
		}
		return false
	})
}

func compareAggregateValues(left, right any) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	if l, ok := numberFromAny(left); ok {
		if r, ok := numberFromAny(right); ok {
			switch {
			case l < r:
				return -1
			case l > r:
				return 1
			default:
				return 0
			}
		}
	}
	l := strings.ToLower(fmt.Sprint(left))
	r := strings.ToLower(fmt.Sprint(right))
	switch {
	case l < r:
		return -1
	case l > r:
		return 1
	default:
		return 0
	}
}

func cleanFields(fields []string) []string {
	out := []string{}
	for _, field := range fields {
		if strings.TrimSpace(field) != "" {
			out = append(out, strings.TrimSpace(field))
		}
	}
	return out
}

func numberFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
