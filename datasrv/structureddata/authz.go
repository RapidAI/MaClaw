package structureddata

import (
	"encoding/json"
	"strings"
)

func ParseAPIKeyPolicies(raw string) ([]APIKeyPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var policies []APIKeyPolicy
	if err := json.Unmarshal([]byte(raw), &policies); err != nil {
		return nil, err
	}
	out := make([]APIKeyPolicy, 0, len(policies))
	for _, policy := range policies {
		policy.ID = strings.TrimSpace(policy.ID)
		policy.Key = strings.TrimSpace(policy.Key)
		policy.TenantID = strings.TrimSpace(policy.TenantID)
		policy.UserID = strings.TrimSpace(policy.UserID)
		policy.Role = strings.ToLower(strings.TrimSpace(policy.Role))
		policy.AllowedDomains = normalizeStringList(policy.AllowedDomains)
		policy.AllowedDatasets = normalizeStringList(policy.AllowedDatasets)
		policy.AllowedActions = normalizeStringList(policy.AllowedActions)
		policy.AllowedViews = normalizeStringList(policy.AllowedViews)
		policy.AllowedReports = normalizeStringList(policy.AllowedReports)
		policy.AllowedDashboards = normalizeStringList(policy.AllowedDashboards)
		if policy.ID == "" && policy.UserID != "" {
			policy.ID = policy.UserID
		}
		if policy.Key == "" {
			continue
		}
		out = append(out, policy)
	}
	return out, nil
}

func normalizeStringList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func principalCanAdmin(p Principal) bool {
	if !strings.EqualFold(strings.TrimSpace(p.Role), "data_admin") {
		return false
	}
	if p.Policy != nil && !p.Policy.AllowAdmin {
		return false
	}
	return true
}

func principalCanReadSensitive(p Principal) bool {
	if p.Policy != nil {
		return p.Policy.AllowSensitive || principalCanAdmin(p)
	}
	return strings.EqualFold(p.Role, "data_admin") || strings.EqualFold(p.Role, "data_auditor")
}

func principalCanUseDomain(p Principal, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || p.Policy == nil || !policyHasAnyScope(p.Policy) {
		return true
	}
	if policyAllowsDomainDirect(p.Policy, domain) {
		return true
	}
	return policyHasScopedResourceInDomain(p.Policy, domain)
}

func principalCanUseDataset(p Principal, datasetID string) bool {
	datasetID = strings.ToLower(strings.TrimSpace(datasetID))
	if datasetID == "" || p.Policy == nil {
		return true
	}
	if len(p.Policy.AllowedDatasets) > 0 && containsPolicyValue(p.Policy.AllowedDatasets, datasetID) {
		return true
	}
	if !policyHasAnyScope(p.Policy) {
		return true
	}
	if p.Policy.AllowRawData && policyAllowsDomainDirect(p.Policy, datasetDomain(datasetID)) {
		return true
	}
	if len(p.Policy.AllowedDatasets) > 0 || !p.Policy.AllowRawData {
		return false
	}
	return policyAllowsDomainDirect(p.Policy, datasetDomain(datasetID))
}

func principalCanUseAction(p Principal, actionID string) bool {
	actionID = strings.ToLower(strings.TrimSpace(actionID))
	if actionID == "" || p.Policy == nil {
		return true
	}
	if len(p.Policy.AllowedActions) > 0 && containsPolicyValue(p.Policy.AllowedActions, actionID) {
		return true
	}
	if len(p.Policy.AllowedActions) > 0 {
		return false
	}
	return !policyHasAnyScope(p.Policy) || policyAllowsDomainDirect(p.Policy, datasetDomain(actionID))
}

func principalCanUseView(p Principal, viewID string) bool {
	viewID = strings.ToLower(strings.TrimSpace(viewID))
	if viewID == "" || p.Policy == nil {
		return true
	}
	if len(p.Policy.AllowedViews) > 0 && containsPolicyValue(p.Policy.AllowedViews, viewID) {
		return true
	}
	if len(p.Policy.AllowedViews) > 0 {
		return false
	}
	return !policyHasAnyScope(p.Policy) || policyAllowsDomainDirect(p.Policy, datasetDomain(viewID))
}

func principalCanUseReport(p Principal, reportID string) bool {
	reportID = strings.ToLower(strings.TrimSpace(reportID))
	if reportID == "" || p.Policy == nil {
		return true
	}
	if len(p.Policy.AllowedReports) > 0 && containsPolicyValue(p.Policy.AllowedReports, reportID) {
		return true
	}
	if len(p.Policy.AllowedReports) > 0 {
		return false
	}
	return !policyHasAnyScope(p.Policy) || policyAllowsDomainDirect(p.Policy, datasetDomain(reportID))
}

func principalCanUseDashboard(p Principal, dashboardID string) bool {
	dashboardID = strings.ToLower(strings.TrimSpace(dashboardID))
	if dashboardID == "" || p.Policy == nil {
		return true
	}
	if len(p.Policy.AllowedDashboards) > 0 && containsPolicyValue(p.Policy.AllowedDashboards, dashboardID) {
		return true
	}
	if len(p.Policy.AllowedDashboards) > 0 {
		return false
	}
	return !policyHasAnyScope(p.Policy) || policyAllowsDomainDirect(p.Policy, datasetDomain(dashboardID))
}

func policyHasAnyScope(policy *APIKeyPolicy) bool {
	if policy == nil {
		return false
	}
	return len(policy.AllowedDomains) > 0 ||
		len(policy.AllowedDatasets) > 0 ||
		len(policy.AllowedActions) > 0 ||
		len(policy.AllowedViews) > 0 ||
		len(policy.AllowedReports) > 0 ||
		len(policy.AllowedDashboards) > 0
}

func policyAllowsDomainDirect(policy *APIKeyPolicy, domain string) bool {
	if policy == nil {
		return false
	}
	return containsPolicyValue(policy.AllowedDomains, domain)
}

func policyHasScopedResourceInDomain(policy *APIKeyPolicy, domain string) bool {
	if policy == nil {
		return false
	}
	for _, list := range [][]string{policy.AllowedDatasets, policy.AllowedActions, policy.AllowedViews, policy.AllowedReports, policy.AllowedDashboards} {
		for _, item := range list {
			if datasetDomain(item) == domain || item == "*" || strings.HasPrefix(item, domain+".") {
				return true
			}
		}
	}
	return false
}

func containsPolicyValue(items []string, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, item := range items {
		if item == "*" || item == value {
			return true
		}
		if strings.HasSuffix(item, ".*") && strings.HasPrefix(value, strings.TrimSuffix(item, "*")) {
			return true
		}
	}
	return false
}

func datasetDomain(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if idx := strings.Index(id, "."); idx > 0 {
		return id[:idx]
	}
	return id
}
