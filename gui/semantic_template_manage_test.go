package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func templateManageClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelTemplateManage,
		Confidence: .98,
		ToolNames:  []string{"manage_template", "create_template", "launch_template"},
	}
}

func TestIMSemanticTemplateUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelTemplateManage)}
	h.semanticTrustedTemplate = func(userID, name, codingTool string) (string, error) {
		t.Fatalf("planning must not execute the manager user=%q name=%q", userID, name)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "会话模板", "lansenger", "root-template", "turn-template", templateManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedTemplateAdapter || selection.FitProof.MatchedCapability != tool.CapabilityTemplateManageSession {
		t.Fatalf("selection=%+v", selection)
	}
	if !semanticSelectionRequiresReceipt(selection) || !semanticBuiltinLocalMutationSelection(selection) {
		t.Fatalf("template must use the local mutation receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "manage_template", "create_template", "list_templates", "launch_template")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["coding_tool"]; !ok || len(properties) != 2 {
		t.Fatalf("template schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"action", "tool", "tool_name", "launch", "yolo_mode", "model_config",
		"env_vars", "project_path", "template_name", "channel", "destination",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing template schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedTemplateAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"action":"launch","yolo_mode":true}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("forged launch fields=%q", got)
	}
}

func TestIMSemanticTemplateExecutesFieldPresenceWithoutLaunch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelTemplateManage)}
	var seenName, seenTool string
	h.semanticTrustedTemplate = func(userID, name, codingTool string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seenName, seenTool = name, codingTool
		return "模板已创建: daily（工具=claude）", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "会话模板", "lansenger", "root-template-exec", "turn-template-exec", templateManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"name":"daily","coding_tool":"claude"}`)
	if !strings.Contains(got, "模板已创建") || strings.Contains(got, "launch") {
		t.Fatalf("bound template=%q", got)
	}
	if seenName != "daily" || seenTool != "claude" {
		t.Fatalf("dispatch name=%q tool=%q", seenName, seenTool)
	}
	if replay := cb.ExecuteTool(name, `{"name":"daily","coding_tool":"claude"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticTemplateRejectsFieldPresenceAndDeliveryTokens(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelTemplateManage)}
	h.semanticTrustedTemplate = func(string, string, string) (string, error) {
		return "模板已创建: x（工具=y） [file_base64:abc]", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "会话模板", "lansenger", "root-template-both", "turn-template-both", templateManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"coding_tool":"claude"}`); !strings.Contains(got, "trusted_template_field_presence_rejected") {
		t.Fatalf("coding_tool only=%q", got)
	}

	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "会话模板", "lansenger", "root-template-token", "turn-template-token", templateManageClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("second defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_template_delivery_token") {
		t.Fatalf("delivery token=%q", got)
	}
	if _, err := h.administerTrustedTemplate("", "", ""); err == nil || !strings.Contains(err.Error(), "trusted_template_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}

func TestIMSemanticTemplatePersistsNameAndToolOnly(t *testing.T) {
	mgr, err := remote.NewSessionTemplateManager(filepath.Join(t.TempDir(), "templates.json"))
	if err != nil {
		t.Fatalf("template manager: %v", err)
	}
	h := &IMMessageHandler{templateManager: mgr}
	created, err := h.administerTrustedTemplate("user-1", "daily", "claude")
	if err != nil || !strings.Contains(created, "模板已创建") || strings.Contains(created, "项目=") {
		t.Fatalf("create=%q err=%v", created, err)
	}
	listed, err := h.administerTrustedTemplate("user-2", "", "")
	if err != nil || !strings.Contains(listed, "daily") {
		t.Fatalf("process-wide residual list=%q err=%v", listed, err)
	}
	got, err := h.administerTrustedTemplate("user-1", "daily", "")
	if err != nil || !strings.Contains(got, "工具=claude") || strings.Contains(got, "yolo") {
		t.Fatalf("get=%q err=%v", got, err)
	}
	if _, err := h.administerTrustedTemplate("user-1", "", "claude"); err == nil {
		t.Fatal("coding_tool only must fail closed")
	}
	if _, err := h.administerTrustedTemplate("user-1", "", ""); err != nil {
		t.Fatalf("empty list err=%v", err)
	}
}
