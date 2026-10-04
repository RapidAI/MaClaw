package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
)

// openAIStreamTail collects the terminal SSE events of one chat completion so
// the handler can stamp the settled credit debit onto the finish chunk before
// the client observes finish_reason=stop and stops reading.
type openAIStreamTail struct {
	buf      bytes.Buffer
	started  bool
	holdRest bool
}

type openAIStreamTailKey struct{}

func withOpenAIStreamTail(ctx context.Context, tail *openAIStreamTail) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, openAIStreamTailKey{}, tail)
}

func openAIStreamTailFrom(ctx context.Context) *openAIStreamTail {
	if ctx == nil {
		return nil
	}
	tail, _ := ctx.Value(openAIStreamTailKey{}).(*openAIStreamTail)
	return tail
}

func (t *openAIStreamTail) markStarted() {
	if t != nil {
		t.started = true
	}
}

func (t *openAIStreamTail) append(p []byte) {
	if t == nil || len(p) == 0 {
		return
	}
	_, _ = t.buf.Write(p)
}

// retain returns the bytes that must be written now. A stop chunk waits for
// the settled debit, but any visible delta on that chunk is peeled off and
// returned immediately so the last tokens are not stuck behind the ledger
// write. Tool-call finishes are not held: the client keeps reading until EOF.
func (t *openAIStreamTail) retain(rendered []byte) []byte {
	if t == nil {
		return rendered
	}
	framed := normalizeSSEEvent(rendered)
	if len(framed) == 0 {
		return nil
	}
	if t.holdRest {
		t.append(framed)
		return nil
	}
	if !sseEventHoldsForCredits(sseEventLines(framed)) {
		return framed
	}
	t.holdRest = true
	immediate, rest, peeled := peelStopEventDelta(framed)
	if !peeled {
		t.append(framed)
		return nil
	}
	if len(rest) > 0 {
		t.append(rest)
	}
	return immediate
}

// officialSettledCreditsPtr is the grant debit to show the client. It is nil
// unless this request used the Maclaw official provider and the flush recorded
// a durable debit (including a settled zero).
func officialSettledCreditsPtr(ctx context.Context, providerID string) *float64 {
	if !IsMaClawProviderRequest(providerID) {
		return nil
	}
	credits, ok := settledDeductedCredits(ctx)
	if !ok {
		return nil
	}
	return &credits
}

func flushOpenAIStreamTail(w http.ResponseWriter, tail *openAIStreamTail, deducted *float64) {
	if w == nil || tail == nil || !tail.started {
		return
	}
	body := append([]byte(nil), tail.buf.Bytes()...)
	tail.buf.Reset()
	tail.holdRest = false
	if deducted != nil {
		body = injectCreditsDeductedSSE(body, *deducted)
	}
	if len(body) == 0 {
		return
	}
	_, _ = w.Write(body)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func roundReportedCredits(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return math.Round(v*1000) / 1000
}

func sseEventHoldsForCredits(event [][]byte) bool {
	payload := strings.TrimSpace(openAIStreamEventPayload(event))
	// [DONE] is held only after a stop chunk has already started the tail.
	// Treating it as a hold by itself would delay every stream's trailer until
	// billing, including tool-call rounds that the client is still reading.
	if payload == "" || payload == "[DONE]" {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(payload), &obj); err != nil {
		return false
	}
	return openAIPayloadHasStop(obj)
}

func openAIPayloadHasStop(obj map[string]any) bool {
	choices, _ := obj["choices"].([]any)
	for _, raw := range choices {
		choice, _ := raw.(map[string]any)
		if choice == nil {
			continue
		}
		finish, _ := choice["finish_reason"].(string)
		if strings.EqualFold(strings.TrimSpace(finish), "stop") {
			return true
		}
	}
	return false
}

func injectCreditsDeductedJSON(body []byte, credits float64) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return body
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage == nil {
		usage = map[string]any{}
		payload["usage"] = usage
	}
	usage["credits_deducted"] = roundReportedCredits(credits)
	out, err := marshalSSEObject(payload)
	if err != nil {
		return body
	}
	return out
}

func injectCreditsDeductedSSE(raw []byte, credits float64) []byte {
	credits = roundReportedCredits(credits)
	events := splitSSEEvents(raw)
	merged := map[string]any{}
	stopIdx := -1
	for i, event := range events {
		payload := strings.TrimSpace(sseEventDataPayload(event))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(payload), &obj); err != nil {
			continue
		}
		if usage, ok := obj["usage"].(map[string]any); ok {
			merged = mergeUsageMaps(merged, usage)
		}
		if stopIdx < 0 && openAIPayloadHasStop(obj) {
			stopIdx = i
		}
	}
	merged["credits_deducted"] = credits
	if stopIdx >= 0 {
		events[stopIdx] = rewriteSSEEventUsage(events[stopIdx], merged)
		return joinSSEEvents(events)
	}
	extra := encodeSSEData(map[string]any{"usage": merged})
	return insertSSEBeforeDone(events, extra)
}

func mergeUsageMaps(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = map[string]any{}
	}
	for key, value := range src {
		if key == "credits_deducted" {
			dst[key] = value
			continue
		}
		if isZeroJSONNumber(value) {
			if _, exists := dst[key]; exists {
				continue
			}
		}
		dst[key] = value
	}
	return dst
}

func isZeroJSONNumber(value any) bool {
	switch v := value.(type) {
	case float64:
		return v == 0
	case int:
		return v == 0
	case json.Number:
		f, err := v.Float64()
		return err == nil && f == 0
	default:
		return false
	}
}

func splitSSEEvents(raw []byte) [][]byte {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	parts := bytes.Split(raw, []byte("\n\n"))
	events := make([][]byte, 0, len(parts))
	for _, part := range parts {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}
		events = append(events, append([]byte(nil), part...))
	}
	return events
}

func joinSSEEvents(events [][]byte) []byte {
	if len(events) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, event := range events {
		buf.Write(bytes.TrimRight(event, "\n"))
		buf.WriteString("\n\n")
	}
	return buf.Bytes()
}

func sseEventDataPayload(event []byte) string {
	for _, line := range bytes.Split(event, []byte("\n")) {
		trimmed := strings.TrimSpace(string(line))
		if strings.HasPrefix(trimmed, "data:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		}
	}
	return ""
}

func rewriteSSEEventUsage(event []byte, usage map[string]any) []byte {
	payload := sseEventDataPayload(event)
	if payload == "" || payload == "[DONE]" {
		return event
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(payload), &obj); err != nil || obj == nil {
		return event
	}
	obj["usage"] = usage
	encoded, err := marshalSSEObject(obj)
	if err != nil {
		return event
	}
	var buf bytes.Buffer
	replaced := false
	for _, line := range bytes.Split(event, []byte("\n")) {
		trimmed := strings.TrimSpace(string(line))
		if !replaced && strings.HasPrefix(trimmed, "data:") {
			buf.WriteString("data: ")
			buf.Write(encoded)
			replaced = true
		} else if len(line) > 0 {
			buf.Write(line)
		} else {
			continue
		}
		buf.WriteByte('\n')
	}
	if !replaced {
		buf.WriteString("data: ")
		buf.Write(encoded)
		buf.WriteByte('\n')
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func encodeSSEData(payload map[string]any) []byte {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return append(append([]byte("data: "), encoded...), '\n')
}

func insertSSEBeforeDone(events [][]byte, extra []byte) []byte {
	if len(extra) == 0 {
		return joinSSEEvents(events)
	}
	out := make([][]byte, 0, len(events)+1)
	inserted := false
	for _, event := range events {
		if !inserted && strings.TrimSpace(sseEventDataPayload(event)) == "[DONE]" {
			out = append(out, extra)
			inserted = true
		}
		out = append(out, event)
	}
	if !inserted {
		out = append(out, extra)
	}
	return joinSSEEvents(out)
}

func normalizeSSEEvent(raw []byte) []byte {
	trimmed := bytes.TrimRight(raw, "\n")
	if len(bytes.TrimSpace(trimmed)) == 0 {
		return nil
	}
	out := make([]byte, 0, len(trimmed)+2)
	out = append(out, trimmed...)
	return append(out, '\n', '\n')
}

func sseEventLines(raw []byte) [][]byte {
	parts := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	lines := make([][]byte, 0, len(parts))
	for _, part := range parts {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}
		lines = append(lines, part)
	}
	return lines
}

// peelStopEventDelta copies a stop event into an immediate delta (no
// finish_reason, so the client keeps reading) and a held stop event whose
// delta text is cleared (so the later stamp does not repeat those tokens).
func peelStopEventDelta(rendered []byte) (immediate, rest []byte, ok bool) {
	payload := strings.TrimSpace(sseEventDataPayload(rendered))
	if payload == "" || payload == "[DONE]" {
		return nil, nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(payload), &obj); err != nil || obj == nil {
		return nil, nil, false
	}
	if !openAIPayloadHasStop(obj) || !openAIPayloadHasVisibleDelta(obj) {
		return nil, nil, false
	}
	raw, err := marshalSSEObject(obj)
	if err != nil {
		return nil, nil, false
	}
	var forward, held map[string]any
	if err := json.Unmarshal(raw, &forward); err != nil || forward == nil {
		return nil, nil, false
	}
	if err := json.Unmarshal(raw, &held); err != nil || held == nil {
		return nil, nil, false
	}
	stripTerminalStreamFields(forward)
	stripVisibleDelta(held)
	now, err := encodeSSEEvent(forward)
	if err != nil || len(now) == 0 {
		return nil, nil, false
	}
	kept, err := encodeSSEEvent(held)
	if err != nil || len(kept) == 0 {
		return nil, nil, false
	}
	return now, kept, true
}

func openAIPayloadHasVisibleDelta(obj map[string]any) bool {
	choices, _ := obj["choices"].([]any)
	for _, raw := range choices {
		choice, _ := raw.(map[string]any)
		if choice == nil {
			continue
		}
		delta, _ := choice["delta"].(map[string]any)
		if deltaHasVisiblePayload(delta) {
			return true
		}
	}
	return false
}

func deltaHasVisiblePayload(delta map[string]any) bool {
	if len(delta) == 0 {
		return false
	}
	for _, key := range []string{"content", "reasoning_content", "reasoning", "thinking"} {
		if text, ok := delta[key].(string); ok && text != "" {
			return true
		}
	}
	if calls, ok := delta["tool_calls"].([]any); ok && len(calls) > 0 {
		return true
	}
	if call, ok := delta["function_call"].(map[string]any); ok && len(call) > 0 {
		return true
	}
	return false
}

func stripTerminalStreamFields(obj map[string]any) {
	delete(obj, "usage")
	choices, _ := obj["choices"].([]any)
	for _, raw := range choices {
		choice, _ := raw.(map[string]any)
		if choice == nil {
			continue
		}
		delete(choice, "finish_reason")
	}
}

func stripVisibleDelta(obj map[string]any) {
	choices, _ := obj["choices"].([]any)
	for _, raw := range choices {
		choice, _ := raw.(map[string]any)
		if choice == nil {
			continue
		}
		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}
		for _, key := range []string{"content", "reasoning_content", "reasoning", "thinking", "tool_calls", "function_call"} {
			delete(delta, key)
		}
	}
}

func marshalSSEObject(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func encodeSSEEvent(payload map[string]any) ([]byte, error) {
	encoded, err := marshalSSEObject(payload)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(encoded)+8)
	out = append(out, "data: "...)
	out = append(out, encoded...)
	out = append(out, '\n', '\n')
	return out, nil
}
