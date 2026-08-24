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

type fakeHostAudioSynthesizer struct {
	principal Principal
	text      string
	result    string
	err       error
}

func (f *fakeHostAudioSynthesizer) PlayReviewedHostSpeech(_ context.Context, principal Principal, text string) (string, error) {
	f.principal = principal
	f.text = text
	return f.result, f.err
}

type fakeHostSpeechPlayer struct {
	wav []byte
	err error
}

func (f *fakeHostSpeechPlayer) PlaySpeech(_ context.Context, wav []byte) error {
	f.wav = append([]byte(nil), wav...)
	return f.err
}

func TestReviewedHostAudioSynthesizeExecutesWithoutCoordinatorAndRejectsSoup(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	player := &fakeHostAudioSynthesizer{result: "Speech played on the host. This is not a send."}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{AudioSynthesize: player})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-play", TurnID: "turn-play", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "play", Capability: CapabilityAudioSynthesize, Required: true,
		}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("play plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].AdapterName == "tts" || plan.Selections[0].AdapterName == "tts_render" || plan.Selections[0].AdapterName == "tts_local" {
		t.Fatalf("play leaked soup: %#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("play must use the local mutation receipt, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "t", UserID: "u"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"text":"hello voice"}`)
	if !result.Succeeded || result.Unknown || !strings.Contains(result.Result, "This is not a send.") || strings.Contains(result.Result, "[voice_base64") || player.text != "hello voice" {
		t.Fatalf("play result=%#v player=%#v", result, player)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"text":"hello","channel":"lansenger","path":"out.wav"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("path soup must fail closed, result=%#v", rejected)
	}
	bypass := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"text":"[voice_base64|a.wav|audio/wav]AAAA"}`)
	if bypass.Succeeded || bypass.Unknown {
		t.Fatalf("voice_base64 must fail closed, result=%#v", bypass)
	}
	renderPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-render", TurnID: "turn-render", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "render", Capability: CapabilityAudioRender, Required: true,
		}},
	})
	if err != nil || len(renderPlan.Selections) != 0 {
		t.Fatalf("audio.render.speech must not be satisfied by local playback, plan=%#v err=%v", renderPlan, err)
	}
}

func TestReviewedHostAudioSynthesizeIsAbsentWithoutPlayer(t *testing.T) {
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
			ID: "play", Capability: CapabilityAudioSynthesize, Required: true,
		}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("play without player must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostAudioSynthesizeRejectsSoupFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostAudioSynthesizeProvider(&fakeHostAudioSynthesizer{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityAudioSynthesize || provider.AdapterName == "tts" || provider.AdapterName == "tts_local" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["text"]; !ok || len(props) != 1 {
		t.Fatalf("play schema=%#v", props)
	}
	for _, key := range []string{"path", "channel", "destination", "group_name", "file_name", "action"} {
		if _, ok := props[key]; ok {
			t.Fatalf("play schema leaked %s", key)
		}
	}
}

func TestReviewedHostAudioSynthesizeCallbackPlaysWithoutSend(t *testing.T) {
	principal := Principal{TenantID: "t", UserID: "u"}
	synth := &fakeHostSpeechSynthesizer{wav: reviewedHostTestWAV()}
	player := &fakeHostSpeechPlayer{}
	cb := &coreAgentCallbacks{
		principal:         principal,
		speechSynthesizer: synth,
		speechPlayer:      player,
		imFileHandler: func(map[string]interface{}) string {
			t.Fatal("local playback must not call send_file soup")
			return "sent"
		},
	}
	out, err := cb.PlayReviewedHostSpeech(context.Background(), principal, "hello")
	if err != nil || !strings.Contains(out, "This is not a send.") || strings.Contains(out, "[voice_base64") || cb.reviewedHostGeneratedSpeech != nil {
		t.Fatalf("play=%q err=%v generated=%v", out, err, cb.reviewedHostGeneratedSpeech)
	}
	if synth.text != "hello" || string(player.wav) != string(reviewedHostTestWAV()) {
		t.Fatalf("synth=%#v player=%#v", synth, player)
	}
	empty := &coreAgentCallbacks{principal: principal, speechSynthesizer: synth}
	if _, err := empty.PlayReviewedHostSpeech(context.Background(), principal, "hello"); err == nil {
		t.Fatal("missing player must stay unpublished")
	}
}

func TestReviewedDynamicIntentRulesResolveAudioSynthesizeWithoutDeliver(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	resolver := &IntentLabelCapabilityNeedResolver{
		Classifier: fixedIntentClassificationSource{result: intent.ClassificationResult{
			Primary:    intent.LabelAudioSynthesize,
			Confidence: .99,
			ToolNames:  []string{"tts", "tts_local", "tts_render", "send_to_im"},
		}},
		Registry: registry, Rules: ReviewedDynamicIntentCapabilityNeedRules(),
	}
	resolution, err := resolver.ResolveDynamicCapabilityNeeds(context.Background(), DynamicCapabilityNeedRequest{UserText: "read this paragraph aloud"})
	if err != nil || !resolution.Managed || len(resolution.Needs) != 1 {
		t.Fatalf("resolution=%#v err=%v", resolution, err)
	}
	if resolution.Needs[0].Capability != CapabilityAudioSynthesize {
		t.Fatalf("needs=%#v", resolution.Needs)
	}
	if resolution.Needs[0].Capability == CapabilityAudioRender || resolution.Needs[0].Capability == CapabilityArtifactDeliverCurrent {
		t.Fatal("audio_synthesize must not resolve to render or current-channel deliver")
	}
}

func TestReviewedHostOwnedServicesPublishAudioSynthesizeWhenPlayerReady(t *testing.T) {
	cb := &coreAgentCallbacks{
		principal:         Principal{TenantID: "t", UserID: "u"},
		speechSynthesizer: &fakeHostSpeechSynthesizer{wav: reviewedHostTestWAV()},
		speechPlayer:      &fakeHostSpeechPlayer{},
	}
	services := cb.reviewedHostOwnedServices()
	if services.AudioSynthesize == nil || services.AudioRender == nil {
		t.Fatal("ready synth plus player must publish local playback and render")
	}
	noPlayer := (&coreAgentCallbacks{
		principal:         Principal{TenantID: "t", UserID: "u"},
		speechSynthesizer: &fakeHostSpeechSynthesizer{wav: reviewedHostTestWAV()},
	}).reviewedHostOwnedServices()
	if noPlayer.AudioSynthesize != nil {
		t.Fatal("missing player must keep local playback unpublished")
	}
	if noPlayer.AudioRender == nil {
		t.Fatal("synthesizer alone must still publish render")
	}
}

func TestReviewedHostNativeSpeechPlayerReadyFollowsDisplay(t *testing.T) {
	player := reviewedHostNativeSpeechPlayer{}
	ok, _ := remote.DetectDisplayServer()
	if player.Ready() != ok {
		t.Fatalf("native ready=%v display=%v goos=%s", player.Ready(), ok, runtime.GOOS)
	}
}
