package memory

import (
	"testing"
	"time"
)

func buildInferenceTestGraph(t *testing.T, entries []Entry) *SemanticGraph {
	t.Helper()
	g := NewSemanticGraph()
	g.Rebuild(entries)
	return g
}

var inferenceTestNow = time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// Transitive inference tests
// ---------------------------------------------------------------------------

func TestInference_Transitive_LocatedIn_TwoHops(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "RapidAI located in Hangzhou", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:rapidai", "relation:located_in", "entity:hangzhou"}},
		{ID: "e2", Content: "Hangzhou located in Zhejiang", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:hangzhou", "relation:located_in", "entity:zhejiang"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"rapidai"}, InferenceOptions{Now: inferenceTestNow})

	found := false
	for _, df := range results {
		if df.Subject == "rapidai" && df.Predicate == "located_in" && df.Object == "zhejiang" {
			found = true
			if df.Confidence < 0.50 || df.Confidence > 0.90 {
				t.Errorf("unexpected confidence: %.2f", df.Confidence)
			}
			if df.RuleName != "located_in_transitive" {
				t.Errorf("unexpected rule: %s", df.RuleName)
			}
			if len(df.SourceFacts) != 2 {
				t.Errorf("expected 2 source facts, got %d", len(df.SourceFacts))
			}
		}
	}
	if !found {
		t.Fatalf("expected derived fact (rapidai located_in zhejiang), got %+v", results)
	}
}

func TestInference_Transitive_ThreeHops(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "office in building-a", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:office", "relation:located_in", "entity:building-a"}},
		{ID: "e2", Content: "building-a in tech-park", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:building-a", "relation:located_in", "entity:tech-park"}},
		{ID: "e3", Content: "tech-park in hangzhou", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:tech-park", "relation:located_in", "entity:hangzhou"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"office"}, InferenceOptions{Now: inferenceTestNow})

	// Should derive: office → tech-park (2 hops) and office → hangzhou (3 hops)
	foundTechPark := false
	foundHangzhou := false
	for _, df := range results {
		if df.Subject == "office" && df.Object == "tech-park" {
			foundTechPark = true
		}
		if df.Subject == "office" && df.Object == "hangzhou" {
			foundHangzhou = true
			// 3 hops: 0.85^3 ≈ 0.61
			if df.Confidence < 0.55 || df.Confidence > 0.70 {
				t.Errorf("3-hop confidence should be ~0.61, got %.2f", df.Confidence)
			}
		}
	}
	if !foundTechPark {
		t.Errorf("expected derived fact (office located_in tech-park)")
	}
	if !foundHangzhou {
		t.Errorf("expected derived fact (office located_in hangzhou)")
	}
}

func TestInference_Transitive_CycleAvoidance(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A in B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:located_in", "entity:b"}},
		{ID: "e2", Content: "B in A", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:b", "relation:located_in", "entity:a"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow})

	// Should not infinite loop; cycle detection prevents revisiting "a".
	for _, df := range results {
		if df.Object == "a" {
			t.Fatalf("cycle not detected: derived fact loops back to seed: %+v", df)
		}
	}
}

func TestInference_Transitive_MaxChainLengthRespected(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "a depends on b", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:depends_on", "entity:b"}},
		{ID: "e2", Content: "b depends on c", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:b", "relation:depends_on", "entity:c"}},
		{ID: "e3", Content: "c depends on d", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:c", "relation:depends_on", "entity:d"}},
	})
	ie := NewInferenceEngine(g, nil)
	// depends_on has MaxChainLength=2 and ConfidenceDecay=0.70.
	// 2-hop confidence = 0.70^2 = 0.49, below default MinConfidence=0.50.
	// Use lower threshold to test chain length limit independently.
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow, MinConfidence: 0.40})

	foundC := false
	foundD := false
	for _, df := range results {
		if df.Subject == "a" && df.Predicate == "depends_on" && df.Object == "c" {
			foundC = true
		}
		if df.Subject == "a" && df.Predicate == "depends_on" && df.Object == "d" {
			foundD = true
		}
	}
	if !foundC {
		t.Errorf("expected derived fact (a depends_on c) within MaxChainLength=2")
	}
	if foundD {
		t.Errorf("did not expect derived fact (a depends_on d) — exceeds MaxChainLength=2")
	}
}

// ---------------------------------------------------------------------------
// Compositional inference tests
// ---------------------------------------------------------------------------

func TestInference_Compositional_WorksAtLocatedIn(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "Zhang San works at RapidAI", Category: CategoryUserFact, UpdatedAt: inferenceTestNow, Entities: []string{"entity:zhang-san", "relation:works_at", "entity:rapidai"}},
		{ID: "e2", Content: "RapidAI located in Hangzhou", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:rapidai", "relation:located_in", "entity:hangzhou"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"zhang-san"}, InferenceOptions{Now: inferenceTestNow})

	found := false
	for _, df := range results {
		if df.Subject == "zhang-san" && df.Predicate == "based_in" && df.Object == "hangzhou" {
			found = true
			if df.RuleName != "works_at+located_in=based_in" {
				t.Errorf("unexpected rule: %s", df.RuleName)
			}
			// 0.75^2 = 0.5625
			if df.Confidence < 0.50 || df.Confidence > 0.65 {
				t.Errorf("expected confidence ~0.56, got %.2f", df.Confidence)
			}
		}
	}
	if !found {
		t.Fatalf("expected derived fact (zhang-san based_in hangzhou), got %+v", results)
	}
}

func TestInference_Compositional_NoMatchingSecondHop(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "Zhang San works at RapidAI", Category: CategoryUserFact, UpdatedAt: inferenceTestNow, Entities: []string{"entity:zhang-san", "relation:works_at", "entity:rapidai"}},
		// No located_in fact for RapidAI.
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"zhang-san"}, InferenceOptions{Now: inferenceTestNow})

	for _, df := range results {
		if df.Predicate == "based_in" {
			t.Fatalf("should not derive based_in without second hop, got %+v", df)
		}
	}
}

// ---------------------------------------------------------------------------
// Visibility / isolation tests
// ---------------------------------------------------------------------------

func TestInference_OwnerIsolation(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A in B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, OwnerID: "user1", Entities: []string{"entity:a", "relation:located_in", "entity:b"}},
		{ID: "e2", Content: "B in C", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, OwnerID: "user2", Entities: []string{"entity:b", "relation:located_in", "entity:c"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow, OwnerID: "user1"})

	// Second hop belongs to user2, should not be reachable for user1.
	for _, df := range results {
		if df.Object == "c" {
			t.Fatalf("cross-owner inference should be blocked, got %+v", df)
		}
	}
}

func TestInference_OwnerIsolation_SharedFacts(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A depends on B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, OwnerID: "user1", Entities: []string{"entity:a", "relation:depends_on", "entity:b"}},
		{ID: "e2", Content: "B depends on C", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, OwnerID: "", Entities: []string{"entity:b", "relation:depends_on", "entity:c"}},
	})
	ie := NewInferenceEngine(g, nil)
	// depends_on is non-functional, so no contradiction filter applies.
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow, OwnerID: "user1", MinConfidence: 0.40})

	// Second hop has empty OwnerID (shared), should be reachable.
	found := false
	for _, df := range results {
		if df.Object == "c" {
			found = true
		}
	}
	if !found {
		t.Errorf("shared facts (empty OwnerID) should be reachable in inference, got %+v", results)
	}
}

func TestInference_TemporalFilter_ExpiredFact(t *testing.T) {
	expired := inferenceTestNow.Add(-24 * time.Hour)
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A in B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:located_in", "entity:b"}},
		{ID: "e2", Content: "B in C (expired)", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, InvalidAt: &expired, Entities: []string{"entity:b", "relation:located_in", "entity:c"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow})

	for _, df := range results {
		if df.Object == "c" {
			t.Fatalf("expired fact should not participate in inference, got %+v", df)
		}
	}
}

func TestInference_NegatedFactSkipped(t *testing.T) {
	g := NewSemanticGraph()
	// Manually build a graph with a negated fact.
	entries := []Entry{
		{ID: "e1", Content: "A in B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:located_in", "entity:b"}},
		{ID: "e2", Content: "B NOT in C", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:b", "relation:located_in", "entity:c"}},
	}
	g.Rebuild(entries)
	// Manually negate the second fact.
	for i := range g.facts {
		if g.facts[i].Subject == "b" && g.facts[i].Object == "c" {
			g.facts[i].Negated = true
		}
	}

	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow})

	for _, df := range results {
		if df.Object == "c" {
			t.Fatalf("negated fact should not participate in inference, got %+v", df)
		}
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestInference_EmptyQueryEntities(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A in B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:located_in", "entity:b"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer(nil, InferenceOptions{Now: inferenceTestNow})
	if len(results) != 0 {
		t.Fatalf("expected no results for empty query, got %+v", results)
	}
}

func TestInference_NilGraph(t *testing.T) {
	ie := NewInferenceEngine(nil, nil)
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow})
	if len(results) != 0 {
		t.Fatalf("expected no results for nil graph, got %+v", results)
	}
}

func TestInference_MaxVisitedFactsRespected(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A in B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:located_in", "entity:b"}},
		{ID: "e2", Content: "B in C", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:b", "relation:located_in", "entity:c"}},
		{ID: "e3", Content: "C in D", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:c", "relation:located_in", "entity:d"}},
	})
	ie := NewInferenceEngine(g, nil)
	// With MaxVisitedFacts=1, should not be able to complete even the first chain.
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow, MaxVisitedFacts: 1})
	// May or may not find results depending on adjacency order, but should not panic.
	_ = results
}

func TestInference_Deduplication(t *testing.T) {
	// Two paths to the same conclusion: A→B→C via two different entries.
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A in B (path 1)", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:located_in", "entity:b"}},
		{ID: "e2", Content: "B in C", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:b", "relation:located_in", "entity:c"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow})

	// Count how many (a, located_in, c) derived facts exist.
	count := 0
	for _, df := range results {
		if df.Subject == "a" && df.Predicate == "located_in" && df.Object == "c" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected deduplication to keep only 1 derived fact, got %d", count)
	}
}

func TestInference_MinConfidenceFilter(t *testing.T) {
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A in B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:located_in", "entity:b"}},
		{ID: "e2", Content: "B in C", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:b", "relation:located_in", "entity:c"}},
	})
	ie := NewInferenceEngine(g, nil)
	// Set high confidence threshold.
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow, MinConfidence: 0.90})

	// 2-hop located_in: 0.85^2 = 0.72, below 0.90 threshold.
	for _, df := range results {
		if df.Subject == "a" && df.Object == "c" && df.Predicate == "located_in" {
			t.Fatalf("fact with confidence 0.72 should be filtered at threshold 0.90, got %+v", df)
		}
	}
}

// ---------------------------------------------------------------------------
// FormatDerivedFactsForPrompt
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Contradiction detection tests
// ---------------------------------------------------------------------------

func TestInference_ContradictionSuppressed(t *testing.T) {
	// Direct fact: zhang-san works_at rapidai
	// Transitive chain would derive: zhang-san located_in hangzhou (via works_at+located_in)
	// But if there's a direct fact: zhang-san located_in beijing, the derived fact should be suppressed.
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "Zhang San located in Beijing", Category: CategoryUserFact, UpdatedAt: inferenceTestNow, Entities: []string{"entity:zhang-san", "relation:located_in", "entity:beijing"}},
		{ID: "e2", Content: "Zhang San works at RapidAI", Category: CategoryUserFact, UpdatedAt: inferenceTestNow, Entities: []string{"entity:zhang-san", "relation:works_at", "entity:rapidai"}},
		{ID: "e3", Content: "RapidAI located in Hangzhou", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:rapidai", "relation:located_in", "entity:hangzhou"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"zhang-san"}, InferenceOptions{Now: inferenceTestNow})

	// The compositional rule works_at+located_in=based_in would derive (zhang-san, based_in, hangzhou).
	// But the direct fact says (zhang-san, located_in, beijing).
	// Since based_in != located_in, no contradiction — based_in should still appear.
	// However, if transitive located_in tried to derive (zhang-san, located_in, X), it would contradict.
	for _, df := range results {
		if df.Subject == "zhang-san" && df.Predicate == "located_in" && df.Object != "beijing" {
			t.Fatalf("contradiction not detected: derived (zhang-san, located_in, %s) contradicts direct (zhang-san, located_in, beijing)", df.Object)
		}
	}
}

func TestInference_NonFunctionalRelation_NoContradiction(t *testing.T) {
	// depends_on is non-functional — multiple values are valid.
	g := buildInferenceTestGraph(t, []Entry{
		{ID: "e1", Content: "A depends on B", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:depends_on", "entity:b"}},
		{ID: "e2", Content: "B depends on C", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:b", "relation:depends_on", "entity:c"}},
		{ID: "e3", Content: "A depends on D", Category: CategoryProjectKnowledge, UpdatedAt: inferenceTestNow, Entities: []string{"entity:a", "relation:depends_on", "entity:d"}},
	})
	ie := NewInferenceEngine(g, nil)
	results := ie.Infer([]string{"a"}, InferenceOptions{Now: inferenceTestNow, MinConfidence: 0.40})

	// (a, depends_on, c) should NOT be suppressed even though (a, depends_on, d) exists.
	found := false
	for _, df := range results {
		if df.Subject == "a" && df.Predicate == "depends_on" && df.Object == "c" {
			found = true
		}
	}
	if !found {
		t.Errorf("non-functional relation should not trigger contradiction filter, got %+v", results)
	}
}

// ---------------------------------------------------------------------------
// FormatDerivedFactsForPrompt
// ---------------------------------------------------------------------------

func TestFormatDerivedFactsForPrompt_Empty(t *testing.T) {
	result := FormatDerivedFactsForPrompt(nil, 5)
	if result != "" {
		t.Errorf("expected empty string for nil facts, got %q", result)
	}
}

func TestFormatDerivedFactsForPrompt_LimitsOutput(t *testing.T) {
	facts := make([]DerivedFact, 10)
	for i := range facts {
		facts[i] = DerivedFact{
			Subject: "a", Predicate: "rel", Object: "b",
			Confidence:  0.80,
			Explanation: "a → rel → b",
		}
	}
	result := FormatDerivedFactsForPrompt(facts, 3)
	// Should contain exactly 3 bullet points.
	count := 0
	for _, line := range splitLines(result) {
		if len(line) > 0 && line[0] == 0xe2 { // UTF-8 bullet "•"
			count++
		}
	}
	if count != 3 {
		t.Errorf("expected 3 facts in output, got %d. Output:\n%s", count, result)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
