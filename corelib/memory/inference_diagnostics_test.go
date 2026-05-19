package memory

import (
	"path/filepath"
	"testing"
)

func TestInferenceDiagnosticsForHostReportsRulesAndGraphStats(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	diagnostics := store.InferenceDiagnosticsForHost()
	if !diagnostics.EngineActive || diagnostics.RuleCount == 0 || len(diagnostics.Rules) == 0 {
		t.Fatalf("expected active inference diagnostics: %+v", diagnostics)
	}
}

func TestTestInferenceForHostMapsDerivedFacts(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if facts := store.TestInferenceForHost("", InferenceOptions{}); facts != nil {
		t.Fatalf("empty query should not infer facts: %+v", facts)
	}
	mapped := InferenceFactsForHost([]DerivedFact{{
		Subject: "react", Predicate: "depends_on", Object: "vite", RuleName: "test", Confidence: 0.8,
		SourceFacts: []SemanticFact{{Subject: "react", Predicate: "depends_on", Object: "tooling"}},
	}})
	if len(mapped) != 1 || mapped[0].SourceCount != 1 || mapped[0].RuleName != "test" {
		t.Fatalf("unexpected mapped facts: %+v", mapped)
	}
}
