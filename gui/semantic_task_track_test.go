package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func taskTrackClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelTaskTrack,
		Confidence: .98,
		ToolNames:  []string{"task"},
	}
}

func TestIMSemanticTaskUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelTaskTrack)}
	h.semanticTrustedTask = func(userID, title, description, id, status, note string) (string, error) {
		t.Fatalf("planning must not execute the tracker user=%q title=%q", userID, title)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "待办清单", "lansenger", "root-task", "turn-task", taskTrackClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedTaskAdapter || selection.FitProof.MatchedCapability != tool.CapabilityTaskTrackLocal {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("task must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "task")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["title"]; !ok || len(properties) != 5 {
		t.Fatalf("task schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "task_id", "delegate_to", "depends_on", "status_note",
		"channel", "destination", "group_name",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing task schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedTaskAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"action":"delegate","delegate_to":"bot"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged delegate fields=%q", got)
	}
}

func TestIMSemanticTaskExecutesFieldPresenceWithoutDelegate(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelTaskTrack)}
	var seenTitle, seenID, seenStatus, seenNote string
	h.semanticTrustedTask = func(userID, title, description, id, status, note string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenTitle, seenID, seenStatus, seenNote = title, id, status, note
		return "任务已创建: t1 [pending] write docs", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "待办清单", "lansenger", "root-task-exec", "turn-task-exec", taskTrackClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"title":"write docs"}`)
	if !strings.Contains(got, "任务已创建") || strings.Contains(got, "delegate") {
		t.Fatalf("bound task=%q", got)
	}
	if seenTitle != "write docs" || seenID != "" || seenStatus != "" || seenNote != "" {
		t.Fatalf("dispatch title=%q id=%q status=%q note=%q", seenTitle, seenID, seenStatus, seenNote)
	}
	if replay := cb.ExecuteTool(name, `{"title":"write docs"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticTaskRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelTaskTrack)}
	h.semanticTrustedTask = func(string, string, string, string, string, string) (string, error) {
		return "任务已创建: t1 [pending] x [voice_base64:abc]", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "待办清单", "lansenger", "root-task-both", "turn-task-both", taskTrackClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"status":"completed"}`); !strings.Contains(got, "trusted_task_field_presence_rejected") {
		t.Fatalf("status without id=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "待办清单", "lansenger", "root-task-token", "turn-task-token", taskTrackClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_task_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.administerTrustedTask("", "", "", "", "", ""); err == nil || !strings.Contains(err.Error(), "trusted_task_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

// One handler serves many IM users from one process. This used to assert the
// opposite — that user-2 could read user-1's list — as a record of the leak.
func TestOneUsersTodoListIsNotAnothersToRead(t *testing.T) {
	h := &IMMessageHandler{taskStore: task.NewStore()}
	created, err := h.administerTrustedTask("user-1", "write docs", "", "", "", "")
	if err != nil || !strings.Contains(created, "任务已创建") {
		t.Fatalf("create=%q err=%v", created, err)
	}
	listed, err := h.administerTrustedTask("user-2", "", "", "", "", "")
	if err != nil {
		t.Fatalf("list err=%v", err)
	}
	if strings.Contains(listed, "write docs") {
		t.Fatalf("user-2 was shown user-1's task: %q", listed)
	}
	own, err := h.administerTrustedTask("user-1", "", "", "", "", "")
	if err != nil || !strings.Contains(own, "write docs") {
		t.Fatalf("user-1 lost their own task: %q err=%v", own, err)
	}
}

// Reaching another principal's task by ID must read as absence. Reporting a
// distinct "not yours" would confirm the ID exists, which is the disclosure
// the scoping is for.
func TestAnotherUsersTaskIDReadsAsAbsent(t *testing.T) {
	h := &IMMessageHandler{taskStore: task.NewStore()}
	if _, err := h.administerTrustedTask("user-1", "write docs", "", "", "", ""); err != nil {
		t.Fatalf("create err=%v", err)
	}
	// user-1's first task is task-1; user-2 guesses it.
	_, err := h.administerTrustedTask("user-2", "", "", "task-1", "completed", "")
	if err == nil {
		t.Fatal("user-2 updated user-1's task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err=%v, want an absence rather than an ownership disclosure", err)
	}
	if _, err := h.administerTrustedTask("user-2", "", "", "task-1", "", ""); err == nil {
		t.Fatal("user-2 deleted user-1's task")
	}
	own, err := h.administerTrustedTask("user-1", "", "", "", "", "")
	if err != nil || !strings.Contains(own, "write docs") {
		t.Fatalf("user-1's task did not survive: %q err=%v", own, err)
	}
}

func TestSemanticTaskFieldPresenceStillFailsClosed(t *testing.T) {
	h := &IMMessageHandler{taskStore: task.NewStore()}
	if _, err := h.administerTrustedTask("user-1", "", "", "", "completed", ""); err == nil {
		t.Fatal("status without id must fail closed")
	}
	if _, err := h.administerTrustedTask("user-1", "x", "", "t1", "", ""); err == nil {
		t.Fatal("title plus id must fail closed")
	}
}
