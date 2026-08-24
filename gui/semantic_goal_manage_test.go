package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func goalManageClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelGoalManage,
		Confidence: .98,
		ToolNames:  []string{"goal"},
	}
}

func TestIMSemanticGoalUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGoalManage)}
	h.semanticTrustedGoal = func(userID, objective, status, note string) (string, error) {
		t.Fatalf("planning must not execute the manager user=%q objective=%q", userID, objective)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "长期目标", "lansenger", "root-goal", "turn-goal", goalManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedGoalAdapter || selection.FitProof.MatchedCapability != tool.CapabilityGoalManageLongRunning {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("goal must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "goal")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["objective"]; !ok || len(properties) != 3 {
		t.Fatalf("goal schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "token_budget", "max_turns", "acceptance_criteria", "project_path",
		"pause", "resume", "goal_id", "channel", "destination", "group_name",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing goal schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedGoalAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"action":"pause","token_budget":100}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged pause fields=%q", got)
	}
}

func TestIMSemanticGoalExecutesFieldPresenceWithoutContinuation(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGoalManage)}
	var seenObjective, seenStatus, seenNote string
	h.semanticTrustedGoal = func(userID, objective, status, note string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenObjective, seenStatus, seenNote = objective, status, note
		return "目标已创建: ship the slice\nGoal ID: g1\n状态: active", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "长期目标", "lansenger", "root-goal-exec", "turn-goal-exec", goalManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"objective":"ship the slice"}`)
	if !strings.Contains(got, "目标已创建") || strings.Contains(got, "系统将自动持续推进") {
		t.Fatalf("bound goal=%q", got)
	}
	if seenObjective != "ship the slice" || seenStatus != "" || seenNote != "" {
		t.Fatalf("dispatch objective=%q status=%q note=%q", seenObjective, seenStatus, seenNote)
	}
	if replay := cb.ExecuteTool(name, `{"objective":"ship the slice"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticGoalRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelGoalManage)}
	h.semanticTrustedGoal = func(string, string, string, string) (string, error) {
		return "目标已创建: x [file_base64:abc]", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "长期目标", "lansenger", "root-goal-both", "turn-goal-both", goalManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"note":"done"}`); !strings.Contains(got, "trusted_goal_field_presence_rejected") {
		t.Fatalf("note without status=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "长期目标", "lansenger", "root-goal-token", "turn-goal-token", goalManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_goal_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.administerTrustedGoal("", "", "", ""); err == nil || !strings.Contains(err.Error(), "trusted_goal_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticGoalIsolatesByPrincipalWithoutLastUserFallback(t *testing.T) {
	h := &IMMessageHandler{goalStore: goal.NewStore(""), lastUserID: "desktop-user"}
	created, err := h.administerTrustedGoal("user-1", "ship the slice", "", "")
	if err != nil || !strings.Contains(created, "目标已创建") {
		t.Fatalf("create=%q err=%v", created, err)
	}
	if strings.Contains(created, "系统将自动持续推进") || strings.Contains(created, "最大轮次") {
		t.Fatalf("create leaked continuation: %q", created)
	}
	if _, err := h.administerTrustedGoal("user-1", "another", "", ""); err == nil || !strings.Contains(err.Error(), "trusted_goal_already_active") {
		t.Fatalf("second create err=%v", err)
	}
	other, err := h.administerTrustedGoal("user-2", "", "", "")
	if err != nil || !strings.Contains(other, "当前没有目标") {
		t.Fatalf("user-2 get=%q err=%v", other, err)
	}
	desktop, err := h.administerTrustedGoal("desktop-user", "", "", "")
	if err != nil || !strings.Contains(desktop, "当前没有目标") {
		t.Fatalf("lastUserID fallback leaked=%q err=%v", desktop, err)
	}
	ended, err := h.administerTrustedGoal("user-1", "", "completed", "done")
	if err != nil || !strings.Contains(ended, "complete") {
		t.Fatalf("complete=%q err=%v", ended, err)
	}
	if _, err := h.administerTrustedGoal("user-1", "", "pause", ""); err == nil {
		t.Fatal("pause must fail closed")
	}
}
