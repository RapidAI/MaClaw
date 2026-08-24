package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func sessionManageClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelSessionManage,
		Confidence: .98,
		ToolNames:  []string{"list_sessions", "send_input", "interrupt_session", "kill_session"},
	}
}

func TestIMSemanticSessionUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSessionManage)}
	h.semanticTrustedSession = func(userID, id string) (string, error) {
		t.Fatalf("planning must not execute the inspector user=%q id=%q", userID, id)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前编码会话", "lansenger", "root-session", "turn-session", sessionManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedSessionAdapter || selection.FitProof.MatchedCapability != tool.CapabilitySessionManageCoding {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("session inspect must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "list_sessions",
		"send_input", "interrupt_session", "kill_session", "get_session_output", "get_session_events", "project_manage", "list_providers")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["id"]; !ok || len(properties) != 1 {
		t.Fatalf("session schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "input", "interrupt", "kill", "send", "launch", "provider",
		"project", "project_path", "yolo_mode", "session_id", "text", "channel", "destination",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing session schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedSessionAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"action":"kill","session_id":"s1"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged drive fields=%q", got)
	}
}

func TestIMSemanticSessionExecutesFieldPresenceWithoutDrive(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSessionManage)}
	var seenID string
	h.semanticTrustedSession = func(userID, id string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenID = id
		if id == "" {
			return "当前没有编码会话。", nil
		}
		return "会话 [s1 [running] 工具=claude 标题=fix]", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前编码会话", "lansenger", "root-session-exec", "turn-session-exec", sessionManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{}`)
	if !strings.Contains(got, "当前没有编码会话") || strings.Contains(got, "send_input") {
		t.Fatalf("bound session list=%q", got)
	}
	if seenID != "" {
		t.Fatalf("list dispatched id=%q", seenID)
	}
	if replay := cb.ExecuteTool(name, `{}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticSessionRejectsDriveFieldsAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelSessionManage)}
	h.semanticTrustedSession = func(string, string) (string, error) {
		return "会话 [s1 [running] 工具=claude 标题=x] [file_base64:abc]", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前编码会话", "lansenger", "root-session-drive", "turn-session-drive", sessionManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"id":"s1","interrupt":true}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "trusted_session_arguments_rejected") {
		t.Fatalf("interrupt=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看当前编码会话", "lansenger", "root-session-token", "turn-session-token", sessionManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_session_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.inspectTrustedSessions("", ""); err == nil || !strings.Contains(err.Error(), "trusted_session_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticSessionInspectsWithoutLaunching(t *testing.T) {
	h := &IMMessageHandler{}
	empty, err := h.inspectTrustedSessions("user-1", "")
	if err != nil || empty != "当前没有编码会话。" {
		t.Fatalf("nil manager list=%q err=%v", empty, err)
	}
	if _, err := h.inspectTrustedSessions("user-1", "missing"); err == nil || !strings.Contains(err.Error(), "trusted_session_not_found") {
		t.Fatalf("nil manager get err=%v", err)
	}

	h.manager = &RemoteSessionManager{sessions: map[string]*RemoteSession{
		"s1": {ID: "s1", Tool: "claude", Title: "fix bug", Status: SessionRunning, ProjectPath: "D:/secret/project"},
	}}
	listed, err := h.inspectTrustedSessions("user-2", "")
	if err != nil || !strings.Contains(listed, "s1") || !strings.Contains(listed, "fix bug") {
		t.Fatalf("process-wide residual list=%q err=%v", listed, err)
	}
	if strings.Contains(listed, "D:/secret/project") || strings.Contains(listed, "send_input") {
		t.Fatalf("list leaked path or drive: %q", listed)
	}
	got, err := h.inspectTrustedSessions("user-1", "s1")
	if err != nil || !strings.Contains(got, "running") || strings.Contains(got, "D:/secret") {
		t.Fatalf("get=%q err=%v", got, err)
	}
	if _, err := h.inspectTrustedSessions("user-1", "nope"); err == nil {
		t.Fatal("unknown id must fail closed")
	}
}
