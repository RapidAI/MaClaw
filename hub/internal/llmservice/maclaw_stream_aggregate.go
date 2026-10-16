package llmservice

import (
	"bytes"
	"encoding/json"
	"strings"
)

// officialStreamBody asks HubCenter to speak SSE for a non-stream official
// call. HubCenter then writes status and heartbeats before the model finishes,
// so a reverse proxy waiting on response headers does not cut the generation.
// A body that is already streaming, or is not a JSON object, is left unchanged.
func officialStreamBody(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return body
	}
	var payload map[string]any
	if err := json.Unmarshal(trimmed, &payload); err != nil || payload == nil {
		return body
	}
	if stream, ok := payload["stream"].(bool); ok && stream {
		return body
	}
	payload["stream"] = true
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func isOfficialEventStream(headerContentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(headerContentType), "text/event-stream") {
		return true
	}
	trim := bytes.TrimSpace(body)
	return bytes.HasPrefix(trim, []byte("data:")) || bytes.HasPrefix(trim, []byte(":"))
}

type aggregatedToolCall struct {
	id   string
	typ  string
	name string
	args string
}

// aggregateOfficialEventStream folds an OpenAI-style SSE body into one chat
// completion JSON document. Comment heartbeats are ignored. An error event
// with no assistant output becomes that error document and a non-200 status.
func aggregateOfficialEventStream(raw []byte) ([]byte, int, error) {
	var content strings.Builder
	var reasoning strings.Builder
	tools := map[int]*aggregatedToolCall{}
	var toolOrder []int
	finish := ""
	var usage json.RawMessage
	modelName := ""
	completionID := ""
	var errorBody []byte
	errorStatus := 0

	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, ":") {
			continue
		}
		if !strings.HasPrefix(trim, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(trim, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if errObj, ok := chunk["error"]; ok && errObj != nil {
			encoded, err := json.Marshal(map[string]any{"error": errObj})
			if err == nil {
				errorBody = encoded
			}
			errorStatus = httpStatusFromStreamError(errObj)
			continue
		}
		if value, ok := chunk["model"].(string); ok && strings.TrimSpace(value) != "" {
			modelName = value
		}
		if value, ok := chunk["id"].(string); ok && strings.TrimSpace(value) != "" {
			completionID = value
		}
		if usageObj, ok := chunk["usage"]; ok && usageObj != nil {
			if encoded, err := json.Marshal(usageObj); err == nil {
				usage = encoded
			}
		}
		choices, _ := chunk["choices"].([]any)
		for _, choice := range choices {
			choiceMap, ok := choice.(map[string]any)
			if !ok {
				continue
			}
			if reason, ok := choiceMap["finish_reason"].(string); ok && reason != "" {
				finish = reason
			}
			delta, _ := choiceMap["delta"].(map[string]any)
			if delta == nil {
				if message, ok := choiceMap["message"].(map[string]any); ok {
					if text, ok := message["content"].(string); ok {
						content.WriteString(text)
					}
					if text, ok := message["reasoning_content"].(string); ok {
						reasoning.WriteString(text)
					}
				}
				continue
			}
			if text, ok := delta["content"].(string); ok {
				content.WriteString(text)
			}
			if text, ok := delta["reasoning_content"].(string); ok {
				reasoning.WriteString(text)
			}
			mergeStreamToolCalls(tools, &toolOrder, delta["tool_calls"])
		}
	}

	if len(errorBody) > 0 && content.Len() == 0 && reasoning.Len() == 0 && len(tools) == 0 {
		if errorStatus == 0 {
			errorStatus = 502
		}
		return errorBody, errorStatus, nil
	}
	if finish == "" {
		finish = "stop"
	}
	message := map[string]any{
		"role":    "assistant",
		"content": content.String(),
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolOrder) > 0 {
		calls := make([]any, 0, len(toolOrder))
		for _, index := range toolOrder {
			call := tools[index]
			typ := call.typ
			if typ == "" {
				typ = "function"
			}
			item := map[string]any{
				"type": typ,
				"function": map[string]any{
					"name":      call.name,
					"arguments": call.args,
				},
			}
			if call.id != "" {
				item["id"] = call.id
			}
			calls = append(calls, item)
		}
		message["tool_calls"] = calls
	}
	out := map[string]any{
		"object": "chat.completion",
		"choices": []any{
			map[string]any{
				"index":         0,
				"message":       message,
				"finish_reason": finish,
			},
		},
	}
	if completionID != "" {
		out["id"] = completionID
	}
	if modelName != "" {
		out["model"] = modelName
	}
	if len(usage) > 0 {
		out["usage"] = json.RawMessage(usage)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, 0, err
	}
	return encoded, 200, nil
}

func httpStatusFromStreamError(errObj any) int {
	payload, ok := errObj.(map[string]any)
	if !ok {
		return 0
	}
	switch code := payload["code"].(type) {
	case float64:
		if code >= 400 && code < 600 {
			return int(code)
		}
	case int:
		if code >= 400 && code < 600 {
			return code
		}
	}
	return 0
}

func mergeStreamToolCalls(tools map[int]*aggregatedToolCall, order *[]int, raw any) {
	items, ok := raw.([]any)
	if !ok {
		return
	}
	for _, item := range items {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		index := 0
		switch value := call["index"].(type) {
		case float64:
			index = int(value)
		case int:
			index = value
		}
		acc := tools[index]
		if acc == nil {
			acc = &aggregatedToolCall{}
			tools[index] = acc
			*order = append(*order, index)
		}
		if id, ok := call["id"].(string); ok && id != "" {
			acc.id = id
		}
		if typ, ok := call["type"].(string); ok && typ != "" {
			acc.typ = typ
		}
		fn, _ := call["function"].(map[string]any)
		if fn == nil {
			continue
		}
		if name, ok := fn["name"].(string); ok && name != "" {
			acc.name = name
		}
		if args, ok := fn["arguments"].(string); ok {
			acc.args += args
		}
	}
}
