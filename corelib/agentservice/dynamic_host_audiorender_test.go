package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostAudioRenderer struct {
	principal Principal
	text      string
	result    string
	err       error
}

func (f *fakeHostAudioRenderer) RenderReviewedHostSpeech(_ context.Context, principal Principal, text string) (string, error) {
	f.principal = principal
	f.text = text
	return f.result, f.err
}

type fakeHostSpeechSynthesizer struct {
	text string
	wav  []byte
	err  error
}

func (f *fakeHostSpeechSynthesizer) RenderSpeech(_ context.Context, text string) ([]byte, error) {
	f.text = text
	return f.wav, f.err
}

func TestReviewedHostAudioRenderExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	renderer := &fakeHostAudioRenderer{result: "Speech artifact published; deliver it through the current-channel voice adapter. This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{AudioRender: renderer})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-render", TurnID: "turn-render", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "render", Capability: CapabilityAudioRender, Required: true,
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("render plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "tts" || plan.Selections[0].AdapterName == "tts_render" || plan.Selections[0].AdapterName == "tts_local" {
		t.Fatalf("render leaked soup: %#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("render must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"text":"hello voice"}`)
	if !result.Succeeded || result.Unknown || !strings.Contains(result.Result, "This is not a send.") || renderer.text != "hello voice" {
		t.Fatalf("render result=%#v writer=%#v", result, renderer)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"text":"hello","channel":"lansenger","path":"out.wav"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("path soup must fail closed, result=%#v", rejected)
	}
	bypass := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"text":"[voice_base64|a.wav|audio/wav]AAAA"}`)
	if bypass.Succeeded || bypass.Unknown {
		t.Fatalf("voice_base64 must fail closed, result=%#v", bypass)
	}
}

func TestReviewedHostAudioRenderAndCurrentVoiceDeliverPlanTogether(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	renderer := &fakeHostAudioRenderer{result: "prepared"}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{
		AudioRender:       renderer,
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
			ID: "render", Capability: CapabilityAudioRender, Required: true,
		}, {
			ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
			Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatVoice},
		}},
	})
	if err != nil || len(plan.Selections) != 2 || len(plan.Unmet) != 0 {
		t.Fatalf("render+deliver plan=%#v err=%v", plan, err)
	}
	var renderID string
	var deliverSel coretool.PlannedSelection
	for _, selection := range plan.Selections {
		if selection.FitProof.MatchedCapability == CapabilityAudioRender {
			renderID = selection.ID
		}
		if selection.FitProof.MatchedCapability == CapabilityArtifactDeliverCurrent {
			deliverSel = selection
		}
	}
	found := false
	for _, requirement := range deliverSel.Requires {
		if requirement == renderID {
			found = true
		}
	}
	if renderID == "" || !found {
		t.Fatalf("deliver must wait for render, render=%s requires=%#v", renderID, deliverSel.Requires)
	}
}

func TestReviewedHostAudioRenderIsAbsentWithoutRenderer(t *testing.T) {
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
			ID: "render", Capability: CapabilityAudioRender, Required: true,
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("render without synthesizer must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostAudioRenderRejectsSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostAudioRenderProvider(&fakeHostAudioRenderer{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityAudioRender || provider.AdapterName == "tts" || provider.AdapterName == "tts_render" || provider.AdapterName == "tts_local" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["text"]; !ok || len(props) != 1 {
		t.Fatalf("render schema=%#v", props)
	}
	for _, key := range []string{"path", "channel", "destination", "group_name", "file_name", "action"} {
		if _, ok := props[key]; ok {
			t.Fatalf("render schema leaked %s", key)
		}
	}
}

func TestBindReviewedHostDeliverableTurnSkipsInboundWhenAudioRenderPresent(t *testing.T) {
	needs := []coretool.CapabilityNeed{{
		ID: "render", Capability: CapabilityAudioRender, Required: true,
	}, {
		ID: "deliver", Capability: CapabilityArtifactDeliverCurrent, Required: true,
		Qualifiers: map[string]string{QualifierArtifactFormat: ArtifactFormatVoice},
	}}
	resolved, doc, img, voice, err := bindReviewedHostDeliverableTurn(needs, reviewedHostDeliverableTurnInputs{
		Voices: []reviewedHostVoiceInput{{FileName: "clip.wav"}},
	})
	if err != nil || doc != nil || img != nil || voice != nil || len(resolved) != 2 {
		t.Fatalf("render+deliver must ignore inbound voice, resolved=%#v err=%v", resolved, err)
	}
	if resolved[1].Qualifiers[QualifierArtifactFormat] != ArtifactFormatVoice {
		t.Fatalf("deliver format rewritten: %#v", resolved[1])
	}
}

func TestReviewedHostAttachmentDeliverPrepareGeneratedSpeechIsNotASend(t *testing.T) {
	generated, err := reviewedHostGeneratedSpeechFromWAV("hello", reviewedHostTestWAV())
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	cb := &coreAgentCallbacks{
		principal:                   principal,
		trustedDestinationID:        "user:alice",
		reviewedHostAudioRender:     true,
		reviewedHostGeneratedSpeech: generated,
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("generated speech deliver must not call send_file soup")
			return "sent"
		},
	}
	services := cb.reviewedHostOwnedServices()
	if services.AttachmentDeliver == nil {
		t.Fatal("generated speech must publish attachment deliver")
	}
	out, err := cb.PrepareReviewedHostAttachmentDeliver(context.Background(), principal, "user:alice")
	if err != nil || !strings.Contains(out, "This is not a send.") {
		t.Fatalf("prepare=%q err=%v", out, err)
	}
}

func TestReviewedHostAudioRenderCallbackPublishesArtifactWithoutSend(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	synth := &fakeHostSpeechSynthesizer{wav: reviewedHostTestWAV()}
	cb := &coreAgentCallbacks{
		principal:               principal,
		reviewedHostAudioRender: true,
		speechSynthesizer:       synth,
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("render must not call send_file soup")
			return "sent"
		},
	}
	out, err := cb.RenderReviewedHostSpeech(context.Background(), principal, "hello")
	if err != nil || !strings.Contains(out, "This is not a send.") || cb.reviewedHostGeneratedSpeech == nil {
		t.Fatalf("render=%q err=%v generated=%v", out, err, cb.reviewedHostGeneratedSpeech)
	}
	if string(cb.reviewedHostGeneratedSpeech.Data) != string(reviewedHostTestWAV()) || synth.text != "hello" {
		t.Fatalf("generated=%#v synth=%#v", cb.reviewedHostGeneratedSpeech, synth)
	}
}

func TestReviewedDynamicIntentRulesResolveAudioDeliverWithoutSynthesize(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary: intent.LabelAudioDeliver, Confidence: .99,
			ToolNames: []string{"tts", "tts_render", "tts_local", "send_to_im", "asr"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "把这段话发成语音消息"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 2 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	got := map[coretool.CapabilityID]bool{}
	for _, need := range resolution.Needs {
		got[need.Capability] = true
	}
	if !got[CapabilityAudioRender] || !got[CapabilityArtifactDeliverCurrent] {
		t.Fatalf("needs=%#v", resolution.Needs)
	}
	if resolution.Needs[0].Capability == CapabilityAudioTranscribe {
		t.Fatal("audio_deliver must not resolve to audio.transcribe.speech")
	}
}

func TestReviewedHostOwnedServicesPublishAudioRenderWhenSynthesizerReady(t *testing.T) {
	cb := &coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}, speechSynthesizer: &fakeHostSpeechSynthesizer{wav: reviewedHostTestWAV()}}
	services := cb.reviewedHostOwnedServices()
	if services.AudioRender == nil {
		t.Fatal("ready synthesizer must publish audio render")
	}
	empty := (&coreAgentCallbacks{principal: Principal{TenantID: "t", UserID: "u"}}).reviewedHostOwnedServices()
	if empty.AudioRender != nil {
		t.Fatal("missing synthesizer must keep audio render unpublished")
	}
}
