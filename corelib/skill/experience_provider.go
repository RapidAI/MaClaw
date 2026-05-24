package skill

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

type ExperienceProvider struct {
	Skills []corelib.NLSkillEntry
}

func NewExperienceProvider(skills []corelib.NLSkillEntry) ExperienceProvider {
	return ExperienceProvider{Skills: append([]corelib.NLSkillEntry(nil), skills...)}
}

func (p ExperienceProvider) ListExperience(_ context.Context, scope lifecycle.Scope) ([]lifecycle.Entry, error) {
	out := make([]lifecycle.Entry, 0, len(p.Skills))
	for _, s := range p.Skills {
		entry, ok := skillExperienceEntry(s)
		if !ok || !skillExperienceTypeAllowed(entry.EntryType, scope.Types) {
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
	queryTokens := skillExperienceTokens(queryText)
	candidates := make([]lifecycle.Candidate, 0, len(p.Skills))
	for _, s := range p.Skills {
		entry, ok := skillExperienceEntry(s)
		if !ok || !skillExperienceTypeAllowed(entry.EntryType, query.Types) {
			continue
		}
		relevance := skillExperienceRelevance(queryTokens, skillExperienceSearchText(s))
		if relevance <= 0 {
			continue
		}
		candidates = append(candidates, lifecycle.Candidate{
			Entry:         entry,
			Relevance:     relevance,
			PriorityScore: skillExperiencePriority(s),
			BoundaryScore: skillExperienceBoundaryScore(s, query.Boundary),
			TokenCost:     skillExperienceTokenCost(entry.Content),
			Reason:        "skill_provider",
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return skillExperienceCandidateScore(candidates[i]) > skillExperienceCandidateScore(candidates[j])
	})
	candidates = limitSkillExperienceCandidates(candidates, query.Limit)
	return candidates, nil
}

func (p ExperienceProvider) UpdateUtility(_ context.Context, update lifecycle.UtilityUpdate) error {
	if strings.TrimSpace(update.EntryID) == "" {
		return nil
	}
	for _, s := range p.Skills {
		if skillExperienceID(s) == update.EntryID {
			return nil
		}
	}
	return nil
}

func skillExperienceEntry(s corelib.NLSkillEntry) (lifecycle.Entry, bool) {
	if !skillExperienceVisible(s) {
		return lifecycle.Entry{}, false
	}
	content := skillExperienceContent(s)
	if strings.TrimSpace(content) == "" {
		return lifecycle.Entry{}, false
	}
	return lifecycle.Entry{
		ID:         skillExperienceID(s),
		EntryType:  skillExperienceType(s),
		WhenToUse:  skillExperienceWhenToUse(s),
		Content:    content,
		SourceType: "skill",
		SourceURL:  s.SkillDir,
		Boundary: lifecycle.Boundary{
			ProjectPath: s.SourceProject,
			SourceScope: s.Source,
			Toolchain:   append([]string(nil), s.RequiresTools...),
		},
		Priority:   skillExperiencePriority(s),
		Governance: skillExperienceGovernance(s),
	}, true
}

func skillExperienceVisible(s corelib.NLSkillEntry) bool {
	status := strings.ToLower(strings.TrimSpace(s.Status))
	return strings.TrimSpace(s.Name) != "" && status != "disabled" && status != "blocked"
}

func skillExperienceID(s corelib.NLSkillEntry) string {
	if s.SkillDir != "" {
		return "skill:" + filepath.ToSlash(filepath.Clean(s.SkillDir))
	}
	return "skill:" + strings.ToLower(strings.TrimSpace(s.Name))
}

func skillExperienceType(s corelib.NLSkillEntry) lifecycle.EntryType {
	status := strings.ToLower(strings.TrimSpace(s.Status))
	if status == "needs_review" || s.LastError != "" || (s.FailureCount > 0 && s.SuccessCount == 0) {
		return lifecycle.EntryTypeFailureSkill
	}
	if s.WorkaroundCount > 0 || s.RepairAttemptCount > 0 || len(s.RepairHistory) > 0 {
		return lifecycle.EntryTypeComparativeSkill
	}
	return lifecycle.EntryTypeSuccessSkill
}

func skillExperienceGovernance(s corelib.NLSkillEntry) lifecycle.GovernanceState {
	switch strings.ToLower(strings.TrimSpace(s.Status)) {
	case "needs_review", "needs_setup":
		return lifecycle.GovernanceDraft
	case "disabled", "blocked":
		return lifecycle.GovernanceBlocked
	default:
		return lifecycle.GovernanceReviewed
	}
}

func skillExperienceWhenToUse(s corelib.NLSkillEntry) string {
	parts := make([]string, 0, 2+len(s.Triggers))
	if s.Description != "" {
		parts = append(parts, s.Description)
	}
	if len(s.Triggers) > 0 {
		parts = append(parts, "triggers: "+strings.Join(s.Triggers, ", "))
	}
	if len(s.RequiredArgs) > 0 {
		parts = append(parts, "required_args: "+strings.Join(s.RequiredArgs, ", "))
	}
	return strings.Join(parts, "; ")
}

func skillExperienceContent(s corelib.NLSkillEntry) string {
	parts := []string{"Skill: " + s.Name}
	if s.Description != "" {
		parts = append(parts, "Use: "+s.Description)
	}
	if len(s.Triggers) > 0 {
		parts = append(parts, "Triggers: "+strings.Join(s.Triggers, ", "))
	}
	if IsKnowledgeSkillType(s.Type) && strings.TrimSpace(s.Content) != "" {
		parts = append(parts, "Knowledge: "+strings.TrimSpace(s.Content))
	}
	if s.LastError != "" {
		parts = append(parts, "Recent failure: "+strings.TrimSpace(s.LastError))
	}
	if len(s.RepairHistory) > 0 {
		last := s.RepairHistory[len(s.RepairHistory)-1]
		parts = append(parts, fmt.Sprintf("Repair evidence: class=%s success=%t explanation=%s", last.ErrorClass, last.Success, last.Explanation))
	}
	return strings.Join(parts, "\n")
}

func skillExperienceSearchText(s corelib.NLSkillEntry) string {
	parts := []string{s.Name, s.DirName, s.Description, s.Content, s.LastError}
	parts = append(parts, s.Triggers...)
	parts = append(parts, s.RequiredArgs...)
	parts = append(parts, s.RequiresTools...)
	for _, op := range s.Operations {
		parts = append(parts, op.Name, op.Description)
		parts = append(parts, op.Labels...)
	}
	for _, r := range s.RepairHistory {
		parts = append(parts, r.ErrorClass, r.Explanation)
	}
	return strings.Join(parts, " ")
}

func skillExperienceRelevance(queryTokens map[string]struct{}, text string) float64 {
	if len(queryTokens) == 0 {
		return 0
	}
	textTokens := skillExperienceTokens(text)
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

func skillExperienceTokens(text string) map[string]struct{} {
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

func skillExperiencePriority(s corelib.NLSkillEntry) float64 {
	score := 0.2
	if s.SuccessCount > 0 {
		success := s.SuccessCount
		if success > 10 {
			success = 10
		}
		score += float64(success) / 10
	}
	if s.UsageCount > 0 {
		usage := s.UsageCount
		if usage > 20 {
			usage = 20
		}
		score += float64(usage) / 40
	}
	if strings.EqualFold(s.Status, "needs_review") {
		score -= 0.3
	}
	if s.FailureCount > s.SuccessCount {
		score -= 0.2
	}
	if score < 0 {
		return 0
	}
	return score
}

func skillExperienceBoundaryScore(s corelib.NLSkillEntry, boundary lifecycle.Boundary) float64 {
	if strings.TrimSpace(boundary.ProjectPath) == "" || strings.TrimSpace(s.SourceProject) == "" {
		return 0
	}
	left := strings.ToLower(filepath.Clean(boundary.ProjectPath))
	right := strings.ToLower(filepath.Clean(s.SourceProject))
	if left == right || strings.Contains(left, right) || strings.Contains(right, left) {
		return 0.35
	}
	return 0
}

func skillExperienceTokenCost(text string) int {
	count := len([]rune(text)) / 4
	if count <= 0 {
		return 1
	}
	return count
}

func skillExperienceCandidateScore(candidate lifecycle.Candidate) float64 {
	return candidate.Relevance + candidate.PriorityScore + candidate.BoundaryScore - float64(candidate.TokenCost)/1000
}

func limitSkillExperienceCandidates(candidates []lifecycle.Candidate, limit int) []lifecycle.Candidate {
	if limit <= 0 || len(candidates) <= limit {
		return candidates
	}
	return candidates[:limit]
}

func skillExperienceTypeAllowed(entryType lifecycle.EntryType, allowed []lifecycle.EntryType) bool {
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

func parseSkillExperienceTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return parsed
}
