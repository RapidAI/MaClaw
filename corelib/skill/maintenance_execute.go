package skill

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	MaintenanceExecutionStatusExecuted = "executed"
	MaintenanceExecutionStatusSkipped  = "skipped"
	MaintenanceExecutionStatusNoop     = "noop"
	MaintenanceExecutionStatusQueued   = "queued"

	MaintenanceExecuteBoundary = "approval-gated skill maintenance execution; only local metadata actions are applied"
)

// SkillMaintenanceExecutionOptions controls approval-gated execution.
type SkillMaintenanceExecutionOptions struct {
	Now                  time.Time
	DryRun               bool
	ApprovedActions      []string
	AllowDuplicateRetire bool
}

// SkillMaintenanceExecutionResult reports the outcome of applying a plan.
type SkillMaintenanceExecutionResult struct {
	OK                   bool                              `json:"ok"`
	DryRun               bool                              `json:"dry_run"`
	Boundary             string                            `json:"boundary"`
	Error                string                            `json:"error,omitempty"`
	ExecutedCount        int                               `json:"executed_count"`
	SkippedCount         int                               `json:"skipped_count"`
	NoopCount            int                               `json:"noop_count"`
	QueuedCount          int                               `json:"queued_count,omitempty"`
	RequiresIndexRefresh bool                              `json:"requires_index_refresh,omitempty"`
	Actions              []SkillMaintenanceExecutionAction `json:"actions"`
}

// SkillMaintenanceExecutionAction is one action execution result.
type SkillMaintenanceExecutionAction struct {
	Action     string                      `json:"action"`
	Skill      string                      `json:"skill"`
	Status     string                      `json:"status"`
	Reason     string                      `json:"reason,omitempty"`
	PatchDraft *SkillMaintenancePatchDraft `json:"patch_draft,omitempty"`
	MergeDraft *SkillMaintenanceMergeDraft `json:"merge_draft,omitempty"`
}

// SkillMaintenancePatchDraft is a non-executing patch suggestion for actions
// that require editing a file-backed skill definition.
type SkillMaintenancePatchDraft struct {
	Kind              string                 `json:"kind"`
	Skill             string                 `json:"skill"`
	SkillDir          string                 `json:"skill_dir,omitempty"`
	TargetFile        string                 `json:"target_file,omitempty"`
	RequiredArgs      []string               `json:"required_args,omitempty"`
	Params            []corelib.NLSkillParam `json:"params,omitempty"`
	SuggestedYAML     string                 `json:"suggested_yaml,omitempty"`
	RecommendedAction string                 `json:"recommended_action"`
	// Repair-only fields (kind=attempt_repair). Never auto-applied.
	ErrorClass string   `json:"error_class,omitempty"`
	LastError  string   `json:"last_error,omitempty"`
	ActionHint string   `json:"action_hint,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
}

// SkillMaintenanceMergeDraft is a non-executing review packet for duplicate
// skill consolidation. It never deletes, disables, or rewrites skills.
type SkillMaintenanceMergeDraft struct {
	Kind              string                       `json:"kind"`
	PrimarySkill      string                       `json:"primary_skill"`
	DuplicateSkill    string                       `json:"duplicate_skill"`
	RecommendedKeep   string                       `json:"recommended_keep"`
	RecommendedRetire string                       `json:"recommended_retire"`
	Reasons           []string                     `json:"reasons,omitempty"`
	PrimarySummary    SkillMaintenanceSkillSummary `json:"primary_summary"`
	DuplicateSummary  SkillMaintenanceSkillSummary `json:"duplicate_summary"`
	RecommendedAction string                       `json:"recommended_action"`
}

// SkillMaintenanceSkillSummary captures enough local evidence for a merge review.
type SkillMaintenanceSkillSummary struct {
	Name         string   `json:"name"`
	Source       string   `json:"source,omitempty"`
	Status       string   `json:"status,omitempty"`
	Description  string   `json:"description,omitempty"`
	Triggers     []string `json:"triggers,omitempty"`
	UsageCount   int      `json:"usage_count"`
	SuccessCount int      `json:"success_count"`
	FailureCount int      `json:"failure_count"`
	SkillDir     string   `json:"skill_dir,omitempty"`
}

// ExecuteSkillMaintenancePlan applies the approval-gated metadata subset of a
// maintenance plan. Repair, merge, and contract rewriting stay skipped here
// because they need dedicated flows with stronger previews and rollback data.
func ExecuteSkillMaintenancePlan(skills []corelib.NLSkillEntry, plan SkillMaintenancePlan, opts SkillMaintenanceExecutionOptions) ([]corelib.NLSkillEntry, SkillMaintenanceExecutionResult) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	out := append([]corelib.NLSkillEntry(nil), skills...)
	approved := maintenanceApprovedActionSet(opts.ApprovedActions)
	result := SkillMaintenanceExecutionResult{
		OK:       true,
		DryRun:   opts.DryRun,
		Boundary: MaintenanceExecuteBoundary,
		Actions:  make([]SkillMaintenanceExecutionAction, 0, len(plan.Actions)),
	}
	if !opts.DryRun && len(approved) == 0 && len(plan.Actions) > 0 {
		result.OK = false
		result.Error = "approved_actions is required when dry_run=false"
		for _, action := range plan.Actions {
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: result.Error})
		}
		return out, result
	}

	for _, action := range plan.Actions {
		if len(approved) > 0 && !approved[maintenanceActionKey(action.Action)] {
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "action was not approved"})
			continue
		}

		skillIndex := findMaintenanceSkill(out, action.Skill)
		if skillIndex < 0 && action.Action != MaintenanceActionRefreshIndex {
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "skill was not found"})
			continue
		}

		switch action.Action {
		case MaintenanceActionMarkNeedsReview:
			if maintenanceStatusIs(out[skillIndex].Status, "needs_review") {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusNoop, Reason: "skill already needs review"})
				continue
			}
			if !opts.DryRun {
				out[skillIndex].Status = "needs_review"
			}
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusExecuted, Reason: "status set to needs_review"})
		case MaintenanceActionRefreshLifecycle:
			if out[skillIndex].RepairAttemptCount == 0 && strings.TrimSpace(out[skillIndex].LastError) == "" {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusNoop, Reason: "lifecycle metadata already clean"})
				continue
			}
			if !opts.DryRun {
				out[skillIndex].RepairAttemptCount = 0
				out[skillIndex].LastError = ""
			}
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusExecuted, Reason: "repair counter and last error cleared"})
		case MaintenanceActionAttemptRepair:
			if isFileBackedMaintenanceSkill(out[skillIndex]) {
				draft := buildFileBackedRepairPatchDraft(out[skillIndex])
				if draft == nil {
					result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusNoop, Reason: "file-backed skill has no repairable last_error for a repair patch draft"})
					continue
				}
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "file-backed skill repair requires a reviewed patch flow", PatchDraft: draft})
				continue
			}
			if !ShouldAttemptRepair(&out[skillIndex]) {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusNoop, Reason: "skill is not currently eligible for self-repair"})
				continue
			}
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusQueued, Reason: "caller should trigger the existing self-repair flow"})
		case MaintenanceActionRefreshIndex:
			result.RequiresIndexRefresh = true
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusExecuted, Reason: "caller should refresh skill indexes after saving"})
		case MaintenanceActionArchiveStale:
			if maintenanceStatusIs(out[skillIndex].Status, "disabled") {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusNoop, Reason: "skill already disabled"})
				continue
			}
			if !corelib.IsLearnedSource(out[skillIndex].Source) {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "only learned or crafted skills can be metadata-archived"})
				continue
			}
			if !opts.DryRun {
				out[skillIndex].Status = "disabled"
				out[skillIndex].LastError = fmt.Sprintf("archived_by_maintenance: %s at %s", action.Reason, opts.Now.Format(time.RFC3339))
			}
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusExecuted, Reason: "stale learned skill disabled without deleting files"})
		case MaintenanceActionImproveContract:
			if isFileBackedMaintenanceSkill(out[skillIndex]) {
				draft := buildContractPatchDraft(out[skillIndex])
				if draft == nil {
					result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusNoop, Reason: "no template placeholders found for contract patch draft"})
					continue
				}
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "file-backed skill contract requires a reviewed patch flow", PatchDraft: draft})
				continue
			}
			required, params := buildMaintenanceContractSuggestion(out[skillIndex])
			if len(params) == 0 {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusNoop, Reason: "no template placeholders found for contract synthesis"})
				continue
			}
			if !opts.DryRun {
				out[skillIndex].RequiredArgs = required
				out[skillIndex].Params = params
			}
			result.RequiresIndexRefresh = true
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusExecuted, Reason: "completed params contract from step templates"})
		case MaintenanceActionMergeDuplicate:
			relatedIndex := findMaintenanceSkill(out, action.RelatedSkill)
			if relatedIndex < 0 {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "related duplicate skill was not found"})
				continue
			}
			draft := buildMergeDuplicateDraft(out[skillIndex], out[relatedIndex], action)
			if !opts.AllowDuplicateRetire {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "duplicate merge requires reviewed consolidation", MergeDraft: draft})
				continue
			}
			retireIndex := findMaintenanceSkill(out, draft.RecommendedRetire)
			if retireIndex < 0 {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "recommended retire skill was not found", MergeDraft: draft})
				continue
			}
			if !corelib.IsLearnedSource(out[retireIndex].Source) {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: "only learned or crafted duplicate skills can be retired by maintenance", MergeDraft: draft})
				continue
			}
			if maintenanceStatusIs(out[retireIndex].Status, "disabled") {
				result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusNoop, Reason: "recommended retire skill already disabled", MergeDraft: draft})
				continue
			}
			if !opts.DryRun {
				out[retireIndex].Status = "disabled"
				out[retireIndex].LastError = fmt.Sprintf("retired_by_maintenance_duplicate: kept %s at %s", draft.RecommendedKeep, opts.Now.Format(time.RFC3339))
			}
			result.RequiresIndexRefresh = true
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusExecuted, Reason: "duplicate skill retired by disabling metadata only", MergeDraft: draft})
		default:
			result.addAction(SkillMaintenanceExecutionAction{Action: action.Action, Skill: action.Skill, Status: MaintenanceExecutionStatusSkipped, Reason: fmt.Sprintf("%s requires a dedicated maintenance flow", action.Action)})
		}
	}

	return out, result
}

func (r *SkillMaintenanceExecutionResult) addAction(action SkillMaintenanceExecutionAction) {
	r.Actions = append(r.Actions, action)
	switch action.Status {
	case MaintenanceExecutionStatusExecuted:
		r.ExecutedCount++
	case MaintenanceExecutionStatusSkipped:
		r.SkippedCount++
	case MaintenanceExecutionStatusNoop:
		r.NoopCount++
	case MaintenanceExecutionStatusQueued:
		r.QueuedCount++
	}
}

func maintenanceApprovedActionSet(actions []string) map[string]bool {
	approved := make(map[string]bool, len(actions))
	for _, action := range actions {
		for _, part := range splitMaintenanceActionNames(action) {
			part = maintenanceActionKey(part)
			if part != "" {
				approved[part] = true
			}
		}
	}
	return approved
}

func splitMaintenanceActionNames(action string) []string {
	return strings.FieldsFunc(action, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	})
}

func maintenanceActionKey(action string) string {
	return strings.ToLower(strings.TrimSpace(action))
}

func findMaintenanceSkill(skills []corelib.NLSkillEntry, name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	for i := range skills {
		if skillDisplayName(skills[i]) == name || skills[i].MatchesName(name) {
			return i
		}
	}
	return -1
}

func isFileBackedMaintenanceSkill(skill corelib.NLSkillEntry) bool {
	return IsFileBackedSkill(skill)
}

func buildContractPatchDraft(skill corelib.NLSkillEntry) *SkillMaintenancePatchDraft {
	required, params := buildMaintenanceContractSuggestion(skill)
	if len(params) == 0 {
		return nil
	}
	return &SkillMaintenancePatchDraft{
		Kind:              MaintenanceActionImproveContract,
		Skill:             skillDisplayName(skill),
		SkillDir:          strings.TrimSpace(skill.SkillDir),
		TargetFile:        "skill.yaml",
		RequiredArgs:      append([]string(nil), required...),
		Params:            cloneMaintenanceParams(params),
		SuggestedYAML:     formatContractPatchYAML(required, params),
		RecommendedAction: "review this contract, then apply it with manage_skill(action=patch) or edit skill.yaml",
	}
}

// buildFileBackedRepairPatchDraft builds a non-executing repair review packet for
// file-backed skills. It never rewrites skill.yaml; operators apply changes after review.
func buildFileBackedRepairPatchDraft(skill corelib.NLSkillEntry) *SkillMaintenancePatchDraft {
	if !isFileBackedMaintenanceSkill(skill) {
		return nil
	}
	lastError := strings.TrimSpace(skill.LastError)
	if lastError == "" {
		return nil
	}
	// Prefer live classification over a stored tag so action hints stay useful.
	classified := ClassifyStepError(0, lastError, lastError, "")
	errorClass := string(classified.Class)
	if errorClass == "" || errorClass == string(ErrUnknown) {
		if tagged := ExtractErrorClass(lastError); tagged != "" {
			errorClass = tagged
		}
	}
	if !IsRepairableError(errorClass) {
		return nil
	}
	hint := strings.TrimSpace(classified.ActionHint)
	if hint == "" {
		hint = extractActionHintFromFormattedError(lastError)
	}
	evidence := skillUsageEvidence(skill)
	evidence = append(evidence,
		"error_class="+errorClass,
		"skill_dir="+strings.TrimSpace(skill.SkillDir),
		"source=file",
	)
	return &SkillMaintenancePatchDraft{
		Kind:              MaintenanceActionAttemptRepair,
		Skill:             skillDisplayName(skill),
		SkillDir:          strings.TrimSpace(skill.SkillDir),
		TargetFile:        "skill.yaml",
		SuggestedYAML:     formatRepairPatchReviewYAML(skill, errorClass, lastError, hint),
		RecommendedAction: "review this repair packet under skill_dir; edit skill.yaml/scripts only after approval; use YAML version restore if a bad edit is applied",
		ErrorClass:        errorClass,
		LastError:         lastError,
		ActionHint:        hint,
		Evidence:          evidence,
	}
}

func extractActionHintFromFormattedError(lastError string) string {
	for _, line := range strings.Split(lastError, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "[action:") {
			return line
		}
	}
	return ""
}

func formatRepairPatchReviewYAML(skill corelib.NLSkillEntry, errorClass, lastError, actionHint string) string {
	var b strings.Builder
	b.WriteString("# file-backed repair patch draft (review only — do not auto-apply)\n")
	b.WriteString("name: ")
	b.WriteString(skillDisplayName(skill))
	b.WriteString("\n")
	b.WriteString("kind: ")
	b.WriteString(MaintenanceActionAttemptRepair)
	b.WriteString("\n")
	b.WriteString("skill_dir: ")
	b.WriteString(strings.TrimSpace(skill.SkillDir))
	b.WriteString("\n")
	b.WriteString("error_class: ")
	b.WriteString(errorClass)
	b.WriteString("\n")
	if actionHint != "" {
		b.WriteString("action_hint: ")
		b.WriteString(strconv.Quote(actionHint))
		b.WriteString("\n")
	}
	b.WriteString("last_error: |\n")
	for _, line := range strings.Split(lastError, "\n") {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	// Conservative, class-specific YAML suggestions operators can copy.
	switch ErrorClass(errorClass) {
	case ErrShebangWindows:
		b.WriteString("suggested_fields:\n")
		b.WriteString("  preferred_shell: bash\n")
	case ErrMissingParam:
		required, params := buildMaintenanceContractSuggestion(skill)
		if len(params) > 0 {
			b.WriteString("suggested_contract:\n")
			b.WriteString(indentYAMLBlock(formatContractPatchYAML(required, params), 2))
		}
	case ErrCommandNotFound, ErrMissingDependency, ErrMissingEnvVar:
		b.WriteString("review_notes:\n")
		b.WriteString("  - verify runtime tools/packages/env vars on PATH for this host\n")
		b.WriteString("  - patch step commands or requirements in skill.yaml / scripts\n")
		b.WriteString("  - re-run skill after install; do not mark healthy until one success\n")
	default:
		b.WriteString("review_notes:\n")
		b.WriteString("  - inspect failed step output and skill.yaml steps\n")
		b.WriteString("  - apply the minimal fix that addresses error_class\n")
		b.WriteString("  - keep Versioner backups before writing skill.yaml\n")
	}
	return b.String()
}

func indentYAMLBlock(text string, spaces int) string {
	pad := strings.Repeat(" ", spaces)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(pad)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func buildMaintenanceContractSuggestion(skill corelib.NLSkillEntry) ([]string, []corelib.NLSkillParam) {
	missingRequired := DetectImplicitRunRequiredArgs(skill.Steps, nil, skill.RequiredArgs, skill.Params)
	required := mergeMaintenanceRequiredArgs(skill.RequiredArgs, missingRequired)
	params := CompleteParamsForRunner(skill.Params, skill.Steps, required)
	if len(params) == len(skill.Params) && len(missingRequired) == 0 {
		return required, nil
	}
	return required, params
}

func mergeMaintenanceRequiredArgs(base []string, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, values := range [][]string{base, extra} {
		for _, value := range values {
			key := canonicalRunVarKey(value)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func buildMergeDuplicateDraft(left, right corelib.NLSkillEntry, action SkillMaintenanceAction) *SkillMaintenanceMergeDraft {
	keep, retire, reasons := chooseMergeKeepSkill(left, right)
	return &SkillMaintenanceMergeDraft{
		Kind:              MaintenanceActionMergeDuplicate,
		PrimarySkill:      skillDisplayName(left),
		DuplicateSkill:    skillDisplayName(right),
		RecommendedKeep:   skillDisplayName(keep),
		RecommendedRetire: skillDisplayName(retire),
		Reasons:           append([]string(nil), reasons...),
		PrimarySummary:    summarizeMaintenanceSkill(left),
		DuplicateSummary:  summarizeMaintenanceSkill(right),
		RecommendedAction: firstNonEmptyMaintenanceString(action.RecommendedAction, "review both skills, merge useful docs/steps manually, then disable the duplicate only after verification"),
	}
}

func chooseMergeKeepSkill(left, right corelib.NLSkillEntry) (corelib.NLSkillEntry, corelib.NLSkillEntry, []string) {
	leftScore := mergeCandidateScore(left)
	rightScore := mergeCandidateScore(right)
	reasons := []string{
		fmt.Sprintf("%s score=%d", skillDisplayName(left), leftScore),
		fmt.Sprintf("%s score=%d", skillDisplayName(right), rightScore),
	}
	if rightScore > leftScore {
		return right, left, reasons
	}
	return left, right, reasons
}

func mergeCandidateScore(skill corelib.NLSkillEntry) int {
	score := skill.SuccessCount*4 + skill.UsageCount - skill.FailureCount*2
	if maintenanceStatusIs(skill.Status, "active") || strings.TrimSpace(skill.Status) == "" {
		score += 5
	}
	if len(skill.Params) > 0 || len(skill.RequiredArgs) > 0 {
		score += 3
	}
	if strings.TrimSpace(skill.Description) != "" {
		score++
	}
	if strings.TrimSpace(skill.SkillDir) != "" {
		score++
	}
	return score
}

func summarizeMaintenanceSkill(skill corelib.NLSkillEntry) SkillMaintenanceSkillSummary {
	return SkillMaintenanceSkillSummary{
		Name:         skillDisplayName(skill),
		Source:       strings.TrimSpace(skill.Source),
		Status:       strings.TrimSpace(skill.Status),
		Description:  strings.TrimSpace(skill.Description),
		Triggers:     append([]string(nil), skill.Triggers...),
		UsageCount:   skill.UsageCount,
		SuccessCount: skill.SuccessCount,
		FailureCount: skill.FailureCount,
		SkillDir:     strings.TrimSpace(skill.SkillDir),
	}
}

func cloneMaintenanceParams(params []corelib.NLSkillParam) []corelib.NLSkillParam {
	out := append([]corelib.NLSkillParam(nil), params...)
	for i := range out {
		out[i].Aliases = append([]string(nil), out[i].Aliases...)
	}
	return out
}

func formatContractPatchYAML(required []string, params []corelib.NLSkillParam) string {
	var b strings.Builder
	if len(required) > 0 {
		b.WriteString("required_args:\n")
		for _, arg := range required {
			b.WriteString("  - ")
			b.WriteString(strconv.Quote(arg))
			b.WriteString("\n")
		}
	}
	b.WriteString("params:\n")
	for _, param := range params {
		b.WriteString("  - name: ")
		b.WriteString(strconv.Quote(param.Name))
		b.WriteString("\n")
		if param.Description != "" {
			b.WriteString("    description: ")
			b.WriteString(strconv.Quote(param.Description))
			b.WriteString("\n")
		}
		if len(param.Aliases) > 0 {
			b.WriteString("    aliases:\n")
			for _, alias := range param.Aliases {
				b.WriteString("      - ")
				b.WriteString(strconv.Quote(alias))
				b.WriteString("\n")
			}
		}
		if param.CLIFlag != "" {
			b.WriteString("    cli_flag: ")
			b.WriteString(strconv.Quote(param.CLIFlag))
			b.WriteString("\n")
		}
		if param.Default != "" {
			b.WriteString("    default: ")
			b.WriteString(strconv.Quote(param.Default))
			b.WriteString("\n")
		}
		if param.Required {
			b.WriteString("    required: true\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
