package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

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
	gotText string
	wav     []byte
	err     error
}

func (f *fakeDeviceSynthesizer) SynthesizeText(text string) ([]byte, error) {
	f.gotText = text
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

func TestAttachDeviceVoicePayloadAttaches16kHzWAV(t *testing.T) {
	longText := "**你好**，这是给硬件宠物的回复。" + strings.Repeat("这是一段很长的回复内容，", 30)
	synth := &fakeDeviceSynthesizer{wav: buildDeviceTestWAV(24000, 24000)} // 1s of 24kHz silence
	resp := &IMAgentResponse{Text: longText}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")

	if resp.VoiceData == "" || resp.VoiceMimeType != "audio/wav" || resp.VoiceFileName != "reply.wav" {
		t.Fatalf("voice fields = %q %q %q", resp.VoiceFileName, resp.VoiceMimeType, resp.VoiceData)
	}
	if got := utf8.RuneCountInString(synth.gotText); got > deviceVoiceMaxRunes {
		t.Fatalf("synthesized text length = %d runes, want <= %d", got, deviceVoiceMaxRunes)
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.VoiceData)
	if err != nil {
		t.Fatalf("VoiceData is not valid base64: %v", err)
	}
	if len(decoded) > deviceVoiceMaxWAVBytes {
		t.Fatalf("decoded WAV = %d bytes, exceeds %d", len(decoded), deviceVoiceMaxWAVBytes)
	}
	if len(decoded) < 44 || string(decoded[0:4]) != "RIFF" || string(decoded[8:12]) != "WAVE" {
		t.Fatalf("decoded payload is not a WAV file")
	}
	if rate := binary.LittleEndian.Uint32(decoded[24:28]); rate != 16000 {
		t.Fatalf("sample rate = %d, want 16000 (ESP32 only accepts 16kHz)", rate)
	}
	if channels := binary.LittleEndian.Uint16(decoded[22:24]); channels != 1 {
		t.Fatalf("channels = %d, want 1", channels)
	}
	if bits := binary.LittleEndian.Uint16(decoded[34:36]); bits != 16 {
		t.Fatalf("bits per sample = %d, want 16", bits)
	}
}

func TestAttachDeviceVoicePayloadShortTextStillGetsVoice(t *testing.T) {
	// The device branch has no minimum-length gate: even a two-character reply
	// must carry voice because the pet has no usable display.
	synth := &fakeDeviceSynthesizer{wav: buildDeviceTestWAV(24000, 2400)}
	resp := &IMAgentResponse{Text: "好的"}
	attachDeviceVoicePayload(synth, resp, "thirdparty")
	if resp.VoiceData == "" {
		t.Fatal("short device reply must still carry voice")
	}
}

func TestAttachDeviceVoicePayloadDegradesOnSynthesizeFailure(t *testing.T) {
	synth := &fakeDeviceSynthesizer{err: fmt.Errorf("model not loaded")}
	resp := &IMAgentResponse{Text: "你好"}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")
	if resp.VoiceData != "" || resp.VoiceFileName != "" || resp.VoiceMimeType != "" {
		t.Fatalf("voice fields must stay empty on failure: %#v", resp)
	}
}

func TestAttachDeviceVoicePayloadDropsOversizedAudio(t *testing.T) {
	// 16 seconds of 24kHz mono -> ~512KB after resample, over the firmware cap.
	synth := &fakeDeviceSynthesizer{wav: buildDeviceTestWAV(24000, 24000*16)}
	resp := &IMAgentResponse{Text: "你好"}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")
	if resp.VoiceData != "" {
		t.Fatalf("oversized audio must be dropped, got %d base64 bytes", len(resp.VoiceData))
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

func TestAttachDeviceVoicePayloadRetriesShorterTextWhenOversized(t *testing.T) {
	// At this rate the 40-rune cut exceeds the 240KB cap after the 16kHz
	// resample while the 25-rune retry fits, so the voice must survive.
	synth := &runeSizedSynthesizer{samplesPerRune: 6000}
	resp := &IMAgentResponse{Text: strings.Repeat("好", 80)}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")
	if resp.VoiceData == "" {
		t.Fatal("oversized first attempt must retry with shorter text, not drop the voice")
	}
	if len(synth.texts) != 2 {
		t.Fatalf("synthesize calls = %d, want 2 (initial + shorter retry)", len(synth.texts))
	}
	if got := utf8.RuneCountInString(synth.texts[1]); got > deviceVoiceRetryMaxRunes {
		t.Fatalf("retry text length = %d runes, want <= %d", got, deviceVoiceRetryMaxRunes)
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.VoiceData)
	if err != nil {
		t.Fatalf("VoiceData is not valid base64: %v", err)
	}
	if len(decoded) > deviceVoiceMaxWAVBytes {
		t.Fatalf("decoded WAV = %d bytes, exceeds %d", len(decoded), deviceVoiceMaxWAVBytes)
	}
}

func TestAttachDeviceVoicePayloadSkipsEmptySpeech(t *testing.T) {
	synth := &fakeDeviceSynthesizer{wav: buildDeviceTestWAV(24000, 2400)}
	resp := &IMAgentResponse{Text: "```\nfmt.Println(1)\n```"}
	attachDeviceVoicePayload(synth, resp, "thirdparty:pet-01")
	if resp.VoiceData != "" {
		t.Fatal("text that cleans to empty speech must not produce voice")
	}
	if synth.gotText != "" {
		t.Fatalf("synthesizer must not be called for empty speech, got %q", synth.gotText)
	}
}
