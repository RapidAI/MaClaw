package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/audioformat"
	"github.com/RapidAI/CodeClaw/corelib/tts"
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

func TestTTSAcceptsRuntimeOwnerForPlatformIsolation(t *testing.T) {
	if !toolAcceptsRuntimePolicyOwnerArg("tts") {
		t.Fatal("tts must accept runtime owner so platform selection stays tab-scoped")
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
	original := weixinPreparePlayableVoiceMP3
	defer func() { weixinPreparePlayableVoiceMP3 = original }()
	weixinPreparePlayableVoiceMP3 = func(ctx context.Context, name string, wav []byte) (tts.PlayableVoiceFile, error) {
		if name != "voice.wav" {
			t.Fatalf("converter name = %q", name)
		}
		if string(wav) != "RIFFxxxxWAVE" {
			t.Fatalf("converter input = %q", wav)
		}
		return tts.PlayableVoiceFile{Data: []byte("ID3mp3"), Name: "voice.mp3", MIME: "audio/mpeg", Converted: true}, nil
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
	mp3Data := append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}, []byte{0xff, 0xfb, 0x90, 0x64}...)
	got, err := prepareWeixinPlayableVoiceFile(context.Background(), "already.mp3", mp3Data)
	if err != nil {
		t.Fatalf("prepareWeixinPlayableVoiceFile(mp3): %v", err)
	}
	if string(got.data) != string(mp3Data) || got.name != "voice.mp3" || got.mime != "audio/mpeg" || got.converted {
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

type fakeDeviceSynthesizer struct {
	texts []string
	wav   []byte
	err   error
}

func (f *fakeDeviceSynthesizer) SynthesizeText(text string) ([]byte, error) {
	f.texts = append(f.texts, text)
	return f.wav, f.err
}

// buildDeviceTestWAV builds a minimal mono 16-bit PCM WAV at the given sample
// rate with all-zero samples (Kokoro emits 24kHz; tests use it to prove the
// 16kHz resample path).
func buildDeviceTestWAV(sampleRate, samples int) []byte {
	dataSize := samples * 2
	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:], 1) // mono
	binary.LittleEndian.PutUint32(buf[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataSize))
	return buf
}

func TestIsThirdPartyVoicePlatform(t *testing.T) {
	for _, platform := range []string{"thirdparty", "thirdparty:pet-01", "ThirdParty:PET"} {
		if !isThirdPartyVoicePlatform(platform) {
			t.Fatalf("isThirdPartyVoicePlatform(%q) = false, want true", platform)
		}
	}
	for _, platform := range []string{"telegram", "weixin_local", "desktop", ""} {
		if isThirdPartyVoicePlatform(platform) {
			t.Fatalf("isThirdPartyVoicePlatform(%q) = true, want false", platform)
		}
	}
}

func TestCleanDeviceReplyTextStripsInternalMetadataAnywhere(t *testing.T) {
	input := "北京：轻霾，注意补水。\nRoute task: vision\nNO AUX/ROUTE — STAYED ON PRIMARY..."
	want := "北京：轻霾，注意补水。"
	if got := cleanDeviceReplyText(input); got != want {
		t.Fatalf("cleanDeviceReplyText() = %q, want %q", got, want)
	}
}

func TestAttachDeviceVoicePayloadAttachesMP3Parts(t *testing.T) {
	longText := "**你好**，这是给硬件宠物的回复。" + strings.Repeat("这是一段很长的回复内容，", 30)
	synth := &fakeDeviceSynthesizer{wav: buildDeviceTestWAV(24000, 24000)} // 1s of 24kHz silence
	resp := &IMAgentResponse{Text: longText}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")

	if len(resp.VoiceParts) < 2 {
		t.Fatalf("voice parts = %d, want multiple ordered parts", len(resp.VoiceParts))
	}
	if resp.VoiceData != "" || resp.VoiceFileName != "" || resp.VoiceMimeType != "" {
		t.Fatalf("hardware response must use only voice_parts: %#v", resp)
	}
	if len(synth.texts) != len(resp.VoiceParts) {
		t.Fatalf("synthesize calls=%d voice parts=%d", len(synth.texts), len(resp.VoiceParts))
	}
	for i, part := range resp.VoiceParts {
		if got := utf8.RuneCountInString(synth.texts[i]); got > deviceVoiceMaxRunes {
			t.Fatalf("part %d synthesized text length = %d runes, want <= %d", i, got, deviceVoiceMaxRunes)
		}
		decoded, err := base64.StdEncoding.DecodeString(part.Data)
		if err != nil {
			t.Fatalf("part %d data is not valid base64: %v", i, err)
		}
		if len(decoded) > deviceVoiceMaxPartBytes {
			t.Fatalf("part %d MP3 = %d bytes, exceeds %d", i, len(decoded), deviceVoiceMaxPartBytes)
		}
		// Device voice parts ride the wire as MP3 (16kHz mono WAV is only the
		// in-process intermediate fed to the encoder).
		if !audioformat.LooksLikeMP3(decoded) {
			t.Fatalf("part %d payload is not MP3", i)
		}
		if part.MimeType != "audio/mpeg" || !strings.HasSuffix(part.FileName, ".mp3") {
			t.Fatalf("part %d file = %q %q, want .mp3 audio/mpeg", i, part.FileName, part.MimeType)
		}
	}
}

func TestAttachDeviceVoicePayloadShortTextStillGetsVoice(t *testing.T) {
	// The device branch has no minimum-length gate: even a two-character reply
	// must carry voice because the pet has no usable display.
	synth := &fakeDeviceSynthesizer{wav: buildDeviceTestWAV(24000, 2400)}
	resp := &IMAgentResponse{Text: "好的"}
	attachDeviceVoicePayload(synth, resp, "thirdparty")
	if len(resp.VoiceParts) != 1 {
		t.Fatal("short device reply must still carry voice")
	}
	if resp.VoiceData != "" {
		t.Fatal("short replies must use the new voice_parts protocol")
	}
}

func TestAttachDeviceVoicePayloadDegradesOnSynthesizeFailure(t *testing.T) {
	synth := &fakeDeviceSynthesizer{err: fmt.Errorf("model not loaded")}
	resp := &IMAgentResponse{Text: "你好"}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")
	if len(resp.VoiceParts) != 0 || resp.VoiceData != "" || resp.VoiceFileName != "" || resp.VoiceMimeType != "" {
		t.Fatalf("voice fields must stay empty on failure: %#v", resp)
	}
}

func TestAttachDeviceVoicePayloadDropsOversizedAudio(t *testing.T) {
	// A single speech chunk whose encoded MP3 exceeds the firmware part cap
	// aborts the whole voice reply: partial audio must never reach the device.
	// 2 runes at this rate -> 40s of audio -> ~640KB MP3 (> 500KB cap).
	synth := &runeSizedSynthesizer{samplesPerRune: 480000}
	resp := &IMAgentResponse{Text: "你好"}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")
	if len(resp.VoiceParts) != 0 {
		t.Fatalf("oversized indivisible audio must be dropped, got %d parts", len(resp.VoiceParts))
	}
}

// runeSizedSynthesizer returns a 24kHz WAV whose duration scales with the
// input length, mimicking how longer Kokoro speech yields larger audio.
type runeSizedSynthesizer struct {
	samplesPerRune int
	texts          []string
}

func (f *runeSizedSynthesizer) SynthesizeText(text string) ([]byte, error) {
	f.texts = append(f.texts, text)
	return buildDeviceTestWAV(24000, utf8.RuneCountInString(text)*f.samplesPerRune), nil
}

func TestAttachDeviceVoicePayloadChunksLongReplyWithoutLosingContent(t *testing.T) {
	// An 80-rune reply splits into 64+16 rune chunks. Under the MP3 wire
	// protocol each chunk encodes well under the part cap, so no further
	// splitting happens; every chunk must survive, in order.
	synth := &runeSizedSynthesizer{samplesPerRune: 6000}
	input := strings.Repeat("好", 80)
	resp := &IMAgentResponse{Text: input}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")
	if len(resp.VoiceParts) != 2 {
		t.Fatalf("long reply must chunk into 2 voice parts, got %d", len(resp.VoiceParts))
	}
	if got := strings.Join(synth.texts, ""); got != input {
		t.Fatalf("synthesized chunks lost content: runes=%d texts=%q", utf8.RuneCountInString(got), synth.texts)
	}
	for i, part := range resp.VoiceParts {
		decoded, err := base64.StdEncoding.DecodeString(part.Data)
		if err != nil || len(decoded) > deviceVoiceMaxPartBytes {
			t.Fatalf("part %d invalid or oversized: bytes=%d err=%v", i, len(decoded), err)
		}
		if !audioformat.LooksLikeMP3(decoded) {
			t.Fatalf("part %d payload is not MP3", i)
		}
	}
}

func TestAttachDeviceVoicePayloadSkipsEmptySpeech(t *testing.T) {
	synth := &fakeDeviceSynthesizer{wav: buildDeviceTestWAV(24000, 2400)}
	resp := &IMAgentResponse{Text: "```\nfmt.Println(1)\n```"}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")
	if len(resp.VoiceParts) != 0 {
		t.Fatal("text that cleans to empty speech must not produce voice")
	}
	if len(synth.texts) != 0 {
		t.Fatalf("synthesizer must not be called for empty speech, got %q", synth.texts)
	}
}
