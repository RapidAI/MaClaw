package guiapp

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestProducedDocumentBindsSpecifiedTargetAndNotGenerate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "崇州天气与风土人情.pdf")
	body := []byte("%PDF-1.4\nproduced weather")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	userID := "desktop-user:weather-task"
	h := registerDocumentGeneratePDF(t)
	msg := IMUserMessage{UserID: userID, Platform: "desktop", Text: "北京天气，生成pdf"}
	h.settleSemanticSessionResidue(msg, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:gen", Capability: agentservice.CapabilityDocumentGenerate, Required: true},
	}}, &IMAgentResponse{LocalFilePath: path})

	target := hostOwnedCurrentChannelDeliveryTarget("desktop", userID)
	if target == nil {
		t.Fatal("desktop delivery target missing")
	}
	classification := &intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.70, Layer: 3}
	ctx := withSemanticDestination(context.Background(), target.DestinationID)
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, userID, "send the document", "desktop", "root-produced", "turn-produced", classification, nil,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("prepared=%#v handled=%v err=%v", prepared, handled, err)
	}
	if planHasCapabilities(prepared.plan, "document.generate.file", "information.search.web") {
		t.Fatalf("delivery of the produced file replanned generate: %#v", prepared.plan.Selections)
	}
	selection, ok := semanticSelectionForCapability(prepared.plan, semanticSpecifiedTargetDeliveryCapability)
	if !ok || selection.AdapterName != semanticSpecifiedTargetDeliveryAdapter || len(selection.ArtifactDependencies) != 1 || selection.ArtifactDependencies[0].Artifact.ID == "" {
		t.Fatalf("selection=%+v ok=%v", selection, ok)
	}
	if selection.ArtifactDependencies[0].ProducerSelection != "" {
		t.Fatalf("specified-target bound a same-plan producer: %+v", selection.ArtifactDependencies[0])
	}

	missing := registerDocumentGeneratePDF(t)
	_, handled, err = missing.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, userID, "send the document", "desktop", "root-missing", "turn-missing", classification, nil,
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "artifact_dependency_missing") {
		t.Fatalf("missing product must fail closed, handled=%v err=%v", handled, err)
	}

	staleBody := []byte("%PDF-1.4\nchanged")
	if err := os.WriteFile(path, staleBody, 0o644); err != nil {
		t.Fatal(err)
	}
	_, handled, err = h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, userID, "send the document", "desktop", "root-stale", "turn-stale", classification, nil,
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "produced_document_stale") {
		t.Fatalf("stale product must fail closed, handled=%v err=%v", handled, err)
	}
}

func TestDeliveryTurnDoesNotReplaceProducedDocument(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "崇州天气与风土人情.pdf")
	replacement := filepath.Join(dir, "发到微信.pdf")
	if err := os.WriteFile(original, []byte("%PDF-1.4\noriginal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("%PDF-1.4\nreplacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	userID := "desktop-user:keep-product"
	h := &IMMessageHandler{}
	msg := IMUserMessage{UserID: userID, Platform: "desktop"}
	h.settleSemanticSessionResidue(msg, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:gen", Capability: "document.generate.file", Required: true},
	}}, &IMAgentResponse{LocalFilePath: original})
	h.settleSemanticSessionResidue(msg, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:send", Capability: agentservice.CapabilityArtifactDeliverSpecified, Required: true, Qualifiers: map[string]string{"format": "file"}},
	}}, &IMAgentResponse{LocalFilePath: replacement})

	inputs, ok, err := h.semanticProducedDocumentInputsForTurn("root", "turn", "session", userID, "desktop")
	if err != nil || !ok || len(inputs) != 1 {
		t.Fatalf("inputs=%d ok=%v err=%v", len(inputs), ok, err)
	}
	if inputs[0].Payload.Ref.Name != "崇州天气与风土人情.pdf" {
		t.Fatalf("name=%q", inputs[0].Payload.Ref.Name)
	}
	decoded, err := base64.StdEncoding.DecodeString(inputs[0].Payload.Base64)
	if err != nil || string(decoded) != "%PDF-1.4\noriginal" {
		t.Fatalf("decoded=%q err=%v", decoded, err)
	}
	h.clearSemanticSessionResidue(userID)
	if _, ok, err := h.semanticProducedDocumentInputsForTurn("root", "turn-2", "session", userID, "desktop"); ok || err != nil {
		t.Fatalf("cleared residue kept the product ok=%v err=%v", ok, err)
	}
}

func TestDesktopSpecifiedTargetForwardsBoundFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "崇州天气与风土人情.pdf")
	body := []byte("%PDF-1.4\nforward me")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	userID := "desktop-user:forward"
	h := registerDocumentGeneratePDF(t)
	h.settleSemanticSessionResidue(IMUserMessage{UserID: userID, Platform: "desktop"}, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:gen", Capability: "document.generate.file", Required: true},
	}}, &IMAgentResponse{LocalFilePath: path})
	target := hostOwnedCurrentChannelDeliveryTarget("desktop", userID)
	classification := &intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.70, Layer: 3}
	ctx := withSemanticDestination(context.Background(), target.DestinationID)

	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "send the document", "desktop", "root-forward-missing-sender", "turn-forward-missing-sender", classification, nil,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("surface handled=%v err=%v defs=%d", handled, err, len(defs))
	}
	selection, ok := semanticSelectionForCapability(surface.plan, semanticSpecifiedTargetDeliveryCapability)
	if !ok {
		t.Fatalf("selections=%#v", surface.plan.Selections)
	}
	rejected := (&sharedAgentLoopCallbacks{
		handler: h, platform: "desktop", userID: userID, semanticSurface: surface,
		loopCtx: &LoopContext{DeliveryTarget: target},
	}).executeBoundSemanticSelection(selection, `{}`)
	if rejected.Succeeded || !strings.Contains(rejected.Result, "im_file_sender_not_configured") {
		t.Fatalf("missing sender result=%+v", rejected)
	}

	var calls int
	var gotName, gotData string
	h.structuredIMFileSender = func(req agent.IMFileDeliveryRequest) error {
		calls++
		gotName = req.FileName
		gotData = req.Data
		return nil
	}
	_, surface, handled, err = h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "send the document", "desktop", "root-forward", "turn-forward", classification, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("forward surface handled=%v err=%v", handled, err)
	}
	selection, ok = semanticSelectionForCapability(surface.plan, semanticSpecifiedTargetDeliveryCapability)
	if !ok {
		t.Fatal("specified-target selection missing")
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, platform: "desktop", userID: userID, semanticSurface: surface,
		loopCtx: &LoopContext{DeliveryTarget: target},
	}
	forwarded := cb.executeBoundSemanticSelection(selection, `{}`)
	if !forwarded.Succeeded || forwarded.AwaitingReceipt || calls != 1 {
		t.Fatalf("forwarded=%+v calls=%d", forwarded, calls)
	}
	if gotName != "崇州天气与风土人情.pdf" {
		t.Fatalf("name=%q", gotName)
	}
	decoded, err := base64.StdEncoding.DecodeString(gotData)
	if err != nil || string(decoded) != string(body) {
		t.Fatalf("decoded=%q err=%v", decoded, err)
	}
	if !strings.Contains(forwarded.Result, "desktop IM sender") {
		t.Fatalf("result=%q", forwarded.Result)
	}
	again := cb.executeBoundSemanticSelection(selection, `{}`)
	if !again.Succeeded || calls != 1 {
		t.Fatalf("second call resent the file: forwarded=%+v calls=%d", again, calls)
	}
}

func TestHostSpecifiedTargetDeliversWhenModelStops(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "崇州天气与风土人情.pdf")
	body := []byte("%PDF-1.4\nforward when the model stops")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	const userID = "desktop-user:host-specified-stop"
	h := registerDocumentGeneratePDF(t)
	h.settleSemanticSessionResidue(IMUserMessage{UserID: userID, Platform: "desktop"}, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:gen", Capability: "document.generate.file", Required: true},
	}}, &IMAgentResponse{LocalFilePath: path})
	target := hostOwnedCurrentChannelDeliveryTarget("desktop", userID)
	classification := &intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.70, Layer: 3}
	ctx := withSemanticDestination(context.Background(), target.DestinationID)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "发到微信", "desktop", "root-host-stop", "turn-host-stop", classification, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	if planHasCapabilities(surface.plan, "artifact.deliver.current_channel", "document.generate.file") {
		t.Fatalf("document delivery grew a generate or current-channel grant: %#v", surface.plan.Selections)
	}
	if name, _ := soleLiveSemanticGrantByAdapter(surface, semanticSpecifiedTargetDeliveryAdapter); name == "" {
		t.Fatalf("specified-target grant was not live for a bound document: %#v", surface.grants)
	}
	if name, _ := soleLiveSemanticGrantByAdapter(surface, "semantic_deliver_current_file"); name != "" {
		t.Fatalf("current-channel grant would consume the document before WeChat: %s", name)
	}
	var calls int
	var gotName, gotMessage string
	h.structuredIMFileSender = func(req agent.IMFileDeliveryRequest) error {
		calls++
		gotName = req.FileName
		gotMessage = req.Message
		return nil
	}
	app := &App{}
	app.assistantSessionWorkingDirs.Store(userID, root)
	h.app = app
	cb := &sharedAgentLoopCallbacks{
		handler: h, platform: "desktop", userID: userID, userText: "发到微信",
		semanticSurface: surface, loopCtx: &LoopContext{DeliveryTarget: target},
	}
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if calls != 1 || cb.turnPublishedPDF {
		t.Fatalf("calls=%d published=%v text=%q", calls, cb.turnPublishedPDF, resp.Text)
	}
	if gotName != "崇州天气与风土人情.pdf" {
		t.Fatalf("name=%q", gotName)
	}
	want := localizeIMProactiveCaption(h.imUILangOrZh(), "崇州天气与风土人情.pdf", "application/pdf")
	if resp.Text != want || gotMessage != want || strings.Contains(resp.Text, "已生成") || strings.Contains(resp.Text, "发到微信") {
		t.Fatalf("text=%q message=%q want=%q", resp.Text, gotMessage, want)
	}
	if resp.FileName != "崇州天气与风土人情.pdf" || filepath.Base(resp.LocalFilePath) != "崇州天气与风土人情.pdf" {
		t.Fatalf("file identity lost: name=%q path=%q", resp.FileName, resp.LocalFilePath)
	}
	if _, err := os.Stat(filepath.Join(root, "发到微信.pdf")); err == nil {
		t.Fatal("model stop wrote a report named from the utterance")
	}
	if name, _ := soleLiveSemanticGrantByAdapter(surface, semanticSpecifiedTargetDeliveryAdapter); name != "" {
		t.Fatalf("grant stayed live after the host send: %s", name)
	}
	attachSharedLoopArtifacts(resp, cb)
	if calls != 1 {
		t.Fatalf("second attach sent again: calls=%d", calls)
	}
}

func TestSendFilePetitionOnBoundSpecifiedTargetKeepsIMSend(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "崇州天气与风土人情.pdf")
	body := []byte("%PDF-1.4\npetition must not steal")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	const userID = "desktop-user:petition-specified"
	h := registerDocumentGeneratePDF(t)
	h.settleSemanticSessionResidue(IMUserMessage{UserID: userID, Platform: "desktop"}, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:gen", Capability: "document.generate.file", Required: true},
	}}, &IMAgentResponse{LocalFilePath: path})
	target := hostOwnedCurrentChannelDeliveryTarget("desktop", userID)
	ctx := withSemanticDestination(context.Background(), target.DestinationID)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "发到微信", "desktop", "root-petition-specified", "turn-petition-specified",
		&intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.70, Layer: 3}, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	calls := 0
	h.structuredIMFileSender = func(req agent.IMFileDeliveryRequest) error {
		calls++
		if req.FileName != "崇州天气与风土人情.pdf" {
			t.Fatalf("name=%q", req.FileName)
		}
		return nil
	}
	app := &App{}
	app.assistantSessionWorkingDirs.Store(userID, root)
	h.app = app
	cb := &sharedAgentLoopCallbacks{
		handler: h, platform: "desktop", userID: userID, userText: "发到微信",
		semanticSurface: surface, loopCtx: &LoopContext{DeliveryTarget: target},
	}
	for _, name := range []string{"send_file", "generate_pdf", "office"} {
		granted, message := cb.PetitionToolCall(name)
		if !granted || !strings.Contains(message, "send_to_im") || strings.Contains(message, "参数不变") || cb.semanticEffectfulPetitionConsumed {
			t.Fatalf("%s petition granted=%v consumed=%v message=%q", name, granted, cb.semanticEffectfulPetitionConsumed, message)
		}
	}
	for _, capability := range []tool.CapabilityID{"artifact.deliver.current_channel", "document.generate.file", tool.CapabilityDocumentWriteOffice} {
		if planHasCapabilities(surface.plan, capability) {
			t.Fatalf("petition grew %s: %#v", capability, surface.plan.Selections)
		}
	}
	rejected := cb.ExecuteToolCall("send_to_im", `{"path":"崇州天气与风土人情.pdf"}`, "model-legacy-path").Result
	if calls != 0 || !strings.Contains(rejected, "parameter_unknown_field") {
		t.Fatalf("legacy path args calls=%d result=%q", calls, rejected)
	}
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if calls != 1 || cb.turnPublishedPDF || strings.Contains(resp.Text, "已生成") || strings.Contains(resp.Text, "发到微信") {
		t.Fatalf("calls=%d published=%v text=%q", calls, cb.turnPublishedPDF, resp.Text)
	}
	want := localizeIMProactiveCaption(h.imUILangOrZh(), "崇州天气与风土人情.pdf", "application/pdf")
	if resp.Text != want || resp.FileName != "崇州天气与风土人情.pdf" {
		t.Fatalf("text=%q name=%q want=%q", resp.Text, resp.FileName, want)
	}
}

func TestSpecifiedTargetDeliveryEndsTheTurn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "崇州天气与风土人情.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\nstop after send"), 0o644); err != nil {
		t.Fatal(err)
	}
	const userID = "desktop-user:specified-stop"
	h := registerDocumentGeneratePDF(t)
	h.settleSemanticSessionResidue(IMUserMessage{UserID: userID, Platform: "desktop"}, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:gen", Capability: "document.generate.file", Required: true},
	}}, &IMAgentResponse{LocalFilePath: path})
	target := hostOwnedCurrentChannelDeliveryTarget("desktop", userID)
	ctx := withSemanticDestination(context.Background(), target.DestinationID)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "发到微信", "desktop", "root-specified-stop", "turn-specified-stop",
		&intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.70, Layer: 3}, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, loopCtx: &LoopContext{DeliveryTarget: target}}
	if stop, code, _ := cb.EarlyStop(); stop || code != "" {
		t.Fatalf("fresh delivery turn must not stop: stop=%v code=%q", stop, code)
	}
	specifiedID := ""
	for _, selection := range surface.plan.Selections {
		if semanticSpecifiedTargetArtifactDelivery(selection) {
			specifiedID = selection.ID
			continue
		}
		surface.completed[selection.ID] = true
	}
	if specifiedID == "" {
		t.Fatal("specified-target selection missing")
	}
	if stop, _, _ := cb.EarlyStop(); stop {
		t.Fatal("open specified-target must not end the turn")
	}
	surface.completed[specifiedID] = true
	stop, code, text := cb.EarlyStop()
	if !stop || code != "" || text != "" {
		t.Fatalf("sent specified-target must end the turn: stop=%v code=%q text=%q", stop, code, text)
	}
}

func TestHostSpecifiedTargetOnWeixinDoesNotCallIMFileSenderWhenModelStops(t *testing.T) {
	userID := "wx-user-host-stop"
	h := &IMMessageHandler{registry: NewToolRegistry()}
	body := []byte("%PDF-1.4\nweixin host stop")
	attachment := MessageAttachment{
		Type: "file", FileName: "崇州天气与风土人情.pdf", MimeType: "application/pdf",
		Data: base64.StdEncoding.EncodeToString(body),
	}
	calls := 0
	h.imFileSender = func(string, string, string, string) error {
		calls++
		return nil
	}
	destination := "user:" + userID
	ctx := withSemanticDestination(context.Background(), destination)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "发到微信", "weixin", "root-wx-host-stop", "turn-wx-host-stop",
		&intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.90, Layer: 3},
		[]MessageAttachment{attachment},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	if name, _ := soleLiveSemanticGrantByAdapter(surface, semanticSpecifiedTargetDeliveryAdapter); name == "" {
		t.Fatalf("specified-target grant was not live: %#v", surface.grants)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, platform: "weixin", userID: userID, userText: "发到微信", semanticSurface: surface,
		loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: semanticChannelScope("weixin"), DestinationID: destination}},
	}
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if calls != 0 || cb.filesForwarded != 0 || cb.turnPublishedPDF {
		t.Fatalf("weixin host flush called the sender: calls=%d forwarded=%d published=%v text=%q", calls, cb.filesForwarded, cb.turnPublishedPDF, resp.Text)
	}
	if resp.FileData != attachment.Data || strings.TrimSpace(resp.FileName) == "" {
		t.Fatalf("gateway attachment lost: name=%q dataEmpty=%v", resp.FileName, resp.FileData == "")
	}
	if strings.Contains(resp.Text, "已生成") || strings.Contains(resp.Text, "发到微信") {
		t.Fatalf("text=%q", resp.Text)
	}
}

func TestHostCurrentChannelStopDoesNotForwardBoundFile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "崇州天气与风土人情.pdf")
	body := []byte("%PDF-1.4\nlocal card host stop")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	userID := "desktop-user:current-host-stop"
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.settleSemanticSessionResidue(IMUserMessage{UserID: userID, Platform: "desktop"}, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:office", Capability: tool.CapabilityDocumentWriteOffice, Required: true},
	}}, &IMAgentResponse{LocalFilePath: path})
	calls := 0
	h.structuredIMFileSender = func(agent.IMFileDeliveryRequest) error {
		calls++
		return nil
	}
	target := hostOwnedCurrentChannelDeliveryTarget("desktop", userID)
	ctx := withSemanticDestination(context.Background(), target.DestinationID)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "send the document", "desktop", "root-current-host-stop", "turn-current-host-stop",
		&intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: 0.90, Layer: 3}, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	if name, _ := soleLiveSemanticGrantByAdapter(surface, "semantic_deliver_current_file"); name == "" {
		t.Fatalf("current-channel grant missing: %#v", surface.grants)
	}
	if name, _ := soleLiveSemanticGrantByAdapter(surface, semanticSpecifiedTargetDeliveryAdapter); name != "" {
		t.Fatalf("attachment delivery exposed specified-target: %s", name)
	}
	app := &App{}
	app.assistantSessionWorkingDirs.Store(userID, root)
	h.app = app
	cb := &sharedAgentLoopCallbacks{
		handler: h, platform: "desktop", userID: userID, semanticSurface: surface,
		loopCtx: &LoopContext{DeliveryTarget: target},
	}
	resp := &IMAgentResponse{}
	attachSharedLoopArtifacts(resp, cb)
	if calls != 0 || cb.filesForwarded != 0 || cb.turnPublishedPDF {
		t.Fatalf("current-channel host flush forwarded the file: calls=%d forwarded=%d published=%v", calls, cb.filesForwarded, cb.turnPublishedPDF)
	}
	if resp.FileName != "崇州天气与风土人情.pdf" || strings.Contains(resp.Text, "已生成") {
		t.Fatalf("local card lost: name=%q text=%q", resp.FileName, resp.Text)
	}
}

func TestWeixinSpecifiedTargetDoesNotCallIMFileSender(t *testing.T) {
	userID := "wx-user"
	h := &IMMessageHandler{registry: NewToolRegistry()}
	// The produced-document store is desktop-scoped. This turn carries the
	// file as a channel attachment so the assertion is only about the sender.
	attachment := MessageAttachment{
		Type: "file", FileName: "崇州天气与风土人情.pdf", MimeType: "application/pdf",
		Data: base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\nweixin")),
	}
	calls := 0
	h.imFileSender = func(string, string, string, string) error {
		calls++
		return nil
	}
	destination := "user:" + userID
	ctx := withSemanticDestination(context.Background(), destination)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "send the document", "weixin", "root-wx", "turn-wx",
		&intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.90, Layer: 3},
		[]MessageAttachment{attachment},
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, semanticSpecifiedTargetDeliveryCapability)
	if !ok {
		t.Fatalf("selections=%#v unmet=%#v", surface.plan.Selections, surface.plan.Unmet)
	}
	result := (&sharedAgentLoopCallbacks{
		handler: h, platform: "weixin", userID: userID, semanticSurface: surface,
		loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: semanticChannelScope("weixin"), DestinationID: destination}},
	}).executeBoundSemanticSelection(selection, `{}`)
	if result.Succeeded || !result.AwaitingReceipt || calls != 0 {
		t.Fatalf("weixin result=%+v calls=%d", result, calls)
	}
}

func TestDesktopCurrentChannelDoesNotForwardBoundFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "崇州天气与风土人情.pdf")
	body := []byte("%PDF-1.4\nlocal card")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	userID := "desktop-user:local-card"
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.settleSemanticSessionResidue(IMUserMessage{UserID: userID, Platform: "desktop"}, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:office", Capability: tool.CapabilityDocumentWriteOffice, Required: true},
	}}, &IMAgentResponse{LocalFilePath: path})
	calls := 0
	h.imFileSender = func(string, string, string, string) error {
		calls++
		return nil
	}
	target := hostOwnedCurrentChannelDeliveryTarget("desktop", userID)
	ctx := withSemanticDestination(context.Background(), target.DestinationID)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "send the document", "desktop", "root-current", "turn-current",
		&intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: 0.90, Layer: 3}, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, "artifact.deliver.current_channel")
	if !ok || planHasCapabilities(surface.plan, "document.generate.file") {
		t.Fatalf("selection ok=%v plan=%#v", ok, surface.plan.Selections)
	}
	result := (&sharedAgentLoopCallbacks{
		handler: h, platform: "desktop", userID: userID, semanticSurface: surface,
		loopCtx: &LoopContext{DeliveryTarget: target},
	}).executeBoundSemanticSelection(selection, `{}`)
	if result.Succeeded || !result.AwaitingReceipt || calls != 0 || !strings.Contains(result.Result, "current channel") {
		t.Fatalf("current-channel result=%+v calls=%d", result, calls)
	}
}

func TestAttachedExistingPDFIsNotAGenerateReceipt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("%PDF-1.4\noriginal-weather")
	original := filepath.Join(root, "崇州天气与风土人情.pdf")
	if err := os.WriteFile(original, body, 0o644); err != nil {
		t.Fatal(err)
	}
	const owner = "desktop-user:deliver-receipt"
	app := &App{}
	app.assistantSessionWorkingDirs.Store(owner, root)
	cb := &sharedAgentLoopCallbacks{
		handler:                  &IMMessageHandler{app: app},
		userID:                   owner,
		platform:                 "desktop",
		userText:                 "发到微信",
		filesForwarded:           1,
		semanticDeliveryFileData: base64.StdEncoding.EncodeToString(body),
		semanticDeliveryFileName: "崇州天气与风土人情.pdf",
		semanticDeliveryFileMIME: "application/pdf",
	}
	resp := &IMAgentResponse{Error: "LLM call failed: timeout"}
	attachSharedLoopArtifacts(resp, cb)
	if cb.turnPublishedPDF || strings.Contains(resp.Text, "已生成") || strings.Contains(resp.Text, "发到微信") {
		t.Fatalf("delivery of an existing PDF was labeled as a new report: published=%v text=%q", cb.turnPublishedPDF, resp.Text)
	}
	want := localizeIMProactiveCaption(cb.handler.imUILangOrZh(), "崇州天气与风土人情.pdf", "application/pdf")
	if resp.Text != want || !strings.Contains(resp.Text, "崇州天气与风土人情.pdf") {
		t.Fatalf("empty forward should name the artifact, text=%q want=%q", resp.Text, want)
	}
	if resp.Error != "" {
		t.Fatalf("stale timeout hid the delivered file: %q", resp.Error)
	}
	if resp.FileName != "崇州天气与风土人情.pdf" || filepath.Base(resp.LocalFilePath) != "崇州天气与风土人情.pdf" {
		t.Fatalf("file identity lost: name=%q path=%q", resp.FileName, resp.LocalFilePath)
	}
	if _, err := os.Stat(filepath.Join(root, "发到微信.pdf")); err == nil {
		t.Fatal("delivery wrote a report named from the utterance")
	}
}

func TestDesktopSpecifiedTargetDoesNotForwardUnsupportedKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "崇州天气与风土人情.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\nkind gate"), 0o644); err != nil {
		t.Fatal(err)
	}
	userID := "desktop-user:kind-gate"
	h := registerDocumentGeneratePDF(t)
	h.settleSemanticSessionResidue(IMUserMessage{UserID: userID, Platform: "desktop"}, &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:gen", Capability: "document.generate.file", Required: true},
	}}, &IMAgentResponse{LocalFilePath: path})
	target := hostOwnedCurrentChannelDeliveryTarget("desktop", userID)
	ctx := withSemanticDestination(context.Background(), target.DestinationID)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, userID, "send the document", "desktop", "root-kind", "turn-kind",
		&intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.70, Layer: 3}, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v", handled, err)
	}
	selection, ok := semanticSelectionForCapability(surface.plan, semanticSpecifiedTargetDeliveryCapability)
	if !ok {
		t.Fatal("specified-target selection missing")
	}
	payload, err := tool.NewArtifactPayload(surface.scope, "trusted-input:bundle", "bundle", "application/octet-stream", base64.StdEncoding.EncodeToString([]byte("not-a-document")), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := surface.artifacts.store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	selection.ID = selection.ID + ":bundle"
	selection.Consumes = []tool.ArtifactContract{{Kind: "bundle", Required: true}}
	selection.ArtifactDependencies = []tool.ArtifactDependency{{
		ArtifactID: ref.ID,
		Artifact:   tool.ArtifactBindingFromRef(ref),
		Contract:   tool.ArtifactContract{Kind: "bundle", Required: true},
	}}
	calls := 0
	h.structuredIMFileSender = func(agent.IMFileDeliveryRequest) error {
		calls++
		return nil
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, platform: "desktop", userID: userID, semanticSurface: surface,
		loopCtx: &LoopContext{DeliveryTarget: target},
	}
	result := cb.executeBoundSemanticSelection(selection, `{}`)
	if calls != 0 || cb.filesForwarded != 0 || cb.semanticDeliveryFileData != "" || cb.semanticDeliveryImageKey != "" || cb.semanticDeliveryVoiceData != "" {
		t.Fatalf("unsupported kind left the host: calls=%d forwarded=%d result=%+v", calls, cb.filesForwarded, result)
	}
	if !strings.Contains(result.Result, "current_channel_artifact_kind_unsupported") {
		t.Fatalf("result=%+v", result)
	}
}
