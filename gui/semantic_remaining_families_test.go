package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestIMSemanticOfficeWriteUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice)}
	h.semanticTrustedOfficeWrite = func(userID, path string, data map[string]interface{}) (string, error) {
		if userID != "user-1" || path != "sheet.xlsx" {
			t.Fatalf("user=%q path=%q", userID, path)
		}
		return "Wrote spreadsheet sheet.xlsx", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "写一个表格", "lansenger", "root-office", "turn-office", &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	officeSelectionFound := false
	for _, selection := range surface.plan.Selections {
		if selection.AdapterName == semanticTrustedOfficeWriteAdapter {
			officeSelectionFound = true
		}
		if selection.AdapterName == "office" {
			t.Fatal("adapter leaked soup name")
		}
	}
	if !officeSelectionFound {
		t.Fatalf("office selection missing: %#v", surface.plan.Selections)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedOfficeWriteAdapter)
	if name != "office" {
		t.Fatalf("managed office name=%q, want office", name)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"path":"sheet.xlsx","sheets":[{"name":"S1","rows":[["a"]]}],"action":"write_excel"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") && !strings.Contains(got, "parameter_schema_invalid") {
		t.Fatalf("action soup=%q", got)
	}
	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "写一个表格", "lansenger", "root-office-exec", "turn-office-exec", &intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("exec surface handled=%v err=%v", handled, err)
	}
	name = semanticGrantNameForAdapter(surface, semanticTrustedOfficeWriteAdapter)
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"path":"sheet.xlsx","sheets":[{"name":"S1","rows":[["a"]]}]}`); !strings.Contains(got, "Wrote spreadsheet") {
		t.Fatalf("write=%q", got)
	}
}

// semanticGrantNameForAdapter finds the model-visible grant name bound to a
// trusted host adapter on a materialized surface.
func semanticGrantNameForAdapter(surface *semanticCallSurface, adapter string) string {
	if surface == nil {
		return ""
	}
	for name, grant := range surface.grants {
		if grant.AdapterName == adapter {
			return name
		}
	}
	return ""
}

func TestSemanticTreeConfirmedIsRouteAuthorityNotFamilyAllowlist(t *testing.T) {
	tree := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 3}
	if !semanticTreeConfirmedClassification(tree) || !semanticClassificationPlansBelowResolverFloor(tree) {
		t.Fatal("L3 !Degraded 0.75 is the route authority and must plan")
	}
	l2 := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 2}
	if semanticTreeConfirmedClassification(l2) || semanticClassificationPlansBelowResolverFloor(l2) {
		t.Fatal("L2 0.75 is an embedding guess and must not mint writes")
	}
	degraded := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 3, Degraded: true}
	if semanticTreeConfirmedClassification(degraded) || semanticClassificationPlansBelowResolverFloor(degraded) {
		t.Fatal("Degraded tree must not count as confirmed")
	}
	office := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.75, Layer: 3}
	if !semanticClassificationPlansBelowResolverFloor(office) {
		t.Fatal("tree confirmation is not a privilege-family allowlist")
	}
	weakTree := intent.ClassificationResult{Primary: intent.LabelDocumentRead, Confidence: 0.55, Layer: 3}
	if semanticTreeConfirmedClassification(weakTree) || semanticClassificationPlansBelowResolverFloor(weakTree) {
		t.Fatal("tree 0.55 is below the 0.70 signal floor; Degraded is not required")
	}
	codingGuess := intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.72, Layer: 2}
	if semanticClassificationPlansBelowResolverFloor(codingGuess) {
		t.Fatal("L2 coding 0.72 is an embedding guess, not a coding-family hole")
	}
	fusion := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 23}
	if !semanticTreeConfirmedClassification(fusion) {
		t.Fatal("fusion layer 23 is the same route authority as L3")
	}
	if !semanticReadOnlyUnderstandHint(intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: 0.75, Layer: 3}) {
		t.Fatal("tree-confirmed file_read must be an understand hint, not a second 0.78 vote")
	}
}

func TestSemanticDegradedOfficeHintPlansBelowResolverFloor(t *testing.T) {
	// A tree-timeout L2 office guess at the lookup floor plans through the
	// governed office surface instead of collapsing to unknown leftover.
	hint := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.75, Layer: 2, Degraded: true}
	if !semanticOfficeGovernedHint(hint) || !semanticClassificationPlansBelowResolverFloor(hint) {
		t.Fatal("degraded office hint at 0.75 must plan below the resolver floor")
	}
	// Sub-floor hints and non-office degraded mutating families stay a miss.
	weak := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.65, Layer: 2, Degraded: true}
	if semanticOfficeGovernedHint(weak) || semanticClassificationPlansBelowResolverFloor(weak) {
		t.Fatal("sub-floor office hint must not mint needs")
	}
	if semanticOfficeGovernedHint(intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.9, Layer: 3}) {
		t.Fatal("non-degraded office is the resolver floor's job, not the hint gate")
	}
	degradedShell := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 2, Degraded: true}
	if semanticClassificationPlansBelowResolverFloor(degradedShell) {
		t.Fatal("degraded shell guess must stay a miss")
	}
}

func TestSemanticDegradedOfficeHintRendersGovernedOfficeSurface(t *testing.T) {
	// End-to-end: a tree-timeout L2 office hint still materializes the managed
	// office surface, so a slow hub no longer strips document tools off
	// 「生成PPT/报告」 turns.
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice)}
	if err := h.registry.Register(RegisteredTool{
		Name: "office", Description: "office doc tool", Status: RegToolAvailable,
		InputSchema:          map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string"}}},
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: tool.CapabilityDocumentWriteOffice, Qualifiers: map[string]string{"format": "spreadsheet"}, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectSensitive},
		Handler:              func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "生成庆祝布偶宝宝5岁生日的ppt", "desktop", "root-office-hint", "turn-office-hint",
		&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.75, Layer: 2, Degraded: true},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("degraded office hint must plan, handled=%v err=%v defs=%#v", handled, err, defs)
	}
	found := false
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityDocumentWriteOffice {
			found = true
		}
	}
	if !found {
		t.Fatalf("office capability not planned: %#v", surface.plan.Selections)
	}
}

func TestSemanticSubFloorShellCommandPlansInsteadOfLeftover(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelShellCommand)}
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "cleared", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "清空当前目录", "desktop", "root-subfloor-shell", "turn-subfloor-shell",
		&intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 3},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("sub-floor shell must plan, handled=%v err=%v defs=%#v", handled, err, defs)
	}
	if name := semanticGrantNameForAdapter(surface, semanticTrustedShellAdapter); name != "bash" {
		t.Fatalf("shell grant=%q, want bash; leftover would have stripped it", name)
	}
	if _, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityShellExecuteLocal); !ok {
		t.Fatalf("shell capability missing: %#v", surface.plan.Selections)
	}
}

func TestSemanticTreeConfirmedShellLoopCloseKeepsBash(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelShellCommand)}
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	result := &intent.ClassificationResult{
		Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 3,
		Reason: "tree-after-embedding: shell_command (0.750)",
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "清空当前目录"}, false, false, result)
	if profile.PromptIsLight() {
		t.Fatalf("tree shell closed as light would drop bash into leftover: %+v", profile)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "清空当前目录", "desktop", "root-tree-shell-close", "turn-tree-shell-close", result,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("tree shell must plan, handled=%v err=%v", handled, err)
	}
	tools := closedManagedSemanticDefinitionsForTurn(defs, surface, profile.PromptIsLight())
	kept := false
	for _, def := range tools {
		if extractToolName(def) == "bash" {
			kept = true
		}
	}
	if len(tools) == 0 || !kept {
		t.Fatalf("loop-start close must keep bash, tools=%#v", tools)
	}
}

func TestSemanticEmbeddingGuessShellBelowResolverFloorFallsThrough(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelShellCommand)}
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "清空当前目录", "desktop", "root-l2-shell", "turn-l2-shell",
		&intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 2},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("L2 embedding guess below 0.78 must not mint bash: handled=%v err=%v surface=%#v", handled, err, surface)
	}
}

func TestSemanticTreeConfirmedOfficePlansBelowResolverFloor(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice)}
	h.semanticTrustedOfficeWrite = func(string, string, map[string]interface{}) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "写一个表格", "desktop", "root-tree-office", "turn-tree-office",
		&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.75, Layer: 3},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("tree-confirmed office must plan, handled=%v err=%v defs=%#v", handled, err, defs)
	}
	if name := semanticGrantNameForAdapter(surface, semanticTrustedOfficeWriteAdapter); name != "office" {
		t.Fatalf("managed office name=%q, want office", name)
	}
}

func TestSemanticTreeConfirmedFileReadPlansInsteadOfChatProjection(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	result := intent.ClassificationResult{Primary: intent.LabelFileRead, Confidence: 0.75, Layer: 3}
	if semanticNeedsChatProjection(result) {
		t.Fatal("tree-confirmed file_read 0.75 must not chat-project under the L2 0.78 understand floor")
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看看 notes.txt", "desktop", "root-tree-fread", "turn-tree-fread", &result,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("tree-confirmed file_read must plan, handled=%v err=%v defs=%#v", handled, err, defs)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityFSReadLocal) {
		t.Fatalf("selections=%#v, want fs.read.local", surface.plan.Selections)
	}
}

func TestSemanticWeakTreeDocumentReadBelowHintFloorFallsThrough(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	result := intent.ClassificationResult{
		Primary: intent.LabelDocumentRead, Confidence: 0.55, Layer: 3,
		Reason: "tree-after-embedding: document_read (0.550)",
	}
	if !semanticNeedsChatProjection(result) {
		t.Fatal("tree document_read 0.55 must still chat; production trees are not Degraded")
	}
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "图上有什么？", "desktop", "root-weak-tree-doc", "turn-weak-tree-doc", &result,
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("tree 0.55 document_read must fall through: handled=%v err=%v surface=%#v", handled, err, surface)
	}
}

func TestSemanticDegradedTreeShellFallsThroughToLeftover(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelShellCommand)}
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "清空当前目录", "desktop", "root-degraded-tree-shell", "turn-degraded-tree-shell",
		&intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.75, Layer: 3, Degraded: true},
	)
	if err != nil || handled || surface != nil {
		t.Fatalf("Degraded L3 shell must not mint bash: handled=%v err=%v surface=%#v", handled, err, surface)
	}
}

func TestSemanticSubFloorFileWritePlansInsteadOfLeftover(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelFileWrite)}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "把这段保存到 notes.txt", "desktop", "root-subfloor-write", "turn-subfloor-write",
		&intent.ClassificationResult{Primary: intent.LabelFileWrite, Confidence: 0.75, Layer: 3},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("sub-floor file_write must plan, handled=%v err=%v defs=%#v", handled, err, defs)
	}
	found := false
	for _, def := range defs {
		if extractToolName(def) == "write_file" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sub-floor file_write must issue write_file, defs=%#v", defs)
	}
}

func TestIMSemanticShellUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelShellCommand)}
	h.semanticTrustedShell = func(userID, command string, timeout time.Duration) (string, error) {
		if userID != "user-1" || command != "echo hi" {
			t.Fatalf("user=%q command=%q", userID, command)
		}
		return "hi", nil
	}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "运行 echo hi", "lansenger", "root-shell", "turn-shell", &intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, tool.CapabilityShellExecuteLocal)
	if !ok || selection.AdapterName != semanticTrustedShellAdapter {
		t.Fatalf("selection=%+v found=%v", selection, ok)
	}
	name := semanticGrantNameForAdapter(surface, semanticTrustedShellAdapter)
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"command":"echo hi","project_path":"/tmp"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("project_path soup=%q", got)
	}
	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "运行 echo hi", "lansenger", "root-shell-exec", "turn-shell-exec", &intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("exec surface handled=%v err=%v", handled, err)
	}
	name = semanticGrantNameForAdapter(surface, semanticTrustedShellAdapter)
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"command":"echo hi"}`); got != "hi" {
		t.Fatalf("shell=%q", got)
	}
}

func TestIMSemanticDelegateRequiresChildReceipt(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelDelegateTask)}
	registerBuiltinTools(h.registry, h)
	_, _, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "交给子代理", "lansenger", "root-del-unmet", "turn-del-unmet", &intent.ClassificationResult{Primary: intent.LabelDelegateTask, Confidence: .98},
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("delegate without runner must be unmet handled=%v err=%v", handled, err)
	}
	h.semanticTrustedDelegate = func(userID, task string) (string, error) {
		return "child completed: " + task, nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "交给子代理", "lansenger", "root-del", "turn-del", &intent.ClassificationResult{Primary: intent.LabelDelegateTask, Confidence: .98},
	)
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"task":"summarize","delegate_to":"coder"}`); !strings.Contains(got, "parameter_unknown_field") && !strings.Contains(got, "parameter_reserved_field") {
		t.Fatalf("delegate_to soup=%q", got)
	}
	defs, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "交给子代理", "lansenger", "root-del-exec", "turn-del-exec", &intent.ClassificationResult{Primary: intent.LabelDelegateTask, Confidence: .98},
	)
	if err != nil || !handled {
		t.Fatalf("exec surface handled=%v err=%v", handled, err)
	}
	name = extractToolName(defs[0])
	cb = &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{"task":"summarize"}`); !strings.Contains(got, "child completed") {
		t.Fatalf("delegate=%q", got)
	}
}

func TestIMSemanticSSHBrowserCURequireBoundRuntime(t *testing.T) {
	for _, tc := range []struct {
		label intent.IntentLabel
		hook  func(*IMMessageHandler)
	}{
		{intent.LabelSSH, func(h *IMMessageHandler) {
			h.semanticTrustedSSH = func(userID, command string) (string, error) { return "remote:" + command, nil }
		}},
		{intent.LabelBrowser, func(h *IMMessageHandler) {
			h.semanticTrustedBrowser = func(userID, action, url string) (string, error) { return "navigated " + url, nil }
		}},
		{intent.LabelComputerUse, func(h *IMMessageHandler) {
			h.semanticTrustedComputerUse = func(userID, action string) (string, error) { return "observed", nil }
		}},
	} {
		h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, tc.label)}
		registerBuiltinTools(h.registry, h)
		_, _, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
			"user-1", "do it", "lansenger", "root-unmet-"+string(tc.label), "turn-unmet", &intent.ClassificationResult{Primary: tc.label, Confidence: .98},
		)
		if !handled || err == nil || !strings.Contains(err.Error(), "unmet") {
			t.Fatalf("%s without runtime must be unmet handled=%v err=%v", tc.label, handled, err)
		}
		tc.hook(h)
		defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
			"user-1", "do it", "lansenger", "root-"+string(tc.label), "turn-ready", &intent.ClassificationResult{Primary: tc.label, Confidence: .98},
		)
		if err != nil || !handled || surface == nil || len(defs) < 1 {
			t.Fatalf("%s ready handled=%v err=%v", tc.label, handled, err)
		}
		name := extractToolName(defs[0])
		if surface.plan.Selections[0].AdapterName == name {
			t.Fatalf("%s adapter leaked soup %q", tc.label, name)
		}
	}
}

func TestIMSemanticDelegatePublishesWhenHostAvailable(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelDelegateTask), app: &App{}}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "交给子代理", "lansenger", "root-del-host", "turn-del-host", &intent.ClassificationResult{Primary: intent.LabelDelegateTask, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("host delegate must publish handled=%v err=%v defs=%#v", handled, err, defs)
	}
	if surface.plan.Selections[0].AdapterName != semanticTrustedDelegateAdapter {
		t.Fatalf("selection=%+v", surface.plan.Selections[0])
	}
	if name := extractToolName(defs[0]); name != "delegate_task" {
		t.Fatalf("managed delegate name=%q, want delegate_task", name)
	}
}

func TestIMSemanticComputerUsePublishesWhenDesktopHostEnabled(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelComputerUse), app: &App{}}
	if !semanticTrustedComputerUsePublished(h) {
		t.Fatal("desktop host with default CU enabled must publish")
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "看一下桌面", "lansenger", "root-cu-host", "turn-cu-host", &intent.ClassificationResult{Primary: intent.LabelComputerUse, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("host CU must publish handled=%v err=%v defs=%#v", handled, err, defs)
	}
	if name := extractToolName(defs[0]); name != "computer_use" {
		t.Fatalf("managed computer use name=%q, want computer_use", name)
	}
}

func TestTrustedRuntimeDefaultsStayUnpublishedWithoutHost(t *testing.T) {
	h := &IMMessageHandler{}
	if semanticTrustedSSHPublished(h) || semanticTrustedBrowserPublished(h) || semanticTrustedComputerUsePublished(h) || semanticTrustedDelegatePublished(h) {
		t.Fatal("bare handler must not publish ssh/browser/CU/delegate")
	}
	if _, err := h.executeTrustedSSH("user-1", "uname"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("ssh without session: %v", err)
	}
	if _, err := h.controlTrustedBrowser("user-1", "snapshot", ""); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("browser without host: %v", err)
	}
	if _, err := h.controlTrustedDesktop("user-1", "observe"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("CU without host: %v", err)
	}
	if _, err := h.runTrustedDelegate("user-1", "summarize"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("delegate without host: %v", err)
	}
}

func TestIMSemanticDocumentDeliveryIsSpecifiedTarget(t *testing.T) {
	managed, unmapped := imSemanticIntentCoverage(intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: .98})
	if !managed || unmapped != "" {
		t.Fatalf("document_delivery coverage managed=%v unmapped=%q", managed, unmapped)
	}
	if semanticFileDeliveryPublished("weixin") != true || semanticFileDeliveryPublished("ve_group_executor") {
		t.Fatal("weixin publishes file deliver; VE does not")
	}
}

func TestPhaseCUnmappedCapabilityLabelDoesNotUseLegacyRouter(t *testing.T) {
	fixture := semanticUnmigratedFixtureLabel(t)
	h := &IMMessageHandler{registry: NewToolRegistry()}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "写个函数", "lansenger", "root-unmapped", "turn-unmapped", &intent.ClassificationResult{
		Primary: fixture, Confidence: .98,
	})
	if !handled || prepared != nil || err == nil || !strings.Contains(err.Error(), "unmapped capability label") {
		t.Fatalf("%s must HostReject, prepared=%#v handled=%v err=%v", fixture, prepared, handled, err)
	}
}

func TestPhaseCSemanticTurnDoesNotRebuildToolsAfterLightDeny(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelShellCommand)}
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "run echo", "lansenger", "root-light", "turn-light", &intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, tools: defs, loopCtx: &LoopContext{}}
	if cb.UpgradeLightPromptToFull("light tool deny") {
		t.Fatal("governed semantic turn must not upgrade light and rebuild tools")
	}
	if len(cb.BuildTools("")) != len(defs) {
		t.Fatalf("BuildTools mutated the closed surface: %d vs %d", len(cb.BuildTools("")), len(defs))
	}
}

func TestPhaseCSemanticSurfaceNeverExposesLegacySoupNames(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice)}
	h.semanticTrustedOfficeWrite = func(string, string, map[string]interface{}) (string, error) { return "ok", nil }
	h.semanticTrustedShell = func(string, string, time.Duration) (string, error) { return "ok", nil }
	registerBuiltinTools(h.registry, h)
	for _, tc := range []struct {
		label intent.IntentLabel
		text  string
	}{
		{intent.LabelOffice, "写一个表格"},
		{intent.LabelShellCommand, "运行 echo hi"},
	} {
		defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
			"user-1", tc.text, "lansenger", "root-soup-"+string(tc.label), "turn-soup", &intent.ClassificationResult{Primary: tc.label, Confidence: .98},
		)
		if err != nil || !handled || surface == nil {
			t.Fatalf("%s handled=%v err=%v", tc.label, handled, err)
		}
		want := map[intent.IntentLabel]string{
			intent.LabelOffice:       "office",
			intent.LabelShellCommand: "bash",
		}[tc.label]
		found := false
		for _, def := range defs {
			name := extractToolName(def)
			grant := surface.grants[name]
			if grant.AdapterName == "office" || grant.AdapterName == "bash" || grant.AdapterName == "write_excel" {
				t.Fatalf("%s adapter leaked soup name %q", tc.label, grant.AdapterName)
			}
			if name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s missing stable name %q in %#v", tc.label, want, defs)
		}
	}
}

func TestPhaseCGenericQAMaySkipSemanticSurface(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	prepared, handled, err := h.semanticPlanForTurnWithClassification("user", "你好", "lansenger", "root-qa", "turn-qa", &intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: .98,
	})
	if handled || prepared != nil || err != nil {
		t.Fatalf("generic Q&A must skip semantic surface prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
}
