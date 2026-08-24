package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/goal"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostGoalProviderID     = "core-goal"
	reviewedHostGoalImplementation = "local"
	reviewedHostGoalAdapterName    = "host_goal_manage_longrunning"
	reviewedHostGoalObjectiveMax   = 2000
	reviewedHostGoalNoteMax        = 5000
)

type reviewedHostGoalManager interface {
	ManageReviewedHostGoal(ctx context.Context, principal Principal, objective, status, note string) (string, error)
}

func reviewedHostGoalInvocationSchema() map[string]interface{} {
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

func reviewedHostGoalContractDigest() string {
	return coretool.SchemaDigest([]byte("goal.manage.longrunning:v1:host-goal-manage"))
}

func reviewedHostGoalDispatch(objective, status, note string) (string, bool) {
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

func reviewedHostGoalStatus(raw string) (goal.Status, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "complete", "completed":
		return goal.StatusComplete, true
	case "failed", "fail":
		return goal.StatusFailed, true
	default:
		return "", false
	}
}

// ProjectReviewedHostGoalProvider projects the host-owned long-running goal
// record. It is not a Skill/MCP discovery entry and must not import the GUI
// goal action catalog or start the continuation engine. Field presence
// decides create/get/complete/fail. token_budget, max_turns, pause, resume,
// action, and project_path are rejected. This is not task.track.local or
// agent.delegate.subtask. The host process observes the goal store, so the
// handler result is the local completion receipt.
func ProjectReviewedHostGoalProvider(manager reviewedHostGoalManager) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if manager == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host goal manager is unavailable")
	}
	parameters := reviewedHostGoalInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host goal schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostGoalContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-goal-objective-or-status-or-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostGoalAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostGoalProviderID,
			ImplementationID: reviewedHostGoalImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityGoalManage,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostGoal(manager)}, nil
}

func AttachReviewedHostGoalProvider(catalog DynamicSemanticCatalog, manager reviewedHostGoalManager) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostGoalProvider(manager)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostGoal(manager reviewedHostGoalManager) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if manager == nil {
			return "", fmt.Errorf("host_goal_unavailable")
		}
		if len(args) > 3 {
			return "", fmt.Errorf("host_goal_arguments_rejected")
		}
		objective, status, note := "", "", ""
		for key, raw := range args {
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_goal_arguments_rejected")
			}
			switch key {
			case "objective":
				objective = value
			case "status":
				status = value
			case "note":
				note = value
			default:
				return "", fmt.Errorf("host_goal_arguments_rejected")
			}
		}
		objective, status, note = strings.TrimSpace(objective), strings.TrimSpace(status), strings.TrimSpace(note)
		if _, ok := reviewedHostGoalDispatch(objective, status, note); !ok {
			return "", fmt.Errorf("host_goal_field_presence_rejected")
		}
		return manager.ManageReviewedHostGoal(ctx, principal, objective, status, note)
	}
}

func (c *coreAgentCallbacks) ManageReviewedHostGoal(ctx context.Context, principal Principal, objective, status, note string) (string, error) {
	if c == nil || c.goals == nil {
		return "", fmt.Errorf("host_goal_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_goal_principal_mismatch")
	}
	objective, status, note = strings.TrimSpace(objective), strings.TrimSpace(status), strings.TrimSpace(note)
	op, ok := reviewedHostGoalDispatch(objective, status, note)
	if !ok {
		return "", fmt.Errorf("host_goal_field_presence_rejected")
	}
	if objective != "" && utf8.RuneCountInString(objective) > reviewedHostGoalObjectiveMax {
		return "", fmt.Errorf("host_goal_objective_too_large")
	}
	if note != "" && utf8.RuneCountInString(note) > reviewedHostGoalNoteMax {
		return "", fmt.Errorf("host_goal_note_too_large")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	ownerID := memoryOwnerIDForPrincipal(principal)
	switch op {
	case "create":
		if existing := c.goals.Get(ownerID); existing != nil && !existing.IsTerminal() {
			return "", fmt.Errorf("host_goal_already_active")
		}
		created, err := c.goals.Set(ownerID, objective)
		if err != nil {
			return "", err
		}
		return reviewedHostGoalProjection("created", created), nil
	case "update":
		parsed, statusOK := reviewedHostGoalStatus(status)
		if !statusOK {
			return "", fmt.Errorf("host_goal_status_rejected")
		}
		current := c.goals.Get(ownerID)
		if current == nil {
			return "", fmt.Errorf("host_goal_not_found")
		}
		if current.IsTerminal() {
			return "", fmt.Errorf("host_goal_already_terminal")
		}
		if !c.goals.UpdateStatus(ownerID, current.GoalID, parsed, note) {
			return "", fmt.Errorf("host_goal_update_failed")
		}
		updated := c.goals.Get(ownerID)
		if updated == nil {
			return "", fmt.Errorf("host_goal_update_failed")
		}
		return reviewedHostGoalProjection("updated", updated), nil
	default:
		current := c.goals.Get(ownerID)
		if current == nil {
			return "当前没有目标。", nil
		}
		return reviewedHostGoalProjection("current", current), nil
	}
}

func reviewedHostGoalProjection(kind string, g *goal.Goal) string {
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
