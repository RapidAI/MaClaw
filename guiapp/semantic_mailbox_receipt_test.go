package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestMailboxSendPlanMaterializesOnlyTheTrustedSendAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-mail-send", "向 znsoft@163.com 发邮件报告系统状态", "desktop", "root-mail-send", "turn-mail-send",
		&intent.ClassificationResult{Primary: intent.LabelMailboxSend, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedMailboxSendAdapter) == "" {
		t.Fatal("send plan did not materialize the trusted mailbox send adapter")
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

func TestMailboxConfigurePlanDoesNotMaterializeSend(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-mail-cfg", "帮我配置邮箱", "desktop", "root-mail-cfg", "turn-mail-cfg",
		&intent.ClassificationResult{Primary: intent.LabelMailboxConfigure, Confidence: .92},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%v err=%v", handled, surface != nil, err)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedMailboxConfigureAdapter) == "" {
		t.Fatal("configure plan did not materialize the trusted configure adapter")
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedMailboxSendAdapter) != "" {
		t.Fatal("configure plan materialized mailbox send")
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
	got := cb.ExecuteTool(name, `{"action":"send","to":["a@ex.com"],"subject":"状态","text":"在线"}`)
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
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID, semanticSurface: surface, tools: defs}
	got := cb.ExecuteTool(name, `{"action":"save","email":"other@ex.com","name":"other-box","outbound":{"host":"smtp.example.com","port":2525,"security":"tls"}}`)
	if !strings.Contains(got, "other-box") || strings.Contains(got, "[system rejected]") {
		t.Fatalf("result=%q", got)
	}
	if _, live := surface.grants[surface.resolveFunctionName(name)]; live {
		t.Fatal("saved configure left the grant live")
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
