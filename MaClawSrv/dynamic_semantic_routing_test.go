package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
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
	other.rememberPrincipal(agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}, "北京天气")
	other.rememberPrincipal(agentservice.Principal{TenantID: "tenant-b", UserID: "user-b"}, "北京天气")
	if _, ok := other.principalForLateTree("北京天气"); ok {
		t.Fatal("two tenants saying the same thing must not share a model")
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
