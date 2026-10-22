package llmservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// ProviderArrayMemberView is one upstream with its key redacted.
type ProviderArrayMemberView struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	APIURL               string            `json:"api_url"`
	Protocol             string            `json:"protocol,omitempty"`
	Models               []string          `json:"models"`
	Enabled              bool              `json:"enabled"`
	APIKeyConfigured     bool              `json:"api_key_configured"`
	APIKeyLast4          string            `json:"api_key_last4,omitempty"`
	Priority             int               `json:"priority"`
	DispatchWeight       int               `json:"dispatch_weight"`
	RequestsPerMinute    int               `json:"requests_per_minute"`
	RequestsPerDay       int               `json:"requests_per_day"`
	RateLimitCooldownSec int               `json:"rate_limit_cooldown_sec"`
	ModelMap             map[string]string `json:"model_map,omitempty"`
	AllowedNodeIDs       []string          `json:"allowed_node_ids"`
	CapabilityTags       []string          `json:"capability_tags"`
	ExtraBody            map[string]any    `json:"extra_body,omitempty"`
	ExtraBodyPriority    string            `json:"extra_body_priority,omitempty"`
	DropFields           []string          `json:"drop_fields,omitempty"`
	Health               MemberHealth      `json:"health"`
}

// ProviderArrayView is one logical provider and the members that share it.
type ProviderArrayView struct {
	ID       string                    `json:"id"`
	Name     string                    `json:"name"`
	Timezone string                    `json:"timezone,omitempty"`
	Members  []ProviderArrayMemberView `json:"members"`
}

// ProviderArrayInventory is the AI-facing catalog. Health counts live in the
// current process and reset when it restarts.
type ProviderArrayInventory struct {
	HealthScope string              `json:"health_scope"`
	Arrays      []ProviderArrayView `json:"arrays"`
}

// ProviderMemberPatch updates one member. Nil fields are left unchanged.
// ModelMapSet with a nil or empty ModelMap clears the mapping.
type ProviderMemberPatch struct {
	Enabled              *bool
	DispatchWeight       *int
	Priority             *int
	RequestsPerMinute    *int
	RequestsPerDay       *int
	RateLimitCooldownSec *int
	ModelMapSet          bool
	ModelMap             map[string]string
	AllowedNodeIDsSet    bool
	AllowedNodeIDs       []string
	CapabilityTagsSet    bool
	CapabilityTags       []string
	ArrayIDSet           bool
	ArrayID              string
	ExtraBodySet         bool
	ExtraBody            map[string]any
	ExtraBodyPrioritySet bool
	ExtraBodyPriority    string
	DropFieldsSet        bool
	DropFields           []string
}

// ListProviderArrayInventory returns arrays and members without upstream secrets.
func (s *Service) ListProviderArrayInventory(ctx context.Context) (*ProviderArrayInventory, error) {
	if s == nil {
		return nil, fmt.Errorf("llm service is required")
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	byID := map[string]llmpool.ProviderConfig{}
	for _, provider := range reg.Providers {
		byID[strings.ToLower(strings.TrimSpace(provider.ID))] = provider
	}
	out := &ProviderArrayInventory{HealthScope: "process", Arrays: []ProviderArrayView{}}
	for _, arr := range reg.ProviderArrays {
		id := strings.TrimSpace(arr.ID)
		if id == "" {
			continue
		}
		view := ProviderArrayView{
			ID:       id,
			Name:     strings.TrimSpace(arr.Name),
			Timezone: strings.TrimSpace(arr.Timezone),
			Members:  make([]ProviderArrayMemberView, 0, len(arr.MemberIDs)),
		}
		if view.Name == "" {
			view.Name = id
		}
		for _, memberID := range arr.MemberIDs {
			provider, ok := byID[strings.ToLower(strings.TrimSpace(memberID))]
			if !ok {
				continue
			}
			view.Members = append(view.Members, memberView(provider, now))
		}
		out.Arrays = append(out.Arrays, view)
	}
	return out, nil
}

func memberView(provider llmpool.ProviderConfig, now time.Time) ProviderArrayMemberView {
	configured, last4 := apiKeyDisclosure(provider.APIKey)
	protocol := strings.TrimSpace(provider.Protocol)
	if protocol == "" {
		protocol = "openai"
	}
	models := append([]string(nil), provider.Models...)
	if models == nil {
		models = []string{}
	}
	nodes := append([]string(nil), provider.AllowedNodeIDs...)
	if nodes == nil {
		nodes = []string{}
	}
	tags := append([]string(nil), provider.CapabilityTags...)
	if tags == nil {
		tags = []string{}
	}
	return ProviderArrayMemberView{
		ID:                   provider.ID,
		Name:                 provider.Name,
		APIURL:               provider.APIURL,
		Protocol:             protocol,
		Models:               models,
		Enabled:              !provider.Paused,
		APIKeyConfigured:     configured,
		APIKeyLast4:          last4,
		Priority:             provider.Priority,
		DispatchWeight:       provider.DispatchWeight,
		RequestsPerMinute:    provider.RequestsPerMinute,
		RequestsPerDay:       provider.RequestsPerDay,
		RateLimitCooldownSec: provider.RateLimitCooldownSec,
		ModelMap:             cloneStringMap(provider.ModelMap),
		AllowedNodeIDs:       nodes,
		CapabilityTags:       tags,
		ExtraBody:            cloneAnyMap(provider.ExtraBody),
		ExtraBodyPriority:    provider.ExtraBodyPriority,
		DropFields:           append([]string(nil), provider.DropFields...),
		Health:               memberHealthSnapshot(&provider, now),
	}
}

// MemberStatusView is the saved member as an automation client may see it:
// configuration plus process-local health, with the upstream key redacted.
func MemberStatusView(provider llmpool.ProviderConfig, now time.Time) ProviderArrayMemberView {
	return memberView(provider, now)
}

// apiKeyDisclosure reports whether a secret is stored and, when it is longer
// than four characters, its last four. Short secrets are not echoed.
func apiKeyDisclosure(secret string) (configured bool, last4 string) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false, ""
	}
	runes := []rune(secret)
	if len(runes) <= 4 {
		return true, ""
	}
	return true, string(runes[len(runes)-4:])
}

// PatchProviderMember applies a partial update. enabled false pauses dispatch.
func (s *Service) PatchProviderMember(ctx context.Context, id string, patch ProviderMemberPatch) error {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return fmt.Errorf("provider id required")
	}
	return s.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		idx := providerIndex(reg, id)
		if idx < 0 {
			return false, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
		}
		before := reg.Providers[idx]
		provider, err := applyProviderMemberPatch(reg, before, patch)
		if err != nil {
			return false, err
		}
		if !providerMemberPatchChanged(before, provider) {
			return false, nil
		}
		reg.Providers[idx] = provider
		return true, nil
	})
}

// PreviewProviderMember applies patch in memory and returns the member that
// would be stored. It does not write the registry.
func (s *Service) PreviewProviderMember(ctx context.Context, id string, patch ProviderMemberPatch) (llmpool.ProviderConfig, error) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return llmpool.ProviderConfig{}, fmt.Errorf("provider id required")
	}
	reg, err := s.LoadRegistry(ctx)
	if err != nil {
		return llmpool.ProviderConfig{}, err
	}
	idx := providerIndex(reg, id)
	if idx < 0 {
		return llmpool.ProviderConfig{}, fmt.Errorf("%w: %s", ErrProviderNotFound, id)
	}
	return applyProviderMemberPatch(reg, reg.Providers[idx], patch)
}

func applyProviderMemberPatch(reg *Registry, provider llmpool.ProviderConfig, patch ProviderMemberPatch) (llmpool.ProviderConfig, error) {
	if patch.Enabled != nil {
		provider.Paused = !*patch.Enabled
	}
	if patch.DispatchWeight != nil {
		provider.DispatchWeight = *patch.DispatchWeight
	}
	if patch.Priority != nil {
		provider.Priority = *patch.Priority
	}
	if patch.RequestsPerMinute != nil {
		provider.RequestsPerMinute = *patch.RequestsPerMinute
	}
	if patch.RequestsPerDay != nil {
		provider.RequestsPerDay = *patch.RequestsPerDay
	}
	if patch.RateLimitCooldownSec != nil {
		provider.RateLimitCooldownSec = *patch.RateLimitCooldownSec
	}
	if patch.ModelMapSet {
		provider.ModelMap = cloneStringMap(patch.ModelMap)
	}
	if patch.AllowedNodeIDsSet {
		provider.AllowedNodeIDs = append([]string(nil), patch.AllowedNodeIDs...)
		provider.AllowedNodes = ""
	}
	if patch.CapabilityTagsSet {
		provider.CapabilityTags = append([]string(nil), patch.CapabilityTags...)
	}
	if patch.ArrayIDSet {
		target := strings.TrimSpace(patch.ArrayID)
		if target == "" {
			return provider, fmt.Errorf("provider array id required")
		}
		arr := findProviderArray(reg, target)
		if arr == nil {
			return provider, fmt.Errorf("%w: %s", ErrArrayNotFound, target)
		}
		provider.ArrayID = arr.ID
		provider.ArrayName = arr.Name
	}
	if patch.ExtraBodySet {
		provider.ExtraBody = cloneAnyMap(patch.ExtraBody)
	}
	if patch.ExtraBodyPrioritySet {
		provider.ExtraBodyPriority = patch.ExtraBodyPriority
	}
	if patch.DropFieldsSet {
		if patch.DropFields == nil {
			provider.DropFields = []string{}
		} else {
			provider.DropFields = append([]string(nil), patch.DropFields...)
		}
	}
	if err := normalizeProviderUpstreamOverrides(&provider); err != nil {
		return provider, err
	}
	if err := validateMemberPolicy(&provider); err != nil {
		return provider, err
	}
	return provider, nil
}

func providerMemberPatchChanged(before, after llmpool.ProviderConfig) bool {
	return before.Paused != after.Paused ||
		before.DispatchWeight != after.DispatchWeight ||
		before.Priority != after.Priority ||
		before.RequestsPerMinute != after.RequestsPerMinute ||
		before.RequestsPerDay != after.RequestsPerDay ||
		before.RateLimitCooldownSec != after.RateLimitCooldownSec ||
		before.AllowedNodes != after.AllowedNodes ||
		!stringSliceEqual(before.AllowedNodeIDs, after.AllowedNodeIDs) ||
		!stringSliceEqual(before.CapabilityTags, after.CapabilityTags) ||
		!stringMapEqual(before.ModelMap, after.ModelMap) ||
		!strings.EqualFold(strings.TrimSpace(before.ArrayID), strings.TrimSpace(after.ArrayID)) ||
		!memberUpstreamOverridesEqual(before, after)
}

func memberUpstreamOverridesEqual(before, after llmpool.ProviderConfig) bool {
	if before.ExtraBodyPriority != after.ExtraBodyPriority || !stringSliceEqual(before.DropFields, after.DropFields) {
		return false
	}
	left, errLeft := json.Marshal(before.ExtraBody)
	right, errRight := json.Marshal(after.ExtraBody)
	if errLeft != nil || errRight != nil {
		return false
	}
	return string(left) == string(right)
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
