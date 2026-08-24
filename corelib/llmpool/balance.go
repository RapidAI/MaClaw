package llmpool

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProviderLBInput is the card-side view of a provider used to compute LB groups.
type ProviderLBInput struct {
	ID      string
	Paused  bool
	Billing ProviderBillingPolicy
}

// ProviderLBAnnotation is attached to admin list payloads.
type ProviderLBAnnotation struct {
	ProviderID        string  `json:"provider_id,omitempty"`
	CurrentMultiplier float64 `json:"current_multiplier"`
	LBGroup           string  `json:"lb_group,omitempty"`
	LBGroupSize       int     `json:"lb_group_size,omitempty"`
	LBEligible        bool    `json:"lb_eligible,omitempty"`
}

// ProviderDispatchMeta is the runtime view of a provider used by Balance.
type ProviderDispatchMeta struct {
	ID             string
	Sequence       int
	MaxConcurrency int
	Billing        ProviderBillingPolicy
	SkipWRR        bool
}

// BalanceCandidate is one scored route plus the provider facts needed to group it.
type BalanceCandidate struct {
	Route               DispatchProviderRoute
	Score               int
	ResolutionTier      int
	EffectiveMultiplier float64
	Sequence            int
	MaxConcurrency      int
	// SkipWRR keeps the candidate in failover order (paused / circuit-open)
	// but excludes it from the WRR pick set.
	SkipWRR bool
}

// BalancedRoute is a dispatch route after multiplier grouping and WRR rotation.
type BalancedRoute struct {
	Route       DispatchProviderRoute
	BandKey     string
	FirstInBand bool
	SkipWRR     bool
}

// WRRMember is one unique provider inside a score band.
type WRRMember struct {
	ID       string
	Weight   int
	Sequence int
}

const maxWRRGroupStates = 512

type wrrState struct {
	fingerprint   string
	currentWeight map[string]int
	lastUsed      uint64
}

// WRRScheduler is a process-local nginx smooth weighted round-robin.
type WRRScheduler struct {
	mu     sync.Mutex
	clock  uint64
	groups map[string]*wrrState
}

func NewWRRScheduler() *WRRScheduler {
	return &WRRScheduler{groups: map[string]*wrrState{}}
}

func (s *WRRScheduler) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.groups = map[string]*wrrState{}
	s.clock = 0
	s.mu.Unlock()
}

func (s *WRRScheduler) forgetIfMembershipChanged(groupKey string, members []WRRMember) {
	if s == nil {
		return
	}
	fp := wrrFingerprint(members)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groups == nil {
		return
	}
	state := s.groups[groupKey]
	if state != nil && state.fingerprint != fp {
		delete(s.groups, groupKey)
	}
}

// Next returns the next provider ID for groupKey. A single member is returned
// as-is. Equal current weights break toward the smaller sequence.
func (s *WRRScheduler) Next(groupKey string, members []WRRMember) string {
	members = normalizeWRRMembers(members)
	if len(members) == 0 {
		return ""
	}
	if s == nil {
		return members[0].ID
	}
	if len(members) == 1 {
		s.forgetIfMembershipChanged(groupKey, members)
		return members[0].ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groups == nil {
		s.groups = map[string]*wrrState{}
	}
	fp := wrrFingerprint(members)
	state := s.groups[groupKey]
	if state == nil || state.fingerprint != fp {
		state = &wrrState{fingerprint: fp, currentWeight: map[string]int{}}
		s.groups[groupKey] = state
	}
	s.clock++
	state.lastUsed = s.clock
	if len(s.groups) > maxWRRGroupStates {
		evictOldestWRRState(s.groups, groupKey)
	}
	total := 0
	for _, member := range members {
		total += member.Weight
		state.currentWeight[member.ID] += member.Weight
	}
	best := members[0]
	bestWeight := state.currentWeight[best.ID]
	for _, member := range members[1:] {
		cw := state.currentWeight[member.ID]
		if cw > bestWeight || (cw == bestWeight && member.Sequence < best.Sequence) {
			best = member
			bestWeight = cw
		}
	}
	state.currentWeight[best.ID] -= total
	return best.ID
}

// LBGroupKey formats a multiplier as the admin badge id, for example "x1" or "x0.5".
func LBGroupKey(multiplier float64) string {
	return "x" + FormatCreditMultiplierHeader(NormalizeCreditMultiplier(multiplier))
}

// AnnotateProviderLBGroups groups providers by the vendor multiplier in effect at now.
// Paused providers still count toward group size and keep the badge, but are not eligible.
func AnnotateProviderLBGroups(providers []ProviderLBInput, now time.Time) []ProviderLBAnnotation {
	if len(providers) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	out := make([]ProviderLBAnnotation, len(providers))
	counts := map[string]int{}
	for i, provider := range providers {
		current := ResolveCreditMultiplier(provider.Billing, now)
		key := LBGroupKey(current)
		out[i] = ProviderLBAnnotation{
			ProviderID:        strings.TrimSpace(provider.ID),
			CurrentMultiplier: current,
			LBGroup:           key,
		}
		counts[key]++
	}
	for i := range out {
		size := counts[out[i].LBGroup]
		out[i].LBGroupSize = size
		if size < 2 {
			out[i].LBGroup = ""
			out[i].LBEligible = false
			continue
		}
		out[i].LBEligible = !providers[i].Paused
	}
	return out
}

func ProviderLBInputFromConfig(p ProviderConfig) ProviderLBInput {
	return ProviderLBInput{ID: p.ID, Paused: p.Paused, Billing: p.BillingPolicy()}
}

func MetaFromProvider(p ProviderConfig) ProviderDispatchMeta {
	return ProviderDispatchMeta{
		ID:             p.ID,
		Sequence:       p.Sequence,
		MaxConcurrency: p.MaxConcurrency,
		Billing:        p.BillingPolicy(),
		SkipWRR:        p.Paused,
	}
}

func EffectiveRouteMultiplier(meta ProviderDispatchMeta, route DispatchProviderRoute, now time.Time) float64 {
	vendor := ResolveCreditMultiplier(meta.Billing, now)
	return CombineCreditMultipliers(vendor, NormalizeCreditMultiplier(route.CreditMultiplier))
}

// BalanceProviderRoutes groups candidates by effective multiplier (cheap first),
// then by score band, then rotates the first provider in each band with WRR.
// Members of a multiplier band are equal-weight: concurrency is only used to
// skip a full member, not to claim a larger share. Remaining members of a
// band follow sequence order, not ring order.
func BalanceProviderRoutes(sched *WRRScheduler, pool string, candidates []BalanceCandidate) []BalancedRoute {
	if len(candidates) == 0 {
		return nil
	}
	type band struct {
		key        string
		multiplier float64
		score      int
		tier       int
		items      []BalanceCandidate
	}
	bands := make([]*band, 0, len(candidates))
	index := map[string]*band{}
	for _, candidate := range candidates {
		candidate.Route.ProviderID = strings.TrimSpace(candidate.Route.ProviderID)
		if candidate.Route.ProviderID == "" {
			continue
		}
		candidate.EffectiveMultiplier = NormalizeCreditMultiplier(candidate.EffectiveMultiplier)
		candidate.ResolutionTier = normalizedResolutionTier(candidate.ResolutionTier)
		key := bandKey(candidate)
		item, ok := index[key]
		if !ok {
			item = &band{
				key:        key,
				multiplier: candidate.EffectiveMultiplier,
				score:      candidate.Score,
				tier:       candidate.ResolutionTier,
			}
			index[key] = item
			bands = append(bands, item)
		}
		item.items = append(item.items, candidate)
	}
	sort.SliceStable(bands, func(i, j int) bool {
		if bands[i].multiplier != bands[j].multiplier {
			return bands[i].multiplier < bands[j].multiplier
		}
		if bands[i].score != bands[j].score {
			return bands[i].score > bands[j].score
		}
		if bands[i].tier != bands[j].tier {
			return bands[i].tier < bands[j].tier
		}
		return bands[i].key < bands[j].key
	})
	out := make([]BalancedRoute, 0, len(candidates))
	for _, item := range bands {
		out = append(out, rotateBand(sched, pool, item.key, item.items)...)
	}
	return out
}

// ShouldSkipFullBandMember reports whether a full provider should be skipped
// because a later same-band sibling can still accept work, or because this is a
// failover member and only the WRR winner may queue.
// atCapacity must be true for any provider that cannot accept work now
// (at concurrency limit, circuit-open, or backing off).
// Later SkipWRR siblings (paused, circuit-open, or request-ineligible) do not
// count as capacity; the winner should queue rather than fail over to them.
// A current SkipWRR member is only skipped when a later eligible sibling can
// accept work, so circuit-open failover can still reach BeforeAttempt.
func ShouldSkipFullBandMember(routes []BalancedRoute, index int, atCapacity func(providerID string) bool) bool {
	if index < 0 || index >= len(routes) || atCapacity == nil {
		return false
	}
	id := strings.TrimSpace(routes[index].Route.ProviderID)
	if id == "" {
		return false
	}
	if routes[index].SkipWRR {
		// Circuit-open / ineligible members stay in failover order so
		// BeforeAttempt can still issue a half-open probe. Only skip them
		// when a later eligible sibling can take the work now.
		return laterEligibleBandMemberHasCapacity(routes, index, atCapacity)
	}
	if !atCapacity(id) {
		return false
	}
	if laterEligibleBandMemberHasCapacity(routes, index, atCapacity) {
		return true
	}
	return !routes[index].FirstInBand
}

func laterEligibleBandMemberHasCapacity(routes []BalancedRoute, index int, atCapacity func(providerID string) bool) bool {
	id := strings.TrimSpace(routes[index].Route.ProviderID)
	band := routes[index].BandKey
	for _, later := range routes[index+1:] {
		if later.BandKey != band || later.SkipWRR {
			continue
		}
		laterID := strings.TrimSpace(later.Route.ProviderID)
		if laterID == "" || laterID == id {
			continue
		}
		if !atCapacity(laterID) {
			return true
		}
	}
	return false
}

func rotateBand(sched *WRRScheduler, pool, key string, items []BalanceCandidate) []BalancedRoute {
	if len(items) == 0 {
		return nil
	}
	members := bandWRRMembers(items)
	pick := ""
	if len(members) > 0 {
		if sched != nil {
			pick = sched.Next(wrrGroupKey(pool, key), members)
		}
		if pick == "" {
			pick = members[0].ID
		}
	} else {
		pick = firstSequenceProvider(items)
	}
	byProvider := map[string][]BalanceCandidate{}
	order := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		id := strings.TrimSpace(item.Route.ProviderID)
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			order = append(order, id)
		}
		byProvider[id] = append(byProvider[id], item)
	}
	emitOrder := make([]string, 0, len(order))
	if _, ok := byProvider[pick]; ok {
		emitOrder = append(emitOrder, pick)
	}
	rest := make([]string, 0, len(order))
	for _, id := range order {
		if id == pick {
			continue
		}
		rest = append(rest, id)
	}
	sort.SliceStable(rest, func(i, j int) bool {
		si := bandSequence(byProvider[rest[i]])
		sj := bandSequence(byProvider[rest[j]])
		if si != sj {
			return si < sj
		}
		return rest[i] < rest[j]
	})
	emitOrder = append(emitOrder, rest...)
	out := make([]BalancedRoute, 0, len(items))
	first := true
	for _, id := range emitOrder {
		for _, item := range byProvider[id] {
			out = append(out, BalancedRoute{Route: item.Route, BandKey: key, FirstInBand: first, SkipWRR: item.SkipWRR})
			first = false
		}
	}
	return out
}

func bandWRRMembers(items []BalanceCandidate) []WRRMember {
	seen := map[string]WRRMember{}
	order := make([]string, 0, len(items))
	for _, item := range items {
		if item.SkipWRR {
			continue
		}
		id := strings.TrimSpace(item.Route.ProviderID)
		if id == "" {
			continue
		}
		member, ok := seen[id]
		if !ok {
			member = WRRMember{ID: id, Weight: 1, Sequence: EffectiveProviderSequence(item.Sequence)}
			order = append(order, id)
		}
		if seq := EffectiveProviderSequence(item.Sequence); seq < member.Sequence {
			member.Sequence = seq
		}
		seen[id] = member
	}
	members := make([]WRRMember, 0, len(order))
	for _, id := range order {
		members = append(members, seen[id])
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].Sequence != members[j].Sequence {
			return members[i].Sequence < members[j].Sequence
		}
		return members[i].ID < members[j].ID
	})
	return members
}

func firstSequenceProvider(items []BalanceCandidate) string {
	bestID := ""
	bestSeq := 0
	for _, item := range items {
		id := strings.TrimSpace(item.Route.ProviderID)
		if id == "" {
			continue
		}
		seq := EffectiveProviderSequence(item.Sequence)
		if bestID == "" || seq < bestSeq || (seq == bestSeq && id < bestID) {
			bestID = id
			bestSeq = seq
		}
	}
	return bestID
}

func evictOldestWRRState(groups map[string]*wrrState, keep string) {
	for len(groups) > maxWRRGroupStates {
		oldestKey := ""
		var oldest uint64
		for key, state := range groups {
			if key == keep {
				continue
			}
			if oldestKey == "" || state.lastUsed < oldest {
				oldestKey = key
				oldest = state.lastUsed
			}
		}
		if oldestKey == "" {
			return
		}
		delete(groups, oldestKey)
	}
}

func bandSequence(items []BalanceCandidate) int {
	best := EffectiveProviderSequence(0)
	for i, item := range items {
		seq := EffectiveProviderSequence(item.Sequence)
		if i == 0 || seq < best {
			best = seq
		}
	}
	return best
}

func bandKey(candidate BalanceCandidate) string {
	return fmt.Sprintf("%s|s%d|t%d", LBGroupKey(candidate.EffectiveMultiplier), candidate.Score, candidate.ResolutionTier)
}

func wrrGroupKey(pool, bandKey string) string {
	pool = strings.TrimSpace(pool)
	if pool == "" {
		return bandKey
	}
	return pool + "\x1e" + bandKey
}

func normalizeWRRMembers(members []WRRMember) []WRRMember {
	if len(members) == 0 {
		return nil
	}
	out := make([]WRRMember, 0, len(members))
	seen := map[string]struct{}{}
	for _, member := range members {
		id := strings.TrimSpace(member.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if member.Weight <= 0 {
			member.Weight = 1
		}
		member.ID = id
		member.Sequence = EffectiveProviderSequence(member.Sequence)
		out = append(out, member)
	}
	return out
}

func wrrFingerprint(members []WRRMember) string {
	parts := make([]string, 0, len(members))
	for _, member := range members {
		parts = append(parts, member.ID+":"+strconv.Itoa(member.Weight)+":"+strconv.Itoa(member.Sequence))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
