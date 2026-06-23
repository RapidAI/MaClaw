package httpapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func collectLLMServiceProviderReferenceIssues(serviceReg *llmservice.Registry, providerReg *im.LLMProviderRegistry) []string {
	if serviceReg == nil {
		serviceReg = &llmservice.Registry{}
	}
	if providerReg == nil {
		providerReg = &im.LLMProviderRegistry{}
	}
	serviceReg.Normalize()
	configuredProviders := map[string]struct{}{}
	for _, provider := range providerReg.Providers {
		id := strings.ToLower(strings.TrimSpace(provider.ID))
		if id == "" {
			continue
		}
		configuredProviders[id] = struct{}{}
	}
	issues := make([]string, 0)
	for _, group := range serviceReg.ModelServiceGroups {
		for _, model := range group.Models {
			missing := make([]string, 0)
			seenMissing := map[string]struct{}{}
			for _, providerID := range model.ProviderIDs {
				providerID = strings.TrimSpace(providerID)
				if providerID == "" {
					continue
				}
				key := strings.ToLower(providerID)
				// Built-in providers (e.g. maclaw_official) are virtual and never
				// appear in the user-configured provider registry — skip them.
				if llmservice.IsBuiltinProvider(key) {
					continue
				}
				if _, ok := configuredProviders[key]; ok {
					continue
				}
				if _, ok := seenMissing[key]; ok {
					continue
				}
				seenMissing[key] = struct{}{}
				missing = append(missing, providerID)
			}
			if len(missing) == 0 {
				continue
			}
			sort.Strings(missing)
			issues = append(issues, fmt.Sprintf("service group %q model %q references unknown providers: %s", group.ID, model.Name, strings.Join(missing, ", ")))
		}
	}
	sort.Strings(issues)
	return issues
}

func filterAuthorizedModelsByProviderRegistry(status *llmservice.ServiceStatus, models []llmservice.AuthorizedModel, providerReg *im.LLMProviderRegistry) (*llmservice.ServiceStatus, []llmservice.AuthorizedModel) {
	if status == nil {
		return nil, nil
	}
	filtered := filterAuthorizedModelsForConfiguredProviders(models, providerReg)
	filteredStatus := *status
	filteredStatus.AuthorizedModels = filtered
	filteredStatus.AvailableModels = make([]string, 0, len(filtered))
	for _, model := range filtered {
		filteredStatus.AvailableModels = append(filteredStatus.AvailableModels, model.Name)
	}
	filteredStatus.Active = status.Active && len(filtered) > 0
	filteredStatus.SkipLLMConfig = status.SkipLLMConfig && len(filtered) > 0
	if len(filtered) > 0 {
		if best := llmservice.SelectBestModelForRequest(nil, filtered); best != nil {
			filteredStatus.DefaultModel = best.Name
		} else {
			filteredStatus.DefaultModel = filtered[0].Name
		}
	} else {
		filteredStatus.DefaultModel = ""
	}
	return &filteredStatus, filtered
}

func filterAuthorizedModelsForConfiguredProviders(models []llmservice.AuthorizedModel, providerReg *im.LLMProviderRegistry) []llmservice.AuthorizedModel {
	configuredProviders := map[string]struct{}{}
	if providerReg != nil {
		for _, provider := range providerReg.Providers {
			id := strings.ToLower(strings.TrimSpace(provider.ID))
			if id == "" {
				continue
			}
			configuredProviders[id] = struct{}{}
		}
	}
	filtered := make([]llmservice.AuthorizedModel, 0, len(models))
	for _, model := range models {
		clone := model
		clone.ProviderIDs = nil
		clone.ServiceGroupIDs = nil
		clone.ProviderServiceGroups = map[string][]string{}
		clone.ProviderCreditMultipliers = map[string]float64{}
		clone.CreditMultiplier = 0
		serviceGroupSet := map[string]struct{}{}
		for _, providerID := range model.ProviderIDs {
			providerID = strings.TrimSpace(providerID)
			if providerID == "" {
				continue
			}
			key := strings.ToLower(providerID)
			// Built-in providers are always considered "configured" — they
			// route through HubCenter and don't need a local provider entry.
			if !llmservice.IsBuiltinProvider(key) {
				if _, ok := configuredProviders[key]; !ok {
					continue
				}
			}
			clone.ProviderIDs = append(clone.ProviderIDs, providerID)
			if groups := model.ProviderServiceGroups[key]; len(groups) > 0 {
				clone.ProviderServiceGroups[key] = append([]string(nil), groups...)
				for _, groupID := range groups {
					serviceGroupSet[strings.ToLower(strings.TrimSpace(groupID))] = struct{}{}
				}
			}
			if multiplier, ok := model.ProviderCreditMultipliers[key]; ok && multiplier > 0 {
				clone.ProviderCreditMultipliers[key] = multiplier
				if clone.CreditMultiplier <= 0 || multiplier < clone.CreditMultiplier {
					clone.CreditMultiplier = multiplier
				}
			}
		}
		if len(clone.ProviderIDs) == 0 {
			continue
		}
		if clone.CreditMultiplier <= 0 {
			clone.CreditMultiplier = 1
		}
		for _, groupID := range model.ServiceGroupIDs {
			if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(groupID))]; ok || len(serviceGroupSet) == 0 {
				clone.ServiceGroupIDs = append(clone.ServiceGroupIDs, groupID)
			}
		}
		filtered = append(filtered, clone)
	}
	return filtered
}

func explainFilteredServiceStatusIssues(status *llmservice.ServiceStatus, filtered []llmservice.AuthorizedModel, providerReg *im.LLMProviderRegistry) []string {
	if status == nil {
		return nil
	}
	reasons := append([]string(nil), status.InactiveReasons...)
	grantStateReasons := serviceStatusGrantStateReasons(status)
	if len(status.ServiceGroupIDs) == 0 {
		if len(grantStateReasons) > 0 {
			reasons = append(reasons, grantStateReasons...)
		} else {
			reasons = append(reasons, "no service-group entitlement is active for this user")
		}
	}
	if len(status.AuthorizedModels) > 0 && len(filtered) == 0 {
		reasons = append(reasons, "authorized service groups exist, but none route to a live LLM provider")
	}
	if len(status.ActiveGrants) > 0 && len(filtered) == 0 {
		reasons = append(reasons, "active grants exist, but they currently expose no live model routes")
	}
	if providerReg == nil || len(providerReg.Providers) == 0 {
		// Only report "no providers configured" if the filtered result is also
		// empty. Built-in providers (maclaw_official) don't need user-configured
		// entries, so having filtered models means the system is functional.
		if len(filtered) == 0 {
			reasons = append(reasons, "no LLM providers are currently configured")
		}
	}
	return dedupeServiceStatusReasons(reasons)
}

func serviceStatusGrantStateReasons(status *llmservice.ServiceStatus) []string {
	if status == nil {
		return nil
	}
	reasons := make([]string, 0, len(status.CreditGrants))
	for _, grant := range status.CreditGrants {
		switch strings.ToLower(strings.TrimSpace(grant.Status)) {
		case "period_limited":
			reason := "current period credit limit is exhausted"
			if grant.RetryAfterAt != "" {
				reason += "; retry after " + grant.RetryAfterAt
			}
			reasons = append(reasons, reason)
		case "exhausted":
			reasons = append(reasons, "grant credits are exhausted")
		case "queued":
			reason := "grant is not active yet"
			if grant.RetryAfterAt != "" {
				reason += "; starts at " + grant.RetryAfterAt
			}
			reasons = append(reasons, reason)
		case "expired":
			reasons = append(reasons, "grant has expired")
		}
	}
	return dedupeServiceStatusReasons(reasons)
}

func dedupeServiceStatusReasons(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		clean := strings.TrimSpace(item)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}
