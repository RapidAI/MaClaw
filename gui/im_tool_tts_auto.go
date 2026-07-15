package main

import (
	"encoding/base64"
	"log"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

// maybeAttachVoiceSummary generates a voice summary of the agent response
// and attaches it to resp for IM channels.
func (h *IMMessageHandler) maybeAttachVoiceSummary(resp *IMAgentResponse, platform string, voiceReply bool) {
	if resp == nil {
		log.Printf("[tts-auto] skip reason=nil_response platform=%s voice_reply=%v", platform, voiceReply)
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
