package llmservice

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

const memberHealthWindow = 50

// memberOutcome is one finished upstream dial kept for the health window.
type memberOutcome struct {
	status  int
	latency time.Duration
	err     string
}

type memberCounter struct {
	minuteStart         time.Time
	minuteCount         int
	dayStart            time.Time
	dayRequests         int
	dayInput            int64
	dayOutput           int64
	outcomes            []memberOutcome
	lastError           string
	lastErrorAt         time.Time
	consecutiveFailures int
}

var memberRuntimeStore = struct {
	sync.Mutex
	byID map[string]*memberCounter
}{byID: map[string]*memberCounter{}}

var arrayMemberWRR = llmpool.NewWRRScheduler()

func resetMemberRuntime() {
	memberRuntimeStore.Lock()
	memberRuntimeStore.byID = map[string]*memberCounter{}
	memberRuntimeStore.Unlock()
}

func memberCounterFor(id string) *memberCounter {
	id = providerIDKey(id)
	if id == "" {
		return nil
	}
	if memberRuntimeStore.byID == nil {
		memberRuntimeStore.byID = map[string]*memberCounter{}
	}
	counter := memberRuntimeStore.byID[id]
	if counter == nil {
		counter = &memberCounter{}
		memberRuntimeStore.byID[id] = counter
	}
	return counter
}

// memberCounterPeek reads a counter without creating one. Callers hold memberRuntimeStore.
func memberCounterPeek(id string) *memberCounter {
	id = providerIDKey(id)
	if id == "" || memberRuntimeStore.byID == nil {
		return nil
	}
	return memberRuntimeStore.byID[id]
}

func memberLocation(provider *llmpool.ProviderConfig) *time.Location {
	name := llmpool.DefaultCreditMultiplierTimezone
	if provider != nil && strings.TrimSpace(provider.Timezone) != "" {
		name = strings.TrimSpace(provider.Timezone)
	}
	loc, err := time.LoadLocation(name)
	if err != nil || loc == nil {
		loc, err = time.LoadLocation(llmpool.DefaultCreditMultiplierTimezone)
	}
	if err != nil || loc == nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func memberQuotaWindows(provider *llmpool.ProviderConfig, now time.Time) (minute, day time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	loc := memberLocation(provider)
	local := now.In(loc)
	minute = time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, loc)
	day = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	return minute, day
}

func (c *memberCounter) roll(minute, day time.Time) {
	if c == nil {
		return
	}
	if !c.minuteStart.Equal(minute) {
		c.minuteStart = minute
		c.minuteCount = 0
	}
	if !c.dayStart.Equal(day) {
		c.dayStart = day
		c.dayRequests = 0
		c.dayInput = 0
		c.dayOutput = 0
	}
}

// memberQuotaBlocked reports that the member should not be dialed. until is
// when the blocking window ends.
func memberQuotaBlocked(provider *llmpool.ProviderConfig, now time.Time) (until time.Time, blocked bool) {
	if provider == nil || (provider.RequestsPerMinute <= 0 && provider.RequestsPerDay <= 0) {
		return time.Time{}, false
	}
	minute, day := memberQuotaWindows(provider, now)
	memberRuntimeStore.Lock()
	defer memberRuntimeStore.Unlock()
	counter := memberCounterPeek(provider.ID)
	if counter == nil {
		return time.Time{}, false
	}
	counter.roll(minute, day)
	return memberQuotaDeadline(provider, counter, minute, day)
}

// memberQuotaDeadline reports the later active cap. A member that has used
// both its minute and its day budget stays blocked until the day ends.
func memberQuotaDeadline(provider *llmpool.ProviderConfig, counter *memberCounter, minute, day time.Time) (time.Time, bool) {
	if provider == nil || counter == nil {
		return time.Time{}, false
	}
	var until time.Time
	if provider.RequestsPerMinute > 0 && counter.minuteCount >= provider.RequestsPerMinute {
		until = nextMemberWindow(minute, true)
	}
	if provider.RequestsPerDay > 0 && counter.dayRequests >= provider.RequestsPerDay {
		dayUntil := nextMemberWindow(day, false)
		if dayUntil.After(until) {
			until = dayUntil
		}
	}
	return until, !until.IsZero()
}

// nextMemberWindow is the next clock minute or the next local midnight.
// Adding a fixed 24 hours misses a daylight-saving change.
func nextMemberWindow(start time.Time, minute bool) time.Time {
	if minute {
		return time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), start.Minute()+1, 0, 0, start.Location())
	}
	return time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 0, 0, start.Location())
}

// memberQuotaLease is one admitted dial. Release only returns that dial's
// own minute and day, so a cancel after the window rolls does not free a
// later request.
type memberQuotaLease struct {
	id     string
	minute time.Time
	day    time.Time
	held   bool
}

// admitMemberQuota counts one upstream dial when the member is still under its
// cap. A concurrent caller that loses the race gets blocked instead.
func admitMemberQuota(provider *llmpool.ProviderConfig, now time.Time) (memberQuotaLease, time.Time, bool) {
	if provider == nil {
		return memberQuotaLease{}, time.Time{}, false
	}
	minute, day := memberQuotaWindows(provider, now)
	memberRuntimeStore.Lock()
	defer memberRuntimeStore.Unlock()
	counter := memberCounterFor(provider.ID)
	if counter == nil {
		return memberQuotaLease{}, time.Time{}, false
	}
	counter.roll(minute, day)
	if until, blocked := memberQuotaDeadline(provider, counter, minute, day); blocked {
		return memberQuotaLease{}, until, true
	}
	counter.minuteCount++
	counter.dayRequests++
	return memberQuotaLease{id: providerIDKey(provider.ID), minute: minute, day: day, held: true}, time.Time{}, false
}

// releaseMemberQuota returns one admit that never received an upstream
// response, so a canceled dial does not consume a free-tier cap.
func releaseMemberQuota(lease memberQuotaLease) {
	if !lease.held || lease.id == "" {
		return
	}
	memberRuntimeStore.Lock()
	defer memberRuntimeStore.Unlock()
	counter := memberCounterPeek(lease.id)
	if counter == nil {
		return
	}
	if counter.minuteStart.Equal(lease.minute) && counter.minuteCount > 0 {
		counter.minuteCount--
	}
	if counter.dayStart.Equal(lease.day) && counter.dayRequests > 0 {
		counter.dayRequests--
	}
}

func noteMemberAttempt(providerID string, status int, latency time.Duration, errMsg, secret string) {
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return
	}
	errMsg = trimMemberError(RedactConfiguredSecret(errMsg, secret))
	failures := 0
	func() {
		memberRuntimeStore.Lock()
		defer memberRuntimeStore.Unlock()
		counter := memberCounterFor(providerID)
		if counter == nil {
			return
		}
		counter.outcomes = append(counter.outcomes, memberOutcome{status: status, latency: latency, err: errMsg})
		if extra := len(counter.outcomes) - memberHealthWindow; extra > 0 {
			copy(counter.outcomes, counter.outcomes[extra:])
			counter.outcomes = counter.outcomes[:memberHealthWindow]
		}
		if errMsg != "" {
			counter.lastError = errMsg
			counter.lastErrorAt = time.Now().UTC()
		}
		switch {
		case memberAttemptSucceeded(status, errMsg):
			counter.consecutiveFailures = 0
		case memberAttemptFailed(status, errMsg):
			counter.consecutiveFailures++
			failures = counter.consecutiveFailures
		}
	}()
	recordMemberHealthAttempt(providerID, status, latency, errMsg)
	if failures > 0 {
		noteTokenBankAutoPause(providerID, status, errMsg, failures)
	}
}

func noteMemberTokens(providerID string, inputTokens, outputTokens int64) {
	if inputTokens == 0 && outputTokens == 0 {
		return
	}
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return
	}
	func() {
		memberRuntimeStore.Lock()
		defer memberRuntimeStore.Unlock()
		counter := memberCounterFor(providerID)
		if counter == nil {
			return
		}
		counter.rollTo(time.Now())
		if inputTokens > 0 {
			counter.dayInput += inputTokens
		}
		if outputTokens > 0 {
			counter.dayOutput += outputTokens
		}
	}()
	recordMemberHealthTokens(providerID, inputTokens, outputTokens)
}

// rollTo moves the day and minute windows before tokens are added, so a dial
// that finishes after midnight is counted on the new day.
func (c *memberCounter) rollTo(now time.Time) {
	if c == nil {
		return
	}
	loc := time.FixedZone("CST", 8*3600)
	if !c.dayStart.IsZero() && c.dayStart.Location() != nil {
		loc = c.dayStart.Location()
	}
	local := now.In(loc)
	c.roll(
		time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, loc),
		time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc),
	)
}

var memberErrorURL = regexp.MustCompile(`https?://[^\s]+`)

// RedactConfiguredSecret hides a stored upstream key that an error or reply echoed back.
func RedactConfiguredSecret(msg, secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) >= 8 && strings.Contains(msg, secret) {
		msg = strings.ReplaceAll(msg, secret, "****")
	}
	return msg
}

// RedactStatusError removes credentials embedded in an upstream error before
// it is returned to an automation client.
func RedactStatusError(msg string) string {
	return trimMemberError(msg)
}

// RedactStatusText strips credentials from a probe reply without shortening it.
func RedactStatusText(msg string) string {
	return redactMemberError(msg)
}

func redactMemberError(msg string) string {
	return memberErrorURL.ReplaceAllStringFunc(msg, func(raw string) string {
		trimmed := strings.TrimRight(raw, ".,;:)]}>\"'")
		suffix := raw[len(trimmed):]
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" {
			return "http://redacted" + suffix
		}
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath() + suffix
	})
}

func trimMemberError(msg string) string {
	msg = redactMemberError(msg)
	msg = strings.Join(strings.Fields(strings.TrimSpace(msg)), " ")
	if msg == "" {
		return ""
	}
	if utf8.RuneCountInString(msg) <= 240 {
		return msg
	}
	runes := []rune(msg)
	return string(runes[:240])
}

// MemberHealth is the process-local view a caller can poll.
type MemberHealth struct {
	WindowRequests    int       `json:"window_requests"`
	SuccessRate       float64   `json:"success_rate"`
	Status429         int       `json:"status_429"`
	Status5xx         int       `json:"status_5xx"`
	AvgLatencyMs      int64     `json:"avg_latency_ms"`
	LastError         string    `json:"last_error,omitempty"`
	LastErrorAt       time.Time `json:"last_error_at,omitempty"`
	TodayRequests     int       `json:"today_requests"`
	TodayInputTokens  int64     `json:"today_input_tokens"`
	TodayOutputTokens int64     `json:"today_output_tokens"`
	MinuteRequests    int       `json:"minute_requests"`
	CooldownUntil     time.Time `json:"cooldown_until,omitempty"`
	QuotaUntil        time.Time `json:"quota_until,omitempty"`
}

func memberHealthSnapshot(provider *llmpool.ProviderConfig, now time.Time) MemberHealth {
	health := MemberHealth{}
	if provider == nil {
		return health
	}
	if until := arrayMemberCooldownUntil(provider.ID); !until.IsZero() && until.After(now) {
		health.CooldownUntil = until.UTC()
	}
	minute, day := memberQuotaWindows(provider, now)
	memberRuntimeStore.Lock()
	defer memberRuntimeStore.Unlock()
	counter := memberCounterPeek(provider.ID)
	if counter == nil {
		return health
	}
	counter.roll(minute, day)
	if until, blocked := memberQuotaDeadline(provider, counter, minute, day); blocked {
		health.QuotaUntil = until.UTC()
	}
	health.TodayRequests = counter.dayRequests
	health.TodayInputTokens = counter.dayInput
	health.TodayOutputTokens = counter.dayOutput
	health.MinuteRequests = counter.minuteCount
	health.LastError = counter.lastError
	health.LastErrorAt = counter.lastErrorAt
	if len(counter.outcomes) == 0 {
		return health
	}
	var latency time.Duration
	ok := 0
	for _, item := range counter.outcomes {
		health.WindowRequests++
		latency += item.latency
		if item.status >= 200 && item.status < 400 {
			ok++
		}
		if item.status == 429 {
			health.Status429++
		}
		if item.status >= 500 {
			health.Status5xx++
		}
	}
	health.SuccessRate = math.Round(float64(ok)/float64(health.WindowRequests)*10000) / 10000
	if health.WindowRequests > 0 {
		health.AvgLatencyMs = latency.Milliseconds() / int64(health.WindowRequests)
	}
	return health
}

func withoutQuotaBlockedMembers(members []*llmpool.ProviderConfig, now time.Time) []*llmpool.ProviderConfig {
	if len(members) == 0 {
		return members
	}
	out := make([]*llmpool.ProviderConfig, 0, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}
		if _, blocked := memberQuotaBlocked(member, now); blocked {
			continue
		}
		out = append(out, member)
	}
	return out
}

func arrayRouteQuotaBlocked(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool) bool {
	if accept == nil {
		accept = acceptLiveProvider
	}
	_, _, members := lookupProviderArray(reg, routeProviderID, accept)
	if len(members) == 0 {
		return false
	}
	now := time.Now()
	saw := false
	for _, member := range members {
		if member == nil || arrayMemberCooling(member.ID) {
			continue
		}
		saw = true
		if _, blocked := memberQuotaBlocked(member, now); !blocked {
			return false
		}
	}
	return saw
}

func memberQuotaError(member *llmpool.ProviderConfig, until time.Time) error {
	id := ""
	if member != nil {
		id = strings.TrimSpace(member.ID)
	}
	return fmt.Errorf("provider %s is over its request quota until %s", id, until.UTC().Format(time.RFC3339))
}

func arrayMembersUseWeight(members []*llmpool.ProviderConfig) bool {
	seen := -1
	for _, member := range members {
		if member == nil {
			continue
		}
		if member.DispatchWeight > 1 {
			return true
		}
		weight := effectiveDispatchWeight(member)
		if seen < 0 {
			seen = weight
			continue
		}
		if weight != seen {
			return true
		}
	}
	return false
}

// effectiveDispatchWeight scales a Token Bank member by its recent success
// rate. Other members keep the weight stored on the provider. Fewer than five
// samples leaves the stored weight alone so a new share is not punished.
func effectiveDispatchWeight(member *llmpool.ProviderConfig) int {
	weight := 1
	if member != nil && member.DispatchWeight > 0 {
		weight = member.DispatchWeight
	}
	if member == nil || !IsTokenBankMemberID(member.ID) {
		return weight
	}
	health := memberHealthSnapshot(member, time.Now())
	if health.WindowRequests < 5 {
		return weight
	}
	scaled := int(math.Round(100 * health.SuccessRate))
	if health.AvgLatencyMs > 5000 && scaled > 1 {
		scaled /= 2
	}
	if scaled < 1 {
		scaled = 1
	}
	return scaled * weight
}

// orderArrayMembers keeps the historical round-robin when every member has an
// equal share. A dispatch_weight above 1 uses smooth weighted round-robin and
// puts the chosen member first so failover still walks the rest.
func orderArrayMembers(arrayID string, members []*llmpool.ProviderConfig) []*llmpool.ProviderConfig {
	if len(members) <= 1 || !arrayMembersUseWeight(members) {
		return rotateProviderArray(arrayID, members)
	}
	wrrMembers := make([]llmpool.WRRMember, 0, len(members))
	for i, member := range members {
		if member == nil || strings.TrimSpace(member.ID) == "" {
			continue
		}
		wrrMembers = append(wrrMembers, llmpool.WRRMember{ID: member.ID, Weight: effectiveDispatchWeight(member), Sequence: i})
	}
	pick := arrayMemberWRR.Next("array:"+strings.TrimSpace(arrayID), wrrMembers)
	if pick == "" {
		return members
	}
	var first *llmpool.ProviderConfig
	rest := make([]*llmpool.ProviderConfig, 0, len(members))
	for _, member := range members {
		if first == nil && member != nil && member.ID == pick {
			first = member
			continue
		}
		rest = append(rest, member)
	}
	if first == nil {
		return members
	}
	return append([]*llmpool.ProviderConfig{first}, rest...)
}

func lookupModelMap(provider *llmpool.ProviderConfig, name string) string {
	name = strings.TrimSpace(name)
	if provider == nil || name == "" || len(provider.ModelMap) == 0 {
		return ""
	}
	if value, ok := provider.ModelMap[name]; ok {
		return strings.TrimSpace(value)
	}
	for key, value := range provider.ModelMap {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// UpstreamModelForMember rewrites a public model name through the member map.
func UpstreamModelForMember(provider *llmpool.ProviderConfig, publicModel string) string {
	publicModel = strings.TrimSpace(publicModel)
	if mapped := lookupModelMap(provider, publicModel); mapped != "" {
		return mapped
	}
	return publicModel
}

// chatTestUpstreamModel is the model name an admin status test sends.
// A service-group test passes the route name and the client band separately.
// auto and official-* only choose a price. A provider Test Status call leaves
// both empty and passes the catalog model it selected. That name is kept,
// after the member model_map, so the button still dials the model it shows.
func chatTestUpstreamModel(member *llmpool.ProviderConfig, routeModel, logicalModel, requestedModel string) string {
	routeModel = strings.TrimSpace(routeModel)
	logicalModel = strings.TrimSpace(logicalModel)
	requestedModel = strings.TrimSpace(requestedModel)
	if routeModel == "" && logicalModel == "" {
		if requestedModel == "" && member != nil && len(member.Models) > 0 {
			requestedModel = strings.TrimSpace(member.Models[0])
		}
		if !isBillingBandName(requestedModel) {
			if mapped := UpstreamModelForMember(member, requestedModel); mapped != "" {
				return mapped
			}
			return requestedModel
		}
		logicalModel = requestedModel
	}
	return memberUpstreamModel(member, routeModel, logicalModel, "")
}

// memberUpstreamModel is the model name sent to this one member.
// auto/mid/low/high only choose the billing group. They are not upstream
// model ids. A band request uses this member's model_map for that band, or
// the model configured on the member. A name written on the route is used
// only when this member actually lists it.
func memberUpstreamModel(member *llmpool.ProviderConfig, routeModel, logicalModel, fallback string) string {
	logicalModel = strings.TrimSpace(logicalModel)
	routeModel = strings.TrimSpace(routeModel)
	fallback = strings.TrimSpace(fallback)
	// A bank member serves only the model in its id. The generic fallback
	// below sends a member's own catalog entry when the route names something
	// else. That is right for a normal array and wrong here: the tier array
	// mixes models, and this response would be the other model.
	if served, ok := tokenBankServedModel(member); ok {
		if tokenBankRequestNamesMatch(served, logicalModel, routeModel, fallback) {
			return served
		}
		return ""
	}
	if mapped := lookupMemberBandModel(member, logicalModel); mapped != "" && !isBillingBandName(mapped) {
		return mapped
	}
	if !isBillingBandName(logicalModel) && routeModel != "" && !strings.EqualFold(routeModel, logicalModel) {
		if mapped := lookupModelMap(member, routeModel); mapped != "" && !isBillingBandName(mapped) {
			return mapped
		}
	}
	configured := memberConfiguredModels(member)
	if isBillingBandName(logicalModel) {
		if picked := configuredModel(configured, routeModel); picked != "" {
			return picked
		}
		if len(configured) > 0 {
			return configured[0]
		}
		// No catalog on this member. A concrete name on the route is the
		// upstream id. If the route has none, dial with the logical model.
		// Skipping here would drop providers that were never given a model list.
		if name := nonBandName(routeModel, fallback); name != "" {
			return name
		}
		// Nothing concrete is configured. Existing providers are still dialed
		// with the logical model.
		return logicalModel
	}
	pinned := routeModel
	if pinned == "" {
		pinned = fallback
	}
	if pinned != "" && !isBillingBandName(pinned) && (configuredModel(configured, pinned) != "" || len(configured) == 0) {
		return pinned
	}
	if picked := configuredModel(configured, logicalModel); picked != "" {
		return picked
	}
	// Several configured models and the route name is not one of them.
	// Send this member's own model. A foreign pin is not an upstream id.
	if len(configured) > 0 {
		return configured[0]
	}
	if !isBillingBandName(logicalModel) {
		return logicalModel
	}
	return ""
}

// tokenBankRequestNamesMatch reports whether a bank member's model is one of
// the concrete names on this request. Band names are not model ids. When the
// request names no concrete model, the member's own model is the upstream id.
func tokenBankRequestNamesMatch(served string, names ...string) bool {
	sawConcrete := false
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || isBillingBandName(name) {
			continue
		}
		sawConcrete = true
		if strings.EqualFold(served, name) {
			return true
		}
	}
	return !sawConcrete
}

// isBillingBandName reports a client billing id. auto/mid/low/high select a
// group and a price. official-* is the same id after canonicalization.
func isBillingBandName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	canon := llmpool.CanonicalClientModel(name)
	return strings.EqualFold(canon, "auto") || llmpool.IsOfficialTierName(canon)
}

func lookupMemberBandModel(member *llmpool.ProviderConfig, name string) string {
	if mapped := nonBandMappedModel(member, name); mapped != "" {
		return mapped
	}
	if !isBillingBandName(name) {
		return ""
	}
	canon := llmpool.CanonicalClientModel(name)
	for _, alias := range billingBandAliases(canon) {
		if strings.EqualFold(alias, name) {
			continue
		}
		if mapped := nonBandMappedModel(member, alias); mapped != "" {
			return mapped
		}
	}
	return ""
}

func nonBandMappedModel(member *llmpool.ProviderConfig, name string) string {
	mapped := lookupModelMap(member, name)
	if mapped == "" || isBillingBandName(mapped) {
		return ""
	}
	return mapped
}

func billingBandAliases(canon string) []string {
	switch canon {
	case "auto":
		return []string{"auto", "default"}
	case llmpool.OfficialTierLow:
		return []string{llmpool.OfficialTierLow, "low"}
	case llmpool.OfficialTierMid:
		return []string{llmpool.OfficialTierMid, "mid"}
	case llmpool.OfficialTierHigh:
		return []string{llmpool.OfficialTierHigh, "high"}
	default:
		return nil
	}
}

func memberConfiguredModels(member *llmpool.ProviderConfig) []string {
	if member == nil {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, model := range member.Models {
		model = strings.TrimSpace(model)
		if model == "" || isBillingBandName(model) {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}
	return out
}

func configuredModel(configured []string, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || isBillingBandName(name) {
		return ""
	}
	for _, model := range configured {
		if strings.EqualFold(model, name) {
			return model
		}
	}
	return ""
}

func nonBandName(names ...string) string {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" && !isBillingBandName(name) {
			return name
		}
	}
	return ""
}

func memberRateLimitPause(member *llmpool.ProviderConfig) time.Duration {
	if member != nil && member.RateLimitCooldownSec > 0 {
		return time.Duration(member.RateLimitCooldownSec) * time.Second
	}
	pause := time.Duration(proxyRateLimitCooldownMS) * time.Millisecond
	if member != nil && member.CircuitBreakerCooldownMS > 0 {
		if configured := time.Duration(member.CircuitBreakerCooldownMS) * time.Millisecond; configured > pause {
			pause = configured
		}
	}
	return pause
}

const (
	maxDispatchWeight       = 10000
	maxRequestsPerMinute    = 100000
	maxRequestsPerDay       = 1000000
	maxRateLimitCooldownSec = 86400
	maxModelMapEntries      = 32
	maxModelMapEntryRunes   = 128
)

func validateMemberPolicy(provider *llmpool.ProviderConfig) error {
	if provider == nil {
		return nil
	}
	if provider.DispatchWeight < 0 || provider.DispatchWeight > maxDispatchWeight {
		return fmt.Errorf("dispatch_weight must be between 0 and %d", maxDispatchWeight)
	}
	if provider.RequestsPerMinute < 0 || provider.RequestsPerMinute > maxRequestsPerMinute {
		return fmt.Errorf("requests_per_minute must be between 0 and %d", maxRequestsPerMinute)
	}
	if provider.RequestsPerDay < 0 || provider.RequestsPerDay > maxRequestsPerDay {
		return fmt.Errorf("requests_per_day must be between 0 and %d", maxRequestsPerDay)
	}
	if provider.RateLimitCooldownSec < 0 || provider.RateLimitCooldownSec > maxRateLimitCooldownSec {
		return fmt.Errorf("rate_limit_cooldown_sec must be between 0 and %d", maxRateLimitCooldownSec)
	}
	if provider.ModelMap == nil {
		return nil
	}
	if len(provider.ModelMap) > maxModelMapEntries {
		return fmt.Errorf("model_map has too many entries")
	}
	cleaned := make(map[string]string, len(provider.ModelMap))
	seen := map[string]struct{}{}
	for key, value := range provider.ModelMap {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			return fmt.Errorf("model_map keys and values must be non-empty")
		}
		if utf8.RuneCountInString(key) > maxModelMapEntryRunes || utf8.RuneCountInString(value) > maxModelMapEntryRunes {
			return fmt.Errorf("model_map entry is too long")
		}
		folded := strings.ToLower(key)
		if _, ok := seen[folded]; ok {
			return fmt.Errorf("model_map key %s is duplicated", key)
		}
		seen[folded] = struct{}{}
		cleaned[key] = value
	}
	provider.ModelMap = cleaned
	return nil
}
