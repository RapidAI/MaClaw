package skill

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	MaintenanceActionMarkNeedsReview  = "mark_needs_review"
	MaintenanceActionAttemptRepair    = "attempt_repair"
	MaintenanceActionMergeDuplicate   = "merge_duplicate"
	MaintenanceActionArchiveStale     = "archive_stale"
	MaintenanceActionRefreshLifecycle = "refresh_lifecycle"
	MaintenanceActionImproveContract  = "improve_contract"
	MaintenanceActionRefreshIndex     = "refresh_index"

	MaintenanceRiskLow    = "low"
	MaintenanceRiskMedium = "medium"
)

// SkillMaintenancePlan is a read-only curator report for installed skills.
// It does not mutate skills; execution is intentionally left to a later,
// approval-gated step.
type SkillMaintenancePlan struct {
	GeneratedAt string                   `json:"generated_at"`
	Summary     string                   `json:"summary"`
	Actions     []SkillMaintenanceAction `json:"actions"`
}

// SkillMaintenanceAction describes one recommended skill maintenance action.
type SkillMaintenanceAction struct {
	Action            string   `json:"action"`
	Skill             string   `json:"skill"`
	RelatedSkill      string   `json:"related_skill,omitempty"`
	Risk              string   `json:"risk"`
	Reason            string   `json:"reason"`
	Evidence          []string `json:"evidence,omitempty"`
	RecommendedAction string   `json:"recommended_action"`
}

// SkillMaintenancePlanOptions configures BuildSkillMaintenancePlan.
type SkillMaintenancePlanOptions struct {
	Now                 time.Time
	StaleAfterDays      int
	MinFailureRuns      int
	DuplicateSimilarity float64
	MaxActions          int
}

func defaultMaintenancePlanOptions(opts SkillMaintenancePlanOptions) SkillMaintenancePlanOptions {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.StaleAfterDays <= 0 {
		opts.StaleAfterDays = 90
	}
	if opts.MinFailureRuns <= 0 {
		opts.MinFailureRuns = 3
	}
	if opts.DuplicateSimilarity <= 0 {
		opts.DuplicateSimilarity = 0.82
	}
	if opts.MaxActions <= 0 {
		opts.MaxActions = 50
	}
	return opts
}

// BuildSkillMaintenancePlan builds a deterministic, read-only curator plan.
// It closes the maintenance part of the self-evolution loop: local execution
// signals are turned into review, merge, archive, lifecycle, and contract
// recommendations without changing the skill library.
func BuildSkillMaintenancePlan(skills []corelib.NLSkillEntry, opts SkillMaintenancePlanOptions) SkillMaintenancePlan {
	opts = defaultMaintenancePlanOptions(opts)
	actions := make([]SkillMaintenanceAction, 0)

	for i := range skills {
		s := skills[i]
		if shouldSkipCuratorSkill(s) {
			continue
		}
		if action, ok := failedSkillMaintenanceAction(s, opts.MinFailureRuns); ok {
			actions = append(actions, action)
		}
		if action, ok := staleSkillMaintenanceAction(s, opts.Now, opts.StaleAfterDays); ok {
			actions = append(actions, action)
		}
		if action, ok := lifecycleMaintenanceAction(s); ok {
			actions = append(actions, action)
		}
		if action, ok := refreshIndexMaintenanceAction(s); ok {
			actions = append(actions, action)
		}
		if action, ok := contractMaintenanceAction(s); ok {
			actions = append(actions, action)
		}
	}

	actions = append(actions, duplicateSkillMaintenanceActions(skills, opts.DuplicateSimilarity)...)
	sortMaintenanceActions(actions)
	if len(actions) > opts.MaxActions {
		actions = actions[:opts.MaxActions]
	}

	return SkillMaintenancePlan{
		GeneratedAt: opts.Now.Format(time.RFC3339),
		Summary:     maintenancePlanSummary(actions),
		Actions:     actions,
	}
}

func shouldSkipCuratorSkill(s corelib.NLSkillEntry) bool {
	return skillDisplayName(s) == "" || IsKnowledgeSkillType(s.Type) || IsInstructionOnlySkillType(s.Type) || maintenanceStatusIs(s.Status, "disabled")
}

func failedSkillMaintenanceAction(s corelib.NLSkillEntry, minRuns int) (SkillMaintenanceAction, bool) {
	if maintenanceStatusIs(s.Status, "disabled") || maintenanceStatusIs(s.Status, "needs_review") {
		return SkillMaintenanceAction{}, false
	}
	if s.UsageCount < minRuns {
		return SkillMaintenanceAction{}, false
	}
	if action, ok := repairableFailedSkillMaintenanceAction(s); ok {
		return action, true
	}
	if s.SuccessCount == 0 {
		return SkillMaintenanceAction{
			Action:            MaintenanceActionMarkNeedsReview,
			Skill:             skillDisplayName(s),
			Risk:              MaintenanceRiskMedium,
			Reason:            fmt.Sprintf("0/%d successful runs", s.UsageCount),
			Evidence:          skillUsageEvidence(s),
			RecommendedAction: "mark skill status as needs_review before future automatic routing",
		}, true
	}
	failureRate := float64(s.FailureCount) / float64(maxInt(s.UsageCount, 1))
	if s.UsageCount >= minRuns*2 && failureRate >= 0.75 {
		return SkillMaintenanceAction{
			Action:            MaintenanceActionMarkNeedsReview,
			Skill:             skillDisplayName(s),
			Risk:              MaintenanceRiskMedium,
			Reason:            fmt.Sprintf("failure rate %.0f%% across %d runs", failureRate*100, s.UsageCount),
			Evidence:          skillUsageEvidence(s),
			RecommendedAction: "review recent failures and repair or disable the skill",
		}, true
	}
	return SkillMaintenanceAction{}, false
}

func repairableFailedSkillMaintenanceAction(s corelib.NLSkillEntry) (SkillMaintenanceAction, bool) {
	lastError := strings.TrimSpace(s.LastError)
	if lastError == "" {
		return SkillMaintenanceAction{}, false
	}
	errorClass := ExtractErrorClass(lastError)
	if !IsRepairableError(errorClass) {
		return SkillMaintenanceAction{}, false
	}
	// File-backed skills never auto-rewrite disk definitions. Surface a reviewed
	// repair patch draft so operators/GUI can human-approve edits under skill_dir.
	if isFileBackedMaintenanceSkill(s) {
		return SkillMaintenanceAction{
			Action: MaintenanceActionAttemptRepair,
			Skill:  skillDisplayName(s),
			Risk:   MaintenanceRiskLow,
			Reason: "file-backed skill has a repairable failure; generate a review-only repair patch draft",
			Evidence: append(skillUsageEvidence(s),
				"error_class="+errorClass,
				"skill_dir="+strings.TrimSpace(s.SkillDir),
				"source=file",
			),
			RecommendedAction: "open the file-backed repair patch draft, review skill.yaml/scripts, then apply manually or via approved YAML restore flow",
		}, true
	}
	if s.RepairAttemptCount >= SelfRepairMaxAttempts {
		return SkillMaintenanceAction{}, false
	}
	return SkillMaintenanceAction{
		Action: MaintenanceActionAttemptRepair,
		Skill:  skillDisplayName(s),
		Risk:   MaintenanceRiskLow,
		Reason: "recent failure is classified as repairable and self-repair attempts remain",
		Evidence: append(skillUsageEvidence(s),
			"error_class="+errorClass,
			fmt.Sprintf("repair_attempt_count=%d", s.RepairAttemptCount),
		),
		RecommendedAction: "let self-repair run before marking this skill as needs_review",
	}, true
}

func staleSkillMaintenanceAction(s corelib.NLSkillEntry, now time.Time, staleAfterDays int) (SkillMaintenanceAction, bool) {
	if !corelib.IsLearnedSource(s.Source) {
		return SkillMaintenanceAction{}, false
	}
	last := parseSkillTime(firstNonEmptyMaintenanceString(s.LastUsedAt, s.CreatedAt))
	if last.IsZero() || now.Sub(last) < time.Duration(staleAfterDays)*24*time.Hour {
		return SkillMaintenanceAction{}, false
	}
	if s.UsageCount > 0 && s.SuccessCount > 0 {
		return SkillMaintenanceAction{}, false
	}
	return SkillMaintenanceAction{
		Action:            MaintenanceActionArchiveStale,
		Skill:             skillDisplayName(s),
		Risk:              MaintenanceRiskMedium,
		Reason:            fmt.Sprintf("learned skill has no successful use for at least %d days", staleAfterDays),
		Evidence:          []string{"source=" + s.Source, "last_seen=" + last.Format(time.RFC3339)},
		RecommendedAction: "archive after user approval; keep backup for restore",
	}, true
}

func lifecycleMaintenanceAction(s corelib.NLSkillEntry) (SkillMaintenanceAction, bool) {
	if s.RepairAttemptCount <= 0 || len(s.RepairHistory) == 0 {
		return SkillMaintenanceAction{}, false
	}
	last := s.RepairHistory[len(s.RepairHistory)-1]
	if !last.Success && s.SuccessCount == 0 {
		return SkillMaintenanceAction{}, false
	}
	return SkillMaintenanceAction{
		Action:            MaintenanceActionRefreshLifecycle,
		Skill:             skillDisplayName(s),
		Risk:              MaintenanceRiskLow,
		Reason:            "skill has repair history and later success evidence",
		Evidence:          []string{fmt.Sprintf("repair_attempt_count=%d", s.RepairAttemptCount), "last_repair_at=" + s.LastRepairAt},
		RecommendedAction: "mark latest repair verified and reset consecutive repair counter",
	}, true
}

func refreshIndexMaintenanceAction(s corelib.NLSkillEntry) (SkillMaintenanceAction, bool) {
	if strings.TrimSpace(s.LastRepairAt) == "" || strings.TrimSpace(s.SkillDir) == "" {
		return SkillMaintenanceAction{}, false
	}
	return SkillMaintenanceAction{
		Action:            MaintenanceActionRefreshIndex,
		Skill:             skillDisplayName(s),
		Risk:              MaintenanceRiskLow,
		Reason:            "skill was repaired or updated and should refresh routing/prompt indexes",
		Evidence:          []string{"last_repair_at=" + s.LastRepairAt, "skill_dir=" + s.SkillDir},
		RecommendedAction: "refresh skill scan cache, router skill index, and prompt skill summary before the next run",
	}, true
}

func contractMaintenanceAction(s corelib.NLSkillEntry) (SkillMaintenanceAction, bool) {
	if maintenanceStatusIs(s.Status, "disabled") || len(s.Steps) == 0 {
		return SkillMaintenanceAction{}, false
	}
	_, params := buildMaintenanceContractSuggestion(s)
	if len(params) == 0 || len(params) == len(s.Params) {
		return SkillMaintenanceAction{}, false
	}
	return SkillMaintenanceAction{
		Action:            MaintenanceActionImproveContract,
		Skill:             skillDisplayName(s),
		Risk:              MaintenanceRiskLow,
		Reason:            "executable skill has steps but incomplete parameter contract",
		Evidence:          []string{fmt.Sprintf("steps=%d", len(s.Steps)), fmt.Sprintf("params=%d", len(s.Params)), fmt.Sprintf("required_args=%d", len(s.RequiredArgs)), fmt.Sprintf("detected_params=%d", len(params)-len(s.Params))},
		RecommendedAction: "add params schema with names, aliases, defaults, required flags, and CLI mapping",
	}, true
}

func duplicateSkillMaintenanceActions(skills []corelib.NLSkillEntry, threshold float64) []SkillMaintenanceAction {
	actions := make([]SkillMaintenanceAction, 0)
	for i := 0; i < len(skills); i++ {
		left := skills[i]
		if !corelib.IsLearnedSource(left.Source) || shouldSkipCuratorSkill(left) {
			continue
		}
		for j := i + 1; j < len(skills); j++ {
			right := skills[j]
			if !corelib.IsLearnedSource(right.Source) || shouldSkipCuratorSkill(right) {
				continue
			}
			sim := skillTextSimilarity(left, right)
			if sim < threshold {
				continue
			}
			actions = append(actions, SkillMaintenanceAction{
				Action:            MaintenanceActionMergeDuplicate,
				Skill:             skillDisplayName(left),
				RelatedSkill:      skillDisplayName(right),
				Risk:              MaintenanceRiskLow,
				Reason:            "learned/crafted skills look semantically duplicated",
				Evidence:          []string{fmt.Sprintf("similarity=%.2f", sim), "left_source=" + left.Source, "right_source=" + right.Source},
				RecommendedAction: "review both skills and merge the better steps/docs into one canonical skill",
			})
		}
	}
	return actions
}

func skillTextSimilarity(a, b corelib.NLSkillEntry) float64 {
	left := maintenanceTokenSet(a.Name + " " + a.Description + " " + strings.Join(a.Triggers, " "))
	right := maintenanceTokenSet(b.Name + " " + b.Description + " " + strings.Join(b.Triggers, " "))
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	var intersection int
	for token := range left {
		if right[token] {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	union := len(left) + len(right) - intersection
	return float64(intersection) / float64(union)
}

func maintenanceTokenSet(text string) map[string]bool {
	tokens := make(map[string]bool)
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		token := strings.ToLower(current.String())
		if len([]rune(token)) >= 2 {
			tokens[token] = true
		}
		current.Reset()
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r >= 0x4E00 && r <= 0x9FFF {
				flush()
				tokens[string(r)] = true
				continue
			}
			current.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func sortMaintenanceActions(actions []SkillMaintenanceAction) {
	sort.SliceStable(actions, func(i, j int) bool {
		if maintenanceRiskRank(actions[i].Risk) != maintenanceRiskRank(actions[j].Risk) {
			return maintenanceRiskRank(actions[i].Risk) > maintenanceRiskRank(actions[j].Risk)
		}
		if actions[i].Action != actions[j].Action {
			return actions[i].Action < actions[j].Action
		}
		if actions[i].Skill != actions[j].Skill {
			return actions[i].Skill < actions[j].Skill
		}
		return actions[i].RelatedSkill < actions[j].RelatedSkill
	})
}

func maintenanceRiskRank(risk string) int {
	switch risk {
	case MaintenanceRiskMedium:
		return 2
	case MaintenanceRiskLow:
		return 1
	default:
		return 0
	}
}

func maintenancePlanSummary(actions []SkillMaintenanceAction) string {
	if len(actions) == 0 {
		return "0 actions, skill library looks healthy"
	}
	highest := MaintenanceRiskLow
	for _, action := range actions {
		if maintenanceRiskRank(action.Risk) > maintenanceRiskRank(highest) {
			highest = action.Risk
		}
	}
	return fmt.Sprintf("%d actions, highest risk %s", len(actions), highest)
}

func skillUsageEvidence(s corelib.NLSkillEntry) []string {
	return []string{
		fmt.Sprintf("usage_count=%d", s.UsageCount),
		fmt.Sprintf("success_count=%d", s.SuccessCount),
		fmt.Sprintf("failure_count=%d", s.FailureCount),
	}
}

func skillDisplayName(s corelib.NLSkillEntry) string {
	if strings.TrimSpace(s.Name) != "" {
		return strings.TrimSpace(s.Name)
	}
	return strings.TrimSpace(s.DirName)
}

func parseSkillTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func firstNonEmptyMaintenanceString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maintenanceStatusIs(status, want string) bool {
	return strings.EqualFold(strings.TrimSpace(status), want)
}
