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
	{ObjectRole: "sales_order", Title: "Sales Order", Description: "Customer sales order header: amount, owner, stage, and payment status. Line items live in sales.order_lines.", Domain: "sales", DatasetID: "sales.orders", TemplateID: "sales.orders", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}, PreferredAction: "sales.order_upsert", PreferredView: "sales.order_overview", PreferredReport: "sales.order_summary_by_stage", PreferredDashboard: "sales.overview", Required: true},
	{ObjectRole: "sales_order_line", Title: "Sales Order Line", Description: "Sales order line: item, quantity, unit price, and amount, referenced to the order header.", Domain: "sales", DatasetID: "sales.order_lines", TemplateID: "sales.order_lines", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}},
	{ObjectRole: "sales_quote", Title: "Sales Quote", Description: "Customer sales quotation header. Line items live in sales.quote_lines.", Domain: "sales", DatasetID: "sales.quotes", TemplateID: "sales.quotes", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}},
	{ObjectRole: "sales_quote_line", Title: "Sales Quote Line", Description: "Sales quotation line: item, quantity, unit price, and amount.", Domain: "sales", DatasetID: "sales.quote_lines", TemplateID: "sales.quote_lines", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}},
	{ObjectRole: "sales_shipment", Title: "Sales Shipment", Description: "Outbound delivery header against a customer sales order. Line quantities live in sales.shipment_lines.", Domain: "sales", DatasetID: "sales.shipments", TemplateID: "sales.shipments", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}},
	{ObjectRole: "sales_shipment_line", Title: "Sales Shipment Line", Description: "Outbound shipment line quantity linked to the shipment header and item master.", Domain: "sales", DatasetID: "sales.shipment_lines", TemplateID: "sales.shipment_lines", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}},
	{ObjectRole: "sales_return", Title: "Sales Return", Description: "Customer sales return header. Line quantities live in sales.return_lines.", Domain: "sales", DatasetID: "sales.returns", TemplateID: "sales.returns", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}},
	{ObjectRole: "sales_return_line", Title: "Sales Return Line", Description: "Sales return line: item, quantity, unit price, and amount.", Domain: "sales", DatasetID: "sales.return_lines", TemplateID: "sales.return_lines", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}},
	{ObjectRole: "service_ticket", Title: "After-sales Service Ticket", Description: "After-sales work order linked to a customer, originating sales order, item, or installed asset.", Domain: "sales", DatasetID: "sales.service_tickets", TemplateID: "sales.service_tickets", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}, PreferredAction: "sales.service_ticket_upsert", PreferredView: "sales.service_ticket_queue", PreferredDashboard: "sales.overview"},
	{ObjectRole: "project", Title: "Project", Description: "Company project master. One catalog per tenant.", Domain: "company", DatasetID: "company.projects", TemplateID: "company.projects", AppIDs: []string{"mis.company"}, BlueprintIDs: []string{"mis.company"}, PreferredAction: "company.project_upsert", PreferredView: "company.project_directory", PreferredDashboard: "company.overview"},
	{ObjectRole: "sales_contact", Title: "Sales Contact", Description: "Customer contact person, role, communication details, owner, and lifecycle status.", Domain: "sales", DatasetID: "sales.contacts", TemplateID: "sales.contacts", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}, PreferredAction: "sales.contact_upsert", PreferredView: "sales.contact_directory", PreferredReport: "sales.contacts_by_status", PreferredDashboard: "sales.overview"},
	{ObjectRole: "sales_opportunity", Title: "Sales Opportunity", Description: "Sales opportunity pipeline, expected amount, probability, owner, and expected close date.", Domain: "sales", DatasetID: "sales.opportunities", TemplateID: "sales.opportunities", AppIDs: []string{"mis.sales"}, BlueprintIDs: []string{"mis.sales"}, PreferredAction: "sales.opportunity_upsert", PreferredView: "sales.opportunity_pipeline", PreferredReport: "sales.opportunity_pipeline_by_stage", PreferredDashboard: "sales.overview"},
	{ObjectRole: "expense_report", Title: "Expense Report", Description: "Employee reimbursement and operating expense record for approval and finance processing.", Domain: "finance", DatasetID: "finance.expenses", TemplateID: "finance.expenses", AppIDs: []string{"mis.expense"}, BlueprintIDs: []string{"mis.expense"}, PreferredAction: "finance.expense_submit", PreferredView: "finance.expense_review", PreferredReport: "finance.expense_by_department", PreferredDashboard: "finance.overview", Required: true},
	{ObjectRole: "invoice", Title: "Invoice", Description: "Issued and received invoice header with amount, tax, counterparty, and lifecycle status. Line items live in finance.invoice_lines.", Domain: "finance", DatasetID: "finance.invoices", TemplateID: "finance.invoices", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.invoice_upsert", PreferredView: "finance.invoice_status", PreferredReport: "finance.invoice_status_summary", PreferredDashboard: "finance.overview"},
	{ObjectRole: "invoice_line", Title: "Invoice Line", Description: "Invoice line: item, quantity, unit price, tax, and amount, referenced to the invoice header.", Domain: "finance", DatasetID: "finance.invoice_lines", TemplateID: "finance.invoice_lines", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}},
	{ObjectRole: "chart_of_accounts", Title: "Chart of Accounts", Description: "Shared accounting subject master for voucher classification. One catalog per tenant.", Domain: "finance", DatasetID: "finance.accounts", TemplateID: "finance.accounts", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.account_upsert", PreferredView: "finance.account_directory", PreferredReport: "finance.account_count_by_type", PreferredDashboard: "finance.overview", Required: true},
	{ObjectRole: "bank_account", Title: "Bank Account", Description: "Company bank account master for receipts and payments. One catalog per tenant.", Domain: "finance", DatasetID: "finance.bank_accounts", TemplateID: "finance.bank_accounts", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.bank_account_upsert", PreferredView: "finance.bank_account_directory", PreferredDashboard: "finance.overview"},
	{ObjectRole: "payment", Title: "Payment", Description: "Accounts payable and receivable payment record.", Domain: "finance", DatasetID: "finance.payments", TemplateID: "finance.payments", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.payment_upsert", PreferredView: "finance.payment_tracker", PreferredReport: "finance.payment_status_summary", PreferredDashboard: "finance.overview"},
	{ObjectRole: "budget", Title: "Budget", Description: "Department, category, period, budget amount, committed amount, and lifecycle status.", Domain: "finance", DatasetID: "finance.budgets", TemplateID: "finance.budgets", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.budget_upsert", PreferredView: "finance.budget_control", PreferredReport: "finance.budget_by_department", PreferredDashboard: "finance.overview"},
	{ObjectRole: "voucher", Title: "Accounting Voucher", Description: "Accounting voucher header for controlled finance posting. Canonical lines live in finance.voucher_lines.", Domain: "finance", DatasetID: "finance.vouchers", TemplateID: "finance.vouchers", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}, PreferredAction: "finance.voucher_upsert", PreferredView: "finance.voucher_register", PreferredReport: "finance.voucher_status_summary", PreferredDashboard: "finance.overview"},
	{ObjectRole: "voucher_line", Title: "Voucher Line", Description: "Accounting voucher line: account, debit, and credit, referenced to the voucher header.", Domain: "finance", DatasetID: "finance.voucher_lines", TemplateID: "finance.voucher_lines", AppIDs: []string{"mis.finance"}, BlueprintIDs: []string{"mis.finance"}},
	{ObjectRole: "employee", Title: "Employee", Description: "Employee profile, organization, role, and employment status.", Domain: "hr", DatasetID: "hr.employees", TemplateID: "hr.employees", AppIDs: []string{"mis.hr", "mis.expense"}, BlueprintIDs: []string{"mis.hr", "mis.expense"}, PreferredAction: "hr.employee_upsert", PreferredView: "hr.employee_roster", PreferredReport: "hr.employee_count_by_department", PreferredDashboard: "hr.overview", Required: true},
	{ObjectRole: "payroll", Title: "Payroll", Description: "Monthly payroll summary keyed to the employee master.", Domain: "hr", DatasetID: "hr.payroll", TemplateID: "hr.payroll", AppIDs: []string{"mis.hr"}, BlueprintIDs: []string{"mis.hr"}, PreferredAction: "hr.payroll_upsert", PreferredView: "hr.payroll_review", PreferredReport: "hr.payroll_status_summary", PreferredDashboard: "hr.overview"},
	{ObjectRole: "leave_request", Title: "Leave Request", Description: "Employee leave and absence approval record.", Domain: "hr", DatasetID: "hr.leave_requests", TemplateID: "hr.leave_requests", AppIDs: []string{"mis.hr"}, BlueprintIDs: []string{"mis.hr"}, PreferredAction: "hr.leave_request_upsert", PreferredView: "hr.leave_request_review", PreferredReport: "hr.leave_by_status", PreferredDashboard: "hr.overview"},
	{ObjectRole: "contract", Title: "Contract", Description: "Contract lifecycle, amount, counterparty, owner, and expiration tracking.", Domain: "legal", DatasetID: "legal.contracts", TemplateID: "legal.contracts", AppIDs: []string{"mis.legal"}, BlueprintIDs: []string{"mis.legal"}, PreferredAction: "legal.contract_upsert", PreferredView: "legal.contract_register", PreferredReport: "legal.contract_value_by_status", PreferredDashboard: "legal.overview"},
	{ObjectRole: "supplier", Title: "Supplier", Description: "Supplier master data for procurement, finance, contracts, and payment workflows.", Domain: "procurement", DatasetID: "procurement.suppliers", TemplateID: "procurement.suppliers", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}, PreferredAction: "procurement.supplier_upsert", PreferredView: "procurement.supplier_directory", PreferredReport: "procurement.supplier_by_status", PreferredDashboard: "procurement.overview"},
	{ObjectRole: "purchase_order", Title: "Purchase Order", Description: "Supplier purchase order header: amount, requester, approval, and fulfillment tracking. Line items live in procurement.purchase_order_lines.", Domain: "procurement", DatasetID: "procurement.purchase_orders", TemplateID: "procurement.purchase_orders", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}, PreferredAction: "procurement.purchase_order_upsert", PreferredView: "procurement.purchase_order_tracker", PreferredReport: "procurement.po_status_summary", PreferredDashboard: "procurement.overview", Required: true},
	{ObjectRole: "purchase_order_line", Title: "Purchase Order Line", Description: "Purchase order line: item, quantity, unit price, and amount, referenced to the PO header.", Domain: "procurement", DatasetID: "procurement.purchase_order_lines", TemplateID: "procurement.purchase_order_lines", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}},
	{ObjectRole: "purchase_receipt", Title: "Purchase Receipt", Description: "Inbound purchase receipt header against a supplier PO. Line quantities live in procurement.receipt_lines.", Domain: "procurement", DatasetID: "procurement.receipts", TemplateID: "procurement.receipts", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}},
	{ObjectRole: "purchase_receipt_line", Title: "Purchase Receipt Line", Description: "Inbound receipt line quantity linked to the receipt header and item master.", Domain: "procurement", DatasetID: "procurement.receipt_lines", TemplateID: "procurement.receipt_lines", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}},
	{ObjectRole: "purchase_return", Title: "Purchase Return", Description: "Supplier purchase return header. Line quantities live in procurement.return_lines.", Domain: "procurement", DatasetID: "procurement.returns", TemplateID: "procurement.returns", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}},
	{ObjectRole: "purchase_return_line", Title: "Purchase Return Line", Description: "Purchase return line: item, quantity, unit price, and amount.", Domain: "procurement", DatasetID: "procurement.return_lines", TemplateID: "procurement.return_lines", AppIDs: []string{"mis.procurement"}, BlueprintIDs: []string{"mis.procurement"}},
	{ObjectRole: "inventory_item", Title: "Inventory Item", Description: "Inventory item master: identity and specification. On-hand quantity is on stock balances and movements.", Domain: "inventory", DatasetID: "inventory.items", TemplateID: "inventory.items", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}, PreferredAction: "inventory.item_upsert", PreferredView: "inventory.stock_overview", PreferredReport: "inventory.quantity_by_warehouse", PreferredDashboard: "inventory.overview", Required: true},
	{ObjectRole: "inventory_stock_balance", Title: "Inventory Stock Balance", Description: "On-hand quantity by item and warehouse. Quantity changes post through inventory movements.", Domain: "inventory", DatasetID: "inventory.stock_balances", TemplateID: "inventory.stock_balances", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}, PreferredAction: "inventory.stock_update", PreferredView: "inventory.stock_overview", PreferredReport: "inventory.quantity_by_warehouse", PreferredDashboard: "inventory.overview"},
	{ObjectRole: "inventory_warehouse", Title: "Inventory Warehouse", Description: "Warehouse master data for stock, movement, transfer, and responsible-person tracking.", Domain: "inventory", DatasetID: "inventory.warehouses", TemplateID: "inventory.warehouses", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}, PreferredAction: "inventory.warehouse_upsert", PreferredView: "inventory.warehouse_directory", PreferredReport: "inventory.warehouse_by_status", PreferredDashboard: "inventory.overview"},
	{ObjectRole: "inventory_movement", Title: "Inventory Movement", Description: "Inventory receipt, issue, transfer, adjustment, reservation, and release movement record.", Domain: "inventory", DatasetID: "inventory.movements", TemplateID: "inventory.movements", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}, PreferredAction: "inventory.movement_record", PreferredView: "inventory.movement_ledger", PreferredReport: "inventory.movement_by_type", PreferredDashboard: "inventory.overview", Required: true},
	{ObjectRole: "inventory_stocktake", Title: "Inventory Stocktake", Description: "Physical inventory count header. Counted quantities live in inventory.stocktake_lines.", Domain: "inventory", DatasetID: "inventory.stocktakes", TemplateID: "inventory.stocktakes", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}},
	{ObjectRole: "inventory_stocktake_line", Title: "Inventory Stocktake Line", Description: "Physical count line: item, book quantity, and counted quantity.", Domain: "inventory", DatasetID: "inventory.stocktake_lines", TemplateID: "inventory.stocktake_lines", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}},
	{ObjectRole: "inventory_transfer", Title: "Inventory Transfer", Description: "Warehouse-to-warehouse transfer header. Line quantities live in inventory.transfer_lines.", Domain: "inventory", DatasetID: "inventory.transfers", TemplateID: "inventory.transfers", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}},
	{ObjectRole: "inventory_transfer_line", Title: "Inventory Transfer Line", Description: "Transfer line quantity linked to the transfer header and item master.", Domain: "inventory", DatasetID: "inventory.transfer_lines", TemplateID: "inventory.transfer_lines", AppIDs: []string{"mis.inventory"}, BlueprintIDs: []string{"mis.inventory"}},
	{ObjectRole: "bom", Title: "Bill of Materials", Description: "Finished-item BOM header. Component rows live in manufacturing.bom_components.", Domain: "manufacturing", DatasetID: "manufacturing.boms", TemplateID: "manufacturing.boms", AppIDs: []string{"mis.manufacturing"}, BlueprintIDs: []string{"mis.manufacturing"}, PreferredAction: "manufacturing.bom_upsert", PreferredView: "manufacturing.bom_directory", PreferredReport: "manufacturing.bom_by_status", PreferredDashboard: "manufacturing.overview"},
	{ObjectRole: "bom_component", Title: "BOM Component", Description: "BOM component row: item and quantity referenced to the BOM header and inventory item master.", Domain: "manufacturing", DatasetID: "manufacturing.bom_components", TemplateID: "manufacturing.bom_components", AppIDs: []string{"mis.manufacturing"}, BlueprintIDs: []string{"mis.manufacturing"}},
	{ObjectRole: "production_order", Title: "Production Order", Description: "Production work-order header for a finished item. Component lines live in manufacturing.production_order_lines.", Domain: "manufacturing", DatasetID: "manufacturing.production_orders", TemplateID: "manufacturing.production_orders", AppIDs: []string{"mis.manufacturing"}, BlueprintIDs: []string{"mis.manufacturing"}, PreferredAction: "manufacturing.production_order_upsert", PreferredView: "manufacturing.production_order_tracker", PreferredReport: "manufacturing.production_by_status", PreferredDashboard: "manufacturing.overview"},
	{ObjectRole: "production_order_line", Title: "Production Order Line", Description: "Production order component issue line: item, warehouse, and quantity.", Domain: "manufacturing", DatasetID: "manufacturing.production_order_lines", TemplateID: "manufacturing.production_order_lines", AppIDs: []string{"mis.manufacturing"}, BlueprintIDs: []string{"mis.manufacturing"}},
	{ObjectRole: "fixed_asset", Title: "Fixed Asset", Description: "Company asset register, ownership, depreciation, and lifecycle status. One catalog per tenant.", Domain: "assets", DatasetID: "assets.fixed_assets", TemplateID: "assets.fixed_assets", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}, PreferredAction: "assets.fixed_asset_upsert", PreferredView: "assets.asset_register", PreferredReport: "assets.value_by_department", PreferredDashboard: "assets.overview"},
	{ObjectRole: "asset_maintenance_order", Title: "Asset Maintenance Order", Description: "Repair and preventive-maintenance work order against the single fixed-asset register.", Domain: "assets", DatasetID: "assets.maintenance_orders", TemplateID: "assets.maintenance_orders", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}, PreferredAction: "assets.maintenance_order_upsert", PreferredView: "assets.maintenance_queue", PreferredReport: "assets.maintenance_by_status", PreferredDashboard: "assets.overview"},
	{ObjectRole: "asset_maintenance_order_line", Title: "Asset Maintenance Order Line", Description: "Spare parts consumed on an asset maintenance work order.", Domain: "assets", DatasetID: "assets.maintenance_order_lines", TemplateID: "assets.maintenance_order_lines", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}},
	{ObjectRole: "asset_transfer", Title: "Asset Transfer", Description: "Internal asset transfer header. Line assets live in assets.transfer_lines.", Domain: "assets", DatasetID: "assets.transfers", TemplateID: "assets.transfers", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}},
	{ObjectRole: "asset_transfer_line", Title: "Asset Transfer Line", Description: "Asset moved on a transfer document, referenced to the single fixed-asset register.", Domain: "assets", DatasetID: "assets.transfer_lines", TemplateID: "assets.transfer_lines", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}},
	{ObjectRole: "asset_disposal", Title: "Asset Disposal", Description: "Asset disposal header. Disposed assets live in assets.disposal_lines.", Domain: "assets", DatasetID: "assets.disposals", TemplateID: "assets.disposals", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}},
	{ObjectRole: "asset_disposal_line", Title: "Asset Disposal Line", Description: "Asset disposed on a disposal document, referenced to the single fixed-asset register.", Domain: "assets", DatasetID: "assets.disposal_lines", TemplateID: "assets.disposal_lines", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}},
	{ObjectRole: "asset_depreciation_run", Title: "Asset Depreciation Run", Description: "Period depreciation run header. Per-asset amounts live in assets.depreciation_lines.", Domain: "assets", DatasetID: "assets.depreciation_runs", TemplateID: "assets.depreciation_runs", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}},
	{ObjectRole: "asset_depreciation_line", Title: "Asset Depreciation Line", Description: "Per-asset depreciation amount referenced to the single fixed-asset register.", Domain: "assets", DatasetID: "assets.depreciation_lines", TemplateID: "assets.depreciation_lines", AppIDs: []string{"mis.assets"}, BlueprintIDs: []string{"mis.assets"}},
}

func (s *Service) ListBusinessObjects(ctx context.Context, p Principal, in QueryBusinessObjectsInput) ([]BusinessObjectCatalog, error) {
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
