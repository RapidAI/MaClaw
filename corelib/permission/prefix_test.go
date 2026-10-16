package permission

import "testing"

func TestArgsPredicatePrefix(t *testing.T) {
	p := ArgsPredicate{Field: "path", Prefix: "/workspace/project/"}
	if !p.matches(map[string]interface{}{"path": "/workspace/project/src/main.go"}) {
		t.Fatalf("prefix match failed")
	}
	if p.matches(map[string]interface{}{"path": "/workspace/other/main.go"}) {
		t.Fatalf("prefix must not match outside the prefix")
	}
	if p.matches(map[string]interface{}{"path": "/workspace/project"}) {
		t.Fatalf("bare directory without trailing slash is outside the prefix")
	}
	if p.matches(nil) {
		t.Fatalf("nil args never match")
	}
}

func TestRuleWithPrefixPredicateParticipatesInDecide(t *testing.T) {
	rules := []Rule{
		{Tool: "read_file", Effect: EffectDeny, When: &ArgsPredicate{Field: "path", Prefix: "/etc/"}, Source: "test"},
	}
	snap := mustLoad(t, rules)
	if d := snap.Decide("read_file", KindRead, map[string]interface{}{"path": "/etc/passwd"}); d.Effect != EffectDeny {
		t.Fatalf("in-prefix: %+v", d)
	}
	if d := snap.Decide("read_file", KindRead, map[string]interface{}{"path": "/home/u/f.txt"}); !d.Default {
		t.Fatalf("out-of-prefix must default: %+v", d)
	}
}

// TestArgsPredicatePrefixRawBoundarySemantics pins the deliberately raw
// semantics of Prefix: it is strings.HasPrefix with no path-segment
// boundary, so "/safe" matches "/safe-evil/x". The documented authoring
// rule is to include the trailing separator ("/safe/"); the engine must not
// silently rewrite or reject boundary-less prefixes.
func TestArgsPredicatePrefixRawBoundarySemantics(t *testing.T) {
	p := ArgsPredicate{Field: "path", Prefix: "/safe"}
	if !p.matches(map[string]interface{}{"path": "/safe-evil/x"}) {
		t.Fatal("raw semantics: \"/safe\" must match \"/safe-evil/x\"")
	}
	if !p.matches(map[string]interface{}{"path": "/safe/ok"}) {
		t.Fatal("prefix must match inside the prefix")
	}
	if p.matches(map[string]interface{}{"path": "/other/safe"}) {
		t.Fatal("prefix must not match when not a prefix")
	}

	// With the trailing separator the boundary is expressed in the rule
	// text, as the doc comment directs authors.
	boundary := ArgsPredicate{Field: "path", Prefix: "/safe/"}
	if boundary.matches(map[string]interface{}{"path": "/safe-evil/x"}) {
		t.Fatal("\"/safe/\" must NOT match \"/safe-evil/x\"")
	}
	if !boundary.matches(map[string]interface{}{"path": "/safe/ok"}) {
		t.Fatal("\"/safe/\" must match \"/safe/ok\"")
	}
}

// TestArgsPredicateSkipsNonScalarValues: rendering a slice or map through
// %v produces artifacts like "[a b]" that could spuriously satisfy a Prefix
// matcher; non-scalar values must never match.
func TestArgsPredicateSkipsNonScalarValues(t *testing.T) {
	p := ArgsPredicate{Field: "path", Prefix: "["}
	if p.matches(map[string]interface{}{"path": []any{"a"}}) {
		t.Fatal("slice value must not match Prefix \"[\"")
	}
	if p.matches(map[string]interface{}{"path": []string{"/etc/passwd"}}) {
		t.Fatal("string slice must not match Prefix \"[\"")
	}
	if p.matches(map[string]interface{}{"path": map[string]interface{}{"a": 1}}) {
		t.Fatal("map value must not match Prefix \"[\"")
	}
	// Scalar behavior is unchanged for the other matchers.
	eq := ArgsPredicate{Field: "op", Equals: "1"}
	if !eq.matches(map[string]interface{}{"op": float64(1)}) {
		t.Fatal("float64 scalar must keep matching Equals")
	}
	if !eq.matches(map[string]interface{}{"op": "1"}) {
		t.Fatal("string scalar must keep matching Equals")
	}
	num := ArgsPredicate{Field: "op", In: []string{"true"}}
	if !num.matches(map[string]interface{}{"op": true}) {
		t.Fatal("bool scalar must keep matching In")
	}
}
