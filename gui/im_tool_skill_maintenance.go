package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

const skillMaintenancePlanBoundary = "read-only skill maintenance plan; no skill was modified, archived, merged, deleted, installed, or executed"

func (h *IMMessageHandler) toolSkillMaintenanceDrafts(_ map[string]interface{}) string {
	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	exec.mu.RLock()
	skills := exec.loadSkills()
	exec.mu.RUnlock()
	drafts := cskill.CollectMaintenanceReviewDrafts(skills, cskill.SkillMaintenancePlanOptions{
		Now:        time.Now(),
		MaxActions: 40,
	})
	payload := map[string]interface{}{
		"ok":            true,
		"non_executing": true,
		"boundary":      "read-only maintenance review drafts; no skill was modified",
		"drafts":        drafts,
		"patch_count":   len(drafts.PatchDrafts),
		"merge_count":   len(drafts.MergeDrafts),
		"queued_repair": len(drafts.QueuedRepair),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("收集维护草案失败: %v", err)
	}
	return string(data)
}

func (h *IMMessageHandler) toolSkillMaintenancePlan(args map[string]interface{}) string {
	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor 未初始化"
	}

	exec.mu.RLock()
	skills := exec.loadSkills()
	exec.mu.RUnlock()

	opts := cskill.SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		StaleAfterDays:      skillMaintenanceIntArg(args, "stale_after_days"),
		MinFailureRuns:      skillMaintenanceIntArg(args, "min_failure_runs"),
		MaxActions:          skillMaintenanceIntArg(args, "max_actions"),
		DuplicateSimilarity: skillMaintenanceFloatArg(args, "duplicate_similarity"),
	}
	plan := cskill.BuildSkillMaintenancePlan(skills, opts)
	payload := map[string]interface{}{
		"ok":                      true,
		"non_executing":           true,
		"boundary":                skillMaintenancePlanBoundary,
		"maintenance_plan_status": "local_skill_maintenance_plan_no_llm",
		"plan":                    plan,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("生成 Skill 维护计划失败: %v", err)
	}
	return string(data)
}

func (h *IMMessageHandler) toolExecuteSkillMaintenancePlan(args map[string]interface{}) string {
	exec := h.getSkillExecutor()
	if exec == nil {
		return "Skill Executor 未初始化"
	}

	dryRun := skillMaintenanceBoolArg(args, "dry_run", true)
	if !dryRun && !skillMaintenanceBoolArg(args, "confirm", false) {
		return `{"ok":false,"dry_run":false,"error":"confirm=true is required when dry_run=false"}`
	}
	approvedActions := skillMaintenanceStringListArg(args, "approved_actions")
	approvedDraftIDs := skillMaintenanceStringListArg(args, "approved_draft_ids")
	approvedReviewTraceIDs := skillMaintenanceStringListArg(args, "approved_review_trace_ids")
	if len(approvedReviewTraceIDs) > 0 {
		resolved, err := h.skillMaintenanceDraftIDsFromApprovedReviews(approvedReviewTraceIDs, dryRun)
		if err != nil {
			data, marshalErr := json.Marshal(map[string]interface{}{"ok": false, "dry_run": dryRun, "error": err.Error()})
			if marshalErr != nil {
				return fmt.Sprintf(`{"ok":false,"dry_run":%t,"error":%q}`, dryRun, err.Error())
			}
			return string(data)
		}
		approvedDraftIDs = append(approvedDraftIDs, resolved...)
	}
	if !dryRun && len(approvedActions) == 0 && len(approvedDraftIDs) == 0 {
		return `{"ok":false,"dry_run":false,"error":"approved_actions, approved_draft_ids, or approved_review_trace_ids is required when dry_run=false"}`
	}

	exec.mu.Lock()
	skills := exec.loadSkills()
	planOpts := cskill.SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		StaleAfterDays:      skillMaintenanceIntArg(args, "stale_after_days"),
		MinFailureRuns:      skillMaintenanceIntArg(args, "min_failure_runs"),
		MaxActions:          skillMaintenanceIntArg(args, "max_actions"),
		DuplicateSimilarity: skillMaintenanceFloatArg(args, "duplicate_similarity"),
	}
	plan := cskill.BuildSkillMaintenancePlan(skills, planOpts)
	var updated []corelib.NLSkillEntry
	var result cskill.SkillMaintenanceExecutionResult
	if len(approvedDraftIDs) > 0 {
		updated, result = cskill.ExecuteReviewedGovernanceDrafts(skills, cskill.GovernanceDraftExecutionOptions{
			Now:                  time.Now(),
			DryRun:               dryRun,
			ReviewedDraftIDs:     approvedDraftIDs,
			PlanOptions:          planOpts,
			AllowDuplicateRetire: skillMaintenanceBoolArg(args, "allow_duplicate_retire", false),
		})
	} else {
		updated, result = cskill.ExecuteSkillMaintenancePlan(skills, plan, cskill.SkillMaintenanceExecutionOptions{
			Now:                  time.Now(),
			DryRun:               dryRun,
			ApprovedActions:      approvedActions,
			AllowDuplicateRetire: skillMaintenanceBoolArg(args, "allow_duplicate_retire", false),
		})
	}
	repairTargets := skillMaintenanceRepairTargets(updated, result)
	if !dryRun && result.OK {
		if err := exec.saveSkills(updated); err != nil {
			exec.mu.Unlock()
			result.OK = false
			result.Error = "skill maintenance save failed: " + err.Error()
			auditErr := h.recordSkillDraftExecutionAudit(approvedReviewTraceIDs, skillDraftExecutionBlocked, result.Error)
			return skillMaintenanceExecutionPayload(map[string]interface{}{
				"ok":                           false,
				"dry_run":                      dryRun,
				"boundary":                     result.Boundary,
				"error":                        result.Error,
				"review_execution_audit_error": skillMaintenanceErrorString(auditErr),
				"plan_summary":                 plan.Summary,
				"self_repair_triggers_started": 0,
				"result":                       result,
			})
		}
	}
	exec.mu.Unlock()
	auditStatus := skillDraftExecutionPreviewed
	if !result.OK {
		auditStatus = skillDraftExecutionBlocked
	} else if !dryRun {
		auditStatus = skillDraftExecutionApplied
	}
	auditErr := h.recordSkillDraftExecutionAudit(approvedReviewTraceIDs, auditStatus, skillMaintenanceExecutionAuditNote(result))

	if result.RequiresIndexRefresh && !dryRun && result.OK {
		h.refreshSkillIndexesAfterMutation("")
	}
	triggeredRepairs := 0
	if !dryRun && result.OK && len(repairTargets) > 0 {
		if runner := h.getSkillRunner(); runner != nil {
			for _, target := range repairTargets {
				cp := target
				if runner.canStartRepairSkill(&cp) {
					triggeredRepairs++
					go runner.maybeRepairSkill(&cp)
				}
			}
		}
	}
	payload := map[string]interface{}{
		"ok":                           result.OK,
		"dry_run":                      result.DryRun,
		"boundary":                     result.Boundary,
		"error":                        result.Error,
		"review_execution_audit_error": "",
		"plan_summary":                 plan.Summary,
		"self_repair_triggers_started": triggeredRepairs,
		"result":                       result,
	}
	if auditErr != nil {
		payload["review_execution_audit_error"] = auditErr.Error()
	}
	return skillMaintenanceExecutionPayload(payload)
}

func skillMaintenanceExecutionPayload(payload map[string]interface{}) string {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("Skill maintenance execution failed: %v", err)
	}
	return string(data)
}

func skillMaintenanceErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func skillMaintenanceExecutionAuditNote(result cskill.SkillMaintenanceExecutionResult) string {
	parts := []string{strings.TrimSpace(result.Boundary)}
	if errText := strings.TrimSpace(result.Error); errText != "" {
		parts = append(parts, "error: "+errText)
	}
	if len(result.Actions) > 0 {
		parts = append(parts, fmt.Sprintf("actions: %d", len(result.Actions)))
	}
	return strings.TrimSpace(strings.Join(parts, "; "))
}

func (h *IMMessageHandler) recordSkillDraftExecutionAudit(traceIDs []string, status, note string) error {
	if len(traceIDs) == 0 {
		return nil
	}
	if h == nil || h.app == nil || h.app.memoryStore == nil {
		return fmt.Errorf("memory store not initialized")
	}
	now := time.Now().UTC()
	entries := make([]memory.Entry, 0, len(traceIDs))
	for _, traceID := range traceIDs {
		entry, err := findExperienceMemoryEntryByTraceID(h.app.memoryStore, traceID)
		if err != nil {
			return err
		}
		detail, ok := traceDetailFromMemoryEntry(entry)
		if !ok || detail.Kind != "skill_draft_review" || detail.FollowUpStatus != experienceFollowUpOutcomeCompleted || strings.TrimSpace(detail.DraftID) == "" {
			return fmt.Errorf("review trace %q is not a completed skill draft review with draft id", traceID)
		}
		entry.Content = appendSkillDraftExecutionRecord(entry.Content, status, note, "manage_skill", now)
		entry.Tags = applySkillDraftExecutionTags(entry.Tags, status, now)
		if err := memory.ScanForInjection(entry.Content); err != nil {
			return fmt.Errorf("review trace %q execution audit rejected: %w", traceID, err)
		}
		entries = append(entries, entry)
	}
	return h.app.memoryStore.UpdateEntriesByID(entries)
}

func (h *IMMessageHandler) skillMaintenanceDraftIDsFromApprovedReviews(traceIDs []string, dryRun bool) ([]string, error) {
	if h == nil || h.app == nil || h.app.memoryStore == nil {
		return nil, fmt.Errorf("memory store not initialized")
	}
	out := make([]string, 0, len(traceIDs))
	seen := map[string]bool{}
	for _, traceID := range traceIDs {
		entry, err := findExperienceMemoryEntryByTraceID(h.app.memoryStore, traceID)
		if err != nil {
			return nil, err
		}
		detail, ok := traceDetailFromMemoryEntry(entry)
		if !ok || detail.Kind != "skill_draft_review" {
			return nil, fmt.Errorf("review trace %q is not a skill draft review", traceID)
		}
		if detail.FollowUpStatus != experienceFollowUpOutcomeCompleted {
			return nil, fmt.Errorf("skill draft review %q is not completed", traceID)
		}
		draftID := strings.TrimSpace(detail.DraftID)
		if draftID == "" {
			return nil, fmt.Errorf("skill draft review %q has no draft id", traceID)
		}
		executionStatus := strings.TrimSpace(detail.DraftExecutionStatus)
		switch executionStatus {
		case "", skillDraftExecutionPreviewed:
			if !dryRun && executionStatus != skillDraftExecutionPreviewed {
				return nil, fmt.Errorf("skill draft review %q must be previewed before execution", traceID)
			}
		case skillDraftExecutionApplied:
			return nil, fmt.Errorf("skill draft review %q was already applied", traceID)
		case skillDraftExecutionBlocked:
			return nil, fmt.Errorf("skill draft review %q is blocked and needs repair review", traceID)
		case skillDraftExecutionReopened:
			return nil, fmt.Errorf("skill draft review %q was reopened; execute the replacement review trace", traceID)
		case skillDraftExecutionClosed:
			return nil, fmt.Errorf("skill draft review %q is closed", traceID)
		default:
			return nil, fmt.Errorf("skill draft review %q has unsupported execution status %q", traceID, executionStatus)
		}
		if !seen[draftID] {
			seen[draftID] = true
			out = append(out, draftID)
		}
	}
	return out, nil
}

func skillMaintenanceRepairTargets(skills []corelib.NLSkillEntry, result cskill.SkillMaintenanceExecutionResult) []corelib.NLSkillEntry {
	wanted := make(map[string]bool)
	for _, action := range result.Actions {
		if action.Action == cskill.MaintenanceActionAttemptRepair && action.Status == cskill.MaintenanceExecutionStatusQueued {
			wanted[action.Skill] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	targets := make([]corelib.NLSkillEntry, 0, len(wanted))
	for _, skill := range skills {
		for name := range wanted {
			if skill.MatchesName(name) || skill.Name == name {
				targets = append(targets, skill)
				delete(wanted, name)
				break
			}
		}
	}
	return targets
}

func skillMaintenanceIntArg(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return 0
}

func skillMaintenanceFloatArg(args map[string]interface{}, key string) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case json.Number:
		if n, err := v.Float64(); err == nil {
			return n
		}
	}
	return 0
}

func skillMaintenanceBoolArg(args map[string]interface{}, key string, fallback bool) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}

func skillMaintenanceStringListArg(args map[string]interface{}, key string) []string {
	switch v := args[key].(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil
		}
		return []string{v}
	}
	return nil
}
