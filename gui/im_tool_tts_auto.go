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
	if err != nil || !cfg.TTSEnabled || !cfg.TTSAutoVoiceSummary {
		return
	}
	if utf8.RuneCountInString(resp.Text) < 20 {
		return
	}

	ogg, wav, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, resp.Text, 300)
	if ogg != nil {
		resp.VoiceData = base64.StdEncoding.EncodeToString(ogg)
		resp.VoiceFileName = "voice.ogg"
		resp.VoiceMimeType = "audio/ogg"
	} else if wav != nil {
		// Opus failed, fall back to WAV (企微/QQ still get voice bubble).
		resp.VoiceData = base64.StdEncoding.EncodeToString(wav)
		resp.VoiceFileName = "voice.wav"
		resp.VoiceMimeType = "audio/wav"
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
		"qqbot_local", "telegram_local", "weixin":
		return true
	}
	return false
}
