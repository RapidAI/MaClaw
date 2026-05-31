package workflow

import "strings"

// PhaseMeta is the dashboard-facing projection of a PhaseTemplate. It is the
// single serialized shape consumed by every renderer (GUI adapter, TUI adapter,
// the code generator, and the contract test).
type PhaseMeta struct {
	ID              string `json:"id"`               // canonical phase id (aliases applied)
	Name            string `json:"name"`             // display label (template Name)
	Index           int    `json:"index"`            // 0-based order after dedup
	ExpectsDocument bool   `json:"expects_document"` // produces a preview document
	CanSkip         bool   `json:"can_skip"`         // phase is optional
	NeedsConfirm    bool   `json:"needs_confirm"`    // phase pauses for user confirmation
}

// CanonicalPhaseID applies the phase-ID alias table so that backend IDs and the
// dashboard's legacy IDs agree on one canonical key. The known aliases collapse
// to a single canonical form:
//
//	requirements | requirement | req                  -> requirements
//	design       | tech_design | technical_design     -> design
//	tasks        | task        | task_plan | task_breakdown -> tasks
//
// Any phase ID that is not a known alias is returned unchanged. An empty or
// whitespace-only input that matches no alias yields the empty string, which the
// metadata deriver treats as a phase to drop.
func CanonicalPhaseID(phaseID string) string {
	switch strings.ToLower(strings.TrimSpace(phaseID)) {
	case "requirements", "requirement", "req":
		return "requirements"
	case "design", "tech_design", "technical_design":
		return "design"
	case "tasks", "task", "task_plan", "task_breakdown":
		return "tasks"
	default:
		return phaseID
	}
}

// PhaseExpectsDocument is the single rule for whether a phase yields a preview
// document. Execution phases use a non-document tool policy (ToolFilterFull or
// ToolFilterOpsControlled); every other policy produces a document.
func PhaseExpectsDocument(p PhaseTemplate) bool {
	return p.ToolPolicy != ToolFilterFull && p.ToolPolicy != ToolFilterOpsControlled
}

// PhaseMetadata projects a template's phases into ordered, de-duplicated
// PhaseMeta. It is the single mechanism that turns a template into renderable
// phase metadata; the GUI/TUI adapters and the code generator all call it.
//
// Returns nil for a nil or empty template. Phase IDs are canonicalized; phases
// whose canonical ID is empty or already seen are dropped (first occurrence
// wins). Each emitted phase receives a contiguous 0-based Index in slice order.
func PhaseMetadata(tmpl *WorkflowTemplate) []PhaseMeta {
	if tmpl == nil || len(tmpl.Phases) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(tmpl.Phases))
	metas := make([]PhaseMeta, 0, len(tmpl.Phases))

	for _, phase := range tmpl.Phases {
		id := CanonicalPhaseID(phase.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		metas = append(metas, PhaseMeta{
			ID:              id,
			Name:            phase.Name,
			Index:           len(metas),
			ExpectsDocument: PhaseExpectsDocument(phase),
			CanSkip:         phase.CanSkip,
			NeedsConfirm:    phase.NeedsConfirm,
		})
	}

	if len(metas) == 0 {
		return nil
	}
	return metas
}
