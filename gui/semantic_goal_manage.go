package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedGoalAdapter        = "semantic_administer_trusted_goal"
	semanticTrustedGoalImplementation = "trusted-goal-manage-v1"
	semanticTrustedGoalObjectiveMax   = 2000
	semanticTrustedGoalNoteMax        = 5000
	semanticTrustedGoalTimeout        = 10 * time.Second
)

func semanticUnpublishedLegacyGoalProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityGoalManageLongRunning {
			return true
		}
	}
	return false
}

func semanticTrustedGoalDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedGoalAdapter,
			"description": "Read or update the current principal's long-running goal record. Field presence decides create, get, or end the current goal.",
			"parameters":  semanticTrustedGoalInvocationSchema(),
		},
	}
}

func semanticTrustedGoalInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"objective": map[string]interface{}{"type": "string"},
			"status":    map[string]interface{}{"type": "string"},
			"note":      map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedGoalArgsAllowed(args map[string]interface{}) (objective, status, note string, err error) {
	if len(args) > 3 {
		return "", "", "", fmt.Errorf("trusted_goal_arguments_rejected")
	}
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", "", fmt.Errorf("trusted_goal_arguments_rejected")
		}
		switch key {
		case "objective":
			objective = strings.TrimSpace(value)
		case "status":
			status = strings.TrimSpace(value)
		case "note":
			note = strings.TrimSpace(value)
		default:
			return "", "", "", fmt.Errorf("trusted_goal_arguments_rejected")
		}
	}
	if _, ok := semanticTrustedGoalDispatch(objective, status, note); !ok {
		return "", "", "", fmt.Errorf("trusted_goal_field_presence_rejected")
	}
	return objective, status, note, nil
}

func semanticTrustedGoalDispatch(objective, status, note string) (string, bool) {
	hasObjective := objective != ""
	hasStatus := status != ""
	hasNote := note != ""
	if hasObjective {
		if hasStatus || hasNote {
			return "", false
		}
		return "create", true
	}
	if hasStatus {
		return "update", true
	}
	if hasNote {
		return "", false
	}
	return "get", true
}

func semanticTrustedGoalStatus(raw string) (goal.Status, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "complete", "completed":
		return goal.StatusComplete, true
	case "failed", "fail":
		return goal.StatusFailed, true
	default:
		return "", false
	}
}

func (h *IMMessageHandler) administerTrustedGoal(principalID, objective, status, note string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_goal_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_goal_principal_required")
	}
	if h.semanticTrustedGoal != nil {
		return h.semanticTrustedGoal(principalID, objective, status, note)
	}
	store := h.getGoalStore()
	if store == nil {
		return "", fmt.Errorf("trusted_goal_unavailable")
	}
	objective, status, note = strings.TrimSpace(objective), strings.TrimSpace(status), strings.TrimSpace(note)
	op, ok := semanticTrustedGoalDispatch(objective, status, note)
	if !ok {
		return "", fmt.Errorf("trusted_goal_field_presence_rejected")
	}
	if objective != "" && utf8.RuneCountInString(objective) > semanticTrustedGoalObjectiveMax {
		return "", fmt.Errorf("trusted_goal_objective_too_large")
	}
	if note != "" && utf8.RuneCountInString(note) > semanticTrustedGoalNoteMax {
		return "", fmt.Errorf("trusted_goal_note_too_large")
	}
	ctx, cancel := context.WithTimeout(context.Background(), semanticTrustedGoalTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	switch op {
	case "create":
		if existing := store.Get(principalID); existing != nil && !existing.IsTerminal() {
			return "", fmt.Errorf("trusted_goal_already_active")
		}
		created, err := store.Set(principalID, objective)
		if err != nil {
			return "", err
		}
		return semanticTrustedGoalProjection("created", created), nil
	case "update":
		parsed, statusOK := semanticTrustedGoalStatus(status)
		if !statusOK {
			return "", fmt.Errorf("trusted_goal_status_rejected")
		}
		current := store.Get(principalID)
		if current == nil {
			return "", fmt.Errorf("trusted_goal_not_found")
		}
		if current.IsTerminal() {
			return "", fmt.Errorf("trusted_goal_already_terminal")
		}
		if !store.UpdateStatus(principalID, current.GoalID, parsed, note) {
			return "", fmt.Errorf("trusted_goal_update_failed")
		}
		updated := store.Get(principalID)
		if updated == nil {
			return "", fmt.Errorf("trusted_goal_update_failed")
		}
		return semanticTrustedGoalProjection("updated", updated), nil
	default:
		current := store.Get(principalID)
		if current == nil {
			return "当前没有目标。", nil
		}
		return semanticTrustedGoalProjection("current", current), nil
	}
}

func semanticTrustedGoalProjection(kind string, g *goal.Goal) string {
	if g == nil {
		return "当前没有目标。"
	}
	switch kind {
	case "created":
		return fmt.Sprintf("目标已创建: %s\nGoal ID: %s\n状态: %s", g.Objective, g.GoalID, g.Status)
	case "updated":
		result := fmt.Sprintf("目标已更新: %s\nGoal ID: %s\n状态: %s", g.Objective, g.GoalID, g.Status)
		if strings.TrimSpace(g.Summary) != "" {
			result += "\n备注: " + g.Summary
		}
		return result
	default:
		result := fmt.Sprintf("目标 [%s]: %s\nGoal ID: %s", g.Status, g.Objective, g.GoalID)
		if strings.TrimSpace(g.Summary) != "" {
			result += "\n备注: " + g.Summary
		}
		return result
	}
}

func semanticTrustedGoalResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_goal_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_goal_empty")
	}
	return text, nil
}
