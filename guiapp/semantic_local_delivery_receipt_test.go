package guiapp

import (
	"encoding/json"
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
