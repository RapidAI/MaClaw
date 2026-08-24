package llmservice

import (
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestApplyProxyWorkloadRoutingSkipsL1ForConcreteModel(t *testing.T) {
	group := &llmpool.ServiceGroup{
		ID:     llmpool.OfficialGroupID,
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmpool.ModelConfig{
			{Name: "auto", ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
		},
	}
	reg := &Registry{ServiceGroups: []llmpool.ServiceGroup{*group}}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": llmpool.OfficialTierHigh}}
	dispatch := matchProxyGroupModel(reg, group, llmpool.OfficialTierHigh)
	model, matched, _ := applyProxyWorkloadRouting(req, nil, reg, group, dispatch, llmpool.OfficialTierHigh)
	if model != llmpool.OfficialTierHigh {
		t.Fatalf("model = %q, want official-high", model)
	}
	if matched == nil || matched.ID != llmpool.OfficialGroupID {
		t.Fatalf("matched = %#v", matched)
	}
	if req.WorkloadClass != llmpool.WorkloadClassPlan {
		t.Fatalf("class = %q", req.WorkloadClass)
	}
}

func TestApplyProxyWorkloadRoutingResolvesAuto(t *testing.T) {
	group := officialDynamicFixtureGroup()
	reg := &Registry{ServiceGroups: []llmpool.ServiceGroup{group}}
	header := http.Header{}
	header.Set(llmpool.WorkloadClassHeader, "plan")
	req := &ProxyRequest{Header: header, Body: map[string]any{"model": "auto"}}
	dispatch := matchProxyGroupModel(reg, &group, "auto")
	model, _, _ := applyProxyWorkloadRouting(req, nil, reg, &reg.ServiceGroups[0], dispatch, "auto")
	if model != llmpool.OfficialTierHigh {
		t.Fatalf("model = %q, want official-high", model)
	}
}

func officialDynamicFixtureGroup() llmpool.ServiceGroup {
	return llmpool.ServiceGroup{
		ID:     llmpool.OfficialGroupID,
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmpool.ModelConfig{
			{Name: "auto", ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierHigh, ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierMid, ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
			{Name: llmpool.OfficialTierLow, ProviderIDs: []string{"p1"}, ProviderConfigs: []llmpool.ModelProviderConfig{{ProviderID: "p1"}}},
		},
	}
}
