package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func liveDataVisualClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual}, Confidence: .98}
}

func TestRebuiltWeatherCardDoesNotGrantScreenshot(t *testing.T) {
	rebuilt := classificationFromGrantedNeeds([]tool.CapabilityNeed{
		{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}},
		{Capability: "visual.render.live_data"},
		{Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}},
	})
	if rebuilt.HasLabel(intent.LabelScreenshot) || !rebuilt.HasLabel(intent.LabelLiveDataVisual) {
		t.Fatalf("weather card rebuilt as %+v", rebuilt)
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Chongzhou weather: cloudy", nil }
	registerBuiltinTools(h.registry, h)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "崇州天气", "desktop", "root-chongzhou", "turn-chongzhou", &rebuilt,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("handled=%v surface=%#v err=%v", handled, surface, err)
	}
	if semanticGrantNameForAdapter(surface, "screenshot") != "" {
		t.Fatalf("weather card granted screenshot: %#v", surface.grants)
	}
	if semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter) != "web_search" {
		t.Fatalf("weather card missing web_search: %#v", surface.grants)
	}
}

func TestLookupBudgetKeepsOneSearchWhenLiveDataAlsoCarriesSearch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "tofu fat stays in the curd", nil }
	registerBuiltinTools(h.registry, h)
	classification := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelSearch},
		Confidence: 0.85,
	}
	ctx := withSemanticPlanningBudget(context.Background(), 1)
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, "user-1", "所以，多的脂肪去哪了？", "desktop", "root-fat", "turn-fat", classification, nil,
	)
	if err != nil || !handled || surface == nil || len(surface.plan.Selections) == 0 {
		t.Fatalf("handled=%v selections=%d unmet=%v err=%v", handled, selectionCount(surface), plannedUnmet(surface), err)
	}
	families := map[string]struct{}{}
	for _, selection := range surface.plan.Selections {
		if !strings.Contains(selection.NeedID, "information.search.web") {
			continue
		}
		families[tool.RepeatFamilyID(selection.NeedID)] = struct{}{}
	}
	if len(families) != 1 {
		t.Fatalf("search families=%d, want 1; selections=%#v", len(families), surface.plan.Selections)
	}
	for _, item := range surface.plan.Unmet {
		if item.ReasonCode == "planning_budget_exceeded" {
			t.Fatalf("first wave still rejected: %#v", surface.plan.Unmet)
		}
	}
}

func selectionCount(surface *semanticCallSurface) int {
	if surface == nil {
		return 0
	}
	return len(surface.plan.Selections)
}

func TestLiveVisualPlannerBudgetOfOneRejectsTheCard(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Beijing weather: clear", nil }
	ctx := withSemanticPlanningBudget(context.Background(), 1)
	_, surface, _, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		ctx, "user-1", "北京天气", "desktop", "root-budget-1", "turn-budget-1", liveDataVisualClassification(), nil,
	)
	if err == nil && surface != nil && len(surface.plan.Unmet) == 0 && planHasCapabilities(surface.plan, "visual.render.live_data", "artifact.deliver.current_channel") {
		t.Fatal("planning budget 1 must not look like a complete weather card")
	}
	profile := classifyIMExecutionProfileWithSemantic(IMUserMessage{Text: "北京天气"}, false, false, liveDataVisualClassification())
	open := withSemanticPlanningBudget(context.Background(), profile.ToolBudget)
	_, planned, plannedHandled, plannedErr := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		open, "user-1", "北京天气", "desktop", "root-budget-open", "turn-budget-open", liveDataVisualClassification(), nil,
	)
	if plannedErr != nil || !plannedHandled || planned == nil || len(planned.plan.Unmet) != 0 {
		t.Fatalf("profile budget must keep the card handled=%v unmet=%v err=%v", plannedHandled, plannedUnmet(planned), plannedErr)
	}
	if !planHasCapabilities(planned.plan, "information.search.web", "visual.render.live_data", "artifact.deliver.current_channel") {
		t.Fatalf("plan=%#v", planned.plan.Selections)
	}
}

func plannedUnmet(surface *semanticCallSurface) []tool.UnmetNeed {
	if surface == nil {
		return nil
	}
	return surface.plan.Unmet
}

func TestLiveDataVisualPlansClosedArtifactPipeline(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Beijing weather: clear, 28C", nil }
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "生成一张北京天气实况图", "desktop", "root-live-visual", "turn-live-visual", liveDataVisualClassification(), nil,
	)
	// The first face is web_search. web_fetch stays latent, and the renderer
	// stays in the plan until search completes. Baseline workspace tools are
	// not part of this face.
	baseline := semanticBaselineGrantNames(surface)
	face := 0
	for _, def := range defs {
		if !baseline[extractToolName(def)] {
			face++
		}
	}
	if err != nil || !handled || surface == nil || face != 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	if semanticGrantNameForAdapter(surface, "screenshot") != "" {
		t.Fatalf("weather card listed screenshot: %#v", surface.grants)
	}
	if !planHasCapabilities(surface.plan, "information.search.web", "visual.render.live_data", "artifact.deliver.current_channel") {
		t.Fatalf("plan=%#v", surface.plan.Selections)
	}
	searchName := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	if searchName != "web_search" {
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
	if got := cb.ExecuteTool(deliverName, `{}`); !strings.Contains(got, "Delivery committed") {
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
	baseline := semanticBaselineGrantNames(surface)
	face := 0
	for _, def := range defs {
		if !baseline[extractToolName(def)] {
			face++
		}
	}
	if err != nil || !handled || surface == nil || face != 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	searchName := semanticGrantNameForAdapter(surface, semanticTrustedWebSearchAdapter)
	if searchName == "" {
		t.Fatalf("search grant missing: defs=%#v", defs)
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop", userText: "生成一张北京天气实况图", loopCtx: &LoopContext{DeliveryTarget: &agent.DeliveryTarget{ChannelScope: "desktop", DestinationID: "user:user-1"}}}
	if got := cb.ExecuteTool(searchName, `{"query":"北京天气"}`); strings.Contains(got, "[system rejected]") {
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
