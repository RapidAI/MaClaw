package llmpool

import (
	"math"
	"strings"
)

// ClientModelHeader carries the model the user selected. Hub rewrites the
// forwarded body to the resolved official band, so billing must read this
// header to charge auto, low, mid, or high instead of the internal route.
const ClientModelHeader = "X-MaClaw-Client-Model"

// DefaultCapabilityCatalog is what a dynamic group lists when its catalog is empty.
func DefaultCapabilityCatalog() []string {
	return []string{"auto", OfficialTierLow, OfficialTierMid, OfficialTierHigh}
}

// CanonicalClientModel maps the names a user can pick onto logical models.
// auto stays auto. low/mid/high are the official bands.
func CanonicalClientModel(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "", "auto", "default":
		return "auto"
	case OfficialTierHigh, "high":
		return OfficialTierHigh
	case OfficialTierMid, "mid":
		return OfficialTierMid
	case OfficialTierLow, "low":
		return OfficialTierLow
	default:
		return strings.TrimSpace(model)
	}
}

// DefaultCapabilityBillingMultiplier is the coefficient used when a band has
// no explicit billing multiplier. auto and mid are 1, low is 0.5, high is 2.
func DefaultCapabilityBillingMultiplier(model string) float64 {
	switch CanonicalClientModel(model) {
	case OfficialTierLow:
		return 0.5
	case OfficialTierHigh:
		return 2
	default:
		return 1
	}
}

// CapabilityBillingMultiplier returns the stored coefficient when it is a
// positive finite number, otherwise the band default.
func CapabilityBillingMultiplier(model string, configured float64) float64 {
	if configured > 0 && !math.IsNaN(configured) && !math.IsInf(configured, 0) {
		return configured
	}
	return DefaultCapabilityBillingMultiplier(model)
}

// GroupCapabilityBillingMultiplier is the billing coefficient for the model
// the user selected inside one service group.
func GroupCapabilityBillingMultiplier(group *ServiceGroup, clientModel string) float64 {
	canon := CanonicalClientModel(clientModel)
	return CapabilityBillingMultiplier(canon, configuredCapabilityMultiplier(group, clientModel, canon))
}

func configuredCapabilityMultiplier(group *ServiceGroup, clientModel, canon string) float64 {
	if group == nil {
		return 0
	}
	want := strings.TrimSpace(clientModel)
	for _, model := range group.Models {
		if want != "" && strings.EqualFold(strings.TrimSpace(model.Name), want) {
			return model.BillingMultiplier
		}
	}
	for _, model := range group.Models {
		if CanonicalClientModel(model.Name) == canon {
			return model.BillingMultiplier
		}
	}
	return 0
}
