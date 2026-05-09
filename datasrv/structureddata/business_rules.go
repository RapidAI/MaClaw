package structureddata

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

var businessRules = []BusinessRuleDefinition{
	{
		ID:                "company.department.quality_required",
		Domain:            "company",
		DatasetID:         "company.departments",
		BusinessAction:    "company.department_upsert",
		Title:             "Department master data changes require quality verification",
		Description:       "Department and cost center master data is shared by HR, finance, procurement, and assets, so changes should be previewed and quality checked.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		DefaultApprover:   "operations_manager",
		RecommendedChecks: []string{"validate department code uniqueness", "verify parent department reference", "review downstream reporting impact"},
	},
	{
		ID:               "sales.order.high_value_approval",
		Domain:           "sales",
		DatasetID:        "sales.orders",
		BusinessAction:   "sales.order_upsert",
		Title:            "High-value sales orders require approval awareness",
		Description:      "Large sales commitments should be previewed and routed through approval before downstream fulfillment or revenue reporting.",
		Severity:         "high",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "sales_order",
		RequiresQuality:  true,
		ConditionsMode:   "all",
		Conditions:       []BusinessRuleCondition{{Field: "amount", Op: "gte", Value: 100000, Description: "high-value order threshold"}},
		DefaultApprover:  "sales_manager",
		RecommendedChecks: []string{
			"validate customer and amount before commitment",
			"check contract or quote reference for high-value order",
			"create record approval before fulfillment",
		},
	},
	{
		ID:                "sales.opportunity.quality_required",
		Domain:            "sales",
		DatasetID:         "sales.opportunities",
		BusinessAction:    "sales.opportunity_upsert",
		Title:             "Sales opportunity changes require pipeline quality verification",
		Description:       "Opportunity pipeline data drives forecasting and handoff to orders, so writes should be dry-run and quality checked.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		DefaultApprover:   "sales_manager",
		RecommendedChecks: []string{"validate opportunity number uniqueness", "review customer reference", "verify amount, probability, and close date"},
	},
	{
		ID:                "sales.contact.quality_required",
		Domain:            "sales",
		DatasetID:         "sales.contacts",
		BusinessAction:    "sales.contact_upsert",
		Title:             "Sales contact changes require contact quality verification",
		Description:       "Contact data drives account follow-up and sales communication, so writes should be previewed and checked for customer reference and sensitive communication fields.",
		Severity:          "medium",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		DefaultApprover:   "sales_manager",
		RecommendedChecks: []string{"validate contact number uniqueness", "review customer reference", "verify phone and email sensitivity before export"},
	},
	{
		ID:               "sales.opportunity.stage_risk_approval",
		Domain:           "sales",
		DatasetID:        "sales.opportunities",
		BusinessAction:   "sales.opportunity_stage_update",
		Title:            "Won, lost, or cancelled opportunity stages require approval awareness",
		Description:      "Opportunity win/loss/cancellation affects forecast and downstream order creation, so status changes should be dry-run, quality checked, and approval-aware.",
		Severity:         "high",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "sales_opportunity_stage",
		RequiresQuality:  true,
		ConditionsMode:   "any",
		DefaultApprover:  "sales_manager",
		Conditions: []BusinessRuleCondition{
			{Field: "stage", Op: "in", Value: []any{"won", "lost", "cancelled"}, Description: "material opportunity lifecycle stage"},
		},
		RecommendedChecks: []string{"verify win/loss evidence", "review forecast impact", "run opportunity pipeline report after change"},
	},
	{
		ID:               "finance.expense.approval_required",
		Domain:           "finance",
		DatasetID:        "finance.expenses",
		BusinessAction:   "finance.expense_submit",
		Title:            "Expense submissions require approval workflow",
		Description:      "Expense records should be dry-run first and then submitted for finance approval before payment or settlement.",
		Severity:         "high",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "expense",
		RequiresQuality:  true,
		DefaultApprover:  "finance_manager",
		RecommendedChecks: []string{
			"validate required expense fields",
			"check duplicate expense_no",
			"create record approval after commit when payment is not immediate",
		},
	},
	{
		ID:              "finance.expense.high_value_backup",
		Domain:          "finance",
		DatasetID:       "finance.expenses",
		BusinessAction:  "finance.expense_submit",
		Title:           "High-value expenses require backup checkpoint awareness",
		Description:     "Large expense submissions should create a recoverable checkpoint before bulk correction, import, or payment workflow cleanup.",
		Severity:        "critical",
		RequiresDryRun:  true,
		RequiresBackup:  true,
		RequiresQuality: true,
		ConditionsMode:  "all",
		Conditions:      []BusinessRuleCondition{{Field: "amount", Op: "gte", Value: 50000, Description: "high-value expense threshold"}},
		DefaultApprover: "finance_manager",
		RecommendedChecks: []string{
			"create backup before high-value expense cleanup",
			"verify approver and payment evidence",
			"run finance expense quality check",
		},
	},
	{
		ID:                "finance.invoice.quality_required",
		Domain:            "finance",
		DatasetID:         "finance.invoices",
		BusinessAction:    "finance.invoice_upsert",
		Title:             "Invoice changes require quality verification",
		Description:       "Invoice lifecycle changes should keep tax, amount, counterparty, and status consistent for downstream finance reports.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		RecommendedChecks: []string{"validate invoice required fields", "review status and tax fields before reporting"},
	},
	{
		ID:                "finance.payment.approval_required",
		Domain:            "finance",
		DatasetID:         "finance.payments",
		BusinessAction:    "finance.payment_upsert",
		Title:             "Payment changes require finance approval workflow",
		Description:       "Payment records influence cash movement and settlement state, so agent writes should be dry-run, backed up, quality checked, and approval-aware.",
		Severity:          "critical",
		RequiresDryRun:    true,
		RequiresApproval:  true,
		ApprovalKind:      "finance_payment",
		RequiresBackup:    true,
		RequiresQuality:   true,
		DefaultApprover:   "finance_manager",
		RecommendedChecks: []string{"validate counterparty and amount", "verify invoice or expense reference", "create backup before payment batch changes"},
	},
	{
		ID:               "finance.payment.status_approval_required",
		Domain:           "finance",
		DatasetID:        "finance.payments",
		BusinessAction:   "finance.payment_status_update",
		Title:            "Paid, received, failed, or cancelled payment statuses require approval awareness",
		Description:      "Payment lifecycle status changes should be dry-run and approval-aware before downstream accounting or bank reconciliation actions.",
		Severity:         "critical",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "finance_payment_status",
		RequiresQuality:  true,
		ConditionsMode:   "any",
		DefaultApprover:  "finance_manager",
		Conditions: []BusinessRuleCondition{
			{Field: "status", Op: "in", Value: []any{"paid", "received", "failed", "cancelled"}, Description: "material payment lifecycle status"},
		},
		RecommendedChecks: []string{"verify payment evidence", "review cashflow report impact", "run payment status report after change"},
	},
	{
		ID:                "finance.account.quality_required",
		Domain:            "finance",
		DatasetID:         "finance.accounts",
		BusinessAction:    "finance.account_upsert",
		Title:             "Chart of accounts changes require finance quality verification",
		Description:       "Account codes classify financial activity, so changes should be previewed and quality checked before voucher posting workflows depend on them.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		DefaultApprover:   "finance_manager",
		RecommendedChecks: []string{"validate account code uniqueness", "verify account type", "review active/inactive account usage"},
	},
	{
		ID:                "finance.voucher.approval_required",
		Domain:            "finance",
		DatasetID:         "finance.vouchers",
		BusinessAction:    "finance.voucher_upsert",
		Title:             "Accounting vouchers require finance approval workflow",
		Description:       "Accounting voucher lines affect finance reports and posting workflows, so agent writes should be dry-run, backed up, quality checked, and approval-aware.",
		Severity:          "critical",
		RequiresDryRun:    true,
		RequiresApproval:  true,
		ApprovalKind:      "finance_voucher",
		RequiresBackup:    true,
		RequiresQuality:   true,
		DefaultApprover:   "finance_manager",
		RecommendedChecks: []string{"validate debit and credit totals", "verify voucher period and source reference", "create backup before voucher batch changes"},
	},
	{
		ID:                "finance.budget.approval_required",
		Domain:            "finance",
		DatasetID:         "finance.budgets",
		BusinessAction:    "finance.budget_upsert",
		Title:             "Budget changes require finance approval workflow",
		Description:       "Budgets constrain expense, payment, and planning workflows, so writes should be dry-run, quality checked, and approval-aware.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresApproval:  true,
		ApprovalKind:      "finance_budget",
		RequiresQuality:   true,
		DefaultApprover:   "finance_manager",
		RecommendedChecks: []string{"validate budget period and department", "review budget, committed, and actual amounts", "run budget report after change"},
	},
	{
		ID:               "finance.budget.status_approval_required",
		Domain:           "finance",
		DatasetID:        "finance.budgets",
		BusinessAction:   "finance.budget_status_update",
		Title:            "Budget activation, freeze, closure, or cancellation requires approval awareness",
		Description:      "Material budget lifecycle changes should be dry-run and approval-aware before downstream spending controls depend on them.",
		Severity:         "high",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "finance_budget_status",
		RequiresQuality:  true,
		ConditionsMode:   "any",
		DefaultApprover:  "finance_manager",
		Conditions: []BusinessRuleCondition{
			{Field: "status", Op: "in", Value: []any{"approved", "active", "frozen", "closed", "cancelled"}, Description: "material budget lifecycle status"},
		},
		RecommendedChecks: []string{"verify budget approval evidence", "review expense impact", "run budget status report after change"},
	},
	{
		ID:               "finance.voucher.status_approval_required",
		Domain:           "finance",
		DatasetID:        "finance.vouchers",
		BusinessAction:   "finance.voucher_status_update",
		Title:            "Posted, reversed, or voided voucher statuses require approval awareness",
		Description:      "Voucher posting, reversal, and voiding should be dry-run and approval-aware before downstream finance reporting changes.",
		Severity:         "critical",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "finance_voucher_status",
		RequiresBackup:   true,
		RequiresQuality:  true,
		ConditionsMode:   "any",
		DefaultApprover:  "finance_manager",
		Conditions: []BusinessRuleCondition{
			{Field: "status", Op: "in", Value: []any{"posted", "reversed", "voided"}, Description: "material voucher lifecycle status"},
		},
		RecommendedChecks: []string{"verify posting approval evidence", "review period report impact", "run voucher status report after change"},
	},
	{
		ID:               "sales.order.status_risk_approval",
		Domain:           "sales",
		DatasetID:        "sales.orders",
		BusinessAction:   "sales.order_status_update",
		Title:            "Risky sales order status changes require approval awareness",
		Description:      "Cancellation, loss, refund, or write-off style order status changes should be previewed and approved before downstream reporting changes.",
		Severity:         "high",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "sales_order_status",
		RequiresQuality:  true,
		ConditionsMode:   "any",
		DefaultApprover:  "sales_manager",
		Conditions: []BusinessRuleCondition{
			{Field: "stage", Op: "in", Value: []any{"cancelled", "lost", "write_off"}, Description: "risky sales stage"},
			{Field: "payment_status", Op: "in", Value: []any{"refunded", "chargeback"}, Description: "risky payment status"},
		},
		RecommendedChecks: []string{"verify cancellation/refund evidence", "review revenue report impact", "create record approval before final status change"},
	},
	{
		ID:                "hr.employee.approval_required",
		Domain:            "hr",
		DatasetID:         "hr.employees",
		BusinessAction:    "hr.employee_upsert",
		Title:             "Employee master data changes require HR approval",
		Description:       "Employee roster changes affect people, payroll, and access workflows, so they should be previewed and approved.",
		Severity:          "critical",
		RequiresDryRun:    true,
		RequiresApproval:  true,
		ApprovalKind:      "hr_employee",
		RequiresQuality:   true,
		DefaultApprover:   "hr_manager",
		RecommendedChecks: []string{"validate employee id uniqueness", "verify department and manager references", "review sensitive fields before export"},
	},
	{
		ID:                "hr.payroll.approval_required",
		Domain:            "hr",
		DatasetID:         "hr.payroll",
		BusinessAction:    "hr.payroll_upsert",
		Title:             "Payroll changes require HR approval workflow",
		Description:       "Payroll lines are sensitive and financially material, so agent writes should be dry-run, quality checked, and routed to HR approval before payment.",
		Severity:          "critical",
		RequiresDryRun:    true,
		RequiresApproval:  true,
		ApprovalKind:      "hr_payroll",
		RequiresBackup:    true,
		RequiresQuality:   true,
		DefaultApprover:   "hr_manager",
		RecommendedChecks: []string{"validate payroll month and employee reference", "verify gross/tax/net pay consistency", "create backup before payroll batch changes"},
	},
	{
		ID:               "hr.payroll.status_approval_required",
		Domain:           "hr",
		DatasetID:        "hr.payroll",
		BusinessAction:   "hr.payroll_status_update",
		Title:            "Payroll payment status changes require approval awareness",
		Description:      "Payroll payment and cancellation status changes should be approval-aware and quality checked before downstream payment or accounting actions.",
		Severity:         "critical",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "hr_payroll_status",
		RequiresQuality:  true,
		ConditionsMode:   "any",
		DefaultApprover:  "hr_manager",
		Conditions: []BusinessRuleCondition{
			{Field: "status", Op: "in", Value: []any{"paid", "cancelled", "suspended"}, Description: "sensitive payroll lifecycle status"},
		},
		RecommendedChecks: []string{"verify payment approval evidence", "review payroll status impact", "run payroll status report after change"},
	},
	{
		ID:                "hr.leave.approval_required",
		Domain:            "hr",
		DatasetID:         "hr.leave_requests",
		BusinessAction:    "hr.leave_request_upsert",
		Title:             "Leave requests require HR approval awareness",
		Description:       "Leave and absence records affect attendance, staffing, and payroll inputs, so writes should be previewed, quality checked, and routed through HR approval.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresApproval:  true,
		ApprovalKind:      "hr_leave",
		RequiresQuality:   true,
		DefaultApprover:   "hr_manager",
		RecommendedChecks: []string{"validate employee reference", "check leave date range and days", "review overlapping leave before approval"},
	},
	{
		ID:               "hr.leave.status_approval_required",
		Domain:           "hr",
		DatasetID:        "hr.leave_requests",
		BusinessAction:   "hr.leave_request_status_update",
		Title:            "Leave approval status changes require HR approval awareness",
		Description:      "Approving, rejecting, or cancelling leave should be approval-aware and quality checked before attendance and payroll downstream use.",
		Severity:         "high",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "hr_leave_status",
		RequiresQuality:  true,
		ConditionsMode:   "any",
		DefaultApprover:  "hr_manager",
		Conditions: []BusinessRuleCondition{
			{Field: "status", Op: "in", Value: []any{"approved", "rejected", "cancelled"}, Description: "material leave lifecycle status"},
		},
		RecommendedChecks: []string{"verify manager approval evidence", "review leave balance or overlap", "run leave status report after change"},
	},
	{
		ID:                "procurement.purchase.approval_required",
		Domain:            "procurement",
		DatasetID:         "procurement.purchase_orders",
		BusinessAction:    "procurement.purchase_order_upsert",
		Title:             "Purchase orders require approval before fulfillment",
		Description:       "Purchase orders influence spend commitments and receiving workflows, so agent writes should be dry-run and approval-aware.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresApproval:  true,
		ApprovalKind:      "purchase",
		RequiresQuality:   true,
		DefaultApprover:   "procurement_manager",
		RecommendedChecks: []string{"validate supplier and amount", "check approval_status before receiving", "run procurement reports after import"},
	},
	{
		ID:                "procurement.supplier.quality_required",
		Domain:            "procurement",
		DatasetID:         "procurement.suppliers",
		BusinessAction:    "procurement.supplier_upsert",
		Title:             "Supplier master data changes require quality verification",
		Description:       "Supplier data is shared by procurement, finance, payments, invoices, and contracts, so agent writes should be previewed and quality checked.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		DefaultApprover:   "procurement_manager",
		RecommendedChecks: []string{"validate supplier number uniqueness", "review payment terms and tax number", "check downstream purchase/payment references"},
	},
	{
		ID:                "inventory.movement.quality_required",
		Domain:            "inventory",
		DatasetID:         "inventory.movements",
		BusinessAction:    "inventory.movement_record",
		Title:             "Inventory movements require quality verification",
		Description:       "Inventory movement records explain stock changes, so writes should be dry-run and checked for item reference, movement type, quantity, and warehouse fields.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		DefaultApprover:   "inventory_manager",
		RecommendedChecks: []string{"validate item reference", "verify movement type and quantity", "review warehouse fields before posting"},
	},
	{
		ID:                "inventory.warehouse.quality_required",
		Domain:            "inventory",
		DatasetID:         "inventory.warehouses",
		BusinessAction:    "inventory.warehouse_upsert",
		Title:             "Warehouse master data changes require quality verification",
		Description:       "Warehouse master data is shared by stock, movements, transfers, and responsible-person workflows, so writes should be previewed and quality checked.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		DefaultApprover:   "inventory_manager",
		RecommendedChecks: []string{"validate warehouse code uniqueness", "verify manager reference", "review downstream stock and movement reports"},
	},
	{
		ID:               "inventory.movement.status_approval_required",
		Domain:           "inventory",
		DatasetID:        "inventory.movements",
		BusinessAction:   "inventory.movement_status_update",
		Title:            "Posted or cancelled inventory movements require approval awareness",
		Description:      "Posting or cancelling inventory movements affects stock traceability and downstream reports, so status changes should be dry-run, quality checked, and approval-aware.",
		Severity:         "high",
		RequiresDryRun:   true,
		RequiresApproval: true,
		ApprovalKind:     "inventory_movement_status",
		RequiresQuality:  true,
		ConditionsMode:   "any",
		DefaultApprover:  "inventory_manager",
		Conditions: []BusinessRuleCondition{
			{Field: "status", Op: "in", Value: []any{"posted", "cancelled"}, Description: "material inventory movement status"},
		},
		RecommendedChecks: []string{"verify movement evidence", "review stock impact", "run inventory movement report after change"},
	},
	{
		ID:                "assets.fixed_asset.backup_required",
		Domain:            "assets",
		DatasetID:         "assets.fixed_assets",
		BusinessAction:    "assets.fixed_asset_upsert",
		Title:             "Fixed asset register changes require backup checkpoint",
		Description:       "Asset register changes affect depreciation, custody, and audit reporting; create a backup before bulk changes.",
		Severity:          "high",
		RequiresDryRun:    true,
		RequiresBackup:    true,
		RequiresQuality:   true,
		RecommendedChecks: []string{"create backup before bulk asset import", "validate asset_no uniqueness", "review custodian and location changes"},
	},
	{
		ID:                "bulk.schema.admin_required",
		Title:             "Schema and bulk operations require data_admin governance",
		Description:       "Dataset, field, schema proposal apply, restore, and bulk mutation operations require explicit administrative confirmation.",
		Severity:          "critical",
		RequiresDryRun:    true,
		RequiresBackup:    true,
		RequiresQuality:   true,
		RequiresAdmin:     true,
		RecommendedChecks: []string{"create backup", "dry-run operation plan", "run quality check after apply"},
	},
	{
		ID:                "connector.production_sync.guardrails",
		Title:             "Production connector sync requires readiness, dry-run, and checkpointing",
		Description:       "CRM, ERP, HRIS, finance, warehouse, and asset connector sync should use sync-plan before production writes.",
		Severity:          "critical",
		RequiresDryRun:    true,
		RequiresQuality:   true,
		RequiresAdmin:     true,
		RecommendedChecks: []string{"validate connector config", "check connector readiness", "dry-run first page", "monitor dead letters"},
	},
}

func (s *Service) ListBusinessRules(ctx context.Context, p Principal, in QueryBusinessRulesInput) ([]BusinessRuleDefinition, error) {
	_ = ctx
	_ = p
	severity := strings.ToLower(strings.TrimSpace(in.Severity))
	if severity != "" && accessReviewSeverityRank(severity) == 0 {
		return nil, fmt.Errorf("%w: invalid severity", ErrInvalidInput)
	}
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	beforeID := strings.TrimSpace(in.BeforeID)
	out := []BusinessRuleDefinition{}
	for _, rule := range businessRules {
		if !businessRuleMatches(rule, in) {
			continue
		}
		out = append(out, cloneBusinessRules([]BusinessRuleDefinition{rule})[0])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if beforeID != "" {
		filtered := out[:0]
		for _, rule := range out {
			if rule.ID < beforeID {
				filtered = append(filtered, rule)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Service) EvaluateBusinessRules(ctx context.Context, p Principal, in EvaluateBusinessRulesInput) (*BusinessRuleEvaluation, error) {
	_ = ctx
	actionID := strings.TrimSpace(in.BusinessAction)
	datasetID := strings.TrimSpace(in.DatasetID)
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if actionID != "" {
		if action, ok := findBusinessAction(actionID); ok {
			if datasetID == "" {
				datasetID = action.DatasetID
			}
			if domain == "" {
				domain = action.Domain
			}
		}
	}
	rules, _ := s.ListBusinessRules(ctx, p, QueryBusinessRulesInput{Domain: domain, DatasetID: datasetID, BusinessAction: actionID})
	out := &BusinessRuleEvaluation{BusinessAction: actionID, DatasetID: datasetID, Domain: domain, DryRun: in.DryRun}
	checkSet := map[string]struct{}{}
	for _, rule := range rules {
		ruleMatch := evaluateBusinessRuleConditions(rule, in.Data)
		out.RuleEvaluations = append(out.RuleEvaluations, ruleMatch)
		if !ruleMatch.Applies {
			continue
		}
		out.MatchedRules = append(out.MatchedRules, rule)
		out.RequiresDryRun = out.RequiresDryRun || rule.RequiresDryRun
		out.RequiresApproval = out.RequiresApproval || rule.RequiresApproval
		out.RequiresBackup = out.RequiresBackup || rule.RequiresBackup
		out.RequiresQuality = out.RequiresQuality || rule.RequiresQuality
		out.RequiresAdmin = out.RequiresAdmin || rule.RequiresAdmin
		for _, check := range rule.RecommendedChecks {
			check = strings.TrimSpace(check)
			if check != "" {
				checkSet[check] = struct{}{}
			}
		}
	}
	for check := range checkSet {
		out.RecommendedChecks = append(out.RecommendedChecks, check)
	}
	sort.Strings(out.RecommendedChecks)
	out.Summary = businessRuleEvaluationSummary(*out)
	s.applyBusinessRuleApprovalState(ctx, p, out, in)
	out.NextSteps = businessRuleNextSteps(*out, in)
	out.GovernanceStatus, out.StatusReasons = businessRuleGovernanceStatus(*out, p)
	out.GateStatuses = businessRuleGateStatuses(*out, p)
	out.CanExecuteNow, out.RecommendedAction = businessRuleExecutionAdvice(*out)
	return out, nil
}

func businessRuleMatches(rule BusinessRuleDefinition, in QueryBusinessRulesInput) bool {
	hasScopeFilter := strings.TrimSpace(in.Domain) != "" || strings.TrimSpace(in.DatasetID) != "" || strings.TrimSpace(in.BusinessAction) != ""
	ruleIsGlobal := strings.TrimSpace(rule.Domain) == "" && strings.TrimSpace(rule.DatasetID) == "" && strings.TrimSpace(rule.BusinessAction) == ""
	if hasScopeFilter && ruleIsGlobal {
		return false
	}
	if domain := strings.ToLower(strings.TrimSpace(in.Domain)); domain != "" && strings.ToLower(rule.Domain) != domain && rule.Domain != "" {
		return false
	}
	if datasetID := strings.TrimSpace(in.DatasetID); datasetID != "" && rule.DatasetID != datasetID && rule.DatasetID != "" {
		return false
	}
	if actionID := strings.TrimSpace(in.BusinessAction); actionID != "" && rule.BusinessAction != actionID && rule.BusinessAction != "" {
		return false
	}
	if severity := strings.ToLower(strings.TrimSpace(in.Severity)); severity != "" && strings.ToLower(rule.Severity) != severity {
		return false
	}
	return true
}

func businessRuleAppliesToData(rule BusinessRuleDefinition, data map[string]any) bool {
	return evaluateBusinessRuleConditions(rule, data).Applies
}

func evaluateBusinessRuleConditions(rule BusinessRuleDefinition, data map[string]any) BusinessRuleMatch {
	out := BusinessRuleMatch{RuleID: rule.ID, ConditionsMode: strings.ToLower(strings.TrimSpace(rule.ConditionsMode))}
	if len(rule.Conditions) == 0 {
		out.Applies = true
		return out
	}
	if out.ConditionsMode == "" {
		out.ConditionsMode = "all"
	}
	if out.ConditionsMode == "any" {
		for _, condition := range rule.Conditions {
			result := evaluateBusinessRuleCondition(condition, data)
			out.ConditionResults = append(out.ConditionResults, result)
			if result.Matched {
				out.Applies = true
			}
		}
		return out
	}
	out.Applies = true
	for _, condition := range rule.Conditions {
		result := evaluateBusinessRuleCondition(condition, data)
		out.ConditionResults = append(out.ConditionResults, result)
		if !result.Matched {
			out.Applies = false
		}
	}
	return out
}

func businessRuleConditionApplies(condition BusinessRuleCondition, data map[string]any) bool {
	return evaluateBusinessRuleCondition(condition, data).Matched
}

func evaluateBusinessRuleCondition(condition BusinessRuleCondition, data map[string]any) BusinessRuleConditionEvaluation {
	out := BusinessRuleConditionEvaluation{Condition: condition}
	field := strings.TrimSpace(condition.Field)
	if field == "" {
		out.Matched = true
		out.Reason = "empty field condition treated as matched"
		return out
	}
	actual, ok := businessRuleConditionValue(data, field)
	out.Actual = actual
	op := strings.ToLower(strings.TrimSpace(condition.Op))
	if op == "" {
		op = "eq"
	}
	switch op {
	case "exists", "present":
		out.Matched = ok
		if !ok {
			out.Reason = "field missing"
		}
		return out
	case "not_exists", "missing":
		out.Matched = !ok
		if ok {
			out.Reason = "field present"
		}
		return out
	}
	if !ok {
		out.Reason = "field missing"
		return out
	}
	switch op {
	case "gt", "gte", "lt", "lte":
		left, leftOK := numberFromAny(actual)
		right, rightOK := numberFromAny(condition.Value)
		if !leftOK || !rightOK {
			out.Reason = "numeric comparison requires numeric actual and value"
			return out
		}
		switch op {
		case "gt":
			out.Matched = left > right
		case "gte":
			out.Matched = left >= right
		case "lt":
			out.Matched = left < right
		case "lte":
			out.Matched = left <= right
		}
	case "eq":
		out.Matched = strings.EqualFold(strings.TrimSpace(anyToRuleString(actual)), strings.TrimSpace(anyToRuleString(condition.Value)))
	case "neq":
		out.Matched = !strings.EqualFold(strings.TrimSpace(anyToRuleString(actual)), strings.TrimSpace(anyToRuleString(condition.Value)))
	case "contains":
		out.Matched = strings.Contains(strings.ToLower(anyToRuleString(actual)), strings.ToLower(anyToRuleString(condition.Value)))
	case "in":
		out.Matched = businessRuleValueIn(actual, condition.Value)
	case "not_in":
		out.Matched = !businessRuleValueIn(actual, condition.Value)
	case "empty":
		out.Matched = strings.TrimSpace(anyToRuleString(actual)) == ""
	case "not_empty":
		out.Matched = strings.TrimSpace(anyToRuleString(actual)) != ""
	default:
		out.Reason = "unsupported operator"
		return out
	}
	if !out.Matched && out.Reason == "" {
		out.Reason = "condition not matched"
	}
	return out
}

func businessRuleConditionValue(data map[string]any, field string) (any, bool) {
	if data == nil {
		return nil, false
	}
	if value, ok := data[field]; ok {
		return value, true
	}
	var current any = data
	for _, part := range strings.Split(field, ".") {
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

func businessRuleValueIn(actual any, candidates any) bool {
	actualString := strings.TrimSpace(anyToRuleString(actual))
	switch typed := candidates.(type) {
	case []any:
		for _, candidate := range typed {
			if strings.EqualFold(actualString, strings.TrimSpace(anyToRuleString(candidate))) {
				return true
			}
		}
	case []string:
		for _, candidate := range typed {
			if strings.EqualFold(actualString, strings.TrimSpace(candidate)) {
				return true
			}
		}
	default:
		return strings.EqualFold(actualString, strings.TrimSpace(anyToRuleString(typed)))
	}
	return false
}

func anyToRuleString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func businessRuleGovernanceStatus(eval BusinessRuleEvaluation, p Principal) (string, []string) {
	reasons := []string{}
	if eval.RequiresAdmin {
		reasons = append(reasons, "data_admin required")
		if strings.TrimSpace(p.Role) != "data_admin" {
			return "blocked_for_admin", reasons
		}
	}
	if eval.RequiresApproval {
		reasons = append(reasons, "approval required")
	}
	if eval.RequiresBackup {
		reasons = append(reasons, "backup required")
	}
	if eval.RequiresQuality {
		reasons = append(reasons, "quality check recommended")
	}
	if eval.RequiresDryRun {
		reasons = append(reasons, "dry-run required or recommended")
	}
	if len(reasons) > 0 {
		return "needs_review", reasons
	}
	return "clear", nil
}

func businessRuleEvaluationSummary(eval BusinessRuleEvaluation) string {
	if len(eval.MatchedRules) == 0 {
		return "No governed business rule matched the provided data."
	}
	parts := make([]string, 0, len(eval.MatchedRules))
	for _, rule := range eval.MatchedRules {
		if strings.TrimSpace(rule.Title) != "" {
			parts = append(parts, rule.Title)
			continue
		}
		parts = append(parts, rule.ID)
	}
	return "Matched business governance rules: " + strings.Join(parts, "; ")
}

func businessRuleExecutionAdvice(eval BusinessRuleEvaluation) (bool, string) {
	if eval.GovernanceStatus == "blocked_for_admin" {
		return false, "request_data_admin"
	}
	if eval.GovernanceStatus == "clear" {
		return true, "execute_business_action"
	}
	for _, step := range eval.NextSteps {
		if strings.TrimSpace(step.Action) != "" {
			return false, step.Action
		}
	}
	return false, "review_governance"
}

func businessRuleGateStatuses(eval BusinessRuleEvaluation, p Principal) []BusinessRuleGateStatus {
	out := []BusinessRuleGateStatus{}
	add := func(gate, status, action, description string) {
		out = append(out, BusinessRuleGateStatus{Gate: gate, Status: status, Action: action, Description: description})
	}
	if eval.RequiresAdmin {
		status := "complete"
		if strings.TrimSpace(p.Role) != "data_admin" {
			status = "blocked"
		}
		add("admin", status, "request_data_admin", "Administrative role is required for this governed operation.")
	}
	if eval.RequiresBackup {
		add("backup", "pending", "create_backup", "Create a backup checkpoint before continuing.")
	}
	if eval.RequiresDryRun {
		status := "pending"
		if eval.DryRun {
			status = "complete"
		}
		add("dry_run", status, "execute_business_action", "Preview and validate the business action before committing.")
	}
	if eval.RequiresQuality {
		add("quality", "pending", "run_quality_check", "Run quality checks around the governed dataset.")
	}
	if eval.RequiresApproval {
		switch eval.ApprovalStatus {
		case recordApprovalStatusApproved:
			add("approval", "complete", "get_record_approval", "A matching approval has already been approved.")
		case recordApprovalStatusPending:
			add("approval", "pending", "get_record_approval", "A matching approval is already pending review.")
		default:
			add("approval", "pending", "create_record_approval", "Create an approval request for the governed business record.")
		}
	}
	return out
}

func businessRuleNextSteps(eval BusinessRuleEvaluation, in EvaluateBusinessRulesInput) []BusinessIntentNextStep {
	steps := []BusinessIntentNextStep{}
	add := func(action, purpose, description string, adminOnly, dryRun bool, params map[string]any) {
		steps = append(steps, BusinessIntentNextStep{
			Order:            len(steps) + 1,
			Action:           action,
			Purpose:          purpose,
			Description:      description,
			AdminOnly:        adminOnly,
			DryRun:           dryRun,
			BodyTemplate:     params,
			ToolCallTemplate: businessIntentToolCallTemplate(action, dryRun, params, nil),
			Params:           params,
		})
	}
	if eval.RequiresBackup {
		add("create_backup", "reliability", "Create a backup before risky business or bulk work.", true, false, map[string]any{"name": "pre-change checkpoint", "note": eval.Summary})
	}
	if eval.RequiresDryRun && eval.BusinessAction != "" && !eval.DryRun {
		add("execute_business_action", "business_write_preview", "Dry-run the business action before writing.", false, true, map[string]any{"business_action_id": eval.BusinessAction, "record_id": strings.TrimSpace(in.RecordID), "data": cloneJSONMap(in.Data), "dry_run": true})
	}
	if eval.RequiresQuality && eval.DatasetID != "" {
		add("run_quality_check", "quality", "Run quality checks around the governed dataset.", false, false, map[string]any{"dataset_id": eval.DatasetID, "include_warnings": true, "checks": eval.RecommendedChecks})
	}
	if eval.RequiresApproval && eval.DatasetID != "" && eval.ApprovalStatus == recordApprovalStatusPending && eval.ApprovalID != "" {
		add("get_record_approval", "business_approval", "Read the existing pending approval request.", false, false, map[string]any{"approval_id": eval.ApprovalID})
	}
	if eval.RequiresApproval && eval.DatasetID != "" && eval.ApprovalStatus == "" {
		add("create_record_approval", "business_approval", "Create an approval request after the target record exists.", false, false, map[string]any{
			"dataset_id":  eval.DatasetID,
			"id":          firstNonEmpty(strings.TrimSpace(in.RecordID), "<record_id>"),
			"kind":        firstNonEmpty(firstApprovalKind(eval.MatchedRules), "business_record"),
			"priority":    businessRuleApprovalPriority(eval),
			"summary":     eval.Summary,
			"request":     map[string]any{"business_action_id": eval.BusinessAction, "record_id": strings.TrimSpace(in.RecordID), "data": cloneJSONMap(in.Data), "matched_rules": matchedRuleIDs(eval.MatchedRules)},
			"assigned_to": firstNonEmpty(firstDefaultApprover(eval.MatchedRules), "<approver>"),
			"reason":      eval.Summary,
		})
	}
	return steps
}

func (s *Service) applyBusinessRuleApprovalState(ctx context.Context, p Principal, eval *BusinessRuleEvaluation, in EvaluateBusinessRulesInput) {
	if eval == nil || !eval.RequiresApproval || strings.TrimSpace(eval.DatasetID) == "" || strings.TrimSpace(in.RecordID) == "" {
		return
	}
	eval.ApprovalKind = firstNonEmpty(firstApprovalKind(eval.MatchedRules), "business_record")
	for _, status := range []string{recordApprovalStatusPending, recordApprovalStatusApproved} {
		items, err := s.store.ListRecordApprovals(ctx, p.TenantID, QueryRecordApprovalsInput{
			DatasetID: strings.TrimSpace(eval.DatasetID),
			RecordID:  strings.TrimSpace(in.RecordID),
			Status:    status,
			Kind:      eval.ApprovalKind,
			Limit:     1,
		})
		if err != nil || len(items) == 0 {
			continue
		}
		eval.ApprovalID = items[0].ID
		eval.ApprovalStatus = items[0].Status
		return
	}
}

func firstDefaultApprover(rules []BusinessRuleDefinition) string {
	for _, rule := range rules {
		if strings.TrimSpace(rule.DefaultApprover) != "" {
			return strings.TrimSpace(rule.DefaultApprover)
		}
	}
	return ""
}

func firstApprovalKind(rules []BusinessRuleDefinition) string {
	for _, rule := range rules {
		if strings.TrimSpace(rule.ApprovalKind) != "" {
			return strings.TrimSpace(rule.ApprovalKind)
		}
	}
	return ""
}

func businessRuleApprovalPriority(eval BusinessRuleEvaluation) string {
	for _, rule := range eval.MatchedRules {
		if strings.EqualFold(rule.Severity, "critical") {
			return "critical"
		}
	}
	for _, rule := range eval.MatchedRules {
		if strings.EqualFold(rule.Severity, "high") {
			return "high"
		}
	}
	return "medium"
}

func matchedRuleIDs(rules []BusinessRuleDefinition) []string {
	out := make([]string, 0, len(rules))
	for _, rule := range rules {
		out = append(out, rule.ID)
	}
	sort.Strings(out)
	return out
}
