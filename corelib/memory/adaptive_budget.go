package memory

import "math"

// AdaptiveBudgetCalculator determines the proactive recall budget based on
// topic density — the ratio of matching entries to total active entries.
// When density exceeds topicDensityThreshold (0.15), the budget expands to
// allow more entries and tokens to be injected into the system prompt.
type AdaptiveBudgetCalculator struct{}

// Calculate returns the recall budget for the given counts.
//
// topicDensity = matchingEntries / totalActiveEntries
//
// When density > topicDensityThreshold:
//
//	expandedCount = min(24, max(12, floor(matchingEntries * 0.4)))
//	maxTokens = expandedMaxTokens (5000)
//	poolLimit = expandedMaxEntries * 4
//
// When density <= topicDensityThreshold (or totalActiveEntries == 0):
//
//	maxEntries = defaultMaxEntries (12)
//	maxTokens = defaultMaxTokens (2500)
//	poolLimit = defaultMaxEntries * 4
func (c *AdaptiveBudgetCalculator) Calculate(matchingEntries, totalActiveEntries int) AdaptiveBudgetResult {
	// Guard against invalid inputs.
	if matchingEntries < 0 {
		matchingEntries = 0
	}
	if totalActiveEntries <= 0 {
		return AdaptiveBudgetResult{
			MaxEntries:   defaultMaxEntries,
			MaxTokens:    defaultMaxTokens,
			Expanded:     false,
			TopicDensity: 0,
		}
	}

	density := float64(matchingEntries) / float64(totalActiveEntries)

	if density <= topicDensityThreshold {
		return AdaptiveBudgetResult{
			MaxEntries:   defaultMaxEntries,
			MaxTokens:    defaultMaxTokens,
			Expanded:     false,
			TopicDensity: density,
		}
	}

	// Density exceeds threshold — expand the budget.
	expandedCount := int(math.Floor(float64(matchingEntries) * expansionFactor))
	if expandedCount < defaultMaxEntries {
		expandedCount = defaultMaxEntries
	}
	if expandedCount > expandedMaxEntries {
		expandedCount = expandedMaxEntries
	}

	return AdaptiveBudgetResult{
		MaxEntries:   expandedCount,
		MaxTokens:    expandedMaxTokens,
		Expanded:     true,
		TopicDensity: density,
	}
}

// PoolLimit returns the candidate pool limit for the given budget result.
// The pool limit scales proportionally with MaxEntries to ensure sufficient
// scoring candidates are available for selection.
func (r AdaptiveBudgetResult) PoolLimit() int {
	return r.MaxEntries * 4
}
