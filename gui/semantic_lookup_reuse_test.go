package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func pengzhouWeatherPDFText() string {
	return "\u5f6d\u5dde\u5929\u6c14\uff0c\u751f\u6210pdf\u7248\u672c\u62a5\u544a"
}

func nanjingWeatherPDFText() string {
	return "\u5357\u4eac\u5929\u6c14\uff0c\u751f\u6210pdf\u62a5\u544a"
}

func shanghaiWeatherPDFText() string {
	return "\u4e0a\u6d77\u5929\u6c14\uff0c\u751f\u6210pdf\u62a5\u544a"
}

func pengzhouRefreshWeatherPDFText() string {
	return "\u91cd\u65b0\u67e5\u5f6d\u5dde\u5929\u6c14\uff0c\u751f\u6210pdf\u62a5\u544a"
}

func pengzhouWeatherReport() string {
	return "\u5f6d\u5dde\u4eca\u65e5\u591a\u4e91\u8f6c\u5c0f\u96e8\uff0c\u6c14\u6e2922\u523029\u6444\u6c0f\u5ea6\uff0c\u4e1c\u5357\u98ce3\u52304\u7ea7\uff0c\u7a7a\u6c14\u8d28\u91cf\u826f\u597d\u3002\u5348\u540e\u5916\u51fa\u5efa\u8bae\u643a\u5e26\u96e8\u5177\uff0c\u8def\u9762\u53ef\u80fd\u6e7f\u6ed1\u3002\u591c\u95f4\u6c14\u6e29\u4e0b\u964d\u8f83\u660e\u663e\uff0c\u8bf7\u9002\u5f53\u6dfb\u8863\u3002\u660e\u5929\u767d\u5929\u4ecd\u6709\u5206\u6563\u9635\u96e8\u3002"
}

func sameTopicPengzhouHistory() []agent.ConversationEntry {
	return []agent.ConversationEntry{
		{Role: "user", Content: "\u5f6d\u5dde\u5929\u6c14\uff0c\u751f\u6210pdf"},
		{Role: "tool", ToolName: "web_search", Content: "Pengzhou weather: cloudy, 26C, light rain in the afternoon."},
		{Role: "assistant", Content: pengzhouWeatherReport()},
	}
}

func TestLookupTopicKeyStripsGenerateAffordance(t *testing.T) {
	if got := lookupTopicKey(pengzhouWeatherPDFText()); got != "\u5f6d\u5dde" {
		t.Fatalf("topic=%q", got)
	}
	if got := lookupTopicKey(nanjingWeatherPDFText()); got != "\u5357\u4eac" {
		t.Fatalf("nanjing topic=%q", got)
	}
}

func TestConversationReusesSameTopicSearchFacts(t *testing.T) {
	if !conversationHasReusableLookupFacts(sameTopicPengzhouHistory(), pengzhouWeatherPDFText()) {
		t.Fatal("same-topic prior search must satisfy generate facts")
	}
}

func TestConversationDoesNotReuseDifferentCity(t *testing.T) {
	if conversationHasReusableLookupFacts(sameTopicPengzhouHistory(), shanghaiWeatherPDFText()) {
		t.Fatal("Shanghai must not reuse Pengzhou search facts")
	}
}

func TestConversationDoesNotReuseWhenUserAsksRefresh(t *testing.T) {
	if conversationHasReusableLookupFacts(sameTopicPengzhouHistory(), pengzhouRefreshWeatherPDFText()) {
		t.Fatal("explicit refresh must keep this-turn lookup")
	}
}

func TestConversationReusesSameTopicAssistantReport(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "\u5f6d\u5dde\u5929\u6c14\uff0c\u751f\u6210pdf"},
		{Role: "assistant", Content: pengzhouWeatherReport()},
	}
	if !conversationHasReusableLookupFacts(history, pengzhouWeatherPDFText()) {
		t.Fatal("same-topic assistant report must satisfy generate facts")
	}
}

func TestSemanticNeedsDropLookupWhenConversationHasFacts(t *testing.T) {
	needs := []tool.CapabilityNeed{
		{ID: "search", Capability: "information.search.web", Required: true},
		{ID: "generate", Capability: "document.generate.file", Required: true},
		{ID: "deliver", Capability: "artifact.deliver.current_channel", Required: true},
	}
	ctx := withSemanticConversationHistory(context.Background(), sameTopicPengzhouHistory())
	got := semanticNeedsForReusableConversationLookup(needs, ctx, pengzhouWeatherPDFText())
	if semanticNeedsHaveLookup(got) || !semanticNeedsHaveGenerate(got) || len(got) != 2 {
		t.Fatalf("reusable facts must drop only lookup: %#v", got)
	}
}

func TestSemanticNeedsKeepLookupWithoutHistory(t *testing.T) {
	needs := []tool.CapabilityNeed{
		{ID: "search", Capability: "information.search.web", Required: true},
		{ID: "generate", Capability: "document.generate.file", Required: true},
	}
	got := semanticNeedsForReusableConversationLookup(needs, context.Background(), pengzhouWeatherPDFText())
	if !semanticNeedsHaveLookup(got) || !semanticNeedsHaveGenerate(got) {
		t.Fatalf("cold turn must keep lookup: %#v", got)
	}
}

func TestSemanticNeedsKeepLookupForLiveDataVisual(t *testing.T) {
	needs := []tool.CapabilityNeed{
		{ID: "search", Capability: "information.search.web", Required: true},
		{ID: "generate", Capability: "document.generate.file", Required: true},
		{ID: "render", Capability: "visual.render.live_data", Required: true},
	}
	ctx := withSemanticConversationHistory(context.Background(), sameTopicPengzhouHistory())
	got := semanticNeedsForReusableConversationLookup(needs, ctx, pengzhouWeatherPDFText()+"，生成天气实况图")
	if !semanticNeedsHaveLookup(got) {
		t.Fatalf("live-data visual must retain current lookup: %#v", got)
	}
}

func TestIMSemanticRepeatWeatherPDFOmitsLookupNeed(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	ctx := withSemanticConversationHistory(context.Background(), sameTopicPengzhouHistory())
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", pengzhouWeatherPDFText(), "desktop", "root-reuse", "turn-reuse", liveDataGenerateClassification(), nil,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("repeat weather+pdf must plan generate, handled=%v err=%v", handled, err)
	}
	if planHasCapabilities(prepared.plan, "information.search.web") {
		t.Fatalf("same-topic facts must not mint another search: %#v", prepared.plan.Selections)
	}
	if !planHasCapabilities(prepared.plan, "document.generate.file", "artifact.deliver.current_channel") {
		t.Fatalf("selections=%#v", prepared.plan.Selections)
	}
	for _, selection := range prepared.plan.Selections {
		if selection.FitProof.MatchedCapability != "document.generate.file" {
			continue
		}
		for _, requirement := range selection.Requires {
			if strings.Contains(requirement, "information.search") || strings.Contains(requirement, "selection:") && strings.Contains(requirement, "search") {
				t.Fatalf("generate still depends on lookup: %#v", selection.Requires)
			}
		}
	}
}

func TestIMSemanticRefreshKeepsLookupNeed(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	ctx := withSemanticConversationHistory(context.Background(), sameTopicPengzhouHistory())
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", pengzhouRefreshWeatherPDFText(), "desktop", "root-refresh", "turn-refresh", liveDataGenerateClassification(), nil,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("refresh must still plan, handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file") {
		t.Fatalf("explicit refresh must keep lookup: %#v", prepared.plan.Selections)
	}
}

func TestIMSemanticDifferentCityKeepsLookupNeed(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	ctx := withSemanticConversationHistory(context.Background(), sameTopicPengzhouHistory())
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", shanghaiWeatherPDFText(), "desktop", "root-city", "turn-city", liveDataGenerateClassification(), nil,
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("new city must still plan, handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, "information.search.web", "document.generate.file") {
		t.Fatalf("new city must keep lookup: %#v", prepared.plan.Selections)
	}
}

func TestIMSemanticRepeatWeatherPDFIssuesGenerateFirst(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	h.app = app
	loopCtx := h.prepareIMLoopContext(nil, IMUserMessage{
		UserID: "user-1", Platform: "desktop", Text: pengzhouWeatherPDFText(),
	}, nil, false, false)
	loopCtx.History = sameTopicPengzhouHistory()
	requestCtx, cancel := semanticRoutingContext(loopCtx)
	t.Cleanup(cancel)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachments(
		requestCtx, "user-1", pengzhouWeatherPDFText(), "desktop", "root-reuse-surface", "turn-reuse-surface", liveDataGenerateClassification(), nil,
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	if planHasCapabilities(surface.plan, "information.search.web") {
		t.Fatalf("surface must omit lookup: %#v", surface.plan.Selections)
	}
	name, grant := soleLiveSemanticGrantByAdapter(surface, "generate_pdf")
	if name == "" || grant.Token == "" {
		t.Fatalf("first request must issue generate_pdf: defs=%#v grants=%#v", defs, surface.grants)
	}
}
