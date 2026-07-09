package structureddata

import (
	"context"
	"sort"
	"strings"
)

func (s *Service) ListBusinessDomains(ctx context.Context, p Principal, query ...QueryBusinessDomainsInput) ([]BusinessDomainCatalog, error) {
	domains, err := s.businessDomainNames(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]BusinessDomainCatalog, 0, len(domains))
	for _, domain := range domains {
		catalog, err := s.GetBusinessDomain(ctx, p, domain)
		if err != nil {
			return nil, err
		}
		out = append(out, *catalog)
	}
	if len(query) == 0 {
		return out, nil
	}
	return paginateBusinessDomains(out, query[0]), nil
}

func paginateBusinessDomains(items []BusinessDomainCatalog, in QueryBusinessDomainsInput) []BusinessDomainCatalog {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]BusinessDomainCatalog(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].Domain > out[j].Domain })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, domain := range out {
			if domain.Domain < beforeID {
				filtered = append(filtered, domain)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) GetBusinessDomain(ctx context.Context, p Principal, domain string) (*BusinessDomainCatalog, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, ErrInvalidInput
	}
	datasets, err := s.store.ListDatasets(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	datasetCaps := make([]DatasetCapability, 0)
	existingDatasetIDs := map[string]struct{}{}
	for _, dataset := range datasets {
		existingDatasetIDs[dataset.ID] = struct{}{}
		if strings.ToLower(strings.TrimSpace(dataset.Domain)) != domain {
			continue
		}
		fields, err := s.store.ListFields(ctx, p.TenantID, dataset.ID)
		if err != nil {
			return nil, err
		}
		datasetCaps = append(datasetCaps, DatasetCapability{Dataset: dataset, Fields: fields})
	}
	sort.Slice(datasetCaps, func(i, j int) bool { return datasetCaps[i].Dataset.ID < datasetCaps[j].Dataset.ID })

	catalog := &BusinessDomainCatalog{
		Domain:          domain,
		Title:           businessDomainTitle(domain),
		Initialized:     len(datasetCaps) > 0,
		UseCases:        businessDomainUseCases(domain),
		Datasets:        datasetCaps,
		Templates:       templatesForDomain(domain),
		BusinessActions: businessActionsForDomain(domain),
		BusinessViews:   businessViewsForDomain(domain),
		Dashboards:      dashboardsForDomain(domain),
		Reports:         reportsForDomain(domain),
	}
	for _, tmpl := range catalog.Templates {
		if _, ok := existingDatasetIDs[tmpl.ID]; !ok {
			catalog.MissingTemplates = append(catalog.MissingTemplates, tmpl.ID)
		}
	}
	if len(catalog.Templates) == 0 && len(catalog.BusinessActions) == 0 && len(catalog.BusinessViews) == 0 && len(catalog.Dashboards) == 0 && len(catalog.Reports) == 0 && len(catalog.Datasets) == 0 {
		return nil, ErrDatasetNotFound
	}
	return catalog, nil
}

func (s *Service) businessDomainNames(ctx context.Context, p Principal) ([]string, error) {
	domainSet := map[string]struct{}{}
	datasets, err := s.store.ListDatasets(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	for _, dataset := range datasets {
		addDomainName(domainSet, dataset.Domain)
	}
	for _, tmpl := range datasetTemplates {
		addDomainName(domainSet, tmpl.Domain)
	}
	for _, action := range businessActions {
		addDomainName(domainSet, action.Domain)
	}
	for _, view := range businessViews {
		addDomainName(domainSet, view.Domain)
	}
	for _, dashboard := range dashboardDefinitions {
		addDomainName(domainSet, dashboard.Domain)
	}
	for _, report := range reportDefinitions {
		addDomainName(domainSet, report.Domain)
	}
	out := make([]string, 0, len(domainSet))
	for domain := range domainSet {
		out = append(out, domain)
	}
	sort.Strings(out)
	return out, nil
}

func addDomainName(out map[string]struct{}, domain string) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain != "" {
		out[domain] = struct{}{}
	}
}

func businessDomainTitle(domain string) string {
	switch domain {
	case "assets":
		return "Fixed Assets"
	case "company":
		return "Company Overview"
	case "finance":
		return "Finance"
	case "hr":
		return "Human Resources"
	case "inventory":
		return "Inventory"
	case "legal":
		return "Legal"
	case "procurement":
		return "Procurement"
	case "sales":
		return "Sales"
	default:
		if domain == "" {
			return ""
		}
		return strings.ToUpper(domain[:1]) + domain[1:]
	}
}

func businessDomainUseCases(domain string) []BusinessDomainUseCase {
	useCases := map[string][]BusinessDomainUseCase{
		"sales": {
			{ID: "sales.opportunity_pipeline", Title: "Maintain sales opportunity", Description: "Create or update opportunity pipeline stage, expected amount, probability, owner, and close date.", IntentHints: []string{"opportunity", "pipeline", "deal", "lead", "商机", "销售机会", "销售线索"}, PreferredAction: "sales.opportunity_upsert", PreferredView: "sales.opportunity_pipeline", PreferredReport: "sales.opportunity_pipeline_by_stage", PreferredDashboard: "sales.overview", DryRunRecommended: true},
			{ID: "sales.record_order", Title: "Record or update sales order", Description: "Create or update order amount, customer, owner, stage, and payment status.", IntentHints: []string{"new order", "update order", "sales changed", "客户订单", "销售订单"}, PreferredAction: "sales.order_upsert", PreferredView: "sales.order_overview", PreferredReport: "sales.order_summary_by_stage", PreferredDashboard: "sales.overview", DryRunRecommended: true},
			{ID: "sales.update_order_status", Title: "Update sales order status", Description: "Change stage, payment status, owner, or delivery status without replacing the whole order.", IntentHints: []string{"order status", "payment status", "delivery status", "订单状态", "回款状态"}, PreferredAction: "sales.order_status_update", PreferredView: "sales.order_overview", PreferredReport: "sales.order_summary_by_stage", PreferredDashboard: "sales.overview", DryRunRecommended: true},
			{ID: "sales.customer_directory", Title: "Maintain customer directory", Description: "Create or update customer master data and inspect customer list.", IntentHints: []string{"customer", "客户", "客户资料", "客户名录"}, PreferredAction: "sales.customer_upsert", PreferredView: "sales.customer_directory", PreferredReport: "sales.revenue_by_customer", PreferredDashboard: "sales.overview", DryRunRecommended: true},
			{ID: "sales.contact_directory", Title: "Maintain sales contacts", Description: "Create or update customer contact people, role, communication details, owner, and lifecycle status.", IntentHints: []string{"contact", "customer contact", "联系人", "客户联系人", "销售联系人"}, PreferredAction: "sales.contact_upsert", PreferredView: "sales.contact_directory", PreferredReport: "sales.contacts_by_status", PreferredDashboard: "sales.overview", DryRunRecommended: true},
		},
		"finance": {
			{ID: "finance.submit_expense", Title: "Submit or update expense", Description: "Record reimbursement or operating expense data for finance review.", IntentHints: []string{"expense", "reimbursement", "费用", "报销"}, PreferredAction: "finance.expense_submit", PreferredView: "finance.expense_review", PreferredReport: "finance.expense_by_department", PreferredDashboard: "finance.overview", DryRunRecommended: true},
			{ID: "finance.invoice_lifecycle", Title: "Maintain invoice lifecycle", Description: "Create or update invoice amount, tax, counterparty, status, and issue date.", IntentHints: []string{"invoice", "发票", "开票", "收票"}, PreferredAction: "finance.invoice_upsert", PreferredView: "finance.invoice_status", PreferredReport: "finance.invoice_status_summary", PreferredDashboard: "finance.overview", DryRunRecommended: true},
			{ID: "finance.payment_lifecycle", Title: "Maintain payment lifecycle", Description: "Create, approve, pay, receive, fail, or cancel payable and receivable payment records.", IntentHints: []string{"payment", "cashflow", "accounts payable", "accounts receivable", "付款", "收款", "回款"}, PreferredAction: "finance.payment_upsert", PreferredView: "finance.payment_tracker", PreferredReport: "finance.payment_status_summary", PreferredDashboard: "finance.overview", DryRunRecommended: true},
			{ID: "finance.chart_of_accounts", Title: "Maintain chart of accounts", Description: "Create or update lightweight account codes and account types for voucher classification.", IntentHints: []string{"chart of accounts", "account code", "accounting subject", "科目", "会计科目"}, PreferredAction: "finance.account_upsert", PreferredView: "finance.account_directory", PreferredReport: "finance.account_count_by_type", PreferredDashboard: "finance.overview", DryRunRecommended: true},
			{ID: "finance.budget_control", Title: "Maintain budget control", Description: "Create, adjust, approve, activate, freeze, close, or analyze department/category budgets.", IntentHints: []string{"budget", "budget control", "department budget", "预算", "预算控制", "部门预算"}, PreferredAction: "finance.budget_upsert", PreferredView: "finance.budget_control", PreferredReport: "finance.budget_by_department", PreferredDashboard: "finance.overview", DryRunRecommended: true},
			{ID: "finance.voucher_lifecycle", Title: "Maintain accounting voucher", Description: "Create, review, approve, post, reverse, or void lightweight accounting vouchers.", IntentHints: []string{"voucher", "journal", "ledger", "posting", "会计凭证", "凭证", "记账"}, PreferredAction: "finance.voucher_upsert", PreferredView: "finance.voucher_register", PreferredReport: "finance.voucher_status_summary", PreferredDashboard: "finance.overview", DryRunRecommended: true},
		},
		"hr": {
			{ID: "hr.employee_profile", Title: "Maintain employee profile", Description: "Create or update employee roster, department, position, manager, and employment status.", IntentHints: []string{"employee", "staff", "员工", "人事", "入职", "离职"}, PreferredAction: "hr.employee_upsert", PreferredView: "hr.employee_roster", PreferredReport: "hr.employee_count_by_department", PreferredDashboard: "hr.overview", DryRunRecommended: true},
			{ID: "hr.payroll_processing", Title: "Process payroll", Description: "Create, adjust, review, approve, and pay monthly payroll lines.", IntentHints: []string{"payroll", "salary", "wage", "pay run", "薪资", "工资", "发薪"}, PreferredAction: "hr.payroll_upsert", PreferredView: "hr.payroll_review", PreferredReport: "hr.payroll_status_summary", PreferredDashboard: "hr.overview", DryRunRecommended: true},
			{ID: "hr.leave_request", Title: "Manage leave requests", Description: "Create, review, approve, reject, cancel, or close employee leave and absence requests.", IntentHints: []string{"leave", "vacation", "absence", "sick leave", "business trip", "remote work"}, PreferredAction: "hr.leave_request_upsert", PreferredView: "hr.leave_request_review", PreferredReport: "hr.leave_by_status", PreferredDashboard: "hr.overview", DryRunRecommended: true},
			{ID: "hr.headcount_report", Title: "Analyze headcount", Description: "Read headcount grouped by department.", IntentHints: []string{"headcount", "organization", "人数", "部门人数"}, PreferredView: "hr.employee_roster", PreferredReport: "hr.employee_count_by_department", PreferredDashboard: "hr.overview"},
		},
		"legal": {
			{ID: "legal.contract_register", Title: "Maintain contract register", Description: "Create or update contract lifecycle, amount, owner, counterparty, and expiration.", IntentHints: []string{"contract", "合同", "签约", "到期"}, PreferredAction: "legal.contract_upsert", PreferredView: "legal.contract_register", PreferredReport: "legal.contract_value_by_status", PreferredDashboard: "legal.overview", DryRunRecommended: true},
		},
		"procurement": {
			{ID: "procurement.purchase_order", Title: "Maintain purchase order", Description: "Create or update supplier purchase order amount, requester, department, category, and expected date.", IntentHints: []string{"purchase order", "procurement", "采购订单", "采购"}, PreferredAction: "procurement.purchase_order_upsert", PreferredView: "procurement.purchase_order_tracker", PreferredReport: "procurement.po_status_summary", PreferredDashboard: "procurement.overview", DryRunRecommended: true},
			{ID: "procurement.supplier_directory", Title: "Maintain supplier directory", Description: "Create or update supplier master data, payment terms, tax number, contact, and lifecycle status.", IntentHints: []string{"supplier", "vendor", "payment terms", "供应商", "供方", "付款条件"}, PreferredAction: "procurement.supplier_upsert", PreferredView: "procurement.supplier_directory", PreferredReport: "procurement.supplier_by_status", PreferredDashboard: "procurement.overview", DryRunRecommended: true},
			{ID: "procurement.purchase_status", Title: "Update purchase order status", Description: "Change approval, ordering, receiving, cancellation, or closing status.", IntentHints: []string{"purchase status", "received", "采购状态", "到货", "收货"}, PreferredAction: "procurement.purchase_order_status_update", PreferredView: "procurement.purchase_order_tracker", PreferredReport: "procurement.po_amount_by_supplier", PreferredDashboard: "procurement.overview", DryRunRecommended: true},
		},
		"inventory": {
			{ID: "inventory.item_master", Title: "Maintain inventory item", Description: "Create or update item master data and warehouse stock levels.", IntentHints: []string{"inventory item", "stock item", "物料", "库存物料"}, PreferredAction: "inventory.item_upsert", PreferredView: "inventory.stock_overview", PreferredReport: "inventory.quantity_by_warehouse", PreferredDashboard: "inventory.overview", DryRunRecommended: true},
			{ID: "inventory.warehouse_directory", Title: "Maintain warehouse directory", Description: "Create or update warehouse master data, location, manager, and lifecycle status.", IntentHints: []string{"warehouse", "storage location", "库房", "仓库", "仓储"}, PreferredAction: "inventory.warehouse_upsert", PreferredView: "inventory.warehouse_directory", PreferredReport: "inventory.warehouse_by_status", PreferredDashboard: "inventory.overview", DryRunRecommended: true},
			{ID: "inventory.stock_update", Title: "Update stock movement or quantity", Description: "Update quantity, warehouse, status, and movement time without replacing item master data.", IntentHints: []string{"stock update", "low stock", "库存变动", "库存预警", "低库存"}, PreferredAction: "inventory.stock_update", PreferredView: "inventory.stock_overview", PreferredReport: "inventory.low_stock_summary", PreferredDashboard: "inventory.overview", DryRunRecommended: true},
			{ID: "inventory.movement_ledger", Title: "Record inventory movement", Description: "Record receipt, issue, transfer, adjustment, reservation, release, or posting movements for traceability.", IntentHints: []string{"inventory movement", "stock movement", "receipt", "issue", "transfer", "入库", "出库", "调拨", "库存流水"}, PreferredAction: "inventory.movement_record", PreferredView: "inventory.movement_ledger", PreferredReport: "inventory.movement_by_type", PreferredDashboard: "inventory.overview", DryRunRecommended: true},
		},
		"assets": {
			{ID: "assets.fixed_asset_register", Title: "Register fixed asset", Description: "Create or update asset register, custodian, department, cost, status, and location.", IntentHints: []string{"fixed asset", "asset register", "固定资产", "资产登记"}, PreferredAction: "assets.fixed_asset_upsert", PreferredView: "assets.asset_register", PreferredReport: "assets.value_by_department", PreferredDashboard: "assets.overview", DryRunRecommended: true},
			{ID: "assets.fixed_asset_status", Title: "Update fixed asset status", Description: "Change asset lifecycle, transfer, maintenance, disposal, loss, custodian, or location.", IntentHints: []string{"asset status", "asset transfer", "资产状态", "资产调拨", "资产处置"}, PreferredAction: "assets.fixed_asset_status_update", PreferredView: "assets.asset_register", PreferredReport: "assets.status_summary", PreferredDashboard: "assets.overview", DryRunRecommended: true},
		},
		"company": {
			{ID: "company.overview", Title: "Company MIS overview", Description: "Read company-level operational health, inbox summary, and core business metrics.", IntentHints: []string{"company overview", "MIS overview", "公司数据", "经营概览"}, PreferredDashboard: "company.overview"},
			{ID: "company.department_directory", Title: "Maintain department directory", Description: "Create or update department hierarchy, manager, cost center, and department status.", IntentHints: []string{"department", "organization", "cost center", "部门", "组织架构", "成本中心"}, PreferredAction: "company.department_upsert", PreferredView: "company.department_directory", PreferredReport: "company.department_count_by_status", PreferredDashboard: "company.overview", DryRunRecommended: true},
		},
	}
	out := append([]BusinessDomainUseCase(nil), useCases[domain]...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func templatesForDomain(domain string) []DatasetTemplate {
	out := []DatasetTemplate{}
	for _, tmpl := range datasetTemplates {
		if strings.ToLower(strings.TrimSpace(tmpl.Domain)) == domain {
			clone := tmpl
			clone.Fields = append([]DatasetTemplateField(nil), tmpl.Fields...)
			clone.SampleData = append([]map[string]interface{}(nil), tmpl.SampleData...)
			out = append(out, clone)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func businessActionsForDomain(domain string) []BusinessAction {
	out := []BusinessAction{}
	for _, action := range businessActions {
		if strings.ToLower(strings.TrimSpace(action.Domain)) == domain {
			clone := action
			clone.RequiredFields = append([]string(nil), action.RequiredFields...)
			clone.SuggestedTags = append([]string(nil), action.SuggestedTags...)
			clone.InputFields = append([]DatasetTemplateField(nil), action.InputFields...)
			out = append(out, clone)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func businessViewsForDomain(domain string) []BusinessViewDefinition {
	out := []BusinessViewDefinition{}
	for _, view := range businessViews {
		if strings.ToLower(strings.TrimSpace(view.Domain)) == domain {
			out = append(out, cloneBusinessView(view))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dashboardsForDomain(domain string) []DashboardDefinition {
	out := []DashboardDefinition{}
	for _, dashboard := range dashboardDefinitions {
		if strings.ToLower(strings.TrimSpace(dashboard.Domain)) == domain {
			clone := dashboard
			clone.ReportIDs = append([]string(nil), dashboard.ReportIDs...)
			out = append(out, clone)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func reportsForDomain(domain string) []ReportDefinition {
	out := []ReportDefinition{}
	for _, report := range reportDefinitions {
		if strings.ToLower(strings.TrimSpace(report.Domain)) == domain {
			clone := report
			clone.Aggregate.GroupBy = append([]string(nil), report.Aggregate.GroupBy...)
			clone.Aggregate.Metrics = append([]AggregateMetric(nil), report.Aggregate.Metrics...)
			clone.Aggregate.Sort = append([]SortSpec(nil), report.Aggregate.Sort...)
			out = append(out, clone)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
