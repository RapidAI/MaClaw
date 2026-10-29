// Chat: the /algo streaming wire (signed envelope). The request body is a
// plain OpenAI-shaped JSON object that the wasm encodes; the response is an
// SSE stream whose data lines wrap the standard OpenAI chunks:
//
//	data:{"headers":{…},"body":"{\"choices\":[{\"delta\":{\"content\":…}}]}"}
//	data:{"body":"{\"choices\":[{…,\"finish_reason\":\"stop\"}]}"}
//	data:[DONE]
//	data:{"firstTokenDuration":…}   ← telemetry tail, ignored
package qoder

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/qoder/cosy"
)

// Both Qoder editions withhold HTTP headers until the first token. The shared
// OAuth client budget is 15s and is meant for token calls; using it here fails
// Test & Save with Client.Timeout while still awaiting headers. 国内 talks to
// gateway.qoder.com.cn and 国际 talks to api3.qoder.sh, and both use the same
// generation budget as every other provider probe.
const (
	chatProbeHeaderTimeout = corelib.LLMProbeHeaderTimeout
	chatProbeTotalTimeout  = corelib.LLMProbeTotalTimeout
	chatProbeMaxTokens     = corelib.LLMProbeMaxOutputTokens
)

// ChatMessage is one OpenAI-style conversation turn.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest carries the probe parameters for ChatProbe.
type ChatRequest struct {
	Model       string
	Messages    []ChatMessage
	MaxTokens   int
	Temperature float64
}

type chatEnvelope struct {
	Headers json.RawMessage `json:"headers"`
	Body    string          `json:"body"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage"`
	// Server-side error chunks ({code:"provider_error", …}) surface as a
	// body-level code/message pair instead of a choices array.
	Code    string `json:"code"`
	Message string `json:"message"`
}

// chatBodyJSON builds the wire body: OpenAI-shaped messages plus the session
// metadata the agent_chat_generation router expects.
func chatBodyJSON(req ChatRequest) (string, error) {
	// The model key runs through the same canonicalization as the agent-loop
	// transport so legacy display names (qwen3.8-max …) cannot leak to the
	// agent_chat node's model validation.
	model := canonicalQoderModelKey(req.Model)
	body := map[string]any{
		"model":    model,
		"messages": req.Messages,
	}
	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	applyAgentChatContract(body)
	raw, err := marshalAgentChatJSON(body)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ChatProbe sends one non-streaming probe turn and returns the concatenated
// assistant text. Detect-and-save uses it; the agent runtime's full streaming
// adapter rides the same envelope.
func ChatProbe(ctx context.Context, profile Profile, uid, accessToken, model, prompt string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("未登录，无法发起 Qoder 聊天")
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultModel
	}
	// The signer and the body must carry the same catalog key. Leaving a
	// legacy display name on the signer sends the gateway a model it will
	// not start, and the probe then sits until the header budget expires.
	model = canonicalQoderModelKey(model)
	signing, err := cosy.New(MachineID(), ClientVersion, strings.TrimSpace(uid))
	if err != nil {
		return "", fmt.Errorf("Qoder 聊天签名上下文初始化失败: %w", err)
	}
	defer signing.Close()
	if err := signing.RefreshAuthFields(strings.TrimSpace(accessToken)); err != nil {
		return "", fmt.Errorf("Qoder 聊天签名材料注入失败: %w", err)
	}
	bodyJSON, err := chatBodyJSON(ChatRequest{
		Model:       model,
		Messages:    []ChatMessage{{Role: "user", Content: prompt}},
		MaxTokens:   chatProbeMaxTokens,
		Temperature: 0.3,
	})
	if err != nil {
		return "", err
	}
	prepared, err := signing.PrepareInferRequest(strings.TrimRight(profile.ChatOrigin, "/"), bodyJSON, model, "system")
	if err != nil {
		return "", fmt.Errorf("Qoder 聊天请求构建失败: %w", err)
	}
	callCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, chatProbeTotalTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, prepared.URL, strings.NewReader(prepared.Body))
	if err != nil {
		return "", err
	}
	for key, value := range prepared.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := oauth.DoNoFollowWithin(req, chatProbeHeaderTimeout, chatProbeTotalTimeout)
	if err != nil {
		return "", fmt.Errorf("发送 Qoder 聊天请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if text := truncateForError(snippet); text != "" {
			return "", fmt.Errorf("Qoder 聊天请求失败 (HTTP %d): %s", resp.StatusCode, text)
		}
		return "", fmt.Errorf("Qoder 聊天请求失败 (HTTP %d)", resp.StatusCode)
	}
	return parseChatSSE(resp.Body)
}

// parseChatSSE scans the envelope stream and accumulates assistant text. An
// upstream failure frame (event:error → data:{stackTrace,…}) carries no body,
// so a stream that ends without any token is an error, never a silent pass —
// the connection test must not save a provider that produced nothing.
func parseChatSSE(body io.Reader) (string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var content strings.Builder
	var finishSeen bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" || data == "[DONE]" {
			if data == "[DONE]" {
				break
			}
			continue
		}
		var envelope chatEnvelope
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			continue
		}
		// The failure tail ({"success":false,"msgCode":500,…}) unmarshals into
		// an envelope too (unknown keys ignored), so classify it by field
		// before treating the line as a chunk.
		var failure struct {
			MsgInfo int    `json:"msgCode"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &failure); err == nil && failure.MsgInfo != 0 {
			return "", fmt.Errorf("Qoder 上游错误 (HTTP %d): %s", failure.MsgInfo, failure.Message)
		}
		if envelope.Body == "" {
			continue
		}
		var chunk chatChunk
		if err := json.Unmarshal([]byte(envelope.Body), &chunk); err != nil {
			continue
		}
		if chunk.Code != "" {
			return "", fmt.Errorf("Qoder 聊天被上游拒绝 (%s): %s", chunk.Code, chunk.Message)
		}
		for _, choice := range chunk.Choices {
			// A reasoning model can spend the whole reply in reasoning_content.
			// That text is enough for the connection test.
			piece := choice.Delta.Content
			if strings.TrimSpace(piece) == "" {
				piece = choice.Delta.Reasoning
			}
			content.WriteString(piece)
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finishSeen = true
			}
		}
		if text := strings.TrimSpace(content.String()); text != "" {
			// The connection test only needs proof of a reply. Closing the
			// body here stops the rest of the generation.
			return clipProbeText(text), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(content.String()) == "" && !finishSeen {
		return "", fmt.Errorf("Qoder 未返回任何模型内容")
	}
	if strings.TrimSpace(content.String()) == "" {
		return "", fmt.Errorf("Qoder 模型回复为空 (finish=%v)", finishSeen)
	}
	return clipProbeText(content.String()), nil
}

func clipProbeText(text string) string {
	runes := []rune(strings.TrimSpace(text))
	const maxProbeRunes = 120
	if len(runes) <= maxProbeRunes {
		return string(runes)
	}
	return string(runes[:maxProbeRunes])
}
