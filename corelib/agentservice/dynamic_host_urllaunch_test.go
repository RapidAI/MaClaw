package agentservice

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostURLLauncher struct {
	principal Principal
	rawURL    string
	result    string
	err       error
}

func (f *fakeHostURLLauncher) OpenReviewedHostURL(_ context.Context, principal Principal, rawURL string) (string, error) {
	f.principal = principal
	f.rawURL = rawURL
	return f.result, f.err
}

type fakeHostURLOpener struct {
	rawURL string
	err    error
}

func (f *fakeHostURLOpener) OpenURL(_ context.Context, rawURL string) error {
	f.rawURL = rawURL
	return f.err
}

func TestReviewedHostURLLaunchExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	launcher := &fakeHostURLLauncher{result: "URL opened with the system handler. This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{URLLaunch: launcher})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-url", TurnID: "turn-url", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "open", Capability: CapabilitySystemLaunch, Required: true,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindURL},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("url plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "open" || plan.Selections[0].AdapterName == "browser" {
		t.Fatalf("url leaked soup: %#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("url must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.com/docs"}`)
	if !result.Succeeded || result.Unknown || !strings.Contains(result.Result, "This is not a send.") || launcher.rawURL != "https://example.com/docs" {
		t.Fatalf("url result=%#v launcher=%#v", result, launcher)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"https://example.com","path":"notes.pdf","channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("open soup must fail closed, result=%#v", rejected)
	}
	fileURL := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"url":"file:///tmp/secret.pdf"}`)
	if fileURL.Succeeded || fileURL.Unknown {
		t.Fatalf("file URL must fail closed, result=%#v", fileURL)
	}
	docPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-doc", TurnID: "turn-doc", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "open", Capability: CapabilitySystemLaunch, Required: true,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindDocument},
		}},
	})
	if err != nil || len(docPlan.Selections) != 0 {
		t.Fatalf("document open must not be satisfied by url launch, plan=%#v err=%v", docPlan, err)
	}
}

func TestReviewedHostURLLaunchIsAbsentWithoutLauncher(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "open", Capability: CapabilitySystemLaunch, Required: true,
			Qualifiers: map[string]string{QualifierLaunchKind: LaunchKindURL},
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("url without launcher must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostURLLaunchRejectsSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostURLLaunchProvider(&fakeHostURLLauncher{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilitySystemLaunch || provider.AdapterName == "open" {
		t.Fatalf("provider=%#v", provider)
	}
	if provider.Provides[0].Qualifiers[QualifierLaunchKind] != LaunchKindURL {
		t.Fatalf("kind=%#v", provider.Provides[0].Qualifiers)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["url"]; !ok || len(props) != 1 {
		t.Fatalf("url schema=%#v", props)
	}
	for _, key := range []string{"path", "target", "app", "channel", "destination", "group_name"} {
		if _, ok := props[key]; ok {
			t.Fatalf("url schema leaked %s", key)
		}
	}
}

func TestReviewedHostURLLaunchCallbackOpensHTTPSWithoutSend(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	opened := &fakeHostURLOpener{}
	cb := &coreAgentCallbacks{
		principal:   principal,
		urlLauncher: opened,
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("url open must not call send_file soup")
			return "sent"
		},
	}
	out, err := cb.OpenReviewedHostURL(context.Background(), principal, "https://example.com/a")
	if err != nil || !strings.Contains(out, "This is not a send.") || opened.rawURL != "https://example.com/a" {
		t.Fatalf("open=%q err=%v opened=%q", out, err, opened.rawURL)
	}
	if _, err := cb.OpenReviewedHostURL(context.Background(), principal, "file:///tmp/secret.pdf"); err == nil {
		t.Fatal("file URL must stay unpublished")
	}
	if _, err := cb.OpenReviewedHostURL(context.Background(), principal, "mailto:a@example.com"); err == nil {
		t.Fatal("mailto must stay unpublished")
	}
	if _, err := cb.OpenReviewedHostURL(context.Background(), principal, "https://user:pass@example.com/"); err == nil {
		t.Fatal("userinfo must stay unpublished")
	}
	if _, err := cb.OpenReviewedHostURL(context.Background(), principal, "calc"); err == nil {
		t.Fatal("app name must stay unpublished")
	}
	empty := &coreAgentCallbacks{principal: principal}
	if _, err := empty.OpenReviewedHostURL(context.Background(), principal, "https://example.com"); err == nil {
		t.Fatal("missing opener must stay unpublished")
	}
}

func TestReviewedDynamicIntentRulesResolveAppLaunchAsURLWithoutDocument(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary:    intent.LabelAppLaunch,
			Confidence: .99,
			ToolNames:  []string{"open", "browser", "send_file"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "open this URL in the default browser"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilitySystemLaunch || resolution.Needs[0].Qualifiers[QualifierLaunchKind] != LaunchKindURL {
		t.Fatalf("needs=%#v", resolution.Needs)
	}
	if resolution.Needs[0].Qualifiers[QualifierLaunchKind] == LaunchKindDocument || resolution.Needs[0].Capability == CapabilityBrowserControl {
		t.Fatal("app_launch must not resolve to document open or browser.control.web")
	}
}

func TestReviewedHostOwnedServicesPublishURLLaunchWhenOpenerReady(t *testing.T) {
	cb := &coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}, urlLauncher: &fakeHostURLOpener{}}
	services := cb.reviewedHostOwnedServices()
	if services.URLLaunch == nil {
		t.Fatal("ready opener must publish url launch")
	}
	if services.SystemLaunch != nil {
		t.Fatal("url opener must not publish document launch")
	}
	empty := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}}).reviewedHostOwnedServices()
	if empty.URLLaunch != nil {
		t.Fatal("missing opener must keep url launch unpublished")
	}
}

func TestReviewedHostNativeURLLauncherReadyFollowsDisplay(t *testing.T) {
	launcher := reviewedHostNativeURLLauncher{}
	ok, _ := remote.DetectDisplayServer()
	if launcher.Ready() != ok {
		t.Fatalf("native ready=%v display=%v goos=%s", launcher.Ready(), ok, runtime.GOOS)
	}
}

func TestReviewedHostPublicLaunchURLRejectsNonHTTPS(t *testing.T) {
	if _, err := reviewedHostPublicLaunchURL("https://example.com/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewedHostPublicLaunchURL("http://example.com/x"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"file:///tmp/a", "javascript:alert(1)", "mailto:a@b.com", "ftp://example.com", "C:\\\\Windows\\\\calc.exe", ""} {
		if _, err := reviewedHostPublicLaunchURL(raw); err == nil {
			t.Fatalf("must reject %q", raw)
		}
	}
}
