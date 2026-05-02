package main

import "testing"

func TestIsIMPlatformIncludesLocalGateways(t *testing.T) {
	for _, platform := range []string{"weixin_local", "telegram_local", "qqbot_local", "lansenger_local"} {
		if !isIMPlatform(platform) {
			t.Fatalf("isIMPlatform(%q) = false, want true", platform)
		}
	}
}

func TestSelectTTSVoicePayloadIsPlatformAware(t *testing.T) {
	ogg := []byte("ogg")
	wav := []byte("wav")

	data, name, mime := selectTTSVoicePayload("weixin_local", ogg, wav)
	if string(data) != "wav" || name != "voice.wav" || mime != "audio/wav" {
		t.Fatalf("weixin payload = %q %q %q, want wav voice.wav audio/wav", data, name, mime)
	}

	data, name, mime = selectTTSVoicePayload("telegram_local", ogg, wav)
	if string(data) != "ogg" || name != "voice.ogg" || mime != "audio/ogg" {
		t.Fatalf("telegram payload = %q %q %q, want ogg voice.ogg audio/ogg", data, name, mime)
	}
}

func TestShouldEmitDesktopTTSPlaybackSkipsIMPlatforms(t *testing.T) {
	for _, platform := range []string{"weixin", "weixin_local", "telegram_local", "qqbot_local", "lansenger_local", "scheduler", "agentnet"} {
		if shouldEmitDesktopTTSPlayback(platform) {
			t.Fatalf("shouldEmitDesktopTTSPlayback(%q) = true, want false", platform)
		}
	}
}

func TestShouldEmitDesktopTTSPlaybackAllowsDesktopContexts(t *testing.T) {
	for _, platform := range []string{"", "desktop", "tui"} {
		if !shouldEmitDesktopTTSPlayback(platform) {
			t.Fatalf("shouldEmitDesktopTTSPlayback(%q) = false, want true", platform)
		}
	}
}
