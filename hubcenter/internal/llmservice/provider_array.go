package llmservice

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
		if strings.TrimSpace(arr.ID) == "" {
			continue
		}
		// Only an operator-created array may sit empty. A derived array that
		// lost its last provider is removed here; delete also drops the record.
		// A platform-owned array (Token Bank tiers) is kept regardless.
		if len(arr.MemberIDs) == 0 && !arr.Manual && !providerArrayProtected(&arr) {
			continue
		}
		kept = append(kept, arr)
	}
	reg.ProviderArrays = kept
}

func withoutProviderArray(arrays []llmpool.ProviderArray, id string) []llmpool.ProviderArray {
	id = strings.TrimSpace(id)
	kept := make([]llmpool.ProviderArray, 0, len(arrays))
	for _, arr := range arrays {
		if id != "" && strings.EqualFold(strings.TrimSpace(arr.ID), id) {
			continue
		}
		kept = append(kept, arr)
	}
	return kept
}

func applyProviderArrayNames(reg *Registry) {
	for i := range reg.Providers {
		name := strings.TrimSpace(reg.Providers[i].ArrayName)
		reg.Providers[i].ArrayName = ""
		reg.Providers[i].ArrayIndependent = false
		if name == "" {
			continue
		}
		if arr := findProviderArray(reg, reg.Providers[i].ArrayID); arr != nil && !providerArrayProtected(arr) {
			arr.Name = name
		}
	}
	for i := range reg.ProviderArrays {
		arr := &reg.ProviderArrays[i]
		if strings.TrimSpace(arr.Name) != "" {
			continue
		}
		if canonical, ok := tokenBankArrayName(arr.ID); ok {
			arr.Name = canonical
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
			if arrayBillingReady(arr) {
				if idx := providerIndex(reg, arr.MemberIDs[0]); idx >= 0 {
					copyArrayBillingToProvider(arr, &reg.Providers[idx])
				}
			} else if src := findProvider(reg, arr.MemberIDs[0]); src != nil {
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
	// An array that carries no price of its own must not wipe the member's
	// price. The member price is the fallback for every route without an
	// explicit service-group override (llmpool.EffectiveRouteTokenPricing), so
	// clearing it leaves the request with no resolvable price at all and it
	// falls through to the defensive billing branch. This bites the Token Bank
	// tier arrays in particular: they only ever carry a multiplier. See the
	// design doc, section 3.4 (A2).
	if arr.TokenPricing.HasCreditPricing() {
		dst.TokenPricing = arr.TokenPricing.Clone()
	}
	dst.NormalizeBilling()
}

func publishProviderBillingToArray(reg *Registry, providerID string) {
	idx := providerIndex(reg, providerID)
	if idx < 0 {
		return
	}
	provider := &reg.Providers[idx]
	arr := findProviderArray(reg, canonicalProviderArrayID(*provider))
	if arr == nil {
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
				// Only array targets collapse. Two legacy rows that name the
				// same provider and an empty model must both survive a save.
				if _, targetIsArray := arrayIDs[strings.ToLower(providerID)]; targetIsArray {
					if idx, ok := seen[key]; ok {
						out[idx] = preferProviderRouteConfig(out[idx], pc)
						continue
					}
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

// rejectProtectedArrayRename refuses a display-name change on a platform
// array. Submitting the name the array already has is not a rename.
func rejectProtectedArrayRename(arr *llmpool.ProviderArray, name string) error {
	if arr == nil || !providerArrayProtected(arr) {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" || name == strings.TrimSpace(arr.Name) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrArrayProtected, arr.ID)
}

const maxProviderArrayNameRunes = 80

func normalizeProviderArrayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("array name required")
	}
	if utf8.RuneCountInString(name) > maxProviderArrayNameRunes {
		return "", fmt.Errorf("array name is too long")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("array name required")
		}
	}
	return name, nil
}

func normalizeProviderArrayID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("provider array id required")
	}
	if utf8.RuneCountInString(id) > maxProviderArrayNameRunes {
		return "", fmt.Errorf("provider array id is too long")
	}
	for _, r := range id {
		if r <= 0x20 || r == 0x7f || r == '/' || r == '\\' || r == '?' || r == '#' {
			return "", fmt.Errorf("provider array id required")
		}
	}
	return id, nil
}

// AddProviderArray creates a logical array before any provider joins it.
// Billing stays on the array and is copied onto members as they are added.
func (s *Service) AddProviderArray(ctx context.Context, id, name string, billing ProviderArrayBilling) error {
	id, err := normalizeProviderArrayID(id)
	if err != nil {
		return err
	}
	name, err = normalizeProviderArrayName(name)
	if err != nil {
		return err
	}
	if canonical, ok := tokenBankArrayName(id); ok && name != canonical {
		return fmt.Errorf("%w: %s", ErrArrayProtected, id)
	}
	policy, err := normalizeProviderArrayBilling(billing)
	if err != nil {
		return err
	}
	return s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		if findProviderArray(reg, id) != nil || findProvider(reg, id) != nil {
			return false, fmt.Errorf("provider array %s already exists", id)
		}
		arr := llmpool.ProviderArray{ID: id, Name: name, Manual: true}
		copyProviderBillingToArray(&policy, &arr)
		reg.ProviderArrays = append(reg.ProviderArrays, arr)
		return true, nil
	})
}

func normalizeProviderArrayBilling(billing ProviderArrayBilling) (llmpool.ProviderConfig, error) {
	policy := llmpool.ProviderConfig{
		Timezone:                 billing.Timezone,
		CreditMultiplier:         billing.CreditMultiplier,
		CreditMultiplierSchedule: append([]llmpool.CreditMultiplierWindow(nil), billing.CreditMultiplierSchedule...),
		TokenPricing:             billing.TokenPricing.Clone(),
	}
	policy.NormalizeBilling()
	if err := validateProviderDefaultBilling(policy); err != nil {
		return llmpool.ProviderConfig{}, err
	}
	return policy, nil
}

// ProviderArrayBilling is the shared token price and vendor multiplier for
// every member of one logical array.
type ProviderArrayBilling struct {
	Timezone                 string
	CreditMultiplier         float64
	CreditMultiplierSchedule []llmpool.CreditMultiplierWindow
	TokenPricing             llmpool.TokenPricing
}

// UpdateProviderArray sets the array name and publishes billing onto the
// array and every current member. A one-member array otherwise copies the
// member's price back over the array on the next save.
func (s *Service) UpdateProviderArray(ctx context.Context, id, name string, billing ProviderArrayBilling) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("provider array id required")
	}
	nextName, err := normalizeProviderArrayName(name)
	if err != nil {
		return err
	}
	policy, err := normalizeProviderArrayBilling(billing)
	if err != nil {
		return err
	}
	return s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		arr := findProviderArray(reg, id)
		if arr == nil {
			return false, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
		}
		if err := rejectProtectedArrayRename(arr, nextName); err != nil {
			return false, err
		}
		arr.Name = nextName
		copyProviderBillingToArray(&policy, arr)
		arrayID := strings.TrimSpace(arr.ID)
		for i := range reg.Providers {
			if !strings.EqualFold(canonicalProviderArrayID(reg.Providers[i]), arrayID) {
				continue
			}
			copyArrayBillingToProvider(arr, &reg.Providers[i])
		}
		return true, nil
	})
}

// RenameProviderArray sets the display name of a logical provider array.
// Service groups route by array id, so a rename does not change dispatch.
func (s *Service) RenameProviderArray(ctx context.Context, id, name string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("provider array id required")
	}
	name, err := normalizeProviderArrayName(name)
	if err != nil {
		return err
	}
	return s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		arr := findProviderArray(reg, id)
		if arr == nil {
			return false, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
		}
		if err := rejectProtectedArrayRename(arr, name); err != nil {
			return false, err
		}
		if arr.Name == name {
			return false, nil
		}
		arr.Name = name
		return true, nil
	})
}

// ProviderArrayReferences returns service groups that route to the array or
// any of its members. Returned groups are copies.
func (s *Service) ProviderArrayReferences(ctx context.Context, id string) ([]llmpool.ServiceGroup, error) {
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	arr := findProviderArray(reg, id)
	if arr == nil {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, strings.TrimSpace(id))
	}
	return cloneServiceGroups(providerArrayReferencedGroups(reg, arr)), nil
}

func providerArrayRouteIDs(arr *llmpool.ProviderArray) []string {
	if arr == nil {
		return nil
	}
	ids := make([]string, 0, 1+len(arr.MemberIDs))
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		ids = append(ids, id)
	}
	add(arr.ID)
	for _, id := range arr.MemberIDs {
		add(id)
	}
	return ids
}

func providerArrayReferencedGroups(reg *Registry, arr *llmpool.ProviderArray) []llmpool.ServiceGroup {
	if reg == nil || arr == nil {
		return nil
	}
	checkIDs := providerArrayRouteIDs(arr)
	var referenced []llmpool.ServiceGroup
	for _, group := range reg.ServiceGroups {
		if groupReferencesAnyFold(group, checkIDs) {
			referenced = append(referenced, group)
		}
	}
	return referenced
}

// groupReferencesAnyFold matches route ids without case sensitivity. Stored
// routes are normally canonical, but a differently cased id must still block
// deleting the array those routes dispatch to.
func groupReferencesAnyFold(g llmpool.ServiceGroup, ids []string) bool {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		for _, model := range g.Models {
			for _, providerID := range model.ProviderIDs {
				if strings.EqualFold(strings.TrimSpace(providerID), id) {
					return true
				}
			}
			for _, cfg := range model.ProviderConfigs {
				if strings.EqualFold(strings.TrimSpace(cfg.ProviderID), id) {
					return true
				}
			}
		}
	}
	return false
}

// DeleteProviderArray removes every provider that belongs to a logical array.
// A service group that still routes to the array, or to any current member,
// blocks the delete. The array stays intact until those routes are removed.
func (s *Service) DeleteProviderArray(ctx context.Context, id string) ([]string, error) {
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
	if providerArrayProtected(arr) {
		return nil, fmt.Errorf("%w: %s", ErrArrayProtected, id)
	}
	referenced := providerArrayReferencedGroups(reg, arr)
	if len(referenced) > 0 {
		return serviceGroupNames(referenced), fmt.Errorf("%w: %s", ErrProviderInUse, id)
	}
	filtered := make([]llmpool.ProviderConfig, 0, len(reg.Providers))
	for _, provider := range reg.Providers {
		if strings.EqualFold(canonicalProviderArrayID(provider), id) {
			continue
		}
		filtered = append(filtered, provider)
	}
	next := cloneRegistry(reg)
	next.Providers = filtered
	next.ProviderArrays = withoutProviderArray(next.ProviderArrays, id)
	if err := s.persistRegistry(ctx, next); err != nil {
		return nil, err
	}
	return nil, nil
}

func lookupProviderArray(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool) (logicalID string, profile *llmpool.ProviderConfig, members []*llmpool.ProviderConfig) {
	if accept == nil {
		accept = acceptLiveProvider
	}
	routeProviderID = strings.TrimSpace(routeProviderID)
	// The route id is the array id. A provider who left can still own that id.
	// When resolve follows that leaver, stay on the array current members name,
	// using the array record's casing so rotation state stays one key.
	resolved := resolveProviderArrayID(reg, routeProviderID)
	logicalID = resolved
	// A route that names an array uses that array's stored casing, even when
	// resolve follows a provider who left or the route only differs by case.
	if arrayIDListed(reg, routeProviderID) {
		logicalID = canonicalListedArrayID(reg, routeProviderID)
	}
	ids := providerArrayMemberIDs(reg, logicalID)
	if len(ids) == 0 {
		if provider := findProvider(reg, routeProviderID); provider != nil && providerStillOnArray(*provider, logicalID, routeProviderID) {
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

// providerChatTestFilterModel is the name used to drop token-bank members that
// serve a different model. A concrete route pin wins. A billing band keeps
// every member, matching tokenBankMembersForModel.
func providerChatTestFilterModel(routeModel, logicalModel, requestedModel string) string {
	routeModel = strings.TrimSpace(routeModel)
	if routeModel != "" && !isBillingBandName(routeModel) {
		return routeModel
	}
	if logical := strings.TrimSpace(logicalModel); logical != "" {
		return logical
	}
	return strings.TrimSpace(requestedModel)
}

// providerChatTestArrayMembers is the member class a status test may dial.
// It follows the production egress filters and then puts a recovery probe
// behind siblings that are already in rotation. It does not call
// orderArrayMembers or orderTokenBankReady: both advance the production cursor.
func providerChatTestArrayMembers(reg *Registry, id, filterModel string) []*llmpool.ProviderConfig {
	_, _, members := lookupProviderArray(reg, id, acceptLiveProvider)
	members = withoutCoolingArrayMembers(members)
	members = withoutQuotaBlockedMembers(members, time.Now())
	members = tokenBankMembersForModel(members, filterModel)
	members = withoutClosedShareWindows(members, time.Now())
	ready, delayed := splitArrayProbeMembers(members)
	// An admin status check has no request id, so it is not a canary hit.
	// Drop canaries when a stable sibling can answer. orderTokenBankReady
	// does this and then rotates; rotating would take a production turn.
	ready = withoutTokenBankCanaries(ready, time.Now())
	if len(ready) == 0 {
		return delayed
	}
	if len(delayed) == 0 {
		return ready
	}
	return append(ready, delayed...)
}

// withoutTokenBankCanaries keeps the members a non-canary request dials.
// A canary stays when it is the only supply. Ordinary providers stay.
func withoutTokenBankCanaries(members []*llmpool.ProviderConfig, now time.Time) []*llmpool.ProviderConfig {
	if !membersIncludeTokenBank(members) {
		return members
	}
	rest := make([]*llmpool.ProviderConfig, 0, len(members))
	sawCanary := false
	for _, member := range members {
		if tokenBankMemberInCanary(member, now) {
			sawCanary = true
			continue
		}
		rest = append(rest, member)
	}
	if !sawCanary || len(rest) == 0 {
		return members
	}
	return rest
}

// providerChatTestCandidates lists members a status test may try, in dial order.
// A service-group test sets preferArray. The route may store an array id or a
// member id; lookupProviderArray resolves either one to the array production
// dials. A provider Test Status call sets preferArray false and tests that
// member alone, including a member who also owns the array id. An id that is
// only an array still expands. When every filtered member is cooling, over
// quota, or outside a share window, the test falls back to a live member and
// then to any member, including a paused one, so the admin still reaches an
// upstream instead of "provider not found".
func providerChatTestCandidates(reg *Registry, id string, preferArray bool, filterModel string) []*llmpool.ProviderConfig {
	id = strings.TrimSpace(id)
	if reg == nil || id == "" {
		return nil
	}
	if !preferArray {
		if provider := findProvider(reg, id); provider != nil {
			return []*llmpool.ProviderConfig{provider}
		}
	}
	if filtered := providerChatTestArrayMembers(reg, id, filterModel); len(filtered) > 0 {
		return filtered
	}
	_, _, members := lookupProviderArray(reg, id, acceptLiveProvider)
	if len(members) == 0 {
		_, _, members = lookupProviderArray(reg, id, func(provider *llmpool.ProviderConfig) bool { return provider != nil })
	}
	if len(members) > 0 {
		return members
	}
	if provider := findProvider(reg, id); provider != nil {
		return []*llmpool.ProviderConfig{provider}
	}
	return nil
}

func arrayEgressCandidates(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool, model string) (logicalID string, profile *llmpool.ProviderConfig, members []*llmpool.ProviderConfig) {
	return arrayEgressForBucket(reg, routeProviderID, accept, model, "")
}

// arrayEgressForBucket is arrayEgressCandidates for one request id. The id is
// the canary bucket: the same id always picks the same side of the 5% cut.
// An empty bucket is not a hit, so a caller that has no request leaves canaries
// out of rotation whenever a stable sibling can answer.
func arrayEgressForBucket(reg *Registry, routeProviderID string, accept func(*llmpool.ProviderConfig) bool, model, bucket string) (logicalID string, profile *llmpool.ProviderConfig, members []*llmpool.ProviderConfig) {
	logicalID, profile, members = lookupProviderArray(reg, routeProviderID, accept)
	// Drop cooling members before rotating so a paused member does not consume
	// a turn, and an array that is entirely cooling does not move the cursor.
	members = withoutCoolingArrayMembers(members)
	members = withoutQuotaBlockedMembers(members, time.Now())
	// A tier array mixes models. Drop the other models before rotating, or
	// their members advance this model's cursor and then get filtered out.
	members = tokenBankMembersForModel(members, model)
	// A closed share window is not dialed. Drop it before rotating so it does
	// not take a turn from the sibling that is open.
	members = withoutClosedShareWindows(members, time.Now())
	// Rotate only the members that should take traffic now. A recovery probe
	// stays behind them and does not consume a turn while a sibling can answer.
	// Canaries are outside that cursor unless they are the only supply.
	ready, delayed := splitArrayProbeMembers(members)
	ready = orderTokenBankReady(logicalID, model, bucket, ready, time.Now())
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

// arrayRouteServeWindowClosed reports that this model still has configured
// members and every one of them is outside its serve window. It ignores
// pause, cooling, and quota so the diagnostic names the window instead of
// hiding behind those states. acceptLiveProvider already drops closed
// members from the dispatch list, so this runs on an any-member lookup.
func arrayRouteServeWindowClosed(reg *Registry, routeProviderID string, model string) bool {
	_, _, members := lookupProviderArray(reg, routeProviderID, func(provider *llmpool.ProviderConfig) bool { return provider != nil })
	members = tokenBankMembersForModel(members, model)
	if len(members) == 0 {
		return false
	}
	now := time.Now()
	for _, member := range members {
		if member == nil || llmpool.ServeWindowsAllows(member.ServeWindows, now) {
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
		if len(out) > 0 {
			return out
		}
		// The saved member list is empty. Providers that still name this array
		// are the members; do not treat a former owner with a blank array id
		// as one of them.
		return providersWithArrayID(reg, arrayID)
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

// providerStillOnArray reports that this provider is the array the route
// named. A provider who moved to another array must not fill an empty one.
func providerStillOnArray(provider llmpool.ProviderConfig, logicalID, routeProviderID string) bool {
	canon := canonicalProviderArrayID(provider)
	return strings.EqualFold(canon, logicalID) || strings.EqualFold(canon, routeProviderID)
}

func providersWithArrayID(reg *Registry, arrayID string) []string {
	if reg == nil {
		return nil
	}
	arrayID = strings.TrimSpace(arrayID)
	var out []string
	for i := range reg.Providers {
		if !strings.EqualFold(strings.TrimSpace(reg.Providers[i].ArrayID), arrayID) {
			continue
		}
		if id := strings.TrimSpace(reg.Providers[i].ID); id != "" {
			out = append(out, id)
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

func arrayMemberCooldownUntil(providerID string) time.Time {
	providerID = providerIDKey(providerID)
	if providerID == "" {
		return time.Time{}
	}
	providerArrayFailurePause.Lock()
	defer providerArrayFailurePause.Unlock()
	state := providerArrayFailurePause.states[providerID]
	if state == nil || !time.Now().Before(state.until) {
		return time.Time{}
	}
	return state.until
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
