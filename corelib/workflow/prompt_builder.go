package workflow

import (
	"fmt"
	"strings"
)

// BuildPhaseSystemPrompt builds the system prompt for a workflow phase.
// Includes phase instructions, durable workflow input, structured form data,
// intent summary, previous phase outputs, and phase checklist constraints.
func BuildPhaseSystemPrompt(state *WorkflowState, phase *PhaseTemplate, registry *WorkflowRegistry) string {
	if state == nil || phase == nil {
		return ""
	}

	var b strings.Builder

	fmt.Fprintf(&b, "## Current Phase: %s\n\n", phase.Name)
	if strings.TrimSpace(phase.Description) != "" {
		fmt.Fprintf(&b, "%s\n\n", phase.Description)
	}

	if strings.TrimSpace(phase.Prompt) != "" {
		fmt.Fprintf(&b, "## Phase Instructions\n\n%s\n\n", phase.Prompt)
	}

	if registry != nil {
		tmpl := registry.Match(state.Type)
		if tmpl != nil && tmpl.NeedsInputDocument() {
			req := tmpl.RequiresInput
			if state.IsWaitingForInput(tmpl) {
				b.WriteString("## Required Workflow Input\n\n")
				fmt.Fprintf(&b, "%s\n\n", req.Description)
				if len(req.FileTypes) > 0 {
					fmt.Fprintf(&b, "Supported file types: %s\n", strings.Join(req.FileTypes, ", "))
				}
				if req.AcceptText {
					b.WriteString("The user may paste document text directly or provide a URL/path for the system to inspect.\n")
				}
				b.WriteString("\nAsk the user to provide the required input before producing this phase deliverable. Do not fabricate the source material.\n\n")
			} else if state.PhaseIndex == 0 && strings.TrimSpace(req.AnalysisHint) != "" {
				b.WriteString("## Input Analysis Guidance\n\n")
				fmt.Fprintf(&b, "%s\n\n", req.AnalysisHint)
			}
		}
	}

	if state.InputPayload != nil && (strings.TrimSpace(state.InputPayload.Text) != "" || len(state.InputPayload.Attachments) > 0) {
		b.WriteString("## Workflow Source Material\n\n")
		if strings.TrimSpace(state.InputPayload.Text) != "" {
			fmt.Fprintf(&b, "### Text or Path\n\n%s\n\n", state.InputPayload.Text)
		}
		if len(state.InputPayload.Attachments) > 0 {
			b.WriteString("### Attachments\n\n")
			for _, att := range state.InputPayload.Attachments {
				name := strings.TrimSpace(att.FileName)
				if name == "" {
					name = "unnamed"
				}
				fmt.Fprintf(&b, "- %s (%s, %s, %d bytes)\n", name, att.Type, att.MimeType, att.Size)
			}
			b.WriteString("\n")
		}
		b.WriteString("Treat the workflow source material above as durable evidence for this phase and later phases.\n\n")
	}

	b.WriteString("## User Intent Summary\n\n")
	if state.Intent.Category != "" {
		fmt.Fprintf(&b, "- Category: %s\n", string(state.Intent.Category))
	}
	if strings.TrimSpace(state.Intent.Summary) != "" {
		fmt.Fprintf(&b, "- Summary: %s\n", state.Intent.Summary)
	}
	if len(state.Intent.Goals) > 0 {
		b.WriteString("- Goals:\n")
		for _, g := range state.Intent.Goals {
			fmt.Fprintf(&b, "  - %s\n", g)
		}
	}
	if len(state.Intent.Constraints) > 0 {
		b.WriteString("- Constraints:\n")
		for _, c := range state.Intent.Constraints {
			fmt.Fprintf(&b, "  - %s\n", c)
		}
	}
	b.WriteString("\n")

	if phase.InputSchema != nil && !state.PhaseFormSkipped && !phaseFormDataWasSkipped(state.PhaseFormData) {
		if rendered := renderPhaseFormData(state.PhaseFormData, phase.InputSchema); rendered != "" {
			b.WriteString("## User-Provided Structured Context\n\n")
			b.WriteString(rendered)
			b.WriteString("\nUse this structured context when producing the current phase deliverable.\n\n")
		} else if state.PhaseFormSubmitted {
			b.WriteString("## User-Provided Structured Context\n\n")
			b.WriteString("The user submitted the structured form without optional details. Do not invent missing optional values.\n\n")
		}
	}

	if state.PhaseIndex > 0 && registry != nil {
		tmpl := registry.Match(state.Type)
		if tmpl != nil && len(tmpl.Phases) > 0 {
			var prevOutputs []string
			for i := 0; i < state.PhaseIndex && i < len(tmpl.Phases); i++ {
				pid := tmpl.Phases[i].ID
				if output := strings.TrimSpace(state.PhaseOutputs[pid]); output != "" {
					limit := 600
					if i == state.PhaseIndex-1 {
						limit = 1200
					}
					summary := truncateRunesSmart(output, limit)
					prevOutputs = append(prevOutputs, fmt.Sprintf("### %s (summary)\n\n%s", tmpl.Phases[i].Name, summary))
				}
			}
			if len(prevOutputs) > 0 {
				b.WriteString("## Previous Phase Outputs (summarized; full content remains in conversation/artifacts)\n\n")
				b.WriteString(strings.Join(prevOutputs, "\n\n"))
				b.WriteString("\n\n")
			}
		}
	}

	if len(phase.Checklist) > 0 {
		b.WriteString("## Quality Checklist\n\n")
		for _, item := range phase.Checklist {
			fmt.Fprintf(&b, "- [ ] %s\n", item)
		}
		b.WriteString("\n")
	}

	if phase.NeedsConfirm {
		b.WriteString("## Review Gate\n\n")
		b.WriteString("This phase requires explicit user review before the workflow may advance. Follow these rules:\n")
		b.WriteString("1. Produce only the deliverable for the current phase, then stop.\n")
		b.WriteString("2. Do not begin the next phase in the same response.\n")
		b.WriteString("3. End by asking the user to confirm the deliverable or provide requested changes.\n")
		b.WriteString("4. If required information is missing, ask a focused clarification question instead of inventing defaults.\n\n")
	}

	return b.String()
}

func phaseFormDataWasSkipped(formData map[string]interface{}) bool {
	if len(formData) == 0 {
		return false
	}
	_, skipped := formData["_skipped"]
	return skipped
}

func renderPhaseFormData(formData map[string]interface{}, schema *PhaseInputSchema) string {
	if len(formData) == 0 || schema == nil {
		return ""
	}
	var b strings.Builder
	for _, field := range schema.Fields {
		val, ok := formData[field.Name]
		if !ok || val == nil {
			continue
		}
		valStr := fmt.Sprintf("%v", val)
		if strings.TrimSpace(valStr) == "" || valStr == "[]" {
			continue
		}
		label := strings.TrimSpace(field.Label)
		if label == "" {
			label = field.Name
		}
		fmt.Fprintf(&b, "- **%s**: %s\n", label, valStr)
	}
	return b.String()
}

// BuildQualityGatePrompt builds the prompt for quality gate checking.
// Asks the LLM to check the output against the phase's checklist items
// and return a structured assessment.
func BuildQualityGatePrompt(phase *PhaseTemplate, output string) string {
	if phase == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("Review the following workflow phase deliverable against the quality checklist.\n\n")
	fmt.Fprintf(&b, "Phase: %s\n\n", phase.Name)
	b.WriteString("Deliverable content:\n\n")
	b.WriteString(output)
	b.WriteString("\n\n")

	b.WriteString("Checklist:\n\n")
	for i, item := range phase.Checklist {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}

	b.WriteString("\nReturn a JSON array. Each item must contain description, passed (bool), and note fields. Example:\n")
	b.WriteString("[{\"description\":\"Checklist item\",\"passed\":true,\"note\":\"Brief rationale\"}]")
	b.WriteString("\n")

	return b.String()
}

// GetToolFilterForPhase returns the tool filtering policy for a phase.
func GetToolFilterForPhase(phase *PhaseTemplate) ToolFilterPolicy {
	if phase == nil {
		return ToolFilterNone
	}
	return phase.ToolPolicy
}

// truncateRunesSmart truncates a string to at most maxRunes runes, trying
// to break at a paragraph boundary ("\n\n") or line boundary ("\n") rather
// than mid-sentence. Falls back to a hard rune cut with an ASCII truncation
// marker if no good break point is found within the last 20% of the budget.
func truncateRunesSmart(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}

	searchStart := maxRunes - maxRunes/5
	if searchStart < 0 {
		searchStart = 0
	}

	bestBreak := -1
	for i := maxRunes - 1; i >= searchStart; i-- {
		if runes[i] == '\n' {
			if i > 0 && runes[i-1] == '\n' {
				return string(runes[:i-1]) + "\n\n...(truncated)"
			}
			if bestBreak < 0 {
				bestBreak = i
			}
		}
	}

	if bestBreak >= 0 {
		return string(runes[:bestBreak]) + "\n...(truncated)"
	}

	return string(runes[:maxRunes]) + "...(truncated)"
}
