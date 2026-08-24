package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func scheduleManageClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelScheduleManage, Confidence: .98}
}

func scheduleDispatchClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelScheduleDispatch, Confidence: .98}
}

func scheduleDispatchDestinationCtx(destination string) context.Context {
	return withSemanticDestination(context.Background(), destination)
}

func TestIMSemanticScheduleListIsManagedAdminister(t *testing.T) {
	managed, unmapped := imSemanticIntentCoverage(*scheduleManageClassification())
	if !managed || unmapped != "" {
		t.Fatalf("list schedule must be managed, managed=%v unmapped=%q", managed, unmapped)
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, resolved, err := semanticIntentNeedsFromClassification(registry, *scheduleManageClassification())
	if err != nil || !resolved || len(needs) != 1 || needs[0].Capability != tool.CapabilityScheduleAdministerLocal {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, resolved, err)
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "list scheduled tasks", "desktop", "root-sched-list", "turn-sched-list", scheduleManageClassification(),
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("list must plan, handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, tool.CapabilityScheduleAdministerLocal) {
		t.Fatalf("list selections=%#v", prepared.plan.Selections)
	}
	for _, selection := range prepared.plan.Selections {
		if selection.AdapterName == "manage_schedule" {
			t.Fatal("managed list selected the merged manage_schedule handler")
		}
		if selection.FitProof.MatchedCapability == tool.CapabilityScheduleAdministerLocal && selection.AdapterName != semanticTrustedScheduleAdapter {
			t.Fatalf("administer adapter=%q", selection.AdapterName)
		}
	}
}

func TestIMSemanticScheduleDispatchDoesNotMaterializeAdministerOnly(t *testing.T) {
	managed, unmapped := imSemanticIntentCoverage(*scheduleDispatchClassification())
	if !managed || unmapped != "" {
		t.Fatalf("dispatch must stay on the managed path, managed=%v unmapped=%q", managed, unmapped)
	}
	registry := newIMSemanticCapabilityRegistry()
	needs, resolved, err := semanticIntentNeedsFromClassification(registry, *scheduleDispatchClassification())
	if err != nil || !resolved || len(needs) != 2 {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, resolved, err)
	}
	foundAdmin, foundDispatch := false, false
	for _, need := range needs {
		switch need.Capability {
		case tool.CapabilityScheduleAdministerLocal:
			foundAdmin = need.Required
		case tool.CapabilityScheduleDispatchChannel:
			foundDispatch = need.Required
		}
	}
	if !foundAdmin || !foundDispatch {
		t.Fatalf("dispatch needs=%#v", needs)
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user", "send to the group every morning", "desktop", "root-sched-dispatch", "turn-sched-dispatch", scheduleDispatchClassification(),
	)
	if !handled {
		t.Fatal("dispatch must stay managed")
	}
	if err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("dispatch must fail closed as unmet, err=%v", err)
	}
	if surface != nil || len(defs) != 0 {
		t.Fatalf("dispatch must not materialize administer-only grants: defs=%#v surface=%#v", defs, surface)
	}
	prepared, _, planErr := h.semanticPlanForTurnWithClassification(
		"user", "send to the group every morning", "desktop", "root-sched-dispatch-plan", "turn-sched-dispatch-plan", scheduleDispatchClassification(),
	)
	if planErr == nil || !strings.Contains(planErr.Error(), "unmet") {
		t.Fatalf("plan must report unmet dispatch, err=%v", planErr)
	}
	if prepared != nil {
		for _, item := range prepared.plan.Unmet {
			switch item.ReasonCode {
			case "policy_denied", "no_feasible_provider", "catalog_incomplete":
				return
			}
		}
		t.Fatalf("unmet=%#v", prepared.plan.Unmet)
	}
}

func TestScheduleAdministerRejectsDeliveryBinding(t *testing.T) {
	h := &IMMessageHandler{}
	got := h.toolAdministerScheduledTask(map[string]interface{}{
		"action": "create", "name": "daily", "task_action": "remind", "hour": 9,
		"delivery": map[string]interface{}{"enabled": true, "channel": "lansenger"},
	})
	if !strings.Contains(got, "不能绑定渠道投递") {
		t.Fatalf("create with delivery=%q", got)
	}
	got = h.toolAdministerScheduledTask(map[string]interface{}{
		"action": "create", "name": "daily", "task_action": "remind", "hour": 9, "group_name": "ops",
	})
	if !strings.Contains(got, "不能绑定渠道投递") {
		t.Fatalf("create with group_name=%q", got)
	}
}

func TestScheduleAdministerSchemaHasNoDeliveryFields(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	entry, ok := h.registry.Get("schedule_administer")
	if !ok {
		t.Fatal("schedule_administer missing")
	}
	schema, err := semanticInvocationSchema(map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":       "schedule_administer",
			"parameters": entry.InputSchema,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	properties, _ := schema["properties"].(map[string]interface{})
	if properties == nil {
		properties = entry.InputSchema
	}
	for _, blocked := range []string{"channel", "destination", "path", "delivery", "group_name", "group_id", "user_id"} {
		if _, found := properties[blocked]; found {
			t.Fatalf("administer schema still exposes %s", blocked)
		}
	}
	if extractToolName(map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "manage_schedule"}}) == "schedule_administer" {
		t.Fatal("name collision")
	}
}

func TestIMSemanticScheduleDispatchMaterializesWithTrustedGroup(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	for _, channel := range []string{"desktop", "tui", "lansenger"} {
		ctx := scheduleDispatchDestinationCtx("group:research")
		prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
			ctx, "user", "send to the group every morning", channel, "root-sched-ok-"+channel, "turn-sched-ok-"+channel, scheduleDispatchClassification(), nil,
		)
		if err != nil || !handled || prepared == nil || len(prepared.plan.Unmet) != 0 {
			t.Fatalf("channel=%s prepared=%#v handled=%v err=%v", channel, prepared, handled, err)
		}
		if !planHasCapabilities(prepared.plan, tool.CapabilityScheduleAdministerLocal, tool.CapabilityScheduleDispatchChannel) {
			t.Fatalf("channel=%s selections=%#v", channel, prepared.plan.Selections)
		}
		var administerID, dispatchAdapter string
		for _, selection := range prepared.plan.Selections {
			if selection.AdapterName == "manage_schedule" {
				t.Fatalf("channel=%s selected merged manage_schedule", channel)
			}
			if selection.FitProof.MatchedCapability == tool.CapabilityScheduleAdministerLocal {
				if selection.AdapterName != semanticTrustedScheduleAdapter {
					t.Fatalf("channel=%s administer adapter=%q", channel, selection.AdapterName)
				}
				administerID = selection.ID
			}
			if selection.FitProof.MatchedCapability == tool.CapabilityScheduleDispatchChannel {
				if selection.AdapterName != "semantic_schedule_dispatch" {
					t.Fatalf("channel=%s dispatch adapter=%q", channel, selection.AdapterName)
				}
				if len(selection.Effects) != 1 || selection.Effects[0] != tool.EffectExternalEffect {
					t.Fatalf("channel=%s dispatch effects=%#v", channel, selection.Effects)
				}
				dispatchAdapter = selection.AdapterName
				foundAdmin := false
				for _, requirement := range selection.Requires {
					if requirement == administerID {
						foundAdmin = true
					}
				}
				if administerID != "" && !foundAdmin {
					t.Fatalf("channel=%s dispatch Requires=%#v, want administer", channel, selection.Requires)
				}
			}
		}
		if dispatchAdapter == "" {
			t.Fatalf("channel=%s missing dispatch selection", channel)
		}
	}
}

func TestIMSemanticScheduleDispatchFailsClosedWithoutTrustedDestination(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	for _, destination := range []string{"", "research-group", "group:", "user:"} {
		ctx := context.Background()
		if destination != "" {
			ctx = scheduleDispatchDestinationCtx(destination)
		}
		defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
			ctx, "user", "send to the group every morning", "desktop", "root-sched-nodest", "turn-sched-nodest", scheduleDispatchClassification(), nil,
		)
		if !handled {
			t.Fatalf("destination=%q must stay managed", destination)
		}
		if err == nil || !strings.Contains(err.Error(), "unmet") {
			t.Fatalf("destination=%q must fail closed, err=%v", destination, err)
		}
		if surface != nil || len(defs) != 0 {
			t.Fatalf("destination=%q must not materialize administer-only: defs=%#v", destination, defs)
		}
	}
}

func TestIMSemanticScheduleDispatchUnsupportedChannelStaysUnmet(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	ctx := scheduleDispatchDestinationCtx("group:research")
	for _, channel := range []string{"weixin", "ve_group_executor"} {
		prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
			ctx, "user", "send to the group every morning", channel, "root-sched-"+channel, "turn-sched-"+channel, scheduleDispatchClassification(), nil,
		)
		if !handled {
			t.Fatalf("channel=%s must stay managed", channel)
		}
		if err == nil || !strings.Contains(err.Error(), "unmet") {
			t.Fatalf("channel=%s must fail closed, err=%v prepared=%#v", channel, err, prepared)
		}
	}
}

func TestScheduleDispatchSchemaHasNoDeliveryFields(t *testing.T) {
	schema, err := semanticInvocationSchema(semanticScheduleDispatchDefinition())
	if err != nil {
		t.Fatal(err)
	}
	properties, _ := schema["properties"].(map[string]interface{})
	for _, blocked := range []string{"channel", "destination", "path", "delivery", "group_name", "group_id", "user_id"} {
		if _, found := properties[blocked]; found {
			t.Fatalf("dispatch schema still exposes %s", blocked)
		}
	}
}

func TestScheduleDispatchPreparesDeliveryRecordAndIsNotSent(t *testing.T) {
	manager, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	h := &IMMessageHandler{registry: NewToolRegistry(), scheduledTaskManager: manager}
	registerBuiltinTools(h.registry, h)
	ctx := scheduleDispatchDestinationCtx("group:research")
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, "user", "send to the group every morning", "desktop", "root-sched-prep", "turn-sched-prep", scheduleDispatchClassification(), nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%#v err=%v", handled, surface, err)
	}
	var dispatch tool.PlannedSelection
	var administerName string
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityScheduleDispatchChannel {
			dispatch = selection
		}
	}
	for name, grant := range surface.grants {
		if grant.AdapterName == semanticTrustedScheduleAdapter {
			administerName = name
		}
	}
	if dispatch.ID == "" || administerName == "" {
		t.Fatalf("plan selections=%#v grants=%#v", surface.plan.Selections, surface.grants)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop",
		loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "group:research"}},
	}
	created := cb.ExecuteTool(administerName, `{"name":"morning-brief","task_action":"remind","hour":9}`)
	if !strings.Contains(created, "定时任务已创建") {
		t.Fatalf("create=%q", created)
	}
	var createdTask *scheduler.ScheduledTask
	for _, task := range manager.List() {
		if task.Name == "morning-brief" {
			copyTask := task
			createdTask = &copyTask
		}
	}
	if createdTask == nil || createdTask.Delivery != nil {
		t.Fatalf("managed create must not write Delivery: %#v", createdTask)
	}
	if err := (&App{}).deliverScheduledTaskResult(createdTask, "ok", nil); err != nil {
		t.Fatal(err)
	}
	if createdTask.Delivery != nil {
		t.Fatal("legacy fire path must not attach Delivery after create")
	}
	var dispatchName string
	for name, grant := range surface.grants {
		if grant.AdapterName == "semantic_schedule_dispatch" {
			dispatchName = name
		}
	}
	if dispatchName == "" {
		t.Fatalf("dispatch grant missing after administer completed: %#v", surface.grants)
	}
	if _, err := tool.CanonicalizeInvocationArguments(`{"delivery":{"channel":"lansenger","group_name":"ops"}}`, surface.parameterSchemas["semantic_schedule_dispatch"]); err == nil {
		t.Fatal("model delivery fields must be rejected by the dispatch schema")
	}
	got := cb.ExecuteTool(dispatchName, `{}`)
	if !strings.Contains(got, "prepared") || strings.Contains(got, "已发送") || !strings.Contains(got, "not a send") {
		t.Fatalf("dispatch must prepare, not send: %q", got)
	}
	if cb.semanticDelivery == nil || cb.semanticDelivery.DestinationID != "group:research" {
		t.Fatalf("prepared projection=%#v", cb.semanticDelivery)
	}
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if resp.FileData != "" || resp.ImageKey != "" || resp.SemanticDelivery != nil {
		t.Fatalf("dispatch must not project a current-channel send: %+v", resp)
	}
	record, err := surface.artifacts.store.Delivery(surface.scope, dispatch.ID)
	if err != nil || record.State != tool.DeliveryPrepared {
		t.Fatalf("prepared record=%#v err=%v", record, err)
	}
	if record.ChannelScope != "desktop" || record.DestinationID != "group:research" {
		t.Fatalf("record target=%+v", record)
	}
	execution, err := surface.executor.Execution(surface.scope, dispatch.ID)
	if err == nil && execution.State == tool.PlanExecutionSucceeded {
		t.Fatal("dispatch prepare must not project PlanExecutionSucceeded")
	}
	if _, claimed, err := surface.artifacts.store.ClaimDeliveryDispatch(surface.scope, dispatch.ID, time.Now().UTC()); err != nil || !claimed {
		t.Fatalf("first CAS claim claimed=%t err=%v", claimed, err)
	}
	if claim, claimed, err := surface.artifacts.store.ClaimDeliveryDispatch(surface.scope, dispatch.ID, time.Now().UTC()); err != nil || claimed || claim.State != tool.DeliveryDispatching {
		t.Fatalf("second CAS must not replay claim=%#v claimed=%t err=%v", claim, claimed, err)
	}
}

func TestManagedScheduleDispatchFireRequiresBinding(t *testing.T) {
	if !managedScheduleDispatchFireWired() {
		t.Fatal("due-time fire should be admitted for host-owned bindings")
	}
	task := &scheduler.ScheduledTask{ID: "legacy", Name: "old", Delivery: &scheduler.TaskDelivery{Enabled: true, Channel: "lansenger"}}
	if task.Delivery == nil || !task.Delivery.Enabled {
		t.Fatal("legacy tasks keep Delivery for the old dispatcher")
	}
	managed := &scheduler.ScheduledTask{ID: "managed", Name: "new"}
	if err := (&App{}).deliverScheduledTaskResult(managed, "done", nil); err != nil {
		t.Fatal(err)
	}
	if managed.Delivery != nil {
		t.Fatal("managed fire no-op must not invent Delivery")
	}
}
