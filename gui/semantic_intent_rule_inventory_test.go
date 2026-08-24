package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// unmigratedSemanticIntentLabels lists capability labels that deliberately have
// no rule in imSemanticIntentRuleSet.
//
// This is not a list of things that quietly fall back. A capability label with
// no rule makes semanticPlanForTurn return an error, so the turn HostRejects
// instead of reaching the legacy name router. Every entry is therefore a
// family that the IM shared-turn semantic surface cannot serve, and the list as
// a whole is what the migration still owes.
//
// The coding family used to sit here and no longer does: coding, bug_fix, and
// maintenance now share semanticCodingCapabilityRule.
var unmigratedSemanticIntentLabels = map[intent.IntentLabel]string{
	intent.LabelWorkflowTask: "multi-turn workflow loop owns the route; " +
		"a single-turn capability plan is the wrong unit for it",
}

// semanticSyntheticUnmappedLabel is a capability label that exists only for
// tests and can never acquire a rule.
//
// The mixed fail-closed assertions need "a capability label with no rule";
// which one is irrelevant to what they check. Naming a real family for that
// job is what makes them rot. Coding played the part until it gained a rule,
// at which point every such assertion would have started passing because the
// mix was fully managed rather than because it failed closed — and a vacuous
// fail-closed test still passes, so nothing would have reported it.
//
// Reading the fixture from the unmigrated inventory fixed the naming but not
// the dependency. The inventory holds exactly one family now, so migrating
// workflow_task would have skipped this entire suite rather than failed it:
// the coverage would leave quietly, which is the same failure mode in a
// politer form.
//
// A synthetic label removes the dependency. It sits outside
// intent.AllLabels(), so the inventory test never sees it as an unaccounted
// family, and it is not an intent.IsNonCapabilityLabel(), so routing has to
// treat it as a capability family it cannot serve.
const semanticSyntheticUnmappedLabel = intent.IntentLabel("synthetic_unmigrated_capability")

// semanticUnmigratedFixtureLabel returns a capability label guaranteed to have
// no rule, for the tests that assert a managed family mixed with an unmapped
// one fails closed rather than running the migrated subset.
func semanticUnmigratedFixtureLabel(t *testing.T) intent.IntentLabel {
	t.Helper()
	return semanticSyntheticUnmappedLabel
}

// semanticRealUnmigratedLabel returns a genuinely unmigrated family, or skips.
// Only the bridge test below needs one; everything else uses the synthetic.
func semanticRealUnmigratedLabel(t *testing.T) intent.IntentLabel {
	t.Helper()
	candidates := make([]string, 0, len(unmigratedSemanticIntentLabels))
	for label := range unmigratedSemanticIntentLabels {
		if len(imSemanticIntentRuleSet[label]) == 0 {
			candidates = append(candidates, string(label))
		}
	}
	if len(candidates) == 0 {
		t.Skip("every capability family is migrated; there is no real unmapped label left to compare against")
	}
	sort.Strings(candidates)
	return intent.IntentLabel(candidates[0])
}

// The synthetic fixture is only sound while these hold. If the taxonomy ever
// starts validating labels, or the synthetic name is added to the taxonomy,
// the suite would be testing a path production cannot reach.
func TestTheSyntheticUnmappedFixtureIsStillUnmappable(t *testing.T) {
	if semanticSyntheticUnmappedLabel.IsNonCapabilityLabel() {
		t.Fatal("the fixture became a generic label; it would no longer force a capability rejection")
	}
	if len(imSemanticIntentRuleSet[semanticSyntheticUnmappedLabel]) > 0 {
		t.Fatal("the synthetic fixture acquired a capability rule")
	}
	for _, label := range intent.AllLabels() {
		if label == semanticSyntheticUnmappedLabel {
			t.Fatal("the synthetic fixture entered the real taxonomy; pick a name that cannot collide")
		}
	}
}

// The bridge: a synthetic stand-in is only worth having if it is treated the
// same as the real thing. While any real unmigrated family exists, both must
// produce the same rejection, so a divergence shows up here rather than as a
// suite that silently stops testing production behavior.
func TestTheSyntheticFixtureRejectsExactlyLikeARealUnmigratedFamily(t *testing.T) {
	realLabel := semanticRealUnmigratedLabel(t)

	reject := func(label intent.IntentLabel) (bool, error) {
		h := &IMMessageHandler{}
		_, handled, err := h.semanticPlanForTurnWithClassification(
			"user", "mixed request", "lansenger", "root-"+string(label), "turn-"+string(label),
			&intent.ClassificationResult{
				Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{label}, Confidence: .98,
			})
		return handled, err
	}

	realHandled, realErr := reject(realLabel)
	synthHandled, synthErr := reject(semanticSyntheticUnmappedLabel)
	if realErr == nil || synthErr == nil {
		t.Fatalf("both must fail closed: real=%v synthetic=%v", realErr, synthErr)
	}
	if realHandled != synthHandled {
		t.Fatalf("host ownership differs: real handled=%v, synthetic handled=%v", realHandled, synthHandled)
	}
	normalize := func(err error, label intent.IntentLabel) string {
		return strings.ReplaceAll(err.Error(), string(label), "<label>")
	}
	if got, want := normalize(synthErr, semanticSyntheticUnmappedLabel), normalize(realErr, realLabel); got != want {
		t.Fatalf("synthetic rejection %q differs from real rejection %q", got, want)
	}
}

func TestSemanticIntentRuleCoverageInventory(t *testing.T) {
	var unaccounted []string
	for _, label := range intent.AllLabels() {
		_, mapped := imSemanticIntentRuleSet[label]
		reason, declared := unmigratedSemanticIntentLabels[label]

		if label.IsNonCapabilityLabel() {
			if mapped {
				t.Errorf("non-capability label %q has a capability rule", label)
			}
			if declared {
				t.Errorf("non-capability label %q is listed as unmigrated; it can never own a rule", label)
			}
			continue
		}
		switch {
		case mapped && declared:
			t.Errorf("label %q has a capability rule now; delete its unmigrated entry (%s)", label, reason)
		case !mapped && !declared:
			unaccounted = append(unaccounted, string(label))
		}
	}
	if len(unaccounted) > 0 {
		sort.Strings(unaccounted)
		t.Fatalf("capability labels with neither a rule nor a reviewed reason: %v\n"+
			"A turn classified with one of these HostRejects. Either add a rule to "+
			"imSemanticIntentRuleSet or record why the family is not migrated yet.", unaccounted)
	}
}
