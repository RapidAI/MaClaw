package llm

// Native Gemini generateContent / streamGenerateContent.
// The OpenAI-compatible AI Studio route stays on /openai and is not this wire.
// This is an API key (x-goog-api-key), not a Google AI Pro subscription login.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	googleDefaultBase        = "https://generativelanguage.googleapis.com/v1beta"
	googleErrorBodyLimit     = 1 << 20
	googleSuccessBodyLimit   = 8 << 20
	googleStreamScanBytes    = 4 << 20
	googleModelListPageLimit = 5
	// Direct REST generateContent does not send continuationToken. A non-streaming
	// call above 32768 is HTTP 400. A streaming call above 65536 is long decode,
	// which needs another hop this client does not run. 65536 itself is the
	// stream hop and stays.
	googleNonStreamHopMaxOutputTokens = 32768
	googleStreamHopMaxOutputTokens    = 65536
)

// GoogleGenerateRequestOptions is the native generateContent request.
// A nil ToolChoice leaves the provider default. ParallelToolCalls is refused:
// the field is not a generateContent property, and dropping it would make the
// tool-surface receipt authorize a different policy than the model sees.
type GoogleGenerateRequestOptions struct {
	Stream                  bool
	Tools                   []map[string]interface{}
	ExplicitToolReplacement bool
	ToolChoice              interface{}
	ParallelToolCalls       *bool
	ResponseFormat          interface{}
	MaxOutputTokens         int
}

// GoogleModel is one generateContent model from the native models.list.
type GoogleModel struct {
	ID          string
	DisplayName string
}

// GoogleGenerateContentBase normalizes a provider URL to the generateContent
// root. A mistaken OpenAI /openai or :generateContent suffix is removed.
// A path with no v1, v1beta, v1alpha, or v<digits> segment gets /v1beta.
func GoogleGenerateContentBase(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return googleDefaultBase
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return googleDefaultBase
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = googleGenerateContentPath(parsed.Path)
	return strings.TrimSuffix(parsed.String(), "/")
}

func googleGenerateContentPath(path string) string {
	path = strings.TrimSuffix(path, "/")
	path = stripGoogleMethodSuffix(path)
	path = strings.TrimSuffix(path, "/")
	path = stripGoogleOpenAISuffix(path)
	path = strings.TrimSuffix(path, "/")
	path = stripGoogleModelsSuffix(path)
	path = strings.TrimSuffix(path, "/")
	if !googlePathHasVersion(path) {
		if path == "" {
			return "/v1beta"
		}
		return path + "/v1beta"
	}
	if path == "" {
		return "/v1beta"
	}
	return path
}

func stripGoogleMethodSuffix(path string) string {
	lower := strings.ToLower(path)
	for _, marker := range []string{":streamgeneratecontent", ":generatecontent"} {
		if i := strings.Index(lower, marker); i >= 0 {
			return path[:i]
		}
	}
	return path
}

func stripGoogleOpenAISuffix(path string) string {
	lower := strings.ToLower(path)
	for _, suffix := range []string{"/openai/chat/completions", "/chat/completions", "/openai"} {
		if strings.HasSuffix(lower, suffix) {
			return path[:len(path)-len(suffix)]
		}
	}
	return path
}

func stripGoogleModelsSuffix(path string) string {
	segs := strings.Split(path, "/")
	if len(segs) >= 2 && strings.EqualFold(segs[len(segs)-1], "models") {
		segs = segs[:len(segs)-1]
		return strings.Join(segs, "/")
	}
	if len(segs) >= 3 && strings.EqualFold(segs[len(segs)-2], "models") {
		segs = segs[:len(segs)-2]
		return strings.Join(segs, "/")
	}
	return path
}

func googlePathHasVersion(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		seg = strings.ToLower(strings.TrimSpace(seg))
		switch seg {
		case "v1", "v1beta", "v1alpha":
			return true
		}
		if len(seg) >= 2 && seg[0] == 'v' && googleAllDigits(seg[1:]) {
			return true
		}
	}
	return false
}

func googleAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func googleRequestModel(model string) string {
	model = strings.TrimSpace(model)
	model = strings.TrimPrefix(model, "models/")
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return strings.TrimSpace(model)
}

// GoogleGenerateContentEndpoint is the POST URL for one model.
func GoogleGenerateContentEndpoint(base, model string, stream bool) (string, error) {
	model = googleRequestModel(model)
	if model == "" {
		return "", fmt.Errorf("google model is empty")
	}
	root := GoogleGenerateContentBase(base)
	escaped := url.PathEscape(model)
	if stream {
		return root + "/models/" + escaped + ":streamGenerateContent?alt=sse", nil
	}
	return root + "/models/" + escaped + ":generateContent", nil
}

// BuildGoogleGenerateContentRequestBody converts OpenAI-style messages into a
// generateContent body. It does not write reasoning_effort or thinking.type.
func BuildGoogleGenerateContentRequestBody(cfg corelib.MaclawLLMConfig, messages []interface{}, opts GoogleGenerateRequestOptions) (map[string]interface{}, error) {
	if opts.ParallelToolCalls != nil {
		return nil, fmt.Errorf("google generateContent does not accept parallel_tool_calls")
	}
	// The parser numbers each response call_1, call_2, … so those ids repeat
	// across turns. Stamp the provider id onto each call before the shared
	// sanitizer drops google_function_call_id. A map keyed only by call_N
	// would attach a later turn's id to an earlier functionResponse.
	messages = preserveGoogleFunctionCallIDs(messages)
	toolNames := googleToolResultNameHints(messages)
	messages = SanitizeOpenAICompatRequestMessages(messages, true)
	systemText, contents, err := convertMessagesToGoogleContents(messages, toolNames)
	if err != nil {
		return nil, err
	}
	if len(contents) == 0 {
		return nil, fmt.Errorf("google generateContent requires at least one content turn")
	}
	body := map[string]interface{}{"contents": contents}
	if systemText != "" {
		body["systemInstruction"] = map[string]interface{}{
			"parts": []interface{}{map[string]interface{}{"text": systemText}},
		}
	}
	decls := googleFunctionDeclarations(opts.Tools)
	if len(decls) > 0 {
		body["tools"] = []interface{}{map[string]interface{}{"functionDeclarations": decls}}
	} else if opts.ExplicitToolReplacement {
		body["tools"] = []interface{}{}
	}
	toolConfig, err := googleToolConfig(opts.ToolChoice)
	if err != nil {
		return nil, err
	}
	if toolConfig != nil {
		body["toolConfig"] = toolConfig
	}
	maxTokens := opts.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = cfg.EffectiveMaxOutputTokens()
	}
	generation := map[string]interface{}{"maxOutputTokens": maxTokens}
	if cfg.Temperature != nil {
		generation["temperature"] = *cfg.Temperature
	}
	if err := applyGoogleResponseFormat(generation, opts.ResponseFormat); err != nil {
		return nil, err
	}
	corelib.ApplyGeminiGenerateContentThinking(cfg, generation)
	body["generationConfig"] = generation
	corelib.ApplyMemberUpstreamBody(body, cfg.MemberExtraBody, cfg.MemberExtraBodyPriority, cfg.MemberDropFields)
	// Member extra_body can replace generationConfig after the value above was chosen.
	// The hop cap is a property of the bytes we send, so it is applied last.
	clampGoogleGenerationMaxOutputTokens(body, opts.Stream)
	return body, nil
}

// clampGoogleGenerationMaxOutputTokens keeps maxOutputTokens inside one REST hop.
// This client never sends continuationToken, so a larger ceiling cannot be resumed:
// non-streaming REST rejects it, and streaming REST would end on CONTINUATION
// while the configured total still had room.
func clampGoogleGenerationMaxOutputTokens(body map[string]interface{}, stream bool) {
	generation := toStringInterfaceMap(body["generationConfig"])
	if generation == nil {
		return
	}
	raw, ok := generation["maxOutputTokens"]
	if !ok {
		return
	}
	number, ok := googleNumber(raw)
	if !ok {
		return
	}
	limit := googleNonStreamHopMaxOutputTokens
	if stream {
		limit = googleStreamHopMaxOutputTokens
	}
	if number > float64(limit) {
		generation["maxOutputTokens"] = limit
	}
}

// BuildGoogleGenerateContentRequestData returns the endpoint and JSON body.
func BuildGoogleGenerateContentRequestData(cfg corelib.MaclawLLMConfig, messages []interface{}, opts GoogleGenerateRequestOptions) (string, []byte, error) {
	endpoint, err := GoogleGenerateContentEndpoint(cfg.URL, cfg.UpstreamModel(), opts.Stream)
	if err != nil {
		return "", nil, err
	}
	payload, err := BuildGoogleGenerateContentRequestBody(cfg, messages, opts)
	if err != nil {
		return "", nil, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}
	return endpoint, data, nil
}

func googleToolResultNameHints(messages []interface{}) map[string]string {
	names := map[string]string{}
	known := map[string]string{}
	for _, message := range messages {
		mm := toStringInterfaceMap(message)
		if mm == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(stringField(mm, "role"))) {
		case "assistant", "model":
			for _, call := range googleToolCallsFromMessage(mm) {
				if call.ID != "" && call.Function.Name != "" {
					known[call.ID] = call.Function.Name
				}
			}
		case "tool", "function":
			id := strings.TrimSpace(stringField(mm, "tool_call_id"))
			name := strings.TrimSpace(stringField(mm, "name"))
			if id != "" && name != "" {
				names[id] = name
			}
		}
	}
	for id, name := range known {
		if names[id] == "" {
			names[id] = name
		}
	}
	return names
}

// preserveGoogleFunctionCallIDs copies assistant messages that carry a model
// functionCall.id and stores that id in extra_content.google.function_call_id.
// SanitizeOpenAICompatRequestMessages rebuilds tool calls as id/type/function
// plus thought_signature and extra_content. The top-level google_function_call_id
// field is dropped there on purpose: copying it in the shared sanitizer would
// put a Gemini field on strict OpenAI bodies. extra_content is the object that
// sanitizer already keeps. This copy is only the generateContent input. The
// caller's history is left unchanged, and the Gemini body does not send
// extra_content.
func preserveGoogleFunctionCallIDs(messages []interface{}) []interface{} {
	if len(messages) == 0 {
		return messages
	}
	out := make([]interface{}, len(messages))
	copy(out, messages)
	for i, message := range messages {
		mm := toStringInterfaceMap(message)
		if mm == nil {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(stringField(mm, "role")))
		if role != "assistant" && role != "model" {
			continue
		}
		rebuilt, ok := googleToolCallMapsWithProviderID(mm["tool_calls"])
		if !ok {
			continue
		}
		cloned := make(map[string]interface{}, len(mm))
		for key, value := range mm {
			cloned[key] = value
		}
		cloned["tool_calls"] = rebuilt
		out[i] = cloned
	}
	return out
}

func googleToolCallMapsWithProviderID(raw interface{}) ([]interface{}, bool) {
	if calls, ok := raw.([]ToolCall); ok {
		if !toolCallsHaveProviderID(calls) {
			return nil, false
		}
		out := make([]interface{}, 0, len(calls))
		for _, call := range calls {
			if strings.TrimSpace(call.Function.Name) == "" {
				continue
			}
			item := map[string]interface{}{
				"id":   call.ID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      call.Function.Name,
					"arguments": call.Function.Arguments,
				},
			}
			if sig := strings.TrimSpace(call.ThoughtSignature); sig != "" {
				item["thought_signature"] = sig
			}
			if id := strings.TrimSpace(call.GoogleFunctionCallID); id != "" {
				item["extra_content"] = map[string]interface{}{
					"google": map[string]interface{}{"function_call_id": id},
				}
			}
			out = append(out, item)
		}
		return out, len(out) > 0
	}
	items := toInterfaceSlice(raw)
	if len(items) == 0 {
		return nil, false
	}
	out := make([]interface{}, 0, len(items))
	anyProviderID := false
	for _, item := range items {
		call := toStringInterfaceMap(item)
		if call == nil {
			continue
		}
		name := ""
		args := ""
		if fn := toStringInterfaceMap(call["function"]); fn != nil {
			name = stringField(fn, "name")
			args = normalizeOpenAIToolArgumentsString(fn["arguments"])
		}
		if strings.TrimSpace(name) == "" {
			name = strings.TrimSpace(stringField(call, "name"))
		}
		if name == "" {
			continue
		}
		next := map[string]interface{}{
			"id":   stringField(call, "id"),
			"type": "function",
			"function": map[string]interface{}{
				"name":      name,
				"arguments": args,
			},
		}
		if sig := thoughtSignatureFromToolCallMap(call); sig != "" {
			next["thought_signature"] = sig
		}
		providerID := googleProviderFunctionCallID(call)
		if providerID != "" {
			anyProviderID = true
			next["extra_content"] = mergeGoogleProviderFunctionCallID(call["extra_content"], providerID)
		} else if extra := call["extra_content"]; extra != nil {
			next["extra_content"] = cloneToolDefinitionValue(extra)
		}
		out = append(out, next)
	}
	if !anyProviderID {
		return nil, false
	}
	return out, len(out) > 0
}

func toolCallsHaveProviderID(calls []ToolCall) bool {
	for _, call := range calls {
		if strings.TrimSpace(call.Function.Name) != "" && strings.TrimSpace(call.GoogleFunctionCallID) != "" {
			return true
		}
	}
	return false
}

func googleProviderFunctionCallID(call map[string]interface{}) string {
	if call == nil {
		return ""
	}
	if id := strings.TrimSpace(stringField(call, "google_function_call_id")); id != "" {
		return id
	}
	extra := toStringInterfaceMap(call["extra_content"])
	google := toStringInterfaceMap(extra["google"])
	return strings.TrimSpace(stringField(google, "function_call_id"))
}

func mergeGoogleProviderFunctionCallID(extra interface{}, providerID string) interface{} {
	root, _ := cloneToolDefinitionValue(extra).(map[string]interface{})
	if root == nil {
		root = map[string]interface{}{}
	}
	google, _ := root["google"].(map[string]interface{})
	if google == nil {
		google = map[string]interface{}{}
	} else {
		google, _ = cloneToolDefinitionValue(google).(map[string]interface{})
	}
	google["function_call_id"] = providerID
	root["google"] = google
	return root
}

func convertMessagesToGoogleContents(messages []interface{}, toolNames map[string]string) (string, []interface{}, error) {
	var system strings.Builder
	var contents []interface{}
	known := map[string]string{}
	for id, name := range toolNames {
		known[id] = name
	}
	// call_N is reused every model turn. Each occurrence keeps its own provider
	// id, and the matching functionResponse consumes the oldest one.
	pendingProviderIDs := map[string][]string{}
	for _, message := range messages {
		mm := toStringInterfaceMap(message)
		if mm == nil {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(stringField(mm, "role")))
		switch role {
		case "system", "developer":
			if text := strings.TrimSpace(extractContentString(mm)); text != "" {
				if system.Len() > 0 {
					system.WriteByte('\n')
				}
				system.WriteString(text)
			}
		case "assistant", "model":
			for _, call := range googleToolCallsFromMessage(mm) {
				if call.ID != "" && call.Function.Name != "" {
					known[call.ID] = call.Function.Name
				}
				if call.ID != "" {
					pendingProviderIDs[call.ID] = append(pendingProviderIDs[call.ID], strings.TrimSpace(call.GoogleFunctionCallID))
				}
			}
			appendGoogleContent(&contents, "model", googleModelParts(mm))
		case "tool", "function":
			id := strings.TrimSpace(stringField(mm, "tool_call_id"))
			name := strings.TrimSpace(stringField(mm, "name"))
			if name == "" {
				name = known[id]
			}
			if name == "" {
				return "", nil, fmt.Errorf("google function response %q has no function name", id)
			}
			response := map[string]interface{}{
				"name":     name,
				"response": googleFunctionResponseObject(stringifyOpenAIToolContent(mm["content"])),
			}
			if gid := shiftGoogleProviderID(pendingProviderIDs, id); gid != "" {
				response["id"] = gid
			}
			part := map[string]interface{}{"functionResponse": response}
			appendGoogleContent(&contents, "user", []interface{}{part})
		default:
			appendGoogleContent(&contents, "user", googlePartsFromUserContent(mm["content"]))
		}
	}
	return system.String(), contents, nil
}

func appendGoogleContent(contents *[]interface{}, role string, parts []interface{}) {
	if len(parts) == 0 {
		return
	}
	if n := len(*contents); n > 0 {
		if last, ok := (*contents)[n-1].(map[string]interface{}); ok && last["role"] == role {
			existing, _ := last["parts"].([]interface{})
			last["parts"] = append(existing, parts...)
			return
		}
	}
	*contents = append(*contents, map[string]interface{}{"role": role, "parts": parts})
}

func shiftGoogleProviderID(pending map[string][]string, localID string) string {
	if pending == nil || localID == "" {
		return ""
	}
	queue := pending[localID]
	if len(queue) == 0 {
		return ""
	}
	pending[localID] = queue[1:]
	return strings.TrimSpace(queue[0])
}

func googleModelParts(mm map[string]interface{}) []interface{} {
	var parts []interface{}
	if text := extractContentString(mm); text != "" {
		parts = append(parts, map[string]interface{}{"text": text})
	}
	for _, call := range googleToolCallsFromMessage(mm) {
		functionCall := map[string]interface{}{
			"name": call.Function.Name,
			"args": googleFunctionArgs(call.Function.Arguments),
		}
		if gid := strings.TrimSpace(call.GoogleFunctionCallID); gid != "" {
			functionCall["id"] = gid
		}
		part := map[string]interface{}{"functionCall": functionCall}
		if sig := strings.TrimSpace(call.ThoughtSignature); sig != "" {
			part["thoughtSignature"] = sig
		}
		parts = append(parts, part)
	}
	if sig := strings.TrimSpace(stringField(mm, "thought_signature")); sig != "" {
		attachGoogleMessageSignature(parts, sig)
	}
	return parts
}

// attachGoogleMessageSignature replays a text-part signature. A function call
// keeps the signature stored on that call. The message-level signature lands
// on the last text part, or on the last unsigned function call when the turn
// has no text.
func attachGoogleMessageSignature(parts []interface{}, sig string) {
	for i := len(parts) - 1; i >= 0; i-- {
		part, _ := parts[i].(map[string]interface{})
		if part == nil {
			continue
		}
		if _, ok := part["text"]; !ok {
			continue
		}
		part["thoughtSignature"] = sig
		return
	}
	for i := len(parts) - 1; i >= 0; i-- {
		part, _ := parts[i].(map[string]interface{})
		if part == nil {
			continue
		}
		if _, ok := part["functionCall"]; !ok {
			continue
		}
		if strings.TrimSpace(stringField(part, "thoughtSignature")) == "" {
			part["thoughtSignature"] = sig
			return
		}
	}
}

func googleToolCallsFromMessage(mm map[string]interface{}) []ToolCall {
	if mm == nil {
		return nil
	}
	if calls, ok := mm["tool_calls"].([]ToolCall); ok {
		return assignGoogleCallIDs(calls)
	}
	items := toInterfaceSlice(mm["tool_calls"])
	if len(items) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(items))
	for _, item := range items {
		call := toStringInterfaceMap(item)
		if call == nil {
			continue
		}
		tc := ToolCall{ID: stringField(call, "id"), Type: "function"}
		if fn := toStringInterfaceMap(call["function"]); fn != nil {
			tc.Function.Name = stringField(fn, "name")
			tc.Function.Arguments = normalizeOpenAIToolArgumentsString(fn["arguments"])
		}
		if tc.Function.Name == "" {
			tc.Function.Name = stringField(call, "name")
		}
		tc.ThoughtSignature = thoughtSignatureFromToolCallMap(call)
		tc.GoogleFunctionCallID = googleProviderFunctionCallID(call)
		if strings.TrimSpace(tc.Function.Name) == "" {
			continue
		}
		out = append(out, tc)
	}
	return assignGoogleCallIDs(out)
}

func assignGoogleCallIDs(calls []ToolCall) []ToolCall {
	out := make([]ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		if strings.TrimSpace(out[i].ID) == "" {
			out[i].ID = fmt.Sprintf("call_%d", i+1)
		}
	}
	return out
}

func googleFunctionArgs(raw string) map[string]interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]interface{}{}
	}
	var value interface{}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return map[string]interface{}{}
	}
	obj, ok := value.(map[string]interface{})
	if !ok || obj == nil {
		return map[string]interface{}{}
	}
	return obj
}

func googleFunctionResponseObject(content string) map[string]interface{} {
	content = strings.TrimSpace(content)
	if content == "" {
		return map[string]interface{}{"result": ""}
	}
	var value interface{}
	if json.Unmarshal([]byte(content), &value) != nil {
		return map[string]interface{}{"result": content}
	}
	if obj, ok := value.(map[string]interface{}); ok && obj != nil {
		return obj
	}
	return map[string]interface{}{"result": value}
}

func googlePartsFromUserContent(content interface{}) []interface{} {
	if content == nil {
		return nil
	}
	if text, ok := content.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []interface{}{map[string]interface{}{"text": text}}
	}
	items := toInterfaceSlice(content)
	if len(items) == 0 {
		if text := strings.TrimSpace(textFromResponsesContent(content)); text != "" {
			return []interface{}{map[string]interface{}{"text": text}}
		}
		return nil
	}
	var parts []interface{}
	for _, item := range items {
		if text, ok := item.(string); ok {
			if text != "" {
				parts = append(parts, map[string]interface{}{"text": text})
			}
			continue
		}
		block := toStringInterfaceMap(item)
		if block == nil {
			continue
		}
		switch strings.ToLower(stringField(block, "type")) {
		case "", "text", "input_text":
			if text := stringField(block, "text"); text != "" {
				parts = append(parts, map[string]interface{}{"text": text})
			}
		case "image_url":
			if part := googlePartFromImageBlock(block); part != nil {
				parts = append(parts, part)
			}
		case "image":
			if part := googlePartFromAnthropicImage(block); part != nil {
				parts = append(parts, part)
			}
		default:
			if text := stringField(block, "text"); text != "" {
				parts = append(parts, map[string]interface{}{"text": text})
			}
		}
	}
	return parts
}

func googlePartFromImageBlock(block map[string]interface{}) map[string]interface{} {
	return googlePartFromAnthropicSource(openAIImageURLToAnthropicSource(block))
}

func googlePartFromAnthropicImage(block map[string]interface{}) map[string]interface{} {
	return googlePartFromAnthropicSource(toStringInterfaceMap(block["source"]))
}

func googlePartFromAnthropicSource(source map[string]interface{}) map[string]interface{} {
	if source == nil {
		return nil
	}
	switch strings.ToLower(stringField(source, "type")) {
	case "base64":
		data := stringField(source, "data")
		if data == "" {
			return nil
		}
		mime := stringField(source, "media_type")
		if mime == "" {
			mime = "image/png"
		}
		return map[string]interface{}{
			"inlineData": map[string]interface{}{"mimeType": mime, "data": data},
		}
	case "url":
		raw := strings.TrimSpace(stringField(source, "url"))
		if raw == "" {
			return nil
		}
		return map[string]interface{}{
			"fileData": map[string]interface{}{"mimeType": mimeFromImageURL(raw), "fileUri": raw},
		}
	default:
		return nil
	}
}

func mimeFromImageURL(raw string) string {
	path := strings.ToLower(raw)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	switch {
	case strings.HasSuffix(path, ".jpg"), strings.HasSuffix(path, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(path, ".gif"):
		return "image/gif"
	case strings.HasSuffix(path, ".webp"):
		return "image/webp"
	case strings.HasSuffix(path, ".png"):
		return "image/png"
	default:
		return "image/png"
	}
}

func googleFunctionDeclarations(tools []map[string]interface{}) []interface{} {
	var decls []interface{}
	for _, tool := range tools {
		fn := toStringInterfaceMap(tool["function"])
		if fn == nil {
			fn = tool
		}
		name := strings.TrimSpace(stringField(fn, "name"))
		if name == "" {
			continue
		}
		decl := map[string]interface{}{"name": name}
		if desc := stringField(fn, "description"); desc != "" {
			decl["description"] = desc
		}
		params := fn["parameters"]
		if params == nil {
			params = fn["input_schema"]
		}
		if params == nil {
			params = tool["input_schema"]
		}
		if params != nil {
			// Gemini rejects additionalProperties and several JSON Schema
			// keywords. Use the same reduction the tool-surface receipt freezes,
			// so the digest still matches the body that leaves the host.
			decl["parameters"] = cloneToolDefinitionValue(corelib.SanitizeCodeGenOpenAIToolSchemaValue(params))
		}
		decls = append(decls, decl)
	}
	return decls
}

func googleToolConfig(choice interface{}) (map[string]interface{}, error) {
	if choice == nil {
		return nil, nil
	}
	switch value := choice.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "provider-default":
			return nil, nil
		case "auto":
			return googleFunctionCallingConfig("AUTO", nil), nil
		case "none":
			return googleFunctionCallingConfig("NONE", nil), nil
		case "required", "any":
			return googleFunctionCallingConfig("ANY", nil), nil
		default:
			return nil, fmt.Errorf("unsupported tool_choice %q", value)
		}
	default:
		decoded := toStringInterfaceMap(choice)
		if decoded == nil {
			return nil, fmt.Errorf("unsupported tool_choice")
		}
		name := ""
		if fn := toStringInterfaceMap(decoded["function"]); fn != nil {
			name = strings.TrimSpace(stringField(fn, "name"))
		}
		if name == "" {
			name = strings.TrimSpace(stringField(decoded, "name"))
		}
		if name == "" {
			return nil, fmt.Errorf("specific tool_choice requires a function name")
		}
		return googleFunctionCallingConfig("ANY", []string{name}), nil
	}
}

func googleFunctionCallingConfig(mode string, names []string) map[string]interface{} {
	calling := map[string]interface{}{"mode": mode}
	if len(names) > 0 {
		allowed := make([]interface{}, len(names))
		for i, name := range names {
			allowed[i] = name
		}
		calling["allowedFunctionNames"] = allowed
	}
	return map[string]interface{}{"functionCallingConfig": calling}
}

func applyGoogleResponseFormat(generation map[string]interface{}, format interface{}) error {
	if format == nil {
		return nil
	}
	decoded := toStringInterfaceMap(format)
	if decoded == nil {
		return fmt.Errorf("unsupported response_format")
	}
	switch strings.ToLower(strings.TrimSpace(stringField(decoded, "type"))) {
	case "":
		return nil
	case "json_object":
		generation["responseMimeType"] = "application/json"
		return nil
	case "json_schema":
		generation["responseMimeType"] = "application/json"
		schema := toStringInterfaceMap(decoded["json_schema"])
		if schema == nil {
			return fmt.Errorf("json_schema response_format requires a schema")
		}
		body := schema["schema"]
		if body == nil {
			body = schema
		}
		// responseSchema is the OpenAPI Schema proto. It has no additionalProperties
		// field, so the JSON Schema callers send (strict objects, anyOf) is rejected
		// as an unknown name. responseJsonSchema accepts that JSON Schema and must
		// not be set together with responseSchema. Do not run the tool-parameter
		// sanitizer here: it deletes additionalProperties, anyOf, and $ref, which
		// this field keeps. Function declarations stay on the sanitized parameters
		// proto and are a different field.
		generation["responseJsonSchema"] = cloneToolDefinitionValue(body)
		return nil
	default:
		return fmt.Errorf("unsupported response_format %q", stringField(decoded, "type"))
	}
}

func googleGenerateOptionsFromOpenAI(opts OpenAIChatRequestOptions) (GoogleGenerateRequestOptions, error) {
	if opts.ParallelToolCalls != nil {
		return GoogleGenerateRequestOptions{}, fmt.Errorf("google generateContent does not accept parallel_tool_calls")
	}
	return GoogleGenerateRequestOptions{
		Stream:                  opts.Stream,
		Tools:                   opts.Tools,
		ExplicitToolReplacement: opts.ExplicitToolReplacement,
		ToolChoice:              opts.ToolChoice,
		ResponseFormat:          opts.ResponseFormat,
	}, nil
}

// DoGoogleRequest sends one non-streaming generateContent request.
func DoGoogleRequest(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, client *http.Client) (*Response, error) {
	return DoGoogleRequestWithOptions(ctx, cfg, messages, client, GoogleGenerateRequestOptions{Tools: tools})
}

// DoGoogleRequestWithOptions retries a thinking-off refusal once. Callers that
// already retry that refusal must not call this and then retry again.
func DoGoogleRequestWithOptions(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, opts GoogleGenerateRequestOptions) (*Response, error) {
	resp, err := doGoogleRequestOnce(ctx, cfg, messages, client, opts)
	if next, ok := ConfigAfterThinkingRejection(cfg, err); ok {
		return doGoogleRequestOnce(ctx, next, messages, client, opts)
	}
	return resp, err
}

func doGoogleRequestOnce(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, opts GoogleGenerateRequestOptions) (*Response, error) {
	opts.Stream = false
	endpoint, data, err := BuildGoogleGenerateContentRequestData(cfg, messages, opts)
	if err != nil {
		return nil, err
	}
	traceFields := RequestTraceLogFields(ctx)
	log.Printf("[LLM] POST %s model=%s configured_model=%s protocol=google %s", endpoint, cfg.UpstreamModel(), cfg.Model, traceFields)
	startedAt := time.Now()
	body, status, err := googleHTTP(ctx, cfg, endpoint, data, client)
	if err != nil && status == 0 {
		log.Printf("[LLM] done %s model=%s protocol=google status=error elapsed=%s err=%v %s", endpoint, cfg.UpstreamModel(), time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	log.Printf("[LLM] done %s model=%s protocol=google status=%d elapsed=%s body_len=%d %s", endpoint, cfg.UpstreamModel(), status, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
	if se := newHTTPStatusError(status, body); se != nil {
		return nil, se
	}
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	return ParseGoogleGenerateContent(body)
}

func googleHTTP(ctx context.Context, cfg corelib.MaclawLLMConfig, endpoint string, data []byte, client *http.Client) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	setGoogleAPIHeaders(req, cfg)
	client = HTTPClientForRequestContext(ctx, client)
	if client == nil {
		client = SharedHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	limit := googleSuccessBodyLimit
	if resp.StatusCode != http.StatusOK {
		limit = googleErrorBodyLimit
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(limit)))
	if readErr != nil {
		return body, resp.StatusCode, readErr
	}
	return body, resp.StatusCode, nil
}

func setGoogleAPIHeaders(req *http.Request, cfg corelib.MaclawLLMConfig) {
	if req == nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(cfg.Key); key != "" {
		req.Header.Set("x-goog-api-key", key)
	}
	if ua := strings.TrimSpace(cfg.UserAgent()); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
}

// DoGoogleRequestStream sends a streaming generateContent request.
// It does not retry a thinking-off refusal. The desktop and agent loops own
// that retry, or fall back to the non-stream helper which retries once.
func DoGoogleRequestStream(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, client *http.Client, onToken TokenCallback) (*Response, error) {
	return DoGoogleRequestStreamWithReasoning(ctx, cfg, messages, tools, client, onToken, nil)
}

// DoGoogleRequestStreamWithReasoning streams text and thought deltas.
// An empty tool list is sent explicitly, matching the Anthropic stream path.
func DoGoogleRequestStreamWithReasoning(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, client *http.Client, onToken TokenCallback, onReasoning TokenCallback) (*Response, error) {
	return DoGoogleRequestStreamWithOptions(ctx, cfg, messages, client, onToken, onReasoning, GoogleGenerateRequestOptions{
		Stream:                  true,
		Tools:                   tools,
		ExplicitToolReplacement: true,
	})
}

// DoGoogleRequestStreamWithOptions streams one generateContent call.
func DoGoogleRequestStreamWithOptions(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client, onToken TokenCallback, onReasoning TokenCallback, opts GoogleGenerateRequestOptions) (*Response, error) {
	opts.Stream = true
	endpoint, data, err := BuildGoogleGenerateContentRequestData(cfg, messages, opts)
	if err != nil {
		return nil, err
	}
	traceFields := RequestTraceLogFields(ctx)
	log.Printf("[LLM-stream] POST %s model=%s configured_model=%s protocol=google %s", endpoint, cfg.UpstreamModel(), cfg.Model, traceFields)
	startedAt := time.Now()
	reqCtx, cancelReq, markHeaders, headerDeadline := streamingRequestContext(ctx)
	defer cancelReq()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	setGoogleAPIHeaders(req, cfg)
	streamClient := httpClientWithStreamBodyDeadline(HTTPClientForRequestContext(ctx, client), markHeaders)
	resp, err := streamClient.Do(req)
	if err != nil {
		err = normalizeStreamSetupError(headerDeadline, err)
		log.Printf("[LLM-stream] done %s model=%s protocol=google status=error elapsed=%s err=%v %s", endpoint, cfg.UpstreamModel(), time.Since(startedAt).Round(time.Millisecond), err, traceFields)
		return nil, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, googleErrorBodyLimit))
		log.Printf("[LLM-stream] done %s model=%s protocol=google status=%d elapsed=%s body_len=%d %s", endpoint, cfg.UpstreamModel(), resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), len(body), traceFields)
		if se := streamHTTPStatusError(resp.StatusCode, body); se != nil {
			return nil, se
		}
		return nil, fmt.Errorf("[%s] HTTP %d", endpoint, resp.StatusCode)
	}
	parsed, err := parseGoogleGenerateContentStream(resp.Body, onToken, onReasoning)
	log.Printf("[LLM-stream] done %s model=%s protocol=google status=%d elapsed=%s err=%v %s", endpoint, cfg.UpstreamModel(), resp.StatusCode, time.Since(startedAt).Round(time.Millisecond), err, traceFields)
	if err != nil {
		return parsed, fmt.Errorf("[%s] %w", endpoint, err)
	}
	return parsed, nil
}

// ParseGoogleGenerateContent reads one non-streaming generateContent JSON body.
func ParseGoogleGenerateContent(body []byte) (*Response, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse google response: %w", err)
	}
	state := &googleStreamState{seen: map[string]struct{}{}}
	state.addChunk(raw, nil, nil)
	return state.response(), nil
}

func parseGoogleGenerateContentStream(r io.Reader, onToken TokenCallback, onReasoning TokenCallback) (*Response, error) {
	br := bufio.NewReader(r)
	var skipped []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			if err == io.EOF {
				return &Response{Choices: []Choice{{Message: Message{Role: "assistant"}, FinishReason: "stop"}}}, nil
			}
			return nil, err
		}
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			skipped = append(skipped, b)
			continue
		}
		if err := br.UnreadByte(); err != nil {
			return nil, err
		}
		if b == '{' {
			rest, err := io.ReadAll(io.LimitReader(br, googleSuccessBodyLimit))
			if err != nil {
				return nil, err
			}
			parsed, err := ParseGoogleGenerateContent(append(skipped, rest...))
			if err != nil {
				return nil, err
			}
			if parsed != nil && len(parsed.Choices) > 0 {
				if onToken != nil && parsed.Choices[0].Message.Content != "" {
					onToken(parsed.Choices[0].Message.Content)
				}
				if onReasoning != nil && parsed.Choices[0].Message.ReasoningContent != "" {
					onReasoning(parsed.Choices[0].Message.ReasoningContent)
				}
			}
			return parsed, nil
		}
		break
	}
	scanner := bufio.NewScanner(br)
	scanner.Buffer(make([]byte, 64*1024), googleStreamScanBytes)
	state := &googleStreamState{seen: map[string]struct{}{}}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(strings.ToLower(line), "event:") {
			continue
		}
		payload := line
		if strings.HasPrefix(strings.ToLower(line), "data:") {
			payload = strings.TrimSpace(line[len("data:"):])
		}
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return state.response(), fmt.Errorf("parse google stream chunk: %w", err)
		}
		state.addChunk(chunk, onToken, onReasoning)
	}
	if err := scanner.Err(); err != nil {
		return state.response(), err
	}
	return state.response(), nil
}

type googleStreamState struct {
	text      strings.Builder
	reasoning strings.Builder
	calls     []ToolCall
	seen      map[string]struct{}
	textSig   string
	finish    string
	usage     *Usage
	id        string
}

func (s *googleStreamState) addChunk(raw map[string]interface{}, onToken TokenCallback, onReasoning TokenCallback) {
	if raw == nil {
		return
	}
	if id := strings.TrimSpace(stringField(raw, "responseId")); id != "" {
		s.id = id
	}
	if usage := googleUsage(raw["usageMetadata"]); usage != nil {
		s.usage = usage
	}
	candidates, _ := raw["candidates"].([]interface{})
	if len(candidates) == 0 {
		if feedback := toStringInterfaceMap(raw["promptFeedback"]); feedback != nil && strings.TrimSpace(stringField(feedback, "blockReason")) != "" && s.finish == "" {
			s.finish = "SAFETY"
		}
		return
	}
	candidate := toStringInterfaceMap(candidates[0])
	if candidate == nil {
		return
	}
	if reason := strings.TrimSpace(stringField(candidate, "finishReason")); reason != "" {
		s.finish = reason
	}
	content := toStringInterfaceMap(candidate["content"])
	parts, _ := content["parts"].([]interface{})
	for _, rawPart := range parts {
		part := toStringInterfaceMap(rawPart)
		if part == nil {
			continue
		}
		if call := toStringInterfaceMap(part["functionCall"]); call != nil {
			s.addFunctionCall(call, stringField(part, "thoughtSignature"))
			continue
		}
		text := stringField(part, "text")
		// Thought parts are not replayed, so their signature cannot move onto
		// another part. A later parallel function call must stay unsigned, and
		// a visible text part keeps the signature that arrived on that part.
		if googlePartIsThought(part) {
			if text == "" {
				continue
			}
			s.reasoning.WriteString(text)
			if onReasoning != nil {
				onReasoning(text)
			}
			continue
		}
		if sig := strings.TrimSpace(stringField(part, "thoughtSignature")); sig != "" {
			s.textSig = sig
		}
		if text == "" {
			continue
		}
		s.text.WriteString(text)
		if onToken != nil {
			onToken(text)
		}
	}
}

func (s *googleStreamState) addFunctionCall(call map[string]interface{}, signature string) {
	name := strings.TrimSpace(stringField(call, "name"))
	if name == "" {
		return
	}
	args := googleFunctionArgsJSON(call["args"])
	signature = strings.TrimSpace(signature)
	providerID := strings.TrimSpace(stringField(call, "id"))
	key := name + "\n" + args + "\n" + signature + "\n" + providerID
	if _, ok := s.seen[key]; ok {
		return
	}
	s.seen[key] = struct{}{}
	s.calls = append(s.calls, ToolCall{
		ID:                   fmt.Sprintf("call_%d", len(s.calls)+1),
		Type:                 "function",
		ThoughtSignature:     signature,
		GoogleFunctionCallID: providerID,
		Function: ToolCallFunction{
			Name:      name,
			Arguments: args,
		},
	})
}

func googleFunctionArgsJSON(raw interface{}) string {
	if raw == nil {
		return "{}"
	}
	obj, ok := raw.(map[string]interface{})
	if !ok || obj == nil {
		return "{}"
	}
	data, err := json.Marshal(obj)
	if err != nil || len(data) == 0 {
		return "{}"
	}
	return string(data)
}

func googlePartIsThought(part map[string]interface{}) bool {
	thought, ok := part["thought"].(bool)
	return ok && thought
}

func (s *googleStreamState) response() *Response {
	if s == nil {
		return &Response{Choices: []Choice{{Message: Message{Role: "assistant"}, FinishReason: "stop"}}}
	}
	msg := Message{
		Role:             "assistant",
		Content:          s.text.String(),
		ReasoningContent: s.reasoning.String(),
		ToolCalls:        s.calls,
		ThoughtSignature: s.textSig,
	}
	return &Response{
		ResponseID: s.id,
		Choices: []Choice{{
			Message:      msg,
			FinishReason: googleFinishReason(s.finish, len(s.calls) > 0),
		}},
		Usage: s.usage,
	}
}

func googleFinishReason(reason string, hasTools bool) string {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "MAX_TOKENS":
		return "length"
	case "CONTINUATION":
		// The hop ended with maxOutputTokens still unused. Resuming requires the
		// same request plus continuationToken, not a new turn and not a clean stop.
		// Calls already in this hop are still calls to run.
		if hasTools {
			return "tool_calls"
		}
		return "continuation"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	default:
		if hasTools {
			return "tool_calls"
		}
		return "stop"
	}
}

func googleUsage(raw interface{}) *Usage {
	meta := toStringInterfaceMap(raw)
	if meta == nil {
		return nil
	}
	fields := map[string]interface{}{}
	if value, ok := meta["promptTokenCount"]; ok {
		if number, ok := googleNumber(value); ok {
			fields["prompt_tokens"] = number
		}
	}
	_, hasCandidates := meta["candidatesTokenCount"]
	_, hasThoughts := meta["thoughtsTokenCount"]
	if hasCandidates || hasThoughts {
		candidates, _ := googleNumber(meta["candidatesTokenCount"])
		thoughts, _ := googleNumber(meta["thoughtsTokenCount"])
		fields["completion_tokens"] = candidates + thoughts
	}
	if value, ok := meta["totalTokenCount"]; ok {
		if number, ok := googleNumber(value); ok {
			fields["total_tokens"] = number
		}
	}
	if value, ok := meta["cachedContentTokenCount"]; ok {
		if number, ok := googleNumber(value); ok {
			fields["cached_input_tokens"] = number
		}
	}
	if len(fields) == 0 {
		return nil
	}
	usage := usageFromFields(corelib.ParseLLMUsageFields(fields))
	return &usage
}

func googleNumber(value interface{}) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		parsed, err := n.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

// ListGoogleModels reads models.list and keeps generateContent models.
// The API key is sent as x-goog-api-key. names are returned without the models/ prefix.
func ListGoogleModels(ctx context.Context, cfg corelib.MaclawLLMConfig, client *http.Client) ([]GoogleModel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	base := GoogleGenerateContentBase(cfg.URL)
	client = HTTPClientForRequestContext(ctx, client)
	if client == nil {
		client = SharedHTTPClient
	}
	var out []GoogleModel
	pageToken := ""
	for page := 0; page < googleModelListPageLimit; page++ {
		endpoint := base + "/models?pageSize=1000"
		if pageToken != "" {
			endpoint += "&pageToken=" + url.QueryEscape(pageToken)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		setGoogleAPIHeaders(req, cfg)
		req.Header.Del("Content-Type")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, googleSuccessBodyLimit))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if se := newHTTPStatusError(resp.StatusCode, body); se != nil {
			return nil, se
		}
		var listed struct {
			Models []struct {
				Name                       string   `json:"name"`
				DisplayName                string   `json:"displayName"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &listed); err != nil {
			return nil, fmt.Errorf("parse google model list: %w", err)
		}
		for _, model := range listed.Models {
			if !googleModelSupportsGenerateContent(model.Name, model.SupportedGenerationMethods) {
				continue
			}
			id := googleRequestModel(model.Name)
			if id == "" {
				continue
			}
			out = append(out, GoogleModel{ID: id, DisplayName: strings.TrimSpace(model.DisplayName)})
		}
		pageToken = strings.TrimSpace(listed.NextPageToken)
		if pageToken == "" {
			break
		}
	}
	return out, nil
}

func googleModelSupportsGenerateContent(name string, methods []string) bool {
	for _, method := range methods {
		if strings.EqualFold(strings.TrimSpace(method), "generateContent") {
			return true
		}
	}
	if len(methods) > 0 {
		return false
	}
	id := strings.ToLower(googleRequestModel(name))
	return strings.Contains(id, "gemini") && !strings.Contains(id, "embedding")
}
