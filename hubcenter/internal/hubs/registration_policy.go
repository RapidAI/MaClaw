package hubs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

const systemKeyHubRegistrationPolicies = "hub_registration_policies"

var ErrInvalidRegistrationPolicy = errors.New("invalid registration policy")

type HubRegistrationPolicyConfig struct {
	HubOrigin          string                                       `json:"hub_origin"`
	DefaultSignupScope string                                       `json:"default_signup_scope"`
	Tenants            map[string]store.HubTenantRegistrationPolicy `json:"tenants"`
}

type hubRegistrationPolicyStore struct {
	Hubs map[string]HubRegistrationPolicyConfig `json:"hubs"`
}

type UpdateHubRegistrationPolicyRequest struct {
	HubOrigin          string                                `json:"hub_origin,omitempty"`
	DefaultSignupScope string                                `json:"default_signup_scope,omitempty"`
	Tenant             UpdateTenantRegistrationPolicyRequest `json:"tenant,omitempty"`
}

type UpdateTenantRegistrationPolicyRequest struct {
	TenantID                string `json:"tenant_id,omitempty"`
	TenantName              string `json:"tenant_name,omitempty"`
	SignupScope             string `json:"signup_scope,omitempty"`
	IsPublicFallback        *bool  `json:"is_public_fallback,omitempty"`
	InviteEnabled           *bool  `json:"invite_enabled,omitempty"`
	MaxActiveInvites        int    `json:"max_active_invites,omitempty"`
	MonthlyInviteQuota      int    `json:"monthly_invite_quota,omitempty"`
	PerInviteMaxUsesDefault int    `json:"per_invite_max_uses_default,omitempty"`
	PerInviteMaxUsesMax     int    `json:"per_invite_max_uses_max,omitempty"`
	Status                  string `json:"status,omitempty"`
}

func (s *Service) HubRegistrationPolicies(ctx context.Context) (map[string]HubRegistrationPolicyConfig, error) {
	state, err := s.loadRegistrationPolicyStore(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]HubRegistrationPolicyConfig, len(state.Hubs))
	for hubID, cfg := range state.Hubs {
		out[hubID] = normalizeHubRegistrationPolicyConfig(cfg)
	}
	if s != nil && s.hubs != nil {
		hubs, err := s.hubs.ListAll(ctx)
		if err != nil {
			return nil, err
		}
		for _, hub := range hubs {
			if hub == nil || strings.TrimSpace(hub.ID) == "" {
				continue
			}
			if _, ok := out[hub.ID]; ok {
				continue
			}
			out[hub.ID] = hubRegistrationPolicyConfigFromRow(hub)
		}
	}
	return out, nil
}

func (s *Service) UpdateHubRegistrationPolicy(ctx context.Context, hubID string, req UpdateHubRegistrationPolicyRequest) (HubRegistrationPolicyConfig, error) {
	hubID = strings.TrimSpace(hubID)
	if hubID == "" {
		return HubRegistrationPolicyConfig{}, ErrHubNotFound
	}
	hub, err := s.hubs.GetByID(ctx, hubID)
	if err != nil {
		return HubRegistrationPolicyConfig{}, err
	}
	if hub == nil {
		return HubRegistrationPolicyConfig{}, ErrHubNotFound
	}
	state, err := s.loadRegistrationPolicyStore(ctx)
	if err != nil {
		return HubRegistrationPolicyConfig{}, err
	}
	cfg, ok := state.Hubs[hubID]
	if !ok {
		cfg = hubRegistrationPolicyConfigFromRow(hub)
	}
	cfg = normalizeHubRegistrationPolicyConfig(cfg)
	if strings.TrimSpace(req.HubOrigin) != "" {
		cfg.HubOrigin = normalizeHubOrigin(req.HubOrigin)
	}
	if strings.TrimSpace(req.DefaultSignupScope) != "" {
		cfg.DefaultSignupScope = normalizeSignupScope(req.DefaultSignupScope)
	}
	if req.Tenant.hasUpdate() {
		policy := mergeTenantRegistrationPolicy(cfg.Tenants[normalizeHubSyncTenantID(req.Tenant.TenantID)], req.Tenant)
		cfg.Tenants[policy.TenantID] = policy
	}
	state.Hubs[hubID] = cfg
	if err := validateRegistrationPolicyStore(state); err != nil {
		return HubRegistrationPolicyConfig{}, err
	}
	if err := s.saveRegistrationPolicyStore(ctx, state); err != nil {
		return HubRegistrationPolicyConfig{}, err
	}
	if err := s.persistHubRegistrationPolicyFields(ctx, hub, cfg); err != nil {
		return HubRegistrationPolicyConfig{}, err
	}
	s.refreshRoutes(ctx)
	return cfg, nil
}

func (s *Service) ensureDefaultHubRegistrationPolicy(ctx context.Context, hubID string) error {
	hubID = strings.TrimSpace(hubID)
	if hubID == "" || s == nil || s.settings == nil {
		return nil
	}
	state, err := s.loadRegistrationPolicyStore(ctx)
	if err != nil {
		return err
	}
	if _, ok := state.Hubs[hubID]; ok {
		return nil
	}
	var hub *store.HubInstance
	if s.hubs != nil {
		var err error
		hub, err = s.hubs.GetByID(ctx, hubID)
		if err != nil {
			return err
		}
	}
	state.Hubs[hubID] = hubRegistrationPolicyConfigFromRow(hub)
	if err := s.saveRegistrationPolicyStore(ctx, state); err != nil {
		return err
	}
	if hub == nil {
		return nil
	}
	return s.persistHubRegistrationPolicyFields(ctx, hub, state.Hubs[hubID])
}

func hubRegistrationPolicyConfigFromRow(hub *store.HubInstance) HubRegistrationPolicyConfig {
	cfg := HubRegistrationPolicyConfig{
		HubOrigin:          "self_hosted",
		DefaultSignupScope: "domain_restricted",
		Tenants:            map[string]store.HubTenantRegistrationPolicy{},
	}
	if hub == nil {
		return cfg
	}
	if strings.TrimSpace(hub.HubOrigin) != "" {
		cfg.HubOrigin = hub.HubOrigin
	}
	if strings.TrimSpace(hub.DefaultSignupScope) != "" {
		cfg.DefaultSignupScope = hub.DefaultSignupScope
	}
	if strings.TrimSpace(hub.RegistrationPolicyJSON) != "" {
		var state store.HubRegistrationPolicyState
		if err := json.Unmarshal([]byte(hub.RegistrationPolicyJSON), &state); err == nil {
			for tenantID, policy := range state.Tenants {
				if strings.TrimSpace(policy.TenantID) == "" {
					policy.TenantID = tenantID
				}
				cfg.Tenants[normalizeHubSyncTenantID(policy.TenantID)] = policy
			}
		}
	}
	return normalizeHubRegistrationPolicyConfig(cfg)
}

func (s *Service) persistHubRegistrationPolicyFields(ctx context.Context, hub *store.HubInstance, cfg HubRegistrationPolicyConfig) error {
	if s == nil || s.hubs == nil || hub == nil {
		return nil
	}
	cfg = normalizeHubRegistrationPolicyConfig(cfg)
	data, err := json.Marshal(store.HubRegistrationPolicyState{Tenants: cfg.Tenants})
	if err != nil {
		return err
	}
	hub.HubOrigin = cfg.HubOrigin
	hub.DefaultSignupScope = cfg.DefaultSignupScope
	hub.RegistrationPolicyJSON = string(data)
	hub.UpdatedAt = time.Now()
	return s.hubs.UpdateRegistration(ctx, hub)
}

func (req UpdateTenantRegistrationPolicyRequest) hasUpdate() bool {
	return strings.TrimSpace(req.TenantID) != "" ||
		strings.TrimSpace(req.TenantName) != "" ||
		strings.TrimSpace(req.SignupScope) != "" ||
		req.IsPublicFallback != nil ||
		req.InviteEnabled != nil ||
		req.MaxActiveInvites != 0 ||
		req.MonthlyInviteQuota != 0 ||
		req.PerInviteMaxUsesDefault != 0 ||
		req.PerInviteMaxUsesMax != 0 ||
		strings.TrimSpace(req.Status) != ""
}

func mergeTenantRegistrationPolicy(existing store.HubTenantRegistrationPolicy, req UpdateTenantRegistrationPolicyRequest) store.HubTenantRegistrationPolicy {
	if strings.TrimSpace(existing.Status) == "" {
		existing.InviteEnabled = true
	}
	if strings.TrimSpace(req.TenantID) != "" {
		existing.TenantID = req.TenantID
	}
	if strings.TrimSpace(req.TenantName) != "" {
		existing.TenantName = req.TenantName
	}
	if strings.TrimSpace(req.SignupScope) != "" {
		existing.SignupScope = req.SignupScope
	}
	if req.IsPublicFallback != nil {
		existing.IsPublicFallback = *req.IsPublicFallback
	}
	if req.InviteEnabled != nil {
		existing.InviteEnabled = *req.InviteEnabled
	}
	if req.MaxActiveInvites != 0 {
		existing.MaxActiveInvites = req.MaxActiveInvites
	}
	if req.MonthlyInviteQuota != 0 {
		existing.MonthlyInviteQuota = req.MonthlyInviteQuota
	}
	if req.PerInviteMaxUsesDefault != 0 {
		existing.PerInviteMaxUsesDefault = req.PerInviteMaxUsesDefault
	}
	if req.PerInviteMaxUsesMax != 0 {
		existing.PerInviteMaxUsesMax = req.PerInviteMaxUsesMax
	}
	if strings.TrimSpace(req.Status) != "" {
		existing.Status = req.Status
	}
	return normalizeTenantRegistrationPolicy(existing)
}

func validateRegistrationPolicyStore(state hubRegistrationPolicyStore) error {
	publicFallbacks := 0
	for _, cfg := range state.Hubs {
		cfg = normalizeHubRegistrationPolicyConfig(cfg)
		if cfg.HubOrigin != "official" && cfg.DefaultSignupScope == "public" {
			return ErrInvalidRegistrationPolicy
		}
		for _, policy := range cfg.Tenants {
			if !policy.IsPublicFallback || strings.EqualFold(strings.TrimSpace(policy.Status), "disabled") {
				continue
			}
			if cfg.HubOrigin != "official" || effectiveTenantSignupScope(cfg, policy) != "public" {
				return ErrInvalidRegistrationPolicy
			}
			publicFallbacks++
			if publicFallbacks > 1 {
				return ErrInvalidRegistrationPolicy
			}
		}
	}
	return nil
}

func effectiveTenantSignupScope(cfg HubRegistrationPolicyConfig, policy store.HubTenantRegistrationPolicy) string {
	scope := normalizeSignupScope(policy.SignupScope)
	if scope == "inherit" || scope == "" {
		return normalizeSignupScope(cfg.DefaultSignupScope)
	}
	return scope
}

func (s *Service) loadRegistrationPolicyStore(ctx context.Context) (hubRegistrationPolicyStore, error) {
	state := hubRegistrationPolicyStore{Hubs: map[string]HubRegistrationPolicyConfig{}}
	if s == nil || s.settings == nil {
		return state, nil
	}
	raw, err := s.settings.Get(ctx, systemKeyHubRegistrationPolicies)
	if err != nil || strings.TrimSpace(raw) == "" {
		return state, nil
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return hubRegistrationPolicyStore{}, err
	}
	if state.Hubs == nil {
		state.Hubs = map[string]HubRegistrationPolicyConfig{}
	}
	return state, nil
}

func (s *Service) saveRegistrationPolicyStore(ctx context.Context, state hubRegistrationPolicyStore) error {
	if s == nil || s.settings == nil {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.settings.Set(ctx, systemKeyHubRegistrationPolicies, string(data))
}

func normalizeHubRegistrationPolicyConfig(cfg HubRegistrationPolicyConfig) HubRegistrationPolicyConfig {
	cfg.HubOrigin = normalizeHubOrigin(cfg.HubOrigin)
	cfg.DefaultSignupScope = normalizeSignupScope(cfg.DefaultSignupScope)
	normalizedTenants := make(map[string]store.HubTenantRegistrationPolicy, len(cfg.Tenants))
	for _, value := range cfg.Tenants {
		policy := normalizeTenantRegistrationPolicy(value)
		normalizedTenants[policy.TenantID] = policy
	}
	if normalizedTenants == nil {
		normalizedTenants = map[string]store.HubTenantRegistrationPolicy{}
	}
	cfg.Tenants = normalizedTenants
	return cfg
}

func normalizeHubOrigin(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "official":
		return "official"
	default:
		return "self_hosted"
	}
}

func normalizeSignupScope(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "public", "domain_restricted", "invite_only", "inherit":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "domain_restricted"
	}
}

func normalizeTenantRegistrationPolicy(policy store.HubTenantRegistrationPolicy) store.HubTenantRegistrationPolicy {
	policy.TenantID = normalizeHubSyncTenantID(policy.TenantID)
	policy.TenantName = strings.TrimSpace(policy.TenantName)
	policy.SignupScope = normalizeSignupScope(policy.SignupScope)
	if policy.SignupScope == "inherit" || policy.SignupScope == "" {
		policy.SignupScope = "inherit"
	}
	if strings.TrimSpace(policy.Status) == "" {
		policy.Status = "active"
	}
	if policy.MaxActiveInvites <= 0 {
		policy.MaxActiveInvites = 100
	}
	if policy.MonthlyInviteQuota <= 0 {
		policy.MonthlyInviteQuota = 500
	}
	if policy.PerInviteMaxUsesDefault <= 0 {
		policy.PerInviteMaxUsesDefault = 1
	}
	if policy.PerInviteMaxUsesMax <= 0 {
		policy.PerInviteMaxUsesMax = 20
	}
	if policy.PerInviteMaxUsesDefault > policy.PerInviteMaxUsesMax {
		policy.PerInviteMaxUsesDefault = policy.PerInviteMaxUsesMax
	}
	return policy
}
