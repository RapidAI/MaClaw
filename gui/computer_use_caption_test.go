package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func setupComputerUseCaptionTest(t *testing.T) {
	t.Helper()
	prevCfg := computerUseCaptionConfigFn
	prevVision := computerUseVisionFn
	globalComputerUse.mu.Lock()
	globalComputerUse.turnVisionKnown = false
	globalComputerUse.turnVision = false
	globalComputerUse.mu.Unlock()
	t.Cleanup(func() {
		computerUseCaptionConfigFn = prevCfg
		computerUseVisionFn = prevVision
		globalComputerUse.mu.Lock()
		globalComputerUse.turnVisionKnown = false
		globalComputerUse.turnVision = false
		globalComputerUse.mu.Unlock()
	})
}

func TestApplyComputerUseCaptionsLabelsUnlabeled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "image_url") {
			t.Errorf("caption request missing image: %s", string(body))
		}
		if !strings.Contains(string(body), `"max_tokens":80`) && !strings.Contains(string(body), `"max_tokens": 80`) {
			t.Errorf("caption request missing max_tokens cap: %s", string(body))
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"name":"Save","type":"button"}`}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	setupComputerUseCaptionTest(t)
	computerUseVisionFn = func() bool { return false }
	computerUseCaptionConfigFn = func() (corelib.MaclawLLMConfig, bool) {
		return corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "caption-model", Protocol: "openai"}, true
	}

	pngB64 := tinyCaptionPNG(t, 32, 32)
	marks := []computeruse.MarkedElement{
		{Ref: "e0", Name: "OK", Type: "button", BBox: [4]int{0, 0, 10, 10}},
		{Ref: "e1", Name: "", Type: "interactable", BBox: [4]int{8, 8, 12, 12}},
	}
	n := applyComputerUseCaptions(pngB64, marks)
	if n != 1 {
		t.Fatalf("applied=%d want 1", n)
	}
	if marks[0].Name != "OK" {
		t.Fatalf("labeled box changed: %+v", marks[0])
	}
	if marks[1].Name != "Save" || marks[1].Type != "button" {
		t.Fatalf("unlabeled box: %+v", marks[1])
	}
}

func TestApplyComputerUseCaptionsSkippedWhenChatHasVision(t *testing.T) {
	called := false
	setupComputerUseCaptionTest(t)
	computerUseCaptionConfigFn = func() (corelib.MaclawLLMConfig, bool) {
		called = true
		return corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Model: "x"}, true
	}
	setComputerUseTurnVision(true)
	n := applyComputerUseCaptions("not-a-png", []computeruse.MarkedElement{
		{Name: "", BBox: [4]int{0, 0, 8, 8}},
	})
	if n != 0 || called {
		t.Fatalf("vision chat must skip caption (n=%d called=%v)", n, called)
	}
}

func TestApplyComputerUseCaptionsSkippedWhenUnset(t *testing.T) {
	setupComputerUseCaptionTest(t)
	computerUseVisionFn = func() bool { return false }
	computerUseCaptionConfigFn = func() (corelib.MaclawLLMConfig, bool) {
		return corelib.MaclawLLMConfig{}, false
	}
	n := applyComputerUseCaptions("x", []computeruse.MarkedElement{{Name: "", BBox: [4]int{0, 0, 8, 8}}})
	if n != 0 {
		t.Fatalf("unset caption applied %d", n)
	}
}

func TestApplyComputerUseCaptionsSkipsConfigWhenAllLabeled(t *testing.T) {
	called := false
	setupComputerUseCaptionTest(t)
	computerUseVisionFn = func() bool { return false }
	computerUseCaptionConfigFn = func() (corelib.MaclawLLMConfig, bool) {
		called = true
		return corelib.MaclawLLMConfig{URL: "http://127.0.0.1:1", Key: "k", Model: "caption-model"}, true
	}
	n := applyComputerUseCaptions("not-a-png", []computeruse.MarkedElement{
		{Name: "OK", Type: "button", BBox: [4]int{0, 0, 40, 20}},
	})
	if n != 0 || called {
		t.Fatalf("labeled observe must not load caption config (n=%d called=%v)", n, called)
	}
}

func TestApplyComputerUseCaptionsCancelsOnUnauthorized(t *testing.T) {
	testCaptionBatchAbortsOnStatus(t, http.StatusUnauthorized)
}

func TestApplyComputerUseCaptionsCancelsOnTooManyRequests(t *testing.T) {
	testCaptionBatchAbortsOnStatus(t, http.StatusTooManyRequests)
}

func testCaptionBatchAbortsOnStatus(t *testing.T, status int) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"stop"}`))
	}))
	t.Cleanup(srv.Close)
	setupComputerUseCaptionTest(t)
	computerUseVisionFn = func() bool { return false }
	computerUseCaptionConfigFn = func() (corelib.MaclawLLMConfig, bool) {
		return corelib.MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "caption-model", Protocol: "openai"}, true
	}

	marks := make([]computeruse.MarkedElement, computeruse.DefaultCaptionMaxBoxes)
	for i := range marks {
		marks[i] = computeruse.MarkedElement{
			Ref:  fmt.Sprintf("e%d", i),
			Name: "",
			BBox: [4]int{i * 2, i * 2, 10, 10},
		}
	}
	n := applyComputerUseCaptions(tinyCaptionPNG(t, 64, 64), marks)
	if n != 0 {
		t.Fatalf("status %d applied %d captions", status, n)
	}
	got := hits.Load()
	if got < 1 {
		t.Fatal("expected at least one caption request")
	}
	if got >= int32(len(marks)) {
		t.Fatalf("%d should cancel remaining caption calls, hits=%d boxes=%d", status, got, len(marks))
	}
}

func TestDecodeObserveImageAcceptsJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	got, err := decodePNGBase64(base64.StdEncoding.EncodeToString(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 24 || got.Bounds().Dy() != 16 {
		t.Fatalf("jpeg decode bounds=%v", got.Bounds())
	}
	raw, err := decodePNGBase64(base64.RawStdEncoding.EncodeToString(buf.Bytes()))
	if err != nil {
		t.Fatalf("unpadded jpeg: %v", err)
	}
	if raw.Bounds().Dx() != 24 || raw.Bounds().Dy() != 16 {
		t.Fatalf("unpadded jpeg bounds=%v", raw.Bounds())
	}
}

func TestShouldAbortCaptionBatch(t *testing.T) {
	if !shouldAbortCaptionBatch(fmt.Errorf("caption HTTP 401")) {
		t.Fatal("401")
	}
	if !shouldAbortCaptionBatch(fmt.Errorf("HTTP 403: body_len=12")) {
		t.Fatal("403")
	}
	if !shouldAbortCaptionBatch(&llm.HTTPStatusError{StatusCode: http.StatusTooManyRequests}) {
		t.Fatal("429")
	}
	if shouldAbortCaptionBatch(fmt.Errorf("caption HTTP 500")) {
		t.Fatal("500 is not fatal for the rest of the batch")
	}
	if shouldAbortCaptionBatch(fmt.Errorf("timeout 4010ms")) {
		t.Fatal("4010 must not look like HTTP 401")
	}
	if shouldAbortCaptionBatch(&llm.HTTPStatusError{StatusCode: 4010}) {
		t.Fatal("status 4010 is not 401")
	}
	if !shouldAbortCaptionBatch(&llm.HTTPStatusError{StatusCode: http.StatusUnauthorized}) {
		t.Fatal("structured 401")
	}
}

func TestCaptionUsesAnthropicProtocolOrWireAPI(t *testing.T) {
	if captionUsesAnthropic(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses"}) {
		t.Fatal("openai responses is not anthropic")
	}
	if !captionUsesAnthropic(corelib.MaclawLLMConfig{Protocol: "anthropic"}) {
		t.Fatal("protocol anthropic")
	}
	if !captionUsesAnthropic(corelib.MaclawLLMConfig{WireAPI: "anthropic"}) {
		t.Fatal("wire_api anthropic with empty protocol")
	}
}

func TestCaptionRequestConfigDisablesThinkingWhenNeeded(t *testing.T) {
	auto := captionRequestConfig(corelib.MaclawLLMConfig{URL: "https://api.example.test/v1", Model: "llava", ThinkingMode: ""})
	if auto.ThinkingMode != "" {
		t.Fatalf("generic auto should stay auto, got %q", auto.ThinkingMode)
	}
	on := captionRequestConfig(corelib.MaclawLLMConfig{URL: "https://api.anthropic.com", Model: "claude-sonnet", ThinkingMode: "enabled"})
	if on.ThinkingMode != "disabled" {
		t.Fatalf("explicit thinking should be disabled, got %q", on.ThinkingMode)
	}
	ds := captionRequestConfig(corelib.MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-reasoner", ThinkingMode: ""})
	if ds.ThinkingMode != "disabled" {
		t.Fatalf("deepseek auto-thinking should be disabled, got %q", ds.ThinkingMode)
	}
	glm := captionRequestConfig(corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3", ThinkingMode: "enabled"})
	if glm.ThinkingMode != "enabled" {
		t.Fatalf("glm-5.3 caption must keep always-on thinking, got %q", glm.ThinkingMode)
	}
	glmOff := captionRequestConfig(corelib.MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3", ThinkingMode: "disabled"})
	if glmOff.ThinkingMode != "enabled" {
		t.Fatalf("glm-5.3 caption must coerce disabled thinking, got %q", glmOff.ThinkingMode)
	}
}

func TestCropImageBoxDownscalesLargeBoxes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{R: 8, G: 16, B: 24, A: 255})
		}
	}
	b64, err := cropImageBoxBase64(img, [4]int{0, 0, 400, 300}, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodePNGBase64(b64)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() > computerUseCaptionMaxCropEdge || got.Bounds().Dy() > computerUseCaptionMaxCropEdge {
		t.Fatalf("crop not downscaled: %v", got.Bounds())
	}
	if got.Bounds().Dx() < 2 || got.Bounds().Dy() < 2 {
		t.Fatalf("crop collapsed: %v", got.Bounds())
	}
}

func tinyCaptionPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 80, B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}
