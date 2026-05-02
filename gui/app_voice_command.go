package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

const voiceCommandNormalizationTimeout = 3 * time.Second

type VoiceCommandNormalizationResult struct {
	IsCommand     bool    `json:"is_command"`
	CorrectedText string  `json:"corrected_text"`
	Confidence    float64 `json:"confidence"`
	Reason        string  `json:"reason,omitempty"`
}

func normalizeVoiceCommandLLMResult(raw, fallback string) VoiceCommandNormalizationResult {
	result := VoiceCommandNormalizationResult{IsCommand: true, CorrectedText: strings.TrimSpace(fallback), Reason: "llm_parse_fallback"}
	text := strings.TrimSpace(stripFunctionCalls(stripThinkTags(raw)))
	if text == "" {
		return result
	}
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end >= start {
			text = text[start : end+1]
		}
	}
	var parsed struct {
		IsCommand     bool    `json:"is_command"`
		CorrectedText string  `json:"corrected_text"`
		Confidence    float64 `json:"confidence"`
		Reason        string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		log.Printf("[voice-command] parse normalization result failed: %v raw=%q", err, truncateRunes(text, 200))
		return result
	}
	corrected := strings.TrimSpace(parsed.CorrectedText)
	if corrected == "" {
		corrected = strings.TrimSpace(fallback)
	}
	return VoiceCommandNormalizationResult{
		IsCommand:     parsed.IsCommand,
		CorrectedText: corrected,
		Confidence:    parsed.Confidence,
		Reason:        strings.TrimSpace(parsed.Reason),
	}
}

func (a *App) NormalizeVoiceCommand(text string) VoiceCommandNormalizationResult {
	input := strings.TrimSpace(text)
	result := VoiceCommandNormalizationResult{CorrectedText: input}
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
				"The user text comes from local ASR and may contain homophone mistakes, missing words, repeated filler words, or short noisy fragments.",
				"Decide whether the text is a user command, question, or request that should be sent to the agent. If it is, correct obvious ASR mistakes while preserving the user's intent and produce the final text to send.",
				"If it is only background conversation, laughter, filler, music/video captured by the microphone, or has no clear request, set is_command=false.",
				"Return JSON only, with this shape: {\"is_command\":true|false,\"corrected_text\":\"...\",\"confidence\":0..1,\"reason\":\"...\"}.",
			}, "\n"),
		},
		map[string]string{
			"role":    "user",
			"content": input,
		},
	}

	client := &http.Client{Timeout: voiceCommandNormalizationTimeout}
	resp, err := doSimpleLLMRequest(context.Background(), cfg, messages, client, voiceCommandNormalizationTimeout)
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
