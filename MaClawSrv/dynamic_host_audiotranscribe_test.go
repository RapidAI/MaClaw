package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestSrvReviewedHostWAVEngineUsesHostASR(t *testing.T) {
	dataRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataRoot, "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "models", srvASRModelFilename), testGGUFModelBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	models := newSrvAIModelManager(dataRoot)
	fake := &fakeSrvASRTranscriber{text: "host transcript"}
	models.asrMgr = fake
	engine := srvReviewedHostWAVEngine{models: models}
	got, err := engine.TranscribeWAV(context.Background(), testWAVBytes())
	if err != nil || got != "host transcript" {
		t.Fatalf("transcribe=%q err=%v", got, err)
	}
	if len(fake.seen) != 1 {
		t.Fatalf("asr seen=%#v", fake.seen)
	}
	if !engine.Ready() {
		t.Fatal("model file must make the host engine ready")
	}
	wireSrvReviewedHostSpeechTranscriber(&agentservice.CoreAgentExecutor{}, &HTTPServer{aiModels: models})
	wireSrvReviewedHostSpeechTranscriber(nil, nil)
}

func TestSrvReviewedHostWAVEngineUnreadyWithoutModel(t *testing.T) {
	engine := srvReviewedHostWAVEngine{models: newSrvAIModelManager(t.TempDir())}
	if engine.Ready() {
		t.Fatal("missing ASR model must stay unready")
	}
	if _, err := engine.TranscribeWAV(context.Background(), testWAVBytes()); err == nil {
		t.Fatal("unready engine must fail closed")
	}
}
