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
			if _, ok := configuredProviders[strings.ToLower(providerID)]; !ok {
				continue
			}
			clone.ProviderIDs = append(clone.ProviderIDs, providerID)
			key := strings.ToLower(providerID)
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
	filteredStatus := *status
	filteredStatus.AuthorizedModels = filtered
	filteredStatus.AvailableModels = make([]string, 0, len(filtered))
	for _, model := range filtered {
		filteredStatus.AvailableModels = append(filteredStatus.AvailableModels, model.Name)
	}
	filteredStatus.Active = len(filtered) > 0
	filteredStatus.SkipLLMConfig = len(filtered) > 0
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
