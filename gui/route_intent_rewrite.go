package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// routeIntentRewriteSystemPrompt asks a small model to expand the user message
// into structured tool-routing hints. Output is JSON only.
const routeIntentRewriteSystemPrompt = `You rewrite user messages ONLY for tool routing (not for answering the user).
Reply with a single JSON object, no markdown, no prose:
{
  "intent": "start_recording|transcribe_audio|write_minutes|file_ops|browser|ssh|search|coding|chat|other",
  "query_for_route": "expanded English or Chinese description of the tool action needed",
  "tool_families": ["recording"|"audio"|"browser"|"ssh"|"office"|"search"|"files"|"memory"|"coding"],
  "must_include": ["tool_name", ...],
  "must_exclude": ["tool_name", ...],
  "confidence": 0.0
}

Rules:
- query_for_route must be specific about the ACTION (start desktop long-form mic recording vs transcribe existing audio vs write minutes from notes).
- If the user wants to START meeting/long-form recording now: intent=start_recording, tool_families=["recording"], must_include=["record_audio"], must_exclude=[], confidence high. Do NOT send them to search folders for audio files.
- If the user wants to transcribe an existing file: intent=transcribe_audio, must_include=["asr"].
- If the user wants minutes/summary from existing notes/transcript (not start mic): intent=write_minutes; do not must_include record_audio.
- must_include/must_exclude only use real tool ids when sure: record_audio, asr, tts, bash, read_file, write_file, list_directory, memory, browser, ssh, office, generate_pdf, send_file, web_search, web_fetch, session_search, ask_user.
- confidence < 0.45 if unsure.
- Prefer Chinese query_for_route when the user wrote Chinese.`

// rewriteRouteIntentForTools calls a lightweight LLM to expand short/ambiguous
// user text into RouteIntent. Returns nil on skip, timeout, or parse failure
// so routing always degrades to the original message.
func (h *IMMessageHandler) rewriteRouteIntentForTools(userMessage string) *tool.RouteIntent {
	if h == nil || !tool.ShouldAttemptRouteIntentRewrite(userMessage) {
		return nil
	}
	// Lexical detectors already pin with high confidence — skip the LLM hop.
	if tool.HasStrongLocalRouteSignal(userMessage) {
		return nil
	}
	// Keep rewrite off the critical path of multi-second agent turns: hard cap.
	const timeoutSec = 2
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second+200*time.Millisecond)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "route-intent-rewrite"})

	result, err := h.LLMClassify(ctx, LLMClassifyRequest{
		SystemPrompt:      routeIntentRewriteSystemPrompt,
		UserMessage:       strings.TrimSpace(userMessage),
		TimeoutSec:        timeoutSec,
		Tag:               "route-intent-rewrite",
		PreferLightweight: true,
	})
	if err != nil {
		log.Printf("[route-intent-rewrite] skip: %v", err)
		return nil
	}
	if result == nil || strings.TrimSpace(result.Text) == "" {
		return nil
	}
	intent := tool.ParseRouteIntentJSON(result.Text)
	if intent == nil || !intent.Usable() {
		log.Printf("[route-intent-rewrite] unusable parse raw=%q", truncateForLogGUI(result.Text, 120))
		return nil
	}
	// Log expanded pins against a nil availability map for diagnostics only.
	log.Printf("[route-intent-rewrite] ok intent=%q conf=%.2f must_include=%v families=%v query_len=%d latency=%.0fms",
		intent.Intent, intent.Confidence, intent.MustInclude, intent.ToolFamilies,
		len([]rune(intent.QueryForRoute)), result.Latency.Seconds()*1000)
	return intent
}
