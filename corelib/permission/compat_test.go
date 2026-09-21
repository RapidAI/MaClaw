package permission

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/database"
)

// Consistency contract: a rule built from DatabaseWritePredicate must agree
// with the legacy database.IsWriteAction gate for every action in (and out
// of) the list — this is the anti-drift pin between the two truth sources.
func TestDatabaseWritePredicateMatchesIsWriteAction(t *testing.T) {
	p := DatabaseWritePredicate()
	if p == nil || p.Field != "action" {
		t.Fatalf("predicate: %+v", p)
	}
	cases := append(database.WriteActions(),
		"query", "inspect", "", "execute ", "EXECUTE", "nonsense")
	for _, action := range cases {
		args := map[string]interface{}{"action": action}
		got := p.matches(args)
		want := database.IsWriteAction(action)
		if got != want {
			t.Fatalf("action %q: predicate=%v IsWriteAction=%v", action, got, want)
		}
	}
	// nil args never matches (IsWriteAction("") is false, consistent).
	if p.matches(nil) {
		t.Fatalf("nil args must not match")
	}
}

func TestDatabaseWritePredicateRuleParticipatesInDecide(t *testing.T) {
	snap := mustLoad(t, []Rule{
		{Tool: "database", Effect: EffectDeny, When: DatabaseWritePredicate(), Source: "compat"},
	})
	if d := snap.Decide("database", KindMutate, map[string]interface{}{"action": "export_excel"}); d.Effect != EffectDeny {
		t.Fatalf("write action must deny: %+v", d)
	}
	if d := snap.Decide("database", KindMutate, map[string]interface{}{"action": "query"}); !d.Default {
		t.Fatalf("read action must default: %+v", d)
	}
}
