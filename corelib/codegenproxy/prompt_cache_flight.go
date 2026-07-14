package codegenproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

// Default wait for co-waiters on an in-flight cache fill.
const promptCacheSingleflightWait = 15 * time.Second

// promptCacheFlight coalesces concurrent cache-miss fills for the same key.
type promptCacheFlight struct {
	done    chan struct{}
	payload []byte
	ok      bool
}

func (s *Server) joinPromptCacheFlight(key string) (*promptCacheFlight, bool) {
	if s == nil || key == "" {
		return nil, true
	}
	s.promptCacheFlightMu.Lock()
	defer s.promptCacheFlightMu.Unlock()
	if s.promptCacheFlights == nil {
		s.promptCacheFlights = make(map[string]*promptCacheFlight)
	}
	if f := s.promptCacheFlights[key]; f != nil {
		return f, false
	}
	f := &promptCacheFlight{done: make(chan struct{})}
	s.promptCacheFlights[key] = f
	return f, true
}

func (s *Server) finishPromptCacheFlight(key string, f *promptCacheFlight, payload []byte, ok bool) {
	if f == nil {
		return
	}
	if ok && len(payload) > 0 {
		f.payload = append([]byte(nil), payload...)
		f.ok = true
	}
	if s != nil {
		s.promptCacheFlightMu.Lock()
		if s.promptCacheFlights[key] == f {
			delete(s.promptCacheFlights, key)
		}
		s.promptCacheFlightMu.Unlock()
	}
	close(f.done)
}

// waitPromptCacheFlight waits for a leader fill. Returns payload when shared
// successfully; ok=false means the waiter should perform its own upstream call.
func waitPromptCacheFlight(r *http.Request, f *promptCacheFlight, wait time.Duration) (payload []byte, ok bool) {
	if f == nil {
		return nil, false
	}
	if wait <= 0 {
		wait = promptCacheSingleflightWait
	}
	var ctxDone <-chan struct{}
	if r != nil && r.Context() != nil {
		ctxDone = r.Context().Done()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-f.done:
		if f.ok && len(f.payload) > 0 {
			return append([]byte(nil), f.payload...), true
		}
		return nil, false
	case <-ctxDone:
		return nil, false
	case <-timer.C:
		return nil, false
	}
}

// writePromptCacheHitPayload writes a cached non-streaming chat.completion body
// either as JSON or as synthesized SSE. Returns false if streaming synthesis fails.
func writePromptCacheHitPayload(w http.ResponseWriter, payload []byte, isStreaming bool) bool {
	if w == nil || len(payload) == 0 {
		return false
	}
	if isStreaming {
		sseBody, err := corelib.SynthesizeOpenAIChatCompletionSSE(payload)
		if err != nil {
			return false
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(sseBody)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", "HIT")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
	return true
}

// writePromptCacheHitResponsesPayload serves a cached chat.completion payload on
// the OpenAI Responses API surface (JSON or synthesized Responses SSE).
// Payload remains chat.completion JSON so /v1/chat/completions and /v1/responses share keys.
func writePromptCacheHitResponsesPayload(w http.ResponseWriter, chatPayload []byte, model, reqID string, isStreaming bool) bool {
	if w == nil || len(chatPayload) == 0 {
		return false
	}
	if !isStreaming {
		responsesBody, err := convertOpenAIChatResponseToResponses(chatPayload, model)
		if err != nil {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(responsesBody)
		return true
	}
	return writePromptCacheHitResponsesStream(w, chatPayload, model, reqID)
}

func writePromptCacheHitResponsesStream(w http.ResponseWriter, chatPayload []byte, model, reqID string) bool {
	var chat openaiChatResponse
	if err := json.Unmarshal(chatPayload, &chat); err != nil {
		return false
	}
	if len(chat.Choices) == 0 {
		return false
	}
	msg := chat.Choices[0].Message
	// Flusher is optional: httptest.ResponseRecorder (unit tests) does not implement it.
	// Production http.Server ResponseWriters do; flush when available for progressive SSE.
	flusher, _ := w.(http.Flusher)
	text := openAIMessageContentToString(msg.Content)
	hasTools := len(msg.ToolCalls) > 0 || msg.FunctionCall != nil
	respID := "resp_" + shortSHA256(reqID+":"+model+":cache")
	msgID := "msg_" + shortSHA256(respID+":message")
	seq := 1
	nextOutputIndex := 0
	textOutputIndex := -1

	// Rebuild tool accumulators so response.completed matches live stream shape.
	toolCalls := map[int]*responsesStreamToolCallAccum{}
	var toolOrder []int
	for i, tc := range msg.ToolCalls {
		callID := tc.ID
		if callID == "" {
			callID = fmt.Sprintf("call_%s_%d", shortSHA256(respID), i)
		}
		acc := &responsesStreamToolCallAccum{
			Index:       i,
			OutputIndex: -1,
			ID:          callID,
			ItemID:      "fc_" + shortSHA256(callID),
			Name:        tc.Function.Name,
			Arguments:   tc.Function.Arguments,
		}
		toolCalls[i] = acc
		toolOrder = append(toolOrder, i)
	}
	if msg.FunctionCall != nil {
		idx := len(toolOrder)
		callID := "call_legacy_function"
		acc := &responsesStreamToolCallAccum{
			Index:       idx,
			OutputIndex: -1,
			ID:          callID,
			ItemID:      "fc_" + shortSHA256(respID+":legacy_function"),
			Name:        msg.FunctionCall.Name,
			Arguments:   msg.FunctionCall.Arguments,
		}
		toolCalls[idx] = acc
		toolOrder = append(toolOrder, idx)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Cache", "HIT")
	w.WriteHeader(http.StatusOK)

	writeResponsesSSE(w, "response.created", map[string]interface{}{
		"type":            "response.created",
		"sequence_number": seq,
		"response":        responsesStreamResponseObject(respID, model, "", false, -1, nil, nil, nil),
	})
	seq++
	if flusher != nil {
		flusher.Flush()
	}

	// Emit text message item when there is content, or when the reply is text-only.
	if text != "" || !hasTools {
		textOutputIndex = nextOutputIndex
		nextOutputIndex++
		writeResponsesSSE(w, "response.output_item.added", map[string]interface{}{
			"type":            "response.output_item.added",
			"sequence_number": seq,
			"output_index":    textOutputIndex,
			"item": map[string]interface{}{
				"id":      msgID,
				"type":    "message",
				"status":  "in_progress",
				"role":    "assistant",
				"content": []interface{}{},
			},
		})
		seq++
		writeResponsesSSE(w, "response.content_part.added", map[string]interface{}{
			"type":            "response.content_part.added",
			"sequence_number": seq,
			"item_id":         msgID,
			"output_index":    textOutputIndex,
			"content_index":   0,
			"part": map[string]interface{}{
				"type":        "output_text",
				"text":        "",
				"annotations": []interface{}{},
			},
		})
		seq++
		if text != "" {
			writeResponsesSSE(w, "response.output_text.delta", map[string]interface{}{
				"type":            "response.output_text.delta",
				"sequence_number": seq,
				"item_id":         msgID,
				"output_index":    textOutputIndex,
				"content_index":   0,
				"delta":           text,
				"logprobs":        []interface{}{},
			})
			seq++
		}
		writeResponsesSSE(w, "response.output_text.done", map[string]interface{}{
			"type":            "response.output_text.done",
			"sequence_number": seq,
			"item_id":         msgID,
			"output_index":    textOutputIndex,
			"content_index":   0,
			"text":            text,
			"logprobs":        []interface{}{},
		})
		seq++
		writeResponsesSSE(w, "response.content_part.done", map[string]interface{}{
			"type":            "response.content_part.done",
			"sequence_number": seq,
			"item_id":         msgID,
			"output_index":    textOutputIndex,
			"content_index":   0,
			"part": map[string]interface{}{
				"type":        "output_text",
				"text":        text,
				"annotations": []interface{}{},
			},
		})
		seq++
		writeResponsesSSE(w, "response.output_item.done", map[string]interface{}{
			"type":            "response.output_item.done",
			"sequence_number": seq,
			"output_index":    textOutputIndex,
			"item": map[string]interface{}{
				"id":     msgID,
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []interface{}{map[string]interface{}{
					"type":        "output_text",
					"text":        text,
					"annotations": []interface{}{},
				}},
			},
		})
		seq++
	}

	for _, idx := range toolOrder {
		acc := toolCalls[idx]
		if acc == nil || acc.Name == "" {
			continue
		}
		acc.OutputIndex = nextOutputIndex
		nextOutputIndex++
		writeResponsesSSE(w, "response.output_item.added", map[string]interface{}{
			"type":            "response.output_item.added",
			"sequence_number": seq,
			"output_index":    acc.OutputIndex,
			"item": map[string]interface{}{
				"id":        acc.ItemID,
				"type":      "function_call",
				"status":    "in_progress",
				"call_id":   acc.ID,
				"name":      acc.Name,
				"arguments": "",
			},
		})
		seq++
		if acc.Arguments != "" {
			writeResponsesSSE(w, "response.function_call_arguments.delta", map[string]interface{}{
				"type":            "response.function_call_arguments.delta",
				"sequence_number": seq,
				"item_id":         acc.ItemID,
				"output_index":    acc.OutputIndex,
				"delta":           acc.Arguments,
			})
			seq++
		}
		writeResponsesSSE(w, "response.function_call_arguments.done", map[string]interface{}{
			"type":            "response.function_call_arguments.done",
			"sequence_number": seq,
			"item_id":         acc.ItemID,
			"output_index":    acc.OutputIndex,
			"arguments":       acc.Arguments,
		})
		seq++
		writeResponsesSSE(w, "response.output_item.done", map[string]interface{}{
			"type":            "response.output_item.done",
			"sequence_number": seq,
			"output_index":    acc.OutputIndex,
			"item": map[string]interface{}{
				"id":        acc.ItemID,
				"type":      "function_call",
				"status":    "completed",
				"call_id":   acc.ID,
				"name":      acc.Name,
				"arguments": acc.Arguments,
			},
		})
		seq++
	}

	writeResponsesSSE(w, "response.completed", map[string]interface{}{
		"type":            "response.completed",
		"sequence_number": seq,
		"response":        responsesStreamResponseObject(respID, model, text, true, textOutputIndex, toolOrder, toolCalls, chat.Usage),
	})
	if flusher != nil {
		flusher.Flush()
	}
	return true
}

// promptCacheOptions returns the shared deterministic-cache option set.
func promptCacheOptions() corelib.LLMPromptCacheOptions {
	return corelib.LLMPromptCacheOptions{
		Enabled:                      true,
		NormalizeDeterministicParams: true,
		IgnoreModelField:             false,
		// Codex attaches per-request tracing metadata and user IDs. They do not
		// affect generation, but would otherwise make every cache key unique.
		IgnoreUserField:     true,
		IgnoreMetadataField: true,
	}
}

// resolvePromptCacheKey returns a cache key for a chat-completions-shaped body, or "" if ineligible.
func resolvePromptCacheKey(chatBody []byte, normalizedModel, originalModel string) (string, string) {
	var parsed map[string]any
	if json.Unmarshal(chatBody, &parsed) != nil {
		return "", "invalid_json"
	}
	opts := promptCacheOptions()
	if decision := corelib.LLMPromptCacheable(parsed, opts); !decision.Cacheable {
		return "", decision.Reason
	}
	key, _, err := corelib.LLMPromptCacheKey(normalizedModel, originalModel, parsed, opts)
	if err != nil || key == "" {
		return "", "key_error"
	}
	return key, ""
}

// storePromptCacheChatPayload stores an upstream chat.completion body when policy allows.
// Returns true when the payload was accepted into the cache.
//
// Unlike Hub's default store policy, the local codegenproxy exact-match cache
// accepts tool_call responses: replaying the model's tool decision for an
// identical request is safe and useful for Codex/agent retries.
func (s *Server) storePromptCacheChatPayload(ctx context.Context, promptCache *llmpool.Cache, cacheKey, model string, chatRespBody []byte, statusCode int) (bool, string) {
	if s == nil || promptCache == nil || cacheKey == "" || len(chatRespBody) == 0 {
		return false, "empty_payload"
	}
	maxBytes := corelib.DefaultLLMPromptCacheMaxResponseBytes
	store := corelib.LLMPromptCacheShouldStoreWithLimit(chatRespBody, statusCode, maxBytes)
	if !store.Store {
		// Allow tool_calls / function_call payloads through for exact-match proxy cache.
		if store.Reason != "tool_calls" {
			return false, store.Reason
		}
	}
	entry := &llmpool.CacheEntry{
		CacheKey:     cacheKey,
		Model:        model,
		Kind:         "full",
		Payload:      chatRespBody,
		PayloadBytes: int64(len(chatRespBody)),
	}
	if err := promptCache.Put(ctx, entry); err != nil {
		return false, "put_failed"
	}
	return true, ""
}

// buildChatCompletionFromResponsesStream rebuilds a non-streaming chat.completion
// body from a completed Responses stream accumulation (text-only or with tools).
func buildChatCompletionFromResponsesStream(model, text string, usage *openaiUsage, toolOrder []int, toolCalls map[int]*responsesStreamToolCallAccum) []byte {
	msg := map[string]interface{}{
		"role": "assistant",
	}
	if text != "" {
		msg["content"] = text
	} else {
		msg["content"] = nil
	}
	var tcs []interface{}
	for _, idx := range toolOrder {
		acc := toolCalls[idx]
		if acc == nil || acc.Name == "" {
			continue
		}
		id := acc.ID
		if id == "" {
			id = "call_" + shortSHA256(model+":"+acc.Name)
		}
		tcs = append(tcs, map[string]interface{}{
			"id":   id,
			"type": "function",
			"function": map[string]interface{}{
				"name":      acc.Name,
				"arguments": acc.Arguments,
			},
		})
	}
	finish := "stop"
	if len(tcs) > 0 {
		msg["tool_calls"] = tcs
		finish = "tool_calls"
	}
	payload := map[string]interface{}{
		"id":      "chatcmpl_" + shortSHA256(model+":"+text),
		"object":  "chat.completion",
		"model":   model,
		"choices": []interface{}{map[string]interface{}{"index": 0, "message": msg, "finish_reason": finish}},
	}
	if usage != nil {
		payload["usage"] = usage
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return body
}
