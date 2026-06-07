package memory

import (
	"math"
	"testing"

	"pgregory.net/rapid"
)

// Feature: knowledge-retrieval-multipage-recall, Property 4: Adaptive expansion formula correctness
//
// For any integer matchingEntries >= 0 and totalActiveEntries > 0,
// when matchingEntries / totalActiveEntries > 0.15, the expanded entry count
// SHALL equal min(24, max(12, floor(matchingEntries * 0.4))).
//
// **Validates: Requirements 2.3**
func TestProperty4_AdaptiveExpansionFormula(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	rapid.Check(t, func(rt *rapid.T) {
		matchingEntries := rapid.IntRange(0, 500).Draw(rt, "matchingEntries")
		totalActiveEntries := rapid.IntRange(0, 1000).Draw(rt, "totalActiveEntries")

		result := calc.Calculate(matchingEntries, totalActiveEntries)

		// Case 1: totalActiveEntries == 0 → no expansion, density is 0.
		if totalActiveEntries == 0 {
			if result.MaxEntries != defaultMaxEntries {
				rt.Fatalf("totalActiveEntries=0: expected MaxEntries=%d, got %d",
					defaultMaxEntries, result.MaxEntries)
			}
			if result.Expanded {
				rt.Fatal("totalActiveEntries=0: expected Expanded=false")
			}
			return
		}

		density := float64(matchingEntries) / float64(totalActiveEntries)

		// Case 2: density <= 0.15 → no expansion.
		if density <= topicDensityThreshold {
			if result.MaxEntries != defaultMaxEntries {
				rt.Fatalf("density=%.4f <= threshold: expected MaxEntries=%d, got %d",
					density, defaultMaxEntries, result.MaxEntries)
			}
			if result.Expanded {
				rt.Fatalf("density=%.4f <= threshold: expected Expanded=false", density)
			}
			return
		}

		// Case 3: density > 0.15 → expansion formula applies.
		// Expected: min(24, max(12, floor(matchingEntries * 0.4)))
		expected := int(math.Floor(float64(matchingEntries) * expansionFactor))
		if expected < defaultMaxEntries {
			expected = defaultMaxEntries
		}
		if expected > expandedMaxEntries {
			expected = expandedMaxEntries
		}

		if result.MaxEntries != expected {
			rt.Fatalf("density=%.4f > threshold: expected MaxEntries=%d (min(24, max(12, floor(%d * 0.4)))), got %d",
				density, expected, matchingEntries, result.MaxEntries)
		}
		if !result.Expanded {
			rt.Fatalf("density=%.4f > threshold: expected Expanded=true", density)
		}
		if result.MaxTokens != expandedMaxTokens {
			rt.Fatalf("density=%.4f > threshold: expected MaxTokens=%d, got %d",
				density, expandedMaxTokens, result.MaxTokens)
		}
	})
}
