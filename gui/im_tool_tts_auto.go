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
	if resp == nil || resp.Error != "" || resp.Text == "" || resp.VoiceData != "" {
		return
	}
	if !isIMPlatform(platform) {
		return
	}
	if h.app == nil || h.app.ttsManager == nil {
		return
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return
	}
	if !voiceReply && !cfg.TTSAutoVoiceSummary {
		return
	}
	if !voiceReply && utf8.RuneCountInString(resp.Text) < 20 {
		return
	}

	ogg, wav, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, resp.Text, 300)
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
