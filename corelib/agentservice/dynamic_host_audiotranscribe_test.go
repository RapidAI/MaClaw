package agentservice

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostAudioTranscriber struct {
	principal Principal
	result    string
	err       error
}

func (f *fakeHostAudioTranscriber) TranscribeReviewedHostAudio(_ context.Context, principal Principal, args map[string]interface{}) (string, error) {
	f.principal = principal
	return f.result, f.err
}

type fakeSpeechTranscriber struct {
	mime   string
	data   []byte
	result string
	err    error
}

func (f *fakeSpeechTranscriber) TranscribeSpeech(_ context.Context, mime string, data []byte) (string, error) {
	f.mime = mime
	f.data = append([]byte(nil), data...)
	return f.result, f.err
}

func TestReviewedHostAudioTranscribeExecutesWithoutCoordinatorAndRejectsPath(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	transcriber := &fakeHostAudioTranscriber{result: "hello transcript"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{AudioTranscribe: transcriber})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "asr", Capability: CapabilityAudioTranscribe, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("audio transcribe plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityAudioTranscribe {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host audio transcribe is read-only, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	got := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !got.Succeeded || transcriber.principal.TenantID != principal.TenantID || transcriber.principal.UserID != principal.UserID {
		t.Fatalf("transcribe result=%#v transcriber=%#v", got, transcriber)
	}
	pathSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"path":"clip.wav"}`)
	if pathSoup.Succeeded || pathSoup.Unknown {
		t.Fatalf("path must fail closed, result=%#v", pathSoup)
	}
	minutes := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"for_minutes":true}`)
	if minutes.Succeeded || minutes.Unknown {
		t.Fatalf("minutes must fail closed, result=%#v", minutes)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "lookup", Capability: CapabilityInformationLookup, Required: true}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("information.lookup must not be satisfied by host audio transcribe, plan=%#v err=%v", lookupPlan, err)
	}
}

func TestReviewedHostAudioTranscribeIsAbsentWithoutProvider(t *testing.T) {
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
		Needs: []coretool.CapabilityNeed{{ID: "asr", Capability: CapabilityAudioTranscribe, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("audio transcribe without provider must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostAudioTranscribeRejectsASRFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostAudioTranscribeProvider(&fakeHostAudioTranscriber{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityAudioTranscribe || provider.AdapterName == "asr" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if len(props) != 0 {
		t.Fatalf("audio transcribe schema=%#v", props)
	}
	for _, key := range []string{"path", "format", "for_minutes", "minutes", "url", "channel", "destination", "file_path"} {
		if _, ok := props[key]; ok {
			t.Fatalf("audio transcribe schema leaked %s", key)
		}
	}
}

func TestReviewedHostAudioInputsRequireOneTrustedClip(t *testing.T) {
	if _, err := applyReviewedHostAudioInputs([]coretool.CapabilityNeed{{Capability: CapabilityAudioTranscribe, Required: true}}, nil); err == nil {
		t.Fatal("missing audio must fail closed")
	}
	two := []reviewedHostAudioInput{{MIME: "audio/wav"}, {MIME: "audio/mpeg"}}
	if _, err := applyReviewedHostAudioInputs([]coretool.CapabilityNeed{{Capability: CapabilityAudioTranscribe, Required: true}}, two); err == nil {
		t.Fatal("ambiguous audio must fail closed")
	}
	inputs, err := reviewedHostAudioInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "clip.wav", MimeType: "audio/wav",
		Data: base64.StdEncoding.EncodeToString([]byte("RIFF....")),
	}})
	if err != nil || len(inputs) != 1 || inputs[0].MIME != "audio/wav" {
		t.Fatalf("inputs=%#v err=%v", inputs, err)
	}
	sniffed, err := reviewedHostAudioInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "file", MimeType: "application/octet-stream",
		Data: base64.StdEncoding.EncodeToString(reviewedHostTestWAV()),
	}})
	if err != nil || len(sniffed) != 1 || sniffed[0].MIME != "audio/wav" {
		t.Fatalf("unnamed wav must bind as trusted audio, inputs=%#v err=%v", sniffed, err)
	}
	needs, err := applyReviewedHostAudioInputs([]coretool.CapabilityNeed{{Capability: CapabilityAudioTranscribe, Required: true}}, inputs)
	if err != nil || len(needs) != 1 {
		t.Fatalf("apply=%#v err=%v", needs, err)
	}
}

func TestReviewedHostAudioTranscribeUsesTrustedBytesAndRejectsDeliveryTokens(t *testing.T) {
	pcm := []byte("RIFF trusted-audio")
	inputs, err := reviewedHostAudioInputsForTurn("root", "turn", "tenant:user", []agent.MessageAttachment{{
		FileName: "clip.wav", MimeType: "audio/wav",
		Data: base64.StdEncoding.EncodeToString(pcm),
	}})
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	engine := &fakeSpeechTranscriber{result: "recognized speech"}
	cb := &coreAgentCallbacks{principal: principal, reviewedHostAudio: &inputs[0], speechTranscriber: engine}
	got, err := cb.TranscribeReviewedHostAudio(context.Background(), principal, map[string]interface{}{})
	if err != nil || got != "recognized speech" {
		t.Fatalf("transcribe=%q err=%v", got, err)
	}
	if engine.mime != "audio/wav" || string(engine.data) != string(pcm) {
		t.Fatalf("engine=%#v", engine)
	}
	if _, err := cb.TranscribeReviewedHostAudio(context.Background(), Principal{TenantID: "tenant", UserID: "other"}, map[string]interface{}{}); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
	engine.result = "ok [voice_base64|secret]"
	if _, err := cb.TranscribeReviewedHostAudio(context.Background(), principal, map[string]interface{}{}); err == nil {
		t.Fatal("voice_base64 bypass must fail closed")
	}
	services := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if services.AudioTranscribe != nil {
		t.Fatal("audio transcribe must not attach without a trusted clip and speech engine")
	}
}

type fakeWAVSpeechEngine struct {
	wav    []byte
	result string
	err    error
}

func (f *fakeWAVSpeechEngine) TranscribeWAV(_ context.Context, wav []byte) (string, error) {
	f.wav = append([]byte(nil), wav...)
	return f.result, f.err
}

func TestNewReviewedHostSpeechTranscriberConvertsWAVAndRejectsBypass(t *testing.T) {
	if NewReviewedHostSpeechTranscriber(nil) != nil {
		t.Fatal("nil engine must stay absent")
	}
	pcm := reviewedHostTestWAV()
	engine := &fakeWAVSpeechEngine{result: "converted speech"}
	transcriber := NewReviewedHostSpeechTranscriber(engine)
	got, err := transcriber.TranscribeSpeech(context.Background(), "audio/wav", pcm)
	if err != nil || got != "converted speech" {
		t.Fatalf("transcribe=%q err=%v", got, err)
	}
	if len(engine.wav) < 12 || string(engine.wav[8:12]) != "WAVE" {
		t.Fatalf("engine wav=%#v", engine.wav)
	}
	if _, err := transcriber.TranscribeSpeech(context.Background(), "audio/mp4", []byte("not-m4a")); err == nil {
		t.Fatal("unsupported decode must fail closed")
	}
	engine.result = "ok [voice_base64|secret]"
	if _, err := transcriber.TranscribeSpeech(context.Background(), "audio/wav", pcm); err == nil {
		t.Fatal("voice_base64 bypass must fail closed")
	}
}

type gatedWAVSpeechEngine struct {
	ready bool
}

func (g gatedWAVSpeechEngine) Ready() bool { return g.ready }

func (gatedWAVSpeechEngine) TranscribeWAV(context.Context, []byte) (string, error) {
	return "should-not-run", nil
}

func TestReviewedHostSpeechReadyAndAudioTurnBinding(t *testing.T) {
	if reviewedHostSpeechReady(nil) {
		t.Fatal("nil transcriber is not ready")
	}
	if !reviewedHostSpeechReady(&fakeSpeechTranscriber{result: "ok"}) {
		t.Fatal("engine without Ready() is usable once attached")
	}
	unready := NewReviewedHostSpeechTranscriber(gatedWAVSpeechEngine{ready: false})
	if reviewedHostSpeechReady(unready) {
		t.Fatal("unready host engine must stay absent at plan time")
	}
	if _, err := unready.TranscribeSpeech(context.Background(), "audio/wav", reviewedHostTestWAV()); err == nil {
		t.Fatal("unready host engine must not transcribe")
	}
	clock := []coretool.CapabilityNeed{{Capability: CapabilityCurrentTime, Required: true}}
	got, err := bindReviewedHostAudioTurn(clock, nil, errors.New("trusted_audio_attachment_content_missing"))
	if err != nil || len(got) != 1 || got[0].Capability != CapabilityCurrentTime {
		t.Fatalf("corrupt audio must not fail a non-transcribe turn, got=%#v err=%v", got, err)
	}
	asrNeed := []coretool.CapabilityNeed{{Capability: CapabilityAudioTranscribe, Required: true}}
	if _, err := bindReviewedHostAudioTurn(asrNeed, nil, errors.New("trusted_audio_attachment_content_missing")); err == nil {
		t.Fatal("transcribe need must fail closed on corrupt audio")
	}
	if _, err := bindReviewedHostAudioTurn(asrNeed, nil, nil); err == nil {
		t.Fatal("transcribe need must fail closed without an attachment")
	}
	unreadyCB := &coreAgentCallbacks{
		principal:         Principal{TenantID: "tenant", UserID: "user"},
		reviewedHostAudio: &reviewedHostAudioInput{MIME: "audio/wav", Data: reviewedHostTestWAV()},
		speechTranscriber: NewReviewedHostSpeechTranscriber(gatedWAVSpeechEngine{ready: false}),
	}
	if _, err := unreadyCB.TranscribeReviewedHostAudio(context.Background(), unreadyCB.principal, map[string]interface{}{}); err == nil {
		t.Fatal("execute must fail closed when the host engine is no longer ready")
	}
	raw := reviewedHostTestWAV()
	inputs, err := reviewedHostAudioInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "clip.wav", MimeType: "audio/wav",
		Data: base64.RawStdEncoding.EncodeToString(raw),
	}})
	if err != nil || len(inputs) != 1 || len(inputs[0].Data) != len(raw) {
		t.Fatalf("raw std base64 must be accepted, inputs=%#v err=%v", inputs, err)
	}
	urlInputs, err := reviewedHostAudioInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "clip.wav", MimeType: "audio/wav",
		Data: base64.URLEncoding.EncodeToString(raw),
	}})
	if err != nil || len(urlInputs) != 1 || string(urlInputs[0].Data) != string(raw) {
		t.Fatalf("url-safe base64 must be accepted, inputs=%#v err=%v", urlInputs, err)
	}
	if _, ok := reviewedHostAudioMIME(agent.MessageAttachment{Type: "audio", MimeType: "audio/flac", FileName: "clip.flac"}); ok {
		t.Fatal("unknown audio/* must stay outside the trusted allowlist")
	}
	if mime, ok := ReviewedHostTrustedAudioMIME(agent.MessageAttachment{FileName: "note.ogg", MimeType: "audio/ogg"}); !ok || mime != "audio/ogg" {
		t.Fatalf("exported audio allowlist mime=%q ok=%v", mime, ok)
	}
	if _, err := reviewedHostAudioInputsForTurn("root", "turn", "user", []agent.MessageAttachment{{
		FileName: "clip.wav", MimeType: "audio/wav", Data: "!!!invalid!!!",
	}}); err == nil {
		t.Fatal("invalid audio alphabet must fail closed")
	}
}

func reviewedHostTestWAV() []byte {
	pcm := []byte{0, 0, 16, 0, 0, 0, 240, 255}
	out := make([]byte, 44+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(pcm)))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], 1)
	binary.LittleEndian.PutUint32(out[24:28], 16000)
	binary.LittleEndian.PutUint32(out[28:32], 32000)
	binary.LittleEndian.PutUint16(out[32:34], 2)
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm)))
	copy(out[44:], pcm)
	return out
}
