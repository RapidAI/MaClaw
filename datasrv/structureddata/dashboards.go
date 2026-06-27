package structureddata

import (
	"context"
	"sort"
	"strings"
)

var dashboardDefinitions = []DashboardDefinition{
	{ID: "company.overview", Domain: "company", Title: "Company MIS overview", Description: "Operational health, pending work, organization, and core business metrics.", ReportIDs: []string{"company.department_count_by_status", "sales.order_summary_by_stage", "finance.invoice_status_summary", "finance.payment_status_summary", "finance.voucher_status_summary", "hr.employee_count_by_department", "hr.payroll_status_summary", "inventory.low_stock_summary"}},
	{ID: "sales.overview", Domain: "sales", Title: "Sales overview", Description: "Opportunity pipeline, order stage, contacts, and customer revenue summary.", ReportIDs: []string{"sales.opportunity_pipeline_by_stage", "sales.opportunity_pipeline_by_owner", "sales.order_summary_by_stage", "sales.revenue_by_customer", "sales.contacts_by_status"}},
	{ID: "finance.overview", Domain: "finance", Title: "Finance overview", Description: "Budget, expenses, invoice status, payment cashflow, and voucher summary.", ReportIDs: []string{"finance.budget_by_department", "finance.budget_by_status", "finance.expense_by_department", "finance.invoice_status_summary", "finance.payment_status_summary", "finance.payment_cashflow_by_type", "finance.voucher_status_summary", "finance.voucher_by_period"}},
	{ID: "hr.overview", Domain: "hr", Title: "HR overview", Description: "Headcount, leave requests, payroll status, and payroll amount summary.", ReportIDs: []string{"hr.employee_count_by_department", "hr.leave_by_status", "hr.leave_by_department", "hr.payroll_status_summary", "hr.payroll_by_department"}},
	{ID: "legal.overview", Domain: "legal", Title: "Legal overview", Description: "Contract status and amount summary.", ReportIDs: []string{"legal.contract_value_by_status"}},
	{ID: "procurement.overview", Domain: "procurement", Title: "Procurement overview", Description: "Supplier directory, purchase order status, and supplier spend summary.", ReportIDs: []string{"procurement.supplier_by_status", "procurement.supplier_by_category", "procurement.po_status_summary", "procurement.po_amount_by_supplier"}},
	{ID: "inventory.overview", Domain: "inventory", Title: "Inventory overview", Description: "Warehouse status, warehouse quantity, low-stock, and movement summary.", ReportIDs: []string{"inventory.warehouse_by_status", "inventory.quantity_by_warehouse", "inventory.low_stock_summary", "inventory.movement_by_type", "inventory.movement_by_warehouse"}},
	{ID: "assets.overview", Domain: "assets", Title: "Assets overview", Description: "Fixed asset status and value summary.", ReportIDs: []string{"assets.status_summary", "assets.value_by_department"}},
}

func (s *Service) ListDashboards(ctx context.Context, p Principal, query ...QueryDashboardsInput) ([]DashboardDefinition, error) {
	_ = ctx
	out := append([]DashboardDefinition(nil), dashboardDefinitions...)
	if p.Policy != nil {
		out = filterCapabilityDashboards(p, out)
	}
	if len(query) == 0 {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	return paginateDashboards(out, query[0]), nil
}

func paginateDashboards(items []DashboardDefinition, in QueryDashboardsInput) []DashboardDefinition {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]DashboardDefinition(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, dashboard := range out {
			if dashboard.ID < beforeID {
				filtered = append(filtered, dashboard)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) GetDashboard(ctx context.Context, p Principal, dashboardID string) (*DashboardDefinition, error) {
	_ = ctx
	_ = p
	id := strings.TrimSpace(dashboardID)
	for _, dashboard := range dashboardDefinitions {
		if dashboard.ID == id {
			clone := dashboard
			clone.ReportIDs = append([]string(nil), dashboard.ReportIDs...)
			return &clone, nil
		}
	}
	return nil, ErrDatasetNotFound
}

func (s *Service) RunDashboard(ctx context.Context, p Principal, dashboardID string) (*DashboardResult, error) {
	dashboard, err := s.GetDashboard(ctx, p, dashboardID)
	if err != nil {
		return nil, err
	}
	stats, statsErr := s.SystemStats(ctx, p)
	inbox, inboxErr := s.MISInboxSummary(ctx, p, QueryMISInboxInput{Limit: 500})
	out := &DashboardResult{
		Dashboard:   *dashboard,
		Reports:     make([]DashboardReport, 0, len(dashboard.ReportIDs)),
		GeneratedAt: s.now().UTC(),
	}
	if statsErr == nil {
		out.Stats = stats
	}
	if inboxErr == nil {
		out.InboxSummary = inbox
	}
	for _, reportID := range dashboard.ReportIDs {
		report, err := s.GetReport(ctx, p, reportID)
		item := DashboardReport{ReportID: reportID}
		if report != nil {
			item.Title = report.Title
		}
		if err == nil {
			result, runErr := s.RunReport(ctx, p, reportID, AggregateInput{})
			if runErr == nil {
				item.Result = result
			} else {
				item.Error = runErr.Error()
			}
		} else {
			item.Error = err.Error()
		}
		out.Reports = append(out.Reports, item)
	}
	applyDashboardResultPackage(out)
	return out, nil
}
