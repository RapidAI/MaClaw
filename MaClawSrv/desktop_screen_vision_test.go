package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/ocr"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestFormatDesktopScreenVisionLabelsControlsAndKeepsBrowserClicksOnRefs(t *testing.T) {
	elements := []taskengine.UIElement{{
		Type:         "interactable",
		Name:         "element_0",
		BBox:         [4]int{100, 200, 80, 40},
		Interactable: true,
		Confidence:   0.9,
		Source:       "yolo",
	}}
	lines := []taskengine.OCRResult{{
		Text:       "确定",
		Confidence: 0.95,
		BBox:       [4]int{100, 200, 80, 40},
	}}

	click := formatDesktopScreenVision(elements, lines, true)
	if !strings.Contains(click, `screen_vision:`) || !strings.Contains(click, `"确定"`) || !strings.Contains(click, "center=140,220") || !strings.Contains(click, "app_run click uses these pixel coordinates.") {
		t.Fatalf("click vision = %q", click)
	}
	if strings.Contains(click, "Do not pixel-click the browser.") {
		t.Fatalf("app vision must not ban pixel clicks: %q", click)
	}

	page := formatDesktopScreenVision(elements, lines, false)
	if !strings.Contains(page, "Web clicks still use probe refs.") || strings.Contains(page, "app_run click uses these pixel coordinates") {
		t.Fatalf("page vision = %q", page)
	}
	if formatDesktopScreenVision(nil, nil, true) != "" {
		t.Fatal("empty vision should stay empty")
	}
	ocrOnly := formatDesktopScreenVision(nil, lines, true)
	if !strings.Contains(ocrOnly, `"确定"`) || !strings.Contains(ocrOnly, "center=140,220") {
		t.Fatalf("ocr-only vision = %q", ocrOnly)
	}
	if strings.Count(click, "center=140,220") != 1 {
		t.Fatalf("ocr text inside a box was listed twice: %q", click)
	}
	far := formatDesktopScreenVision(elements, []taskengine.OCRResult{{
		Text:       "开始",
		Confidence: 0.9,
		BBox:       [4]int{400, 500, 40, 20},
	}}, true)
	if !strings.Contains(far, `"开始"`) || !strings.Contains(far, "center=420,510") {
		t.Fatalf("ocr outside a box has no click: %q", far)
	}
	panel := formatDesktopScreenVision([]taskengine.UIElement{{
		Type:         "interactable",
		Name:         "element_0",
		BBox:         [4]int{0, 0, 800, 600},
		Interactable: true,
		Source:       "yolo",
	}}, []taskengine.OCRResult{{
		Text:       "开始",
		Confidence: 0.9,
		BBox:       [4]int{100, 200, 40, 20},
	}}, true)
	if !strings.Contains(panel, `"开始"`) || !strings.Contains(panel, "center=120,210") {
		t.Fatalf("a window box swallowed the label: %q", panel)
	}
	wordy := make([]taskengine.OCRResult, 45)
	for i := range wordy {
		wordy[i] = taskengine.OCRResult{
			Text:       "字",
			Confidence: 0.9,
			BBox:       [4]int{1000 + i*30, 10, 20, 16},
		}
	}
	capped := formatDesktopScreenVision(elements, wordy, true)
	if !strings.Contains(capped, "center=140,220") {
		t.Fatalf("text lines hid the button: %q", capped)
	}
	boxes := make([]taskengine.UIElement, 40)
	for i := range boxes {
		boxes[i] = taskengine.UIElement{
			Type:         "interactable",
			Name:         "element_0",
			BBox:         [4]int{20 + i*12, 0, 8, 8},
			Interactable: true,
			Source:       "yolo",
		}
	}
	dense := formatDesktopScreenVision(boxes, []taskengine.OCRResult{{
		Text:       "开始",
		Confidence: 0.9,
		BBox:       [4]int{900, 900, 40, 20},
	}}, true)
	if !strings.Contains(dense, `"开始"`) || !strings.Contains(dense, "center=920,910") {
		t.Fatalf("nameless boxes hid the word: %q", dense)
	}
	speck := formatDesktopScreenVision(nil, []taskengine.OCRResult{{
		Text:       "噪",
		Confidence: 0.1,
		BBox:       [4]int{1, 1, 10, 10},
	}}, true)
	if strings.Contains(speck, "center=") || !strings.Contains(speck, "噪") {
		t.Fatalf("speck vision = %q", speck)
	}
}

func TestDesktopPixelsTooLargeRejectsABombAndKeepsAn8KShot(t *testing.T) {
	if desktopPixelsTooLarge(1920, 1080) || desktopPixelsTooLarge(9840, 3840) {
		t.Fatal("a real desktop screenshot was rejected")
	}
	if !desktopPixelsTooLarge(20000, 20000) || !desktopPixelsTooLarge(0, 1080) {
		t.Fatal("an oversized screenshot was accepted")
	}
}

func TestClipVisionBoxKeepsAnEdgeLabelInsideTheScreen(t *testing.T) {
	box, ok := clipVisionBox([4]int{1900, 10, 40, 20}, 1920, 1080)
	if !ok {
		t.Fatal("edge label was dropped")
	}
	cx, cy := box[0]+box[2]/2, box[1]+box[3]/2
	if cx < 0 || cy < 0 || cx >= 1920 || cy >= 1080 || cx > desktopClickMax || cy > desktopClickMax {
		t.Fatalf("center %d,%d is not a click on a %dx%d screen", cx, cy, 1920, 1080)
	}
	if _, ok := clipVisionBox([4]int{-80, -80, 10, 10}, 1920, 1080); ok {
		t.Fatal("a box outside the screenshot was kept")
	}
	inside, ok := clipVisionBox([4]int{100, 200, 80, 40}, 1920, 1080)
	if !ok || inside != [4]int{100, 200, 80, 40} {
		t.Fatalf("in-screen box changed: %#v ok=%v", inside, ok)
	}
}

func TestDesktopVisionReusesTheAttachedPNGEncoding(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G'}
	want := base64.StdEncoding.EncodeToString(png)
	if got := desktopVisionEncodedPNG(nil, png); got != want {
		t.Fatalf("encoded=%q", got)
	}
	ctx := context.WithValue(context.Background(), desktopVisionEncodedKey{}, want)
	if got := desktopVisionEncodedPNG(ctx, []byte("other")); got != want {
		t.Fatalf("attachment encoding was ignored: %q", got)
	}
}

func TestDesktopCaptureAppendsScreenVisionWithoutChangingProbeClicks(t *testing.T) {
	previous := desktopScreenVision
	desktopScreenVision = func(_ context.Context, png []byte, clickPixels bool) string {
		if len(png) == 0 {
			t.Fatal("vision received an empty screenshot")
		}
		if clickPixels {
			return "screen_vision:\n  e0 [button] \"确定\" center=4,3\napp_run click uses these pixel coordinates. Prefer the center of a labeled control.\n"
		}
		return "screen_vision:\n  ocr: 确定\nThese pixels describe the page. Web clicks still use probe refs.\n"
	}
	t.Cleanup(func() { desktopScreenVision = previous })

	shot := desktopTestPNG(t, 11, 7, false)
	previousShot := desktopRemoteScreenshot
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		return shot, nil
	}
	t.Cleanup(func() { desktopRemoteScreenshot = previousShot })

	scope := agentruntime.Scope{TenantID: "tenant-vision", UserID: "alice"}
	ctx, _ := withDesktopShotSlot(context.Background())
	text, err := desktopCapture(ctx, scope, ":20", true, "")
	plain, images := agentruntime.ExtractModelImages(text)
	if err != nil || len(images) != 1 || !strings.Contains(plain, "确定") || !strings.Contains(plain, "center=4,3") {
		t.Fatalf("plain=%q images=%d err=%v", plain, len(images), err)
	}

	seen := context.WithValue(ctx, desktopLLMSeesImagesKey{}, true)
	skipped, err := desktopCapture(seen, scope, ":20", true, "")
	plain, images = agentruntime.ExtractModelImages(skipped)
	if err != nil || len(images) != 1 || strings.Contains(plain, "screen_vision:") || strings.Contains(plain, "确定") {
		t.Fatalf("vision model still ran local eyes: plain=%q images=%d err=%v", plain, len(images), err)
	}

	page, err := desktopCapture(ctx, scope, ":20", false, "")
	plain, images = agentruntime.ExtractModelImages(page)
	if err != nil || len(images) != 1 || strings.Contains(plain, "app_run click uses these pixel coordinates") || !strings.Contains(plain, "probe refs") {
		t.Fatalf("page plain=%q images=%d err=%v", plain, len(images), err)
	}

	previousSaved := desktopRemoteSavedPath
	previousPages := desktopOpenPages
	desktopRemoteSavedPath = func(name string) string { return "/home/desktop/Desktop/" + name }
	desktopOpenPages = func(string, string) string {
		return "Open browser pages (1), including tabs behind the front window:\n- attached Canvas — https://example.com/canvas"
	}
	t.Cleanup(func() {
		desktopRemoteSavedPath = previousSaved
		desktopOpenPages = previousPages
	})
	named, err := desktopCapture(ctx, scope, ":20", true, "shot.png")
	if err != nil {
		t.Fatal(err)
	}
	shown, err := withDesktopPages(named, nil, "http://desktop", "tab")
	plain, _ = agentruntime.ExtractModelImages(shown)
	clickAt := strings.LastIndex(plain, "app_run click uses these pixel coordinates.")
	pagesAt := strings.Index(plain, "Open browser pages")
	centerAt := strings.LastIndex(plain, "center=4,3")
	if err != nil || clickAt < 0 || pagesAt < 0 || centerAt < pagesAt || clickAt < centerAt || !strings.Contains(plain, "Saved /home/desktop/Desktop/shot.png") {
		t.Fatalf("saved shot buried the click instruction: %q err=%v", plain, err)
	}

	// A newline before the attached image used to make that glue the last
	// line, so the page list landed after the coordinates.
	block := "Desktop screenshot of display :20: 11x7 pixels. Saved /home/desktop/Desktop/shot.png.\nscreen_vision:\n  e0 [button] \"确定\" center=9,8\napp_run click uses these pixel coordinates. Prefer the center of a labeled control.\n"
	glued := agentruntime.AttachModelImage(block, "image/png", base64.StdEncoding.EncodeToString(shot))
	shown, err = withDesktopPages(glued, nil, "http://desktop", "tab")
	plain, images = agentruntime.ExtractModelImages(shown)
	clickAt = strings.LastIndex(plain, "app_run click uses these pixel coordinates.")
	pagesAt = strings.Index(plain, "Open browser pages")
	centerAt = strings.LastIndex(plain, "center=9,8")
	if err != nil || len(images) != 1 || clickAt < 0 || pagesAt < 0 || centerAt < pagesAt || clickAt < centerAt {
		t.Fatalf("image glue hid the click line: %q images=%d err=%v", plain, len(images), err)
	}
}

func TestDesktopTurnSeesImagesFollowsTheChatModel(t *testing.T) {
	vision := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Config: corelib.AppConfig{
		MaclawLLMCurrentProvider: "vision",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "vision", URL: "https://llm.example/v1", Key: "k", Model: "see", SupportsVision: true,
		}},
	}}}
	if !desktopTurnSeesImages(vision) || desktopScreenCheckPrompt(true) == desktopScreenCheckPrompt(false) {
		t.Fatal("vision model should look at the screenshot and skip the local reading")
	}
	text := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Config: corelib.AppConfig{
		MaclawLLMCurrentProvider: "text",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name: "text", URL: "https://llm.example/v1", Key: "k", Model: "read",
		}},
	}}}
	if desktopTurnSeesImages(text) || desktopTurnSeesImages(agentruntime.TurnRequest{}) {
		t.Fatal("a text model or an unknown turn should keep local OCR and OmniParser")
	}
}

func TestOCRTierReadyDoesNotWaitForEveryTier(t *testing.T) {
	manager := newSrvAIModelManager(t.TempDir())
	dir := filepath.Dir(manager.modelPath(ocr.DetModelFilename(corelib.DefaultOCRModelTier)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte{0x08, 0x08, 's', 'm', 'a', 'l', 'l'}
	for _, name := range []string{ocr.DetModelFilename(corelib.DefaultOCRModelTier), ocr.RecModelFilename(corelib.DefaultOCRModelTier)} {
		if err := os.WriteFile(filepath.Join(dir, name), payload, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !manager.ocrTierReady() {
		t.Fatal("default OCR tier is installed but vision would skip it")
	}
	if ready, _ := manager.ocrModelsReady(); ready {
		t.Fatal("a partial OCR bundle must stay not-ready so the rest still downloads")
	}
}

func TestDownloadOmniParserDropsACorruptWeight(t *testing.T) {
	t.Setenv("MACLAW_DISABLE_MODEL_DOWNLOADS", "1")
	manager := newSrvAIModelManager(t.TempDir())
	path := manager.modelPath(srvOmniParserFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("this is not a yolow weight file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manager.downloadOmniParser(corelib.AppConfig{}, nil); err == nil {
		t.Fatal("corrupt weight was treated as installed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("corrupt weight remained: %v", err)
	}
}

func TestSrvAIModelManagerInstallsOCRBundleAndOmniParserHeader(t *testing.T) {
	dataRoot := t.TempDir()
	manager := newSrvAIModelManager(dataRoot)
	if ready, _ := manager.ocrModelsReady(); ready {
		t.Fatal("missing OCR models reported ready")
	}
	if ready, _ := yolowFileReady(manager.modelPath(srvOmniParserFilename)); ready {
		t.Fatal("missing omniparser reported ready")
	}
	if got := manager.screenVision(context.Background(), []byte{0x89, 'P', 'N', 'G'}, true); got != "" {
		t.Fatalf("vision before weights are installed = %q", got)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range srvOCRModelFilenames() {
		w, err := zw.Create("nested/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(append([]byte{0x08, 0x08}, []byte("fake onnx "+name)...)); err != nil {
			t.Fatal(err)
		}
	}
	skip, err := zw.Create("dict.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := skip.Write([]byte("ignored")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := manager.modelPath(ocr.ModelsZipFilename)
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractSrvOCRModelsZip(zipPath, filepath.Dir(zipPath)); err != nil {
		t.Fatal(err)
	}
	ready, size := manager.ocrModelsReady()
	if !ready || size <= 0 {
		t.Fatalf("ocr ready=%v size=%d", ready, size)
	}
	detName := ocr.DetModelFilename(corelib.DefaultOCRModelTier)
	detPath := filepath.Join(filepath.Dir(zipPath), detName)
	if err := os.WriteFile(detPath, []byte{0x08, 0x08, 's', 't', 'a', 'l', 'e'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractSrvOCRModelsZip(zipPath, filepath.Dir(zipPath)); err != nil {
		t.Fatal(err)
	}
	replaced, err := os.ReadFile(detPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(replaced, []byte("fake onnx "+detName)) {
		t.Fatalf("re-extract left the stale model: %q", replaced)
	}
	status := manager.statusOne(srvAIModelOCR, corelib.AppConfig{})
	if !status.Enabled || !status.Ready || status.Filename != ocr.DetModelFilename("small") {
		t.Fatalf("ocr status = %#v", status)
	}

	weights := make([]byte, 32)
	copy(weights, "YOLW")
	if err := os.WriteFile(manager.modelPath(srvOmniParserFilename), weights, 0o644); err != nil {
		t.Fatal(err)
	}
	parserStatus := manager.statusOne(srvAIModelOmniParser, corelib.AppConfig{})
	if !parserStatus.Enabled || !parserStatus.Ready || parserStatus.SizeBytes != 32 {
		t.Fatalf("omniparser status = %#v", parserStatus)
	}

	outside := filepath.Join(dataRoot, "escaped.onnx")
	var slip bytes.Buffer
	slipZip := zip.NewWriter(&slip)
	entry, err := slipZip.Create("../escaped.onnx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte{0x08, 0x08, 'x'}); err != nil {
		t.Fatal(err)
	}
	if err := slipZip.Close(); err != nil {
		t.Fatal(err)
	}
	slipPath := filepath.Join(t.TempDir(), ocr.ModelsZipFilename)
	if err := os.WriteFile(slipPath, slip.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := extractSrvOCRModelsZip(slipPath, filepath.Dir(zipPath)); err == nil {
		t.Fatal("zip slip was accepted")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("zip slip wrote %s: %v", outside, err)
	}
}
