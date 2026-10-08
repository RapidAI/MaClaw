package trae

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPrepareBodyMapsOpenAIToSolo(t *testing.T) {
	payload := []byte(`{
		"model": "glm-5.2",
		"stream": false,
		"messages": [
			{"role": "system", "content": "You are Claude Code, Anthropic's official CLI for Claude."},
			{"role": "user", "content": "hello"}
		],
		"tools": [{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}]
	}`)
	body, originalStream := prepareBody(payload, CNProfile())
	if originalStream {
		t.Fatal("original stream must stay false")
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["function"] != "solo_work_lite" {
		t.Fatalf("function wrong: %v", obj["function"])
	}
	if obj["stream"] != true {
		t.Fatalf("stream must be forced: %v", obj["stream"])
	}
	if obj["config_name"] != "glm-5.2" || obj["model"] != "glm-5.2" {
		t.Fatalf("model/config_name wrong: %v/%v", obj["model"], obj["config_name"])
	}
	messages := obj["messages"].([]any)
	first := messages[0].(map[string]any)
	chips, ok := first["content"].([]any)
	if !ok {
		t.Fatalf("string content must become chip array: %T", first["content"])
	}
	chip := chips[0].(map[string]any)
	if chip["type"] != "text" || chip["text"] != "You are Claude Code, Anthropic's official CLI for Claude." {
		t.Fatalf("chip wrong: %v", chip)
	}
	tools := obj["tools"].([]any)
	record := tools[0].(map[string]any)
	fn := record["function"].(map[string]any)
	if _, isString := fn["parameters"].(string); !isString {
		t.Fatalf("parameters must serialize to string: %T", fn["parameters"])
	}
}

func TestPrepareBodyDefaultsModel(t *testing.T) {
	payload := []byte(`{"messages":[{"role":"user","content":"hi"}],"stream":true}`)
	body, originalStream := prepareBody(payload, CNProfile())
	if !originalStream {
		t.Fatal("original stream must stay true")
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["config_name"] != CNProfile().DefaultModel {
		t.Fatalf("fallback model wrong: %v", obj["config_name"])
	}
}

func TestPrepareBodyToolChoiceNoneSuppresses(t *testing.T) {
	payload := []byte(`{"model":"m","messages":[],"stream":true,"tool_choice":{"type":"none"},"tools":[{"type":"function","function":{"name":"x"}}]}`)
	body, _ := prepareBody(payload, CNProfile())
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	if _, has := obj["tools"]; has {
		t.Fatalf("tools must be dropped on none: %v", obj["tools"])
	}
	if _, has := obj["tool_choice"]; has {
		t.Fatalf("tool_choice must be dropped on none: %v", obj["tool_choice"])
	}
}

func TestPrepareBodyAssistantToolCallsShape(t *testing.T) {
	payload := []byte(`{"model":"m","messages":[{"role":"assistant","content":"","tool_calls":[{"id":"t1","type":"function","function":{"name":"run","arguments":"{}"}}]}],"stream":true}`)
	body, _ := prepareBody(payload, CNProfile())
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatal(err)
	}
	messages := obj["messages"].([]any)
	call := messages[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if _, hasOpenAI := call["function"]; hasOpenAI {
		t.Fatalf("OpenAI function block must be rewritten to function_call: %v", call)
	}
	if fc, ok := call["function_call"].(map[string]any); !ok || fc["name"] != "run" {
		t.Fatalf("function_call wrong: %v", call["function_call"])
	}
}

func TestSSEScannerTracksPartialLines(t *testing.T) {
	scanner := &sseScanner{}
	events := scanner.step([]byte("id:1\nevent:output\ndata:{\"response\":\"he"))
	if len(events) != 0 {
		t.Fatalf("incomplete chunk must not emit: %v", events)
	}
	events = scanner.step([]byte("llo\"}\n\nif len == 0 then n"))
	if len(events) != 1 || events[0].kind != "output" || events[0].response != "hello" {
		t.Fatalf("output event wrong: %+v", events)
	}
	events = scanner.close()
	if len(events) != 0 {
		t.Fatalf("trailing junk must not emit an event: %v", events)
	}
}

func TestSSEScannerChunksAcrossReads(t *testing.T) {
	scanner := &sseScanner{}
	if events := scanner.step([]byte("event:done\n")); len(events) != 0 {
		t.Fatalf("event line alone must wait for data: %v", events)
	}
	events := scanner.step([]byte("data:{\"finish_reason\":\"stop\"}\n\n"))
	if len(events) != 1 || events[0].kind != "done" || events[0].finishReason != "stop" {
		t.Fatalf("done event wrong: %+v", events)
	}
}

func TestAggregateSSEBuildsChatCompletion(t *testing.T) {
	upstream := "" +
		"event:metadata\ndata:{\"session_id\":\"abc\"}\n\n" +
		"event:output\ndata:{\"response\":\"答\",\"reasoning_content\":\"思\"}\n\n" +
		"event:token_usage\ndata:{\"prompt_tokens\":21,\"completion_tokens\":1,\"total_tokens\":22}\n\n" +
		"event:done\ndata:{\"finish_reason\":\"stop\"}\n\n"
	raw, err := aggregateSSE(strings.NewReader(upstream), "glm-5.2")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Object  string         `json:"object"`
		Model   string         `json:"model"`
		Usage   map[string]any `json:"usage"`
		Choices []struct {
			Message struct {
				Role             string `json:"role"`
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Object != "chat.completion" || result.Model != "glm-5.2" {
		t.Fatalf("envelope wrong: %+v", result)
	}
	if result.Choices[0].Message.Content != "答" || result.Choices[0].Message.ReasoningContent != "思" {
		t.Fatalf("message wrong: %+v", result.Choices[0])
	}
	if result.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish wrong: %s", result.Choices[0].FinishReason)
	}
	if result.Usage["total_tokens"].(float64) != 22 {
		t.Fatalf("usage wrong: %v", result.Usage)
	}
}

func TestAggregateSSESurfacesStreamError(t *testing.T) {
	upstream := "event:error\ndata:{\"code\":1005,\"message\":\"plan limit\"}\n\n"
	if _, err := aggregateSSE(strings.NewReader(upstream), "m"); err == nil {
		t.Fatal("stream error must fail the aggregate")
	} else if !strings.Contains(err.Error(), "1005") {
		t.Fatalf("error must carry the code: %v", err)
	}
}

func TestAggregateSSESoloToolCallToOpenAI(t *testing.T) {
	upstream := "" +
		"event:output\ndata:{\"response\":\"\"}\n\n" +
		"event:output\ndata:{\"tool_calls\":[{\"id\":\"c1\",\"type\":\"function\",\"function_call\":{\"name\":\"run\",\"arguments\":\"{\\\"a\\\":\"}}]}\n\n" +
		"event:output\ndata:{\"tool_calls\":[{\"index\":0,\"function_call\":{\"arguments\":\"1}\"}}]}\n\n" +
		"event:done\ndata:{\"finish_reason\":\"tool_calls\"}\n\n"
	raw, err := aggregateSSE(strings.NewReader(upstream), "m")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content   string           `json:"content"`
				ToolCalls []map[string]any `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	call := result.Choices[0].Message.ToolCalls[0]
	if _, ok := call["function_call"]; ok {
		t.Fatalf("upstream function_call must become function: %v", call)
	}
	fn := call["function"].(map[string]any)
	if fn["name"] != "run" || fn["arguments"] != `{"a":1}` {
		t.Fatalf("merged call wrong: %v", fn)
	}
	if result.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish wrong: %s", result.Choices[0].FinishReason)
	}
}

// staticTransport replays one canned upstream response at the request the
// transport actually issued, so the URL/header rewrite is observable.
type staticTransport struct {
	lastReq *http.Request
	resp    *http.Response
}

func (t *staticTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.lastReq = req
	return t.resp, nil
}

// testKey is a JWT whose data.id claim yields a stable account identity.
func testKey() string {
	payload, _ := json.Marshal(map[string]any{"data": map[string]any{"id": "77"}})
	return "header." + base64RawURL(payload) + ".sig"
}

func TestTransportStreamTranslatesEndToEnd(t *testing.T) {
	upstream := "event:metadata\ndata:{\"session_id\":\"s\"}\n\n" +
		"event:output\ndata:{\"response\":\"你好\"}\n\n" +
		"event:output\ndata:{\"reasoning_content\":\"思考\"}\n\n" +
		"event:token_usage\ndata:{\"prompt_tokens\":2,\"completion_tokens\":2,\"total_tokens\":4}\n\n" +
		"event:done\ndata:{\"finish_reason\":\"stop\"}\n\n"
	base := &staticTransport{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}}
	client := WrapClient(&http.Client{Transport: base})

	body := `{"model":"glm-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(http.MethodPost, CNProfile().ChatHost+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testKey())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// The canonical URL rewrite and the SOLO header family land upstream.
	if base.lastReq == nil || base.lastReq.URL.String() != CNProfile().ChatHost+ChatPath {
		t.Fatalf("upstream URL not rewritten: %v", base.lastReq)
	}
	if got := base.lastReq.Header.Get("Authorization"); got != "Cloud-IDE-JWT "+testKey() {
		t.Fatalf("upstream Authorization wrong: %q", got)
	}
	if got := base.lastReq.Header.Get("X-Ide-Version"); got != CNProfile().IDEVersion {
		t.Fatalf("X-Ide-Version wrong: %q", got)
	}
	if uid := base.lastReq.Header.Get("X-Uid"); uid != "77" {
		t.Fatalf("X-Uid wrong: %q", uid)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("translated content-type wrong: %q", ct)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"finish_reason":"stop"`) {
		t.Fatalf("finish chunk missing: %s", text)
	}
	if !strings.Contains(text, "你好") || !strings.Contains(text, "思考") {
		t.Fatalf("content/reasoning chunks missing: %s", text)
	}
	if !strings.Contains(text, `"total_tokens":4`) {
		t.Fatalf("usage chunk missing: %s", text)
	}
	if !strings.HasSuffix(strings.TrimRight(text, "\n"), "data: [DONE]") {
		t.Fatalf("terminal [DONE] missing: %q", text[len(text)-60:])
	}
}

func TestTransportNonStreamAggregatesEndToEnd(t *testing.T) {
	upstream := "event:output\ndata:{\"response\":\"答\"}\n\n" +
		"event:done\ndata:{\"finish_reason\":\"stop\"}\n\n"
	base := &staticTransport{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}}
	client := WrapClient(&http.Client{Transport: base})

	body := `{"model":"glm-5.2","stream":false,"messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(http.MethodPost, CNProfile().ChatHost+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testKey())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("aggregated content-type wrong: %q", ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Object string `json:"object"`
		Model  string `json:"model"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Object != "chat.completion" || result.Model != "glm-5.2" {
		t.Fatalf("aggregated body wrong: %s", string(raw))
	}
}

func TestChatAndCatalogShareSoloHeaderFamily(t *testing.T) {
	key := testKey()
	chat := make(http.Header)
	applyChatHeaders(CNProfile(), chat, "Bearer "+key, true)
	catalog := make(http.Header)
	catalog.Set("Content-Type", "application/json")
	catalog.Set("Accept", "application/json")
	sessionKey := soloSessionKey("Bearer " + key)
	catalog.Set("Authorization", "Cloud-IDE-JWT "+sessionKey)
	catalog.Set("X-Cloudide-Token", sessionKey)
	stampSoloAccountFamily(CNProfile(), catalog, sessionKey)
	for _, name := range []string{
		"User-Agent", "x-app-id", "X-App-Version", "X-Ide-Version",
		"X-Ide-Version-Code", "X-App-Version-Code", "X-Ide-Version-Type",
		"X-Device-Type", "X-OS-Version", "X-Device-Brand",
		"Request-Traffic-Type", "X-Uid", "X-Machine-Id", "X-Device-Id",
	} {
		if chat.Get(name) != catalog.Get(name) {
			t.Fatalf("header %s diverges: chat=%q catalog=%q", name, chat.Get(name), catalog.Get(name))
		}
	}
}

func TestTransportPassesForeignHostThrough(t *testing.T) {
	base := &staticTransport{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}}
	client := WrapClient(&http.Client{Transport: base})
	req, _ := http.NewRequest(http.MethodPost, "https://api.deepseek.com/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}
	if base.lastReq.URL.Host != "api.deepseek.com" {
		t.Fatalf("foreign host must not be rewritten: %s", base.lastReq.URL)
	}
}

func TestAdaptTargetMatchesRealmHosts(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, CNProfile().ChatHost+"/v1/chat/completions", nil)
	if _, ok := adaptTarget(req); !ok {
		t.Fatal("cn chat host must adapt")
	}
	req, _ = http.NewRequest(http.MethodPost, GlobalProfile().ChatHost+"/chat/completions", nil)
	if _, ok := adaptTarget(req); !ok {
		t.Fatal("global chat host must adapt")
	}
	req, _ = http.NewRequest(http.MethodPost, "https://api.deepseek.com/v1/chat/completions", nil)
	if _, ok := adaptTarget(req); ok {
		t.Fatal("foreign host must not adapt")
	}
	req, _ = http.NewRequest(http.MethodGet, CNProfile().ChatHost+"/x", nil)
	if _, ok := adaptTarget(req); ok {
		t.Fatal("non-POST must not adapt")
	}
}

func TestProfileByChatHostResolvesEditionByAuthBase(t *testing.T) {
	if profile, ok := ProfileByChatHost(CNProfile().AuthBase); !ok || profile.ID != "cn" {
		t.Fatalf("auth base must resolve cn: %+v %v", profile, ok)
	}
	if profile, ok := ProfileByChatHost(GlobalProfile().AuthBase); !ok || profile.ID != "global" {
		t.Fatalf("global auth base must resolve: %+v %v", profile, ok)
	}
}

func TestCanonicalChatURLPinsHost(t *testing.T) {
	profile := CNProfile()
	if got := CanonicalChatURL(profile, "https://trae-api-cn.mchost.guru:443/"); got != profile.ChatHost {
		t.Fatalf("canonical url wrong: %s", got)
	}
	if got := CanonicalChatURL(profile, "https://api.example.com"); got != "https://api.example.com" {
		t.Fatalf("foreign url must stay: %s", got)
	}
}
