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
		ID:          "sales.orders",
		Domain:      "sales",
		Name:        "orders",
		Title:       "Sales Orders",
		Description: "Customer orders, order amount, owner, stage, and payment status.",
		Fields: []DatasetTemplateField{
			{Key: "order_no", Type: "string", Title: "Order No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "customer", Type: "string", Title: "Customer", Required: true, Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "contact_ref", Type: "record_ref", Title: "Contact Ref", Indexed: true, Config: refFieldConfig("sales.contacts")},
			{Key: "opportunity_ref", Type: "record_ref", Title: "Opportunity Ref", Indexed: true, Config: refFieldConfig("sales.opportunities")},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
			{Key: "stage", Type: "string", Title: "Stage", Indexed: true, Config: enumFieldConfig("draft", "confirmed", "won", "lost", "cancelled", "fulfilled")},
			{Key: "payment_status", Type: "string", Title: "Payment Status", Indexed: true, Config: enumFieldConfig("unpaid", "partial", "paid", "refunded", "overdue")},
			{Key: "order_date", Type: "date", Title: "Order Date", Indexed: true},
		},
		SampleData: []map[string]interface{}{{"order_no": "SO-2026-0001", "customer": "Acme", "amount": 8800, "currency": "CNY", "owner": "Alice", "stage": "confirmed", "payment_status": "unpaid", "order_date": "2026-05-05"}},
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
		Description: "Issued and received invoice records.",
		Fields: []DatasetTemplateField{
			{Key: "invoice_no", Type: "string", Title: "Invoice No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "counterparty", Type: "string", Title: "Counterparty", Required: true, Indexed: true},
			{Key: "customer_ref", Type: "record_ref", Title: "Customer Ref", Indexed: true, Config: refFieldConfig("sales.customers")},
			{Key: "contract_ref", Type: "record_ref", Title: "Contract Ref", Indexed: true, Config: refFieldConfig("legal.contracts")},
			{Key: "invoice_type", Type: "string", Title: "Invoice Type", Indexed: true},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "tax_amount", Type: "number", Title: "Tax Amount", Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "issued", "received", "verified", "paid", "voided", "overdue")},
			{Key: "issue_date", Type: "date", Title: "Issue Date", Indexed: true},
		},
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
			{Key: "invoice_ref", Type: "record_ref", Title: "Invoice Ref", Indexed: true, Config: refFieldConfig("finance.invoices")},
			{Key: "expense_ref", Type: "record_ref", Title: "Expense Ref", Indexed: true, Config: refFieldConfig("finance.expenses")},
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
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "inactive", "archived")},
		},
		SampleData: []map[string]interface{}{{"account_code": "1001", "account_name": "Cash", "account_type": "asset", "currency": "CNY", "status": "active"}},
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
			{Key: "summary", Type: "string", Title: "Summary", Indexed: true},
			{Key: "debit_total", Type: "number", Title: "Debit Total", Required: true, Indexed: true},
			{Key: "credit_total", Type: "number", Title: "Credit Total", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "reviewing", "approved", "posted", "reversed", "voided")},
			{Key: "posted_at", Type: "datetime", Title: "Posted At", Indexed: true},
			{Key: "lines", Type: "array", Title: "Voucher Lines"},
		},
		SampleData: []map[string]interface{}{{"voucher_no": "VCH-2026-0001", "period": "2026-05", "voucher_type": "receipt", "summary": "Receive customer payment", "debit_total": 8800, "credit_total": 8800, "currency": "CNY", "status": "draft", "lines": []any{map[string]any{"account_code": "1001", "debit": 8800, "credit": 0}, map[string]any{"account_code": "6001", "debit": 0, "credit": 8800}}}},
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
			{Key: "leave_type", Type: "string", Title: "Leave Type", Required: true, Indexed: true, Config: enumFieldConfig("annual", "sick", "personal", "maternity", "paternity", "business_trip", "remote", "other")},
			{Key: "start_date", Type: "date", Title: "Start Date", Required: true, Indexed: true},
			{Key: "end_date", Type: "date", Title: "End Date", Required: true, Indexed: true},
			{Key: "days", Type: "number", Title: "Days", Required: true, Indexed: true},
			{Key: "reason", Type: "string", Title: "Reason", Sensitive: true},
			{Key: "approver", Type: "string", Title: "Approver", Indexed: true},
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
			{Key: "contract_type", Type: "string", Title: "Contract Type", Indexed: true},
			{Key: "amount", Type: "number", Title: "Amount", Indexed: true},
			{Key: "owner", Type: "string", Title: "Owner", Indexed: true},
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
			{Key: "category", Type: "string", Title: "Category", Indexed: true},
			{Key: "amount", Type: "number", Title: "Amount", Required: true, Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("draft", "submitted", "approved", "ordered", "partially_received", "received", "cancelled", "closed")},
			{Key: "order_date", Type: "date", Title: "Order Date", Indexed: true},
			{Key: "expected_date", Type: "date", Title: "Expected Date", Indexed: true},
		},
	},
	{
		ID:          "inventory.items",
		Domain:      "inventory",
		Name:        "items",
		Title:       "Inventory Items",
		Description: "Inventory item master and stock levels by warehouse.",
		Fields: []DatasetTemplateField{
			{Key: "item_no", Type: "string", Title: "Item No", Required: true, Indexed: true, Config: uniqueFieldConfig()},
			{Key: "name", Type: "string", Title: "Name", Required: true, Indexed: true},
			{Key: "category", Type: "string", Title: "Category", Indexed: true},
			{Key: "warehouse", Type: "string", Title: "Warehouse", Indexed: true},
			{Key: "warehouse_ref", Type: "record_ref", Title: "Warehouse Ref", Indexed: true, Config: refFieldConfig("inventory.warehouses")},
			{Key: "quantity", Type: "number", Title: "Quantity", Indexed: true},
			{Key: "unit", Type: "string", Title: "Unit", Indexed: true},
			{Key: "reorder_level", Type: "number", Title: "Reorder Level", Indexed: true},
			{Key: "last_po_ref", Type: "record_ref", Title: "Last PO Ref", Indexed: true, Config: refFieldConfig("procurement.purchase_orders")},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("active", "low_stock", "reserved", "inactive", "obsolete")},
			{Key: "last_movement_at", Type: "datetime", Title: "Last Movement At", Indexed: true},
		},
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
			{Key: "custodian", Type: "string", Title: "Custodian", Indexed: true},
			{Key: "custodian_ref", Type: "record_ref", Title: "Custodian Ref", Indexed: true, Config: refFieldConfig("hr.employees")},
			{Key: "purchase_cost", Type: "number", Title: "Purchase Cost", Indexed: true},
			{Key: "currency", Type: "string", Title: "Currency", Indexed: true},
			{Key: "status", Type: "string", Title: "Status", Indexed: true, Config: enumFieldConfig("planned", "in_use", "idle", "maintenance", "transferred", "disposed", "lost")},
			{Key: "purchase_date", Type: "date", Title: "Purchase Date", Indexed: true},
			{Key: "location", Type: "string", Title: "Location", Indexed: true},
		},
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
