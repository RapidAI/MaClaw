package httpapi

import (
	"context"
	"math"
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

// systemFreeBotBilling marks a request that must not consume the system-free
// new-user benefit. Maclaw GUI calls leave this unset, so the benefit still
// applies there. A matched bot token also carries the credits cap from bot
// management; a limit of zero means that cap is not applied.
type systemFreeBotBilling struct {
	skipNewUserLimit bool
	tenantID         string
	ownerID          string
	limit            float64
	used             float64
	add              func(ctx context.Context, tenantID, ownerID, requestID string, credits float64) error
}

type systemFreeBotBillingKey struct{}

type systemFreeBotBiller interface {
	LookupBotLLMToken(ctx context.Context, tenantID, rawToken string) (botmgmt.SystemFreeBotMatch, bool)
	AddSystemFreeCreditsUsed(ctx context.Context, tenantID, ownerID, requestID string, credits float64) error
}

// botSystemFreeSettle is copied onto a credit charge so settlement can skip
// the new-user card and, when a cap is configured, add the price to the bot budget.
type botSystemFreeSettle struct {
	skipGrant bool
	tenantID  string
	ownerID   string
	limit     float64
	add       func(ctx context.Context, tenantID, ownerID, requestID string, credits float64) error
}

func withSystemFreeBotBilling(ctx context.Context, billing systemFreeBotBilling) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, systemFreeBotBillingKey{}, billing)
}

func systemFreeBotBillingFrom(ctx context.Context) (systemFreeBotBilling, bool) {
	if ctx == nil {
		return systemFreeBotBilling{}, false
	}
	billing, ok := ctx.Value(systemFreeBotBillingKey{}).(systemFreeBotBilling)
	if !ok || !billing.skipNewUserLimit {
		return systemFreeBotBilling{}, false
	}
	return billing, true
}

func systemFreeBotBillerFrom(sources []any) systemFreeBotBiller {
	for _, source := range sources {
		biller, ok := source.(systemFreeBotBiller)
		if ok && biller != nil {
			return biller
		}
	}
	return nil
}

// attachSystemFreeBotBilling records an explicit system-free pin. Official
// bot turns do not send that pin, so they keep the GUI grant order, including
// every card on the owner's official entitlement. The desktop app does not
// send the pin either.
func attachSystemFreeBotBilling(ctx context.Context, r *http.Request, principal *auth.ViewerPrincipal, sources []any) context.Context {
	if principal == nil || principal.IsLLMEndpointAPIKey() || r == nil {
		return ctx
	}
	if !llmservice.IsSystemFreeServiceGroup(r.Header.Get(llmpool.ServiceGroupIDHeader)) {
		return ctx
	}
	var match botmgmt.SystemFreeBotMatch
	matched := false
	var add func(ctx context.Context, tenantID, ownerID, requestID string, credits float64) error
	if biller := systemFreeBotBillerFrom(sources); biller != nil {
		match, matched = biller.LookupBotLLMToken(ctx, principal.TenantID, extractBearerToken(r))
		add = biller.AddSystemFreeCreditsUsed
	}
	billing := systemFreeBotBilling{
		skipNewUserLimit: true,
		tenantID:         principal.TenantID,
		add:              add,
	}
	if matched {
		billing.ownerID = match.HubUserID
		billing.limit = match.Limit
		billing.used = match.Used
	}
	return withSystemFreeBotBilling(ctx, billing)
}

func systemFreeBotBudgetForGroups(ctx context.Context, groups []string) (systemFreeBotBilling, bool) {
	billing, ok := systemFreeBotBillingFrom(ctx)
	if !ok || !groupsAreSystemFree(groups) {
		return systemFreeBotBilling{}, false
	}
	return billing, true
}

func groupsAreSystemFree(groups []string) bool {
	if len(groups) == 0 {
		return false
	}
	for _, id := range groups {
		if !llmservice.IsSystemFreeServiceGroup(id) {
			return false
		}
	}
	return true
}

func botSystemFreeSettleFrom(ctx context.Context, groups []string) botSystemFreeSettle {
	billing, ok := systemFreeBotBudgetForGroups(ctx, groups)
	if !ok {
		return botSystemFreeSettle{}
	}
	return botSystemFreeSettle{
		skipGrant: true,
		tenantID:  billing.tenantID,
		ownerID:   billing.ownerID,
		limit:     billing.limit,
		add:       billing.add,
	}
}

func systemFreeBotRemaining(billing systemFreeBotBilling) float64 {
	remaining := billing.limit - billing.used
	if math.IsNaN(remaining) || math.IsInf(remaining, 0) || remaining < 0 {
		return 0
	}
	return math.Round(remaining*1000) / 1000
}

func systemFreeBotCreditsExhausted() llmBillingDenial {
	return llmBillingDenial{
		Code:    "LLM_SERVICE_CREDITS_EXHAUSTED",
		Message: "bot system-free credits limit is exhausted",
	}
}

func registryForSystemFreeBot(ctx context.Context, reg *llmservice.Registry, userID, email string, groups []string) *llmservice.Registry {
	if _, ok := systemFreeBotBudgetForGroups(ctx, groups); !ok || reg == nil {
		return reg
	}
	return llmservice.RegistryIgnoringSystemFreeNewUserLimit(reg, userID, email)
}
