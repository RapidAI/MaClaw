package main

import (
	"encoding/base64"
	"log"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

const (
	deviceVoicePartsArtifactMIME = "application/vnd.maclaw.voice-parts+json"
	// deviceVoiceMaxRunes bounds each hardware audio payload, not the complete
	// answer. Long answers are synthesized and queued as ordered parts.
	deviceVoiceMaxRunes = 40
	// deviceVoiceRetryMaxRunes is the shorter cut retried once when the first
	// synthesis still exceeds deviceVoiceMaxWAVBytes.
	deviceVoiceRetryMaxRunes = 25
	// deviceVoiceMaxWAVBytes is a safety net below the firmware's
	// maxDownloadBytes (256KB); oversized audio is dropped rather than sent.
	deviceVoiceMaxWAVBytes = 240 * 1024
)

// maybeAttachVoiceSummary generates a voice summary of the agent response
// and attaches it to resp for IM channels.
func (h *IMMessageHandler) maybeAttachVoiceSummary(resp *IMAgentResponse, platform string, voiceReply bool) {
	if resp == nil {
		log.Printf("[tts-auto] skip reason=nil_response platform=%s voice_reply=%v", platform, voiceReply)
		return
	}
	if isThirdPartyVoicePlatform(platform) {
		// Hardware pets have no readable screen, so voice is the primary
		// channel: even error replies are spoken when there is text to read
		// aloud (the IM-channel eligibility gate below intentionally still
		// skips errors).
		if resp.Text != "" && len(resp.VoiceParts) == 0 && resp.VoiceData == "" {
			h.attachDeviceVoiceReply(resp, platform)
		}
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
	if h.app == nil || h.app.ttsManager == nil {
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
	ogg, wav, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, speak, 0)
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
	if h.app == nil || h.app.ttsManager == nil {
		log.Printf("[tts-auto] device skip reason=tts_manager_nil platform=%s", platform)
		return
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		log.Printf("[tts-auto] device skip reason=tts_disabled_or_config_error platform=%s config_err=%v", platform, err)
		return
	}
	attachDeviceVoicePayload(h.app.ttsManager, resp, platform)
}

// attachDeviceVoicePayload synthesizes a short 16kHz mono WAV (the only audio
// format ESP32 firmware accepts) and attaches it to resp. Failures only log;
// resp.VoiceParts stays empty so the caller falls back to text-only delivery.
func attachDeviceVoicePayload(synth tts.TextSynthesizer, resp *IMAgentResponse, platform string) {
	clean := tts.CleanForSpeech(resp.Text)
	speechParts := tts.PrepareSpeechChunks(clean, 0, deviceVoiceMaxRunes)
	if len(speechParts) == 0 {
		log.Printf("[tts-auto] device skip reason=empty_after_clean platform=%s", platform)
		return
	}
	voiceParts := make([]IMVoicePart, 0, len(speechParts))
	for index := 0; index < len(speechParts); index++ {
		speak := speechParts[index]
		wav, err := synthesizeDeviceWAV(synth, speak, platform)
		if err != nil {
			return
		}
		if len(wav) > deviceVoiceMaxWAVBytes {
			// Preserve all content: split an unusually slow segment into smaller
			// segments and synthesize every resulting part instead of truncating
			// the retry to a prefix.
			retryParts := tts.PrepareSpeechChunks(speak, 0, deviceVoiceRetryMaxRunes)
			if len(retryParts) > 1 {
				log.Printf("[tts-auto] device split oversized segment platform=%s part=%d/%d bytes=%d subparts=%d", platform, index+1, len(speechParts), len(wav), len(retryParts))
				speechParts = append(speechParts[:index], append(retryParts, speechParts[index+1:]...)...)
				index--
				continue
			}
		}
		if len(wav) > deviceVoiceMaxWAVBytes {
			log.Printf("[tts-auto] device skip reason=wav_too_large platform=%s part=%d/%d bytes=%d", platform, index+1, len(speechParts), len(wav))
			return
		}
		voiceParts = append(voiceParts, IMVoicePart{
			Data:     base64.StdEncoding.EncodeToString(wav),
			FileName: "reply-" + strconv.Itoa(index+1) + ".wav",
			MimeType: "audio/wav",
		})
	}
	resp.VoiceParts = voiceParts
	log.Printf("[tts-auto] device attached voice parts=%d", len(voiceParts))
}

// synthesizeDeviceWAV synthesizes speak and converts it to 16kHz mono 16-bit
// WAV, the only audio format ESP32 firmware accepts.
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
