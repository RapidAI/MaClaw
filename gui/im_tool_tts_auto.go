package main

import (
	"context"
	"encoding/base64"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

const (
	// Each part stays semantically natural for synthesis and is MP3-compressed
	// before delivery. The firmware advertises a 512 KiB per-object limit.
	// Smaller parts reduce time-to-first-speech after the result page appears.
	// The ESP32 reuses HTTPS connections, so the extra bounded requests are less
	// noticeable than waiting for a roughly 100-rune synthesis/encode operation.
	deviceVoiceChunkRunes   = 64
	deviceVoiceMaxPartBytes = 500 * 1024
	// Compatibility name retained for tests and callers predating MP3 delivery.
	deviceVoiceMaxRunes = deviceVoiceChunkRunes
)

// The MP3 encoder and the local synthesizer are both CPU/memory intensive.
// Hardware replies are serialized so concurrent device requests cannot run
// several complete long-form encodes at once and exhaust the GUI process.
var deviceVoiceEncodeMu sync.Mutex

// cleanDeviceReplyText removes narration-status wording that some models put
// anywhere in an otherwise useful answer. Speech is an automatic output side
// effect for the hardware pet, so completion wording is neither result content
// nor something the device should speak.
func cleanDeviceReplyText(text string) string {
	text = tts.StripInternalResponseMetadata(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && isDeviceSpeechCompletionLine(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && isDeviceSpeechCompletionLine(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		lines[i] = removeDeviceSpeechCompletionWording(line)
	}
	return trimDeviceReplyLeadingDecoration(strings.TrimSpace(strings.Join(lines, "\n")))
}

// trimDeviceReplyLeadingDecoration removes presentation-only emoji/dingbats
// from the beginning of a hardware answer. The compact ESP font can render
// unsupported variants as fragments such as "?I?", which looks like content.
func trimDeviceReplyLeadingDecoration(text string) string {
	text = strings.TrimSpace(text)
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		if r == utf8.RuneError {
			// A malformed leading UTF-8 byte is presentation debris to this
			// hardware surface. Leaving it in makes the ESP renderer emit a
			// literal question mark before the useful answer.
			if size > 0 {
				text = text[size:]
				continue
			}
			break
		}
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.In(r, unicode.Cf) ||
			r == '\ufeff' || r == '\u200d' || r == '\ufe0e' || r == '\ufe0f' ||
			(r >= 0x2300 && r <= 0x27ff) || r >= 0x1f000 {
			text = text[size:]
			continue
		}
		break
	}
	return strings.TrimSpace(text)
}

func removeDeviceSpeechCompletionWording(line string) string {
	original := line
	trimmedOriginal := strings.TrimSpace(original)
	removedAtEnd := false
	// Longest first so the shorter forms cannot leave polite prefixes behind.
	for _, phrase := range []string{
		"已为您语音播报完毕", "已为您语音播报完成",
		"已为您播报完毕", "已为您播报完成",
		"语音播报完毕", "语音播报完成",
		"已播报完毕", "已播报完成",
		"播报完毕", "播报完成",
	} {
		for _, ending := range []string{phrase, phrase + "。", phrase + "！", phrase + "!", phrase + "～", phrase + "~"} {
			if strings.HasSuffix(trimmedOriginal, ending) {
				removedAtEnd = true
			}
		}
		line = strings.ReplaceAll(line, "（"+phrase+"）", "")
		line = strings.ReplaceAll(line, "("+phrase+")", "")
		for _, punctuation := range []string{"，", ",", "：", ":", "；", ";", "、"} {
			line = strings.ReplaceAll(line, phrase+punctuation, "")
		}
		line = strings.ReplaceAll(line, phrase, "")
	}
	if line == original {
		return line
	}
	line = strings.ReplaceAll(line, "（）", "")
	line = strings.ReplaceAll(line, "()", "")
	line = strings.TrimSpace(line)
	line = strings.TrimLeftFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	if removedAtEnd {
		line = strings.TrimRightFunc(line, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
		})
	}
	return strings.TrimSpace(line)
}

func isDeviceSpeechCompletionLine(line string) bool {
	line = strings.TrimSpace(strings.Trim(line, "*_`#"))
	line = strings.TrimFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	line = strings.ReplaceAll(line, " ", "")
	switch line {
	case "播报完毕", "已播报完毕", "已为您播报完毕", "语音播报完毕",
		"播报完成", "已播报完成", "已为您播报完成", "语音播报完成":
		return true
	default:
		return false
	}
}

// maybeAttachVoiceSummary generates a voice summary of the agent response
// and attaches it to resp for IM channels.
func (h *IMMessageHandler) maybeAttachVoiceSummary(resp *IMAgentResponse, platform string, voiceReply bool) {
	if resp == nil {
		log.Printf("[tts-auto] skip reason=nil_response platform=%s voice_reply=%v", platform, voiceReply)
		return
	}
	if isThirdPartyVoicePlatform(platform) {
		// The gateway streams hardware speech after the Agent returns. Doing the
		// complete synthesis here blocks the final response path and leaves the
		// ESP32 showing "remote processing" until every chunk has encoded.
		log.Printf("[tts-auto] device speech deferred to gateway platform=%s text_runes=%d", platform, utf8.RuneCountInString(resp.Text))
		return
	}
	if resp.Error != "" || resp.Text == "" || resp.VoiceData != "" {
		log.Printf("[tts-auto] skip reason=response_not_eligible platform=%s voice_reply=%v err=%v text_len=%d has_voice=%v", platform, voiceReply, resp.Error != "", utf8.RuneCountInString(resp.Text), resp.VoiceData != "")
		return
	}
	if !isIMPlatform(platform) {
		log.Printf("[tts-auto] skip reason=not_im platform=%s voice_reply=%v", platform, voiceReply)
		return
	}
	if h.app == nil {
		log.Printf("[tts-auto] skip reason=tts_manager_nil platform=%s voice_reply=%v", platform, voiceReply)
		return
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		log.Printf("[tts-auto] skip reason=tts_disabled_or_config_error platform=%s voice_reply=%v config_err=%v", platform, voiceReply, err)
		return
	}
	if !voiceReply && !cfg.TTSAutoVoiceSummary {
		log.Printf("[tts-auto] skip reason=auto_summary_disabled platform=%s voice_reply=%v", platform, voiceReply)
		return
	}
	if !voiceReply && utf8.RuneCountInString(resp.Text) < 20 {
		log.Printf("[tts-auto] skip reason=text_too_short platform=%s voice_reply=%v text_len=%d", platform, voiceReply, utf8.RuneCountInString(resp.Text))
		return
	}
	manager := h.app.ttsManagerForSynthesis()
	if manager == nil {
		log.Printf("[tts-auto] skip reason=tts_manager_nil platform=%s voice_reply=%v", platform, voiceReply)
		return
	}

	// Auto-summary must stay short (IM latency / payload limits). Explicit tts tool
	// handles true long-form reading. Voice-input replies may be a bit longer.
	speak := resp.Text
	if !voiceReply {
		speak = tts.CapSpeechText(resp.Text, tts.AutoSpeechMaxRunes)
	} else {
		// Full long-form with a soft platform safety cap.
		speak = tts.CapSpeechText(resp.Text, tts.MaxLongFormSpeechRunes)
	}
	if speak == "" {
		log.Printf("[tts-auto] skip reason=empty_after_clean platform=%s voice_reply=%v", platform, voiceReply)
		return
	}
	ogg, wav, err := tts.SynthesizeVoiceOGG(manager, speak, 0)
	amr := synthesizeAMRForPlatform(platform, wav)
	voiceData, voiceName, voiceMime := selectTTSVoicePayload(platform, ogg, wav, amr)
	if voiceData != nil {
		resp.VoiceData = base64.StdEncoding.EncodeToString(voiceData)
		resp.VoiceFileName = voiceName
		resp.VoiceMimeType = voiceMime
	} else {
		if err != nil {
			log.Printf("[tts-auto] error: %v", err)
		} else {
			log.Printf("[tts-auto] no native voice payload for platform=%s", platform)
		}
		return
	}
	log.Printf("[tts-auto] attached voice: %s %d bytes", resp.VoiceFileName, len(resp.VoiceData))
}

// isIMPlatform returns true if the platform is an IM channel (not desktop).
func isIMPlatform(platform string) bool {
	return normalizeIMMessagePlatformKind(platform).IsIMChannel()
}

// isThirdPartyVoicePlatform reports whether the platform is a third-party
// hardware gateway (ESP32 pet). Kept narrow like isThirdPartyFileOriginPlatform:
// making thirdparty:* an IM platform globally would alter unrelated behavior.
func isThirdPartyVoicePlatform(platform string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	return platform == "thirdparty" || strings.HasPrefix(platform, "thirdparty:")
}

// attachDeviceVoiceReply attaches a voice reply for ESP32 hardware pets. Voice
// is the device's primary output modality, so every non-empty text reply gets
// one — independent of the IM auto-summary gates (TTSAutoVoiceSummary toggle,
// minimum text length). Any failure degrades silently: the text reply is still
// delivered without voice.
func (h *IMMessageHandler) attachDeviceVoiceReply(resp *IMAgentResponse, platform string) {
	if h.app == nil {
		log.Printf("[tts-auto] device skip reason=tts_manager_nil platform=%s", platform)
		return
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		log.Printf("[tts-auto] device skip reason=tts_disabled_or_config_error platform=%s config_err=%v", platform, err)
		return
	}
	if manager := h.app.ttsManagerForSynthesis(); manager != nil {
		attachDeviceVoicePayload(manager, resp, platform)
	} else {
		log.Printf("[tts-auto] device skip reason=tts_manager_nil platform=%s", platform)
	}
}

// attachDeviceVoicePayload synthesizes the complete cleaned reply as ordered,
// compressed speech parts. Failures only log; the text reply is still delivered.
func attachDeviceVoicePayload(synth tts.TextSynthesizer, resp *IMAgentResponse, platform string) {
	voiceParts := make([]IMVoicePart, 0)
	if !streamDeviceVoicePayload(synth, resp.Text, platform, func(part IMVoicePart, _, _ int) bool {
		voiceParts = append(voiceParts, part)
		return true
	}) {
		return
	}
	resp.VoiceParts = voiceParts
}

// streamDeviceVoicePayload synthesizes and publishes each semantic part as
// soon as it is ready. The caller sends the terminal text only after this
// function succeeds, preserving ordered complete playback without making the
// first audio wait for the entire long answer.
func streamDeviceVoicePayload(synth tts.TextSynthesizer, text, platform string, publish func(IMVoicePart, int, int) bool) bool {
	parts := tts.PrepareSpeechChunks(text, 0, deviceVoiceChunkRunes)
	if len(parts) == 0 {
		log.Printf("[tts-auto] device skip reason=empty_after_clean platform=%s", platform)
		return false
	}
	return streamPreparedDeviceVoicePayload(synth, parts, platform, publish)
}

// streamPreparedDeviceVoicePayload synthesizes a chunk plan whose exact size
// is already known to the caller. Device gateways use that cheap plan to open
// the terminal result surface before the expensive synthesis/MP3 work starts.
func streamPreparedDeviceVoicePayload(synth tts.TextSynthesizer, parts []string, platform string, publish func(IMVoicePart, int, int) bool) bool {
	if len(parts) == 0 {
		return false
	}
	started := time.Now()
	deviceVoiceEncodeMu.Lock()
	defer deviceVoiceEncodeMu.Unlock()
	totalBytes := 0
	for i, speak := range parts {
		chunkStarted := time.Now()
		wav, err := synthesizeDeviceWAV(synth, speak, platform)
		if err != nil {
			log.Printf("[tts-auto] device incomplete: chunk %d/%d synthesis failed platform=%s", i+1, len(parts), platform)
			return false
		}
		mp3, err := tts.EncodeWAVToMP3Context(context.Background(), wav)
		if err != nil {
			log.Printf("[tts-auto] device incomplete: chunk %d/%d mp3 encode failed platform=%s: %v", i+1, len(parts), platform, err)
			return false
		}
		if len(mp3) == 0 || len(mp3) > deviceVoiceMaxPartBytes {
			log.Printf("[tts-auto] device incomplete: chunk %d/%d mp3 bytes=%d platform=%s", i+1, len(parts), len(mp3), platform)
			return false
		}
		part := IMVoicePart{
			Data:     base64.StdEncoding.EncodeToString(mp3),
			FileName: "reply.mp3",
			MimeType: "audio/mpeg",
		}
		if publish == nil || !publish(part, i+1, len(parts)) {
			log.Printf("[tts-auto] device stream stopped: part=%d/%d platform=%s", i+1, len(parts), platform)
			return false
		}
		totalBytes += len(mp3)
		log.Printf("[tts-auto] device chunk ready: part=%d/%d runes=%d bytes=%d elapsed=%s platform=%s",
			i+1, len(parts), utf8.RuneCountInString(speak), len(mp3), time.Since(chunkStarted).Round(time.Millisecond), platform)
	}
	log.Printf("[tts-auto] device streamed complete voice: parts=%d text_runes=%d bytes=%d elapsed=%s platform=%s",
		len(parts), speechChunkRunes(parts), totalBytes,
		time.Since(started).Round(time.Millisecond), platform)
	return true
}

func speechChunkRunes(parts []string) int {
	total := 0
	for _, part := range parts {
		total += utf8.RuneCountInString(part)
	}
	return total
}

// synthesizeDeviceWAV synthesizes speak and normalizes it to 16kHz mono 16-bit
// WAV — the canonical in-process intermediate each voice part is MP3-encoded
// from before delivery to the ESP32 firmware.
func synthesizeDeviceWAV(synth tts.TextSynthesizer, speak, platform string) ([]byte, error) {
	wav, err := synth.SynthesizeText(speak)
	if err != nil {
		log.Printf("[tts-auto] device synthesize failed platform=%s: %v", platform, err)
		return nil, err
	}
	wav, err = audioconv.ToWAV(wav, audioconv.FormatWAV)
	if err != nil {
		log.Printf("[tts-auto] device wav convert failed platform=%s: %v", platform, err)
		return nil, err
	}
	return wav, nil
}

func shouldEmitDesktopTTSPlayback(platform string) bool {
	return normalizeIMMessagePlatformKind(platform).IsDesktopPlaybackTarget()
}

func selectTTSVoicePayload(platform string, ogg, wav, amr []byte) ([]byte, string, string) {
	platformKind := normalizeIMMessagePlatformKind(platform)
	switch {
	case platformKind.PrefersAMRVoice():
		if amr != nil {
			return amr, "voice.amr", "audio/amr"
		}
	case platformKind.PrefersWAVVoice():
		if wav != nil {
			return wav, "voice.wav", "audio/wav"
		}
	case platformKind.PrefersOGGVoice():
		if ogg != nil {
			return ogg, "voice.ogg", "audio/ogg"
		}
		if wav != nil {
			return wav, "voice.wav", "audio/wav"
		}
	default:
		if ogg != nil {
			return ogg, "voice.ogg", "audio/ogg"
		}
		if wav != nil {
			return wav, "voice.wav", "audio/wav"
		}
	}
	return nil, "", ""
}

func synthesizeAMRForPlatform(platform string, wav []byte) []byte {
	if !normalizeIMMessagePlatformKind(platform).PrefersAMRVoice() || wav == nil {
		return nil
	}
	amr, err := tts.EncodeWAVToAMR(wav)
	if err != nil {
		log.Printf("[tts-auto] AMR encode failed for platform=%s: %v", platform, err)
		return nil
	}
	return amr
}

func isVoiceInputMessage(msg IMUserMessage) bool {
	switch normalizeIMMediaKind(msg.MessageType) {
	case imMediaVoice, imMediaAudio:
		return true
	}
	for _, att := range msg.Attachments {
		switch normalizeIMMediaKind(att.Type) {
		case imMediaVoice, imMediaAudio:
			return true
		}
	}
	return false
}
