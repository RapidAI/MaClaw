package llmservice

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func cloneProviderArrays(in []llmpool.ProviderArray) []llmpool.ProviderArray {
	if in == nil {
		return nil
	}
	out := make([]llmpool.ProviderArray, len(in))
	for i, arr := range in {
		out[i] = arr
		out[i].MemberIDs = append([]string(nil), arr.MemberIDs...)
		out[i].CreditMultiplierSchedule = cloneCreditWindows(arr.CreditMultiplierSchedule)
		out[i].TokenPricing = arr.TokenPricing.Clone()
	}
	return out
}

func canonicalProviderArrayID(provider llmpool.ProviderConfig) string {
	if id := strings.TrimSpace(provider.ArrayID); id != "" {
		return id
	}
	return strings.TrimSpace(provider.ID)
}

func sameProviderArray(a, b llmpool.ProviderConfig) bool {
	return strings.EqualFold(canonicalProviderArrayID(a), canonicalProviderArrayID(b))
}

func findProviderArray(reg *Registry, id string) *llmpool.ProviderArray {
	id = strings.TrimSpace(id)
	if reg == nil || id == "" {
		return nil
	}
	folded := -1
	for i := range reg.ProviderArrays {
		got := strings.TrimSpace(reg.ProviderArrays[i].ID)
		if got == id {
			return &reg.ProviderArrays[i]
		}
		if folded < 0 && strings.EqualFold(got, id) {
			folded = i
		}
	}
	if folded >= 0 {
		return &reg.ProviderArrays[folded]
	}
	return nil
}

// canonicalJoinArrayID resolves the array a new provider should join.
// An empty result means the provider becomes its own array.
func canonicalJoinArrayID(reg *Registry, requested, selfID string) (string, error) {
	requested = strings.TrimSpace(requested)
	selfID = strings.TrimSpace(selfID)
	if requested == "" || strings.EqualFold(requested, selfID) {
		return "", nil
	}
	if arr := findProviderArray(reg, requested); arr != nil {
		return arr.ID, nil
	}
	if idx := providerIndex(reg, requested); idx >= 0 {
		return canonicalProviderArrayID(reg.Providers[idx]), nil
	}
	return "", fmt.Errorf("provider array %s not found", requested)
}

// independentProviderArrayID is the array id to use when a provider leaves
// its current array. The provider's own id stays with the shared array when
// that id is what service groups already route to.
func independentProviderArrayID(reg *Registry, existing llmpool.ProviderConfig) string {
	providerID := strings.TrimSpace(existing.ID)
	current := canonicalProviderArrayID(existing)
	if providerID == "" {
		return current
	}
	if providerArraySiblingCount(reg, providerID) > 0 {
		if strings.EqualFold(current, providerID) {
			return providerID + "#solo"
		}
		return providerID
	}
	if arr := findProviderArray(reg, providerID); arr != nil && arrayHasOtherMembers(arr, providerID) {
		if current != "" && !strings.EqualFold(current, providerID) {
			return current
		}
		return providerID + "#solo"
	}
	if current != "" && !strings.EqualFold(current, providerID) {
		return current
	}
	return providerID
}

func arrayHasOtherMembers(arr *llmpool.ProviderArray, providerID string) bool {
	if arr == nil {
		return false
	}
	for _, id := range arr.MemberIDs {
		if !strings.EqualFold(strings.TrimSpace(id), providerID) && strings.TrimSpace(id) != "" {
			return true
		}
	}
	return false
}

func providerArraySiblingCount(reg *Registry, providerID string) int {
	providerID = strings.TrimSpace(providerID)
	idx := providerIndex(reg, providerID)
	if reg == nil || idx < 0 {
		return 0
	}
	arrayID := canonicalProviderArrayID(reg.Providers[idx])
	if arrayID == "" {
		return 0
	}
	count := 0
	for _, provider := range reg.Providers {
		if strings.EqualFold(strings.TrimSpace(provider.ID), providerID) {
			continue
		}
		if strings.EqualFold(canonicalProviderArrayID(provider), arrayID) {
			count++
		}
	}
	return count
}

func normalizeProviderArrays(reg *Registry) {
	if reg == nil {
		return
	}
	ensureProviderArrayIDs(reg)
	rebuildProviderArrayMembers(reg)
	dropEmptyProviderArrays(reg)
	applyProviderArrayNames(reg)
	syncProviderArrayBilling(reg)
	collapseServiceGroupArrayRoutes(reg)
}

func ensureProviderArrayIDs(reg *Registry) {
	for i := range reg.Providers {
		provider := &reg.Providers[i]
		requested := strings.TrimSpace(provider.ArrayID)
		if requested == "" {
			requested = strings.TrimSpace(provider.ID)
		}
		if requested == "" {
			continue
		}
		if arr := findProviderArray(reg, requested); arr != nil {
			provider.ArrayID = arr.ID
			continue
		}
		if other := findProvider(reg, requested); other != nil && !strings.EqualFold(strings.TrimSpace(other.ID), strings.TrimSpace(provider.ID)) {
			if aid := strings.TrimSpace(other.ArrayID); aid != "" {
				if arr := findProviderArray(reg, aid); arr != nil {
					provider.ArrayID = arr.ID
					continue
				}
			}
		}
		reg.ProviderArrays = append(reg.ProviderArrays, llmpool.ProviderArray{ID: requested})
		provider.ArrayID = requested
	}
}

func rebuildProviderArrayMembers(reg *Registry) {
	grouped := map[string][]string{}
	for _, provider := range reg.Providers {
		arrayID := strings.TrimSpace(provider.ArrayID)
		providerID := strings.TrimSpace(provider.ID)
		if arrayID == "" || providerID == "" {
			continue
		}
		key := strings.ToLower(arrayID)
		grouped[key] = append(grouped[key], providerID)
	}
	for i := range reg.ProviderArrays {
		arr := &reg.ProviderArrays[i]
		want := map[string]string{}
		for _, id := range grouped[strings.ToLower(strings.TrimSpace(arr.ID))] {
			want[strings.ToLower(id)] = id
		}
		seen := map[string]struct{}{}
		members := make([]string, 0, len(want))
		for _, id := range arr.MemberIDs {
			canon, ok := want[strings.ToLower(strings.TrimSpace(id))]
			if !ok {
				continue
			}
			key := strings.ToLower(canon)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			members = append(members, canon)
		}
		for _, id := range grouped[strings.ToLower(strings.TrimSpace(arr.ID))] {
			key := strings.ToLower(id)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			members = append(members, id)
		}
		arr.MemberIDs = members
	}
}

func dropEmptyProviderArrays(reg *Registry) {
	kept := make([]llmpool.ProviderArray, 0, len(reg.ProviderArrays))
	for _, arr := range reg.ProviderArrays {
		if len(arr.MemberIDs) == 0 || strings.TrimSpace(arr.ID) == "" {
			continue
		}
		kept = append(kept, arr)
	}
	reg.ProviderArrays = kept
}

func applyProviderArrayNames(reg *Registry) {
	for i := range reg.Providers {
		name := strings.TrimSpace(reg.Providers[i].ArrayName)
		reg.Providers[i].ArrayName = ""
		reg.Providers[i].ArrayIndependent = false
		if name == "" {
			continue
		}
		if arr := findProviderArray(reg, reg.Providers[i].ArrayID); arr != nil {
			arr.Name = name
		}
	}
	for i := range reg.ProviderArrays {
		arr := &reg.ProviderArrays[i]
		if strings.TrimSpace(arr.Name) != "" {
			continue
		}
		if src := arrayBillingSource(reg, arr); src != nil {
			arr.Name = providerDisplayName(src)
		}
		if strings.TrimSpace(arr.Name) == "" {
			arr.Name = arr.ID
		}
	}
}

func syncProviderArrayBilling(reg *Registry) {
	for i := range reg.ProviderArrays {
		arr := &reg.ProviderArrays[i]
		if len(arr.MemberIDs) == 0 {
			continue
		}
		if len(arr.MemberIDs) == 1 {
			if src := findProvider(reg, arr.MemberIDs[0]); src != nil {
				copyProviderBillingToArray(src, arr)
			}
			continue
		}
		if !arrayBillingReady(arr) {
			if src := arrayBillingSource(reg, arr); src != nil {
				copyProviderBillingToArray(src, arr)
			}
		}
		for pi := range reg.Providers {
			if !strings.EqualFold(strings.TrimSpace(reg.Providers[pi].ArrayID), strings.TrimSpace(arr.ID)) {
				continue
			}
			copyArrayBillingToProvider(arr, &reg.Providers[pi])
		}
	}
}

func arrayBillingSource(reg *Registry, arr *llmpool.ProviderArray) *llmpool.ProviderConfig {
	if arr == nil {
		return nil
	}
	var first *llmpool.ProviderConfig
	for _, id := range arr.MemberIDs {
		src := findProvider(reg, id)
		if src == nil {
			continue
		}
		if first == nil {
			first = src
		}
		if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(arr.ID)) {
			return src
		}
	}
	return first
}

func providerDisplayName(provider *llmpool.ProviderConfig) string {
	if provider == nil {
		return ""
	}
	if name := strings.TrimSpace(provider.Name); name != "" {
		return name
	}
	return strings.TrimSpace(provider.ID)
}

func arrayBillingReady(arr *llmpool.ProviderArray) bool {
	if arr == nil {
		return false
	}
	if arr.CreditMultiplier > 0 || strings.TrimSpace(arr.Timezone) != "" {
		return true
	}
	if len(arr.CreditMultiplierSchedule) > 0 || arr.TokenPricing.HasCreditPricing() || len(arr.TokenPricing.PriceSchedule) > 0 {
		return true
	}
	return false
}

func copyProviderBillingToArray(src *llmpool.ProviderConfig, arr *llmpool.ProviderArray) {
	if src == nil || arr == nil {
		return
	}
	arr.Timezone = src.Timezone
	arr.CreditMultiplier = src.CreditMultiplier
	arr.CreditMultiplierSchedule = cloneCreditWindows(src.CreditMultiplierSchedule)
	arr.TokenPricing = src.TokenPricing.Clone()
}

func copyArrayBillingToProvider(arr *llmpool.ProviderArray, dst *llmpool.ProviderConfig) {
	if arr == nil || dst == nil {
		return
	}
	dst.Timezone = arr.Timezone
	dst.CreditMultiplier = arr.CreditMultiplier
	dst.CreditMultiplierSchedule = cloneCreditWindows(arr.CreditMultiplierSchedule)
	dst.TokenPricing = arr.TokenPricing.Clone()
	dst.NormalizeBilling()
}

func publishProviderBillingToArray(reg *Registry, providerID string) {
	idx := providerIndex(reg, providerID)
	if idx < 0 {
		return
	}
	provider := &reg.Providers[idx]
	arr := findProviderArray(reg, canonicalProviderArrayID(*provider))
	if arr == nil || len(arr.MemberIDs) < 2 {
		return
	}
	copyProviderBillingToArray(provider, arr)
}

func collapseServiceGroupArrayRoutes(reg *Registry) {
	arrayIDs := map[string]struct{}{}
	for _, arr := range reg.ProviderArrays {
		if id := strings.TrimSpace(arr.ID); id != "" {
			arrayIDs[strings.ToLower(id)] = struct{}{}
		}
	}
	memberArray := map[string]string{}
	for _, provider := range reg.Providers {
		providerID := strings.TrimSpace(provider.ID)
		arrayID := strings.TrimSpace(provider.ArrayID)
		if providerID == "" || arrayID == "" {
			continue
		}
		memberArray[strings.ToLower(providerID)] = arrayID
	}
	for gi := range reg.ServiceGroups {
		for mi := range reg.ServiceGroups[gi].Models {
			model := &reg.ServiceGroups[gi].Models[mi]
			configs := modelProviderConfigs(*model)
			out := make([]llmpool.ModelProviderConfig, 0, len(configs))
			seen := map[string]int{}
			for _, pc := range configs {
				providerID := strings.TrimSpace(pc.ProviderID)
				// A route that already names a live array stays there. The
				// provider who used to own that id may have left for a solo
				// array, and their provider id must not pull the route along.
				if _, isArray := arrayIDs[strings.ToLower(providerID)]; !isArray {
					if arrayID, ok := memberArray[strings.ToLower(providerID)]; ok {
						providerID = arrayID
					}
				}
				if providerID == "" {
					continue
				}
				pc.ProviderID = providerID
				pc.Model = strings.TrimSpace(pc.Model)
				key := strings.ToLower(providerID) + "\x00" + pc.Model
				if idx, ok := seen[key]; ok {
					out[idx] = preferProviderRouteConfig(out[idx], pc)
					continue
				}
				seen[key] = len(out)
				out = append(out, pc)
			}
			model.ProviderConfigs = out
			ids := make([]string, 0, len(out))
			seenID := map[string]struct{}{}
			for _, pc := range out {
				key := strings.ToLower(pc.ProviderID)
				if _, ok := seenID[key]; ok {
					continue
				}
				seenID[key] = struct{}{}
				ids = append(ids, pc.ProviderID)
			}
			model.ProviderIDs = ids
		}
	}
}

func preferProviderRouteConfig(current, candidate llmpool.ModelProviderConfig) llmpool.ModelProviderConfig {
	if candidate.TokenPricingOverride && !current.TokenPricingOverride {
		return candidate
	}
	if !current.TokenPricingOverride && !current.TokenPricing.HasCreditPricing() && candidate.TokenPricing.HasCreditPricing() {
		return candidate
	}
	return current
}

// providerDeleteRouteIDs are the service-group route ids removed with a
// provider who is the last member of an array. The provider's own id is
// omitted when that id is still a different live array.
func providerDeleteRouteIDs(reg *Registry, provider llmpool.ProviderConfig) []string {
	providerID := strings.TrimSpace(provider.ID)
	arrayID := canonicalProviderArrayID(provider)
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		for _, got := range ids {
			if strings.EqualFold(got, id) {
				return
			}
		}
		ids = append(ids, id)
	}
	if arr := findProviderArray(reg, providerID); arr == nil || strings.EqualFold(arr.ID, arrayID) {
		add(providerID)
	}
	add(arrayID)
	return ids
}

func retargetProviderRoutes(reg *Registry, fromID, toID string) {
	fromID = strings.TrimSpace(fromID)
	toID = strings.TrimSpace(toID)
	if reg == nil || fromID == "" || toID == "" || strings.EqualFold(fromID, toID) {
		return
	}
	for gi := range reg.ServiceGroups {
		for mi := range reg.ServiceGroups[gi].Models {
			model := &reg.ServiceGroups[gi].Models[mi]
			for i, providerID := range model.ProviderIDs {
				if strings.EqualFold(strings.TrimSpace(providerID), fromID) {
					model.ProviderIDs[i] = toID
				}
			}
			for i := range model.ProviderConfigs {
				if strings.EqualFold(strings.TrimSpace(model.ProviderConfigs[i].ProviderID), fromID) {
					model.ProviderConfigs[i].ProviderID = toID
				}
			}
		}
	}
}

// DeleteProviderArray removes every member of a logical provider. prune strips
// the array id and each member id from service groups in the same write.
func (s *Service) DeleteProviderArray(ctx context.Context, id string, prune bool) ([]string, error) {
	defer s.lockRegistryWrite()()
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	arr := findProviderArray(reg, id)
	if arr == nil {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, strings.TrimSpace(id))
	}
	id = arr.ID
	checkIDs := append([]string{id}, arr.MemberIDs...)
	var referenced []llmpool.ServiceGroup
	for _, group := range reg.ServiceGroups {
		for _, checkID := range checkIDs {
			if groupReferencesProvider(group, strings.TrimSpace(checkID)) {
				referenced = append(referenced, group)
				break
			}
		}
	}
	if len(referenced) > 0 && !prune {
		return serviceGroupNames(referenced), fmt.Errorf("%w: %s", ErrProviderInUse, id)
	}
	drop := map[string]struct{}{}
	for _, memberID := range arr.MemberIDs {
		drop[strings.ToLower(strings.TrimSpace(memberID))] = struct{}{}
	}
	filtered := make([]llmpool.ProviderConfig, 0, len(reg.Providers))
	for _, provider := range reg.Providers {
		if _, ok := drop[strings.ToLower(strings.TrimSpace(provider.ID))]; ok {
			continue
		}
		filtered = append(filtered, provider)
	}
	next := cloneRegistry(reg)
	next.Providers = filtered
	var pruned []string
	if prune {
		seen := map[string]struct{}{}
		for _, checkID := range checkIDs {
			for _, name := range pruneProviderFromGroups(next, strings.TrimSpace(checkID)) {
				if _, ok := seen[name]; ok {
					continue
				}
				seen[name] = struct{}{}
				pruned = append(pruned, name)
			}
		}
	}
	if err := s.persistRegistry(ctx, next); err != nil {
		return nil, err
	}
	return pruned, nil
}

func lookupProviderArray(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool) (logicalID string, profile *llmpool.ProviderConfig, members []*llmpool.ProviderConfig) {
	if accept == nil {
		accept = acceptLiveProvider
	}
	logicalID = resolveProviderArrayID(reg, routeProviderID)
	ids := providerArrayMemberIDs(reg, logicalID)
	if len(ids) == 0 {
		if provider := findProvider(reg, routeProviderID); provider != nil {
			logicalID = strings.TrimSpace(provider.ID)
			if logicalID == "" {
				logicalID = strings.TrimSpace(routeProviderID)
			}
			ids = []string{strings.TrimSpace(provider.ID)}
		}
	}
	// Billing follows a member of this array. The provider who originally
	// owned the array id may have left, and that record is a different provider.
	var profileSrc *llmpool.ProviderConfig
	for _, id := range ids {
		provider := findProvider(reg, id)
		if provider == nil {
			continue
		}
		if profileSrc == nil {
			profileSrc = provider
		}
		if !accept(provider) {
			continue
		}
		copied := *provider
		members = append(members, &copied)
	}
	if profileSrc != nil {
		copied := *profileSrc
		profile = &copied
	}
	if logicalID == "" && profile != nil {
		logicalID = strings.TrimSpace(profile.ID)
	}
	return logicalID, profile, members
}

func arrayEgressCandidates(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool) (logicalID string, profile *llmpool.ProviderConfig, members []*llmpool.ProviderConfig) {
	logicalID, profile, members = lookupProviderArray(reg, routeProviderID, accept)
	// Drop cooling members before rotating so a paused member does not consume
	// a turn, and an array that is entirely cooling does not move the cursor.
	members = withoutCoolingArrayMembers(members)
	// Rotate only the members that should take traffic now. A recovery probe
	// stays behind them and does not consume a turn while a sibling can answer.
	ready, delayed := splitArrayProbeMembers(members)
	ready = rotateProviderArray(logicalID, ready)
	if len(delayed) == 0 {
		return logicalID, profile, ready
	}
	return logicalID, profile, append(ready, delayed...)
}

// arrayRouteCooling reports that every member this request could call is inside
// its failure pause. The request should move on without dialing them.
func arrayRouteCooling(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool) bool {
	if accept == nil {
		accept = acceptLiveProvider
	}
	_, _, members := lookupProviderArray(reg, routeProviderID, accept)
	if len(members) == 0 {
		return false
	}
	for _, member := range members {
		if member == nil || !arrayMemberCooling(member.ID) {
			return false
		}
	}
	return true
}

func resolveProviderArrayID(reg *Registry, id string) string {
	id = strings.TrimSpace(id)
	if id == "" || reg == nil {
		return id
	}
	for _, arr := range reg.ProviderArrays {
		arrayID := strings.TrimSpace(arr.ID)
		if strings.EqualFold(arrayID, id) {
			return arrayID
		}
		for _, memberID := range arr.MemberIDs {
			if strings.EqualFold(strings.TrimSpace(memberID), id) {
				return arrayID
			}
		}
	}
	if provider := findProvider(reg, id); provider != nil {
		return canonicalProviderArrayID(*provider)
	}
	return id
}

func providerArrayMemberIDs(reg *Registry, arrayID string) []string {
	arrayID = strings.TrimSpace(arrayID)
	if reg == nil || arrayID == "" {
		return nil
	}
	for _, arr := range reg.ProviderArrays {
		if !strings.EqualFold(strings.TrimSpace(arr.ID), arrayID) {
			continue
		}
		out := make([]string, 0, len(arr.MemberIDs))
		for _, id := range arr.MemberIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	var out []string
	for _, provider := range reg.Providers {
		if strings.EqualFold(canonicalProviderArrayID(provider), arrayID) {
			if id := strings.TrimSpace(provider.ID); id != "" {
				out = append(out, id)
			}
		}
	}
	return out
}

var providerArrayCursor = struct {
	sync.Mutex
	next map[string]int
}{next: map[string]int{}}

func rotateProviderArray(arrayID string, members []*llmpool.ProviderConfig) []*llmpool.ProviderConfig {
	if len(members) <= 1 {
		return members
	}
	arrayID = strings.TrimSpace(arrayID)
	providerArrayCursor.Lock()
	defer providerArrayCursor.Unlock()
	if providerArrayCursor.next == nil {
		providerArrayCursor.next = map[string]int{}
	}
	// Keep the cursor inside the member count. A wrapped negative int modulo
	// is negative in Go and would panic on the slice index below.
	start := providerArrayCursor.next[arrayID]
	if start < 0 {
		start = 0
	}
	start %= len(members)
	providerArrayCursor.next[arrayID] = (start + 1) % len(members)
	if start == 0 {
		return members
	}
	out := make([]*llmpool.ProviderConfig, len(members))
	for i := range members {
		out[i] = members[(start+i)%len(members)]
	}
	return out
}

// arrayMemberFailurePause is how long a failed member of a multi-member array
// stays out of rotation. A single 5xx does not open the circuit breaker
// (threshold defaults to 2), so without this pause the next request would
// wait on the same dead upstream again.
var arrayMemberFailurePause = 30 * time.Second

// arrayMemberProbeMax is how long one recovery dial keeps other requests off
// a member whose pause just ended. The default upstream timeout is 10 minutes,
// so a shorter cap would let the next request wait on the same dead call.
const arrayMemberProbeMax = 10 * time.Minute

type arrayMemberPauseState struct {
	until      time.Time
	probe      bool
	probeStart time.Time
}

var providerArrayFailurePause = struct {
	sync.Mutex
	states map[string]*arrayMemberPauseState
}{states: map[string]*arrayMemberPauseState{}}

func pauseArrayMember(providerID string, d time.Duration) {
	providerID = providerIDKey(providerID)
	if providerID == "" || d <= 0 {
		return
	}
	until := time.Now().Add(d)
	providerArrayFailurePause.Lock()
	defer providerArrayFailurePause.Unlock()
	if providerArrayFailurePause.states == nil {
		providerArrayFailurePause.states = map[string]*arrayMemberPauseState{}
	}
	state := providerArrayFailurePause.states[providerID]
	if state == nil {
		state = &arrayMemberPauseState{}
		providerArrayFailurePause.states[providerID] = state
	}
	state.probe = false
	if state.until.After(until) {
		return
	}
	state.until = until
}

func clearArrayMemberPause(providerID string) {
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return
	}
	providerArrayFailurePause.Lock()
	delete(providerArrayFailurePause.states, providerID)
	providerArrayFailurePause.Unlock()
}

func releaseArrayMemberProbe(providerID string) {
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return
	}
	providerArrayFailurePause.Lock()
	defer providerArrayFailurePause.Unlock()
	state := providerArrayFailurePause.states[providerID]
	if state == nil {
		return
	}
	state.probe = false
	if state.until.IsZero() {
		delete(providerArrayFailurePause.states, providerID)
	}
}

func arrayMemberCooling(providerID string) bool {
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return false
	}
	providerArrayFailurePause.Lock()
	defer providerArrayFailurePause.Unlock()
	return arrayMemberCoolingLocked(providerID, time.Now())
}

func arrayMemberCoolingLocked(providerID string, now time.Time) bool {
	state := providerArrayFailurePause.states[providerID]
	if state == nil {
		return false
	}
	if now.Before(state.until) {
		return true
	}
	return state.probe && now.Sub(state.probeStart) < arrayMemberProbeMax
}

// arrayMemberDialGate skips a member that is still paused. The first caller
// after the pause ends is the only probe; everyone else keeps skipping until
// that dial finishes, fails again, or the probe slot goes stale.
func arrayMemberDialGate(providerID string) (skip bool, claimed bool) {
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return false, false
	}
	now := time.Now()
	providerArrayFailurePause.Lock()
	defer providerArrayFailurePause.Unlock()
	if arrayMemberCoolingLocked(providerID, now) {
		return true, false
	}
	state := providerArrayFailurePause.states[providerID]
	if state == nil {
		return false, false
	}
	// Keep the expired deadline. Releasing this probe must not look like the
	// member never failed, or the next burst of requests all dial it at once.
	state.probe = true
	state.probeStart = now
	return false, true
}

// arrayMemberDeferProbe reports that the pause has expired but the member has
// not been dialed again yet. For one extra pause window, keep it behind
// siblings that are already in rotation. After that it rejoins normal order
// so a recovered member is not left idle.
func arrayMemberDeferProbe(providerID string) bool {
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return false
	}
	now := time.Now()
	providerArrayFailurePause.Lock()
	defer providerArrayFailurePause.Unlock()
	state := providerArrayFailurePause.states[providerID]
	if state == nil || state.probe || now.Before(state.until) {
		return false
	}
	return now.Before(state.until.Add(arrayMemberFailurePause))
}

func splitArrayProbeMembers(members []*llmpool.ProviderConfig) (ready, delayed []*llmpool.ProviderConfig) {
	if len(members) < 2 {
		return members, nil
	}
	ready = make([]*llmpool.ProviderConfig, 0, len(members))
	for _, member := range members {
		if member != nil && arrayMemberDeferProbe(member.ID) {
			delayed = append(delayed, member)
			continue
		}
		ready = append(ready, member)
	}
	if len(ready) == 0 || len(delayed) == 0 {
		return members, nil
	}
	return ready, delayed
}

func withoutCoolingArrayMembers(members []*llmpool.ProviderConfig) []*llmpool.ProviderConfig {
	if len(members) == 0 {
		return members
	}
	live := make([]*llmpool.ProviderConfig, 0, len(members))
	cooled := false
	for _, member := range members {
		if member != nil && arrayMemberCooling(member.ID) {
			cooled = true
			continue
		}
		live = append(live, member)
	}
	if !cooled {
		return members
	}
	if len(live) == 0 {
		return nil
	}
	return live
}
