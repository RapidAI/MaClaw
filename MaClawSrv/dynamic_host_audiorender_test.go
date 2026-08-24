package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestSrvReviewedHostSpeechRendererUsesHostTTS(t *testing.T) {
	dataRoot := t.TempDir()
	seedReadyTTSModel(t, dataRoot)
	models := newSrvAIModelManager(dataRoot)
	fake := &fakeSrvTTSSynthesizer{wav: []byte("RIFF-host-tts")}
	models.ttsMgr = fake
	engine := srvReviewedHostSpeechRenderer{models: models}
	if !engine.Ready() {
		t.Fatal("model file must make the host renderer ready")
	}
	got, err := engine.RenderSpeech(context.Background(), "hello voice")
	if err != nil || string(got) != "RIFF-host-tts" {
		t.Fatalf("render=%q err=%v", got, err)
	}
	if len(fake.seen) != 1 {
		t.Fatalf("tts seen=%#v", fake.seen)
	}
	wireSrvReviewedHostSpeechSynthesizer(&agentservice.CoreAgentExecutor{}, &HTTPServer{aiModels: models})
	wireSrvReviewedHostSpeechSynthesizer(nil, nil)
}

func TestSrvReviewedHostSpeechRendererUnreadyWithoutModel(t *testing.T) {
	engine := srvReviewedHostSpeechRenderer{models: newSrvAIModelManager(t.TempDir())}
	if engine.Ready() {
		t.Fatal("missing TTS model must stay unready")
	}
	if _, err := engine.RenderSpeech(context.Background(), "hello"); err == nil {
		t.Fatal("unready engine must fail closed")
	}
}
