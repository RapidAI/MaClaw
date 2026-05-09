package structureddata

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *Service) ListAccessPolicyPresets(ctx context.Context, p Principal, query ...QueryAccessPolicyPresetsInput) ([]AccessPolicyPreset, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	out := cloneAccessPolicyPresets(accessPolicyPresets)
	if len(query) == 0 {
		return out, nil
	}
	return paginateAccessPolicyPresets(out, query[0]), nil
}

func paginateAccessPolicyPresets(items []AccessPolicyPreset, in QueryAccessPolicyPresetsInput) []AccessPolicyPreset {
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := cloneAccessPolicyPresets(items)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	beforeID := strings.TrimSpace(in.BeforeID)
	if beforeID != "" {
		filtered := out[:0]
		for _, preset := range out {
			if preset.ID < beforeID {
				filtered = append(filtered, preset)
			}
		}
		out = filtered
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Service) CheckAccess(ctx context.Context, p Principal, in AccessCheckInput) (*AccessCheckResult, error) {
	target, err := s.accessCheckPrincipal(ctx, p, in.KeyID)
	if err != nil {
		return nil, err
	}
	resourceType := strings.ToLower(strings.TrimSpace(in.ResourceType))
	resourceID := strings.ToLower(strings.TrimSpace(in.ResourceID))
	if resourceType == "" {
		return nil, fmt.Errorf("%w: resource_type is required", ErrInvalidInput)
	}
	allowed := false
	reasons := []string{}
	switch resourceType {
	case "domain":
		allowed = principalCanUseDomain(target, resourceID)
	case "dataset", "raw_dataset", "template":
		allowed = principalCanUseDataset(target, resourceID)
		if !allowed {
			reasons = append(reasons, "raw dataset access requires allowed_datasets or allow_raw_data with a matching domain")
		}
	case "business_action", "action":
		allowed = principalCanUseAction(target, resourceID) && principalCanUseDomain(target, datasetDomain(resourceID))
	case "business_view", "view":
		allowed = principalCanUseView(target, resourceID) && principalCanUseDomain(target, datasetDomain(resourceID))
	case "report":
		allowed = principalCanUseReport(target, resourceID) && principalCanUseDomain(target, datasetDomain(resourceID))
	case "dashboard":
		allowed = principalCanUseDashboard(target, resourceID) && principalCanUseDomain(target, datasetDomain(resourceID))
	case "admin":
		allowed = principalCanAdmin(target)
		if !allowed {
			reasons = append(reasons, "admin access requires data_admin plus allow_admin for scoped API keys")
		}
	case "sensitive":
		allowed = principalCanReadSensitive(target)
		if !allowed {
			reasons = append(reasons, "sensitive field access requires allow_sensitive or data_admin")
		}
	default:
		return nil, fmt.Errorf("%w: unsupported resource_type", ErrInvalidInput)
	}
	if allowed {
		reasons = append(reasons, "allowed by current role and scoped policy")
	} else if len(reasons) == 0 {
		reasons = append(reasons, "not covered by scoped policy")
	}
	return &AccessCheckResult{
		Allowed:      allowed,
		APIKeyID:     target.APIKeyID,
		TenantID:     target.TenantID,
		UserID:       target.UserID,
		Role:         target.Role,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Reasons:      reasons,
	}, nil
}

func (s *Service) ReviewAccess(ctx context.Context, p Principal, in AccessReviewInput) (*AccessReviewResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	minSeverity := strings.ToLower(strings.TrimSpace(in.MinSeverity))
	if minSeverity != "" && accessReviewSeverityRank(minSeverity) == 0 {
		return nil, fmt.Errorf("%w: invalid min_severity", ErrInvalidInput)
	}
	items, err := s.ListAPIKeyPolicies(ctx, p, QueryAPIKeyPoliciesInput{Limit: 500})
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	out := &AccessReviewResult{
		TenantID:    p.TenantID,
		GeneratedAt: now,
		Total:       len(items),
		ByStatus:    map[string]int{},
		BySeverity:  map[string]int{},
		MinSeverity: minSeverity,
		Findings:    []AccessReviewItem{},
	}
	for _, item := range items {
		out.ByStatus[item.Status]++
		if finding, ok := accessReviewFinding(now, item); ok {
			out.BySeverity[finding.Severity]++
			if minSeverity != "" && accessReviewSeverityRank(finding.Severity) < accessReviewSeverityRank(minSeverity) {
				continue
			}
			out.Findings = append(out.Findings, finding)
		}
	}
	out.Filtered = len(out.Findings)
	return out, nil
}

func (s *Service) PlanAccessRemediation(ctx context.Context, p Principal, in AccessRemediationPlanInput) (*AccessRemediationPlan, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	minSeverity := strings.ToLower(strings.TrimSpace(in.MinSeverity))
	if minSeverity != "" && accessReviewSeverityRank(minSeverity) == 0 {
		return nil, fmt.Errorf("%w: invalid min_severity", ErrInvalidInput)
	}
	records, err := s.ListAPIKeyPolicies(ctx, p, QueryAPIKeyPoliciesInput{Limit: 500})
	out := &AccessRemediationPlan{
		TenantID:    p.TenantID,
		GeneratedAt: s.now().UTC(),
		Items:       []AccessRemediationItem{},
	}
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		finding, ok := accessReviewFinding(out.GeneratedAt, record)
		if !ok {
			continue
		}
		if minSeverity != "" && accessReviewSeverityRank(finding.Severity) < accessReviewSeverityRank(minSeverity) {
			continue
		}
		out.Items = append(out.Items, remediationItemsForFinding(finding, record)...)
	}
	out.Total = len(out.Items)
	return out, nil
}

func (s *Service) accessCheckPrincipal(ctx context.Context, p Principal, keyID string) (Principal, error) {
	keyID = strings.ToLower(strings.TrimSpace(keyID))
	if keyID == "" {
		return p, nil
	}
	if !principalCanAdmin(p) {
		return Principal{}, ErrForbidden
	}
	record, err := s.store.GetAPIKeyPolicy(ctx, p.TenantID, keyID)
	if err != nil {
		return Principal{}, err
	}
	return Principal{
		TenantID: record.TenantID,
		UserID:   record.UserID,
		Role:     record.Role,
		APIKeyID: record.ID,
		Policy:   apiKeyPolicyFromRecord(*record),
	}, nil
}

func remediationItemsForFinding(finding AccessReviewItem, record APIKeyPolicyRecord) []AccessRemediationItem {
	items := []AccessRemediationItem{}
	add := func(code, action, method, endpoint, reason string, payload map[string]any, destructive bool) {
		if !accessReviewContainsCode(finding.Codes, code) {
			return
		}
		items = append(items, AccessRemediationItem{
			KeyID:       finding.KeyID,
			Severity:    finding.Severity,
			Codes:       []string{code},
			Action:      action,
			Requires:    []string{"data_admin", "human_confirm"},
			Reason:      reason,
			Method:      method,
			Endpoint:    endpoint,
			Payload:     payload,
			Destructive: destructive,
		})
	}
	keyPath := "/api/v1/data/access/api-keys/" + finding.KeyID
	add("expired", "disable_key", "DELETE", keyPath, "expired managed key can no longer authenticate and should usually be disabled after owner review", nil, true)
	add("expiring_soon", "review_or_extend_expiration", "PATCH", keyPath, "key expires soon; extend only if the owner still needs access", accessRemediationUpdatePayload(record, func(payload map[string]any) { payload["expires_at"] = "SET_NEW_EXPIRATION" }), false)
	add("allow_admin", "remove_admin_scope", "PATCH", keyPath, "allow_admin should be limited to break-glass or schema administration agents", accessRemediationUpdatePayload(record, func(payload map[string]any) { payload["allow_admin"] = false }), false)
	add("allow_sensitive", "remove_sensitive_scope", "PATCH", keyPath, "sensitive data access should be limited to trusted HR, finance, or audit agents", accessRemediationUpdatePayload(record, func(payload map[string]any) { payload["allow_sensitive"] = false }), false)
	add("raw_dataset_access", "restrict_to_business_capabilities", "PATCH", keyPath, "raw dataset access should be replaced with business actions, views, reports, or dashboards when possible", accessRemediationUpdatePayload(record, func(payload map[string]any) {
		payload["allow_raw_data"] = false
		payload["allowed_datasets"] = []string{}
	}), false)
	add("never_used", "disable_unused_key", "DELETE", keyPath, "unused key is a standing authorization with no observed business use", nil, true)
	add("stale_last_used", "rotate_or_disable_stale_key", "POST", keyPath+"/rotate", "stale key should be rotated or disabled after owner review", nil, true)
	if len(items) == 0 {
		items = append(items, AccessRemediationItem{
			KeyID:    finding.KeyID,
			Severity: finding.Severity,
			Codes:    append([]string(nil), finding.Codes...),
			Action:   "manual_review",
			Requires: []string{"data_admin"},
			Reason:   finding.Recommended,
		})
	}
	return items
}

func accessRemediationUpdatePayload(record APIKeyPolicyRecord, mutate func(map[string]any)) map[string]any {
	payload := map[string]any{
		"user_id":            record.UserID,
		"role":               record.Role,
		"enabled":            record.Enabled,
		"allowed_domains":    append([]string(nil), record.AllowedDomains...),
		"allowed_datasets":   append([]string(nil), record.AllowedDatasets...),
		"allowed_actions":    append([]string(nil), record.AllowedActions...),
		"allowed_views":      append([]string(nil), record.AllowedViews...),
		"allowed_reports":    append([]string(nil), record.AllowedReports...),
		"allowed_dashboards": append([]string(nil), record.AllowedDashboards...),
		"allow_raw_data":     record.AllowRawData,
		"allow_sensitive":    record.AllowSensitive,
		"allow_admin":        record.AllowAdmin,
		"note":               record.Note,
		"expires_at":         "",
	}
	if record.ExpiresAt != nil {
		payload["expires_at"] = record.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if mutate != nil {
		mutate(payload)
	}
	return payload
}

func accessReviewContainsCode(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func accessReviewFinding(now time.Time, item APIKeyPolicyRecord) (AccessReviewItem, bool) {
	codes := []string{}
	severity := ""
	recommended := ""
	if item.Status == "expired" {
		codes = append(codes, "expired")
		severity = maxAccessReviewSeverity(severity, "high")
		recommended = "disable or rotate only if the agent still needs access"
	}
	if item.Status == "expiring_soon" {
		codes = append(codes, "expiring_soon")
		severity = maxAccessReviewSeverity(severity, "medium")
		recommended = "review owner and extend only if still needed"
	}
	if item.Status == "disabled" {
		codes = append(codes, "disabled")
		severity = maxAccessReviewSeverity(severity, "info")
	}
	if item.AllowAdmin {
		codes = append(codes, "allow_admin")
		severity = maxAccessReviewSeverity(severity, "critical")
		recommended = "keep only for break-glass or schema administration agents"
	}
	if item.AllowRawData || len(item.AllowedDatasets) > 0 {
		codes = append(codes, "raw_dataset_access")
		severity = maxAccessReviewSeverity(severity, "high")
		if recommended == "" {
			recommended = "prefer business actions, views, reports, and dashboards over raw dataset access"
		}
	}
	if item.AllowSensitive {
		codes = append(codes, "allow_sensitive")
		severity = maxAccessReviewSeverity(severity, "high")
		if recommended == "" {
			recommended = "limit sensitive access to trusted HR, finance, or audit agents"
		}
	}
	if item.Enabled && item.LastUsedAt == nil && now.Sub(item.CreatedAt) > 30*24*time.Hour {
		codes = append(codes, "never_used")
		severity = maxAccessReviewSeverity(severity, "medium")
		if recommended == "" {
			recommended = "disable unused keys or confirm the owner still needs them"
		}
	}
	if item.Enabled && item.LastUsedAt != nil && now.Sub(*item.LastUsedAt) > 90*24*time.Hour {
		codes = append(codes, "stale_last_used")
		severity = maxAccessReviewSeverity(severity, "medium")
		if recommended == "" {
			recommended = "rotate or disable stale keys after owner review"
		}
	}
	if len(codes) == 0 {
		return AccessReviewItem{}, false
	}
	if severity == "" {
		severity = "info"
	}
	return AccessReviewItem{
		KeyID:       item.ID,
		UserID:      item.UserID,
		Role:        item.Role,
		Status:      item.Status,
		Severity:    severity,
		Codes:       codes,
		Summary:     strings.Join(codes, ", "),
		Recommended: recommended,
	}, true
}

func maxAccessReviewSeverity(current, candidate string) string {
	if accessReviewSeverityRank(candidate) > accessReviewSeverityRank(current) {
		return candidate
	}
	return current
}

func accessReviewSeverityRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

var accessPolicyPresets = []AccessPolicyPreset{
	{
		ID:          "sales-operator",
		Title:       "Sales operator agent",
		Description: "Can maintain customers, contacts, opportunities, and sales order workflow; can run sales views and reports; no raw dataset API.",
		Role:        "data_user",
		AllowedActions: []string{
			"sales.customer_upsert",
			"sales.contact_upsert",
			"sales.opportunity_upsert",
			"sales.opportunity_stage_update",
			"sales.order_upsert",
			"sales.order_status_update",
		},
		AllowedViews:      []string{"sales.order_overview", "sales.customer_overview", "sales.opportunity_pipeline"},
		AllowedReports:    []string{"sales.order_summary_by_stage", "sales.revenue_by_customer", "sales.contacts_by_status", "sales.opportunity_pipeline_by_stage", "sales.opportunity_pipeline_by_owner"},
		AllowedDashboards: []string{"sales.overview"},
	},
	{
		ID:          "finance-reporter",
		Title:       "Finance reporting agent",
		Description: "Can run finance dashboards, views, and reports; cannot change finance records or read raw datasets.",
		Role:        "data_auditor",
		AllowedViews: []string{
			"finance.expense_overview",
			"finance.invoice_overview",
			"finance.payment_overview",
			"finance.budget_overview",
			"finance.voucher_overview",
		},
		AllowedReports: []string{
			"finance.expense_by_department",
			"finance.invoice_status_summary",
			"finance.payment_status_summary",
			"finance.payment_cashflow_by_type",
			"finance.budget_by_department",
			"finance.budget_by_status",
			"finance.voucher_status_summary",
			"finance.voucher_by_period",
		},
		AllowedDashboards: []string{"finance.overview"},
	},
	{
		ID:          "finance-operator",
		Title:       "Finance operator agent",
		Description: "Can process expenses, invoices, payments, budgets, and vouchers through business actions; no raw dataset API.",
		Role:        "data_user",
		AllowedActions: []string{
			"finance.expense_submit",
			"finance.expense_status_update",
			"finance.invoice_upsert",
			"finance.invoice_status_update",
			"finance.payment_upsert",
			"finance.budget_upsert",
			"finance.voucher_upsert",
		},
		AllowedViews:      []string{"finance.expense_overview", "finance.invoice_overview", "finance.payment_overview", "finance.budget_overview", "finance.voucher_overview"},
		AllowedReports:    []string{"finance.expense_by_department", "finance.invoice_status_summary", "finance.payment_status_summary", "finance.budget_by_status", "finance.voucher_status_summary"},
		AllowedDashboards: []string{"finance.overview"},
	},
	{
		ID:                "hr-auditor",
		Title:             "HR auditor agent",
		Description:       "Can inspect HR dashboards and reports with sensitive fields allowed for trusted audit use; no raw writes.",
		Role:              "data_auditor",
		AllowedViews:      []string{"hr.employee_directory", "hr.leave_overview", "hr.payroll_overview"},
		AllowedReports:    []string{"hr.employee_count_by_department", "hr.leave_by_status", "hr.leave_by_department", "hr.payroll_status_summary", "hr.payroll_by_department"},
		AllowedDashboards: []string{"hr.overview"},
		AllowSensitive:    true,
	},
	{
		ID:          "inventory-operator",
		Title:       "Inventory operator agent",
		Description: "Can maintain inventory items, warehouses, and stock movement through business actions; can run inventory reports.",
		Role:        "data_user",
		AllowedActions: []string{
			"inventory.item_upsert",
			"inventory.warehouse_upsert",
			"inventory.stock_update",
			"inventory.movement_upsert",
		},
		AllowedViews:      []string{"inventory.stock_overview", "inventory.movement_overview", "inventory.warehouse_overview"},
		AllowedReports:    []string{"inventory.quantity_by_warehouse", "inventory.low_stock_summary", "inventory.warehouse_by_status", "inventory.movement_by_type", "inventory.movement_by_warehouse"},
		AllowedDashboards: []string{"inventory.overview"},
	},
	{
		ID:          "domain-dashboard-reader",
		Title:       "Domain dashboard reader",
		Description: "Template for a dashboard-only agent. Pick domains or reports after applying it.",
		Role:        "data_auditor",
	},
}

func cloneAccessPolicyPresets(items []AccessPolicyPreset) []AccessPolicyPreset {
	out := make([]AccessPolicyPreset, 0, len(items))
	for _, item := range items {
		item.AllowedDomains = append([]string(nil), item.AllowedDomains...)
		item.AllowedDatasets = append([]string(nil), item.AllowedDatasets...)
		item.AllowedActions = append([]string(nil), item.AllowedActions...)
		item.AllowedViews = append([]string(nil), item.AllowedViews...)
		item.AllowedReports = append([]string(nil), item.AllowedReports...)
		item.AllowedDashboards = append([]string(nil), item.AllowedDashboards...)
		out = append(out, item)
	}
	return out
}
