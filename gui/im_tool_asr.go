package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/audioconv"
)

func asrToolPathArg(args map[string]interface{}) string {
	path, _ := args["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		// Accept common aliases the model may invent.
		for _, key := range []string{"file", "file_path", "audio_path"} {
			if v, _ := args[key].(string); strings.TrimSpace(v) != "" {
				path = strings.TrimSpace(v)
				break
			}
		}
	}
	// Strip wrapping quotes models sometimes copy from path blocks.
	path = strings.TrimSpace(path)
	if len(path) >= 2 {
		if (path[0] == '"' && path[len(path)-1] == '"') || (path[0] == '\'' && path[len(path)-1] == '\'') {
			path = strings.TrimSpace(path[1 : len(path)-1])
		}
	}
	return audioconv.StripFileURL(path)
}

// prepareASRToolWAV reads a local audio file and converts it to 16kHz mono WAV.
// On failure returns a user/agent-facing error string (empty wav).
func prepareASRToolWAV(absPath, formatHint string) (wav []byte, errMsg string) {
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Sprintf("文件不存在或无法访问: %v", err)
	}
	if info.IsDir() {
		return nil, fmt.Sprintf("%s 是目录，请传入音频文件路径", absPath)
	}
	if info.Size() <= 0 {
		return nil, "音频文件为空"
	}
	if info.Size() > int64(audioconv.MaxNativeAudioInputBytes) {
		return nil, fmt.Sprintf("音频文件过大（%d bytes，上限 %d）。请先裁剪或压缩后再转写。", info.Size(), audioconv.MaxNativeAudioInputBytes)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Sprintf("读取失败: %v", err)
	}

	formatHint = audioconv.NormalizeFormatHint(formatHint)
	if formatHint == "" {
		formatHint = audioconv.FormatFromPath(absPath)
	}

	wav, err = decodeASRAudio(data, formatHint)
	if err != nil {
		if audioconv.IsNativeDecodeUnsupported(err) || shouldSuggestExternalConvert(formatHint, err) {
			return nil, audioconv.AgentConvertHint(absPath, resolveASRFormatLabel(err, formatHint, absPath, data))
		}
		if formatHint == "" {
			return nil, fmt.Sprintf("无法识别或解码音频格式: %v。直接支持: %s。其它格式请先转为 16kHz mono WAV。", err, audioconv.DirectASRFormats)
		}
		return nil, fmt.Sprintf("音频解码失败（format=%s）: %v。直接支持: %s。", formatHint, err, audioconv.DirectASRFormats)
	}
	return wav, ""
}

// shouldSuggestExternalConvert is true when the format is clearly outside the
// native decoder set (e.g. flac/webm) so the agent should ffmpeg→wav→asr.
// Corrupt direct-format files (broken mp3/wav) stay on the generic decode error.
func shouldSuggestExternalConvert(formatHint string, err error) bool {
	if err == nil {
		return false
	}
	if formatHint != "" && !audioconv.IsDirectASRFormat(formatHint) {
		return true
	}
	// Auto-detect empty / failed with "unsupported format" (unknown container).
	msg := err.Error()
	return strings.Contains(msg, "unsupported format")
}

func resolveASRFormatLabel(err error, formatHint, absPath string, data []byte) string {
	if label := audioconv.NativeDecodeUnsupportedFormat(err); label != "" {
		return label
	}
	if formatHint != "" {
		return formatHint
	}
	if label := audioconv.FormatFromPath(absPath); label != "" {
		return label
	}
	if label := audioconv.DetectFormat(data); label != "" {
		return label
	}
	if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(absPath)), "."); ext != "" {
		return ext
	}
	return "unknown"
}

// decodeASRAudio converts bytes to 16k mono WAV.
// If a non-empty format hint fails (wrong extension / bad hint), retries with
// content auto-detection before giving up.
func decodeASRAudio(data []byte, formatHint string) ([]byte, error) {
	formatHint = audioconv.NormalizeFormatHint(formatHint)
	wav, err := audioconv.ToWAV(data, formatHint)
	if err == nil {
		return wav, nil
	}
	// Native m4a/aac unsupported is definitive after ToWAV's own content re-check.
	if audioconv.IsNativeDecodeUnsupported(err) {
		return nil, err
	}
	if formatHint == "" {
		return nil, err
	}
	// Wrong extension / incorrect format arg: try magic-byte detection.
	if retry, retryErr := audioconv.ToWAV(data, ""); retryErr == nil {
		return retry, nil
	} else if audioconv.IsNativeDecodeUnsupported(retryErr) {
		return nil, retryErr
	}
	return nil, err
}

// toolASR runs local speech-to-text on a file path.
// Direct formats: wav, mp3, ogg/opus, silk (via audioconv.ToWAV).
// For m4a/aac/others, return a clear convert-then-retry hint so the agent can
// use bash+ffmpeg; do not shell out to whisper from this tool.
func (h *IMMessageHandler) toolASR(args map[string]interface{}) string {
	path := asrToolPathArg(args)
	if path == "" {
		return "缺少 path 参数。用法: asr(path=\"本地音频文件路径\")。直接支持: " + audioconv.DirectASRFormats +
			"。其它格式请先用 bash+ffmpeg 转为 16kHz mono 16-bit WAV 再调用。"
	}

	ownerID, hasRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if hasRuntimeOwner && ownerID == "" {
		return "asr failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}

	if h == nil || h.app == nil {
		return "语音识别不可用（应用未就绪）。"
	}
	if !h.app.GetASREnabled() {
		return "语音识别未启用。请在设置 → 语音识别 中开启 ASR，并等待模型下载完成。"
	}
	if !h.app.IsASRReady() {
		return "语音识别模型未就绪。请在设置中启用 ASR 并等待 SenseVoice 模型下载完成。"
	}

	absPath, err := h.resolveFileToolPathForOwner(path, ownerID)
	if err != nil {
		return err.Error()
	}

	formatHint, _ := args["format"].(string)
	wav, errMsg := prepareASRToolWAV(absPath, formatHint)
	if errMsg != "" {
		return errMsg
	}

	text, err := h.app.TranscribeWAVBytes(wav)
	if err != nil {
		return fmt.Sprintf("语音识别失败: %v", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "转写结果为空（可能是静音、噪声或时长过短）。"
	}
	return text
}
