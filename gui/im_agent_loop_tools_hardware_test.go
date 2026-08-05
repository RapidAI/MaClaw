package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func hardwareToolDefinition(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": name,
		},
	}
}

func TestFilterToolsForHardwareAutoSpeech(t *testing.T) {
	tools := []map[string]interface{}{
		hardwareToolDefinition("web_search"),
		hardwareToolDefinition("tts"),
		hardwareToolDefinition("read_file"),
	}

	for _, platform := range []string{"thirdparty", "thirdparty:esp32-bread", " ThirdParty:PET "} {
		got := filterToolsForHardwareAutoSpeech(tools, platform)
		if len(got) != 2 {
			t.Fatalf("platform %q returned %d tools, want 2", platform, len(got))
		}
		for _, def := range got {
			if tool.ExtractToolName(def) == "tts" {
				t.Fatalf("platform %q still exposes tts", platform)
			}
		}
	}

	if got := filterToolsForHardwareAutoSpeech(tools, "desktop"); len(got) != len(tools) {
		t.Fatalf("desktop returned %d tools, want %d", len(got), len(tools))
	}
	if got := filterToolsForHardwareAutoSpeech(tools, "weixin"); len(got) != len(tools) {
		t.Fatalf("weixin returned %d tools, want %d", len(got), len(tools))
	}
}

func TestFilterToolsForHardwareAutoSpeechRemovesEveryTTSDefinition(t *testing.T) {
	tools := []map[string]interface{}{
		hardwareToolDefinition("TTS"),
		hardwareToolDefinition("tts"),
		hardwareToolDefinition("device_action"),
	}
	got := filterToolsForHardwareAutoSpeech(tools, "thirdparty:esp32")
	if len(got) != 1 || tool.ExtractToolName(got[0]) != "device_action" {
		t.Fatalf("unexpected filtered tools: %#v", got)
	}
}
