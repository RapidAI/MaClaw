package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const (
	mobileAgentMaxIterations = 4
	mobileAgentToolResultMax = 6000 // runes
)

// mobileLLMToolCall is one OpenAI-compatible tool call from the model.
type mobileLLMToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type mobileLLMCompletion struct {
	Content   string
	ToolCalls []mobileLLMToolCall
	RequestID string
}

// mobileAgentEventWriter emits progressive SSE events during the agent loop.
type mobileAgentEventWriter func(event string, data map[string]any)

func mobileAgentToolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "web_search",
				"description": "Search the public web for up-to-date facts. Use when the answer needs current information beyond the provided evidence snippets.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "Search query in the user's language when possible",
						},
						"max_results": map[string]any{
							"type":        "integer",
							"description": "Max results 1-10 (default 5)",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "web_fetch",
				"description": "Fetch and extract readable text from a public http(s) URL to verify details or read a page.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "Fully-qualified http(s) URL",
						},
						"max_chars": map[string]any{
							"type":        "integer",
							"description": "Max characters to return (default 4000, max 8000)",
						},
					},
					"required": []string{"url"},
				},
			},
		},
	}
}

func mobileAgentSystemAddon() string {
	// Legacy mini-loop only has web tools. Prefer not to permanently deny SSH —
	// the full core agent path handles Linux ops when available.
	return `

You may call tools when needed:
- web_search: discover sources for current facts
- web_fetch: open a specific URL to extract details
This fallback path has web tools only. For Linux server ops the user can paste host, username and password in chat (or use 连服务器); a later turn on the full agent path enables ssh.
Do not invent tool results. Prefer concise tool use (usually 0–2 calls). After tools, produce the final Markdown answer for the user.`
}

// mobileRunAgentLoop runs the Hub-side agent for Mobile.
// Prefer corelib/agentservice.CoreAgentExecutor (full tools: web, skills, MCP when wired).
// Falls back to a minimal web-only loop if the shared runtime cannot start.
// emit may be nil for non-stream JSON path.
func mobileRunAgentLoop(
	ctx context.Context,
	r *http.Request,
	principal *auth.ViewerPrincipal,
	officialLLM http.Handler,
	delegated mobileLlmAuthorizationRecord,
	useDelegated bool,
	baseMessages []map[string]string,
	emit mobileAgentEventWriter,
) (string, string, error) {
	if len(baseMessages) == 0 {
		return "", "", fmt.Errorf("agent messages are required")
	}
	if principal == nil {
		return "", "", errString("mobile account is no longer available")
	}
	// Keep the lifecycle read lock for the complete agent invocation. An agent
	// may write its workspace or materialize a private key after an LLM/tool
	// round trip, so merely checking the tombstone at request start would let an
	// in-flight invocation recreate files after an unbind. The unbind path takes
	// the write lock when it marks the owner deleted, which waits for this run to
	// finish before removing the agent runtime directory.
	mobileKnowledgePurgeState.RLock()
	defer mobileKnowledgePurgeState.RUnlock()
	if !mobileOwnerWriteAllowedLocked(principal.TenantID, mobilePrincipalOwnerID(principal)) {
		return "", "", errString("mobile account is no longer available")
	}
	if answer, requestID, ok := mobileTryCoreAgent(ctx, r, principal, officialLLM, delegated, useDelegated, baseMessages, emit); ok {
		return answer, requestID, nil
	}
	return mobileRunLegacyAgentLoop(ctx, r, officialLLM, delegated, useDelegated, baseMessages, emit)
}

// mobileRunLegacyAgentLoop is the previous web_search/web_fetch-only loop kept as
// a safety net when core agentservice cannot be started (misconfig, etc.).
func mobileRunLegacyAgentLoop(
	ctx context.Context,
	r *http.Request,
	officialLLM http.Handler,
	delegated mobileLlmAuthorizationRecord,
	useDelegated bool,
	baseMessages []map[string]string,
	emit mobileAgentEventWriter,
) (string, string, error) {
	// Promote to []map[string]any so we can attach tool_calls / tool roles.
	messages := make([]map[string]any, 0, len(baseMessages)+mobileAgentMaxIterations*3)
	for i, m := range baseMessages {
		role := m["role"]
		content := m["content"]
		if i == 0 && role == "system" {
			content = content + mobileAgentSystemAddon()
		}
		messages = append(messages, map[string]any{"role": role, "content": content})
	}

	tools := mobileAgentToolDefinitions()
	var lastRequestID string
	for iter := 0; iter < mobileAgentMaxIterations; iter++ {
		if ctx.Err() != nil {
			return "", lastRequestID, ctx.Err()
		}
		comp, err := mobileLLMChatCompletion(ctx, r, officialLLM, delegated, useDelegated, messages, tools)
		if err != nil {
			// Some backends reject tools; fall back to a plain completion once.
			if iter == 0 && len(tools) > 0 {
				plain, plainErr := mobileLLMChatCompletion(ctx, r, officialLLM, delegated, useDelegated, messages, nil)
				if plainErr == nil {
					return strings.TrimSpace(plain.Content), plain.RequestID, nil
				}
			}
			return "", lastRequestID, err
		}
		if comp.RequestID != "" {
			lastRequestID = comp.RequestID
		}
		if len(comp.ToolCalls) == 0 {
			return strings.TrimSpace(comp.Content), lastRequestID, nil
		}

		// Record assistant tool_calls turn.
		assistantMsg := map[string]any{
			"role":       "assistant",
			"content":    comp.Content,
			"tool_calls": mobileToolCallsForAPI(comp.ToolCalls),
		}
		messages = append(messages, assistantMsg)

		for _, tc := range comp.ToolCalls {
			if emit != nil {
				emit("tool_call", map[string]any{
					"id":        tc.ID,
					"name":      tc.Name,
					"arguments": mobileClipRunes(tc.Arguments, 500),
				})
			}
			result := mobileExecuteAgentTool(ctx, tc.Name, tc.Arguments)
			if emit != nil {
				emit("tool_result", map[string]any{
					"id":     tc.ID,
					"name":   tc.Name,
					"result": mobileClipRunes(result, 800),
				})
			}
			messages = append(messages, map[string]any{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      result,
			})
		}
	}
	// Force a final synthesis without tools.
	finalMessages := append(append([]map[string]any{}, messages...), map[string]any{
		"role":    "user",
		"content": "请根据已有工具结果，直接给出最终中文 Markdown 回答，不要再调用工具。",
	})
	comp, err := mobileLLMChatCompletion(ctx, r, officialLLM, delegated, useDelegated, finalMessages, nil)
	if err != nil {
		return "", lastRequestID, err
	}
	if comp.RequestID != "" {
		lastRequestID = comp.RequestID
	}
	return strings.TrimSpace(comp.Content), lastRequestID, nil
}

func mobileToolCallsForAPI(calls []mobileLLMToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, tc := range calls {
		out = append(out, map[string]any{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]any{
				"name":      tc.Name,
				"arguments": tc.Arguments,
			},
		})
	}
	return out
}

func mobileExecuteAgentTool(ctx context.Context, name, argumentsJSON string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	args := map[string]any{}
	if strings.TrimSpace(argumentsJSON) != "" {
		_ = json.Unmarshal([]byte(argumentsJSON), &args)
	}
	switch name {
	case "web_search":
		return mobileToolWebSearch(ctx, args)
	case "web_fetch":
		return mobileToolWebFetch(ctx, args)
	default:
		return fmt.Sprintf("unsupported tool %q", name)
	}
}

func mobileToolWebSearch(ctx context.Context, args map[string]any) string {
	query := strings.TrimSpace(fmt.Sprint(args["query"]))
	if query == "" || query == "<nil>" {
		return "缺少 query 参数"
	}
	maxResults := 5
	switch v := args["max_results"].(type) {
	case float64:
		if int(v) > 0 {
			maxResults = int(v)
		}
	case int:
		if v > 0 {
			maxResults = v
		}
	}
	if maxResults > 10 {
		maxResults = 10
	}
	results, err := mobileWebSearch(ctx, query, maxResults)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}
	if len(results) == 0 {
		return "未找到相关结果。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 条结果:\n", len(results))
	for i, r := range results {
		title := mobileCleanSearchText(r.Title, 120)
		snippet := mobileCleanSearchText(r.Snippet, 220)
		fmt.Fprintf(&b, "\n%d. %s\n   %s\n", i+1, title, strings.TrimSpace(r.URL))
		if snippet != "" {
			fmt.Fprintf(&b, "   %s\n", snippet)
		}
	}
	return mobileClipRunes(b.String(), mobileAgentToolResultMax)
}

func mobileToolWebFetch(ctx context.Context, args map[string]any) string {
	rawURL := strings.TrimSpace(fmt.Sprint(args["url"]))
	if rawURL == "" || rawURL == "<nil>" {
		return "缺少 url 参数"
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "url 必须是 http(s) 地址"
	}
	maxChars := 4000
	switch v := args["max_chars"].(type) {
	case float64:
		if int(v) > 0 {
			maxChars = int(v)
		}
	case int:
		if v > 0 {
			maxChars = v
		}
	}
	if maxChars > 8000 {
		maxChars = 8000
	}
	opts := &websearch.FetchOptions{TimeoutS: 20, MaxChars: maxChars}
	result, err := websearch.FetchCtx(ctx, rawURL, opts)
	if err != nil {
		return fmt.Sprintf("抓取失败: %v（可改用 web_search）", err)
	}
	if result == nil {
		return "抓取失败: empty result"
	}
	title := mobileCleanSearchText(result.Title, 160)
	content := mobileClipRunes(result.Content, maxChars)
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "标题: %s\n", title)
	}
	fmt.Fprintf(&b, "URL: %s\n\n%s", rawURL, content)
	return mobileClipRunes(b.String(), mobileAgentToolResultMax)
}

func mobileLLMChatCompletion(
	ctx context.Context,
	r *http.Request,
	officialLLM http.Handler,
	delegated mobileLlmAuthorizationRecord,
	useDelegated bool,
	messages []map[string]any,
	tools []map[string]any,
) (mobileLLMCompletion, error) {
	if useDelegated {
		return mobileThirdPartyChatCompletion(ctx, delegated, messages, tools)
	}
	if officialLLM == nil {
		return mobileLLMCompletion{}, fmt.Errorf("no LLM backend available")
	}
	return mobileOfficialChatCompletion(r, officialLLM, messages, tools)
}

func mobileOfficialChatCompletion(r *http.Request, handler http.Handler, messages []map[string]any, tools []map[string]any) (mobileLLMCompletion, error) {
	payload := map[string]any{
		"model":    "auto",
		"messages": messages,
		"stream":   false,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return mobileLLMCompletion{}, err
	}
	request := r.Clone(r.Context())
	request.Method = http.MethodPost
	urlCopy := *request.URL
	request.URL = &urlCopy
	request.URL.Path = "/api/llm/v1/chat/completions"
	request.RequestURI = request.URL.RequestURI()
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.Header = r.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	requestID := recorder.Header().Get("X-MaClaw-Request-ID")
	if recorder.Code < http.StatusOK || recorder.Code >= http.StatusMultipleChoices {
		return mobileLLMCompletion{}, fmt.Errorf("official LLM returned HTTP %d", recorder.Code)
	}
	return mobileParseChatCompletion(recorder.Body.Bytes(), requestID)
}

func mobileThirdPartyChatCompletion(ctx context.Context, record mobileLlmAuthorizationRecord, messages []map[string]any, tools []map[string]any) (mobileLLMCompletion, error) {
	if protocol := strings.TrimSpace(strings.ToLower(record.Protocol)); protocol != "" && protocol != "openai" {
		return mobileLLMCompletion{}, fmt.Errorf("unsupported desktop LLM protocol %q", protocol)
	}
	endpoint, err := mobileOpenAIChatCompletionURL(record.ProviderURL)
	if err != nil {
		return mobileLLMCompletion{}, err
	}
	model := strings.TrimSpace(record.Model)
	if model == "" {
		model = "auto"
	}
	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return mobileLLMCompletion{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return mobileLLMCompletion{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(record.APIKey))
	req.Header.Set("Content-Type", "application/json")
	// Bound third-party tool rounds so a slow provider cannot hang mobile forever.
	client := mobileLLMHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return mobileLLMCompletion{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return mobileLLMCompletion{}, err
	}
	requestID := response.Header.Get("X-Request-ID")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return mobileLLMCompletion{}, fmt.Errorf("desktop delegated LLM returned HTTP %d", response.StatusCode)
	}
	return mobileParseChatCompletion(raw, requestID)
}

func mobileParseChatCompletion(raw []byte, requestID string) (mobileLLMCompletion, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content   any `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return mobileLLMCompletion{}, err
	}
	if len(response.Choices) == 0 {
		return mobileLLMCompletion{}, fmt.Errorf("LLM response did not contain choices")
	}
	msg := response.Choices[0].Message
	content := mobileAnyContentToString(msg.Content)
	calls := make([]mobileLLMToolCall, 0, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			id = fmt.Sprintf("call_%d", i+1)
		}
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			continue
		}
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		calls = append(calls, mobileLLMToolCall{ID: id, Name: name, Arguments: args})
	}
	if strings.TrimSpace(content) == "" && len(calls) == 0 {
		return mobileLLMCompletion{}, fmt.Errorf("LLM response did not contain an answer")
	}
	return mobileLLMCompletion{
		Content:   content,
		ToolCalls: calls,
		RequestID: requestID,
	}, nil
}

func mobileAnyContentToString(content any) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, part := range v {
			switch p := part.(type) {
			case string:
				b.WriteString(p)
			case map[string]any:
				if t, ok := p["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}
