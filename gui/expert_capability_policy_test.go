package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestExpertCapabilityRulesOldCardDoesNotChangeGenerateSurface(t *testing.T) {
	swapExpertStoreForTest(t)
	const expertID = "expert-tools-only"
	if err := defaultExpertStore.Save(ExpertDefinition{
		ID: expertID, Name: "Legacy card", Tools: []string{"bash", "office", "generate_pdf"},
	}); err != nil {
		t.Fatal(err)
	}
	userID := expertSessionUserID(expertID)
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		userID, "生成pdf报告", "desktop", "root-expert-legacy", "turn-expert-legacy", documentGenerateClassification(),
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("legacy expert card must keep the managed generate surface, handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("legacy Tools allow-list changed generate plan: %#v", prepared.plan.Selections)
	}
	constraints, err := expertCapabilityPolicyConstraints(expertDefForUserID(userID))
	if err != nil || len(constraints) != 0 {
		t.Fatalf("Tools must not project capability constraints: %#v err=%v", constraints, err)
	}
}

func TestExpertCapabilityRulesDenyGenerateDoesNotMaterialize(t *testing.T) {
	swapExpertStoreForTest(t)
	const expertID = "expert-deny-generate"
	if err := defaultExpertStore.Save(ExpertDefinition{
		ID: expertID, Name: "No generate",
		CapabilityRules: []ExpertCapabilityRule{{
			Capability: "document.generate.file", Effect: "deny",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	userID := expertSessionUserID(expertID)
	h := registerDocumentGeneratePDF(t)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		userID, "生成pdf报告", "desktop", "root-expert-deny", "turn-expert-deny", documentGenerateClassification(),
	)
	if !handled {
		t.Fatal("denied generate must stay on the managed path")
	}
	if err == nil || !strings.Contains(err.Error(), "policy_denied") {
		t.Fatalf("want policy_denied, handled=%v err=%v", handled, err)
	}
	if prepared != nil {
		for _, selection := range prepared.plan.Selections {
			if selection.FitProof.MatchedCapability == "document.generate.file" {
				t.Fatalf("denied generate materialized: %#v", prepared.plan.Selections)
			}
		}
	}
}

func TestExpertToolNameAllowListDoesNotInventCapabilityDeny(t *testing.T) {
	swapExpertStoreForTest(t)
	def := &ExpertDefinition{ID: "bash-only", Tools: []string{"bash"}}
	constraints, err := expertCapabilityPolicyConstraints(def)
	if err != nil || len(constraints) != 0 {
		t.Fatalf("Tools=[bash] must not become a capability deny: %#v err=%v", constraints, err)
	}
	if err := defaultExpertStore.Save(ExpertDefinition{ID: "bash-only", Name: "Bash", Tools: []string{"bash"}}); err != nil {
		t.Fatal(err)
	}
	loaded := expertDefForUserID(expertSessionUserID("bash-only"))
	constraints, err = expertCapabilityPolicyConstraints(loaded)
	if err != nil || len(constraints) != 0 || loaded == nil || len(loaded.CapabilityRules) != 0 {
		t.Fatalf("saved Tools=[bash] invented capability rules: def=%#v constraints=%#v err=%v", loaded, constraints, err)
	}
}

func TestExpertUnknownCapabilityRuleCannotPublishOrLoad(t *testing.T) {
	swapExpertStoreForTest(t)
	err := defaultExpertStore.Save(ExpertDefinition{
		ID: "expert-unknown-cap", Name: "Bad",
		CapabilityRules: []ExpertCapabilityRule{{Capability: "not.a.real.capability", Effect: "deny"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown capability") {
		t.Fatalf("unknown capability must not publish: %v", err)
	}
	if err := validateExpertCapabilityRules([]ExpertCapabilityRule{{
		Capability: "document.generate.file", Qualifiers: map[string]string{"format": "docx"}, Effect: "deny",
	}}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unknown qualifier value must not load: %v", err)
	}
	if err := validateExpertCapabilityRules([]ExpertCapabilityRule{{
		Capability: "document.generate.file", Qualifiers: map[string]string{"layout": "wide"}, Effect: "deny",
	}}); err == nil || !strings.Contains(err.Error(), "does not declare qualifier") {
		t.Fatalf("unknown qualifier key must not load: %v", err)
	}
	if err := validateExpertCapabilityRules([]ExpertCapabilityRule{{
		Capability: "document.generate.file", Effect: "allow",
	}}); err == nil {
		t.Fatal("effect allow must not load")
	}
	def := &ExpertDefinition{ID: "injected", CapabilityRules: []ExpertCapabilityRule{{Capability: "mystery.capability", Effect: "deny"}}}
	if _, err := expertCapabilityPolicyConstraints(def); err == nil {
		t.Fatal("invalid loaded rules must fail closed")
	}

	path := defaultExpertStore.path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `{"experts":[{"id":"hand-edited","name":"Bad","capability_rules":[{"capability":"not.a.real.capability","effect":"deny"}]}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := defaultExpertStore.List(); err == nil || !strings.Contains(err.Error(), "unknown capability") {
		t.Fatalf("unknown capability must not load: %v", err)
	}
	h := registerDocumentGeneratePDF(t)
	_, handled, planErr := h.semanticPlanForTurnWithClassification(
		expertSessionUserID("hand-edited"), "生成pdf报告", "desktop", "root-expert-bad-load", "turn-expert-bad-load", documentGenerateClassification(),
	)
	if !handled || planErr == nil || !strings.Contains(planErr.Error(), "unknown capability") {
		t.Fatalf("invalid expert store must fail closed on managed generate, handled=%v err=%v", handled, planErr)
	}
}

func TestExpertCapabilityRulesRequireConfirmationIsPolicyOnly(t *testing.T) {
	constraints, err := expertCapabilityPolicyConstraints(&ExpertDefinition{
		ID:              "confirm-generate",
		CapabilityRules: []ExpertCapabilityRule{{Capability: "document.generate.file", Effect: "require_confirmation"}},
	})
	if err != nil || len(constraints) != 1 || constraints[0].Effect != "require_confirmation" || constraints[0].Authority != tool.AuthorityPolicy {
		t.Fatalf("confirmation rule=%#v err=%v", constraints, err)
	}
}

func TestSaveExpertOmittingCapabilityRulesPreservesExisting(t *testing.T) {
	swapExpertStoreForTest(t)
	app := &App{}
	first, err := app.SaveExpert(`{"name":"Policy expert","system_prompt":"p","capability_rules":[{"capability":"document.generate.file","effect":"deny"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	var saved ExpertDefinition
	if err := json.Unmarshal([]byte(first), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.CapabilityRules) != 1 || saved.CapabilityRules[0].Effect != "deny" {
		t.Fatalf("first save dropped rules: %#v", saved.CapabilityRules)
	}
	second, err := app.SaveExpert(`{"id":"` + saved.ID + `","name":"Policy expert","system_prompt":"updated"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(second), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.CapabilityRules) != 1 || saved.CapabilityRules[0].Capability != "document.generate.file" {
		t.Fatalf("editor-style save wiped capability rules: %#v", saved.CapabilityRules)
	}
}

func TestExportableExpertDefinitionKeepsCapabilityRules(t *testing.T) {
	def := testExpert("expert-policy", "2026-08-01T00:00:00Z")
	def.CapabilityRules = []ExpertCapabilityRule{{Capability: "document.generate.file", Effect: "deny"}}
	got := exportableExpertDefinition(def)
	if len(got.CapabilityRules) != 1 || got.CapabilityRules[0].Capability != "document.generate.file" || got.CapabilityRules[0].Effect != "deny" {
		t.Fatalf("portable expert must keep capability rules: %#v", got.CapabilityRules)
	}
}
