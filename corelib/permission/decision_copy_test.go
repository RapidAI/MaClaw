package permission

import "testing"

// TestDecideReturnsDeepCopy verifies that mutating the returned Decision's
// Rule cannot leak back into the snapshot: Kind and When (including the
// In/InFold slices) must be deep-copied, so a second Decide still sees the
// original rule values.
func TestDecideReturnsDeepCopy(t *testing.T) {
	rules := []Rule{
		{
			Tool:   "database",
			Effect: EffectDeny,
			Kind:   accessKindPtr(KindMutate),
			When:   &ArgsPredicate{Field: "action", In: []string{"drop"}, InFold: []string{"DROP"}},
			Source: "test",
		},
	}
	snap := mustLoad(t, rules)
	args := map[string]interface{}{"action": "drop"}

	first := snap.Decide("database", KindMutate, args)
	if first.Default || first.Effect != EffectDeny || first.Rule == nil {
		t.Fatalf("first decide: %+v", first)
	}

	// Mutate every reachable field through the returned decision.
	*first.Rule.Kind = KindExecute
	first.Rule.When.In[0] = "select"
	first.Rule.When.InFold[0] = "SELECT"
	first.Rule.Tool = "other_tool"
	first.Rule.Effect = EffectAllow

	second := snap.Decide("database", KindMutate, args)
	if second.Default || second.Effect != EffectDeny {
		t.Fatalf("snapshot mutated through decision: %+v", second)
	}
	if second.Rule.Tool != "database" || *second.Rule.Kind != KindMutate ||
		second.Rule.When.In[0] != "drop" || second.Rule.When.InFold[0] != "DROP" {
		t.Fatalf("rule fields not restored: %+v", second.Rule)
	}
	// The mutation must also not have changed evaluation semantics: the
	// original predicate still matches the original args...
	if !snap.rules[0].When.matches(args) {
		t.Fatal("predicate no longer matches original args after decision mutation")
	}
	// ...and a caller that keeps the first (mutated) decision sees its own
	// mutation locally — it is a copy, not a view.
	if first.Rule.When.In[0] != "select" {
		t.Fatalf("first decision lost its own mutation: %+v", first.Rule.When)
	}
}

func accessKindPtr(k AccessKind) *AccessKind { return &k }
