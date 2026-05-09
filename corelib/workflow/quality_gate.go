package workflow

import (
	"strings"
	"time"
)

// RunQualityGate records checklist items for human review.
// Phase quality is not inferred from keyword matches; review/confirmation
// remains the authoritative transition mechanism.
func RunQualityGate(phase *PhaseTemplate, output string) *QualityGateResult {
	if phase == nil || len(phase.Checklist) == 0 || strings.TrimSpace(output) == "" {
		return nil
	}

	items := make([]GateCheckItem, 0, len(phase.Checklist))
	for _, desc := range phase.Checklist {
		items = append(items, GateCheckItem{
			Description: desc,
			Passed:      false,
			Note:        "requires review confirmation",
		})
	}

	return &QualityGateResult{
		PhaseID:   phase.ID,
		Passed:    false,
		Items:     items,
		CheckedAt: time.Now(),
	}
}
