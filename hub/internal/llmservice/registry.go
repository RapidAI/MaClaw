package llmservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const RegistryKey = "llm_service_registry"

const (
	DefaultModelServiceGroupID   = "default"
	DefaultModelServiceGroupName = "Default (No Model Access)"
	DefaultTokensPerCredit       = 10000
)

type SystemSettingsRepository interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

type Registry struct {
	ModelServiceGroups          []ModelServiceGroup `json:"model_service_groups"`
	GroupBindings               []GroupBinding      `json:"group_bindings,omitempty"`
	UserBindings                []UserBinding       `json:"user_bindings,omitempty"`
	Cards                       []RechargeCard      `json:"cards,omitempty"`
	Grants                      []Grant             `json:"grants,omitempty"`
	DefaultNewUserServiceGroups []string            `json:"default_new_user_service_groups,omitempty"`
	DefaultNewUserDurationDays  int                 `json:"default_new_user_duration_days,omitempty"`
	TokensPerCredit             int                 `json:"tokens_per_credit,omitempty"`
}

type ModelServiceGroup struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Models      []ModelServiceModel `json:"models,omitempty"`
}

type ModelServiceModel struct {
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	ProviderIDs      []string `json:"provider_ids,omitempty"`
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
	ID              string     `json:"id"`
	CodeHash        string     `json:"code_hash,omitempty"`
	Label           string     `json:"label,omitempty"`
	ServiceGroupIDs []string   `json:"service_group_ids,omitempty"`
	DurationDays    int        `json:"duration_days"`
	Credits         float64    `json:"credits,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	RedeemedByEmail string     `json:"redeemed_by_email,omitempty"`
	RedeemedAt      *time.Time `json:"redeemed_at,omitempty"`
}

type Grant struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	ServiceGroupID string    `json:"service_group_id"`
	Source         string    `json:"source"`
	CardID         string    `json:"card_id,omitempty"`
	StartsAt       time.Time `json:"starts_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
	CreditsTotal   float64   `json:"credits_total,omitempty"`
	CreditsUsed    float64   `json:"credits_used,omitempty"`
}

type AuthorizedModel struct {
	Name             string   `json:"name"`
	ProviderIDs      []string `json:"provider_ids,omitempty"`
	ServiceGroupIDs  []string `json:"service_group_ids,omitempty"`
	CapabilityTags   []string `json:"capability_tags,omitempty"`
	Priority         int      `json:"priority,omitempty"`
	ResolutionTier   int      `json:"resolution_tier,omitempty"`
	CreditMultiplier float64  `json:"credit_multiplier,omitempty"`
}

type ActiveGrant struct {
	ServiceGroupID   string    `json:"service_group_id"`
	Source           string    `json:"source"`
	ExpiresAt        time.Time `json:"expires_at"`
	CreditsTotal     float64   `json:"credits_total,omitempty"`
	CreditsUsed      float64   `json:"credits_used,omitempty"`
	CreditsRemaining float64   `json:"credits_remaining,omitempty"`
}

type ServiceStatus struct {
	Active            bool              `json:"active"`
	SkipLLMConfig     bool              `json:"skip_llm_config"`
	AuthMode          string            `json:"auth_mode"`
	ServiceGroupIDs   []string          `json:"service_group_ids,omitempty"`
	ServiceGroupNames []string          `json:"service_group_names,omitempty"`
	AvailableModels   []string          `json:"available_models,omitempty"`
	AuthorizedModels  []AuthorizedModel `json:"authorized_models,omitempty"`
	ActiveGrants      []ActiveGrant     `json:"active_grants,omitempty"`
	NearestExpiresAt  string            `json:"nearest_expires_at,omitempty"`
	DefaultModel      string            `json:"default_model,omitempty"`
	HubLLMBaseURL     string            `json:"hub_llm_base_url,omitempty"`
	CreditsAvailable  float64           `json:"credits_available,omitempty"`
	TokensPerCredit   int               `json:"tokens_per_credit,omitempty"`
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
		for j := range g.Models {
			m := &g.Models[j]
			m.Name = strings.TrimSpace(m.Name)
			m.Description = strings.TrimSpace(m.Description)
			m.ProviderIDs = normalizeStringSlice(m.ProviderIDs)
			m.CapabilityTags = normalizeStringSlice(m.CapabilityTags)
			if m.ResolutionTier < 0 {
				m.ResolutionTier = 0
			}
			if m.CreditMultiplier <= 0 {
				m.CreditMultiplier = 1
			}
		}
	}
	for i := range r.GroupBindings {
		r.GroupBindings[i].GroupID = strings.TrimSpace(r.GroupBindings[i].GroupID)
		r.GroupBindings[i].ServiceGroupIDs = normalizeStringSlice(r.GroupBindings[i].ServiceGroupIDs)
	}
	for i := range r.UserBindings {
		r.UserBindings[i].Email = normalizeEmail(r.UserBindings[i].Email)
		r.UserBindings[i].ServiceGroupIDs = normalizeStringSlice(r.UserBindings[i].ServiceGroupIDs)
	}
	for i := range r.Cards {
		r.Cards[i].ID = strings.TrimSpace(r.Cards[i].ID)
		r.Cards[i].CodeHash = strings.TrimSpace(r.Cards[i].CodeHash)
		r.Cards[i].Label = strings.TrimSpace(r.Cards[i].Label)
		r.Cards[i].ServiceGroupIDs = normalizeStringSlice(r.Cards[i].ServiceGroupIDs)
		r.Cards[i].RedeemedByEmail = normalizeEmail(r.Cards[i].RedeemedByEmail)
		if r.Cards[i].DurationDays < 0 {
			r.Cards[i].DurationDays = 0
		}
		if r.Cards[i].Credits < 0 {
			r.Cards[i].Credits = 0
		}
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
	}
	r.DefaultNewUserServiceGroups = normalizeStringSlice(r.DefaultNewUserServiceGroups)
	if r.DefaultNewUserDurationDays < 0 {
		r.DefaultNewUserDurationDays = 0
	}
	if r.TokensPerCredit <= 0 {
		r.TokensPerCredit = DefaultTokensPerCredit
	}
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
	if r.TokensPerCredit <= 0 {
		r.TokensPerCredit = DefaultTokensPerCredit
	}
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

func (r *Registry) FindCardByCode(code string) (*RechargeCard, int) {
	hash := HashCode(code)
	for i := range r.Cards {
		if r.Cards[i].CodeHash == hash {
			return &r.Cards[i], i
		}
	}
	return nil, -1
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
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func GenerateCardCode() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	parts := []string{
		strings.ToUpper(hex.EncodeToString(buf[0:4])),
		strings.ToUpper(hex.EncodeToString(buf[4:8])),
		strings.ToUpper(hex.EncodeToString(buf[8:12])),
	}
	return strings.Join(parts, "-"), nil
}

func NewID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + strings.ToLower(hex.EncodeToString(buf))
}

var _ SystemSettingsRepository = (store.SystemSettingsRepository)(nil)
