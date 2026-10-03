package llmservice

import (
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

const (
	// TokenBankCanaryPercent is the share of requests that try a new model first.
	TokenBankCanaryPercent = 5
	// TokenBankCanaryHealthyRate is the success rate that ends the limit after
	// the canary deadline. Below it, and with at least five samples, the member
	// stays on the 5% path.
	TokenBankCanaryHealthyRate = 0.8
	// TokenBankCanaryWindow is the default length of that limited path.
	// Token bank settings canary_window_hours is what a new publish actually uses.
	TokenBankCanaryWindow = 24 * time.Hour
)

// TokenBankCanaryDeadline is the default window after the model was first published.
func TokenBankCanaryDeadline(published time.Time) time.Time {
	return TokenBankCanaryDeadlineAfter(published, TokenBankCanaryWindow)
}

// TokenBankCanaryDeadlineAfter is window after published.
// A zero published time or a non-positive window means no canary: the model
// is a full member immediately.
func TokenBankCanaryDeadlineAfter(published time.Time, window time.Duration) time.Time {
	if published.IsZero() || window <= 0 {
		return time.Time{}
	}
	return published.UTC().Add(window)
}

// tokenBankCachedMemberAllowed applies the private-share audience to a cache
// hit. The cache key is the service group, model, and body, so a completion
// stored for one tenant would otherwise be returned to a tenant the member
// does not allow. The dial window does not apply: a hit does not call the
// shared key, and skipping it would dial an in-window sibling instead.
// Public members and ordinary providers stay cacheable.
func tokenBankCachedMemberAllowed(provider *llmpool.ProviderConfig, req *ProxyRequest) bool {
	if provider == nil || !IsTokenBankMemberID(provider.ID) {
		return true
	}
	hubID, tenantID := "", ""
	if req != nil {
		hubID = req.HubID
		tenantID = req.TenantID
	}
	return TokenBankAudienceAllows(provider.TokenBankVisibility, provider.TokenBankAudiences, hubID, tenantID)
}

// tokenBankMemberServing reports whether this member may answer now.
// Ordinary providers always may. A bank member must match its audience and
// its share window. An empty window is always open.
func tokenBankMemberServing(provider *llmpool.ProviderConfig, hubID, tenantID string, now time.Time) bool {
	if provider == nil || !IsTokenBankMemberID(provider.ID) {
		return true
	}
	if !llmpool.TokenBankShareWindowAllows(provider.TokenBankShareWindow, now) {
		return false
	}
	return TokenBankAudienceAllows(provider.TokenBankVisibility, provider.TokenBankAudiences, hubID, tenantID)
}

// TokenBankAudienceAllows reports whether this request may use the member.
// Public and empty visibility allow everyone. A private member needs one
// audience row whose set ids all match.
func TokenBankAudienceAllows(visibility string, audiences []llmpool.TokenBankAudience, hubID, tenantID string) bool {
	if !strings.EqualFold(strings.TrimSpace(visibility), "private") {
		return true
	}
	hubID = strings.TrimSpace(hubID)
	tenantID = strings.TrimSpace(tenantID)
	for _, item := range audiences {
		hub := strings.TrimSpace(item.HubID)
		tenant := strings.TrimSpace(item.TenantID)
		if hub == "" && tenant == "" {
			continue
		}
		if hub != "" && !strings.EqualFold(hub, hubID) {
			continue
		}
		if tenant != "" && !strings.EqualFold(tenant, tenantID) {
			continue
		}
		return true
	}
	return false
}

// TokenBankCanaryHit is sticky for one bucket (the request id). The same id
// always lands on the same side of the 5% cut.
func TokenBankCanaryHit(bucket string) bool {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return false
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(bucket))
	return int(h.Sum32()%100) < TokenBankCanaryPercent
}

func tokenBankMemberInCanary(member *llmpool.ProviderConfig, now time.Time) bool {
	if member == nil || !IsTokenBankMemberID(member.ID) {
		return false
	}
	untilRaw := strings.TrimSpace(member.TokenBankCanaryUntil)
	if untilRaw == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, untilRaw)
	if err != nil {
		return false
	}
	if now.Before(until) {
		return true
	}
	health := memberHealthSnapshot(member, now)
	if health.WindowRequests < 5 {
		return false
	}
	return health.SuccessRate < TokenBankCanaryHealthyRate
}

// quotedTokenBankMemberID is the member a claimed quote priced. Empty when
// this request has no quote or the quote is for an ordinary provider.
func quotedTokenBankMemberID(req *ProxyRequest) string {
	if req == nil || req.Quote == nil {
		return ""
	}
	id := strings.TrimSpace(req.Quote.MemberID)
	if !IsTokenBankMemberID(id) {
		return ""
	}
	return id
}

// restrictToQuotedTokenBankMember keeps only the member the quote priced.
// A different member of the tier has a different price, so it cannot fill in.
func restrictToQuotedTokenBankMember(members []*llmpool.ProviderConfig, memberID string) []*llmpool.ProviderConfig {
	memberID = strings.TrimSpace(memberID)
	if memberID == "" {
		return members
	}
	for _, member := range members {
		if member != nil && member.ID == memberID {
			return []*llmpool.ProviderConfig{member}
		}
	}
	return nil
}

// tokenBankRouteMembers is who can answer this route for this request.
// A claimed quote dials the member it priced and does not take another
// rotation turn. A new quote for a token-bank tier uses the same candidate
// list a live dial uses, including one rotation turn, so the frozen price is
// that member. Ordinary arrays still do not move their cursor on a quote.
func tokenBankRouteMembers(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool, model string, req *ProxyRequest, rotate bool) (string, *llmpool.ProviderConfig, []*llmpool.ProviderConfig) {
	quoted := quotedTokenBankMemberID(req)
	bucket := ""
	if req != nil {
		bucket = req.RequestID
	}
	var logicalID string
	var profile *llmpool.ProviderConfig
	var members []*llmpool.ProviderConfig
	if quoted != "" || !rotate {
		logicalID, profile, members = lookupProviderArray(reg, routeProviderID, accept)
		if quoted == "" && !rotate && membersIncludeTokenBank(members) {
			logicalID, profile, members = arrayEgressForBucket(reg, routeProviderID, accept, model, bucket)
		}
	} else {
		logicalID, profile, members = arrayEgressForBucket(reg, routeProviderID, accept, model, bucket)
	}
	members = prepareTokenBankRequestMembers(members, req, model)
	if quoted != "" {
		members = restrictToQuotedTokenBankMember(members, quoted)
	}
	return logicalID, profileForTokenBankDial(profile, members), members
}

// profileForTokenBankDial is the provider whose price represents this dial.
// An ordinary array keeps its existing profile. A token-bank tier uses the
// member at the front of the dial list, which is the one the quote or the
// rotation selected.
func profileForTokenBankDial(profile *llmpool.ProviderConfig, members []*llmpool.ProviderConfig) *llmpool.ProviderConfig {
	if len(members) == 0 || members[0] == nil {
		return profile
	}
	if !membersIncludeTokenBank(members) {
		if profile != nil {
			return profile
		}
		copied := *members[0]
		return &copied
	}
	copied := *members[0]
	return &copied
}

func prepareTokenBankRequestMembers(members []*llmpool.ProviderConfig, req *ProxyRequest, model string) []*llmpool.ProviderConfig {
	hubID, tenantID, bucket := "", "", ""
	if req != nil {
		hubID = req.HubID
		tenantID = req.TenantID
		bucket = req.RequestID
	}
	return PrepareTokenBankEgress(tokenBankMembersForModel(members, model), hubID, tenantID, bucket, time.Now())
}

// tokenBankMembersForModel keeps non-bank members and bank members that can
// answer this request. A tier array holds every model of that tier. Another
// model's member is not supply for a concrete model name: treating it as a
// canary sibling hides the only real member from the other 95% of traffic,
// and dialing it would answer with the other model.
//
// official-low, official-mid, official-high, and auto are billing bands, not
// model ids. A band route is how that tier takes traffic. Every member stays,
// and memberUpstreamModel dials the model encoded in that member's id.
func tokenBankMembersForModel(members []*llmpool.ProviderConfig, model string) []*llmpool.ProviderConfig {
	model = strings.TrimSpace(model)
	if model == "" || len(members) == 0 || isBillingBandName(model) {
		return members
	}
	out := make([]*llmpool.ProviderConfig, 0, len(members))
	for _, member := range members {
		if member == nil {
			out = append(out, member)
			continue
		}
		served, ok := tokenBankServedModel(member)
		if ok && !strings.EqualFold(served, model) {
			continue
		}
		out = append(out, member)
	}
	return out
}

// withoutClosedShareWindows drops bank members whose dial window is closed.
// Call it before rotation. A closed member is not dialed, so it must not
// consume a round-robin turn that belongs to an open sibling. Ordinary
// providers and an empty window stay. PrepareTokenBankEgress repeats this
// check for the path that does not rotate.
func withoutClosedShareWindows(members []*llmpool.ProviderConfig, now time.Time) []*llmpool.ProviderConfig {
	if len(members) == 0 {
		return members
	}
	live := make([]*llmpool.ProviderConfig, 0, len(members))
	closed := false
	for _, member := range members {
		if member != nil && IsTokenBankMemberID(member.ID) && !llmpool.TokenBankShareWindowAllows(member.TokenBankShareWindow, now) {
			closed = true
			continue
		}
		live = append(live, member)
	}
	if !closed {
		return members
	}
	if len(live) == 0 {
		return nil
	}
	return live
}

// arrayRouteShareWindowClosed reports that this caller could dial a token-bank
// member of this model after cooling and quota, and every such member is
// outside its dial window. A private member this hub or tenant cannot use does
// not count: naming the window would publish that share's hours. A missing
// provider, a non-bank member, and an open sibling are not this case.
func arrayRouteShareWindowClosed(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool, model, hubID, tenantID string) bool {
	if accept == nil {
		accept = acceptLiveProvider
	}
	_, _, members := lookupProviderArray(reg, routeProviderID, accept)
	members = withoutCoolingArrayMembers(members)
	members = withoutQuotaBlockedMembers(members, time.Now())
	members = tokenBankMembersForModel(members, model)
	if len(members) == 0 {
		return false
	}
	now := time.Now()
	sawClosed := false
	for _, member := range members {
		if member == nil || !IsTokenBankMemberID(member.ID) {
			return false
		}
		if !TokenBankAudienceAllows(member.TokenBankVisibility, member.TokenBankAudiences, hubID, tenantID) {
			continue
		}
		if llmpool.TokenBankShareWindowAllows(member.TokenBankShareWindow, now) {
			return false
		}
		sawClosed = true
	}
	return sawClosed
}

// arrayRouteModelPaused reports that this model has members and every one of
// them is paused. The array profile is the first member of the tier, which
// may be a different model. Using that pause bit would hide a closed share
// window behind "paused".
func arrayRouteModelPaused(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool, model string) bool {
	if accept == nil {
		accept = acceptLiveProvider
	}
	_, _, members := lookupProviderArray(reg, routeProviderID, func(provider *llmpool.ProviderConfig) bool {
		if provider == nil {
			return false
		}
		if provider.Paused {
			return true
		}
		return accept(provider)
	})
	members = tokenBankMembersForModel(members, model)
	if len(members) == 0 {
		return false
	}
	for _, member := range members {
		if member == nil || !member.Paused {
			return false
		}
	}
	return true
}

// dispatchOrderKey isolates a bank model's rotation from the other models
// that share its tier array. A normal array keeps the array id, including
// when the request named a model, so its existing cursor is left alone.
func dispatchOrderKey(arrayID, model string, members []*llmpool.ProviderConfig) string {
	arrayID = strings.TrimSpace(arrayID)
	if !membersIncludeTokenBank(members) {
		return arrayID
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || isBillingBandName(model) {
		return arrayID
	}
	return arrayID + "\x00" + model
}

func membersIncludeTokenBank(members []*llmpool.ProviderConfig) bool {
	for _, member := range members {
		if member != nil && IsTokenBankMemberID(member.ID) {
			return true
		}
	}
	return false
}

// tokenBankServedModel is the model encoded in a bank member id. Settlement
// and dispatch both use that model, not the caller's logical name.
func tokenBankServedModel(member *llmpool.ProviderConfig) (string, bool) {
	if member == nil || !IsTokenBankMemberID(member.ID) {
		return "", false
	}
	_, model, ok := ParseTokenBankMemberID(member.ID)
	model = strings.TrimSpace(model)
	if !ok || model == "" {
		return "", false
	}
	return model, true
}

// orderTokenBankReady rotates only members this request will actually dial.
// A canary is absent from 95% of requests, so leaving it in the rotated list
// makes the cursor land on it and then skip the next stable sibling. A canary
// hit is served first and does not advance that cursor. An array with no
// canary, or whose only supply is still in canary, keeps one shared rotation.
func orderTokenBankReady(logicalID, model, bucket string, ready []*llmpool.ProviderConfig, now time.Time) []*llmpool.ProviderConfig {
	if !membersIncludeTokenBank(ready) {
		return orderArrayMembers(dispatchOrderKey(logicalID, model, ready), ready)
	}
	canary := make([]*llmpool.ProviderConfig, 0, len(ready))
	rest := make([]*llmpool.ProviderConfig, 0, len(ready))
	for _, member := range ready {
		if tokenBankMemberInCanary(member, now) {
			canary = append(canary, member)
			continue
		}
		rest = append(rest, member)
	}
	if len(canary) == 0 || len(rest) == 0 {
		return orderArrayMembers(dispatchOrderKey(logicalID, model, ready), ready)
	}
	if TokenBankCanaryHit(bucket) {
		return append(canary, rest...)
	}
	return orderArrayMembers(dispatchOrderKey(logicalID, model, rest), rest)
}

// PrepareTokenBankEgress drops private members this request cannot use and
// members whose share window is closed, then puts canary members first only
// for the 5% bucket. When siblings exist, the other 95% never dial the canary.
// A canary that is the only member still serves, because there is no other
// supply to protect. A closed window drops the member even when it is the
// only one: the owner asked for that model to stay idle outside the cheap hours.
func PrepareTokenBankEgress(members []*llmpool.ProviderConfig, hubID, tenantID, bucket string, now time.Time) []*llmpool.ProviderConfig {
	if len(members) == 0 {
		return members
	}
	filtered := make([]*llmpool.ProviderConfig, 0, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}
		if !tokenBankMemberServing(member, hubID, tenantID, now) {
			continue
		}
		filtered = append(filtered, member)
	}
	var canary, rest []*llmpool.ProviderConfig
	for _, member := range filtered {
		if tokenBankMemberInCanary(member, now) {
			canary = append(canary, member)
			continue
		}
		rest = append(rest, member)
	}
	if len(canary) == 0 {
		return filtered
	}
	if TokenBankCanaryHit(bucket) {
		return append(canary, rest...)
	}
	if len(rest) == 0 {
		return canary
	}
	return rest
}

var tokenBankKeySeq sync.Map

func tokenBankKeyCounter(id string) *atomic.Uint64 {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "_"
	}
	if existing, ok := tokenBankKeySeq.Load(id); ok {
		return existing.(*atomic.Uint64)
	}
	fresh := &atomic.Uint64{}
	actual, _ := tokenBankKeySeq.LoadOrStore(id, fresh)
	return actual.(*atomic.Uint64)
}

// RotateTokenBankKey returns a copy whose APIKey is the next key in the
// rotation. One key, or a non-bank member, is returned unchanged. The stored
// member is not mutated.
func RotateTokenBankKey(member *llmpool.ProviderConfig) *llmpool.ProviderConfig {
	if member == nil || !IsTokenBankMemberID(member.ID) {
		return member
	}
	keys := make([]string, 0, 1+len(member.TokenBankExtraKeys))
	seen := map[string]struct{}{}
	add := func(key string) {
		key = strings.TrimSpace(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	add(member.APIKey)
	for _, key := range member.TokenBankExtraKeys {
		add(key)
	}
	if len(keys) <= 1 {
		return member
	}
	n := tokenBankKeyCounter(member.ID).Add(1)
	copied := *member
	// The copy must not share the registry slices. A later append on the
	// attempt would otherwise rewrite the member every request sees.
	copied.TokenBankExtraKeys = append([]string(nil), member.TokenBankExtraKeys...)
	copied.TokenBankAudiences = append([]llmpool.TokenBankAudience(nil), member.TokenBankAudiences...)
	copied.TokenBankShareWindow.Days = append([]int(nil), member.TokenBankShareWindow.Days...)
	copied.APIKey = keys[int(n%uint64(len(keys)))]
	return &copied
}

// redactConfiguredError removes configured keys from err.Error while keeping
// the original error on the unwrap chain. A message that contains no key is
// returned as-is.
func redactConfiguredError(member *llmpool.ProviderConfig, err error) error {
	if err == nil || member == nil {
		return err
	}
	raw := err.Error()
	text := RedactProviderSecrets(raw, member)
	if text == raw {
		return err
	}
	return &tokenBankRedactedError{text: text, err: err}
}

type tokenBankRedactedError struct {
	text string
	err  error
}

func (e *tokenBankRedactedError) Error() string { return e.text }

func (e *tokenBankRedactedError) Unwrap() error { return e.err }

// RedactProviderSecrets hides the primary key and every extra key an error might echo.
func RedactProviderSecrets(msg string, provider *llmpool.ProviderConfig) string {
	if provider == nil {
		return msg
	}
	msg = RedactConfiguredSecret(msg, provider.APIKey)
	for _, key := range provider.TokenBankExtraKeys {
		msg = RedactConfiguredSecret(msg, key)
	}
	return msg
}
