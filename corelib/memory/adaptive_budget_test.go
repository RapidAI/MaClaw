package memory

import (
	"math"
	"testing"
)

func TestAdaptiveBudgetCalculator_ZeroEntries(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	result := calc.Calculate(0, 0)

	if result.MaxEntries != defaultMaxEntries {
		t.Errorf("MaxEntries = %d, want %d", result.MaxEntries, defaultMaxEntries)
	}
	if result.MaxTokens != defaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, defaultMaxTokens)
	}
	if result.Expanded {
		t.Error("Expanded = true, want false")
	}
	if result.TopicDensity != 0 {
		t.Errorf("TopicDensity = %f, want 0", result.TopicDensity)
	}
	if result.PoolLimit() != defaultMaxEntries*4 {
		t.Errorf("PoolLimit = %d, want %d", result.PoolLimit(), defaultMaxEntries*4)
	}
}

func TestAdaptiveBudgetCalculator_ZeroMatchingEntries(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	result := calc.Calculate(0, 100)

	if result.MaxEntries != defaultMaxEntries {
		t.Errorf("MaxEntries = %d, want %d", result.MaxEntries, defaultMaxEntries)
	}
	if result.MaxTokens != defaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, defaultMaxTokens)
	}
	if result.Expanded {
		t.Error("Expanded = true, want false")
	}
	if result.TopicDensity != 0 {
		t.Errorf("TopicDensity = %f, want 0", result.TopicDensity)
	}
}

func TestAdaptiveBudgetCalculator_BelowThreshold(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// 10 matching out of 100 total → density = 0.10 (below 0.15)
	result := calc.Calculate(10, 100)

	if result.MaxEntries != defaultMaxEntries {
		t.Errorf("MaxEntries = %d, want %d", result.MaxEntries, defaultMaxEntries)
	}
	if result.MaxTokens != defaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, defaultMaxTokens)
	}
	if result.Expanded {
		t.Error("Expanded = true, want false")
	}
	expectedDensity := 0.10
	if math.Abs(result.TopicDensity-expectedDensity) > 1e-9 {
		t.Errorf("TopicDensity = %f, want %f", result.TopicDensity, expectedDensity)
	}
}

func TestAdaptiveBudgetCalculator_AtThreshold(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// 15 matching out of 100 total → density = 0.15 (exactly at threshold, not exceeding)
	result := calc.Calculate(15, 100)

	if result.MaxEntries != defaultMaxEntries {
		t.Errorf("MaxEntries = %d, want %d", result.MaxEntries, defaultMaxEntries)
	}
	if result.MaxTokens != defaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, defaultMaxTokens)
	}
	if result.Expanded {
		t.Error("Expanded = true, want false (density == threshold, not >)")
	}
}

func TestAdaptiveBudgetCalculator_AboveThreshold_SmallExpansion(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// 20 matching out of 100 total → density = 0.20 (above 0.15)
	// expandedCount = floor(20 * 0.4) = 8 → clamped to min 12
	result := calc.Calculate(20, 100)

	if result.MaxEntries != 12 {
		t.Errorf("MaxEntries = %d, want 12 (clamped to min)", result.MaxEntries)
	}
	if result.MaxTokens != expandedMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, expandedMaxTokens)
	}
	if !result.Expanded {
		t.Error("Expanded = false, want true")
	}
	if result.PoolLimit() != 12*4 {
		t.Errorf("PoolLimit = %d, want %d", result.PoolLimit(), 12*4)
	}
}

func TestAdaptiveBudgetCalculator_AboveThreshold_MediumExpansion(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// 40 matching out of 100 total → density = 0.40 (above 0.15)
	// expandedCount = floor(40 * 0.4) = 16
	result := calc.Calculate(40, 100)

	if result.MaxEntries != 16 {
		t.Errorf("MaxEntries = %d, want 16", result.MaxEntries)
	}
	if result.MaxTokens != expandedMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, expandedMaxTokens)
	}
	if !result.Expanded {
		t.Error("Expanded = false, want true")
	}
	if result.PoolLimit() != 16*4 {
		t.Errorf("PoolLimit = %d, want %d", result.PoolLimit(), 16*4)
	}
}

func TestAdaptiveBudgetCalculator_AboveThreshold_LargeExpansion(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// 55 matching out of 100 total → density = 0.55 (above 0.15)
	// expandedCount = floor(55 * 0.4) = 22
	result := calc.Calculate(55, 100)

	if result.MaxEntries != 22 {
		t.Errorf("MaxEntries = %d, want 22", result.MaxEntries)
	}
	if result.MaxTokens != expandedMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, expandedMaxTokens)
	}
	if !result.Expanded {
		t.Error("Expanded = false, want true")
	}
}

func TestAdaptiveBudgetCalculator_AboveThreshold_CappedAt24(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// 80 matching out of 100 total → density = 0.80 (above 0.15)
	// expandedCount = floor(80 * 0.4) = 32 → clamped to max 24
	result := calc.Calculate(80, 100)

	if result.MaxEntries != 24 {
		t.Errorf("MaxEntries = %d, want 24 (capped at max)", result.MaxEntries)
	}
	if result.MaxTokens != expandedMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, expandedMaxTokens)
	}
	if !result.Expanded {
		t.Error("Expanded = false, want true")
	}
	if result.PoolLimit() != 24*4 {
		t.Errorf("PoolLimit = %d, want %d", result.PoolLimit(), 24*4)
	}
}

func TestAdaptiveBudgetCalculator_MatchingExceedsTotal(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// Edge case: matchingEntries > totalActiveEntries (shouldn't happen normally
	// but the calculator should handle it gracefully)
	// 120 matching out of 100 total → density = 1.20 (above 0.15)
	// expandedCount = floor(120 * 0.4) = 48 → clamped to max 24
	result := calc.Calculate(120, 100)

	if result.MaxEntries != 24 {
		t.Errorf("MaxEntries = %d, want 24 (capped at max)", result.MaxEntries)
	}
	if !result.Expanded {
		t.Error("Expanded = false, want true")
	}
}

func TestAdaptiveBudgetCalculator_SmallStore(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// Small store: 5 matching out of 10 total → density = 0.50 (above 0.15)
	// expandedCount = floor(5 * 0.4) = 2 → clamped to min 12
	result := calc.Calculate(5, 10)

	if result.MaxEntries != 12 {
		t.Errorf("MaxEntries = %d, want 12 (clamped to min)", result.MaxEntries)
	}
	if !result.Expanded {
		t.Error("Expanded = false, want true")
	}
	expectedDensity := 0.5
	if math.Abs(result.TopicDensity-expectedDensity) > 1e-9 {
		t.Errorf("TopicDensity = %f, want %f", result.TopicDensity, expectedDensity)
	}
}

func TestAdaptiveBudgetCalculator_BoundaryExpansionValues(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// Test the exact boundary where expansion factor produces 12:
	// floor(30 * 0.4) = 12 → exactly the minimum, should still be 12
	// density = 30/100 = 0.30 > 0.15
	result := calc.Calculate(30, 100)
	if result.MaxEntries != 12 {
		t.Errorf("30 matching: MaxEntries = %d, want 12", result.MaxEntries)
	}
	if !result.Expanded {
		t.Error("30 matching: Expanded = false, want true")
	}

	// Test the exact boundary where expansion factor produces 24:
	// floor(60 * 0.4) = 24 → exactly the maximum
	// density = 60/100 = 0.60 > 0.15
	result = calc.Calculate(60, 100)
	if result.MaxEntries != 24 {
		t.Errorf("60 matching: MaxEntries = %d, want 24", result.MaxEntries)
	}

	// Just above 24: floor(61 * 0.4) = 24
	result = calc.Calculate(61, 100)
	if result.MaxEntries != 24 {
		t.Errorf("61 matching: MaxEntries = %d, want 24", result.MaxEntries)
	}
}

func TestAdaptiveBudgetCalculator_JustAboveThreshold(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// 16 matching out of 100 total → density = 0.16 (just above 0.15)
	// expandedCount = floor(16 * 0.4) = 6 → clamped to min 12
	result := calc.Calculate(16, 100)

	if result.MaxEntries != 12 {
		t.Errorf("MaxEntries = %d, want 12", result.MaxEntries)
	}
	if result.MaxTokens != expandedMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, expandedMaxTokens)
	}
	if !result.Expanded {
		t.Error("Expanded = false, want true")
	}
}

func TestAdaptiveBudgetCalculator_PoolLimitProportional(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	tests := []struct {
		name             string
		matching         int
		total            int
		expectedEntries  int
		expectedPoolLim  int
	}{
		{"default budget", 5, 100, 12, 48},
		{"expanded min", 20, 100, 12, 48},
		{"expanded 16", 40, 100, 16, 64},
		{"expanded 22", 55, 100, 22, 88},
		{"expanded max", 80, 100, 24, 96},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calc.Calculate(tt.matching, tt.total)
			if result.MaxEntries != tt.expectedEntries {
				t.Errorf("MaxEntries = %d, want %d", result.MaxEntries, tt.expectedEntries)
			}
			if result.PoolLimit() != tt.expectedPoolLim {
				t.Errorf("PoolLimit = %d, want %d", result.PoolLimit(), tt.expectedPoolLim)
			}
		})
	}
}

func TestAdaptiveBudgetCalculator_TotalActiveEntriesZero_MatchingNonZero(t *testing.T) {
	calc := &AdaptiveBudgetCalculator{}

	// Edge case: matchingEntries > 0 but totalActiveEntries == 0
	// This shouldn't happen logically but the calculator should handle it gracefully.
	result := calc.Calculate(5, 0)

	if result.MaxEntries != defaultMaxEntries {
		t.Errorf("MaxEntries = %d, want %d", result.MaxEntries, defaultMaxEntries)
	}
	if result.MaxTokens != defaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", result.MaxTokens, defaultMaxTokens)
	}
	if result.Expanded {
		t.Error("Expanded = true, want false (totalActiveEntries == 0)")
	}
	if result.TopicDensity != 0 {
		t.Errorf("TopicDensity = %f, want 0", result.TopicDensity)
	}
}
