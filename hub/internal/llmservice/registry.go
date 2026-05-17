package llmservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const RegistryKey = "llm_service_registry"

const (
	DefaultModelServiceGroupID   = "default"
	DefaultModelServiceGroupName = "Default (No Model Access)"
	DefaultTokensPerCredit       = 10000
	DefaultNewUserCredits        = 1000
	CardCodeLength               = 20

	AccessPolicyFree          = "free"
	AccessPolicyGrantRequired = "grant_required"
)

const cardCodeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type Registry struct {
	ModelServiceGroups          []ModelServiceGroup `json:"model_service_groups"`
	GlobalServiceGroupIDs       []string            `json:"global_service_group_ids,omitempty"`
	GroupBindings               []GroupBinding      `json:"group_bindings,omitempty"`
	UserBindings                []UserBinding       `json:"user_bindings,omitempty"`
	Cards                       []RechargeCard      `json:"cards,omitempty"`
	Grants                      []Grant             `json:"grants,omitempty"`
	DefaultNewUserServiceGroups []string            `json:"default_new_user_service_groups,omitempty"`
	DefaultNewUserDurationDays  int                 `json:"default_new_user_duration_days,omitempty"`
	DefaultNewUserCredits       float64             `json:"default_new_user_credits,omitempty"`
	TokensPerCredit             int                 `json:"tokens_per_credit,omitempty"`
}

type ModelServiceGroup struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	AccessPolicy string              `json:"access_policy,omitempty"`
	Models       []ModelServiceModel `json:"models,omitempty"`
}

type ModelServiceModel struct {
	Name             string                       `json:"name"`
	Description      string                       `json:"description,omitempty"`
	ProviderIDs      []string                     `json:"provider_ids,omitempty"`
	ProviderConfigs  []ModelServiceProviderConfig `json:"provider_configs,omitempty"`
	CapabilityTags   []string                     `json:"capability_tags,omitempty"`
	Priority         int                          `json:"priority,omitempty"`
	ResolutionTier   int                          `json:"resolution_tier,omitempty"`
	CreditMultiplier float64                      `json:"credit_multiplier,omitempty"`
}

type ModelServiceProviderConfig struct {
	ProviderID       string   `json:"provider_id"`
	CapabilityTags   []string `json:"capability_tags,omitempty"`
	Priority         int      `json:"priority,omitempty"`
	ResolutionTier   int      `json:"resolution_tier,omitempty"`
	CreditMultiplier float64  `json:"credit_multiplier,omitempty"`
}

type GroupBinding struct {
	GroupID         string   `json:"group_id"`
	ServiceGroupIDs []string `json:"service_group_ids,omitempty"`
}

type UserBinding struct {
	Email           string   `json:"email"`
	ServiceGroupIDs []string `json:"service_group_ids,omitempty"`
}

type RechargeCard struct {
	ID              string             `json:"id"`
	CodeHash        string             `json:"code_hash,omitempty"`
	EncryptedCode   string             `json:"encrypted_code,omitempty"`
	Label           string             `json:"label,omitempty"`
	ServiceGroupIDs []string           `json:"service_group_ids,omitempty"`
	DurationDays    int                `json:"duration_days"`
	Credits         float64            `json:"credits,omitempty"`
	PeriodLimits    CreditPeriodLimits `json:"period_limits,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	RedeemedByEmail string             `json:"redeemed_by_email,omitempty"`
	RedeemedAt      *time.Time         `json:"redeemed_at,omitempty"`
}

type CreditPeriodLimits struct {
	FiveHour float64 `json:"five_hour,omitempty"`
	Daily    float64 `json:"daily,omitempty"`
	Weekly   float64 `json:"weekly,omitempty"`
	Monthly  float64 `json:"monthly,omitempty"`
}

type CreditPeriodUsage struct {
	FiveHour GrantUsageWindow `json:"five_hour,omitempty"`
	Daily    GrantUsageWindow `json:"daily,omitempty"`
	Weekly   GrantUsageWindow `json:"weekly,omitempty"`
	Monthly  GrantUsageWindow `json:"monthly,omitempty"`
}

type GrantUsageWindow struct {
	WindowStart time.Time `json:"window_start,omitempty"`
	CreditsUsed float64   `json:"credits_used,omitempty"`
}

// PlainCode returns the decrypted card code, or empty string if unavailable
// (legacy cards created before encrypted storage was added).
func (c RechargeCard) PlainCode() string {
	return DecryptCardCode(c.EncryptedCode)
}

type Grant struct {
	ID             string             `json:"id"`
	Email          string             `json:"email"`
	ServiceGroupID string             `json:"service_group_id"`
	Source         string             `json:"source"`
	CardID         string             `json:"card_id,omitempty"`
	StartsAt       time.Time          `json:"starts_at"`
	ExpiresAt      time.Time          `json:"expires_at"`
	CreatedAt      time.Time          `json:"created_at"`
	CreditsTotal   float64            `json:"credits_total,omitempty"`
	CreditsUsed    float64            `json:"credits_used,omitempty"`
	PeriodLimits   CreditPeriodLimits `json:"period_limits,omitempty"`
	PeriodUsage    CreditPeriodUsage  `json:"period_usage,omitempty"`
}

type AuthorizedModel struct {
	Name                      string              `json:"name"`
	ProviderIDs               []string            `json:"provider_ids,omitempty"`
	ServiceGroupIDs           []string            `json:"service_group_ids,omitempty"`
	CapabilityTags            []string            `json:"capability_tags,omitempty"`
	Priority                  int                 `json:"priority,omitempty"`
	ResolutionTier            int                 `json:"resolution_tier,omitempty"`
	CreditMultiplier          float64             `json:"credit_multiplier,omitempty"`
	ProviderCapabilityTags    map[string][]string `json:"provider_capability_tags,omitempty"`
	ProviderPriorities        map[string]int      `json:"provider_priorities,omitempty"`
	ProviderResolutionTiers   map[string]int      `json:"provider_resolution_tiers,omitempty"`
	ProviderServiceGroups     map[string][]string `json:"provider_service_groups,omitempty"`
	ProviderCreditMultipliers map[string]float64  `json:"provider_credit_multipliers,omitempty"`
}

type ActiveGrant struct {
	ServiceGroupID    string    `json:"service_group_id"`
	Source            string    `json:"source"`
	StartsAt          time.Time `json:"starts_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	Active            bool      `json:"active"`
	Effective         bool      `json:"effective"`
	Status            string    `json:"status,omitempty"`
	StatusReason      string    `json:"status_reason,omitempty"`
	CreditsTotal      float64   `json:"credits_total,omitempty"`
	CreditsUsed       float64   `json:"credits_used,omitempty"`
	CreditsAvailable  float64   `json:"credits_available,omitempty"`
	RetryAfterSeconds int64     `json:"retry_after_seconds,omitempty"`
	RetryAfterAt      string    `json:"retry_after_at,omitempty"`
	CreditsRemaining  float64   `json:"credits_remaining,omitempty"`
}

type ServiceStatus struct {
	Active             bool              `json:"active"`
	SkipLLMConfig      bool              `json:"skip_llm_config"`
	AuthMode           string            `json:"auth_mode"`
	ServiceGroupIDs    []string          `json:"service_group_ids,omitempty"`
	ServiceGroupNames  []string          `json:"service_group_names,omitempty"`
	AvailableModels    []string          `json:"available_models,omitempty"`
	AuthorizedModels   []AuthorizedModel `json:"authorized_models,omitempty"`
	ActiveGrants       []ActiveGrant     `json:"active_grants,omitempty"`
	CreditGrants       []ActiveGrant     `json:"credit_grants,omitempty"`
	InactiveReasons    []string          `json:"inactive_reasons,omitempty"`
	NearestExpiresAt   string            `json:"nearest_expires_at,omitempty"`
	EffectiveExpiresAt string            `json:"effective_expires_at,omitempty"`
	DefaultModel       string            `json:"default_model,omitempty"`
	HubLLMBaseURL      string            `json:"hub_llm_base_url,omitempty"`
	CreditsTotal       float64           `json:"credits_total"`
	CreditsUsed        float64           `json:"credits_used"`
	CreditsRemaining   float64           `json:"credits_remaining"`
	CreditsAvailable   float64           `json:"credits_available"`
	TokensPerCredit    int               `json:"tokens_per_credit,omitempty"`
}

func LoadRegistry(ctx context.Context, system SystemSettingsRepository) (*Registry, error) {
	raw, err := system.Get(ctx, RegistryKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		reg := &Registry{}
		reg.Normalize()
		return reg, nil
	}
	var reg Registry
	if err := json.Unmarshal([]byte(raw), &reg); err != nil {
		return nil, err
	}
	reg.Normalize()
	return &reg, nil
}

func SaveRegistry(ctx context.Context, system SystemSettingsRepository, reg *Registry) error {
	if reg == nil {
		reg = &Registry{}
	}
	reg.Normalize()
	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	return system.Set(ctx, RegistryKey, string(data))
}

func (r *Registry) Normalize() {
	if r == nil {
		return
	}
	r.ensureBuiltinModelServiceGroups()
	r.ensureDefaultNewUserSettings()
	for i := range r.ModelServiceGroups {
		g := &r.ModelServiceGroups[i]
		g.ID = strings.TrimSpace(g.ID)
		g.Name = strings.TrimSpace(g.Name)
		g.Description = strings.TrimSpace(g.Description)
		g.AccessPolicy = NormalizeAccessPolicy(g.AccessPolicy)
		for j := range g.Models {
			m := &g.Models[j]
			m.Name = strings.TrimSpace(m.Name)
			m.Description = strings.TrimSpace(m.Description)
			m.normalizeProviderConfigs()
		}
		g.mergeModelsByName()
	}
	r.GlobalServiceGroupIDs = normalizeStringSlice(r.GlobalServiceGroupIDs)
	for i := range r.GroupBindings {
		r.GroupBindings[i].GroupID = strings.TrimSpace(r.GroupBindings[i].GroupID)
		r.GroupBindings[i].ServiceGroupIDs = normalizeStringSlice(r.GroupBindings[i].ServiceGroupIDs)
	}
	r.GroupBindings = mergeGroupBindings(r.GroupBindings)
	for i := range r.UserBindings {
		r.UserBindings[i].Email = normalizeEmail(r.UserBindings[i].Email)
		r.UserBindings[i].ServiceGroupIDs = normalizeStringSlice(r.UserBindings[i].ServiceGroupIDs)
	}
	r.UserBindings = mergeUserBindings(r.UserBindings)
	for i := range r.Cards {
		r.Cards[i].ID = strings.TrimSpace(r.Cards[i].ID)
		r.Cards[i].CodeHash = strings.TrimSpace(r.Cards[i].CodeHash)
		r.Cards[i].EncryptedCode = strings.TrimSpace(r.Cards[i].EncryptedCode)
		r.Cards[i].Label = strings.TrimSpace(r.Cards[i].Label)
		r.Cards[i].ServiceGroupIDs = normalizeStringSlice(r.Cards[i].ServiceGroupIDs)
		r.Cards[i].RedeemedByEmail = normalizeEmail(r.Cards[i].RedeemedByEmail)
		if r.Cards[i].DurationDays < 0 {
			r.Cards[i].DurationDays = 0
		}
		if r.Cards[i].Credits < 0 {
			r.Cards[i].Credits = 0
		}
		r.Cards[i].PeriodLimits = normalizeCreditPeriodLimits(r.Cards[i].PeriodLimits)
	}
	for i := range r.Grants {
		r.Grants[i].ID = strings.TrimSpace(r.Grants[i].ID)
		r.Grants[i].Email = normalizeEmail(r.Grants[i].Email)
		r.Grants[i].ServiceGroupID = strings.TrimSpace(r.Grants[i].ServiceGroupID)
		r.Grants[i].Source = strings.TrimSpace(r.Grants[i].Source)
		r.Grants[i].CardID = strings.TrimSpace(r.Grants[i].CardID)
		if r.Grants[i].CreditsTotal < 0 {
			r.Grants[i].CreditsTotal = 0
		}
		if r.Grants[i].CreditsUsed < 0 {
			r.Grants[i].CreditsUsed = 0
		}
		if r.Grants[i].CreditsUsed > r.Grants[i].CreditsTotal && r.Grants[i].CreditsTotal > 0 {
			r.Grants[i].CreditsUsed = r.Grants[i].CreditsTotal
		}
		r.Grants[i].PeriodLimits = normalizeCreditPeriodLimits(r.Grants[i].PeriodLimits)
		r.Grants[i].PeriodUsage = normalizeCreditPeriodUsage(r.Grants[i].PeriodUsage)
	}
	r.DefaultNewUserServiceGroups = normalizeStringSlice(r.DefaultNewUserServiceGroups)
	if r.DefaultNewUserDurationDays < 0 {
		r.DefaultNewUserDurationDays = 0
	}
	if r.DefaultNewUserCredits < 0 {
		r.DefaultNewUserCredits = 0
	}
	if r.TokensPerCredit <= 0 {
		r.TokensPerCredit = DefaultTokensPerCredit
	}
}

func mergeGroupBindings(items []GroupBinding) []GroupBinding {
	indexByGroup := map[string]int{}
	out := make([]GroupBinding, 0, len(items))
	for _, item := range items {
		groupID := strings.TrimSpace(item.GroupID)
		if groupID == "" {
			continue
		}
		ids := normalizeStringSlice(item.ServiceGroupIDs)
		if len(ids) == 0 {
			continue
		}
		key := strings.ToLower(groupID)
		if idx, ok := indexByGroup[key]; ok {
			out[idx].ServiceGroupIDs = normalizeStringSlice(append(out[idx].ServiceGroupIDs, ids...))
			continue
		}
		indexByGroup[key] = len(out)
		out = append(out, GroupBinding{GroupID: groupID, ServiceGroupIDs: ids})
	}
	return out
}

func mergeUserBindings(items []UserBinding) []UserBinding {
	indexByEmail := map[string]int{}
	out := make([]UserBinding, 0, len(items))
	for _, item := range items {
		email := normalizeEmail(item.Email)
		if email == "" {
			continue
		}
		ids := normalizeStringSlice(item.ServiceGroupIDs)
		if len(ids) == 0 {
			continue
		}
		if idx, ok := indexByEmail[email]; ok {
			out[idx].ServiceGroupIDs = normalizeStringSlice(append(out[idx].ServiceGroupIDs, ids...))
			continue
		}
		indexByEmail[email] = len(out)
		out = append(out, UserBinding{Email: email, ServiceGroupIDs: ids})
	}
	return out
}

func normalizeCreditPeriodLimits(limits CreditPeriodLimits) CreditPeriodLimits {
	if limits.FiveHour < 0 {
		limits.FiveHour = 0
	}
	if limits.Daily < 0 {
		limits.Daily = 0
	}
	if limits.Weekly < 0 {
		limits.Weekly = 0
	}
	if limits.Monthly < 0 {
		limits.Monthly = 0
	}
	return limits
}

func normalizeCreditPeriodUsage(usage CreditPeriodUsage) CreditPeriodUsage {
	usage.FiveHour.CreditsUsed = mathMaxZero(usage.FiveHour.CreditsUsed)
	usage.Daily.CreditsUsed = mathMaxZero(usage.Daily.CreditsUsed)
	usage.Weekly.CreditsUsed = mathMaxZero(usage.Weekly.CreditsUsed)
	usage.Monthly.CreditsUsed = mathMaxZero(usage.Monthly.CreditsUsed)
	return usage
}

func mathMaxZero(v float64) float64 {
	if v < 0 {
		return 0
	}
	return v
}

func (r *Registry) ensureDefaultNewUserSettings() {
	if r == nil {
		return
	}
	if len(r.DefaultNewUserServiceGroups) == 0 {
		r.DefaultNewUserServiceGroups = []string{DefaultModelServiceGroupID}
	}
	if r.DefaultNewUserDurationDays <= 0 {
		r.DefaultNewUserDurationDays = 30
	}
	if r.DefaultNewUserCredits == 0 {
		r.DefaultNewUserCredits = DefaultNewUserCredits
	}
	if r.TokensPerCredit <= 0 {
		r.TokensPerCredit = DefaultTokensPerCredit
	}
}
func (m *ModelServiceModel) normalizeProviderConfigs() {
	if m == nil {
		return
	}
	legacyTags := normalizeStringSlice(m.CapabilityTags)
	legacyPriority := m.Priority
	legacyResolution := m.ResolutionTier
	if legacyResolution < 0 {
		legacyResolution = 0
	}
	legacyMultiplier := normalizeCreditMultiplier(m.CreditMultiplier)

	orderedProviderIDs := normalizeStringSlice(m.ProviderIDs)
	configByID := map[string]ModelServiceProviderConfig{}
	for _, cfg := range m.ProviderConfigs {
		cfg.ProviderID = strings.TrimSpace(cfg.ProviderID)
		if cfg.ProviderID == "" {
			continue
		}
		cfg.CapabilityTags = normalizeStringSlice(cfg.CapabilityTags)
		if cfg.ResolutionTier < 0 {
			cfg.ResolutionTier = 0
		}
		if cfg.CreditMultiplier <= 0 {
			cfg.CreditMultiplier = legacyMultiplier
		}
		configByID[strings.ToLower(cfg.ProviderID)] = cfg
		if !containsNormalizedString(orderedProviderIDs, cfg.ProviderID) {
			orderedProviderIDs = append(orderedProviderIDs, cfg.ProviderID)
		}
	}

	normalizedConfigs := make([]ModelServiceProviderConfig, 0, len(orderedProviderIDs))
	for _, providerID := range orderedProviderIDs {
		key := strings.ToLower(strings.TrimSpace(providerID))
		cfg, ok := configByID[key]
		if !ok {
			cfg = ModelServiceProviderConfig{ProviderID: providerID}
		}
		cfg.ProviderID = providerID
		if len(cfg.CapabilityTags) == 0 {
			cfg.CapabilityTags = append([]string(nil), legacyTags...)
		}
		if cfg.Priority == 0 {
			cfg.Priority = legacyPriority
		}
		if cfg.ResolutionTier == 0 {
			cfg.ResolutionTier = legacyResolution
		}
		if cfg.CreditMultiplier <= 0 {
			cfg.CreditMultiplier = legacyMultiplier
		}
		normalizedConfigs = append(normalizedConfigs, cfg)
	}

	m.ProviderIDs = orderedProviderIDs
	m.ProviderConfigs = normalizedConfigs
	m.CapabilityTags = nil
	m.Priority = 0
	m.ResolutionTier = 0
	m.CreditMultiplier = 1
	for _, cfg := range normalizedConfigs {
		m.CapabilityTags = mergeStrings(m.CapabilityTags, cfg.CapabilityTags)
		if cfg.Priority > m.Priority {
			m.Priority = cfg.Priority
		}
		if m.ResolutionTier == 0 || (cfg.ResolutionTier > 0 && cfg.ResolutionTier < m.ResolutionTier) {
			m.ResolutionTier = cfg.ResolutionTier
		}
		if candidate := normalizeCreditMultiplier(cfg.CreditMultiplier); m.CreditMultiplier == 0 || candidate < m.CreditMultiplier {
			m.CreditMultiplier = candidate
		}
	}
	if m.CreditMultiplier <= 0 {
		m.CreditMultiplier = 1
	}
}

func (m ModelServiceModel) providerConfigByID(providerID string) (ModelServiceProviderConfig, bool) {
	key := strings.ToLower(strings.TrimSpace(providerID))
	if key == "" {
		return ModelServiceProviderConfig{}, false
	}
	for _, cfg := range m.ProviderConfigs {
		if strings.ToLower(strings.TrimSpace(cfg.ProviderID)) == key {
			return cfg, true
		}
	}
	return ModelServiceProviderConfig{}, false
}

func (g *ModelServiceGroup) mergeModelsByName() {
	if g == nil || len(g.Models) <= 1 {
		return
	}
	merged := make([]ModelServiceModel, 0, len(g.Models))
	indexByName := map[string]int{}
	for _, model := range g.Models {
		key := strings.ToLower(strings.TrimSpace(model.Name))
		if key == "" {
			merged = append(merged, model)
			continue
		}
		idx, ok := indexByName[key]
		if !ok {
			merged = append(merged, model)
			indexByName[key] = len(merged) - 1
			continue
		}
		merged[idx] = mergeModelServiceModel(merged[idx], model)
	}
	for i := range merged {
		merged[i].normalizeProviderConfigs()
	}
	g.Models = merged
}

func mergeModelServiceModel(dst ModelServiceModel, src ModelServiceModel) ModelServiceModel {
	if strings.TrimSpace(dst.Name) == "" {
		dst.Name = strings.TrimSpace(src.Name)
	}
	if strings.TrimSpace(dst.Description) == "" {
		dst.Description = strings.TrimSpace(src.Description)
	}
	dst.ProviderIDs = mergeStrings(dst.ProviderIDs, src.ProviderIDs)
	configIndex := map[string]int{}
	for i, cfg := range dst.ProviderConfigs {
		key := strings.ToLower(strings.TrimSpace(cfg.ProviderID))
		if key == "" {
			continue
		}
		configIndex[key] = i
	}
	for _, cfg := range src.ProviderConfigs {
		key := strings.ToLower(strings.TrimSpace(cfg.ProviderID))
		if key == "" {
			continue
		}
		if idx, ok := configIndex[key]; ok {
			dst.ProviderConfigs[idx] = mergeModelServiceProviderConfig(dst.ProviderConfigs[idx], cfg)
			continue
		}
		dst.ProviderConfigs = append(dst.ProviderConfigs, cfg)
		configIndex[key] = len(dst.ProviderConfigs) - 1
	}
	dst.CapabilityTags = mergeStrings(dst.CapabilityTags, src.CapabilityTags)
	if src.Priority > dst.Priority {
		dst.Priority = src.Priority
	}
	if dst.ResolutionTier == 0 || (src.ResolutionTier > 0 && src.ResolutionTier < dst.ResolutionTier) {
		dst.ResolutionTier = src.ResolutionTier
	}
	if candidate := normalizeCreditMultiplier(src.CreditMultiplier); dst.CreditMultiplier == 0 || candidate < dst.CreditMultiplier {
		dst.CreditMultiplier = candidate
	}
	return dst
}

func mergeModelServiceProviderConfig(dst ModelServiceProviderConfig, src ModelServiceProviderConfig) ModelServiceProviderConfig {
	if strings.TrimSpace(dst.ProviderID) == "" {
		dst.ProviderID = strings.TrimSpace(src.ProviderID)
	}
	dst.CapabilityTags = mergeStrings(dst.CapabilityTags, src.CapabilityTags)
	if src.Priority > dst.Priority {
		dst.Priority = src.Priority
	}
	if dst.ResolutionTier == 0 || (src.ResolutionTier > 0 && src.ResolutionTier < dst.ResolutionTier) {
		dst.ResolutionTier = src.ResolutionTier
	}
	if candidate := normalizeCreditMultiplier(src.CreditMultiplier); dst.CreditMultiplier == 0 || candidate < dst.CreditMultiplier {
		dst.CreditMultiplier = candidate
	}
	return dst
}
func containsNormalizedString(items []string, target string) bool {
	needle := strings.ToLower(strings.TrimSpace(target))
	if needle == "" {
		return false
	}
	for _, item := range items {
		if strings.ToLower(strings.TrimSpace(item)) == needle {
			return true
		}
	}
	return false
}

func (r *Registry) ensureBuiltinModelServiceGroups() {
	if r == nil {
		return
	}
	for i := range r.ModelServiceGroups {
		if strings.EqualFold(strings.TrimSpace(r.ModelServiceGroups[i].ID), DefaultModelServiceGroupID) {
			r.ModelServiceGroups[i].ID = DefaultModelServiceGroupID
			if strings.TrimSpace(r.ModelServiceGroups[i].Name) == "" {
				r.ModelServiceGroups[i].Name = DefaultModelServiceGroupName
			}
			r.ModelServiceGroups[i].Description = "Built-in fallback group with no model permissions."
			r.ModelServiceGroups[i].Models = nil
			return
		}
	}
	r.ModelServiceGroups = append([]ModelServiceGroup{{
		ID:          DefaultModelServiceGroupID,
		Name:        DefaultModelServiceGroupName,
		Description: "Built-in fallback group with no model permissions.",
		Models:      nil,
	}}, r.ModelServiceGroups...)
}

func IsBuiltinModelServiceGroupID(id string) bool {
	return strings.EqualFold(strings.TrimSpace(id), DefaultModelServiceGroupID)
}

func (r *Registry) FindModelServiceGroup(id string) *ModelServiceGroup {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for i := range r.ModelServiceGroups {
		if strings.EqualFold(strings.TrimSpace(r.ModelServiceGroups[i].ID), id) {
			return &r.ModelServiceGroups[i]
		}
	}
	return nil
}

func (r *Registry) AccessPolicyForServiceGroup(id string) string {
	group := r.FindModelServiceGroup(id)
	if group == nil {
		return AccessPolicyFree
	}
	return NormalizeAccessPolicy(group.AccessPolicy)
}

func (r *Registry) FindCardByCode(code string) (*RechargeCard, int) {
	hash := HashCode(code)
	for i := range r.Cards {
		if r.Cards[i].CodeHash == hash {
			return &r.Cards[i], i
		}
	}
	return nil, -1
}

func (r *Registry) FindCardByID(id string) (*RechargeCard, int) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, -1
	}
	for i := range r.Cards {
		if strings.EqualFold(strings.TrimSpace(r.Cards[i].ID), id) {
			return &r.Cards[i], i
		}
	}
	return nil, -1
}

func NormalizeAccessPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case AccessPolicyGrantRequired:
		return AccessPolicyGrantRequired
	default:
		return AccessPolicyFree
	}
}

func normalizeStringSlice(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func HashCode(code string) string {
	sum := sha256.Sum256([]byte(NormalizeCardCode(code)))
	return hex.EncodeToString(sum[:])
}

func NormalizeCardCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func ValidateCardCode(code string) error {
	code = NormalizeCardCode(code)
	if len(code) != CardCodeLength {
		return fmt.Errorf("redeem code must be %d letters or digits", CardCodeLength)
	}
	for _, ch := range code {
		if (ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'Z') {
			continue
		}
		return fmt.Errorf("redeem code must contain only letters or digits")
	}
	return nil
}

func GenerateCardCode() (string, error) {
	buf := make([]byte, CardCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, CardCodeLength)
	for i, b := range buf {
		out[i] = cardCodeAlphabet[int(b)%len(cardCodeAlphabet)]
	}
	return string(out), nil
}

func NewID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + strings.ToLower(hex.EncodeToString(buf))
}

var _ SystemSettingsRepository = (store.SystemSettingsRepository)(nil)
