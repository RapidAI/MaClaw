package llm

// Streaming support for the OpenAI Responses API.  The Responses API uses
// named SSE events (rather than chat-completions' choices[].delta format), so
// it lives beside the non-streaming Responses parser instead of the chat
// stream implementation.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const responsesStreamMaxEventBytes = 256 * 1024

type responsesStreamItem struct {
	itemType string
	callID   string
	name     string
	args     strings.Builder
}

// DoResponsesAPIRequestStream sends a streaming Responses API request. Text
// and display-safe reasoning summaries are delivered separately so callers
// can render reasoning while the turn is still active.  Providers must never
// expose private chain-of-thought through this API; only the summaries they
// explicitly emit are forwarded.
func DoResponsesAPIRequestStream(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
	onText TokenCallback,
	onReasoning TokenCallback,
) (*Response, error) {
	req, _, endpoint, err := NewResponsesAPIRequest(ctx, cfg, messages, ResponsesAPIRequestOptions{
		Stream: true,
		Tools:  tools,
	})
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Body: append([]byte(nil), body...)}
	}

	// A few compatible providers accept stream=true but still return a regular
	// Response object. Preserve compatibility and surface its safe summary.
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		parsed, parseErr := ParseNonStreamResponsesAPIResponse(resp)
		if parseErr == nil && len(parsed.Choices) > 0 {
			msg := parsed.Choices[0].Message
			if msg.ReasoningContent != "" && onReasoning != nil {
				onReasoning(msg.ReasoningContent)
			}
			if msg.Content != "" && onText != nil {
				onText(msg.Content)
			}
		}
		return parsed, parseErr
	}

	var textBuf strings.Builder
	var reasoningBuf strings.Builder
	items := make(map[int]*responsesStreamItem)
	var usage *Usage
	finishReason := ""
	seenEvent := false

	emitReasoningSummary := func(summary string) {
		delta := appendResponsesStreamReasoning(&reasoningBuf, summary)
		if delta != "" && onReasoning != nil {
			onReasoning(delta)
		}
	}
	emitReasoningDelta := func(delta string) {
		if delta == "" {
			return
		}
		reasoningBuf.WriteString(delta)
		if onReasoning != nil {
			onReasoning(delta)
		}
	}
	emitTextSummary := func(text string) {
		delta := appendResponsesStreamText(&textBuf, text)
		if delta != "" && onText != nil {
			onText(delta)
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(scanResponsesAPIStreamEvent)
	scanner.Buffer(make([]byte, 0, 64*1024), responsesStreamMaxEventBytes)
	for scanner.Scan() {
		eventType, payload, ok := parseResponsesAPIStreamEvent(scanner.Text())
		if !ok {
			continue
		}
		seenEvent = true
		if payload == "[DONE]" {
			break
		}

		switch strings.TrimSpace(eventType) {
		case "response.output_item.added":
			var added struct {
				OutputIndex int                    `json:"output_index"`
				Item        map[string]interface{} `json:"item"`
			}
			if json.Unmarshal([]byte(payload), &added) != nil {
				continue
			}
			item := &responsesStreamItem{}
			item.itemType, _ = added.Item["type"].(string)
			item.callID, _ = added.Item["call_id"].(string)
			item.name, _ = added.Item["name"].(string)
			if args, _ := added.Item["arguments"].(string); args != "" {
				if len(args) > MaxToolArgumentsBytes {
					return nil, responsesStreamToolArgumentsTooLarge(item.name, len(args))
				}
				item.args.WriteString(args)
			}
			items[added.OutputIndex] = item

		case "response.output_text.delta":
			var delta struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &delta) == nil && delta.Delta != "" {
				textBuf.WriteString(delta.Delta)
				if onText != nil {
					onText(delta.Delta)
				}
			}
		case "response.output_text.done":
			var done struct {
				Text  string `json:"text"`
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &done) == nil {
				emitTextSummary(firstResponsesStreamText(done.Text, done.Delta))
			}

		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning_content.delta", "response.reasoning.delta":
			var delta struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &delta) == nil {
				emitReasoningDelta(delta.Delta)
			}
		case "response.reasoning_summary_text.done", "response.reasoning_text.done", "response.reasoning_content.done", "response.reasoning.done":
			var done struct {
				Text  string `json:"text"`
				Delta string `json:"delta"`
			}
			if json.Unmarshal([]byte(payload), &done) == nil {
				emitReasoningSummary(firstResponsesStreamText(done.Text, done.Delta))
			}

		case "response.function_call_arguments.delta":
			var delta struct {
				Delta       string `json:"delta"`
				OutputIndex int    `json:"output_index"`
			}
			if json.Unmarshal([]byte(payload), &delta) != nil || delta.Delta == "" {
				continue
			}
			item := items[delta.OutputIndex]
			if item == nil {
				item = &responsesStreamItem{itemType: "function_call"}
				items[delta.OutputIndex] = item
			}
			item.args.WriteString(delta.Delta)
			if item.args.Len() > MaxToolArgumentsBytes {
				return nil, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", item.name, item.args.Len(), MaxToolArgumentsBytes)
			}

		case "response.function_call_arguments.done":
			var done struct {
				Arguments   string `json:"arguments"`
				OutputIndex int    `json:"output_index"`
			}
			if json.Unmarshal([]byte(payload), &done) == nil && done.Arguments != "" {
				item := items[done.OutputIndex]
				if item == nil {
					item = &responsesStreamItem{itemType: "function_call"}
					items[done.OutputIndex] = item
				}
				if len(done.Arguments) > MaxToolArgumentsBytes {
					return nil, responsesStreamToolArgumentsTooLarge(item.name, len(done.Arguments))
				}
				item.args.Reset()
				item.args.WriteString(done.Arguments)
			}

		case "response.output_item.done":
			var done struct {
				OutputIndex int                    `json:"output_index"`
				Item        responsesAPIOutputItem `json:"item"`
			}
			if json.Unmarshal([]byte(payload), &done) == nil {
				emitReasoningSummary(responsesStreamDisplaySummary(done.Item))
				if done.Item.Type == "message" {
					emitTextSummary(responsesStreamMessageText(done.Item))
				}
				if done.Item.Type == "function_call" {
					item := items[done.OutputIndex]
					if item == nil {
						item = &responsesStreamItem{}
						items[done.OutputIndex] = item
					}
					item.itemType, item.callID, item.name = done.Item.Type, done.Item.CallID, done.Item.Name
					if done.Item.Arguments != "" {
						if len(done.Item.Arguments) > MaxToolArgumentsBytes {
							return nil, responsesStreamToolArgumentsTooLarge(item.name, len(done.Item.Arguments))
						}
						item.args.Reset()
						item.args.WriteString(done.Item.Arguments)
					}
				}
			}

		case "response.completed":
			if eventUsage := ExtractResponsesAPIUsageFromEventPayload([]byte(payload)); eventUsage != nil {
				usage = eventUsage
			}
			for _, summary := range responsesCompletedStreamReasoning(payload) {
				emitReasoningSummary(summary)
			}
			for _, text := range responsesCompletedStreamText(payload) {
				emitTextSummary(text)
			}
			for outputIndex, item := range responsesCompletedStreamToolCalls(payload) {
				if item.args.Len() > MaxToolArgumentsBytes {
					return nil, responsesStreamToolArgumentsTooLarge(item.name, item.args.Len())
				}
				items[outputIndex] = mergeResponsesStreamToolItem(items[outputIndex], item)
			}
			goto completed
		case "response.incomplete":
			finishReason = "length"
			goto completed
		case "response.failed", "error":
			return nil, responsesStreamEventError(payload)
		}
	}

completed:
	if err := scanner.Err(); err != nil {
		if textBuf.Len() > 0 || reasoningBuf.Len() > 0 || len(items) > 0 {
			return responsesStreamPartialResponse(textBuf.String(), reasoningBuf.String(), items, usage, finishReason), fmt.Errorf("Responses API SSE stream read error: %w", err)
		}
		return nil, fmt.Errorf("Responses API SSE stream read error: %w", err)
	}
	if !seenEvent {
		return nil, fmt.Errorf("Responses API stream contained no SSE events from %s", endpoint)
	}

	return responsesStreamPartialResponse(textBuf.String(), reasoningBuf.String(), items, usage, finishReason), nil
}

func responsesStreamPartialResponse(text, reasoning string, items map[int]*responsesStreamItem, usage *Usage, finishReason string) *Response {
	msg := Message{Role: "assistant", Content: StripAllExtra(text), ReasoningContent: reasoning}
	indices := make([]int, 0, len(items))
	for index := range items {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		item := items[index]
		if strings.EqualFold(strings.TrimSpace(item.itemType), "function_call") {
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: item.callID, Type: "function", Function: ToolCallFunction{Name: item.name, Arguments: item.args.String()}})
		}
	}
	if finishReason == "" {
		finishReason = "stop"
		if len(msg.ToolCalls) > 0 {
			finishReason = "tool_calls"
		}
	}
	finishReason, truncatedTools, truncatedArgs := filterStreamTruncatedToolCalls(&msg, finishReason)
	return &Response{Choices: []Choice{{Message: msg, FinishReason: finishReason, TruncatedToolNames: truncatedTools, TruncatedToolArgs: truncatedArgs}}, Usage: usage}
}

func responsesStreamDisplaySummary(item responsesAPIOutputItem) string {
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "reasoning", "analysis", "reasoning_summary":
	default:
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

func responsesStreamMessageText(item responsesAPIOutputItem) string {
	if !strings.EqualFold(strings.TrimSpace(item.Type), "message") {
		return ""
	}
	var out strings.Builder
	for _, part := range item.Content {
		switch part.Type {
		case "output_text", "text", "input_text":
			if part.Text != "" {
				out.WriteString(part.Text)
			} else {
				out.WriteString(part.Content)
			}
		}
	}
	return strings.TrimSpace(out.String())
}

func responsesCompletedStreamReasoning(payload string) []string {
	var completed struct {
		Response struct {
			Output []responsesAPIOutputItem `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &completed) != nil {
		return nil
	}
	var summaries []string
	for _, item := range completed.Response.Output {
		if summary := responsesStreamDisplaySummary(item); summary != "" {
			summaries = append(summaries, summary)
		}
	}
	return summaries
}

func responsesCompletedStreamText(payload string) []string {
	text := make([]string, 0)
	for _, item := range responsesCompletedStreamOutput(payload) {
		if content := responsesStreamMessageText(item); content != "" {
			text = append(text, content)
		}
	}
	return text
}

func responsesCompletedStreamToolCalls(payload string) map[int]*responsesStreamItem {
	output := responsesCompletedStreamOutput(payload)
	items := make(map[int]*responsesStreamItem)
	for index, item := range output {
		if !strings.EqualFold(strings.TrimSpace(item.Type), "function_call") {
			continue
		}
		streamItem := &responsesStreamItem{itemType: item.Type, callID: item.CallID, name: item.Name}
		streamItem.args.WriteString(item.Arguments)
		items[index] = streamItem
	}
	return items
}

// mergeResponsesStreamToolItem lets response.completed fill only the fields a
// compatible provider omitted from earlier stream events. It must not replace
// already-received delta arguments with an incomplete final snapshot.
func mergeResponsesStreamToolItem(existing, completed *responsesStreamItem) *responsesStreamItem {
	if existing == nil {
		return completed
	}
	if completed == nil {
		return existing
	}
	if existing.itemType == "" {
		existing.itemType = completed.itemType
	}
	if existing.callID == "" {
		existing.callID = completed.callID
	}
	if existing.name == "" {
		existing.name = completed.name
	}
	completedArgs := completed.args.String()
	if completedArgs == "" {
		return existing
	}
	existingArgs := existing.args.String()
	switch {
	case existingArgs == "", strings.HasPrefix(completedArgs, existingArgs):
		existing.args.Reset()
		existing.args.WriteString(completedArgs)
	}
	return existing
}

func responsesCompletedStreamOutput(payload string) []responsesAPIOutputItem {
	var completed struct {
		Response struct {
			Output []responsesAPIOutputItem `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &completed) != nil {
		return nil
	}
	return completed.Response.Output
}

func appendResponsesStreamReasoning(buf *strings.Builder, summary string) string {
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
		delta := summary[len(existing):]
		buf.WriteString(delta)
		return delta
	}
	buf.WriteByte('\n')
	buf.WriteString(summary)
	return "\n" + summary
}

func appendResponsesStreamText(buf *strings.Builder, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	existing := buf.String()
	if existing == "" {
		buf.WriteString(text)
		return text
	}
	if strings.Contains(existing, text) {
		return ""
	}
	if strings.HasPrefix(text, existing) {
		delta := text[len(existing):]
		buf.WriteString(delta)
		return delta
	}
	if strings.HasSuffix(existing, text) {
		return ""
	}
	buf.WriteByte('\n')
	buf.WriteString(text)
	return "\n" + text
}

func responsesStreamToolArgumentsTooLarge(name string, size int) error {
	if strings.TrimSpace(name) == "" {
		name = "function_call"
	}
	return fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", name, size, MaxToolArgumentsBytes)
}

func firstResponsesStreamText(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func responsesStreamEventError(payload string) error {
	var event struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		Response struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &event) == nil {
		message, code := event.Error.Message, event.Error.Code
		if message == "" {
			message, code = event.Response.Error.Message, event.Response.Error.Code
		}
		if message != "" {
			if code != "" {
				return fmt.Errorf("Responses API stream failed: %s (code=%s)", message, code)
			}
			return fmt.Errorf("Responses API stream failed: %s", message)
		}
	}
	return fmt.Errorf("Responses API stream failed: payload_len=%d", len(payload))
}

func scanResponsesAPIStreamEvent(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.Index(data, []byte("\r\n\r\n")); i >= 0 {
		return i + 4, bytes.TrimRight(data[:i], "\r\n"), nil
	}
	if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
		return i + 2, bytes.TrimRight(data[:i], "\r\n"), nil
	}
	// Some compatible gateways omit the blank SSE separator. The following
	// event field still provides an unambiguous boundary. Avoid data[1:] when
	// Scanner invokes the split function with an empty/one-byte buffer.
	if len(data) > 1 {
		if i := bytes.Index(data[1:], []byte("\r\nevent:")); i >= 0 {
			boundary := i + 1
			return boundary + 2, bytes.TrimRight(data[:boundary], "\r\n"), nil
		}
		if i := bytes.Index(data[1:], []byte("\nevent:")); i >= 0 {
			boundary := i + 1
			return boundary + 1, bytes.TrimRight(data[:boundary], "\r\n"), nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), bytes.TrimRight(data, "\r\n"), nil
	}
	return 0, nil, nil
}

func parseResponsesAPIStreamEvent(frame string) (eventType, payload string, ok bool) {
	var lines []string
	for _, line := range strings.Split(frame, "\n") {
		line = strings.TrimSuffix(line, "\r")
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if len(lines) == 0 {
		return "", "", false
	}
	payload = strings.Join(lines, "\n")
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
