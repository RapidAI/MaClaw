package permission

import "testing"

func TestWithAdditionalRulesAppendsAndKeepsReceiverImmutable(t *testing.T) {
	base := mustLoad(t, nil)
	extended := base.WithAdditionalRules([]Rule{
		{Tool: "bash", Effect: EffectAllow, Subject: "expert-a", Source: "expert-definitions"},
	})

	// Receiver unchanged: its own Decide still reports Default and knows
	// nothing about the appended subject rule.
	if d := base.DecideFor("expert-a", "bash", KindExecute, nil); !d.Default {
		t.Fatalf("receiver must be immutable, got %+v", d)
	}
	// New snapshot: the appended allow participates for the subject and keeps
	// its provenance...
	if d := extended.DecideFor("expert-a", "bash", KindExecute, nil); d.Default || d.Effect != EffectAllow {
		t.Fatalf("appended allow should decide for subject, got %+v", d)
	}
	if d := extended.DecideFor("expert-a", "bash", KindExecute, nil); d.Rule.Source != "expert-definitions" {
		t.Fatalf("appended rule must keep its provenance, got %+v", d.Rule)
	}
	// ...while other subjects are untouched (subject-scoped rule does not match).
	if d := extended.DecideFor("expert-b", "bash", KindExecute, nil); !d.Default {
		t.Fatalf("unmatched subject should default, got %+v", d)
	}
}

func TestWithAdditionalRulesLoadedDenyBeatsAppendedAllow(t *testing.T) {
	base := mustLoad(t, []Rule{{Tool: "web_search", Effect: EffectDeny}})
	extended := base.WithAdditionalRules([]Rule{
		{Tool: "web_search", Effect: EffectAllow, Subject: "expert-a"},
	})
	if d := extended.DecideFor("expert-a", "web_search", KindRead, nil); d.Default || d.Effect != EffectDeny {
		t.Fatalf("loaded deny must outrank appended subject allow: %+v", d)
	}
	// Other subjects fall through to the loaded global rule (the appended rule
	// is subject-scoped and does not match them).
	if d := extended.DecideFor("expert-b", "web_search", KindRead, nil); d.Default || d.Effect != EffectDeny {
		t.Fatalf("unmatched subject should hit the global deny, got %+v", d)
	}
}

func TestWithAdditionalRulesInvalidBatchDropped(t *testing.T) {
	base := mustLoad(t, []Rule{{Tool: "bash", Effect: EffectDeny}})
	extended := base.WithAdditionalRules([]Rule{
		{Tool: "read_file", Effect: EffectAllow, Subject: "expert-a"},
		{Tool: "", Effect: EffectAllow}, // one bad apple drops the whole batch
	})
	if extended != base {
		t.Fatal("invalid batch must return the receiver unchanged")
	}
	if d := extended.DecideFor("expert-a", "read_file", KindRead, nil); !d.Default {
		t.Fatalf("dropped batch must not participate, got %+v", d)
	}
	// Invalid effect is rejected too.
	if again := base.WithAdditionalRules([]Rule{{Tool: "read_file", Effect: "maybe"}}); again != base {
		t.Fatal("invalid effect must return the receiver unchanged")
	}
}

func TestWithAdditionalRulesFillsSourceAndEmptyBatch(t *testing.T) {
	base := mustLoad(t, nil)
	extended := base.WithAdditionalRules([]Rule{{Tool: "bash", Effect: EffectAllow, Subject: "expert-a"}})
	if d := extended.DecideFor("expert-a", "bash", KindExecute, nil); d.Rule.Source != sourceAdditional {
		t.Fatalf("empty Source must become %q, got %+v", sourceAdditional, d.Rule)
	}
	if again := extended.WithAdditionalRules(nil); again != extended {
		t.Fatal("empty batch must return the receiver unchanged")
	}
}

// TestWithAdditionalRulesRejectsDeadPredicate: a batch containing a When
// predicate with no matchers is dropped whole, exactly like the other
// invalid-rule cases (receiver returned unchanged).
func TestWithAdditionalRulesRejectsDeadPredicate(t *testing.T) {
	base := mustLoad(t, nil)
	extended := base.WithAdditionalRules([]Rule{
		{Tool: "bash", Effect: EffectAllow},
		{Tool: "read_file", Effect: EffectDeny, When: &ArgsPredicate{Field: "path"}},
	})
	if extended != base {
		t.Fatal("batch with a dead-predicate rule must be dropped whole (receiver returned)")
	}
	if d := extended.Decide("bash", KindExecute, nil); !d.Default {
		t.Fatalf("dropped batch must leave no rules behind: %+v", d)
	}
}
