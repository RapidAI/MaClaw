package main

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	// firstAgentLoopRequestTargetTokens is a latency budget, not a context
	// window limit. Reasoning providers commonly spend materially longer before
	// the first token when a new turn includes a large archived transcript.
	// Later rounds return to the provider's normal context limit so tool-heavy
	// work is not artificially constrained.
	firstAgentLoopRequestTargetTokens = 24_000
	// Keep enough prior context for a follow-up to remain coherent after the
	// system prompt, tools and current user message are accounted for.
	firstAgentLoopRequestHistoryReserveTokens = 4_000
)

// firstAgentLoopRequestTokenLimit applies a bounded latency budget only to the
// first provider request of a turn. It never cuts below the system prompt,
// tool definitions, or the current user message, so local-file/document turns
// retain the material the user just supplied. trimConversation subsequently
// preserves tool-call/result groups when it removes older history.
func firstAgentLoopRequestTokenLimit(effectiveLimit int, conversation []interface{}, tools []map[string]interface{}) int {
	if effectiveLimit <= 0 {
		effectiveLimit = corelib.DefaultContextTokens * 80 / 100
	}

	protectedTokens := agent.EstimateToolsTokens(tools)
	if len(conversation) > 0 {
		protectedTokens += estimateSingleMsgTokens(conversation[0])
	}
	// The current user message must be fully represented. A trailing system
	// notice (for example a drift warning) is not mistaken for that message.
	for i := len(conversation) - 1; i >= 0; i-- {
		if msgRole(conversation[i]) == "user" {
			protectedTokens += estimateSingleMsgTokens(conversation[i])
			break
		}
	}

	limit := firstAgentLoopRequestTargetTokens
	if minimum := protectedTokens + firstAgentLoopRequestHistoryReserveTokens; minimum > limit {
		limit = minimum
	}
	if limit > effectiveLimit {
		return effectiveLimit
	}
	return limit
}

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
