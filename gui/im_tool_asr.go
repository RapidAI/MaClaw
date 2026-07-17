package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
)

// Long ASR transcripts must not be dumped wholesale into the model context:
// conversation tool-result caps (~4KB) will truncate them mid-way and the
// agent cannot build accurate meeting minutes from a head/tail scrap.
//
// Strategy when over budget:
//  1. Persist the full transcript next to the audio as *_transcript.txt
//  2. Also write *_transcript.md (and short-only best-effort *_transcript.pdf)
//  3. Return a structured preview (head + tail) with the file path
//  4. Instruct the agent to map-reduce for summary/decisions and to assemble
//     the "full transcript" minutes section from the file without retyping.

const (
	// asrInlineMaxTokens: return full text only when it fits comfortably under
	// the default tool-result budget after CJK estimation.
	asrInlineMaxTokens = 2200
	// asrInlineMaxBytes: hard byte guard aligned with MaxToolResultLen headroom.
	asrInlineMaxBytes = 3500
	asrPreviewHeadRunes = 900
	asrPreviewTailRunes = 450
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
	result := formatASRToolResult(absPath, text)
	// Minutes draft (extractive or LLM map-reduce) only when explicitly requested.
	// Plain "transcribe only" keeps the long-transcript spill + preview, no draft.
	if asrShouldSpillToFile(text) && asrToolBoolArg(args, "for_minutes", "minutes", "meeting_minutes") {
		result = h.enrichLongASRWithMinutesDraft(absPath, text, result, true)
	}
	return result
}

// asrToolBoolArg reads a loose boolean from common tool-arg shapes.
func asrToolBoolArg(args map[string]interface{}, keys ...string) bool {
	if args == nil {
		return false
	}
	for _, key := range keys {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case bool:
			return t
		case string:
			s := strings.ToLower(strings.TrimSpace(t))
			if s == "1" || s == "true" || s == "yes" || s == "y" || s == "on" || s == "minutes" {
				return true
			}
		case float64:
			return t != 0
		case int:
			return t != 0
		case int64:
			return t != 0
		}
	}
	return false
}

// asrTranscriptSidecarPath returns the path used to persist a long transcript
// next to the source audio (same basename, "_transcript.txt" suffix).
func asrTranscriptSidecarPath(audioPath string) string {
	return asrSiblingWithSuffix(audioPath, "_transcript.txt", "transcript.txt")
}

// asrTranscriptMarkdownPath returns the markdown archive path next to the audio
// (same basename, "_transcript.md" suffix). Used for send_file / generate_pdf.
func asrTranscriptMarkdownPath(audioPath string) string {
	return asrSiblingWithSuffix(audioPath, "_transcript.md", "transcript.md")
}

// asrTranscriptPDFPath returns the PDF archive path next to the audio.
func asrTranscriptPDFPath(audioPath string) string {
	return asrSiblingWithSuffix(audioPath, "_transcript.pdf", "transcript.pdf")
}

func asrSiblingWithSuffix(audioPath, suffix, emptyFallback string) string {
	audioPath = strings.TrimSpace(audioPath)
	if audioPath == "" {
		return emptyFallback
	}
	ext := filepath.Ext(audioPath)
	base := strings.TrimSuffix(audioPath, ext)
	if strings.TrimSpace(base) == "" {
		base = audioPath
	}
	return base + suffix
}

// asrTranscriptTitle returns a short document title from the audio path basename.
func asrTranscriptTitle(audioPath string) string {
	base := strings.TrimSpace(filepath.Base(strings.TrimSpace(audioPath)))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "转写"
	}
	name := strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
	if name == "" {
		return "转写"
	}
	return name
}

// buildASRTranscriptMarkdown builds a minimal archive document (title + body).
// Not meeting minutes — no summary/decisions sections.
func buildASRTranscriptMarkdown(audioPath, text string) string {
	text = strings.TrimSpace(text)
	title := asrTranscriptTitle(audioPath)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("## 转写正文 / Transcript\n\n")
	b.WriteString(text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// writeASRTranscriptPDF best-effort writes *_transcript.pdf from markdown body.
// Only for content that passes ValidatePDFContent (size/paragraph limits).
// Returns the path written, or empty when skipped/failed.
func writeASRTranscriptPDF(audioPath, mdContent string) string {
	mdContent = strings.TrimSpace(mdContent)
	if mdContent == "" || strings.TrimSpace(audioPath) == "" {
		return ""
	}
	if err := swarm.ValidatePDFContent(mdContent); err != nil {
		return ""
	}
	pdfPath := asrTranscriptPDFPath(audioPath)
	title := asrTranscriptTitle(audioPath)
	abs, err := swarm.GenerateToFile(mdContent, title, "", pdfPath)
	if err != nil {
		log.Printf("[asr] write transcript pdf failed path=%s err=%v", pdfPath, err)
		return ""
	}
	if strings.TrimSpace(abs) != "" {
		return abs
	}
	return pdfPath
}

// writeASRTranscriptArchives writes markdown always and PDF for short transcripts
// (PDF generation is skipped for long spills to keep the asr tool path responsive).
func writeASRTranscriptArchives(audioPath, text string, allowPDF bool) (mdPath, pdfPath string) {
	text = strings.TrimSpace(text)
	if text == "" || strings.TrimSpace(audioPath) == "" {
		return "", ""
	}
	mdBody := buildASRTranscriptMarkdown(audioPath, text)
	mdPath = asrTranscriptMarkdownPath(audioPath)
	if err := os.WriteFile(mdPath, []byte(mdBody), 0o644); err != nil {
		log.Printf("[asr] write transcript md failed path=%s err=%v", mdPath, err)
		mdPath = ""
	}
	if allowPDF && mdPath != "" {
		pdfPath = writeASRTranscriptPDF(audioPath, mdBody)
	}
	return mdPath, pdfPath
}

// asrShouldSpillToFile reports whether the transcript is too large to return
// inline into the agent conversation.
func asrShouldSpillToFile(text string) bool {
	if text == "" {
		return false
	}
	if len(text) > asrInlineMaxBytes {
		return true
	}
	return corelib.EstimateTextTokens(text) > asrInlineMaxTokens
}

func asrPreviewHeadTail(text string, headRunes, tailRunes int) (head, tail string, omittedRunes int) {
	runes := []rune(text)
	n := len(runes)
	if headRunes < 0 {
		headRunes = 0
	}
	if tailRunes < 0 {
		tailRunes = 0
	}
	if n <= headRunes+tailRunes {
		return text, "", 0
	}
	head = string(runes[:headRunes])
	tail = string(runes[n-tailRunes:])
	omittedRunes = n - headRunes - tailRunes
	return head, tail, omittedRunes
}

// formatASRToolResult packages ASR output for the agent.
// Always best-effort writes *_transcript.md next to the audio so archives do not
// depend on the model retyping content. Short transcripts also attempt *_transcript.pdf.
// Long transcripts spill plain text to *_transcript.txt and return a preview only.
func formatASRToolResult(audioPath, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "转写结果为空（可能是静音、噪声或时长过短）。"
	}

	spill := asrShouldSpillToFile(text)
	mdPath, pdfPath := writeASRTranscriptArchives(audioPath, text, !spill)

	if !spill {
		if mdPath == "" && pdfPath == "" {
			return text
		}
		// Keep full short text for chat; point agent at host-written archives.
		var b strings.Builder
		b.WriteString(text)
		b.WriteString("\n\n[ASR archive]\n")
		if mdPath != "" {
			b.WriteString(fmt.Sprintf("transcript_md: %s\n", mdPath))
		}
		if pdfPath != "" {
			b.WriteString(fmt.Sprintf("transcript_pdf: %s\n", pdfPath))
		}
		b.WriteString("Host saved transcript archive(s) next to the audio. When the user wants a saved transcript (e.g. post-recording transcribe-only), send_file transcript_md")
		if pdfPath != "" {
			b.WriteString(" and transcript_pdf")
		} else {
			b.WriteString("; if transcript_pdf is missing, call generate_pdf from the markdown body")
		}
		b.WriteString(" (plus MP3 when archiving a meeting recording).\n")
		return b.String()
	}

	outPath := asrTranscriptSidecarPath(audioPath)
	if err := os.WriteFile(outPath, []byte(text), 0o644); err != nil {
		// Still return a truncated preview so the agent is not empty-handed.
		head, tail, omitted := asrPreviewHeadTail(text, asrPreviewHeadRunes, asrPreviewTailRunes)
		var b strings.Builder
		b.WriteString("[ASR long transcript — file save FAILED]\n")
		b.WriteString(fmt.Sprintf("save_error: %v\n", err))
		if mdPath != "" {
			b.WriteString(fmt.Sprintf("transcript_md: %s\n", mdPath))
		}
		b.WriteString(fmt.Sprintf("chars: %d\n", utf8.RuneCountInString(text)))
		b.WriteString(fmt.Sprintf("est_tokens: %d\n", corelib.EstimateTextTokens(text)))
		b.WriteString("WARNING: full text could not be saved as .txt; only a preview is shown. Do not invent missing middle content.\n")
		if omitted > 0 {
			b.WriteString(fmt.Sprintf("\n--- preview_head ---\n%s\n\n... (%d runes omitted) ...\n\n--- preview_tail ---\n%s\n", head, omitted, tail))
		} else {
			b.WriteString("\n")
			b.WriteString(text)
		}
		return b.String()
	}

	head, tail, omitted := asrPreviewHeadTail(text, asrPreviewHeadRunes, asrPreviewTailRunes)
	var b strings.Builder
	b.WriteString("[ASR long transcript]\n")
	b.WriteString(fmt.Sprintf("transcript_file: %s\n", outPath))
	if mdPath != "" {
		b.WriteString(fmt.Sprintf("transcript_md: %s\n", mdPath))
	}
	if audioPath != "" {
		b.WriteString(fmt.Sprintf("audio_path: %s\n", audioPath))
	}
	b.WriteString(fmt.Sprintf("chars: %d\n", utf8.RuneCountInString(text)))
	b.WriteString(fmt.Sprintf("bytes: %d\n", len(text)))
	b.WriteString(fmt.Sprintf("est_tokens: %d\n", corelib.EstimateTextTokens(text)))
	b.WriteString("\n")
	b.WriteString("LONG-CONTENT RULES:\n")
	b.WriteString("1) Full plain text lives in transcript_file; markdown archive may already be at transcript_md. Do NOT re-ask asr for the whole audio unless transcription failed.\n")
	b.WriteString("2) Transcribe-only: prefer send_file transcript_md (or assemble FROM transcript_file if md missing); generate_pdf from the completed .md if transcript_pdf is absent; short chat preview only — do not dump full text into chat. If generate_pdf rejects size, convert from .md via bash or deliver md and report PDF failure.\n")
	b.WriteString("3) Minutes: call asr with for_minutes=true to get engine_minutes_draft; full-transcript section of .md/PDF must assemble FROM transcript_file without rewriting.\n")
	b.WriteString("4) Never paste the entire transcript into chat (context overflow).\n")
	b.WriteString("\n")
	if omitted > 0 {
		b.WriteString("--- preview_head ---\n")
		b.WriteString(head)
		b.WriteString(fmt.Sprintf("\n\n... (%d runes omitted; see transcript_file) ...\n\n", omitted))
		b.WriteString("--- preview_tail ---\n")
		b.WriteString(tail)
		b.WriteString("\n")
	} else {
		b.WriteString(text)
		b.WriteString("\n")
	}
	return b.String()
}
