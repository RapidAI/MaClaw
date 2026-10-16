package llm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBuildOpenAIChatRequestDataRoutesOpenCodeFreeModelsToZen(t *testing.T) {
	for _, model := range []string{"glm-5.3-flash-free", "deepseek-v4.1-flash-free"} {
		endpoint, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
			URL:   "https://opencode.ai/zen/go/v1",
			Model: model,
		}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
		if err != nil {
			t.Fatal(err)
		}
		if endpoint != "https://opencode.ai/zen/v1/chat/completions" {
			t.Fatalf("%s endpoint = %s", model, endpoint)
		}
		text := string(body)
		for _, want := range []string{`"model":"` + model + `"`, `"stream":true`, `"name":"bash"`, `"name":"read"`, `"tool_choice":"none"`} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s body missing %s: %s", model, want, text)
			}
		}
		if strings.Contains(text, `"max_tokens"`) {
			t.Fatalf("%s body injected the default output cap: %s", model, text)
		}
	}
}

func TestBuildOpenAIChatRequestDataKeepsOpenCodeFreeOutputCap(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:             "https://opencode.ai/zen/v1",
		Model:           "glm-5.3-flash-free",
		MaxOutputTokens: 1024,
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"max_tokens":1024`) {
		t.Fatalf("body = %s", body)
	}
}

func TestBuildOpenAIChatRequestDataRoutesOpenCodeMeteredModelsToGo(t *testing.T) {
	endpoint, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/v1",
		Model: "glm-5.3-flash",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	if strings.Contains(string(body), `"name":"bash"`) || !strings.Contains(string(body), `"stream":false`) {
		t.Fatalf("metered body = %s", body)
	}
}

func TestBuildOpenAIChatRequestDataKeepsOtherZenFreeModelsOnZen(t *testing.T) {
	endpoint, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/v1",
		Model: "hy3-free",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://opencode.ai/zen/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	text := string(body)
	if strings.Contains(text, `"name":"bash"`) || !strings.Contains(text, `"stream":false`) || !strings.Contains(text, `"model":"hy3-free"`) {
		t.Fatalf("body = %s", text)
	}
}

func TestBuildOpenAIChatRequestDataKeepsGoFreeModelsOnGo(t *testing.T) {
	endpoint, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/v1",
		Model: "cat-jam-free",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	if strings.Contains(string(body), `"name":"bash"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestBuildOpenAIChatRequestDataRewritesRetiredOpenCodeModel(t *testing.T) {
	endpoint, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/v1",
		Model: "big-pickle",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://opencode.ai/zen/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	text := string(body)
	for _, want := range []string{`"model":"glm-5.3-flash-free"`, `"stream":true`, `"name":"bash"`, `"name":"read"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %s: %s", want, text)
		}
	}
}

func TestDoOpenAIRequestCollectsOpenCodeZenFreeStream(t *testing.T) {
	var sawURL string
	client := &http.Client{Transport: openAISDKHeaderRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawURL = req.URL.String()
		if got := req.Header.Get("x-opencode-client"); got != "cli" {
			t.Fatalf("x-opencode-client = %q", got)
		}
		body := "data: {\"choices\":[{\"delta\":{\"content\":\"PING\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
	resp, err := DoOpenAIRequest(context.Background(), corelib.MaclawLLMConfig{
		URL:       "https://opencode.ai/zen/go/v1",
		Model:     "glm-5.3-flash-free",
		Protocol:  "openai",
		AgentType: "opencode",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil, client)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sawURL, "https://opencode.ai/zen/v1/chat/completions") {
		t.Fatalf("url = %s", sawURL)
	}
	if resp == nil || len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "PING" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestBuildOpenAIChatRequestDataOmitsZenAgentReasoningControls(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:             "https://opencode.ai/zen/v1",
		Model:           "glm-5.3-flash-free",
		ThinkingMode:    "enabled",
		ReasoningEffort: "high",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{
		Stream: false,
		Tools:  []map[string]interface{}{{"type": "function", "function": map[string]interface{}{"name": "web_search"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, unwanted := range []string{`"thinking"`, `"budget_tokens"`, `"reasoning_effort"`, `"enable_thinking"`} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("body contains %s: %s", unwanted, text)
		}
	}
	if !strings.Contains(text, `"name":"web_search"`) || !strings.Contains(text, `"name":"bash"`) {
		t.Fatalf("body = %s", text)
	}
	if strings.Contains(text, `"tool_choice":"none"`) {
		t.Fatalf("caller tools were forced off: %s", text)
	}
}

func TestBuildOpenAIChatRequestDataExtendsExistingOpenCodeBashSchema(t *testing.T) {
	original := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "bash",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
	}
	_, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/v1",
		Model: "glm-5.3-flash-free",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{
		Tools: []map[string]interface{}{original},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"description"`) || !strings.Contains(text, `"filePath"`) {
		t.Fatalf("body = %s", text)
	}
	params := original["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	required := params["required"].([]string)
	if len(required) != 1 || required[0] != "command" {
		t.Fatalf("caller schema mutated: %#v", params)
	}
}

func TestBuildOpenAIChatRequestDataKeepsOpenCodeGoReasoningControls(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:          "https://opencode.ai/zen/v1",
		Model:        "glm-5.3-flash",
		ThinkingMode: "enabled",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"thinking"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestBuildOpenAIChatRequestDataKeepsOpenCodeFreeDeepSeekImages(t *testing.T) {
	_, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/v1",
		Model: "deepseek-v4.1-flash-free",
	}, []interface{}{map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "what is this"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,aaaa"}},
		},
	}}, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "image_url") {
		t.Fatalf("body = %s", body)
	}
}
