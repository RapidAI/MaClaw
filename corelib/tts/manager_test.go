package tts

import "testing"

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
