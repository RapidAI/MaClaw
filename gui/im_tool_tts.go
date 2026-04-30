package main

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// toolTTS synthesizes speech from text and delivers it as a voice message.
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

	ogg, wav, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, text, 300)

	// Desktop panel: emit WAV for frontend playback (reuse the WAV from synthesis).
	if h.app.ctx != nil && wav != nil {
		runtime.EventsEmit(h.app.ctx, "tts:audio", base64.StdEncoding.EncodeToString(wav))
	}

	if ogg != nil {
		log.Printf("[tts-tool] OGG Opus: %d bytes", len(ogg))
		return fmt.Sprintf("[voice_base64|voice.ogg|audio/ogg]%s", base64.StdEncoding.EncodeToString(ogg))
	}
	if wav != nil {
		// Opus failed, fall back to WAV.
		log.Printf("[tts-tool] Opus failed (%v), using WAV: %d bytes", err, len(wav))
		return fmt.Sprintf("[voice_base64|voice.wav|audio/wav]%s", base64.StdEncoding.EncodeToString(wav))
	}

	log.Printf("[tts-tool] error: %v", err)
	return fmt.Sprintf("语音合成失败: %v", err)
}
