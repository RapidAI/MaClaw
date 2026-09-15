package llmservice

import (
	"net/http"
	"strings"
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

func TestResolveDynamicAuthorizedModelPinPlanDoesNotAttachLow(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	header := http.Header{}
	header.Set(llmpool.WorkflowTypeHeader, "business_plan")
	selected, _, dec, err := ResolveDynamicAuthorizedModel(header, map[string]any{"model": llmpool.OfficialTierHigh}, models, reg, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selected == nil || selected.Name != llmpool.OfficialTierHigh {
		t.Fatalf("selected = %#v", selected)
	}
	if dec == nil || !dec.Passthrough || dec.Class != llmpool.WorkloadClassPlan {
		t.Fatalf("decision = %#v, want pin passthrough plan", dec)
	}
	if len(selected.AvailabilityFallbacks) != 1 || selected.AvailabilityFallbacks[0].Name != llmpool.OfficialTierMid {
		t.Fatalf("plan pin fallbacks = %#v, want official-mid only", selected.AvailabilityFallbacks)
	}
}

func TestResolveDynamicAuthorizedModelNoDynamicGroupPlanDoesNotAttachLow(t *testing.T) {
	models := []AuthorizedModel{
		{Name: llmpool.OfficialTierHigh, ServiceGroupIDs: []string{"static"}, ProviderIDs: []string{"p"}, CreditMultiplier: 2},
		{Name: llmpool.OfficialTierMid, ServiceGroupIDs: []string{"static"}, ProviderIDs: []string{"p"}, CreditMultiplier: 1},
		{Name: llmpool.OfficialTierLow, ServiceGroupIDs: []string{"static"}, ProviderIDs: []string{"p"}, CreditMultiplier: 0.1},
	}
	header := http.Header{}
	header.Set(llmpool.WorkflowTypeHeader, "business_plan")
	selected, _, dec, err := ResolveDynamicAuthorizedModel(header, map[string]any{"model": "auto"}, models, &Registry{}, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if dec == nil || dec.Class != llmpool.WorkloadClassPlan {
		t.Fatalf("decision = %#v, want plan", dec)
	}
	if selected == nil || selected.Name != llmpool.OfficialTierMid {
		t.Fatalf("selected = %#v, want official-mid, not cheapest official-low", selected)
	}
	for _, fb := range selected.AvailabilityFallbacks {
		if fb.Name == llmpool.OfficialTierLow {
			t.Fatalf("fallbacks = %#v, plan must not attach official-low", selected.AvailabilityFallbacks)
		}
	}
}

func TestResolveDynamicAuthorizedModelFallsBackWhenHighUnauthorized(t *testing.T) {
	group := dynamicFixture()
	group.Models = []ModelServiceModel{
		{Name: "auto", ProviderIDs: []string{MaClawOfficialProviderID}, ProviderConfigs: []ModelServiceProviderConfig{{ProviderID: MaClawOfficialProviderID, Model: llmpool.OfficialTierMid}}},
		{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{MaClawOfficialProviderID}, ProviderConfigs: []ModelServiceProviderConfig{{ProviderID: MaClawOfficialProviderID, Model: llmpool.OfficialTierHigh}}},
		{Name: llmpool.OfficialTierMid, ProviderIDs: []string{MaClawOfficialProviderID}, ProviderConfigs: []ModelServiceProviderConfig{{ProviderID: MaClawOfficialProviderID, Model: llmpool.OfficialTierMid}}},
	}
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{group}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	filtered := models[:0]
	for _, model := range models {
		if strings.EqualFold(model.Name, llmpool.OfficialTierHigh) {
			continue
		}
		filtered = append(filtered, model)
	}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	selected, name, dec, err := ResolveDynamicAuthorizedModel(header, map[string]any{"model": "auto"}, filtered, reg, "")
	if err != nil {
		t.Fatalf("ResolveDynamicAuthorizedModel() error = %v", err)
	}
	if selected == nil || selected.Name != llmpool.OfficialTierMid || name != llmpool.OfficialTierMid {
		t.Fatalf("selected = %#v name=%s, want official-mid", selected, name)
	}
	if dec == nil || !dec.AvailabilityFallback || dec.ResolvedModel != llmpool.OfficialTierMid {
		t.Fatalf("decision = %#v", dec)
	}
}

func TestResolveDynamicAuthorizedModelAttachesAvailabilityFallbacks(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	selected, _, _, err := ResolveDynamicAuthorizedModel(header, map[string]any{"model": "auto"}, models, reg, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selected == nil || selected.Name != llmpool.OfficialTierHigh {
		t.Fatalf("selected = %#v", selected)
	}
	if len(selected.AvailabilityFallbacks) != 1 || selected.AvailabilityFallbacks[0].Name != llmpool.OfficialTierMid {
		t.Fatalf("plan fallbacks = %#v, want official-mid only", selected.AvailabilityFallbacks)
	}
	if got := selected.AvailabilityFallbacks[0].ChargedServiceGroupIDs; len(got) != 1 || got[0] != "coding-auto" {
		t.Fatalf("fallback charged group = %#v, want coding-auto", got)
	}
}

func TestSiblingAuthorizedModelsStayInTheSameGroup(t *testing.T) {
	selected := &AuthorizedModel{
		Name:                   llmpool.OfficialTierHigh,
		ChargedServiceGroupIDs: []string{"coding-auto"},
		ServiceGroupIDs:        []string{"coding-auto"},
	}
	models := []AuthorizedModel{
		*selected,
		{Name: llmpool.OfficialTierMid, ServiceGroupIDs: []string{"other-group"}},
		{Name: llmpool.OfficialTierLow, ServiceGroupIDs: []string{"coding-auto"}},
	}
	got := siblingAuthorizedModels(models, selected, llmpool.WorkloadClassChat)
	if len(got) != 1 || got[0].Name != llmpool.OfficialTierLow {
		t.Fatalf("siblings = %#v, want same-group official-low only", got)
	}
	if ids := got[0].ChargedServiceGroupIDs; len(ids) != 1 || ids[0] != "coding-auto" {
		t.Fatalf("sibling charged group = %#v, want selected group's id", ids)
	}
	orphan := &AuthorizedModel{Name: llmpool.OfficialTierHigh}
	if extras := siblingAuthorizedModels(models, orphan, llmpool.WorkloadClassChat); len(extras) != 0 {
		t.Fatalf("ungrouped model must not inherit other groups, got %#v", extras)
	}
}

func TestFallbackAuthorizedOfficialModelStaysInRequestedGroup(t *testing.T) {
	models := []AuthorizedModel{
		{Name: llmpool.OfficialTierMid, ServiceGroupIDs: []string{"other-group"}},
		{Name: llmpool.OfficialTierLow, ServiceGroupIDs: []string{"coding-auto"}},
	}
	if got := fallbackAuthorizedOfficialModel(models, llmpool.OfficialTierHigh, llmpool.WorkloadClassChat, "coding-auto"); got == nil || got.Name != llmpool.OfficialTierLow {
		t.Fatalf("same-group fallback = %#v, want official-low", got)
	}
	if got := fallbackAuthorizedOfficialModel(models, llmpool.OfficialTierHigh, llmpool.WorkloadClassChat, "coding-auto"); got != nil && got.ServiceGroupIDs[0] == "other-group" {
		t.Fatalf("must not take other-group official-mid, got %#v", got)
	}
	if got := fallbackAuthorizedOfficialModel(models, llmpool.OfficialTierHigh, llmpool.WorkloadClassChat); got != nil {
		t.Fatalf("pin without group must not fallback, got %#v", got)
	}
}

func TestFallbackAuthorizedOfficialModelSkipsTierWithoutChargedProviders(t *testing.T) {
	models := []AuthorizedModel{
		{
			Name:            llmpool.OfficialTierMid,
			ProviderIDs:     []string{"other-provider"},
			ServiceGroupIDs: []string{"coding-auto"},
			ProviderServiceGroups: map[string][]string{
				"other-provider": {"other-group"},
			},
		},
		{
			Name:            llmpool.OfficialTierLow,
			ProviderIDs:     []string{MaClawOfficialProviderID},
			ServiceGroupIDs: []string{"coding-auto"},
			ProviderServiceGroups: map[string][]string{
				MaClawOfficialProviderID: {"coding-auto"},
			},
		},
	}
	got := fallbackAuthorizedOfficialModel(models, llmpool.OfficialTierHigh, llmpool.WorkloadClassChat, "coding-auto")
	if got == nil || got.Name != llmpool.OfficialTierLow {
		t.Fatalf("fallback = %#v, want coding-auto official-low", got)
	}
}

func TestResolveDynamicAuthorizedModelPinSkipsTierWithoutChargedProviders(t *testing.T) {
	models := []AuthorizedModel{
		{
			Name:            llmpool.OfficialTierMid,
			ProviderIDs:     []string{"other-provider"},
			ServiceGroupIDs: []string{"coding-auto"},
			ProviderServiceGroups: map[string][]string{
				"other-provider": {"other-group"},
			},
		},
		{
			Name:            llmpool.OfficialTierLow,
			ProviderIDs:     []string{MaClawOfficialProviderID},
			ServiceGroupIDs: []string{"coding-auto"},
			ProviderServiceGroups: map[string][]string{
				MaClawOfficialProviderID: {"coding-auto"},
			},
		},
	}
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	selected, _, dec, err := ResolveDynamicAuthorizedModel(nil, map[string]any{"model": llmpool.OfficialTierMid}, models, reg, "coding-auto")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selected == nil || selected.Name != llmpool.OfficialTierLow {
		t.Fatalf("selected = %#v, want official-low in coding-auto", selected)
	}
	if dec == nil || !dec.AvailabilityFallback {
		t.Fatalf("decision = %#v, want availability fallback", dec)
	}
}

func TestPinChargedGroupPrefersGroupThatHasProviders(t *testing.T) {
	coding := dynamicFixture()
	other := dynamicFixture()
	other.ID = "other-group"
	other.Name = "Other"
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{other, coding}}
	model := &AuthorizedModel{
		Name:            llmpool.OfficialTierMid,
		ProviderIDs:     []string{MaClawOfficialProviderID},
		ServiceGroupIDs: []string{"other-group", "coding-auto"},
		ProviderServiceGroups: map[string][]string{
			MaClawOfficialProviderID: {"coding-auto"},
		},
	}
	pinChargedGroup(model, reg)
	if len(model.ChargedServiceGroupIDs) != 1 || model.ChargedServiceGroupIDs[0] != "coding-auto" {
		t.Fatalf("charged = %#v, want coding-auto which actually has the provider", model.ChargedServiceGroupIDs)
	}
	if len(model.ProviderIDs) != 1 || model.ProviderIDs[0] != MaClawOfficialProviderID {
		t.Fatalf("providers = %#v, restrict must keep the serving backend", model.ProviderIDs)
	}
}

func TestFallbackAuthorizedOfficialModelSkipsEarlierOtherGroupDuplicate(t *testing.T) {
	models := []AuthorizedModel{
		{Name: llmpool.OfficialTierMid, ServiceGroupIDs: []string{"other-group"}},
		{Name: llmpool.OfficialTierMid, ServiceGroupIDs: []string{"coding-auto"}},
		{Name: llmpool.OfficialTierLow, ServiceGroupIDs: []string{"coding-auto"}},
	}
	got := fallbackAuthorizedOfficialModel(models, llmpool.OfficialTierHigh, llmpool.WorkloadClassChat, "coding-auto")
	if got == nil || got.Name != llmpool.OfficialTierMid || len(got.ServiceGroupIDs) != 1 || got.ServiceGroupIDs[0] != "coding-auto" {
		t.Fatalf("same-group fallback = %#v, want coding-auto official-mid", got)
	}
}

func TestSiblingAuthorizedModelsSkipsEarlierOtherGroupDuplicate(t *testing.T) {
	selected := &AuthorizedModel{
		Name:                   llmpool.OfficialTierHigh,
		ChargedServiceGroupIDs: []string{"coding-auto"},
		ServiceGroupIDs:        []string{"coding-auto"},
	}
	models := []AuthorizedModel{
		*selected,
		{Name: llmpool.OfficialTierMid, ServiceGroupIDs: []string{"other-group"}},
		{Name: llmpool.OfficialTierMid, ServiceGroupIDs: []string{"coding-auto"}},
		{Name: llmpool.OfficialTierLow, ServiceGroupIDs: []string{"coding-auto"}},
	}
	got := siblingAuthorizedModels(models, selected, llmpool.WorkloadClassChat)
	if len(got) != 2 || got[0].Name != llmpool.OfficialTierMid || got[1].Name != llmpool.OfficialTierLow {
		t.Fatalf("siblings = %#v, want same-group mid then low", got)
	}
	if ids := got[0].ServiceGroupIDs; len(ids) != 1 || ids[0] != "coding-auto" {
		t.Fatalf("mid sibling groups = %#v, want coding-auto", ids)
	}
}

func TestResolveDynamicAuthorizedModelPinChargesRequestedGroup(t *testing.T) {
	coding := dynamicFixture()
	other := dynamicFixture()
	other.ID = "other-group"
	other.Name = "Other"
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{other, coding}}
	models, _ := buildAuthorizedModels(reg, []string{"other-group", "coding-auto"})
	selected, name, _, err := ResolveDynamicAuthorizedModel(nil, map[string]any{"model": llmpool.OfficialTierMid}, models, reg, "coding-auto")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selected == nil || name != llmpool.OfficialTierMid {
		t.Fatalf("selected = %#v name=%s", selected, name)
	}
	if len(selected.ChargedServiceGroupIDs) != 1 || selected.ChargedServiceGroupIDs[0] != "coding-auto" {
		t.Fatalf("charged = %#v, want coding-auto", selected.ChargedServiceGroupIDs)
	}
}

func TestResolveDynamicAuthorizedModelPinDropsOtherGroupProviders(t *testing.T) {
	coding := dynamicFixture()
	other := dynamicFixture()
	other.ID = "other-group"
	other.Name = "Other"
	for i := range other.Models {
		other.Models[i].ProviderIDs = []string{"other-provider"}
		for j := range other.Models[i].ProviderConfigs {
			other.Models[i].ProviderConfigs[j].ProviderID = "other-provider"
		}
	}
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{other, coding}}
	models, _ := buildAuthorizedModels(reg, []string{"other-group", "coding-auto"})
	selected, _, _, err := ResolveDynamicAuthorizedModel(nil, map[string]any{"model": llmpool.OfficialTierMid}, models, reg, "coding-auto")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selected == nil {
		t.Fatal("selected is nil")
	}
	for _, id := range selected.ProviderIDs {
		if strings.EqualFold(id, "other-provider") {
			t.Fatalf("providers = %#v, must not dispatch other-group backend", selected.ProviderIDs)
		}
	}
	if len(selected.ProviderIDs) != 1 || selected.ProviderIDs[0] != MaClawOfficialProviderID {
		t.Fatalf("providers = %#v, want only %s", selected.ProviderIDs, MaClawOfficialProviderID)
	}
	for _, fb := range selected.AvailabilityFallbacks {
		for _, id := range fb.ProviderIDs {
			if strings.EqualFold(id, "other-provider") {
				t.Fatalf("fallback %s providers = %#v, must not dispatch other-group backend", fb.Name, fb.ProviderIDs)
			}
		}
	}
}

func TestResolveDynamicAuthorizedModelPinDoesNotUseOtherGroupHigh(t *testing.T) {
	coding := dynamicFixture()
	filtered := coding.Models[:0]
	for _, model := range coding.Models {
		if strings.EqualFold(model.Name, llmpool.OfficialTierHigh) {
			continue
		}
		filtered = append(filtered, model)
	}
	coding.Models = filtered
	other := dynamicFixture()
	other.ID = "other-group"
	other.Name = "Other"
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{other, coding}}
	models, _ := buildAuthorizedModels(reg, []string{"other-group", "coding-auto"})
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	selected, _, dec, err := ResolveDynamicAuthorizedModel(header, map[string]any{"model": llmpool.OfficialTierHigh}, models, reg, "coding-auto")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if selected == nil || selected.Name != llmpool.OfficialTierMid {
		t.Fatalf("selected = %#v, want coding-auto official-mid", selected)
	}
	if dec == nil || !dec.AvailabilityFallback || dec.ResolvedModel != llmpool.OfficialTierMid {
		t.Fatalf("decision = %#v, want availability fallback to official-mid", dec)
	}
	if len(selected.ChargedServiceGroupIDs) != 1 || selected.ChargedServiceGroupIDs[0] != "coding-auto" {
		t.Fatalf("charged = %#v, want coding-auto", selected.ChargedServiceGroupIDs)
	}
	for _, fb := range selected.AvailabilityFallbacks {
		if fb.Name == llmpool.OfficialTierHigh {
			t.Fatalf("must not attach other-group official-high, fallbacks = %#v", selected.AvailabilityFallbacks)
		}
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
