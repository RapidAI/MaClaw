package llmservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestEnsureTokenBankArraysSeedsThreeTierArrays(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure token bank arrays: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	// Every tier array carries a neutral 1. The tier rate lives in
	// TokenBankTierMultiplier, because this value is multiplied into what the
	// consumer pays. (design doc §3.4-A1)
	want := map[string]float64{
		TokenBankArrayLow:  1,
		TokenBankArrayMid:  1,
		TokenBankArrayHigh: 1,
	}
	if len(reg.ProviderArrays) != len(want) {
		t.Fatalf("arrays = %#v, want exactly the three token bank arrays", reg.ProviderArrays)
	}
	for _, arr := range reg.ProviderArrays {
		multiplier, ok := want[arr.ID]
		if !ok {
			t.Fatalf("unexpected array %q", arr.ID)
		}
		if arr.CreditMultiplier != multiplier {
			t.Fatalf("%s multiplier = %v, want %v", arr.ID, arr.CreditMultiplier, multiplier)
		}
		if !arr.System {
			t.Fatalf("%s system = false, want true", arr.ID)
		}
		if !arr.Manual {
			t.Fatalf("%s manual = false, want true", arr.ID)
		}
	}
}

func TestEnsureTokenBankArraysIsIdempotent(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	first, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	second, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(second.ProviderArrays) != len(first.ProviderArrays) {
		t.Fatalf("arrays = %d after second ensure, want %d", len(second.ProviderArrays), len(first.ProviderArrays))
	}
}

// An empty derived array is dropped on normalize. The Token Bank arrays must
// survive that, otherwise a tier array disappears once its last model leaves.
// addTokenBankProviders appends members to an existing registry. The registry
// already carries the seeded tier arrays, so the caller must not replace it.
func addTokenBankProviders(t *testing.T, svc *Service, providers ...llmpool.ProviderConfig) {
	t.Helper()
	ctx := context.Background()
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	reg.Providers = append(reg.Providers, providers...)
	if err := svc.SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}
}

func TestTokenBankArraysSurviveBeingEmpty(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	addTokenBankProviders(t, svc, llmpool.ProviderConfig{ID: "solo", Name: "Solo", APIURL: "https://solo.example"})
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	for _, id := range []string{TokenBankArrayLow, TokenBankArrayMid, TokenBankArrayHigh} {
		if findProviderArray(reg, id) == nil {
			t.Fatalf("array %s was dropped, token bank arrays must survive empty", id)
		}
	}
}

func TestDeleteTokenBankArrayIsRejected(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for _, id := range []string{TokenBankArrayLow, TokenBankArrayMid, TokenBankArrayHigh} {
		if _, err := svc.DeleteProviderArray(ctx, id); !errors.Is(err, ErrArrayProtected) {
			t.Fatalf("delete %s = %v, want ErrArrayProtected", id, err)
		}
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if findProviderArray(reg, TokenBankArrayHigh) == nil {
		t.Fatalf("token_bank_high must still exist after a rejected delete")
	}
}

// A tier array is protected by its id. Clearing the System bit must not make
// delete, or the empty-array cleanup, remove it.
func TestDeleteTokenBankArrayRejectsClearedSystemFlag(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	for i := range reg.ProviderArrays {
		reg.ProviderArrays[i].System = false
		reg.ProviderArrays[i].Manual = false
	}
	if err := svc.SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("save cleared flags: %v", err)
	}
	for _, id := range []string{TokenBankArrayLow, TokenBankArrayMid, TokenBankArrayHigh} {
		if _, err := svc.DeleteProviderArray(ctx, id); !errors.Is(err, ErrArrayProtected) {
			t.Fatalf("delete %s = %v, want ErrArrayProtected", id, err)
		}
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	for _, id := range []string{TokenBankArrayLow, TokenBankArrayMid, TokenBankArrayHigh} {
		if findProviderArray(reg, id) == nil {
			t.Fatalf("array %s was dropped after its System bit was cleared", id)
		}
	}
}

// Deleting the last member must not take the tier array with it. The member
// delete path used to drop the array record whenever no sibling remained.
// The model route goes away: nothing in the array can answer it anymore.
func TestDeleteLastTokenBankMemberKeepsArray(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	const memberID = "tbk_s1__model-a"
	addTokenBankProviders(t, svc, llmpool.ProviderConfig{
		ID: memberID, Name: "model-a", APIURL: "https://a.example",
		ArrayID: TokenBankArrayLow, Models: []string{"model-a"},
	})
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	reg.ServiceGroups = append(reg.ServiceGroups, llmpool.ServiceGroup{
		ID: "pool", Name: "Pool", AgentID: "maclaw_official",
		Models: []llmpool.ModelConfig{{
			Name:            "model-a",
			ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: memberID, Model: "model-a"}},
		}},
	})
	if err := svc.SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("save group: %v", err)
	}
	if _, err := svc.DeleteProvider(ctx, memberID, false); err != nil {
		t.Fatalf("delete last member: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if findProvider(reg, memberID) != nil {
		t.Fatalf("member %s still present", memberID)
	}
	for _, id := range []string{TokenBankArrayLow, TokenBankArrayMid, TokenBankArrayHigh} {
		if findProviderArray(reg, id) == nil {
			t.Fatalf("array %s was removed with the last member", id)
		}
	}
	if len(reg.ServiceGroups) != 1 || len(reg.ServiceGroups[0].Models) != 1 {
		t.Fatalf("groups = %#v", reg.ServiceGroups)
	}
	configs := reg.ServiceGroups[0].Models[0].ProviderConfigs
	if len(configs) != 0 || len(reg.ServiceGroups[0].Models[0].ProviderIDs) != 0 {
		t.Fatalf("routes = %#v, the last member must drop the model it alone served", reg.ServiceGroups[0].Models[0])
	}
}

func TestDeleteTokenBankMemberKeepsRouteServedBySibling(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	const model = "gpt-4o"
	first := "tbk_s1__" + encodeTokenBankModel(model)
	second := "tbk_s2__" + encodeTokenBankModel(model)
	addTokenBankProviders(t, svc,
		llmpool.ProviderConfig{ID: first, Name: model, APIURL: "https://a.example", ArrayID: TokenBankArrayLow, Models: []string{model}},
		llmpool.ProviderConfig{ID: second, Name: model, APIURL: "https://b.example", ArrayID: TokenBankArrayLow, Models: []string{model}},
	)
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	reg.ServiceGroups = append(reg.ServiceGroups, llmpool.ServiceGroup{
		ID: "pool", Name: "Pool", AgentID: "maclaw_official",
		Models: []llmpool.ModelConfig{{
			Name:            model,
			ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: TokenBankArrayLow, Model: model}},
		}},
	})
	if err := svc.SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("save group: %v", err)
	}
	if _, err := svc.DeleteProvider(ctx, first, false); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if findProvider(reg, first) != nil || findProvider(reg, second) == nil {
		t.Fatalf("providers = %#v", reg.Providers)
	}
	if findProviderArray(reg, TokenBankArrayLow) == nil {
		t.Fatal("token_bank_low was removed while a sibling still serves it")
	}
	configs := reg.ServiceGroups[0].Models[0].ProviderConfigs
	if len(configs) != 1 || configs[0].ProviderID != TokenBankArrayLow {
		t.Fatalf("routes = %#v, want the tier array while a sibling still serves %s", configs, model)
	}
}

func TestDeleteTokenBankMemberDropsRouteOnlyThatMemberServed(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	const goneModel = "gpt-4o"
	const keptModel = "claude"
	goneMember := "tbk_s1__" + encodeTokenBankModel(goneModel)
	keptMember := "tbk_s2__" + encodeTokenBankModel(keptModel)
	addTokenBankProviders(t, svc,
		llmpool.ProviderConfig{ID: goneMember, Name: goneModel, APIURL: "https://a.example", ArrayID: TokenBankArrayLow, Models: []string{goneModel}},
		llmpool.ProviderConfig{ID: keptMember, Name: keptModel, APIURL: "https://b.example", ArrayID: TokenBankArrayLow, Models: []string{keptModel}},
	)
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	reg.ServiceGroups = append(reg.ServiceGroups, llmpool.ServiceGroup{
		ID: "pool", Name: "Pool", AgentID: "maclaw_official",
		Models: []llmpool.ModelConfig{
			{Name: goneModel, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: TokenBankArrayLow, Model: goneModel}}},
			{Name: keptModel, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: TokenBankArrayLow, Model: keptModel}}},
		},
	})
	if err := svc.SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("save group: %v", err)
	}
	if _, err := svc.DeleteProvider(ctx, goneMember, false); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if findProvider(reg, goneMember) != nil || findProvider(reg, keptMember) == nil {
		t.Fatalf("providers = %#v", reg.Providers)
	}
	if findProviderArray(reg, TokenBankArrayLow) == nil {
		t.Fatal("token_bank_low was removed while another model still uses it")
	}
	if len(reg.ServiceGroups) != 1 || len(reg.ServiceGroups[0].Models) != 2 {
		t.Fatalf("groups = %#v", reg.ServiceGroups)
	}
	byName := map[string]llmpool.ModelConfig{}
	for _, model := range reg.ServiceGroups[0].Models {
		byName[model.Name] = model
	}
	if configs := byName[goneModel].ProviderConfigs; len(configs) != 0 || len(byName[goneModel].ProviderIDs) != 0 {
		t.Fatalf("gone model routes = %#v", byName[goneModel])
	}
	kept := byName[keptModel].ProviderConfigs
	if len(kept) != 1 || kept[0].ProviderID != TokenBankArrayLow {
		t.Fatalf("kept model routes = %#v", byName[keptModel])
	}
}

func TestSetProviderMemberTokenBankTierMovesMember(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	addTokenBankProviders(t, svc,
		llmpool.ProviderConfig{ID: "tbk_s1__model-a", Name: "model-a", APIURL: "https://a.example", ArrayID: TokenBankArrayMid, Models: []string{"model-a"}},
		llmpool.ProviderConfig{ID: "tbk_s1__model-b", Name: "model-b", APIURL: "https://a.example", ArrayID: TokenBankArrayMid, Models: []string{"model-b"}},
	)
	arrayID, tier, err := svc.SetProviderMemberTokenBankTier(ctx, "tbk_s1__model-a", "high")
	if err != nil {
		t.Fatalf("set tier: %v", err)
	}
	if arrayID != TokenBankArrayHigh || tier != TokenBankTierHigh {
		t.Fatalf("array = %s tier = %s, want token_bank_high/high", arrayID, tier)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	moved := findProvider(reg, "tbk_s1__model-a")
	stayed := findProvider(reg, "tbk_s1__model-b")
	if moved == nil || moved.ArrayID != TokenBankArrayHigh {
		t.Fatalf("moved member = %#v, want token_bank_high", moved)
	}
	if stayed == nil || stayed.ArrayID != TokenBankArrayMid {
		t.Fatalf("other member = %#v, want unchanged token_bank_mid", stayed)
	}
	// Regrading a model must not re-price consumers, so the member keeps a
	// neutral multiplier even in the high array. The 2x belongs to settlement.
	if moved.CreditMultiplier != tokenBankArrayCreditMultiplier {
		t.Fatalf("moved multiplier = %v, want neutral %v: the tier rate must not reach the consumer bill",
			moved.CreditMultiplier, tokenBankArrayCreditMultiplier)
	}
	if rate, err := TokenBankTierMultiplier(tier); err != nil || rate != 2 {
		t.Fatalf("tier multiplier = %v err = %v, want the high tier settlement rate 2", rate, err)
	}
}

func TestTokenBankTierMultiplierRates(t *testing.T) {
	want := map[string]float64{
		TokenBankTierLow:  0.5,
		TokenBankTierMid:  1,
		TokenBankTierHigh: 2,
	}
	for tier, rate := range want {
		got, err := TokenBankTierMultiplier(tier)
		if err != nil {
			t.Fatalf("%s multiplier: %v", tier, err)
		}
		if got != rate {
			t.Fatalf("%s multiplier = %v, want %v", tier, got, rate)
		}
	}
	if _, err := TokenBankTierMultiplier("ultra"); !errors.Is(err, ErrUnknownTokenBankTier) {
		t.Fatalf("err = %v, want ErrUnknownTokenBankTier", err)
	}
	if _, ok := TokenBankTierMultiplierOfArray("some_other_array"); ok {
		t.Fatal("non token bank array must not resolve a tier rate")
	}
	if rate, ok := TokenBankTierMultiplierOfArray(TokenBankArrayHigh); !ok || rate != 2 {
		t.Fatalf("array rate = %v ok = %v, want 2/true", rate, ok)
	}
}

// An operator must not be able to smuggle a rate onto a tier array: restarting
// the service pins it back, and a time-of-use schedule would silently win over
// the multiplier when the consumer is billed. (design doc §3.4-A1/A3)
func TestEnsureTokenBankArraysPinsBillingFields(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		arr := findProviderArray(reg, TokenBankArrayHigh)
		if arr == nil {
			t.Fatal("missing high array")
		}
		arr.CreditMultiplier = 2
		arr.Timezone = "Asia/Shanghai"
		arr.CreditMultiplierSchedule = []llmpool.CreditMultiplierWindow{{Multiplier: 3}}
		return true, nil
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	arr := findProviderArray(reg, TokenBankArrayHigh)
	if arr == nil {
		t.Fatal("missing high array")
	}
	if arr.CreditMultiplier != tokenBankArrayCreditMultiplier {
		t.Fatalf("multiplier = %v, want pinned %v", arr.CreditMultiplier, tokenBankArrayCreditMultiplier)
	}
	if len(arr.CreditMultiplierSchedule) != 0 {
		t.Fatalf("schedule = %#v, want cleared", arr.CreditMultiplierSchedule)
	}
	if strings.TrimSpace(arr.Timezone) != "" {
		t.Fatalf("timezone = %q, want cleared", arr.Timezone)
	}
}

// A tier array carries no price of its own. Copying its billing onto a member
// must therefore leave the member's price alone, otherwise the route has no
// resolvable price and the request falls through billing. (design doc §3.4-A2)
func TestCopyArrayBillingKeepsMemberPriceWhenArrayHasNone(t *testing.T) {
	arr := &llmpool.ProviderArray{ID: TokenBankArrayMid, Name: "Token Bank 中档", CreditMultiplier: 1}
	member := &llmpool.ProviderConfig{
		ID:           "tbk_s1__model-a",
		TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 6},
	}
	copyArrayBillingToProvider(arr, member)
	if member.TokenPricing.InputCreditsPer10K != 3 || member.TokenPricing.OutputCreditsPer10K != 6 {
		t.Fatalf("member pricing = %#v, want the member's own price preserved", member.TokenPricing)
	}
	if member.CreditMultiplier != 1 {
		t.Fatalf("member multiplier = %v, want the array rate 1", member.CreditMultiplier)
	}
}

func TestTokenBankTierRejectsUnknownTier(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if _, _, err := svc.SetProviderMemberTokenBankTier(ctx, "tbk_s1__model-a", "ultra"); !errors.Is(err, ErrUnknownTokenBankTier) {
		t.Fatalf("err = %v, want ErrUnknownTokenBankTier", err)
	}
}

func TestRenameTokenBankArrayIsRejected(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	want := map[string]string{
		TokenBankArrayLow:  "Token Bank 低档",
		TokenBankArrayMid:  "Token Bank 中档",
		TokenBankArrayHigh: "Token Bank 高档",
	}
	for id, name := range want {
		if err := svc.RenameProviderArray(ctx, id, "自定义"); !errors.Is(err, ErrArrayProtected) {
			t.Fatalf("rename %s = %v, want ErrArrayProtected", id, err)
		}
		if err := svc.RenameProviderArray(ctx, id, "  "+name+"  "); err != nil {
			t.Fatalf("same-name rename %s = %v", id, err)
		}
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	for id, name := range want {
		arr := findProviderArray(reg, id)
		if arr == nil || arr.Name != name {
			t.Fatalf("%s = %#v, want name %q", id, arr, name)
		}
	}
	for i := range reg.ProviderArrays {
		reg.ProviderArrays[i].System = false
	}
	if err := svc.SaveRegistry(ctx, reg); err != nil {
		t.Fatalf("clear system: %v", err)
	}
	if err := svc.RenameProviderArray(ctx, TokenBankArrayLow, "自定义"); !errors.Is(err, ErrArrayProtected) {
		t.Fatalf("rename after clearing system = %v, want ErrArrayProtected", err)
	}
}

func TestUpdateTokenBankArrayRejectsRenameAndKeepsSameNameBilling(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	rejected := ProviderArrayBilling{
		CreditMultiplier: 1,
		TokenPricing:     llmpool.TokenPricing{InputCreditsPer10K: 9, OutputCreditsPer10K: 9},
	}
	if err := svc.UpdateProviderArray(ctx, TokenBankArrayHigh, "自定义高档", rejected); !errors.Is(err, ErrArrayProtected) {
		t.Fatalf("update = %v, want ErrArrayProtected", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	arr := findProviderArray(reg, TokenBankArrayHigh)
	if arr == nil || arr.Name != "Token Bank 高档" || arr.TokenPricing.InputCreditsPer10K != 0 || arr.CreditMultiplier != 1 {
		t.Fatalf("rejected update changed array = %#v", arr)
	}
	kept := ProviderArrayBilling{
		Timezone:         "Asia/Shanghai",
		CreditMultiplier: 1,
		TokenPricing:     llmpool.TokenPricing{InputCreditsPer10K: 3, OutputCreditsPer10K: 6},
	}
	if err := svc.UpdateProviderArray(ctx, TokenBankArrayHigh, "Token Bank 高档", kept); err != nil {
		t.Fatalf("same-name update: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	arr = findProviderArray(reg, TokenBankArrayHigh)
	if arr == nil || arr.Name != "Token Bank 高档" || arr.Timezone != "Asia/Shanghai" || arr.TokenPricing.InputCreditsPer10K != 3 || arr.TokenPricing.OutputCreditsPer10K != 6 {
		t.Fatalf("same-name billing = %#v", arr)
	}
}

func TestEnsureTokenBankArraysRestoresDisplayName(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		for i := range reg.ProviderArrays {
			reg.ProviderArrays[i].Name = "drifted"
		}
		return true, nil
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("restore: %v", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	for _, spec := range tokenBankArraySpecs() {
		arr := findProviderArray(reg, spec.ID)
		if arr == nil || arr.Name != spec.Name {
			t.Fatalf("%s = %#v, want name %q", spec.ID, arr, spec.Name)
		}
	}
}

func TestSaveRegistryDoesNotRenameTokenBankArrayFromMember(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	addTokenBankProviders(t, svc, llmpool.ProviderConfig{
		ID: "m1", Name: "WorkBuddy", APIURL: "https://wb.example",
		ArrayID: TokenBankArrayMid, ArrayName: "偷换的名字",
	})
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	arr := findProviderArray(reg, TokenBankArrayMid)
	if arr == nil || arr.Name != "Token Bank 中档" {
		t.Fatalf("member array name overwrote the tier = %#v", arr)
	}
	if err := svc.MutateRegistry(ctx, func(reg *Registry) (bool, error) {
		found := findProviderArray(reg, TokenBankArrayMid)
		if found == nil {
			t.Fatal("missing mid array")
		}
		found.Name = ""
		return true, nil
	}); err != nil {
		t.Fatalf("clear name: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	arr = findProviderArray(reg, TokenBankArrayMid)
	if arr == nil || arr.Name != "Token Bank 中档" {
		t.Fatalf("empty name = %#v, want the platform name", arr)
	}
}

func TestImportTokenBankArrayRejectsRename(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	_, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID:   TokenBankArrayMid,
		Name: "自定义中档",
		Providers: []llmpool.ProviderConfig{{
			ID: "imp-1", Name: "Imported", APIURL: "https://imp.example",
		}},
	}})
	if !errors.Is(err, ErrArrayProtected) {
		t.Fatalf("import = %v, want ErrArrayProtected", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	arr := findProviderArray(reg, TokenBankArrayMid)
	if arr == nil || arr.Name != "Token Bank 中档" {
		t.Fatalf("array = %#v, want the platform name", arr)
	}
	if findProvider(reg, "imp-1") != nil {
		t.Fatal("rejected import must not persist the member")
	}

	if _, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID:   TokenBankArrayLow,
		Name: "Token Bank 低档",
		Providers: []llmpool.ProviderConfig{{
			ID: "imp-low", Name: "Low member", APIURL: "https://low.example",
		}},
	}}); err != nil {
		t.Fatalf("same-name import: %v", err)
	}
	reg, err = svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	low := findProviderArray(reg, TokenBankArrayLow)
	if low == nil || low.Name != "Token Bank 低档" || findProvider(reg, "imp-low") == nil {
		t.Fatalf("low array = %#v", low)
	}
}

func TestImportTokenBankArrayOmitsNameKeepsPlatformName(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	result, err := svc.ImportProviderArrays(ctx, []ProviderArrayImport{{
		ID: TokenBankArrayHigh,
		Providers: []llmpool.ProviderConfig{{
			ID: "imp-high", Name: "Not the array", APIURL: "https://high.example",
		}},
	}})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(result.Arrays) != 1 || result.Arrays[0].Name != "Token Bank 高档" {
		t.Fatalf("result = %+v, want the platform name", result.Arrays)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	arr := findProviderArray(reg, TokenBankArrayHigh)
	if arr == nil || arr.Name != "Token Bank 高档" || findProvider(reg, "imp-high") == nil {
		t.Fatalf("array = %#v", arr)
	}
}

func TestAddTokenBankArrayRejectsCustomName(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := svc.AddProviderArray(ctx, TokenBankArrayLow, "自定义", ProviderArrayBilling{CreditMultiplier: 1}); !errors.Is(err, ErrArrayProtected) {
		t.Fatalf("add existing = %v, want ErrArrayProtected", err)
	}
	reg, err := svc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if arr := findProviderArray(reg, TokenBankArrayLow); arr == nil || arr.Name != "Token Bank 低档" {
		t.Fatalf("array = %#v", arr)
	}

	fresh := NewService(&mockSystemSettings{})
	if err := fresh.AddProviderArray(ctx, TokenBankArrayMid, "自定义中档", ProviderArrayBilling{CreditMultiplier: 1}); !errors.Is(err, ErrArrayProtected) {
		t.Fatalf("add missing = %v, want ErrArrayProtected", err)
	}
	reg, err = fresh.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load fresh: %v", err)
	}
	if findProviderArray(reg, TokenBankArrayMid) != nil {
		t.Fatal("rejected add must not create the array")
	}
	if err := fresh.AddProviderArray(ctx, TokenBankArrayMid, "Token Bank 中档", ProviderArrayBilling{CreditMultiplier: 1}); err != nil {
		t.Fatalf("add canonical: %v", err)
	}
	reg, err = fresh.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload fresh: %v", err)
	}
	if arr := findProviderArray(reg, TokenBankArrayMid); arr == nil || arr.Name != "Token Bank 中档" {
		t.Fatalf("created = %#v", arr)
	}
}

func TestMoveProviderMemberToUnknownArrayRejected(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	if err := svc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	addTokenBankProviders(t, svc, llmpool.ProviderConfig{ID: "m1", Name: "M1", APIURL: "https://a.example"})
	if _, err := svc.MoveProviderMemberToArray(ctx, "m1", "no_such_array"); !errors.Is(err, ErrArrayNotFound) {
		t.Fatalf("err = %v, want ErrArrayNotFound", err)
	}
}
