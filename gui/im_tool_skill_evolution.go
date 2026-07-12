package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// toolSkillEvolutionStatus returns a read-only JSON snapshot of the desktop
// skill evolution pipeline for the manage_skill tool.
func (h *IMMessageHandler) toolSkillEvolutionStatus(_ map[string]interface{}) string {
	if h == nil || h.app == nil {
		return "Skill evolution status unavailable: app not initialized"
	}
	st := h.app.GetSkillEvolutionStatus()
	payload := map[string]interface{}{
		"ok":                      true,
		"non_executing":           true,
		"boundary":                "read-only skill evolution pipeline status",
		"pipeline_started":        st.PipelineStarted,
		"pending_skills":          st.PendingSkills,
		"coalesced_notifications": st.CoalescedNotifications,
		"dropped_notifications":   st.DroppedNotifications,
		"processed_requests":      st.ProcessedRequests,
		"enable_repair":           st.EnableRepair,
		"enable_optimizer":        st.EnableOptimizer,
		"enable_promoter":         st.EnablePromoter,
		"repair_cooldown":         st.RepairCooldown,
		"repair_cooldown_hours":   st.RepairCooldownHours,
		"has_repair_hook":         st.HasRepairHook,
		"has_optimizer":           st.HasOptimizer,
		"has_promoter":            st.HasPromoter,
		"env_disabled":            st.EnvDisabled,
		"config_enabled":          st.ConfigEnabled,
		"config_disabled":         st.ConfigDisabled,
		"disabled":                st.Disabled,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("Skill evolution status marshal failed: %v", err)
	}
	return string(data)
}

// toolSkillEvolutionAudit returns durable evolution audit rows (JSONL).
// Args: limit (optional int), name (optional skill name substring filter).
func (h *IMMessageHandler) toolSkillEvolutionAudit(args map[string]interface{}) string {
	limit := 50
	if args != nil {
		switch v := args["limit"].(type) {
		case float64:
			limit = int(v)
		case int:
			limit = v
		case string:
			if n, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &limit); n != 1 || err != nil {
				limit = 50
			}
		}
	}
	name := strings.TrimSpace(stringVal(args, "name"))
	if name == "" {
		name = strings.TrimSpace(stringVal(args, "skill"))
	}
	payload := cskill.EvolutionAuditToolPayload("", limit, name)
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("Skill evolution audit marshal failed: %v", err)
	}
	return string(data)
}

// toolSetSkillEvolutionEnabled persists skill_evolution_enabled via config.
// Args: enabled (bool, required). true also clears nothing session-side on desktop
// (desktop has no session kill switch; env still overrides automatic path).
func (h *IMMessageHandler) toolSetSkillEvolutionEnabled(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return `{"ok":false,"error":"app not initialized"}`
	}
	enabled, ok := boolArgPresent(args, "enabled")
	if !ok {
		// Accept enable=true / value=true aliases used by some models.
		if v, present := boolArgPresent(args, "enable"); present {
			enabled, ok = v, true
		} else if v, present := boolArgPresent(args, "value"); present {
			enabled, ok = v, true
		}
	}
	if !ok {
		return `{"ok":false,"error":"enabled is required for set_evolution_enabled (true or false)"}`
	}
	if _, err := h.app.PatchConfigFields(map[string]interface{}{
		"skill_evolution_enabled": enabled,
	}); err != nil {
		data, _ := json.MarshalIndent(map[string]interface{}{
			"ok": false, "error": err.Error(),
		}, "", "  ")
		return string(data)
	}
	st := h.app.GetSkillEvolutionStatus()
	payload := map[string]interface{}{
		"ok":              true,
		"enabled":         enabled,
		"config_enabled":  st.ConfigEnabled,
		"config_disabled": st.ConfigDisabled,
		"env_disabled":    st.EnvDisabled,
		"disabled":        st.Disabled,
		"message":         "skill_evolution_enabled updated",
	}
	if st.EnvDisabled {
		payload["note"] = "MACLAW_DISABLE_SKILL_EVOLUTION still suppresses automatic evolution"
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

// boolArgPresent reads a boolean tool arg and reports whether the key was set.
func boolArgPresent(args map[string]interface{}, key string) (bool, bool) {
	if args == nil {
		return false, false
	}
	v, exists := args[key]
	if !exists || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		if s == "1" || s == "true" || s == "yes" || s == "on" {
			return true, true
		}
		if s == "0" || s == "false" || s == "no" || s == "off" {
			return false, true
		}
		return false, false
	case float64:
		return t != 0, true
	case int:
		return t != 0, true
	default:
		return false, false
	}
}

// toolTriggerSkillRepair starts an immediate self-repair attempt for a named skill.
//
// Args:
//   - name (required): skill name
//   - force (optional): skip usage-rate threshold when true
//   - wait (optional): run synchronously and return updated audit fields
func (h *IMMessageHandler) toolTriggerSkillRepair(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return `{"ok":false,"error":"app not initialized"}`
	}
	name := strings.TrimSpace(stringVal(args, "name"))
	if name == "" {
		return `{"ok":false,"error":"name is required for trigger_repair"}`
	}
	force := boolVal(args, "force")
	wait := boolVal(args, "wait")

	h.app.ensureSkillRunner()
	runner := h.app.skillRunner
	if runner == nil || runner.executor == nil {
		return `{"ok":false,"error":"skill runner not initialized"}`
	}

	runner.executor.mu.RLock()
	skills := runner.executor.loadSkills()
	runner.executor.mu.RUnlock()

	var found *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].MatchesName(name) {
			found = cskill.CloneNLSkillEntry(&skills[i])
			break
		}
	}
	if found == nil {
		return fmt.Sprintf(`{"ok":false,"error":"skill %q not found"}`, name)
	}

	eligible := cskill.ShouldAttemptRepair(found)
	forced := false
	if !eligible && force && cskill.CanForceAttemptRepair(found) {
		eligible = true
		forced = true
	}
	if !eligible {
		reason := "not eligible for self-repair"
		switch {
		case found.LastError == "":
			reason = "no LastError recorded; run the skill first so failures can be diagnosed"
		case cskill.IsFileBackedSkill(*found):
			reason = "file-backed skills require a reviewed patch flow"
		case found.RepairAttemptCount >= cskill.SelfRepairMaxAttempts:
			reason = fmt.Sprintf("repair attempt limit reached (%d)", cskill.SelfRepairMaxAttempts)
		case !cskill.IsRepairableError(cskill.ExtractErrorClass(found.LastError)):
			reason = fmt.Sprintf("error class %q is not auto-repairable", cskill.ExtractErrorClass(found.LastError))
		case !force:
			reason = "usage statistics do not meet auto-repair threshold; pass force=true to try anyway"
		}
		data, _ := json.MarshalIndent(map[string]interface{}{
			"ok":         false,
			"skill":      found.Name,
			"error":      reason,
			"last_error": found.LastError,
		}, "", "  ")
		return string(data)
	}
	if runner.buildSkillRepairer() == nil {
		data, _ := json.MarshalIndent(map[string]interface{}{
			"ok":    false,
			"skill": found.Name,
			"error": "LLM not configured for skill repair",
		}, "", "  ")
		return string(data)
	}

	if startedAt, repairing := runner.repairingSkills.Load(found.Name); repairing {
		if t, ok := startedAt.(time.Time); ok && time.Since(t) < 5*time.Minute {
			data, _ := json.MarshalIndent(map[string]interface{}{
				"ok":    false,
				"skill": found.Name,
				"error": "skill is already being auto-repaired; retry shortly",
			}, "", "  ")
			return string(data)
		}
		runner.repairingSkills.Delete(found.Name)
	}

	runner.markSelfRepairPending(found.Name)
	if wait {
		runner.maybeRepairSkillWithForce(found, forced)
		runner.executor.mu.RLock()
		skills = runner.executor.loadSkills()
		runner.executor.mu.RUnlock()
		payload := map[string]interface{}{
			"ok":      true,
			"skill":   found.Name,
			"forced":  forced,
			"waited":  true,
			"message": "self-repair attempt finished",
		}
		for i := range skills {
			if skills[i].MatchesName(found.Name) {
				payload["repair_attempt_count"] = skills[i].RepairAttemptCount
				payload["last_repair_at"] = skills[i].LastRepairAt
				payload["last_error"] = skills[i].LastError
				payload["status"] = skills[i].Status
				break
			}
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		return string(data)
	}

	go runner.maybeRepairSkillWithForce(found, forced)
	data, _ := json.MarshalIndent(map[string]interface{}{
		"ok":      true,
		"skill":   found.Name,
		"forced":  forced,
		"waited":  false,
		"message": "self-repair started in background; check skill details or evolution_status later",
	}, "", "  ")
	return string(data)
}

// toolTriggerSkillOptimize runs a one-shot LLM optimization for a named skill.
func (h *IMMessageHandler) toolTriggerSkillOptimize(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return `{"ok":false,"error":"app not initialized"}`
	}
	name := strings.TrimSpace(stringVal(args, "name"))
	if name == "" {
		return `{"ok":false,"error":"name is required for trigger_optimize"}`
	}
	force := boolVal(args, "force")

	h.app.ensureEvolutionPipeline()
	p := h.app.evolutionPipeline
	if p == nil || p.Optimizer == nil {
		return `{"ok":false,"error":"skill optimizer not available"}`
	}

	// Load skill entry.
	var found *corelib.NLSkillEntry
	if h.app.skillExecutor != nil {
		h.app.skillExecutor.mu.RLock()
		skills := h.app.skillExecutor.loadSkills()
		h.app.skillExecutor.mu.RUnlock()
		for i := range skills {
			if skills[i].MatchesName(name) {
				found = cskill.CloneNLSkillEntry(&skills[i])
				break
			}
		}
	}
	if found == nil {
		return fmt.Sprintf(`{"ok":false,"error":"skill %q not found"}`, name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res := p.TriggerOptimize(ctx, found, force)
	payload := map[string]interface{}{
		"ok":          res.Optimized || (res.Attempted && res.Explanation != ""),
		"skill":       found.Name,
		"forced":      force,
		"attempted":   res.Attempted,
		"optimized":   res.Optimized,
		"skipped":     res.Skipped,
		"skip_reason": res.SkipReason,
		"explanation": res.Explanation,
	}
	if res.Skipped && !res.Attempted {
		payload["ok"] = false
		payload["error"] = res.SkipReason
	}
	if res.Attempted && !res.Optimized && res.Explanation != "" && !res.Skipped {
		// LLM ran but no change — still ok for diagnostics.
		payload["ok"] = true
		payload["message"] = "optimization attempted but no changes applied"
	}
	if res.Optimized {
		payload["message"] = "optimization applied"
		payload["optimization_count"] = found.OptimizationCount
		payload["last_optimized_at"] = found.LastOptimizedAt
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

// TriggerSkillSelfRepair is a Wails binding for the skill detail "Repair now" button.
// Always waits for the repair attempt to finish so the UI can show updated audit fields.
func (a *App) TriggerSkillSelfRepair(name string, force bool) string {
	h := &IMMessageHandler{app: a}
	return h.toolTriggerSkillRepair(map[string]interface{}{
		"name":  name,
		"force": force,
		"wait":  true,
	})
}

// BatchTriggerSkillSelfRepair runs force self-repair for multiple skills sequentially.
// Partial success is allowed; each skill's raw result is summarized.
func (a *App) BatchTriggerSkillSelfRepair(names []string, force bool) map[string]interface{} {
	out := map[string]interface{}{
		"ok":        false,
		"force":     force,
		"succeeded": []string{},
		"failed":    []string{},
		"messages":  []string{},
		"count":     0,
	}
	seen := map[string]bool{}
	succeeded := make([]string, 0, len(names))
	failed := make([]string, 0)
	messages := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		rawResult := a.TriggerSkillSelfRepair(name, force)
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(rawResult), &parsed); err != nil {
			failed = append(failed, name)
			messages = append(messages, name+": invalid response")
			continue
		}
		ok, _ := parsed["ok"].(bool)
		msg := strings.TrimSpace(fmt.Sprint(parsed["message"]))
		if msg == "" || msg == "<nil>" {
			msg = strings.TrimSpace(fmt.Sprint(parsed["error"]))
		}
		if msg == "" || msg == "<nil>" {
			msg = rawResult
		}
		if len(msg) > 160 {
			msg = msg[:160] + "…"
		}
		messages = append(messages, name+": "+msg)
		if ok {
			succeeded = append(succeeded, name)
		} else {
			failed = append(failed, name)
		}
	}
	out["succeeded"] = succeeded
	out["failed"] = failed
	out["messages"] = messages
	out["count"] = len(succeeded)
	out["ok"] = len(failed) == 0 && len(succeeded) > 0
	if len(succeeded) == 0 && len(failed) == 0 {
		out["error"] = "no skill names provided"
	}
	return out
}

// TriggerSkillOptimize is a Wails binding for optional UI optimize actions.
func (a *App) TriggerSkillOptimize(name string, force bool) string {
	h := &IMMessageHandler{app: a}
	return h.toolTriggerSkillOptimize(map[string]interface{}{
		"name":  name,
		"force": force,
	})
}

// BatchTriggerSkillOptimize runs force optimize for multiple skills sequentially.
// Partial success is allowed; each skill's raw result is summarized.
func (a *App) BatchTriggerSkillOptimize(names []string, force bool) map[string]interface{} {
	out := map[string]interface{}{
		"ok":        false,
		"force":     force,
		"succeeded": []string{},
		"failed":    []string{},
		"messages":  []string{},
		"count":     0,
	}
	seen := map[string]bool{}
	succeeded := make([]string, 0, len(names))
	failed := make([]string, 0)
	messages := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		rawResult := a.TriggerSkillOptimize(name, force)
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(rawResult), &parsed); err != nil {
			failed = append(failed, name)
			messages = append(messages, name+": invalid response")
			continue
		}
		ok, _ := parsed["ok"].(bool)
		msg := strings.TrimSpace(fmt.Sprint(parsed["message"]))
		if msg == "" || msg == "<nil>" {
			msg = strings.TrimSpace(fmt.Sprint(parsed["explanation"]))
		}
		if msg == "" || msg == "<nil>" {
			msg = strings.TrimSpace(fmt.Sprint(parsed["error"]))
		}
		if msg == "" || msg == "<nil>" {
			msg = strings.TrimSpace(fmt.Sprint(parsed["skip_reason"]))
		}
		if msg == "" || msg == "<nil>" {
			msg = rawResult
		}
		if len(msg) > 160 {
			msg = msg[:160] + "…"
		}
		messages = append(messages, name+": "+msg)
		if ok {
			succeeded = append(succeeded, name)
		} else {
			failed = append(failed, name)
		}
	}
	out["succeeded"] = succeeded
	out["failed"] = failed
	out["messages"] = messages
	out["count"] = len(succeeded)
	out["ok"] = len(failed) == 0 && len(succeeded) > 0
	if len(succeeded) == 0 && len(failed) == 0 {
		out["error"] = "no skill names provided"
	}
	return out
}

// boolVal reads a boolean-ish tool arg.
func boolVal(args map[string]interface{}, key string) bool {
	if args == nil {
		return false
	}
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return false
	}
}
