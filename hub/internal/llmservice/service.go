package llmservice

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

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

func ResolveStatusFromRegistryForUser(ctx context.Context, reg *Registry, securitySvc *security.SecurityService, userID, email string, hubBaseURL string) (*ServiceStatus, []AuthorizedModel, error) {
	if reg == nil {
		reg = &Registry{}
	}
	reg.Normalize()
	owner := newUserAccountRef(userID, email)
	email = owner.Email
	now := time.Now().UTC()
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
	status.CreditGrants = creditGrantSummariesForOwner(reg, owner, now)
	for _, g := range status.CreditGrants {
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
		for _, g := range grants {
			creditsAvailable += availableGrantCredits(g, now)
			status.ActiveGrants = append(status.ActiveGrants, grantSummary(g, now))
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
	if len(serviceGroupIDs) > 0 {
		creditsAvailable = availableCreditsForServiceGroups(reg, owner, serviceGroupIDs, now)
	}
	if effectiveExpiresAt := effectiveGrantExpiresAt(reg, owner, now); effectiveExpiresAt != nil {
		status.EffectiveExpiresAt = effectiveExpiresAt.Format(time.RFC3339)
	}
	status.CreditsAvailable = roundCredits(creditsAvailable)
	// Unlimited free path: hide credit meters when nothing is currently
	// spendable on a metered grant (CreditsAvailable==0). If a paid point card
	// is currently spendable (Available>0), keep showing its balance even when
	// a free unlimited gift is also present.
	if status.Active && len(serviceGroupIDs) > 0 && status.CreditsAvailable <= 0 && (hasUnlimitedActiveGrantForServiceGroups(reg, owner, serviceGroupIDs, now) || hasEarlyStartableUnmeteredUnlimitedGrant(reg, owner, serviceGroupIDs, now)) {
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
	var latestExpired *Grant
	for _, g := range reg.Grants {
		if !grantMatchesUser(g, owner) {
			continue
		}
		if reg.FindModelServiceGroup(g.ServiceGroupID) == nil {
			continue
		}
		if !g.ExpiresAt.After(now) {
			if latestExpired == nil || g.ExpiresAt.After(latestExpired.ExpiresAt) {
				copyGrant := g
				latestExpired = &copyGrant
			}
			continue
		}
		items = append(items, grantSummary(g, now))
	}
	if len(items) == 0 && latestExpired != nil {
		items = append(items, grantSummary(*latestExpired, now))
	}
	sort.Slice(items, func(i, j int) bool {
		if rankI, rankJ := grantSummarySortRank(items[i]), grantSummarySortRank(items[j]); rankI != rankJ {
			return rankI < rankJ
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
		fhStart := fiveHourWindowStart(now)
		dStart := dayWindowStart(now)
		wStart := weekWindowStart(now)
		mStart := monthWindowStart(now)
		summary.PeriodUsage = &ActiveGrantPeriodUsage{
			FiveHour: ActiveGrantUsageWindow{
				WindowStart: g.PeriodUsage.FiveHour.WindowStart,
				WindowEnd:   fhStart.Add(5 * time.Hour),
				CreditsUsed: roundCredits(g.PeriodUsage.FiveHour.CreditsUsed),
			},
			Daily: ActiveGrantUsageWindow{
				WindowStart: g.PeriodUsage.Daily.WindowStart,
				WindowEnd:   dStart.AddDate(0, 0, 1),
				CreditsUsed: roundCredits(g.PeriodUsage.Daily.CreditsUsed),
			},
			Weekly: ActiveGrantUsageWindow{
				WindowStart: g.PeriodUsage.Weekly.WindowStart,
				WindowEnd:   wStart.AddDate(0, 0, 7),
				CreditsUsed: roundCredits(g.PeriodUsage.Weekly.CreditsUsed),
			},
			Monthly: ActiveGrantUsageWindow{
				WindowStart: g.PeriodUsage.Monthly.WindowStart,
				WindowEnd:   mStart.AddDate(0, 1, 0),
				CreditsUsed: roundCredits(g.PeriodUsage.Monthly.CreditsUsed),
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
	if !now.Before(g.ExpiresAt) {
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
		if !strings.EqualFold(strings.TrimSpace(grant.ServiceGroupID), serviceGroupID) {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
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
		if !grantMatchesUser(g, owner) || !strings.EqualFold(g.ServiceGroupID, serviceGroupID) {
			continue
		}
		// Only consider grants currently within their validity window.
		if now.Before(g.StartsAt) || !now.Before(g.ExpiresAt) {
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
		if !grantMatchesUser(g, owner) {
			continue
		}
		if reg.FindModelServiceGroup(g.ServiceGroupID) == nil {
			continue
		}
		if g.CreditsTotal > 0 && remainingGrantCredits(g) <= 0 {
			continue
		}
		if !g.ExpiresAt.After(now) {
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
		if !now.Before(g.ExpiresAt) {
			continue
		}
		appendID(g.ServiceGroupID)
		if now.Before(g.StartsAt) {
			continue
		}
		if status, _, active, _ := grantStatus(g, now); !active || status != "active" {
			continue
		}
		activeGrants = append(activeGrants, g)
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

// BuildAuthorizedModelsForServiceGroups returns the API-facing model summaries
// for service groups using the same merge and provider metadata rules as
// ResolveStatusFromRegistry.
func BuildAuthorizedModelsForServiceGroups(reg *Registry, serviceGroupIDs []string) ([]AuthorizedModel, string) {
	return buildAuthorizedModels(reg, serviceGroupIDs)
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
	return GrantDefaultServiceForNewUserID(ctx, system, "", email)
}

func GrantDefaultServiceForNewUserID(ctx context.Context, system SystemSettingsRepository, userID, email string) error {
	return grantNewUserBenefitForUserID(ctx, system, userID, email, "new_user_default", 0.30, false)
}

func GrantEmailConfirmedBenefitForUser(ctx context.Context, system SystemSettingsRepository, email string) error {
	return GrantEmailConfirmedBenefitForUserID(ctx, system, "", email)
}

func GrantEmailConfirmedBenefitForUserID(ctx context.Context, system SystemSettingsRepository, userID, email string) error {
	return grantNewUserBenefitForUserID(ctx, system, userID, email, "new_user_email_confirmed", 0.70, true)
}

func GrantPhoneVerifiedBenefitForUser(ctx context.Context, system SystemSettingsRepository, email string) error {
	return GrantPhoneVerifiedBenefitForUserID(ctx, system, "", email)
}

func GrantPhoneVerifiedBenefitForUserID(ctx context.Context, system SystemSettingsRepository, userID, email string) error {
	return grantNewUserBenefitForUserID(ctx, system, userID, email, "new_user_phone_verified", 0.70, true)
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

func grantNewUserBenefit(ctx context.Context, system SystemSettingsRepository, email, source string, ratio float64, useRegistrationWindow bool) error {
	return grantNewUserBenefitForUserID(ctx, system, "", email, source, ratio, useRegistrationWindow)
}

func grantNewUserBenefitForUserID(ctx context.Context, system SystemSettingsRepository, userID, email, source string, ratio float64, useRegistrationWindow bool) error {
	owner := newUserAccountRef(userID, email)
	email = owner.Email
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

func ApplyCreditUsageToRegistry(reg *Registry, email string, serviceGroupIDs []string, credits float64, now time.Time) float64 {
	return applyCreditUsageToRegistry(reg, newUserAccountRef("", email), serviceGroupIDs, credits, now)
}

func ApplyCreditUsageToRegistryForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, credits float64, now time.Time) float64 {
	return applyCreditUsageToRegistry(reg, newUserAccountRef(userID, email), serviceGroupIDs, credits, now)
}

func applyCreditUsageToRegistry(reg *Registry, owner userAccountRef, serviceGroupIDs []string, credits float64, now time.Time) float64 {
	if reg == nil || credits <= 0 {
		return 0
	}
	serviceGroupIDs = normalizeStringSlice(serviceGroupIDs)
	if owner.empty() || len(serviceGroupIDs) == 0 {
		return 0
	}
	if hasUnmeteredUnlimitedActiveGrantForServiceGroups(reg, owner, serviceGroupIDs, now) {
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
	serviceGroupSet := map[string]struct{}{}
	for _, id := range serviceGroupIDs {
		serviceGroupSet[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	}
	for i, grant := range reg.Grants {
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if !now.Before(grant.ExpiresAt) {
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
		if consumableGrantCredits(candidateGrant, now) <= 0 {
			continue
		}
		candidates = append(candidates, candidate{idx: i, g: candidateGrant, earlyStart: earlyStart})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].earlyStart != candidates[j].earlyStart {
			return !candidates[i].earlyStart
		}
		if candidates[i].g.ExpiresAt.Equal(candidates[j].g.ExpiresAt) {
			return candidates[i].g.CreatedAt.Before(candidates[j].g.CreatedAt)
		}
		return candidates[i].g.ExpiresAt.Before(candidates[j].g.ExpiresAt)
	})
	remaining := credits
	consumed := 0.0
	for _, cand := range candidates {
		grant := &reg.Grants[cand.idx]
		if cand.earlyStart {
			shiftGrantToEarlyStart(grant, now)
		}
		available := consumableGrantCredits(*grant, now)
		if available <= 0 {
			continue
		}
		use := math.Min(available, remaining)
		grant.CreditsUsed = roundCredits(grant.CreditsUsed + use)
		applyGrantPeriodUsage(grant, use, now)
		consumed += use
		remaining -= use
		if remaining <= 0 {
			break
		}
	}
	return roundCredits(consumed)
}

func AvailableCreditsForServiceGroups(reg *Registry, email string, serviceGroupIDs []string, now time.Time) float64 {
	return availableCreditsForServiceGroups(reg, newUserAccountRef("", email), serviceGroupIDs, now)
}

func AvailableCreditsForServiceGroupsForUserID(reg *Registry, userID, email string, serviceGroupIDs []string, now time.Time) float64 {
	return availableCreditsForServiceGroups(reg, newUserAccountRef(userID, email), serviceGroupIDs, now)
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
	total := 0.0
	for i, grant := range reg.Grants {
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if !now.Before(grant.ExpiresAt) {
			continue
		}
		if now.Before(grant.StartsAt) {
			if !canEarlyStartQueuedGrant(reg, owner, grant, i, serviceGroupSet, now) {
				continue
			}
			grant = grantWithEarlyStartWindow(grant, now)
		}
		total += availableGrantCredits(grant, now)
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
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if grant.CreditsTotal > 0 || hasGrantPeriodLimits(grant) || !grant.StartsAt.After(now) || !grant.ExpiresAt.After(now) {
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
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
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
	if reg == nil || !queued.StartsAt.After(now) || !queued.ExpiresAt.After(now) {
		return false
	}
	if queued.CreditsTotal > 0 && remainingGrantCredits(queued) <= 0 {
		return false
	}
	blockedByExhausted := false
	blockedByPeriodLimit := false
	for _, grant := range reg.Grants {
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
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
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if !grant.StartsAt.After(now) || !grant.ExpiresAt.After(now) {
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
	check(limits.FiveHour, usage.FiveHour, fiveHourWindowStart(now))
	check(limits.Daily, usage.Daily, dayWindowStart(now))
	check(limits.Weekly, usage.Weekly, weekWindowStart(now))
	check(limits.Monthly, usage.Monthly, monthWindowStart(now))
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
	fiveHourStart := fiveHourWindowStart(now)
	dayStart := dayWindowStart(now)
	weekStart := weekWindowStart(now)
	monthStart := monthWindowStart(now)
	check(limits.FiveHour, usage.FiveHour, fiveHourStart, fiveHourStart.Add(5*time.Hour))
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
	apply(grant.PeriodLimits.FiveHour, &grant.PeriodUsage.FiveHour, fiveHourWindowStart(now))
	apply(grant.PeriodLimits.Daily, &grant.PeriodUsage.Daily, dayWindowStart(now))
	apply(grant.PeriodLimits.Weekly, &grant.PeriodUsage.Weekly, weekWindowStart(now))
	apply(grant.PeriodLimits.Monthly, &grant.PeriodUsage.Monthly, monthWindowStart(now))
}

func fiveHourWindowStart(t time.Time) time.Time {
	t = t.UTC()
	window := int64((5 * time.Hour) / time.Second)
	return time.Unix((t.Unix()/window)*window, 0).UTC()
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
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if !grant.StartsAt.After(now) || !grant.ExpiresAt.After(now) {
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
		if !grantMatchesUser(grant, owner) {
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
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
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
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
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
		if !grantMatchesUser(grant, owner) {
			continue
		}
		if _, ok := serviceGroupSet[strings.ToLower(strings.TrimSpace(grant.ServiceGroupID))]; !ok {
			continue
		}
		if now.Before(grant.StartsAt) || !now.Before(grant.ExpiresAt) {
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
	if hasUnmeteredUnlimitedActiveGrantForServiceGroups(reg, owner, grantRequiredGroupIDs, now) {
		return true, AccessPolicyGrantRequired, "", "", 0, true, true
	}
	if hasEarlyStartableUnmeteredUnlimitedGrant(reg, owner, grantRequiredGroupIDs, now) {
		return true, AccessPolicyGrantRequired, "", "", 0, true, true
	}
	availableCredits := availableCreditsForServiceGroups(reg, owner, grantRequiredGroupIDs, now)
	if availableCredits > 0 {
		return true, AccessPolicyGrantRequired, "", "", roundCredits(availableCredits), true, true
	}
	hasActiveGrant := hasActiveGrantForServiceGroups(reg, owner, grantRequiredGroupIDs, now)
	if hasActiveGrant {
		if hasUnlimitedActiveGrantForServiceGroups(reg, owner, grantRequiredGroupIDs, now) {
			return true, AccessPolicyGrantRequired, "", "", 0, true, true
		}
		if retryAt := periodLimitRetryAtForServiceGroups(reg, owner, grantRequiredGroupIDs, now); retryAt != nil {
			return false, AccessPolicyGrantRequired, "LLM_SERVICE_PERIOD_LIMITED", fmt.Sprintf("current period credit limit is exhausted; try again after %s", retryAt.Format(time.RFC3339)), 0, true, true
		}
		if startsAt := grantStartAtForServiceGroups(reg, owner, grantRequiredGroupIDs, now); startsAt != nil {
			return false, AccessPolicyGrantRequired, "LLM_SERVICE_GRANT_QUEUED", fmt.Sprintf("selected model grant is not active yet; starts at %s", startsAt.Format(time.RFC3339)), 0, true, true
		}
		return false, AccessPolicyGrantRequired, "LLM_SERVICE_CREDITS_EXHAUSTED", "selected model grant credits are exhausted", 0, true, true
	}
	hasAnyGrant := hasAnyGrantForServiceGroups(reg, owner, grantRequiredGroupIDs)
	if hasAnyGrant {
		if startsAt := grantStartAtForServiceGroups(reg, owner, grantRequiredGroupIDs, now); startsAt != nil {
			return false, AccessPolicyGrantRequired, "LLM_SERVICE_GRANT_QUEUED", fmt.Sprintf("selected model grant is not active yet; starts at %s", startsAt.Format(time.RFC3339)), 0, false, true
		}
		return false, AccessPolicyGrantRequired, "LLM_SERVICE_GRANT_EXPIRED", "selected model grant has expired", 0, false, true
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
