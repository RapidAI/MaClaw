package llmservice

import (
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestResolveDynamicAuthorizedModelRoutesAuto(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	selected, name, dec, err := ResolveDynamicAuthorizedModel(header, map[string]any{"model": "auto"}, models, reg, "")
	if err != nil {
		t.Fatalf("ResolveDynamicAuthorizedModel() error = %v", err)
	}
	if selected == nil || selected.Name != llmpool.OfficialTierHigh || name != llmpool.OfficialTierHigh {
		t.Fatalf("selected = %#v name=%s", selected, name)
	}
	if dec == nil || dec.Class != llmpool.WorkloadClassPlan || dec.Passthrough {
		t.Fatalf("decision = %#v", dec)
	}
	if len(selected.ChargedServiceGroupIDs) != 1 || selected.ChargedServiceGroupIDs[0] != "coding-auto" {
		t.Fatalf("charged = %#v", selected.ChargedServiceGroupIDs)
	}
	if dec.Attribution.RequestedGroup != "coding-auto" || dec.Attribution.RequestedModel != "auto" {
		t.Fatalf("attribution request = %#v", dec.Attribution)
	}
	if dec.Attribution.ResolvedModel != llmpool.OfficialTierHigh || dec.Attribution.OfficialProviderPool != llmpool.OfficialGroupID {
		t.Fatalf("attribution resolved = %#v", dec.Attribution)
	}
	if dec.Attribution.SelectionReason != "dynamic workload route" {
		t.Fatalf("reason = %q", dec.Attribution.SelectionReason)
	}
}

func TestResolveDynamicAuthorizedModelWithHeadCanPromote(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	header := http.Header{}
	selected, name, dec, err := ResolveDynamicAuthorizedModelWithHead(header, map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}, models, reg, "", &llmpool.HeadRuntime{
		Mode: llmpool.PipelineOn,
		Predict: func(string) llmpool.HeadPrediction {
			return llmpool.HeadPrediction{Class: llmpool.WorkloadClassPlan, MaxP: 0.91}
		},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selected == nil || name != llmpool.OfficialTierHigh {
		t.Fatalf("selected = %#v name=%s", selected, name)
	}
	if dec == nil || !dec.HeadUsed || dec.Class != llmpool.WorkloadClassPlan {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestResolveDynamicAuthorizedModelPinSkipsL1(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	selected, name, dec, err := ResolveDynamicAuthorizedModel(header, map[string]any{"model": llmpool.OfficialTierMid}, models, reg, "")
	if err != nil {
		t.Fatalf("ResolveDynamicAuthorizedModel() error = %v", err)
	}
	if selected == nil || selected.Name != llmpool.OfficialTierMid || name != llmpool.OfficialTierMid {
		t.Fatalf("selected = %#v name=%s", selected, name)
	}
	if dec == nil || !dec.Passthrough {
		t.Fatalf("expected pin passthrough, got %#v", dec)
	}
}

func TestOfficialUpstreamModelAndChargeSplit(t *testing.T) {
	model := &AuthorizedModel{
		Name:                   llmpool.OfficialTierHigh,
		ChargedServiceGroupIDs: []string{"coding-auto"},
		ServiceGroupIDs:        []string{"coding-auto"},
		ProviderUpstreamModels: map[string]string{"maclaw_official": llmpool.OfficialTierHigh},
		ProviderServiceGroups:  map[string][]string{"maclaw_official": {llmpool.OfficialGroupID}},
	}
	if got := OfficialUpstreamModel(model, MaClawOfficialProviderID); got != llmpool.OfficialTierHigh {
		t.Fatalf("OfficialUpstreamModel = %q", got)
	}
	if got := ChargedServiceGroupIDs(model, MaClawOfficialProviderID); len(got) != 1 || got[0] != "coding-auto" {
		t.Fatalf("ChargedServiceGroupIDs = %#v", got)
	}
	if got := ServiceGroupIDsForProvider(model, MaClawOfficialProviderID); len(got) != 1 || got[0] != llmpool.OfficialGroupID {
		t.Fatalf("ServiceGroupIDsForProvider = %#v", got)
	}
}

func TestOfficialUpstreamModelForLogicalModelKeepsSiblingRoutesDistinct(t *testing.T) {
	model := &AuthorizedModel{
		Name: llmpool.OfficialTierHigh,
		ProviderUpstreamModels: map[string]string{
			"maclaw_official": llmpool.OfficialTierMid,
		},
		ProviderUpstreamRouteModels: map[string]map[string]string{
			"maclaw_official": {
				llmpool.OfficialTierHigh: llmpool.OfficialTierHigh,
				llmpool.OfficialTierMid:  llmpool.OfficialTierMid,
			},
		},
	}
	if got := OfficialUpstreamModelForLogicalModel(model, MaClawOfficialProviderID, llmpool.OfficialTierHigh); got != llmpool.OfficialTierHigh {
		t.Fatalf("high upstream = %q", got)
	}
	if got := OfficialUpstreamModelForLogicalModel(model, MaClawOfficialProviderID, llmpool.OfficialTierMid); got != llmpool.OfficialTierMid {
		t.Fatalf("mid upstream = %q", got)
	}
}

func TestOfficialUpstreamModelUsesChargedServiceGroup(t *testing.T) {
	model := &AuthorizedModel{
		Name:                   "shared",
		ChargedServiceGroupIDs: []string{"group-one"},
		ProviderServiceGroupUpstreams: map[string]map[string]string{
			"maclaw_official": {
				"group-one": "upstream-one",
				"group-two": "upstream-two",
			},
		},
	}
	if got := OfficialUpstreamModel(model, MaClawOfficialProviderID); got != "upstream-one" {
		t.Fatalf("first charged group upstream = %q", got)
	}
	model.ChargedServiceGroupIDs = []string{"group-two"}
	if got := OfficialUpstreamModel(model, MaClawOfficialProviderID); got != "upstream-two" {
		t.Fatalf("second charged group upstream = %q", got)
	}
}

func TestCloneAuthorizedModelDoesNotShareRouteBillingMaps(t *testing.T) {
	base := &AuthorizedModel{
		ProviderServiceGroupUpstreams: map[string]map[string]string{"provider-a": {"group-a": "upstream-a"}},
		ProviderRouteBilling: map[string]map[string]ProviderRouteBilling{"provider-a": {
			"upstream-a": {BillingMode: llmpool.BillingModePaid, TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2}},
		}},
	}
	clone := cloneAuthorizedModel(base)
	clone.ProviderServiceGroupUpstreams["provider-a"]["group-a"] = "changed"
	route := clone.ProviderRouteBilling["provider-a"]["upstream-a"]
	route.TokenPricing.InputCreditsPer10K = 99
	clone.ProviderRouteBilling["provider-a"]["upstream-a"] = route
	if got := base.ProviderServiceGroupUpstreams["provider-a"]["group-a"]; got != "upstream-a" {
		t.Fatalf("original upstream map mutated: %q", got)
	}
	if got := base.ProviderRouteBilling["provider-a"]["upstream-a"].TokenPricing.InputCreditsPer10K; got != 1 {
		t.Fatalf("original route price mutated: %v", got)
	}
}

func TestPublicAuthorizedModelsHidesInternalDynamicNames(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	public := PublicAuthorizedModels(models, reg)
	if len(public) != 1 || public[0].Name != "auto" {
		t.Fatalf("public = %#v", public)
	}
}

func TestValidateDynamicGroupsRequiresOfficialTier(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	reg.ModelServiceGroups[0].Models[0].ProviderConfigs[0].Model = "gpt-4"
	if err := reg.ValidateDynamicGroups(); err == nil {
		t.Fatal("expected official model enum error")
	}
}

func TestModelServiceGroupToPoolGroupPreservesRouteBilling(t *testing.T) {
	pricing := llmpool.TokenPricing{
		InputCreditsPer10K:  1,
		OutputCreditsPer10K: 4,
		Timezone:            "Asia/Shanghai",
		Version:             "2026-08",
	}
	group := ModelServiceGroup{Models: []ModelServiceModel{{
		Name:        "auto",
		ProviderIDs: []string{MaClawOfficialProviderID},
		ProviderConfigs: []ModelServiceProviderConfig{{
			ProviderID:   MaClawOfficialProviderID,
			Model:        llmpool.OfficialTierMid,
			BillingMode:  llmpool.BillingModePaid,
			TokenPricing: pricing,
		}},
	}}}

	pool := group.ToPoolGroup()
	if len(pool.Models) != 1 || len(pool.Models[0].ProviderConfigs) != 1 {
		t.Fatalf("pool configs = %#v", pool.Models)
	}
	got := pool.Models[0].ProviderConfigs[0]
	if got.BillingMode != llmpool.BillingModePaid ||
		got.TokenPricing.InputCreditsPer10K != pricing.InputCreditsPer10K ||
		got.TokenPricing.OutputCreditsPer10K != pricing.OutputCreditsPer10K ||
		got.TokenPricing.Timezone != pricing.Timezone ||
		got.TokenPricing.Version != pricing.Version {
		t.Fatalf("route billing lost during pool conversion: %#v", got)
	}
}

func dynamicFixture() ModelServiceGroup {
	return ModelServiceGroup{
		ID:     "coding-auto",
		Name:   "Coding Auto",
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []ModelServiceModel{
			{Name: "auto", ProviderIDs: []string{MaClawOfficialProviderID}, ProviderConfigs: []ModelServiceProviderConfig{{ProviderID: MaClawOfficialProviderID, Model: llmpool.OfficialTierMid}}},
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{MaClawOfficialProviderID}, ProviderConfigs: []ModelServiceProviderConfig{{ProviderID: MaClawOfficialProviderID, Model: llmpool.OfficialTierHigh}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{MaClawOfficialProviderID}, ProviderConfigs: []ModelServiceProviderConfig{{ProviderID: MaClawOfficialProviderID, Model: llmpool.OfficialTierMid}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{MaClawOfficialProviderID}, ProviderConfigs: []ModelServiceProviderConfig{{ProviderID: MaClawOfficialProviderID, Model: llmpool.OfficialTierLow}}},
		},
	}
}
