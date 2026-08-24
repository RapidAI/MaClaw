package agentservice

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostVisualCapturer struct {
	principal Principal
	result    string
	err       error
}

func (f *fakeHostVisualCapturer) CaptureReviewedHostDesktop(_ context.Context, principal Principal) (string, error) {
	f.principal = principal
	return f.result, f.err
}

type fakeHostDesktopCapturer struct {
	png []byte
	err error
}

func (f *fakeHostDesktopCapturer) CapturePrimary(_ context.Context) ([]byte, error) {
	return f.png, f.err
}

func TestReviewedHostVisualCaptureExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	capturer := &fakeHostVisualCapturer{result: "Screenshot artifact published; deliver it through the current-channel image adapter. This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{VisualCapture: capturer})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-cap", TurnID: "turn-cap", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "capture", Capability: CapabilityVisualCapture, Required: true,
			Qualifiers: map[string]string{QualifierCaptureDisplay: CaptureDisplayPrimary},
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("capture plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "screenshot" || plan.Selections[0].AdapterName == "computer_use" {
		t.Fatalf("capture leaked soup: %#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("capture must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !result.Succeeded || result.Unknown || !strings.Contains(result.Result, "This is not a send.") {
		t.Fatalf("capture result=%#v", result)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"session_id":"s1","display":1,"channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("screenshot soup must fail closed, result=%#v", rejected)
	}
}

func TestReviewedHostVisualCaptureAndCurrentImageDeliverPlanTogether(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		VisualCapture:     &fakeHostVisualCapturer{result: "prepared"},
		AttachmentDeliver: &fakeHostAttachmentDeliverer{result: "not a send"},
		DestinationID:     "user:alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-chain", TurnID: "turn-chain", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "capture", Capability: CapabilityVisualCapture, Required: true,
			Qualifiers: map[string]string{QualifierCaptureDisplay: CaptureDisplayPrimary},
		}, {
			ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage},
		}},
	})
	if err != nil || len(plan.Selections) != 2 || len(plan.Unmet) != 0 {
		t.Fatalf("capture+deliver plan=%#v err=%v", plan, err)
	}
	var captureID string
	var deliverSel coretool.PlannedSelection
	for _, selection := range plan.Selections {
		if selection.FitProof.MatchedCapability == CapabilityVisualCapture {
			captureID = selection.ID
		}
		if selection.FitProof.MatchedCapability == CapabilityArtifactDeliverCurrent {
			deliverSel = selection
		}
	}
	found := false
	for _, requirement := range deliverSel.Requires {
		if requirement == captureID {
			found = true
		}
	}
	if captureID == "" || !found {
		t.Fatalf("deliver must wait for capture, capture=%s requires=%#v", captureID, deliverSel.Requires)
	}
}

func TestReviewedHostVisualCaptureIsAbsentWithoutCapturer(t *testing.T) {
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
			ID: "capture", Capability: CapabilityVisualCapture, Required: true,
			Qualifiers: map[string]string{QualifierCaptureDisplay: CaptureDisplayPrimary},
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("capture without capturer must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostVisualCaptureRejectsSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostVisualCaptureProvider(&fakeHostVisualCapturer{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityVisualCapture || provider.AdapterName == "screenshot" || provider.AdapterName == "computer_use" {
		t.Fatalf("provider=%#v", provider)
	}
	if provider.Provides[0].Qualifiers[QualifierCaptureDisplay] != CaptureDisplayPrimary {
		t.Fatalf("display=%#v", provider.Provides[0].Qualifiers)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if len(props) != 0 {
		t.Fatalf("capture schema=%#v", props)
	}
	for _, key := range []string{"session_id", "display", "path", "channel", "destination"} {
		if _, ok := props[key]; ok {
			t.Fatalf("capture schema leaked %s", key)
		}
	}
}

func TestBindReviewedHostDeliverableTurnSkipsInboundWhenVisualCapturePresent(t *testing.T) {
	needs := []coretool.CapabilityNeed{{
		ID: "capture", Capability: CapabilityVisualCapture, Required: true,
		Qualifiers: map[string]string{QualifierCaptureDisplay: CaptureDisplayPrimary},
	}, {
		ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
		Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatImage},
	}}
	resolved, doc, img, voice, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{
		Images: []reviewedHostImageInput{{FileName: "inbound.png"}},
	})
	if err != nil || doc != nil || img != nil || voice != nil || len(resolved) != 2 {
		t.Fatalf("capture+deliver must ignore inbound image, resolved=%#v err=%v", resolved, err)
	}
	if resolved[1].Qualifiers[QualifierArtifactFormat] != ArtifactFormatImage {
		t.Fatalf("deliver format rewritten: %#v", resolved[1])
	}
}

func TestReviewedHostAttachmentDeliverPrepareGeneratedImageIsNotASend(t *testing.T) {
	generated, err := reviewedHostGeneratedImageFromPNG(reviewedHostTestPNG())
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:                  principal,
		trustedDestinationID:       "user:alice",
		reviewedHostVisualCapture:  true,
		reviewedHostGeneratedImage: generated,
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("generated screenshot deliver must not call send_file soup")
			return "sent"
		},
	}
	services := cb.reviewedHostOwnedServices()
	if services.AttachmentDeliver == nil {
		t.Fatal("generated screenshot must publish attachment deliver")
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
}

func TestReviewedHostVisualCaptureCallbackPublishesArtifactWithoutSend(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:                 principal,
		reviewedHostVisualCapture: true,
		desktopCapturer:           &fakeHostDesktopCapturer{png: reviewedHostTestPNG()},
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("capture must not call send_file soup")
			return "sent"
		},
	}
	out, err := cb.CaptureReviewedHostDesktop(context.Background(), principal)
	if err != nil || !strings.Contains(out, "This is not a send.") || cb.reviewedHostGeneratedImage == nil {
		t.Fatalf("capture=%q err=%v generated=%v", out, err, cb.reviewedHostGeneratedImage)
	}
	if string(cb.reviewedHostGeneratedImage.Data) != string(reviewedHostTestPNG()) {
		t.Fatalf("generated=%#v", cb.reviewedHostGeneratedImage)
	}
}

func TestReviewedDynamicIntentRulesResolveScreenshotWithoutComputerUse(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelScreenshot, Confidence: .99,
			ToolNames: []string{"screenshot", "computer_use", "send_file"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "截一张当前屏幕发给我"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 2 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	got := map[coretool.CapabilityID]bool{}
	for _, need := range resolution.Needs {
		got[need.Capability] = true
	}
	if !got[CapabilityVisualCapture] || !got[CapabilityArtifactDeliverCurrent] {
		t.Fatalf("needs=%#v", resolution.Needs)
	}
	if resolution.Needs[0].Capability == CapabilityComputerUse {
		t.Fatal("screenshot must not resolve to computer.control.desktop")
	}
}

func TestReviewedHostOwnedServicesPublishVisualCaptureWhenCapturerReady(t *testing.T) {
	cb := &coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}, desktopCapturer: &fakeHostDesktopCapturer{png: reviewedHostTestPNG()}}
	services := cb.reviewedHostOwnedServices()
	if services.VisualCapture == nil {
		t.Fatal("ready capturer must publish visual capture")
	}
	empty := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}}).reviewedHostOwnedServices()
	if empty.VisualCapture != nil {
		t.Fatal("missing capturer must keep visual capture unpublished")
	}
}

func TestReviewedHostNativeDesktopCapturerReadyOnlyOnWindowsDisplay(t *testing.T) {
	capturer := reviewedHostNativeDesktopCapturer{}
	if runtime.GOOS != "windows" && capturer.Ready() {
		t.Fatal("non-Windows native capturer must stay unready")
	}
}
