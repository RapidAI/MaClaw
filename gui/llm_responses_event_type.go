package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

type responsesEventType string

// responsesAPIReasoningOutputItem decodes the display-safe summary carried by
// a final reasoning output item. It intentionally has no private reasoning
// fields: the UI may render only content the provider explicitly returns.
type responsesAPIReasoningOutputItem struct {
	Type    string                         `json:"type"`
	Summary []responsesAPIReasoningContent `json:"summary,omitempty"`
	Content []responsesAPIReasoningContent `json:"content,omitempty"`
}

type responsesAPIReasoningContent struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content string `json:"content"`
}

func (item responsesAPIReasoningOutputItem) DisplaySummary() string {
	if !isResponsesReasoningItemType(item.Type) {
		return ""
	}
	parts := item.Summary
	if len(parts) == 0 {
		parts = item.Content
	}
	var out strings.Builder
	for _, part := range parts {
		switch part.Type {
		case "summary_text", "reasoning_text", "reasoning_content", "text", "output_text":
			if part.Text != "" {
				out.WriteString(part.Text)
			} else {
				out.WriteString(part.Content)
			}
		}
	}
	return strings.TrimSpace(out.String())
}

// appendResponsesReasoningSummary deduplicates the common Responses pattern
// where a provider streams a summary and repeats it in output_item.done or
// response.completed. It preserves non-overlapping content from compatible
// providers that emit a partial delta followed by the final text.
func appendResponsesReasoningSummary(buf *strings.Builder, summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	existing := buf.String()
	if existing == "" {
		buf.WriteString(summary)
		return summary
	}
	if strings.Contains(existing, summary) {
		return ""
	}
	if strings.HasPrefix(summary, existing) {
		suffix := summary[len(existing):]
		// Keep the frontend stream and saved message consistent: only append the
		// missing suffix instead of replacing already-emitted text in the buffer.
		buf.WriteString(suffix)
		return suffix
	}
	if overlap := responsesReasoningOverlap(existing, summary); overlap > 0 {
		suffix := summary[overlap:]
		buf.WriteString(suffix)
		return suffix
	}
	if !needsReasoningSummarySeparator(existing, summary) {
		buf.WriteString(summary)
		return summary
	}
	buf.WriteByte('\n')
	buf.WriteString(summary)
	return "\n" + summary
}

// responsesReasoningOverlap returns the largest byte prefix of next that is a
// suffix of prior. JSON text is UTF-8; adjust candidate boundaries so partial
// overlap never splits a rune.
func responsesReasoningOverlap(prior, next string) int {
	max := len(prior)
	if len(next) < max {
		max = len(next)
	}
	for n := max; n > 0; n-- {
		if !utf8.ValidString(prior[len(prior)-n:]) || !utf8.ValidString(next[:n]) {
			continue
		}
		if prior[len(prior)-n:] == next[:n] {
			return n
		}
	}
	return 0
}

// responsesCompletedReasoningSummaries extracts only display-safe reasoning
// summaries from the final response object. Some gateways omit both deltas and
// output_item.done, exposing the completed response as the only source.
func responsesCompletedReasoningSummaries(payload []byte) []string {
	var event struct {
		Response struct {
			Output []responsesAPIReasoningOutputItem `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal(payload, &event) != nil {
		return nil
	}
	summaries := make([]string, 0, len(event.Response.Output))
	for _, item := range event.Response.Output {
		if summary := item.DisplaySummary(); summary != "" {
			summaries = append(summaries, summary)
		}
	}
	return summaries
}

func needsReasoningSummarySeparator(existing, summary string) bool {
	if existing == "" || summary == "" {
		return false
	}
	last, _ := utf8LastRune(existing)
	first, _ := utf8FirstRune(summary)
	return !unicode.IsSpace(last) && !unicode.IsSpace(first)
}

func utf8FirstRune(s string) (rune, bool) {
	for _, r := range s {
		return r, true
	}
	return 0, false
}

func utf8LastRune(s string) (rune, bool) {
	var last rune
	for _, r := range s {
		last = r
	}
	return last, last != 0
}

func isResponsesReasoningItemType(itemType string) bool {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "reasoning", "analysis", "reasoning_summary":
		return true
	default:
		return false
	}
}

const (
	responsesEventUnknown         responsesEventType = ""
	responsesEventOutputItemAdded responsesEventType = "response.output_item.added"
	responsesEventOutputTextDelta responsesEventType = "response.output_text.delta"
	// Responses-capable reasoning models emit their display-safe reasoning
	// summary through this event instead of chat-completions' reasoning_content.
	responsesEventReasoningSummaryTextDelta responsesEventType = "response.reasoning_summary_text.delta"
	// response.reasoning_summary_part.added carries the whole summary for one
	// part in part.text instead of streaming it through text deltas.
	responsesEventReasoningSummaryPartAdded responsesEventType = "response.reasoning_summary_part.added"
	// Keep the common compatible-provider spellings as aliases. Several
	// OpenAI-compatible Responses gateways use one of these names.
	responsesEventReasoningTextDelta         responsesEventType = "response.reasoning_text.delta"
	responsesEventReasoningContentDelta      responsesEventType = "response.reasoning_content.delta"
	responsesEventReasoningDelta             responsesEventType = "response.reasoning.delta"
	responsesEventFunctionCallArgumentsDelta responsesEventType = "response.function_call_arguments.delta"
	responsesEventFunctionCallArgumentsDone  responsesEventType = "response.function_call_arguments.done"
	responsesEventOutputItemDone             responsesEventType = "response.output_item.done"
	responsesEventCompleted                  responsesEventType = "response.completed"
	responsesEventFailed                     responsesEventType = "response.failed"
	responsesEventIncomplete                 responsesEventType = "response.incomplete"
	responsesEventError                      responsesEventType = "error"
)

func normalizeResponsesEventType(eventType string) responsesEventType {
	switch responsesEventType(strings.TrimSpace(eventType)) {
	case responsesEventOutputItemAdded:
		return responsesEventOutputItemAdded
	case responsesEventOutputTextDelta:
		return responsesEventOutputTextDelta
	case responsesEventReasoningSummaryTextDelta:
		return responsesEventReasoningSummaryTextDelta
	case responsesEventReasoningSummaryPartAdded:
		return responsesEventReasoningSummaryPartAdded
	case responsesEventReasoningTextDelta:
		return responsesEventReasoningTextDelta
	case responsesEventReasoningContentDelta:
		return responsesEventReasoningContentDelta
	case responsesEventReasoningDelta:
		return responsesEventReasoningDelta
	case responsesEventFunctionCallArgumentsDelta:
		return responsesEventFunctionCallArgumentsDelta
	case responsesEventFunctionCallArgumentsDone:
		return responsesEventFunctionCallArgumentsDone
	case responsesEventOutputItemDone:
		return responsesEventOutputItemDone
	case responsesEventCompleted:
		return responsesEventCompleted
	case responsesEventFailed:
		return responsesEventFailed
	case responsesEventIncomplete:
		return responsesEventIncomplete
	case responsesEventError:
		return responsesEventError
	default:
		return responsesEventUnknown
	}
}

// scanResponsesSSEEvent splits a Server-Sent Events stream into complete
// events. SSE permits both LF and CRLF line endings and multiple data lines.
func scanResponsesSSEEvent(data []byte, atEOF bool) (advance int, token []byte, err error) {
	crlfBoundary := bytes.Index(data, []byte("\r\n\r\n"))
	lfBoundary := bytes.Index(data, []byte("\n\n"))
	if lfBoundary >= 0 {
		// In a CRLF delimiter the \n\n substring starts at the first CRLF's
		// LF; it is not an independent LF separator.
		if lfBoundary > 0 && data[lfBoundary-1] == '\r' {
			lfBoundary = -1
		}
	}
	switch {
	case crlfBoundary >= 0 && (lfBoundary < 0 || crlfBoundary <= lfBoundary):
		return crlfBoundary + 4, bytes.TrimRight(data[:crlfBoundary], "\r\n"), nil
	case lfBoundary >= 0:
		return lfBoundary + 2, bytes.TrimRight(data[:lfBoundary], "\r\n"), nil
	}
	// Older compatible gateways sometimes omit the SSE blank separator. The
	// next event field still gives an unambiguous frame boundary. bufio.Scanner
	// may invoke a SplitFunc with an empty buffer, so do not slice data[1:]
	// unless there is a byte to skip.
	if len(data) > 1 {
		if nextEvent := bytes.Index(data[1:], []byte("\r\nevent:")); nextEvent >= 0 {
			boundary := nextEvent + 1
			return boundary + 2, bytes.TrimRight(data[:boundary], "\r\n"), nil
		}
		if nextEvent := bytes.Index(data[1:], []byte("\nevent:")); nextEvent >= 0 {
			boundary := nextEvent + 1
			return boundary + 1, bytes.TrimRight(data[:boundary], "\r\n"), nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), bytes.TrimRight(data, "\r\n"), nil
	}
	return 0, nil, nil
}

// parseResponsesSSEEvent returns a named event and its combined data payload.
// If a compatible gateway omits event:, Responses payloads conventionally
// carry their type in JSON, which is used as a safe fallback.
func parseResponsesSSEEvent(frame string) (eventType, payload string, ok bool) {
	var dataLines []string
	for _, line := range strings.Split(frame, "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			value := strings.TrimPrefix(line, "data:")
			value = strings.TrimPrefix(value, " ")
			dataLines = append(dataLines, value)
		}
	}
	if len(dataLines) == 0 {
		return "", "", false
	}
	payload = strings.Join(dataLines, "\n")
	if eventType == "" && payload != "[DONE]" {
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(payload), &envelope) == nil {
			eventType = envelope.Type
		}
	}
	return strings.TrimSpace(eventType), strings.TrimSpace(payload), true
}
