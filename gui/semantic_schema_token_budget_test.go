package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func testSelectionSchemaTokenCost(selection tool.PlannedSelection) int {
	cost := 24 + 8*len(selection.ParameterAuthorization.AllowedFields)
	if strings.TrimSpace(selection.ParameterAuthorization.Digest) != "" {
		cost += 4
	}
	return cost
}

func TestIMSemanticHostSchemaTokenBudgetDefaultKeepsGenerateChain(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	if err := h.registry.Register(RegisteredTool{
		Name: "web_search", Status: RegToolAvailable, InputSchema: map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	profile := fullExecutionProfile("schema-token default")
	if profile.SchemaTokenBudget != 0 || profile.ToolBudget != 0 {
		t.Fatalf("default profile must leave both budgets unlimited: %+v", profile)
	}
	ctx := withSemanticSchemaTokenBudget(context.Background(), profile.SchemaTokenBudget)
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", "查询南京天气，并生成pdf报告", "desktop", "root-schema-default", "turn-schema-default",
		liveDataGenerateClassification(), nil,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("default schema-token budget must fully materialize, handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("default schema-token budget truncated generate chain: %#v", prepared.plan.Selections)
	}
	if len(prepared.plan.Unmet) != 0 {
		t.Fatalf("default schema-token budget must not report unmet: %#v", prepared.plan.Unmet)
	}
}

func TestIMSemanticHostSchemaTokenBudgetSmallReportsBudgetExceeded(t *testing.T) {
	h := registerDocumentGeneratePDF(t)
	if err := h.registry.Register(RegisteredTool{
		Name: "web_search", Status: RegToolAvailable, InputSchema: map[string]interface{}{},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	unlimited, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "查询南京天气，并生成pdf报告", "desktop", "root-schema-measure", "turn-schema-measure", liveDataGenerateClassification(),
	)
	if err != nil || !handled || unlimited == nil || len(unlimited.plan.Selections) < 2 {
		t.Fatalf("need a full generate chain to measure schema cost, handled=%v err=%v", handled, err)
	}
	searchCost := 0
	for _, selection := range unlimited.plan.Selections {
		if selection.FitProof.MatchedCapability == "information.search.web" {
			searchCost = testSelectionSchemaTokenCost(selection)
		}
	}
	if searchCost <= 0 {
		t.Fatal("could not measure search schema token cost")
	}
	ctx := withSemanticSchemaTokenBudget(context.Background(), searchCost+1)
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", "查询南京天气，并生成pdf报告", "desktop", "root-schema-small", "turn-schema-small",
		liveDataGenerateClassification(), nil,
	)
	if !handled || prepared == nil {
		t.Fatalf("budgeted plan must remain inspectable, handled=%v err=%v", handled, err)
	}
	if err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("schema-token cut must not be silent, err=%v", err)
	}
	if len(prepared.plan.Selections) != 1 || prepared.plan.Selections[0].FitProof.MatchedCapability != "information.search.web" {
		t.Fatalf("small schema-token budget must keep the search wave: %#v", prepared.plan.Selections)
	}
	if len(prepared.plan.Unmet) != 2 {
		t.Fatalf("generate and deliver must remain unmet, unmet=%#v", prepared.plan.Unmet)
	}
	for _, item := range prepared.plan.Unmet {
		if item.ReasonCode != "budget_exceeded" && item.ReasonCode != "planning_budget_exceeded" {
			t.Fatalf("unmet %s=%q, want a budget reason", item.NeedID, item.ReasonCode)
		}
	}
}

func TestSemanticRoutingContextCopiesSchemaTokenBudget(t *testing.T) {
	loop := &LoopContext{Runtime: RuntimeContext{Execution: ExecutionProfile{SchemaTokenBudget: 29, ToolBudget: 8}}}
	ctx, cancel := semanticRoutingContext(loop)
	defer cancel()
	if semanticSchemaTokenBudget(ctx) != 29 {
		t.Fatalf("SchemaTokenBudget was not copied, got %d", semanticSchemaTokenBudget(ctx))
	}
	if semanticPlanningBudget(ctx) != 8 {
		t.Fatalf("ToolBudget must stay independent, got %d", semanticPlanningBudget(ctx))
	}
	budget := semanticHostPlanningBudget(semanticPlanningBudget(ctx), semanticSchemaTokenBudget(ctx))
	if budget.MaxSelections != 8 || budget.MaxSchemaTokens != 29 {
		t.Fatalf("host budget=%+v", budget)
	}
}