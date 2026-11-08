package llmservice

import (
	"encoding/json"
	"io"
	"strings"
)

// ProbeStream is the text, tool calls, and end marker read from one probe's
// upstream SSE body. Err is an error object inside the stream, including a
// JSON error body that had no SSE frames. End is false when the protocol's
// completion marker never arrived.
type ProbeStream struct {
	Reply string
	Calls []ProviderToolCall
	End   bool
	Err   string
}

// Failure is the probe error for this stream, or empty when the stream
// completed with text or a tool call. The local and hop probes share it.
func (p ProbeStream) Failure() string {
	switch {
	case p.Err != "":
		return "model returned an error: " + p.Err
	case !p.End:
		return "stream ended before the end marker"
	case strings.TrimSpace(p.Reply) == "" && len(p.Calls) == 0:
		return "model returned no completion content"
	default:
		return ""
	}
}

const probeBodyLimit = 64 << 10

// ReadProbeBody reads a probe response up to the availability-test limit.
func ReadProbeBody(r io.Reader) (data []byte, truncated bool, err error) {
	limited := &io.LimitedReader{R: r, N: probeBodyLimit + 1}
	data, err = io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	return data, len(data) > probeBodyLimit, nil
}

// ParseProbeStream assembles one streaming probe. kind is openai, anthropic,
// responses, or google. data: [DONE] ends any of them. Anthropic also ends on
// message_stop, Responses on response.completed or response.incomplete, and
// Google on a non-empty finishReason. OpenAI also ends when a fully read body
// carries a legal finish_reason (stop, length, tool_calls, function_call, or
// content_filter). Callers parse only after a clean read, so a transport cut
// never reaches this function and is not treated as that ending.
func ParseProbeStream(body []byte, kind string) ProbeStream {
	kind = strings.ToLower(strings.TrimSpace(kind))
	var text, reasoning strings.Builder
	calls := map[int]*probeToolAcc{}
	var order []int
	var out ProbeStream
	framed := false
	googleCalls := map[string]struct{}{}
	var legacy probeToolAcc
	forEachProbeSSE(body, func(event, data string) {
		framed = true
		data = strings.TrimSpace(data)
		if data == "" {
			// Anthropic and Responses name the end on the SSE event line.
			// A frame with that name and no data payload is still the marker.
			if probeNamedEnd(kind, event) {
				out.End = true
			}
			return
		}
		if data == "[DONE]" {
			out.End = true
			return
		}
		switch kind {
		case "anthropic":
			if event == "message_stop" || probeAnthropicEvent(data, &text, &reasoning, calls, &order) {
				out.End = true
			}
		case "responses":
			if probeResponsesEvent(event, data, &text, &reasoning, calls, &order) {
				out.End = true
			}
		case "google":
			if probeGoogleEvent(data, &text, &reasoning, calls, &order, googleCalls) {
				out.End = true
			}
		default:
			if probeOpenAIEvent(data, &text, &reasoning, calls, &order, &legacy) {
				out.End = true
			}
		}
		if msg := probeStreamError(event, data); msg != "" && out.Err == "" {
			out.Err = msg
		}
	})
	// A 200 JSON error with no SSE framing is the model's error. A JSON chat
	// completion is not a stream: it stays on the missing-marker path.
	if !framed && out.Err == "" {
		if msg := probeStreamError("", strings.TrimSpace(string(body))); msg != "" {
			out.Err = msg
		}
	}
	reply := strings.TrimSpace(text.String())
	if reply == "" {
		reply = strings.TrimSpace(reasoning.String())
	}
	out.Reply = reply
	for _, idx := range order {
		call := calls[idx]
		if call == nil {
			continue
		}
		name := strings.TrimSpace(call.name)
		args := call.args.String()
		if name == "" && strings.TrimSpace(args) == "" {
			continue
		}
		out.Calls = append(out.Calls, ProviderToolCall{Name: name, Arguments: args})
	}
	// A legacy function_call is the tool call only when tool_calls never started.
	// An indexed slot wins even if that slot had no name yet.
	if len(order) == 0 && (strings.TrimSpace(legacy.name) != "" || strings.TrimSpace(legacy.args.String()) != "") {
		out.Calls = append(out.Calls, ProviderToolCall{Name: strings.TrimSpace(legacy.name), Arguments: legacy.args.String()})
	}
	return out
}

type probeToolAcc struct {
	name string
	args strings.Builder
}

func (a *probeToolAcc) setName(name string) {
	name = strings.TrimSpace(name)
	if name != "" {
		a.name = name
	}
}

// fillName keeps the first name. Responses snapshots repeat the name on later
// events and must not replace the one already taken from the stream.
func (a *probeToolAcc) fillName(name string) {
	if a == nil || a.name != "" {
		return
	}
	a.setName(name)
}

func (a *probeToolAcc) addArgs(raw any) {
	switch typed := raw.(type) {
	case nil:
	case string:
		a.args.WriteString(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err == nil {
			a.args.Write(encoded)
		}
	}
}

func probeTool(calls map[int]*probeToolAcc, order *[]int, index int) *probeToolAcc {
	if call := calls[index]; call != nil {
		return call
	}
	call := &probeToolAcc{}
	calls[index] = call
	*order = append(*order, index)
	return call
}

// forEachProbeSSE delivers one callback per data line. These protocols put one
// JSON value on each data line. A blank line is the SSE event separator, but
// several gateways omit it and write the next data line immediately. Joining
// those lines into one payload hides [DONE] and a later finishReason.
// An event name with no data, including message_stop and response.completed,
// is delivered when the event ends.
func forEachProbeSSE(body []byte, fn func(event, data string)) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	event := ""
	sawData := false
	flushName := func() {
		if event != "" && !sawData {
			fn(event, "")
		}
		event = ""
		sawData = false
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushName()
			continue
		}
		switch {
		case strings.HasPrefix(strings.ToLower(trimmed), "event:"):
			if sawData {
				event = ""
				sawData = false
			}
			event = strings.TrimSpace(trimmed[len("event:"):])
		case strings.HasPrefix(strings.ToLower(trimmed), "data:"):
			fn(event, strings.TrimSpace(trimmed[len("data:"):]))
			sawData = true
		}
	}
	flushName()
}

func probeNamedEnd(kind, event string) bool {
	switch kind {
	case "anthropic":
		return event == "message_stop"
	case "responses":
		return event == "response.completed" || event == "response.incomplete"
	default:
		return false
	}
}

func probeStreamError(event, payload string) string {
	var doc struct {
		Type     string          `json:"type"`
		Message  string          `json:"message"`
		Error    json.RawMessage `json:"error"`
		Response struct {
			Error json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return ""
	}
	if msg := probeStreamErrorText(doc.Error); msg != "" {
		return msg
	}
	if strings.EqualFold(doc.Type, "error") && strings.TrimSpace(doc.Message) != "" {
		return strings.TrimSpace(doc.Message)
	}
	// response.failed carries the provider message under response.error and
	// often omits a top-level type, leaving the name on the SSE event line.
	// A completion marker is not an error, including one whose error is null.
	kind := strings.ToLower(strings.TrimSpace(doc.Type))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(event))
	}
	if kind == "response.failed" || kind == "error" {
		return probeStreamErrorText(doc.Response.Error)
	}
	return ""
}

func probeStreamErrorText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && strings.TrimSpace(asString) != "" {
		return strings.TrimSpace(asString)
	}
	var obj struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if msg := strings.TrimSpace(obj.Message); msg != "" {
		return msg
	}
	return strings.TrimSpace(obj.Code)
}

// probeOpenAIEvent reports whether this chunk carries a legal finish_reason.
// [DONE] is still an end marker. A missing or unknown reason does not end the
// stream: a clean close after only a partial delta stays unfinished.
func probeOpenAIEvent(payload string, text, reasoning *strings.Builder, calls map[int]*probeToolAcc, order *[]int, legacy *probeToolAcc) bool {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
				Thinking         string `json:"thinking"`
				ToolCalls        []struct {
					Index    int `json:"index"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
				// function_call is the legacy single-call delta. Arguments stay a
				// string, matching sseFunctionCallDelta: a non-string fails this
				// chunk the same way the live parser drops it.
				FunctionCall *struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function_call"`
			} `json:"delta"`
			// null becomes empty. A non-string fails this chunk, matching the
			// live parser's string field.
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil || len(chunk.Choices) == 0 {
		return false
	}
	choice := chunk.Choices[0]
	if choice.Delta.Content != "" {
		text.WriteString(choice.Delta.Content)
	} else if choice.Message.Content != "" {
		text.WriteString(choice.Message.Content)
	}
	// Same priority as openAIReasoningText: reasoning_content, then reasoning,
	// then thinking.
	switch {
	case choice.Delta.ReasoningContent != "":
		reasoning.WriteString(choice.Delta.ReasoningContent)
	case choice.Delta.Reasoning != "":
		reasoning.WriteString(choice.Delta.Reasoning)
	case choice.Delta.Thinking != "":
		reasoning.WriteString(choice.Delta.Thinking)
	}
	if legacy != nil && choice.Delta.FunctionCall != nil {
		fc := choice.Delta.FunctionCall
		if fc.Name != "" {
			if legacy.name == "" {
				legacy.name = fc.Name
			} else {
				legacy.name += fc.Name
			}
		}
		if fc.Arguments != "" {
			legacy.args.WriteString(fc.Arguments)
		}
	}
	for _, call := range choice.Delta.ToolCalls {
		acc := probeTool(calls, order, call.Index)
		acc.setName(call.Function.Name)
		if len(call.Function.Arguments) > 0 && string(call.Function.Arguments) != "null" {
			var raw any
			if err := json.Unmarshal(call.Function.Arguments, &raw); err == nil {
				acc.addArgs(raw)
			}
		}
	}
	for i, call := range choice.Message.ToolCalls {
		acc := probeTool(calls, order, i)
		acc.setName(call.Function.Name)
		if len(call.Function.Arguments) > 0 && string(call.Function.Arguments) != "null" {
			var raw any
			if err := json.Unmarshal(call.Function.Arguments, &raw); err == nil {
				acc.addArgs(raw)
			}
		}
	}
	return probeLegalOpenAIFinish(choice.FinishReason)
}

// probeLegalOpenAIFinish is the OpenAI chat completion stop set. Anything else,
// including an empty reason, "error", or a partial close, is not an ending.
func probeLegalOpenAIFinish(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "stop", "length", "tool_calls", "function_call", "content_filter":
		return true
	default:
		return false
	}
}

// probeAnthropicEvent reports whether this payload is the message_stop marker.
func probeAnthropicEvent(payload string, text, reasoning *strings.Builder, calls map[int]*probeToolAcc, order *[]int) bool {
	var event struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
		ContentBlock *struct {
			Type     string `json:"type"`
			Name     string `json:"name"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"content_block"`
	}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return false
	}
	switch event.Type {
	case "content_block_start":
		if event.ContentBlock == nil {
			return false
		}
		switch event.ContentBlock.Type {
		case "text":
			text.WriteString(event.ContentBlock.Text)
		case "thinking":
			thought := event.ContentBlock.Thinking
			if thought == "" {
				thought = event.ContentBlock.Text
			}
			reasoning.WriteString(thought)
		case "tool_use":
			probeTool(calls, order, event.Index).setName(event.ContentBlock.Name)
		}
	case "content_block_delta":
		switch event.Delta.Type {
		case "text_delta":
			text.WriteString(event.Delta.Text)
		case "thinking_delta", "reasoning_delta":
			// anthropicDeltaThinkingText: thinking wins, otherwise the text field.
			if event.Delta.Thinking != "" {
				reasoning.WriteString(event.Delta.Thinking)
			} else {
				reasoning.WriteString(event.Delta.Text)
			}
		case "input_json_delta":
			probeTool(calls, order, event.Index).addArgs(event.Delta.PartialJSON)
		}
	case "message_stop":
		return true
	}
	return false
}

type probeResponsesPart struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content string `json:"content"`
}

type probeResponsesItem struct {
	Type      string               `json:"type"`
	Name      string               `json:"name"`
	Arguments json.RawMessage      `json:"arguments"`
	Content   []probeResponsesPart `json:"content"`
	Summary   []probeResponsesPart `json:"summary"`
}

func probeResponsesEvent(event, payload string, text, reasoning *strings.Builder, calls map[int]*probeToolAcc, order *[]int) bool {
	var doc struct {
		Type        string             `json:"type"`
		Delta       any                `json:"delta"`
		Text        string             `json:"text"`
		Arguments   json.RawMessage    `json:"arguments"`
		OutputIndex int                `json:"output_index"`
		Part        probeResponsesPart `json:"part"`
		Item        probeResponsesItem `json:"item"`
		Response    struct {
			Output []probeResponsesItem `json:"output"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return false
	}
	kind := doc.Type
	if kind == "" {
		kind = event
	}
	switch kind {
	case "response.output_text.delta":
		if delta, ok := doc.Delta.(string); ok {
			text.WriteString(delta)
		}
	case "response.output_text.done":
		// A provider may send the whole answer only on .done. The snapshot
		// merges with deltas so a later copy is not written twice.
		probeAppendSnapshot(text, probeResponsesDoneText(doc.Text, doc.Delta), false)
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning_content.delta", "response.reasoning.delta":
		if delta, ok := doc.Delta.(string); ok {
			reasoning.WriteString(delta)
		}
	case "response.reasoning_summary_text.done", "response.reasoning_text.done", "response.reasoning_content.done", "response.reasoning.done":
		probeAppendSnapshot(reasoning, probeResponsesDoneText(doc.Text, doc.Delta), true)
	case "response.reasoning_summary_part.added":
		probeAppendSnapshot(reasoning, doc.Part.Text, true)
	case "response.output_item.added":
		if strings.EqualFold(strings.TrimSpace(doc.Item.Type), "function_call") {
			probeResponsesFunction(calls, order, doc.OutputIndex, doc.Item.Name, doc.Item.Arguments, false)
		}
	case "response.function_call_arguments.delta":
		probeTool(calls, order, doc.OutputIndex).addArgs(doc.Delta)
	case "response.function_call_arguments.done":
		if args, ok := probeJSONString(doc.Arguments); ok && args != "" {
			probeResponsesFunction(calls, order, doc.OutputIndex, "", doc.Arguments, true)
		}
	case "response.output_item.done":
		probeResponsesDoneItem(text, reasoning, calls, order, doc.OutputIndex, doc.Item)
	case "response.completed", "response.incomplete":
		for i, item := range doc.Response.Output {
			probeResponsesDoneItem(text, reasoning, calls, order, i, item)
		}
		return true
	}
	return false
}

// probeResponsesDoneText prefers the text field, then a string delta. That is
// the order firstResponsesStreamText uses for *.done events.
func probeResponsesDoneText(text string, delta any) string {
	if text != "" {
		return text
	}
	if typed, ok := delta.(string); ok {
		return typed
	}
	return ""
}

func probeResponsesDoneItem(text, reasoning *strings.Builder, calls map[int]*probeToolAcc, order *[]int, index int, item probeResponsesItem) {
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "message":
		probeAppendSnapshot(text, probeResponsesMessageText(item.Content), false)
	case "reasoning", "analysis", "reasoning_summary":
		probeAppendSnapshot(reasoning, probeResponsesReasoningText(item.Summary, item.Content), true)
	case "function_call":
		probeResponsesFunction(calls, order, index, item.Name, item.Arguments, true)
	}
}

func probeResponsesFunction(calls map[int]*probeToolAcc, order *[]int, index int, name string, raw json.RawMessage, snapshot bool) {
	acc := probeTool(calls, order, index)
	acc.fillName(name)
	if snapshot {
		if args, ok := probeJSONString(raw); ok {
			if args != "" {
				probeMergeArgs(acc, args)
			}
			return
		}
		// A non-string snapshot is kept only when no arguments arrived yet, so
		// a later completed object does not append a second copy.
		if acc.args.Len() > 0 || len(raw) == 0 || string(raw) == "null" {
			return
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			acc.addArgs(decoded)
		}
		return
	}
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		acc.addArgs(decoded)
	}
}

func probeResponsesMessageText(parts []probeResponsesPart) string {
	var b strings.Builder
	for _, part := range parts {
		switch part.Type {
		case "", "output_text", "text", "input_text":
			if part.Text != "" {
				b.WriteString(part.Text)
			} else {
				b.WriteString(part.Content)
			}
		}
	}
	return b.String()
}

func probeResponsesReasoningText(summary, content []probeResponsesPart) string {
	parts := summary
	if len(parts) == 0 {
		parts = content
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Text != "" {
			b.WriteString(part.Text)
			continue
		}
		switch part.Type {
		case "", "summary_text", "reasoning_text", "reasoning_content", "text", "output_text", "thinking":
			b.WriteString(part.Content)
		}
	}
	return b.String()
}

// probeAppendSnapshot merges a later full snapshot into text already taken
// from deltas. A repeat or an extension is not written twice. A different
// snapshot stays on its own line, matching the live Responses stream parser.
func probeAppendSnapshot(buf *strings.Builder, snapshot string, reasoning bool) {
	snapshot = strings.TrimSpace(snapshot)
	if buf == nil || snapshot == "" {
		return
	}
	existing := buf.String()
	if existing == "" {
		buf.WriteString(snapshot)
		return
	}
	if strings.Contains(existing, snapshot) {
		return
	}
	if strings.HasPrefix(snapshot, existing) {
		buf.WriteString(snapshot[len(existing):])
		return
	}
	if !reasoning && strings.HasSuffix(existing, snapshot) {
		return
	}
	buf.WriteByte('\n')
	buf.WriteString(snapshot)
}

func probeMergeArgs(acc *probeToolAcc, candidate string) {
	if acc == nil || candidate == "" {
		return
	}
	current := acc.args.String()
	if current != "" && !strings.HasPrefix(candidate, current) {
		return
	}
	acc.args.Reset()
	acc.args.WriteString(candidate)
}

func probeJSONString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false
	}
	return text, true
}

func probeGoogleEvent(payload string, text, reasoning *strings.Builder, calls map[int]*probeToolAcc, order *[]int, seen map[string]struct{}) bool {
	var doc struct {
		Candidates []struct {
			FinishReason string `json:"finishReason"`
			Content      struct {
				Parts []struct {
					Text             string `json:"text"`
					Thought          bool   `json:"thought"`
					ThoughtSignature string `json:"thoughtSignature"`
					FunctionCall     struct {
						ID   string          `json:"id"`
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil || len(doc.Candidates) == 0 {
		return false
	}
	if seen == nil {
		seen = map[string]struct{}{}
	}
	candidate := doc.Candidates[0]
	for _, part := range candidate.Content.Parts {
		name := strings.TrimSpace(part.FunctionCall.Name)
		callID := strings.TrimSpace(part.FunctionCall.ID)
		hasCall := name != "" || callID != "" || (len(part.FunctionCall.Args) > 0 && string(part.FunctionCall.Args) != "null")
		if hasCall {
			// Gemini repeats a finished functionCall on the chunk that carries
			// finishReason. The live stream parser keeps the first one. A call
			// with a different name, args, signature, or id is a second call.
			if name != "" {
				args := probeGoogleArgs(part.FunctionCall.Args)
				key := name + "\n" + args + "\n" + strings.TrimSpace(part.ThoughtSignature) + "\n" + callID
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
					acc := probeTool(calls, order, len(*order))
					acc.setName(name)
					acc.addArgs(args)
				}
			}
			continue
		}
		if part.Thought {
			reasoning.WriteString(part.Text)
			continue
		}
		text.WriteString(part.Text)
	}
	return strings.TrimSpace(candidate.FinishReason) != ""
}

func probeGoogleArgs(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return "{}"
	}
	data, err := json.Marshal(obj)
	if err != nil || len(data) == 0 {
		return "{}"
	}
	return string(data)
}
