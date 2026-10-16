package windsurf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Upstream speaks the Devin Cascade API while presenting OpenAI chat and
// model endpoints to CodexProxy.
type Upstream struct{}

func (Upstream) DoModels(ctx context.Context, endpoint, apiKey, _ string) (*http.Response, error) {
	models, err := ListModels(ctx, APIRoot(endpoint), apiKey)
	if err != nil {
		return errorResponse(http.StatusBadGateway, err.Error()), nil
	}
	data := make([]map[string]string, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]string{"id": model.ID, "name": model.Name})
	}
	raw, _ := json.Marshal(map[string]any{"object": "list", "data": data})
	return jsonResponse(http.StatusOK, raw), nil
}

func (Upstream) DoChatCompletions(ctx context.Context, endpoint, apiKey, _ string, body []byte, _ string, stream bool) (*http.Response, error) {
	req, err := parseOpenAIChat(body)
	if err != nil {
		return errorResponse(http.StatusBadRequest, err.Error()), nil
	}
	if !stream {
		return completeChat(ctx, APIRoot(endpoint), apiKey, req)
	}
	return streamChat(ctx, APIRoot(endpoint), apiKey, req)
}

// APIRoot drops the OpenAI suffix CodexProxy appends to the Devin API host.
func APIRoot(endpoint string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(strings.TrimSpace(endpoint), "/")
	}
	path := parsed.Path
	for _, suffix := range []string{"/chat/completions", "/models", "/v1"} {
		path = strings.TrimSuffix(path, suffix)
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func parseOpenAIChat(body []byte) (chatRequest, error) {
	var raw struct {
		Model               string          `json:"model"`
		Messages            []openAIMessage `json:"messages"`
		Tools               []openAITool    `json:"tools"`
		ToolChoice          json.RawMessage `json:"tool_choice"`
		ParallelToolCalls   *bool           `json:"parallel_tool_calls"`
		MaxTokens           int             `json:"max_tokens"`
		MaxCompletionTokens int             `json:"max_completion_tokens"`
		Temperature         *float64        `json:"temperature"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return chatRequest{}, fmt.Errorf("无法解析对话请求")
	}
	req := chatRequest{model: strings.TrimSpace(raw.Model), temperature: raw.Temperature}
	req.maxTokens = raw.MaxCompletionTokens
	if req.maxTokens <= 0 {
		req.maxTokens = raw.MaxTokens
	}
	for _, msg := range raw.Messages {
		text, images := messageContent(msg.Content)
		item := chatMessage{role: strings.TrimSpace(msg.Role), text: text, toolCallID: msg.ToolCallID, images: images}
		for _, call := range msg.ToolCalls {
			name := call.Function.Name
			args := call.Function.Arguments
			if name == "" {
				name = call.Name
			}
			if args == "" {
				args = call.Arguments
			}
			item.toolCalls = append(item.toolCalls, toolCall{id: call.ID, name: name, args: args})
		}
		req.messages = append(req.messages, item)
	}
	for _, tool := range raw.Tools {
		name := tool.Function.Name
		if name == "" {
			name = tool.Name
		}
		if name == "" {
			continue
		}
		params := "{}"
		if len(tool.Function.Parameters) > 0 {
			params = string(tool.Function.Parameters)
		} else if len(tool.Parameters) > 0 {
			params = string(tool.Parameters)
		}
		desc := tool.Function.Description
		if desc == "" {
			desc = tool.Description
		}
		req.tools = append(req.tools, toolDef{name: name, description: desc, parameters: params})
	}
	applyToolChoice(&req, raw.ToolChoice)
	if raw.ParallelToolCalls != nil && !*raw.ParallelToolCalls {
		req.disableParallel = true
	}
	return req, nil
}

// applyToolChoice maps the agent loop's OpenAI tool_choice onto Devin.
// "none" drops the tool list so the model cannot call one. A named function
// is sent as ChatToolChoice.tool_name and the other tools are left out.
// "auto" and "required" keep the list; the Cascade schema's option_name
// strings are not set by the installed client, so they are not invented here.
func applyToolChoice(req *chatRequest, raw json.RawMessage) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return
	}
	var mode string
	if json.Unmarshal(raw, &mode) == nil {
		if strings.EqualFold(strings.TrimSpace(mode), "none") {
			req.tools = nil
		}
		return
	}
	var choice struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &choice) != nil {
		return
	}
	if strings.EqualFold(choice.Type, "none") {
		req.tools = nil
		return
	}
	name := strings.TrimSpace(choice.Function.Name)
	if name == "" {
		name = strings.TrimSpace(choice.Name)
	}
	if !strings.EqualFold(choice.Type, "function") || name == "" || !toolOffered(req.tools, name) {
		return
	}
	req.forcedTool = name
	kept := make([]toolDef, 0, 1)
	for _, tool := range req.tools {
		if tool.name == name {
			kept = append(kept, tool)
		}
	}
	req.tools = kept
}

type openAIMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Function  struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type openAITool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Function    struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func messageText(raw json.RawMessage) string {
	text, _ := messageContent(raw)
	return text
}

func messageContent(raw json.RawMessage) (string, []chatImage) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return "", nil
	}
	var b strings.Builder
	var images []chatImage
	for _, part := range parts {
		var item struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			ImageURL json.RawMessage `json:"image_url"`
			Source   struct {
				Type      string `json:"type"`
				MediaType string `json:"media_type"`
				Data      string `json:"data"`
			} `json:"source"`
		}
		if json.Unmarshal(part, &item) != nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(item.Type)) {
		case "", "text":
			if item.Text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(item.Text)
		case "image_url", "input_image", "image":
			if image, ok := imageFromPart(item.ImageURL, item.Source.MediaType, item.Source.Data); ok {
				images = append(images, image)
			}
		}
	}
	return b.String(), images
}

func imageFromPart(imageURL json.RawMessage, mediaType, inline string) (chatImage, bool) {
	if image, ok := imageFromDataURL(inline, mediaType); ok {
		return image, true
	}
	imageURL = bytes.TrimSpace(imageURL)
	if len(imageURL) == 0 || string(imageURL) == "null" {
		return chatImage{}, false
	}
	var urlText string
	if json.Unmarshal(imageURL, &urlText) != nil {
		var wrapped struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(imageURL, &wrapped) != nil {
			return chatImage{}, false
		}
		urlText = wrapped.URL
	}
	return imageFromDataURL(urlText, mediaType)
}

func imageFromDataURL(raw, mime string) (chatImage, bool) {
	raw = strings.TrimSpace(raw)
	mime = strings.TrimSpace(mime)
	if raw == "" {
		return chatImage{}, false
	}
	if !strings.HasPrefix(strings.ToLower(raw), "data:") {
		if strings.Contains(raw, "://") {
			return chatImage{}, false
		}
		payload := stripBase64Space(raw)
		if mime == "" || payload == "" {
			return chatImage{}, false
		}
		return chatImage{mime: mime, data: payload}, true
	}
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return chatImage{}, false
	}
	meta := raw[len("data:"):comma]
	payload := stripBase64Space(raw[comma+1:])
	if payload == "" || !strings.Contains(strings.ToLower(meta), "base64") {
		return chatImage{}, false
	}
	if semi := strings.Index(meta, ";"); semi >= 0 {
		meta = meta[:semi]
	}
	if meta != "" {
		mime = meta
	}
	if mime == "" {
		mime = "image/png"
	}
	return chatImage{mime: mime, data: payload}, true
}

const maxSSEData = 32 * 1024

func writeSplitDelta(writeChoices func(any) error, field, text string, emitted *bool) error {
	for _, part := range splitUTF8(text, maxSSEData) {
		if part == "" {
			continue
		}
		if err := writeChoices([]map[string]any{{"index": 0, "delta": map[string]string{field: part}}}); err != nil {
			return err
		}
		*emitted = true
	}
	return nil
}

func splitUTF8(text string, max int) []string {
	if text == "" || max <= 0 || len(text) <= max {
		return []string{text}
	}
	var out []string
	for len(text) > 0 {
		n := max
		if n >= len(text) {
			n = len(text)
		} else {
			for n > 0 && !utf8.RuneStart(text[n]) {
				n--
			}
			if n == 0 {
				n = 1
				for n < len(text) && !utf8.RuneStart(text[n]) {
					n++
				}
			}
		}
		out = append(out, text[:n])
		text = text[n:]
	}
	return out
}

func contextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func stripBase64Space(raw string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, raw)
}

func completeChat(ctx context.Context, apiServer, apiKey string, req chatRequest) (*http.Response, error) {
	var text, reasoning strings.Builder
	type call struct {
		id, name, args string
	}
	var calls []call
	finish := "stop"
	var prompt, completion int
	handle := func(event chatEvent) error {
		switch event.kind {
		case "text":
			text.WriteString(event.text)
		case "reasoning":
			reasoning.WriteString(event.text)
		case "tool_start":
			calls = append(calls, call{id: event.id, name: event.name})
		case "tool_args":
			id := event.id
			if id == "" && len(calls) > 0 {
				id = calls[len(calls)-1].id
			}
			found := -1
			for i := range calls {
				if calls[i].id == id {
					found = i
					break
				}
			}
			if found < 0 {
				calls = append(calls, call{id: id})
				found = len(calls) - 1
			}
			calls[found].args += event.text
		case "finish":
			finish = event.reason
		case "usage":
			prompt = event.promptTokens
			completion = event.completionTokens
		}
		return nil
	}
	err := forEachChatEvent(ctx, apiServer, apiKey, req, handle)
	if err != nil && mcpConfigError(err) && len(req.tools) > 0 && text.Len() == 0 && len(calls) == 0 {
		req.tools = nil
		err = forEachChatEvent(ctx, apiServer, apiKey, req, handle)
	}
	if err != nil {
		if contextDone(err) {
			return nil, err
		}
		return errorResponse(http.StatusBadGateway, explainDevin(err)), nil
	}
	if finish == "stop" && len(calls) > 0 {
		finish = "tool_calls"
	}
	message := map[string]any{"role": "assistant", "content": text.String()}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(calls) > 0 {
		toolCalls := make([]map[string]any, 0, len(calls))
		for _, call := range calls {
			toolCalls = append(toolCalls, map[string]any{
				"id":   call.id,
				"type": "function",
				"function": map[string]string{
					"name":      call.name,
					"arguments": call.args,
				},
			})
		}
		message["tool_calls"] = toolCalls
	}
	payload := map[string]any{
		"id":      "chatcmpl-devin",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
	}
	if prompt > 0 || completion > 0 {
		payload["usage"] = map[string]int{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      prompt + completion,
		}
	}
	raw, _ := json.Marshal(payload)
	return jsonResponse(http.StatusOK, raw), nil
}

func streamChat(ctx context.Context, apiServer, apiKey string, req chatRequest) (*http.Response, error) {
	body, err := openChat(ctx, apiServer, apiKey, req)
	if err != nil && mcpConfigError(err) && len(req.tools) > 0 {
		req.tools = nil
		body, err = openChat(ctx, apiServer, apiKey, req)
	}
	if err != nil {
		if contextDone(err) {
			return nil, err
		}
		return completedStream(req.model, explainDevin(err)), nil
	}
	pr, pw := io.Pipe()
	go func() {
		active := body
		defer func() {
			if active != nil {
				_ = active.Close()
			}
		}()
		// Every chunk shares one completion id. A tool call id belongs only
		// inside delta.tool_calls. Putting it in the chunk id makes the agent
		// treat the stream as spliced and generate the whole turn again.
		const completionID = "chatcmpl-devin"
		created := time.Now().Unix()
		writeChoices := func(choices any, extra map[string]any) error {
			payload := make(map[string]any, len(extra)+5)
			for key, value := range extra {
				payload[key] = value
			}
			// Envelope fields are written last so a tool id or usage map cannot
			// replace the completion id.
			payload["id"] = completionID
			payload["object"] = "chat.completion.chunk"
			payload["created"] = created
			payload["model"] = req.model
			payload["choices"] = choices
			raw, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			if _, err := pw.Write([]byte("data: ")); err != nil {
				return err
			}
			if _, err := pw.Write(raw); err != nil {
				return err
			}
			_, err = pw.Write([]byte("\n\n"))
			return err
		}
		var prompt, completion int
		finish := "stop"
		toolIndex := map[string]int{}
		nextIndex := 0
		lastToolID := ""
		emitted := false
		sawTool := false
		toolSlot := func(toolID string) (int, string) {
			if toolID == "" {
				toolID = lastToolID
			}
			if toolID != "" {
				lastToolID = toolID
			}
			idx, ok := toolIndex[toolID]
			if !ok {
				idx = nextIndex
				toolIndex[toolID] = idx
				nextIndex++
			}
			sawTool = true
			return idx, toolID
		}
		handle := func(event chatEvent) error {
			switch event.kind {
			case "text":
				return writeSplitDelta(func(choices any) error { return writeChoices(choices, nil) }, "content", event.text, &emitted)
			case "reasoning":
				return writeSplitDelta(func(choices any) error { return writeChoices(choices, nil) }, "reasoning_content", event.text, &emitted)
			case "tool_start":
				idx, toolID := toolSlot(event.id)
				if err := writeChoices([]map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{
					"index": idx, "id": toolID, "type": "function",
					"function": map[string]string{"name": event.name, "arguments": ""},
				}}}}}, nil); err != nil {
					return err
				}
				emitted = true
				return nil
			case "tool_args":
				idx, _ := toolSlot(event.id)
				for _, part := range splitUTF8(event.text, maxSSEData) {
					if part == "" {
						continue
					}
					if err := writeChoices([]map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{{
						"index": idx, "function": map[string]string{"arguments": part},
					}}}}}, nil); err != nil {
						return err
					}
					emitted = true
				}
				return nil
			case "finish":
				finish = event.reason
				return nil
			case "usage":
				prompt, completion = event.promptTokens, event.completionTokens
				return nil
			default:
				return nil
			}
		}
		err := readChatEvents(active, handle)
		if err != nil && !emitted && mcpConfigError(err) && len(req.tools) > 0 {
			_ = active.Close()
			active = nil
			req.tools = nil
			next, err2 := openChat(ctx, apiServer, apiKey, req)
			if err2 != nil {
				err = err2
			} else {
				active = next
				err = readChatEvents(active, handle)
			}
		}
		if err != nil && contextDone(err) {
			_ = pw.CloseWithError(err)
			return
		}
		if err != nil && !emitted {
			if werr := writeChoices([]map[string]any{{"index": 0, "delta": map[string]string{"content": explainDevin(err)}}}, nil); werr != nil {
				_ = pw.CloseWithError(werr)
				return
			}
		}
		// A tool id that already reached the client is not a finished call when
		// the upstream then fails. tool_calls would execute the empty arguments;
		// length makes the agent recover the cut call instead.
		if err != nil && sawTool && (finish == "stop" || finish == "tool_calls") {
			finish = "length"
		} else if finish == "stop" && sawTool {
			finish = "tool_calls"
		}
		var extra map[string]any
		if prompt > 0 || completion > 0 {
			extra = map[string]any{"usage": map[string]int{
				"prompt_tokens": prompt, "completion_tokens": completion, "total_tokens": prompt + completion,
			}}
		}
		if err := writeChoices([]map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finish}}, extra); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
		_ = pw.Close()
	}()
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        header,
		Body:          pr,
		ContentLength: -1,
	}, nil
}

func completedStream(model, text string) *http.Response {
	var buf bytes.Buffer
	write := func(choices any) {
		raw, _ := json.Marshal(map[string]any{
			"id": "chatcmpl-devin", "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model,
			"choices": choices,
		})
		buf.WriteString("data: ")
		buf.Write(raw)
		buf.WriteString("\n\n")
	}
	write([]map[string]any{{"index": 0, "delta": map[string]string{"content": text}}})
	write([]map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}})
	buf.WriteString("data: [DONE]\n\n")
	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(buf.Bytes())),
		ContentLength: int64(buf.Len()),
	}
}

func jsonResponse(status int, body []byte) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func errorResponse(status int, message string) *http.Response {
	raw, _ := json.Marshal(map[string]any{"error": map[string]string{"message": message, "type": "devin_error"}})
	return jsonResponse(status, raw)
}
