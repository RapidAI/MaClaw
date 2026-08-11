package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrVisionUnsupported is returned (wrapped) by the sendImage callback when the
// currently active model has no image-input capability. It signals a
// configuration/capability state, not an endpoint failure: composite providers
// must NOT count it against the vision circuit breaker or log it as a failure,
// because it is the expected steady state for text-only models.
var ErrVisionUnsupported = errors.New("current model does not support vision")

// LLMVisionProvider uses a multimodal LLM to recognize text in screenshots.
type LLMVisionProvider struct {
	// sendImage sends a base64 PNG + prompt to the LLM and returns the text response.
	sendImage func(base64PNG string, prompt string) (string, error)
}

// NewLLMVisionProvider creates an LLM Vision OCR provider.
// sendImage should send the image to a multimodal LLM and return the response text.
func NewLLMVisionProvider(sendImage func(string, string) (string, error)) *LLMVisionProvider {
	return &LLMVisionProvider{sendImage: sendImage}
}

// Recognize implements OCRProvider by sending the screenshot to an LLM.
func (p *LLMVisionProvider) Recognize(pngBase64 string) ([]OCRResult, error) {
	if p.sendImage == nil {
		return nil, fmt.Errorf("LLM vision not configured")
	}

	prompt := `请识别这张网页截图中的所有可见文本内容。
按从上到下、从左到右的顺序列出每个文本区域。
输出 JSON 数组格式: [{"text": "文本内容", "confidence": 0.95, "bbox": [x, y, w, h]}]
如果无法精确确定坐标，bbox 可以用 [0,0,0,0]。
只输出 JSON，不要其他内容。`

	resp, err := p.sendImage(pngBase64, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM vision: %w", err)
	}

	// Try to parse as JSON array
	resp = strings.TrimSpace(resp)
	// Strip markdown code fences if present
	resp = strings.TrimPrefix(resp, "```json")
	resp = strings.TrimPrefix(resp, "```")
	resp = strings.TrimSuffix(resp, "```")
	resp = strings.TrimSpace(resp)

	var results []OCRResult
	if err := json.Unmarshal([]byte(resp), &results); err != nil {
		// Unparseable output (prose, markdown tables, ...) carries no bounding
		// boxes — downstream element labeling would silently degrade. Report an
		// error so composite providers can fall back to the local OCR engine.
		return nil, fmt.Errorf("LLM vision: response is not a JSON array: %w", err)
	}
	return results, nil
}

// IsAvailable implements OCRProvider.
func (p *LLMVisionProvider) IsAvailable() bool {
	return p.sendImage != nil
}

// Close implements OCRProvider.
func (p *LLMVisionProvider) Close() {}
