package llmservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
)

// cardRedeemMu serializes card redemption to prevent TOCTOU race conditions
// where two concurrent requests could both pass the "not yet redeemed" check
// and create duplicate grants from the same card.
var cardRedeemMu sync.Mutex

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
	owner := newUserAccountRef("", email)
	diag := &EntitlementDiagnostic{Email: email}
	if email == "" {
		status, _, err := ResolveStatusFromRegistry(ctx, reg, securitySvc, email, hubBaseURL)
		if err != nil {
			return nil, err
		}
		diag.ServiceStatus = status
		return diag, nil
	}
	for _, binding := range reg.UserBindings {
		if normalizeEmail(binding.Email) != email {
			continue
		}
		knownIDs := knownServiceGroupIDs(reg, binding.ServiceGroupIDs)
		if len(knownIDs) == 0 {
			continue
		}
		binding.ServiceGroupIDs = knownIDs
		diag.DirectUserBindings = append(diag.DirectUserBindings, binding)
	}
	if securitySvc != nil {
		groupIDs, err := securitySvc.ResolveUserGroupChain(ctx, email)
		if err != nil {
			return nil, err
		}
		diag.ResolvedSecurityGroupIDs = append([]string(nil), groupIDs...)
		if len(diag.DirectUserBindings) == 0 {
			diag.MatchedGroupBindings = appliedGroupBindings(reg, groupIDs)
		}
	}
	_, grants, err := effectiveServiceGroupIDsForOwner(ctx, reg, securitySvc, owner, time.Now().UTC())
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

func knownServiceGroupIDs(reg *Registry, ids []string) []string {
	if reg == nil {
		return nil
	}
	knownIDs := make([]string, 0, len(ids))
	for _, id := range normalizeStringSlice(ids) {
		if reg.FindModelServiceGroup(id) != nil {
			knownIDs = append(knownIDs, id)
		}
	}
	return knownIDs
}

func appliedGroupBindings(reg *Registry, groupIDs []string) []GroupBinding {
	if reg == nil || len(groupIDs) == 0 {
		return nil
	}
	bindingsByGroup := map[string][]string{}
	for _, binding := range reg.GroupBindings {
		groupID := strings.ToLower(strings.TrimSpace(binding.GroupID))
		if groupID == "" {
			continue
		}
		bindingsByGroup[groupID] = append(bindingsByGroup[groupID], binding.ServiceGroupIDs...)
	}
	for _, groupID := range groupIDs {
		cleanGroupID := strings.TrimSpace(groupID)
		knownIDs := knownServiceGroupIDs(reg, bindingsByGroup[strings.ToLower(cleanGroupID)])
		if len(knownIDs) == 0 {
			continue
		}
		return []GroupBinding{{GroupID: cleanGroupID, ServiceGroupIDs: knownIDs}}
	}
	return nil
}

func ResolveServiceStatus(ctx context.Context, system SystemSettingsRepository, securitySvc *security.SecurityService, email string, hubBaseURL string) (*ServiceStatus, error) {
	return ResolveServiceStatusForUserID(ctx, system, securitySvc, "", email, hubBaseURL)
}

func ResolveServiceStatusForUserID(ctx context.Context, system SystemSettingsRepository, securitySvc *security.SecurityService, userID, email string, hubBaseURL string) (*ServiceStatus, error) {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return nil, err
	}
	status, _, err := ResolveStatusFromRegistryForUser(ctx, reg, securitySvc, userID, email, hubBaseURL)
	if err != nil {
		return nil, err
	}
	return status, nil
}

func ResolveStatusFromRegistry(ctx context.Context, reg *Registry, securitySvc *security.SecurityService, email string, hubBaseURL string) (*ServiceStatus, []AuthorizedModel, error) {
	return ResolveStatusFromRegistryForUser(ctx, reg, securitySvc, "", email, hubBaseURL)
}

// ResolveStatusForForcedServiceGroup authorizes only the given service group
// and ignores user bindings, grants, and new-user benefits. It is used by
// admin-issued service-group API keys that authenticate as sys_user.
func ResolveStatusForForcedServiceGroup(reg *Registry, serviceGroupID, hubBaseURL string) (*ServiceStatus, []AuthorizedModel, error) {
	if reg == nil {
		reg = &Registry{}
	}
	reg.Normalize()
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	group := reg.FindModelServiceGroup(serviceGroupID)
	if serviceGroupID == "" || group == nil {
		return &ServiceStatus{
			Active:          false,
			AuthMode:        "service_group_api_key",
			InactiveReasons: []string{"service group is not configured"},
			HubLLMBaseURL:   strings.TrimRight(strings.TrimSpace(hubBaseURL), "/"),
			TokensPerCredit: reg.TokensPerCredit,
		}, nil, nil
	}
	serviceGroupIDs := []string{group.ID}
	models, defaultModel := buildAuthorizedModels(reg, serviceGroupIDs)
	availableModels := make([]string, 0, len(models))
	for _, model := range models {
		availableModels = append(availableModels, model.Name)
	}
	active := len(models) > 0
	status := &ServiceStatus{
		Active:            active,
		SkipLLMConfig:     active,
		AuthMode:          "service_group_api_key",
		ServiceGroupIDs:   append([]string(nil), serviceGroupIDs...),
		ServiceGroupNames: []string{firstNonEmpty(group.Name, group.ID)},
		AvailableModels:   availableModels,
		AuthorizedModels:  models,
		DefaultModel:      defaultModel,
		HubLLMBaseURL:     strings.TrimRight(strings.TrimSpace(hubBaseURL), "/"),
		TokensPerCredit:   reg.TokensPerCredit,
	}
	if !active {
		status.InactiveReasons = []string{"service group has no authorized models"}
	}
	return status, models, nil
}

func ResolveStatusFromRegistryForUser(ctx context.Context, reg *Registry, securitySvc *security.SecurityService, userID, email string, hubBaseURL string) (*ServiceStatus, []AuthorizedModel, error) {
	if reg == nil {
		reg = &Registry{}
	}
	reg.Normalize()
	owner := newUserAccountRef(userID, email)
	now := time.Now().UTC()
	email = owner.Email
	serviceGroupIDs, grants, err := effectiveServiceGroupIDsForOwner(ctx, reg, securitySvc, owner, now)
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
	active := hasEligibleAuthorizedModel(reg, owner, models, now)
	status := &ServiceStatus{
		Active:            active,
		SkipLLMConfig:     active,
		AuthMode:          "viewer_bearer_token",
		ServiceGroupIDs:   append([]string(nil), serviceGroupIDs...),
		ServiceGroupNames: serviceGroupNames,
		AvailableModels:   availableModels,
		AuthorizedModels:  models,
		DefaultModel:      defaultModel,
		HubLLMBaseURL:     strings.TrimRight(strings.TrimSpace(hubBaseURL), "/"),
		TokensPerCredit:   reg.TokensPerCredit,
	}
	status.ResetVouchers = liveResetVouchersForOwner(reg, owner, now)
	status.CreditGrants = creditGrantSummariesForOwner(reg, owner, now)
	for _, g := range status.CreditGrants {
		// A new-user limit card has no lifetime balance. Its CreditsAvailable
		// value is the remaining amount in a short window and is exposed through
		// PeriodLimits/PeriodUsage below; including it in account totals would
		// falsely turn a zero-total entitlement into a 10-credit wallet.
		if isNewUserLimitCardSource(g.Source) && g.CreditsTotal <= 0 {
			continue
		}
		// Only accumulate credits from currently effective grants.
		// Exclude "queued" (not yet started) and "expired" grants from totals
		// so users see accurate available credits.
		grantStatus := strings.ToLower(strings.TrimSpace(g.Status))
		if grantStatus == "queued" || grantStatus == "expired" {
			continue
		}
		status.CreditsTotal += g.CreditsTotal
		status.CreditsUsed += g.CreditsUsed
		status.CreditsRemaining += g.CreditsRemaining
	}
	status.CreditsTotal = roundCredits(status.CreditsTotal)
	status.CreditsUsed = roundCredits(status.CreditsUsed)
	status.CreditsRemaining = roundCredits(status.CreditsRemaining)
	if !status.Active || len(serviceGroupIDs) == 0 {
		status.InactiveReasons = grantStateInactiveReasons(status.CreditGrants)
	}
	if len(grants) > 0 {
		status.ActiveGrants = make([]ActiveGrant, 0, len(grants))
		var nearest *time.Time
		activeHeldByGroup := reservedBillingCreditsByGroup(reg, owner, now)
		for _, g := range grants {
			g = effectiveGrantForRegistry(reg, g)
			creditsAvailable += availableGrantCredits(g, now)
			summary := grantSummary(g, now)
			summary.HeldCredits = activeHeldByGroup[strings.ToLower(strings.TrimSpace(summary.ServiceGroupID))]
			status.ActiveGrants = append(status.ActiveGrants, summary)
			// Permanent grants use a far-future storage sentinel. It is not a
			// customer-visible expiry date, so do not surface year 9999 through
			// nearest_expires_at.
			if !g.Permanent && (nearest == nil || g.ExpiresAt.Before(*nearest)) {
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
	if len(serviceGroupIDs) > 0 {
		creditsAvailable = availableCreditsForServiceGroups(reg, owner, serviceGroupIDs, now)
	}
	// A welcome limit card's spendable amount is an anchored five-hour/daily window, not a
	// lifetime account balance. Its detailed period allowance remains on the
	// grant summary; do not expose it as the wallet-level available amount.
	if hasPeriodLimitedNewUserLimitCardForAnyServiceGroup(reg, owner, serviceGroupIDs, now) {
		creditsAvailable = 0
	}
	if effectiveExpiresAt := effectiveGrantExpiresAt(reg, owner, now); effectiveExpiresAt != nil {
		status.EffectiveExpiresAt = effectiveExpiresAt.Format(time.RFC3339)
	}
	status.CreditsAvailable = roundCredits(creditsAvailable)
	// Unlimited free path: hide credit meters when nothing is currently
	// spendable on a metered grant (CreditsAvailable==0). If a paid point card
	// is currently spendable (Available>0), keep showing its balance even when
	// a free unlimited gift is also present.
	if status.Active && len(serviceGroupIDs) > 0 && status.CreditsAvailable <= 0 && (hasUnlimitedActiveGrantForServiceGroups(reg, owner, serviceGroupIDs, now) || hasEarlyStartableUnmeteredUnlimitedGrant(reg, owner, serviceGroupIDs, now)) && !hasPeriodLimitedNewUserLimitCardForAnyServiceGroup(reg, owner, serviceGroupIDs, now) {
		status.CreditsTotal = 0
		status.CreditsUsed = 0
		status.CreditsRemaining = 0
		status.CreditsAvailable = 0
	} else if status.CreditsAvailable > status.CreditsRemaining {
		// Early-started queued top-ups can make currently spendable credits exceed
		// effective lifetime remaining. Lift remaining up so UI is not understated.
		// Never shrink remaining down to a period window (Available < Remaining).
		status.CreditsRemaining = status.CreditsAvailable
	} else if status.CreditsRemaining <= 0 && status.CreditsAvailable > 0 {
		status.CreditsRemaining = status.CreditsAvailable
	}
	if status.CreditsRemaining > 0 && status.CreditsTotal < status.CreditsUsed+status.CreditsRemaining {
		status.CreditsTotal = roundCredits(status.CreditsUsed + status.CreditsRemaining)
	}
	return status, models, nil
}

type userAccountRef struct {
	UserID string
	Email  string
}

func newUserAccountRef(userID, email string) userAccountRef {
	if strings.TrimSpace(email) == "" && strings.TrimSpace(userID) != "" {
		value := strings.TrimSpace(userID)
		if strings.Contains(value, "@") || strings.HasPrefix(strings.ToLower(value), "phone:") {
			email = value
			userID = ""
		}
	}
	return userAccountRef{UserID: normalizeUserID(userID), Email: normalizeEmail(email)}
}

func (r userAccountRef) empty() bool {
	return r.UserID == "" && r.Email == ""
}

func voucherOwnerKey(owner userAccountRef) string {
	if owner.UserID != "" {
		return "id:" + owner.UserID
	}
	return "email:" + owner.Email
}

func grantMatchesUser(g Grant, owner userAccountRef) bool {
	if owner.UserID != "" && normalizeUserID(g.UserID) == owner.UserID {
		return true
	}
	return owner.Email != "" && normalizeEmail(g.Email) == owner.Email
}

func bindingMatchesUser(binding UserBinding, owner userAccountRef) bool {
	if owner.UserID != "" && normalizeUserID(binding.UserID) == owner.UserID {
		return true
	}
	return owner.Email != "" && normalizeEmail(binding.Email) == owner.Email
}

func cardRedeemedByUser(card RechargeCard, owner userAccountRef) bool {
	if owner.UserID != "" && normalizeUserID(card.RedeemedByUserID) == owner.UserID {
		return true
	}
	return owner.Email != "" && normalizeEmail(card.RedeemedByEmail) == owner.Email
}

func creditGrantSummaries(reg *Registry, email string, now time.Time) []ActiveGrant {
	return creditGrantSummariesForOwner(reg, newUserAccountRef("", email), now)
}

func creditGrantSummariesForOwner(reg *Registry, owner userAccountRef, now time.Time) []ActiveGrant {
	if reg == nil {
		return nil
	}
	items := make([]ActiveGrant, 0)
	limitedCardGroups := activePeriodLimitedNewUserLimitCardGroupSet(reg, owner, allServiceGroupIDSet(reg), now)
	seenLimitCardGroups := map[string]struct{}{}
	var latestExpired *Grant
	for _, g := range reg.Grants {
		if !grantMatchesUser(g, owner) {
			continue
		}
		if g.Frozen {
			continue
		}
		if isNewUserLimitCardSource(g.Source) && !isActiveNewUserLimitCardPolicyGroup(reg, g.ServiceGroupID) {
			continue
		}
		if reg.FindModelServiceGroup(g.ServiceGroupID) == nil {
			continue
		}
		effective := effectiveGrantForRegistry(reg, g)
		isLimitCard := isNewUserLimitCardSource(effective.Source)
		groupID := strings.ToLower(strings.TrimSpace(effective.ServiceGroupID))
		if isLimitCard {
			if _, duplicate := seenLimitCardGroups[groupID]; duplicate {
				continue
			}
		} else if nonWelcomeGrantBlockedByLimitCard(reg, effective, limitedCardGroups) {
			continue
		}
		if !grantIsValidAt(effective, now) {
			if latestExpired == nil || effective.ExpiresAt.After(latestExpired.ExpiresAt) {
				copyGrant := effective
				latestExpired = &copyGrant
			}
			continue
		}
		if isLimitCard {
			// Only live qualifications occupy the per-group slot. An expired
			// row must not hide a later backfilled card on the same group.
			seenLimitCardGroups[groupID] = struct{}{}
		}
		items = append(items, grantSummary(effective, now))
	}
	if len(items) == 0 && latestExpired != nil {
		items = append(items, grantSummary(*latestExpired, now))
	}
	if len(items) > 0 {
		// Attribute the group's in-flight reservation holds to each visible grant
		// so status consumers can render "frozen" credit alongside period windows.
		// The value is group-scoped, not grant-scoped; consumers merging several
		// grants of one group must take the maximum, not the sum.
		heldByGroup := reservedBillingCreditsByGroup(reg, owner, now)
		for i := range items {
			items[i].HeldCredits = heldByGroup[strings.ToLower(strings.TrimSpace(items[i].ServiceGroupID))]
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if rankI, rankJ := grantSummarySortRank(items[i]), grantSummarySortRank(items[j]); rankI != rankJ {
			return rankI < rankJ
		}
		iWelcome := isNewUserLimitCardSource(items[i].Source)
		jWelcome := isNewUserLimitCardSource(items[j].Source)
		if iWelcome != jWelcome {
			return iWelcome
		}
		if items[i].StartsAt.Equal(items[j].StartsAt) {
			if items[i].ExpiresAt.Equal(items[j].ExpiresAt) {
				return items[i].ServiceGroupID < items[j].ServiceGroupID
			}
			return items[i].ExpiresAt.Before(items[j].ExpiresAt)
		}
		return items[i].StartsAt.Before(items[j].StartsAt)
	})
	return items
}

func allServiceGroupIDSet(reg *Registry) map[string]struct{} {
	if reg == nil || len(reg.ModelServiceGroups) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(reg.ModelServiceGroups))
	for _, group := range reg.ModelServiceGroups {
		if id := strings.ToLower(strings.TrimSpace(group.ID)); id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func grantSummarySortRank(grant ActiveGrant) int {
	switch strings.ToLower(strings.TrimSpace(grant.Status)) {
	case "active":
		return 0
	case "period_limited":
		return 1
	case "queued":
		return 2
	case "exhausted":
		return 3
	case "expired":
		return 4
	default:
		if grant.Active {
			return 0
		}
		return 5
	}
}

func grantStateInactiveReasons(grants []ActiveGrant) []string {
	if len(grants) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(grants))
	seen := map[string]struct{}{}
	add := func(reason string) {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return
		}
		key := strings.ToLower(reason)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		reasons = append(reasons, reason)
	}
	for _, grant := range grants {
		switch strings.ToLower(strings.TrimSpace(grant.Status)) {
		case "period_limited":
			reason := "current period credit limit is exhausted"
			if grant.RetryAfterAt != "" {
				reason += "; retry after " + grant.RetryAfterAt
			}
			add(reason)
		case "exhausted":
			add("grant credits are exhausted")
		case "queued":
			reason := "grant is not active yet"
			if grant.RetryAfterAt != "" {
				reason += "; starts at " + grant.RetryAfterAt
			}
			add(reason)
		case "expired":
			add("grant has expired")
		}
	}
	return reasons
}

func grantSummary(g Grant, now time.Time) ActiveGrant {
	status, reason, active, retryAt := grantStatus(g, now)
	available := availableGrantCredits(g, now)
	// A grant is "effective" (counts toward credit totals) when it is currently
	// within its validity period. Queued (not yet started) and expired grants
	// are not effective.
	effective := status != "queued" && status != "expired"
	summary := ActiveGrant{
		ID:               g.ID,
		ServiceGroupID:   g.ServiceGroupID,
		Source:           g.Source,
		CardID:           g.CardID,
		StartsAt:         g.StartsAt,
		ExpiresAt:        g.ExpiresAt,
		Permanent:        g.Permanent,
		RollingFiveHour:  g.RollingFiveHour,
		Active:           active,
		Effective:        effective,
		Status:           status,
		StatusReason:     reason,
		CreditsTotal:     roundCredits(g.CreditsTotal),
		CreditsUsed:      roundCredits(g.CreditsUsed),
		CreditsAvailable: available,
		CreditsRemaining: remainingGrantCredits(g),
	}
	if retryAt != nil && retryAt.After(now) {
		summary.RetryAfterSeconds = int64(math.Ceil(retryAt.Sub(now).Seconds()))
		summary.RetryAfterAt = retryAt.Format(time.RFC3339)
	}
	// Include period limits and usage when the grant has any period constraints.
	if g.PeriodLimits.FiveHour > 0 || g.PeriodLimits.Daily > 0 || g.PeriodLimits.Weekly > 0 || g.PeriodLimits.Monthly > 0 {
		limits := g.PeriodLimits
		summary.PeriodLimits = &limits
		// Build API-facing usage summary with precise window_end computed from
		// the same window functions used for billing eligibility checks.
		fhStart := grantFiveHourWindowStart(g, now)
		fhEnd := fhStart.Add(5 * time.Hour)
		fhUsed := 0.0
		if g.RollingFiveHour {
			fhStart, fhEnd, fhUsed = rollingFiveHourUsage(g, now)
		} else if g.PeriodUsage.FiveHour.WindowStart.Equal(fhStart) {
			fhUsed = g.PeriodUsage.FiveHour.CreditsUsed
		}
		dStart := grantDayWindowStart(g, now)
		wStart := grantWeekWindowStart(g, now)
		mStart := grantMonthWindowStart(g, now)
		dUsed := 0.0
		if g.PeriodUsage.Daily.WindowStart.Equal(dStart) {
			dUsed = g.PeriodUsage.Daily.CreditsUsed
		}
		wUsed := 0.0
		if g.PeriodUsage.Weekly.WindowStart.Equal(wStart) {
			wUsed = g.PeriodUsage.Weekly.CreditsUsed
		}
		mUsed := 0.0
		if g.PeriodUsage.Monthly.WindowStart.Equal(mStart) {
			mUsed = g.PeriodUsage.Monthly.CreditsUsed
		}
		summary.PeriodUsage = &ActiveGrantPeriodUsage{
			FiveHour: ActiveGrantUsageWindow{
				WindowStart: fhStart,
				WindowEnd:   fhEnd,
				CreditsUsed: roundCredits(fhUsed),
				Rolling:     g.RollingFiveHour,
			},
			Daily: ActiveGrantUsageWindow{
				WindowStart: dStart,
				WindowEnd:   dStart.AddDate(0, 0, 1),
				CreditsUsed: roundCredits(dUsed),
			},
			Weekly: ActiveGrantUsageWindow{
				WindowStart: wStart,
				WindowEnd:   wStart.AddDate(0, 0, 7),
				CreditsUsed: roundCredits(wUsed),
			},
			Monthly: ActiveGrantUsageWindow{
				WindowStart: mStart,
				WindowEnd:   mStart.AddDate(0, 1, 0),
				CreditsUsed: roundCredits(mUsed),
			},
		}
	}
	return summary
}

func grantStatus(g Grant, now time.Time) (string, string, bool, *time.Time) {
	if now.Before(g.StartsAt) {
		copyVal := g.StartsAt
		return "queued", "grant starts in the future", false, &copyVal
	}
	if !grantIsValidAt(g, now) {
		return "expired", "grant has expired", false, nil
	}
	if g.CreditsTotal > 0 {
		remaining := remainingGrantCredits(g)
		if remaining <= 0 {
			return "exhausted", "grant credits are exhausted", false, nil
		}
	}
	if availableGrantPeriodCredits(g, now) <= 0 {
		return "period_limited", "current period credit limit is exhausted", false, grantPeriodRetryAt(g, now)
	}
	return "active", "grant is active", true, nil
}

func grantIsValidAt(g Grant, now time.Time) bool {
	return g.Permanent || g.ExpiresAt.After(now)
}

// effectiveGrantForRegistry projects a welcome limit-card qualification into
// the tenant's current policy. The returned value is intentionally a copy:
// policy fields must never be written back to the user grant. Welcome cards
// are valid overlays for both free and grant-required service groups; this is
// what lets an administrator backfill an existing recharge-card user without
// hiding the new benefit.
func effectiveGrantForRegistry(reg *Registry, grant Grant) Grant {
	if strings.TrimSpace(grant.BillingTimezone) == "" && reg != nil {
		grant.BillingTimezone = reg.UserBillingTimezones[normalizeEmail(grant.Email)]
	}
	grant.BillingTimezone = normalizeBillingTimezone(grant.BillingTimezone)
	if !isNewUserLimitCardSource(grant.Source) {
		return grant
	}
	// The configured group list is part of the shared qualification policy.
	// Once a group is removed, a historical qualification must not fall back to
	// legacy fields embedded in its Grant record.
	if reg == nil || !isActiveNewUserLimitCardPolicyGroup(reg, grant.ServiceGroupID) {
		grant.ExpiresAt = time.Time{}
		grant.Permanent = false
		grant.PeriodLimits = CreditPeriodLimits{}
		grant.RollingFiveHour = false
		grant.AnchoredFiveHour = false
		return grant
	}
	policy := reg.DefaultNewUserLimitCard
	// Only these two limits are configurable for the welcome qualification.
	// Keep the projection defensive even for callers that construct Registry
	// directly instead of going through Normalize.
	grant.PeriodLimits = CreditPeriodLimits{FiveHour: policy.PeriodLimits.FiveHour, Daily: policy.PeriodLimits.Daily}
	grant.RollingFiveHour = false
	grant.AnchoredFiveHour = true
	grant.Permanent = policy.DurationDays <= 0
	if grant.Permanent {
		grant.ExpiresAt = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
	} else {
		grant.ExpiresAt = grant.StartsAt.UTC().AddDate(0, 0, policy.DurationDays)
	}
	return grant
}

func isActiveNewUserLimitCardPolicyGroup(reg *Registry, serviceGroupID string) bool {
	if reg == nil || !containsNormalizedString(reg.DefaultNewUserLimitCard.ServiceGroupIDs, serviceGroupID) {
		return false
	}
	// The service group must still exist, but its access policy is deliberately
	// not restricted here. Grant-required groups (for example, redeem/recharge)
	// are a supported target for manually issued welcome cards.
	return reg.FindModelServiceGroup(serviceGroupID) != nil
}

// applyGrantPeriodUsageForRegistry persists only usage state from the
// effective view. It deliberately leaves policy-derived fields absent from the
// stored qualification.
func applyGrantPeriodUsageForRegistry(reg *Registry, grant *Grant, credits float64, now time.Time) {
	if grant == nil {
		return
	}
	effective := effectiveGrantForRegistry(reg, *grant)
	applyGrantPeriodUsage(&effective, credits, now)
	grant.PeriodUsage = effective.PeriodUsage
	grant.UsageEvents = effective.UsageEvents
	grant.BillingTimezone = effective.BillingTimezone
}

// SetUserBillingTimezone records an account's initial IANA timezone. This is
// intentionally write-once: accepting a new client value on every request
// would let a user move a calendar quota reset forwards.
func SetUserBillingTimezone(ctx context.Context, system SystemSettingsRepository, email, timezone string) (string, error) {
	email = normalizeEmail(email)
	timezone = normalizeBillingTimezone(timezone)
	if email == "" || timezone == "" {
		return "", nil
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return "", err
	}
	if reg.UserBillingTimezones == nil {
		reg.UserBillingTimezones = map[string]string{}
	}
	if existing := normalizeBillingTimezone(reg.UserBillingTimezones[email]); existing != "" {
		return existing, nil
	}
	reg.UserBillingTimezones[email] = timezone
	if err := SaveRegistry(ctx, system, reg); err != nil {
		return "", err
	}
	return timezone, nil
}

// normalizeBillingTimezone accepts only canonical IANA location names. Fixed
// offsets and the host-dependent "Local" label are deliberately rejected so
// daylight-saving changes are calculated by the server from its timezone DB.
func normalizeBillingTimezone(value string) string {
	value = strings.TrimSpace(value)
	// Intl.DateTimeFormat reports "UTC" on some platforms. It is the only
	// slash-less zone we accept: unlike an arbitrary offset it remains a stable
	// server-side tzdata location.
	switch value {
	case "UTC", "Etc/UTC", "Etc/GMT", "Etc/UCT", "Etc/Universal", "Etc/Zulu":
		return "UTC"
	}
	if strings.HasPrefix(value, "Etc/GMT") {
		return ""
	}
	if value == "" || !strings.Contains(value, "/") {
		return ""
	}
	location, err := time.LoadLocation(value)
	if err != nil || location == nil || location.String() != value {
		return ""
	}
	return value
}

// NormalizeBillingTimezone validates an IANA timezone supplied by an
// authenticated client. It is exported for transport handlers only; billing
// always reads the persisted value.
func NormalizeBillingTimezone(value string) string {
	return normalizeBillingTimezone(value)
}

func grantBillingLocation(grant Grant) *time.Location {
	if timezone := normalizeBillingTimezone(grant.BillingTimezone); timezone != "" {
		if location, err := time.LoadLocation(timezone); err == nil {
			return location
		}
	}
	return time.UTC
}

func calendarDayWindowStart(t time.Time, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	local := t.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func grantDayWindowStart(grant Grant, now time.Time) time.Time {
	return calendarDayWindowStart(now, grantBillingLocation(grant))
}

func grantWeekWindowStart(grant Grant, now time.Time) time.Time {
	start := grantDayWindowStart(grant, now)
	offset := (int(start.Weekday()) + 6) % 7
	return start.AddDate(0, 0, -offset)
}

func grantMonthWindowStart(grant Grant, now time.Time) time.Time {
	location := grantBillingLocation(grant)
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
}

func RedeemCard(ctx context.Context, system SystemSettingsRepository, securitySvc *security.SecurityService, email, code, hubBaseURL string) (*ServiceStatus, error) {
	return RedeemCardForUserID(ctx, system, securitySvc, "", email, code, hubBaseURL)
}

func RedeemCardForUserID(ctx context.Context, system SystemSettingsRepository, securitySvc *security.SecurityService, userID, email, code, hubBaseURL string) (*ServiceStatus, error) {
	owner := newUserAccountRef(userID, email)
	accountEmail := owner.Email
	code = NormalizeCardCode(code)
	if owner.empty() {
		return nil, fmt.Errorf("user is required")
	}
	if code == "" {
		return nil, fmt.Errorf("redeem code is required")
	}
	if err := ValidateCardCode(code); err != nil {
		return nil, err
	}

	// Serialize card redemption to prevent TOCTOU race:
	// Without this lock, two concurrent requests with the same card code could
	// both pass the RedeemedAt==nil check and create duplicate grants.
	cardRedeemMu.Lock()
	defer cardRedeemMu.Unlock()

	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return nil, err
	}
	// Promote historical metered grants still queued under the old policy so a
	// fresh top-up is not stacked after an obsolete queue window. Best-effort
	// persist so a failed redeem code still repairs ownership timeline.
	if n := PromoteQueuedMeteredGrants(reg, time.Now().UTC()); n > 0 {
		_ = SaveRegistry(ctx, system, reg)
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
		// Metered top-ups (credits > 0) always start immediately so purchased
		// point cards increase available balance right away. Duration-only /
		// unmetered cards still queue after active grants to extend the window.
		startsAt := now
		if creditsPerGroup <= 0 {
			startsAt = nextGrantStart(reg, owner, serviceGroupID, now)
		}
		expiresAt := startsAt.Add(time.Duration(days) * 24 * time.Hour)
		reg.Grants = append(reg.Grants, Grant{
			ID:             NewID("grant"),
			UserID:         owner.UserID,
			Email:          accountEmail,
			ServiceGroupID: serviceGroupID,
			Source:         "card",
			CardID:         card.ID,
			StartsAt:       startsAt,
			ExpiresAt:      expiresAt,
			CreatedAt:      now,
			CreditsTotal:   creditsPerGroup,
			PeriodLimits:   card.PeriodLimits,
		})
	}
	reg.Cards[idx].RedeemedByUserID = owner.UserID
	reg.Cards[idx].RedeemedByEmail = accountEmail
	reg.Cards[idx].RedeemedAt = &now
	if err := SaveRegistry(ctx, system, reg); err != nil {
		return nil, err
	}
	status, _, err := ResolveStatusFromRegistryForUser(ctx, reg, securitySvc, owner.UserID, owner.Email, hubBaseURL)
	if err != nil {
		return nil, err
	}
	return status, nil
}

// PromoteQueuedMeteredGrants starts historical metered grants that are still
// queued (StartsAt in the future) because the old redeem policy stacked point
// cards after an active grant. Only rewrites grants that share an owner+service
// group with another grant already inside its validity window; a sole scheduled
// future grant is left alone so LLM_SERVICE_GRANT_QUEUED remains meaningful.
// Unmetered / duration-only grants (CreditsTotal <= 0) keep serial stacking.
// Returns the number of grants rewritten.
func PromoteQueuedMeteredGrants(reg *Registry, now time.Time) int {
	if reg == nil {
		return 0
	}
	now = now.UTC()
	changed := 0
	for i := range reg.Grants {
		g := &reg.Grants[i]
		if g.Frozen {
			continue
		}
		if !g.StartsAt.After(now) {
			continue
		}
		if g.CreditsTotal <= 0 {
			// Duration-only / unlimited grants still queue for serial validity windows.
			continue
		}
		if remainingGrantCredits(*g) <= 0 {
			continue
		}
		owner := newUserAccountRef(g.UserID, g.Email)
		if !hasActiveWindowGrantForOwnerGroup(reg, owner, g.ServiceGroupID, now, i) {
			// Sole/scheduled future metered grants are intentional (admin gift, delayed start).
			continue
		}
		duration := g.ExpiresAt.Sub(g.StartsAt)
		if duration <= 0 {
			duration = 30 * 24 * time.Hour
		}
		g.StartsAt = now
		g.ExpiresAt = now.Add(duration)
		changed++
	}
	return changed
}

// hasActiveWindowGrantForOwnerGroup reports whether the owner already has another
// grant for the service group whose validity window covers now (active, exhausted,
// or period-limited — anything with StartsAt <= now < ExpiresAt).
func hasActiveWindowGrantForOwnerGroup(reg *Registry, owner userAccountRef, serviceGroupID string, now time.Time, skipIndex int) bool {
	if reg == nil || owner.empty() {
		return false
	}
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if serviceGroupID == "" {
		return false
	}
	for i, grant := range reg.Grants {
		if i == skipIndex {
			continue
		}
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), serviceGroupID) {
			continue
		}
		if now.Before(grant.StartsAt) || !grantIsValidAt(grant, now) {
			continue
		}
		return true
	}
	return false
}

func nextGrantStart(reg *Registry, owner userAccountRef, serviceGroupID string, now time.Time) time.Time {
	// Scan currently-active grants (within their validity window right now)
	// to determine whether the user has any remaining credits. If all active
	// grants are exhausted, the new grant starts immediately so the top-up is
	// usable right away.
	hasActiveWithCredits := false
	latestActiveExpiry := now
	for _, g := range reg.Grants {
		g = effectiveGrantForRegistry(reg, g)
		if !grantMatchesUser(g, owner) || !strings.EqualFold(g.ServiceGroupID, serviceGroupID) {
			continue
		}
		if g.Frozen {
			continue
		}
		// Only consider grants currently within their validity window.
		if now.Before(g.StartsAt) || !grantIsValidAt(g, now) {
			continue
		}
		// Track the latest expiry among active grants for queuing.
		if g.ExpiresAt.After(latestActiveExpiry) {
			latestActiveExpiry = g.ExpiresAt
		}
		if !hasActiveWithCredits {
			if g.CreditsTotal <= 0 {
				// Unlimited grant: still active.
				hasActiveWithCredits = true
			} else if remainingGrantCredits(g) > 0 {
				hasActiveWithCredits = true
			}
		}
	}
	if !hasActiveWithCredits {
		// All current grants are exhausted (or none exist): start immediately.
		return now
	}
	// There's still an active grant with remaining credits. Queue the new grant
	// after the latest active grant's expiry to avoid overlap.
	return latestActiveExpiry
}

func effectiveGrantExpiresAt(reg *Registry, owner userAccountRef, now time.Time) *time.Time {
	if reg == nil {
		return nil
	}
	var latest *time.Time
	for _, g := range reg.Grants {
		g = effectiveGrantForRegistry(reg, g)
		if !grantMatchesUser(g, owner) {
			continue
		}
		if g.Frozen {
			continue
		}
		if reg.FindModelServiceGroup(g.ServiceGroupID) == nil {
			continue
		}
		if g.CreditsTotal > 0 && remainingGrantCredits(g) <= 0 {
			continue
		}
		// A usable permanent grant makes the entitlement long-term. Its sentinel
		// expiry must never override the UI with a literal 9999 date.
		if g.Permanent {
			return nil
		}
		if !grantIsValidAt(g, now) {
			continue
		}
		if latest == nil || g.ExpiresAt.After(*latest) {
			copyVal := g.ExpiresAt
			latest = &copyVal
		}
	}
	return latest
}

func hasGrantWithSource(reg *Registry, email, serviceGroupID, source string) bool {
	return findGrantWithSource(reg, newUserAccountRef("", email), serviceGroupID, source) != nil
}

func findGrantWithSource(reg *Registry, owner userAccountRef, serviceGroupID, source string) *Grant {
	if reg == nil {
		return nil
	}
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	source = strings.TrimSpace(source)
	for i := range reg.Grants {
		grant := &reg.Grants[i]
		if !grantMatchesUser(*grant, owner) {
			continue
		}
		if grant.Frozen {
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

func effectiveServiceGroupIDs(ctx context.Context, reg *Registry, securitySvc userGroupResolver, email string, now time.Time) ([]string, []Grant, error) {
	return effectiveServiceGroupIDsForOwner(ctx, reg, securitySvc, newUserAccountRef("", email), now)
}

func effectiveServiceGroupIDsForOwner(ctx context.Context, reg *Registry, securitySvc userGroupResolver, owner userAccountRef, now time.Time) ([]string, []Grant, error) {
	serviceGroupIDs := make([]string, 0)
	seen := map[string]struct{}{}
	appendID := func(id string) bool {
		id = strings.TrimSpace(id)
		if id == "" {
			return false
		}
		if reg.FindModelServiceGroup(id) == nil {
			return false
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
		serviceGroupIDs = append(serviceGroupIDs, id)
		return true
	}
	appendIDs := func(ids []string) bool {
		added := false
		for _, id := range ids {
			if appendID(id) {
				added = true
			}
		}
		return added
	}
	policyApplied := false
	for _, ub := range reg.UserBindings {
		if !bindingMatchesUser(ub, owner) {
			continue
		}
		if appendIDs(ub.ServiceGroupIDs) {
			policyApplied = true
		}
	}
	if !policyApplied && securitySvc != nil && owner.Email != "" {
		groupIDs, err := securitySvc.ResolveUserGroupChain(ctx, owner.Email)
		if err != nil {
			return nil, nil, err
		}
		bindingsByGroup := map[string][]string{}
		for _, binding := range reg.GroupBindings {
			groupID := strings.ToLower(strings.TrimSpace(binding.GroupID))
			if groupID == "" {
				continue
			}
			bindingsByGroup[groupID] = append(bindingsByGroup[groupID], binding.ServiceGroupIDs...)
		}
		for _, groupID := range groupIDs {
			if appendIDs(bindingsByGroup[strings.ToLower(strings.TrimSpace(groupID))]) {
				policyApplied = true
				break
			}
		}
	}
	if !policyApplied && appendIDs(reg.GlobalServiceGroupIDs) {
		policyApplied = true
	}
	if !policyApplied {
		appendIDs(reg.DefaultNewUserServiceGroups)
	}
	activeGrants := make([]Grant, 0)
	for _, g := range reg.Grants {
		if !grantMatchesUser(g, owner) {
			continue
		}
		if g.Frozen {
			continue
		}
		effective := effectiveGrantForRegistry(reg, g)
		if !grantIsValidAt(effective, now) {
			continue
		}
		appendID(effective.ServiceGroupID)
		if now.Before(effective.StartsAt) {
			continue
		}
		if status, _, active, _ := grantStatus(effective, now); !active || status != "active" {
			continue
		}
		activeGrants = append(activeGrants, effective)
	}
	return serviceGroupIDs, activeGrants, nil
}

func hasEligibleAuthorizedModel(reg *Registry, owner userAccountRef, models []AuthorizedModel, now time.Time) bool {
	if len(models) == 0 {
		return false
	}
	for i := range models {
		model := &models[i]
		providerIDs := model.ProviderIDs
		if len(providerIDs) == 0 {
			providerIDs = keysOfProviderServiceGroups(model.ProviderServiceGroups)
		}
		for _, providerID := range providerIDs {
			allowed, _, _, _, _, _, _ := billingEligibilityForServiceGroups(reg, owner, ServiceGroupIDsForProvider(model, providerID), now)
			if allowed {
				return true
			}
		}
	}
	return false
}

func keysOfProviderServiceGroups(items map[string][]string) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
					Name:                          model.Name,
					Kind:                          llmpool.NormalizeServiceGroupKind(group.Kind),
					CapabilityTags:                append([]string(nil), model.CapabilityTags...),
					Priority:                      model.Priority,
					ResolutionTier:                model.ResolutionTier,
					CreditMultiplier:              normalizeCreditMultiplier(model.CreditMultiplier),
					ProviderCapabilityTags:        map[string][]string{},
					ProviderPriorities:            map[string]int{},
					ProviderResolutionTiers:       map[string]int{},
					ProviderServiceGroups:         map[string][]string{},
					ProviderCreditMultipliers:     map[string]float64{},
					ProviderUpstreamModels:        map[string]string{},
					ProviderUpstreamRouteModels:   map[string]map[string]string{},
					ProviderServiceGroupUpstreams: map[string]map[string]string{},
					ProviderBillingModes:          map[string]string{},
					ProviderTokenPricing:          map[string]llmpool.TokenPricing{},
					ProviderRouteBilling:          map[string]map[string]ProviderRouteBilling{},
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
				if models[idx].ProviderUpstreamModels == nil {
					models[idx].ProviderUpstreamModels = map[string]string{}
				}
				if upstream := strings.TrimSpace(cfg.Model); upstream != "" {
					models[idx].ProviderUpstreamModels[key] = upstream
				}
				if models[idx].ProviderUpstreamRouteModels == nil {
					models[idx].ProviderUpstreamRouteModels = map[string]map[string]string{}
				}
				if models[idx].ProviderUpstreamRouteModels[key] == nil {
					models[idx].ProviderUpstreamRouteModels[key] = map[string]string{}
				}
				models[idx].ProviderUpstreamRouteModels[key][strings.ToLower(strings.TrimSpace(model.Name))] = strings.TrimSpace(cfg.Model)
				if models[idx].ProviderServiceGroupUpstreams == nil {
					models[idx].ProviderServiceGroupUpstreams = map[string]map[string]string{}
				}
				if models[idx].ProviderServiceGroupUpstreams[key] == nil {
					models[idx].ProviderServiceGroupUpstreams[key] = map[string]string{}
				}
				models[idx].ProviderServiceGroupUpstreams[key][strings.ToLower(strings.TrimSpace(serviceGroupID))] = strings.TrimSpace(cfg.Model)
				if models[idx].ProviderRouteBilling == nil {
					models[idx].ProviderRouteBilling = map[string]map[string]ProviderRouteBilling{}
				}
				if models[idx].ProviderRouteBilling[key] == nil {
					models[idx].ProviderRouteBilling[key] = map[string]ProviderRouteBilling{}
				}
				routeKey := normalizedUpstreamModelKey(cfg.Model, model.Name)
				models[idx].ProviderRouteBilling[key][routeKey] = ProviderRouteBilling{
					BillingMode:          llmpool.NormalizeBillingMode(cfg.BillingMode),
					TokenPricingOverride: cfg.TokenPricingOverride,
					TokenPricing:         cfg.TokenPricing,
				}
				if mode := llmpool.NormalizeBillingMode(cfg.BillingMode); mode != "" {
					models[idx].ProviderBillingModes[key] = mode
				}
				if llmpool.NormalizeBillingMode(cfg.BillingMode) != llmpool.BillingModeFree && cfg.TokenPricing.HasCreditPricing() {
					models[idx].ProviderTokenPricing[key] = cfg.TokenPricing
				}
				if group.IsDynamic() {
					models[idx].Kind = llmpool.ServiceGroupKindDynamic
				}
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

// BuildAuthorizedModelsForServiceGroups returns the API-facing model summaries
// for service groups using the same merge and provider metadata rules as
// ResolveStatusFromRegistry.
func BuildAuthorizedModelsForServiceGroups(reg *Registry, serviceGroupIDs []string) ([]AuthorizedModel, string) {
	return buildAuthorizedModels(reg, serviceGroupIDs)
}

func normalizedProviderKey(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func normalizedUpstreamModelKey(upstreamModel, logicalModel string) string {
	if key := strings.ToLower(strings.TrimSpace(upstreamModel)); key != "" {
		return key
	}
	return strings.ToLower(strings.TrimSpace(logicalModel))
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

var requestProviderWRR = llmpool.NewWRRScheduler()

func ResetRequestProviderWRR() {
	requestProviderWRR.Reset()
}

func OrderProvidersForRequest(body map[string]any, model *AuthorizedModel) []string {
	return providerIDsFromBalancedRoutes(orderProvidersForRequest(body, model, nil, time.Time{}, true, ""))
}

func PeekProvidersForRequest(body map[string]any, model *AuthorizedModel) []string {
	return providerIDsFromBalancedRoutes(orderProvidersForRequest(body, model, nil, time.Time{}, false, ""))
}

func OrderProvidersForRequestWithMeta(body map[string]any, model *AuthorizedModel, metas map[string]llmpool.ProviderDispatchMeta, now time.Time) []llmpool.BalancedRoute {
	return orderProvidersForRequest(body, model, metas, now, true, "")
}

func OrderProvidersForRequestWithMetaInPool(body map[string]any, model *AuthorizedModel, metas map[string]llmpool.ProviderDispatchMeta, now time.Time, pool string) []llmpool.BalancedRoute {
	return orderProvidersForRequest(body, model, metas, now, true, pool)
}

func orderProvidersForRequest(body map[string]any, model *AuthorizedModel, metas map[string]llmpool.ProviderDispatchMeta, now time.Time, rotate bool, pool string) []llmpool.BalancedRoute {
	if model == nil || len(model.ProviderIDs) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	scored := llmpool.OrderScoredProviderRoutes(body, dispatchModelFromAuthorized(model))
	candidates := make([]llmpool.BalanceCandidate, 0, len(scored))
	for _, item := range scored {
		providerID := item.Route.ProviderID
		meta, ok := metas[normalizedProviderKey(providerID)]
		if !ok {
			meta = llmpool.ProviderDispatchMeta{ID: providerID, Sequence: item.Route.OriginalIndex + 1}
		}
		if meta.Sequence <= 0 {
			meta.Sequence = item.Route.OriginalIndex + 1
		}
		candidates = append(candidates, llmpool.BalanceCandidate{
			Route:               item.Route,
			Score:               item.Score,
			ResolutionTier:      item.ResolutionTier,
			EffectiveMultiplier: llmpool.EffectiveRouteMultiplier(meta, item.Route, now),
			Sequence:            meta.Sequence,
			MaxConcurrency:      meta.MaxConcurrency,
			SkipWRR:             meta.SkipWRR,
		})
	}
	var sched *llmpool.WRRScheduler
	if rotate {
		sched = requestProviderWRR
	}
	if strings.TrimSpace(pool) == "" && model != nil {
		pool = strings.TrimSpace(model.Name)
	}
	return llmpool.BalanceProviderRoutes(sched, pool, candidates)
}

func dispatchModelFromAuthorized(model *AuthorizedModel) *llmpool.DispatchModel {
	if model == nil {
		return nil
	}
	dm := &llmpool.DispatchModel{
		Name:                      model.Name,
		CapabilityTags:            append([]string(nil), model.CapabilityTags...),
		Priority:                  model.Priority,
		ResolutionTier:            model.ResolutionTier,
		CreditMultiplier:          model.CreditMultiplier,
		ProviderCapabilityTags:    map[string][]string{},
		ProviderPriorities:        map[string]int{},
		ProviderResolutionTiers:   map[string]int{},
		ProviderCreditMultipliers: map[string]float64{},
	}
	for _, id := range model.ProviderIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		dm.ProviderIDs = append(dm.ProviderIDs, id)
		if tags := CapabilityTagsForProvider(model, id); len(tags) > 0 {
			dm.ProviderCapabilityTags[id] = tags
		}
		dm.ProviderPriorities[id] = PriorityForProvider(model, id)
		if tier := ResolutionTierForProvider(model, id); tier != 0 {
			dm.ProviderResolutionTiers[id] = tier
		}
		dm.ProviderCreditMultipliers[id] = CreditMultiplierForProvider(model, id)
	}
	return dm
}

func providerIDsFromBalancedRoutes(routes []llmpool.BalancedRoute) []string {
	if len(routes) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(routes))
	for _, item := range routes {
		ordered = append(ordered, item.Route.ProviderID)
	}
	return ordered
}

func normalizeCreditMultiplier(v float64) float64 {
	return llmpool.NormalizeCreditMultiplier(v)
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
	return GrantDefaultServiceForNewUserID(ctx, system, "", email)
}

func GrantDefaultServiceForNewUserID(ctx context.Context, system SystemSettingsRepository, userID, email string) error {
	if IsSystemLLMUser(userID, email) {
		return nil
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return err
	}
	if reg.NewUserBenefitMode() == NewUserBenefitModeLimitCard {
		return grantNewUserLimitCardForRegistry(ctx, system, reg, userID, email)
	}
	return grantNewUserBenefitForRegistry(ctx, system, reg, userID, email, "new_user_default", 0.30, false)
}

// grantNewUserLimitCardForUserID issues the automatic welcome-rate-limit
// entitlement at registration. Existing accounts are backfilled by
// EnsureNewUserLimitCardForUserID when they load service status.
func grantNewUserLimitCardForUserID(ctx context.Context, system SystemSettingsRepository, userID, email string) error {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return err
	}
	return grantNewUserLimitCardForRegistry(ctx, system, reg, userID, email)
}

func grantNewUserLimitCardForRegistry(ctx context.Context, system SystemSettingsRepository, reg *Registry, userID, email string) error {
	owner := newUserAccountRef(userID, email)
	if owner.empty() {
		return fmt.Errorf("email is required")
	}
	_, err := ensureNewUserLimitCardForRegistry(ctx, system, reg, userID, email)
	return err
}

// EnsureNewUserLimitCardForUserID backfills the configured welcome limit card for
// an existing account. It is idempotent: users who already hold a live card are
// left unchanged. Status/account reads use this so recharge-card holders see the
// overlay without a separate admin issuance.
func EnsureNewUserLimitCardForUserID(ctx context.Context, system SystemSettingsRepository, userID, email string) (bool, error) {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return false, err
	}
	return ensureNewUserLimitCardForRegistry(ctx, system, reg, userID, email)
}

func ensureNewUserLimitCardForRegistry(ctx context.Context, system SystemSettingsRepository, reg *Registry, userID, email string) (bool, error) {
	if IsSystemLLMUser(userID, email) {
		return false, nil
	}
	if reg == nil || reg.NewUserBenefitMode() != NewUserBenefitModeLimitCard {
		return false, nil
	}
	owner := newUserAccountRef(userID, email)
	if owner.empty() {
		return false, nil
	}
	if IssueNewUserLimitCards(reg, []VoucherUser{{ID: owner.UserID, Email: owner.Email}}, time.Now().UTC()) == 0 {
		return false, nil
	}
	if system == nil {
		return true, nil
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		return false, err
	}
	return true, nil
}

// IssueNewUserLimitCards grants the currently configured welcome limit-card
// qualification to each unique target user. Existing qualifications are
// preserved so repeated admin issuance is idempotent.
//
// Purchased credits cards, historical Credits gifts, and expired or frozen
// limit cards must not block the overlay. Administrators backfill recharge-card
// holders so a live welcome card remains visible beside their existing balance.
// Only a still-valid limit card on the same policy group is a duplicate.
func IssueNewUserLimitCards(reg *Registry, users []VoucherUser, now time.Time) int {
	if reg == nil {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	policyGroups := normalizeStringSlice(reg.DefaultNewUserLimitCard.ServiceGroupIDs)
	if len(policyGroups) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(users))
	cardKeys := newLiveNewUserLimitCardIndex(reg, now)
	issued := 0
	for _, user := range users {
		owner := newUserAccountRef(user.ID, user.Email)
		if owner.empty() || IsSystemLLMUser(owner.UserID, owner.Email) {
			continue
		}
		key := voucherOwnerKey(owner)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		userIssued := false
		for _, serviceGroupID := range policyGroups {
			if !isActiveNewUserLimitCardPolicyGroup(reg, serviceGroupID) || ownerHasNewUserLimitCardKey(cardKeys.group, owner, serviceGroupID) {
				continue
			}
			pruneStaleNewUserLimitCardsForOwnerGroup(reg, owner, serviceGroupID, now)
			reg.Grants = append(reg.Grants, Grant{ID: NewID("grant"), UserID: owner.UserID, Email: owner.Email, ServiceGroupID: serviceGroupID, Source: "new_user_limit_card", StartsAt: now, CreatedAt: now})
			recordNewUserLimitCardKeys(cardKeys.group, owner.UserID, owner.Email, serviceGroupID)
			recordLiveNewUserLimitCardOwner(cardKeys.owner, owner.UserID, owner.Email)
			userIssued = true
		}
		if userIssued {
			issued++
		}
	}
	return issued
}

func isLiveNewUserLimitCard(reg *Registry, grant Grant, now time.Time) bool {
	if grant.Frozen || !isNewUserLimitCardSource(grant.Source) {
		return false
	}
	if !isActiveNewUserLimitCardPolicyGroup(reg, grant.ServiceGroupID) {
		return false
	}
	return grantIsValidAt(effectiveGrantForRegistry(reg, grant), now)
}

func newUserLimitCardOwnerGroupKeys(userID, email, serviceGroupID string) []string {
	groupKey := strings.ToLower(strings.TrimSpace(serviceGroupID))
	keys := make([]string, 0, 2)
	if id := normalizeUserID(userID); id != "" {
		keys = append(keys, "id:"+id+"\x00"+groupKey)
	}
	if em := normalizeEmail(email); em != "" {
		keys = append(keys, "email:"+em+"\x00"+groupKey)
	}
	return keys
}

func recordNewUserLimitCardKeys(dst map[string]struct{}, userID, email, serviceGroupID string) {
	if dst == nil {
		return
	}
	for _, key := range newUserLimitCardOwnerGroupKeys(userID, email, serviceGroupID) {
		dst[key] = struct{}{}
	}
}

func ownerHasNewUserLimitCardKey(keys map[string]struct{}, owner userAccountRef, serviceGroupID string) bool {
	for _, key := range newUserLimitCardOwnerGroupKeys(owner.UserID, owner.Email, serviceGroupID) {
		if _, ok := keys[key]; ok {
			return true
		}
	}
	return false
}

type liveNewUserLimitCardIndex struct {
	group map[string]struct{}
	owner map[string]struct{}
}

func newLiveNewUserLimitCardIndex(reg *Registry, now time.Time) liveNewUserLimitCardIndex {
	idx := liveNewUserLimitCardIndex{group: make(map[string]struct{}), owner: make(map[string]struct{})}
	if reg == nil {
		return idx
	}
	for _, grant := range reg.Grants {
		if !isLiveNewUserLimitCard(reg, grant, now) {
			continue
		}
		recordNewUserLimitCardKeys(idx.group, grant.UserID, grant.Email, grant.ServiceGroupID)
		recordLiveNewUserLimitCardOwner(idx.owner, grant.UserID, grant.Email)
	}
	return idx
}

func liveNewUserLimitCardOwnerKeys(reg *Registry, now time.Time) map[string]struct{} {
	keys := make(map[string]struct{})
	if reg == nil {
		return keys
	}
	for _, grant := range reg.Grants {
		if !isLiveNewUserLimitCard(reg, grant, now) {
			continue
		}
		recordLiveNewUserLimitCardOwner(keys, grant.UserID, grant.Email)
	}
	return keys
}

func recordLiveNewUserLimitCardOwner(dst map[string]struct{}, userID, email string) {
	if dst == nil {
		return
	}
	if id := normalizeUserID(userID); id != "" {
		dst["id:"+id] = struct{}{}
	}
	if em := normalizeEmail(email); em != "" {
		dst["email:"+em] = struct{}{}
	}
}

func HasLiveNewUserLimitCard(reg *Registry, userID, email string) bool {
	owner := newUserAccountRef(userID, email)
	if owner.empty() {
		return false
	}
	return ownerHasLiveNewUserLimitCard(liveNewUserLimitCardOwnerKeys(reg, time.Now().UTC()), owner)
}

func hasActiveNewUserLimitCardPolicy(reg *Registry) bool {
	if reg == nil {
		return false
	}
	for _, id := range normalizeStringSlice(reg.DefaultNewUserLimitCard.ServiceGroupIDs) {
		if isActiveNewUserLimitCardPolicyGroup(reg, id) {
			return true
		}
	}
	return false
}

func NeedsNewUserLimitCardBackfill(reg *Registry, userID, email string) bool {
	if IsSystemLLMUser(userID, email) {
		return false
	}
	if reg == nil || reg.NewUserBenefitMode() != NewUserBenefitModeLimitCard || !hasActiveNewUserLimitCardPolicy(reg) {
		return false
	}
	if newUserAccountRef(userID, email).empty() {
		return false
	}
	return !HasLiveNewUserLimitCard(reg, userID, email)
}

func ownerHasLiveNewUserLimitCard(ownerKeys map[string]struct{}, owner userAccountRef) bool {
	if owner.UserID != "" {
		if _, ok := ownerKeys["id:"+owner.UserID]; ok {
			return true
		}
	}
	if owner.Email != "" {
		if _, ok := ownerKeys["email:"+owner.Email]; ok {
			return true
		}
	}
	return false
}

func liveResetVouchersForOwner(reg *Registry, owner userAccountRef, now time.Time) []ResetVoucher {
	if reg == nil || len(reg.ResetVouchers) == 0 {
		return nil
	}
	if !ownerHasLiveNewUserLimitCard(liveNewUserLimitCardOwnerKeys(reg, now), owner) {
		return nil
	}
	var out []ResetVoucher
	for _, voucher := range reg.ResetVouchers {
		if voucher.RedeemedAt != nil || !resetVoucherMatchesOwner(voucher, owner) {
			continue
		}
		if !voucher.ExpiresAt.IsZero() && !voucher.ExpiresAt.After(now) {
			continue
		}
		out = append(out, voucher)
	}
	return out
}

func pruneStaleNewUserLimitCardsForOwnerGroup(reg *Registry, owner userAccountRef, serviceGroupID string, now time.Time) {
	if reg == nil || owner.empty() || strings.TrimSpace(serviceGroupID) == "" {
		return
	}
	filtered := reg.Grants[:0]
	for _, grant := range reg.Grants {
		if isNewUserLimitCardSource(grant.Source) && grantMatchesUser(grant, owner) && strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), serviceGroupID) && !isLiveNewUserLimitCard(reg, grant, now) {
			continue
		}
		filtered = append(filtered, grant)
	}
	reg.Grants = filtered
}

// RevokeNewUserLimitCards removes welcome limit-card qualifications for each
// target user. Historical non-welcome grants are left untouched.
func RevokeNewUserLimitCards(reg *Registry, users []VoucherUser) int {
	if reg == nil || len(reg.Grants) == 0 {
		return 0
	}
	targets := make(map[string]struct{}, len(users))
	for _, user := range users {
		owner := newUserAccountRef(user.ID, user.Email)
		if owner.empty() {
			continue
		}
		if owner.UserID != "" {
			targets["id:"+owner.UserID] = struct{}{}
		}
		if owner.Email != "" {
			targets["email:"+owner.Email] = struct{}{}
		}
	}
	if len(targets) == 0 {
		return 0
	}
	filtered := reg.Grants[:0]
	revoked := 0
	revokedUsers := make(map[string]struct{})
	for _, grant := range reg.Grants {
		if !isNewUserLimitCardSource(grant.Source) {
			filtered = append(filtered, grant)
			continue
		}
		matched := false
		if id := normalizeUserID(grant.UserID); id != "" {
			_, matched = targets["id:"+id]
		}
		if !matched {
			if email := normalizeEmail(grant.Email); email != "" {
				_, matched = targets["email:"+email]
			}
		}
		if matched {
			userKey := normalizeUserID(grant.UserID)
			if userKey == "" {
				userKey = "email:" + normalizeEmail(grant.Email)
			} else {
				userKey = "id:" + userKey
			}
			if _, seen := revokedUsers[userKey]; !seen {
				revokedUsers[userKey] = struct{}{}
				revoked++
			}
			continue
		}
		filtered = append(filtered, grant)
	}
	reg.Grants = filtered
	return revoked
}

func GrantEmailConfirmedBenefitForUser(ctx context.Context, system SystemSettingsRepository, email string) error {
	return GrantEmailConfirmedBenefitForUserID(ctx, system, "", email)
}

func GrantEmailConfirmedBenefitForUserID(ctx context.Context, system SystemSettingsRepository, userID, email string) error {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return err
	}
	if reg.NewUserBenefitMode() != NewUserBenefitModeCredits {
		return nil
	}
	return grantNewUserBenefitForRegistry(ctx, system, reg, userID, email, "new_user_email_confirmed", 0.70, true)
}

func GrantPhoneVerifiedBenefitForUser(ctx context.Context, system SystemSettingsRepository, email string) error {
	return GrantPhoneVerifiedBenefitForUserID(ctx, system, "", email)
}

func GrantPhoneVerifiedBenefitForUserID(ctx context.Context, system SystemSettingsRepository, userID, email string) error {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return err
	}
	if reg.NewUserBenefitMode() != NewUserBenefitModeCredits {
		return nil
	}
	return grantNewUserBenefitForRegistry(ctx, system, reg, userID, email, "new_user_phone_verified", 0.70, true)
}

// ResetNewUserLimitCardUsage clears the current rate-limit usage for every
// new-user limit-card grant. The configured policy remains unchanged; the next
// eligibility check therefore sees the full five-hour and daily allowances.
// It returns the number of grants whose usage state was changed.
func ResetNewUserLimitCardUsage(reg *Registry, now time.Time) int {
	if reg == nil {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	changed := 0
	for i := range reg.Grants {
		grant := &reg.Grants[i]
		if !isNewUserLimitCardSource(grant.Source) {
			continue
		}
		if grant.CreditsUsed == 0 && len(grant.UsageEvents) == 0 && grant.PeriodUsage == (CreditPeriodUsage{}) {
			continue
		}
		resetNewUserLimitCardUsageForGrant(reg, grant, now)
		changed++
	}
	return changed
}

type VoucherUser struct{ ID, Email string }

// IssueResetVouchers appends one voucher per unique target user who currently
// holds a live new-user limit card. Missing, expired, or frozen qualifications
// are skipped. Repeated issuances create additional one-time vouchers.
func IssueResetVouchers(reg *Registry, users []VoucherUser, expiresDays int, now time.Time) int {
	if reg == nil || expiresDays <= 0 {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	ownerKeys := liveNewUserLimitCardOwnerKeys(reg, now)
	if len(ownerKeys) == 0 {
		return 0
	}
	expires := now.AddDate(0, 0, expiresDays)
	issued := 0
	seen := make(map[string]struct{}, len(users))
	for _, user := range users {
		owner := newUserAccountRef(user.ID, user.Email)
		if owner.empty() {
			continue
		}
		key := voucherOwnerKey(owner)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if !ownerHasLiveNewUserLimitCard(ownerKeys, owner) {
			continue
		}
		reg.ResetVouchers = append(reg.ResetVouchers, ResetVoucher{ID: NewID("reset_voucher"), UserID: owner.UserID, Email: owner.Email, IssuedAt: now, ExpiresAt: expires})
		issued++
	}
	return issued
}

var errNoActiveNewUserLimitCard = errors.New("no active new-user limit card")

// RedeemResetVoucher consumes a voucher and resets the holder's live new-user
// limit-card usage. It leaves the voucher unused when no live card remains.
func RedeemResetVoucher(reg *Registry, ownerID, email, voucherID string, now time.Time) (bool, error) {
	if reg == nil {
		return false, fmt.Errorf("voucher not found")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	owner := newUserAccountRef(ownerID, email)
	for i := range reg.ResetVouchers {
		v := &reg.ResetVouchers[i]
		if !strings.EqualFold(strings.TrimSpace(v.ID), strings.TrimSpace(voucherID)) {
			continue
		}
		if !resetVoucherMatchesOwner(*v, owner) {
			return false, fmt.Errorf("voucher does not belong to this user")
		}
		if v.RedeemedAt != nil {
			return false, fmt.Errorf("voucher already used")
		}
		if !v.ExpiresAt.IsZero() && !v.ExpiresAt.After(now) {
			return false, fmt.Errorf("voucher expired")
		}
		resetAny := false
		for j := range reg.Grants {
			g := &reg.Grants[j]
			if !grantMatchesUser(*g, owner) || !isLiveNewUserLimitCard(reg, *g, now) {
				continue
			}
			resetNewUserLimitCardUsageForGrant(reg, g, now)
			resetAny = true
		}
		if !resetAny {
			return false, errNoActiveNewUserLimitCard
		}
		v.RedeemedAt = &now
		return true, nil
	}
	return false, fmt.Errorf("voucher not found")
}

// resetVoucherMatchesOwner treats a persisted user ID as authoritative. Email
// matching is retained only for legacy vouchers that were issued before IDs
// were stored, preventing a same-email/different-ID account from redeeming a
// voucher belonging to another user.
func resetVoucherMatchesOwner(v ResetVoucher, owner userAccountRef) bool {
	voucherUserID := normalizeUserID(v.UserID)
	if voucherUserID != "" && owner.UserID != "" {
		return voucherUserID == owner.UserID
	}
	if voucherUserID != "" {
		return false
	}
	return owner.Email != "" && normalizeEmail(v.Email) == owner.Email
}

func resetNewUserLimitCardUsageForGrant(reg *Registry, grant *Grant, now time.Time) {
	if grant == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	grant.UsageEvents = nil
	effective := effectiveGrantForRegistry(reg, *grant)
	grant.BillingTimezone = effective.BillingTimezone
	// Restart the five-hour cycle at `now`. Limit cards persist the anchor in
	// PeriodUsage, not AnchoredFiveHour; snapping to the UTC epoch would leave
	// the next reset at the old window end.
	grant.PeriodUsage = CreditPeriodUsage{
		FiveHour: GrantUsageWindow{WindowStart: now},
		Daily:    GrantUsageWindow{WindowStart: grantDayWindowStart(effective, now)},
	}
}

func GrantInvitationCodeBenefitForUser(ctx context.Context, system SystemSettingsRepository, email, invitationCodeID, serviceGroupID string, durationDays int, credits float64) error {
	return GrantInvitationCodeBenefitForUserID(ctx, system, "", email, invitationCodeID, serviceGroupID, durationDays, credits)
}

func GrantInvitationCodeBenefitForUserID(ctx context.Context, system SystemSettingsRepository, userID, email, invitationCodeID, serviceGroupID string, durationDays int, credits float64) error {
	owner := newUserAccountRef(userID, email)
	email = owner.Email
	invitationCodeID = strings.TrimSpace(invitationCodeID)
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if invitationCodeID == "" || serviceGroupID == "" || durationDays <= 0 || credits <= 0 || math.IsNaN(credits) || math.IsInf(credits, 0) {
		return nil
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return err
	}
	if reg.FindModelServiceGroup(serviceGroupID) == nil {
		return nil
	}
	for _, grant := range reg.Grants {
		if grantMatchesUser(grant, owner) &&
			strings.TrimSpace(grant.ServiceGroupID) == serviceGroupID &&
			strings.TrimSpace(grant.Source) == "invitation_code" &&
			strings.TrimSpace(grant.CardID) == invitationCodeID {
			return nil
		}
	}
	now := time.Now().UTC()
	startsAt := nextGrantStart(reg, owner, serviceGroupID, now)
	reg.Grants = append(reg.Grants, Grant{
		ID:             NewID("grant"),
		UserID:         owner.UserID,
		Email:          email,
		ServiceGroupID: serviceGroupID,
		Source:         "invitation_code",
		CardID:         invitationCodeID,
		StartsAt:       startsAt,
		ExpiresAt:      startsAt.Add(time.Duration(durationDays) * 24 * time.Hour),
		CreatedAt:      now,
		CreditsTotal:   roundCredits(credits),
	})
	return SaveRegistry(ctx, system, reg)
}

// GrantUserReferralBenefitForUserID creates one independently expiring reward
// grant for a successful user referral. The referral ID is its idempotency key:
// retries return the original grant instead of duplicating credits. Unlike card
// top-ups, its time window starts at registration, never after a prior grant.
func GrantUserReferralBenefitForUserID(ctx context.Context, system SystemSettingsRepository, userID, email, referralID, serviceGroupID string, durationDays int, credits float64, registeredAt time.Time) (string, error) {
	owner := newUserAccountRef(userID, email)
	if owner.empty() || strings.TrimSpace(referralID) == "" || strings.TrimSpace(serviceGroupID) == "" || durationDays <= 0 || credits <= 0 || math.IsNaN(credits) || math.IsInf(credits, 0) {
		return "", nil
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return "", err
	}
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if err := ReferralRewardServiceGroupAllowed(reg, serviceGroupID); err != nil {
		if errors.Is(err, ErrReferralServiceGroupMissing) {
			return "", nil
		}
		return "", err
	}
	for _, grant := range reg.Grants {
		if grantMatchesUser(grant, owner) && strings.EqualFold(strings.TrimSpace(grant.Source), "user_referral") && strings.TrimSpace(grant.CardID) == strings.TrimSpace(referralID) && strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), serviceGroupID) {
			return grant.ID, nil
		}
	}
	if registeredAt.IsZero() {
		registeredAt = time.Now().UTC()
	}
	registeredAt = registeredAt.UTC()
	grantID := NewID("grant")
	reg.Grants = append(reg.Grants, Grant{ID: grantID, UserID: owner.UserID, Email: owner.Email, ServiceGroupID: serviceGroupID, Source: "user_referral", CardID: strings.TrimSpace(referralID), StartsAt: registeredAt, ExpiresAt: registeredAt.Add(time.Duration(durationDays) * 24 * time.Hour), CreatedAt: registeredAt, CreditsTotal: roundCredits(credits)})
	if err := SaveRegistry(ctx, system, reg); err != nil {
		return "", err
	}
	return grantID, nil
}

// FreezeUserReferralBenefits retains every referral grant for audit while
// preventing its remaining credits from being selected for future usage.
func FreezeUserReferralBenefits(ctx context.Context, system SystemSettingsRepository, referralID string) error {
	referralID = strings.TrimSpace(referralID)
	if referralID == "" {
		return nil
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil || reg == nil {
		return err
	}
	changed := false
	for idx := range reg.Grants {
		grant := &reg.Grants[idx]
		if strings.EqualFold(strings.TrimSpace(grant.Source), "user_referral") && strings.TrimSpace(grant.CardID) == referralID && !grant.Frozen {
			grant.Frozen = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return SaveRegistry(ctx, system, reg)
}

var (
	ErrReferralServiceGroupRequired   = errors.New("service group ID is required")
	ErrReferralServiceGroupSystemFree = errors.New("referral rewards cannot use the reserved system-free service group")
	ErrReferralServiceGroupMissing    = errors.New("referral service group does not exist")
	ErrReferralServiceGroupNotMetered = errors.New("referral rewards require a metered service group")
)

// ReferralRewardServiceGroupAllowed reports whether a group can receive
// metered invitation credits. Free-policy groups, including system-free,
// make billing unlimited and are rejected.
func ReferralRewardServiceGroupAllowed(reg *Registry, serviceGroupID string) error {
	serviceGroupID = strings.TrimSpace(serviceGroupID)
	if serviceGroupID == "" {
		return ErrReferralServiceGroupRequired
	}
	if IsSystemFreeServiceGroup(serviceGroupID) {
		return ErrReferralServiceGroupSystemFree
	}
	if reg == nil || reg.FindModelServiceGroup(serviceGroupID) == nil {
		return ErrReferralServiceGroupMissing
	}
	if reg.AccessPolicyForServiceGroup(serviceGroupID) != AccessPolicyGrantRequired {
		return ErrReferralServiceGroupNotMetered
	}
	return nil
}

// DetachSystemFreeFromReferralOwners freezes leaked system-free grants on
// accounts that already received a user-referral reward. Invitation-code
// overlays are the leak that made invitees unlimited; those bindings are
// stripped. Inviters who only received a metered referral grant keep any
// intentional system-free binding. Frozen grants stay for audit. When
// reissueGroupID is a metered group, frozen system-free user_referral
// grants are copied onto that group so the original credit amount is kept.
func DetachSystemFreeFromReferralOwners(ctx context.Context, system SystemSettingsRepository, reissueGroupID string) (int, error) {
	if system == nil {
		return 0, nil
	}
	reg, err := LoadRegistry(ctx, system)
	if err != nil || reg == nil {
		return 0, err
	}
	owners := newReferralAccountSet()
	for _, grant := range reg.Grants {
		if strings.EqualFold(strings.TrimSpace(grant.Source), "user_referral") {
			owners.add(grant.UserID, grant.Email)
		}
	}
	if owners.empty() {
		return 0, nil
	}
	leaked := newReferralAccountSet()
	changed := 0
	for idx := range reg.Grants {
		grant := &reg.Grants[idx]
		if !IsSystemFreeServiceGroup(grant.ServiceGroupID) || !owners.contains(grant.UserID, grant.Email) {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(grant.Source))
		if source != "invitation_code" && source != "user_referral" {
			continue
		}
		if source == "invitation_code" {
			leaked.add(grant.UserID, grant.Email)
		}
		if !grant.Frozen {
			grant.Frozen = true
			changed++
		}
	}
	bindings := make([]UserBinding, 0, len(reg.UserBindings))
	for _, binding := range reg.UserBindings {
		if !leaked.contains(binding.UserID, binding.Email) {
			bindings = append(bindings, binding)
			continue
		}
		kept := make([]string, 0, len(binding.ServiceGroupIDs))
		for _, id := range binding.ServiceGroupIDs {
			if IsSystemFreeServiceGroup(id) {
				changed++
				continue
			}
			kept = append(kept, id)
		}
		if len(kept) == 0 {
			continue
		}
		binding.ServiceGroupIDs = kept
		bindings = append(bindings, binding)
	}
	reg.UserBindings = bindings
	reissueGroupID = strings.TrimSpace(reissueGroupID)
	if ReferralRewardServiceGroupAllowed(reg, reissueGroupID) == nil {
		have := map[string]struct{}{}
		grantKey := func(userID, email, cardID string) string {
			return normalizeUserID(userID) + "\n" + normalizeEmail(email) + "\n" + strings.TrimSpace(cardID)
		}
		for _, grant := range reg.Grants {
			if strings.EqualFold(strings.TrimSpace(grant.Source), "user_referral") && strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), reissueGroupID) {
				have[grantKey(grant.UserID, grant.Email, grant.CardID)] = struct{}{}
			}
		}
		existing := append([]Grant(nil), reg.Grants...)
		now := time.Now().UTC()
		for _, grant := range existing {
			if !IsSystemFreeServiceGroup(grant.ServiceGroupID) || !strings.EqualFold(strings.TrimSpace(grant.Source), "user_referral") || grant.CreditsTotal <= 0 {
				continue
			}
			if !grantIsValidAt(grant, now) {
				continue
			}
			key := grantKey(grant.UserID, grant.Email, grant.CardID)
			if _, ok := have[key]; ok {
				continue
			}
			reg.Grants = append(reg.Grants, Grant{
				ID:             NewID("grant"),
				UserID:         grant.UserID,
				Email:          grant.Email,
				ServiceGroupID: reissueGroupID,
				Source:         "user_referral",
				CardID:         strings.TrimSpace(grant.CardID),
				StartsAt:       grant.StartsAt,
				ExpiresAt:      grant.ExpiresAt,
				CreatedAt:      now,
				CreditsTotal:   grant.CreditsTotal,
			})
			have[key] = struct{}{}
			changed++
		}
	}
	if changed == 0 {
		return 0, nil
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		return 0, err
	}
	return changed, nil
}

type referralAccountSet struct {
	ids    map[string]struct{}
	emails map[string]struct{}
}

func newReferralAccountSet() referralAccountSet {
	return referralAccountSet{ids: map[string]struct{}{}, emails: map[string]struct{}{}}
}

func (s *referralAccountSet) add(userID, email string) {
	if s == nil {
		return
	}
	if id := normalizeUserID(userID); id != "" {
		s.ids[id] = struct{}{}
	}
	if email := normalizeEmail(email); email != "" {
		s.emails[email] = struct{}{}
	}
}

func (s referralAccountSet) contains(userID, email string) bool {
	if id := normalizeUserID(userID); id != "" {
		if _, ok := s.ids[id]; ok {
			return true
		}
	}
	if email := normalizeEmail(email); email != "" {
		if _, ok := s.emails[email]; ok {
			return true
		}
	}
	return false
}

func (s referralAccountSet) empty() bool {
	return len(s.ids) == 0 && len(s.emails) == 0
}

func grantNewUserBenefit(ctx context.Context, system SystemSettingsRepository, email, source string, ratio float64, useRegistrationWindow bool) error {
	return grantNewUserBenefitForUserID(ctx, system, "", email, source, ratio, useRegistrationWindow)
}

func grantNewUserBenefitForUserID(ctx context.Context, system SystemSettingsRepository, userID, email, source string, ratio float64, useRegistrationWindow bool) error {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		return err
	}
	return grantNewUserBenefitForRegistry(ctx, system, reg, userID, email, source, ratio, useRegistrationWindow)
}

func grantNewUserBenefitForRegistry(ctx context.Context, system SystemSettingsRepository, reg *Registry, userID, email, source string, ratio float64, useRegistrationWindow bool) error {
	if IsSystemLLMUser(userID, email) {
		return nil
	}
	owner := newUserAccountRef(userID, email)
	email = owner.Email
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if ratio <= 0 {
		return nil
	}
	if reg == nil {
		return nil
	}
	serviceGroupIDs := normalizeStringSlice(reg.DefaultNewUserServiceGroups)
	if len(serviceGroupIDs) == 0 {
		return nil
	}
	validServiceGroupIDs := make([]string, 0, len(serviceGroupIDs))
	for _, serviceGroupID := range serviceGroupIDs {
		if IsSystemFreeServiceGroup(serviceGroupID) {
			continue
		}
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
		if findGrantWithSource(reg, owner, serviceGroupID, source) != nil {
			continue
		}
		startsAt := nextGrantStart(reg, owner, serviceGroupID, now)
		expiresAt := startsAt.Add(time.Duration(days) * 24 * time.Hour)
		if useRegistrationWindow {
			base := findGrantWithSource(reg, owner, serviceGroupID, "new_user_default")
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
			UserID:         owner.UserID,
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

// MinimumRequestCredits is the minimum credit charge for a successful LLM request.
// This prevents successful tiny requests or upstream responses that omit usage
// from appearing as zero-cost in credit-card usage displays.
const MinimumRequestCredits = 0.1

// EstimateCreditsWithFloor is like EstimateCredits but applies a minimum charge
// floor for successful requests. Use this when the request is known to have
// succeeded (statusCode < 400 and response body is non-empty).
func EstimateCreditsWithFloor(tokens int64, multiplier float64, tokensPerCredit int) float64 {
	credits := EstimateCredits(tokens, multiplier, tokensPerCredit)
	if credits < MinimumRequestCredits {
		return MinimumRequestCredits
	}
	return credits
}

// BillingGroupMultiplier resolves the only multiplier that may affect a user
// credit charge. Provider/model CreditMultiplier remains a dispatch concern.
func BillingGroupMultiplier(reg *Registry, serviceGroupIDs []string) float64 {
	if reg == nil {
		return 1
	}
	for _, id := range serviceGroupIDs {
		group := reg.FindModelServiceGroup(id)
		if group == nil {
			continue
		}
		if group.BillingGroupMultiplier > 0 && !math.IsNaN(group.BillingGroupMultiplier) && !math.IsInf(group.BillingGroupMultiplier, 0) {
			return group.BillingGroupMultiplier
		}
		return 1
	}
	return 1
}

// ResolveTokenPricingForProvider returns the model-route pricing configured
// for providerID. The returned snapshot must be persisted by a later ledger
// implementation; for now it keeps billing deterministic within a request.
func ResolveTokenPricingForProvider(model *AuthorizedModel, providerID string, startedAt time.Time) (llmpool.ResolvedTokenPricing, bool) {
	return ResolveTokenPricingForProviderRoute(model, providerID, "", startedAt)
}

// PricingSourceForProviderRoute reports the configuration owner of a frozen
// route price for audit and usage reports. An explicit route override is the
// only case where a service-group price replaces the provider base price; an
// override flag without a usable price cannot supply a price and therefore
// does not claim the source either (mirrors EffectiveRouteTokenPricing).
func PricingSourceForProviderRoute(model *AuthorizedModel, providerID, upstreamModel string) string {
	if model != nil {
		if route, ok := providerRouteBilling(model, normalizedProviderKey(providerID), upstreamModel); ok &&
			route.TokenPricingOverride && route.TokenPricing.HasCreditPricing() {
			return llmpool.PricingSourceServiceGroupOverride
		}
	}
	return llmpool.PricingSourceProvider
}

// ResolveTokenPricingForProviderRoute resolves one provider/model route. The
// concrete upstream model takes precedence, preventing a provider reused by
// multiple routes from inheriting another route's price during settlement. A
// route entry only supplies the price when the service group explicitly
// overrides the provider base price; otherwise the provider-level price is
// used (mirrors llmpool.EffectiveRouteTokenPricing).
func ResolveTokenPricingForProviderRoute(model *AuthorizedModel, providerID, upstreamModel string, startedAt time.Time) (llmpool.ResolvedTokenPricing, bool) {
	if model == nil {
		return llmpool.ResolvedTokenPricing{}, false
	}
	providerKey := normalizedProviderKey(providerID)
	if route, ok := providerRouteBilling(model, providerKey, upstreamModel); ok {
		if route.BillingMode == llmpool.BillingModeFree {
			return llmpool.ResolvedTokenPricing{}, false
		}
		if route.TokenPricingOverride && route.TokenPricing.HasCreditPricing() {
			return llmpool.ResolveTokenPricing(route.TokenPricing, startedAt)
		}
	}
	if llmpool.NormalizeBillingMode(model.ProviderBillingModes[providerKey]) == llmpool.BillingModeFree {
		return llmpool.ResolvedTokenPricing{}, false
	}
	pricing, ok := model.ProviderTokenPricing[providerKey]
	if !ok {
		return llmpool.ResolvedTokenPricing{}, false
	}
	return llmpool.ResolveTokenPricing(pricing, startedAt)
}

// IsFreeBillingRoute reports whether providerID is explicitly configured as a
// free route for model. An explicit free route must never fall through to the
// legacy CreditMultiplier billing path when it has no token-price snapshot.
func IsFreeBillingRoute(model *AuthorizedModel, providerID string) bool {
	return IsFreeBillingProviderRoute(model, providerID, "")
}

// IsFreeBillingProviderRoute reports a terminal free policy for a concrete
// provider route. The upstream-model argument removes ambiguity when one
// provider is configured under several logical models.
func IsFreeBillingProviderRoute(model *AuthorizedModel, providerID, upstreamModel string) bool {
	if model == nil {
		return false
	}
	providerKey := normalizedProviderKey(providerID)
	if route, ok := providerRouteBilling(model, providerKey, upstreamModel); ok {
		return route.BillingMode == llmpool.BillingModeFree
	}
	return llmpool.NormalizeBillingMode(model.ProviderBillingModes[providerKey]) == llmpool.BillingModeFree
}

func providerRouteBilling(model *AuthorizedModel, providerKey, upstreamModel string) (ProviderRouteBilling, bool) {
	if model == nil || providerKey == "" {
		return ProviderRouteBilling{}, false
	}
	routes := model.ProviderRouteBilling[providerKey]
	if len(routes) == 0 {
		return ProviderRouteBilling{}, false
	}
	if upstreamKey := normalizedUpstreamModelKey(upstreamModel, model.Name); upstreamKey != "" {
		if route, ok := routes[upstreamKey]; ok {
			return route, true
		}
	}
	if len(routes) == 1 {
		for _, route := range routes {
			return route, true
		}
	}
	return ProviderRouteBilling{}, false
}

// EstimateTokenPricingCredits calculates input and output separately, then
// applies the route's provider-owned minimum after the service-group markup.
func EstimateTokenPricingCredits(inputTokens, outputTokens int64, pricing llmpool.ResolvedTokenPricing, billingGroupMultiplier float64) float64 {
	return EstimateTokenPricingCreditsWithCache(inputTokens, outputTokens, 0, 0, pricing, billingGroupMultiplier)
}

func EstimateTokenPricingCreditsWithCache(inputTokens, outputTokens, cachedInputTokens, cacheWriteTokens int64, pricing llmpool.ResolvedTokenPricing, billingGroupMultiplier float64) float64 {
	if microcredits, ok := llmpool.EstimateTokenPricingMicrocreditsWithCache(inputTokens, outputTokens, cachedInputTokens, cacheWriteTokens, pricing, billingGroupMultiplier); ok {
		return llmpool.MicrocreditsToCredits(microcredits)
	}
	if billingGroupMultiplier <= 0 || math.IsNaN(billingGroupMultiplier) || math.IsInf(billingGroupMultiplier, 0) {
		billingGroupMultiplier = 1
	}
	// Clamp in the same order as the fixed-point path: totals first, then the
	// cache legs bounded by the (already non-negative) input total.
	inputTokens = maxInt64(inputTokens, 0)
	outputTokens = maxInt64(outputTokens, 0)
	cachedInputTokens = maxInt64(cachedInputTokens, 0)
	cacheWriteTokens = maxInt64(cacheWriteTokens, 0)
	if cachedInputTokens > inputTokens {
		cachedInputTokens = inputTokens
	}
	if cacheWriteTokens > inputTokens-cachedInputTokens {
		cacheWriteTokens = inputTokens - cachedInputTokens
	}
	p := pricing.TokenPricing.WithCachePricingDefaults()
	input := float64(inputTokens-cachedInputTokens-cacheWriteTokens) * p.InputCreditsPer10K * billingGroupMultiplier / 10000
	input += float64(cachedInputTokens) * llmpool.OptionalTokenPriceValue(p.CacheReadCreditsPer10K) * billingGroupMultiplier / 10000
	input += float64(cacheWriteTokens) * llmpool.OptionalTokenPriceValue(p.CacheWriteCreditsPer10K) * billingGroupMultiplier / 10000
	output := float64(outputTokens) * p.OutputCreditsPer10K * billingGroupMultiplier / 10000
	// This fallback exists for inputs the fixed-point path rejected; a negative,
	// NaN, or infinite price component must still never turn into a negative or
	// unbounded debit. Fail safe to zero, matching the fixed-point path's
	// rejection of non-finite configuration.
	if input < 0 || math.IsNaN(input) || math.IsInf(input, 0) {
		input = 0
	}
	if output < 0 || math.IsNaN(output) || math.IsInf(output, 0) {
		output = 0
	}
	credits := input + output
	minimum := pricing.MinimumRequestCredits * billingGroupMultiplier
	if minimum < 0 || math.IsNaN(minimum) || math.IsInf(minimum, 0) {
		minimum = 0
	}
	if credits < minimum {
		credits = minimum
	}
	// roundCredits uses binary floating-point half-up, so at an exact x.xxx5
	// boundary it can differ by 0.001 from the fixed-point path's integer
	// quantum rounding. Acceptable here: this fallback only runs when the
	// fixed-point calculation already rejected the configuration.
	return roundCredits(credits)
}

func maxInt64(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}

func ApplyCreditUsageToRegistry(reg *Registry, email string, serviceGroupIDs []string, credits float64, now time.Time) float64 {
	return applyCreditUsageToRegistry(reg, newUserAccountRef("", email), serviceGroupIDs, credits, now)
}

func ApplyCreditUsageToRegistryForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, credits float64, now time.Time) float64 {
	return applyCreditUsageToRegistry(reg, newUserAccountRef(userID, email), serviceGroupIDs, credits, now)
}

// HasBillingRequest reports whether this request was already finalized. It is
// deliberately request-scoped, rather than user-scoped, so a network replay
// cannot debit a successful upstream response twice.
func HasBillingRequest(reg *Registry, requestID string) bool {
	if reg == nil || strings.TrimSpace(requestID) == "" {
		return false
	}
	for _, entry := range reg.BillingLedger {
		if strings.EqualFold(strings.TrimSpace(entry.RequestID), strings.TrimSpace(requestID)) {
			return true
		}
	}
	return false
}

// BillingLedgerEntryForRequest returns a copy of an existing immutable entry.
// It enables the staged SQLite audit mirror to be repaired after a transient
// mirror write failure without charging the user's mutable balance again.
func BillingLedgerEntryForRequest(reg *Registry, requestID string) (BillingLedgerEntry, bool) {
	if reg == nil || strings.TrimSpace(requestID) == "" {
		return BillingLedgerEntry{}, false
	}
	for _, entry := range reg.BillingLedger {
		if strings.EqualFold(strings.TrimSpace(entry.RequestID), strings.TrimSpace(requestID)) {
			if entry.Pricing != nil {
				pricing := *entry.Pricing
				// The copy must not alias the ledger's presence-aware price
				// pointers; the entry is an immutable fact shared by repair
				// and replay readers.
				pricing.TokenPricing = pricing.TokenPricing.Clone()
				entry.Pricing = &pricing
			}
			entry.ServiceGroupIDs = append([]string(nil), entry.ServiceGroupIDs...)
			return entry, true
		}
	}
	return BillingLedgerEntry{}, false
}

// BillingLedgerRegistryKeep bounds how many settlement entries are retained
// inside the service registry JSON. The registry is hot-loaded on every
// service-account poll, so an unbounded append-only ledger (tens of MB in
// production) dominated hub CPU. Durable settlement facts live in the
// dedicated llm_billing_ledger SQLite table; the registry-embedded copy only
// backs the request-replay idempotency check and the transient mirror-repair
// lookup, both of which only need recent entries.
const BillingLedgerRegistryKeep = 2000

// TrimBillingLedgerForRegistry drops the oldest registry-embedded ledger
// entries beyond BillingLedgerRegistryKeep, preserving the newest entries.
// Callers persist the registry afterwards when they save it for any reason.
func TrimBillingLedgerForRegistry(reg *Registry) {
	if reg == nil || len(reg.BillingLedger) <= BillingLedgerRegistryKeep {
		return
	}
	reg.BillingLedger = append([]BillingLedgerEntry(nil), reg.BillingLedger[len(reg.BillingLedger)-BillingLedgerRegistryKeep:]...)
}

func AppendBillingLedgerEntry(reg *Registry, entry BillingLedgerEntry) {
	if reg == nil || strings.TrimSpace(entry.RequestID) == "" || HasBillingRequest(reg, entry.RequestID) {
		return
	}
	entry.RequestID = strings.TrimSpace(entry.RequestID)
	entry.UserID = strings.TrimSpace(entry.UserID)
	entry.Email = normalizeEmail(entry.Email)
	entry.ProviderID = strings.TrimSpace(entry.ProviderID)
	entry.ServiceGroupIDs = normalizeStringSlice(entry.ServiceGroupIDs)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	entry.RequestedCredits = roundCredits(entry.RequestedCredits)
	entry.DeductedCredits = roundCredits(entry.DeductedCredits)
	if entry.RequestedMicrocredits <= 0 && entry.RequestedCredits > 0 {
		entry.RequestedMicrocredits = int64(math.Round(entry.RequestedCredits * float64(llmpool.MicrocreditsPerCredit)))
	}
	if entry.DeductedMicrocredits <= 0 && entry.DeductedCredits > 0 {
		entry.DeductedMicrocredits = int64(math.Round(entry.DeductedCredits * float64(llmpool.MicrocreditsPerCredit)))
	}
	reg.BillingLedger = append(reg.BillingLedger, entry)
	TrimBillingLedgerForRegistry(reg)
}

func applyCreditUsageToRegistry(reg *Registry, owner userAccountRef, serviceGroupIDs []string, credits float64, now time.Time) float64 {
	if reg == nil || credits <= 0 {
		return 0
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if owner.empty() || len(serviceGroupIDs) == 0 {
		return 0
	}
	// Route selection can yield more than one service group for the same
	// provider. If any of them remains an ordinary free route, this request is
	// free: do not debit a welcome limit card that happens to be attached to a
	// different group on that provider. This must mirror billing eligibility;
	// otherwise the request is correctly admitted as free but silently consumes
	// the card's five-hour/daily allowance.
	if hasUnrestrictedFreeServiceGroup(reg, owner, serviceGroupIDs, now) {
		return 0
	}
	// A normal unlimited entitlement bypasses billing. A new-user limit card
	// needs ledger entries only when it has a configured period cap.
	if hasUnmeteredUnlimitedActiveGrantForServiceGroups(reg, owner, serviceGroupIDs, now) && !hasPeriodLimitedNewUserLimitCardForAnyServiceGroup(reg, owner, serviceGroupIDs, now) {
		return 0
	}
	if idx := earlyStartableUnmeteredUnlimitedGrantIndex(reg, owner, serviceGroupIDs, now); idx >= 0 {
		shiftGrantToEarlyStart(&reg.Grants[idx], now)
		return 0
	}
	type candidate struct {
		idx        int
		g          Grant
		earlyStart bool
	}
	candidates := make([]candidate, 0)
	// Qualification issuance is idempotent, but old data may contain duplicate
	// records. They represent one shared policy entitlement per group, never
	// multiple independent allowances.
	limitCardCandidateGroups := map[string]struct{}{}
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	}
	limitCardGroupSet := activePeriodLimitedNewUserLimitCardGroupSet(reg, owner, serviceGroupSet, now)
	for i, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if nonWelcomeGrantBlockedByLimitCard(reg, grant, limitCardGroupSet) {
			continue
		}
		if !grantIsValidAt(grant, now) {
			continue
		}
		earlyStart := false
		if now.Before(grant.StartsAt) {
			if !canEarlyStartQueuedGrant(reg, owner, grant, i, serviceGroupSet, now) {
				continue
			}
			earlyStart = true
		}
		candidateGrant := grant
		if earlyStart {
			candidateGrant = grantWithEarlyStartWindow(grant, now)
		}
		if skipDuplicateNewUserLimitCardGroup(limitCardCandidateGroups, reg, grant) {
			continue
		}
		if consumableGrantCredits(candidateGrant, now) <= 0 {
			continue
		}
		candidates = append(candidates, candidate{idx: i, g: candidateGrant, earlyStart: earlyStart})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].earlyStart != candidates[j].earlyStart {
			return !candidates[i].earlyStart
		}
		left, right := candidates[i].g, candidates[j].g
		if leftWelcome, rightWelcome := isNewUserLimitCardSource(left.Source), isNewUserLimitCardSource(right.Source); leftWelcome != rightWelcome {
			return leftWelcome
		}
		// Each referral reward has an independent validity window. When two
		// referral rewards compete, consume the oldest issuance first rather than
		// allowing a newer, earlier-expiring reward to jump the queue. Keep the
		// pre-existing expiry ordering for every other pairing so card, system and
		// queued-grant semantics remain unchanged.
		if left.Source == "user_referral" && right.Source == "user_referral" {
			if !left.StartsAt.Equal(right.StartsAt) {
				return left.StartsAt.Before(right.StartsAt)
			}
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.Before(right.CreatedAt)
			}
			if left.ID != right.ID {
				return left.ID < right.ID
			}
			return candidates[i].idx < candidates[j].idx
		}
		if left.ExpiresAt.Equal(right.ExpiresAt) {
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.Before(right.CreatedAt)
			}
			return candidates[i].idx < candidates[j].idx
		}
		return left.ExpiresAt.Before(right.ExpiresAt)
	})
	remaining := credits
	consumed := 0.0
	for _, cand := range candidates {
		grant := &reg.Grants[cand.idx]
		if cand.earlyStart {
			shiftGrantToEarlyStart(grant, now)
		}
		effective := effectiveGrantForRegistry(reg, *grant)
		available := consumableGrantCredits(effective, now)
		if available <= 0 {
			continue
		}
		use := math.Min(available, remaining)
		grant.CreditsUsed = roundCredits(grant.CreditsUsed + use)
		applyGrantPeriodUsageForRegistry(reg, grant, use, now)
		consumed += use
		remaining -= use
		if remaining <= 0 {
			break
		}
	}
	return roundCredits(consumed)
}

func hasNewUserLimitCardForAnyServiceGroup(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) bool {
	for _, serviceGroupID := range serviceGroupIDs {
		if hasNewUserLimitCardForServiceGroup(reg, owner, serviceGroupID, now) {
			return true
		}
	}
	return false
}

func AvailableCreditsForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) float64 {
	return availableCreditsForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs, now)
}

func AvailableCreditsForServiceGroupsForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, now time.Time) float64 {
	return availableCreditsForServiceGroups(reg, newUserAccountRef(userID, email), serviceGroupIDs, now)
}

// HeldBillingCreditsForServiceGroupsForUserID reports how many of the owner's
// credits are currently held by in-flight billing reservations on the given
// service groups. Admission subtracts these holds from the spendable balance;
// callers surface the same number so a denial or status readout can explain
// why the available amount is lower than the raw period window remaining.
func HeldBillingCreditsForServiceGroupsForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, now time.Time) float64 {
	return reservedBillingCreditsForServiceGroups(reg, newUserAccountRef(userID, email), serviceGroupIDs, now)
}

// ReserveBillingCreditsForUserID records a short-lived admission hold without
// prematurely adding usage to a grant. This makes concurrent preflight checks
// see each other's maximum possible cost while preserving the existing rule
// that period usage is recorded only for actual upstream consumption.
func ReserveBillingCreditsForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, requestID string, credits float64, expiresAt, now time.Time) (float64, bool) {
	if reg == nil || strings.TrimSpace(requestID) == "" || credits <= 0 || expiresAt.IsZero() {
		return 0, false
	}
	pruneExpiredBillingReservations(reg, now)
	if HasBillingRequest(reg, requestID) {
		return 0, true
	}
	for _, reservation := range reg.BillingReservations {
		// A terminal row (usage_unresolved) no longer holds Credits; a retried
		// requestID must establish a fresh hold instead of resurrecting the
		// released one.
		if reservation.Status == "" && strings.EqualFold(strings.TrimSpace(reservation.RequestID), strings.TrimSpace(requestID)) {
			return roundCredits(reservation.Credits), true
		}
	}
	owner := newUserAccountRef(userID, email)
	groups := normalizeStringSlice(serviceGroupIDs)
	if owner.empty() || len(groups) == 0 {
		return 0, false
	}
	available := availableCreditsForServiceGroups(reg, owner, groups, now)
	if available > 0 && available+0.000001 < credits {
		return 0, false
	}
	// Existing unlimited entitlements use 0 as their compatibility availability
	// value; they never need a finite monetary hold. Do not treat a finite
	// balance that other in-flight requests have fully reserved as unlimited.
	if available <= 0 {
		allowed, _, _, _, _, _, _ := billingEligibilityForServiceGroups(reg, owner, groups, now)
		return 0, allowed
	}
	credits = roundCredits(credits)
	reg.BillingReservations = append(reg.BillingReservations, BillingReservation{
		RequestID:       strings.TrimSpace(requestID),
		UserID:          owner.UserID,
		Email:           owner.Email,
		ServiceGroupIDs: groups,
		Credits:         credits,
		ExpiresAt:       expiresAt.UTC(),
		CreatedAt:       now.UTC(),
	})
	return credits, true
}

// ReleaseBillingReservation removes an unfinished hold. Final settlement also
// calls it in the same registry write before applying actual usage.
func ReleaseBillingReservation(reg *Registry, requestID string, now time.Time) bool {
	if reg == nil || strings.TrimSpace(requestID) == "" {
		return false
	}
	pruneExpiredBillingReservations(reg, now)
	removed := false
	out := reg.BillingReservations[:0]
	for _, reservation := range reg.BillingReservations {
		if strings.EqualFold(strings.TrimSpace(reservation.RequestID), strings.TrimSpace(requestID)) {
			removed = true
			continue
		}
		out = append(out, reservation)
	}
	reg.BillingReservations = out
	return removed
}

// MarkBillingReservationUsageUnresolved releases a sent hold while retaining
// the row as terminal audit evidence, mirroring the local lost-response path:
// the hold stops counting against the balance (see reservationHoldReleased)
// and a later prune pass drops the row after the evidence retention window.
// It returns false when no live (unsettled, non-terminal) row matches.
func MarkBillingReservationUsageUnresolved(reg *Registry, requestID string, now time.Time) bool {
	if reg == nil || strings.TrimSpace(requestID) == "" {
		return false
	}
	pruneExpiredBillingReservations(reg, now)
	for i := range reg.BillingReservations {
		if reg.BillingReservations[i].Status != "" || !strings.EqualFold(strings.TrimSpace(reg.BillingReservations[i].RequestID), strings.TrimSpace(requestID)) {
			continue
		}
		reg.BillingReservations[i].Status = BillingReservationUsageUnresolved
		return true
	}
	return false
}

// MarkBillingReservationSent records the point at which the request may have
// left Hub. Recovery must retain a sent reservation until it is settled or a
// trusted upstream reconciliation proves that no billable work occurred.
func MarkBillingReservationSent(reg *Registry, requestID string, now time.Time) bool {
	if reg == nil || strings.TrimSpace(requestID) == "" {
		return false
	}
	for i := range reg.BillingReservations {
		// A terminal row (usage_unresolved) from an earlier lost attempt must not
		// shadow the live reservation when a retry reuses the request ID.
		if reg.BillingReservations[i].Status != "" || !strings.EqualFold(strings.TrimSpace(reg.BillingReservations[i].RequestID), strings.TrimSpace(requestID)) {
			continue
		}
		if reg.BillingReservations[i].SentAt.IsZero() {
			reg.BillingReservations[i].SentAt = now.UTC()
		}
		return true
	}
	return false
}

// SetBillingReservationBillingDetails freezes the dispatch identity and both
// independently-owned pricing factors needed by delayed official reconciliation
// after a response or trailer is lost.
func SetBillingReservationBillingDetails(reg *Registry, requestID, providerID string, providerMultiplier, billingGroupMultiplier float64) bool {
	if reg == nil || strings.TrimSpace(requestID) == "" || strings.TrimSpace(providerID) == "" || providerMultiplier < 0 || math.IsNaN(providerMultiplier) || math.IsInf(providerMultiplier, 0) || billingGroupMultiplier <= 0 || math.IsNaN(billingGroupMultiplier) || math.IsInf(billingGroupMultiplier, 0) {
		return false
	}
	for i := range reg.BillingReservations {
		// See MarkBillingReservationSent: never freeze details onto a terminal
		// evidence row while a live retry reservation carries the same ID.
		if reg.BillingReservations[i].Status != "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(reg.BillingReservations[i].RequestID), strings.TrimSpace(requestID)) {
			reg.BillingReservations[i].ProviderID = strings.TrimSpace(providerID)
			reg.BillingReservations[i].ProviderMultiplier = providerMultiplier
			reg.BillingReservations[i].BillingGroupMultiplier = billingGroupMultiplier
			return true
		}
	}
	return false
}

// SentBillingReservationsForUserID returns copies, preserving Registry's
// single-save mutation boundary for recovery and settlement.
func SentBillingReservationsForUserID(reg *Registry, userID, email string) []BillingReservation {
	if reg == nil {
		return nil
	}
	owner := newUserAccountRef(userID, email)
	if owner.empty() {
		return nil
	}
	out := make([]BillingReservation, 0)
	for _, reservation := range reg.BillingReservations {
		if reservation.SentAt.IsZero() || reservation.Status != "" || !sameReservationOwner(reservation, owner) {
			continue
		}
		copyReservation := reservation
		copyReservation.ServiceGroupIDs = append([]string(nil), reservation.ServiceGroupIDs...)
		out = append(out, copyReservation)
	}
	return out
}

// SentBillingReservations returns copies of every sent reservation. It is used
// by the background reconciliation worker; callers must still scope their
// registry/settings repository to one tenant before using the result.
// Terminal rows (usage_unresolved) are excluded: their holds are already
// released and no upstream reconciliation applies to them.
func SentBillingReservations(reg *Registry) []BillingReservation {
	if reg == nil {
		return nil
	}
	out := make([]BillingReservation, 0)
	for _, reservation := range reg.BillingReservations {
		if reservation.SentAt.IsZero() || reservation.Status != "" {
			continue
		}
		copyReservation := reservation
		copyReservation.ServiceGroupIDs = append([]string(nil), reservation.ServiceGroupIDs...)
		out = append(out, copyReservation)
	}
	return out
}

func pruneExpiredBillingReservations(reg *Registry, now time.Time) {
	if reg == nil || len(reg.BillingReservations) == 0 {
		return
	}
	out := reg.BillingReservations[:0]
	for _, reservation := range reg.BillingReservations {
		// A quote TTL may expire while a streaming response is in progress or
		// after a transport failure. Once sent, only settlement/reconciliation may
		// release the hold; expiration alone is safe only before dispatch.
		if reservation.SentAt.IsZero() && (reservation.ExpiresAt.IsZero() || !reservation.ExpiresAt.After(now)) {
			continue
		}
		if reservation.Status != "" && now.Sub(reservation.SentAt) > BillingReservationTerminalRetention {
			// Terminal evidence rows are not kept forever: past the retention
			// window the audit fact is logged and the row is dropped so the
			// registry JSON cannot grow without bound on lost responses.
			log.Printf("[llm-billing] dropping %s reservation %s for provider %q after retention window (sent_at=%s, credits=%.3f)",
				reservation.Status, reservation.RequestID, reservation.ProviderID, reservation.SentAt.UTC().Format(time.RFC3339), reservation.Credits)
			continue
		}
		if sentLocalReservationUnresolved(reservation, now) {
			// A sent local (non-official) reservation has no upstream
			// reconciliation channel. Past the recovery window the response is
			// considered lost: release the hold from the balance while keeping
			// the row as usage_unresolved evidence (design §8) rather than
			// silently deleting it or holding the Credits forever.
			reservation.Status = BillingReservationUsageUnresolved
		}
		out = append(out, reservation)
	}
	reg.BillingReservations = out
}

// sentLocalReservationUnresolved reports whether a sent non-official
// reservation has outlived the response-recovery window without a settlement.
// Official reservations return false: HubCenter's authenticated attempt
// endpoint remains their recovery channel. A reservation without a frozen
// ProviderID predates the dispatch-identity fields and is treated as local.
func sentLocalReservationUnresolved(reservation BillingReservation, now time.Time) bool {
	if reservation.SentAt.IsZero() || reservation.Status != "" || IsBuiltinProvider(reservation.ProviderID) {
		return false
	}
	return now.Sub(reservation.SentAt) > SentLocalBillingReservationMaxAge
}

// reservationHoldReleased reports whether the reservation no longer holds
// Credits against the owner's balance: either it carries a terminal marker
// (usage_unresolved) or it is a sent local reservation that has just crossed
// the recovery window and not yet been marked by a prune pass.
func reservationHoldReleased(reservation BillingReservation, now time.Time) bool {
	return reservation.Status != "" || sentLocalReservationUnresolved(reservation, now)
}

func reservedBillingCreditsForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) float64 {
	if reg == nil || owner.empty() || len(serviceGroupIDs) == 0 {
		return 0
	}
	groupSet := make(map[string]struct{}, len(serviceGroupIDs))
	for _, id := range serviceGroupIDs {
		groupSet[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	}
	total := 0.0
	for _, reservation := range reg.BillingReservations {
		if (reservation.SentAt.IsZero() && (reservation.ExpiresAt.IsZero() || !reservation.ExpiresAt.After(now))) || reservationHoldReleased(reservation, now) || !sameReservationOwner(reservation, owner) {
			continue
		}
		for _, id := range reservation.ServiceGroupIDs {
			if _, ok := groupSet[strings.ToLower(strings.TrimSpace(id))]; ok {
				total += reservation.Credits
				break
			}
		}
	}
	return roundCredits(total)
}

// reservedBillingCreditsByGroup attributes every live hold of the owner to each
// of its service groups in one reservation pass. A hold spanning several
// groups counts toward each of them, matching what per-group admission checks
// would subtract for that group.
func reservedBillingCreditsByGroup(reg *Registry, owner userAccountRef, now time.Time) map[string]float64 {
	out := map[string]float64{}
	if reg == nil || owner.empty() {
		return out
	}
	for _, reservation := range reg.BillingReservations {
		if (reservation.SentAt.IsZero() && (reservation.ExpiresAt.IsZero() || !reservation.ExpiresAt.After(now))) || reservationHoldReleased(reservation, now) || !sameReservationOwner(reservation, owner) {
			continue
		}
		for _, id := range reservation.ServiceGroupIDs {
			key := strings.ToLower(strings.TrimSpace(id))
			if key != "" {
				out[key] += reservation.Credits
			}
		}
	}
	for key, total := range out {
		out[key] = roundCredits(total)
	}
	return out
}

func sameReservationOwner(reservation BillingReservation, owner userAccountRef) bool {
	if owner.UserID != "" && normalizeUserID(reservation.UserID) == owner.UserID {
		return true
	}
	return owner.UserID == "" && owner.Email != "" && normalizeEmail(reservation.Email) == owner.Email
}

func availableCreditsForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) float64 {
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
	limitCardGroupSet := activePeriodLimitedNewUserLimitCardGroupSet(reg, owner, serviceGroupSet, now)
	total := 0.0
	seenLimitCardGroups := map[string]struct{}{}
	for i, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if nonWelcomeGrantBlockedByLimitCard(reg, grant, limitCardGroupSet) {
			continue
		}
		if !grantIsValidAt(grant, now) {
			continue
		}
		if now.Before(grant.StartsAt) {
			if !canEarlyStartQueuedGrant(reg, owner, grant, i, serviceGroupSet, now) {
				continue
			}
			grant = grantWithEarlyStartWindow(grant, now)
		}
		if skipDuplicateNewUserLimitCardGroup(seenLimitCardGroups, reg, grant) {
			continue
		}
		total += availableGrantCredits(grant, now)
	}
	// Holds are not grant usage. They only reduce admission availability until
	// actual usage is finalized or the hold expires/releases.
	if total > 0 {
		total -= reservedBillingCreditsForServiceGroups(reg, owner, serviceGroupIDs, now)
		if total < 0 {
			total = 0
		}
	}
	return roundCredits(total)
}

func hasEarlyStartableUnmeteredUnlimitedGrant(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) bool {
	return earlyStartableUnmeteredUnlimitedGrantIndex(reg, owner, serviceGroupIDs, now) >= 0
}

func earlyStartableUnmeteredUnlimitedGrantIndex(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) int {
	if reg == nil {
		return -1
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if owner.empty() || len(serviceGroupIDs) == 0 {
		return -1
	}
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	}
	if !queuedGrantBlockedOnlyByExhausted(reg, owner, serviceGroupSet, now) {
		return -1
	}
	bestIdx := -1
	for i, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if grant.CreditsTotal > 0 || hasGrantPeriodLimits(grant) || !grant.StartsAt.After(now) || !grantIsValidAt(grant, now) {
			continue
		}
		if !canEarlyStartQueuedGrant(reg, owner, grant, i, serviceGroupSet, now) {
			continue
		}
		if bestIdx < 0 || queuedGrantPrecedes(grant, i, reg.Grants[bestIdx], bestIdx) {
			bestIdx = i
		}
	}
	return bestIdx
}

func queuedGrantBlockedOnlyByExhausted(reg *Registry, owner userAccountRef, serviceGroupSet map[string]struct{}, now time.Time) bool {
	blockedByExhausted := false
	for _, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !grantIsValidAt(grant, now) {
			continue
		}
		if availableGrantCredits(grant, now) > 0 || (grant.CreditsTotal <= 0 && !hasGrantPeriodLimits(grant)) {
			return false
		}
		status, _, _, _ := grantStatus(grant, now)
		switch status {
		case "exhausted":
			blockedByExhausted = true
		case "period_limited":
			return false
		}
	}
	return blockedByExhausted
}

func canEarlyStartQueuedGrant(reg *Registry, owner userAccountRef, queued Grant, queuedIndex int, serviceGroupSet map[string]struct{}, now time.Time) bool {
	queued = effectiveGrantForRegistry(reg, queued)
	if reg == nil || queued.Frozen || !queued.StartsAt.After(now) || !grantIsValidAt(queued, now) {
		return false
	}
	if queued.CreditsTotal > 0 && remainingGrantCredits(queued) <= 0 {
		return false
	}
	blockedByExhausted := false
	blockedByPeriodLimit := false
	for _, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !grantIsValidAt(grant, now) {
			continue
		}
		if availableGrantCredits(grant, now) > 0 || (grant.CreditsTotal <= 0 && !hasGrantPeriodLimits(grant)) {
			return false
		}
		status, _, _, _ := grantStatus(grant, now)
		switch status {
		case "exhausted":
			blockedByExhausted = true
		case "period_limited":
			blockedByPeriodLimit = true
		}
	}
	if blockedByPeriodLimit {
		return !hasGrantPeriodLimits(queued) && !hasEarlierQueuedGrant(reg, owner, queued, queuedIndex, serviceGroupSet, now, false)
	}
	return blockedByExhausted && !hasEarlierQueuedGrant(reg, owner, queued, queuedIndex, serviceGroupSet, now, true)
}

func hasEarlierQueuedGrant(reg *Registry, owner userAccountRef, queued Grant, queuedIndex int, serviceGroupSet map[string]struct{}, now time.Time, includePeriodLimited bool) bool {
	for idx, grant := range reg.Grants {
		if idx == queuedIndex || (queued.ID != "" && grant.ID == queued.ID) {
			continue
		}
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if !grant.StartsAt.After(now) || !grantIsValidAt(grant, now) {
			continue
		}
		if grant.CreditsTotal > 0 && remainingGrantCredits(grant) <= 0 {
			continue
		}
		if !includePeriodLimited && hasGrantPeriodLimits(grant) {
			continue
		}
		if availableGrantCredits(grantWithEarlyStartWindow(grant, now), now) <= 0 {
			continue
		}
		if queuedGrantPrecedes(grant, idx, queued, queuedIndex) {
			return true
		}
	}
	return false
}

func queuedGrantPrecedes(a Grant, aIndex int, b Grant, bIndex int) bool {
	if !a.StartsAt.Equal(b.StartsAt) {
		return a.StartsAt.Before(b.StartsAt)
	}
	if !a.ExpiresAt.Equal(b.ExpiresAt) {
		return a.ExpiresAt.Before(b.ExpiresAt)
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	if a.ID != "" && b.ID != "" && a.ID != b.ID {
		return a.ID < b.ID
	}
	return aIndex >= 0 && bIndex >= 0 && aIndex < bIndex
}

func grantWithEarlyStartWindow(grant Grant, now time.Time) Grant {
	shiftGrantToEarlyStart(&grant, now)
	return grant
}

func shiftGrantToEarlyStart(grant *Grant, now time.Time) {
	if grant == nil || !grant.StartsAt.After(now) {
		return
	}
	duration := grant.ExpiresAt.Sub(grant.StartsAt)
	if duration <= 0 {
		return
	}
	grant.StartsAt = now
	grant.ExpiresAt = now.Add(duration)
}

func availableGrantCredits(grant Grant, now time.Time) float64 {
	periodRemaining := availableGrantPeriodCredits(grant, now)
	if periodRemaining <= 0 {
		return 0
	}
	if grant.CreditsTotal <= 0 {
		if !hasGrantPeriodLimits(grant) {
			return 0
		}
		return periodRemaining
	}
	remaining := remainingGrantCredits(grant)
	if remaining <= 0 {
		return 0
	}
	return roundCredits(math.Min(remaining, periodRemaining))
}

func consumableGrantCredits(grant Grant, now time.Time) float64 {
	return availableGrantCredits(grant, now)
}

func hasGrantPeriodLimits(grant Grant) bool {
	limits := grant.PeriodLimits
	return limits.FiveHour > 0 || limits.Daily > 0 || limits.Weekly > 0 || limits.Monthly > 0
}

func availableGrantPeriodCredits(grant Grant, now time.Time) float64 {
	limits := grant.PeriodLimits
	usage := grant.PeriodUsage
	available := math.Inf(1)
	check := func(limit float64, window GrantUsageWindow, start time.Time) {
		if limit <= 0 {
			return
		}
		used := 0.0
		if window.WindowStart.Equal(start) {
			used = window.CreditsUsed
		}
		remain := roundCredits(limit - used)
		if remain < 0 {
			remain = 0
		}
		if remain < available {
			available = remain
		}
	}
	if limits.FiveHour > 0 && grant.RollingFiveHour {
		_, _, used := rollingFiveHourUsage(grant, now)
		remain := roundCredits(limits.FiveHour - used)
		if remain < 0 {
			remain = 0
		}
		if remain < available {
			available = remain
		}
	} else {
		fiveHourStart := grantFiveHourWindowStart(grant, now)
		check(limits.FiveHour, usage.FiveHour, fiveHourStart)
	}
	check(limits.Daily, usage.Daily, grantDayWindowStart(grant, now))
	check(limits.Weekly, usage.Weekly, grantWeekWindowStart(grant, now))
	check(limits.Monthly, usage.Monthly, grantMonthWindowStart(grant, now))
	if math.IsInf(available, 1) {
		return math.MaxFloat64
	}
	return roundCredits(available)
}

func grantPeriodRetryAt(grant Grant, now time.Time) *time.Time {
	limits := grant.PeriodLimits
	usage := grant.PeriodUsage
	var retryAt *time.Time
	check := func(limit float64, window GrantUsageWindow, start, end time.Time) {
		if limit <= 0 || !window.WindowStart.Equal(start) || roundCredits(limit-window.CreditsUsed) > 0 {
			return
		}
		if retryAt == nil || end.After(*retryAt) {
			copyVal := end
			retryAt = &copyVal
		}
	}
	if limits.FiveHour > 0 && grant.RollingFiveHour {
		_, rollingRetryAt, used := rollingFiveHourUsage(grant, now)
		if roundCredits(limits.FiveHour-used) <= 0 && !rollingRetryAt.IsZero() {
			copyVal := rollingRetryAt
			retryAt = &copyVal
		}
	}
	fiveHourStart := grantFiveHourWindowStart(grant, now)
	dayStart := grantDayWindowStart(grant, now)
	weekStart := grantWeekWindowStart(grant, now)
	monthStart := grantMonthWindowStart(grant, now)
	if !grant.RollingFiveHour {
		check(limits.FiveHour, usage.FiveHour, fiveHourStart, fiveHourStart.Add(5*time.Hour))
	}
	check(limits.Daily, usage.Daily, dayStart, dayStart.AddDate(0, 0, 1))
	check(limits.Weekly, usage.Weekly, weekStart, weekStart.AddDate(0, 0, 7))
	check(limits.Monthly, usage.Monthly, monthStart, monthStart.AddDate(0, 1, 0))
	return retryAt
}

func applyGrantPeriodUsage(grant *Grant, credits float64, now time.Time) {
	if grant == nil || credits <= 0 {
		return
	}
	apply := func(limit float64, window *GrantUsageWindow, start time.Time) {
		if limit <= 0 || window == nil {
			return
		}
		if !window.WindowStart.Equal(start) {
			window.WindowStart = start
			window.CreditsUsed = 0
		}
		window.CreditsUsed = roundCredits(window.CreditsUsed + credits)
	}
	if grant.PeriodLimits.FiveHour > 0 && grant.RollingFiveHour {
		grant.UsageEvents = append(grant.UsageEvents, CreditUsageEvent{OccurredAt: now.UTC(), CreditsUsed: roundCredits(credits)})
		pruneRollingFiveHourEvents(grant, now)
	} else {
		fiveHourStart := grantFiveHourWindowStart(*grant, now)
		apply(grant.PeriodLimits.FiveHour, &grant.PeriodUsage.FiveHour, fiveHourStart)
	}
	apply(grant.PeriodLimits.Daily, &grant.PeriodUsage.Daily, grantDayWindowStart(*grant, now))
	apply(grant.PeriodLimits.Weekly, &grant.PeriodUsage.Weekly, grantWeekWindowStart(*grant, now))
	apply(grant.PeriodLimits.Monthly, &grant.PeriodUsage.Monthly, grantMonthWindowStart(*grant, now))
}

func pruneRollingFiveHourEvents(grant *Grant, now time.Time) {
	if grant == nil || len(grant.UsageEvents) == 0 {
		return
	}
	cutoff := now.UTC().Add(-5 * time.Hour)
	kept := grant.UsageEvents[:0]
	for _, event := range grant.UsageEvents {
		if event.OccurredAt.After(cutoff) && event.CreditsUsed > 0 {
			kept = append(kept, event)
		}
	}
	grant.UsageEvents = kept
}

func rollingFiveHourUsage(grant Grant, now time.Time) (time.Time, time.Time, float64) {
	cutoff := now.UTC().Add(-5 * time.Hour)
	used := 0.0
	var earliest time.Time
	for _, event := range grant.UsageEvents {
		if !event.OccurredAt.After(cutoff) || event.CreditsUsed <= 0 {
			continue
		}
		used += event.CreditsUsed
		if earliest.IsZero() || event.OccurredAt.Before(earliest) {
			earliest = event.OccurredAt
		}
	}
	if earliest.IsZero() {
		return cutoff, time.Time{}, roundCredits(used)
	}
	return earliest, earliest.Add(5 * time.Hour), roundCredits(used)
}

func fiveHourWindowStart(t time.Time) time.Time {
	t = t.UTC()
	window := int64((5 * time.Hour) / time.Second)
	return time.Unix((t.Unix()/window)*window, 0).UTC()
}

func grantFiveHourWindowStart(grant Grant, now time.Time) time.Time {
	if grant.AnchoredFiveHour && !grant.RollingFiveHour {
		return anchoredFiveHourWindowStart(grant, now)
	}
	return fiveHourWindowStart(now)
}

// anchoredFiveHourWindowStart returns the current five-hour window for a
// grant whose first-use timestamp is stored in PeriodUsage.FiveHour.WindowStart.
// Before first use, now is used as a provisional start; the first usage write
// persists it as the anchor for all subsequent windows.
func anchoredFiveHourWindowStart(grant Grant, now time.Time) time.Time {
	now = now.UTC()
	anchor := grant.PeriodUsage.FiveHour.WindowStart
	if anchor.IsZero() {
		return now
	}
	anchor = anchor.UTC()
	if now.Before(anchor) {
		return anchor
	}
	periods := int64(now.Sub(anchor) / (5 * time.Hour))
	return anchor.Add(time.Duration(periods) * 5 * time.Hour)
}

func dayWindowStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func weekWindowStart(t time.Time) time.Time {
	start := dayWindowStart(t)
	offset := (int(start.Weekday()) + 6) % 7
	return start.AddDate(0, 0, -offset)
}

func monthWindowStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func HasAnyGrantForServiceGroups(reg *Registry, email string, serviceGroupIDs []string) bool {
	return hasAnyGrantForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs)
}

func hasAnyGrantForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string) bool {
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
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if isNewUserLimitCardSource(grant.Source) && !isActiveNewUserLimitCardPolicyGroup(reg, grant.ServiceGroupID) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; ok {
			return true
		}
	}
	return false
}

func GrantStartAtForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) *time.Time {
	return grantStartAtForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs, now)
}

func GrantStartAtForServiceGroupsForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, now time.Time) *time.Time {
	return grantStartAtForServiceGroups(reg, newUserAccountRef(userID, email), serviceGroupIDs, now)
}

func grantStartAtForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) *time.Time {
	if reg == nil {
		return nil
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if len(serviceGroupIDs) == 0 {
		return nil
	}
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(id)] = struct{}{}
	}
	var startsAt *time.Time
	for _, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if !grant.StartsAt.After(now) || !grantIsValidAt(grant, now) {
			continue
		}
		if grant.CreditsTotal > 0 && remainingGrantCredits(grant) <= 0 {
			continue
		}
		if startsAt == nil || grant.StartsAt.Before(*startsAt) {
			copyVal := grant.StartsAt
			startsAt = &copyVal
		}
	}
	return startsAt
}

func HasActiveGrantForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) bool {
	return hasActiveGrantForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs, now)
}

func hasActiveGrantForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) bool {
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
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !grantIsValidAt(grant, now) {
			continue
		}
		return true
	}
	return false
}
func PeriodLimitRetryAtForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) *time.Time {
	return periodLimitRetryAtForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs, now)
}

func PeriodLimitRetryAtForServiceGroupsForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, now time.Time) *time.Time {
	return periodLimitRetryAtForServiceGroups(reg, newUserAccountRef(userID, email), serviceGroupIDs, now)
}

func periodLimitRetryAtForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) *time.Time {
	if reg == nil {
		return nil
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if len(serviceGroupIDs) == 0 {
		return nil
	}
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(id)] = struct{}{}
	}
	var retryAt *time.Time
	for _, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !grantIsValidAt(grant, now) {
			continue
		}
		if (grant.CreditsTotal > 0 && remainingGrantCredits(grant) <= 0) || availableGrantPeriodCredits(grant, now) > 0 {
			continue
		}
		candidate := grantPeriodRetryAt(grant, now)
		if candidate != nil && candidate.After(now) && (retryAt == nil || candidate.Before(*retryAt)) {
			copyVal := *candidate
			retryAt = &copyVal
		}
	}
	return retryAt
}

func HasUnlimitedActiveGrantForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) bool {
	return hasUnlimitedActiveGrantForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs, now)
}

func hasUnlimitedActiveGrantForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) bool {
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
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !grantIsValidAt(grant, now) {
			continue
		}
		if grant.CreditsTotal > 0 || availableGrantPeriodCredits(grant, now) <= 0 {
			continue
		}
		return true
	}
	return false
}

func HasUnmeteredUnlimitedActiveGrantForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) bool {
	return hasUnmeteredUnlimitedActiveGrantForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs, now)
}

func hasUnmeteredUnlimitedActiveGrantForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) bool {
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
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if grant.Frozen {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !grantIsValidAt(grant, now) {
			continue
		}
		if grant.CreditsTotal <= 0 && !hasGrantPeriodLimits(grant) {
			return true
		}
	}
	return false
}

func BillingEligibilityForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) (bool, string, string, string, float64, bool, bool) {
	return billingEligibilityForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs, now)
}

func BillingEligibilityForServiceGroupsForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, now time.Time) (bool, string, string, string, float64, bool, bool) {
	return billingEligibilityForServiceGroups(reg, newUserAccountRef(userID, email), serviceGroupIDs, now)
}

func billingEligibilityForServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) (bool, string, string, string, float64, bool, bool) {
	if reg == nil {
		return true, AccessPolicyFree, "", "", 0, false, false
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if len(serviceGroupIDs) == 0 {
		return true, AccessPolicyFree, "", "", 0, false, false
	}

	// A provider may belong to several service groups. Any free group that is
	// not currently overlaid by a new-user limit card keeps its normal free
	// access path; it must never be blocked by an exhausted card on a separate
	// group. All remaining groups require grant-backed eligibility.
	if hasUnrestrictedFreeServiceGroup(reg, owner, serviceGroupIDs, now) {
		return true, AccessPolicyFree, "", "", 0, false, false
	}
	grantRequiredGroupIDs := append([]string(nil), serviceGroupIDs...)
	if len(grantRequiredGroupIDs) == 0 {
		return true, AccessPolicyFree, "", "", 0, false, false
	}
	return billingEligibilityForGrantBackedServiceGroups(reg, owner, grantRequiredGroupIDs, now)
}

// hasUnrestrictedFreeServiceGroup reports whether a provider route includes a
// group that should keep its normal no-ledger free behavior for this user. An
// active period-limited welcome card is the only per-user overlay that changes
// that behavior.
func hasUnrestrictedFreeServiceGroup(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) bool {
	if reg == nil {
		return false
	}
	for _, serviceGroupID := range normalizeStringSlice(serviceGroupIDs) {
		// Callers that account usage can operate on a lightweight registry with
		// grants only. Unknown groups in that compatibility path are not evidence
		// of an explicitly configured free route.
		if reg.FindModelServiceGroup(serviceGroupID) != nil && reg.AccessPolicyForServiceGroup(serviceGroupID) == AccessPolicyFree && !hasPeriodLimitedNewUserLimitCardForServiceGroup(reg, owner, serviceGroupID, now) {
			return true
		}
	}
	return false
}

func billingEligibilityForGrantBackedServiceGroups(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) (bool, string, string, string, float64, bool, bool) {
	if hasUnmeteredUnlimitedActiveGrantForServiceGroups(reg, owner, serviceGroupIDs, now) && !hasPeriodLimitedNewUserLimitCardForAnyServiceGroup(reg, owner, serviceGroupIDs, now) {
		return true, AccessPolicyGrantRequired, "", "", 0, true, true
	}
	if hasEarlyStartableUnmeteredUnlimitedGrant(reg, owner, serviceGroupIDs, now) && !hasPeriodLimitedNewUserLimitCardForAnyServiceGroup(reg, owner, serviceGroupIDs, now) {
		return true, AccessPolicyGrantRequired, "", "", 0, true, true
	}
	availableCredits := availableCreditsForServiceGroups(reg, owner, serviceGroupIDs, now)
	if availableCredits > 0 {
		return true, AccessPolicyGrantRequired, "", "", roundCredits(availableCredits), true, true
	}
	hasActiveGrant := hasActiveGrantForServiceGroups(reg, owner, serviceGroupIDs, now)
	if hasActiveGrant {
		if hasUnlimitedActiveGrantForServiceGroups(reg, owner, serviceGroupIDs, now) && !hasPeriodLimitedNewUserLimitCardForAnyServiceGroup(reg, owner, serviceGroupIDs, now) {
			return true, AccessPolicyGrantRequired, "", "", 0, true, true
		}
		if retryAt := periodLimitRetryAtForServiceGroups(reg, owner, serviceGroupIDs, now); retryAt != nil {
			return false, AccessPolicyGrantRequired, "LLM_SERVICE_PERIOD_LIMITED", fmt.Sprintf("current period credit limit is exhausted; try again after %s", retryAt.Format(time.RFC3339)), 0, true, true
		}
		if startsAt := grantStartAtForServiceGroups(reg, owner, serviceGroupIDs, now); startsAt != nil {
			return false, AccessPolicyGrantRequired, "LLM_SERVICE_GRANT_QUEUED", fmt.Sprintf("selected model grant is not active yet; starts at %s", startsAt.Format(time.RFC3339)), 0, true, true
		}
		return false, AccessPolicyGrantRequired, "LLM_SERVICE_CREDITS_EXHAUSTED", "selected model grant credits are exhausted", 0, true, true
	}
	hasAnyGrant := hasAnyGrantForServiceGroups(reg, owner, serviceGroupIDs)
	if hasAnyGrant {
		if startsAt := grantStartAtForServiceGroups(reg, owner, serviceGroupIDs, now); startsAt != nil {
			return false, AccessPolicyGrantRequired, "LLM_SERVICE_GRANT_QUEUED", fmt.Sprintf("selected model grant is not active yet; starts at %s", startsAt.Format(time.RFC3339)), 0, false, true
		}
		return false, AccessPolicyGrantRequired, "LLM_SERVICE_GRANT_EXPIRED", "selected model grant has expired", 0, false, true
	}
	return false, AccessPolicyGrantRequired, "LLM_SERVICE_CREDITS_REQUIRED", "selected model requires a grant-backed service group with remaining credits", 0, false, false
}

func hasNewUserLimitCardForServiceGroup(reg *Registry, owner userAccountRef, serviceGroupID string, now time.Time) bool {
	if reg == nil || owner.empty() || strings.TrimSpace(serviceGroupID) == "" {
		return false
	}
	for _, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) || grant.Frozen || !isNewUserLimitCardSource(grant.Source) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), strings.TrimSpace(serviceGroupID)) {
			continue
		}
		if !now.Before(grant.StartsAt) && grantIsValidAt(grant, now) {
			return true
		}
	}
	return false
}

func hasPeriodLimitedNewUserLimitCardForServiceGroup(reg *Registry, owner userAccountRef, serviceGroupID string, now time.Time) bool {
	if reg == nil || owner.empty() || strings.TrimSpace(serviceGroupID) == "" {
		return false
	}
	for _, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) || grant.Frozen || !isNewUserLimitCardSource(grant.Source) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), strings.TrimSpace(serviceGroupID)) {
			continue
		}
		if !now.Before(grant.StartsAt) && grantIsValidAt(grant, now) && hasGrantPeriodLimits(grant) {
			return true
		}
	}
	return false
}

func hasPeriodLimitedNewUserLimitCardForAnyServiceGroup(reg *Registry, owner userAccountRef, serviceGroupIDs []string, now time.Time) bool {
	for _, serviceGroupID := range serviceGroupIDs {
		if hasPeriodLimitedNewUserLimitCardForServiceGroup(reg, owner, serviceGroupID, now) {
			return true
		}
	}
	return false
}

func isNewUserLimitCardSource(source string) bool {
	return strings.EqualFold(strings.TrimSpace(source), "new_user_limit_card")
}

// skipDuplicateNewUserLimitCardGroup records the first live welcome
// qualification for grant's service group. Later copies of the same policy
// entitlement are skipped so they cannot mint a second allowance, including
// when the first copy's period is already exhausted.
func skipDuplicateNewUserLimitCardGroup(seen map[string]struct{}, reg *Registry, grant Grant) bool {
	if seen == nil || !isNewUserLimitCardSource(grant.Source) || !isActiveNewUserLimitCardPolicyGroup(reg, grant.ServiceGroupID) {
		return false
	}
	groupID := strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))
	if _, duplicate := seen[groupID]; duplicate {
		return true
	}
	seen[groupID] = struct{}{}
	return false
}

func nonWelcomeGrantBlockedByLimitCard(reg *Registry, grant Grant, limitCardGroupSet map[string]struct{}) bool {
	if reg == nil || isNewUserLimitCardSource(grant.Source) {
		return false
	}
	if _, limited := limitCardGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !limited {
		return false
	}
	// Free groups stay exclusive to the welcome overlay so a leftover gift cannot
	// bypass the five-hour/daily cap. Grant-required groups (recharge) consume
	// the welcome period first, then remaining point-card credits.
	return reg.AccessPolicyForServiceGroup(grant.ServiceGroupID) == AccessPolicyFree
}

// activePeriodLimitedNewUserLimitCardGroupSet identifies selected groups whose
// current entitlement is governed by a welcome limit card. Callers use it to
// keep leftover gifts on free groups from increasing the card's spendable
// allowance. Grant-required groups still fall back to point cards after the
// welcome period is exhausted.
func activePeriodLimitedNewUserLimitCardGroupSet(reg *Registry, owner userAccountRef, serviceGroupSet map[string]struct{}, now time.Time) map[string]struct{} {
	if reg == nil || owner.empty() || len(serviceGroupSet) == 0 {
		return nil
	}
	limited := make(map[string]struct{})
	for _, grant := range reg.Grants {
		grant = effectiveGrantForRegistry(reg, grant)
		if !grantMatchesUser(grant, owner) || grant.Frozen || !isNewUserLimitCardSource(grant.Source) {
			continue
		}
		groupID := strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))
		if _, selected := serviceGroupSet[groupID]; !selected {
			continue
		}
		if now.Before(grant.StartsAt) || !grantIsValidAt(grant, now) || !hasGrantPeriodLimits(grant) {
			continue
		}
		limited[groupID] = struct{}{}
	}
	if len(limited) == 0 {
		return nil
	}
	return limited
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
	SelectedModel        string   `json:"selected_model"`
	RequestedGroup       string   `json:"requested_group,omitempty"`
	RequestedModel       string   `json:"requested_model,omitempty"`
	WorkloadClass        string   `json:"workload_class,omitempty"`
	ClassSource          string   `json:"class_source,omitempty"`
	ResolvedModel        string   `json:"resolved_model,omitempty"`
	ResolvedProvider     string   `json:"resolved_provider,omitempty"`
	UpstreamModel        string   `json:"upstream_model,omitempty"`
	OfficialProviderPool string   `json:"official_provider_pool,omitempty"`
	CapabilityNeeds      []string `json:"capability_needs,omitempty"`
	MatchedTags          []string `json:"matched_tags,omitempty"`
	Score                int      `json:"score"`
	Priority             int      `json:"priority,omitempty"`
	ResolutionTier       int      `json:"resolution_tier,omitempty"`
	CreditMultiplier     float64  `json:"credit_multiplier,omitempty"`
	SelectionReason      string   `json:"selection_reason,omitempty"`
}

func (d *ModelSelectionDebug) ApplyAttribution(attr llmpool.RouteAttribution) {
	if d == nil {
		return
	}
	d.RequestedGroup = attr.RequestedGroup
	d.RequestedModel = attr.RequestedModel
	d.WorkloadClass = attr.WorkloadClass
	d.ClassSource = attr.ClassSource
	d.ResolvedModel = attr.ResolvedModel
	d.ResolvedProvider = attr.ResolvedProvider
	d.UpstreamModel = attr.UpstreamModel
	d.OfficialProviderPool = attr.OfficialProviderPool
	if attr.SelectionReason != "" {
		d.SelectionReason = attr.SelectionReason
	}
}

func (d *ModelSelectionDebug) ApplyResolvedProvider(providerID, upstreamModel, officialPool string) {
	if d == nil {
		return
	}
	d.ResolvedProvider = strings.TrimSpace(providerID)
	if name := strings.TrimSpace(upstreamModel); name != "" {
		d.UpstreamModel = name
	}
	if pool := strings.TrimSpace(officialPool); pool != "" {
		d.OfficialProviderPool = pool
	}
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
	capabilityNeeds := llmpool.DetectCapabilityNeeds(body)
	scored := make([]scoredModel, 0, len(models))
	for i, model := range models {
		score := llmpool.CapabilityMatchScore(model.CapabilityTags, capabilityNeeds)
		matchedTags := make([]string, 0, len(capabilityNeeds))
		have := map[string]struct{}{}
		for _, tag := range model.CapabilityTags {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag != "" {
				have[tag] = struct{}{}
			}
		}
		for need := range capabilityNeeds {
			if _, ok := have[need]; ok {
				matchedTags = append(matchedTags, need)
			}
		}
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
