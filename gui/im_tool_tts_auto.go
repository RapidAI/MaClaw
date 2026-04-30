package main

import (
	"encoding/base64"
	"log"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

// maybeAttachVoiceSummary generates a voice summary of the agent response
// and attaches it to resp for IM channels. Called as a post-processing step
// after the agent loop completes.
//
// Skips if:
//   - Voice already attached by tts tool
//   - Not an IM platform
//   - TTS not enabled or auto-summary not enabled
//   - Response too short (< 20 runes) or has error
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

	opus, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, resp.Text, 300)
	if err != nil {
		log.Printf("[tts-auto] error: %v", err)
		return
	}

	resp.VoiceData = base64.StdEncoding.EncodeToString(opus)
	resp.VoiceFileName = "voice.ogg"
	resp.VoiceMimeType = "audio/ogg"
	log.Printf("[tts-auto] attached voice summary: %d bytes OGG", len(opus))
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
