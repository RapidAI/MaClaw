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
// Google on a non-empty finishReason.
func ParseProbeStream(body []byte, kind string) ProbeStream {
	kind = strings.ToLower(strings.TrimSpace(kind))
	var text, reasoning strings.Builder
	calls := map[int]*probeToolAcc{}
	var order []int
	var out ProbeStream
	framed := false
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
			if probeGoogleEvent(data, &text, &reasoning, calls, &order) {
				out.End = true
			}
		default:
			probeOpenAIEvent(data, &text, &reasoning, calls, &order)
		}
		if msg := probeStreamError(data); msg != "" && out.Err == "" {
			out.Err = msg
		}
	})
	// A 200 JSON error with no SSE framing is the model's error. A JSON chat
	// completion is not a stream: it stays on the missing-marker path.
	if !framed && out.Err == "" {
		if msg := probeStreamError(strings.TrimSpace(string(body))); msg != "" {
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

func forEachProbeSSE(body []byte, fn func(event, data string)) {
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, frame := range strings.Split(text, "\n\n") {
		event := ""
		var data []string
		for _, line := range strings.Split(frame, "\n") {
			line = strings.TrimRight(line, "\r")
			switch {
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if event == "" && len(data) == 0 {
			continue
		}
		fn(event, strings.Join(data, "\n"))
	}
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

func probeStreamError(payload string) string {
	var doc struct {
		Type    string          `json:"type"`
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return ""
	}
	if len(doc.Error) > 0 && string(doc.Error) != "null" {
		var asString string
		if err := json.Unmarshal(doc.Error, &asString); err == nil && strings.TrimSpace(asString) != "" {
			return strings.TrimSpace(asString)
		}
		var obj struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(doc.Error, &obj); err == nil && strings.TrimSpace(obj.Message) != "" {
			return strings.TrimSpace(obj.Message)
		}
	}
	if strings.EqualFold(doc.Type, "error") && strings.TrimSpace(doc.Message) != "" {
		return strings.TrimSpace(doc.Message)
	}
	return ""
}

func probeOpenAIEvent(payload string, text, reasoning *strings.Builder, calls map[int]*probeToolAcc, order *[]int) {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
				Reasoning        string `json:"reasoning"`
				ToolCalls        []struct {
					Index    int `json:"index"`
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			Message struct {
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
		return
	}
	choice := chunk.Choices[0]
	if choice.Delta.Content != "" {
		text.WriteString(choice.Delta.Content)
	} else if choice.Message.Content != "" {
		text.WriteString(choice.Message.Content)
	}
	if choice.Delta.ReasoningContent != "" {
		reasoning.WriteString(choice.Delta.ReasoningContent)
	} else if choice.Delta.Reasoning != "" {
		reasoning.WriteString(choice.Delta.Reasoning)
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
			reasoning.WriteString(event.ContentBlock.Thinking)
		case "tool_use":
			probeTool(calls, order, event.Index).setName(event.ContentBlock.Name)
		}
	case "content_block_delta":
		switch event.Delta.Type {
		case "text_delta":
			text.WriteString(event.Delta.Text)
		case "thinking_delta":
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

func probeResponsesEvent(event, payload string, text, reasoning *strings.Builder, calls map[int]*probeToolAcc, order *[]int) bool {
	var doc struct {
		Type        string `json:"type"`
		Delta       any    `json:"delta"`
		OutputIndex int    `json:"output_index"`
		Item        struct {
			Type      string          `json:"type"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"item"`
		Response struct {
			Output []struct {
				Type    string `json:"type"`
				Name    string `json:"name"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Summary []struct {
					Text string `json:"text"`
				} `json:"summary"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"output"`
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
	case "response.reasoning_summary_text.delta":
		if delta, ok := doc.Delta.(string); ok {
			reasoning.WriteString(delta)
		}
	case "response.output_item.added":
		if doc.Item.Type == "function_call" {
			acc := probeTool(calls, order, doc.OutputIndex)
			acc.setName(doc.Item.Name)
			if len(doc.Item.Arguments) > 0 && string(doc.Item.Arguments) != "null" {
				var raw any
				if err := json.Unmarshal(doc.Item.Arguments, &raw); err == nil {
					acc.addArgs(raw)
				}
			}
		}
	case "response.function_call_arguments.delta":
		probeTool(calls, order, doc.OutputIndex).addArgs(doc.Delta)
	case "response.completed", "response.incomplete":
		if strings.TrimSpace(text.String()) == "" && len(*order) == 0 {
			for i, item := range doc.Response.Output {
				switch item.Type {
				case "message":
					for _, part := range item.Content {
						if part.Type == "" || part.Type == "output_text" {
							text.WriteString(part.Text)
						}
					}
				case "reasoning":
					for _, part := range item.Summary {
						reasoning.WriteString(part.Text)
					}
				case "function_call":
					acc := probeTool(calls, order, i)
					acc.setName(item.Name)
					if len(item.Arguments) > 0 && string(item.Arguments) != "null" {
						var raw any
						if err := json.Unmarshal(item.Arguments, &raw); err == nil {
							acc.addArgs(raw)
						}
					}
				}
			}
		}
		return true
	}
	return false
}

func probeGoogleEvent(payload string, text, reasoning *strings.Builder, calls map[int]*probeToolAcc, order *[]int) bool {
	var doc struct {
		Candidates []struct {
			FinishReason string `json:"finishReason"`
			Content      struct {
				Parts []struct {
					Text         string `json:"text"`
					Thought      bool   `json:"thought"`
					FunctionCall struct {
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
	candidate := doc.Candidates[0]
	for _, part := range candidate.Content.Parts {
		if part.FunctionCall.Name != "" || len(part.FunctionCall.Args) > 0 {
			acc := probeTool(calls, order, len(*order))
			acc.setName(part.FunctionCall.Name)
			if len(part.FunctionCall.Args) > 0 && string(part.FunctionCall.Args) != "null" {
				var raw any
				if err := json.Unmarshal(part.FunctionCall.Args, &raw); err == nil {
					acc.addArgs(raw)
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
