package llm

// Unified OpenAI-compatible LLM HTTP client.
// All packages (gui, tui, hub/corelib/agent) should use these functions
// instead of implementing their own request/response logic.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const maxToolArgumentsBytes = 180 * 1024

// HTTPStatusError carries an LLM HTTP error body for structured callers while
// keeping Error() body-free so logs and UI messages do not echo sensitive data.
type HTTPStatusError struct {
	StatusCode int
	Body       []byte
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "HTTP error"
	}
	return fmt.Sprintf("HTTP %d: body_len=%d", e.StatusCode, len(e.Body))
}

// OpenAIChatRequestOptions controls how an OpenAI-compatible chat/completions
// request is built.
type OpenAIChatRequestOptions struct {
	Stream         bool
	Tools          []map[string]interface{}
	ExtraBody      map[string]interface{}
	PassThrough    map[string]interface{}
	ToolChoice     interface{}
	ResponseFormat interface{}
}

var openAIChatPassThroughKeys = []string{
	"temperature",
	"top_p",
	"max_tokens",
	"max_completion_tokens",
	"presence_penalty",
	"frequency_penalty",
	"stop",
	"parallel_tool_calls",
	"user",
	"seed",
	"n",
	"logprobs",
	"top_logprobs",
	"stream_options",
	"metadata",
	"store",
	"service_tier",
	"reasoning_effort",
	"modalities",
	"prediction",
	"audio",
	"web_search_options",
}

func buildOpenAIChatRequestBody(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts OpenAIChatRequestOptions,
) map[string]interface{} {
	// Provider-specific message adaptation
	if corelib.NeedsSystemMerge(cfg) {
		messages = corelib.MergeSystemIntoUser(messages)
	}
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		messages = sanitizeConservativeOpenAICompatMessages(messages)
		messages = relocateOversizedSystemPromptForOpenAICompat(cfg, messages)
	}
	if corelib.IsDeepSeekFlashOpenAICompat(cfg) {
		messages = normalizeOpenAICompatDeveloperMessages(messages)
	}
	if corelib.IsDeepSeekFlashOpenAICompat(cfg) || corelib.IsGLMCodingPlanOpenAICompat(cfg) {
		messages = normalizeOpenAICompatTextOnlyMessageContent(messages)
	}
	messages = normalizeOpenAIChatToolCallLinkage(messages)
	messages = sanitizeOpenAIChatMessagesForSDKCompatibility(messages, corelib.IsDeepSeekThinking(cfg))
	messages = sanitizeEmptyToolCalls(messages)
	messages = sanitizeInvalidToolCallArguments(messages)
	messages = sanitizeOrphanedToolCalls(messages, cfg.NeedsConservativeOpenAICompatSanitization() || corelib.IsGLMCodingPlanOpenAICompat(cfg))
	messages = ensureOpenAIChatMessagesNotEmpty(messages)
	if corelib.IsGLMCodingPlanOpenAICompat(cfg) {
		messages = normalizeGLMCodingPlanEmptyUserContent(messages)
	}

	model := cfg.UpstreamModel()
	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   opts.Stream,
	}
	if len(opts.Tools) > 0 && !ShouldOmitOpenAIToolsForInitialRequest(cfg, messages) {
		tools := sanitizeOpenAIChatToolsForSDK(opts.Tools)
		if cfg.NeedsConservativeOpenAICompatSanitization() {
			tools = corelib.SanitizeCodeGenOpenAIChatTools(tools)
		}
		if len(tools) > 0 {
			reqBody["tools"] = tools
		}
	}
	if opts.ToolChoice != nil {
		if toolChoice := sanitizeOpenAIToolChoiceForProvider(cfg, opts.ToolChoice); toolChoice != nil {
			reqBody["tool_choice"] = toolChoice
		}
	}
	if opts.ResponseFormat != nil {
		if responseFormat := sanitizeOpenAIResponseFormatForProvider(cfg, opts.ResponseFormat); responseFormat != nil {
			reqBody["response_format"] = responseFormat
		}
	}
	for _, k := range openAIChatPassThroughKeys {
		if opts.PassThrough != nil {
			if v, ok := opts.PassThrough[k]; ok {
				reqBody[k] = v
			}
		}
	}
	for k, v := range opts.ExtraBody {
		switch k {
		case "model", "messages", "stream", "tools", "tool_choice", "response_format":
			continue
		default:
			reqBody[k] = v
		}
	}
	if cfg.NeedsConservativeOpenAICompatSanitization() {
		corelib.SanitizeCodeGenOpenAICompatBody(reqBody)
	}
	sanitizeOpenAIChatRequestBodyForSDKCompatibility(reqBody)
	if corelib.IsDeepSeekFlashOpenAICompat(cfg) {
		normalizeDeepSeekFlashUnsupportedOptions(reqBody)
		ensureDeepSeekFlashJSONResponseInstruction(reqBody)
	}
	return reqBody
}

func ShouldOmitOpenAIToolsForInitialRequest(cfg corelib.MaclawLLMConfig, messages []interface{}) bool {
	_ = cfg
	_ = messages
	return false
}

func BuildOpenAIChatRequestData(
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts OpenAIChatRequestOptions,
) (endpoint string, body []byte, err error) {
	endpoint = BuildOpenAIChatCompletionsEndpoint(corelib.NormalizeGLMCodingPlanOpenAIBaseURL(cfg.URL, cfg.UserAgent()))
	body, err = json.Marshal(buildOpenAIChatRequestBody(cfg, messages, opts))
	return endpoint, body, err
}

func BuildOpenAIChatCompletionsEndpoint(rawURL string) string {
	endpoint := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if strings.HasSuffix(endpoint, "/chat/completions") {
		return endpoint
	}
	if llmEndpointHasVersionSuffix(endpoint) {
		return endpoint + "/chat/completions"
	}
	return endpoint + "/v1/chat/completions"
}

func BuildOpenAIModelsEndpointCandidates(rawURL, protocol string) []string {
	baseURL := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if baseURL == "" {
		return nil
	}
	if strings.HasSuffix(baseURL, "/chat/completions") {
		baseURL = strings.TrimRight(strings.TrimSuffix(baseURL, "/chat/completions"), "/")
	}
	if strings.HasSuffix(baseURL, "/models") {
		return []string{baseURL}
	}
	candidates := []string{baseURL + "/models"}
	if strings.TrimSpace(protocol) == "anthropic" {
		candidates = []string{baseURL + "/v1/models", baseURL + "/models"}
	} else if !llmEndpointHasVersionSuffix(baseURL) {
		candidates = append(candidates, baseURL+"/v1/models")
	}
	return dedupeOpenAIEndpointCandidates(candidates)
}

func dedupeOpenAIEndpointCandidates(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func NewOpenAIChatRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	opts OpenAIChatRequestOptions,
) (*http.Request, []byte, string, error) {
	endpoint, data, err := BuildOpenAIChatRequestData(cfg, messages, opts)
	if err != nil {
		return nil, nil, endpoint, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, nil, endpoint, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", cfg.UserAgent())
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, cfg.UserAgent())
	return req, data, endpoint, nil
}

func SummarizeOpenAIChatRequestBody(body []byte) string {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Sprintf("invalid_json req_len=%d", len(body))
	}
	messagesLen := jsonArrayLen(payload["messages"])
	toolsLen := jsonArrayLen(payload["tools"])
	functionsLen := jsonArrayLen(payload["functions"])
	_, hasStreamOptions := payload["stream_options"]
	_, hasToolChoice := payload["tool_choice"]
	_, hasResponseFormat := payload["response_format"]
	return fmt.Sprintf("req_len=%d model=%q stream=%v messages=%d tools=%d functions=%d stream_options=%t tool_choice=%t response_format=%t",
		len(body), payload["model"], payload["stream"], messagesLen, toolsLen, functionsLen, hasStreamOptions, hasToolChoice, hasResponseFormat)
}

func jsonArrayLen(value interface{}) int {
	switch v := value.(type) {
	case []interface{}:
		return len(v)
	case []map[string]interface{}:
		return len(v)
	default:
		return 0
	}
}

func sanitizeConservativeOpenAICompatMessages(messages []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		switch m := message.(type) {
		case map[string]interface{}:
			_, hasReasoning := m["reasoning_content"]
			role, _ := m["role"].(string)
			isLegacyToolResult := role == "tool_result"
			isToolResult := role == "tool" || isLegacyToolResult
			contentIsNil := false
			if content, ok := m["content"]; ok && content == nil {
				contentIsNil = true
			}
			contentNeedsString := false
			if isToolResult {
				if content, ok := m["content"]; ok {
					_, contentNeedsString = content.(string)
					contentNeedsString = !contentNeedsString
				}
			}
			_, hasToolUseID := m["tool_use_id"]
			if !hasReasoning && !contentIsNil && !isLegacyToolResult && !contentNeedsString && !hasToolUseID {
				normalized = append(normalized, message)
				continue
			}
			patched := make(map[string]interface{}, len(m)+1)
			for k, v := range m {
				if k == "reasoning_content" {
					continue
				}
				if k == "role" && isLegacyToolResult {
					patched[k] = "tool"
					continue
				}
				if k == "tool_use_id" {
					if _, ok := m["tool_call_id"]; !ok {
						patched["tool_call_id"] = v
					}
					continue
				}
				if k == "content" && v == nil {
					patched[k] = ""
					continue
				}
				if k == "content" && isToolResult {
					patched[k] = stringifyOpenAIToolContent(v)
					continue
				}
				patched[k] = v
			}
			normalized = append(normalized, patched)
		case map[string]string:
			role := m["role"]
			isLegacyToolResult := role == "tool_result"
			_, hasToolUseID := m["tool_use_id"]
			if _, ok := m["reasoning_content"]; !ok && !isLegacyToolResult && !hasToolUseID {
				normalized = append(normalized, message)
				continue
			}
			patched := make(map[string]string, len(m))
			for k, v := range m {
				if k == "reasoning_content" {
					continue
				}
				if k == "role" && isLegacyToolResult {
					patched[k] = "tool"
					continue
				}
				if k == "tool_use_id" {
					if _, ok := m["tool_call_id"]; !ok {
						patched["tool_call_id"] = v
					}
					continue
				}
				patched[k] = v
			}
			normalized = append(normalized, patched)
		default:
			normalized = append(normalized, message)
		}
	}
	return normalizeConservativeOpenAICompatSystemMessages(normalized)
}

func normalizeConservativeOpenAICompatSystemMessages(messages []interface{}) []interface{} {
	if len(messages) == 0 {
		return messages
	}
	systemIndex := -1
	systemContent := make([]string, 0, 1)
	for i, message := range messages {
		if messageRole(message) != "system" {
			continue
		}
		if systemIndex < 0 {
			systemIndex = i
		}
		if content := stringifyOpenAIToolContent(messageContent(message)); strings.TrimSpace(content) != "" {
			systemContent = append(systemContent, content)
		}
	}
	if systemIndex < 0 {
		return messages
	}
	out := make([]interface{}, 0, len(messages)-len(systemContent)+1)
	out = append(out, withMessageContent(messages[systemIndex], strings.Join(systemContent, "\n\n")))
	for i, message := range messages {
		if i == systemIndex || messageRole(message) == "system" {
			continue
		}
		out = append(out, message)
	}
	return out
}

const conservativeOpenAICompatSystemPromptLimit = 12 * 1024

const conservativeOpenAICompatCompactSystemPrompt = "You are MaClaw, a helpful AI assistant. Follow the user's instructions, use available tools when appropriate, and answer in Chinese unless asked otherwise."

const openAICompatUnsupportedNonTextContentPlaceholder = "[Unsupported non-text content omitted]"

func relocateOversizedSystemPromptForOpenAICompat(cfg corelib.MaclawLLMConfig, messages []interface{}) []interface{} {
	if len(messages) == 0 || !cfg.NeedsConservativeOpenAICompatSanitization() {
		return messages
	}
	if messageRole(messages[0]) != "system" {
		return messages
	}
	systemContent := strings.TrimSpace(stringifyOpenAIToolContent(messageContent(messages[0])))
	if len(systemContent) <= conservativeOpenAICompatSystemPromptLimit {
		return messages
	}
	out := make([]interface{}, len(messages))
	copy(out, messages)
	out[0] = withMessageContent(out[0], conservativeOpenAICompatCompactSystemPrompt)
	context := compactOversizedOpenAICompatSystemContext(systemContent)
	contextBlock := "[Runtime context]\n" + context + "\n[/Runtime context]"
	for i := 1; i < len(out); i++ {
		if messageRole(out[i]) != "user" {
			continue
		}
		out[i] = withMessageContent(out[i], joinOpenAICompatText(contextBlock, messageContent(out[i])))
		return out
	}
	out = append(out, map[string]interface{}{
		"role":    "user",
		"content": contextBlock,
	})
	return out
}

func compactOversizedOpenAICompatSystemContext(systemContent string) string {
	systemContent = strings.TrimSpace(systemContent)
	parts := make([]string, 0, 2)
	if task := extractOpenAICompatTaskContext(systemContent); task != "" {
		parts = append(parts, task)
	}
	if skill := extractOpenAICompatSkillPreference(systemContent); skill != "" {
		parts = append(parts, skill)
	}
	if len(parts) > 0 {
		return limitOpenAICompatRuntimeContext(strings.Join(parts, "\n\n"), conservativeOpenAICompatRuntimeContextLimit)
	}
	return limitOpenAICompatRuntimeContext(systemContent, conservativeOpenAICompatRuntimeContextLimit)
}

func CompactOpenAICompatMessagesForToollessRetry(cfg corelib.MaclawLLMConfig, messages []interface{}) []interface{} {
	if !cfg.NeedsConservativeOpenAICompatSanitization() {
		return nil
	}
	runtimeContext := compactOpenAICompatRuntimeContext(messages)
	latestUser := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messageRole(messages[i]) != "user" {
			continue
		}
		latestUser = strings.TrimSpace(stringifyOpenAIToolContent(messageContent(messages[i])))
		if latestUser != "" {
			break
		}
	}
	if latestUser == "" {
		return nil
	}
	userContentParts := []string{
		"[Compatibility retry]",
		"The previous full runtime context and tool schemas were omitted because the OpenAI-compatible server rejected them with HTTP 400.",
		"Complete the user's latest request directly. Do not mention this retry.",
	}
	if runtimeContext != "" {
		userContentParts = append(userContentParts, "", "[Relevant runtime context]", runtimeContext)
	}
	userContentParts = append(userContentParts, "", "[User request]", latestUser)
	return []interface{}{
		map[string]interface{}{
			"role":    "system",
			"content": conservativeOpenAICompatCompactSystemPrompt,
		},
		map[string]interface{}{
			"role":    "user",
			"content": strings.Join(userContentParts, "\n"),
		},
	}
}

const conservativeOpenAICompatRuntimeContextLimit = 8 * 1024

func compactOpenAICompatRuntimeContext(messages []interface{}) string {
	parts := make([]string, 0, 2)
	for _, message := range messages {
		if messageRole(message) != "system" {
			continue
		}
		content := strings.TrimSpace(stringifyOpenAIToolContent(messageContent(message)))
		if content == "" {
			continue
		}
		if section := extractOpenAICompatTaskContext(content); section != "" {
			parts = append(parts, section)
		}
		if skill := extractOpenAICompatSkillPreference(content); skill != "" {
			parts = append(parts, skill)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return limitOpenAICompatRuntimeContext(strings.Join(parts, "\n\n"), conservativeOpenAICompatRuntimeContextLimit)
}

func extractOpenAICompatTaskContext(content string) string {
	lines := strings.Split(content, "\n")
	keywords := []string{
		"当前任务",
		"用户需求",
		"用户选择的本地文件路径",
		"项目路径",
		"阶段指令",
		"重要约束",
		"Current task",
		"User request",
		"Selected local file",
		"Project path",
		"Phase instruction",
		"Important constraints",
	}
	var out []string
	capturing := false
	blankRun := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if containsOpenAICompatTaskKeyword(trimmed, keywords) {
			capturing = true
			blankRun = 0
		} else if capturing && strings.HasPrefix(trimmed, "## ") && !containsOpenAICompatTaskKeyword(trimmed, keywords) {
			capturing = false
		}
		if !capturing {
			continue
		}
		if trimmed == "" {
			blankRun++
			if blankRun > 2 {
				continue
			}
		} else {
			blankRun = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func extractOpenAICompatSkillPreference(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	capturing := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "[Skill preference]") {
			capturing = true
		} else if capturing && strings.HasPrefix(trimmed, "## ") {
			break
		}
		if capturing {
			out = append(out, line)
		}
	}
	return limitOpenAICompatRuntimeContext(strings.TrimSpace(strings.Join(out, "\n")), 1200)
}

func containsOpenAICompatTaskKeyword(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func limitOpenAICompatRuntimeContext(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	if limit <= 32 {
		return text[:limit]
	}
	return strings.TrimSpace(text[:limit-32]) + "\n[...truncated for compatibility...]"
}

func normalizeOpenAICompatDeveloperMessages(messages []interface{}) []interface{} {
	out := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		switch m := message.(type) {
		case map[string]interface{}:
			if role, _ := m["role"].(string); role == "developer" {
				patched := make(map[string]interface{}, len(m))
				for k, v := range m {
					patched[k] = v
				}
				patched["role"] = "system"
				out = append(out, patched)
				continue
			}
		case map[string]string:
			if m["role"] == "developer" {
				patched := make(map[string]string, len(m))
				for k, v := range m {
					patched[k] = v
				}
				patched["role"] = "system"
				out = append(out, patched)
				continue
			}
		}
		out = append(out, message)
	}
	return out
}

func normalizeOpenAICompatTextOnlyMessageContent(messages []interface{}) []interface{} {
	out := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		mm := toStringInterfaceMap(message)
		if mm == nil {
			out = append(out, message)
			continue
		}
		content, ok := textFromOpenAIContentBlocks(mm["content"])
		if !ok {
			out = append(out, message)
			continue
		}
		if content == "" && messageRole(mm) == "user" {
			content = openAICompatUnsupportedNonTextContentPlaceholder
		}
		patched := make(map[string]interface{}, len(mm))
		for k, v := range mm {
			patched[k] = v
		}
		patched["content"] = content
		out = append(out, patched)
	}
	return out
}

func textFromOpenAIContentBlocks(content interface{}) (string, bool) {
	var blocks []interface{}
	switch v := content.(type) {
	case []interface{}:
		blocks = v
	case []map[string]interface{}:
		blocks = make([]interface{}, 0, len(v))
		for _, item := range v {
			blocks = append(blocks, item)
		}
	default:
		blocks = toInterfaceSlice(v)
		if len(blocks) == 0 {
			return "", false
		}
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		mm := toStringInterfaceMap(block)
		if mm == nil {
			continue
		}
		switch strings.TrimSpace(stringValue(mm["type"])) {
		case "text", "input_text", "output_text":
		default:
			continue
		}
		if text := stringValue(mm["text"]); text != "" {
			parts = append(parts, text)
			continue
		}
		if text := stringValue(mm["content"]); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, ""), true
}

func joinOpenAICompatText(left string, right interface{}) string {
	rightText := strings.TrimSpace(stringifyOpenAIToolContent(right))
	left = strings.TrimSpace(left)
	switch {
	case left == "":
		return rightText
	case rightText == "":
		return left
	default:
		return left + "\n\n" + rightText
	}
}

func messageRole(message interface{}) string {
	m := toStringInterfaceMap(message)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(stringValue(m["role"]))
}

func messageContent(message interface{}) interface{} {
	m := toStringInterfaceMap(message)
	if m == nil {
		return nil
	}
	return m["content"]
}

func withMessageContent(message interface{}, content string) interface{} {
	switch m := message.(type) {
	case map[string]interface{}:
		patched := make(map[string]interface{}, len(m))
		for k, v := range m {
			patched[k] = v
		}
		patched["content"] = content
		return patched
	case map[string]string:
		patched := make(map[string]string, len(m))
		for k, v := range m {
			patched[k] = v
		}
		patched["content"] = content
		return patched
	default:
		if mm := toStringInterfaceMap(message); mm != nil {
			patched := make(map[string]interface{}, len(mm))
			for k, v := range mm {
				patched[k] = v
			}
			patched["content"] = content
			return patched
		}
		return message
	}
}

func stringifyOpenAIToolContent(content interface{}) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func sanitizeOpenAIChatMessagesForSDKCompatibility(messages []interface{}, preserveReasoningContent bool) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		mm := toStringInterfaceMap(message)
		if mm == nil {
			continue
		}
		role := strings.TrimSpace(stringValue(mm["role"]))
		if role == "" {
			continue
		}
		switch role {
		case "system", "developer":
			out := map[string]interface{}{"role": role}
			if name := strings.TrimSpace(stringValue(mm["name"])); name != "" {
				out["name"] = name
			}
			out["content"] = sanitizeOpenAIMessageContent(mm["content"], false)
			normalized = append(normalized, out)
		case "user":
			out := map[string]interface{}{"role": "user"}
			if name := strings.TrimSpace(stringValue(mm["name"])); name != "" {
				out["name"] = name
			}
			out["content"] = sanitizeOpenAIMessageContent(mm["content"], false)
			normalized = append(normalized, out)
		case "assistant":
			out := map[string]interface{}{"role": "assistant"}
			if name := strings.TrimSpace(stringValue(mm["name"])); name != "" {
				out["name"] = name
			}
			if content, ok := mm["content"]; ok {
				out["content"] = sanitizeOpenAIMessageContent(content, false)
			}
			if preserveReasoningContent {
				if reasoning, ok := mm["reasoning_content"].(string); ok {
					out["reasoning_content"] = reasoning
				}
			}
			if toolCalls := sanitizeOpenAIToolCallsForSDK(mm["tool_calls"]); len(toolCalls) > 0 {
				out["tool_calls"] = toolCalls
			}
			if functionCall := sanitizeOpenAIFunctionCallForSDK(mm["function_call"]); functionCall != nil {
				out["function_call"] = functionCall
			}
			normalized = append(normalized, out)
		case "tool":
			callID := strings.TrimSpace(stringValue(mm["tool_call_id"]))
			if callID == "" {
				continue
			}
			normalized = append(normalized, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": callID,
				"content":      stringifyOpenAIToolContent(mm["content"]),
			})
		case "function":
			name := strings.TrimSpace(stringValue(mm["name"]))
			if name == "" {
				continue
			}
			normalized = append(normalized, map[string]interface{}{
				"role":    "function",
				"name":    name,
				"content": stringifyOpenAIToolContent(mm["content"]),
			})
		default:
			continue
		}
	}
	return normalized
}

func normalizeOpenAIChatToolCallLinkage(messages []interface{}) []interface{} {
	if len(messages) == 0 {
		return messages
	}
	out := make([]interface{}, 0, len(messages))
	var pendingToolCallIDs []string
	for _, message := range messages {
		mm := toStringInterfaceMap(message)
		if mm == nil {
			out = append(out, message)
			pendingToolCallIDs = nil
			continue
		}
		role := strings.TrimSpace(stringValue(mm["role"]))
		switch role {
		case "assistant":
			normalizedCalls, ok := normalizeOpenAIChatToolCallsForLinkage(mm["tool_calls"])
			if !ok || len(normalizedCalls) == 0 {
				out = append(out, message)
				pendingToolCallIDs = nil
				continue
			}
			patched := make(map[string]interface{}, len(mm))
			for k, v := range mm {
				patched[k] = v
			}
			patched["tool_calls"] = normalizedCalls
			pendingToolCallIDs = make([]string, 0, len(normalizedCalls))
			for _, call := range normalizedCalls {
				if id := strings.TrimSpace(stringValue(call["id"])); id != "" {
					pendingToolCallIDs = append(pendingToolCallIDs, id)
				}
			}
			out = append(out, patched)
		case "tool":
			if len(pendingToolCallIDs) == 0 {
				out = append(out, message)
				continue
			}
			callID := strings.TrimSpace(stringValue(mm["tool_call_id"]))
			if callID == "" {
				patched := make(map[string]interface{}, len(mm)+1)
				for k, v := range mm {
					patched[k] = v
				}
				patched["tool_call_id"] = pendingToolCallIDs[0]
				out = append(out, patched)
				pendingToolCallIDs = pendingToolCallIDs[1:]
				continue
			}
			if callID == pendingToolCallIDs[0] {
				pendingToolCallIDs = pendingToolCallIDs[1:]
			}
			out = append(out, message)
		default:
			out = append(out, message)
			pendingToolCallIDs = nil
		}
	}
	return out
}

func normalizeOpenAIChatToolCallsForLinkage(raw interface{}) ([]map[string]interface{}, bool) {
	if raw == nil {
		return nil, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	var calls []map[string]interface{}
	if err := json.Unmarshal(data, &calls); err != nil {
		return nil, false
	}
	out := make([]map[string]interface{}, 0, len(calls))
	for _, call := range calls {
		fn := toStringInterfaceMap(call["function"])
		if fn == nil {
			continue
		}
		name := strings.TrimSpace(stringValue(fn["name"]))
		if name == "" {
			continue
		}
		id := strings.TrimSpace(stringValue(call["id"]))
		if id == "" {
			id = randomContentToolCallID()
		}
		out = append(out, map[string]interface{}{
			"id":   id,
			"type": normalizeToolCallType(stringValue(call["type"])),
			"function": map[string]interface{}{
				"name":      name,
				"arguments": normalizeOpenAIToolArgumentsString(fn["arguments"]),
			},
		})
	}
	return out, len(out) > 0
}

func ensureOpenAIChatMessagesNotEmpty(messages []interface{}) []interface{} {
	if len(messages) > 0 {
		return messages
	}
	return []interface{}{map[string]interface{}{"role": "user", "content": ""}}
}

const glmCodingPlanEmptyUserContentPlaceholder = "[No user content provided]"

func normalizeGLMCodingPlanEmptyUserContent(messages []interface{}) []interface{} {
	out := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		mm := toStringInterfaceMap(message)
		if mm == nil || strings.TrimSpace(stringValue(mm["role"])) != "user" {
			out = append(out, message)
			continue
		}
		content := sanitizeOpenAIMessageContent(mm["content"], false)
		text, ok := content.(string)
		if !ok || strings.TrimSpace(text) != "" {
			out = append(out, message)
			continue
		}
		patched := make(map[string]interface{}, len(mm))
		for k, v := range mm {
			patched[k] = v
		}
		patched["content"] = glmCodingPlanEmptyUserContentPlaceholder
		out = append(out, patched)
	}
	return out
}

func sanitizeOpenAIMessageContent(content interface{}, allowNil bool) interface{} {
	switch v := content.(type) {
	case nil:
		if allowNil {
			return nil
		}
		return ""
	case string:
		return v
	case []interface{}:
		blocks := sanitizeOpenAIContentBlocks(v)
		if len(blocks) == 0 {
			return ""
		}
		return blocks
	case []map[string]interface{}:
		items := make([]interface{}, 0, len(v))
		for _, item := range v {
			items = append(items, item)
		}
		blocks := sanitizeOpenAIContentBlocks(items)
		if len(blocks) == 0 {
			return ""
		}
		return blocks
	default:
		return stringifyOpenAIToolContent(v)
	}
}

func sanitizeOpenAIContentBlocks(blocks []interface{}) []interface{} {
	out := make([]interface{}, 0, len(blocks))
	for _, block := range blocks {
		mm := toStringInterfaceMap(block)
		if mm == nil {
			continue
		}
		typ := strings.TrimSpace(stringValue(mm["type"]))
		switch typ {
		case "text", "input_text", "output_text":
			text := stringValue(mm["text"])
			if text == "" {
				text = stringValue(mm["content"])
			}
			out = append(out, map[string]interface{}{"type": "text", "text": text})
		case "image_url":
			if imageURL := toStringInterfaceMap(mm["image_url"]); imageURL != nil {
				clean := map[string]interface{}{}
				if url := strings.TrimSpace(stringValue(imageURL["url"])); url != "" {
					clean["url"] = url
				}
				if detail := strings.TrimSpace(stringValue(imageURL["detail"])); detail != "" {
					clean["detail"] = detail
				}
				if len(clean) > 0 {
					out = append(out, map[string]interface{}{"type": "image_url", "image_url": clean})
				}
			}
		case "input_audio":
			if audio := toStringInterfaceMap(mm["input_audio"]); audio != nil {
				clean := map[string]interface{}{}
				if data := stringValue(audio["data"]); data != "" {
					clean["data"] = data
				}
				if format := strings.TrimSpace(stringValue(audio["format"])); format != "" {
					clean["format"] = format
				}
				if len(clean) > 0 {
					out = append(out, map[string]interface{}{"type": "input_audio", "input_audio": clean})
				}
			}
		case "file":
			if file := toStringInterfaceMap(mm["file"]); file != nil {
				clean := map[string]interface{}{}
				for _, key := range []string{"file_id", "filename", "file_data"} {
					if value := stringValue(file[key]); value != "" {
						clean[key] = value
					}
				}
				if len(clean) > 0 {
					out = append(out, map[string]interface{}{"type": "file", "file": clean})
				}
			}
		}
	}
	return out
}

func sanitizeOpenAIToolCallsForSDK(raw interface{}) []interface{} {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var calls []map[string]interface{}
	if err := json.Unmarshal(data, &calls); err != nil {
		return nil
	}
	out := make([]interface{}, 0, len(calls))
	for _, call := range calls {
		id := strings.TrimSpace(stringValue(call["id"]))
		fn, _ := call["function"].(map[string]interface{})
		name := strings.TrimSpace(stringValue(fn["name"]))
		if name == "" {
			continue
		}
		if id == "" {
			id = randomContentToolCallID()
		}
		args := normalizeOpenAIToolArgumentsString(fn["arguments"])
		out = append(out, map[string]interface{}{
			"id":   id,
			"type": "function",
			"function": map[string]interface{}{
				"name":      name,
				"arguments": args,
			},
		})
	}
	return out
}

func sanitizeOpenAIChatToolsForSDK(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		fn := toStringInterfaceMap(tool["function"])
		if fn == nil {
			continue
		}
		name := strings.TrimSpace(stringValue(fn["name"]))
		if name == "" {
			continue
		}
		cleanFn := map[string]interface{}{"name": name}
		if desc := stringValue(fn["description"]); desc != "" {
			cleanFn["description"] = desc
		}
		if params, ok := fn["parameters"]; ok && params != nil {
			cleanFn["parameters"] = sanitizeOpenAIToolParametersForSDK(params)
		}
		if strict, ok := fn["strict"].(bool); ok {
			cleanFn["strict"] = strict
		}
		out = append(out, map[string]interface{}{
			"type":     "function",
			"function": cleanFn,
		})
	}
	return out
}

func SanitizeOpenAIChatToolsForSDK(tools []map[string]interface{}) []map[string]interface{} {
	return sanitizeOpenAIChatToolsForSDK(tools)
}

func sanitizeOpenAIToolParametersForSDK(params interface{}) interface{} {
	clean := sanitizeOpenAIToolSchemaShape(params)
	if clean != nil {
		return clean
	}
	return params
}

func sanitizeOpenAIToolSchemaShape(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x)+2)
		for k, val := range x {
			switch k {
			case "properties":
				out[k] = sanitizeOpenAIToolSchemaProperties(val)
			case "required":
				if required := sanitizeOpenAIToolSchemaRequired(val); len(required) > 0 {
					out[k] = required
				}
			default:
				out[k] = sanitizeOpenAIToolSchemaShape(val)
			}
		}
		typ := strings.TrimSpace(stringValue(out["type"]))
		if typ == "" {
			out["type"] = "object"
			typ = "object"
		}
		if typ == "object" {
			if _, ok := out["properties"]; !ok {
				out["properties"] = map[string]interface{}{}
			}
		}
		if typ == "array" {
			if _, ok := out["items"]; !ok {
				out["items"] = map[string]interface{}{"type": "string"}
			}
		}
		return out
	case map[string]string:
		out := make(map[string]interface{}, len(x)+2)
		for k, val := range x {
			if k == "required" {
				continue
			}
			out[k] = val
		}
		typ := strings.TrimSpace(stringValue(out["type"]))
		if typ == "" {
			out["type"] = "object"
			typ = "object"
		}
		if typ == "object" {
			if _, ok := out["properties"]; !ok {
				out["properties"] = map[string]interface{}{}
			}
		}
		if typ == "array" {
			if _, ok := out["items"]; !ok {
				out["items"] = map[string]interface{}{"type": "string"}
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = sanitizeOpenAIToolSchemaShape(val)
		}
		return out
	case []map[string]interface{}:
		out := make([]interface{}, 0, len(x))
		for _, val := range x {
			out = append(out, sanitizeOpenAIToolSchemaShape(val))
		}
		return out
	default:
		if mm := toStringInterfaceMap(v); mm != nil {
			return sanitizeOpenAIToolSchemaShape(mm)
		}
		if items := toInterfaceSlice(v); len(items) > 0 {
			return sanitizeOpenAIToolSchemaShape(items)
		}
		return v
	}
}

func sanitizeOpenAIToolSchemaRequired(raw interface{}) []interface{} {
	items := toInterfaceSlice(raw)
	if len(items) == 0 {
		if s, ok := openAISchemaRequiredString(raw); ok {
			return []interface{}{s}
		}
		return nil
	}
	out := make([]interface{}, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		name, ok := openAISchemaRequiredString(item)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func openAISchemaRequiredString(raw interface{}) (string, bool) {
	switch v := raw.(type) {
	case string:
		s := strings.TrimSpace(v)
		return s, s != ""
	case []byte:
		s := strings.TrimSpace(string(v))
		return s, s != ""
	default:
		return "", false
	}
}

func sanitizeOpenAIToolSchemaProperties(v interface{}) interface{} {
	props := toStringInterfaceMap(v)
	if props == nil {
		return v
	}
	out := make(map[string]interface{}, len(props))
	for name, schema := range props {
		out[name] = sanitizeOpenAIToolSchemaShape(schema)
	}
	return out
}

func sanitizeOpenAIFunctionCallForSDK(raw interface{}) map[string]interface{} {
	mm := toStringInterfaceMap(raw)
	if mm == nil {
		return nil
	}
	name := strings.TrimSpace(stringValue(mm["name"]))
	if name == "" {
		return nil
	}
	args := normalizeOpenAIToolArgumentsString(mm["arguments"])
	return map[string]interface{}{"name": name, "arguments": args}
}

func sanitizeOpenAIResponseFormatForSDK(raw interface{}) map[string]interface{} {
	mm := toStringInterfaceMap(raw)
	if mm == nil {
		return nil
	}
	typ := strings.TrimSpace(stringValue(mm["type"]))
	switch typ {
	case "text", "json_object":
		return map[string]interface{}{"type": typ}
	case "json_schema":
		js := toStringInterfaceMap(mm["json_schema"])
		if js == nil {
			return nil
		}
		name := strings.TrimSpace(stringValue(js["name"]))
		schema, hasSchema := js["schema"]
		if name == "" || !hasSchema {
			return nil
		}
		clean := map[string]interface{}{"name": name, "schema": schema}
		if desc := stringValue(js["description"]); desc != "" {
			clean["description"] = desc
		}
		if strict, ok := js["strict"].(bool); ok {
			clean["strict"] = strict
		}
		return map[string]interface{}{"type": "json_schema", "json_schema": clean}
	default:
		return nil
	}
}

func sanitizeOpenAIResponseFormatForProvider(cfg corelib.MaclawLLMConfig, raw interface{}) map[string]interface{} {
	if corelib.IsDeepSeekFlashOpenAICompat(cfg) {
		mm := toStringInterfaceMap(raw)
		if mm == nil {
			return nil
		}
		if strings.TrimSpace(stringValue(mm["type"])) == "json_schema" {
			return map[string]interface{}{"type": "json_object"}
		}
	}
	responseFormat := sanitizeOpenAIResponseFormatForSDK(raw)
	if responseFormat == nil {
		return nil
	}
	if !corelib.IsDeepSeekFlashOpenAICompat(cfg) {
		return responseFormat
	}
	if responseFormat["type"] == "json_schema" {
		return map[string]interface{}{"type": "json_object"}
	}
	return responseFormat
}

func ensureDeepSeekFlashJSONResponseInstruction(reqBody map[string]interface{}) {
	responseFormat := toStringInterfaceMap(reqBody["response_format"])
	if responseFormat == nil || responseFormat["type"] != "json_object" {
		return
	}
	messages := toInterfaceSlice(reqBody["messages"])
	if len(messages) == 0 || openAICompatMessagesMentionJSON(messages) {
		return
	}
	reqBody["messages"] = prependOrMergeOpenAICompatSystemMessage(messages, "Respond with a valid JSON object.")
}

func normalizeDeepSeekFlashUnsupportedOptions(reqBody map[string]interface{}) {
	if reqBody == nil {
		return
	}
	if n, ok := positiveIntValue(reqBody["n"]); ok && n > 1 {
		reqBody["n"] = 1
	}
}

func positiveIntValue(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, v > 0
	case int64:
		return int(v), v > 0
	case float64:
		if v == float64(int(v)) && v > 0 {
			return int(v), true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil && i > 0 {
			return int(i), true
		}
	}
	return 0, false
}

func openAICompatMessagesMentionJSON(messages []interface{}) bool {
	for _, message := range messages {
		if strings.Contains(strings.ToLower(stringifyOpenAIToolContent(messageContent(message))), "json") {
			return true
		}
		mm := toStringInterfaceMap(message)
		if mm == nil {
			continue
		}
		content, ok := textFromOpenAIContentBlocks(mm["content"])
		if ok && strings.Contains(strings.ToLower(content), "json") {
			return true
		}
	}
	return false
}

func prependOrMergeOpenAICompatSystemMessage(messages []interface{}, content string) []interface{} {
	if len(messages) == 0 {
		return []interface{}{map[string]interface{}{"role": "system", "content": content}}
	}
	if messageRole(messages[0]) == "system" {
		out := make([]interface{}, len(messages))
		copy(out, messages)
		out[0] = withMessageContent(out[0], joinOpenAICompatText(stringifyOpenAIToolContent(messageContent(out[0])), content))
		return out
	}
	out := make([]interface{}, 0, len(messages)+1)
	out = append(out, map[string]interface{}{"role": "system", "content": content})
	out = append(out, messages...)
	return out
}

func stringValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func normalizeOpenAIToolArgumentsString(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return "{}"
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || !json.Valid([]byte(trimmed)) {
			return "{}"
		}
		return v
	case []byte:
		trimmed := strings.TrimSpace(string(v))
		if trimmed == "" || !json.Valid([]byte(trimmed)) {
			return "{}"
		}
		return string(v)
	default:
		data, err := json.Marshal(v)
		if err != nil || !json.Valid(data) {
			return "{}"
		}
		return string(data)
	}
}

func sanitizeOpenAIChatRequestBodyForSDKCompatibility(reqBody map[string]interface{}) {
	if reqBody == nil {
		return
	}
	stream, _ := reqBody["stream"].(bool)
	if !stream {
		delete(reqBody, "stream_options")
	}
	if streamOptions, ok := reqBody["stream_options"]; ok {
		if sanitized := sanitizeOpenAIStreamOptionsForSDK(streamOptions); sanitized != nil {
			reqBody["stream_options"] = sanitized
		} else {
			delete(reqBody, "stream_options")
		}
	}
	if _, hasTools := reqBody["tools"]; !hasTools {
		delete(reqBody, "tool_choice")
	}
	if toolChoice, ok := reqBody["tool_choice"]; ok {
		if sanitized := sanitizeOpenAIToolChoiceForSDK(toolChoice); sanitized != nil {
			reqBody["tool_choice"] = sanitized
		} else {
			delete(reqBody, "tool_choice")
		}
	}
}

func sanitizeOpenAIStreamOptionsForSDK(raw interface{}) map[string]interface{} {
	mm := toStringInterfaceMap(raw)
	if mm == nil {
		return nil
	}
	out := map[string]interface{}{}
	if includeUsage, ok := mm["include_usage"].(bool); ok {
		out["include_usage"] = includeUsage
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sanitizeOpenAIToolChoiceForSDK(raw interface{}) interface{} {
	switch v := raw.(type) {
	case string:
		switch strings.TrimSpace(v) {
		case "none", "auto", "required":
			return strings.TrimSpace(v)
		default:
			return nil
		}
	case map[string]interface{}, map[string]string:
		mm := toStringInterfaceMap(v)
		if mm == nil {
			return nil
		}
		return sanitizeOpenAIToolChoiceMapForSDK(mm)
	default:
		mm := toStringInterfaceMap(raw)
		if mm == nil {
			return nil
		}
		return sanitizeOpenAIToolChoiceMapForSDK(mm)
	}
}

func sanitizeOpenAIToolChoiceMapForSDK(mm map[string]interface{}) interface{} {
	typ := strings.TrimSpace(stringValue(mm["type"]))
	if typ == "" {
		typ = "function"
	}
	if typ != "function" {
		return nil
	}
	fn := toStringInterfaceMap(mm["function"])
	if fn == nil {
		return nil
	}
	name := strings.TrimSpace(stringValue(fn["name"]))
	if name == "" {
		return nil
	}
	return map[string]interface{}{
		"type":     "function",
		"function": map[string]interface{}{"name": name},
	}
}

func sanitizeOpenAIToolChoiceForProvider(cfg corelib.MaclawLLMConfig, raw interface{}) interface{} {
	toolChoice := sanitizeOpenAIToolChoiceForSDK(raw)
	if toolChoice == nil {
		return nil
	}
	if !corelib.IsDeepSeekFlashOpenAICompat(cfg) {
		return toolChoice
	}
	if choice, ok := toolChoice.(string); ok {
		switch choice {
		case "none", "auto":
			return choice
		default:
			return "auto"
		}
	}
	return "auto"
}

func SanitizeConservativeOpenAICompatMessages(messages []interface{}) []interface{} {
	messages = sanitizeConservativeOpenAICompatMessages(messages)
	messages = sanitizeOpenAIChatMessagesForSDKCompatibility(messages, false)
	messages = sanitizeEmptyToolCalls(messages)
	messages = sanitizeInvalidToolCallArguments(messages)
	return sanitizeOrphanedToolCalls(messages, true)
}

func SanitizeOpenAICompatRequestMessages(messages []interface{}, stripTrailing bool) []interface{} {
	messages = normalizeOpenAIChatToolCallLinkage(messages)
	messages = sanitizeOpenAIChatMessagesForSDKCompatibility(messages, false)
	messages = sanitizeEmptyToolCalls(messages)
	messages = sanitizeInvalidToolCallArguments(messages)
	return sanitizeOrphanedToolCalls(messages, stripTrailing)
}

func sanitizeEmptyToolCalls(messages []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, message := range messages {
		if !hasEmptyToolCalls(message) {
			normalized = append(normalized, message)
			continue
		}
		normalized = append(normalized, copyMapWithout(message, "tool_calls"))
	}
	return normalized
}

func hasEmptyToolCalls(message interface{}) bool {
	m, ok := toMapInterface(message)
	if !ok {
		return false
	}
	raw, exists := m["tool_calls"]
	if !exists || raw == nil {
		return false
	}
	switch v := raw.(type) {
	case []interface{}:
		return len(v) == 0
	case []ToolCall:
		return len(v) == 0
	case []map[string]interface{}:
		return len(v) == 0
	default:
		data, err := json.Marshal(v)
		return err == nil && string(data) == "[]"
	}
}

func sanitizeInvalidToolCallArguments(messages []interface{}) []interface{} {
	normalized := make([]interface{}, 0, len(messages))
	for _, m := range messages {
		mm, ok := toMapInterface(m)
		if !ok {
			normalized = append(normalized, m)
			continue
		}
		role, _ := mm["role"].(string)
		if role != "assistant" {
			normalized = append(normalized, m)
			continue
		}
		patchedToolCalls, changed := sanitizeToolCallInvalidArguments(mm["tool_calls"])
		if !changed {
			normalized = append(normalized, m)
			continue
		}

		patched := make(map[string]interface{}, len(mm))
		for k, v := range mm {
			patched[k] = v
		}
		patched["tool_calls"] = patchedToolCalls
		normalized = append(normalized, patched)
	}
	return normalized
}

func sanitizeToolCallInvalidArguments(raw interface{}) (interface{}, bool) {
	switch toolCalls := raw.(type) {
	case []ToolCall:
		if len(toolCalls) == 0 {
			return raw, false
		}
		patchedCalls := make([]ToolCall, len(toolCalls))
		copy(patchedCalls, toolCalls)
		changed := false
		for i := range patchedCalls {
			args := strings.TrimSpace(patchedCalls[i].Function.Arguments)
			if args == "" || !json.Valid([]byte(args)) {
				patchedCalls[i].Function.Arguments = "{}"
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		return patchedCalls, true
	case []interface{}:
		return sanitizeToolCallInterfaceSliceInvalidArguments(toolCalls)
	case []map[string]interface{}:
		if len(toolCalls) == 0 {
			return raw, false
		}
		patchedCalls := make([]map[string]interface{}, len(toolCalls))
		changed := false
		for i, call := range toolCalls {
			patchedCall, patched := sanitizeToolCallMapInvalidArguments(call)
			patchedCalls[i] = patchedCall
			if patched {
				changed = true
			}
		}
		if !changed {
			return raw, false
		}
		return patchedCalls, true
	default:
		calls := toInterfaceSlice(raw)
		if len(calls) == 0 {
			return raw, false
		}
		patched, changed := sanitizeToolCallInterfaceSliceInvalidArguments(calls)
		if !changed {
			return raw, false
		}
		return patched, true
	}
}

func sanitizeToolCallInterfaceSliceInvalidArguments(toolCalls []interface{}) (interface{}, bool) {
	if len(toolCalls) == 0 {
		return toolCalls, false
	}
	patchedCalls := make([]interface{}, 0, len(toolCalls))
	changed := false
	for _, call := range toolCalls {
		callMap := toStringInterfaceMap(call)
		if callMap == nil {
			patchedCalls = append(patchedCalls, call)
			continue
		}
		patchedCall, patched := sanitizeToolCallMapInvalidArguments(callMap)
		if !patched {
			patchedCalls = append(patchedCalls, call)
			continue
		}
		patchedCalls = append(patchedCalls, patchedCall)
		changed = true
	}
	if !changed {
		return toolCalls, false
	}
	return patchedCalls, true
}

func sanitizeToolCallMapInvalidArguments(callMap map[string]interface{}) (map[string]interface{}, bool) {
	fn := toStringInterfaceMap(callMap["function"])
	if fn == nil {
		return callMap, false
	}
	normalizedArgs := normalizeOpenAIToolArgumentsString(fn["arguments"])
	if existing, ok := fn["arguments"].(string); ok && existing == normalizedArgs {
		return callMap, false
	}
	patchedCall := make(map[string]interface{}, len(callMap))
	for k, v := range callMap {
		patchedCall[k] = v
	}
	patchedFn := make(map[string]interface{}, len(fn))
	for k, v := range fn {
		patchedFn[k] = v
	}
	patchedFn["arguments"] = normalizedArgs
	patchedCall["function"] = patchedFn
	return patchedCall, true
}

// sanitizeOrphanedToolCalls detects assistant messages with tool_calls that are
// not followed by a complete set of matching tool result messages. It strips
// the orphaned tool_calls field and drops orphaned role:"tool" messages. This
// prevents HTTP 400 errors from providers that strictly enforce:
//
//	"An assistant message with 'tool_calls' must be followed by tool messages
//	 responding to each 'tool_call_id'."
//
// This is a compat/migration layer for persisted conversation histories that
// were corrupted by a bug in trimHistoryWithSummary (missing group-align on
// the second-pass recentStart calculation). The root cause is fixed in
// gui/im_conversation_trim.go  - new compactions will not produce orphans.
// This function handles pre-existing corrupted data gracefully.
func sanitizeOrphanedToolCalls(messages []interface{}, stripTrailing bool) []interface{} {
	if len(messages) == 0 {
		return messages
	}

	stripToolCalls := make(map[int]bool)
	validToolMessages := make(map[int]bool)
	dropToolMessages := make(map[int]bool)

	for i, m := range messages {
		if !msgIsAssistantWithToolCalls(m) {
			continue
		}
		tcIDs := extractToolCallIDs(m)
		if len(tcIDs) == 0 {
			continue
		}

		idSet := make(map[string]bool, len(tcIDs))
		for _, id := range tcIDs {
			idSet[id] = true
		}

		j := i + 1
		for j < len(messages) {
			mm, ok := toMapInterface(messages[j])
			if !ok {
				break
			}
			if role, _ := mm["role"].(string); role != "tool" {
				break
			}
			j++
		}
		if j >= len(messages) && !stripTrailing && j == i+1 {
			continue
		}

		foundIDs := make(map[string]bool)
		hasExtraToolMessage := false
		for k := i + 1; k < j; k++ {
			mm, ok := toMapInterface(messages[k])
			if !ok {
				break
			}
			if tcID, _ := mm["tool_call_id"].(string); tcID != "" {
				if idSet[tcID] {
					foundIDs[tcID] = true
				} else {
					hasExtraToolMessage = true
				}
			} else {
				hasExtraToolMessage = true
			}
		}
		complete := !hasExtraToolMessage && len(foundIDs) == len(idSet)
		for _, id := range tcIDs {
			if !foundIDs[id] {
				complete = false
				break
			}
		}
		if complete {
			for k := i + 1; k < j; k++ {
				validToolMessages[k] = true
			}
			continue
		}
		stripToolCalls[i] = true
		for k := i + 1; k < j; k++ {
			dropToolMessages[k] = true
		}
	}

	for i, m := range messages {
		if !isToolRoleMessage(m) {
			continue
		}
		if !validToolMessages[i] {
			dropToolMessages[i] = true
		}
	}

	if len(stripToolCalls) == 0 && len(dropToolMessages) == 0 {
		return messages
	}

	result := make([]interface{}, 0, len(messages))
	for i, m := range messages {
		if dropToolMessages[i] {
			log.Printf("[LLM sanitize] dropping orphaned tool message at index %d", i)
			continue
		}
		if !stripToolCalls[i] {
			result = append(result, m)
			continue
		}
		log.Printf("[LLM sanitize] stripping orphaned tool_calls from assistant message at index %d (ids=%v)", i, extractToolCallIDs(m))
		patched := copyMapWithout(m, "tool_calls")
		result = append(result, patched)
	}
	return result
}

func isToolRoleMessage(m interface{}) bool {
	mm, ok := toMapInterface(m)
	if !ok {
		return false
	}
	role, _ := mm["role"].(string)
	return role == "tool"
}

// msgIsAssistantWithToolCalls checks if a message is an assistant message
// with a non-empty tool_calls field.
func msgIsAssistantWithToolCalls(m interface{}) bool {
	mm, ok := toMapInterface(m)
	if !ok {
		return false
	}
	role, _ := mm["role"].(string)
	if role != "assistant" {
		return false
	}
	tc := mm["tool_calls"]
	if tc == nil {
		return false
	}
	switch v := tc.(type) {
	case []interface{}:
		return len(v) > 0
	case []ToolCall:
		return len(v) > 0
	default:
		// For other slice types, marshal to check.
		data, err := json.Marshal(v)
		if err != nil {
			return false
		}
		return len(data) > 2 // "[]" is 2 bytes
	}
}

// extractToolCallIDs extracts tool_call IDs from an assistant message.
func extractToolCallIDs(m interface{}) []string {
	mm, ok := toMapInterface(m)
	if !ok {
		return nil
	}
	tc := mm["tool_calls"]
	if tc == nil {
		return nil
	}
	switch v := tc.(type) {
	case []ToolCall:
		ids := make([]string, 0, len(v))
		for _, call := range v {
			if call.ID != "" {
				ids = append(ids, call.ID)
			}
		}
		return ids
	case []interface{}:
		ids := make([]string, 0, len(v))
		for _, call := range v {
			callMap, ok := call.(map[string]interface{})
			if !ok {
				continue
			}
			if id, _ := callMap["id"].(string); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	case []map[string]interface{}:
		ids := make([]string, 0, len(v))
		for _, callMap := range v {
			if id := strings.TrimSpace(stringValue(callMap["id"])); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	default:
		items := toInterfaceSlice(v)
		if len(items) == 0 {
			return nil
		}
		ids := make([]string, 0, len(items))
		for _, call := range items {
			callMap := toStringInterfaceMap(call)
			if callMap == nil {
				continue
			}
			if id := strings.TrimSpace(stringValue(callMap["id"])); id != "" {
				ids = append(ids, id)
			}
		}
		return ids
	}
}

// toMapInterface converts a message to map[string]interface{} if possible.
func toMapInterface(m interface{}) (map[string]interface{}, bool) {
	if mm := toStringInterfaceMap(m); mm != nil {
		return mm, true
	}
	return nil, false
}

// copyMapWithout creates a shallow copy of a message map, excluding the
// specified key.
func copyMapWithout(m interface{}, excludeKey string) interface{} {
	switch v := m.(type) {
	case map[string]interface{}:
		patched := make(map[string]interface{}, len(v))
		for k, val := range v {
			if k == excludeKey {
				continue
			}
			patched[k] = val
		}
		return patched
	case map[string]string:
		patched := make(map[string]string, len(v))
		for k, val := range v {
			if k == excludeKey {
				continue
			}
			patched[k] = val
		}
		return patched
	default:
		mm := toStringInterfaceMap(m)
		if mm != nil {
			patched := make(map[string]interface{}, len(mm))
			for k, val := range mm {
				if k == excludeKey {
					continue
				}
				patched[k] = val
			}
			return patched
		}
		return m
	}
}

// DoOpenAIRequest sends a non-streaming OpenAI-compatible chat completion
// request. It handles provider quirks (e.g. MiniMax system-role merge)
// in one place so callers don't need to worry about them.
//
// The caller provides a context for cancellation/timeout control.
// tools may be nil for simple requests without tool calling.
func DoOpenAIRequest(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, error) {
	result, _, err := DoOpenAIRequestRaw(ctx, cfg, messages, tools, client)
	return result, err
}

func DoOpenAIRequestRaw(
	ctx context.Context,
	cfg corelib.MaclawLLMConfig,
	messages []interface{},
	tools []map[string]interface{},
	client *http.Client,
) (*Response, []byte, error) {
	endpoint, reqBody, err := BuildOpenAIChatRequestData(cfg, messages, OpenAIChatRequestOptions{
		Stream: false,
		Tools:  tools,
	})
	if err != nil {
		return nil, nil, err
	}
	traceFields := RequestTraceLogFields(ctx)
	upstreamModel := cfg.UpstreamModel()
	log.Printf("[LLM] POST %s model=%s configured_model=%s protocol=%s %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, traceFields)

	startedAt := time.Now()
	body, statusCode, err := openAISDKChatRaw(ctx, cfg, reqBody, client)
	if err != nil {
		log.Printf("[LLM] done %s model=%s configured_model=%s protocol=%s status=error http_status=%d elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, statusCode, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		if statusCode == 0 {
			return nil, nil, fmt.Errorf("[%s] %w", endpoint, err)
		}
	}
	requestSummary := ""
	if statusCode != http.StatusOK {
		requestSummary = " request=" + SummarizeOpenAIChatRequestBody(reqBody)
	}
	log.Printf("[LLM] done %s model=%s configured_model=%s protocol=%s status=%d elapsed=%s body_len=%d%s %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, statusCode, time.Since(startedAt).Round(time.Millisecond), len(body), requestSummary, traceFields)
	if statusCode != http.StatusOK {
		compactRetryAttempted := false
		if ShouldRetryOpenAIWithoutTools(cfg, statusCode, messages, tools) {
			log.Printf("[LLM] retry_without_tools %s model=%s configured_model=%s reason=conservative_openai_compat_400 tools=%d %s", endpoint, upstreamModel, cfg.Model, len(tools), traceFields)
			endpoint, reqBody, err = BuildOpenAIChatRequestData(cfg, messages, OpenAIChatRequestOptions{Stream: false})
			if err != nil {
				return nil, body, err
			}
			retryStartedAt := time.Now()
			body, statusCode, err = openAISDKChatRaw(ctx, cfg, reqBody, client)
			if err != nil {
				log.Printf("[LLM] retry_without_tools done %s model=%s configured_model=%s status=error http_status=%d elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(retryStartedAt).Round(time.Millisecond), err, traceFields)
				if statusCode == 0 {
					return nil, body, fmt.Errorf("[%s] %w", endpoint, err)
				}
			}
			requestSummary = ""
			if statusCode != http.StatusOK {
				requestSummary = " request=" + SummarizeOpenAIChatRequestBody(reqBody)
			}
			log.Printf("[LLM] retry_without_tools done %s model=%s configured_model=%s protocol=%s status=%d elapsed=%s body_len=%d%s %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, statusCode, time.Since(retryStartedAt).Round(time.Millisecond), len(body), requestSummary, traceFields)
			if statusCode == http.StatusOK {
				result, err := ParseNonStreamOpenAIResponseBody(body)
				if err != nil {
					return nil, body, err
				}
				return result, body, nil
			}
			if compactMessages := CompactOpenAICompatMessagesForToollessRetry(cfg, messages); len(compactMessages) > 0 {
				compactRetryAttempted = true
				log.Printf("[LLM] retry_compact_without_tools %s model=%s configured_model=%s reason=conservative_openai_compat_400 %s", endpoint, upstreamModel, cfg.Model, traceFields)
				endpoint, reqBody, err = BuildOpenAIChatRequestData(cfg, compactMessages, OpenAIChatRequestOptions{Stream: false})
				if err != nil {
					return nil, body, err
				}
				compactStartedAt := time.Now()
				body, statusCode, err = openAISDKChatRaw(ctx, cfg, reqBody, client)
				if err != nil {
					log.Printf("[LLM] retry_compact_without_tools done %s model=%s configured_model=%s status=error http_status=%d elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(compactStartedAt).Round(time.Millisecond), err, traceFields)
					if statusCode == 0 {
						return nil, body, fmt.Errorf("[%s] %w", endpoint, err)
					}
				}
				requestSummary = ""
				if statusCode != http.StatusOK {
					requestSummary = " request=" + SummarizeOpenAIChatRequestBody(reqBody)
				}
				log.Printf("[LLM] retry_compact_without_tools done %s model=%s configured_model=%s protocol=%s status=%d elapsed=%s body_len=%d%s %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, statusCode, time.Since(compactStartedAt).Round(time.Millisecond), len(body), requestSummary, traceFields)
				if statusCode == http.StatusOK {
					result, err := ParseNonStreamOpenAIResponseBody(body)
					if err != nil {
						return nil, body, err
					}
					return result, body, nil
				}
			}
		}
		if !compactRetryAttempted && ShouldRetryOpenAIWithCompact(cfg, statusCode, messages) {
			if compactMessages := CompactOpenAICompatMessagesForToollessRetry(cfg, messages); len(compactMessages) > 0 {
				log.Printf("[LLM] retry_compact_without_tools %s model=%s configured_model=%s reason=conservative_openai_compat_400_direct %s", endpoint, upstreamModel, cfg.Model, traceFields)
				endpoint, reqBody, err = BuildOpenAIChatRequestData(cfg, compactMessages, OpenAIChatRequestOptions{Stream: false})
				if err != nil {
					return nil, body, err
				}
				compactStartedAt := time.Now()
				body, statusCode, err = openAISDKChatRaw(ctx, cfg, reqBody, client)
				if err != nil {
					log.Printf("[LLM] retry_compact_without_tools done %s model=%s configured_model=%s status=error http_status=%d elapsed=%s err=%v %s", endpoint, upstreamModel, cfg.Model, statusCode, time.Since(compactStartedAt).Round(time.Millisecond), err, traceFields)
					if statusCode == 0 {
						return nil, body, fmt.Errorf("[%s] %w", endpoint, err)
					}
				}
				requestSummary = ""
				if statusCode != http.StatusOK {
					requestSummary = " request=" + SummarizeOpenAIChatRequestBody(reqBody)
				}
				log.Printf("[LLM] retry_compact_without_tools done %s model=%s configured_model=%s protocol=%s status=%d elapsed=%s body_len=%d%s %s", endpoint, upstreamModel, cfg.Model, cfg.Protocol, statusCode, time.Since(compactStartedAt).Round(time.Millisecond), len(body), requestSummary, traceFields)
				if statusCode == http.StatusOK {
					result, err := ParseNonStreamOpenAIResponseBody(body)
					if err != nil {
						return nil, body, err
					}
					return result, body, nil
				}
			}
		}
		return nil, body, &HTTPStatusError{StatusCode: statusCode, Body: body}
	}

	result, err := ParseNonStreamOpenAIResponseBody(body)
	if err != nil {
		return nil, body, err
	}
	return result, body, nil
}

func ShouldRetryOpenAIWithoutTools(cfg corelib.MaclawLLMConfig, statusCode int, messages []interface{}, tools []map[string]interface{}) bool {
	return statusCode == http.StatusBadRequest &&
		len(tools) > 0 &&
		cfg.NeedsConservativeOpenAICompatSanitization() &&
		!hasOpenAIToolInteractionMessages(messages)
}

func ShouldRetryOpenAIWithCompact(cfg corelib.MaclawLLMConfig, statusCode int, messages []interface{}) bool {
	return statusCode == http.StatusBadRequest &&
		cfg.NeedsConservativeOpenAICompatSanitization() &&
		len(CompactOpenAICompatMessagesForToollessRetry(cfg, messages)) > 0
}

func hasOpenAIToolInteractionMessages(messages []interface{}) bool {
	for _, message := range messages {
		m, ok := toMapInterface(message)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		if role == "tool" || role == "function" {
			return true
		}
		if _, ok := m["tool_calls"]; ok {
			return msgIsAssistantWithToolCalls(m)
		}
		if raw, ok := m["function_call"]; ok && !isEmptyFunctionCall(raw) {
			return true
		}
	}
	return false
}

func isEmptyFunctionCall(raw interface{}) bool {
	if raw == nil {
		return true
	}
	switch v := raw.(type) {
	case map[string]interface{}:
		return len(v) == 0
	case map[string]string:
		return len(v) == 0
	default:
		data, err := json.Marshal(raw)
		if err == nil && string(data) == "{}" {
			return true
		}
		mm := toStringInterfaceMap(raw)
		if mm == nil {
			return false
		}
		if len(mm) == 0 {
			return true
		}
		name := strings.TrimSpace(stringValue(mm["name"]))
		args := strings.TrimSpace(stringValue(mm["arguments"]))
		return name == "" && (args == "" || args == "{}")
	}
}

// sseChunk represents a single SSE chunk from an OpenAI-compatible streaming response.
type sseChunk struct {
	Choices []struct {
		Delta struct {
			Content          string                `json:"content"`
			ReasoningContent string                `json:"reasoning_content"`
			ToolCalls        []sseToolCallDelta    `json:"tool_calls"`
			FunctionCall     *sseFunctionCallDelta `json:"function_call,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
}

// sseToolCallDelta represents a tool call fragment in an SSE delta.
type sseToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type sseFunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ParseSSEToResponse parses an SSE-formatted response body (lines prefixed
// with "data: ") into a single *Response by accumulating all chunks.
// This handles the case where an API gateway returns streaming SSE format
// even though the request did not ask for streaming.
func ParseSSEToResponse(body []byte) (*Response, error) {
	var contentBuf, reasoningBuf strings.Builder
	var finishReason string
	var usage *Usage

	// toolCalls accumulated by index
	type toolCallAcc struct {
		ID      string
		Type    string
		Name    string
		ArgsBuf strings.Builder
	}
	toolCalls := make(map[int]*toolCallAcc)
	legacyFunctionCall := &toolCallAcc{Type: "function"}
	legacyFunctionCallSeen := false

	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var chunk sseChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		contentBuf.WriteString(delta.Content)
		reasoningBuf.WriteString(delta.ReasoningContent)

		if chunk.Choices[0].FinishReason != "" {
			finishReason = chunk.Choices[0].FinishReason
			if finishReason == "function_call" {
				finishReason = "tool_calls"
			}
		}

		if delta.FunctionCall != nil {
			legacyFunctionCallSeen = true
			if delta.FunctionCall.Name != "" {
				if legacyFunctionCall.Name == "" {
					legacyFunctionCall.Name = delta.FunctionCall.Name
				} else {
					legacyFunctionCall.Name += delta.FunctionCall.Name
				}
			}
			if delta.FunctionCall.Arguments != "" {
				legacyFunctionCall.ArgsBuf.WriteString(delta.FunctionCall.Arguments)
				if legacyFunctionCall.ArgsBuf.Len() > maxToolArgumentsBytes {
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", legacyFunctionCall.Name, legacyFunctionCall.ArgsBuf.Len(), maxToolArgumentsBytes)
				}
			}
		}

		// Accumulate tool calls by index
		for _, tc := range delta.ToolCalls {
			acc, ok := toolCalls[tc.Index]
			if !ok {
				acc = &toolCallAcc{}
				toolCalls[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.ID = tc.ID
			}
			if tc.Type != "" {
				acc.Type = tc.Type
			}
			if tc.Function.Name != "" {
				acc.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.ArgsBuf.WriteString(tc.Function.Arguments)
				if acc.ArgsBuf.Len() > maxToolArgumentsBytes {
					return nil, fmt.Errorf("tool arguments too large for %s: %d bytes exceeds limit %d", acc.Name, acc.ArgsBuf.Len(), maxToolArgumentsBytes)
				}
			}
		}

	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("SSE stream read error: %w", err)
	}

	msg := Message{
		Role:             "assistant",
		Content:          StripAllExtra(contentBuf.String()),
		ReasoningContent: reasoningBuf.String(),
	}

	// Assemble tool calls in index order
	if len(toolCalls) > 0 {
		// Find max index
		maxIdx := 0
		for idx := range toolCalls {
			if idx > maxIdx {
				maxIdx = idx
			}
		}
		for i := 0; i <= maxIdx; i++ {
			acc, ok := toolCalls[i]
			if !ok {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   acc.ID,
				Type: normalizeToolCallType(acc.Type),
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      acc.Name,
					Arguments: acc.ArgsBuf.String(),
				},
			})
		}
	}
	if len(toolCalls) == 0 && legacyFunctionCallSeen {
		if call, ok := normalizePlainContentToolCallWithID("", legacyFunctionCall.Type, legacyFunctionCall.Name, json.RawMessage(legacyFunctionCall.ArgsBuf.String())); ok {
			msg.ToolCalls = append(msg.ToolCalls, call)
			msg.Content = ""
			finishReason = "tool_calls"
		}
	}
	if len(msg.ToolCalls) == 0 {
		rawContent := contentBuf.String()
		if contentCalls, malformed := ParseContentToolCallsDetailed(rawContent); len(contentCalls) > 0 {
			msg.ToolCalls = append(msg.ToolCalls, contentCalls...)
			msg.Content = ""
			finishReason = "tool_calls"
		} else if malformed {
			msg.Content = MalformedContentToolCallErrorMsg
			finishReason = "stop"
		}
	}

	return &Response{
		Choices: []Choice{{
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: usage,
	}, nil
}
