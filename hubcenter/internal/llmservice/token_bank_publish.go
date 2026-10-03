package llmservice

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// tokenBankRouteMu orders share routing changes. Republish, pause, resume,
// take-out, automatic pause, and a tier regrade all take it. The database
// write and the registry write of one change stay inside one hold, so a resume
// cannot land between an automatic pause's two writes and leave the row active
// while the member stays paused.
var tokenBankRouteMu sync.Mutex

// WithTokenBankRouteLock runs fn while share routing changes are excluded.
func WithTokenBankRouteLock(fn func() error) error {
	if fn == nil {
		return nil
	}
	tokenBankRouteMu.Lock()
	defer tokenBankRouteMu.Unlock()
	return fn()
}

// Token Bank share → registry publication.
//
// A share row in the store (token_bank_shares / token_bank_models) is only a
// record. What actually makes a shared model reachable is a registry member:
// dispatch matches on provider id, and a provider only enters a service group's
// routing pool through that group's ModelConfig. This file is the bridge, and
// it exists because the two halves were written separately — the store half in
// P0-1/P0-2, and the routing half here. Without it the store fills up with
// shares that no request can ever reach.
//
// Three invariants, each of which has a concrete failure behind it:
//
//  1. Member ids are derived, never generated (TokenBankMemberID). The id is
//     the only link back from a dispatched provider to the share that owns it,
//     so settlement can recover share_id, owner and tier from the provider id
//     alone. A random id would make every settled call unattributable.
//
//  2. The array is a function of the tier, never independent (§3.4, A1). The
//     tier arrays carry a pinned multiplier of 1.0 so the tier rate cannot
//     reach the consumer's bill; the tier itself lives on the provider's
//     ArrayID, which is what "regrading a model" actually moves.
//
//  3. Removal is idempotent and complete. Taking a share out must leave no
//     member behind in any service group, or dispatch keeps routing to an
//     upstream whose key was erased.

// tokenBankMemberPrefix is the provider-id prefix for every Token Bank member.
// Settlement and the admin UI both parse it, so it is one constant, not a
// literal repeated at each call site.
const tokenBankMemberPrefix = "tbk_"

// tokenBankMemberSeparator splits the share id from the model in a member id.
// The share id is a generated opaque id and never contains it; the model name
// is encoded (see encodeTokenBankModel) precisely so that it might.
const tokenBankMemberSeparator = "__"

// TokenBankMemberID builds the registry provider id for one model of a share.
//
// The model name is base64url-encoded rather than embedded raw. Model names can
// contain '/', ':' and '@' (e.g. "meta-llama/Llama-3.3-70B-Instruct"), all of
// which are meaningful in provider ids, route ids and URLs elsewhere in the
// system, and a raw name would also collide a model containing the separator
// with a different (share, model) pair.
func TokenBankMemberID(shareID, model string) string {
	shareID = strings.TrimSpace(shareID)
	model = strings.TrimSpace(model)
	if shareID == "" || model == "" {
		return ""
	}
	return tokenBankMemberPrefix + shareID + tokenBankMemberSeparator + encodeTokenBankModel(model)
}

// ParseTokenBankMemberID recovers the share id and model from a provider id.
// The second result is false for any id that is not a Token Bank member, so a
// caller can use it as a cheap type test without a separate prefix check.
func ParseTokenBankMemberID(providerID string) (shareID, model string, ok bool) {
	providerID = strings.TrimSpace(providerID)
	if !strings.HasPrefix(providerID, tokenBankMemberPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(providerID, tokenBankMemberPrefix)
	idx := strings.Index(rest, tokenBankMemberSeparator)
	if idx <= 0 {
		return "", "", false
	}
	shareID = rest[:idx]
	decoded, err := decodeTokenBankModel(rest[idx+len(tokenBankMemberSeparator):])
	if err != nil || decoded == "" {
		return "", "", false
	}
	return shareID, decoded, true
}

// IsTokenBankMemberID reports whether an id names a Token Bank member.
func IsTokenBankMemberID(providerID string) bool {
	_, _, ok := ParseTokenBankMemberID(providerID)
	return ok
}

// encodeTokenBankModel is base64url without padding: URL-safe, filesystem-safe
// and case-preserving, and the encoding is fixed-length per input length so a
// re-encode always compares equal.
func encodeTokenBankModel(model string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(model)))
}

func decodeTokenBankModel(encoded string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// TokenBankPublishSpec is one model to publish as a registry member.
//
// It carries the decrypted upstream credential because building the member is
// the only thing that may see it: everything else — the store, the admin API,
// the automation API — handles the share row, which keeps the key encrypted.
type TokenBankPublishSpec struct {
	ShareID        string
	OwnerUserID    string
	DisplayName    string
	Model          string
	ArrayID        string
	APIURL         string
	APIKey         string
	Protocol       string
	ServiceGroupID string
	// MaxInputTokensPerRequest / MaxOutputTokensPerRequest are the share-level
	// caps (§3.1). They are enforced by the proxy, not here; the publisher only
	// has to make sure a regrade does not silently drop them.
	MaxInputTokensPerRequest  int64
	MaxOutputTokensPerRequest int64
	Tier                      string
	TierMultiplier            float64
	// Visibility and Audiences are copied onto every member so dispatch can
	// enforce a private share without a store read on the request path.
	Visibility string
	Audiences  []llmpool.TokenBankAudience
	// CanaryUntil is applied only when the member is new. A republish keeps
	// the deadline already on the registry member.
	CanaryUntil time.Time
	// ExtraKeys are plaintext upstream keys rotated with APIKey. They are
	// already decrypted by the caller; the store keeps only envelopes.
	ExtraKeys []string
	// SharePaused is the share row's paused status at load time. A new member
	// has no registry pause to copy, so without this a republish puts a paused
	// share back into traffic.
	SharePaused bool
	// ShareWindow limits when dispatch may dial this member. The zero value
	// means always. It is not copied onto CreditMultiplierSchedule.
	ShareWindow llmpool.TokenBankShareWindow
}

// TokenBankPublishResult reports what publication changed, so the HTTP layer
// can tell "created" from "already published" without a second registry read.
type TokenBankPublishResult struct {
	Added   []string // member ids that did not exist before
	Updated []string // member ids that existed and were refreshed
	Removed []string // member ids removed because their model is no longer published
}

// PublishTokenBankShare makes the given models reachable and removes any member
// of this share that is no longer in the list.
//
// It is a full reconciliation of one share rather than a per-model upsert,
// because the interesting failure is not "a new model was added" — it is "a
// model was removed or disabled and the stale member kept serving traffic to a
// credential the owner believes they revoked".
func (s *Service) PublishTokenBankShare(ctx context.Context, specs []TokenBankPublishSpec) (TokenBankPublishResult, error) {
	var result TokenBankPublishResult
	if s == nil {
		return result, fmt.Errorf("llm service is required")
	}
	byMember := make(map[string]TokenBankPublishSpec, len(specs))
	var shareID string
	for _, spec := range specs {
		memberID := TokenBankMemberID(spec.ShareID, spec.Model)
		if memberID == "" {
			continue
		}
		if shareID == "" {
			shareID = strings.TrimSpace(spec.ShareID)
		}
		byMember[memberID] = spec
	}
	if shareID == "" {
		// Nothing to publish is not an error: a share whose every model failed
		// probing publishes zero members, and that is a valid state.
		return result, nil
	}

	err := s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		result = TokenBankPublishResult{}
		changed := false
		seen := make(map[string]struct{}, len(byMember))
		for memberID, spec := range byMember {
			seen[memberID] = struct{}{}
			provider := tokenBankMemberProvider(spec)
			idx := providerIndex(reg, memberID)
			if idx < 0 {
				provider.Paused = spec.SharePaused || tokenBankShareHasPausedMember(reg, spec.ShareID)
				reg.Providers = append(reg.Providers, provider)
				result.Added = append(result.Added, memberID)
				changed = true
				continue
			}
			// Refresh share-owned fields in place. Rebuilding the member from the
			// share row would drop operator edits the row does not carry.
			merged := mergeRepublishedTokenBankMember(reg.Providers[idx], provider, spec.SharePaused)
			if merged.Sequence <= 0 {
				merged.Sequence = nextProviderSequence(reg)
			}
			reg.Providers[idx] = merged
			result.Updated = append(result.Updated, memberID)
			changed = true
		}

		// Drop members of this share that are no longer published. Collect
		// first, filter second: the detach step needs to resolve each removed
		// member's array, and the provider rows must still be present for it.
		var removed []string
		keep := make([]llmpool.ProviderConfig, 0, len(reg.Providers))
		for _, provider := range reg.Providers {
			memberShareID, _, isMember := ParseTokenBankMemberID(provider.ID)
			if isMember && strings.EqualFold(memberShareID, shareID) {
				if _, stillPublished := seen[strings.TrimSpace(provider.ID)]; !stillPublished {
					removed = append(removed, strings.TrimSpace(provider.ID))
					continue
				}
			}
			keep = append(keep, provider)
		}
		if len(removed) > 0 {
			// A removed member may still be referenced by a service group's
			// routing table. Leaving the reference would keep it in the
			// dispatch candidate list where it resolves to nothing.
			detachTokenBankMembersFromServiceGroups(reg, removed)
			reg.Providers = keep
			result.Removed = removed
			changed = true
		}

		if !changed {
			return false, nil
		}
		attachTokenBankMembersToServiceGroups(reg, byMember)
		// A tier change or a retired model leaves the old array on that model
		// while other models still occupy the array. Detach only drops an array
		// that has no members left, so prune per model after the new routes exist.
		pruneTokenBankArrayRoutes(reg)
		return true, nil
	})
	if err != nil {
		return TokenBankPublishResult{}, err
	}
	return result, nil
}

// UnpublishTokenBankShare removes every member of a share. It is the routing
// half of "取出" (take out): the store row is deleted by the caller, and this
// must run as well or the model keeps being dispatched with a key that is about
// to be erased.
func (s *Service) UnpublishTokenBankShare(ctx context.Context, shareID string) ([]string, error) {
	shareID = strings.TrimSpace(shareID)
	if s == nil || shareID == "" {
		return nil, nil
	}
	var removed []string
	err := s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		removed = nil
		kept := reg.Providers[:0]
		for _, provider := range reg.Providers {
			memberShareID, _, isMember := ParseTokenBankMemberID(provider.ID)
			if !isMember || !strings.EqualFold(memberShareID, shareID) {
				kept = append(kept, provider)
				continue
			}
			removed = append(removed, strings.TrimSpace(provider.ID))
		}
		if len(removed) == 0 {
			return false, nil
		}
		// Detach before the providers leave the list: resolving a member's
		// array needs the provider row, and it is gone one line later.
		detachTokenBankMembersFromServiceGroups(reg, removed)
		reg.Providers = kept
		pruneTokenBankArrayRoutes(reg)
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

// tokenBankShareHasPausedMember reports whether this share already has a
// paused member. A model published after the pause must start paused too.
func tokenBankShareHasPausedMember(reg *Registry, shareID string) bool {
	shareID = strings.TrimSpace(shareID)
	if reg == nil || shareID == "" {
		return false
	}
	for i := range reg.Providers {
		memberShareID, _, ok := ParseTokenBankMemberID(reg.Providers[i].ID)
		if ok && strings.EqualFold(memberShareID, shareID) && reg.Providers[i].Paused {
			return true
		}
	}
	return false
}

// SetTokenBankSharePaused pauses or resumes every member of a share at once.
// Pausing a share must pause all of its models: leaving some live while the
// share reads "paused" is exactly the state an owner pauses to escape.
func (s *Service) SetTokenBankSharePaused(ctx context.Context, shareID string, paused bool) (int, error) {
	shareID = strings.TrimSpace(shareID)
	if s == nil || shareID == "" {
		return 0, nil
	}
	affected := 0
	err := s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		affected = 0
		changed := false
		for i := range reg.Providers {
			memberShareID, _, isMember := ParseTokenBankMemberID(reg.Providers[i].ID)
			if !isMember || !strings.EqualFold(memberShareID, shareID) {
				continue
			}
			affected++
			if reg.Providers[i].Paused != paused {
				reg.Providers[i].Paused = paused
				changed = true
			}
		}
		return changed, nil
	})
	if err != nil {
		return 0, err
	}
	return affected, nil
}

// SetTokenBankMemberKey replaces the upstream credential of every model of a
// share in place. The member ids, arrays and service-group membership are
// deliberately untouched: the point of a key rotation is that nothing else
// about the share changes.
func (s *Service) SetTokenBankMemberKey(ctx context.Context, shareID, apiURL, apiKey, protocol string) (int, error) {
	shareID = strings.TrimSpace(shareID)
	if s == nil || shareID == "" {
		return 0, nil
	}
	affected := 0
	err := s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		affected = 0
		changed := false
		for i := range reg.Providers {
			memberShareID, _, isMember := ParseTokenBankMemberID(reg.Providers[i].ID)
			if !isMember || !strings.EqualFold(memberShareID, shareID) {
				continue
			}
			affected++
			if url := strings.TrimSpace(apiURL); url != "" && reg.Providers[i].APIURL != url {
				reg.Providers[i].APIURL = url
				changed = true
			}
			if key := strings.TrimSpace(apiKey); key != "" && reg.Providers[i].APIKey != key {
				reg.Providers[i].APIKey = key
				changed = true
			}
			if proto := strings.TrimSpace(protocol); proto != "" && reg.Providers[i].Protocol != proto {
				reg.Providers[i].Protocol = proto
				changed = true
			}
		}
		return changed, nil
	})
	if err != nil {
		return 0, err
	}
	return affected, nil
}

// TokenBankShareMemberIDs lists the registry members currently published for a
// share. The admin UI uses it to show whether a store row is actually wired
// into routing, which is the difference between "shared" and "shared and
// reachable".
func (s *Service) TokenBankShareMemberIDs(ctx context.Context, shareID string) ([]string, error) {
	shareID = strings.TrimSpace(shareID)
	if s == nil || shareID == "" {
		return nil, nil
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, provider := range reg.Providers {
		memberShareID, _, isMember := ParseTokenBankMemberID(provider.ID)
		if isMember && strings.EqualFold(memberShareID, shareID) {
			out = append(out, strings.TrimSpace(provider.ID))
		}
	}
	return out, nil
}

// mergeRepublishedTokenBankMember overlays a freshly built share member onto
// the registry row that already serves that model.
//
// The share owns credentials, tier, window, audiences, and caps. The provider
// editor owns capability tags, dispatch weight, model map, rate limits, node
// allowlist, and the other dial settings. Pause, sequence, and the canary
// deadline stay as well: an empty canary means this member is already at full
// share, and a republish must not open a new window.
//
// The consumer multiplier and its schedule are cleared. The tier array pins
// that rate, and a schedule on the member would change the bill. The member's
// own token price stays: a tier array has no price, and wiping the member
// price leaves the route with nothing to settle. Account login is cleared so
// the static API key is not turned into a WorkBuddy session.
func mergeRepublishedTokenBankMember(existing, fresh llmpool.ProviderConfig, sharePaused bool) llmpool.ProviderConfig {
	merged := existing
	merged.Name = fresh.Name
	merged.APIURL = fresh.APIURL
	merged.APIKey = fresh.APIKey
	merged.Protocol = fresh.Protocol
	merged.Models = append([]string(nil), fresh.Models...)
	merged.ArrayID = fresh.ArrayID
	merged.MaxInputTokensPerRequest = fresh.MaxInputTokensPerRequest
	merged.MaxOutputTokensPerRequest = fresh.MaxOutputTokensPerRequest
	merged.TokenBankOwnerUserID = fresh.TokenBankOwnerUserID
	merged.TokenBankTier = fresh.TokenBankTier
	merged.TokenBankTierMultiplier = fresh.TokenBankTierMultiplier
	merged.TokenBankShareDisplayName = fresh.TokenBankShareDisplayName
	merged.TokenBankVisibility = fresh.TokenBankVisibility
	merged.TokenBankAudiences = append([]llmpool.TokenBankAudience(nil), fresh.TokenBankAudiences...)
	merged.TokenBankExtraKeys = append([]string(nil), fresh.TokenBankExtraKeys...)
	merged.TokenBankShareWindow = fresh.TokenBankShareWindow
	merged.TokenBankShareWindow.Days = append([]int(nil), fresh.TokenBankShareWindow.Days...)
	merged.CreditMultiplier = 0
	merged.CreditMultiplierSchedule = nil
	merged.Timezone = ""
	merged.AuthKind = ""
	merged.AllowedNodes = ""
	merged.ArrayName = ""
	merged.ArrayIndependent = false
	clearWorkBuddySecrets(&merged)
	merged.WorkBuddySessionID = ""
	if sharePaused {
		merged.Paused = true
	}
	merged.CapabilityTags = append([]string(nil), existing.CapabilityTags...)
	merged.AllowedNodeIDs = append([]string(nil), existing.AllowedNodeIDs...)
	merged.ModelMap = cloneStringMap(existing.ModelMap)
	merged.TokenPricing = existing.TokenPricing.Clone()
	return merged
}

// tokenBankMemberProvider builds the registry member for one published model.
func tokenBankMemberProvider(spec TokenBankPublishSpec) llmpool.ProviderConfig {
	arrayID := strings.TrimSpace(spec.ArrayID)
	if _, ok := TokenBankTierOfArray(arrayID); !ok {
		// An unrecognized array is normalized to the mid tier rather than
		// rejected: the tier is metadata on the share, and a bad value must not
		// make a model unreachable. The admin regrade path has its own stricter
		// validation.
		arrayID = TokenBankArrayMid
	}
	protocol := strings.TrimSpace(spec.Protocol)
	if protocol == "" {
		protocol = "openai"
	}
	model := strings.TrimSpace(spec.Model)
	displayName := strings.TrimSpace(spec.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(spec.ShareID)
	}
	multiplier := spec.TierMultiplier
	if multiplier <= 0 && strings.TrimSpace(spec.OwnerUserID) != "" {
		multiplier = 1
	}
	provider := llmpool.ProviderConfig{
		ID:       TokenBankMemberID(spec.ShareID, model),
		Name:     "Token Bank · " + displayName + " · " + model,
		APIURL:   strings.TrimSpace(spec.APIURL),
		APIKey:   strings.TrimSpace(spec.APIKey),
		Protocol: protocol,
		Models:   []string{model},
		// The array is the tier, and the tier array's own multiplier is pinned
		// at 1.0, so no billing field is set here on purpose. Setting a
		// multiplier would leak the settlement rate into the consumer's bill
		// (see the design doc, §3.4-A1, and tokenBankArrayCreditMultiplier).
		// ShareWindow is a dial window only. It is not a credit schedule.
		ArrayID:                   arrayID,
		MaxInputTokensPerRequest:  spec.MaxInputTokensPerRequest,
		MaxOutputTokensPerRequest: spec.MaxOutputTokensPerRequest,
		TokenBankOwnerUserID:      strings.TrimSpace(spec.OwnerUserID),
		TokenBankTier:             strings.TrimSpace(spec.Tier),
		TokenBankTierMultiplier:   multiplier,
		TokenBankShareDisplayName: displayName,
		TokenBankVisibility:       strings.TrimSpace(spec.Visibility),
		TokenBankAudiences:        append([]llmpool.TokenBankAudience(nil), spec.Audiences...),
		TokenBankExtraKeys:        append([]string(nil), spec.ExtraKeys...),
		TokenBankShareWindow:      spec.ShareWindow,
	}
	provider.TokenBankShareWindow.Days = append([]int(nil), spec.ShareWindow.Days...)
	if !spec.CanaryUntil.IsZero() {
		provider.TokenBankCanaryUntil = spec.CanaryUntil.UTC().Format(time.RFC3339)
	}
	return provider
}

// detachTokenBankMembersFromServiceGroups drops a withdrawn member from every
// service group's routing tables.
//
// The route target is the member's array. The array is removed from every
// model only when it has no members left. Removing it earlier would take the
// other models in that tier offline. A model whose own members are gone while
// the array still holds other models is removed later by pruneTokenBankArrayRoutes.
func detachTokenBankMembersFromServiceGroups(reg *Registry, providerIDs []string) {
	if reg == nil || len(providerIDs) == 0 {
		return
	}
	arrayIDs := map[string]string{}
	empty := map[string]bool{}
	for _, providerID := range providerIDs {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			continue
		}
		idx := providerIndex(reg, providerID)
		if idx < 0 {
			continue
		}
		arrayID := canonicalProviderArrayID(reg.Providers[idx])
		if arrayID == "" {
			continue
		}
		arrayIDs[strings.ToLower(providerID)] = arrayID
		if !arrayHasOtherMembersExcept(reg, arrayID, providerIDs) {
			empty[strings.ToLower(arrayID)] = true
		}
	}
	if len(arrayIDs) == 0 {
		return
	}
	targets := map[string]struct{}{}
	for key, arrayID := range arrayIDs {
		// The member id itself is always removed (it may appear as a legacy
		// per-member route from before arrays existed).
		targets[key] = struct{}{}
		if empty[strings.ToLower(arrayID)] {
			targets[strings.ToLower(arrayID)] = struct{}{}
		}
	}
	for gi := range reg.ServiceGroups {
		group := &reg.ServiceGroups[gi]
		for mi := range group.Models {
			model := &group.Models[mi]
			model.ProviderIDs = withoutProviderIDs(model.ProviderIDs, targets)
			kept := model.ProviderConfigs[:0]
			for _, pc := range model.ProviderConfigs {
				if _, drop := targets[strings.ToLower(strings.TrimSpace(pc.ProviderID))]; drop {
					continue
				}
				kept = append(kept, pc)
			}
			model.ProviderConfigs = kept
		}
	}
}

// arrayHasOtherMembersExcept reports whether the array still holds a member
// outside the given set. The removed providers are still present in the
// registry at call time, so they must be excluded explicitly.
func arrayHasOtherMembersExcept(reg *Registry, arrayID string, removing []string) bool {
	if reg == nil {
		return false
	}
	gone := make(map[string]struct{}, len(removing))
	for _, id := range removing {
		gone[strings.ToLower(strings.TrimSpace(id))] = struct{}{}
	}
	for _, provider := range reg.Providers {
		id := strings.ToLower(strings.TrimSpace(provider.ID))
		if _, isGone := gone[id]; isGone {
			continue
		}
		if strings.EqualFold(canonicalProviderArrayID(provider), strings.TrimSpace(arrayID)) {
			return true
		}
	}
	return false
}

func withoutProviderIDs(ids []string, drop map[string]struct{}) []string {
	if len(ids) == 0 {
		return ids
	}
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, gone := drop[strings.ToLower(strings.TrimSpace(id))]; gone {
			continue
		}
		kept = append(kept, id)
	}
	return kept
}

// attachTokenBankMembersToServiceGroups adds each published model's ARRAY to
// the service group the spec names, keyed by its logical model name.
//
// The route target is the tier array, not the member, and that is the whole
// point of §3.4: several owners may share a model named "gpt-4o", and they all
// land in one array so the group routes to the array and the array load-balances
// across members. Adding the member id directly would also be undone on the
// next registry save by collapseServiceGroupArrayRoutes, which rewrites every
// member route to its containing array.
func attachTokenBankMembersToServiceGroups(reg *Registry, specs map[string]TokenBankPublishSpec) {
	if reg == nil || len(specs) == 0 {
		return
	}
	for _, spec := range specs {
		group := findServiceGroupByID(reg, spec.ServiceGroupID)
		if group == nil {
			continue
		}
		modelName := strings.TrimSpace(spec.Model)
		if modelName == "" {
			continue
		}
		arrayID := strings.TrimSpace(spec.ArrayID)
		if _, ok := TokenBankTierOfArray(arrayID); !ok {
			arrayID = TokenBankArrayMid
		}
		model := findOrCreateModelConfig(group, modelName)
		if !containsProviderID(model.ProviderIDs, arrayID) {
			model.ProviderIDs = append(model.ProviderIDs, arrayID)
		}
		if !containsProviderConfig(model.ProviderConfigs, arrayID) {
			model.ProviderConfigs = append(model.ProviderConfigs, llmpool.ModelProviderConfig{
				ProviderID: arrayID,
			})
		}
	}
}

func findServiceGroupByID(reg *Registry, id string) *llmpool.ServiceGroup {
	id = strings.TrimSpace(id)
	if reg == nil || id == "" {
		return nil
	}
	for i := range reg.ServiceGroups {
		if strings.EqualFold(strings.TrimSpace(reg.ServiceGroups[i].ID), id) {
			return &reg.ServiceGroups[i]
		}
	}
	return nil
}

func findOrCreateModelConfig(group *llmpool.ServiceGroup, name string) *llmpool.ModelConfig {
	for i := range group.Models {
		if strings.EqualFold(strings.TrimSpace(group.Models[i].Name), name) {
			return &group.Models[i]
		}
	}
	group.Models = append(group.Models, llmpool.ModelConfig{Name: name})
	return &group.Models[len(group.Models)-1]
}

func containsProviderID(ids []string, providerID string) bool {
	for _, id := range ids {
		if strings.EqualFold(strings.TrimSpace(id), providerID) {
			return true
		}
	}
	return false
}

// pruneTokenBankArrayRoutes drops a tier array from a model that no remaining
// member of that array serves. The array stays on every model that still has
// a member. Leaving the route would keep the model in the dispatch list with
// nothing that can answer it.
func pruneTokenBankArrayRoutes(reg *Registry) {
	if reg == nil {
		return
	}
	for gi := range reg.ServiceGroups {
		group := &reg.ServiceGroups[gi]
		for mi := range group.Models {
			model := &group.Models[mi]
			drop := map[string]struct{}{}
			note := func(id string) {
				id = strings.TrimSpace(id)
				if !IsTokenBankArray(id) {
					return
				}
				if tokenBankArrayServesModel(reg, id, model.Name) {
					return
				}
				drop[strings.ToLower(id)] = struct{}{}
			}
			for _, id := range model.ProviderIDs {
				note(id)
			}
			for _, pc := range model.ProviderConfigs {
				note(pc.ProviderID)
			}
			if len(drop) == 0 {
				continue
			}
			model.ProviderIDs = withoutProviderIDs(model.ProviderIDs, drop)
			kept := model.ProviderConfigs[:0]
			for _, pc := range model.ProviderConfigs {
				if _, gone := drop[strings.ToLower(strings.TrimSpace(pc.ProviderID))]; gone {
					continue
				}
				kept = append(kept, pc)
			}
			model.ProviderConfigs = kept
		}
	}
}

// tokenBankArrayServesModel reports whether this array still has a member that
// dispatch would use for the named model. A non-bank member stays, because the
// model filter does not drop it. A billing band is not a model id: any member
// of the tier can answer official-low, official-mid, official-high, or auto,
// using the model encoded in its own id.
func tokenBankArrayServesModel(reg *Registry, arrayID, model string) bool {
	arrayID = strings.TrimSpace(arrayID)
	model = strings.TrimSpace(model)
	if reg == nil || arrayID == "" || model == "" {
		return false
	}
	band := isBillingBandName(model)
	for i := range reg.Providers {
		provider := reg.Providers[i]
		if !strings.EqualFold(canonicalProviderArrayID(provider), arrayID) {
			continue
		}
		served, ok := tokenBankServedModel(&provider)
		if band || !ok || strings.EqualFold(served, model) {
			return true
		}
	}
	return false
}

func containsProviderConfig(configs []llmpool.ModelProviderConfig, providerID string) bool {
	for _, pc := range configs {
		if strings.EqualFold(strings.TrimSpace(pc.ProviderID), providerID) {
			return true
		}
	}
	return false
}

// retargetTokenBankModelRoutes adds newArray to routes of this model that name
// the member or one of the arrays it is leaving. A service group that routes
// the model only through a different tier is left alone. That includes a tier
// array that no longer has a member: prune drops the empty array, and a repeat
// label must not subscribe that group to this member's tier.
func retargetTokenBankModelRoutes(reg *Registry, memberID, model, newArray string, followArrays []string) {
	if reg == nil {
		return
	}
	model = strings.TrimSpace(model)
	newArray = strings.TrimSpace(newArray)
	memberID = strings.TrimSpace(memberID)
	if model == "" || newArray == "" {
		return
	}
	for gi := range reg.ServiceGroups {
		group := &reg.ServiceGroups[gi]
		for mi := range group.Models {
			cfg := &group.Models[mi]
			if !strings.EqualFold(strings.TrimSpace(cfg.Name), model) {
				continue
			}
			if !tokenBankRouteFollowsMember(cfg, memberID, followArrays) {
				continue
			}
			addModelRouteTarget(cfg, newArray)
		}
	}
}

func tokenBankRouteFollowsMember(cfg *llmpool.ModelConfig, memberID string, followArrays []string) bool {
	if routeContainsProvider(cfg, memberID) {
		return true
	}
	for _, id := range followArrays {
		if routeContainsProvider(cfg, id) {
			return true
		}
	}
	return false
}

// addModelRouteTarget appends providerID to a model route.
//
// An empty ProviderConfigs list means ProviderIDs is the source of truth.
// The next save copies those ids forward. Putting the new id into a fresh
// one-entry config list would drop every other provider on that save.
func addModelRouteTarget(cfg *llmpool.ModelConfig, providerID string) {
	if cfg == nil {
		return
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return
	}
	if len(cfg.ProviderConfigs) == 0 {
		if !containsProviderID(cfg.ProviderIDs, providerID) {
			cfg.ProviderIDs = append(cfg.ProviderIDs, providerID)
		}
		return
	}
	if !containsProviderID(cfg.ProviderIDs, providerID) {
		cfg.ProviderIDs = append(cfg.ProviderIDs, providerID)
	}
	if !containsProviderConfig(cfg.ProviderConfigs, providerID) {
		cfg.ProviderConfigs = append(cfg.ProviderConfigs, llmpool.ModelProviderConfig{
			ProviderID: providerID,
		})
	}
}

func routeContainsProvider(cfg *llmpool.ModelConfig, providerID string) bool {
	providerID = strings.TrimSpace(providerID)
	if cfg == nil || providerID == "" {
		return false
	}
	return containsProviderID(cfg.ProviderIDs, providerID) || containsProviderConfig(cfg.ProviderConfigs, providerID)
}

func modelRouteSignature(reg *Registry) string {
	if reg == nil {
		return ""
	}
	var b strings.Builder
	for _, group := range reg.ServiceGroups {
		for _, model := range group.Models {
			b.WriteString(group.ID)
			b.WriteByte('|')
			b.WriteString(model.Name)
			b.WriteByte('|')
			for _, id := range model.ProviderIDs {
				b.WriteString(id)
				b.WriteByte(',')
			}
			b.WriteByte('|')
			for _, pc := range model.ProviderConfigs {
				b.WriteString(pc.ProviderID)
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}
