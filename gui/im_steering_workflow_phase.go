package main

import (
	"path/filepath"
	"strings"
)

type officeToolAction string

const (
	officeToolActionUnknown     officeToolAction = ""
	officeToolActionGeneratePDF officeToolAction = "generate_pdf"
)

func normalizeOfficeToolAction(action string) officeToolAction {
	switch officeToolAction(strings.TrimSpace(action)) {
	case officeToolActionGeneratePDF:
		return officeToolActionGeneratePDF
	default:
		return officeToolActionUnknown
	}
}

// matchPhaseKind extracts a workflow phase enum from known stable file tokens.
func (d *SteeringWorkflowDetector) matchPhaseKind(fileName string) workflowPhaseKind {
	candidates := workflowPhaseTokenCandidates(fileName)
	if len(candidates) == 0 {
		return workflowPhaseUnknown
	}
	for _, candidate := range candidates {
		if phase := normalizeWorkflowPhaseKind(candidate); phase != workflowPhaseUnknown {
			return phase
		}
	}
	return workflowPhaseUnknown
}

func workflowPhaseTokenCandidates(value string) []string {
	value = strings.TrimSpace(strings.ToLower(filepath.Base(value)))
	if value == "" || value == "." || value == string(filepath.Separator) {
		return nil
	}
	value = strings.TrimSuffix(value, filepath.Ext(value))
	tokens := strings.FieldsFunc(value, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	if len(tokens) == 0 {
		return nil
	}
	candidates := make([]string, 0, len(tokens)*2)
	for _, token := range tokens {
		if token != "" {
			candidates = append(candidates, token)
		}
	}
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i] != "" && tokens[i+1] != "" {
			candidates = append(candidates, tokens[i]+"_"+tokens[i+1])
		}
	}
	return candidates
}
