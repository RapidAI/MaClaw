package main

import (
	"encoding/base64"
	"log"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

// maybeAttachVoiceSummary generates a voice summary of the agent response
// and attaches it to resp for IM channels.
func (h *IMMessageHandler) maybeAttachVoiceSummary(resp *IMAgentResponse, platform string) {
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
	if !cfg.TTSAutoVoiceSummary {
		return
	}
	if utf8.RuneCountInString(resp.Text) < 20 {
		return
	}

	ogg, wav, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, resp.Text, 300)
	voiceData, voiceName, voiceMime := selectTTSVoicePayload(platform, ogg, wav)
	if voiceData != nil {
		resp.VoiceData = base64.StdEncoding.EncodeToString(voiceData)
		resp.VoiceFileName = voiceName
		resp.VoiceMimeType = voiceMime
	} else {
		log.Printf("[tts-auto] error: %v", err)
		return
	}
	log.Printf("[tts-auto] attached voice: %s %d bytes", resp.VoiceFileName, len(resp.VoiceData))
}

// isIMPlatform returns true if the platform is an IM channel (not desktop).
func isIMPlatform(platform string) bool {
	switch platform {
	case "feishu", "wecom", "qqbot", "dingtalk", "telegram", "lansenger",
		"qqbot_local", "telegram_local", "weixin", "weixin_local", "lansenger_local":
		return true
	}
	return false
}

func shouldEmitDesktopTTSPlayback(platform string) bool {
	switch platform {
	case "", "desktop", "tui":
		return true
	default:
		return false
	}
}

func selectTTSVoicePayload(platform string, ogg, wav []byte) ([]byte, string, string) {
	switch platform {
	case "weixin", "weixin_local":
		if wav != nil {
			return wav, "voice.wav", "audio/wav"
		}
		if ogg != nil {
			return ogg, "voice.ogg", "audio/ogg"
		}
	case "telegram", "telegram_local", "qqbot", "qqbot_local":
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
