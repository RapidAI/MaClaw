package llmservice

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

type userGroupResolver interface {
	ResolveUserGroupChain(ctx context.Context, email string) ([]string, error)
}

type EntitlementDiagnostic struct {
	Email                    string         `json:"email"`
	ResolvedSecurityGroupIDs []string       `json:"resolved_security_group_ids,omitempty"`
	DirectUserBindings       []UserBinding  `json:"direct_user_bindings,omitempty"`
	MatchedGroupBindings     []GroupBinding `json:"matched_group_bindings,omitempty"`
	ActiveGrants             []Grant        `json:"active_grants,omitempty"`
	BillingRoutes            []BillingRoute `json:"billing_routes,omitempty"`
	ServiceStatus            *ServiceStatus `json:"service_status,omitempty"`
}

type BillingRoute struct {
	ModelName         string   `json:"model_name"`
	ProviderID        string   `json:"provider_id"`
	ServiceGroupIDs   []string `json:"service_group_ids,omitempty"`
	AccessPolicy      string   `json:"access_policy,omitempty"`
	Eligible          bool     `json:"eligible"`
	ReasonCode        string   `json:"reason_code,omitempty"`
	ReasonMessage     string   `json:"reason_message,omitempty"`
	CreditsAvailable  float64  `json:"credits_available,omitempty"`
	HasActiveGrant    bool     `json:"has_active_grant,omitempty"`
	HasAnyGrant       bool     `json:"has_any_grant,omitempty"`
	RequiresGrantOnly bool     `json:"requires_grant_only,omitempty"`
}

func ExplainEntitlementDiagnostic(ctx context.Context, system SystemSettingsRepository, securitySvc *security.SecurityService, email string, hubBaseURL string) (*EntitlementDiagnostic, error) {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return nil, err
	}
	return ExplainEntitlementDiagnosticFromRegistry(ctx, reg, securitySvc, email, hubBaseURL)
}

func ExplainEntitlementDiagnosticFromRegistry(ctx context.Context, reg *Registry, securitySvc *security.SecurityService, email string, hubBaseURL string) (*EntitlementDiagnostic, error) {
	if reg == nil {
		reg = &Registry{}
	}
	reg.Normalize()
	email = normalizeEmail(email)
	diag := &EntitlementDiagnostic{Email: email}
	if email == "" {
		status, _, err := ResolveStatusFromRegistry(ctx, reg, securitySvc, email, hubBaseURL)
		if err != nil {
			return nil, err
		}
		diag.ServiceStatus = status
		return diag, nil
	}
	if securitySvc != nil {
		groupIDs, err := securitySvc.ResolveUserGroupChain(ctx, email)
		if err != nil {
			return nil, err
		}
		diag.ResolvedSecurityGroupIDs = append([]string(nil), groupIDs...)
		groupSet := map[string]struct{}{}
		for _, id := range groupIDs {
			groupSet[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
		}
		for _, binding := range reg.GroupBindings {
			if _, ok := groupSet[strings.ToLower(strings.TrimSpace(binding.GroupID))]; !ok {
				continue
			}
			diag.MatchedGroupBindings = append(diag.MatchedGroupBindings, binding)
		}
	}
	for _, binding := range reg.UserBindings {
		if normalizeEmail(binding.Email) != email {
			continue
		}
		diag.DirectUserBindings = append(diag.DirectUserBindings, binding)
	}
	_, grants, err := effectiveServiceGroupIDs(ctx, reg, securitySvc, email, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	diag.ActiveGrants = append([]Grant(nil), grants...)
	status, _, err := ResolveStatusFromRegistry(ctx, reg, securitySvc, email, hubBaseURL)
	if err != nil {
		return nil, err
	}
	diag.BillingRoutes = ExplainBillingRoutes(reg, email, status.AuthorizedModels, time.Now().UTC())
	diag.ServiceStatus = status
	return diag, nil
}

func ResolveServiceStatus(ctx context.Context, system SystemSettingsRepository, securitySvc *security.SecurityService, email string, hubBaseURL string) (*ServiceStatus, error) {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return nil, err
	}
	status, _, err := ResolveStatusFromRegistry(ctx, reg, securitySvc, email, hubBaseURL)
	if err != nil {
		return nil, err
	}
	return status, nil
}

func ResolveStatusFromRegistry(ctx context.Context, reg *Registry, securitySvc *security.SecurityService, email string, hubBaseURL string) (*ServiceStatus, []AuthorizedModel, error) {
	if reg == nil {
		reg = &Registry{}
	}
	reg.Normalize()
	email = normalizeEmail(email)
	now := time.Now().UTC()
	serviceGroupIDs, grants, err := effectiveServiceGroupIDs(ctx, reg, securitySvc, email, now)
	if err != nil {
		return nil, nil, err
	}
	models, defaultModel := buildAuthorizedModels(reg, serviceGroupIDs)
	serviceGroupNames := make([]string, 0, len(serviceGroupIDs))
	availableModels := make([]string, 0, len(models))
	creditsAvailable := 0.0
	for _, id := range serviceGroupIDs {
		if g := reg.FindModelServiceGroup(id); g != nil {
			serviceGroupNames = append(serviceGroupNames, firstNonEmpty(g.Name, g.ID))
		}
	}
	for _, model := range models {
		availableModels = append(availableModels, model.Name)
	}
	status := &ServiceStatus{
		Active:            len(models) > 0,
		SkipLLMConfig:     len(models) > 0,
		AuthMode:          "viewer_bearer_token",
		ServiceGroupIDs:   append([]string(nil), serviceGroupIDs...),
		ServiceGroupNames: serviceGroupNames,
		AvailableModels:   availableModels,
		AuthorizedModels:  models,
		DefaultModel:      defaultModel,
		HubLLMBaseURL:     strings.TrimRight(strings.TrimSpace(hubBaseURL), "/"),
		TokensPerCredit:   reg.TokensPerCredit,
	}
	if len(grants) > 0 {
		status.ActiveGrants = make([]ActiveGrant, 0, len(grants))
		var nearest *time.Time
		for _, g := range grants {
			remaining := remainingGrantCredits(g)
			if g.CreditsTotal > 0 {
				creditsAvailable += remaining
			}
			status.ActiveGrants = append(status.ActiveGrants, ActiveGrant{ServiceGroupID: g.ServiceGroupID, Source: g.Source, ExpiresAt: g.ExpiresAt, CreditsTotal: g.CreditsTotal, CreditsUsed: g.CreditsUsed, CreditsRemaining: remaining})
			if nearest == nil || g.ExpiresAt.Before(*nearest) {
				copyVal := g.ExpiresAt
				nearest = &copyVal
			}
		}
		sort.Slice(status.ActiveGrants, func(i, j int) bool {
			if status.ActiveGrants[i].ExpiresAt.Equal(status.ActiveGrants[j].ExpiresAt) {
				return status.ActiveGrants[i].ServiceGroupID < status.ActiveGrants[j].ServiceGroupID
			}
			return status.ActiveGrants[i].ExpiresAt.Before(status.ActiveGrants[j].ExpiresAt)
		})
		if nearest != nil {
			status.NearestExpiresAt = nearest.Format(time.RFC3339)
		}
	}
	status.CreditsAvailable = roundCredits(creditsAvailable)
	return status, models, nil
}

func RedeemCard(ctx context.Context, system SystemSettingsRepository, securitySvc *security.SecurityService, email, code, hubBaseURL string) (*ServiceStatus, error) {
	email = normalizeEmail(email)
	code = NormalizeCardCode(code)
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	if code == "" {
		return nil, fmt.Errorf("redeem code is required")
	}
	if err := ValidateCardCode(code); err != nil {
		return nil, err
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return nil, err
	}
	card, idx := reg.FindCardByCode(code)
	if card == nil || idx < 0 {
		return nil, fmt.Errorf("invalid redeem code")
	}
	if card.RedeemedAt != nil {
		return nil, fmt.Errorf("redeem code already used")
	}
	if len(card.ServiceGroupIDs) == 0 {
		return nil, fmt.Errorf("redeem code has no service groups configured")
	}
	days := card.DurationDays
	if days <= 0 {
		days = 30
	}
	now := time.Now().UTC()
	validServiceGroupIDs := make([]string, 0, len(card.ServiceGroupIDs))
	for _, serviceGroupID := range card.ServiceGroupIDs {
		if reg.FindModelServiceGroup(serviceGroupID) == nil {
			continue
		}
		validServiceGroupIDs = append(validServiceGroupIDs, serviceGroupID)
	}
	if len(validServiceGroupIDs) == 0 {
		return nil, fmt.Errorf("redeem code has no valid service groups configured")
	}
	creditsPerGroup := 0.0
	if card.Credits > 0 {
		creditsPerGroup = card.Credits / float64(len(validServiceGroupIDs))
	}
	for _, serviceGroupID := range validServiceGroupIDs {
		startsAt := nextGrantStart(reg, email, serviceGroupID, now)
		expiresAt := startsAt.Add(time.Duration(days) * 24 * time.Hour)
		reg.Grants = append(reg.Grants, Grant{
			ID:             NewID("grant"),
			Email:          email,
			ServiceGroupID: serviceGroupID,
			Source:         "card",
			CardID:         card.ID,
			StartsAt:       startsAt,
			ExpiresAt:      expiresAt,
			CreatedAt:      now,
			CreditsTotal:   creditsPerGroup,
		})
	}
	reg.Cards[idx].RedeemedByEmail = email
	reg.Cards[idx].RedeemedAt = &now
	if err := SaveRegistry(ctx, system, reg); err != nil {
		return nil, err
	}
	status, _, err := ResolveStatusFromRegistry(ctx, reg, securitySvc, email, hubBaseURL)
	if err != nil {
		return nil, err
	}
	return status, nil
}

func nextGrantStart(reg *Registry, email, serviceGroupID string, now time.Time) time.Time {
	start := now
	for _, g := range reg.Grants {
		if !strings.EqualFold(g.Email, email) || !strings.EqualFold(g.ServiceGroupID, serviceGroupID) {
			continue
		}
		if g.ExpiresAt.After(start) {
			start = g.ExpiresAt
		}
	}
	return start
}

func hasGrantWithSource(reg *Registry, email, serviceGroupID, source string) bool {
	return findGrantWithSource(reg, email, serviceGroupID, source) != nil
}

func findGrantWithSource(reg *Registry, email, serviceGroupID, source string) *Grant {
	if reg == nil {
		return nil
	}
	email = normalizeEmail(email)
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	source = strings.TrimSpace(source)
	for i := range reg.Grants {
		grant := &reg.Grants[i]
		if normalizeEmail(grant.Email) != email {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), serviceGroupID) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(grant.Source), source) {
			continue
		}
		return grant
	}
	return nil
}

func effectiveServiceGroupIDs(ctx context.Context, reg *Registry, securitySvc *security.SecurityService, email string, now time.Time) ([]string, []Grant, error) {
	serviceGroupIDs := make([]string, 0)
	seen := map[string]struct{}{}
	appendID := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if reg.FindModelServiceGroup(id) == nil {
			return
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		serviceGroupIDs = append(serviceGroupIDs, id)
	}
	for _, ub := range reg.UserBindings {
		if normalizeEmail(ub.Email) != email {
			continue
		}
		for _, id := range ub.ServiceGroupIDs {
			appendID(id)
		}
	}
	if securitySvc != nil && email != "" {
		groupIDs, err := securitySvc.ResolveUserGroupChain(ctx, email)
		if err != nil {
			return nil, nil, err
		}
		groupSet := map[string]struct{}{}
		for _, id := range groupIDs {
			groupSet[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
		}
		for _, binding := range reg.GroupBindings {
			if _, ok := groupSet[strings.ToLower(strings.TrimSpace(binding.GroupID))]; !ok {
				continue
			}
			for _, id := range binding.ServiceGroupIDs {
				appendID(id)
			}
		}
	}
	activeGrants := make([]Grant, 0)
	for _, g := range reg.Grants {
		if normalizeEmail(g.Email) != email {
			continue
		}
		if now.Before(g.StartsAt) || !now.Before(g.ExpiresAt) {
			continue
		}
		if g.CreditsTotal > 0 && remainingGrantCredits(g) <= 0 {
			continue
		}
		appendID(g.ServiceGroupID)
		activeGrants = append(activeGrants, g)
	}
	return serviceGroupIDs, activeGrants, nil
}

func buildAuthorizedModels(reg *Registry, serviceGroupIDs []string) ([]AuthorizedModel, string) {
	if reg == nil {
		return nil, ""
	}
	modelIndex := map[string]int{}
	models := make([]AuthorizedModel, 0)
	for _, serviceGroupID := range serviceGroupIDs {
		group := reg.FindModelServiceGroup(serviceGroupID)
		if group == nil {
			continue
		}
		for _, model := range group.Models {
			if model.Name == "" || len(model.ProviderIDs) == 0 {
				continue
			}
			idx, ok := modelIndex[strings.ToLower(model.Name)]
			if !ok {
				models = append(models, AuthorizedModel{
					Name:                      model.Name,
					CapabilityTags:            append([]string(nil), model.CapabilityTags...),
					Priority:                  model.Priority,
					ResolutionTier:            model.ResolutionTier,
					CreditMultiplier:          normalizeCreditMultiplier(model.CreditMultiplier),
					ProviderCapabilityTags:    map[string][]string{},
					ProviderPriorities:        map[string]int{},
					ProviderResolutionTiers:   map[string]int{},
					ProviderServiceGroups:     map[string][]string{},
					ProviderCreditMultipliers: map[string]float64{},
				})
				idx = len(models) - 1
				modelIndex[strings.ToLower(model.Name)] = idx
			}
			for _, providerID := range model.ProviderIDs {
				cfg, ok := model.providerConfigByID(providerID)
				if !ok {
					cfg = ModelServiceProviderConfig{
						ProviderID:       providerID,
						CapabilityTags:   append([]string(nil), model.CapabilityTags...),
						Priority:         model.Priority,
						ResolutionTier:   model.ResolutionTier,
						CreditMultiplier: model.CreditMultiplier,
					}
				}
				models[idx].ProviderIDs = mergeStrings(models[idx].ProviderIDs, []string{providerID})
				models[idx].ServiceGroupIDs = mergeStrings(models[idx].ServiceGroupIDs, []string{serviceGroupID})
				models[idx].CapabilityTags = mergeStrings(models[idx].CapabilityTags, cfg.CapabilityTags)
				if cfg.Priority > models[idx].Priority {
					models[idx].Priority = cfg.Priority
				}
				if models[idx].ResolutionTier == 0 || (cfg.ResolutionTier > 0 && cfg.ResolutionTier < models[idx].ResolutionTier) {
					models[idx].ResolutionTier = cfg.ResolutionTier
				}
				key := normalizedProviderKey(providerID)
				if key == "" {
					continue
				}
				models[idx].ProviderCapabilityTags[key] = mergeStrings(models[idx].ProviderCapabilityTags[key], cfg.CapabilityTags)
				if cfg.Priority > models[idx].ProviderPriorities[key] {
					models[idx].ProviderPriorities[key] = cfg.Priority
				}
				if existing := models[idx].ProviderResolutionTiers[key]; existing == 0 || (cfg.ResolutionTier > 0 && cfg.ResolutionTier < existing) {
					models[idx].ProviderResolutionTiers[key] = cfg.ResolutionTier
				}
				models[idx].ProviderServiceGroups[key] = mergeStrings(models[idx].ProviderServiceGroups[key], []string{serviceGroupID})
				candidate := normalizeCreditMultiplier(cfg.CreditMultiplier)
				if existing, ok := models[idx].ProviderCreditMultipliers[key]; !ok || candidate < existing {
					models[idx].ProviderCreditMultipliers[key] = candidate
				}
				if models[idx].CreditMultiplier == 0 || candidate < models[idx].CreditMultiplier {
					models[idx].CreditMultiplier = candidate
				}
			}
		}
	}
	defaultModel := ""
	if len(models) > 0 {
		best := SelectBestModelForRequest(nil, models)
		if best != nil {
			defaultModel = best.Name
		} else {
			defaultModel = models[0].Name
		}
	}
	return models, defaultModel
}

func normalizedProviderKey(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func CapabilityTagsForProvider(model *AuthorizedModel, providerID string) []string {
	if model == nil {
		return nil
	}
	if tags := model.ProviderCapabilityTags[normalizedProviderKey(providerID)]; len(tags) > 0 {
		return append([]string(nil), tags...)
	}
	return append([]string(nil), model.CapabilityTags...)
}

func PriorityForProvider(model *AuthorizedModel, providerID string) int {
	if model == nil {
		return 0
	}
	if value, ok := model.ProviderPriorities[normalizedProviderKey(providerID)]; ok {
		return value
	}
	return model.Priority
}

func ResolutionTierForProvider(model *AuthorizedModel, providerID string) int {
	if model == nil {
		return 0
	}
	if value, ok := model.ProviderResolutionTiers[normalizedProviderKey(providerID)]; ok && value > 0 {
		return value
	}
	return model.ResolutionTier
}

func ServiceGroupIDsForProvider(model *AuthorizedModel, providerID string) []string {
	if model == nil {
		return nil
	}
	if ids := model.ProviderServiceGroups[normalizedProviderKey(providerID)]; len(ids) > 0 {
		return append([]string(nil), ids...)
	}
	return append([]string(nil), model.ServiceGroupIDs...)
}

func CreditMultiplierForProvider(model *AuthorizedModel, providerID string) float64 {
	if model == nil {
		return 1
	}
	if value, ok := model.ProviderCreditMultipliers[normalizedProviderKey(providerID)]; ok && value > 0 {
		return normalizeCreditMultiplier(value)
	}
	return normalizeCreditMultiplier(model.CreditMultiplier)
}

func OrderProvidersForRequest(body map[string]any, model *AuthorizedModel) []string {
	if model == nil || len(model.ProviderIDs) == 0 {
		return nil
	}
	type scoredProvider struct {
		providerID       string
		originalIndex    int
		score            int
		resolutionTier   int
		priority         int
		creditMultiplier float64
	}
	capabilityNeeds := detectCapabilityNeeds(body)
	scored := make([]scoredProvider, 0, len(model.ProviderIDs))
	for idx, providerID := range model.ProviderIDs {
		score := 0
		tags := map[string]struct{}{}
		for _, tag := range CapabilityTagsForProvider(model, providerID) {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			tags[tag] = struct{}{}
		}
		for need, weight := range capabilityNeeds {
			if _, ok := tags[need]; ok {
				score += weight * 100
			}
		}
		priority := PriorityForProvider(model, providerID)
		score += priority
		scored = append(scored, scoredProvider{
			providerID:       providerID,
			originalIndex:    idx,
			score:            score,
			resolutionTier:   normalizedResolutionTier(ResolutionTierForProvider(model, providerID)),
			priority:         priority,
			creditMultiplier: normalizeCreditMultiplier(CreditMultiplierForProvider(model, providerID)),
		})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].resolutionTier != scored[j].resolutionTier {
			return scored[i].resolutionTier < scored[j].resolutionTier
		}
		if scored[i].creditMultiplier != scored[j].creditMultiplier {
			return scored[i].creditMultiplier < scored[j].creditMultiplier
		}
		if scored[i].priority != scored[j].priority {
			return scored[i].priority > scored[j].priority
		}
		return scored[i].originalIndex < scored[j].originalIndex
	})
	ordered := make([]string, 0, len(scored))
	for _, item := range scored {
		ordered = append(ordered, item.providerID)
	}
	return ordered
}

func normalizeCreditMultiplier(v float64) float64 {
	if v <= 0 {
		return 1
	}
	return v
}

func mergeStrings(dst []string, src []string) []string {
	seen := map[string]struct{}{}
	for _, item := range dst {
		seen[strings.ToLower(strings.TrimSpace(item))] = struct{}{}
	}
	for _, item := range src {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		dst = append(dst, trimmed)
	}
	return dst
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func GrantDefaultServiceForNewUser(ctx context.Context, system SystemSettingsRepository, email string) error {
	return grantNewUserBenefit(ctx, system, email, "new_user_default", 0.30, false)
}

func GrantEmailConfirmedBenefitForUser(ctx context.Context, system SystemSettingsRepository, email string) error {
	return grantNewUserBenefit(ctx, system, email, "new_user_email_confirmed", 0.70, true)
}

func grantNewUserBenefit(ctx context.Context, system SystemSettingsRepository, email, source string, ratio float64, useRegistrationWindow bool) error {
	email = normalizeEmail(email)
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if ratio <= 0 {
		return nil
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return err
	}
	serviceGroupIDs := normalizeStringSlice(reg.DefaultNewUserServiceGroups)
	if len(serviceGroupIDs) == 0 {
		return nil
	}
	validServiceGroupIDs := make([]string, 0, len(serviceGroupIDs))
	for _, serviceGroupID := range serviceGroupIDs {
		if reg.FindModelServiceGroup(serviceGroupID) != nil {
			validServiceGroupIDs = append(validServiceGroupIDs, serviceGroupID)
		}
	}
	if len(validServiceGroupIDs) == 0 {
		return nil
	}
	days := reg.DefaultNewUserDurationDays
	if days <= 0 {
		days = 30
	}
	credits := reg.DefaultNewUserCredits
	if credits <= 0 {
		credits = DefaultNewUserCredits
	}
	creditsPerGroup := roundCredits((credits * ratio) / float64(len(validServiceGroupIDs)))
	now := time.Now().UTC()
	created := false
	for _, serviceGroupID := range validServiceGroupIDs {
		if hasGrantWithSource(reg, email, serviceGroupID, source) {
			continue
		}
		startsAt := nextGrantStart(reg, email, serviceGroupID, now)
		expiresAt := startsAt.Add(time.Duration(days) * 24 * time.Hour)
		if useRegistrationWindow {
			base := findGrantWithSource(reg, email, serviceGroupID, "new_user_default")
			if base == nil {
				continue
			}
			startsAt = base.StartsAt
			expiresAt = base.ExpiresAt
			if !now.Before(expiresAt) {
				continue
			}
		}
		reg.Grants = append(reg.Grants, Grant{
			ID:             NewID("grant"),
			Email:          email,
			ServiceGroupID: serviceGroupID,
			Source:         source,
			StartsAt:       startsAt,
			ExpiresAt:      expiresAt,
			CreatedAt:      now,
			CreditsTotal:   creditsPerGroup,
		})
		created = true
	}
	if !created {
		return nil
	}
	return SaveRegistry(ctx, system, reg)
}

func remainingGrantCredits(grant Grant) float64 {
	if grant.CreditsTotal <= 0 {
		return 0
	}
	remaining := grant.CreditsTotal - grant.CreditsUsed
	if remaining < 0 {
		return 0
	}
	return roundCredits(remaining)
}

func roundCredits(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func EstimateCredits(tokens int64, multiplier float64, tokensPerCredit int) float64 {
	if tokens <= 0 {
		return 0
	}
	if tokensPerCredit <= 0 {
		tokensPerCredit = DefaultTokensPerCredit
	}
	return roundCredits((float64(tokens) * normalizeCreditMultiplier(multiplier)) / float64(tokensPerCredit))
}

func ApplyCreditUsageToRegistry(reg *Registry, email string, serviceGroupIDs []string, credits float64, now time.Time) float64 {
	if reg == nil || credits <= 0 {
		return 0
	}
	email = normalizeEmail(email)
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if email == "" || len(serviceGroupIDs) == 0 {
		return 0
	}
	type candidate struct {
		idx int
		g   Grant
	}
	candidates := make([]candidate, 0)
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	}
	for i, grant := range reg.Grants {
		if normalizeEmail(grant.Email) != email {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
			continue
		}
		if grant.CreditsTotal <= 0 || remainingGrantCredits(grant) <= 0 {
			continue
		}
		candidates = append(candidates, candidate{idx: i, g: grant})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].g.ExpiresAt.Equal(candidates[j].g.ExpiresAt) {
			return candidates[i].g.CreatedAt.Before(candidates[j].g.CreatedAt)
		}
		return candidates[i].g.ExpiresAt.Before(candidates[j].g.ExpiresAt)
	})
	remaining := credits
	consumed := 0.0
	for _, cand := range candidates {
		grant := &reg.Grants[cand.idx]
		available := remainingGrantCredits(*grant)
		if available <= 0 {
			continue
		}
		use := math.Min(available, remaining)
		grant.CreditsUsed = roundCredits(grant.CreditsUsed + use)
		consumed += use
		remaining -= use
		if remaining <= 0 {
			break
		}
	}
	return roundCredits(consumed)
}

func AvailableCreditsForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) float64 {
	if reg == nil {
		return 0
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if len(serviceGroupIDs) == 0 {
		return 0
	}
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(id)] = struct{}{}
	}
	total := 0.0
	for _, grant := range reg.Grants {
		if normalizeEmail(grant.Email) != normalizeEmail(email) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
			continue
		}
		total += remainingGrantCredits(grant)
	}
	return roundCredits(total)
}

func HasAnyGrantForServiceGroups(reg *Registry, email string, serviceGroupIDs []string) bool {
	if reg == nil {
		return false
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if len(serviceGroupIDs) == 0 {
		return false
	}
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(id)] = struct{}{}
	}
	for _, grant := range reg.Grants {
		if normalizeEmail(grant.Email) != normalizeEmail(email) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; ok {
			return true
		}
	}
	return false
}

func HasActiveGrantForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) bool {
	if reg == nil {
		return false
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if len(serviceGroupIDs) == 0 {
		return false
	}
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(id)] = struct{}{}
	}
	for _, grant := range reg.Grants {
		if normalizeEmail(grant.Email) != normalizeEmail(email) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
			continue
		}
		return true
	}
	return false
}

func BillingEligibilityForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) (bool, string, string, string, float64, bool, bool) {
	if reg == nil {
		return true, AccessPolicyFree, "", "", 0, false, false
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	grantRequiredGroupIDs := make([]string, 0, len(serviceGroupIDs))
	for _, serviceGroupID := range serviceGroupIDs {
		if reg.AccessPolicyForServiceGroup(serviceGroupID) != AccessPolicyGrantRequired {
			return true, AccessPolicyFree, "", "", 0, false, false
		}
		grantRequiredGroupIDs = append(grantRequiredGroupIDs, serviceGroupID)
	}
	if len(grantRequiredGroupIDs) == 0 {
		return true, AccessPolicyFree, "", "", 0, false, false
	}
	availableCredits := AvailableCreditsForServiceGroups(reg, email, grantRequiredGroupIDs, now)
	if availableCredits > 0 {
		return true, AccessPolicyGrantRequired, "", "", roundCredits(availableCredits), true, true
	}
	hasActiveGrant := HasActiveGrantForServiceGroups(reg, email, grantRequiredGroupIDs, now)
	if hasActiveGrant {
		return false, AccessPolicyGrantRequired, "LLM_SERVICE_CREDITS_EXHAUSTED", "selected model requires remaining credits in an active grant", 0, true, true
	}
	hasAnyGrant := HasAnyGrantForServiceGroups(reg, email, grantRequiredGroupIDs)
	if hasAnyGrant {
		return false, AccessPolicyGrantRequired, "LLM_SERVICE_GRANT_EXPIRED", "selected model requires an active grant for this service group", 0, false, true
	}
	return false, AccessPolicyGrantRequired, "LLM_SERVICE_CREDITS_REQUIRED", "selected model requires a grant-backed service group with remaining credits", 0, false, false
}

func ExplainBillingRoutes(reg *Registry, email string, models []AuthorizedModel, now time.Time) []BillingRoute {
	if reg == nil || len(models) == 0 {
		return nil
	}
	email = normalizeEmail(email)
	routes := make([]BillingRoute, 0)
	for _, model := range models {
		for _, providerID := range model.ProviderIDs {
			serviceGroupIDs := ServiceGroupIDsForProvider(&model, providerID)
			eligible, accessPolicy, code, message, creditsAvailable, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, email, serviceGroupIDs, now)
			routes = append(routes, BillingRoute{
				ModelName:         model.Name,
				ProviderID:        providerID,
				ServiceGroupIDs:   append([]string(nil), serviceGroupIDs...),
				AccessPolicy:      accessPolicy,
				Eligible:          eligible,
				ReasonCode:        code,
				ReasonMessage:     message,
				CreditsAvailable:  creditsAvailable,
				HasActiveGrant:    hasActiveGrant,
				HasAnyGrant:       hasAnyGrant,
				RequiresGrantOnly: accessPolicy == AccessPolicyGrantRequired,
			})
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Eligible != routes[j].Eligible {
			return routes[i].Eligible
		}
		if routes[i].ModelName != routes[j].ModelName {
			return strings.ToLower(routes[i].ModelName) < strings.ToLower(routes[j].ModelName)
		}
		return strings.ToLower(routes[i].ProviderID) < strings.ToLower(routes[j].ProviderID)
	})
	return routes
}

type ModelSelectionDebug struct {
	SelectedModel    string   `json:"selected_model"`
	CapabilityNeeds  []string `json:"capability_needs,omitempty"`
	MatchedTags      []string `json:"matched_tags,omitempty"`
	Score            int      `json:"score"`
	Priority         int      `json:"priority,omitempty"`
	ResolutionTier   int      `json:"resolution_tier,omitempty"`
	CreditMultiplier float64  `json:"credit_multiplier,omitempty"`
	SelectionReason  string   `json:"selection_reason,omitempty"`
}

func SelectBestModelForRequest(body map[string]any, models []AuthorizedModel) *AuthorizedModel {
	selected, _ := SelectBestModelForRequestWithDebug(body, models)
	return selected
}

func SelectBestModelForRequestWithDebug(body map[string]any, models []AuthorizedModel) (*AuthorizedModel, *ModelSelectionDebug) {
	if len(models) == 0 {
		return nil, nil
	}
	type scoredModel struct {
		idx              int
		score            int
		resolutionTier   int
		priority         int
		creditMultiplier float64
		matchedTags      []string
	}
	capabilityNeeds := detectCapabilityNeeds(body)
	scored := make([]scoredModel, 0, len(models))
	for i, model := range models {
		score := 0
		tags := map[string]struct{}{}
		for _, tag := range model.CapabilityTags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			tags[tag] = struct{}{}
		}
		matchedTags := make([]string, 0, len(capabilityNeeds))
		for need, weight := range capabilityNeeds {
			if _, ok := tags[need]; ok {
				score += weight * 100
				matchedTags = append(matchedTags, need)
			}
		}
		score += model.Priority
		scored = append(scored, scoredModel{idx: i, score: score, resolutionTier: normalizedResolutionTier(model.ResolutionTier), priority: model.Priority, creditMultiplier: normalizeCreditMultiplier(model.CreditMultiplier), matchedTags: matchedTags})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].resolutionTier != scored[j].resolutionTier {
			return scored[i].resolutionTier < scored[j].resolutionTier
		}
		if scored[i].creditMultiplier != scored[j].creditMultiplier {
			return scored[i].creditMultiplier < scored[j].creditMultiplier
		}
		if scored[i].priority != scored[j].priority {
			return scored[i].priority > scored[j].priority
		}
		return strings.ToLower(models[scored[i].idx].Name) < strings.ToLower(models[scored[j].idx].Name)
	})
	best := scored[0]
	selected := &models[best.idx]
	capabilityNeedsList := make([]string, 0, len(capabilityNeeds))
	for need := range capabilityNeeds {
		capabilityNeedsList = append(capabilityNeedsList, need)
	}
	sort.Strings(capabilityNeedsList)
	matchedTags := append([]string(nil), best.matchedTags...)
	sort.Strings(matchedTags)
	reasonParts := make([]string, 0, 4)
	if len(matchedTags) > 0 {
		reasonParts = append(reasonParts, "matched="+strings.Join(matchedTags, ","))
	} else {
		reasonParts = append(reasonParts, "matched=none")
	}
	reasonParts = append(reasonParts, fmt.Sprintf("score=%d", best.score))
	reasonParts = append(reasonParts, fmt.Sprintf("resolution=%d", normalizedResolutionTier(selected.ResolutionTier)))
	reasonParts = append(reasonParts, fmt.Sprintf("multiplier=%.3g", normalizeCreditMultiplier(selected.CreditMultiplier)))
	return selected, &ModelSelectionDebug{
		SelectedModel:    selected.Name,
		CapabilityNeeds:  capabilityNeedsList,
		MatchedTags:      matchedTags,
		Score:            best.score,
		Priority:         selected.Priority,
		ResolutionTier:   normalizedResolutionTier(selected.ResolutionTier),
		CreditMultiplier: normalizeCreditMultiplier(selected.CreditMultiplier),
		SelectionReason:  strings.Join(reasonParts, "; "),
	}
}

func normalizedResolutionTier(v int) int {
	if v <= 0 {
		return 1000
	}
	return v
}

func detectCapabilityNeeds(body map[string]any) map[string]int {
	needs := map[string]int{}
	if body == nil {
		return needs
	}
	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		needs["tools"] += 8
	}
	if toolChoice := strings.TrimSpace(strings.ToLower(stringValue(body["tool_choice"]))); toolChoice != "" && toolChoice != "none" {
		needs["tools"] += 4
	}
	text := strings.ToLower(extractRequestText(body))
	addKeywordWeight := func(tag string, weight int, keywords ...string) {
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				needs[tag] += weight
				return
			}
		}
	}
	addKeywordWeight("document", 8, "document", "pdf", "docx", "markdown", "contract", "manual", "spec", "report", "summary", "summarize", "read file")
	addKeywordWeight("reasoning", 5, "reason", "analyze", "analysis", "think", "math", "proof", "deduce")
	addKeywordWeight("tools", 8, "tool", "browser", "search", "function", "call tool", "execute", "fetch")
	return needs
}

func extractRequestText(body map[string]any) string {
	if body == nil {
		return ""
	}
	var parts []string
	if messages, ok := body["messages"].([]any); ok {
		for _, item := range messages {
			parts = append(parts, flattenAnyText(item))
		}
	}
	if input, ok := body["input"]; ok {
		parts = append(parts, flattenAnyText(input))
	}
	return strings.Join(parts, " ")
}

func flattenAnyText(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, flattenAnyText(item))
		}
		return strings.Join(parts, " ")
	case map[string]any:
		parts := make([]string, 0, len(val))
		for _, key := range []string{"content", "text", "input", "name", "description", "arguments"} {
			if sub, ok := val[key]; ok {
				parts = append(parts, flattenAnyText(sub))
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
