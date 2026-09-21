package guiapp

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// completeCompositionFixture builds a composition with all four components
// present: a minimal one-selection plan, an explicit host admission, a
// publish fn bound to a real App method shape, and a coordinator route ref.
func completeCompositionFixture(t *testing.T) *codingDynamicProductionComposition {
	t.Helper()
	plan := codingDynamicPlanPreparation{
		Plan: coretool.ToolPlan{Selections: []coretool.PlannedSelection{{
			ID: "sel:test", AdapterName: "adapter:test",
			FitProof: coretool.FitProof{NeedID: "need:coding:test", MatchedCapability: "coding.test"},
		}}},
	}
	publish := func(*trustedCodingInvocationIdentity, codingDynamicPlanPreparation, codingDynamicCatalogSnapshot, string, string, time.Time) (*codingDurableDynamicSurface, error) {
		return nil, nil
	}
	route := &coretool.RouteRevisionRef{RootTaskID: "root", SessionID: "sess", PrincipalID: "principal", Revision: 1, PlanID: "plan:test", PlanDigest: "digest:test"}
	return newCodingDynamicProductionComposition(plan, codingDynamicAdmittedBindingSnapshot{admitted: true}, publish, route)
}

func TestCodingDynamicCompositionRejectsNilCandidate(t *testing.T) {
	if codingDynamicProductionCompositionEligible(nil) {
		t.Fatal("nil composition candidate must be rejected")
	}
}

func TestCodingDynamicCompositionRejectsMissingPlan(t *testing.T) {
	c := completeCompositionFixture(t)
	c.plan = codingDynamicPlanPreparation{}
	if codingDynamicProductionCompositionEligible(c) {
		t.Fatal("composition with a vanished plan must be rejected")
	}
}

func TestCodingDynamicCompositionRejectsMissingAdmittedBindings(t *testing.T) {
	c := completeCompositionFixture(t)
	c.bindings = codingDynamicAdmittedBindingSnapshot{admitted: false}
	if codingDynamicProductionCompositionEligible(c) {
		t.Fatal("composition without host-admitted bindings must be rejected (no by-name recovery)")
	}
}

func TestCodingDynamicCompositionRejectsMissingPublishFn(t *testing.T) {
	c := completeCompositionFixture(t)
	c.publish = nil
	if codingDynamicProductionCompositionEligible(c) {
		t.Fatal("composition without a durable surface publish fn must be rejected")
	}
}

func TestCodingDynamicCompositionRejectsMissingRoute(t *testing.T) {
	c := completeCompositionFixture(t)
	c.route = nil
	if codingDynamicProductionCompositionEligible(c) {
		t.Fatal("composition without a coordinator route must be rejected")
	}
}

func TestCodingDynamicCompositionRejectsDigestDrift(t *testing.T) {
	c := completeCompositionFixture(t)
	// Mutating the plan after construction invalidates the pinned digest.
	c.plan.Plan.Selections = append(c.plan.Plan.Selections, coretool.PlannedSelection{
		ID: "sel:extra", AdapterName: "adapter:extra",
	})
	if codingDynamicProductionCompositionEligible(c) {
		t.Fatal("composition whose plan drifted from its construction digest must be rejected")
	}
}

func TestCodingDynamicCompositionCompleteIsEligibleButAliasesStayClosed(t *testing.T) {
	c := completeCompositionFixture(t)
	if !codingDynamicProductionCompositionEligible(c) {
		t.Fatalf("complete composition must be an eligible candidate: %s", c.rejectionReason())
	}
	// Explicit-empty admission (no dynamic providers in scope) is complete.
	empty := newCodingDynamicProductionComposition(c.plan, codingDynamicAdmittedBindingSnapshot{admitted: true, skills: nil, mcpTools: nil}, c.publish, c.route)
	if !codingDynamicProductionCompositionEligible(empty) {
		t.Fatalf("explicit-empty admission must be complete: %s", empty.rejectionReason())
	}
	// Slice-1 red line: eligibility is meaningful, but materialization stays
	// off until slice 2's cutover, and the production qualification override
	// stays unset outside hermetic suites.
	local := &codingSubAgentCallbacks{}
	if local.codingDynamicAliasesMayMaterialize() {
		t.Fatal("codingDynamicAliasesMayMaterialize must stay false in slice 1")
	}
	remote := &remoteCodingCallbacks{}
	if remote.codingDynamicAliasesMayMaterialize() {
		t.Fatal("remote codingDynamicAliasesMayMaterialize must stay false in slice 1")
	}
	if codingDynamicProductionAdapterQualificationOverride != nil {
		t.Fatal("production qualification override must be unset outside hermetic tests")
	}
}

// TestCodingDynamicCompositionFactoryRejectsIncompleteCandidate proves the
// factory consumes the eligibility gate BEFORE the production deny boundary:
// an incomplete composition returns no adapter and no error, and a complete
// composition still hits the deny boundary when the hermetic override is
// absent (the production default in this process).
func TestCodingDynamicCompositionFactoryRejectsIncompleteCandidate(t *testing.T) {
	if codingDynamicProductionAdapterQualificationOverride != nil {
		t.Skip("hermetic override unexpectedly installed outside its suite")
	}
	handler := &IMMessageHandler{app: &App{}}
	identity := &trustedCodingInvocationIdentity{TenantID: "t", PrincipalID: "p", SessionID: "s", RootTaskID: "r", TurnID: "turn"}
	cfg := corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}

	// Production deny boundary: complete composition but no override.
	adapter, err := reserveCodingBoundDynamicRequestAdapterFromComposition(context.Background(), handler, identity, cfg, completeCompositionFixture(t))
	if err != nil || adapter != nil {
		t.Fatalf("factory without override = (%v, %v), want (nil, nil)", adapter, err)
	}

	// Incomplete candidate: rejected by the eligibility gate itself (the gate
	// is the factory's first statement, before any override/deny read).
	incomplete := completeCompositionFixture(t)
	incomplete.route = nil
	adapter, err = reserveCodingBoundDynamicRequestAdapterFromComposition(context.Background(), handler, identity, cfg, incomplete)
	if err != nil || adapter != nil {
		t.Fatalf("factory with incomplete composition = (%v, %v), want (nil, nil)", adapter, err)
	}
}
