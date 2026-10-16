package permission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDecideForSubjectScoping(t *testing.T) {
	rules := []Rule{
		{Tool: "bash", Effect: EffectAllow, Subject: "expert-a"},
		{Tool: "bash", Effect: EffectDeny},
	}
	snap := mustLoad(t, rules)

	// Global Decide sees only the global rule.
	if d := snap.Decide("bash", KindExecute, nil); d.Default || d.Effect != EffectDeny {
		t.Fatalf("global decide: %+v", d)
	}
	// Subject-scoped call: the expert's allow is more specific than the global deny? No —
	// effect priority wins: deny outranks allow regardless of specificity.
	if d := snap.DecideFor("expert-a", "bash", KindExecute, nil); d.Effect != EffectDeny {
		t.Fatalf("deny must outrank allow: %+v", d)
	}
	// A subject with no scoped rule falls to the global rule.
	if d := snap.DecideFor("expert-b", "bash", KindExecute, nil); d.Effect != EffectDeny {
		t.Fatalf("fallback: %+v", d)
	}
}

func TestDecideForSameEffectSubjectBeatsGlobal(t *testing.T) {
	rules := []Rule{
		{Tool: "ssh", Effect: EffectAllow, Subject: "expert-a"},
		{Tool: "ssh", Effect: EffectAllow},
		{Tool: "ssh", Effect: EffectDeny, Subject: "expert-b"},
	}
	snap := mustLoad(t, rules)

	if d := snap.DecideFor("expert-a", "ssh", KindExecute, nil); d.Default || d.Effect != EffectAllow || d.Rule.Subject != "expert-a" {
		t.Fatalf("subject allow should beat global allow: %+v", d)
	}
	if d := snap.DecideFor("expert-b", "ssh", KindExecute, nil); d.Effect != EffectDeny {
		t.Fatalf("subject deny: %+v", d)
	}
	if d := snap.DecideFor("expert-c", "ssh", KindExecute, nil); d.Effect != EffectAllow || ruleHasSubject(d.Rule) {
		t.Fatalf("global allow for unmatched subject: %+v", d)
	}
}

func TestDecideForWildcardSubjectMatchesAny(t *testing.T) {
	rules := []Rule{
		{Tool: "memory", Effect: EffectAsk, Subject: "*"},
	}
	snap := mustLoad(t, rules)
	if d := snap.DecideFor("anyone", "memory", KindMutate, nil); d.Effect != EffectAsk {
		t.Fatalf("wildcard subject: %+v", d)
	}
	if d := snap.Decide("memory", KindMutate, nil); d.Effect != EffectAsk {
		t.Fatalf("global decide sees wildcard-subject rule: %+v", d)
	}
}

func TestYoloPinKeepsSubjectScopedAllows(t *testing.T) {
	rules := []Rule{
		{Tool: "*", Effect: EffectAllow},
		{Tool: "web_search", Effect: EffectAllow, Subject: "expert-a"},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	doc := struct {
		Permission struct {
			Rules []Rule `json:"rules"`
		} `json:"permission"`
	}{}
	doc.Permission.Rules = rules
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	snap, err := Load(Options{ManagedYoloPin: true, UserConfigPath: path})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if d := snap.Decide("web_search", KindRead, nil); !d.Default {
		t.Fatalf("global catch-all allow must be pinned, got %+v", d)
	}
	if d := snap.DecideFor("expert-a", "web_search", KindRead, nil); d.Default || d.Effect != EffectAllow {
		t.Fatalf("subject-scoped allow must survive the pin: %+v", d)
	}
}

func mustLoad(t *testing.T, rules []Rule) *Snapshot {
	t.Helper()
	snap, err := Load(Options{ManagedRules: rules})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return snap
}
