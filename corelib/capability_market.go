package corelib

import "strings"

const (
	CapabilityTypeSkill = "skill"
	CapabilityTypeMCP   = "mcp"

	CapabilitySourceEnterpriseHub = "enterprise_hub"
	CapabilitySourceHubCenter     = "hubcenter"
	CapabilitySourceClawHub       = "clawhub"
	CapabilitySourceGitHub        = "github"
	CapabilitySourceLocal         = "local"

	CapabilityPricingFree         = "free"
	CapabilityPricingPaid         = "paid"
	CapabilityPricingFreemium     = "freemium"
	CapabilityPricingSubscription = "subscription"

	CapabilityInstallFromEnterpriseHub     = "install_from_enterprise_hub"
	CapabilityInstallExternalDirect        = "install_external_direct"
	CapabilityInstallCreateImportRequest   = "create_import_request"
	CapabilityInstallCreatePurchaseRequest = "create_purchase_request"
	CapabilityInstallBlocked               = "blocked"
)

type CapabilityInstallDecisionInput struct {
	Policy             CapabilityMarketPolicy
	Source             string
	Pricing            string
	ExistsInEnterprise bool
	ExternalInstallOK  bool
	ExternalPurchaseOK bool
}

type CapabilityInstallDecision struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

func DecideCapabilityInstall(in CapabilityInstallDecisionInput) CapabilityInstallDecision {
	policy := in.Policy.WithDefaults()
	source := strings.TrimSpace(strings.ToLower(in.Source))
	pricing := strings.TrimSpace(strings.ToLower(in.Pricing))
	if pricing == "" {
		pricing = CapabilityPricingFree
	}

	if in.ExistsInEnterprise || source == CapabilitySourceEnterpriseHub {
		return CapabilityInstallDecision{Action: CapabilityInstallFromEnterpriseHub}
	}
	if policy.EffectiveEnterpriseOnlySearch() && source != CapabilitySourceEnterpriseHub {
		return CapabilityInstallDecision{Action: CapabilityInstallBlocked, Reason: "enterprise_only_search"}
	}
	if pricing == CapabilityPricingPaid || pricing == CapabilityPricingSubscription || pricing == CapabilityPricingFreemium {
		if !in.ExternalPurchaseOK {
			return CapabilityInstallDecision{Action: CapabilityInstallBlocked, Reason: "purchase_not_allowed"}
		}
		return CapabilityInstallDecision{Action: CapabilityInstallCreatePurchaseRequest, Reason: "paid_external_capability"}
	}
	if policy.EffectiveEnterpriseOnlyInstall() {
		return CapabilityInstallDecision{Action: CapabilityInstallCreateImportRequest, Reason: "enterprise_only_install"}
	}
	if !in.ExternalInstallOK {
		return CapabilityInstallDecision{Action: CapabilityInstallBlocked, Reason: "external_install_not_allowed"}
	}
	return CapabilityInstallDecision{Action: CapabilityInstallExternalDirect}
}

type CapabilityUpdateDecisionInput struct {
	Policy  CapabilityMarketPolicy
	Source  string
	Pricing string
}

type CapabilityUpdateDecision struct {
	AutoUpdate bool   `json:"auto_update"`
	Policy     string `json:"policy"`
}

func DecideCapabilityUpdate(in CapabilityUpdateDecisionInput) CapabilityUpdateDecision {
	policy := in.Policy.WithDefaults()
	source := strings.TrimSpace(strings.ToLower(in.Source))
	pricing := strings.TrimSpace(strings.ToLower(in.Pricing))
	if pricing == "" {
		pricing = CapabilityPricingFree
	}
	if source == CapabilitySourceEnterpriseHub {
		return CapabilityUpdateDecision{AutoUpdate: true, Policy: policy.UpdatePolicy.EnterpriseHub.Default}
	}
	if source == CapabilitySourceHubCenter && pricing == CapabilityPricingFree {
		return CapabilityUpdateDecision{AutoUpdate: true, Policy: policy.UpdatePolicy.HubCenter.FreeCapability}
	}
	if source == CapabilitySourceHubCenter {
		return CapabilityUpdateDecision{AutoUpdate: false, Policy: policy.UpdatePolicy.HubCenter.PaidCapability}
	}
	return CapabilityUpdateDecision{AutoUpdate: false, Policy: "notify_admin"}
}

const (
	CapabilityMarketplaceHostHub       = "hub"
	CapabilityMarketplaceHostHubCenter = "hubcenter"
)

func AdminMarketplaceSearchSources(host string) []string {
	switch strings.TrimSpace(strings.ToLower(host)) {
	case CapabilityMarketplaceHostHubCenter:
		return []string{CapabilitySourceClawHub, CapabilitySourceGitHub}
	case CapabilityMarketplaceHostHub:
		return []string{CapabilitySourceHubCenter, CapabilitySourceClawHub, CapabilitySourceGitHub}
	default:
		return []string{}
	}
}

func AdminMarketplaceCanSearchSource(host, source string) bool {
	source = strings.TrimSpace(strings.ToLower(source))
	for _, allowed := range AdminMarketplaceSearchSources(host) {
		if source == allowed {
			return true
		}
	}
	return false
}
