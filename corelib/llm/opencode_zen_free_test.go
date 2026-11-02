package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBuildOpenAIChatRequestDataRoutesOpenCodeFreeModelsToZen(t *testing.T) {
	for _, model := range []string{"glm-5.3-flash-free", "deepseek-v4.1-flash-free", "space-bunny-free", "nemotron-3.5-lightning-free", "muse-spark-1.2-contributor-free"} {
		endpoint, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
			URL:   "https://opencode.ai/zen/go/v1",
			Model: model,
		}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
		if err != nil {
			t.Fatal(err)
		}
		if endpoint != "https://opencode.ai/inference/openai/v1/chat/completions" {
			t.Fatalf("%s endpoint = %s", model, endpoint)
		}
		text := string(body)
		for _, want := range []string{`"model":"` + model + `"`, `"stream":true`, `"name":"shell"`, `"name":"read"`, `"name":"glob"`, `"name":"grep"`, `"name":"edit"`, `"name":"write"`, `"tool_choice":"none"`} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s body missing %s: %s", model, want, text)
			}
		}
		if strings.Contains(text, `"max_tokens"`) {
			t.Fatalf("%s body injected the default output cap: %s", model, text)
		}
	}
}

func TestBuildOpenAIChatRequestDataKeepsKeyedFreeModelID(t *testing.T) {
	endpoint, body, err := BuildOpenAIChatRequestData(corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/go/v1",
		Key:   "oc_sk_test",
		Model: "deepseek-v4.1-flash-free",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://opencode.ai/inference/openai/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	text := string(body)
	if !strings.Contains(text, `"model":"deepseek-v4.1-flash-free"`) {
		t.Fatalf("body = %s", text)
	}
	metered := strings.ReplaceAll(text, `"model":"deepseek-v4.1-flash-free"`, "")
	if strings.Contains(metered, "deepseek-v4.1-flash") {
		t.Fatalf("free id was rewritten to the metered model: %s", text)
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
	if endpoint != "https://opencode.ai/inference/go/openai/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	if strings.Contains(string(body), `"name":"shell"`) || !strings.Contains(string(body), `"stream":false`) {
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
	if endpoint != "https://opencode.ai/inference/openai/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	text := string(body)
	for _, want := range []string{`"model":"hy3-free"`, `"stream":true`, `"name":"shell"`, `"name":"read"`, `"name":"glob"`, `"name":"grep"`, `"name":"edit"`, `"name":"write"`, `"tool_choice":"none"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %s: %s", want, text)
		}
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
	if endpoint != "https://opencode.ai/inference/go/openai/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	if strings.Contains(string(body), `"name":"shell"`) {
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
	if endpoint != "https://opencode.ai/inference/openai/v1/chat/completions" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	text := string(body)
	for _, want := range []string{`"model":"glm-5.3-flash-free"`, `"stream":true`, `"name":"shell"`, `"name":"read"`, `"name":"glob"`, `"name":"grep"`, `"name":"edit"`, `"name":"write"`} {
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
	if !strings.Contains(sawURL, "https://opencode.ai/inference/openai/v1/chat/completions") {
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
	if !strings.Contains(text, `"name":"web_search"`) || !strings.Contains(text, `"name":"shell"`) {
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

func TestBuildResponsesAPIRequestDataRoutesMuseToResponses(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{
		URL:       "https://opencode.ai/zen/go/v1",
		Model:     "muse-spark-1.2-contributor-free",
		AgentType: "OpenCode",
	}
	req, body, endpoint, err := NewResponsesAPIRequest(context.Background(), cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "hi"},
	}, ResponsesAPIRequestOptions{Stream: false})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://opencode.ai/inference/openai/v1/responses" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	text := string(body)
	for _, want := range []string{`"model":"muse-spark-1.2-contributor-free"`, `"stream":true`, `"name":"shell"`, `"name":"read"`, `"name":"glob"`, `"name":"grep"`, `"name":"edit"`, `"name":"write"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("body missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, `"tool_choice"`) {
		t.Fatalf("responses body set a tool_choice Zen rejects: %s", text)
	}
	if strings.Contains(text, "max_output_tokens") || strings.Contains(text, "reasoning") {
		t.Fatalf("body = %s", text)
	}
	if req.Header.Get("originator") != "" {
		t.Fatalf("originator = %s", req.Header.Get("originator"))
	}
	if req.Header.Get("x-opencode-client") != "cli" {
		t.Fatalf("x-opencode-client = %s", req.Header.Get("x-opencode-client"))
	}
	session := req.Header.Get(corelib.OpenCodeSessionHeader)
	if session == "" {
		t.Fatal("missing opencode session")
	}
	if got := req.Header.Get(corelib.OpenCodeRequestHeader); got != corelib.OpenCodeClientRequestID(session, "request") {
		t.Fatalf("x-opencode-request = %s", got)
	}
	if req.Header.Get("x-opencode-project") != "global" {
		t.Fatalf("x-opencode-project = %s", req.Header.Get("x-opencode-project"))
	}
}

func TestOpenCodeFreeModelLive(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("OPENCODE_LIVE=1 is not set")
	}
	key := strings.TrimSpace(os.Getenv("OPENCODE_KEY"))
	if key == "" {
		t.Skip("OPENCODE_KEY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	resp, err := DoOpenAIRequest(ctx, corelib.MaclawLLMConfig{
		URL:          "https://opencode.ai/zen/go/v1",
		Key:          key,
		Model:        "deepseek-v4.1-flash-free",
		Protocol:     "openai",
		AgentType:    "opencode",
		ThinkingMode: "disabled",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "Reply with exactly PING"}}, nil, nil)
	if err != nil {
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) && httpErr != nil {
			t.Fatalf("status=%d body=%s", httpErr.StatusCode, httpErr.Body)
		}
		t.Fatal(err)
	}
	if resp == nil || len(resp.Choices) == 0 || !strings.Contains(resp.Choices[0].Message.Content, "PING") {
		t.Fatalf("response = %#v", resp)
	}
}

func TestOpenCodeZenCatalogFreeModelsLive(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("OPENCODE_LIVE=1 is not set")
	}
	key := strings.TrimSpace(os.Getenv("OPENCODE_KEY"))
	if key == "" {
		t.Skip("OPENCODE_KEY is not set")
	}
	for _, model := range []string{"space-bunny-free", "nemotron-3.5-lightning-free"} {
		t.Run(model, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			resp, err := DoOpenAIRequest(ctx, corelib.MaclawLLMConfig{
				URL:       "https://opencode.ai/zen/v1",
				Key:       key,
				Model:     model,
				Protocol:  "openai",
				AgentType: "opencode",
			}, []interface{}{map[string]interface{}{"role": "user", "content": "Reply with exactly PING"}}, nil, nil)
			if err != nil {
				var httpErr *HTTPStatusError
				if errors.As(err, &httpErr) && httpErr != nil {
					t.Fatalf("status=%d body=%s", httpErr.StatusCode, httpErr.Body)
				}
				t.Fatal(err)
			}
			if resp == nil || len(resp.Choices) == 0 || !strings.Contains(resp.Choices[0].Message.Content, "PING") {
				t.Fatalf("response = %#v", resp)
			}
		})
	}
}

func TestOpenCodeFreeIdentifiedClientLive(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("OPENCODE_LIVE=1 is not set")
	}
	key := strings.TrimSpace(os.Getenv("OPENCODE_KEY"))
	if key == "" {
		t.Skip("OPENCODE_KEY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, root, git := openCodeGitProject(wd)
	if root == "" {
		t.Fatal("OpenCode project id was not found for this checkout")
	}
	body := []byte(`{"model":"deepseek-v4.1-flash-free","messages":[{"role":"user","content":"Reply with exactly PING"}]}`)
	callCtx := OpenCodeFreeWithWorkspace(OpenCodeFreeWithKey(ctx, key), root, git)
	resp, err := CompleteOpenCodeFreeClient(callCtx, body)
	if err != nil {
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) && httpErr != nil {
			t.Fatalf("status=%d retry_after=%s body=%s", httpErr.StatusCode, httpErr.RetryAfter, httpErr.Body)
		}
		t.Fatal(err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("response = %#v", resp)
	}
	text := resp.Choices[0].Message.Content
	if text == "" {
		text = resp.Choices[0].Message.ReasoningContent
	}
	if strings.TrimSpace(text) == "" {
		t.Fatalf("response = %#v", resp)
	}
	if len(text) > 300 {
		text = text[:300]
	}
	t.Logf("project=%s text=%s", openCodeFreeProjectID(callCtx), text)
}

func TestOpenCodeFreeExtraToolKeepsOfficialTools(t *testing.T) {
	body, err := openCodeFreeClientBody([]byte(`{"model":"deepseek-v4.1-flash-free","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var before struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(body, &before); err != nil {
		t.Fatal(err)
	}
	payload, err := openCodeFreeWithExtraTool(body, []byte(openCodeFreeCustomTool))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Model string `json:"model"`
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Model != "deepseek-v4.1-flash-free" {
		t.Fatalf("model = %s", decoded.Model)
	}
	if len(decoded.Tools) != len(before.Tools)+1 {
		t.Fatalf("tools = %d, official = %d", len(decoded.Tools), len(before.Tools))
	}
	if decoded.Tools[len(decoded.Tools)-1].Function.Name != "maclaw_note" {
		t.Fatalf("last tool = %s", decoded.Tools[len(decoded.Tools)-1].Function.Name)
	}
	var sawShell bool
	for _, tool := range decoded.Tools[:len(decoded.Tools)-1] {
		if tool.Function.Name == "shell" {
			sawShell = true
		}
		if tool.Function.Name == "maclaw_note" {
			t.Fatal("custom tool replaced an official tool")
		}
	}
	if !sawShell {
		t.Fatal("official shell tool is missing")
	}
	if !strings.Contains(string(payload), "<env>") {
		t.Fatal("system prompt was re-escaped")
	}
	if _, err := openCodeFreeWithExtraTool(body, []byte(`null`)); err == nil {
		t.Fatal("non-object tool was accepted")
	}
	if _, err := openCodeFreeWithExtraTool(body, []byte(`{}`)); err == nil {
		t.Fatal("empty tool was accepted")
	}
	if _, err := openCodeFreeWithExtraTool(body, []byte(`{"type":"function","function":{"name":"shell"}}`)); err == nil {
		t.Fatal("official tool name was appended again")
	}
}

func TestOpenCodeFreeCustomToolLive(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("OPENCODE_LIVE=1 is not set")
	}
	key := strings.TrimSpace(os.Getenv("OPENCODE_KEY"))
	if key == "" {
		t.Skip("OPENCODE_KEY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, root, git := openCodeGitProject(wd)
	if root == "" {
		t.Fatal("OpenCode project id was not found for this checkout")
	}
	body, err := openCodeFreeClientBody([]byte(`{"model":"deepseek-v4.1-flash-free","messages":[{"role":"user","content":"Reply with exactly PING. Do not call any tool."}]}`))
	if err != nil {
		t.Fatal(err)
	}
	callCtx := OpenCodeFreeWithWorkspace(OpenCodeFreeWithKey(ctx, key), root, git)
	body = openCodeFreeApplyWorkspace(body, openCodeFreeWorkspace(callCtx))
	payload, err := openCodeFreeWithExtraTool(body, []byte(openCodeFreeCustomTool))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := openCodeFreePost(callCtx, payload)
	if err != nil {
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) && httpErr != nil {
			t.Fatalf("status=%d retry_after=%s body=%s", httpErr.StatusCode, httpErr.RetryAfter, httpErr.Body)
		}
		t.Fatal(err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		t.Fatalf("response = %#v", resp)
	}
	text := resp.Choices[0].Message.Content
	if text == "" {
		text = resp.Choices[0].Message.ReasoningContent
	}
	var toolName string
	if len(resp.Choices[0].Message.ToolCalls) > 0 {
		toolName = resp.Choices[0].Message.ToolCalls[0].Function.Name
	}
	if strings.TrimSpace(text) == "" && toolName == "" {
		t.Fatalf("response = %#v", resp)
	}
	if len(text) > 300 {
		text = text[:300]
	}
	t.Logf("custom_tool=maclaw_note tool_call=%s text=%s", toolName, text)
}

const openCodeFreeCustomTool = `{"type":"function","function":{"name":"maclaw_note","description":"Record a short note for the user.","parameters":{"type":"object","properties":{"text":{"type":"string","description":"Note text"}},"required":["text"],"additionalProperties":false},"strict":false}}`

func openCodeFreeWithExtraTool(payload, tool []byte) ([]byte, error) {
	var body struct {
		Model               string          `json:"model"`
		Messages            json.RawMessage `json:"messages"`
		Tools               json.RawMessage `json:"tools"`
		Stream              bool            `json:"stream"`
		StreamOptions       json.RawMessage `json:"stream_options"`
		MaxCompletionTokens int             `json:"max_completion_tokens"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}
	tool = bytes.TrimSpace(tool)
	name, ok := openCodeFreeFunctionToolName(tool)
	if !ok {
		return nil, errors.New("custom tool is not a function")
	}
	tools := bytes.TrimSpace(body.Tools)
	if len(tools) < 2 || tools[0] != '[' || tools[len(tools)-1] != ']' {
		return nil, errors.New("opencode tools array missing")
	}
	if openCodeFreeToolsInclude(tools, name) {
		return nil, errors.New("custom tool reuses an official name")
	}
	inner := bytes.TrimSpace(tools[1 : len(tools)-1])
	merged := make([]byte, 0, len(tools)+len(tool)+1)
	merged = append(merged, '[')
	if len(inner) > 0 {
		merged = append(merged, inner...)
		merged = append(merged, ',')
	}
	merged = append(merged, tool...)
	merged = append(merged, ']')
	body.Tools = merged
	var out strings.Builder
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(out.String(), "\n")), nil
}

func openCodeFreeFunctionToolName(tool []byte) (string, bool) {
	if len(tool) == 0 || tool[0] != '{' || !json.Valid(tool) {
		return "", false
	}
	var item struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(tool, &item) != nil || item.Type != "function" {
		return "", false
	}
	name := strings.TrimSpace(item.Function.Name)
	if name == "" {
		return "", false
	}
	return name, true
}

func openCodeFreeToolsInclude(tools []byte, name string) bool {
	var items []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(tools, &items) != nil {
		return false
	}
	for _, item := range items {
		if item.Function.Name == name {
			return true
		}
	}
	return false
}

func TestOpenCodeMuseResponsesLive(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE") != "1" {
		t.Skip("OPENCODE_LIVE=1 is not set")
	}
	key := strings.TrimSpace(os.Getenv("OPENCODE_KEY"))
	if key == "" {
		t.Skip("OPENCODE_KEY is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := DoResponsesAPIRequestStream(ctx, corelib.MaclawLLMConfig{
		URL:             "https://opencode.ai/zen/v1",
		Key:             key,
		Model:           "muse-spark-1.2-contributor-free",
		Protocol:        "openai",
		AgentType:       "opencode",
		WireAPI:         "responses",
		MaxOutputTokens: corelib.LLMProbeMaxOutputTokens,
		ThinkingMode:    "disabled",
	}, []interface{}{map[string]interface{}{"role": "user", "content": "Reply with exactly PING"}}, nil, nil, nil, nil)
	if err != nil {
		var httpErr *HTTPStatusError
		if errors.As(err, &httpErr) && httpErr != nil {
			t.Fatalf("status=%d body=%s", httpErr.StatusCode, httpErr.Body)
		}
		t.Fatal(err)
	}
	if resp == nil || len(resp.Choices) == 0 || !strings.Contains(resp.Choices[0].Message.Content, "PING") {
		t.Fatalf("response = %#v", resp)
	}
}
