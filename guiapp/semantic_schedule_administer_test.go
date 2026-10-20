package guiapp

import (
	"fmt"
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

func TestScheduleUtteranceObligesMutation(t *testing.T) {
	for _, text := range []string{
		"创建定时任务，每小时清除一下rapidai邮箱。",
		"创建一个每小时清空邮箱的定时任务",
		"创建定时任务，先列出已有的",
		"删除定时任务",
		"暂停这个定时任务",
		"每小时清除一下rapidai邮箱",
		"create a scheduled task to empty my mailbox",
		"update the daily reminder",
		"remove the scheduled task",
		"stop the scheduled task",
		"取消这个定时任务",
		"删除这个定时任务",
		"删掉这个定时任务",
		"建立一个定时任务",
		"关掉这个定时任务",
		"不要只列出，创建一个定时任务",
		"不要删除，但是暂停这个定时任务",
		"特别删除这个定时任务",
		"把不要的定时任务删掉",
		"设置一个定时任务",
		"列出定时任务，每小时清除一下rapidai邮箱",
		"每小时查看一下rapidai邮箱",
		"列出定时任务，每小时查看一下rapidai邮箱",
	} {
		if !scheduleUtteranceObligesMutation(text) {
			t.Errorf("owes a change: %s", text)
		}
	}
	for _, text := range []string{
		"", "列出定时任务", "查看定时任务", "有哪些定时任务", "list scheduled tasks", "列出每小时的定时任务",
		"列出我创建的定时任务", "查看我设置的定时任务", "我设置的定时任务有哪些",
		"不要删除定时任务", "别暂停这个定时任务", "do not create a scheduled task",
		"不要把这个定时任务删掉", "别把定时任务暂停", "别再删除定时任务",
		"每天的邮件很多", "帮我设置邮件签名", "每小时的新闻",
		"不要每小时清除一下rapidai邮箱",
		"列出每小时执行的定时任务",
		"查看每小时的定时任务",
	} {
		if scheduleUtteranceObligesMutation(text) {
			t.Errorf("list only: %s", text)
		}
	}
}

func TestTrustedScheduleRangePrecedesPresence(t *testing.T) {
	_, err := semanticTrustedScheduleArgsAllowed(map[string]interface{}{
		"name": "hourly", "task_action": "clear", "interval_minutes": float64(-1),
	})
	if err == nil || !strings.Contains(err.Error(), "trusted_schedule_interval_rejected") {
		t.Fatalf("negative interval err=%v", err)
	}
	_, err = semanticTrustedScheduleArgsAllowed(map[string]interface{}{
		"name": "hourly", "task_action": "clear", "hour": float64(25),
	})
	if err == nil || !strings.Contains(err.Error(), "trusted_schedule_hour_rejected") {
		t.Fatalf("hour err=%v", err)
	}
	_, err = semanticTrustedScheduleArgsAllowed(map[string]interface{}{
		"name": "hourly", "task_action": "clear", "interval_minutes": float64(0),
	})
	if err == nil || !strings.Contains(err.Error(), "trusted_schedule_field_presence_rejected") {
		t.Fatalf("zero interval err=%v", err)
	}
	_, err = semanticTrustedScheduleArgsAllowed(map[string]interface{}{
		"name": "hourly", "task_action": "clear", "hour": float64(9), "end_date": "2026/10/20",
	})
	if err == nil || !strings.Contains(err.Error(), "trusted_schedule_end_date_rejected") {
		t.Fatalf("end date err=%v", err)
	}
	_, err = semanticTrustedScheduleArgsAllowed(map[string]interface{}{
		"name": "hourly", "task_action": "clear", "hour": float64(9),
		"start_date": "2026-12-01", "end_date": "2026-11-01",
	})
	if err == nil || !strings.Contains(err.Error(), "trusted_schedule_date_range_rejected") {
		t.Fatalf("date range err=%v", err)
	}
}

func TestScheduleListFollowUpOpensOneWrite(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.semanticInvocationMu.Lock()
		coordinator := app.semanticExecutionCoordinator
		app.semanticInvocationMu.Unlock()
		if coordinator != nil {
			_ = coordinator.Close()
		}
	})
	h := &IMMessageHandler{app: app, registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelScheduleManage)}
	h.semanticTrustedSchedule = func(userID string, args semanticTrustedScheduleArgs) (string, error) {
		op, ok := semanticTrustedScheduleDispatch(args)
		if !ok {
			return "", fmt.Errorf("trusted_schedule_field_presence_rejected")
		}
		if op == "list" {
			return "当前没有定时任务。", nil
		}
		if op != "create" || args.HasHour || !args.HasInterval || args.IntervalMinutes != 60 {
			t.Fatalf("hourly create=%#v op=%s", args, op)
		}
		return "定时任务已创建: " + args.Name, nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "创建定时任务，每小时清除一下rapidai邮箱。", "lansenger", "root-sched-follow", "turn-sched-follow", scheduleManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	before := len(surface.plan.Selections)
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, userID: "user-1",
		userText: "创建定时任务，每小时清除一下rapidai邮箱。",
	}
	listed := cb.ExecuteTool(name, `{}`)
	if !strings.Contains(listed, "当前没有定时任务") {
		t.Fatalf("list=%q", listed)
	}
	if len(surface.plan.Selections) != before+1 {
		t.Fatalf("selections=%d before=%d", len(surface.plan.Selections), before)
	}
	if !surface.plan.Selections[len(surface.plan.Selections)-1].Continuation {
		t.Fatal("the list follow-up was not the one continuation")
	}
	if cb.scheduleAdministerListed {
		t.Fatal("the list flag stayed set after the follow-up was minted")
	}
	created := cb.ExecuteTool(name, `{"name":"clear-rapidai-mailbox","task_action":"clear the rapidai mailbox","interval_minutes":60}`)
	if strings.Contains(created, "invocation_grant_replayed") || !strings.Contains(created, "定时任务已创建") {
		t.Fatalf("create=%q", created)
	}
	if len(surface.plan.Selections) != before+1 {
		t.Fatalf("create grew the family to %d", len(surface.plan.Selections))
	}
	replay := cb.ExecuteTool(name, `{"name":"clear-rapidai-mailbox","task_action":"clear the rapidai mailbox","interval_minutes":60}`)
	if !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("second create=%q", replay)
	}
}

func TestScheduleListFlagDoesNotLeakAcrossCapabilities(t *testing.T) {
	scheduleID := "selection:need:schedule.administer.local:abc"
	writeID := "selection:need:fs.write.local:abc"
	surface := &semanticCallSurface{
		plan: tool.ToolPlan{Selections: []tool.PlannedSelection{
			{ID: scheduleID, NeedID: "need:schedule.administer.local:abc", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityScheduleAdministerLocal}},
			{ID: writeID, NeedID: "need:fs.write.local:abc", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityFSWriteLocal}},
		}},
		completed:    map[string]bool{},
		materialized: map[string]bool{scheduleID: true, writeID: true},
	}
	cb := &sharedAgentLoopCallbacks{
		semanticSurface: surface, scheduleAdministerListed: true,
		userText: "创建定时任务，每小时清除一下rapidai邮箱。",
	}
	cb.ensureScheduleObservationFollowUp(writeID)
	if !cb.scheduleAdministerListed || len(surface.plan.Selections) != 2 {
		t.Fatalf("unrelated advance selections=%d flag=%v", len(surface.plan.Selections), cb.scheduleAdministerListed)
	}
	cb.userText = "列出定时任务"
	cb.ensureScheduleObservationFollowUp(scheduleID)
	if cb.scheduleAdministerListed || len(surface.plan.Selections) != 2 {
		t.Fatalf("list-only selections=%d flag=%v", len(surface.plan.Selections), cb.scheduleAdministerListed)
	}
	cb.scheduleAdministerListed = true
	cb.userText = "创建定时任务，每小时清除一下rapidai邮箱。"
	cb.ensureScheduleObservationFollowUp(scheduleID)
	if len(surface.plan.Selections) != 3 || !surface.plan.Selections[2].Continuation || cb.scheduleAdministerListed {
		t.Fatalf("in-memory follow-up selections=%d flag=%v", len(surface.plan.Selections), cb.scheduleAdministerListed)
	}
	cb.scheduleAdministerListed = true
	cb.ensureScheduleObservationFollowUp(scheduleID)
	if len(surface.plan.Selections) != 3 {
		t.Fatalf("second list grew selections to %d", len(surface.plan.Selections))
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
	if err != nil || !strings.Contains(created, "定时任务已创建") || !strings.Contains(created, "时间=09:00") {
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
	hourly, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{
		Name: "clear-rapidai", TaskAction: "clear the rapidai mailbox", HasInterval: true, IntervalMinutes: 60,
	})
	if err != nil || !strings.Contains(hourly, "定时任务已创建") || !strings.Contains(hourly, "时间=每1小时") || strings.Contains(hourly, "时间=00:00") {
		t.Fatalf("interval create=%q err=%v", hourly, err)
	}
	tasks = manager.List()
	var hourlyTask *scheduler.ScheduledTask
	for i := range tasks {
		if tasks[i].Name == "clear-rapidai" {
			hourlyTask = &tasks[i]
		}
	}
	if hourlyTask == nil || hourlyTask.IntervalMinutes != 60 || hourlyTask.Hour != 0 || hourlyTask.NextRunAt == nil {
		t.Fatalf("hourly task=%#v", tasks)
	}
	if _, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{Name: "only-name"}); err == nil {
		t.Fatal("name without task_action and a schedule must fail closed")
	}
	if _, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{Name: "no-when", TaskAction: "ping"}); err == nil {
		t.Fatal("name and task_action without hour or interval must fail closed")
	}
	if _, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{Hour: 25, HasHour: true, Name: "x", TaskAction: "y"}); err == nil {
		t.Fatal("invalid hour must fail closed")
	}
}

// TestTrustedScheduleCreateKeepsInitialStatus pins the payload that the host
// rejected as field presence: a complete hourly create that also names the
// store's own initial status. status is not an operation. active stores the
// running record Add already writes, and paused stores it paused.
func TestTrustedScheduleCreateKeepsInitialStatus(t *testing.T) {
	parsed, err := semanticTrustedScheduleArgsAllowed(map[string]interface{}{
		"name":             "每小时清理 rapidai 邮箱",
		"task_action":      "删除 mayong@rapidai.tech 邮箱中的所有邮件",
		"interval_minutes": float64(60),
		"minute":           float64(0),
		"status":           "active",
	})
	if err != nil {
		t.Fatalf("active hourly create err=%v", err)
	}
	if op, ok := semanticTrustedScheduleDispatch(parsed); !ok || op != "create" {
		t.Fatalf("dispatch=%q ok=%v", op, ok)
	}
	manager, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	h := &IMMessageHandler{scheduledTaskManager: manager}
	created, err := h.administerTrustedSchedule("user-1", parsed)
	if err != nil || !strings.Contains(created, "[active]") || !strings.Contains(created, "时间=每1小时") {
		t.Fatalf("create=%q err=%v", created, err)
	}
	tasks := manager.List()
	if len(tasks) != 1 || tasks[0].Status != "active" || tasks[0].IntervalMinutes != 60 || tasks[0].Minute != 0 || tasks[0].Hour != 0 || tasks[0].NextRunAt == nil {
		t.Fatalf("stored=%#v", tasks)
	}
	updated, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{
		ID: tasks[0].ID, TaskAction: "hold", Status: "paused",
	})
	if err != nil || !strings.Contains(updated, "[paused]") || !strings.Contains(updated, "hold") {
		t.Fatalf("update=%q err=%v", updated, err)
	}
	changed := manager.Get(tasks[0].ID)
	if changed == nil || changed.Status != "paused" || changed.Action != "hold" || changed.NextRunAt != nil {
		t.Fatalf("updated record=%#v", changed)
	}

	paused, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{
		Name: "held", TaskAction: "wait", HasHour: true, Hour: 9, Status: "pause",
	})
	if err != nil || !strings.Contains(paused, "[paused]") || strings.Contains(paused, "[active]") {
		t.Fatalf("paused create=%q err=%v", paused, err)
	}
	var held *scheduler.ScheduledTask
	for i := range manager.List() {
		item := manager.List()[i]
		if item.Name == "held" {
			held = &item
		}
	}
	if held == nil || held.Status != "paused" || held.NextRunAt != nil {
		t.Fatalf("paused record=%#v", held)
	}

	if _, err := semanticTrustedScheduleArgsAllowed(map[string]interface{}{"status": "active"}); err == nil || !strings.Contains(err.Error(), "trusted_schedule_field_presence_rejected") {
		t.Fatalf("status without a record err=%v", err)
	}
	if op, ok := semanticTrustedScheduleDispatch(semanticTrustedScheduleArgs{ID: "task-1", Status: "paused"}); !ok || op != "update" {
		t.Fatalf("id plus status dispatch=%q ok=%v", op, ok)
	}
	if _, err := h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{
		Name: "bad", TaskAction: "clear", HasInterval: true, IntervalMinutes: 60, Status: "disabled",
	}); err == nil || !strings.Contains(err.Error(), "trusted_schedule_status_rejected") {
		t.Fatalf("invented status on a create err=%v", err)
	}
}

// TestTrustedScheduleStatusVocabulary pins the alias shape at this host: the
// schema advertises only canonical names, while the runtime also accepts their
// aliases and the empty-is-unset reading. Both come from the shared schedule
// contract, so this host cannot drift from the agent-service surface serving
// the same capability.
func TestTrustedScheduleStatusVocabulary(t *testing.T) {
	schema := semanticTrustedScheduleInvocationSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schedule schema has no properties: %#v", schema)
	}
	status, ok := properties["status"].(map[string]interface{})
	if !ok {
		t.Fatalf("schedule schema has no status property: %#v", properties)
	}
	enum, ok := status["enum"].([]string)
	if !ok || len(enum) == 0 {
		t.Fatalf("status must publish a non-empty enum, got %#v", status["enum"])
	}
	for _, name := range enum {
		canonical, ok := semanticTrustedScheduleStatus(name)
		if !ok || canonical != name {
			t.Fatalf("schema advertises status %q, but the runtime maps it to (%q, %v)", name, canonical, ok)
		}
	}
	for sent, want := range map[string]string{"pause": "paused", "resume": "active", "PAUSED": "paused"} {
		canonical, ok := semanticTrustedScheduleStatus(sent)
		if !ok || canonical != want {
			t.Fatalf("alias %q maps to (%q, %v), want %q", sent, canonical, ok, want)
		}
	}
	if _, ok := semanticTrustedScheduleStatus(""); !ok {
		t.Fatal("empty status must stay accepted-as-unset")
	}
	// An invented status stays refused, and the refusal names the canonical
	// vocabulary so the first failure can redirect the turn instead of ending it.
	for _, invented := range []string{"disabled", "enabled", "off"} {
		_, err := administerScheduleStatusProbe(t, invented)
		if err == nil || !strings.Contains(err.Error(), "trusted_schedule_status_rejected") {
			t.Fatalf("status %q = %v, want trusted_schedule_status_rejected", invented, err)
		}
		for _, valid := range tool.ScheduleStatusOptions() {
			if !strings.Contains(err.Error(), valid) {
				t.Fatalf("refusal for %q = %v does not mention %q", invented, err, valid)
			}
		}
	}
}

// administerScheduleStatusProbe drives one invented status through the real
// administer path. A host with a backing manager reaches the status check the
// same way an update turn does: presence decides update, and the earlier name
// and range gates accept an id-plus-status record.
func administerScheduleStatusProbe(t *testing.T, status string) (string, error) {
	t.Helper()
	h := &IMMessageHandler{}
	mgr, createErr := scheduler.NewManager(filepath.Join(t.TempDir(), "schedules.json"))
	if createErr != nil {
		return "", createErr
	}
	defer mgr.Stop()
	h.SetScheduledTaskManager(mgr)
	return h.administerTrustedSchedule("user-1", semanticTrustedScheduleArgs{ID: "task-1", Status: status})
}
