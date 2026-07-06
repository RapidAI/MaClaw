package main

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib"
)

func calibratedAgentLoopTokenLimit(cfg corelib.MaclawLLMConfig, conversation []interface{}, lastInputTokens int, lastOutputTokens int) (int, int) {
	effectiveTokenLimit := cfg.EffectiveContextTokens()
	estimated := 0
	if lastInputTokens <= 0 {
		return effectiveTokenLimit, estimated
	}
	estimated = estimateConversationTokens(conversation)
	if estimated > 0 {
		ratio := float64(lastInputTokens) / float64(estimated)
		if ratio > 1.15 {
			calibrated := int(float64(effectiveTokenLimit) / ratio)
			if calibrated < 4000 {
				calibrated = 4000
			}
			log.Printf("[trim-calibration] API reported %d tokens, estimated %d (ratio=%.2f), reducing limit from %d to %d",
				lastInputTokens, estimated, ratio, effectiveTokenLimit, calibrated)
			effectiveTokenLimit = calibrated
		}
	}
	originalEffective := cfg.EffectiveContextTokens()
	hardCeiling := originalEffective * 85 / 100
	if lastInputTokens > hardCeiling {
		forcedLimit := originalEffective * 65 / 100
		if forcedLimit < 4000 {
			forcedLimit = 4000
		}
		if forcedLimit < effectiveTokenLimit {
			log.Printf("[trim-hardlimit] API reported %d tokens > 85%% ceiling %d (effective=%d), forcing limit from %d to %d",
				lastInputTokens, hardCeiling, originalEffective, effectiveTokenLimit, forcedLimit)
			effectiveTokenLimit = forcedLimit
		}
	}
	if lastOutputTokens == 0 {
		emptyTrimLimit := cfg.EffectiveContextTokens() * 60 / 100
		if emptyTrimLimit < 4000 {
			emptyTrimLimit = 4000
		}
		if emptyTrimLimit < effectiveTokenLimit {
			log.Printf("[trim-empty-response] previous response was empty (input=%d), forcing aggressive trim from %d to %d",
				lastInputTokens, effectiveTokenLimit, emptyTrimLimit)
			effectiveTokenLimit = emptyTrimLimit
		}
	}
	// Spiral protection: ensure the effective limit never drops below the
	// system prompt size + a minimum conversation margin (3000 tokens).
	// Without this, repeated empty responses can shrink the limit below
	// what's needed to fit the system prompt, causing degenerate behavior.
	if len(conversation) > 0 {
		systemPromptTokens := estimateSingleMsgTokens(conversation[0])
		minLimit := systemPromptTokens + 3000
		if effectiveTokenLimit < minLimit {
			log.Printf("[trim-spiral-guard] effective limit %d < system_prompt(%d)+3000, raising to %d",
				effectiveTokenLimit, systemPromptTokens, minLimit)
			effectiveTokenLimit = minLimit
		}
	}
	return effectiveTokenLimit, estimated
}
