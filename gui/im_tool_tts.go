package main

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// toolTTS synthesizes speech from text and delivers it as a voice message.
// On the desktop panel, synthesis runs asynchronously — the tool returns
// immediately and audio is pushed to the frontend via "tts:audio" event
// once ready. On IM channels, synthesis is synchronous because the voice
// payload must be returned inline for delivery.
func (h *IMMessageHandler) toolTTS(args map[string]interface{}) string {
	text, _ := args["text"].(string)
	if text == "" {
		return "缺少 text 参数"
	}
	if h.app == nil || h.app.ttsManager == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS，并等待模型下载完成。"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return "语音合成未启用。请在设置 → 语音合成中开启。"
	}

	platform := ""
	if h.currentLoopCtx != nil {
		platform = h.currentLoopCtx.Platform
	}

	// Desktop panel: async synthesis — return immediately, push audio when ready.
	if shouldEmitDesktopTTSPlayback(platform) {
		app := h.app
		mgr := app.ttsManager // capture before goroutine to avoid nil race
		go func() {
			_, wav, synthErr := tts.SynthesizeVoiceOGG(mgr, text, 300)
			if synthErr != nil {
				log.Printf("[tts-tool] async synthesis error: %v", synthErr)
				return
			}
			if wav != nil && app.ctx != nil {
				runtime.EventsEmit(app.ctx, "tts:audio", base64.StdEncoding.EncodeToString(wav))
			}
		}()
		return "Voice message is being generated and will play shortly."
	}

	// IM channels: synchronous synthesis — voice payload returned inline.
	ogg, wav, err := tts.SynthesizeVoiceOGG(h.app.ttsManager, text, 300)
	amr := synthesizeAMRForPlatform(platform, wav)
	voiceData, voiceName, voiceMime := selectTTSVoicePayload(platform, ogg, wav, amr)
	if voiceData != nil {
		log.Printf("[tts-tool] voice payload: %s %d bytes", voiceName, len(voiceData))
		return fmt.Sprintf("[voice_base64|%s|%s]%s", voiceName, voiceMime, base64.StdEncoding.EncodeToString(voiceData))
	}
	if err == nil {
		log.Printf("[tts-tool] no native voice payload for platform=%s", platform)
		return "当前通道不支持原生语音消息"
	}

	log.Printf("[tts-tool] error: %v", err)
	return fmt.Sprintf("语音合成失败: %v", err)
}
