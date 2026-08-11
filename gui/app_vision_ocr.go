package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// visionRecognizeImage sends one screenshot to the CURRENT main LLM when it
// supports image input (SupportsVision), and returns the model's text reply.
// It is the sendImage callback behind browser.LLMVisionProvider. When the
// active model has no vision capability it returns an error immediately so the
// caller falls back to the local PP-OCRv6 engine — the "multimodal first, OCR
// only otherwise" policy. Capability is read per call, so switching providers
// or toggling supports_vision takes effect without a restart.
func (a *App) visionRecognizeImage(pngB64, prompt string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app not available")
	}
	cfg := a.GetMaclawLLMConfig()
	if !cfg.SupportsVision {
		return "", browser.ErrVisionUnsupported
	}

	var imageBlock interface{}
	if cfg.Protocol == "anthropic" {
		imageBlock = map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": "image/png",
				"data":       pngB64,
			},
		}
	} else {
		imageBlock = map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": "data:image/png;base64," + pngB64,
			},
		}
	}
	messages := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": prompt},
				imageBlock,
			},
		},
	}

	client := &http.Client{Timeout: 75 * time.Second}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "vision-ocr", OwnerID: "vision-ocr"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, 60*time.Second)
	if err != nil {
		return "", err
	}
	if resp == nil || resp.Content == "" {
		return "", fmt.Errorf("vision model returned empty content")
	}
	return stripFunctionCalls(stripThinkTags(resp.Content)), nil
}

// newVisionFirstOCRProvider composes the vision-LLM channel (current main
// model, when it supports images) with the local OCR engine as fallback.
// app may be nil (tests, tools without an App): the result is the plain
// fallback then. The fallback is gated on the ocr_enabled settings toggle so
// turning OCR off stops every local-engine consumer, not just ocr_recognize.
func newVisionFirstOCRProvider(app *App, fallback browser.OCRProvider) browser.OCRProvider {
	if app == nil {
		return fallback
	}
	if fallback != nil {
		fallback = &configGatedOCRProvider{inner: fallback}
	}
	vision := browser.NewLLMVisionProvider(app.visionRecognizeImage)
	return browser.NewVisionFirstOCRProvider(vision, fallback, func(format string, args ...interface{}) {
		log.Printf("[ocr] "+format, args...)
	})
}

// configGatedOCRProvider applies the ocr_enabled settings toggle to the shared
// local OCR engine on every call. The toggle is read per call (cheap config
// peek) so flipping the setting takes effect without re-registering tools.
type configGatedOCRProvider struct {
	inner browser.OCRProvider
}

func (g *configGatedOCRProvider) Recognize(pngBase64 string) ([]browser.OCRResult, error) {
	if !ocrConfiguredEnabled() {
		return nil, fmt.Errorf("OCR disabled in settings (ocr_enabled=false)")
	}
	return g.inner.Recognize(pngBase64)
}

func (g *configGatedOCRProvider) IsAvailable() bool {
	return g.inner != nil && g.inner.IsAvailable() && ocrConfiguredEnabled()
}

// Close is a no-op: the inner engine is the process-wide shared provider whose
// lifetime the app owns.
func (g *configGatedOCRProvider) Close() {}

// imageTextFromAttachment recognizes the text of a chat image attachment with
// the local OCR engine. It backs BuildUserContent's ImageTextRecognizer hook,
// which only runs when the current model does NOT support vision — so this
// always uses the local engine by design. Returns an error when nothing was
// recognized so the caller can omit the section.
func imageTextFromAttachment(_, base64Data string) (string, error) {
	if !ocrConfiguredEnabled() {
		return "", fmt.Errorf("OCR disabled in settings")
	}
	results, err := sharedNativeOCRProvider().Recognize(base64Data)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "", fmt.Errorf("no text recognized")
	}
	return browser.FormatOCRForLLM(results), nil
}

// ocrConfiguredEnabled peeks ocr_enabled from the on-disk config without an
// App instance (same pattern as ocrConfiguredModelTier). Defaults to true —
// both for fresh installs (DefaultAppConfig) and old configs written before
// the flag existed.
func ocrConfiguredEnabled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return true
	}
	data, err := os.ReadFile(filepath.Join(home, ".maclaw", "config.json"))
	if err != nil {
		return true
	}
	var probe struct {
		OCREnabled *bool `json:"ocr_enabled"`
	}
	if json.Unmarshal(data, &probe) != nil || probe.OCREnabled == nil {
		return true
	}
	return *probe.OCREnabled
}
