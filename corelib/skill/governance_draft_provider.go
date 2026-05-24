package skill

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

const GovernanceDraftExecuteBoundary = "reviewed skill governance draft execution; only approved local metadata actions are applied"

type GovernanceDraftExecutionOptions struct {
	Now                  time.Time
	DryRun               bool
	ReviewedDraftIDs     []string
	PlanOptions          SkillMaintenancePlanOptions
	AllowDuplicateRetire bool
}

// GovernanceDraftProvider exposes skill-maintenance recommendations as
// governed lifecycle evidence. It does not mutate skills and does not run repair;
// it only turns repair/merge/contract/archive signals into reviewable drafts.
type GovernanceDraftProvider struct {
	Skills []corelib.NLSkillEntry
	Plan   SkillMaintenancePlan
}

func NewGovernanceDraftProvider(skills []corelib.NLSkillEntry, opts SkillMaintenancePlanOptions) GovernanceDraftProvider {
	cp := append([]corelib.NLSkillEntry(nil), skills...)
	return GovernanceDraftProvider{Skills: cp, Plan: BuildSkillMaintenancePlan(cp, opts)}
}

func (p GovernanceDraftProvider) ListExperience(_ context.Context, scope lifecycle.Scope) ([]lifecycle.Entry, error) {
	out := make([]lifecycle.Entry, 0, len(p.Plan.Actions))
	for _, action := range p.Plan.Actions {
		entry, ok := governanceDraftEntry(action, p.Skills)
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

func (p GovernanceDraftProvider) SearchExperience(_ context.Context, query lifecycle.Query) ([]lifecycle.Candidate, error) {
	queryText := strings.TrimSpace(query.Text)
	if queryText == "" {
		return nil, nil
	}
	queryTokens := skillExperienceTokens(queryText)
	candidates := make([]lifecycle.Candidate, 0, len(p.Plan.Actions))
	for _, action := range p.Plan.Actions {
		entry, ok := governanceDraftEntry(action, p.Skills)
		if !ok || !skillExperienceTypeAllowed(entry.EntryType, query.Types) {
			continue
		}
		relevance := skillExperienceRelevance(queryTokens, governanceDraftSearchText(entry, action))
		if relevance <= 0 {
			continue
		}
		candidates = append(candidates, lifecycle.Candidate{
			Entry:         entry,
			Relevance:     relevance,
			PriorityScore: governanceDraftPriority(action),
			BoundaryScore: governanceDraftBoundaryScore(entry.Boundary, query.Boundary),
			TokenCost:     skillExperienceTokenCost(entry.Content),
			Reason:        "skill_governance_draft_provider",
		})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return skillExperienceCandidateScore(candidates[i]) > skillExperienceCandidateScore(candidates[j])
	})
	candidates = limitSkillExperienceCandidates(candidates, query.Limit)
	return candidates, nil
}

func (p GovernanceDraftProvider) UpdateUtility(_ context.Context, update lifecycle.UtilityUpdate) error {
	return nil
}

func ExecuteReviewedGovernanceDrafts(skills []corelib.NLSkillEntry, opts GovernanceDraftExecutionOptions) ([]corelib.NLSkillEntry, SkillMaintenanceExecutionResult) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	planOpts := opts.PlanOptions
	planOpts.Now = opts.Now
	plan := BuildSkillMaintenancePlan(skills, planOpts)
	selected, missing := selectGovernanceDraftActions(plan, opts.ReviewedDraftIDs, opts.DryRun)
	if !opts.DryRun && len(governanceDraftIDSet(opts.ReviewedDraftIDs)) == 0 {
		return append([]corelib.NLSkillEntry(nil), skills...), SkillMaintenanceExecutionResult{
			OK:       false,
			DryRun:   false,
			Boundary: GovernanceDraftExecuteBoundary,
			Error:    "reviewed_draft_ids is required when dry_run=false",
		}
	}
	if len(missing) > 0 {
		result := SkillMaintenanceExecutionResult{
			OK:       false,
			DryRun:   opts.DryRun,
			Boundary: GovernanceDraftExecuteBoundary,
			Error:    "one or more reviewed draft ids were not found in the current governance plan",
			Actions:  make([]SkillMaintenanceExecutionAction, 0, len(selected.Actions)+len(missing)),
		}
		for _, action := range selected.Actions {
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: result.Error})
		}
		for _, draftID := range missing {
			result.addAction(SkillMaintenanceExecutionAction{Action: "reviewed_draft", Skill: draftID, Status: MaintenanceExecutionStatusSkipped, Reason: "reviewed draft id was not found in the current governance plan"})
		}
		return append([]corelib.NLSkillEntry(nil), skills...), result
	}
	approved := make([]string, 0, len(selected.Actions))
	for _, action := range selected.Actions {
		approved = append(approved, action.Action)
	}
	updated, result := ExecuteSkillMaintenancePlan(skills, selected, SkillMaintenanceExecutionOptions{
		Now:                  opts.Now,
		DryRun:               opts.DryRun,
		ApprovedActions:      approved,
		AllowDuplicateRetire: opts.AllowDuplicateRetire,
	})
	result.Boundary = GovernanceDraftExecuteBoundary
	return updated, result
}

func selectGovernanceDraftActions(plan SkillMaintenancePlan, draftIDs []string, includeAllWhenEmpty bool) (SkillMaintenancePlan, []string) {
	selected := SkillMaintenancePlan{GeneratedAt: plan.GeneratedAt, Summary: plan.Summary}
	wanted := governanceDraftIDSet(draftIDs)
	missing := make([]string, 0)
	for _, draftID := range draftIDs {
		if strings.TrimSpace(draftID) == "" {
			missing = append(missing, "<empty>")
		}
	}
	matched := map[string]bool{}
	for _, action := range plan.Actions {
		id := governanceDraftID(action)
		if len(wanted) == 0 && len(draftIDs) == 0 && includeAllWhenEmpty {
			selected.Actions = append(selected.Actions, action)
			continue
		}
		if wanted[id] {
			matched[id] = true
			selected.Actions = append(selected.Actions, action)
		}
	}
	for id := range wanted {
		if !matched[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	return selected, missing
}

func governanceDraftIDSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func governanceDraftEntry(action SkillMaintenanceAction, skills []corelib.NLSkillEntry) (lifecycle.Entry, bool) {
	skillName := strings.TrimSpace(action.Skill)
	if skillName == "" || strings.TrimSpace(action.Action) == "" {
		return lifecycle.Entry{}, false
	}
	entryType := lifecycle.EntryTypeComparativeSkill
	if action.Action == MaintenanceActionMarkNeedsReview || action.Action == MaintenanceActionArchiveStale {
		entryType = lifecycle.EntryTypeFailureSkill
	}
	skillEntry, _ := governanceDraftSkill(skills, skillName)
	content := governanceDraftContent(action)
	if strings.TrimSpace(content) == "" {
		return lifecycle.Entry{}, false
	}
	return lifecycle.Entry{
		ID:         governanceDraftID(action),
		EntryType:  entryType,
		WhenToUse:  governanceDraftWhenToUse(action),
		Content:    content,
		SourceType: "skill_governance_draft",
		SourceURL:  skillEntry.SkillDir,
		Boundary: lifecycle.Boundary{
			ProjectPath: skillEntry.SourceProject,
			SourceScope: "skill_governance",
			TaskType:    action.Action,
			Toolchain:   append([]string(nil), skillEntry.RequiresTools...),
		},
		Priority:   governanceDraftPriority(action),
		Governance: lifecycle.GovernanceDraft,
	}, true
}

func governanceDraftID(action SkillMaintenanceAction) string {
	parts := []string{"skill_draft", strings.TrimSpace(action.Action), strings.TrimSpace(action.Skill), strings.TrimSpace(action.RelatedSkill)}
	for i := range parts {
		parts[i] = strings.ToLower(strings.ReplaceAll(parts[i], " ", "-"))
	}
	return strings.Join(parts, ":")
}

func governanceDraftContent(action SkillMaintenanceAction) string {
	parts := []string{
		"Skill governance draft",
		"Action: " + action.Action,
		"Skill: " + action.Skill,
	}
	if action.RelatedSkill != "" {
		parts = append(parts, "Related skill: "+action.RelatedSkill)
	}
	if action.Risk != "" {
		parts = append(parts, "Risk: "+action.Risk)
	}
	if action.Reason != "" {
		parts = append(parts, "Reason: "+action.Reason)
	}
	if len(action.Evidence) > 0 {
		parts = append(parts, "Evidence: "+strings.Join(action.Evidence, "; "))
	}
	if action.RecommendedAction != "" {
		parts = append(parts, "Recommended review action: "+action.RecommendedAction)
	}
	parts = append(parts, "Governance: draft only; review required before repair, merge, archive, or promotion.")
	return strings.Join(parts, "\n")
}

func governanceDraftWhenToUse(action SkillMaintenanceAction) string {
	return fmt.Sprintf("Review %s for skill %s when similar failure, repair, duplicate, lifecycle, or contract evidence appears.", action.Action, action.Skill)
}

func governanceDraftSearchText(entry lifecycle.Entry, action SkillMaintenanceAction) string {
	return strings.Join([]string{entry.ID, entry.WhenToUse, entry.Content, action.RelatedSkill}, " ")
}

func governanceDraftPriority(action SkillMaintenanceAction) float64 {
	score := 0.45
	switch action.Action {
	case MaintenanceActionAttemptRepair, MaintenanceActionMarkNeedsReview:
		score += 0.35
	case MaintenanceActionImproveContract, MaintenanceActionRefreshLifecycle, MaintenanceActionRefreshIndex:
		score += 0.2
	case MaintenanceActionMergeDuplicate, MaintenanceActionArchiveStale:
		score += 0.15
	}
	if action.Risk == MaintenanceRiskMedium {
		score += 0.1
	}
	if len(action.Evidence) > 2 {
		score += 0.05
	}
	if score > 1 {
		return 1
	}
	return score
}

func governanceDraftBoundaryScore(entryBoundary lifecycle.Boundary, queryBoundary lifecycle.Boundary) float64 {
	if strings.TrimSpace(entryBoundary.ProjectPath) == "" || strings.TrimSpace(queryBoundary.ProjectPath) == "" {
		return 0
	}
	left := strings.ToLower(strings.TrimSpace(entryBoundary.ProjectPath))
	right := strings.ToLower(strings.TrimSpace(queryBoundary.ProjectPath))
	if left == right || strings.Contains(left, right) || strings.Contains(right, left) {
		return 0.5
	}
	return 0
}

func governanceDraftSkill(skills []corelib.NLSkillEntry, name string) (corelib.NLSkillEntry, bool) {
	for _, s := range skills {
		if skillDisplayName(s) == name || s.MatchesName(name) {
			return s, true
		}
	}
	return corelib.NLSkillEntry{}, false
}
