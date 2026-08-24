package structureddata

import (
	"context"
	"errors"
	"sort"
	"strings"
)

var datasetTemplates = []DatasetTemplate{
	{
		ID:          "company.departments",
		Domain:      "company",
		Name:        "departments",
		Title:       "Departments",
		Description: "Company department, cost center, manager, and hierarchy master data.",
		Fields: []DatasetTemplateField{
			{Key: "department_code", Type: "string", Title: "Department Code", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "parent_ref", Type: "record_ref", Title: "Parent Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "manager", Type: "string", Title: "Manager", Indexed: true},
			{Key: "manager_ref", Type: "record_ref", Title: "Manager Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "cost_center", Type: "string", Title: "Cost Center", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "inactive", "archived")},
		},
		SampleData: []map[string]interface{}{{"department_code": "D-SALES", "name": "Sales", "manager": "Alice", "cost_center": "CC-SALES", "status": "active"}},
	},
	{
		ID:          "company.projects",
		Domain:      "company",
		Name:        "projects",
		Title:       "Projects",
		Description: "Company project master for delivery, billing, and after-sales work. One catalog per tenant.",
		Fields: []DatasetTemplateField{
			{Key: "project_no", Type: "string", Title: "Project No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "owner_ref", Type: "record_ref", Title: "Owner Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("planned", "active", "on_hold", "completed", "cancelled")},
			{Key: "start_date", Type: "date", Title: "Start Date", Indexed: true},
			{Key: "end_date", Type: "date", Title: "End Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"project_no": "PRJ-2026-0001", "name": "Acme rollout", "status": "active", "start_date": "2026-05-01"}},
	},
	{
		ID:          "sales.orders",
		Domain:      "sales",
		Name:        "orders",
		Title:       "Sales Orders",
		Description: "Customer sales order headers: amount, owner, stage, and payment status. Line items live in sales.order_lines.",
		Fields: []DatasetTemplateField{
			{Key: "order_no", Type: "string", Title: "Order No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "customer", Type: "string", Title: "Customer", Required: true, Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "contact_ref", Type: "record_ref", Title: "Contact Ref", Indexed: true, Config: refFieldConfig("sales.contacts")},
			{Key: "opportunity_ref", Type: "record_ref", Title: "Opportunity Ref", Indexed: true, Config: refFieldConfig("sales.opportunities")},
			{Key: "quote_ref", Type: "record_ref", Title: "Quote Ref", Indexed: true, Config: refFieldConfig("sales.quotes")},
			{Key: "project_ref", Type: "record_ref", Title: "Project Ref", Indexed: true, Config: refFieldConfig("company.projects")},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
			{Key: "owner_ref", Type: "record_ref", Title: "Owner Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "stage", Type: "string", Title: "Stage", Indexed: true, Config: enumFieldConfig("draft", "confirmed", "won", "lost", "cancelled", "fulfilled")},
			{Key: "payment_status", Type: "string", Title: "Payment Status", Indexed: true, Config: enumFieldConfig("unpaid", "partial", "paid", "refunded", "overdue")},
			{Key: "order_date", Type: "date", Title: "Order Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"order_no": "SO-2026-0001", "customer": "Acme", "amount": 8800, "currency": "CNY", "owner": "Alice", "stage": "confirmed", "payment_status": "unpaid", "order_date": "2026-05-05"}},
	},
	{
		ID:          "sales.order_lines",
		Domain:      "sales",
		Name:        "order_lines",
		Title:       "Sales Order Lines",
		Description: "Sales order line items: item, quantity, unit price, and amount, each linked to the order header.",
		Fields: append(commercialDocumentLineFields("order_ref", "sales.orders", "Order Ref"),
			DatasetTemplateField{Key: "quote_line_ref", Type: "record_ref", Title: "Quote Line Ref", Indexed: true, Config: refFieldConfig("sales.quote_lines")},
		),
		SampleData:  []map[string]interface{}{{"line_no": "1", "order_ref": "SO-2026-0001", "item_ref": "ITEM-001", "quantity": 2, "unit": "pcs", "unit_price": 4400, "amount": 8800}},
	},
	{
		ID:          "sales.quotes",
		Domain:      "sales",
		Name:        "quotes",
		Title:       "Sales Quotes",
		Description: "Customer sales quotation headers: quoted amount, validity, owner, and stage. Line items live in sales.quote_lines.",
		Fields: []DatasetTemplateField{
			{Key: "quote_no", Type: "string", Title: "Quote No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "customer", Type: "string", Title: "Customer", Required: true, Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "contact_ref", Type: "record_ref", Title: "Contact Ref", Indexed: true, Config: refFieldConfig("sales.contacts")},
			{Key: "opportunity_ref", Type: "record_ref", Title: "Opportunity Ref", Indexed: true, Config: refFieldConfig("sales.opportunities")},
			{Key: "project_ref", Type: "record_ref", Title: "Project Ref", Indexed: true, Config: refFieldConfig("company.projects")},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
			{Key: "owner_ref", Type: "record_ref", Title: "Owner Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "sent", "accepted", "rejected", "expired", "cancelled")},
			{Key: "quote_date", Type: "date", Title: "Quote Date", Indexed: true},
			{Key: "valid_until", Type: "date", Title: "Valid Until", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"quote_no": "QT-2026-0001", "customer": "Acme", "amount": 12000, "currency": "CNY", "owner": "Alice", "status": "sent", "quote_date": "2026-05-01", "valid_until": "2026-05-31"}},
	},
	{
		ID:          "sales.quote_lines",
		Domain:      "sales",
		Name:        "quote_lines",
		Title:       "Sales Quote Lines",
		Description: "Sales quotation line items: item, quantity, unit price, and amount, each linked to the quote header.",
		Fields:      commercialDocumentLineFields("quote_ref", "sales.quotes", "Quote Ref"),
		SampleData:  []map[string]interface{}{{"line_no": "1", "quote_ref": "QT-2026-0001", "item_ref": "ITEM-001", "quantity": 3, "unit": "pcs", "unit_price": 4000, "amount": 12000}},
	},
	{
		ID:          "sales.shipments",
		Domain:      "sales",
		Name:        "shipments",
		Title:       "Sales Shipments",
		Description: "Sales delivery / outbound shipment headers against a customer order. Line quantities live in sales.shipment_lines.",
		Fields: []DatasetTemplateField{
			{Key: "shipment_no", Type: "string", Title: "Shipment No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "order_ref", Type: "record_ref", Title: "Sales Order Ref", Indexed: true, Config: refFieldConfig("sales.orders")},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "carrier", Type: "string", Title: "Carrier", Indexed: true},
			{Key: "tracking_no", Type: "string", Title: "Tracking No", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "picked", "shipped", "delivered", "cancelled")},
			{Key: "ship_date", Type: "date", Title: "Ship Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"shipment_no": "SHP-2026-0001", "carrier": "SF", "status": "shipped", "ship_date": "2026-05-06"}},
	},
	{
		ID:          "sales.shipment_lines",
		Domain:      "sales",
		Name:        "shipment_lines",
		Title:       "Sales Shipment Lines",
		Description: "Outbound shipment line quantities, each linked to the shipment header and shared item/warehouse masters.",
		Fields: append(quantityDocumentLineFields("shipment_ref", "sales.shipments", "Shipment Ref"),
			DatasetTemplateField{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			DatasetTemplateField{Key: "order_line_ref", Type: "record_ref", Title: "Order Line Ref", Indexed: true, Config: refFieldConfig("sales.order_lines")},
		),
		SampleData: []map[string]interface{}{{"line_no": "1", "shipment_ref": "SHP-2026-0001", "item_ref": "ITEM-001", "quantity": 2, "unit": "pcs"}},
	},
	{
		ID:          "sales.returns",
		Domain:      "sales",
		Name:        "returns",
		Title:       "Sales Returns",
		Description: "Customer sales return headers against a sales order. Line quantities and amounts live in sales.return_lines.",
		Fields: []DatasetTemplateField{
			{Key: "return_no", Type: "string", Title: "Return No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "order_ref", Type: "record_ref", Title: "Sales Order Ref", Indexed: true, Config: refFieldConfig("sales.orders")},
			{Key: "shipment_ref", Type: "record_ref", Title: "Shipment Ref", Indexed: true, Config: refFieldConfig("sales.shipments")},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Return Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "amount", Type: "number", Title: "Amount", Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "reason", Type: "string", Title: "Reason", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "approved", "received", "credited", "cancelled")},
			{Key: "return_date", Type: "date", Title: "Return Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"return_no": "RMA-2026-0001", "amount": 4400, "currency": "CNY", "reason": "defective", "status": "approved", "return_date": "2026-05-10"}},
	},
	{
		ID:          "sales.return_lines",
		Domain:      "sales",
		Name:        "return_lines",
		Title:       "Sales Return Lines",
		Description: "Sales return line items: item, quantity, unit price, and amount, each linked to the return header.",
		Fields: append(commercialDocumentLineFields("return_ref", "sales.returns", "Return Ref"),
			DatasetTemplateField{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			DatasetTemplateField{Key: "order_line_ref", Type: "record_ref", Title: "Order Line Ref", Indexed: true, Config: refFieldConfig("sales.order_lines")},
			DatasetTemplateField{Key: "shipment_line_ref", Type: "record_ref", Title: "Shipment Line Ref", Indexed: true, Config: refFieldConfig("sales.shipment_lines")},
		),
		SampleData:  []map[string]interface{}{{"line_no": "1", "return_ref": "RMA-2026-0001", "item_ref": "ITEM-001", "quantity": 1, "unit": "pcs", "unit_price": 4400, "amount": 4400}},
	},
	{
		ID:          "sales.service_tickets",
		Domain:      "sales",
		Name:        "service_tickets",
		Title:       "After-sales Service Tickets",
		Description: "After-sales work orders linked to a customer and the originating sales order.",
		Fields: []DatasetTemplateField{
			{Key: "ticket_no", Type: "string", Title: "Ticket No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Required: true, Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "order_ref", Type: "record_ref", Title: "Sales Order Ref", Indexed: true, Config: refFieldConfig("sales.orders")},
			{Key: "project_ref", Type: "record_ref", Title: "Project Ref", Indexed: true, Config: refFieldConfig("company.projects")},
			{Key: "item_ref", Type: "record_ref", Title: "Item Ref", Indexed: true, Config: refFieldConfig("inventory.items")},
			{Key: "asset_ref", Type: "record_ref", Title: "Asset Ref", Indexed: true, Config: refFieldConfig("assets.fixed_assets")},
			{Key: "contact_ref", Type: "record_ref", Title: "Contact Ref", Indexed: true, Config: refFieldConfig("sales.contacts")},
			{Key: "assignee_ref", Type: "record_ref", Title: "Assignee Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "subject", Type: "string", Title: "Subject", Required: true, Indexed: true},
			{Key: "priority", Type: "string", Title: "Priority", Indexed: true, Config: enumFieldConfig("low", "medium", "high", "critical")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("open", "in_progress", "waiting", "resolved", "closed", "cancelled")},
			{Key: "opened_at", Type: "datetime", Title: "Opened At", Indexed: true},
			{Key: "resolved_at", Type: "datetime", Title: "Resolved At", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"ticket_no": "TKT-2026-0001", "customer_ref": "CUST-2026-0001", "subject": "Install support", "priority": "medium", "status": "open", "opened_at": "2026-05-07T09:00:00Z"}},
	},
	{
		ID:          "sales.customers",
		Domain:      "sales",
		Name:        "customers",
		Title:       "Customers",
		Description: "Customer master data for sales and service workflows.",
		Fields: []DatasetTemplateField{
			{Key: "customer_no", Type: "string", Title: "Customer No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "industry", Type: "string", Title: "Industry", Indexed: true},
			{Key: "region", Type: "string", Title: "Region", Indexed: true},
			{Key: "contact", Type: "string", Title: "Contact", Indexed: true},
			{Key: "phone", Type: "string", Title: "Phone", Sensitive: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
			{Key: "owner_ref", Type: "record_ref", Title: "Owner Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("lead", "active", "inactive", "blocked", "archived")},
		},
	},
	{
		ID:          "sales.contacts",
		Domain:      "sales",
		Name:        "contacts",
		Title:       "Sales Contacts",
		Description: "Customer contact people, role, communication details, owner, and lifecycle status.",
		Fields: []DatasetTemplateField{
			{Key: "contact_no", Type: "string", Title: "Contact No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "customer", Type: "string", Title: "Customer", Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "role", Type: "string", Title: "Role", Indexed: true},
			{Key: "email", Type: "string", Title: "Email", Sensitive: true},
			{Key: "phone", Type: "string", Title: "Phone", Sensitive: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
			{Key: "owner_ref", Type: "record_ref", Title: "Owner Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "inactive", "left", "blocked", "archived")},
		},
		SampleData: []map[string]interface{}{{"contact_no": "CON-2026-0001", "name": "Chen Wei", "customer": "Acme", "role": "Procurement Manager", "owner": "Alice", "status": "active"}},
	},
	{
		ID:          "sales.opportunities",
		Domain:      "sales",
		Name:        "opportunities",
		Title:       "Sales Opportunities",
		Description: "Sales opportunity pipeline, expected amount, probability, stage, owner, and expected close date.",
		Fields: []DatasetTemplateField{
			{Key: "opportunity_no", Type: "string", Title: "Opportunity No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "customer", Type: "string", Title: "Customer", Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "contact_ref", Type: "record_ref", Title: "Primary Contact Ref", Indexed: true, Config: refFieldConfig("sales.contacts")},
			{Key: "amount", Type: "number", Title: "Expected Amount", Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "probability", Type: "number", Title: "Probability", Indexed: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
			{Key: "owner_ref", Type: "record_ref", Title: "Owner Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "project_ref", Type: "record_ref", Title: "Project Ref", Indexed: true, Config: refFieldConfig("company.projects")},
			{Key: "stage", Type: "string", Title: "Stage", Indexed: true, Config: enumFieldConfig("lead", "qualified", "proposal", "negotiation", "won", "lost", "cancelled")},
			{Key: "expected_close_date", Type: "date", Title: "Expected Close Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"opportunity_no": "OPP-2026-0001", "name": "Acme renewal", "customer": "Acme", "amount": 50000, "currency": "CNY", "probability": 0.6, "owner": "Alice", "stage": "proposal", "expected_close_date": "2026-06-30"}},
	},
	{
		ID:          "procurement.suppliers",
		Domain:      "procurement",
		Name:        "suppliers",
		Title:       "Suppliers",
		Description: "Supplier master data for procurement, finance, contracts, and payment workflows.",
		Fields: []DatasetTemplateField{
			{Key: "supplier_no", Type: "string", Title: "Supplier No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "category", Type: "string", Title: "Category", Indexed: true},
			{Key: "region", Type: "string", Title: "Region", Indexed: true},
			{Key: "contact", Type: "string", Title: "Contact", Indexed: true},
			{Key: "phone", Type: "string", Title: "Phone", Sensitive: true},
			{Key: "payment_terms", Type: "string", Title: "Payment Terms", Indexed: true},
			{Key: "tax_no", Type: "string", Title: "Tax No", Sensitive: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("candidate", "active", "suspended", "blocked", "archived")},
		},
		SampleData: []map[string]interface{}{{"supplier_no": "SUP-2026-0001", "name": "SupplyCo", "category": "materials", "region": "East", "contact": "Bob", "payment_terms": "NET30", "status": "active"}},
	},
	{
		ID:          "finance.expenses",
		Domain:      "finance",
		Name:        "expenses",
		Title:       "Expenses",
		Description: "Employee reimbursement and operating expense records.",
		Fields: []DatasetTemplateField{
			{Key: "expense_no", Type: "string", Title: "Expense No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "applicant", Type: "string", Title: "Applicant", Required: true, Indexed: true},
			{Key: "applicant_ref", Type: "record_ref", Title: "Applicant Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "department", Type: "string", Title: "Department", Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "project_ref", Type: "record_ref", Title: "Project Ref", Indexed: true, Config: refFieldConfig("company.projects")},
			{Key: "category", Type: "string", Title: "Category", Indexed: true},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("submitted", "approved", "rejected", "paid", "cancelled")},
			{Key: "expense_date", Type: "date", Title: "Expense Date", Indexed: true},
		},
	},
	{
		ID:          "finance.invoices",
		Domain:      "finance",
		Name:        "invoices",
		Title:       "Invoices",
		Description: "Issued and received invoice headers with amount, tax, counterparty, due date, and lifecycle status. Line items live in finance.invoice_lines.",
		Fields: []DatasetTemplateField{
			{Key: "invoice_no", Type: "string", Title: "Invoice No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "counterparty", Type: "string", Title: "Counterparty", Required: true, Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "supplier_ref", Type: "record_ref", Title: "Supplier Ref", Indexed: true, Config: refFieldConfig("procurement.suppliers")},
			{Key: "order_ref", Type: "record_ref", Title: "Sales Order Ref", Indexed: true, Config: refFieldConfig("sales.orders")},
			{Key: "purchase_order_ref", Type: "record_ref", Title: "Purchase Order Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_orders")},
			{Key: "project_ref", Type: "record_ref", Title: "Project Ref", Indexed: true, Config: refFieldConfig("company.projects")},
			{Key: "contract_ref", Type: "record_ref", Title: "Contract Ref", Indexed: true, Config: refFieldConfig("legal.contracts")},
			{Key: "invoice_type", Type: "string", Title: "Invoice Type", Indexed: true, Config: enumFieldConfig("issued", "received", "credit_note", "debit_note")},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "tax_amount", Type: "number", Title: "Tax Amount", Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "issued", "received", "verified", "paid", "voided", "overdue")},
			{Key: "issue_date", Type: "date", Title: "Issue Date", Indexed: true},
			{Key: "due_date", Type: "date", Title: "Due Date", Indexed: true},
		},
	},
	{
		ID:          "finance.invoice_lines",
		Domain:      "finance",
		Name:        "invoice_lines",
		Title:       "Invoice Lines",
		Description: "Invoice line items: item, quantity, unit price, tax, and amount, each linked to the invoice header.",
		Fields: append(commercialDocumentLineFields("invoice_ref", "finance.invoices", "Invoice Ref"),
			DatasetTemplateField{Key: "tax_amount", Type: "number", Title: "Tax Amount", Indexed: true},
			DatasetTemplateField{Key: "order_line_ref", Type: "record_ref", Title: "Order Line Ref", Indexed: true, Config: refFieldConfig("sales.order_lines")},
			DatasetTemplateField{Key: "purchase_order_line_ref", Type: "record_ref", Title: "PO Line Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_order_lines")},
		),
		SampleData: []map[string]interface{}{{"line_no": "1", "invoice_ref": "INV-2026-0001", "item_ref": "ITEM-001", "quantity": 2, "unit": "pcs", "unit_price": 4400, "amount": 8800, "tax_amount": 572}},
	},
	{
		ID:          "finance.payments",
		Domain:      "finance",
		Name:        "payments",
		Title:       "Payments",
		Description: "Accounts payable and receivable payment records.",
		Fields: []DatasetTemplateField{
			{Key: "payment_no", Type: "string", Title: "Payment No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "counterparty", Type: "string", Title: "Counterparty", Required: true, Indexed: true},
			{Key: "payment_type", Type: "string", Title: "Payment Type", Required: true, Indexed: true, Config: enumFieldConfig("payable", "receivable", "refund", "transfer")},
			{Key: "supplier_ref", Type: "record_ref", Title: "Supplier Ref", Indexed: true, Config: refFieldConfig("procurement.suppliers")},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "bank_account_ref", Type: "record_ref", Title: "Bank Account Ref", Indexed: true, Config: refFieldConfig("finance.bank_accounts")},
			{Key: "invoice_ref", Type: "record_ref", Title: "Invoice Ref", Indexed: true, Config: refFieldConfig("finance.invoices")},
			{Key: "purchase_order_ref", Type: "record_ref", Title: "Purchase Order Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_orders")},
			{Key: "expense_ref", Type: "record_ref", Title: "Expense Ref", Indexed: true, Config: refFieldConfig("finance.expenses")},
			{Key: "voucher_ref", Type: "record_ref", Title: "Voucher Ref", Indexed: true, Config: refFieldConfig("finance.vouchers")},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "method", Type: "string", Title: "Method", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("planned", "approved", "paid", "received", "failed", "cancelled")},
			{Key: "payment_date", Type: "date", Title: "Payment Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"payment_no": "PAY-2026-0001", "counterparty": "Acme", "payment_type": "receivable", "amount": 8800, "currency": "CNY", "method": "bank_transfer", "status": "planned", "payment_date": "2026-05-05"}},
	},
	{
		ID:          "finance.accounts",
		Domain:      "finance",
		Name:        "accounts",
		Title:       "Chart of Accounts",
		Description: "Lightweight chart of accounts for finance voucher classification.",
		Fields: []DatasetTemplateField{
			{Key: "account_code", Type: "string", Title: "Account Code", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "account_name", Type: "string", Title: "Account Name", Required: true, Indexed: true},
			{Key: "account_type", Type: "string", Title: "Account Type", Required: true, Indexed: true, Config: enumFieldConfig("asset", "liability", "equity", "revenue", "expense", "cost")},
			{Key: "parent_code", Type: "string", Title: "Parent Code", Indexed: true},
			{Key: "parent_ref", Type: "record_ref", Title: "Parent Account Ref", Indexed: true, Config: refFieldConfig("finance.accounts")},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "inactive", "archived")},
		},
		SampleData: []map[string]interface{}{{"account_code": "1001", "account_name": "Cash", "account_type": "asset", "currency": "CNY", "status": "active"}},
	},
	{
		ID:          "finance.bank_accounts",
		Domain:      "finance",
		Name:        "bank_accounts",
		Title:       "Bank Accounts",
		Description: "Company bank account master for receipts, payments, and cash positioning. One catalog per tenant.",
		Fields: []DatasetTemplateField{
			{Key: "account_no", Type: "string", Title: "Account No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "account_name", Type: "string", Title: "Account Name", Required: true, Indexed: true},
			{Key: "bank_name", Type: "string", Title: "Bank Name", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "frozen", "closed")},
		},
		SampleData: []map[string]interface{}{{"account_no": "6222-0001", "account_name": "Operating CNY", "bank_name": "ICBC", "currency": "CNY", "status": "active"}},
	},
	{
		ID:          "finance.budgets",
		Domain:      "finance",
		Name:        "budgets",
		Title:       "Budgets",
		Description: "Department, category, period, budget amount, committed amount, actual amount, and budget lifecycle tracking.",
		Fields: []DatasetTemplateField{
			{Key: "budget_no", Type: "string", Title: "Budget No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "period", Type: "string", Title: "Period", Required: true, Indexed: true},
			{Key: "department", Type: "string", Title: "Department", Required: true, Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "category", Type: "string", Title: "Category", Required: true, Indexed: true},
			{Key: "budget_amount", Type: "number", Title: "Budget Amount", Required: true, Indexed: true},
			{Key: "committed_amount", Type: "number", Title: "Committed Amount", Indexed: true},
			{Key: "actual_amount", Type: "number", Title: "Actual Amount", Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "submitted", "approved", "active", "frozen", "closed", "cancelled")},
		},
		SampleData: []map[string]interface{}{{"budget_no": "BUD-2026-SALES-TRAVEL", "period": "2026", "department": "Sales", "category": "travel", "budget_amount": 120000, "committed_amount": 20000, "actual_amount": 15000, "currency": "CNY", "owner": "Finance", "status": "active"}},
	},
	{
		ID:          "finance.vouchers",
		Domain:      "finance",
		Name:        "vouchers",
		Title:       "Accounting Vouchers",
		Description: "Lightweight accounting voucher headers and entry lines for controlled finance posting workflows.",
		Fields: []DatasetTemplateField{
			{Key: "voucher_no", Type: "string", Title: "Voucher No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "period", Type: "string", Title: "Accounting Period", Required: true, Indexed: true},
			{Key: "voucher_type", Type: "string", Title: "Voucher Type", Required: true, Indexed: true, Config: enumFieldConfig("general", "payment", "receipt", "accrual", "adjustment", "closing")},
			{Key: "source_ref", Type: "record_ref", Title: "Source Ref", Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "summary", Type: "string", Title: "Summary", Indexed: true},
			{Key: "debit_total", Type: "number", Title: "Debit Total", Required: true, Indexed: true},
			{Key: "credit_total", Type: "number", Title: "Credit Total", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "reviewing", "approved", "posted", "reversed", "voided")},
			{Key: "posted_at", Type: "datetime", Title: "Posted At", Indexed: true},
			{Key: "lines", Type: "array", Title: "Voucher Lines Preview"},
		},
		SampleData: []map[string]interface{}{{"voucher_no": "VCH-2026-0001", "period": "2026-05", "voucher_type": "receipt", "summary": "Receive customer payment", "debit_total": 8800, "credit_total": 8800, "currency": "CNY", "status": "draft", "lines": []any{map[string]any{"account_code": "1001", "debit": 8800, "credit": 0}, map[string]any{"account_code": "6001", "debit": 0, "credit": 8800}}}},
	},
	{
		ID:          "finance.voucher_lines",
		Domain:      "finance",
		Name:        "voucher_lines",
		Title:       "Voucher Lines",
		Description: "Accounting voucher entry lines: account, debit, and credit, each linked to the voucher header. Canonical line store (not the header array preview).",
		Fields: []DatasetTemplateField{
			{Key: "line_no", Type: "string", Title: "Line No", Required: true, Indexed: true},
			{Key: "voucher_ref", Type: "record_ref", Title: "Voucher Ref", Required: true, Indexed: true, Config: refFieldConfig("finance.vouchers")},
			{Key: "account_ref", Type: "record_ref", Title: "Account Ref", Required: true, Indexed: true, Config: refFieldConfig("finance.accounts")},
			{Key: "account_code", Type: "string", Title: "Account Code", Indexed: true},
			{Key: "summary", Type: "string", Title: "Line Summary", Indexed: true},
			{Key: "debit", Type: "number", Title: "Debit", Required: true, Indexed: true},
			{Key: "credit", Type: "number", Title: "Credit", Required: true, Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
		},
		SampleData: []map[string]interface{}{
			{"line_no": "1", "voucher_ref": "VCH-2026-0001", "account_ref": "ACC-1001", "account_code": "1001", "summary": "Cash", "debit": 8800, "credit": 0},
			{"line_no": "2", "voucher_ref": "VCH-2026-0001", "account_ref": "ACC-6001", "account_code": "6001", "summary": "Revenue", "debit": 0, "credit": 8800},
		},
	},
	{
		ID:          "hr.employees",
		Domain:      "hr",
		Name:        "employees",
		Title:       "Employees",
		Description: "Employee profile, organization, role, and employment status.",
		Fields: []DatasetTemplateField{
			{Key: "employee_no", Type: "string", Title: "Employee No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "department", Type: "string", Title: "Department", Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "position", Type: "string", Title: "Position", Indexed: true},
			{Key: "manager", Type: "string", Title: "Manager", Indexed: true},
			{Key: "manager_ref", Type: "record_ref", Title: "Manager Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "employment_status", Type: "string", Title: "Employment Status", Indexed: true, Config: enumFieldConfig("onboarding", "probation", "active", "leave", "transferred", "resigned", "terminated")},
			{Key: "hire_date", Type: "date", Title: "Hire Date", Indexed: true},
			{Key: "mobile", Type: "string", Title: "Mobile", Sensitive: true},
		},
	},
	{
		ID:          "hr.payroll",
		Domain:      "hr",
		Name:        "payroll",
		Title:       "Payroll",
		Description: "Monthly payroll summary. Sensitive by default.",
		Fields: []DatasetTemplateField{
			{Key: "payroll_month", Type: "string", Title: "Payroll Month", Required: true, Indexed: true},
			{Key: "employee_no", Type: "string", Title: "Employee No", Required: true, Indexed: true},
			{Key: "employee_ref", Type: "record_ref", Title: "Employee Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "employee_name", Type: "string", Title: "Employee Name", Required: true, Indexed: true},
			{Key: "department", Type: "string", Title: "Department", Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "gross_pay", Type: "number", Title: "Gross Pay", Sensitive: true},
			{Key: "tax", Type: "number", Title: "Tax", Sensitive: true},
			{Key: "net_pay", Type: "number", Title: "Net Pay", Sensitive: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "active", "suspended", "paid", "cancelled")},
		},
	},
	{
		ID:          "hr.leave_requests",
		Domain:      "hr",
		Name:        "leave_requests",
		Title:       "Leave Requests",
		Description: "Employee leave, vacation, sick leave, business trip, and absence approval records.",
		Fields: []DatasetTemplateField{
			{Key: "leave_no", Type: "string", Title: "Leave No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "employee_no", Type: "string", Title: "Employee No", Required: true, Indexed: true},
			{Key: "employee_ref", Type: "record_ref", Title: "Employee Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "employee_name", Type: "string", Title: "Employee Name", Required: true, Indexed: true},
			{Key: "department", Type: "string", Title: "Department", Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "leave_type", Type: "string", Title: "Leave Type", Required: true, Indexed: true, Config: enumFieldConfig("annual", "sick", "personal", "maternity", "paternity", "business_trip", "remote", "other")},
			{Key: "start_date", Type: "date", Title: "Start Date", Required: true, Indexed: true},
			{Key: "end_date", Type: "date", Title: "End Date", Required: true, Indexed: true},
			{Key: "days", Type: "number", Title: "Days", Required: true, Indexed: true},
			{Key: "reason", Type: "string", Title: "Reason", Sensitive: true},
			{Key: "approver", Type: "string", Title: "Approver", Indexed: true},
			{Key: "approver_ref", Type: "record_ref", Title: "Approver Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "submitted", "approved", "rejected", "cancelled", "closed")},
		},
		SampleData: []map[string]interface{}{{"leave_no": "LV-2026-0001", "employee_no": "E001", "employee_name": "Alice", "department": "Sales", "leave_type": "annual", "start_date": "2026-05-20", "end_date": "2026-05-21", "days": 2, "approver": "Manager", "status": "submitted"}},
	},
	{
		ID:          "legal.contracts",
		Domain:      "legal",
		Name:        "contracts",
		Title:       "Contracts",
		Description: "Contract lifecycle, amount, counterparty, owner, and expiration tracking.",
		Fields: []DatasetTemplateField{
			{Key: "contract_no", Type: "string", Title: "Contract No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "counterparty", Type: "string", Title: "Counterparty", Required: true, Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "supplier_ref", Type: "record_ref", Title: "Supplier Ref", Indexed: true, Config: refFieldConfig("procurement.suppliers")},
			{Key: "project_ref", Type: "record_ref", Title: "Project Ref", Indexed: true, Config: refFieldConfig("company.projects")},
			{Key: "contract_type", Type: "string", Title: "Contract Type", Indexed: true},
			{Key: "amount", Type: "number", Title: "Amount", Indexed: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
			{Key: "owner_ref", Type: "record_ref", Title: "Owner Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "reviewing", "signed", "active", "fulfilled", "expired", "terminated")},
			{Key: "signed_date", Type: "date", Title: "Signed Date", Indexed: true},
			{Key: "expires_at", Type: "date", Title: "Expires At", Indexed: true},
		},
	},
	{
		ID:          "procurement.purchase_orders",
		Domain:      "procurement",
		Name:        "purchase_orders",
		Title:       "Purchase Orders",
		Description: "Supplier purchase orders, amount, requester, approval, and fulfillment tracking.",
		Fields: []DatasetTemplateField{
			{Key: "po_no", Type: "string", Title: "PO No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "supplier", Type: "string", Title: "Supplier", Required: true, Indexed: true},
			{Key: "supplier_ref", Type: "record_ref", Title: "Supplier Ref", Indexed: true, Config: refFieldConfig("procurement.suppliers")},
			{Key: "requester", Type: "string", Title: "Requester", Indexed: true},
			{Key: "requester_ref", Type: "record_ref", Title: "Requester Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "department", Type: "string", Title: "Department", Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "category", Type: "string", Title: "Category", Indexed: true},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "submitted", "approved", "ordered", "partially_received", "received", "cancelled", "closed")},
			{Key: "order_date", Type: "date", Title: "Order Date", Indexed: true},
			{Key: "expected_date", Type: "date", Title: "Expected Date", Indexed: true},
		},
	},
	{
		ID:          "procurement.purchase_order_lines",
		Domain:      "procurement",
		Name:        "purchase_order_lines",
		Title:       "Purchase Order Lines",
		Description: "Purchase order line items: item, quantity, unit price, and amount, each linked to the PO header.",
		Fields:      commercialDocumentLineFields("purchase_order_ref", "procurement.purchase_orders", "Purchase Order Ref"),
		SampleData:  []map[string]interface{}{{"line_no": "1", "purchase_order_ref": "PO-2026-0001", "item_ref": "ITEM-001", "quantity": 10, "unit": "pcs", "unit_price": 120, "amount": 1200}},
	},
	{
		ID:          "procurement.receipts",
		Domain:      "procurement",
		Name:        "receipts",
		Title:       "Purchase Receipts",
		Description: "Inbound purchase receipt headers against a supplier purchase order. Line quantities live in procurement.receipt_lines.",
		Fields: []DatasetTemplateField{
			{Key: "receipt_no", Type: "string", Title: "Receipt No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "supplier_ref", Type: "record_ref", Title: "Supplier Ref", Indexed: true, Config: refFieldConfig("procurement.suppliers")},
			{Key: "purchase_order_ref", Type: "record_ref", Title: "Purchase Order Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_orders")},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "received", "inspected", "posted", "cancelled")},
			{Key: "receipt_date", Type: "date", Title: "Receipt Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"receipt_no": "GRN-2026-0001", "status": "received", "receipt_date": "2026-05-08"}},
	},
	{
		ID:          "procurement.receipt_lines",
		Domain:      "procurement",
		Name:        "receipt_lines",
		Title:       "Purchase Receipt Lines",
		Description: "Inbound receipt line quantities, each linked to the receipt header and shared item/warehouse masters.",
		Fields: append(quantityDocumentLineFields("receipt_ref", "procurement.receipts", "Receipt Ref"),
			DatasetTemplateField{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			DatasetTemplateField{Key: "purchase_order_line_ref", Type: "record_ref", Title: "PO Line Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_order_lines")},
		),
		SampleData: []map[string]interface{}{{"line_no": "1", "receipt_ref": "GRN-2026-0001", "item_ref": "ITEM-001", "quantity": 10, "unit": "pcs"}},
	},
	{
		ID:          "procurement.returns",
		Domain:      "procurement",
		Name:        "returns",
		Title:       "Purchase Returns",
		Description: "Supplier purchase return headers against a receipt or purchase order. Line quantities and amounts live in procurement.return_lines.",
		Fields: []DatasetTemplateField{
			{Key: "return_no", Type: "string", Title: "Return No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "supplier_ref", Type: "record_ref", Title: "Supplier Ref", Indexed: true, Config: refFieldConfig("procurement.suppliers")},
			{Key: "purchase_order_ref", Type: "record_ref", Title: "Purchase Order Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_orders")},
			{Key: "receipt_ref", Type: "record_ref", Title: "Receipt Ref", Indexed: true, Config: refFieldConfig("procurement.receipts")},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "amount", Type: "number", Title: "Amount", Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "reason", Type: "string", Title: "Reason", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "approved", "shipped", "credited", "cancelled")},
			{Key: "return_date", Type: "date", Title: "Return Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"return_no": "PRN-2026-0001", "amount": 240, "currency": "CNY", "reason": "quality", "status": "approved", "return_date": "2026-05-12"}},
	},
	{
		ID:          "procurement.return_lines",
		Domain:      "procurement",
		Name:        "return_lines",
		Title:       "Purchase Return Lines",
		Description: "Purchase return line items: item, quantity, unit price, and amount, each linked to the return header.",
		Fields: append(commercialDocumentLineFields("return_ref", "procurement.returns", "Return Ref"),
			DatasetTemplateField{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			DatasetTemplateField{Key: "purchase_order_line_ref", Type: "record_ref", Title: "PO Line Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_order_lines")},
			DatasetTemplateField{Key: "receipt_line_ref", Type: "record_ref", Title: "Receipt Line Ref", Indexed: true, Config: refFieldConfig("procurement.receipt_lines")},
		),
		SampleData:  []map[string]interface{}{{"line_no": "1", "return_ref": "PRN-2026-0001", "item_ref": "ITEM-001", "quantity": 2, "unit": "pcs", "unit_price": 120, "amount": 240}},
	},
	{
		ID:          "inventory.items",
		Domain:      "inventory",
		Name:        "items",
		Title:       "Inventory Items",
		Description: "Inventory item master: identity and specification (item number, name, unit, status). On-hand quantity lives on stock balances and movements, not as the only stock truth on this row.",
		Fields: []DatasetTemplateField{
			{Key: "item_no", Type: "string", Title: "Item No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "category", Type: "string", Title: "Category", Indexed: true},
			{Key: "spec", Type: "string", Title: "Specification", Indexed: true},
			{Key: "unit", Type: "string", Title: "Unit", Indexed: true},
			{Key: "reorder_level", Type: "number", Title: "Reorder Level", Indexed: true},
			{Key: "default_warehouse_ref", Type: "record_ref", Title: "Default Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "last_po_ref", Type: "record_ref", Title: "Last PO Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_orders")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "inactive", "obsolete")},
		},
	},
	{
		ID:          "inventory.stock_balances",
		Domain:      "inventory",
		Name:        "stock_balances",
		Title:       "Stock Balances",
		Description: "On-hand quantity by item and warehouse. Quantity changes are posted through inventory movements; this table is the balance-by-warehouse view.",
		Fields: []DatasetTemplateField{
			{Key: "item_ref", Type: "record_ref", Title: "Item Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.items")},
			{Key: "item_no", Type: "string", Title: "Item No", Indexed: true},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "warehouse", Type: "string", Title: "Warehouse", Indexed: true},
			{Key: "quantity", Type: "number", Title: "On Hand Quantity", Required: true, Indexed: true},
			{Key: "reserved_quantity", Type: "number", Title: "Reserved Quantity", Indexed: true},
			{Key: "unit", Type: "string", Title: "Unit", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "low_stock", "reserved", "inactive")},
			{Key: "last_movement_at", Type: "datetime", Title: "Last Movement At", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"item_ref": "ITEM-001", "item_no": "ITEM-001", "warehouse_ref": "WH-A", "warehouse": "WH-A", "quantity": 100, "reserved_quantity": 0, "unit": "pcs", "status": "active"}},
	},
	{
		ID:          "inventory.warehouses",
		Domain:      "inventory",
		Name:        "warehouses",
		Title:       "Warehouses",
		Description: "Warehouse master data for stock, movement, transfer, and responsible-person tracking.",
		Fields: []DatasetTemplateField{
			{Key: "warehouse_code", Type: "string", Title: "Warehouse Code", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "location", Type: "string", Title: "Location", Indexed: true},
			{Key: "manager", Type: "string", Title: "Manager", Indexed: true},
			{Key: "manager_ref", Type: "record_ref", Title: "Manager Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "inactive", "maintenance", "closed")},
		},
		SampleData: []map[string]interface{}{{"warehouse_code": "WH-A", "name": "Main Warehouse", "location": "Shanghai", "manager": "Alice", "status": "active"}},
	},
	{
		ID:          "inventory.movements",
		Domain:      "inventory",
		Name:        "movements",
		Title:       "Inventory Movements",
		Description: "Inventory receipt, issue, transfer, adjustment, and reservation movement records.",
		Fields: []DatasetTemplateField{
			{Key: "movement_no", Type: "string", Title: "Movement No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "item_no", Type: "string", Title: "Item No", Required: true, Indexed: true},
			{Key: "item_ref", Type: "record_ref", Title: "Item Ref", Indexed: true, Config: refFieldConfig("inventory.items")},
			{Key: "movement_type", Type: "string", Title: "Movement Type", Required: true, Indexed: true, Config: enumFieldConfig("receipt", "issue", "transfer", "adjustment", "reservation", "release")},
			{Key: "quantity", Type: "number", Title: "Quantity", Required: true, Indexed: true},
			{Key: "unit", Type: "string", Title: "Unit", Indexed: true},
			{Key: "from_warehouse", Type: "string", Title: "From Warehouse", Indexed: true},
			{Key: "from_warehouse_ref", Type: "record_ref", Title: "From Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "to_warehouse", Type: "string", Title: "To Warehouse", Indexed: true},
			{Key: "to_warehouse_ref", Type: "record_ref", Title: "To Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "source_ref", Type: "record_ref", Title: "Source Ref", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "confirmed", "posted", "cancelled")},
			{Key: "occurred_at", Type: "datetime", Title: "Occurred At", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"movement_no": "MOV-2026-0001", "item_no": "ITEM-001", "movement_type": "receipt", "quantity": 100, "unit": "pcs", "to_warehouse": "WH-A", "status": "confirmed", "occurred_at": "2026-05-05T10:00:00Z"}},
	},
	{
		ID:          "inventory.stocktakes",
		Domain:      "inventory",
		Name:        "stocktakes",
		Title:       "Inventory Stocktakes",
		Description: "Physical inventory count headers by warehouse. Counted quantities live in inventory.stocktake_lines.",
		Fields: []DatasetTemplateField{
			{Key: "stocktake_no", Type: "string", Title: "Stocktake No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "counted_by_ref", Type: "record_ref", Title: "Counted By Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "counting", "reviewing", "posted", "cancelled")},
			{Key: "count_date", Type: "date", Title: "Count Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"stocktake_no": "STK-2026-0001", "warehouse_ref": "WH-A", "status": "counting", "count_date": "2026-05-15"}},
	},
	{
		ID:          "inventory.stocktake_lines",
		Domain:      "inventory",
		Name:        "stocktake_lines",
		Title:       "Inventory Stocktake Lines",
		Description: "Physical count lines: item, warehouse, book quantity, and counted quantity, each linked to the stocktake header.",
		Fields:      countedDocumentLineFields("stocktake_ref", "inventory.stocktakes", "Stocktake Ref"),
		SampleData:  []map[string]interface{}{{"line_no": "1", "stocktake_ref": "STK-2026-0001", "item_ref": "ITEM-001", "book_quantity": 100, "counted_quantity": 98, "unit": "pcs"}},
	},
	{
		ID:          "inventory.transfers",
		Domain:      "inventory",
		Name:        "transfers",
		Title:       "Inventory Transfers",
		Description: "Warehouse-to-warehouse transfer headers. Line quantities live in inventory.transfer_lines.",
		Fields: []DatasetTemplateField{
			{Key: "transfer_no", Type: "string", Title: "Transfer No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "from_warehouse_ref", Type: "record_ref", Title: "From Warehouse Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "to_warehouse_ref", Type: "record_ref", Title: "To Warehouse Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "in_transit", "received", "cancelled")},
			{Key: "transfer_date", Type: "date", Title: "Transfer Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"transfer_no": "TRF-2026-0001", "from_warehouse_ref": "WH-A", "to_warehouse_ref": "WH-B", "status": "in_transit", "transfer_date": "2026-05-09"}},
	},
	{
		ID:          "inventory.transfer_lines",
		Domain:      "inventory",
		Name:        "transfer_lines",
		Title:       "Inventory Transfer Lines",
		Description: "Transfer line quantities, each linked to the transfer header and shared item master.",
		Fields: append(quantityDocumentLineFields("transfer_ref", "inventory.transfers", "Transfer Ref"),
			DatasetTemplateField{Key: "from_warehouse_ref", Type: "record_ref", Title: "From Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			DatasetTemplateField{Key: "to_warehouse_ref", Type: "record_ref", Title: "To Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
		),
		SampleData: []map[string]interface{}{{"line_no": "1", "transfer_ref": "TRF-2026-0001", "item_ref": "ITEM-001", "quantity": 20, "unit": "pcs"}},
	},
	{
		ID:          "manufacturing.boms",
		Domain:      "manufacturing",
		Name:        "boms",
		Title:       "Bills of Materials",
		Description: "Finished-item BOM headers. Component rows live in manufacturing.bom_components and reference the shared item master.",
		Fields: []DatasetTemplateField{
			{Key: "bom_no", Type: "string", Title: "BOM No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "item_ref", Type: "record_ref", Title: "Finished Item Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.items")},
			{Key: "item_no", Type: "string", Title: "Finished Item No", Indexed: true},
			{Key: "version", Type: "string", Title: "Version", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "active", "obsolete")},
		},
		SampleData: []map[string]interface{}{{"bom_no": "BOM-2026-0001", "item_ref": "ITEM-001", "item_no": "FG-001", "version": "1", "status": "active"}},
	},
	{
		ID:          "manufacturing.bom_components",
		Domain:      "manufacturing",
		Name:        "bom_components",
		Title:       "BOM Components",
		Description: "BOM component rows: component item and quantity, each linked to the BOM header.",
		Fields:      quantityDocumentLineFields("bom_ref", "manufacturing.boms", "BOM Ref"),
		SampleData:  []map[string]interface{}{{"line_no": "1", "bom_ref": "BOM-2026-0001", "item_ref": "ITEM-001", "quantity": 4, "unit": "pcs"}},
	},
	{
		ID:          "manufacturing.production_orders",
		Domain:      "manufacturing",
		Name:        "production_orders",
		Title:       "Production Orders",
		Description: "Production work-order headers for a finished item. Component issue lines live in manufacturing.production_order_lines.",
		Fields: []DatasetTemplateField{
			{Key: "mo_no", Type: "string", Title: "MO No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "item_ref", Type: "record_ref", Title: "Finished Item Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.items")},
			{Key: "bom_ref", Type: "record_ref", Title: "BOM Ref", Indexed: true, Config: refFieldConfig("manufacturing.boms")},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Output Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "sales_order_ref", Type: "record_ref", Title: "Sales Order Ref", Indexed: true, Config: refFieldConfig("sales.orders")},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "owner_ref", Type: "record_ref", Title: "Owner Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "planned_qty", Type: "number", Title: "Planned Quantity", Required: true, Indexed: true},
			{Key: "completed_qty", Type: "number", Title: "Completed Quantity", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "released", "in_progress", "completed", "cancelled")},
			{Key: "due_date", Type: "date", Title: "Due Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"mo_no": "MO-2026-0001", "item_ref": "ITEM-001", "planned_qty": 50, "status": "released", "due_date": "2026-05-20"}},
	},
	{
		ID:          "manufacturing.production_order_lines",
		Domain:      "manufacturing",
		Name:        "production_order_lines",
		Title:       "Production Order Lines",
		Description: "Production order component issue lines: item, warehouse, and quantity, each linked to the MO header.",
		Fields: append(quantityDocumentLineFields("production_order_ref", "manufacturing.production_orders", "Production Order Ref"),
			DatasetTemplateField{Key: "warehouse_ref", Type: "record_ref", Title: "Issue Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
		),
		SampleData: []map[string]interface{}{{"line_no": "1", "production_order_ref": "MO-2026-0001", "item_ref": "ITEM-001", "quantity": 200, "unit": "pcs"}},
	},
	{
		ID:          "assets.fixed_assets",
		Domain:      "assets",
		Name:        "fixed_assets",
		Title:       "Fixed Assets",
		Description: "Company asset register, ownership, depreciation, and lifecycle status.",
		Fields: []DatasetTemplateField{
			{Key: "asset_no", Type: "string", Title: "Asset No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "asset_type", Type: "string", Title: "Asset Type", Indexed: true},
			{Key: "department", Type: "string", Title: "Department", Indexed: true},
			{Key: "department_ref", Type: "record_ref", Title: "Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "custodian", Type: "string", Title: "Custodian", Indexed: true},
			{Key: "custodian_ref", Type: "record_ref", Title: "Custodian Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "purchase_cost", Type: "number", Title: "Purchase Cost", Indexed: true},
			{Key: "residual_value", Type: "number", Title: "Residual Value", Indexed: true},
			{Key: "useful_life_months", Type: "number", Title: "Useful Life Months", Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "supplier_ref", Type: "record_ref", Title: "Supplier Ref", Indexed: true, Config: refFieldConfig("procurement.suppliers")},
			{Key: "purchase_order_ref", Type: "record_ref", Title: "Purchase Order Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_orders")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("planned", "in_use", "idle", "maintenance", "transferred", "disposed", "lost")},
			{Key: "purchase_date", Type: "date", Title: "Purchase Date", Indexed: true},
			{Key: "location", Type: "string", Title: "Location", Indexed: true},
		},
	},
	{
		ID:          "assets.maintenance_orders",
		Domain:      "assets",
		Name:        "maintenance_orders",
		Title:       "Asset Maintenance Orders",
		Description: "Repair and preventive-maintenance work orders against the single fixed-asset register. Spare-part lines live in assets.maintenance_order_lines.",
		Fields: []DatasetTemplateField{
			{Key: "wo_no", Type: "string", Title: "Work Order No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "asset_ref", Type: "record_ref", Title: "Asset Ref", Required: true, Indexed: true, Config: refFieldConfig("assets.fixed_assets")},
			{Key: "work_type", Type: "string", Title: "Work Type", Indexed: true, Config: enumFieldConfig("repair", "preventive", "inspection", "calibration")},
			{Key: "assignee_ref", Type: "record_ref", Title: "Assignee Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "priority", Type: "string", Title: "Priority", Indexed: true, Config: enumFieldConfig("low", "medium", "high", "critical")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("open", "scheduled", "in_progress", "completed", "cancelled")},
			{Key: "scheduled_at", Type: "datetime", Title: "Scheduled At", Indexed: true},
			{Key: "completed_at", Type: "datetime", Title: "Completed At", Indexed: true},
			{Key: "summary", Type: "string", Title: "Summary", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"wo_no": "AM-2026-0001", "asset_ref": "FA-2026-0001", "work_type": "preventive", "priority": "medium", "status": "scheduled", "summary": "Quarterly service"}},
	},
	{
		ID:          "assets.maintenance_order_lines",
		Domain:      "assets",
		Name:        "maintenance_order_lines",
		Title:       "Asset Maintenance Order Lines",
		Description: "Spare parts consumed on an asset maintenance work order, referencing the shared item master.",
		Fields:      quantityDocumentLineFields("maintenance_order_ref", "assets.maintenance_orders", "Maintenance Order Ref"),
		SampleData:  []map[string]interface{}{{"line_no": "1", "maintenance_order_ref": "AM-2026-0001", "item_ref": "ITEM-001", "quantity": 1, "unit": "pcs"}},
	},
	{
		ID:          "assets.transfers",
		Domain:      "assets",
		Name:        "transfers",
		Title:       "Asset Transfers",
		Description: "Internal asset transfer headers (department or location). Line assets live in assets.transfer_lines and reference the single fixed-asset register.",
		Fields: []DatasetTemplateField{
			{Key: "transfer_no", Type: "string", Title: "Transfer No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "from_department_ref", Type: "record_ref", Title: "From Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "to_department_ref", Type: "record_ref", Title: "To Department Ref", Indexed: true, Config: refFieldConfig("company.departments")},
			{Key: "from_location", Type: "string", Title: "From Location", Indexed: true},
			{Key: "to_location", Type: "string", Title: "To Location", Indexed: true},
			{Key: "requested_by_ref", Type: "record_ref", Title: "Requested By Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "approved", "completed", "cancelled")},
			{Key: "transfer_date", Type: "date", Title: "Transfer Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"transfer_no": "AT-2026-0001", "from_location": "HQ-A", "to_location": "Plant-B", "status": "approved", "transfer_date": "2026-05-18"}},
	},
	{
		ID:          "assets.transfer_lines",
		Domain:      "assets",
		Name:        "transfer_lines",
		Title:       "Asset Transfer Lines",
		Description: "Assets moved on a transfer document, each linked to the transfer header and the shared asset register.",
		Fields:      assetDocumentLineFields("transfer_ref", "assets.transfers", "Transfer Ref"),
		SampleData:  []map[string]interface{}{{"line_no": "1", "transfer_ref": "AT-2026-0001", "asset_ref": "FA-2026-0001"}},
	},
	{
		ID:          "assets.disposals",
		Domain:      "assets",
		Name:        "disposals",
		Title:       "Asset Disposals",
		Description: "Asset disposal/scrap/sale headers. Disposed assets live in assets.disposal_lines and reference the single fixed-asset register.",
		Fields: []DatasetTemplateField{
			{Key: "disposal_no", Type: "string", Title: "Disposal No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "method", Type: "string", Title: "Method", Indexed: true, Config: enumFieldConfig("sale", "scrap", "donation", "write_off")},
			{Key: "reason", Type: "string", Title: "Reason", Indexed: true},
			{Key: "approved_by_ref", Type: "record_ref", Title: "Approved By Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "approved", "completed", "cancelled")},
			{Key: "disposal_date", Type: "date", Title: "Disposal Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"disposal_no": "AD-2026-0001", "method": "scrap", "reason": "end of life", "status": "approved", "disposal_date": "2026-05-30"}},
	},
	{
		ID:          "assets.disposal_lines",
		Domain:      "assets",
		Name:        "disposal_lines",
		Title:       "Asset Disposal Lines",
		Description: "Assets disposed on a disposal document, each linked to the disposal header and the shared asset register.",
		Fields: append(assetDocumentLineFields("disposal_ref", "assets.disposals", "Disposal Ref"),
			DatasetTemplateField{Key: "salvage_amount", Type: "number", Title: "Salvage Amount", Indexed: true},
			DatasetTemplateField{Key: "book_value", Type: "number", Title: "Book Value", Indexed: true},
		),
		SampleData: []map[string]interface{}{{"line_no": "1", "disposal_ref": "AD-2026-0001", "asset_ref": "FA-2026-0001", "salvage_amount": 500, "book_value": 1200}},
	},
	{
		ID:          "assets.depreciation_runs",
		Domain:      "assets",
		Name:        "depreciation_runs",
		Title:       "Asset Depreciation Runs",
		Description: "Period depreciation run headers. Per-asset amounts live in assets.depreciation_lines and reference the single fixed-asset register.",
		Fields: []DatasetTemplateField{
			{Key: "run_no", Type: "string", Title: "Run No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "period", Type: "string", Title: "Period", Required: true, Indexed: true},
			{Key: "method", Type: "string", Title: "Method", Indexed: true, Config: enumFieldConfig("straight_line", "declining_balance", "units_of_production")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "posted", "reversed")},
			{Key: "posted_at", Type: "datetime", Title: "Posted At", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"run_no": "DEP-2026-05", "period": "2026-05", "method": "straight_line", "status": "draft"}},
	},
	{
		ID:          "assets.depreciation_lines",
		Domain:      "assets",
		Name:        "depreciation_lines",
		Title:       "Asset Depreciation Lines",
		Description: "Per-asset depreciation amounts for a run, each linked to the run header and the shared asset register.",
		Fields: append(assetDocumentLineFields("run_ref", "assets.depreciation_runs", "Depreciation Run Ref"),
			DatasetTemplateField{Key: "amount", Type: "number", Title: "Depreciation Amount", Required: true, Indexed: true},
			DatasetTemplateField{Key: "book_value", Type: "number", Title: "Book Value After", Indexed: true},
		),
		SampleData: []map[string]interface{}{{"line_no": "1", "run_ref": "DEP-2026-05", "asset_ref": "FA-2026-0001", "amount": 800, "book_value": 9200}},
	},
}

func (s *Service) ListTemplates(ctx context.Context, p Principal, query ...QueryTemplatesInput) ([]DatasetTemplate, error) {
	_ = ctx
	out := append([]DatasetTemplate(nil), datasetTemplates...)
	if p.Policy != nil {
		out = filterCapabilityTemplates(p, out)
	}
	if len(query) == 0 {
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out, nil
	}
	return paginateTemplates(out, query[0]), nil
}

func paginateTemplates(items []DatasetTemplate, in QueryTemplatesInput) []DatasetTemplate {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := append([]DatasetTemplate(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, tmpl := range out {
			if tmpl.ID < beforeID {
				filtered = append(filtered, tmpl)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) GetTemplate(ctx context.Context, p Principal, templateID string) (*DatasetTemplate, error) {
	_ = ctx
	_ = p
	id := strings.TrimSpace(templateID)
	for _, tmpl := range datasetTemplates {
		if tmpl.ID == id {
			clone := tmpl
			clone.Fields = append([]DatasetTemplateField(nil), tmpl.Fields...)
			clone.SampleData = append([]map[string]interface{}(nil), tmpl.SampleData...)
			return &clone, nil
		}
	}
	return nil, ErrDatasetNotFound
}

func (s *Service) CreateDatasetFromTemplate(ctx context.Context, p Principal, templateID string, in CreateFromTemplateInput) (*CreateFromTemplateResult, error) {
	tmpl, err := s.GetTemplate(ctx, p, templateID)
	if err != nil {
		return nil, err
	}
	create := CreateDatasetInput{
		ID:          strings.TrimSpace(in.ID),
		Domain:      firstNonEmpty(in.Domain, tmpl.Domain),
		Name:        firstNonEmpty(in.Name, tmpl.Name),
		Title:       firstNonEmpty(in.Title, tmpl.Title),
		Description: firstNonEmpty(in.Description, tmpl.Description),
	}
	dataset, err := s.CreateDataset(ctx, p, create)
	if err != nil {
		return nil, err
	}
	fields := make([]FieldDefinition, 0, len(tmpl.Fields))
	for _, field := range tmpl.Fields {
		fields = append(fields, FieldDefinition{Key: field.Key, Type: field.Type, Title: field.Title, Required: field.Required, Indexed: field.Indexed, Sensitive: field.Sensitive, Config: field.Config})
	}
	createdFields, err := s.UpsertFields(ctx, p, dataset.ID, UpsertFieldsInput{Fields: fields})
	if err != nil {
		return nil, err
	}
	return &CreateFromTemplateResult{Dataset: dataset, Fields: createdFields}, nil
}

func (s *Service) BootstrapTemplates(ctx context.Context, p Principal, in BootstrapTemplatesInput) (*BootstrapTemplatesResult, error) {
	templateIDs := append([]string(nil), in.TemplateIDs...)
	if len(templateIDs) == 0 {
		templateIDs = make([]string, 0, len(datasetTemplates))
		domainSet := normalizeDomainSet(in.Domains)
		for _, tmpl := range datasetTemplates {
			if len(domainSet) > 0 {
				if _, ok := domainSet[strings.ToLower(strings.TrimSpace(tmpl.Domain))]; !ok {
					continue
				}
			}
			templateIDs = append(templateIDs, tmpl.ID)
		}
	}
	sort.Strings(templateIDs)
	out := &BootstrapTemplatesResult{Created: []CreateFromTemplateResult{}, WouldCreate: []DatasetTemplate{}, Errors: map[string]string{}}
	seen := map[string]struct{}{}
	for _, templateID := range templateIDs {
		templateID = strings.TrimSpace(templateID)
		if templateID == "" {
			continue
		}
		if _, ok := seen[templateID]; ok {
			continue
		}
		seen[templateID] = struct{}{}
		tmpl, err := s.GetTemplate(ctx, p, templateID)
		if err != nil {
			out.Errors[templateID] = err.Error()
			continue
		}
		if _, err := s.GetDataset(ctx, p, tmpl.ID); err == nil {
			out.Skipped = append(out.Skipped, templateID)
			continue
		} else if !errors.Is(err, ErrDatasetNotFound) {
			out.Errors[templateID] = err.Error()
			continue
		}
		if in.DryRun {
			out.WouldCreate = append(out.WouldCreate, *tmpl)
			continue
		}
		result, err := s.CreateDatasetFromTemplate(ctx, p, templateID, CreateFromTemplateInput{})
		if err == nil {
			out.Created = append(out.Created, *result)
			continue
		}
		if errors.Is(err, ErrAlreadyExists) || strings.Contains(err.Error(), ErrAlreadyExists.Error()) {
			out.Skipped = append(out.Skipped, templateID)
			continue
		}
		out.Errors[templateID] = err.Error()
		if !in.SkipExisting {
			continue
		}
	}
	if len(out.Errors) == 0 {
		out.Errors = nil
	}
	if len(out.WouldCreate) == 0 {
		out.WouldCreate = nil
	}
	return out, nil
}

func normalizeDomainSet(domains []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain != "" {
			out[domain] = struct{}{}
		}
	}
	return out
}

func uniqueFieldConfig() map[string]any {
	return map[string]any{"unique": true}
}

func refFieldConfig(datasetID string) map[string]any {
	return map[string]any{"ref_dataset": strings.TrimSpace(datasetID)}
}

func enumFieldConfig(values ...string) map[string]any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return map[string]any{"enum": out}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// misHeaderLineSpec describes a shipped header document and its line dataset.
// Catalog construction and tests walk this list so they cannot drift.
type misHeaderLineSpec struct {
	HeaderTemplateID  string
	LineTemplateID    string
	HeaderRefField    string
	LineQtyAmountKeys []string
}

func shippedMISHeaderLineSpecs() []misHeaderLineSpec {
	return []misHeaderLineSpec{
		{HeaderTemplateID: "sales.orders", LineTemplateID: "sales.order_lines", HeaderRefField: "order_ref", LineQtyAmountKeys: []string{"item_ref", "quantity", "unit_price"}},
		{HeaderTemplateID: "sales.quotes", LineTemplateID: "sales.quote_lines", HeaderRefField: "quote_ref", LineQtyAmountKeys: []string{"item_ref", "quantity", "unit_price"}},
		{HeaderTemplateID: "sales.shipments", LineTemplateID: "sales.shipment_lines", HeaderRefField: "shipment_ref", LineQtyAmountKeys: []string{"item_ref", "quantity"}},
		{HeaderTemplateID: "sales.returns", LineTemplateID: "sales.return_lines", HeaderRefField: "return_ref", LineQtyAmountKeys: []string{"item_ref", "quantity", "unit_price"}},
		{HeaderTemplateID: "procurement.purchase_orders", LineTemplateID: "procurement.purchase_order_lines", HeaderRefField: "purchase_order_ref", LineQtyAmountKeys: []string{"item_ref", "quantity", "unit_price"}},
		{HeaderTemplateID: "procurement.receipts", LineTemplateID: "procurement.receipt_lines", HeaderRefField: "receipt_ref", LineQtyAmountKeys: []string{"item_ref", "quantity"}},
		{HeaderTemplateID: "procurement.returns", LineTemplateID: "procurement.return_lines", HeaderRefField: "return_ref", LineQtyAmountKeys: []string{"item_ref", "quantity", "unit_price"}},
		{HeaderTemplateID: "inventory.transfers", LineTemplateID: "inventory.transfer_lines", HeaderRefField: "transfer_ref", LineQtyAmountKeys: []string{"item_ref", "quantity"}},
		{HeaderTemplateID: "manufacturing.boms", LineTemplateID: "manufacturing.bom_components", HeaderRefField: "bom_ref", LineQtyAmountKeys: []string{"item_ref", "quantity"}},
		{HeaderTemplateID: "manufacturing.production_orders", LineTemplateID: "manufacturing.production_order_lines", HeaderRefField: "production_order_ref", LineQtyAmountKeys: []string{"item_ref", "quantity"}},
		{HeaderTemplateID: "assets.maintenance_orders", LineTemplateID: "assets.maintenance_order_lines", HeaderRefField: "maintenance_order_ref", LineQtyAmountKeys: []string{"item_ref", "quantity"}},
		{HeaderTemplateID: "assets.transfers", LineTemplateID: "assets.transfer_lines", HeaderRefField: "transfer_ref", LineQtyAmountKeys: []string{"asset_ref"}},
		{HeaderTemplateID: "assets.disposals", LineTemplateID: "assets.disposal_lines", HeaderRefField: "disposal_ref", LineQtyAmountKeys: []string{"asset_ref"}},
		{HeaderTemplateID: "assets.depreciation_runs", LineTemplateID: "assets.depreciation_lines", HeaderRefField: "run_ref", LineQtyAmountKeys: []string{"asset_ref", "amount"}},
		{HeaderTemplateID: "finance.invoices", LineTemplateID: "finance.invoice_lines", HeaderRefField: "invoice_ref", LineQtyAmountKeys: []string{"item_ref", "quantity", "unit_price"}},
		{HeaderTemplateID: "finance.vouchers", LineTemplateID: "finance.voucher_lines", HeaderRefField: "voucher_ref", LineQtyAmountKeys: []string{"account_ref", "debit", "credit"}},
		{HeaderTemplateID: "inventory.stocktakes", LineTemplateID: "inventory.stocktake_lines", HeaderRefField: "stocktake_ref", LineQtyAmountKeys: []string{"item_ref", "counted_quantity"}},
	}
}

func shippedPreviouslyPublishedTemplateIDs() []string {
	return []string{
		"company.departments", "company.projects",
		"sales.orders", "sales.order_lines", "sales.quotes", "sales.quote_lines",
		"sales.shipments", "sales.shipment_lines", "sales.returns", "sales.return_lines",
		"sales.service_tickets", "sales.customers", "sales.contacts", "sales.opportunities",
		"procurement.suppliers", "procurement.purchase_orders", "procurement.purchase_order_lines",
		"procurement.receipts", "procurement.receipt_lines",
		"finance.expenses", "finance.invoices", "finance.invoice_lines", "finance.payments",
		"finance.accounts", "finance.bank_accounts", "finance.budgets", "finance.vouchers", "finance.voucher_lines",
		"hr.employees", "hr.payroll", "hr.leave_requests",
		"legal.contracts",
		"inventory.items", "inventory.stock_balances", "inventory.warehouses", "inventory.movements",
		"inventory.stocktakes", "inventory.stocktake_lines",
		"inventory.transfers", "inventory.transfer_lines",
		"procurement.returns", "procurement.return_lines",
		"manufacturing.boms", "manufacturing.bom_components",
		"manufacturing.production_orders", "manufacturing.production_order_lines",
		"assets.fixed_assets", "assets.maintenance_orders", "assets.maintenance_order_lines",
		"assets.transfers", "assets.transfer_lines", "assets.disposals", "assets.disposal_lines",
		"assets.depreciation_runs", "assets.depreciation_lines",
	}
}

func shippedMISSharedMasterRoles() []string {
	return []string{"customer", "supplier", "inventory_item", "department", "employee", "inventory_warehouse", "chart_of_accounts", "fixed_asset", "bank_account", "project"}
}

func commercialDocumentLineFields(headerRefKey, headerDatasetID, headerTitle string) []DatasetTemplateField {
	return []DatasetTemplateField{
		{Key: "line_no", Type: "string", Title: "Line No", Required: true, Indexed: true},
		{Key: headerRefKey, Type: "record_ref", Title: headerTitle, Required: true, Indexed: true, Config: refFieldConfig(headerDatasetID)},
		{Key: "item_ref", Type: "record_ref", Title: "Item Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.items")},
		{Key: "item_no", Type: "string", Title: "Item No", Indexed: true},
		{Key: "quantity", Type: "number", Title: "Quantity", Required: true, Indexed: true},
		{Key: "unit", Type: "string", Title: "Unit", Indexed: true},
		{Key: "unit_price", Type: "number", Title: "Unit Price", Required: true, Indexed: true},
		{Key: "amount", Type: "number", Title: "Amount", Indexed: true},
	}
}

func assetDocumentLineFields(headerRefKey, headerDatasetID, headerTitle string) []DatasetTemplateField {
	return []DatasetTemplateField{
		{Key: "line_no", Type: "string", Title: "Line No", Required: true, Indexed: true},
		{Key: headerRefKey, Type: "record_ref", Title: headerTitle, Required: true, Indexed: true, Config: refFieldConfig(headerDatasetID)},
		{Key: "asset_ref", Type: "record_ref", Title: "Asset Ref", Required: true, Indexed: true, Config: refFieldConfig("assets.fixed_assets")},
	}
}

func quantityDocumentLineFields(headerRefKey, headerDatasetID, headerTitle string) []DatasetTemplateField {
	return []DatasetTemplateField{
		{Key: "line_no", Type: "string", Title: "Line No", Required: true, Indexed: true},
		{Key: headerRefKey, Type: "record_ref", Title: headerTitle, Required: true, Indexed: true, Config: refFieldConfig(headerDatasetID)},
		{Key: "item_ref", Type: "record_ref", Title: "Item Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.items")},
		{Key: "item_no", Type: "string", Title: "Item No", Indexed: true},
		{Key: "quantity", Type: "number", Title: "Quantity", Required: true, Indexed: true},
		{Key: "unit", Type: "string", Title: "Unit", Indexed: true},
	}
}

func countedDocumentLineFields(headerRefKey, headerDatasetID, headerTitle string) []DatasetTemplateField {
	return []DatasetTemplateField{
		{Key: "line_no", Type: "string", Title: "Line No", Required: true, Indexed: true},
		{Key: headerRefKey, Type: "record_ref", Title: headerTitle, Required: true, Indexed: true, Config: refFieldConfig(headerDatasetID)},
		{Key: "item_ref", Type: "record_ref", Title: "Item Ref", Required: true, Indexed: true, Config: refFieldConfig("inventory.items")},
		{Key: "item_no", Type: "string", Title: "Item No", Indexed: true},
		{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
		{Key: "book_quantity", Type: "number", Title: "Book Quantity", Indexed: true},
		{Key: "counted_quantity", Type: "number", Title: "Counted Quantity", Required: true, Indexed: true},
		{Key: "unit", Type: "string", Title: "Unit", Indexed: true},
	}
}

func shippedTemplateByID(id string) (DatasetTemplate, bool) {
	id = strings.TrimSpace(id)
	for _, tmpl := range datasetTemplates {
		if tmpl.ID == id {
			return tmpl, true
		}
	}
	return DatasetTemplate{}, false
}

func templateFieldByKey(tmpl DatasetTemplate, key string) (DatasetTemplateField, bool) {
	key = strings.TrimSpace(key)
	for _, field := range tmpl.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return DatasetTemplateField{}, false
}

func templateRefDataset(field DatasetTemplateField) string {
	if field.Config == nil {
		return ""
	}
	raw, ok := field.Config["ref_dataset"]
	if !ok {
		return ""
	}
	s, _ := raw.(string)
	return strings.TrimSpace(s)
}

func countShippedTemplatesInDomains(domains ...string) int {
	set := normalizeDomainSet(domains)
	n := 0
	for _, tmpl := range datasetTemplates {
		if _, ok := set[strings.ToLower(strings.TrimSpace(tmpl.Domain))]; ok {
			n++
		}
	}
	return n
}
