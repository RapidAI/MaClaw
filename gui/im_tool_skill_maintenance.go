package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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
	skills := exec.loadSkills()
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

	skills := exec.loadSkills()

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

	var plan cskill.SkillMaintenancePlan
	var updated []corelib.NLSkillEntry
	var result cskill.SkillMaintenanceExecutionResult
	var repairTargets []corelib.NLSkillEntry
	commitState := ""
	commitCleanupStatus := ""
	commitRequestID := ""
	commitFailureReason := ""
	noOpCommit := false
	skills := exec.loadSkills()
	planOpts := cskill.SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		StaleAfterDays:      skillMaintenanceIntArg(args, "stale_after_days"),
		MinFailureRuns:      skillMaintenanceIntArg(args, "min_failure_runs"),
		MaxActions:          skillMaintenanceIntArg(args, "max_actions"),
		DuplicateSimilarity: skillMaintenanceFloatArg(args, "duplicate_similarity"),
	}
	plan = cskill.BuildSkillMaintenancePlan(skills, planOpts)
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
	repairTargets = skillMaintenanceRepairTargets(updated, result)
	if !dryRun && result.OK && len(result.Actions) == 0 && result.ExecutedCount == 0 {
		// An approved action can legitimately disappear from a freshly rebuilt
		// plan (for example, the maintenance condition was already repaired).
		// Treat an empty plan as an idempotent no-op instead of inventing a
		// synthetic skill identity or reporting a failed commit.
		noOpCommit = true
		commitState = "skipped"
		commitCleanupStatus = "clear"
		commitRequestID = fmt.Sprintf("evo_im_maintenance_%d", time.Now().UnixNano())
		goto maintenanceCommitDone
	}
	if !dryRun && result.OK {
		// File-backed contract drafts require the desktop reviewed-patch flow;
		// this IM path only owns config-backed metadata mutations. Never let the
		// config-only committer silently treat a YAML draft as a no-op.
		for _, action := range result.Actions {
			if action.PatchDraft != nil {
				result.OK = false
				result.Error = "file-backed maintenance draft requires reviewed desktop apply"
				break
			}
		}
	}
	if !dryRun && result.OK {
		// Batch maintenance must not claim a single-skill admission check is
		// sufficient. Check every entry whose authoritative metadata changes;
		// any pending/unreadable compensation keeps the whole batch fail-closed.
		originalByName := make(map[string]corelib.NLSkillEntry, len(skills))
		for _, entry := range skills {
			originalByName[strings.ToLower(strings.TrimSpace(entry.Name))] = entry
		}
		for i := range updated {
			before, exists := originalByName[strings.ToLower(strings.TrimSpace(updated[i].Name))]
			if exists && reflect.DeepEqual(before, updated[i]) {
				continue
			}
			if err := ensureSkillEvolutionMutationAdmission(h.app, updated[i].Name); err != nil {
				result.OK = false
				result.Error = err.Error()
				break
			}
		}
	}
	if !dryRun && result.OK {
		requestID := fmt.Sprintf("evo_im_maintenance_%d", time.Now().UnixNano())
		configRevision := skillEvolutionConfigRevision(h.app)
		commitSkillName := ""
		if len(updated) > 0 {
			commitSkillName = strings.TrimSpace(updated[0].Name)
		}
		if commitSkillName == "" {
			result.OK = false
			result.Error = "skill maintenance produced no identifiable skill entry"
		}
		if !result.OK {
			// Keep the failure in the structured result; do not attempt a commit
			// with a synthetic skill identity.
			goto maintenanceCommitDone
		}
		committer := &cskill.SkillCommitter{
			SkillLoader: func() []corelib.NLSkillEntry { return exec.loadSkills() },
			SkillSaver: func(entries []corelib.NLSkillEntry) error {
				return exec.withSkillListMutate(func() error { return exec.saveSkills(entries) })
			},
			RollbackSkillSaver: func(entries []corelib.NLSkillEntry) error {
				return exec.withSkillListMutate(func() error { return exec.restoreSkillsSnapshot(entries) })
			},
			EntriesMutator: func([]corelib.NLSkillEntry) ([]corelib.NLSkillEntry, error) {
				return updated, nil
			},
			DefinitionWriter: func(*corelib.NLSkillEntry) error { return nil },
			IndexRefresher: func() error {
				if h.app == nil {
					return fmt.Errorf("app not initialized")
				}
				return h.app.refreshSkillIndexesAfterMutationChecked("")
			},
			FinalAuditor: func(event string, data map[string]string) error {
				return cskill.RecordEvolutionEventStrict(event, data, "desktop")
			},
			ConfigRevision: configRevision,
			// IM maintenance mutates only the config-backed registry snapshot;
			// unchanged plans must be a side-effect-free no-op.
			SkipIfUnchanged: true,
			// IM maintenance does not own file-backed YAML drafts; only the
			// config snapshot is committed by this path.
			SkipDefinitionBackup: true,
		}
		commitResult := committer.Commit(cskill.WithEvolutionRequestMetadata(context.Background(), requestID, 1), commitSkillName, &updated[0], "skill:maintenance_plan_applied", map[string]string{
			"skill": "maintenance", "action": "maintenance_plan", "decision": "applied", "via": "operator",
			"request_id": requestID, "attempt": "1", "config_revision": configRevision, "schema_version": "2", "evidence_mode": "none",
		})
		commitState = commitResult.State
		commitCleanupStatus = commitResult.CleanupStatus
		commitRequestID = commitResult.RequestID
		commitFailureReason = commitResult.FailureReason
		if commitResult.State == "skipped" {
			// A reviewed action whose authoritative state is already identical is
			// not an applied mutation. Keep the review trace previewed so it can be
			// re-evaluated if the plan changes, and avoid an applied audit claim.
			noOpCommit = true
		}
		if (commitResult.State != "committed" && commitResult.State != "skipped") || commitResult.CleanupStatus != "clear" {
			result.OK = false
			result.Error = fmt.Sprintf("skill maintenance not committed: %s (%s)", commitResult.State, commitResult.FailureReason)
		}
	}

maintenanceCommitDone:
	if !result.OK && !dryRun {
		auditErr := h.recordSkillDraftExecutionAudit(approvedReviewTraceIDs, skillDraftExecutionBlocked, result.Error)
		return skillMaintenanceExecutionPayload(map[string]interface{}{
			"ok":                           false,
			"dry_run":                      dryRun,
			"boundary":                     result.Boundary,
			"error":                        result.Error,
			"review_execution_audit_error": skillMaintenanceErrorString(auditErr),
			"plan_summary":                 plan.Summary,
			"self_repair_triggers_started": 0,
			"state":                        commitState,
			"cleanup_status":               commitCleanupStatus,
			"request_id":                   commitRequestID,
			"failure_reason":               commitFailureReason,
			"result":                       result,
		})
	}
	auditStatus := skillDraftExecutionPreviewed
	if !result.OK {
		auditStatus = skillDraftExecutionBlocked
	} else if noOpCommit {
		auditStatus = skillDraftExecutionPreviewed
	} else if !dryRun {
		auditStatus = skillDraftExecutionApplied
	}
	auditErr := h.recordSkillDraftExecutionAudit(approvedReviewTraceIDs, auditStatus, skillMaintenanceExecutionAuditNote(result))

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
		"state":                        commitState,
		"cleanup_status":               commitCleanupStatus,
		"request_id":                   commitRequestID,
		"failure_reason":               commitFailureReason,
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
