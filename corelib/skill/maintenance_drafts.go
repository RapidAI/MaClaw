package skill

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"gopkg.in/yaml.v3"
)

// MaintenanceReviewDrafts is a read-only snapshot of patch/merge drafts that
// require human review before any file or registry mutation.
type MaintenanceReviewDrafts struct {
	GeneratedAt  string                            `json:"generated_at"`
	PlanSummary  string                            `json:"plan_summary"`
	PlanActions  int                               `json:"plan_actions"`
	PatchDrafts  []SkillMaintenancePatchDraft      `json:"patch_drafts"`
	MergeDrafts  []SkillMaintenanceMergeDraft      `json:"merge_drafts"`
	QueuedRepair []SkillMaintenanceExecutionAction `json:"queued_repair,omitempty"`
}

// CollectMaintenanceReviewDrafts builds a local maintenance plan and dry-runs
// execution to surface file-backed patch drafts, merge review packets, and
// repair-queued items. It never mutates skills or disk files.
func CollectMaintenanceReviewDrafts(skills []corelib.NLSkillEntry, opts SkillMaintenancePlanOptions) MaintenanceReviewDrafts {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	plan := BuildSkillMaintenancePlan(skills, opts)
	_, result := ExecuteSkillMaintenancePlan(skills, plan, SkillMaintenanceExecutionOptions{
		Now:    opts.Now,
		DryRun: true,
		// Empty approved set is allowed in dry_run — all plan actions are simulated.
	})

	out := MaintenanceReviewDrafts{
		GeneratedAt: opts.Now.UTC().Format(time.RFC3339),
		PlanSummary: plan.Summary,
		PlanActions: len(plan.Actions),
		PatchDrafts: make([]SkillMaintenancePatchDraft, 0),
		MergeDrafts: make([]SkillMaintenanceMergeDraft, 0),
	}
	for _, action := range result.Actions {
		if action.PatchDraft != nil {
			out.PatchDrafts = append(out.PatchDrafts, *action.PatchDraft)
		}
		if action.MergeDraft != nil {
			out.MergeDrafts = append(out.MergeDrafts, *action.MergeDraft)
		}
		if action.Status == MaintenanceExecutionStatusQueued && action.Action == MaintenanceActionAttemptRepair {
			out.QueuedRepair = append(out.QueuedRepair, action)
		}
	}
	return out
}

// TargetedMaintenanceApplyResult is the outcome of applying one approved draft action.
type TargetedMaintenanceApplyResult struct {
	OK                   bool                            `json:"ok"`
	DryRun               bool                            `json:"dry_run"`
	Error                string                          `json:"error,omitempty"`
	Action               string                          `json:"action"`
	Skill                string                          `json:"skill,omitempty"`
	RelatedSkill         string                          `json:"related_skill,omitempty"`
	Result               SkillMaintenanceExecutionResult `json:"result"`
	RequiresIndexRefresh bool                            `json:"requires_index_refresh,omitempty"`
	PatchDraft           *SkillMaintenancePatchDraft     `json:"patch_draft,omitempty"`
	MergeDraft           *SkillMaintenanceMergeDraft     `json:"merge_draft,omitempty"`
	Message              string                          `json:"message,omitempty"`
	BackupVersion        int                             `json:"backup_version,omitempty"`
	WrittenPath          string                          `json:"written_path,omitempty"`
}

// ApplyTargetedMaintenanceAction runs a single maintenance action against skills.
// It is approval-gated: confirm must be true and dryRun false for mutations.
// Supported actions: improve_contract, merge_duplicate (with allowDuplicateRetire).
func ApplyTargetedMaintenanceAction(
	skills []corelib.NLSkillEntry,
	action, skillName, relatedSkill string,
	dryRun, confirm, allowDuplicateRetire bool,
) ([]corelib.NLSkillEntry, TargetedMaintenanceApplyResult) {
	action = strings.ToLower(strings.TrimSpace(action))
	skillName = strings.TrimSpace(skillName)
	relatedSkill = strings.TrimSpace(relatedSkill)
	out := append([]corelib.NLSkillEntry(nil), skills...)
	res := TargetedMaintenanceApplyResult{
		DryRun:       dryRun,
		Action:       action,
		Skill:        skillName,
		RelatedSkill: relatedSkill,
	}
	if skillName == "" {
		res.Error = "skill is required"
		return out, res
	}
	if action != MaintenanceActionImproveContract && action != MaintenanceActionMergeDuplicate {
		res.Error = fmt.Sprintf("unsupported action %q (supported: improve_contract, merge_duplicate)", action)
		return out, res
	}
	if !dryRun && !confirm {
		res.Error = "confirm=true is required when dry_run=false"
		return out, res
	}
	if action == MaintenanceActionMergeDuplicate {
		if relatedSkill == "" {
			// Allow relatedSkill to be the retire candidate passed as related_skill.
			res.Error = "related_skill is required for merge_duplicate (the other skill in the pair)"
			return out, res
		}
		if !dryRun && !allowDuplicateRetire {
			res.Error = "allow_duplicate_retire=true is required to retire a duplicate"
			return out, res
		}
	}

	plan := SkillMaintenancePlan{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Summary:     "targeted single-action apply",
		Actions: []SkillMaintenanceAction{{
			Action:       action,
			Skill:        skillName,
			RelatedSkill: relatedSkill,
		}},
	}
	updated, execResult := ExecuteSkillMaintenancePlan(out, plan, SkillMaintenanceExecutionOptions{
		Now:                  time.Now(),
		DryRun:               dryRun,
		ApprovedActions:      []string{action},
		AllowDuplicateRetire: allowDuplicateRetire,
	})
	res.Result = execResult
	res.RequiresIndexRefresh = execResult.RequiresIndexRefresh
	res.OK = execResult.OK
	if execResult.Error != "" {
		res.Error = execResult.Error
	}
	for _, a := range execResult.Actions {
		if a.PatchDraft != nil {
			res.PatchDraft = a.PatchDraft
		}
		if a.MergeDraft != nil {
			res.MergeDraft = a.MergeDraft
		}
		if a.Status == MaintenanceExecutionStatusSkipped && res.Message == "" {
			res.Message = a.Reason
		}
		if a.Status == MaintenanceExecutionStatusExecuted {
			res.Message = a.Reason
		}
		if a.Status == MaintenanceExecutionStatusNoop && res.Message == "" {
			res.Message = a.Reason
		}
	}
	if !dryRun && execResult.OK && execResult.ExecutedCount == 0 && res.Error == "" {
		// File-backed improve_contract: controlled YAML write with Versioner backup.
		if action == MaintenanceActionImproveContract && res.PatchDraft != nil {
			idx := findMaintenanceSkill(updated, skillName)
			if idx >= 0 && isFileBackedMaintenanceSkill(updated[idx]) {
				ver, path, err := ApplyFileBackedContractPatch(&updated[idx], res.PatchDraft, &Versioner{})
				if err != nil {
					res.OK = false
					res.Error = err.Error()
					res.Message = "file-backed contract write failed"
					return updated, res
				}
				res.OK = true
				res.BackupVersion = ver
				res.WrittenPath = path
				res.RequiresIndexRefresh = true
				res.Message = fmt.Sprintf("wrote contract to %s (backup v%d)", path, ver)
				res.Result.ExecutedCount = 1
				res.Result.SkippedCount = 0
				// Reflect success on the first matching action row.
				for i := range res.Result.Actions {
					if res.Result.Actions[i].Action == MaintenanceActionImproveContract {
						res.Result.Actions[i].Status = MaintenanceExecutionStatusExecuted
						res.Result.Actions[i].Reason = res.Message
						break
					}
				}
				return updated, res
			}
			res.OK = false
			res.Error = "file-backed skill requires manual YAML apply; patch_draft returned for review"
			if res.Message == "" {
				res.Message = "copy suggested_yaml into skill.yaml after review"
			}
		} else if res.MergeDraft != nil && !allowDuplicateRetire {
			res.OK = false
			res.Error = "merge not applied; set allow_duplicate_retire=true after review"
		} else if res.Error == "" {
			res.OK = false
			res.Error = "action did not execute"
		}
	}
	return updated, res
}

// ApplyFileBackedContractPatch merges draft required_args/params into skill.yaml
// after Versioner backup. Also updates the in-memory entry's Params/RequiredArgs.
// Returns backup version number and written yaml path.
func ApplyFileBackedContractPatch(entry *corelib.NLSkillEntry, draft *SkillMaintenancePatchDraft, versioner *Versioner) (backupVer int, writtenPath string, err error) {
	if entry == nil {
		return 0, "", fmt.Errorf("entry is nil")
	}
	skillDir := strings.TrimSpace(entry.SkillDir)
	if skillDir == "" && draft != nil {
		skillDir = strings.TrimSpace(draft.SkillDir)
	}
	if skillDir == "" {
		return 0, "", fmt.Errorf("skill_dir is empty")
	}
	if draft == nil {
		draft = buildContractPatchDraft(*entry)
	}
	if draft == nil || len(draft.Params) == 0 {
		return 0, "", fmt.Errorf("no contract params to write")
	}

	if versioner == nil {
		versioner = &Versioner{}
	}
	ver, berr := versioner.BackupCurrent(skillDir)
	if berr != nil {
		// No existing skill.yaml: still allow creating one with contract fragment only if file missing.
		// Prefer fail if definition missing so we never invent a full skill definition.
		return 0, "", fmt.Errorf("backup before write: %w", berr)
	}
	_ = versioner.CleanOldVersions(skillDir, 10)

	yamlPath, _, err := currentDefinitionFile(skillDir)
	if err != nil {
		return ver, "", err
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return ver, "", fmt.Errorf("read %s: %w", yamlPath, err)
	}
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ver, "", fmt.Errorf("parse %s: %w", yamlPath, err)
	}
	if raw == nil {
		raw = map[string]interface{}{}
	}

	// Merge contract fields.
	if len(draft.RequiredArgs) > 0 {
		req := make([]interface{}, 0, len(draft.RequiredArgs))
		for _, a := range draft.RequiredArgs {
			req = append(req, a)
		}
		raw["required_args"] = req
	}
	paramMaps := make([]interface{}, 0, len(draft.Params))
	for _, p := range draft.Params {
		m := map[string]interface{}{"name": p.Name}
		if p.Description != "" {
			m["description"] = p.Description
		}
		if len(p.Aliases) > 0 {
			aliases := make([]interface{}, 0, len(p.Aliases))
			for _, al := range p.Aliases {
				aliases = append(aliases, al)
			}
			m["aliases"] = aliases
		}
		if p.CLIFlag != "" {
			m["cli_flag"] = p.CLIFlag
		}
		if p.Default != "" {
			m["default"] = p.Default
		}
		if p.Required {
			m["required"] = true
		}
		paramMaps = append(paramMaps, m)
	}
	raw["params"] = paramMaps

	out, err := yaml.Marshal(raw)
	if err != nil {
		return ver, "", fmt.Errorf("marshal updated YAML: %w", err)
	}
	if err := fileutil.AtomicWriteFile(yamlPath, out, 0o644); err != nil {
		return ver, "", fmt.Errorf("write %s: %w", yamlPath, err)
	}

	// Update in-memory entry so saveSkills keeps config overlay consistent.
	entry.RequiredArgs = append([]string(nil), draft.RequiredArgs...)
	entry.Params = cloneMaintenanceParams(draft.Params)
	entry.SkillDir = skillDir

	log.Printf("[maintenance-draft] wrote contract patch skill=%s path=%s backup=v%d", entry.Name, yamlPath, ver)
	return ver, yamlPath, nil
}

// ResolveLatestContractBackup returns the path to skill.yaml.v{N} for the latest backup, if any.
func ResolveLatestContractBackup(skillDir string) string {
	v := &Versioner{}
	n := v.LatestVersion(skillDir)
	if n <= 0 {
		return ""
	}
	for _, base := range []string{"skill.yaml", "skill.yml"} {
		p := filepath.Join(skillDir, fmt.Sprintf("%s.v%d", base, n))
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
