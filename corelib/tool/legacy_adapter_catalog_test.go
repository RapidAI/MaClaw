package tool

import (
	"testing"
	"time"
)

func TestLegacyCandidateCatalogHasReviewedProvisionForEveryName(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	for name := range LegacyCandidateToolNames {
		provision, ok := LegacyAdapterProvisionForTool(name, now)
		if !ok {
			t.Fatalf("legacy candidate %q has no live reviewed provision", name)
		}
		if provision.Owner == "" || provision.AdapterContract == "" || provision.Capability == "" || provision.DeleteAfter.IsZero() || len(provision.Effects) == 0 {
			t.Fatalf("legacy provision is incomplete: %+v", provision)
		}
	}
}

func TestLegacyAdapterProvisionFailsClosedForUnknownOrExpiredName(t *testing.T) {
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, ok := LegacyAdapterProvisionForTool("not_a_real_tool", now); ok {
		t.Fatal("unknown tool received a legacy adapter provision")
	}
	if _, ok := LegacyAdapterProvisionForTool("bash", now); ok {
		t.Fatal("expired legacy adapter provision remained usable")
	}
	if !LegacyAdapterCatalogIncomplete("bash", now) {
		t.Fatal("expired legacy candidate must become catalog_incomplete")
	}
}

func TestLegacyAdapterProvisionCopiesAreImmutable(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	first, ok := LegacyAdapterProvisionForTool("bash", now)
	if !ok {
		t.Fatal("bash provision missing")
	}
	first.Effects[0] = EffectReadOnly
	second, ok := LegacyAdapterProvisionForTool("bash", now)
	if !ok || second.Effects[0] != EffectLocalMutation {
		t.Fatalf("provision mutation leaked: %+v", second)
	}
}

func TestAmbientAndTimeHostToolsHaveReviewedLegacyProvisions(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	for _, name := range []string{"knowledge_search", "current_datetime"} {
		provision, ok := LegacyAdapterProvisionForTool(name, now)
		if !ok {
			t.Fatalf("%s provision missing", name)
		}
		if provision.Capability == "" || provision.Owner == "" || provision.AdapterContract == "" || len(provision.Effects) == 0 {
			t.Fatalf("%s provision is incomplete: %+v", name, provision)
		}
	}
}

func TestRouterRecordsOnlyReviewedCapabilityRecommendations(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	tools := []map[string]interface{}{
		makeToolDef("task", "task control"),
		makeToolDef("async_wait", "wait for task"),
		makeToolDef("compress_context", "compact context"),
		makeToolDef("read_file", "read local file"),
		makeToolDef("unreviewed_tool", "unreviewed but textually relevant file reader"),
	}
	router.Route("read a local file", tools)
	recommendation := router.LastRoutingRecommendation()
	if len(recommendation.Evidence) == 0 || recommendation.SearchQuery != "read a local file" {
		t.Fatalf("missing routing recommendation: %+v", recommendation)
	}
	foundRead := false
	for _, evidence := range recommendation.Evidence {
		if evidence.ToolName == "unreviewed_tool" {
			t.Fatalf("unreviewed tool leaked into capability recommendation: %+v", recommendation)
		}
		if evidence.ToolName == "read_file" {
			foundRead = evidence.Capability == CapabilityID("workspace.file.read") && evidence.AdapterContract != ""
		}
	}
	if !foundRead {
		t.Fatalf("reviewed read_file capability evidence missing: %+v", recommendation)
	}
}

func TestRouterDoesNotSelectLegacyDynamicGateways(t *testing.T) {
	router := NewRouter(NewDefinitionGenerator(nil, nil))
	tools := []map[string]interface{}{
		makeToolDef("manage_skill", "run an installed skill"),
		makeToolDef("call_mcp_tool", "call a remote MCP tool"),
		makeToolDef("read_file", "read a local file"),
	}
	routed := router.Route("run the installed skill", tools)
	for _, definition := range routed {
		if IsLegacyModelDynamicGateway(ExtractToolName(definition)) {
			t.Fatalf("router selected dynamic gateway: %#v", routed)
		}
	}
	for _, evidence := range router.LastRoutingRecommendation().Evidence {
		if IsLegacyModelDynamicGateway(evidence.ToolName) {
			t.Fatalf("routing recommendation mentioned dynamic gateway: %#v", router.LastRoutingRecommendation())
		}
	}
}
