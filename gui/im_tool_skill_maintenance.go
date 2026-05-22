package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

const skillMaintenancePlanBoundary = "read-only skill maintenance plan; no skill was modified, archived, merged, deleted, installed, or executed"

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
		return `{"ok":false,"dry_run":true,"error":"confirm=true is required when dry_run=false"}`
	}
	approvedActions := skillMaintenanceStringListArg(args, "approved_actions")
	if !dryRun && len(approvedActions) == 0 {
		return `{"ok":false,"dry_run":true,"error":"approved_actions is required when dry_run=false"}`
	}

	exec.mu.Lock()
	skills := exec.loadSkills()
	plan := cskill.BuildSkillMaintenancePlan(skills, cskill.SkillMaintenancePlanOptions{
		Now:                 time.Now(),
		StaleAfterDays:      skillMaintenanceIntArg(args, "stale_after_days"),
		MinFailureRuns:      skillMaintenanceIntArg(args, "min_failure_runs"),
		MaxActions:          skillMaintenanceIntArg(args, "max_actions"),
		DuplicateSimilarity: skillMaintenanceFloatArg(args, "duplicate_similarity"),
	})
	updated, result := cskill.ExecuteSkillMaintenancePlan(skills, plan, cskill.SkillMaintenanceExecutionOptions{
		Now:                  time.Now(),
		DryRun:               dryRun,
		ApprovedActions:      approvedActions,
		AllowDuplicateRetire: skillMaintenanceBoolArg(args, "allow_duplicate_retire", false),
	})
	repairTargets := skillMaintenanceRepairTargets(updated, result)
	if !dryRun && result.OK {
		if err := exec.saveSkills(updated); err != nil {
			exec.mu.Unlock()
			return fmt.Sprintf("Skill maintenance execution failed: %v", err)
		}
	}
	exec.mu.Unlock()

	if result.RequiresIndexRefresh && !dryRun && result.OK {
		h.refreshSkillIndexesAfterMutation("")
	}
	triggeredRepairs := 0
	if !dryRun && result.OK && len(repairTargets) > 0 {
		if runner := h.getSkillRunner(); runner != nil {
			for _, target := range repairTargets {
				cp := target
				triggeredRepairs++
				go runner.maybeRepairSkill(&cp)
			}
		}
	}
	payload := map[string]interface{}{
		"ok":                           result.OK,
		"dry_run":                      result.DryRun,
		"boundary":                     result.Boundary,
		"error":                        result.Error,
		"plan_summary":                 plan.Summary,
		"self_repair_triggers_started": triggeredRepairs,
		"result":                       result,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("Skill maintenance execution failed: %v", err)
	}
	return string(data)
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
