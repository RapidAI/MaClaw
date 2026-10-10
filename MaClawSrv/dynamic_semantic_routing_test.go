package main

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestSrvClassifierTimeoutDoesNotFailTheTurn(t *testing.T) {
	c := newSrvPrincipalIntentClassifier(nil)
	c.tree = func(context.Context, context.Context, agentservice.Principal, string, string) (string, error) {
		return "", context.DeadlineExceeded
	}
	result, err := c.ClassifyDynamicIntent(context.Background(), agentservice.Principal{TenantID: "t", UserID: "u"}, "北京天气")
	if err != nil {
		t.Fatalf("classifier timeout must stay a result so the turn keeps tools: %v", err)
	}
	if !result.Degraded || result.Primary == intent.LabelLiveData {
		t.Fatalf("timeout result = %+v, want a degraded non-grant", result)
	}
}

func TestSrvLookupHintClearsResolverFloor(t *testing.T) {
	c := newSrvPrincipalIntentClassifier(nil)
	c.tree = func(context.Context, context.Context, agentservice.Principal, string, string) (string, error) {
		return `{"top":[{"skill":"live_data","score":0.72}]}`, nil
	}
	result, err := c.ClassifyDynamicIntent(context.Background(), agentservice.Principal{TenantID: "t", UserID: "u"}, "北京天气")
	if err != nil {
		t.Fatal(err)
	}
	if result.Primary != intent.LabelLiveData || result.Confidence < agentservice.ReviewedIntentMinimumConfidence || result.Degraded {
		t.Fatalf("lookup hint = %+v, want live_data at the resolver floor", result)
	}

	coding := newSrvPrincipalIntentClassifier(nil)
	coding.tree = func(context.Context, context.Context, agentservice.Principal, string, string) (string, error) {
		return `{"top":[{"skill":"coding","score":0.72}]}`, nil
	}
	kept, err := coding.ClassifyDynamicIntent(context.Background(), agentservice.Principal{TenantID: "t", UserID: "u"}, "改一下这个函数")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Primary != intent.LabelCoding || kept.Confidence != 0.72 {
		t.Fatalf("non-lookup = %+v, confidence must stay 0.72", kept)
	}
}

func TestSrvDegradedLookupStillPlansWebSearch(t *testing.T) {
	got := projectSrvReadOnlyLookupForPlanning(intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.72, Layer: 2, Degraded: true,
	})
	if got.Degraded || got.Primary != intent.LabelLiveData || got.Confidence < agentservice.ReviewedIntentMinimumConfidence {
		t.Fatalf("degraded lookup = %+v, want a governed live_data hint", got)
	}
	weak := projectSrvReadOnlyLookupForPlanning(intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.40, Degraded: true,
	})
	if !weak.Degraded || weak.Confidence != 0.40 {
		t.Fatalf("sub-floor lookup = %+v, must stay degraded", weak)
	}
	broken := projectSrvReadOnlyLookupForPlanning(intent.ClassificationResult{
		Primary: intent.LabelUnknown, Confidence: 0.90, Degraded: true, ControlPlaneFailure: true,
	})
	if !broken.Degraded || !broken.ControlPlaneFailure || broken.Primary != intent.LabelUnknown {
		t.Fatalf("protocol failure = %+v, must stay a control-plane failure", broken)
	}
}

func TestSrvLateTreeKeepsPrincipalAndRefusesATie(t *testing.T) {
	c := newSrvPrincipalIntentClassifier(nil)
	var seen []agentservice.Principal
	c.tree = func(_ context.Context, _ context.Context, p agentservice.Principal, _, text string) (string, error) {
		if text != "北京天气" {
			return "", context.DeadlineExceeded
		}
		seen = append(seen, p)
		return `{"top":[{"skill":"live_data","score":0.91}]}`, nil
	}
	if _, err := c.ClassifyDynamicIntent(context.Background(), agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}, "北京天气"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.classifyTree(context.Background(), context.Background(), "system", "北京天气"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[1].TenantID != "tenant-a" || seen[1].UserID != "user-a" {
		t.Fatalf("late principal = %#v", seen)
	}

	other := newSrvPrincipalIntentClassifier(nil)
	other.rememberPrincipal(agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}, "北京天气", false)
	other.rememberPrincipal(agentservice.Principal{TenantID: "tenant-b", UserID: "user-b"}, "北京天气", false)
	if _, ok := other.principalForLateTree("北京天气"); ok {
		t.Fatal("two tenants saying the same thing must not share a model")
	}
}

func TestSrvFusionTreeWaitsTheFullLLMBudget(t *testing.T) {
	c := newSrvPrincipalIntentClassifier(nil)
	if c.uic.FusionTreeDeadline() != intent.DefaultLLMTimeout {
		t.Fatalf("fusion deadline = %s, want the desktop LLM budget %s", c.uic.FusionTreeDeadline(), intent.DefaultLLMTimeout)
	}
	if c.uic.FusionTreeDeadline() <= intent.DefaultFusionTreeDeadline {
		t.Fatalf("fusion deadline = %s, still the 12s default that drops the tool surface", c.uic.FusionTreeDeadline())
	}
}

func TestSrvIntentLLMConfigPinsSystemFreeForHubBot(t *testing.T) {
	c := newSrvPrincipalIntentClassifier(nil)
	p := agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}
	plain := c.hubBotLLMConfig(context.Background(), context.Background(), p, "hi", corelib.MaclawLLMConfig{Model: "auto"})
	if plain.ServiceGroupID != "" {
		t.Fatalf("ordinary turn group = %q", plain.ServiceGroupID)
	}
	ctx := agentservice.WithHubBotLLMGroup(context.Background())
	pinned := srvIntentLLMConfig(c.hubBotLLMConfig(ctx, context.Background(), p, "hi", corelib.MaclawLLMConfig{
		URL: "https://hub.example/api/llm/v1", Model: "auto",
	}))
	if pinned.ServiceGroupID != "system-free" || pinned.TaskTypeHint != string(llm.TaskIntent) || !pinned.HubManaged {
		t.Fatalf("hub bot classify = %+v", pinned)
	}
	c.rememberPrincipal(p, "hi", true)
	late := c.hubBotLLMConfig(context.Background(), context.Background(), p, "hi", corelib.MaclawLLMConfig{Model: "auto"})
	if late.ServiceGroupID != "system-free" {
		t.Fatalf("late tree lost the bot group: %+v", late)
	}
	otherText := c.hubBotLLMConfig(context.Background(), context.Background(), p, "other", corelib.MaclawLLMConfig{Model: "auto"})
	if otherText.ServiceGroupID != "" {
		t.Fatalf("another utterance inherited the bot group: %+v", otherText)
	}
	c.rememberPrincipal(p, "hi", false)
	kept := c.hubBotLLMConfig(context.Background(), context.Background(), p, "hi", corelib.MaclawLLMConfig{Model: "auto"})
	if kept.ServiceGroupID != "system-free" {
		t.Fatalf("a later non-bot remember cleared the pin: %+v", kept)
	}
}

func TestSrvIntentLLMConfigMarksOnlyHubClassify(t *testing.T) {
	hub := srvIntentLLMConfig(corelib.MaclawLLMConfig{
		URL: "https://hub.mypapers.top/api/llm/v1", Model: "auto",
	})
	if hub.TaskTypeHint != string(llm.TaskIntent) || !hub.HubManaged {
		t.Fatalf("hub classify hint = %+v", hub)
	}
	if hub.ThinkingMode != "disabled" || hub.ReasoningEffort != "none" || hub.MaxOutputTokens != srvIntentMaxOutputTokens {
		t.Fatalf("hub classify must not think or reserve a chat-sized reply: %+v", hub)
	}
	third := srvIntentLLMConfig(corelib.MaclawLLMConfig{
		URL: "https://api.openai.com/v1", Model: "gpt-4o", ThinkingMode: "enabled", ReasoningEffort: "high",
	})
	if third.TaskTypeHint != "" || third.HubManaged {
		t.Fatalf("third-party endpoint must stay unmarked: %+v", third)
	}
	if third.ThinkingMode != "disabled" || third.ReasoningEffort != "none" || third.MaxOutputTokens != srvIntentMaxOutputTokens {
		t.Fatalf("third-party classify must not think or reserve a chat-sized reply: %+v", third)
	}
	already := srvIntentLLMConfig(corelib.MaclawLLMConfig{
		URL: "https://example.internal/v1", Model: "local", HubManaged: true,
	})
	if already.TaskTypeHint != string(llm.TaskIntent) {
		t.Fatalf("already hub-managed = %+v", already)
	}
	alwaysOn := srvIntentLLMConfig(corelib.MaclawLLMConfig{
		URL: "https://api.openai.com/v1", Model: "glm-5.3", ThinkingMode: "enabled", MaxOutputTokens: 8192,
	})
	if alwaysOn.ThinkingMode != "enabled" || alwaysOn.HubManaged || alwaysOn.MaxOutputTokens != 8192 {
		t.Fatalf("always-on thinking model = %+v", alwaysOn)
	}
}

func TestSrvEmbedderAttachSkipsAWarmModel(t *testing.T) {
	now := time.Now()
	current := srvAIModelEmbedderAdapter{manager: &srvAIModelManager{}}
	same := srvAIModelEmbedderAdapter{manager: current.manager}
	other := srvAIModelEmbedderAdapter{manager: &srvAIModelManager{}}
	if srvEmbedderAttachNeeded(current, same, true, now, now.Add(time.Second)) {
		t.Fatal("a warm model must not be loaded again")
	}
	if srvEmbedderAttachNeeded(current, same, false, now, now.Add(time.Second)) {
		t.Fatal("an in-flight warmup must not be reset")
	}
	if !srvEmbedderAttachNeeded(current, same, false, now, now.Add(srvIntentEmbedderRearm)) {
		t.Fatal("a warmup that never became ready must be attachable again")
	}
	if !srvEmbedderAttachNeeded(current, other, true, now, now) {
		t.Fatal("a different model must attach")
	}
	if srvEmbedderAttachNeeded(current, embedding.NewNoopEmbedder(), false, time.Time{}, now) {
		t.Fatal("a disabled embedder must not attach")
	}
}

func TestSrvLateTreeTombstoneBlocksTheNextTenant(t *testing.T) {
	c := newSrvPrincipalIntentClassifier(nil)
	start := time.Now()
	now := start
	c.clock = func() time.Time { return now }

	c.rememberPrincipal(agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}, "北京天气", false)
	now = start.Add(srvIntentLeaseTTL + time.Second)
	c.rememberPrincipal(agentservice.Principal{TenantID: "tenant-b", UserID: "user-b"}, "北京天气", false)
	if _, ok := c.principalForLateTree("北京天气"); ok {
		t.Fatal("an expired tenant must keep the phrase tombstoned so the next tenant is not billed")
	}

	now = start.Add(srvIntentLeaseTTL + srvIntentLeaseGrace)
	got, ok := c.principalForLateTree("北京天气")
	if !ok || got.TenantID != "tenant-b" || got.UserID != "user-b" {
		t.Fatalf("after the tombstone, the live tenant must remain, got %#v ok=%v", got, ok)
	}
}

func TestSrvLiveDataClassificationGrantsWebSearch(t *testing.T) {
	c := newSrvPrincipalIntentClassifier(nil)
	c.tree = func(context.Context, context.Context, agentservice.Principal, string, string) (string, error) {
		return `{"top":[{"skill":"live_data","score":0.91}]}`, nil
	}
	registry, err := agentservice.NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &agentservice.PrincipalIntentLabelCapabilityNeedResolver{
		Classifier:        c,
		Registry:          registry,
		Rules:             agentservice.ReviewedDynamicIntentCapabilityNeedRules(),
		MinimumConfidence: agentservice.ReviewedIntentMinimumConfidence,
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), agentservice.DynamicCapabilityNeedRequest{
		Principal: agentservice.Principal{TenantID: "t", UserID: "u"},
		UserText:  "北京天气",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.Managed {
		t.Fatal("live weather must be a managed lookup")
	}
	for _, need := range resolution.Needs {
		if need.Capability == agentservice.CapabilityInformationSearchWeb {
			return
		}
	}
	t.Fatalf("needs = %#v, want information.search.web", resolution.Needs)
}
