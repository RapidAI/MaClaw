package main

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// toolTTS synthesizes speech from text and delivers it as a voice message.
//
// Delivery: returns [voice_base64|...] protocol marker which the agent loop
// intercepts and sets on resp.VoiceData. The Hub IM router (or local gateways)
// then sends it as a native voice message.
// Desktop panel also receives a tts:audio event for frontend playback.
func (h *IMMessageHandler) toolTTS(args map[string]interface{}) string {
	text, _ := args["text"].(string)
	if text == "" {
		return "缺少 text 参数"
	}

	if h.app == nil || h.app.ttsManager == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS 并等待模型下载完成。"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return "语音合成未启用。请在设置 → 语音合成中开启。"
	}

	opus, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, text, 300)
	if err != nil {
		log.Printf("[tts-tool] error: %v", err)
		return fmt.Sprintf("语音合成失败: %v", err)
	}

	// Desktop panel: also emit WAV for frontend playback.
	if h.app.ctx != nil {
		if wav, err2 := h.app.ttsManager.SynthesizeText(tts.CleanForSpeech(text)); err2 == nil {
			runtime.EventsEmit(h.app.ctx, "tts:audio", base64.StdEncoding.EncodeToString(wav))
		}
	}

	b64 := base64.StdEncoding.EncodeToString(opus)
	log.Printf("[tts-tool] OGG Opus: %d bytes", len(opus))
	return fmt.Sprintf("[voice_base64|voice.ogg|audio/ogg]%s", b64)
}
