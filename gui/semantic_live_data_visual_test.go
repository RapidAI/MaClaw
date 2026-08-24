package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func liveDataVisualClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual}, Confidence: .98}
}

func TestLiveDataVisualPlansClosedArtifactPipeline(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Beijing weather: clear, 28C", nil }
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "生成一张北京天气实况图", "desktop", "root-live-visual", "turn-live-visual", liveDataVisualClassification(), nil,
	)
	if err != nil || !handled || surface == nil || len(defs) != 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	if !planHasCapabilities(surface.plan, "information.search.web", "visual.render.live_data", "artifact.deliver.current_channel") {
		t.Fatalf("plan=%#v", surface.plan.Selections)
	}
	searchName := extractToolName(defs[0])
	if surface.grants[searchName].AdapterName != semanticTrustedWebSearchAdapter {
		t.Fatalf("initial grants=%#v", surface.grants)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop", userText: "生成一张北京天气实况图", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(searchName, `{"query":"北京天气"}`); strings.Contains(got, "[system rejected]") {
		t.Fatalf("search=%q", got)
	}
	renderName, renderGrant := soleLiveSemanticGrantByAdapter(surface, semanticTrustedLiveDataVisualAdapter)
	if renderName == "" || renderGrant.Token == "" {
		t.Fatalf("renderer not unlocked: %#v", surface.grants)
	}
	if got := cb.ExecuteTool(renderName, `{}`); !strings.Contains(got, "PNG artifact published") {
		t.Fatalf("render=%q", got)
	}
	deliverName, deliverGrant := soleLiveSemanticGrantByAdapter(surface, "semantic_deliver_current_image")
	if deliverName == "" || !currentChannelImageDeliveryReady(surface, deliverGrant) {
		t.Fatalf("delivery not unlocked: grants=%#v", surface.grants)
	}
	if got := cb.ExecuteTool(deliverName, `{}`); !strings.Contains(got, "prepared for delivery") {
		t.Fatalf("deliver=%q", got)
	}
	if cb.semanticDeliveryImageKey == "" {
		t.Fatal("image delivery did not receive the producer ArtifactRef")
	}
}

func TestLiveDataVisualHostClosesModelStopGap(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Beijing weather: clear, 28C", nil }
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "生成一张北京天气实况图", "desktop", "root-live-visual-auto", "turn-live-visual-auto", liveDataVisualClassification(), nil,
	)
	if err != nil || !handled || surface == nil || len(defs) != 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop", userText: "生成一张北京天气实况图", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(extractToolName(defs[0]), `{"query":"北京天气"}`); strings.Contains(got, "[system rejected]") {
		t.Fatalf("search=%q", got)
	}
	resp := &IMAgentResponse{Text: "已查询到北京天气数据"}
	attachSharedLoopArtifacts(resp, cb)
	if resp.ImageKey == "" || resp.SemanticDelivery == nil {
		t.Fatalf("host did not render and deliver requested image: %+v", resp)
	}
}

func TestLiveDataVisualRendererRejectsUntrustedEvidence(t *testing.T) {
	if _, err := renderTrustedLiveDataVisual("生成天气图", "[file_base64|x|application/pdf]AAAA"); err == nil {
		t.Fatal("untrusted evidence rendered into an image")
	}
	if _, err := renderTrustedLiveDataVisual("生成天气图", "北京天气：晴，28℃"); err != nil {
		t.Fatalf("trusted evidence failed to render: %v", err)
	}
}

func TestLiveDataVisualTitleExcludesRenderingInstruction(t *testing.T) {
	if got := liveDataVisualTitle("生成一张北京天气实况图"); got != "生成一张北京天气" {
		t.Fatalf("title=%q", got)
	}
	if got := liveDataVisualTitle(" "); got != "实时数据" {
		t.Fatalf("empty title=%q", got)
	}
}
