package main

import (
	"encoding/base64"
	"encoding/json"
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
	if h.app == nil || h.app.ttsManager == nil {
		return "语音合成不可用（TTS 模型未加载）。请在设置中启用 TTS，并等待模型下载完成。"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil || !cfg.TTSEnabled {
		return "语音合成未启用。请在设置 → 语音合成中开启。"
	}

	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "tts failed: runtime owner is missing; isolated runtime will not fall back to desktop loop"
	}
	platform := consumeRuntimePlatformFromToolArgs(args)
	if platform == "" {
		platform = h.runtimePlatformForOwnerOrCurrent(ownerID, hasRuntimeOwner)
	}

	// Hardware speech is the complete result channel, so do not apply the IM
	// long-form soft cap here. Other channels retain the existing safety cap.
	maxRunes := tts.MaxLongFormSpeechRunes
	if isThirdPartyVoicePlatform(platform) {
		maxRunes = 0
	}
	// Single clean + semantic chunk (avoids CapSpeechText+Split double-clean).
	chunks := tts.PrepareSpeechChunks(text, maxRunes, 0)
	if len(chunks) == 0 {
		return "文本清理后为空，无法合成语音"
	}

	// Desktop panel: async multi-chunk synthesis — queue each segment for playback.
	// Serialize with ttsSpeakMu so concurrent tts calls don't interleave audio chunks.
	if shouldEmitDesktopTTSPlayback(platform) {
		app := h.app
		mgr := app.ttsManager // capture before goroutine to avoid nil race
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

	// Hardware clients use the same ordered, bounded voice_parts protocol for
	// explicit TTS as for automatic reply speech. Preserve the requested text
	// itself here; otherwise the later auto-TTS pass may speak only the tool's
	// status sentence instead of the content the user asked to hear.
	if isThirdPartyVoicePlatform(platform) {
		voiceResp := &IMAgentResponse{Text: strings.Join(chunks, "\n")}
		attachDeviceVoicePayload(h.app.ttsManager, voiceResp, platform)
		if len(voiceResp.VoiceParts) == 0 {
			return "语音合成失败：未生成可供设备播放的音频分段"
		}
		encoded, marshalErr := json.Marshal(voiceResp.VoiceParts)
		if marshalErr != nil {
			return fmt.Sprintf("语音分段编码失败: %v", marshalErr)
		}
		log.Printf("[tts-tool] hardware voice parts=%d json_bytes=%d", len(voiceResp.VoiceParts), len(encoded))
		return fmt.Sprintf("[voice_base64|voice-parts.json|%s]%s", deviceVoicePartsArtifactMIME, base64.StdEncoding.EncodeToString(encoded))
	}

	// IM channels: reuse pre-split parts (no second SplitSpeechChunks pass).
	wav, nChunks, err := tts.SynthesizeSpeechParts(h.app.ttsManager, chunks)
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
