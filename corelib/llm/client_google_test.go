package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestGoogleGenerateContentEndpointNormalizesBaseURL(t *testing.T) {
	cases := []struct {
		base   string
		model  string
		stream bool
		want   string
	}{
		{"https://generativelanguage.googleapis.com/v1beta", "gemini-3.8-flash", false, "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent"},
		{"https://generativelanguage.googleapis.com/v1beta/", "gemini-3.8-flash", false, "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent"},
		{"https://generativelanguage.googleapis.com/v1beta/openai", "gemini-3.8-flash", false, "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent"},
		{"https://generativelanguage.googleapis.com/v1beta/openai/chat/completions", "gemini-3.8-flash", false, "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent"},
		{"https://generativelanguage.googleapis.com/v1beta/models/other:generateContent", "gemini-3.8-flash", false, "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:generateContent"},
		{"generativelanguage.googleapis.com/v1beta", "google/gemini-3.8-flash", true, "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.8-flash:streamGenerateContent?alt=sse"},
		{"https://proxy.example.com", "gemini-3.8-flash", false, "https://proxy.example.com/v1beta/models/gemini-3.8-flash:generateContent"},
		{"https://proxy.example.com/custom/v1", "models/gemini-3.8-flash", false, "https://proxy.example.com/custom/v1/models/gemini-3.8-flash:generateContent"},
	}
	for _, tc := range cases {
		got, err := GoogleGenerateContentEndpoint(tc.base, tc.model, tc.stream)
		if err != nil {
			t.Fatalf("%s: %v", tc.base, err)
		}
		if got != tc.want {
			t.Fatalf("%s => %s, want %s", tc.base, got, tc.want)
		}
	}
}

func TestIsGoogleProtocol(t *testing.T) {
	if !(corelib.MaclawLLMConfig{Protocol: "google"}).IsGoogleProtocol() {
		t.Fatal("google")
	}
	if !(corelib.MaclawLLMConfig{Protocol: " Gemini "}).IsGoogleProtocol() {
		t.Fatal("gemini")
	}
	if (corelib.MaclawLLMConfig{Protocol: "openai", URL: "https://generativelanguage.googleapis.com/v1beta"}).IsGoogleProtocol() {
		t.Fatal("openai protocol is not the native wire")
	}
}

func TestBuildGoogleGenerateContentRequestBody(t *testing.T) {
	temp := 0.2
	cfg := corelib.MaclawLLMConfig{
		URL:             "https://generativelanguage.googleapis.com/v1beta",
		Model:           "gemini-3.8-flash",
		Protocol:        "google",
		MaxOutputTokens: 128,
		Temperature:     &temp,
	}
	body, err := BuildGoogleGenerateContentRequestBody(cfg, []interface{}{
		map[string]interface{}{"role": "system", "content": "be brief"},
		map[string]interface{}{"role": "user", "content": []interface{}{
			map[string]interface{}{"type": "text", "text": "see"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,aaaa"}},
		}},
		map[string]interface{}{
			"role":              "assistant",
			"content":           "calling",
			"thought_signature": "textsig",
			"tool_calls": []interface{}{map[string]interface{}{
				"id":                "call_1",
				"type":              "function",
				"thought_signature": "callsig",
				"function":          map[string]interface{}{"name": "lookup", "arguments": `{"q":"x"}`},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "name": "lookup", "content": "done"},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": `["more"]`},
	}, GoogleGenerateRequestOptions{
		Tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "lookup",
				"description": "Look up",
				"parameters":  map[string]interface{}{"type": "object"},
			},
		}},
		ToolChoice:              map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "lookup"}},
		ExplicitToolReplacement: true,
		ResponseFormat:          map[string]interface{}{"type": "json_object"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := body["messages"]; exists {
		t.Fatalf("native body has messages: %#v", body)
	}
	if body["systemInstruction"].(map[string]interface{})["parts"].([]interface{})[0].(map[string]interface{})["text"] != "be brief" {
		t.Fatalf("system = %#v", body["systemInstruction"])
	}
	contents := body["contents"].([]interface{})
	user := contents[0].(map[string]interface{})
	parts := user["parts"].([]interface{})
	inline := parts[1].(map[string]interface{})["inlineData"].(map[string]interface{})
	if inline["mimeType"] != "image/png" || inline["data"] != "aaaa" {
		t.Fatalf("image = %#v", inline)
	}
	model := contents[1].(map[string]interface{})
	modelParts := model["parts"].([]interface{})
	if modelParts[0].(map[string]interface{})["thoughtSignature"] != "textsig" {
		t.Fatalf("text signature = %#v", modelParts[0])
	}
	call := modelParts[1].(map[string]interface{})
	if call["thoughtSignature"] != "callsig" {
		t.Fatalf("call signature = %#v", call)
	}
	fn := call["functionCall"].(map[string]interface{})
	if fn["name"] != "lookup" || fn["args"].(map[string]interface{})["q"] != "x" {
		t.Fatalf("functionCall = %#v", fn)
	}
	if _, exists := fn["id"]; exists {
		t.Fatalf("invented functionCall id = %#v", fn["id"])
	}
	toolTurn := contents[2].(map[string]interface{})
	if toolTurn["role"] != "user" {
		t.Fatalf("tool role = %#v", toolTurn["role"])
	}
	responses := toolTurn["parts"].([]interface{})
	if len(responses) != 2 {
		t.Fatalf("tool parts = %#v", responses)
	}
	first := responses[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
	if first["name"] != "lookup" || first["response"].(map[string]interface{})["result"] != "done" {
		t.Fatalf("first response = %#v", first)
	}
	if _, exists := first["id"]; exists {
		t.Fatalf("invented functionResponse id = %#v", first["id"])
	}
	second := responses[1].(map[string]interface{})["functionResponse"].(map[string]interface{})
	if _, ok := second["response"].(map[string]interface{})["result"].([]interface{}); !ok {
		t.Fatalf("array result = %#v", second["response"])
	}
	gen := body["generationConfig"].(map[string]interface{})
	if gen["maxOutputTokens"] != 128 || gen["temperature"] != 0.2 || gen["responseMimeType"] != "application/json" {
		t.Fatalf("generationConfig = %#v", gen)
	}
	if gen["responseSchema"] != nil || gen["responseJsonSchema"] != nil {
		t.Fatalf("json_object attached a schema: %#v", gen)
	}
	calling := body["toolConfig"].(map[string]interface{})["functionCallingConfig"].(map[string]interface{})
	if calling["mode"] != "ANY" {
		t.Fatalf("toolConfig = %#v", calling)
	}
	if _, exists := body["parallel_tool_calls"]; exists {
		t.Fatal("parallel_tool_calls leaked onto the google body")
	}
}

func TestGoogleMaxOutputTokensStaysWithinRESTHop(t *testing.T) {
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	tokens := func(cfg corelib.MaclawLLMConfig, opts GoogleGenerateRequestOptions) interface{} {
		t.Helper()
		body, err := BuildGoogleGenerateContentRequestBody(cfg, messages, opts)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := body["continuationToken"]; exists {
			t.Fatal("continuationToken was sent")
		}
		return body["generationConfig"].(map[string]interface{})["maxOutputTokens"]
	}
	if got := tokens(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash"}, GoogleGenerateRequestOptions{}); got != googleNonStreamHopMaxOutputTokens {
		t.Fatalf("default non-stream = %#v", got)
	}
	if got := tokens(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash", MaxOutputTokens: 65536}, GoogleGenerateRequestOptions{}); got != googleNonStreamHopMaxOutputTokens {
		t.Fatalf("configured 65536 non-stream = %#v", got)
	}
	if got := tokens(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash", MaxOutputTokens: 1000}, GoogleGenerateRequestOptions{}); got != 1000 {
		t.Fatalf("explicit 1000 = %#v", got)
	}
	if got := tokens(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash", MaxOutputTokens: 65536}, GoogleGenerateRequestOptions{MaxOutputTokens: 1000}); got != 1000 {
		t.Fatalf("option 1000 = %#v", got)
	}
	if got := tokens(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash"}, GoogleGenerateRequestOptions{Stream: true}); got != googleStreamHopMaxOutputTokens {
		t.Fatalf("default stream = %#v", got)
	}
	if got := tokens(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash"}, GoogleGenerateRequestOptions{Stream: true, MaxOutputTokens: 90000}); got != googleStreamHopMaxOutputTokens {
		t.Fatalf("stream above hop = %#v", got)
	}
	if got := tokens(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash"}, GoogleGenerateRequestOptions{Stream: true, MaxOutputTokens: 1000}); got != 1000 {
		t.Fatalf("stream under hop = %#v", got)
	}
	raised := corelib.MaclawLLMConfig{
		Model: "gemini-3.8-flash",
		MemberExtraBody: map[string]any{
			"generationConfig": map[string]any{"maxOutputTokens": float64(100000)},
		},
	}
	if got := tokens(raised, GoogleGenerateRequestOptions{}); got != googleNonStreamHopMaxOutputTokens {
		t.Fatalf("member extra above hop = %#v", got)
	}
	kept := corelib.MaclawLLMConfig{
		Model:                   "gemini-3.8-flash",
		MaxOutputTokens:         1000,
		MemberExtraBodyPriority: "downstream",
		MemberExtraBody: map[string]any{
			"generationConfig": map[string]any{"maxOutputTokens": float64(100000)},
		},
	}
	if got := tokens(kept, GoogleGenerateRequestOptions{}); got != 1000 {
		t.Fatalf("downstream member did not keep 1000: %#v", got)
	}
}

func TestGoogleContinuationIsNotAFinishedStop(t *testing.T) {
	resp, err := ParseGoogleGenerateContent([]byte(`{
		"candidates":[{"content":{"parts":[{"text":"partial"}]},"finishReason":"CONTINUATION"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].FinishReason != "continuation" || resp.Choices[0].Message.Content != "partial" {
		t.Fatalf("text hop = %#v", resp.Choices[0])
	}
	withCall, err := ParseGoogleGenerateContent([]byte(`{
		"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","args":{"q":"x"}}}]},"finishReason":"CONTINUATION"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if withCall.Choices[0].FinishReason != "tool_calls" || len(withCall.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("call hop = %#v", withCall.Choices[0])
	}
}

func TestGoogleJSONSchemaUsesResponseJsonSchema(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Model: "gemini-3.8-flash", Protocol: "google"}
	body, err := BuildGoogleGenerateContentRequestBody(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "classify"},
	}, GoogleGenerateRequestOptions{
		Tools: []map[string]interface{}{{
			"type": "function",
			"function": map[string]interface{}{
				"name": "lookup",
				"parameters": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"properties":           map[string]interface{}{"q": map[string]interface{}{"type": "string"}},
				},
			},
		}},
		ResponseFormat: map[string]interface{}{
			"type": "json_schema",
			"json_schema": map[string]interface{}{
				"name": "intent",
				"schema": map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []interface{}{"intent"},
					"properties": map[string]interface{}{
						"intent": map[string]interface{}{
							"anyOf": []interface{}{
								map[string]interface{}{"type": "string", "enum": []interface{}{"coding"}},
							},
						},
						"confidence": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	gen := body["generationConfig"].(map[string]interface{})
	if gen["responseMimeType"] != "application/json" {
		t.Fatalf("mime = %#v", gen["responseMimeType"])
	}
	if _, exists := gen["responseSchema"]; exists {
		t.Fatal("responseSchema rejects additionalProperties")
	}
	got, ok := gen["responseJsonSchema"].(map[string]interface{})
	if !ok || got["additionalProperties"] != false {
		t.Fatalf("responseJsonSchema = %#v", gen["responseJsonSchema"])
	}
	props := got["properties"].(map[string]interface{})
	intent := props["intent"].(map[string]interface{})
	if _, ok := intent["anyOf"].([]interface{}); !ok {
		t.Fatalf("anyOf was stripped: %#v", intent)
	}
	decls := body["tools"].([]interface{})[0].(map[string]interface{})["functionDeclarations"].([]interface{})
	decl := decls[0].(map[string]interface{})
	if _, exists := decl["parametersJsonSchema"]; exists {
		t.Fatal("function declarations moved off parameters")
	}
	params := decl["parameters"].(map[string]interface{})
	if _, exists := params["additionalProperties"]; exists {
		t.Fatalf("tool parameters kept additionalProperties: %#v", params)
	}
}

func TestGoogleEchoesProviderFunctionCallID(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Model: "gemini-3.8-flash", Protocol: "google"}
	body, err := BuildGoogleGenerateContentRequestBody(cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "look"},
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{
				map[string]interface{}{
					"id":                      "call_1",
					"type":                    "function",
					"google_function_call_id": "fc-lookup",
					"thought_signature":       "callsig",
					"function":                map[string]interface{}{"name": "lookup", "arguments": `{"q":"x"}`},
				},
				map[string]interface{}{
					"id":       "call_2",
					"type":     "function",
					"function": map[string]interface{}{"name": "other", "arguments": `{}`},
				},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "done"},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_2", "content": "ok"},
	}, GoogleGenerateRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	contents := body["contents"].([]interface{})
	modelParts := contents[1].(map[string]interface{})["parts"].([]interface{})
	firstCall := modelParts[0].(map[string]interface{})["functionCall"].(map[string]interface{})
	secondCall := modelParts[1].(map[string]interface{})["functionCall"].(map[string]interface{})
	if firstCall["id"] != "fc-lookup" {
		t.Fatalf("functionCall id = %#v", firstCall["id"])
	}
	if _, exists := secondCall["id"]; exists {
		t.Fatalf("second functionCall id = %#v", secondCall["id"])
	}
	responses := contents[2].(map[string]interface{})["parts"].([]interface{})
	first := responses[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
	second := responses[1].(map[string]interface{})["functionResponse"].(map[string]interface{})
	if first["id"] != "fc-lookup" || first["name"] != "lookup" {
		t.Fatalf("first functionResponse = %#v", first)
	}
	if _, exists := second["id"]; exists || second["name"] != "other" {
		t.Fatalf("second functionResponse = %#v", second)
	}
}

func TestGoogleFunctionCallIDsStayWithTheirTurn(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Model: "gemini-3.8-flash", Protocol: "google"}
	firstCalls := []ToolCall{{
		ID:                   "call_1",
		Type:                 "function",
		ThoughtSignature:     "sig-1",
		GoogleFunctionCallID: "fc-1",
		Function:             ToolCallFunction{Name: "lookup", Arguments: `{"q":"1"}`},
	}}
	secondCall := map[string]interface{}{
		"id":                      "call_1",
		"type":                    "function",
		"google_function_call_id": "fc-2",
		"thought_signature":       "sig-2",
		"function":                map[string]interface{}{"name": "other", "arguments": `{"q":"2"}`},
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": "one"},
		map[string]interface{}{"role": "assistant", "tool_calls": firstCalls},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "a"},
		map[string]interface{}{"role": "user", "content": "two"},
		map[string]interface{}{"role": "assistant", "tool_calls": []interface{}{secondCall}},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "b"},
		map[string]interface{}{"role": "user", "content": "three"},
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{map[string]interface{}{
				"id":       "call_1",
				"type":     "function",
				"function": map[string]interface{}{"name": "lookup", "arguments": `{"q":"3"}`},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "c"},
	}
	body, err := BuildGoogleGenerateContentRequestBody(cfg, messages, GoogleGenerateRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := messages[1].(map[string]interface{})["tool_calls"].([]ToolCall); !ok {
		t.Fatal("builder rewrote the caller tool-call slice")
	}
	if _, exists := secondCall["extra_content"]; exists {
		t.Fatal("builder wrote extra_content onto the caller call")
	}
	var callIDs, callNames, callSigs, responseIDs, responseNames []interface{}
	for _, raw := range body["contents"].([]interface{}) {
		content := raw.(map[string]interface{})
		for _, rawPart := range content["parts"].([]interface{}) {
			part := rawPart.(map[string]interface{})
			if _, exists := part["extra_content"]; exists {
				t.Fatalf("extra_content leaked onto a part: %#v", part)
			}
			if call, ok := part["functionCall"].(map[string]interface{}); ok {
				callIDs = append(callIDs, call["id"])
				callNames = append(callNames, call["name"])
				callSigs = append(callSigs, part["thoughtSignature"])
			}
			if resp, ok := part["functionResponse"].(map[string]interface{}); ok {
				responseIDs = append(responseIDs, resp["id"])
				responseNames = append(responseNames, resp["name"])
			}
		}
	}
	if len(callIDs) != 3 || callIDs[0] != "fc-1" || callIDs[1] != "fc-2" || callIDs[2] != nil {
		t.Fatalf("functionCall ids = %#v", callIDs)
	}
	if callNames[0] != "lookup" || callNames[1] != "other" || callNames[2] != "lookup" {
		t.Fatalf("functionCall names = %#v", callNames)
	}
	if callSigs[0] != "sig-1" || callSigs[1] != "sig-2" || callSigs[2] != nil {
		t.Fatalf("signatures = %#v", callSigs)
	}
	if len(responseIDs) != 3 || responseIDs[0] != "fc-1" || responseIDs[1] != "fc-2" || responseIDs[2] != nil {
		t.Fatalf("functionResponse ids = %#v", responseIDs)
	}
	if responseNames[0] != "lookup" || responseNames[1] != "other" || responseNames[2] != "lookup" {
		t.Fatalf("functionResponse names = %#v", responseNames)
	}
}

func TestGoogleThinkingConfigFollowsModelWire(t *testing.T) {
	user := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	thinking := func(cfg corelib.MaclawLLMConfig) map[string]interface{} {
		t.Helper()
		body, err := BuildGoogleGenerateContentRequestBody(cfg, user, GoogleGenerateRequestOptions{})
		if err != nil {
			t.Fatal(err)
		}
		gen := body["generationConfig"].(map[string]interface{})
		if _, exists := body["reasoning_effort"]; exists || gen["thinking"] != nil {
			t.Fatalf("native body used an OpenAI thinking field: %#v", body)
		}
		cfgThinking, _ := gen["thinkingConfig"].(map[string]interface{})
		return cfgThinking
	}
	auto := thinking(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash"})
	if auto["includeThoughts"] != true || auto["thinkingLevel"] != nil || auto["thinkingBudget"] != nil {
		t.Fatalf("auto = %#v", auto)
	}
	disabled3 := thinking(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash", ThinkingMode: "disabled"})
	if disabled3["thinkingLevel"] != "low" || disabled3["includeThoughts"] != true {
		t.Fatalf("gemini 3 disabled = %#v", disabled3)
	}
	flash := thinking(corelib.MaclawLLMConfig{Model: "gemini-2.5-flash", ThinkingMode: "disabled"})
	if flash["thinkingBudget"] != 0 || flash["includeThoughts"] != nil {
		t.Fatalf("2.5 flash disabled = %#v", flash)
	}
	pro := thinking(corelib.MaclawLLMConfig{Model: "gemini-2.5-pro", ThinkingMode: "disabled"})
	if pro["thinkingBudget"] != 128 || pro["includeThoughts"] != true {
		t.Fatalf("2.5 pro disabled = %#v", pro)
	}
	old := thinking(corelib.MaclawLLMConfig{Model: "gemini-2.0-flash", ThinkingMode: "disabled"})
	if old != nil {
		t.Fatalf("gemini 2.0 thinkingConfig = %#v", old)
	}
	high := thinking(corelib.MaclawLLMConfig{Model: "gemini-4-argon", ThinkingMode: "enabled", ReasoningEffort: "max"})
	if high["thinkingLevel"] != "high" {
		t.Fatalf("max effort = %#v", high)
	}
}

func TestGoogleEmptyToolsOmission(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{Model: "gemini-2.0-flash"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	omitted, err := BuildGoogleGenerateContentRequestBody(cfg, messages, GoogleGenerateRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := omitted["tools"]; exists {
		t.Fatalf("empty tools were sent: %#v", omitted["tools"])
	}
	explicit, err := BuildGoogleGenerateContentRequestBody(cfg, messages, GoogleGenerateRequestOptions{ExplicitToolReplacement: true})
	if err != nil {
		t.Fatal(err)
	}
	tools, ok := explicit["tools"].([]interface{})
	if !ok || len(tools) != 0 {
		t.Fatalf("explicit empty tools = %#v", explicit["tools"])
	}
}

func TestParseGoogleGenerateContentKeepsSignatureAndUsage(t *testing.T) {
	resp, err := ParseGoogleGenerateContent([]byte(`{
		"responseId":"resp-1",
		"candidates":[{"content":{"parts":[
			{"text":"pong","thoughtSignature":"textsig"},
			{"functionCall":{"id":"fc-lookup","name":"lookup","args":{"q":"x"}},"thoughtSignature":"callsig"}
		]},"finishReason":"MAX_TOKENS"}],
		"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"thoughtsTokenCount":4,"totalTokenCount":9,"cachedContentTokenCount":1}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	msg := resp.Choices[0].Message
	if resp.ResponseID != "resp-1" || msg.Content != "pong" || msg.ThoughtSignature != "textsig" {
		t.Fatalf("message = %#v id=%s", msg, resp.ResponseID)
	}
	if resp.Choices[0].FinishReason != "length" {
		t.Fatalf("finish = %s", resp.Choices[0].FinishReason)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_1" || msg.ToolCalls[0].GoogleFunctionCallID != "fc-lookup" || msg.ToolCalls[0].ThoughtSignature != "callsig" || msg.ToolCalls[0].Function.Name != "lookup" {
		t.Fatalf("calls = %#v", msg.ToolCalls)
	}
	if resp.Usage.PromptTokens != 3 || resp.Usage.CompletionTokens != 6 || resp.Usage.TotalTokens != 9 || resp.Usage.CachedInputTokens != 1 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestGoogleThoughtPartSignatureStaysOffOtherParts(t *testing.T) {
	resp, err := ParseGoogleGenerateContent([]byte(`{
		"candidates":[{"content":{"parts":[
			{"text":"why","thought":true,"thoughtSignature":"thought-sig"},
			{"text":"answer","thoughtSignature":"text-sig"},
			{"functionCall":{"name":"lookup","args":{"q":"paris"}},"thoughtSignature":"call-sig"},
			{"functionCall":{"name":"lookup","args":{"q":"london"}}}
		]},"finishReason":"STOP"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	msg := resp.Choices[0].Message
	if msg.Content != "answer" || msg.ReasoningContent != "why" || msg.ThoughtSignature != "text-sig" {
		t.Fatalf("message = %#v", msg)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("calls = %#v", msg.ToolCalls)
	}
	if msg.ToolCalls[0].ThoughtSignature != "call-sig" || msg.ToolCalls[1].ThoughtSignature != "" {
		t.Fatalf("parallel signatures = %#v %#v", msg.ToolCalls[0].ThoughtSignature, msg.ToolCalls[1].ThoughtSignature)
	}
	body, err := BuildGoogleGenerateContentRequestBody(corelib.MaclawLLMConfig{Model: "gemini-3.8-flash"}, []interface{}{
		map[string]interface{}{"role": "user", "content": "weather"},
		map[string]interface{}{
			"role":              "assistant",
			"content":           msg.Content,
			"thought_signature": msg.ThoughtSignature,
			"tool_calls":        msg.ToolCalls,
		},
		map[string]interface{}{"role": "tool", "tool_call_id": msg.ToolCalls[0].ID, "name": "lookup", "content": "paris"},
		map[string]interface{}{"role": "tool", "tool_call_id": msg.ToolCalls[1].ID, "name": "lookup", "content": "london"},
	}, GoogleGenerateRequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parts := body["contents"].([]interface{})[1].(map[string]interface{})["parts"].([]interface{})
	if parts[0].(map[string]interface{})["thoughtSignature"] != "text-sig" {
		t.Fatalf("replayed text = %#v", parts[0])
	}
	first := parts[1].(map[string]interface{})
	second := parts[2].(map[string]interface{})
	if first["thoughtSignature"] != "call-sig" {
		t.Fatalf("replayed first call = %#v", first)
	}
	if _, exists := second["thoughtSignature"]; exists {
		t.Fatalf("replayed second call gained a signature: %#v", second)
	}
}

func TestParseGoogleGenerateContentStreamConcatenatesDeltas(t *testing.T) {
	raw := "" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hel\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"lo\"},{\"thought\":true,\"text\":\"why\"},{\"functionCall\":{\"name\":\"lookup\",\"args\":{\"q\":\"x\"}},\"thoughtSignature\":\"sig\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup\",\"args\":{\"q\":\"x\"}},\"thoughtSignature\":\"sig\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"thoughtsTokenCount\":1,\"totalTokenCount\":3}}\n" +
		"data: [DONE]\n"
	var text, reasoning strings.Builder
	resp, err := parseGoogleGenerateContentStream(strings.NewReader(raw), func(delta string) { text.WriteString(delta) }, func(delta string) { reasoning.WriteString(delta) })
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "hello" || reasoning.String() != "why" {
		t.Fatalf("deltas text=%q reasoning=%q", text.String(), reasoning.String())
	}
	if resp.Choices[0].Message.Content != "hello" || resp.Choices[0].FinishReason != "tool_calls" || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("response = %#v", resp.Choices[0])
	}
	if resp.Usage.CompletionTokens != 2 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}

func TestListGoogleModelsFiltersGenerateContent(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		if r.Header.Get("Authorization") != "" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("api key = %q", r.Header.Get("x-goog-api-key"))
		}
		w.Header().Set("Content-Type", "application/json")
		if pages == 1 {
			if r.URL.Query().Get("pageToken") != "" {
				t.Errorf("first page token = %q", r.URL.Query().Get("pageToken"))
			}
			_, _ = io.WriteString(w, `{"models":[
				{"name":"models/gemini-3.8-flash","displayName":"Gemini 3.8 Flash","supportedGenerationMethods":["generateContent"]},
				{"name":"models/text-embedding-004","displayName":"Embed","supportedGenerationMethods":["embedContent"]},
				{"name":"models/gemini-embedding-001","displayName":"Gem embed"},
				{"name":"models/gemini-custom","displayName":"Custom"}
			],"nextPageToken":"next"}`)
			return
		}
		if r.URL.Query().Get("pageToken") != "next" {
			t.Errorf("page token = %q", r.URL.Query().Get("pageToken"))
		}
		_, _ = io.WriteString(w, `{"models":[{"name":"models/gemini-4-argon","displayName":"Argon","supportedGenerationMethods":["generateContent"]}]}`)
	}))
	defer srv.Close()
	models, err := ListGoogleModels(context.Background(), corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Protocol: "google"}, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	if strings.Join(ids, ",") != "gemini-3.8-flash,gemini-custom,gemini-4-argon" {
		t.Fatalf("models = %v", ids)
	}
	if pages != 2 {
		t.Fatalf("pages = %d", pages)
	}
}

func TestDoOpenAIRequestGoogleProtocolRetriesThinkingOnce(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("api key = %q", r.Header.Get("x-goog-api-key"))
		}
		if !strings.Contains(strings.ToLower(r.URL.Path), "generatecontent") || strings.Contains(r.URL.Path, "chat/completions") {
			t.Errorf("path = %s", r.URL.Path)
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode: %v", err)
		}
		if _, exists := payload["messages"]; exists {
			t.Errorf("chat body posted to generateContent")
		}
		http.Error(w, `{"error":{"message":"cannot disable thinking, use low"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	cfg := corelib.MaclawLLMConfig{
		URL:          srv.URL,
		Model:        "gemini-3.8-flash",
		Protocol:     "google",
		Key:          "test-key",
		ThinkingMode: "disabled",
	}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	_, err := DoOpenAIRequestWithOptions(context.Background(), cfg, messages, nil, srv.Client(), OpenAIChatRequestOptions{})
	if err == nil {
		t.Fatal("expected thinking rejection")
	}
	if hits != 2 {
		t.Fatalf("non-stream hits = %d, want one retry", hits)
	}
	hits = 0
	_, err = DoOpenAIRequestStreamWithOptions(context.Background(), cfg, messages, nil, srv.Client(), nil, nil, OpenAIChatRequestOptions{Stream: true, Tools: nil})
	if err == nil {
		t.Fatal("expected stream rejection")
	}
	if hits != 1 {
		t.Fatalf("stream hits = %d, want no internal retry", hits)
	}
	if _, err := BuildGoogleGenerateContentRequestBody(cfg, messages, GoogleGenerateRequestOptions{ParallelToolCalls: boolPtr(false)}); err == nil {
		t.Fatal("parallel_tool_calls was accepted")
	}
}

func boolPtr(v bool) *bool { return &v }
