package llmservice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// Token Bank tier arrays. A shared model joins exactly one of these arrays,
// and the array decides which group the model is dispatched from. The tier
// rate itself is NOT the array multiplier: see tokenBankArrayCreditMultiplier.
const (
	TokenBankArrayLow  = "token_bank_low"
	TokenBankArrayMid  = "token_bank_mid"
	TokenBankArrayHigh = "token_bank_high"
)

// Token bank tiers accepted by the automation API.
const (
	TokenBankTierLow  = "low"
	TokenBankTierMid  = "mid"
	TokenBankTierHigh = "high"
)

// tokenBankArrayCreditMultiplier is the multiplier stamped onto the three tier
// arrays. It is pinned to a neutral 1 on purpose.
//
// The array multiplier is copied onto every member (copyArrayBillingToProvider)
// and then multiplied into what the CONSUMER pays (proxyRequestBillingCredits
// -> CombineCreditMultipliers). Carrying the tier rate there would charge a
// consumer 2x for a high tier model and silently re-price consumers every time
// an operator regrades a model, which contradicts the "consumers never see the
// tier" rule. The tier rate is TokenBankTierMultiplier's job and is applied
// only when crediting the provider. See the design doc, section 3.4 (A1).
const tokenBankArrayCreditMultiplier = 1.0

// ErrArrayProtected is returned when a platform-owned array would be deleted
// or renamed. The three Token Bank tier names are part of the platform
// contract and stay on the ids token_bank_low, token_bank_mid, and token_bank_high.
var ErrArrayProtected = errors.New("provider array is protected")

// ErrArrayNotFound is returned when a provider array id does not resolve.
var ErrArrayNotFound = errors.New("provider array not found")

// ErrUnknownTokenBankTier is returned for a tier outside low/mid/high.
var ErrUnknownTokenBankTier = errors.New("unknown token bank tier")

type tokenBankArraySpec struct {
	ID   string
	Name string
}

// tokenBankArraySpecs lists the three platform-owned tier arrays in tier order.
func tokenBankArraySpecs() []tokenBankArraySpec {
	return []tokenBankArraySpec{
		{ID: TokenBankArrayLow, Name: "Token Bank 低档"},
		{ID: TokenBankArrayMid, Name: "Token Bank 中档"},
		{ID: TokenBankArrayHigh, Name: "Token Bank 高档"},
	}
}

// TokenBankArrayID resolves a tier name to its array id.
func TokenBankArrayID(tier string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case TokenBankTierLow:
		return TokenBankArrayLow, nil
	case TokenBankTierMid:
		return TokenBankArrayMid, nil
	case TokenBankTierHigh:
		return TokenBankArrayHigh, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownTokenBankTier, strings.TrimSpace(tier))
	}
}

// tokenBankArrayName is the display name pinned to a tier array. The second
// result is false when the id is not one of the three platform arrays.
func tokenBankArrayName(arrayID string) (string, bool) {
	for _, spec := range tokenBankArraySpecs() {
		if strings.EqualFold(spec.ID, strings.TrimSpace(arrayID)) {
			return spec.Name, true
		}
	}
	return "", false
}

// TokenBankTierOfArray maps an array id back to its tier. The second result is
// false when the array is not one of the Token Bank tier arrays.
func TokenBankTierOfArray(arrayID string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(arrayID)) {
	case TokenBankArrayLow:
		return TokenBankTierLow, true
	case TokenBankArrayMid:
		return TokenBankTierMid, true
	case TokenBankArrayHigh:
		return TokenBankTierHigh, true
	default:
		return "", false
	}
}

// IsTokenBankArray reports whether the id is one of the platform-owned arrays.
func IsTokenBankArray(arrayID string) bool {
	_, ok := TokenBankTierOfArray(arrayID)
	return ok
}

// providerArrayProtected reports an array the platform owns. The Token Bank
// tier ids stay protected even when the System bit was never stamped.
func providerArrayProtected(arr *llmpool.ProviderArray) bool {
	if arr == nil {
		return false
	}
	return arr.System || IsTokenBankArray(arr.ID)
}

// TokenBankArrayForTier maps a tier name to the array a member of that tier
// must join (§3.4). An unrecognised tier falls back to mid, the documented
// default: a model with a garbled tier should still be routable at the neutral
// rate rather than silently absent from every group.
func TokenBankArrayForTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case TokenBankTierLow:
		return TokenBankArrayLow
	case TokenBankTierHigh:
		return TokenBankArrayHigh
	case TokenBankTierMid:
		return TokenBankArrayMid
	default:
		return TokenBankArrayMid
	}
}

// TokenBankTierMultiplier is the settlement coefficient applied when crediting
// a provider for one tier: low 0.5, mid 1, high 2.
//
// It deliberately mirrors llmpool's capability band defaults so a tier means
// the same thing everywhere, but it is consumed only by Token Bank settlement
// and never reaches the consumer billing path.
func TokenBankTierMultiplier(tier string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case TokenBankTierLow:
		return llmpool.DefaultCapabilityBillingMultiplier(llmpool.OfficialTierLow), nil
	case TokenBankTierMid:
		return llmpool.DefaultCapabilityBillingMultiplier(llmpool.OfficialTierMid), nil
	case TokenBankTierHigh:
		return llmpool.DefaultCapabilityBillingMultiplier(llmpool.OfficialTierHigh), nil
	default:
		return 0, fmt.Errorf("%w: %s", ErrUnknownTokenBankTier, strings.TrimSpace(tier))
	}
}

// TokenBankTierMultiplierOfArray resolves the settlement rate for a member from
// the tier array it belongs to. The second result is false when the array is not
// a Token Bank tier array, in which case the caller must decide what to do.
func TokenBankTierMultiplierOfArray(arrayID string) (float64, bool) {
	tier, ok := TokenBankTierOfArray(arrayID)
	if !ok {
		return 0, false
	}
	multiplier, err := TokenBankTierMultiplier(tier)
	if err != nil {
		return 0, false
	}
	return multiplier, true
}

// EnsureTokenBankArrays seeds the three tier arrays. It is idempotent: a
// registry that already carries them is left untouched. Existing arrays are
// re-stamped as system arrays so an array created by hand before this shipped
// becomes protected instead of deletable.
//
// The billing fields are pinned rather than merely defaulted: the multiplier is
// forced back to a neutral 1 and any time-of-use schedule or timezone is
// cleared. Both would otherwise leak into the consumer's bill, and a schedule
// would silently win over the multiplier (llmpool.ResolveCreditMultiplier
// checks windows first). The display name is pinned the same way. Rename,
// update, and import refuse a different name, and a save does not copy a
// member name onto these arrays. This restamp still repairs a name stored
// before that guard. See the design doc, section 3.4 (A1/A3).
func (s *Service) EnsureTokenBankArrays(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("llm service is required")
	}
	return s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		changed := false
		for _, spec := range tokenBankArraySpecs() {
			arr := findProviderArray(reg, spec.ID)
			if arr == nil {
				reg.ProviderArrays = append(reg.ProviderArrays, llmpool.ProviderArray{
					ID:               spec.ID,
					Name:             spec.Name,
					CreditMultiplier: tokenBankArrayCreditMultiplier,
					Manual:           true,
					System:           true,
				})
				changed = true
				continue
			}
			if !arr.System {
				arr.System = true
				changed = true
			}
			if !arr.Manual {
				arr.Manual = true
				changed = true
			}
			if arr.Name != spec.Name {
				arr.Name = spec.Name
				changed = true
			}
			if arr.CreditMultiplier != tokenBankArrayCreditMultiplier {
				arr.CreditMultiplier = tokenBankArrayCreditMultiplier
				changed = true
			}
			if len(arr.CreditMultiplierSchedule) > 0 {
				arr.CreditMultiplierSchedule = nil
				changed = true
			}
			if strings.TrimSpace(arr.Timezone) != "" {
				arr.Timezone = ""
				changed = true
			}
		}
		return changed, nil
	})
}

// MoveProviderMemberToArray moves one member into an existing array. The
// member keeps its credentials and model list; only its array changes.
func (s *Service) MoveProviderMemberToArray(ctx context.Context, providerID, arrayID string) (string, error) {
	providerID = strings.TrimSpace(providerID)
	arrayID = strings.TrimSpace(arrayID)
	if s == nil || providerID == "" {
		return "", fmt.Errorf("provider id required")
	}
	if arrayID == "" {
		return "", fmt.Errorf("provider array id required")
	}
	var resolved string
	if err := s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		arr := findProviderArray(reg, arrayID)
		if arr == nil {
			return false, fmt.Errorf("%w: %s", ErrArrayNotFound, arrayID)
		}
		idx := providerIndex(reg, providerID)
		if idx < 0 {
			return false, fmt.Errorf("%w: %s", ErrProviderNotFound, providerID)
		}
		if strings.EqualFold(strings.TrimSpace(reg.Providers[idx].ArrayID), arr.ID) {
			resolved = arr.ID
			return false, nil
		}
		reg.Providers[idx].ArrayID = arr.ID
		reg.Providers[idx].ArrayName = arr.Name
		resolved = arr.ID
		return true, nil
	}); err != nil {
		return "", err
	}
	return resolved, nil
}

// SetProviderMemberTokenBankTier moves a member into the tier array. It does
// not write the share row, the settlement multiplier, or service-group routes.
// Labeling a published share goes through ApplyTokenBankMemberTier.
func (s *Service) SetProviderMemberTokenBankTier(ctx context.Context, providerID, tier string) (string, string, error) {
	arrayID, err := TokenBankArrayID(tier)
	if err != nil {
		return "", "", err
	}
	resolved, err := s.MoveProviderMemberToArray(ctx, providerID, arrayID)
	if err != nil {
		return "", "", err
	}
	arrayTier, _ := TokenBankTierOfArray(resolved)
	return resolved, arrayTier, nil
}

// ApplyTokenBankMemberTier labels one published Token Bank member.
//
// The member joins the tier array, and TokenBankTierMultiplier becomes the
// settlement rate for that tier (low 0.5, mid 1, high 2). Service-group routes
// that name this member, its current array, or any id in followArrayIDs move
// to the new array. A group that only routes the model through a different
// tier stays there. A tier array that no longer serves the model is pruned.
// Pause, canary, keys, dispatch weight, and model map stay. The array credit
// multiplier stays 1, so the consumer bill does not change.
//
// followArrayIDs covers a share row that still names the array the route
// uses after an earlier label moved only the registry member. An empty list
// follows the member's current array.
//
// This function does not take the route lock. A caller that also writes the
// share row holds WithTokenBankRouteLock around both writes. Taking the lock
// here would deadlock that caller. A repeat call that changes nothing does
// not rewrite the registry.
func (s *Service) ApplyTokenBankMemberTier(ctx context.Context, providerID, tier string, followArrayIDs ...string) error {
	providerID = strings.TrimSpace(providerID)
	if s == nil || providerID == "" {
		return fmt.Errorf("provider id required")
	}
	_, model, ok := ParseTokenBankMemberID(providerID)
	if !ok || strings.TrimSpace(model) == "" {
		return fmt.Errorf("provider is not a token bank member")
	}
	arrayID, err := TokenBankArrayID(tier)
	if err != nil {
		return err
	}
	multiplier, err := TokenBankTierMultiplier(tier)
	if err != nil {
		return err
	}
	tier = strings.ToLower(strings.TrimSpace(tier))
	return s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		arr := findProviderArray(reg, arrayID)
		if arr == nil {
			return false, fmt.Errorf("%w: %s", ErrArrayNotFound, arrayID)
		}
		idx := providerIndex(reg, providerID)
		if idx < 0 {
			return false, fmt.Errorf("%w: %s", ErrProviderNotFound, providerID)
		}
		provider := reg.Providers[idx]
		beforeArray := provider.ArrayID
		beforeTier := provider.TokenBankTier
		beforeRate := provider.TokenBankTierMultiplier
		beforeRoutes := modelRouteSignature(reg)
		follow := tokenBankFollowArrays(provider.ArrayID, followArrayIDs)
		provider.ArrayID = arr.ID
		provider.TokenBankTier = tier
		provider.TokenBankTierMultiplier = multiplier
		reg.Providers[idx] = provider
		retargetTokenBankModelRoutes(reg, provider.ID, model, arr.ID, follow)
		pruneTokenBankArrayRoutes(reg)
		changed := beforeArray != provider.ArrayID ||
			beforeTier != provider.TokenBankTier ||
			beforeRate != provider.TokenBankTierMultiplier ||
			modelRouteSignature(reg) != beforeRoutes
		return changed, nil
	})
}

func tokenBankFollowArrays(current string, extra []string) []string {
	out := make([]string, 0, 1+len(extra))
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
		out = append(out, id)
	}
	add(current)
	for _, id := range extra {
		add(id)
	}
	return out
}
