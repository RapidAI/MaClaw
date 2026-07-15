package httpapi

import (
	"strings"
	"testing"
)

func TestParseLLMPlanDraftStripsFence(t *testing.T) {
	raw := "```json\n{\"intent\":\"system_free.test\",\"summary\":\"test\",\"risk_level\":\"low\",\"steps\":[{\"step_id\":\"s1\",\"tool\":\"system_free.test\",\"mode\":\"probe\"}]}\n```"
	draft, err := parseLLMPlanDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Intent != "system_free.test" {
		t.Fatalf("intent = %q", draft.Intent)
	}
}

func TestValidateAndNormalizeLLMPlanRejectsUnknownTool(t *testing.T) {
	_, err := validateAndNormalizeLLMPlan(&llmPlanDraft{
		Intent: "x",
		Steps:  []configAgentStep{{Tool: "shell.run", Mode: "write"}},
	})
	if err == nil || !strings.Contains(err.Error(), "disallowed") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateAndNormalizeLLMPlanOK(t *testing.T) {
	plan, err := validateAndNormalizeLLMPlan(&llmPlanDraft{
		Intent:    "llm.provider.upsert",
		Summary:   "add provider",
		RiskLevel: "medium",
		Steps: []configAgentStep{
			{Tool: "llm.providers.get", Mode: "read"},
			{Tool: "llm.providers.upsert", Mode: "write", Args: map[string]any{"id": "a", "api_url": "https://x", "api_key": "k", "model": "m"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Planner != "llm" {
		t.Fatalf("planner = %q", plan.Planner)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %d", len(plan.Steps))
	}
	if plan.Steps[0].APIPreview["path"] != "/api/admin/llm/providers" {
		t.Fatalf("api preview = %#v", plan.Steps[0].APIPreview)
	}
}

func TestValidateAndNormalizeLLMPlanInfersMode(t *testing.T) {
	plan, err := validateAndNormalizeLLMPlan(&llmPlanDraft{
		Intent: "system_free.get",
		Steps:  []configAgentStep{{Tool: "system_free.get"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Steps[0].Mode != "read" {
		t.Fatalf("mode = %q", plan.Steps[0].Mode)
	}
}
