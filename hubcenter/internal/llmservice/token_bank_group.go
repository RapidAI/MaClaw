package llmservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// RegistryHostsTokenBank reports whether credits withdrawn into groupID can be
// spent on a Token Bank route.
//
// A Hub names the local grant bucket. The official bucket is
// maclaw_official_group, which is not a HubCenter catalog group: an official
// call is billed on the tenant compute group (the configured default, otherwise
// redeem). The check follows that group. A literal catalog id is tested as
// named. Publish attaches the tier array id, not the member id, because
// collapseServiceGroupArrayRoutes rewrites member routes onto the array.
// A check that only accepts tbk_ would reject every real assignment.
func RegistryHostsTokenBank(reg *Registry, groupID string) bool {
	group := tokenBankSpendGroup(reg, groupID)
	if group == nil {
		return false
	}
	for i := range group.Models {
		if modelHostsTokenBank(&group.Models[i]) {
			return true
		}
	}
	return false
}

// tokenBankSpendGroup is the catalog group whose routes will spend a grant
// written against groupID. The Hub official entry resolves the same way a
// proxy call for model auto does: the configured compute group wins when it
// can serve auto. A later group that hosts Token Bank does not override it.
func tokenBankSpendGroup(reg *Registry, groupID string) *llmpool.ServiceGroup {
	groupID = strings.TrimSpace(groupID)
	if reg == nil || groupID == "" {
		return nil
	}
	if llmpool.IsHubOfficialServiceGroup(groupID) {
		group, _ := matchProxyOfficialComputeFallback(reg, "auto")
		return group
	}
	return findServiceGroupByID(reg, groupID)
}

func modelHostsTokenBank(model *llmpool.ModelConfig) bool {
	if model == nil {
		return false
	}
	for _, id := range model.ProviderIDs {
		if providerIDHostsTokenBank(id) {
			return true
		}
	}
	for _, cfg := range model.ProviderConfigs {
		if providerIDHostsTokenBank(cfg.ProviderID) {
			return true
		}
	}
	return false
}

func providerIDHostsTokenBank(id string) bool {
	id = strings.TrimSpace(id)
	return IsTokenBankArray(id) || IsTokenBankMemberID(id)
}

// ServiceGroupHostsTokenBank loads the registry and applies RegistryHostsTokenBank.
func (s *Service) ServiceGroupHostsTokenBank(ctx context.Context, groupID string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("llm service is required")
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return false, err
	}
	return RegistryHostsTokenBank(reg, groupID), nil
}
