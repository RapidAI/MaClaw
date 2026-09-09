package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestIntentRuleCoverageFromClassification(t *testing.T) {
	rules := map[intent.IntentLabel][]IntentCapabilityNeedTemplate{
		intent.LabelSearch: {{Capability: "information.search.web", Required: true}},
	}

	search := IntentRuleCoverageFromClassification(intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .95}, rules)
	if !search.Managed || search.Unmapped != "" {
		t.Fatalf("search coverage=%+v", search)
	}

	generic := IntentRuleCoverageFromClassification(intent.ClassificationResult{Primary: intent.LabelNonCoding, Confidence: .9}, rules)
	if generic.Managed || generic.Unmapped != "" {
		t.Fatalf("generic non_coding is not a coverage gap, coverage=%+v", generic)
	}

	searchPlusGeneric := IntentRuleCoverageFromClassification(intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelNonCoding}, Confidence: .95,
	}, rules)
	if !searchPlusGeneric.Managed || searchPlusGeneric.Unmapped != "" {
		t.Fatalf("generic secondary must not unmap a governed primary, coverage=%+v", searchPlusGeneric)
	}

	unmappedOnly := IntentRuleCoverageFromClassification(intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: .95}, rules)
	if unmappedOnly.Managed || unmappedOnly.Unmapped != intent.LabelCoding {
		t.Fatalf("unmapped-only coding must stay unmanaged, coverage=%+v", unmappedOnly)
	}

	mixed := IntentRuleCoverageFromClassification(intent.ClassificationResult{
		Primary: intent.LabelSearch, Secondary: []intent.IntentLabel{intent.LabelDocumentDelivery}, Confidence: .95,
	}, rules)
	if !mixed.Managed || mixed.Unmapped != intent.LabelDocumentDelivery {
		t.Fatalf("search+unmigrated must be managed and unmapped, coverage=%+v", mixed)
	}

	codingThenSearch := IntentRuleCoverageFromClassification(intent.ClassificationResult{
		Primary: intent.LabelCoding, Secondary: []intent.IntentLabel{intent.LabelSearch}, Confidence: .95,
	}, rules)
	if !codingThenSearch.Managed || codingThenSearch.Unmapped != intent.LabelCoding {
		t.Fatalf("first unmapped label must win, coverage=%+v", codingThenSearch)
	}
}
