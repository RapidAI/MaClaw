package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/weixin"
)

func TestIsIMPlatformIncludesLocalGateways(t *testing.T) {
	for _, platform := range []string{"weixin_local", "telegram_local", "qqbot_local", "lansenger_local"} {
		if !isIMPlatform(platform) {
			t.Fatalf("isIMPlatform(%q) = false, want true", platform)
		}
	}
}

func TestIsIMPlatformExcludesUnsupportedVoiceGateways(t *testing.T) {
	for _, platform := range []string{} {
		if isIMPlatform(platform) {
			t.Fatalf("isIMPlatform(%q) = true, want false for auto voice", platform)
		}
	}
}

func TestSelectTTSVoicePayloadIsPlatformAware(t *testing.T) {
	ogg := []byte("ogg")
	wav := []byte("wav")
	amr := []byte("#!AMR\n")

	data, name, mime := selectTTSVoicePayload("weixin_local", ogg, wav, amr)
	if string(data) != "wav" || name != "voice.wav" || mime != "audio/wav" {
		t.Fatalf("weixin payload = %q %q %q, want wav voice.wav audio/wav", data, name, mime)
	}

	data, name, mime = selectTTSVoicePayload("telegram_local", ogg, wav, amr)
	if string(data) != "ogg" || name != "voice.ogg" || mime != "audio/ogg" {
		t.Fatalf("telegram payload = %q %q %q, want ogg voice.ogg audio/ogg", data, name, mime)
	}

	data, name, mime = selectTTSVoicePayload("qqbot_local", ogg, wav, amr)
	if string(data) != "wav" || name != "voice.wav" || mime != "audio/wav" {
		t.Fatalf("qqbot payload = %q %q %q, want wav voice.wav audio/wav", data, name, mime)
	}

	data, name, mime = selectTTSVoicePayload("qqbot_local", ogg, nil, amr)
	if data != nil || name != "" || mime != "" {
		t.Fatalf("qqbot ogg fallback = %q %q %q, want no payload", data, name, mime)
	}

	data, name, mime = selectTTSVoicePayload("dingtalk", ogg, wav, amr)
	if string(data) != "ogg" || name != "voice.ogg" || mime != "audio/ogg" {
		t.Fatalf("dingtalk payload = %q %q %q, want ogg voice.ogg audio/ogg", data, name, mime)
	}

	data, name, mime = selectTTSVoicePayload("wecom", ogg, wav, amr)
	if string(data) != string(amr) || name != "voice.amr" || mime != "audio/amr" {
		t.Fatalf("wecom payload = %q %q %q, want amr voice.amr audio/amr", data, name, mime)
	}

	data, name, mime = selectTTSVoicePayload("feishu", ogg, wav, amr)
	if string(data) != "ogg" || name != "voice.ogg" || mime != "audio/ogg" {
		t.Fatalf("feishu payload = %q %q %q, want ogg voice.ogg audio/ogg", data, name, mime)
	}
}

func TestShouldEmitDesktopTTSPlaybackSkipsIMPlatforms(t *testing.T) {
	for _, platform := range []string{"weixin", "weixin_local", "telegram_local", "qqbot_local", "lansenger_local", "scheduler"} {
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

func TestIsVoiceInputMessageUsesStructuralModality(t *testing.T) {
	if !isVoiceInputMessage(IMUserMessage{MessageType: "voice"}) {
		t.Fatal("voice message type was not recognized")
	}
	if !isVoiceInputMessage(IMUserMessage{Attachments: []MessageAttachment{{Type: "audio"}}}) {
		t.Fatal("audio attachment was not recognized")
	}
	if isVoiceInputMessage(IMUserMessage{MessageType: "text", Text: "发我一段语音"}) {
		t.Fatal("text content must not be treated as a voice modality signal")
	}
}

func TestPrepareWeixinPlayableVoiceFileConvertsWAVToMP3(t *testing.T) {
	original := weixinEncodeWAVToMP3
	defer func() { weixinEncodeWAVToMP3 = original }()
	weixinEncodeWAVToMP3 = func(ctx context.Context, wav []byte) ([]byte, error) {
		if string(wav) != "RIFFxxxxWAVE" {
			t.Fatalf("converter input = %q", wav)
		}
		return []byte("ID3mp3"), nil
	}

	got, err := prepareWeixinPlayableVoiceFile(context.Background(), "voice.wav", []byte("RIFFxxxxWAVE"))
	if err != nil {
		t.Fatalf("prepareWeixinPlayableVoiceFile: %v", err)
	}
	if string(got.data) != "ID3mp3" || got.name != "voice.mp3" || got.mime != "audio/mpeg" || !got.converted {
		t.Fatalf("fallback = %#v, want converted mp3", got)
	}
}

func TestPrepareWeixinPlayableVoiceFileKeepsMP3(t *testing.T) {
	got, err := prepareWeixinPlayableVoiceFile(context.Background(), "already.mp3", []byte("ID3payload"))
	if err != nil {
		t.Fatalf("prepareWeixinPlayableVoiceFile(mp3): %v", err)
	}
	if string(got.data) != "ID3payload" || got.name != "voice.mp3" || got.mime != "audio/mpeg" || got.converted {
		t.Fatalf("fallback = %#v, want passthrough mp3", got)
	}
}

func TestWeixinNativeVoiceExperimentVariantsDisabledByDefault(t *testing.T) {
	t.Setenv("MACLAW_WEIXIN_VOICE_EXPERIMENTS", "")
	if got := weixinNativeVoiceExperimentVariants(); len(got) != 0 {
		t.Fatalf("variants = %#v, want disabled", got)
	}
}

func TestWeixinNativeVoiceExperimentVariantsParsesAllowList(t *testing.T) {
	t.Setenv("MACLAW_WEIXIN_VOICE_EXPERIMENTS", "upload_param_encrypt0,raw_aes_encrypt0,bad,upload_param_encrypt0")
	got := weixinNativeVoiceExperimentVariants()
	want := []string{"upload_param_encrypt0", "raw_aes_encrypt0"}
	if len(got) != len(want) {
		t.Fatalf("variants = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("variants = %#v, want %#v", got, want)
		}
	}
}

func TestEchoInboundVoiceForDiagnosticsDisabledByDefault(t *testing.T) {
	t.Setenv("MACLAW_WEIXIN_ECHO_INBOUND_VOICE", "")
	m := &weixinGatewayManager{}
	if got := m.echoInboundVoiceForDiagnostics(context.Background(), nil, weixin.IncomingMessage{MediaType: "voice", MediaData: []byte("x")}, "ctx"); got {
		t.Fatal("echoInboundVoiceForDiagnostics returned true while disabled")
	}
}
