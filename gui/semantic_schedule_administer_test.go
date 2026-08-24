package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestIMSemanticScheduleUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelScheduleManage)}
	h.semanticTrustedSchedule = func(userID string, args semanticTrustedScheduleArgs) (string, error) {
		t.Fatalf("planning must not execute the administrator user=%q name=%q", userID, args.Name)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "列出定时任务", "lansenger", "root-sched", "turn-sched", scheduleManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedScheduleAdapter || selection.FitProof.MatchedCapability != tool.CapabilityScheduleAdministerLocal {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("schedule must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "manage_schedule",
		"schedule_administer", "create_scheduled_task", "list_scheduled_tasks")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["task_action"]; !ok || len(properties) != 11 {
		t.Fatalf("schedule schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "channel", "destination", "group_name", "group_id", "user_id",
		"delivery", "list_targets", "path", "run", "fire",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing schedule schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedScheduleAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"action":"create","group_name":"ops"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged dispatch fields=%q", got)
	}
}

func TestIMSemanticScheduleExecutesFieldPresenceWithoutDispatch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelScheduleManage)}
	var seen semanticTrustedScheduleArgs
	h.semanticTrustedSchedule = func(userID string, args semanticTrustedScheduleArgs) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seen = args
		return "定时任务已创建: standup [active] ID=t1 时间=09:00 操作=remind 下次=-", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "列出定时任务", "lansenger", "root-sched-exec", "turn-sched-exec", scheduleManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"name":"standup","task_action":"remind","hour":9}`)
	if !strings.Contains(got, "定时任务已创建") || strings.Contains(got, "group_name") {
		t.Fatalf("bound schedule=%q", got)
	}
	if seen.Name != "standup" || seen.TaskAction != "remind" || !seen.HasHour || seen.Hour != 9 || seen.ID != "" {
		t.Fatalf("dispatch=%#v", seen)
	}
	if replay := cb.ExecuteTool(name, `{"name":"standup","task_action":"remind","hour":9}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticScheduleRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelScheduleManage)}
	h.semanticTrustedSchedule = func(string, semanticTrustedScheduleArgs) (string, error) {
		return "定时任务已创建: x [file_base64:abc]", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "列出定时任务", "lansenger", "root-sched-both", "turn-sched-both", scheduleManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"name":"standup"}`); !strings.Contains(got, "trusted_schedule_field_presence_rejected") {
		t.Fatalf("name only=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "列出定时任务", "lansenger", "root-sched-token", "turn-sched-token", scheduleManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_schedule_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.administerTrustedSchedule("", semanticTrustedScheduleArgs{}); err == nil || !strings.Contains(err.Error(), "trusted_schedule_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticSchedulePersistsWithoutDelivery(t *testing.T) {
	manager, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	h := &IMMessageHandler{scheduledTaskManager: manager}
	created, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{
		Name: "standup", TaskAction: "remind", Hour: 9, HasHour: true,
	})
	if err != nil || !strings.Contains(created, "定时任务已创建") {
		t.Fatalf("create=%q err=%v", created, err)
	}
	listed, err := h.administerTrustedSchedule("user-2", semanticTrustedScheduleArgs{})
	if err != nil || !strings.Contains(listed, "standup") {
		t.Fatalf("process-wide residual list=%q err=%v", listed, err)
	}
	tasks := manager.List()
	if len(tasks) != 1 || tasks[0].Delivery != nil {
		t.Fatalf("created task=%#v", tasks)
	}
	if _, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{Name: "only-name"}); err == nil {
		t.Fatal("name without task_action and hour must fail closed")
	}
	if _, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{Hour: 25, HasHour: true, Name: "x", TaskAction: "y"}); err == nil {
		t.Fatal("invalid hour must fail closed")
	}
}
