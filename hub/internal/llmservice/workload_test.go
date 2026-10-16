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
	selected, _, dec, err := ResolveDynamicAuthorizedModel(header, map[string]any{"model": "high"}, models, reg, "coding-auto")
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
	alias := &AuthorizedModel{Name: "low"}
	if got := OfficialUpstreamModel(alias, MaClawOfficialProviderID); got != llmpool.OfficialTierLow {
		t.Fatalf("low alias upstream = %q", got)
	}
	routed := &AuthorizedModel{ProviderUpstreamRouteModels: map[string]map[string]string{
		"maclaw_official": {"low": "cheap-low"},
	}}
	if got := OfficialUpstreamModelForLogicalModel(routed, MaClawOfficialProviderID, llmpool.OfficialTierLow); got != "cheap-low" {
		t.Fatalf("alias route upstream = %q", got)
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
	group := dynamicFixture()
	group.ExposedModels = []string{"auto"}
	group.Models = append(group.Models, ModelServiceModel{Name: "secret-internal"})
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{group}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	public := PublicAuthorizedModels(models, reg)
	if len(public) != 1 || public[0].Name != "auto" {
		t.Fatalf("public = %#v", public)
	}
}

func TestPublicAuthorizedModelsExposesCapabilityCatalog(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dynamicFixture()}}
	models, _ := buildAuthorizedModels(reg, []string{"coding-auto"})
	public := PublicAuthorizedModels(models, reg)
	got := make([]string, 0, len(public))
	for _, model := range public {
		got = append(got, model.Name)
	}
	want := []string{"auto", llmpool.OfficialTierHigh, llmpool.OfficialTierMid, llmpool.OfficialTierLow}
	if len(got) != len(want) {
		t.Fatalf("public = %#v", got)
	}
	seen := map[string]bool{}
	for _, name := range got {
		seen[name] = true
	}
	for _, name := range want {
		if !seen[name] {
			t.Fatalf("public = %#v, missing %s", got, name)
		}
	}
}

func TestPublicAuthorizedModelsOffersCapabilityBandsFromAuto(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:     MaClawOfficialServiceGroupID,
		Models: []ModelServiceModel{{Name: "auto", ProviderIDs: []string{MaClawOfficialProviderID}}},
	}}}
	models := []AuthorizedModel{{
		Name:                   "auto",
		ProviderIDs:            []string{MaClawOfficialProviderID},
		ServiceGroupIDs:        []string{MaClawOfficialServiceGroupID},
		ProviderUpstreamModels: map[string]string{MaClawOfficialProviderID: "gpt-4o"},
	}}
	public := PublicAuthorizedModels(models, reg)
	byName := map[string]AuthorizedModel{}
	for _, model := range public {
		byName[model.Name] = model
	}
	for _, name := range []string{"auto", llmpool.OfficialTierLow, llmpool.OfficialTierMid, llmpool.OfficialTierHigh} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("public missing %s: %#v", name, public)
		}
	}
	if byName[llmpool.OfficialTierLow].BillingMultiplier != 0.5 || byName[llmpool.OfficialTierMid].BillingMultiplier != 1 || byName[llmpool.OfficialTierHigh].BillingMultiplier != 2 {
		t.Fatalf("fees = low %v mid %v high %v", byName[llmpool.OfficialTierLow].BillingMultiplier, byName[llmpool.OfficialTierMid].BillingMultiplier, byName[llmpool.OfficialTierHigh].BillingMultiplier)
	}
	low := byName[llmpool.OfficialTierLow]
	if got := OfficialUpstreamModel(&low, MaClawOfficialProviderID); got != llmpool.OfficialTierLow {
		t.Fatalf("pinned upstream = %q", got)
	}
	if got := OfficialUpstreamModel(&models[0], MaClawOfficialProviderID); got != "gpt-4o" {
		t.Fatalf("auto upstream changed to %q", got)
	}
	selected, name, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": "low"}, models, reg, "", nil)
	if err != nil || selected == nil || selected.Name != llmpool.OfficialTierLow || name != "low" || selected.BillingMultiplier != 0.5 {
		t.Fatalf("resolve low = name %q model %#v err %v", name, selected, err)
	}

	staticReg := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:     "coding-pro",
		Models: []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
	}}}
	staticModels := []AuthorizedModel{{Name: "auto", ProviderIDs: []string{"provider-a"}, ServiceGroupIDs: []string{"coding-pro"}}}
	if got := PublicAuthorizedModels(staticModels, staticReg); len(got) != 1 || got[0].Name != "auto" {
		t.Fatalf("static public = %#v", got)
	}
}

func TestResolveMidAliasWhenCatalogOnlyPublishesAuto(t *testing.T) {
	group := ModelServiceGroup{
		ID:            "redeem",
		Kind:          llmpool.ServiceGroupKindDynamic,
		ExposedModels: []string{"auto"},
		Models:        []ModelServiceModel{{Name: "auto", ProviderIDs: []string{MaClawOfficialProviderID}}},
	}
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{group}}
	models := []AuthorizedModel{{
		Name:            "auto",
		ProviderIDs:     []string{MaClawOfficialProviderID},
		ServiceGroupIDs: []string{group.ID},
	}}
	for _, asked := range []string{"mid", "low", "high", llmpool.OfficialTierMid} {
		selected, name, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": asked}, models, reg, "", nil)
		want := llmpool.CanonicalClientModel(asked)
		if err != nil || selected == nil || selected.Name != want || name != asked {
			t.Fatalf("resolve %q = name %q model %#v err %v", asked, name, selected, err)
		}
	}
	if selected, _, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": "mid"}, models, reg, "", nil); err != nil || selected == nil || selected.BillingMultiplier != 1 {
		t.Fatalf("mid fee = %#v err %v", selected, err)
	}

	staticReg := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:     "coding-pro",
		Models: []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
	}}}
	staticModels := []AuthorizedModel{{Name: "auto", ProviderIDs: []string{"provider-a"}, ServiceGroupIDs: []string{"coding-pro"}}}
	if _, _, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": "mid"}, staticModels, staticReg, "", nil); err == nil {
		t.Fatal("static group accepted mid")
	}

	other := ModelServiceGroup{
		ID:            "coding-auto",
		Kind:          llmpool.ServiceGroupKindDynamic,
		ExposedModels: []string{"auto"},
		Models:        []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
	}
	otherReg := &Registry{ModelServiceGroups: []ModelServiceGroup{other}}
	otherModels := []AuthorizedModel{{Name: "auto", ProviderIDs: []string{"provider-a"}, ServiceGroupIDs: []string{other.ID}}}
	if _, _, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": "mid"}, otherModels, otherReg, "", nil); err == nil {
		t.Fatal("non-official dynamic group accepted mid")
	}
}

func TestSystemFreeOfficialAutoAuthorizesCapabilityBands(t *testing.T) {
	group := SystemFreeTemplate()
	group.ExposedModels = []string{"auto"}
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{group}}
	models := []AuthorizedModel{{
		Name:            "auto",
		ProviderIDs:     []string{MaClawOfficialProviderID},
		ServiceGroupIDs: []string{group.ID},
	}}
	public := PublicAuthorizedModels(models, reg)
	byName := map[string]AuthorizedModel{}
	for _, model := range public {
		byName[model.Name] = model
	}
	for _, name := range []string{"auto", llmpool.OfficialTierLow, llmpool.OfficialTierMid, llmpool.OfficialTierHigh} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("public missing %s: %#v", name, public)
		}
	}
	for _, asked := range []string{"mid", "low", "high", llmpool.OfficialTierHigh} {
		selected, name, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": asked}, models, reg, group.ID, nil)
		want := llmpool.CanonicalClientModel(asked)
		if err != nil || selected == nil || selected.Name != want || name != asked {
			t.Fatalf("resolve %q = name %q model %#v err %v", asked, name, selected, err)
		}
	}

	foreign := SystemFreeTemplate()
	foreign.Models = []ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}}
	foreignReg := &Registry{ModelServiceGroups: []ModelServiceGroup{foreign}}
	foreignModels := []AuthorizedModel{{Name: "auto", ProviderIDs: []string{"provider-a"}, ServiceGroupIDs: []string{foreign.ID}}}
	if got := PublicAuthorizedModels(foreignModels, foreignReg); len(got) != 1 || got[0].Name != "auto" {
		t.Fatalf("non-official system-free public = %#v", got)
	}
	if _, _, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": "high"}, foreignModels, foreignReg, foreign.ID, nil); err == nil {
		t.Fatal("non-official system-free accepted high")
	}

	configured := SystemFreeTemplate()
	configured.Models = []ModelServiceModel{{
		Name:            "auto",
		ProviderConfigs: []ModelServiceProviderConfig{{ProviderID: MaClawOfficialProviderID}},
	}}
	configuredReg := &Registry{ModelServiceGroups: []ModelServiceGroup{configured}}
	configuredModels, _ := buildAuthorizedModels(configuredReg, []string{configured.ID})
	if len(configuredModels) != 1 || len(configuredModels[0].ProviderIDs) != 1 || configuredModels[0].ProviderIDs[0] != MaClawOfficialProviderID {
		t.Fatalf("built from provider config = %#v", configuredModels)
	}
	if got := PublicAuthorizedModels(configuredModels, configuredReg); len(got) != 4 {
		t.Fatalf("provider config official auto public = %#v", got)
	}
	if _, _, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": "high"}, configuredModels, configuredReg, configured.ID, nil); err != nil {
		t.Fatal(err)
	}

	stale := SystemFreeTemplate()
	stale.Models = []ModelServiceModel{{
		Name:        "auto",
		ProviderIDs: []string{"provider-a"},
		ProviderConfigs: []ModelServiceProviderConfig{
			{ProviderID: "provider-a"},
			{ProviderID: MaClawOfficialProviderID},
		},
	}}
	staleReg := &Registry{ModelServiceGroups: []ModelServiceGroup{stale}}
	staleModels, _ := buildAuthorizedModels(staleReg, []string{stale.ID})
	if len(staleModels) != 1 || len(staleModels[0].ProviderIDs) != 1 || staleModels[0].ProviderIDs[0] != "provider-a" {
		t.Fatalf("stale official config revived providers: %#v", staleModels)
	}
	if got := PublicAuthorizedModels(staleModels, staleReg); len(got) != 1 || got[0].Name != "auto" {
		t.Fatalf("stale official config public = %#v", got)
	}

	status := &ServiceStatus{
		DefaultModel:      "auto",
		AuthorizedModels:  models,
		AvailableModels:   []string{"auto"},
	}
	PublishStatusCapabilityBands(status, reg)
	if status.DefaultModel != "auto" {
		t.Fatalf("default model changed to %q", status.DefaultModel)
	}
	if len(status.AvailableModels) != 4 || len(status.AuthorizedModels) != 4 {
		t.Fatalf("status models = available %#v authorized %#v", status.AvailableModels, status.AuthorizedModels)
	}
}

func TestResolveKeepsPublishedCapabilityAlias(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:     "coding-pro",
		Models: []ModelServiceModel{{Name: "low", ProviderIDs: []string{"provider-a"}, BillingMultiplier: 0.25}},
	}}}
	models := []AuthorizedModel{{
		Name:              "low",
		ProviderIDs:       []string{"provider-a"},
		ServiceGroupIDs:   []string{"coding-pro"},
		BillingMultiplier: 0.25,
	}}
	for _, asked := range []string{"low", llmpool.OfficialTierLow} {
		selected, name, _, err := ResolveDynamicAuthorizedModelWithHead(nil, map[string]any{"model": asked}, models, reg, "", nil)
		if err != nil || selected == nil || selected.Name != "low" || name != asked || selected.BillingMultiplier != 0.25 {
			t.Fatalf("asked %q resolved name %q model %#v err %v", asked, name, selected, err)
		}
	}
}

func TestFindPublicAuthorizedModelMatchesListedCapabilityAlias(t *testing.T) {
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{{
		ID:     MaClawOfficialServiceGroupID,
		Models: []ModelServiceModel{{Name: "auto", ProviderIDs: []string{MaClawOfficialProviderID}}},
	}}}
	models := []AuthorizedModel{{
		Name:            "auto",
		ProviderIDs:     []string{MaClawOfficialProviderID},
		ServiceGroupIDs: []string{MaClawOfficialServiceGroupID},
	}}
	got, ok := FindPublicAuthorizedModel(models, reg, "low")
	if !ok || got.Name != llmpool.OfficialTierLow || got.BillingMultiplier != 0.5 {
		t.Fatalf("low lookup = %#v ok=%v", got, ok)
	}
	got, ok = FindPublicAuthorizedModel(models, reg, llmpool.OfficialTierHigh)
	if !ok || got.Name != llmpool.OfficialTierHigh || got.BillingMultiplier != 2 {
		t.Fatalf("official-high lookup = %#v ok=%v", got, ok)
	}

	dyn := dynamicFixture()
	dyn.ExposedModels = []string{"auto"}
	reg.ModelServiceGroups = append(reg.ModelServiceGroups, dyn)
	mixed, _ := buildAuthorizedModels(reg, []string{dyn.ID, MaClawOfficialServiceGroupID})
	if _, ok := FindPublicAuthorizedModel(mixed, reg, llmpool.OfficialTierHigh); ok {
		t.Fatal("hidden official-high was returned from the public catalog")
	}
}

func TestHiddenDynamicTierKeepsOfficialCapabilityName(t *testing.T) {
	dyn := dynamicFixture()
	dyn.ExposedModels = []string{"auto"}
	official := ModelServiceGroup{
		ID:     MaClawOfficialServiceGroupID,
		Models: []ModelServiceModel{{Name: "auto", ProviderIDs: []string{MaClawOfficialProviderID}}},
	}
	reg := &Registry{ModelServiceGroups: []ModelServiceGroup{dyn, official}}
	models, _ := buildAuthorizedModels(reg, []string{dyn.ID, MaClawOfficialServiceGroupID})
	public := PublicAuthorizedModels(models, reg)
	seenAuto := false
	for _, model := range public {
		if model.Name == "auto" {
			seenAuto = true
			continue
		}
		if llmpool.IsOfficialTierName(llmpool.CanonicalClientModel(model.Name)) {
			t.Fatalf("hidden tier leaked into the public catalog: %#v", public)
		}
	}
	if !seenAuto {
		t.Fatalf("public = %#v, missing auto", public)
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
