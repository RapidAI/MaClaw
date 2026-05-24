package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

type ExperienceProvider struct {
	Workflows []*WorkflowState
}

func NewExperienceProvider(workflows ...*WorkflowState) ExperienceProvider {
	return ExperienceProvider{Workflows: append([]*WorkflowState(nil), workflows...)}
}

func (p ExperienceProvider) ListExperience(_ context.Context, scope lifecycle.Scope) ([]lifecycle.Entry, error) {
	entries := p.entries()
	out := make([]lifecycle.Entry, 0, len(entries))
	for _, entry := range entries {
		if !workflowExperienceTypeAllowed(entry.EntryType, scope.Types) || !workflowExperienceBoundaryAllowed(entry, scope.Boundary) {
			continue
		}
		out = append(out, entry)
		if scope.Limit > 0 && len(out) >= scope.Limit {
			break
		}
	}
	return out, nil
}

func (p ExperienceProvider) SearchExperience(_ context.Context, query lifecycle.Query) ([]lifecycle.Candidate, error) {
	queryText := strings.TrimSpace(query.Text)
	if queryText == "" {
		return nil, nil
	}
	queryTokens := workflowExperienceTokens(queryText)
	candidates := make([]lifecycle.Candidate, 0)
	for _, entry := range p.entries() {
		if !workflowExperienceTypeAllowed(entry.EntryType, query.Types) || !workflowExperienceBoundaryAllowed(entry, query.Boundary) {
			continue
		}
		relevance := workflowExperienceRelevance(queryTokens, entry.WhenToUse+" "+entry.Content)
		if relevance <= 0 {
			continue
		}
		candidates = append(candidates, lifecycle.Candidate{
			Entry:         entry,
			Relevance:     relevance,
			PriorityScore: entry.Priority,
			BoundaryScore: workflowExperienceBoundaryScore(entry, query.Boundary),
			TokenCost:     workflowExperienceTokenCost(entry.Content),
			Reason:        "workflow_provider",
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return workflowExperienceScore(candidates[i]) > workflowExperienceScore(candidates[j])
	})
	return candidates, nil
}

func (p ExperienceProvider) UpdateUtility(_ context.Context, _ lifecycle.UtilityUpdate) error {
	return nil
}

func (p ExperienceProvider) entries() []lifecycle.Entry {
	var out []lifecycle.Entry
	for _, ws := range p.Workflows {
		if ws == nil || ws.Status == WorkflowCancelled {
			continue
		}
		out = append(out, workflowPhaseOutputEntries(ws)...)
		out = append(out, workflowGateFailureEntries(ws)...)
		if entry, ok := workflowReviewRevisionEntry(ws); ok {
			out = append(out, entry)
		}
	}
	return out
}

func workflowPhaseOutputEntries(ws *WorkflowState) []lifecycle.Entry {
	out := make([]lifecycle.Entry, 0, len(ws.PhaseOutputs))
	for phaseID, output := range ws.PhaseOutputs {
		output = strings.TrimSpace(output)
		if output == "" {
			continue
		}
		out = append(out, lifecycle.Entry{
			ID:         workflowExperienceID(ws, "phase", phaseID),
			EntryType:  lifecycle.EntryTypeEpisodic,
			WhenToUse:  fmt.Sprintf("workflow=%s phase=%s output", ws.Type, phaseID),
			Content:    workflowExperienceTrim(fmt.Sprintf("Workflow %s phase %s output:\n%s", ws.Type, phaseID, output), 1200),
			SourceType: "workflow",
			Boundary:   workflowExperienceBoundary(ws),
			Priority:   0.45,
			Governance: lifecycle.GovernanceEvidenceOnly,
		})
	}
	return out
}

func workflowGateFailureEntries(ws *WorkflowState) []lifecycle.Entry {
	out := make([]lifecycle.Entry, 0, len(ws.GateResults))
	for phaseID, gate := range ws.GateResults {
		if gate == nil || gate.Passed {
			continue
		}
		failed := make([]string, 0, len(gate.Items))
		for _, item := range gate.Items {
			if !item.Passed {
				line := item.Description
				if item.Note != "" {
					line += ": " + item.Note
				}
				failed = append(failed, line)
			}
		}
		if len(failed) == 0 {
			failed = append(failed, "quality gate failed")
		}
		out = append(out, lifecycle.Entry{
			ID:         workflowExperienceID(ws, "gate_failure", phaseID),
			EntryType:  lifecycle.EntryTypeFailureSkill,
			WhenToUse:  fmt.Sprintf("avoid repeating workflow=%s phase=%s quality gate failure", ws.Type, phaseID),
			Content:    fmt.Sprintf("Workflow %s phase %s failed quality gate: %s", ws.Type, phaseID, strings.Join(failed, "; ")),
			SourceType: "workflow",
			Boundary:   workflowExperienceBoundary(ws),
			Priority:   0.8,
			Governance: lifecycle.GovernanceDraft,
		})
	}
	return out
}

func workflowReviewRevisionEntry(ws *WorkflowState) (lifecycle.Entry, bool) {
	phaseID := strings.TrimSpace(ws.PendingReviewPhaseID)
	if phaseID == "" || !ws.PendingReviewRevisionRequested {
		return lifecycle.Entry{}, false
	}
	return lifecycle.Entry{
		ID:         workflowExperienceID(ws, "review_revision", phaseID),
		EntryType:  lifecycle.EntryTypeComparativeSkill,
		WhenToUse:  fmt.Sprintf("workflow=%s phase=%s needs revision after review feedback", ws.Type, phaseID),
		Content:    fmt.Sprintf("Workflow %s phase %s should revise the previous output before advancing; user requested supplemental changes.", ws.Type, phaseID),
		SourceType: "workflow",
		Boundary:   workflowExperienceBoundary(ws),
		Priority:   0.7,
		Governance: lifecycle.GovernanceDraft,
	}, true
}

func workflowExperienceID(ws *WorkflowState, kind string, phaseID string) string {
	return "workflow:" + strings.TrimSpace(ws.ID) + ":" + kind + ":" + strings.TrimSpace(phaseID)
}

func workflowExperienceBoundary(ws *WorkflowState) lifecycle.Boundary {
	return lifecycle.Boundary{OwnerID: ws.UserID, ProjectPath: ws.ProjectPath, Workflow: string(ws.Type)}
}

func workflowExperienceBoundaryAllowed(entry lifecycle.Entry, boundary lifecycle.Boundary) bool {
	if boundary.OwnerID != "" && entry.Boundary.OwnerID != "" && boundary.OwnerID != entry.Boundary.OwnerID {
		return false
	}
	if boundary.ProjectPath != "" && entry.Boundary.ProjectPath != "" && !workflowPathMatches(boundary.ProjectPath, entry.Boundary.ProjectPath) {
		return false
	}
	return true
}

func workflowExperienceBoundaryScore(entry lifecycle.Entry, boundary lifecycle.Boundary) float64 {
	score := 0.0
	if boundary.OwnerID != "" && entry.Boundary.OwnerID == boundary.OwnerID {
		score += 0.25
	}
	if boundary.ProjectPath != "" && workflowPathMatches(boundary.ProjectPath, entry.Boundary.ProjectPath) {
		score += 0.35
	}
	return score
}

func workflowPathMatches(left, right string) bool {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	return left == right || strings.Contains(left, right) || strings.Contains(right, left)
}

func workflowExperienceTypeAllowed(entryType lifecycle.EntryType, allowed []lifecycle.EntryType) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, candidateType := range allowed {
		if entryType == candidateType {
			return true
		}
	}
	return false
}

func workflowExperienceRelevance(queryTokens map[string]struct{}, text string) float64 {
	if len(queryTokens) == 0 {
		return 0
	}
	textTokens := workflowExperienceTokens(text)
	if len(textTokens) == 0 {
		return 0
	}
	matches := 0
	for token := range queryTokens {
		if _, ok := textTokens[token]; ok {
			matches++
		}
	}
	if matches == 0 {
		return 0
	}
	return float64(matches) / float64(len(queryTokens))
}

func workflowExperienceTokens(text string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	})
	out := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, "_- ")
		if len([]rune(field)) < 2 {
			continue
		}
		out[field] = struct{}{}
	}
	return out
}

func workflowExperienceTokenCost(text string) int {
	cost := len([]rune(text)) / 4
	if cost <= 0 {
		return 1
	}
	return cost
}

func workflowExperienceScore(candidate lifecycle.Candidate) float64 {
	return candidate.Relevance + candidate.PriorityScore + candidate.BoundaryScore - float64(candidate.TokenCost)/1000
}

func workflowExperienceTrim(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
