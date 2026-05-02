package main

import (
	"encoding/base64"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// maybeAttachVoiceSummary generates a voice summary of the agent response
// and attaches it to resp for IM channels.
func (h *IMMessageHandler) maybeAttachVoiceSummary(resp *IMAgentResponse, platform string) {
	if resp == nil || resp.Error != "" || resp.Text == "" || resp.VoiceData != "" {
		return
	}
	explicitVoiceRequest := wantsTTSPlayback(h.lastUserText)
	if !explicitVoiceRequest && !isIMPlatform(platform) {
		return
	}
	if h.app == nil || h.app.ttsManager == nil {
		return
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return
	}
	if !explicitVoiceRequest && !cfg.TTSAutoVoiceSummary {
		return
	}
	if !explicitVoiceRequest && utf8.RuneCountInString(resp.Text) < 20 {
		return
	}

	ogg, wav, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, resp.Text, 300)
	if explicitVoiceRequest && h.app.ctx != nil && wav != nil {
		// Desktop panel playback uses the WAV event; IM channels still receive
		// the voice bubble below.
		runtime.EventsEmit(h.app.ctx, "tts:audio", base64.StdEncoding.EncodeToString(wav))
	}
	if !isIMPlatform(platform) {
		if wav == nil {
			log.Printf("[tts-auto] desktop playback unavailable: %v", err)
		}
		return
	}
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

func wantsTTSPlayback(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	negations := []string{"不要读", "别读", "不用读", "不需要读", "不要念", "别念", "不用念", "不需要念", "别朗读", "不要朗读", "不用朗读", "不需要朗读"}
	for _, phrase := range negations {
		if strings.Contains(text, phrase) {
			return false
		}
	}
	if (strings.Contains(text, "读") || strings.Contains(text, "念")) && strings.Contains(text, "给我听") {
		return true
	}
	phrases := []string{
		"tts", "语音播报", "语音播放", "用语音", "转语音",
		"读给我听", "给我读", "读一下给我听", "读出来", "读出声", "读一遍", "朗读",
		"念给我听", "给我念", "念一下", "念出来", "念出声",
		"讲给我听", "说给我听", "读笑话", "读新闻给我听",
		"read aloud", "read it aloud", "read this to me", "say it out loud",
	}
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
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
