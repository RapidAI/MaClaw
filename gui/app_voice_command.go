package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

const voiceCommandNormalizationTimeout = 3 * time.Second

// Skip LLM correction for very long IM transcripts (cost/latency); keep raw ASR.
const maxASRCorrectionRunes = 400

// IM voice ASR latency bounds.
const (
	imASRMaxDurationSec      = 45.0             // only first N seconds are transcribed
	imASRPerClipTimeout      = 18 * time.Second // wall-clock cap per voice clip
	imASRTotalBudget         = 28 * time.Second // all voice clips in one message
	imASRSkipCorrectionAfter = 12 * time.Second // skip LLM correction if ASR already slow
	imASRMinBudgetToStart    = 2 * time.Second  // don't start a clip with less remaining budget
)

// Localized IM ASR progress strings (package-level to avoid per-call map alloc).
var (
	imVoiceASRProgressEN = map[string]string{
		"recognizing": "Transcribing voice…",
		"correcting":  "Correcting transcript…",
		"timeout":     "Voice transcription timed out; using the audio file as-is",
	}
	imVoiceASRProgressZhHans = map[string]string{
		"recognizing": "正在识别语音…",
		"correcting":  "正在纠错语音转写…",
		"timeout":     "语音识别超时，将使用原始语音文件",
	}
	imVoiceASRProgressZhHant = map[string]string{
		"recognizing": "正在識別語音…",
		"correcting":  "正在糾錯語音轉寫…",
		"timeout":     "語音識別超時，將使用原始語音檔案",
	}
)

type VoiceCommandNormalizationResult struct {
	IsCommand     bool    `json:"is_command"`
	CorrectedText string  `json:"corrected_text"`
	Confidence    float64 `json:"confidence"`
	Reason        string  `json:"reason,omitempty"`
}

// extractJSONObject pulls the outermost {...} from model output, stripping
// optional markdown fences (```json ... ```).
func extractJSONObject(raw string) string {
	text := strings.TrimSpace(stripFunctionCalls(stripThinkTags(raw)))
	if text == "" {
		return ""
	}
	// Common LLM wrapping.
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```JSON")
		text = strings.TrimPrefix(text, "```")
		if i := strings.LastIndex(text, "```"); i >= 0 {
			text = text[:i]
		}
		text = strings.TrimSpace(text)
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return ""
	}
	return text[start : end+1]
}

func normalizeVoiceCommandLLMResult(raw, fallback string) VoiceCommandNormalizationResult {
	result := VoiceCommandNormalizationResult{IsCommand: true, CorrectedText: strings.TrimSpace(fallback), Reason: "llm_parse_fallback"}
	text := extractJSONObject(raw)
	if text == "" {
		return result
	}
	var parsed struct {
		IsCommand     bool    `json:"is_command"`
		CorrectedText string  `json:"corrected_text"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		log.Printf("[voice-command] parse normalization result failed: %v raw_len=%d", err, utf8.RuneCountInString(text))
		return result
	}
	corrected := strings.TrimSpace(parsed.CorrectedText)
	// Only fall back to the raw ASR text when the model claims this is a command.
	// Non-commands intentionally use empty corrected_text so continuous mode can drop them.
	// Also reject punctuation-only "corrections" for commands — keep raw ASR instead.
	if parsed.IsCommand && (corrected == "" || !hasASRSpeechContent(corrected)) {
		corrected = strings.TrimSpace(fallback)
	}
	return VoiceCommandNormalizationResult{
		IsCommand:     parsed.IsCommand,
		CorrectedText: corrected,
		Confidence:    parsed.Confidence,
		Reason:        strings.TrimSpace(parsed.Reason),
	}
}

// parseASRCorrectionLLMResult extracts corrected_text from a correction-only
// LLM response. JSON only — plain-text replies are ignored so model apologies
// or chatter cannot replace the transcript. Empty string = no usable correction.
func parseASRCorrectionLLMResult(raw string) string {
	obj := extractJSONObject(raw)
	if obj == "" {
		return ""
	}
	var parsed struct {
		CorrectedText string `json:"corrected_text"`
	}
	if err := json.Unmarshal([]byte(obj), &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.CorrectedText)
}

func (a *App) NormalizeVoiceCommand(text string) VoiceCommandNormalizationResult {
	input := strings.TrimSpace(text)
	result := VoiceCommandNormalizationResult{IsCommand: true, CorrectedText: input}
	if input == "" {
		return result
	}
	if !a.isMaclawLLMConfigured() {
		result.IsCommand = true
		result.Confidence = 0
		result.Reason = "llm_not_configured_fallback"
		return result
	}

	cfg := a.GetMaclawLLMConfig()
	messages := []interface{}{
		map[string]string{
			"role": "system",
			"content": strings.Join([]string{
				"You are a fast pre-filter for a desktop AI assistant continuous voice input stream.",
				"The user text comes from local Chinese/English ASR and may contain homophone mistakes (e.g. 天气→天启, 北京→背景), missing words, repeated fillers, or short noisy fragments.",
				"Decide whether the text is a user command, question, or request that should be sent to the agent.",
				"If it is a real request: set is_command=true, correct obvious ASR mistakes while preserving intent, and put the final sendable text in corrected_text (prefer concise clear Chinese/English the agent can act on).",
				"If it is only background conversation, laughter, filler (嗯/啊/那个), music/video, or has no clear request: set is_command=false and corrected_text=\"\".",
				"Do not invent a new task the user did not imply. Prefer light correction over rewriting.",
				"Return JSON only: {\"is_command\":true|false,\"corrected_text\":\"...\",\"confidence\":0..1,\"reason\":\"...\"}.",
			}, "\n"),
		},
		map[string]string{
			"role":    "user",
			"content": input,
		},
	}

	client := &http.Client{Timeout: voiceCommandNormalizationTimeout}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "voice-command-normalization"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, voiceCommandNormalizationTimeout)
	if err != nil {
		log.Printf("[voice-command] normalization failed, falling back to original: %v", err)
		result.IsCommand = true
		result.Reason = "llm_error_fallback"
		return result
	}
	normalized := normalizeVoiceCommandLLMResult(resp.Content, input)
	log.Printf("[voice-command] normalized is_command=%t confidence=%.2f input=%q corrected=%q reason=%q",
		normalized.IsCommand, normalized.Confidence, truncateRunes(input, 80), truncateRunes(normalized.CorrectedText, 80), normalized.Reason)
	return normalized
}

// CorrectASRText is the Wails-exported intentional-utterance corrector
// (desktop hold-to-talk / callers that should not drop non-commands).
func (a *App) CorrectASRText(text string) string {
	return a.correctASRText(text)
}

// correctASRText applies optional LLM ASR correction for intentional utterances
// (IM voice messages, hold-to-talk). Never drops text as "non-command" because
// the user deliberately sent the audio. Respects asr_voice_correction_enabled.
//
// Uses a correction-only prompt (not the continuous-mode command filter) so short
// legitimate requests are not treated as background noise.
func (a *App) correctASRText(text string) string {
	input := strings.TrimSpace(text)
	if a == nil || input == "" {
		return input
	}
	cfg, err := a.LoadConfig()
	if err != nil || !cfg.ASRVoiceCorrectionEnabled {
		return input
	}
	if !a.isMaclawLLMConfigured() {
		return input
	}
	// Long voice notes: skip LLM correction to bound latency/cost.
	if utf8.RuneCountInString(input) > maxASRCorrectionRunes {
		log.Printf("[voice-command] skip ASR correction: transcript too long runes=%d", utf8.RuneCountInString(input))
		return input
	}
	corrected := strings.TrimSpace(a.correctASRTextWithLLM(input))
	// Reject empty or punctuation-only "corrections" (LLM noise); keep raw ASR.
	if corrected == "" || !hasASRSpeechContent(corrected) {
		return input
	}
	return corrected
}

// hasASRSpeechContent reports whether text contains any letter/digit (real speech).
// Aligns with shouldDropASRText "punctuation-only" and frontend isPunctuationOnlyASRText.
func hasASRSpeechContent(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// correctASRTextWithLLM asks the LLM only to fix ASR errors — no command filtering.
// Returns empty string on failure so callers can fall back to the raw transcript.
func (a *App) correctASRTextWithLLM(input string) string {
	input = strings.TrimSpace(input)
	if a == nil || input == "" {
		return ""
	}
	cfg := a.GetMaclawLLMConfig()
	messages := []interface{}{
		map[string]string{
			"role": "system",
			"content": strings.Join([]string{
				"You correct obvious speech-recognition errors in a short Chinese/English transcript.",
				"The user deliberately sent this voice message (IM or push-to-talk). Always treat it as intentional speech.",
				"Fix clear homophone/typo ASR mistakes (e.g. 背景天气→北京天气) while preserving the user's intent.",
				"Do not invent a new task. Prefer light correction over rewriting. Do not drop the message.",
				"Return JSON only: {\"corrected_text\":\"...\",\"confidence\":0..1,\"reason\":\"...\"}.",
			}, "\n"),
		},
		map[string]string{
			"role":    "user",
			"content": input,
		},
	}
	client := &http.Client{Timeout: voiceCommandNormalizationTimeout}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "asr-text-correction"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, voiceCommandNormalizationTimeout)
	if err != nil {
		log.Printf("[voice-command] ASR text correction failed, keeping original: %v", err)
		return ""
	}
	corrected := parseASRCorrectionLLMResult(resp.Content)
	log.Printf("[voice-command] asr-text-correction input=%q corrected=%q",
		truncateRunes(input, 80), truncateRunes(corrected, 80))
	return corrected
}

// transcribeIMVoiceAttachment runs local ASR on IM voice WAV and optionally
// applies voice correction. Returns empty string on failure (caller keeps fallback text).
//
// Latency controls:
//   - progress callback (optional) so the user sees "识别中"
//   - audio truncated to imASRMaxDurationSec
//   - per-clip wall-clock timeout (capped by overall deadline)
//   - skip LLM correction when ASR already slow
func (a *App) transcribeIMVoiceAttachment(wavData []byte, onProgress func(string), deadline time.Time) string {
	if a == nil {
		return ""
	}
	if !deadline.IsZero() && time.Now().After(deadline) {
		log.Printf("[IM] voice ASR skipped: message total budget exhausted")
		return ""
	}
	// Fast path: skip model load noise when ASR is off or model is missing.
	if !a.IsASRReady() {
		return ""
	}

	// Resolve per-clip timeout against the shared message deadline *before*
	// emitting progress, so we never flash "识别中" then immediately bail.
	timeout := imASRPerClipTimeout
	if !deadline.IsZero() {
		rem := time.Until(deadline)
		if rem < imASRMinBudgetToStart {
			log.Printf("[IM] voice ASR skipped: remaining budget %s too small", rem)
			return ""
		}
		if rem < timeout {
			timeout = rem
		}
	}

	lang := a.CurrentLanguage
	if onProgress != nil {
		onProgress(imVoiceASRProgressText(lang, "recognizing"))
	}
	start := time.Now()

	type asrResult struct {
		text string
		err  error
	}
	ch := make(chan asrResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Non-blocking: if the main path already timed out, drop the panic result.
				select {
				case ch <- asrResult{err: fmt.Errorf("ASR panic: %v", r)}:
				default:
				}
			}
		}()
		text, err := a.transcribeWAVBytes(wavData, asrTranscribeOpts{
			MaxDurationSec: imASRMaxDurationSec,
			SkipVAD:        true, // IM notes are already user-selected utterances
		})
		select {
		case ch <- asrResult{text: text, err: err}:
		default:
			// Timed out; result discarded.
		}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var text string
	select {
	case r := <-ch:
		if r.err != nil {
			log.Printf("[IM] voice ASR failed elapsed=%s err=%v", time.Since(start), r.err)
			return ""
		}
		text = strings.TrimSpace(r.text)
	case <-timer.C:
		log.Printf("[IM] voice ASR timed out after %s", timeout)
		if onProgress != nil {
			onProgress(imVoiceASRProgressText(lang, "timeout"))
		}
		// Underlying ASR may still be running (model lock); next call waits.
		return ""
	}

	if text == "" {
		log.Printf("[IM] voice ASR empty elapsed=%s", time.Since(start))
		return ""
	}

	asrElapsed := time.Since(start)
	// If ASR already took long, skip LLM correction for snappier replies.
	if asrElapsed > imASRSkipCorrectionAfter {
		log.Printf("[IM] voice ASR ok (skip correction, asr slow) elapsed=%s text=%q",
			asrElapsed, truncateRunes(text, 80))
		return text
	}
	if !deadline.IsZero() && time.Until(deadline) < voiceCommandNormalizationTimeout {
		log.Printf("[IM] voice ASR ok (skip correction, total budget low) elapsed=%s text=%q",
			asrElapsed, truncateRunes(text, 80))
		return text
	}

	if onProgress != nil {
		onProgress(imVoiceASRProgressText(lang, "correcting"))
	}
	corrected := a.correctASRText(text)
	if corrected != text {
		log.Printf("[IM] voice ASR corrected elapsed=%s input=%q corrected=%q",
			time.Since(start), truncateRunes(text, 80), truncateRunes(corrected, 80))
	} else {
		log.Printf("[IM] voice ASR ok elapsed=%s text=%q", time.Since(start), truncateRunes(text, 80))
	}
	return corrected
}

// imVoiceASRProgressText localizes short IM ASR progress strings.
func imVoiceASRProgressText(lang, kind string) string {
	var table map[string]string
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		table = imVoiceASRProgressEN
	case appLanguageZhHant:
		table = imVoiceASRProgressZhHant
	default:
		table = imVoiceASRProgressZhHans
	}
	if s, ok := table[kind]; ok {
		return s
	}
	if s, ok := imVoiceASRProgressEN[kind]; ok {
		return s
	}
	return kind
}
