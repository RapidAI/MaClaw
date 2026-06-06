package knowledge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// VisionLLMConfig is the configuration for the optional Vision LLM used by
// the knowledge base to generate high-quality image descriptions.
// Uses OpenAI-compatible /v1/chat/completions endpoint with image_url content blocks.
type VisionLLMConfig struct {
	Enabled     bool   `json:"enabled"`
	BaseURL     string `json:"base_url"`     // e.g. "https://api.openai.com/v1"
	APIKey      string `json:"api_key"`
	Model       string `json:"model"`        // e.g. "gpt-4o-mini", "glm-4v-flash", "qwen-vl-plus"
	MaxTokens   int    `json:"max_tokens"`   // default 500
	TimeoutSec  int    `json:"timeout_sec"`  // default 30
	Verified    bool   `json:"verified"`     // set true after successful health check
	FromMainLLM bool   `json:"from_main_llm"` // true when auto-derived from main LLM config (auto-verified)
}

// ConfigPersister is called when the verified status changes at runtime.
// Implementations should persist the updated config to disk.
type ConfigPersister func(cfg *VisionLLMConfig)

// NewVisionLLMConfigFromMainLLM creates a VisionLLMConfig derived from the main LLM
// when it supports vision. This config is auto-verified (no health check needed
// because the main LLM has already been tested by the user).
func NewVisionLLMConfigFromMainLLM(baseURL, apiKey, model string) *VisionLLMConfig {
	if baseURL == "" || apiKey == "" || model == "" {
		return nil
	}
	return &VisionLLMConfig{
		Enabled:     true,
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       model,
		MaxTokens:   500,
		TimeoutSec:  30,
		Verified:    true,
		FromMainLLM: true,
	}
}

// VisionDescriber calls a Vision LLM to generate image descriptions.
type VisionDescriber struct {
	mu        sync.RWMutex
	cfg       *VisionLLMConfig
	client    *http.Client
	persister ConfigPersister
}

// NewVisionDescriber creates a Vision LLM describer.
// persister is called when verified status changes (may be nil for testing).
func NewVisionDescriber(cfg *VisionLLMConfig, persister ConfigPersister) *VisionDescriber {
	if cfg == nil {
		return nil
	}
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &VisionDescriber{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
		},
		persister: persister,
	}
}

// IsVerified returns true if the Vision LLM is configured, enabled, and has
// passed the health check.
func (v *VisionDescriber) IsVerified() bool {
	if v == nil {
		return false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.cfg != nil && v.cfg.Enabled && v.cfg.Verified
}

// ClearVerified marks the Vision LLM as unverified (runtime failure degradation).
// This persists the change so subsequent imports don't attempt Vision until
// the user re-verifies.
func (v *VisionDescriber) ClearVerified() {
	if v == nil {
		return
	}
	var shouldPersist bool
	v.mu.Lock()
	if v.cfg != nil && v.cfg.Verified {
		v.cfg.Verified = false
		shouldPersist = true
	}
	persister := v.persister
	v.mu.Unlock()
	// Persist outside lock to avoid blocking concurrent reads.
	if shouldPersist && persister != nil {
		persister(v.cfg)
	}
	log.Printf("[knowledge-vision] cleared verified status due to runtime failure")
}

// Close releases resources.
func (v *VisionDescriber) Close() {
	// http.Client doesn't need explicit close, but we nil the reference.
}

// HealthCheck sends a test image to verify the Vision LLM is reachable and
// returns a valid response. Called when the user saves Vision LLM config.
// On success, sets Verified=true and persists.
func (v *VisionDescriber) HealthCheck(ctx context.Context) error {
	if v == nil || v.cfg == nil {
		return fmt.Errorf("vision LLM not configured")
	}
	if v.cfg.BaseURL == "" || v.cfg.APIKey == "" || v.cfg.Model == "" {
		return fmt.Errorf("vision LLM config incomplete: base_url, api_key, and model are required")
	}

	// Generate a tiny 2x2 red test image.
	testImage := generateTestImage()
	b64 := base64.StdEncoding.EncodeToString(testImage)

	// Call the API with a simple prompt.
	resp, err := v.callVisionAPI(ctx, b64, "image/png", "Describe this test image in one sentence.")
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	if strings.TrimSpace(resp) == "" {
		return fmt.Errorf("health check failed: empty response from API")
	}

	// Passed — mark as verified.
	v.mu.Lock()
	v.cfg.Verified = true
	persister := v.persister
	v.mu.Unlock()
	// Persist outside lock to avoid blocking concurrent reads.
	if persister != nil {
		persister(v.cfg)
	}
	log.Printf("[knowledge-vision] health check passed, model=%s", v.cfg.Model)
	return nil
}

// Describe calls the Vision LLM to generate a structured description of the image.
func (v *VisionDescriber) Describe(ctx context.Context, imagePath string, hints ImageHints) (ImageDescription, error) {
	if v == nil || v.cfg == nil {
		return ImageDescription{}, fmt.Errorf("vision LLM not configured")
	}

	// Read and encode image.
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return ImageDescription{}, fmt.Errorf("read image: %w", err)
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	mime := mimeFromPath(imagePath)

	// Build prompt with context hints.
	prompt := buildVisionPrompt(hints)

	// Call API.
	resp, err := v.callVisionAPI(ctx, b64, mime, prompt)
	if err != nil {
		return ImageDescription{}, err
	}

	// Parse structured response.
	return parseVisionResponse(resp, hints), nil
}

// callVisionAPI sends a request to the OpenAI-compatible vision endpoint.
func (v *VisionDescriber) callVisionAPI(ctx context.Context, imageBase64, mimeType, prompt string) (string, error) {
	maxTokens := v.cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 500
	}

	dataURL := fmt.Sprintf("data:%s;base64,%s", mimeType, imageBase64)

	reqBody := map[string]interface{}{
		"model": v.cfg.Model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": dataURL,
						},
					},
				},
			},
		},
		"max_tokens": maxTokens,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(v.cfg.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+v.cfg.APIKey)

	resp, err := v.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vision API returned HTTP %d: %s", resp.StatusCode, truncateBytes(respBody, 200))
	}

	// Parse OpenAI-compatible response.
	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("parse response JSON: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("vision API returned 0 choices")
	}

	return apiResp.Choices[0].Message.Content, nil
}

// buildVisionPrompt constructs the prompt sent to the Vision LLM.
func buildVisionPrompt(hints ImageHints) string {
	var sb strings.Builder
	sb.WriteString(`请描述这张图片的内容。要求：
1. 用中文，2-4句话概括图片主要内容
2. 如果是流程图/架构图/UML图，描述各组件和它们的关系
3. 如果是表格/数据图表，描述关键数据和趋势
4. 如果是截图/界面，描述界面布局和关键元素
5. 提取图中所有可见的文字

`)

	// Add context hints.
	if hints.FileName != "" || hints.ParentTitle != "" || hints.ContextBefore != "" {
		sb.WriteString("上下文信息：\n")
		if hints.FileName != "" {
			fmt.Fprintf(&sb, "- 文件名: %s\n", hints.FileName)
		}
		if hints.SourceTitle != "" {
			fmt.Fprintf(&sb, "- 所在文档: %s\n", hints.SourceTitle)
		}
		if hints.ParentTitle != "" {
			fmt.Fprintf(&sb, "- 所在章节: %s\n", hints.ParentTitle)
		}
		if hints.PageNumber > 0 {
			fmt.Fprintf(&sb, "- 页码: %d\n", hints.PageNumber)
		}
		if hints.ContextBefore != "" {
			fmt.Fprintf(&sb, "- 前文: %s\n", truncateRunes(hints.ContextBefore, 150))
		}
		if hints.ContextAfter != "" {
			fmt.Fprintf(&sb, "- 后文: %s\n", truncateRunes(hints.ContextAfter, 150))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`请按以下 JSON 格式返回（不要包含 markdown 代码块标记）：
{"title": "短标题", "description": "详细描述", "text_content": "图中文字", "entities": ["实体1", "实体2"]}`)

	return sb.String()
}

// parseVisionResponse parses the Vision LLM response text into ImageDescription.
func parseVisionResponse(resp string, hints ImageHints) ImageDescription {
	// Try JSON parse first.
	resp = strings.TrimSpace(resp)
	// Strip markdown code block if present.
	if strings.HasPrefix(resp, "```") {
		lines := strings.Split(resp, "\n")
		if len(lines) >= 3 {
			resp = strings.Join(lines[1:len(lines)-1], "\n")
			resp = strings.TrimSpace(resp)
		}
	}

	var parsed struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		TextContent string   `json:"text_content"`
		Entities    []string `json:"entities"`
	}

	if err := json.Unmarshal([]byte(resp), &parsed); err == nil && parsed.Description != "" {
		return ImageDescription{
			Title:       parsed.Title,
			Description: parsed.Description,
			OCRText:     parsed.TextContent,
			Entities:    parsed.Entities,
		}
	}

	// Fallback: treat entire response as description.
	desc := ImageDescription{
		Title:       inferImageTitle(hints),
		Description: truncateRunes(resp, 500),
	}
	return desc
}

// generateTestImage creates a tiny 2x2 red PNG for health check.
func generateTestImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	red := color.RGBA{R: 255, A: 255}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, red)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// mimeFromPath returns a MIME type based on file extension.
func mimeFromPath(path string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(path)))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	default:
		return "image/png"
	}
}

func truncateBytes(b []byte, maxLen int) string {
	if len(b) <= maxLen {
		return string(b)
	}
	return string(b[:maxLen]) + "..."
}
