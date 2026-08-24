package llmservice

import (
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// BillableCreditMultiplier is the rate Hub uses when deducting user credits.
// Local providers apply vendor time-of-use at request start, then route markup.
// Official MaClaw prefers the HubCenter-applied rate so both sides stay aligned.
func BillableCreditMultiplier(model *AuthorizedModel, providerID string, startedAt time.Time, local *llmpool.ProviderBillingPolicy, official []llmpool.ProviderBillingPolicy, appliedFromUpstream float64, officialProviderID string) float64 {
	route := CreditMultiplierForProvider(model, providerID)
	if IsBuiltinProvider(providerID) {
		if appliedFromUpstream > 0 {
			return normalizeCreditMultiplier(appliedFromUpstream)
		}
		vendor := resolveOfficialVendorMultiplier(official, startedAt, officialProviderID)
		return llmpool.CombineCreditMultipliers(vendor, route)
	}
	vendor := 1.0
	if local != nil {
		vendor = llmpool.ResolveCreditMultiplier(*local, startedAt)
	}
	return llmpool.CombineCreditMultipliers(vendor, route)
}

func resolveOfficialVendorMultiplier(policies []llmpool.ProviderBillingPolicy, startedAt time.Time, providerID string) float64 {
	if policy, ok := llmpool.FindProviderBillingPolicy(policies, providerID); ok {
		return llmpool.ResolveCreditMultiplier(policy, startedAt)
	}
	if len(policies) == 0 {
		return 1
	}
	first := llmpool.ResolveCreditMultiplier(policies[0], startedAt)
	for i := 1; i < len(policies); i++ {
		if llmpool.ResolveCreditMultiplier(policies[i], startedAt) != first {
			return 1
		}
	}
	return first
}
