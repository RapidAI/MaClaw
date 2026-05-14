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
	source := NormalizeCapabilitySource(in.Source)
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
	source := NormalizeCapabilitySource(in.Source)
	pricing := strings.TrimSpace(strings.ToLower(in.Pricing))
	if pricing == "" {
		pricing = CapabilityPricingFree
	}
	if source == CapabilitySourceEnterpriseHub {
		policyName := policy.UpdatePolicy.EnterpriseHub.Default
		return CapabilityUpdateDecision{AutoUpdate: capabilityUpdatePolicyAllowsAuto(policyName), Policy: policyName}
	}
	if source == CapabilitySourceHubCenter && pricing == CapabilityPricingFree {
		policyName := policy.UpdatePolicy.HubCenter.FreeCapability
		return CapabilityUpdateDecision{AutoUpdate: capabilityUpdatePolicyAllowsAuto(policyName), Policy: policyName}
	}
	if source == CapabilitySourceHubCenter {
		policyName := policy.UpdatePolicy.HubCenter.PaidCapability
		return CapabilityUpdateDecision{AutoUpdate: capabilityUpdatePolicyAllowsAuto(policyName), Policy: policyName}
	}
	return CapabilityUpdateDecision{AutoUpdate: false, Policy: "notify_admin"}
}

func capabilityUpdatePolicyAllowsAuto(policyName string) bool {
	switch strings.TrimSpace(strings.ToLower(policyName)) {
	case "auto_update", "auto_update_approved":
		return true
	default:
		return false
	}
}

func NormalizeCapabilityType(capabilityType string) string {
	switch strings.TrimSpace(strings.ToLower(capabilityType)) {
	case "skill", "skills":
		return CapabilityTypeSkill
	case "mcp", "mcps", "mcp_server", "mcp-server", "mcpserver", "model_context_protocol":
		return CapabilityTypeMCP
	default:
		return strings.TrimSpace(strings.ToLower(capabilityType))
	}
}

func NormalizeCapabilitySource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "hub", "enterprise", "enterprise_hub":
		return CapabilitySourceEnterpriseHub
	case "hubcenter", "hub_center":
		return CapabilitySourceHubCenter
	case "clawhub", "claw_hub":
		return CapabilitySourceClawHub
	case "github", "git_hub":
		return CapabilitySourceGitHub
	default:
		return strings.TrimSpace(strings.ToLower(source))
	}
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
	source = NormalizeCapabilitySource(source)
	for _, allowed := range AdminMarketplaceSearchSources(host) {
		if source == allowed {
			return true
		}
	}
	return false
}
