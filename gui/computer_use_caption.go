package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	xdraw "golang.org/x/image/draw"
)

const (
	computerUseCaptionTimeout     = 12 * time.Second
	computerUseCaptionCallTimeout = 6 * time.Second
	computerUseCaptionMaxTokens   = 80
	computerUseCaptionConcurrency = 3
	computerUseCaptionPadPx       = 4
	computerUseCaptionMaxCropEdge = 256
)

const computerUseCaptionPrompt = `This is a cropped desktop UI control. Reply with JSON only:
{"name":"short visible label or icon meaning","type":"button|edit|icon|checkbox|link|menu|tab|other"}
Match any visible text; otherwise English. Empty name if unreadable.`

var computerUseCaptionHTTP = newComputerUseCaptionHTTPClient()

func newComputerUseCaptionHTTPClient() *http.Client {
	transport := http.DefaultTransport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := dt.Clone()
		cloned.MaxIdleConnsPerHost = computerUseCaptionConcurrency
		transport = cloned
	}
	// Bound each call with request context, not Client.Timeout, so a 401/429
	// cancel aborts in-flight Do() instead of racing a client-level timer.
	return &http.Client{Transport: transport}
}

// computerUseCaptionConfigFn returns the optional caption model. Tests and
// bindComputerUseApp replace this; empty means skip NN captioning.
var computerUseCaptionConfigFn = func() (corelib.MaclawLLMConfig, bool) {
	return corelib.MaclawLLMConfig{}, false
}

// applyComputerUseCaptions labels unlabeled SoM boxes when the chat model
// cannot see images. Fail-open: errors leave heuristic names in place.
func applyComputerUseCaptions(pngB64 string, marks []computeruse.MarkedElement) int {
	if computerUseLLMSupportsVision() {
		return 0
	}
	idxs := computeruse.UnlabeledCaptionIndices(marks, computeruse.DefaultCaptionMaxBoxes)
	if len(idxs) == 0 {
		return 0
	}
	cfg, ok := computerUseCaptionConfigFn()
	if !ok || strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return 0
	}
	cfg = captionRequestConfig(cfg)
	img, err := decodePNGBase64(pngB64)
	if err != nil {
		log.Printf("[computer-use] caption decode: %v", err)
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), computerUseCaptionTimeout)
	defer cancel()
	ctx = llm.WithRequestTraceIfMissing(ctx, "computer-use-caption")

	sem := make(chan struct{}, computerUseCaptionConcurrency)
	var mu sync.Mutex
	applied := 0
	var wg sync.WaitGroup
	for _, i := range idxs {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			crop, err := cropImageBoxBase64(img, marks[i].BBox, computerUseCaptionPadPx)
			if err != nil || crop == "" {
				return
			}
			if ctx.Err() != nil {
				return
			}
			raw, err := callCaptionLLM(ctx, cfg, crop)
			if err != nil {
				if shouldAbortCaptionBatch(err) {
					cancel()
				}
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					log.Printf("[computer-use] caption %s: %v", marks[i].Ref, err)
				}
				return
			}
			cap := computeruse.ParseCaptionResponse(raw)
			if cap.Name == "" && cap.Type == "" {
				return
			}
			mu.Lock()
			if computeruse.ApplyCaption(&marks[i], cap) {
				applied++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return applied
}

func callCaptionLLM(ctx context.Context, cfg corelib.MaclawLLMConfig, pngB64 string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, computerUseCaptionCallTimeout)
	defer cancel()
	atts := []agent.MessageAttachment{{MimeType: "image/png", Data: pngB64}}
	var content interface{}
	if captionUsesAnthropic(cfg) {
		content = agent.BuildAnthropicVisionContent(computerUseCaptionPrompt, atts)
	} else {
		content = agent.BuildOpenAIVisionContent(computerUseCaptionPrompt, atts)
	}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": content}}

	maxTok := map[string]interface{}{"max_tokens": computerUseCaptionMaxTokens}
	if captionUsesAnthropic(cfg) {
		resp, err := llm.DoAnthropicRequestWithOptions(callCtx, cfg, messages, nil, computerUseCaptionHTTP, llm.AnthropicMessagesRequestOptions{
			MaxTokens: computerUseCaptionMaxTokens,
		})
		return captionTextFromResponse(resp, err)
	}
	if cfg.IsResponsesAPI() {
		req, _, _, err := llm.NewResponsesAPIRequest(callCtx, cfg, messages, llm.ResponsesAPIRequestOptions{
			Stream:    false,
			ExtraBody: maxTok,
		})
		if err != nil {
			return "", err
		}
		return captionHTTPParse(req, llm.ParseNonStreamResponsesAPIBody)
	}

	req, _, _, err := llm.NewOpenAIChatRequest(callCtx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream:    false,
		ExtraBody: maxTok,
	})
	if err != nil {
		return "", err
	}
	return captionHTTPParse(req, llm.ParseNonStreamOpenAIResponseBody)
}

func captionHTTPParse(req *http.Request, parse func([]byte) (*llm.Response, error)) (string, error) {
	resp, err := computerUseCaptionHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", &llm.HTTPStatusError{StatusCode: resp.StatusCode, Body: append([]byte(nil), body...)}
	}
	parsed, err := parse(body)
	return captionTextFromResponse(parsed, err)
}

func captionUsesAnthropic(cfg corelib.MaclawLLMConfig) bool {
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		return true
	}
	return corelib.IsAnthropicWireAPI(cfg.WireAPI)
}

func captionRequestConfig(cfg corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	// Tiny JSON labels do not need chain-of-thought. Disable thinking when the
	// provider would otherwise spend a reasoning budget, but leave generic
	// OpenAI-compat auto mode alone so we do not inject thinking.type=disabled.
	// GLM-5.3 rejects disabled thinking, so keep its required always-on path.
	if corelib.IsAlwaysOnThinkingModel(cfg) {
		return corelib.CoerceAlwaysOnThinkingMode(cfg)
	}
	if !corelib.IsAutoThinkingMode(cfg.ThinkingMode) ||
		corelib.IsDeepSeekThinkingModeModel(cfg) ||
		corelib.IsQwenOpenAICompat(cfg) {
		cfg.ThinkingMode = "disabled"
	}
	return cfg
}

func shouldAbortCaptionBatch(err error) bool {
	switch captionHTTPStatusCode(err) {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func captionHTTPStatusCode(err error) int {
	if err == nil {
		return 0
	}
	var httpErr *llm.HTTPStatusError
	if errors.As(err, &httpErr) && httpErr != nil {
		return httpErr.StatusCode
	}
	msg := err.Error()
	var code int
	if n, _ := fmt.Sscanf(msg, "caption HTTP %d", &code); n == 1 {
		return code
	}
	if n, _ := fmt.Sscanf(msg, "HTTP %d:", &code); n == 1 {
		return code
	}
	return 0
}

func captionTextFromResponse(resp *llm.Response, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty caption response")
	}
	out := strings.TrimSpace(resp.Choices[0].Message.Content)
	if out == "" {
		out = strings.TrimSpace(resp.Choices[0].Message.ReasoningContent)
	}
	return agent.StripThinkingTags(out), nil
}

func decodePNGBase64(pngB64 string) (image.Image, error) {
	raw, err := base64.StdEncoding.DecodeString(pngB64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(pngB64)
		if err != nil {
			return nil, err
		}
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return img, nil
}

func cropImageBoxBase64(img image.Image, bbox [4]int, pad int) (string, error) {
	if img == nil {
		return "", fmt.Errorf("nil image")
	}
	b := img.Bounds()
	x, y, w, h := bbox[0]-pad, bbox[1]-pad, bbox[2]+2*pad, bbox[3]+2*pad
	if w <= 0 || h <= 0 {
		return "", fmt.Errorf("empty crop")
	}
	minX := b.Min.X + x
	minY := b.Min.Y + y
	maxX := minX + w
	maxY := minY + h
	if minX < b.Min.X {
		minX = b.Min.X
	}
	if minY < b.Min.Y {
		minY = b.Min.Y
	}
	if maxX > b.Max.X {
		maxX = b.Max.X
	}
	if maxY > b.Max.Y {
		maxY = b.Max.Y
	}
	if maxX <= minX || maxY <= minY {
		return "", fmt.Errorf("crop outside image")
	}
	// Copy into a private RGBA so concurrent png.Encode cannot share paletted Pix.
	dst := image.NewRGBA(image.Rect(0, 0, maxX-minX, maxY-minY))
	draw.Draw(dst, dst.Bounds(), img, image.Pt(minX, minY), draw.Src)
	encoded := downsampleCaptionCrop(dst)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, encoded); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func downsampleCaptionCrop(src *image.RGBA) image.Image {
	if src == nil {
		return src
	}
	dx, dy := src.Bounds().Dx(), src.Bounds().Dy()
	if dx <= computerUseCaptionMaxCropEdge && dy <= computerUseCaptionMaxCropEdge {
		return src
	}
	scale := float64(computerUseCaptionMaxCropEdge) / float64(dx)
	if dy > dx {
		scale = float64(computerUseCaptionMaxCropEdge) / float64(dy)
	}
	nw := int(float64(dx)*scale + 0.5)
	nh := int(float64(dy)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return out
}
