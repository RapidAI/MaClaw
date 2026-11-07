package guiapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// A current-channel delivery is this host's own response attach. Its receipt
// cannot arrive from another model round, so it must not hold the stop open.
// Counting it did: every desktop file turn stopped with
// required_effect_unsettled after two no-tool stops, and the kept recovery
// marker then refused the next message as an uncertain external send.
func TestCurrentChannelDeliveryAwaitingReceiptDoesNotBlockModelStop(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	surface.plan.Selections = append(surface.plan.Selections, tool.PlannedSelection{
		ID:       "deliver",
		NeedID:   "need:deliver",
		Provider: tool.ProviderBinding{Kind: "channel", ProviderID: "desktop"},
		FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel"},
	})
	surface.completed["shell"] = true
	surface.completed["report"] = true
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-deliver", userText: "北京天气，生成pdf"}

	if !cb.RequiredEffectUnsettled() {
		t.Fatal("a delivery with no execution record must hold the turn open")
	}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "deliver"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "deliver", tool.PlanExecutionAwaitingReceipt, "", "selection_awaiting_receipt", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if got := cb.hopRunSnapshot().Remaining; got != 1 {
		t.Fatalf("awaiting receipt must stay visible as open work: remaining=%d", got)
	}
	if cb.RequiredEffectUnsettled() {
		t.Fatal("the host-owned delivery receipt is settled by this turn's own response projection, not by another model round")
	}
	if got := hopMissingRequiredEffects(cb.hopRunSnapshot()); len(got) != 0 {
		t.Fatalf("dispatched delivery still reported as a missing deliverable: %#v", got)
	}
}

// A specified-target file send on an IM channel is the same kind of dispatched
// work: the gateway sends the reply attachment after the loop returns, so no
// further model round can change its receipt.
func TestSpecifiedTargetAwaitingGatewayDoesNotBlockModelStop(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	surface.plan.Selections = append(surface.plan.Selections, tool.PlannedSelection{
		ID:       "send",
		NeedID:   "need:send",
		Provider: tool.ProviderBinding{Kind: "channel", ProviderID: "weixin"},
		FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.specified_target"},
	})
	surface.completed["shell"] = true
	surface.completed["report"] = true
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}

	if !cb.RequiredEffectUnsettled() {
		t.Fatal("a send with no execution record must hold the turn open")
	}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "send"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "send", tool.PlanExecutionAwaitingReceipt, "", "selection_awaiting_receipt", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if cb.RequiredEffectUnsettled() {
		t.Fatal("the gateway sends this reply attachment after the loop; another model round cannot settle it")
	}
	if got := hopMissingRequiredEffects(cb.hopRunSnapshot()); len(got) != 0 {
		t.Fatalf("dispatched send still reported as a missing deliverable: %#v", got)
	}
}

// The exception is deliberately narrow. A receipt that waits for a human
// decision still keeps the run open, because ending the turn would report the
// effect as done before anyone approved it.
func TestMailboxConfirmationAwaitingStillBlocksModelStop(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	surface.plan.Selections = append(surface.plan.Selections, tool.PlannedSelection{
		ID:       "send",
		NeedID:   "need:send",
		Provider: tool.ProviderBinding{Kind: "builtin", ProviderID: "im"},
		FitProof: tool.FitProof{MatchedCapability: tool.CapabilityMessageSendMailbox},
	})
	surface.completed["shell"] = true
	surface.completed["report"] = true
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}

	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "send"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "send", tool.PlanExecutionAwaitingReceipt, "", "mailbox_send_awaiting_confirmation", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if !cb.RequiredEffectUnsettled() {
		t.Fatal("a send waiting for user confirmation must still block the stop")
	}
	if got := hopMissingRequiredEffects(cb.hopRunSnapshot()); len(got) != 1 || got[0] != string(tool.CapabilityMessageSendMailbox) {
		t.Fatalf("confirmation is still owed as a deliverable: %#v", got)
	}
}

// The projection that attaches the payload is the channel adapter for the
// local conversation, so it records the delivery receipt exactly once.
func TestDesktopCurrentChannelDeliverySettlesAtResponseProjection(t *testing.T) {
	cb, deliveryName := weatherPDFDesktopReadyForHostDeliver(t)
	selectionID := cb.semanticSurface.grants[deliveryName].SelectionID
	if selectionID == "" {
		t.Fatalf("delivery grant lost its selection: %#v", cb.semanticSurface.grants[deliveryName])
	}
	got := cb.ExecuteToolCall(deliveryName, `{}`, "call-deliver").Result
	if !strings.Contains(got, "Delivery committed") {
		t.Fatalf("delivery result=%q", got)
	}
	record, err := cb.semanticSurface.executor.Execution(cb.semanticSurface.scope, selectionID)
	if err != nil || record.State != tool.PlanExecutionAwaitingReceipt {
		t.Fatalf("precondition: delivery execution=%#v err=%v", record, err)
	}

	resp := &IMAgentResponse{Text: "北京天气简报 已生成"}
	attachSharedLoopArtifacts(resp, cb)

	if resp.LocalFilePath == "" {
		t.Fatalf("local chat delivery did not materialize the file: %+v", resp)
	}
	record, err = cb.semanticSurface.executor.Execution(cb.semanticSurface.scope, selectionID)
	if err != nil || record.State != tool.PlanExecutionSucceeded {
		t.Fatalf("delivery receipt was not settled by the response projection: %#v err=%v", record, err)
	}
	if cb.RequiredEffectUnsettled() {
		t.Fatal("a settled delivery still reported unsettled work")
	}
	if remaining := cb.hopRunSnapshot().Remaining; remaining != 0 {
		t.Fatalf("settled run still reports remaining=%d", remaining)
	}
}

// The image variant is the same host-owned attach. A delivered screenshot must
// settle its receipt and must not keep a stale loop error beside the image.
func TestDesktopScreenshotDeliverySettlesAndClearsStaleError(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticTestClassifier(t)}
	if err := h.registry.Register(RegisteredTool{
		Name: "screenshot", Status: RegToolAvailable,
		InputSchema: map[string]interface{}{"display": map[string]interface{}{"type": "integer"}},
		CapabilityProvisions: []tool.CapabilityProvision{{
			Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Quality: 1,
		}},
		SemanticEffects:  []tool.EffectClass{tool.EffectReadOnly},
		SemanticProduces: []tool.ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}},
		Handler: func(map[string]interface{}) string {
			return "[screenshot_base64]" + testOnePixelPNGBase64
		},
	}); err != nil {
		t.Fatalf("register screenshot: %v", err)
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurn("user-shot", "capture the primary desktop screen", "desktop")
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("surface defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h, semanticSurface: surface, platform: "desktop", userID: "user-shot",
		loopCtx: &LoopContext{DeliveryTarget: hostOwnedCurrentChannelDeliveryTarget("desktop", "user-shot")},
	}
	if got := cb.ExecuteTool(extractToolName(defs[0]), `{}`); got != toolPayloadPreparedMessage {
		t.Fatalf("capture result = %q", got)
	}
	var deliveryName string
	for name, grant := range surface.grants {
		if grant.AdapterName == "semantic_deliver_current_image" {
			deliveryName = name
			break
		}
	}
	if deliveryName == "" {
		t.Fatalf("image delivery selection was not materialized: %#v", surface.grants)
	}
	selectionID := surface.grants[deliveryName].SelectionID
	if got := cb.ExecuteTool(deliveryName, `{}`); !strings.Contains(got, "Delivery committed") {
		t.Fatalf("delivery result = %q", got)
	}

	resp := &IMAgentResponse{ResponseSource: "shared_agent_loop", Error: "required_effect_unsettled"}
	attachSharedLoopArtifacts(resp, cb)

	if resp.ImageKey != testOnePixelPNGBase64 {
		t.Fatalf("projection lost the delivered image: %+v", resp)
	}
	if resp.Error != "" {
		t.Fatalf("stale loop error stayed beside a delivered image: %q", resp.Error)
	}
	record, err := surface.executor.Execution(surface.scope, selectionID)
	if err != nil || record.State != tool.PlanExecutionSucceeded {
		t.Fatalf("image delivery receipt was not settled by the projection: %#v err=%v", record, err)
	}
}

// End to end: after the model actually calls the current-chat attach, the turn
// is finished. The loop must end on that closing sentence instead of spending
// two more rounds and hard-exiting with required_effect_unsettled.
func TestDesktopCurrentChannelDeliveryEndsTheLoopCleanly(t *testing.T) {
	cb, searchName := weatherPDFDesktopBeforeSearch(t)
	if err := cb.syncSemanticToolSurface(); err != nil {
		t.Fatalf("sync initial semantic surface: %v", err)
	}

	request := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode model request: %v", err)
		}
		request++
		var response map[string]interface{}
		switch request {
		case 1:
			response = semanticLoopToolCallResponse("call-search", searchName, `{"query":"南京天气"}`)
		case 2:
			generateName, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "generate_pdf")
			if generateName == "" || grant.Token == "" {
				t.Fatalf("generate grant missing after search: %#v", cb.semanticSurface.grants)
			}
			response = semanticLoopToolCallResponse("call-generate", generateName, `{"content":"# 南京天气\n18C","title":"南京天气"}`)
		case 3:
			deliverName, grant := soleLiveSemanticGrantByAdapter(cb.semanticSurface, "semantic_deliver_current_file")
			if deliverName == "" || grant.Token == "" {
				t.Fatalf("current-chat attach grant missing after generate: %#v", cb.semanticSurface.grants)
			}
			response = semanticLoopToolCallResponse("call-deliver", deliverName, `{}`)
		case 4:
			response = map[string]interface{}{"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "南京天气简报已附上。"},
				"finish_reason": "stop",
			}}}
		default:
			t.Fatalf("delivery turn asked the model again after the attach (request=%d)", request)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("encode model response: %v", err)
		}
	}))
	defer server.Close()

	cb.llmCfg = corelib.MaclawLLMConfig{URL: server.URL, Model: "semantic-loop-test", Key: "test-key"}
	cb.maxIter = 5
	cb.systemPrompt = "test semantic loop"
	result := agent.RunLoop(cb, cb.userText, nil, server.Client(), cb)
	if result.Error != "" {
		t.Fatalf("a dispatched host-owned delivery must end cleanly, got %+v", result)
	}
	if request != 4 {
		t.Fatalf("model requests=%d, want 4", request)
	}
}

// Acceptance names the payload slot this record owns. A screenshot accepted on
// a document record would claim a delivery whose bytes never reached the chat.
func TestLocalChatDeliveryReceiptPairsKindWithPayload(t *testing.T) {
	cases := []struct {
		name       string
		resp       *IMAgentResponse
		kind       string
		materializ bool
		want       tool.DeliveryState
	}{
		{name: "document materialized", resp: &IMAgentResponse{LocalFilePath: "/tmp/a.pdf"}, kind: "document", materializ: true, want: tool.DeliveryAccepted},
		{name: "document staged only", resp: &IMAgentResponse{FileData: "JVBERi0=", FileName: "a.pdf"}, kind: "document", want: tool.DeliveryFailed},
		{name: "document path without materialize", resp: &IMAgentResponse{LocalFilePath: "/tmp/a.pdf"}, kind: "document", want: tool.DeliveryFailed},
		{name: "image carried", resp: &IMAgentResponse{ImageKey: "iVBORw0KGgo="}, kind: "image", want: tool.DeliveryAccepted},
		{name: "image record with file slot only", resp: &IMAgentResponse{LocalFilePath: "/tmp/a.pdf"}, kind: "image", materializ: true, want: tool.DeliveryFailed},
		{name: "audio carried", resp: &IMAgentResponse{VoiceData: "UklGRg=="}, kind: "audio", want: tool.DeliveryAccepted},
		{name: "audio record with image slot only", resp: &IMAgentResponse{ImageKey: "iVBORw0KGgo="}, kind: "audio", want: tool.DeliveryFailed},
		{name: "empty response", resp: &IMAgentResponse{}, kind: "document", want: tool.DeliveryFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, receipt := localChatDeliveryReceipt(tc.resp, tc.kind, tc.materializ)
			if outcome != tc.want {
				t.Fatalf("outcome=%s want %s", outcome, tc.want)
			}
			if (outcome == tool.DeliveryAccepted) != (receipt != "") {
				t.Fatalf("accepted=%v receipt=%q", outcome == tool.DeliveryAccepted, receipt)
			}
		})
	}
	if outcome, _ := localChatDeliveryReceipt(nil, "document", true); outcome != tool.DeliveryFailed {
		t.Fatalf("nil response outcome=%s", outcome)
	}
}

// A marker is incomplete work. Once the projection settled the effect the loop
// stopped for, the marker must retire with the same outcome; otherwise the next
// message recovers it as an interrupted external send and is refused.
func TestHostResolvedUnsettledExitRetiresRecoveryMarker(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.completed["shell"] = true
	surface.completed["report"] = true
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	result := agent.LoopResult{Error: "required_effect_unsettled", HardExit: true}

	if !sharedLoopTurnSettled(result, &IMAgentResponse{Text: "done"}, cb) {
		t.Fatal("a stale unsettled exit with no undispatched work must retire the marker")
	}
	if sharedLoopTurnSettled(result, &IMAgentResponse{Text: "done", Error: "cancelled"}, cb) {
		t.Fatal("a kept error must keep the marker")
	}
	if sharedLoopTurnSettled(agent.LoopResult{Error: "max iterations reached"}, &IMAgentResponse{}, cb) {
		t.Fatal("an unrelated unfinished exit must keep the marker")
	}
}

func currentChannelAttachToolEntry() agent.ConversationEntry {
	return agent.ConversationEntry{
		Role:       "tool",
		ToolName:   "send_file",
		ToolCallID: "call-attach",
		Content:    agent.CurrentChannelAttachResult("selection:chat"),
	}
}

// The host attached the file and cleared the model-stream error. That turn
// is settled. The crash marker has to leave with it, or the next message is
// refused as an uncertain external send. The loop error itself stays on the
// result the trajectory records.
func TestHostSettledCurrentChannelAttachRetiresStreamFailureMarker(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	const runID = "run-letter"
	if err := memory.PersistInFlightCheckpoint(userID, []agent.ConversationEntry{{Role: "user", Content: "写推荐信"}}, "分析", "", runID, agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "send_file", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	turn := []agent.ConversationEntry{
		{Role: "user", Content: "写推荐信"},
		currentChannelAttachToolEntry(),
	}
	streamErr := "LLM call failed: Responses API stream failed: payload_len=285"
	h := &IMMessageHandler{memory: memory}
	cb := &sharedAgentLoopCallbacks{checkpointCommitted: true}
	resp := &IMAgentResponse{LocalFilePath: `C:\letter.pdf`}
	result := agent.LoopResult{Error: streamErr, HardExit: true}
	if !sharedLoopHostSettledStaleModelError(result, resp, cb, turn) {
		t.Fatal("a cleared stream failure after the chat attach did not settle")
	}
	completeSharedInFlightIfSettled(h, cb, userID, runID, result, resp, turn, turn)
	if result.Error != streamErr {
		t.Fatalf("settlement rewrote the loop error: %q", result.Error)
	}
	if task, project := memory.ConsumeInFlightTask(userID); task != "" || project != "" {
		t.Fatalf("marker stayed task=%q project=%q", task, project)
	}
	if slot := h.recoverInterruptedTaskSlot(userID, turn); slot != nil {
		t.Fatalf("settled turn opened a recovery slot: %#v", slot)
	}
	if respHint, handled := h.maybeReturnUnfinishedSlotHint(IMUserMessage{UserID: userID, Platform: "desktop", Lang: "zh", Text: "补充研究所"}, "补充研究所", false, explicitTaskSlotDecision{}, nil); handled || respHint != nil {
		t.Fatalf("next message was refused: handled=%v resp=%#v", handled, respHint)
	}
}

func TestStaleModelErrorKeepsMarkerWhenTheSendIsStillExternal(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	const userID = "desktop-user"
	const runID = "run-im"
	if err := memory.PersistInFlightCheckpoint(userID, []agent.ConversationEntry{{Role: "user", Content: "发到微信"}}, "发到微信", "", runID, agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "send_to_im", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	turn := []agent.ConversationEntry{
		currentChannelAttachToolEntry(),
		{Role: "tool", ToolName: "send_to_im", Content: "sent"},
	}
	streamErr := "LLM call failed: Responses API stream failed: payload_len=285"
	h := &IMMessageHandler{memory: memory}
	cb := &sharedAgentLoopCallbacks{checkpointCommitted: true}
	resp := &IMAgentResponse{LocalFilePath: `C:\letter.pdf`}
	result := agent.LoopResult{Error: streamErr, HardExit: true}
	if sharedLoopHostSettledStaleModelError(result, resp, cb, turn) {
		t.Fatal("send_to_im was treated as a settled chat attach")
	}
	completeSharedInFlightIfSettled(h, cb, userID, runID, result, resp, turn, turn)
	slot := h.recoverInterruptedTaskSlot(userID, turn)
	if slot == nil || slot.LastToolName != "send_to_im" || slot.SideEffectState != "external_uncertain" {
		t.Fatalf("external send marker = %#v", slot)
	}
	hint, handled := h.maybeReturnUnfinishedSlotHint(IMUserMessage{UserID: userID, Platform: "desktop", Lang: "zh", Text: "补充研究所"}, "补充研究所", false, explicitTaskSlotDecision{}, slot)
	if !handled || hint == nil || hint.Error != "semantic_external_delivery_review_required" {
		t.Fatalf("handled=%v hint=%#v", handled, hint)
	}

	stillShowing := &IMAgentResponse{LocalFilePath: `C:\letter.pdf`, Error: streamErr}
	if sharedLoopHostSettledStaleModelError(result, stillShowing, cb, []agent.ConversationEntry{currentChannelAttachToolEntry()}) {
		t.Fatal("a response that still shows the stream error settled the marker")
	}
	if sharedLoopHostSettledStaleModelError(result, &IMAgentResponse{Text: "done"}, cb, []agent.ConversationEntry{currentChannelAttachToolEntry()}) {
		t.Fatal("a stream failure with no attached file settled the marker")
	}
	openShell, _ := newHopRunTestSurface(t)
	open := &sharedAgentLoopCallbacks{semanticSurface: openShell, checkpointCommitted: true}
	if !open.RequiredEffectUnsettled() {
		t.Fatal("open shell fixture was already settled")
	}
	if sharedLoopHostSettledStaleModelError(result, resp, open, []agent.ConversationEntry{currentChannelAttachToolEntry()}) {
		t.Fatal("an open required effect settled because a file was attached")
	}
	if sharedLoopHostSettledStaleModelError(agent.LoopResult{Error: agent.ErrNoProgress}, resp, cb, []agent.ConversationEntry{currentChannelAttachToolEntry()}) {
		t.Fatal("no_progress was reclassified as a stale model error")
	}
	pending := &sharedAgentLoopCallbacks{checkpointCommitted: true, hasPendingToolBatch: true}
	if sharedLoopHostSettledStaleModelError(result, resp, pending, []agent.ConversationEntry{currentChannelAttachToolEntry()}) {
		t.Fatal("a pending tool batch settled because a file was attached")
	}
	blocked := &sharedAgentLoopCallbacks{checkpointCommitted: true, semanticDurabilityBlocked: true}
	if sharedLoopHostSettledStaleModelError(result, resp, blocked, []agent.ConversationEntry{currentChannelAttachToolEntry()}) {
		t.Fatal("a durability failure settled because a file was attached")
	}
	if !sharedLoopTurnSettled(agent.LoopResult{Error: agent.ErrNoProgress}, &IMAgentResponse{}, cb) {
		t.Fatal("no_progress left the finished-work path")
	}
}

// A local skill can write the presentation without calling the office or
// current-channel adapters. The host then attaches that file and clears the
// visible required_effect_unsettled error. The plan has to accept the same
// file, or the recovery marker stays and the next message is an interrupted task.
func TestHostAttachedPresentationSettlesOfficePlan(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "office", NeedID: "need:document.write.office:deck", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
		{
			ID: "deliver", NeedID: "need:deliver",
			Provider: tool.ProviderBinding{Kind: "channel", ProviderID: "desktop"},
			FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel", QualifierBindings: map[string]string{"format": "file"}},
		},
		{
			ID: "acquire", NeedID: "need:artifact.acquire.remote:47dfffbc86c8",
			EvidenceIDs: []string{"intent:archetype_bundle"},
			FitProof:    tool.FitProof{MatchedCapability: tool.CapabilityArtifactAcquireRemote},
		},
	}
	surface.completed = map[string]bool{}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-ppt"}
	if !cb.RequiredEffectUnsettled() {
		t.Fatal("unexecuted office plan was already settled")
	}
	resp := &IMAgentResponse{
		LocalFilePath: `C:\Users\ma139\Desktop\作业\SDG1_Brazil_BolsaFamilia_final.pptx`,
		Error:         "required_effect_unsettled",
		HardExit:      true,
	}
	attachSharedLoopArtifacts(resp, cb)
	if resp.Error != "" || resp.HardExit {
		t.Fatalf("attached presentation kept the loop outcome error=%q hard_exit=%v", resp.Error, resp.HardExit)
	}
	if status, errMsg := classifyIMAgentResponseOutcome(resp); status != "success" || errMsg != "" {
		t.Fatalf("outcome status=%s err=%s", status, errMsg)
	}
	if !surface.completed["office"] || !surface.completed["deliver"] {
		t.Fatalf("file did not close office and delivery: %#v", surface.completed)
	}
	if surface.completed["acquire"] {
		t.Fatal("presentation closed an unrelated download")
	}
	if cb.RequiredEffectUnsettled() {
		t.Fatalf("plan still open after the file was accepted: %#v", cb.hopRunSnapshot())
	}
	result := agent.LoopResult{Error: "required_effect_unsettled", HardExit: true}
	if !sharedLoopTurnSettled(result, resp, cb) {
		t.Fatal("settled presentation left the recovery marker")
	}
}

// A Word file produced outside the office adapter discharges the same unstarted
// office need. The adapter itself only writes spreadsheets and presentations;
// the plan's office document includes .docx. A path-only office call must not
// have recorded a failure, or this attachment could not close the need.
func TestHostAttachedWordSettlesOfficePlan(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "office", NeedID: "need:document.write.office:resume", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
		{
			ID: "deliver", NeedID: "need:deliver",
			Provider: tool.ProviderBinding{Kind: "channel", ProviderID: "desktop"},
			FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel", QualifierBindings: map[string]string{"format": "file"}},
		},
	}
	surface.completed = map[string]bool{}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface, checkpointRunID: "run-docx"}
	resp := &IMAgentResponse{
		LocalFilePath: `F:\resume\resume-inference-engine.docx`,
		FileName:      "resume-inference-engine.docx",
		Error:         "required_effect_unsettled",
		HardExit:      true,
	}
	attachSharedLoopArtifacts(resp, cb)
	if resp.Error != "" || resp.HardExit {
		t.Fatalf("attached word file kept the loop outcome error=%q hard_exit=%v", resp.Error, resp.HardExit)
	}
	if !surface.completed["office"] || !surface.completed["deliver"] {
		t.Fatalf("docx did not close office and delivery: %#v", surface.completed)
	}
	if cb.RequiredEffectUnsettled() {
		t.Fatalf("plan still open after the word file was accepted: %#v", cb.hopRunSnapshot())
	}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "将该文件转换为word 版本"},
		{Role: "assistant", Content: "已生成 Word。"},
	}
	result := agent.LoopResult{Error: "required_effect_unsettled", HardExit: true}
	if noteHostSettledRequiredEffect(history, 0, &result, resp, nil) {
		t.Fatal("discharged word turn asked to record an incomplete class")
	}
	if result.Error != "" || result.HardExit || history[1].TerminalClass != "" {
		t.Fatalf("result=%+v class=%q", result, history[1].TerminalClass)
	}
	final := finalizeHopRun(cb.hopRunSnapshot(), result)
	if final.Status != "completed" || final.Remaining != 0 {
		t.Fatalf("hop run after the word file: status=%s remaining=%d", final.Status, final.Remaining)
	}
	if !sharedLoopTurnSettled(result, resp, cb) {
		t.Fatal("revised loop result left the recovery marker")
	}
}

func TestSharedLoopSchedulesDegradedRecovery(t *testing.T) {
	if !sharedLoopSchedulesDegradedRecovery("", false, "") {
		t.Fatal("a clean finish may recover a degraded turn")
	}
	if sharedLoopSchedulesDegradedRecovery("", false, "required_effect_unsettled") {
		t.Fatal("a discharged required effect must not start another recovery")
	}
	if sharedLoopSchedulesDegradedRecovery("required_effect_unsettled", false, "required_effect_unsettled") {
		t.Fatal("an open required effect is not a clean finish")
	}
	if sharedLoopSchedulesDegradedRecovery("", true, "") {
		t.Fatal("a handoff is not a degraded recovery")
	}
}

func TestHostOpenRequiredEffectKeepsIncompleteClass(t *testing.T) {
	history := []agent.ConversationEntry{{Role: "assistant", Content: "still working"}}
	result := agent.LoopResult{Error: "required_effect_unsettled", HardExit: true}
	resp := &IMAgentResponse{Error: "required_effect_unsettled", HardExit: true}
	if !noteHostSettledRequiredEffect(history, 0, &result, resp, nil) {
		t.Fatal("an open obligation was not stamped")
	}
	if result.Error != "required_effect_unsettled" || !result.HardExit || !resp.HardExit {
		t.Fatalf("open result=%+v resp=%+v", result, resp)
	}
	if history[0].TerminalClass != "required_effect_unsettled" {
		t.Fatalf("class=%q", history[0].TerminalClass)
	}
	if status, _ := classifyIMAgentResponseOutcome(resp); status != "hard_exit" {
		t.Fatalf("open outcome status=%s", status)
	}
}

func TestHostSettledRequiredEffectRewritesTrajectory(t *testing.T) {
	rec := NewTrajectoryRecorderForBaseDir(t.TempDir())
	rec.StartSession("s", "p", "m", "openai", "u", "desktop", nil)
	rec.RecordLoopResult(agent.LoopResult{
		Error:    "required_effect_unsettled",
		HardExit: true,
		HistoryDelta: []agent.ConversationEntry{
			{Role: "user", Content: "将该文件转换为word 版本"},
			{Role: "assistant", Content: "已生成 Word。"},
		},
	})
	history := []agent.ConversationEntry{{Role: "assistant", Content: "已生成 Word。"}}
	result := agent.LoopResult{Error: "required_effect_unsettled", HardExit: true}
	resp := &IMAgentResponse{HardExit: true, LocalFilePath: `F:\resume\resume-inference-engine.docx`}
	if noteHostSettledRequiredEffect(history, 0, &result, resp, rec) {
		t.Fatal("discharged turn asked to stamp an incomplete class")
	}
	rec.mu.Lock()
	status, errMsg := rec.session.Status, rec.session.Error
	rec.mu.Unlock()
	if status != "success" || errMsg != "" || result.Error != "" || result.HardExit {
		t.Fatalf("trajectory status=%q err=%q result=%+v", status, errMsg, result)
	}
}

func TestHostAttachedPresentationDoesNotSettleOtherEffects(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "office", NeedID: "need:document.write.office:deck", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
		{ID: "pdf", NeedID: "need:document.generate.file:report", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}},
		{ID: "shell", NeedID: "need:shell", FitProof: tool.FitProof{MatchedCapability: "shell.execute.remote_host"}},
		{
			ID: "pdf-deliver", NeedID: "need:pdf-deliver",
			Provider: tool.ProviderBinding{Kind: "channel", ProviderID: "desktop"},
			FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel", QualifierBindings: map[string]string{"format": "pdf"}},
		},
	}
	surface.completed = map[string]bool{}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	resp := &IMAgentResponse{LocalFilePath: `C:\work\deck.pptx`}
	cb.settleHostProducedPlanEffects(resp)
	if !surface.completed["office"] {
		t.Fatal("pptx did not close office write")
	}
	if surface.completed["pdf"] || surface.completed["pdf-deliver"] || surface.completed["shell"] {
		t.Fatalf("pptx closed the wrong selections: %#v", surface.completed)
	}
	if !cb.RequiredEffectUnsettled() {
		t.Fatal("remote shell was treated as done because a presentation existed")
	}
	kept := &IMAgentResponse{LocalFilePath: `C:\work\deck.pptx`, Error: "cancelled"}
	cb.settleHostProducedPlanEffects(kept)
	if surface.completed["pdf"] {
		t.Fatal("a kept failure accepted the presentation as a PDF")
	}
}

func TestHostAttachedFileLeavesRecordedFailureAlone(t *testing.T) {
	surface, store := newHopRunTestSurface(t)
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "office", NeedID: "need:document.write.office:deck", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
		{
			ID: "deliver", NeedID: "need:deliver",
			Provider: tool.ProviderBinding{Kind: "channel", ProviderID: "desktop"},
			FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel", QualifierBindings: map[string]string{"format": "file"}},
		},
		{ID: "pdf", NeedID: "need:document.generate.file:report", FitProof: tool.FitProof{MatchedCapability: "document.generate.file"}},
	}
	surface.completed = map[string]bool{}
	if _, _, err := store.Acquire(tool.PlanExecutionRecord{Scope: surface.scope, SelectionID: "office"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Complete(surface.scope, "office", tool.PlanExecutionFailed, "", "office_write_failed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	resp := &IMAgentResponse{LocalFilePath: `C:\work\deck.pptx`, FileName: "other.pdf"}
	cb.settleHostProducedPlanEffects(resp)
	if surface.completed["office"] || surface.completed["pdf"] {
		t.Fatalf("a recorded failure or a foreign file name was accepted: %#v", surface.completed)
	}
	if !surface.completed["deliver"] {
		t.Fatal("the attached presentation did not close the unstarted file delivery")
	}

	notesSurface, _ := newHopRunTestSurface(t)
	notesSurface.plan.Selections = surface.plan.Selections
	notesSurface.completed = map[string]bool{}
	notes := &sharedAgentLoopCallbacks{semanticSurface: notesSurface}
	notes.settleHostProducedPlanEffects(&IMAgentResponse{LocalFilePath: `C:\work\演讲稿.txt`})
	if len(notesSurface.completed) != 0 {
		t.Fatalf("a notes file closed the document plan: %#v", notesSurface.completed)
	}
}

func TestHostAttachedFileLeavesCachedUnknownAlone(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "office", NeedID: "need:document.write.office:deck", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
	}
	surface.completed = map[string]bool{}
	surface.hopEvidence = map[string]HopStepEvidence{
		hopEvidenceKey(surface, "office"): {State: tool.PlanExecutionUnknown, ReasonCode: "selection_execution_unknown"},
	}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	cb.settleHostProducedPlanEffects(&IMAgentResponse{LocalFilePath: `C:\work\deck.pptx`})
	if surface.completed["office"] {
		t.Fatal("an unknown office attempt was marked done because a presentation was attached")
	}
}

// A presentation discharges the office write it matches. It does not make the
// rest of the plan finished, and the visible error stays until nothing
// required is left. A file name with no bytes is not that presentation.
func TestHostAttachedFileKeepsUnsettledWorkVisible(t *testing.T) {
	surface, _ := newHopRunTestSurface(t)
	surface.plan.Selections = []tool.PlannedSelection{
		{ID: "office", NeedID: "need:document.write.office:deck", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
		{ID: "shell", NeedID: "need:shell", FitProof: tool.FitProof{MatchedCapability: "shell.execute.remote_host"}},
	}
	surface.completed = map[string]bool{}
	cb := &sharedAgentLoopCallbacks{semanticSurface: surface}
	resp := &IMAgentResponse{
		LocalFilePath: `C:\work\deck.pptx`,
		FileName:      "deck.pptx",
		Error:         "required_effect_unsettled",
	}
	attachSharedLoopArtifacts(resp, cb)
	if !surface.completed["office"] || surface.completed["shell"] {
		t.Fatalf("presentation settled the wrong steps: %#v", surface.completed)
	}
	if resp.Error != "required_effect_unsettled" {
		t.Fatalf("error = %q, want the remaining shell to stay visible", resp.Error)
	}
	result := agent.LoopResult{Error: "required_effect_unsettled", HardExit: true}
	if sharedLoopTurnSettled(result, resp, cb) {
		t.Fatal("an open shell retired the recovery marker")
	}

	nameOnly, _ := newHopRunTestSurface(t)
	nameOnly.plan.Selections = []tool.PlannedSelection{
		{ID: "office", NeedID: "need:document.write.office:deck", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
	}
	nameOnly.completed = map[string]bool{}
	named := &sharedAgentLoopCallbacks{semanticSurface: nameOnly}
	named.settleHostProducedPlanEffects(&IMAgentResponse{FileName: "deck.pptx", Error: "required_effect_unsettled"})
	if nameOnly.completed["office"] {
		t.Fatal("a file name without bytes closed the office write")
	}

	// The path list is the attachment even when the single-path field was not
	// copied. Settlement and the visible error have to follow that same list.
	pathsOnly, _ := newHopRunTestSurface(t)
	pathsOnly.plan.Selections = []tool.PlannedSelection{
		{ID: "office", NeedID: "need:document.write.office:deck", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
		{
			ID: "deliver", NeedID: "need:deliver",
			Provider: tool.ProviderBinding{Kind: "channel", ProviderID: "desktop"},
			FitProof: tool.FitProof{MatchedCapability: "artifact.deliver.current_channel", QualifierBindings: map[string]string{"format": "file"}},
		},
	}
	pathsOnly.completed = map[string]bool{}
	paths := &sharedAgentLoopCallbacks{semanticSurface: pathsOnly}
	resp = &IMAgentResponse{
		LocalFilePaths: []string{`C:\work\notes.txt`, `C:\work\deck.pptx`},
		Error:          "required_effect_unsettled",
	}
	attachSharedLoopArtifacts(resp, paths)
	if !pathsOnly.completed["office"] || !pathsOnly.completed["deliver"] {
		t.Fatalf("path list did not close office and delivery: %#v", pathsOnly.completed)
	}
	if resp.Error != "" {
		t.Fatalf("settled path list kept the loop error %q", resp.Error)
	}
	if !sharedLoopTurnSettled(result, resp, paths) {
		t.Fatal("settled path list left the recovery marker")
	}
}

func TestUnexecutedArgumentRejectionIsNotAnExecutionOutcome(t *testing.T) {
	got := semanticUnexecutedRejection(fmt.Errorf("trusted_shell_command_required"))
	if !strings.Contains(got, "trusted_shell_command_required") || !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) {
		t.Fatalf("pre-effect refusal = %q", got)
	}
	mentioned := semanticUnexecutedRejection(fmt.Errorf("trusted_shell_command_required: notes mention arguments_rejected"))
	if !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: mentioned}) || !strings.HasPrefix(mentioned, "[system rejected] arguments_rejected:") {
		t.Fatalf("a detail mention hid the refusal code: %q", mentioned)
	}
	if tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: "[system rejected] trusted_office_write_failed: disk full"}) {
		t.Fatal("a write that ran and failed must stay a recorded outcome")
	}
	if tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Succeeded: true, Result: got}) {
		t.Fatal("a successful result must not be retracted")
	}
	model := semanticUnexecutedModelRejection(got)
	if !strings.Contains(model, "trusted_shell_command_required") || !strings.Contains(model, "tool remains available") {
		t.Fatalf("retracted refusal = %q", model)
	}
	again := semanticUnexecutedModelRejection(model)
	if strings.Count(again, "tool remains available") != 1 {
		t.Fatalf("guidance repeated: %q", again)
	}
	incidental := semanticUnexecutedModelRejection("[system rejected] arguments_rejected: the jail said the path was refused before execution")
	if strings.Count(incidental, "tool remains available") != 1 {
		t.Fatalf("incidental wording hid availability: %q", incidental)
	}
	quoted := "[system rejected] trusted_file_write_failed: arguments_rejected was in the file"
	if tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: quoted}) {
		t.Fatal("a write failure that quotes the refusal code must stay an outcome")
	}
}
