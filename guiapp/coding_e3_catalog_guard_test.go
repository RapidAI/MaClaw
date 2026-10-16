package guiapp

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestCodingE3HermeticCatalogSatisfiesProductionNeeds proves the seeded
// loopback inventory fully satisfies the production coding capability needs;
// the matrix scenarios rely on this completeness.
func TestCodingE3HermeticCatalogSatisfiesProductionNeeds(t *testing.T) {
	env := newCodingE3HermeticEnv(t)
	identity := codingE3Identity()
	dynamic, err := env.handler.codingDynamicCatalogForIdentity(context.Background(), identity)
	if err != nil {
		t.Fatalf("catalog err: %v", err)
	}
	t.Logf("coverage=%+v providers=%d", dynamic.Coverage, len(dynamic.Catalog.Providers))
	for _, p := range dynamic.Catalog.Providers {
		t.Logf("provider=%#v", p)
	}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, codingDynamicCapabilityNeeds(), nil, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("plan err: %v", err)
	}
	if len(prepared.Plan.Selections) != 30 || len(prepared.Plan.Unmet) != 0 {
		t.Fatalf("production needs must plan completely on the hermetic catalog: selections=%d unmet=%#v", len(prepared.Plan.Selections), prepared.Plan.Unmet)
	}
}
