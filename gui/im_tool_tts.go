package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tts"
)

// toolTTS synthesizes speech from text and delivers it as a voice message.
// Long text is split into semantic chunks (sentence/paragraph aware) so Kokoro
// does not silently truncate mid-broadcast.
// On the desktop panel, synthesis runs asynchronously — the tool returns
// immediately and each chunk is pushed via "tts:audio" for sequential playback.
// On IM channels, synthesis is synchronous: chunks are concatenated into one voice payload.
func (h *IMMessageHandler) toolTTS(args map[string]interface{}) string {
	text, _ := args["text"].(string)
	if text == "" {
		return "缺少 text 参数"
	}
	if h.app == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS，并等待模型下载完成。"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return "语音合成未启用。请在设置 → 语音合成中开启。"
	}
	manager := h.app.ttsManagerForSynthesis()
	if manager == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS，并等待模型下载完成。"
	}

	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "tts failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	platform := consumeRuntimePlatformFromToolArgs(args)
	if platform == "" {
		platform = h.runtimePlatformForOwnerOrCurrent(ownerID, hasRuntimeOwner)
	}

	// Single clean + soft cap + semantic chunk (avoids CapSpeechText+Split double-clean).
	chunks := tts.PrepareSpeechChunks(text, tts.MaxLongFormSpeechRunes, 0)
	if len(chunks) == 0 {
		return "文本清理后为空，无法合成语音"
	}

	// Desktop panel: async multi-chunk synthesis — queue each segment for playback.
	// Serialize with ttsSpeakMu so concurrent tts calls don't interleave audio chunks.
	if shouldEmitDesktopTTSPlayback(platform) {
		app := h.app
		mgr := manager // capture before goroutine to avoid a voice-change race
		chunkCopy := append([]string(nil), chunks...)
		go func() {
			ttsSpeakMu.Lock()
			defer ttsSpeakMu.Unlock()
			ok := 0
			for i, part := range chunkCopy {
				wav, synthErr := mgr.SynthesizeText(part)
				if synthErr != nil {
					// Continue remaining segments so one bad chunk does not kill the whole read.
					log.Printf("[tts-tool] async synthesis error chunk %d/%d: %v", i+1, len(chunkCopy), synthErr)
					continue
				}
				if len(wav) > 0 && app.ctx != nil {
					app.emitEvent("tts:audio", base64.StdEncoding.EncodeToString(wav))
					ok++
				}
			}
			log.Printf("[tts-tool] desktop long-form speech done: %d/%d chunks ok", ok, len(chunkCopy))
		}()
		if len(chunks) == 1 {
			return "Voice message is being generated and will play shortly."
		}
		return fmt.Sprintf("长文已按语义分成 %d 段，正在依次朗读。", len(chunks))
	}

	// IM channels: reuse pre-split parts (no second SplitSpeechChunks pass).
	wav, nChunks, err := tts.SynthesizeSpeechParts(manager, chunks)
	if err != nil {
		log.Printf("[tts-tool] error: %v", err)
		return fmt.Sprintf("语音合成失败: %v", err)
	}
	ogg, oggErr := tts.EncodeWAVToOpus(wav)
	if oggErr != nil {
		// WAV fallback is fine for platforms that accept it.
		log.Printf("[tts-tool] opus encode failed, trying wav/amr: %v", oggErr)
	}
	amr := synthesizeAMRForPlatform(platform, wav)
	voiceData, voiceName, voiceMime := selectTTSVoicePayload(platform, ogg, wav, amr)
	if voiceData != nil {
		log.Printf("[tts-tool] voice payload: %s %d bytes (%d semantic chunks)", voiceName, len(voiceData), nChunks)
		return fmt.Sprintf("[voice_base64|%s|%s]%s", voiceName, voiceMime, base64.StdEncoding.EncodeToString(voiceData))
	}

	log.Printf("[tts-tool] no native voice payload for platform=%s", platform)
	return "当前通道不支持原生语音消息"
}

// toolTTSLocal synthesizes speech and plays it on the local desktop/TUI host.
// It never returns a voice_base64 channel payload and never reads channel or
// destination arguments. IM callers fail closed instead of sending a bubble.
func (h *IMMessageHandler) toolTTSLocal(args map[string]interface{}) string {
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return "缺少 text 参数"
	}
	if _, exists := args["channel"]; exists {
		return "tts_local does not accept channel"
	}
	if _, exists := args["destination"]; exists {
		return "tts_local does not accept destination"
	}
	if h == nil || h.app == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS，并等待模型下载完成。"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return "语音合成未启用。请在设置 → 语音合成中开启。"
	}
	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "tts_local failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	platform := consumeRuntimePlatformFromToolArgs(args)
	if platform == "" {
		platform = h.runtimePlatformForOwnerOrCurrent(ownerID, hasRuntimeOwner)
	}
	if !shouldEmitDesktopTTSPlayback(platform) || normalizeIMMessagePlatformKind(platform).IsIMChannel() {
		return "本机朗读仅在桌面或终端会话可用，当前通道不能发送语音气泡。"
	}
	manager := h.app.ttsManagerForSynthesis()
	if manager == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS，并等待模型下载完成。"
	}
	chunks := tts.PrepareSpeechChunks(text, tts.MaxLongFormSpeechRunes, 0)
	if len(chunks) == 0 {
		return "文本清理后为空，无法合成语音"
	}
	app := h.app
	mgr := manager
	chunkCopy := append([]string(nil), chunks...)
	go func() {
		ttsSpeakMu.Lock()
		defer ttsSpeakMu.Unlock()
		ok := 0
		for i, part := range chunkCopy {
			wav, synthErr := mgr.SynthesizeText(part)
			if synthErr != nil {
				log.Printf("[tts-local] async synthesis error chunk %d/%d: %v", i+1, len(chunkCopy), synthErr)
				continue
			}
			if len(wav) > 0 && app.ctx != nil {
				app.emitEvent("tts:audio", base64.StdEncoding.EncodeToString(wav))
				ok++
			}
		}
		log.Printf("[tts-local] desktop long-form speech done: %d/%d chunks ok", ok, len(chunkCopy))
	}()
	if len(chunks) == 1 {
		return "Voice playback is being generated and will play locally. This is not a send."
	}
	return fmt.Sprintf("长文已按语义分成 %d 段，正在本机依次朗读。这不是发送。", len(chunks))
}

// toolTTSRender synthesizes speech into a host-owned audio artifact. It never
// plays locally, never returns voice_base64, and never reads channel or
// destination arguments. Delivery is a later current-channel selection.
func (h *IMMessageHandler) toolTTSRender(args map[string]interface{}) string {
	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return "缺少 text 参数"
	}
	if _, exists := args["channel"]; exists {
		return "tts_render does not accept channel"
	}
	if _, exists := args["destination"]; exists {
		return "tts_render does not accept destination"
	}
	if h == nil || h.app == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS，并等待模型下载完成。"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return "语音合成未启用。请在设置 → 语音合成中开启。"
	}
	manager := h.app.ttsManagerForSynthesis()
	if manager == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS，并等待模型下载完成。"
	}
	chunks := tts.PrepareSpeechChunks(text, tts.MaxLongFormSpeechRunes, 0)
	if len(chunks) == 0 {
		return "文本清理后为空，无法合成语音"
	}
	wav, _, err := tts.SynthesizeSpeechParts(manager, chunks)
	if err != nil || len(wav) == 0 {
		if err != nil {
			return "tts_render failed: " + err.Error()
		}
		return "tts_render failed: empty speech artifact"
	}
	return toolPayloadSpeechArtifactPrefix + base64.StdEncoding.EncodeToString(wav)
}
