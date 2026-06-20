package structureddata

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type businessObjectDefinition struct {
	ObjectRole         string
	Title              string
	Description        string
	Domain             string
	DatasetID          string
	TemplateID         string
	AppIDs             []string
	BlueprintIDs       []string
	PreferredAction    string
	PreferredView      string
	PreferredReport    string
	PreferredDashboard string
	Required           bool
}

var businessObjectDefinitions = []businessObjectDefinition{
	{ObjectRole: "department", Title: "Department", Description: "Company department, cost center, manager, and hierarchy master data.", Domain: "company", DatasetID: "company.departments", TemplateID: "company.departments", AppIDs: []string{"mis.company"}, BlueprintIDs: []string{"mis.company"}, PreferredAction: "company.department_upsert", PreferredView: "company.department_directory", PreferredReport: "company.department_count_by_status", PreferredDashboard: "company.overview", Required: true},
	{ObjectRole: "customer", Title: "Customer", Description: "Customer master data for sales and service workflows.", Domain: "sales", DatasetID: "sales.customers", TemplateID: "sales.customers", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}, PreferredAction: "sales.customer_upsert", PreferredView: "sales.customer_directory", PreferredReport: "sales.revenue_by_customer", PreferredDashboard: "sales.overview"},
	{ObjectRole: "sales_order", Title: "Sales Order", Description: "Customer sales order, amount, owner, stage, and payment status.", Domain: "sales", DatasetID: "sales.orders", TemplateID: "sales.orders", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}, PreferredAction: "sales.order_upsert", PreferredView: "sales.order_overview", PreferredReport: "sales.order_summary_by_stage", PreferredDashboard: "sales.overview", Required: true},
	{ObjectRole: "sales_contact", Title: "Sales Contact", Description: "Customer contact person, role, communication details, owner, and lifecycle status.", Domain: "sales", DatasetID: "sales.contacts", TemplateID: "sales.contacts", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}, PreferredAction: "sales.contact_upsert", PreferredView: "sales.contact_directory", PreferredReport: "sales.contacts_by_status", PreferredDashboard: "sales.overview"},
	{ObjectRole: "sales_opportunity", Title: "Sales Opportunity", Description: "Sales opportunity pipeline, expected amount, probability, owner, and expected close date.", Domain: "sales", DatasetID: "sales.opportunities", TemplateID: "sales.opportunities", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}, PreferredAction: "sales.opportunity_upsert", PreferredView: "sales.opportunity_pipeline", PreferredReport: "sales.opportunity_pipeline_by_stage", PreferredDashboard: "sales.overview"},
	{ObjectRole: "expense_report", Title: "Expense Report", Description: "Employee reimbursement and operating expense record for approval and finance processing.", Domain: "finance", DatasetID: "finance.expenses", TemplateID: "finance.expenses", AppIDs: []string{"mis.expense"}, BlueprintIDs: []string{"mis.expense"}, PreferredAction: "finance.expense_submit", PreferredView: "finance.expense_review", PreferredReport: "finance.expense_by_department", PreferredDashboard: "finance.overview", Required: true},
	{ObjectRole: "invoice", Title: "Invoice", Description: "Issued and received invoice record with amount, tax, counterparty, and lifecycle status.", Domain: "finance", DatasetID: "finance.invoices", TemplateID: "finance.invoices", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.invoice_upsert", PreferredView: "finance.invoice_status", PreferredReport: "finance.invoice_status_summary", PreferredDashboard: "finance.overview"},
	{ObjectRole: "payment", Title: "Payment", Description: "Accounts payable and receivable payment record.", Domain: "finance", DatasetID: "finance.payments", TemplateID: "finance.payments", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.payment_upsert", PreferredView: "finance.payment_tracker", PreferredReport: "finance.payment_status_summary", PreferredDashboard: "finance.overview"},
	{ObjectRole: "budget", Title: "Budget", Description: "Department, category, period, budget amount, committed amount, and lifecycle status.", Domain: "finance", DatasetID: "finance.budgets", TemplateID: "finance.budgets", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.budget_upsert", PreferredView: "finance.budget_control", PreferredReport: "finance.budget_by_department", PreferredDashboard: "finance.overview"},
	{ObjectRole: "voucher", Title: "Accounting Voucher", Description: "Lightweight accounting voucher headers and lines for controlled finance posting workflows.", Domain: "finance", DatasetID: "finance.vouchers", TemplateID: "finance.vouchers", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.voucher_upsert", PreferredView: "finance.voucher_register", PreferredReport: "finance.voucher_status_summary", PreferredDashboard: "finance.overview"},
	{ObjectRole: "employee", Title: "Employee", Description: "Employee profile, organization, role, and employment status.", Domain: "hr", DatasetID: "hr.employees", TemplateID: "hr.employees", AppIDs: []string{"mis.hr", "mis.expense"}, BlueprintIDs: []string{"mis.hr", "mis.expense"}, PreferredAction: "hr.employee_upsert", PreferredView: "hr.employee_roster", PreferredReport: "hr.employee_count_by_department", PreferredDashboard: "hr.overview", Required: true},
	{ObjectRole: "leave_request", Title: "Leave Request", Description: "Employee leave and absence approval record.", Domain: "hr", DatasetID: "hr.leave_requests", TemplateID: "hr.leave_requests", AppIDs: []string{"mis.hr"}, BlueprintIDs: []string{"mis.hr"}, PreferredAction: "hr.leave_request_upsert", PreferredView: "hr.leave_request_review", PreferredReport: "hr.leave_by_status", PreferredDashboard: "hr.overview"},
	{ObjectRole: "contract", Title: "Contract", Description: "Contract lifecycle, amount, counterparty, owner, and expiration tracking.", Domain: "legal", DatasetID: "legal.contracts", TemplateID: "legal.contracts", AppIDs: []string{"mis.legal"}, BlueprintIDs: []string{"mis.legal"}, PreferredAction: "legal.contract_upsert", PreferredView: "legal.contract_register", PreferredReport: "legal.contract_value_by_status", PreferredDashboard: "legal.overview"},
	{ObjectRole: "supplier", Title: "Supplier", Description: "Supplier master data for procurement, finance, contracts, and payment workflows.", Domain: "procurement", DatasetID: "procurement.suppliers", TemplateID: "procurement.suppliers", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}, PreferredAction: "procurement.supplier_upsert", PreferredView: "procurement.supplier_directory", PreferredReport: "procurement.supplier_by_status", PreferredDashboard: "procurement.overview"},
	{ObjectRole: "purchase_order", Title: "Purchase Order", Description: "Supplier purchase order, amount, requester, approval, and fulfillment tracking.", Domain: "procurement", DatasetID: "procurement.purchase_orders", TemplateID: "procurement.purchase_orders", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}, PreferredAction: "procurement.purchase_order_upsert", PreferredView: "procurement.purchase_order_tracker", PreferredReport: "procurement.po_status_summary", PreferredDashboard: "procurement.overview", Required: true},
	{ObjectRole: "inventory_item", Title: "Inventory Item", Description: "Inventory item master and stock levels by warehouse.", Domain: "inventory", DatasetID: "inventory.items", TemplateID: "inventory.items", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}, PreferredAction: "inventory.item_upsert", PreferredView: "inventory.stock_overview", PreferredReport: "inventory.quantity_by_warehouse", PreferredDashboard: "inventory.overview", Required: true},
	{ObjectRole: "inventory_warehouse", Title: "Inventory Warehouse", Description: "Warehouse master data for stock, movement, transfer, and responsible-person tracking.", Domain: "inventory", DatasetID: "inventory.warehouses", TemplateID: "inventory.warehouses", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}, PreferredAction: "inventory.warehouse_upsert", PreferredView: "inventory.warehouse_directory", PreferredReport: "inventory.warehouse_by_status", PreferredDashboard: "inventory.overview"},
	{ObjectRole: "inventory_movement", Title: "Inventory Movement", Description: "Inventory receipt, issue, transfer, adjustment, reservation, and release movement record.", Domain: "inventory", DatasetID: "inventory.movements", TemplateID: "inventory.movements", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}, PreferredAction: "inventory.movement_record", PreferredView: "inventory.movement_ledger", PreferredReport: "inventory.movement_by_type", PreferredDashboard: "inventory.overview", Required: true},
	{ObjectRole: "fixed_asset", Title: "Fixed Asset", Description: "Company asset register, ownership, depreciation, and lifecycle status.", Domain: "assets", DatasetID: "assets.fixed_assets", TemplateID: "assets.fixed_assets", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}, PreferredAction: "assets.fixed_asset_upsert", PreferredView: "assets.asset_register", PreferredReport: "assets.value_by_department", PreferredDashboard: "assets.overview"},
}

func (s *Service) ListBusinessObjects(ctx context.Context, p Principal, in QueryBusinessObjectsInput) ([]BusinessObjectCatalog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	datasets, err := s.store.ListDatasets(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	items, err := s.businessObjectCatalogsLocked(ctx, p, datasetMapByID(datasets), in)
	if err != nil {
		return nil, err
	}
	return paginateBusinessObjects(items, in), nil
}

func (s *Service) ResolveObjectRole(ctx context.Context, p Principal, in ResolveObjectRoleInput) (*ResolveObjectRoleResult, error) {
	objectRole := strings.ToLower(strings.TrimSpace(in.ObjectRole))
	if objectRole == "" {
		return nil, ErrInvalidInput
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	datasets, err := s.store.ListDatasets(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	query := QueryBusinessObjectsInput{AppID: in.AppID, BlueprintID: in.BlueprintID, ObjectRole: objectRole, Limit: 2}
	items, err := s.businessObjectCatalogsLocked(ctx, p, datasetMapByID(datasets), query)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrDatasetNotFound
	}
	item := items[0]
	if in.RequireInitialized && !item.Initialized {
		return nil, fmt.Errorf("%w: object role %s is not initialized", ErrDatasetNotFound, objectRole)
	}
	appID := strings.TrimSpace(in.AppID)
	if appID == "" {
		appID = item.RoleBinding.AppID
	}
	blueprintID := strings.TrimSpace(in.BlueprintID)
	if blueprintID == "" {
		blueprintID = item.RoleBinding.BlueprintID
	}
	return &ResolveObjectRoleResult{
		ObjectRole:         item.ObjectRole,
		AppID:              appID,
		BlueprintID:        blueprintID,
		DatasetID:          item.DatasetID,
		TemplateID:         item.TemplateID,
		Initialized:        item.Initialized,
		RoleBinding:        item.RoleBinding,
		BusinessObject:     item,
		RecommendedActions: recommendedObjectActions(item),
	}, nil
}

func (s *Service) businessObjectCatalogsLocked(ctx context.Context, p Principal, datasetByID map[string]Dataset, in QueryBusinessObjectsInput) ([]BusinessObjectCatalog, error) {
	out := make([]BusinessObjectCatalog, 0, len(businessObjectDefinitions))
	installedRoleKeys := map[string]struct{}{}
	installations, err := s.store.ListAppInstallations(ctx, p.TenantID, QueryAppInstallationsInput{AppID: strings.TrimSpace(in.AppID), BlueprintID: strings.TrimSpace(in.BlueprintID), Status: defaultAppInstallationStatus, Limit: 500})
	if err != nil {
		return nil, err
	}
	for _, installation := range installations {
		for _, binding := range installation.RoleBindings {
			def := businessObjectDefinitionForRoleBinding(binding)
			if !businessObjectDefinitionMatches(def, in) {
				continue
			}
			installedRoleKeys[businessObjectAppRoleKey(binding.AppID, binding.ObjectRole)] = struct{}{}
			if p.Policy != nil && (!principalCanUseDomain(p, def.Domain) || !principalCanUseDataset(p, def.DatasetID)) {
				continue
			}
			item, err := s.businessObjectCatalogLocked(ctx, p, datasetByID, def, in)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
	}
	for _, def := range businessObjectDefinitions {
		if staticBusinessObjectOverridden(def, in, installedRoleKeys) || !businessObjectDefinitionMatches(def, in) {
			continue
		}
		if p.Policy != nil && (!principalCanUseDomain(p, def.Domain) || !principalCanUseDataset(p, def.DatasetID)) {
			continue
		}
		item, err := s.businessObjectCatalogLocked(ctx, p, datasetByID, def, in)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return dedupeBusinessObjectCatalogs(out), nil
}

func businessObjectDefinitionForRoleBinding(binding RoleBinding) businessObjectDefinition {
	def, found := businessObjectDefinitionByRole(binding.ObjectRole)
	if !found {
		def = businessObjectDefinition{ObjectRole: strings.ToLower(strings.TrimSpace(binding.ObjectRole)), Title: businessObjectRoleTitle(binding.ObjectRole), Description: "Installed app business object binding."}
	}
	def.ObjectRole = strings.ToLower(strings.TrimSpace(binding.ObjectRole))
	def.Domain = strings.ToLower(strings.TrimSpace(binding.Domain))
	def.DatasetID = strings.ToLower(strings.TrimSpace(binding.DatasetID))
	def.TemplateID = strings.ToLower(strings.TrimSpace(binding.TemplateID))
	def.AppIDs = singleNonEmptyString(binding.AppID)
	def.BlueprintIDs = singleNonEmptyString(binding.BlueprintID)
	def.Required = binding.Required
	base, baseFound := businessObjectDefinitionByRole(binding.ObjectRole)
	if !baseFound || base.DatasetID != def.DatasetID {
		def.PreferredAction = ""
		def.PreferredView = ""
		def.PreferredReport = ""
		def.PreferredDashboard = ""
	}
	return def
}

func businessObjectDefinitionByRole(objectRole string) (businessObjectDefinition, bool) {
	objectRole = strings.ToLower(strings.TrimSpace(objectRole))
	for _, def := range businessObjectDefinitions {
		if def.ObjectRole == objectRole {
			return def, true
		}
	}
	return businessObjectDefinition{}, false
}

func staticBusinessObjectOverridden(def businessObjectDefinition, in QueryBusinessObjectsInput, installedRoleKeys map[string]struct{}) bool {
	appID := strings.TrimSpace(in.AppID)
	if appID == "" {
		appID = firstMatchingOrFirst(def.AppIDs, "")
	}
	_, ok := installedRoleKeys[businessObjectAppRoleKey(appID, def.ObjectRole)]
	return ok
}

func businessObjectAppRoleKey(appID, objectRole string) string {
	return strings.TrimSpace(appID) + "|" + strings.ToLower(strings.TrimSpace(objectRole))
}

func singleNonEmptyString(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}

func businessObjectRoleTitle(objectRole string) string {
	words := strings.Fields(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(objectRole)), "_", " "))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func dedupeBusinessObjectCatalogs(items []BusinessObjectCatalog) []BusinessObjectCatalog {
	out := make([]BusinessObjectCatalog, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		key := businessObjectCursorKey(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (s *Service) businessObjectCatalogLocked(ctx context.Context, p Principal, datasetByID map[string]Dataset, def businessObjectDefinition, in QueryBusinessObjectsInput) (BusinessObjectCatalog, error) {
	binding := roleBindingForDefinition(def, in)
	item := BusinessObjectCatalog{
		ObjectRole:         def.ObjectRole,
		Title:              def.Title,
		Description:        def.Description,
		Domain:             def.Domain,
		DatasetID:          def.DatasetID,
		TemplateID:         def.TemplateID,
		AppIDs:             append([]string(nil), def.AppIDs...),
		BlueprintIDs:       append([]string(nil), def.BlueprintIDs...),
		RoleBinding:        binding,
		PreferredAction:    def.PreferredAction,
		PreferredView:      def.PreferredView,
		PreferredReport:    def.PreferredReport,
		PreferredDashboard: def.PreferredDashboard,
	}
	if dataset, ok := datasetByID[def.DatasetID]; ok {
		item.Initialized = true
		ds := dataset
		item.Dataset = &ds
		fields, err := s.store.ListFields(ctx, p.TenantID, def.DatasetID)
		if err != nil {
			return BusinessObjectCatalog{}, err
		}
		item.Fields = append([]FieldDefinition(nil), fields...)
	}
	if tmpl, ok := templateByID(def.TemplateID); ok {
		item.Template = &tmpl
		item.InputFields = append([]DatasetTemplateField(nil), tmpl.Fields...)
		if !item.Initialized {
			item.MissingTemplates = []string{tmpl.ID}
		}
	}
	item.BusinessActions = businessActionsForObject(p, def.DatasetID)
	item.BusinessViews = businessViewsForObject(p, def.DatasetID)
	item.Reports = reportsForObject(p, def.DatasetID)
	item.Dashboards = dashboardsForObject(p, def.Domain, def.PreferredDashboard)
	return item, nil
}

func datasetMapByID(items []Dataset) map[string]Dataset {
	out := make(map[string]Dataset, len(items))
	for _, item := range items {
		out[strings.TrimSpace(item.ID)] = item
	}
	return out
}

func businessObjectDefinitionMatches(def businessObjectDefinition, in QueryBusinessObjectsInput) bool {
	if objectRole := strings.ToLower(strings.TrimSpace(in.ObjectRole)); objectRole != "" && objectRole != def.ObjectRole {
		return false
	}
	if domain := strings.ToLower(strings.TrimSpace(in.Domain)); domain != "" && domain != def.Domain {
		return false
	}
	if appID := strings.TrimSpace(in.AppID); appID != "" && !containsTrimmedString(def.AppIDs, appID) {
		return false
	}
	if blueprintID := strings.TrimSpace(in.BlueprintID); blueprintID != "" && !containsTrimmedString(def.BlueprintIDs, blueprintID) {
		return false
	}
	return true
}

func roleBindingForDefinition(def businessObjectDefinition, in QueryBusinessObjectsInput) RoleBinding {
	appID := firstMatchingOrFirst(def.AppIDs, in.AppID)
	blueprintID := firstMatchingOrFirst(def.BlueprintIDs, in.BlueprintID)
	return RoleBinding{
		AppID:       appID,
		BlueprintID: blueprintID,
		ObjectRole:  def.ObjectRole,
		Domain:      def.Domain,
		DatasetID:   def.DatasetID,
		TemplateID:  def.TemplateID,
		Required:    def.Required,
	}
}

func firstMatchingOrFirst(items []string, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" && containsTrimmedString(items, requested) {
		return requested
	}
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			return item
		}
	}
	return ""
}

func containsTrimmedString(items []string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, item := range items {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

func templateByID(templateID string) (DatasetTemplate, bool) {
	templateID = strings.TrimSpace(templateID)
	for _, tmpl := range datasetTemplates {
		if tmpl.ID != templateID {
			continue
		}
		clone := tmpl
		clone.Fields = append([]DatasetTemplateField(nil), tmpl.Fields...)
		clone.SampleData = append([]map[string]interface{}(nil), tmpl.SampleData...)
		return clone, true
	}
	return DatasetTemplate{}, false
}

func businessActionsForObject(p Principal, datasetID string) []BusinessAction {
	out := []BusinessAction{}
	for _, action := range businessActions {
		if strings.TrimSpace(action.DatasetID) != datasetID {
			continue
		}
		if p.Policy != nil && (!principalCanUseAction(p, action.ID) || !principalCanUseDomain(p, action.Domain)) {
			continue
		}
		clone := action
		clone.RequiredFields = append([]string(nil), action.RequiredFields...)
		clone.SuggestedTags = append([]string(nil), action.SuggestedTags...)
		clone.InputFields = append([]DatasetTemplateField(nil), action.InputFields...)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func businessViewsForObject(p Principal, datasetID string) []BusinessViewDefinition {
	out := []BusinessViewDefinition{}
	for _, view := range businessViews {
		if strings.TrimSpace(view.DatasetID) != datasetID {
			continue
		}
		if p.Policy != nil && (!principalCanUseView(p, view.ID) || !principalCanUseDomain(p, view.Domain)) {
			continue
		}
		out = append(out, cloneBusinessView(view))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func reportsForObject(p Principal, datasetID string) []ReportDefinition {
	out := []ReportDefinition{}
	for _, report := range reportDefinitions {
		if strings.TrimSpace(report.DatasetID) != datasetID {
			continue
		}
		if p.Policy != nil && (!principalCanUseReport(p, report.ID) || !principalCanUseDomain(p, report.Domain)) {
			continue
		}
		clone := report
		clone.Aggregate.GroupBy = append([]string(nil), report.Aggregate.GroupBy...)
		clone.Aggregate.Metrics = append([]AggregateMetric(nil), report.Aggregate.Metrics...)
		clone.Aggregate.Sort = append([]SortSpec(nil), report.Aggregate.Sort...)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func dashboardsForObject(p Principal, domain string, preferredDashboard string) []DashboardDefinition {
	out := []DashboardDefinition{}
	for _, dashboard := range dashboardDefinitions {
		if strings.TrimSpace(dashboard.ID) != preferredDashboard && strings.TrimSpace(dashboard.Domain) != domain {
			continue
		}
		if p.Policy != nil && (!principalCanUseDashboard(p, dashboard.ID) || !principalCanUseDomain(p, dashboard.Domain)) {
			continue
		}
		clone := dashboard
		clone.ReportIDs = append([]string(nil), dashboard.ReportIDs...)
		out = append(out, clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func recommendedObjectActions(item BusinessObjectCatalog) []string {
	out := []string{}
	if item.PreferredAction != "" {
		out = append(out, item.PreferredAction)
	}
	for _, action := range item.BusinessActions {
		out = append(out, action.ID)
	}
	return uniqueCapabilityStrings(out)
}

func paginateBusinessObjects(items []BusinessObjectCatalog, in QueryBusinessObjectsInput) []BusinessObjectCatalog {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]BusinessObjectCatalog(nil), items...)
	sort.Slice(out, func(i, j int) bool { return businessObjectCursorKey(out[i]) > businessObjectCursorKey(out[j]) })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, item := range out {
			if businessObjectCursorKey(item) < beforeID {
				filtered = append(filtered, item)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func businessObjectCursorKey(item BusinessObjectCatalog) string {
	return strings.Join([]string{strings.TrimSpace(item.RoleBinding.AppID), strings.TrimSpace(item.ObjectRole), strings.TrimSpace(item.DatasetID)}, "|")
}
