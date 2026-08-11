package tts

import "testing"

func TestIsSupportedTTSVoiceID(t *testing.T) {
	for _, voiceID := range SupportedTTSVoiceIDs {
		if !IsSupportedTTSVoiceID(voiceID) {
			t.Fatalf("IsSupportedTTSVoiceID(%q) = false, want true", voiceID)
		}
	}
	if IsSupportedTTSVoiceID("unknown") {
		t.Fatal("IsSupportedTTSVoiceID accepted an unknown voice")
	}
}

func TestNewManagerAlwaysUsesKokoroRuntime(t *testing.T) {
	mgr := NewManager("piper-xiao_ya-zh-fp32.gguf")
	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if mgr.kokoro == nil {
		t.Fatal("NewManager should create a Kokoro manager even for legacy Piper paths")
	}
	if mgr.Loaded() {
		t.Fatal("NewManager should not load any TTS model eagerly")
	}
}
