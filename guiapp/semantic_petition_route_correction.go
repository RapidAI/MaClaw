package guiapp

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type petitionRouteCorrection struct {
	previousLabel intent.IntentLabel
	capability    tool.CapabilityID
	family        string
	selectionIDs  map[string]bool
}

// petitionRouteCorrectionFor recognizes a route change, not a composite task.
// The old shell family must have come from the immediately preceding petition,
// must not own an execution record or pending artifact, and must not be another
// step's dependency. Classifier-owned and attempted work remain parent authority.
func petitionRouteCorrectionFor(surface *semanticCallSurface, next intent.IntentLabel) (petitionRouteCorrection, bool) {
	if surface == nil || surface.replan == nil || len(surface.replan.Petitioned) == 0 {
		return petitionRouteCorrection{}, false
	}
	previous := surface.replan.Petitioned[len(surface.replan.Petitioned)-1]
	var capability tool.CapabilityID
	switch {
	case previous == intent.LabelShellCommand && next == intent.LabelSSH:
		capability = tool.CapabilityShellExecuteLocal
	case previous == intent.LabelSSH && next == intent.LabelShellCommand:
		capability = tool.CapabilityShellExecuteRemoteHost
	default:
		return petitionRouteCorrection{}, false
	}
	correction := petitionRouteCorrection{previousLabel: previous, capability: capability, selectionIDs: map[string]bool{}}
	found := false
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability != capability {
			continue
		}
		family := tool.RepeatFamilyKey(selection.NeedID)
		if family == "" || (found && correction.family != family) {
			return petitionRouteCorrection{}, false
		}
		if surface.completed[selection.ID] || len(surface.pendingArtifacts[selection.ID]) > 0 || hopSelectionEvidence(surface, selection.ID).State != "" {
			return petitionRouteCorrection{}, false
		}
		correction.family = family
		correction.selectionIDs[selection.ID] = true
		found = true
	}
	if !found {
		return petitionRouteCorrection{}, false
	}
	for _, selection := range surface.plan.Selections {
		if tool.RepeatFamilyKey(selection.NeedID) == correction.family {
			continue
		}
		for _, requirement := range selection.Requires {
			if correction.selectionIDs[requirement] || tool.RepeatFamilyKey(requirement) == correction.family {
				return petitionRouteCorrection{}, false
			}
		}
	}
	return correction, true
}

func (c petitionRouteCorrection) removeFromClassification(classification intent.ClassificationResult) intent.ClassificationResult {
	out := classification
	out.Secondary = withoutIntentLabel(out.Secondary, c.previousLabel)
	out.Offered = withoutIntentLabel(out.Offered, c.previousLabel)
	return out
}

func (c petitionRouteCorrection) removeFromPetitioned(labels []intent.IntentLabel) []intent.IntentLabel {
	return withoutIntentLabel(labels, c.previousLabel)
}

func withoutIntentLabel(labels []intent.IntentLabel, remove intent.IntentLabel) []intent.IntentLabel {
	out := make([]intent.IntentLabel, 0, len(labels))
	for _, label := range labels {
		if label != remove {
			out = append(out, label)
		}
	}
	return out
}

func (c petitionRouteCorrection) apply(plan tool.ToolPlan) tool.ToolPlan {
	out := plan
	out.Selections = make([]tool.PlannedSelection, 0, len(plan.Selections))
	for _, selection := range plan.Selections {
		if tool.RepeatFamilyKey(selection.NeedID) == c.family {
			continue
		}
		out.Selections = append(out.Selections, selection)
	}
	out.Unmet = withoutPetitionCorrectedNotes(plan.Unmet, c.family)
	out.Omitted = withoutPetitionCorrectedNotes(plan.Omitted, c.family)
	return out
}

func withoutPetitionCorrectedNotes(notes []tool.UnmetNeed, family string) []tool.UnmetNeed {
	if len(notes) == 0 {
		return nil
	}
	out := make([]tool.UnmetNeed, 0, len(notes))
	for _, note := range notes {
		if tool.RepeatFamilyKey(note.NeedID) != family {
			out = append(out, note)
		}
	}
	return out
}

func validateSemanticPetitionRouteCorrection(parent, child tool.ToolPlan, next intent.IntentLabel, correction petitionRouteCorrection) error {
	if correction.family == "" || semanticPlanHasCapability(child, correction.capability) {
		return fmt.Errorf("semantic petition route correction retained the replaced family")
	}
	reduced := correction.apply(parent)
	if err := validateSemanticPetitionExpansion(reduced, child, next); err != nil {
		return fmt.Errorf("semantic petition route correction: %w", err)
	}
	return nil
}

func petitionRouteCorrectionName(c petitionRouteCorrection, next intent.IntentLabel) string {
	return strings.TrimSpace(string(c.previousLabel)) + "->" + strings.TrimSpace(string(next))
}
