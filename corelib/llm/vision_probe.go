package llm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// visionProbePNG is a 64x64 solid red image. A smaller sample is rejected or
// described vaguely by models that do accept images.
const visionProbePNG = "iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAYAAACqaXHeAAAAY0lEQVR42u3QMREAAAgAoe9fWnN4MlCApuazBAgQIECAAAECBAgQIECAAAECBAgQIECAAAECBAgQIECAAAECBAgQIECAAAECBAgQIECAAAECBAgQIECAAAECBAgQIECAgPsBC0OI4dLW/6NFAAAAAElFTkSuQmCC"

const visionProbeTimeout = 25 * time.Second

// ProbeVision asks the provider whether this model accepts an image.
// supported is meaningful only when known is true. A transport failure or an
// answer that neither describes the image nor refuses it stays unknown.
func ProbeVision(ctx context.Context, cfg corelib.MaclawLLMConfig) (supported, known bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, visionProbeTimeout)
	defer cancel()
	messages := []interface{}{map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "What color is this image? Reply with one color word. If you cannot see the image, say exactly: no image"},
			map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": "data:image/png;base64," + visionProbePNG},
			},
		},
	}}
	client := &http.Client{Timeout: visionProbeTimeout}
	var resp *Response
	var err error
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		resp, err = DoAnthropicRequestWithOptions(ctx, cfg, messages, nil, client, AnthropicMessagesRequestOptions{MaxTokens: 16})
	} else {
		resp, err = DoOpenAIRequestWithOptions(ctx, cfg, messages, nil, client, OpenAIChatRequestOptions{
			PassThrough: map[string]interface{}{"max_tokens": 16},
		})
	}
	if err != nil {
		if ImageInputRejected(err) {
			return false, true
		}
		return false, false
	}
	content := ""
	if resp != nil && len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	return classifyVisionContent(content)
}

// ImageInputRejected reports an upstream 400/422 that refuses image input.
// Quota, auth, and other malformed-request errors are not a capability answer.
func ImageInputRejected(err error) bool {
	var httpErr *HTTPStatusError
	if !errors.As(err, &httpErr) || httpErr == nil {
		return false
	}
	if httpErr.StatusCode != http.StatusBadRequest && httpErr.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	// A thinking-off refusal is not an image-capability answer. Gateways echo
	// the request, so image_url plus "unsupported" would otherwise clear vision.
	if _, rejected := corelib.ThinkingOffRejectedEffort(string(httpErr.Body)); rejected {
		return false
	}
	message := strings.ToLower(string(httpErr.Body))
	hasImage := strings.Contains(message, "image") || strings.Contains(message, "vision") ||
		strings.Contains(message, "图片") || strings.Contains(message, "图像")
	hasRejection := strings.Contains(message, "not support") || strings.Contains(message, "unsupported") ||
		strings.Contains(message, "does not support") || strings.Contains(message, "doesn't support") ||
		strings.Contains(message, "cannot process") || strings.Contains(message, "not enabled") ||
		strings.Contains(message, "不支持") || strings.Contains(message, "无法处理")
	return hasImage && hasRejection
}

func classifyVisionContent(content string) (supported, known bool) {
	lower := strings.ToLower(content)
	if visionReportsNoImage(lower) {
		return false, true
	}
	for _, word := range []string{"red", "scarlet", "crimson", "#ff0000", "ff0000", "255, 0, 0", "255,0,0", "红", "赤"} {
		if strings.Contains(lower, word) {
			return true, true
		}
	}
	return false, false
}

func visionReportsNoImage(lower string) bool {
	for _, neg := range []string{"no image", "can't see", "cannot see", "don't see", "没有图片", "看不到", "无法看到"} {
		if strings.Contains(lower, neg) {
			return true
		}
	}
	return false
}
