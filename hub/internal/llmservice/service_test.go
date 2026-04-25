package llmservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGenerateCardCodeFormat(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 200; i++ {
		code, err := GenerateCardCode()
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateCardCode(code); err != nil {
			t.Fatalf("ValidateCardCode(%q) error = %v", code, err)
		}
		if len(code) != CardCodeLength {
			t.Fatalf("len(%q) = %d, want %d", code, len(code), CardCodeLength)
		}
		if code != strings.ToUpper(code) {
			t.Fatalf("expected uppercase code, got %q", code)
		}
		if _, exists := seen[code]; exists {
			t.Fatalf("duplicate generated code: %q", code)
		}
		seen[code] = struct{}{}
	}
}

func TestValidateCardCodeRejectsInvalidFormat(t *testing.T) {
	invalid := []string{"", "ABC", "abc123", "1234567890123456789-", "1234567890123456789_", "123456789012345678901"}
	for _, code := range invalid {
		if err := ValidateCardCode(code); err == nil {
			t.Fatalf("expected invalid code %q to fail validation", code)
		}
	}
}

type testSystemSettings struct {
	data map[string]string
}

func newTestSystemSettings() *testSystemSettings {
	return &testSystemSettings{data: map[string]string{}}
}

func (s *testSystemSettings) Set(_ context.Context, key, valueJSON string) error {
	s.data[key] = valueJSON
	return nil
}

func (s *testSystemSettings) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}

func TestGrantDefaultServiceForNewUser(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []ModelServiceModel{{
				Name:        "gpt-5",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}

	if err := GrantDefaultServiceForNewUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}

	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(saved.Grants))
	}
	grant := saved.Grants[0]
	if grant.Email != "newuser@example.com" {
		t.Fatalf("unexpected email: %q", grant.Email)
	}
	if grant.ServiceGroupID != "coding-basic" {
		t.Fatalf("unexpected service group: %q", grant.ServiceGroupID)
	}
	if grant.Source != "new_user_default" {
		t.Fatalf("unexpected grant source: %q", grant.Source)
	}
	duration := grant.ExpiresAt.Sub(grant.StartsAt)
	if duration < 7*24*time.Hour-time.Minute || duration > 7*24*time.Hour+time.Minute {
		t.Fatalf("unexpected duration: %s", duration)
	}
}

func TestGrantDefaultServiceForNewUserIsIdempotent(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []ModelServiceModel{{
				Name:        "gpt-5",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		DefaultNewUserServiceGroups: []string{"coding-basic"},
		DefaultNewUserDurationDays:  7,
	}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}
	if err := GrantDefaultServiceForNewUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := GrantDefaultServiceForNewUser(ctx, system, "newuser@example.com"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, grant := range saved.Grants {
		if grant.Email == "newuser@example.com" && grant.ServiceGroupID == "coding-basic" && grant.Source == "new_user_default" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 default grant, got %d", count)
	}
}

func TestRegistryNormalizeDefaultsAccessPolicyToFree(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "group-a",
			Name: "Group A",
		}},
	}
	reg.Normalize()
	group := reg.FindModelServiceGroup("group-a")
	if group == nil {
		t.Fatal("expected group-a to exist")
	}
	if group.AccessPolicy != AccessPolicyFree {
		t.Fatalf("expected access policy %q, got %q", AccessPolicyFree, group.AccessPolicy)
	}
}

func TestRegistryNormalizeEnsuresBuiltinDefaultGroup(t *testing.T) {
	reg := &Registry{}
	reg.Normalize()
	if len(reg.ModelServiceGroups) == 0 {
		t.Fatal("expected builtin default model service group")
	}
	if reg.ModelServiceGroups[0].ID != DefaultModelServiceGroupID {
		t.Fatalf("expected first group id=%q, got %q", DefaultModelServiceGroupID, reg.ModelServiceGroups[0].ID)
	}
	if reg.ModelServiceGroups[0].Name != DefaultModelServiceGroupName {
		t.Fatalf("expected first group name=%q, got %q", DefaultModelServiceGroupName, reg.ModelServiceGroups[0].Name)
	}
	if len(reg.ModelServiceGroups[0].Models) != 0 {
		t.Fatalf("expected builtin default group to have no models, got %d", len(reg.ModelServiceGroups[0].Models))
	}

	reg = &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:     DefaultModelServiceGroupID,
			Name:   "Custom Default Name",
			Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}},
		}},
	}
	reg.Normalize()
	group := reg.FindModelServiceGroup(DefaultModelServiceGroupID)
	if group == nil {
		t.Fatal("expected builtin default group after normalize")
	}
	if len(group.Models) != 0 {
		t.Fatalf("expected builtin default group to stay permissionless, got %d models", len(group.Models))
	}
}

func TestRegistryNormalizeDefaultsNewUsersToBuiltinDefaultGroup(t *testing.T) {
	reg := &Registry{}
	reg.Normalize()
	if len(reg.DefaultNewUserServiceGroups) != 1 || reg.DefaultNewUserServiceGroups[0] != DefaultModelServiceGroupID {
		t.Fatalf("expected default new-user service groups [%q], got %#v", DefaultModelServiceGroupID, reg.DefaultNewUserServiceGroups)
	}
	if reg.DefaultNewUserDurationDays != 30 {
		t.Fatalf("expected default new-user duration 30, got %d", reg.DefaultNewUserDurationDays)
	}
}

func TestSelectBestModelForRequest(t *testing.T) {
	models := []AuthorizedModel{
		{Name: "doc-fast", CapabilityTags: []string{"document"}, ResolutionTier: 1, Priority: 10, CreditMultiplier: 1},
		{Name: "reasoning-pro", CapabilityTags: []string{"reasoning"}, ResolutionTier: 2, Priority: 50, CreditMultiplier: 2},
		{Name: "tool-lite", CapabilityTags: []string{"tools"}, ResolutionTier: 1, Priority: 20, CreditMultiplier: 1},
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Please analyze this PDF document and summarize it."}},
	}
	best := SelectBestModelForRequest(body, models)
	if best == nil || best.Name != "doc-fast" {
		t.Fatalf("expected doc-fast, got %#v", best)
	}
	body = map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Use tools to search and fetch the answer."}},
		"tools":    []any{map[string]any{"type": "function"}},
	}
	best = SelectBestModelForRequest(body, models)
	if best == nil || best.Name != "tool-lite" {
		t.Fatalf("expected tool-lite, got %#v", best)
	}
}

func TestApplyCreditUsageToRegistry(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		Grants: []Grant{{
			ID: "g1", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10, CreditsUsed: 1,
		}, {
			ID: "g2", Email: "user@example.com", ServiceGroupID: "coding-basic", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(2 * time.Hour), CreditsTotal: 5, CreditsUsed: 0,
		}},
	}
	used := ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 6.5, now)
	if used != 6.5 {
		t.Fatalf("expected used credits 6.5, got %v", used)
	}
	if reg.Grants[0].CreditsUsed != 7.5 {
		t.Fatalf("expected first grant credits used 7.5, got %v", reg.Grants[0].CreditsUsed)
	}
	if reg.Grants[1].CreditsUsed != 0 {
		t.Fatalf("expected second grant untouched, got %v", reg.Grants[1].CreditsUsed)
	}
	used = ApplyCreditUsageToRegistry(reg, "user@example.com", []string{"coding-basic"}, 5, now)
	if used != 5 {
		t.Fatalf("expected used credits 5, got %v", used)
	}
	if reg.Grants[0].CreditsUsed != 10 {
		t.Fatalf("expected first grant exhausted, got %v", reg.Grants[0].CreditsUsed)
	}
	if reg.Grants[1].CreditsUsed != 2.5 {
		t.Fatalf("expected second grant credits used 2.5, got %v", reg.Grants[1].CreditsUsed)
	}
}

func TestSelectBestModelForRequestWithDebug(t *testing.T) {
	models := []AuthorizedModel{
		{Name: "doc-fast", CapabilityTags: []string{"document"}, ResolutionTier: 1, Priority: 10, CreditMultiplier: 1},
		{Name: "reasoning-pro", CapabilityTags: []string{"reasoning"}, ResolutionTier: 2, Priority: 50, CreditMultiplier: 2},
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Please analyze this PDF document and summarize it."}},
	}
	best, debug := SelectBestModelForRequestWithDebug(body, models)
	if best == nil || best.Name != "doc-fast" {
		t.Fatalf("expected doc-fast, got %#v", best)
	}
	if debug == nil {
		t.Fatal("expected debug info")
	}
	if debug.SelectedModel != "doc-fast" {
		t.Fatalf("selected model = %q, want doc-fast", debug.SelectedModel)
	}
	if len(debug.MatchedTags) != 1 || debug.MatchedTags[0] != "document" {
		t.Fatalf("matched tags = %#v, want [document]", debug.MatchedTags)
	}
	if debug.SelectionReason == "" {
		t.Fatal("expected selection reason")
	}
}

func TestBuildAuthorizedModelsTracksProviderScopedGroupsAndMultiplier(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "group-a",
			Name: "Group A",
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:       "provider-a",
					CapabilityTags:   []string{"reasoning"},
					Priority:         40,
					ResolutionTier:   2,
					CreditMultiplier: 2,
				}},
			}},
		}, {
			ID:   "group-b",
			Name: "Group B",
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-b"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:       "provider-b",
					CapabilityTags:   []string{"document"},
					Priority:         80,
					ResolutionTier:   1,
					CreditMultiplier: 1,
				}},
			}},
		}, {
			ID:   "group-c",
			Name: "Group C",
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:       "provider-a",
					CapabilityTags:   []string{"tools"},
					Priority:         60,
					ResolutionTier:   3,
					CreditMultiplier: 1.5,
				}},
			}},
		}},
	}
	models, _ := buildAuthorizedModels(reg, []string{"group-a", "group-b", "group-c"})
	if len(models) != 1 {
		t.Fatalf("expected 1 merged model, got %d", len(models))
	}
	model := models[0]
	if got := model.ProviderServiceGroups["provider-a"]; len(got) != 2 || got[0] != "group-a" || got[1] != "group-c" {
		t.Fatalf("provider-a groups = %#v", got)
	}
	if got := model.ProviderServiceGroups["provider-b"]; len(got) != 1 || got[0] != "group-b" {
		t.Fatalf("provider-b groups = %#v", got)
	}
	if got := model.ProviderCreditMultipliers["provider-a"]; got != 1.5 {
		t.Fatalf("provider-a multiplier = %v, want 1.5", got)
	}
	if got := model.ProviderCreditMultipliers["provider-b"]; got != 1 {
		t.Fatalf("provider-b multiplier = %v, want 1", got)
	}
	if !containsString(model.CapabilityTags, "reasoning") || !containsString(model.CapabilityTags, "document") || !containsString(model.CapabilityTags, "tools") {
		t.Fatalf("capability tags = %#v", model.CapabilityTags)
	}
	if model.Priority != 80 {
		t.Fatalf("priority = %d, want 80", model.Priority)
	}
	if model.ResolutionTier != 1 {
		t.Fatalf("resolution tier = %d, want 1", model.ResolutionTier)
	}
	if got := CreditMultiplierForProvider(&model, "provider-a"); got != 1.5 {
		t.Fatalf("CreditMultiplierForProvider(provider-a) = %v, want 1.5", got)
	}
	if got := CreditMultiplierForProvider(&model, "provider-b"); got != 1 {
		t.Fatalf("CreditMultiplierForProvider(provider-b) = %v, want 1", got)
	}
	if got := ServiceGroupIDsForProvider(&model, "provider-a"); len(got) != 2 || got[0] != "group-a" || got[1] != "group-c" {
		t.Fatalf("ServiceGroupIDsForProvider(provider-a) = %#v", got)
	}
}

func TestRegistryNormalizeMigratesLegacyModelFieldsToProviderConfigs(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "group-a",
			Name: "Group A",
			Models: []ModelServiceModel{{
				Name:             "auto",
				ProviderIDs:      []string{"provider-a", "provider-b"},
				CapabilityTags:   []string{"reasoning", "tools"},
				Priority:         50,
				ResolutionTier:   2,
				CreditMultiplier: 1.2,
			}},
		}},
	}
	reg.Normalize()
	model := reg.ModelServiceGroups[1].Models[0]
	if len(model.ProviderConfigs) != 2 {
		t.Fatalf("provider configs = %#v", model.ProviderConfigs)
	}
	for _, cfg := range model.ProviderConfigs {
		if len(cfg.CapabilityTags) != 2 || cfg.Priority != 50 || cfg.ResolutionTier != 2 || cfg.CreditMultiplier != 1.2 {
			t.Fatalf("unexpected migrated config: %#v", cfg)
		}
	}
}

func TestRegistryNormalizeMergesDuplicateModelAliasesWithinGroup(t *testing.T) {
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:   "group-a",
			Name: "Group A",
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:     "provider-a",
					CapabilityTags: []string{"document"},
					Priority:       60,
				}},
			}, {
				Name:        "AUTO",
				ProviderIDs: []string{"provider-b"},
				ProviderConfigs: []ModelServiceProviderConfig{{
					ProviderID:       "provider-b",
					CapabilityTags:   []string{"reasoning", "tools"},
					Priority:         80,
					ResolutionTier:   2,
					CreditMultiplier: 1.5,
				}},
			}},
		}},
	}
	reg.Normalize()
	if len(reg.ModelServiceGroups) < 2 {
		t.Fatalf("expected builtin default group plus custom group, got %#v", reg.ModelServiceGroups)
	}
	models := reg.ModelServiceGroups[1].Models
	if len(models) != 1 {
		t.Fatalf("expected duplicate aliases to merge into one model, got %#v", models)
	}
	model := models[0]
	if model.Name != "auto" {
		t.Fatalf("merged model name = %q, want auto", model.Name)
	}
	if len(model.ProviderIDs) != 2 || model.ProviderIDs[0] != "provider-a" || model.ProviderIDs[1] != "provider-b" {
		t.Fatalf("merged provider ids = %#v", model.ProviderIDs)
	}
	if len(model.ProviderConfigs) != 2 {
		t.Fatalf("merged provider configs = %#v", model.ProviderConfigs)
	}
	if !containsString(model.CapabilityTags, "document") || !containsString(model.CapabilityTags, "reasoning") || !containsString(model.CapabilityTags, "tools") {
		t.Fatalf("merged capability tags = %#v", model.CapabilityTags)
	}
}
func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestRedeemCardCreatesGrantsAndRejectsReuse(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	code := "0123456789ABCDEFGHIJ"
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{
			{ID: "coding-basic", Name: "Coding Basic", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}}},
			{ID: "coding-pro", Name: "Coding Pro", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-b"}}}},
		},
		Cards: []RechargeCard{{
			ID:              "card-1",
			CodeHash:        HashCode(code),
			ServiceGroupIDs: []string{"coding-basic", "coding-pro"},
			DurationDays:    7,
			Credits:         90,
			CreatedAt:       time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	status, err := RedeemCard(ctx, system, nil, "User@Example.COM", strings.ToLower(code), "http://hub.test/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || !status.Active {
		t.Fatalf("expected active status, got %#v", status)
	}
	if status.CreditsAvailable != 90 {
		t.Fatalf("expected credits available 90, got %v", status.CreditsAvailable)
	}

	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(saved.Grants))
	}
	for _, grant := range saved.Grants {
		if grant.Email != "user@example.com" {
			t.Fatalf("expected normalized email, got %q", grant.Email)
		}
		if grant.CreditsTotal != 45 {
			t.Fatalf("expected split credits 45, got %v", grant.CreditsTotal)
		}
		if grant.ExpiresAt.Sub(grant.StartsAt) != 7*24*time.Hour {
			t.Fatalf("expected 7-day grant, got %s", grant.ExpiresAt.Sub(grant.StartsAt))
		}
	}
	card, _ := saved.FindCardByID("card-1")
	if card == nil || card.RedeemedAt == nil || card.RedeemedByEmail != "user@example.com" {
		t.Fatalf("expected redeemed card, got %#v", card)
	}
	if _, err := RedeemCard(ctx, system, nil, "other@example.com", code, "http://hub.test/api/llm/v1"); err == nil {
		t.Fatal("expected reused card to be rejected")
	}
}

func TestRedeemCardStacksExistingGrantForSameServiceGroup(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	now := time.Now().UTC()
	code := "ABCDEFGHIJ0123456789"
	if err := SaveRegistry(ctx, system, &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic", Models: []ModelServiceModel{{Name: "gpt-5", ProviderIDs: []string{"provider-a"}}}}},
		Cards: []RechargeCard{{
			ID:              "card-1",
			CodeHash:        HashCode(code),
			ServiceGroupIDs: []string{"coding-basic"},
			DurationDays:    3,
			CreatedAt:       now,
		}},
		Grants: []Grant{{
			ID:             "grant-existing",
			Email:          "user@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreatedAt:      now.Add(-time.Hour),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := RedeemCard(ctx, system, nil, "user@example.com", code, "http://hub.test/api/llm/v1"); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadRegistry(ctx, system)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Grants) != 2 {
		t.Fatalf("expected 2 grants, got %d", len(saved.Grants))
	}
	newGrant := saved.Grants[1]
	if !newGrant.StartsAt.Equal(saved.Grants[0].ExpiresAt) {
		t.Fatalf("expected new grant to start after existing grant expires, got start=%s existing_expiry=%s", newGrant.StartsAt, saved.Grants[0].ExpiresAt)
	}
	if newGrant.ExpiresAt.Sub(newGrant.StartsAt) != 3*24*time.Hour {
		t.Fatalf("expected 3-day stacked grant, got %s", newGrant.ExpiresAt.Sub(newGrant.StartsAt))
	}
}
func TestRedeemCardRejectsInvalidCodeFormat(t *testing.T) {
	ctx := context.Background()
	system := newTestSystemSettings()
	reg := &Registry{}
	if err := SaveRegistry(ctx, system, reg); err != nil {
		t.Fatal(err)
	}
	if _, err := RedeemCard(ctx, system, nil, "user@example.com", "bad-code", "http://hub.test/api/llm/v1"); err == nil {
		t.Fatal("expected invalid code format to be rejected")
	}
}

func TestBillingEligibilityForServiceGroups(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "free-group", Name: "Free", AccessPolicy: AccessPolicyFree}, {ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   10,
			CreditsUsed:    10,
		}},
	}
	reg.Normalize()

	allowed, policy, code, _, credits, hasActiveGrant, hasAnyGrant := BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"free-group"}, now)
	if !allowed || policy != AccessPolicyFree || code != "" || credits != 0 || hasActiveGrant || hasAnyGrant {
		t.Fatalf("unexpected free eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant)
	}

	allowed, policy, code, _, credits, hasActiveGrant, hasAnyGrant = BillingEligibilityForServiceGroups(reg, "user@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_CREDITS_EXHAUSTED" || credits != 0 || !hasActiveGrant || !hasAnyGrant {
		t.Fatalf("unexpected exhausted eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant)
	}

	allowed, policy, code, _, credits, hasActiveGrant, hasAnyGrant = BillingEligibilityForServiceGroups(reg, "other@example.com", []string{"grant-group"}, now)
	if allowed || policy != AccessPolicyGrantRequired || code != "LLM_SERVICE_CREDITS_REQUIRED" || credits != 0 || hasActiveGrant || hasAnyGrant {
		t.Fatalf("unexpected required eligibility: allowed=%v policy=%q code=%q credits=%v active=%v any=%v", allowed, policy, code, credits, hasActiveGrant, hasAnyGrant)
	}
}

func TestExplainBillingRoutes(t *testing.T) {
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{ID: "grant-group", Name: "Grant", AccessPolicy: AccessPolicyGrantRequired}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   12,
			CreditsUsed:    2,
		}},
	}
	reg.Normalize()
	models := []AuthorizedModel{{
		Name:                  "auto",
		ProviderIDs:           []string{"provider-a"},
		ProviderServiceGroups: map[string][]string{"provider-a": {"grant-group"}},
	}}

	routes := ExplainBillingRoutes(reg, "user@example.com", models, now)
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %#v", routes)
	}
	if !routes[0].Eligible || routes[0].AccessPolicy != AccessPolicyGrantRequired || routes[0].CreditsAvailable != 10 {
		t.Fatalf("unexpected route diagnostic: %#v", routes[0])
	}
}

func TestExplainEntitlementDiagnosticIncludesBillingRoutes(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	reg := &Registry{
		ModelServiceGroups: []ModelServiceGroup{{
			ID:           "grant-group",
			Name:         "Grant Group",
			AccessPolicy: AccessPolicyGrantRequired,
			Models: []ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []UserBinding{{
			Email:           "user@example.com",
			ServiceGroupIDs: []string{"grant-group"},
		}},
		Grants: []Grant{{
			ID:             "grant-1",
			Email:          "user@example.com",
			ServiceGroupID: "grant-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   8,
			CreditsUsed:    3,
		}},
	}
	reg.Normalize()

	diag, err := ExplainEntitlementDiagnosticFromRegistry(ctx, reg, nil, "user@example.com", "http://hub.test/api/llm/v1")
	if err != nil {
		t.Fatal(err)
	}
	if diag == nil || diag.ServiceStatus == nil {
		t.Fatalf("expected service status, got %#v", diag)
	}
	if len(diag.BillingRoutes) != 1 {
		t.Fatalf("expected 1 billing route, got %#v", diag.BillingRoutes)
	}
	route := diag.BillingRoutes[0]
	if route.ModelName != "auto" || route.ProviderID != "provider-a" {
		t.Fatalf("unexpected route identity: %#v", route)
	}
	if !route.Eligible || route.AccessPolicy != AccessPolicyGrantRequired {
		t.Fatalf("unexpected route eligibility: %#v", route)
	}
	if route.CreditsAvailable != 5 {
		t.Fatalf("credits available = %v, want 5", route.CreditsAvailable)
	}
}
