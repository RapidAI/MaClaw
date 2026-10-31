package guiapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func closeSemanticSurface(t *testing.T, surface *semanticCallSurface) {
	t.Helper()
	t.Cleanup(func() {
		if surface != nil && surface.coordinator != nil {
			_ = surface.coordinator.Close()
		}
	})
}

func TestTrustedMailboxSendAutoConfirmsWhenUnattended(t *testing.T) {
	sender := &statusMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	publishUnattendedLoop(h, userID, LoopKindBackground, "scheduler")
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID}
	got := cb.executeTrustedMailboxSend(context.Background(), tool.CanonicalRequest{Values: map[string]interface{}{
		"action": "send", "to": []any{"a@ex.com"}, "subject": "状态", "text": "在线",
	}})
	if !got.Succeeded || got.AwaitingReceipt || got.Result != "邮件已发送" || got.ReasonCode != "" || sender.calls != 1 {
		t.Fatalf("got=%+v calls=%d", got, sender.calls)
	}
}

func TestMailboxSendPlanMaterializesOnlyTheTrustedSendAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-mail-send", "向 znsoft@163.com 发邮件报告系统状态", "desktop", "root-mail-send", "turn-mail-send",
		&intent.ClassificationResult{Primary: intent.LabelMailboxSend, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	closeSemanticSurface(t, surface)
	if got := semanticGrantNameForAdapter(surface, semanticTrustedMailboxSendAdapter); got != "mailbox_send" {
		t.Fatalf("send plan model name=%q, want mailbox_send", got)
	}
	sendDef := semanticDefForGrantName(defs, "mailbox_send")
	if sendDef == nil {
		t.Fatal("model list omitted mailbox_send")
	}
	sendFn, _ := sendDef["function"].(map[string]interface{})
	if sendFn == nil {
		t.Fatal("mailbox_send definition has no function")
	}
	sendDesc, _ := sendFn["description"].(string)
	if !strings.Contains(sendDesc, "freezes a draft") || !strings.Contains(sendDesc, "stays listed") || strings.Contains(sendDesc, "reappear") {
		t.Fatalf("send description=%q", sendDesc)
	}
	for _, def := range defs {
		if name := extractToolName(def); strings.HasPrefix(name, "invoke_") {
			t.Fatalf("model list included rotating name %q", name)
		}
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedMailboxConfigureAdapter) != "" {
		t.Fatal("send plan materialized mailbox configure")
	}
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityMessageConfigureMailbox {
			t.Fatalf("send plan included configure selection %s", selection.ID)
		}
		if selection.AdapterName == "mailbox_send" {
			t.Fatal("managed send plan still selected the legacy mailbox_send adapter")
		}
	}
	if !causesReceipt(surface) {
		t.Fatal("trusted mailbox send is not recognised as a receipt boundary")
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	if got := semanticToolsSearchStatus(cb, "mailbox_send"); got != "[已在当前工具面]" {
		t.Fatalf("discovery status=%s", got)
	}
	for _, entry := range semanticToolsSearchCatalog(cb) {
		if strings.HasPrefix(entry.name, "invoke_") && entry.capability == tool.CapabilityMessageSendMailbox {
			t.Fatalf("discovery advertised rotating mailbox name %q", entry.name)
		}
	}
}

func mailboxAdapterSelections(surface *semanticCallSurface, adapter string) int {
	if surface == nil {
		return 0
	}
	count := 0
	for _, selection := range surface.plan.Selections {
		if selection.AdapterName == adapter {
			count++
		}
	}
	return count
}

func causesReceipt(surface *semanticCallSurface) bool {
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability != tool.CapabilityMessageSendMailbox {
			continue
		}
		if semanticReceiptBoundaryMissing(selection) || !semanticMailboxSendReceiptSelection(selection) {
			return false
		}
	}
	return true
}

func TestMailboxReadPlanRendersTheStableQueryName(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-mail-read", "看看收件箱", "desktop", "root-mail-read", "turn-mail-read",
		&intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	closeSemanticSurface(t, surface)
	if got := semanticGrantNameForAdapter(surface, "mailbox_query"); got != "mailbox_query" {
		t.Fatalf("read plan model name=%q, want mailbox_query", got)
	}
	if semanticDefForGrantName(defs, "mailbox_query") == nil {
		t.Fatal("model list omitted mailbox_query")
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	if got := semanticToolsSearchStatus(cb, "mailbox_query"); got != "[已在当前工具面]" {
		t.Fatalf("discovery status=%s", got)
	}
}

func TestMailboxConfigurePlanDoesNotMaterializeSend(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-mail-cfg", "帮我配置邮箱", "desktop", "root-mail-cfg", "turn-mail-cfg",
		&intent.ClassificationResult{Primary: intent.LabelMailboxConfigure, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	closeSemanticSurface(t, surface)
	if got := semanticGrantNameForAdapter(surface, semanticTrustedMailboxConfigureAdapter); got != "mailbox_configure" {
		t.Fatalf("configure plan model name=%q, want mailbox_configure", got)
	}
	if semanticDefForGrantName(defs, "mailbox_configure") == nil {
		t.Fatal("model list omitted mailbox_configure")
	}
	if siblings := mailboxAdapterSelections(surface, semanticTrustedMailboxConfigureAdapter); siblings < 2 {
		t.Fatalf("configure budget did not expand, selections=%d", siblings)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedMailboxSendAdapter) != "" {
		t.Fatal("configure plan materialized mailbox send")
	}
}

func TestMailboxDeletePlanMaterializesOnlyTheTrustedDeleteAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-mail-delete", "删除邮箱中的所有邮件", "desktop", "root-mail-delete", "turn-mail-delete",
		&intent.ClassificationResult{Primary: intent.LabelMailboxDelete, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	closeSemanticSurface(t, surface)
	if got := semanticGrantNameForAdapter(surface, semanticTrustedMailboxDeleteAdapter); got != "mailbox_delete" {
		t.Fatalf("delete plan model name=%q, want mailbox_delete", got)
	}
	deleteDef := semanticDefForGrantName(defs, "mailbox_delete")
	if deleteDef == nil {
		t.Fatal("model list omitted mailbox_delete")
	}
	deleteFn, _ := deleteDef["function"].(map[string]interface{})
	if deleteFn == nil {
		t.Fatal("mailbox_delete definition has no function")
	}
	deleteDesc, _ := deleteFn["description"].(string)
	if !strings.Contains(deleteDesc, "freezes a draft") || !strings.Contains(deleteDesc, "stays listed") || strings.Contains(deleteDesc, "reappear") {
		t.Fatalf("delete description=%q", deleteDesc)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedMailboxSendAdapter) != "" {
		t.Fatal("delete plan materialized mailbox send")
	}
	for _, selection := range surface.plan.Selections {
		if selection.AdapterName == "mailbox_delete" {
			t.Fatal("managed delete plan still selected the legacy mailbox_delete adapter")
		}
		if selection.FitProof.MatchedCapability == tool.CapabilityMessageDeleteMailbox {
			if semanticReceiptBoundaryMissing(selection) || !semanticMailboxDeleteReceiptSelection(selection) {
				t.Fatalf("delete selection is not a receipt boundary: %+v", selection.AdapterName)
			}
		}
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	if got := semanticToolsSearchStatus(cb, "mailbox_delete"); got != "[已在当前工具面]" {
		t.Fatalf("discovery status=%s", got)
	}
}

func TestTrustedMailboxSendFreezesADraftAndDoesNotDial(t *testing.T) {
	sender := &statusMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		userID, "向 a@ex.com 发一封邮件", "desktop", "root-mail-exec", "turn-mail-exec",
		&intent.ClassificationResult{Primary: intent.LabelMailboxSend, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	t.Cleanup(func() {
		if surface.coordinator != nil {
			_ = surface.coordinator.Close()
		}
	})
	name := semanticGrantNameForAdapter(surface, semanticTrustedMailboxSendAdapter)
	if name == "" {
		t.Fatal("missing send grant")
	}
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID, semanticSurface: surface, tools: defs}
	if !cb.RequiredEffectUnsettled() {
		t.Fatal("send grant with no execution record was treated as settled")
	}
	got := cb.ExecuteToolCall(name, `{"action":"send","to":["a@ex.com"],"subject":"状态","text":"在线"}`, "call-mail-send").Result
	if !strings.HasPrefix(got, "__ASK_USER__") {
		t.Fatalf("send result=%q", got)
	}
	if strings.Contains(got, "external_effect_receipt_boundary_missing") {
		t.Fatalf("receipt gate rejected the trusted send: %s", got)
	}
	if sender.calls != 0 {
		t.Fatalf("SMTP dialed before confirmation: %d", sender.calls)
	}
	if _, live := surface.grants[name]; live {
		t.Fatal("confirmation left the send grant on the model list")
	}
	if !cb.RequiredEffectUnsettled() {
		t.Fatal("confirmation was treated as a settled send")
	}
	retired, ok := surface.retiredGrants[name]
	if !ok {
		t.Fatalf("confirmation did not retire %s", name)
	}
	record, err := surface.executor.Execution(surface.scope, retired.SelectionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != tool.PlanExecutionAwaitingReceipt || record.ReasonCode != mailboxSendAwaitingConfirmation {
		t.Fatalf("confirmation record=%s %s", record.State, record.ReasonCode)
	}
	denial := cb.ToolDenialMessage(name)
	if strings.Contains(denial, "did not complete successfully") || !strings.Contains(denial, "waiting for the user to confirm") {
		t.Fatalf("confirmation denial=%q", denial)
	}
	if got := semanticToolsSearchStatus(cb, name); got != "[等待用户确认：不要再次调用]" {
		t.Fatalf("confirmation discovery=%s", got)
	}
	directory := semanticToolsSearchRun(cb, `{"query":"email"}`)
	confirmLine := "等待用户确认：" + name + "。不要再次调用，也不要用其它工具完成该动作。"
	if !strings.HasSuffix(strings.TrimSpace(directory), confirmLine) {
		t.Fatalf("confirmation directory tail=%q", directory)
	}
	filtered := semanticToolsSearchRun(cb, `{"query":"run a command","needs":["bash"]}`)
	if strings.Contains(filtered, "- "+name+" —") || !strings.HasSuffix(strings.TrimSpace(filtered), confirmLine) {
		t.Fatalf("bash filter dropped the waiting send:\n%s", filtered)
	}
	replay := cb.ExecuteToolCall(name, `{"action":"send","to":["a@ex.com"],"subject":"状态","text":"在线"}`, "call-mail-send").Result
	if !strings.HasPrefix(replay, "__ASK_USER__") || sender.calls != 0 {
		t.Fatalf("same-call replay=%q smtp=%d", replay, sender.calls)
	}
	if cb.IsToolAllowed(name) {
		t.Fatal("confirmation left mailbox_send callable")
	}
	granted, message := cb.PetitionToolCall(name)
	if granted || !strings.Contains(message, "waiting for the user to confirm") || strings.Contains(message, "rendered tool surface") {
		t.Fatalf("unrendered retry granted=%v message=%q", granted, message)
	}
	for id := range surface.completed {
		if strings.Contains(id, "mailbox") || surface.completed[id] {
			selection, ok := semanticSelectionByID(surface.plan, id)
			if ok && selection.FitProof.MatchedCapability == tool.CapabilityMessageSendMailbox {
				t.Fatal("confirmation completed the send selection")
			}
		}
	}
}

func TestTrustedMailboxConfigureRejectsMissingServerBeforeWrite(t *testing.T) {
	sender := &statusMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	before, err := h.app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	beforeCount := len(before.MailboxAccounts)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		userID, "帮我配置邮箱", "desktop", "root-mail-cfg-exec", "turn-mail-cfg-exec",
		&intent.ClassificationResult{Primary: intent.LabelMailboxConfigure, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	t.Cleanup(func() {
		if surface.coordinator != nil {
			_ = surface.coordinator.Close()
		}
	})
	name := semanticGrantNameForAdapter(surface, semanticTrustedMailboxConfigureAdapter)
	if name == "" {
		t.Fatal("missing configure grant")
	}
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID, semanticSurface: surface, tools: defs}
	got := cb.ExecuteTool(name, `{"action":"save","email":"znsoft@163.com","name":"znsoft-163"}`)
	if !strings.Contains(got, "mailbox_configure_server_required") {
		t.Fatalf("configure result=%q", got)
	}
	got = cb.ExecuteTool(name, `{"action":"save","email":"not an email","name":"znsoft-163","outbound":{"host":"smtp.example.com","port":465,"security":"tls"}}`)
	if !strings.Contains(got, "mailbox_configure_identity_invalid") {
		t.Fatalf("configure result=%q", got)
	}
	if _, live := surface.grants[surface.resolveFunctionName(name)]; !live {
		t.Fatal("pre-write configure rejection consumed the grant")
	}
	after, err := h.app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.MailboxAccounts) != beforeCount {
		t.Fatalf("accounts=%d, want %d", len(after.MailboxAccounts), beforeCount)
	}
}

func TestMailboxSendAttachmentRejectionKeepsTheGrant(t *testing.T) {
	sender := &statusMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	dir := t.TempDir()
	h.app.assistantSessionWorkingDirs.Store(userID, dir)
	name := "天气, 报告.pdf"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("%PDF-1.4 weather"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		userID, "发一封邮件", "desktop", "root-mail-attach", "turn-mail-attach",
		&intent.ClassificationResult{Primary: intent.LabelMailboxSend, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	closeSemanticSurface(t, surface)
	toolName := semanticGrantNameForAdapter(surface, semanticTrustedMailboxSendAdapter)
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID, semanticSurface: surface, tools: defs}
	grantLive := func() bool {
		_, live := surface.grants[surface.resolveFunctionName(toolName)]
		return live
	}
	replyArgs, err := json.Marshal(map[string]any{
		"action": "reply", "uid": 3, "text": "收到", "attachments": []string{name},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := cb.ExecuteTool(toolName, string(replyArgs))
	if !strings.Contains(got, "只有发送新邮件可以带附件") || !strings.Contains(got, "tool remains available") || !grantLive() {
		t.Fatalf("reply attachment result=%q live=%v", got, grantLive())
	}
	missingArgs, err := json.Marshal(map[string]any{
		"action": "send", "to": []string{"a@ex.com"}, "subject": "状态", "text": "在线",
		"attachments": []string{filepath.ToSlash(filepath.Join(dir, "missing.pdf"))},
	})
	if err != nil {
		t.Fatal(err)
	}
	got = cb.ExecuteTool(toolName, string(missingArgs))
	if !strings.Contains(got, "找不到附件") || !grantLive() || sender.calls != 0 {
		t.Fatalf("missing attachment result=%q live=%v calls=%d", got, grantLive(), sender.calls)
	}
	okArgs, err := json.Marshal(map[string]any{
		"action": "send", "to": []string{"a@ex.com"}, "subject": "状态", "text": "在线",
		"attachments": []string{filepath.ToSlash(filepath.Join(dir, name))},
	})
	if err != nil {
		t.Fatal(err)
	}
	got = cb.ExecuteTool(toolName, string(okArgs))
	if !strings.Contains(got, "请确认发送这封邮件") || !strings.Contains(got, name) || sender.calls != 0 {
		t.Fatalf("ready attachment result=%q calls=%d", got, sender.calls)
	}
}

func TestTrustedMailboxSendKeepsTheGrantWhenTheMessageIsIncomplete(t *testing.T) {
	sender := &statusMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		userID, "发一封邮件", "desktop", "root-mail-intake", "turn-mail-intake",
		&intent.ClassificationResult{Primary: intent.LabelMailboxSend, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	t.Cleanup(func() {
		if surface.coordinator != nil {
			_ = surface.coordinator.Close()
		}
	})
	name := semanticGrantNameForAdapter(surface, semanticTrustedMailboxSendAdapter)
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID, semanticSurface: surface, tools: defs}
	got := cb.ExecuteTool(name, `{"action":"send","to":["a@ex.com"]}`)
	if !strings.Contains(got, "mailbox_send_message_required") {
		t.Fatalf("result=%q", got)
	}
	got = cb.ExecuteTool(name, `{"action":"send","to":["not an email"],"subject":"状态","text":"在线"}`)
	if !strings.Contains(got, "mailbox_send_recipient_invalid") {
		t.Fatalf("result=%q", got)
	}
	got = cb.ExecuteTool(name, `{"action":"forward","uid":3}`)
	if !strings.Contains(got, "mailbox_send_recipient_required") {
		t.Fatalf("result=%q", got)
	}
	got = cb.ExecuteTool(name, `{"action":"send","to":"a@ex.com","subject":"状态","text":"在线"}`)
	if !strings.Contains(got, "parameter_type_mismatch: to (expected array)") || !strings.Contains(got, "tool remains available") {
		t.Fatalf("string recipient hid the field: %q", got)
	}
	if sender.calls != 0 {
		t.Fatal("incomplete send dialed SMTP")
	}
	if _, live := surface.grants[surface.resolveFunctionName(name)]; !live {
		t.Fatal("incomplete send consumed the grant")
	}
}

func TestTrustedMailboxConfigureSaveAdvancesTheSelection(t *testing.T) {
	sender := &statusMailSender{}
	h, userID := newMailboxToolFixture(t, sender)
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	before, err := h.app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		userID, "帮我配置邮箱", "desktop", "root-mail-save", "turn-mail-save",
		&intent.ClassificationResult{Primary: intent.LabelMailboxConfigure, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	t.Cleanup(func() {
		if surface.coordinator != nil {
			_ = surface.coordinator.Close()
		}
	})
	name := semanticGrantNameForAdapter(surface, semanticTrustedMailboxConfigureAdapter)
	consumed := surface.grants[name]
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID, semanticSurface: surface, tools: defs}
	got := cb.ExecuteTool(name, `{"action":"save","email":"other@ex.com","name":"other-box","outbound":{"host":"smtp.example.com","port":2525,"security":"tls"}}`)
	if !strings.Contains(got, "other-box") || strings.Contains(got, "[system rejected]") {
		t.Fatalf("result=%q", got)
	}
	// Configure's budget is several saves. The prompt name stays
	// mailbox_configure; the next save must be a different grant, and the
	// model list must still carry that name.
	if !surface.completed[consumed.SelectionID] {
		t.Fatal("saved configure did not complete its selection")
	}
	next, live := surface.grants[name]
	if !live {
		t.Fatal("saved configure dropped the remaining mailbox_configure budget")
	}
	if next.Token == "" || next.Token == consumed.Token || next.SelectionID == consumed.SelectionID {
		t.Fatalf("saved configure reused the consumed grant token=%q selection=%q", next.Token, next.SelectionID)
	}
	if retired, ok := surface.retiredGrants[name]; !ok || retired.Token != consumed.Token {
		t.Fatalf("consumed configure grant was not retired under %s: %#v", name, surface.retiredGrants)
	}
	if semanticDefForGrantName(cb.tools, name) == nil {
		t.Fatalf("model list lost %s after the save", name)
	}
	got = cb.ExecuteTool(name, `{"action":"save","email":"znsoft@163.com","name":"znsoft-163"}`)
	if !strings.Contains(got, "mailbox_configure_server_required") {
		t.Fatalf("successor configure result=%q", got)
	}
	if current, ok := surface.grants[name]; !ok || current.Token != next.Token {
		t.Fatal("pre-write rejection consumed the successor grant")
	}
	after, err := h.app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.MailboxAccounts) != len(before.MailboxAccounts)+1 {
		t.Fatalf("accounts=%d, before=%d", len(after.MailboxAccounts), len(before.MailboxAccounts))
	}
	var savedPort int
	for _, account := range after.MailboxAccounts {
		if account.Name == "other-box" && account.Outbound != nil {
			savedPort = account.Outbound.Port
		}
	}
	if savedPort != 2525 {
		t.Fatalf("outbound port=%d", savedPort)
	}
}
