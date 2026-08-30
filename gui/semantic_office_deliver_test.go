package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// The office rule must plan the same write+deliver pair as document_generate:
// without the delivery leg a successful deck/sheet write left the turn dead on
// an unrendered send_file call (production incident: PPT written, send_file
// denied by the rendered-surface fence).
func TestIMSemanticOfficePlansWriteAndDeliver(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	needs, managed, err := semanticIntentNeedsFromClassification(registry, intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98})
	if err != nil || !managed {
		t.Fatalf("needs=%#v managed=%v err=%v", needs, managed, err)
	}
	foundWrite, foundDeliver := false, false
	for _, need := range needs {
		switch need.Capability {
		case tool.CapabilityDocumentWriteOffice:
			if need.Required {
				foundWrite = true
			}
		case "artifact.deliver.current_channel":
			// The leg is planned but optional: channels that deny file
			// delivery (e.g. VE) must not HostReject the whole office turn.
			foundDeliver = need.Qualifiers["format"] == "file"
		}
	}
	if !foundWrite || !foundDeliver {
		t.Fatalf("needs=%#v, want required office write + current-channel file deliver", needs)
	}
	// The office deliver consumes the in-turn artifact, never an attachment.
	resolved, err := semanticNeedsForTrustedDocumentInputs(needs, nil)
	if err != nil {
		t.Fatalf("office file deliver must not require an attachment: %v", err)
	}
	if len(resolved) != len(needs) {
		t.Fatalf("resolved=%#v want %#v", resolved, needs)
	}
}

// The office write adapter must register the produced document as a broker
// artifact bound to its own selection, so the planned send_file selection can
// consume it through the current channel.
func TestIMSemanticOfficeWritePublishesArtifactAndDeliverConsumes(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := app.SetTabWorkingDir("", workspace); err != nil {
		t.Fatalf("SetTabWorkingDir: %v", err)
	}
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice), app: app}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		desktopUserID, "把销售数据整理成表格发给我", "desktop", "root-office-deliver", "turn-office-deliver",
		&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityDocumentWriteOffice, "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", surface.plan.Selections)
	}
	var writeSel, deliverSel tool.PlannedSelection
	for _, selection := range surface.plan.Selections {
		switch selection.FitProof.MatchedCapability {
		case tool.CapabilityDocumentWriteOffice:
			// The office rule carries a draft+revision budget; the delivery
			// binds the first sibling and family matching reaches the rest.
			if writeSel.ID == "" {
				writeSel = selection
			}
		case "artifact.deliver.current_channel":
			deliverSel = selection
		}
	}
	if len(deliverSel.ArtifactDependencies) != 1 || tool.RepeatFamilyID(deliverSel.ArtifactDependencies[0].ProducerSelection) != tool.RepeatFamilyID(writeSel.ID) {
		t.Fatalf("deliver must depend on the office write artifact family: %#v", deliverSel.ArtifactDependencies)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop",
		loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:user-1"}},
	}
	officeName := semanticGrantNameForAdapter(surface, semanticTrustedOfficeWriteAdapter)
	if officeName != "office" {
		t.Fatalf("managed office name=%q, want office", officeName)
	}
	got := cb.ExecuteToolCall(officeName, `{"path":"birthday.pptx","title":"生日","slides":[{"title":"封面","bullets":["快乐"]}]}`, "call-office").Result
	if !strings.Contains(got, "Office document artifact published") {
		t.Fatalf("office write result=%q", got)
	}
	// The write must have registered a presentation artifact for its selection.
	published, err := surface.artifacts.store.PublishedArtifacts(surface.scope, writeSel.ID)
	if err != nil || len(published) == 0 {
		t.Fatalf("office write must publish an artifact: published=%#v err=%v", published, err)
	}
	if published[0].Kind != "document" || published[0].MIMEType != "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		t.Fatalf("artifact=%+v, want document/pptx", published[0])
	}
	// After the write succeeded, the next request surface must carry send_file.
	deliveryName := semanticGrantNameForAdapter(surface, "semantic_deliver_current_file")
	if deliveryName != "send_file" {
		grants := make([]string, 0, len(surface.grants))
		for name := range surface.grants {
			grants = append(grants, name)
		}
		t.Fatalf("send_file grant missing after office write: grants=%v result=%q", grants, got)
	}
	delivered := cb.ExecuteToolCall(deliveryName, `{}`, "call-deliver").Result
	if !strings.Contains(delivered, "Delivery committed to the current channel") {
		t.Fatalf("deliver result=%q", delivered)
	}
	if cb.semanticDeliveryFileData == "" || cb.semanticDeliveryFileMIME != "application/vnd.openxmlformats-officedocument.presentationml.presentation" {
		t.Fatalf("deliver must project the office artifact: mime=%q", cb.semanticDeliveryFileMIME)
	}
}

// The Fix-D fallback for a contradicted tree verdict is a degraded office
// hint. That hint must still plan a full governed office surface (write +
// deliver legs), never a HostReject: the 2026-08-26 turn classified a PPT
// request as browser 0.90 and died at "当前能力目录未覆盖这项请求" because
// the infeasible browser plan was the only route offered.
func TestIMSemanticDegradedOfficeHintPlansWriteAndDeliver(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := app.SetTabWorkingDir("", workspace); err != nil {
		t.Fatalf("SetTabWorkingDir: %v", err)
	}
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice), app: app}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		desktopUserID, "生成庆祝我家布偶宝宝5岁生日的ppt, 没有照片，网上随便找一下布偶照片。", "desktop", "root-office-hint", "turn-office-hint",
		&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .855, Degraded: true},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if !planHasCapabilities(surface.plan, tool.CapabilityDocumentWriteOffice, "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", surface.plan.Selections)
	}
}

// Regression for the 2026-08-26 birthday-deck turn: the rendered office
// schema declares sheets and slides side by side, so the model sent
// {"slides":[...], "sheets":[]}. Strict admission burned the one-shot grant
// on that correctable shape, the tool vanished mid-turn, and the deck was
// never written. The host wash must normalize the empty unused form before
// canonical validation so the write succeeds end to end.
func TestIMSemanticOfficeWriteToleratesEmptyUnusedForm(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := app.SetTabWorkingDir("", workspace); err != nil {
		t.Fatalf("SetTabWorkingDir: %v", err)
	}
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice), app: app}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		desktopUserID, "生成庆祝我家布偶宝宝5岁生日的ppt", "desktop", "root-office-wash", "turn-office-wash",
		&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
	officeName := semanticGrantNameForAdapter(surface, semanticTrustedOfficeWriteAdapter)
	got := cb.ExecuteToolCall(officeName, `{"path":"布偶宝宝5岁生日.pptx","title":"布偶宝宝5岁生日","subtitle":"庆祝","slides":[{"title":"封面","bullets":["快乐"]}],"sheets":[]}`, "call-office-wash").Result
	if strings.Contains(got, "arguments_rejected") || strings.Contains(got, "parameter_schema_invalid") {
		t.Fatalf("empty unused form must not burn the grant: %q", got)
	}
	if !strings.Contains(got, "Office document artifact published") {
		t.Fatalf("office write result=%q", got)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "布偶宝宝5岁生日.pptx")); statErr != nil {
		t.Fatalf("deck missing: %v", statErr)
	}

	// The retry shape from the same turn was worse: both forms stringified.
	// Canonical validation would reject the string type and burn the grant
	// before the executor's tolerance can run, so the host wash must unwrap
	// the arrays ahead of it. A fresh surface carries a fresh one-shot grant.
	defs2, surface2, handled2, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		desktopUserID, "生成庆祝我家布偶宝宝5岁生日的ppt", "desktop", "root-office-wash", "turn-office-wash-2",
		&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98},
	)
	if err != nil || !handled2 || surface2 == nil || len(defs2) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs2, handled2, err)
	}
	cb2 := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface2, platform: "desktop"}
	officeName2 := semanticGrantNameForAdapter(surface2, semanticTrustedOfficeWriteAdapter)
	got2 := cb2.ExecuteToolCall(officeName2, `{"path":"布偶宝宝5岁生日2.pptx","title":"布偶宝宝5岁生日","slides":"[{\"title\":\"封面\",\"bullets\":[\"快乐\"]}]","sheets":"[]"}`, "call-office-wash-2").Result
	if strings.Contains(got2, "arguments_rejected") || strings.Contains(got2, "parameter_schema_invalid") {
		t.Fatalf("stringified forms must not burn the grant: %q", got2)
	}
	if !strings.Contains(got2, "Office document artifact published") {
		t.Fatalf("office write result=%q", got2)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "布偶宝宝5岁生日2.pptx")); statErr != nil {
		t.Fatalf("deck missing: %v", statErr)
	}
}

// Draft-then-revise: the office rule carries a two-write budget, and the
// delivery bound to the first sibling must reach the revision — the newest
// artifact in the repeat family is the one delivered (production 2026-08-26:
// photos landed after the first write; the revision must be what ships).
func TestIMSemanticOfficeRevisionDeliversNewestFamilyArtifact(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	workspace := filepath.Join(t.TempDir(), "workspace")
	if err := app.SetTabWorkingDir("", workspace); err != nil {
		t.Fatalf("SetTabWorkingDir: %v", err)
	}
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelOffice), app: app}
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		desktopUserID, "做个生日PPT发给我", "desktop", "root-office-revise", "turn-office-revise",
		&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: .98},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop",
		loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:user-1"}},
	}
	if got := cb.ExecuteToolCall("office", `{"path":"draft.pptx","slides":[{"title":"草稿"}]}`, "call-draft").Result; !strings.Contains(got, "artifact published") {
		t.Fatalf("draft write=%q", got)
	}
	if got := cb.ExecuteToolCall("office", `{"path":"final.pptx","slides":[{"title":"定稿"}]}`, "call-final").Result; !strings.Contains(got, "artifact published") {
		t.Fatalf("revision write must succeed through the sibling grant: %q", got)
	}
	var revisionSel string
	for _, selection := range surface.plan.Selections {
		if selection.FitProof.MatchedCapability == tool.CapabilityDocumentWriteOffice && strings.HasSuffix(selection.ID, "#02") {
			revisionSel = selection.ID
		}
	}
	if revisionSel == "" {
		t.Fatal("office rule must plan a revision sibling")
	}
	delivered := cb.ExecuteToolCall("send_file", `{}`, "call-deliver").Result
	if !strings.Contains(delivered, "Delivery committed") {
		t.Fatalf("deliver=%q", delivered)
	}
	revisionArtifacts, err := surface.artifacts.store.PublishedArtifacts(surface.scope, revisionSel)
	if err != nil || len(revisionArtifacts) == 0 {
		t.Fatalf("revision must publish an artifact: %#v err=%v", revisionArtifacts, err)
	}
	if cb.semanticPreparedDelivery == nil || cb.semanticPreparedDelivery.ArtifactID != revisionArtifacts[0].ID {
		t.Fatalf("delivered artifact=%+v, want the newest family artifact %s", cb.semanticPreparedDelivery, revisionArtifacts[0].ID)
	}
}
