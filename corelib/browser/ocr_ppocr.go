package browser

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

// ocrMaxLongEdge caps the longest side of images fed to the OCR engine.
// Full multi-monitor captures (e.g. 9840x3840) are downscaled for OCR and
// bboxes are mapped back to the original coordinate space.
const ocrMaxLongEdge = ocr.DefaultMaxLongEdge

// NativeOCRProvider runs OCR locally via the pure-Go PP-OCRv6 engine
// (corelib/ocr). It replaces the former Python RapidOCR sidecar: the models
// are plain ONNX files auto-downloaded to the shared models directory, and
// recognition runs in-process (thread-safe via ocr.Manager).
type NativeOCRProvider struct {
	detPath string
	recPath string
	logger  func(string)

	mu      sync.Mutex
	manager *ocr.Manager
}

// NewNativeOCRProvider creates a provider wrapping the native PP-OCRv6
// engine. Models are NOT loaded until first Recognize/Warm.
func NewNativeOCRProvider(detPath, recPath string, logger func(string)) *NativeOCRProvider {
	return &NativeOCRProvider{detPath: detPath, recPath: recPath, logger: logger}
}

// Recognize implements OCRProvider.
// Large screenshots are downscaled before OCR; bounding boxes are scaled back
// to the original image coordinate space.
func (p *NativeOCRProvider) Recognize(pngBase64 string) ([]OCRResult, error) {
	if p == nil {
		return nil, fmt.Errorf("OCR provider nil")
	}
	ocrB64, scaleX, scaleY, prepErr := ocr.PrepareImageBase64(pngBase64, ocrMaxLongEdge)
	if prepErr != nil {
		// Fail closed: never replay an unprepared payload (mirrors the old
		// sidecar behavior for multi-monitor captures).
		return nil, fmt.Errorf("OCR prepare: %w", prepErr)
	}
	if scaleX != 1 || scaleY != 1 {
		p.log("OCR image downscaled scale_x=%.3f scale_y=%.3f max_edge=%d", scaleX, scaleY, ocrMaxLongEdge)
	}

	raw, err := base64.StdEncoding.DecodeString(ocrB64)
	if err != nil {
		return nil, fmt.Errorf("decode prepared image: %w", err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode prepared image: %w", err)
	}

	results, err := p.ocrManager().Recognize(img)
	if err != nil {
		return nil, err
	}
	results = ocr.ScaleResults(results, scaleX, scaleY)

	out := make([]OCRResult, len(results))
	for i, r := range results {
		out[i] = OCRResult{
			Text:       r.Text,
			Confidence: r.Confidence,
			BBox:       r.BBox,
		}
	}
	return out, nil
}

// IsAvailable implements OCRProvider: both model files are present and valid.
func (p *NativeOCRProvider) IsAvailable() bool {
	if p == nil {
		return false
	}
	// Snapshot the paths under lock: SetModelPaths may swap them concurrently.
	p.mu.Lock()
	detPath, recPath := p.detPath, p.recPath
	p.mu.Unlock()
	_, detOK := ocr.ModelFileStatus(detPath)
	_, recOK := ocr.ModelFileStatus(recPath)
	return detOK && recOK
}

// Installed reports whether the OCR model files are on disk. Same as
// IsAvailable; kept for call-site compatibility with the old sidecar.
func (p *NativeOCRProvider) Installed() bool {
	return p.IsAvailable()
}

// Ready reports whether the models are currently loaded in memory.
func (p *NativeOCRProvider) Ready() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.manager != nil && p.manager.Loaded()
}

// Warm loads the OCR models if the files are installed. Does not download
// (that stays on the background model download). Used by Computer Use
// startup warmup.
func (p *NativeOCRProvider) Warm() error {
	if p == nil {
		return fmt.Errorf("OCR provider nil")
	}
	if p.managerLoaded() {
		return nil
	}
	if !p.IsAvailable() {
		return fmt.Errorf("OCR not installed yet (will download in background)")
	}
	// Trigger a real load through the manager with a tiny blank image.
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	if _, err := p.ocrManager().Recognize(img); err != nil {
		return fmt.Errorf("OCR warm: %w", err)
	}
	return nil
}

// SetModelPaths updates the det/rec model paths, e.g. after the configured
// OCR model tier changed. When the paths differ, any loaded manager is shut
// down so the next Recognize reloads from the new files; unchanged paths are
// a no-op and keep the warmed models in memory.
func (p *NativeOCRProvider) SetModelPaths(detPath, recPath string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.detPath == detPath && p.recPath == recPath {
		return
	}
	if p.manager != nil {
		p.manager.Shutdown()
		p.manager = nil
	}
	p.detPath = detPath
	p.recPath = recPath
}

// Close implements OCRProvider. Releases the models; subsequent Recognize
// calls reload them.
func (p *NativeOCRProvider) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.manager != nil {
		p.manager.Shutdown()
		p.manager = nil
	}
}

// ── internal ──

func (p *NativeOCRProvider) ocrManager() *ocr.Manager {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.manager == nil {
		p.manager = ocr.NewManager(p.detPath, p.recPath)
	}
	return p.manager
}

func (p *NativeOCRProvider) managerLoaded() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.manager != nil && p.manager.Loaded()
}

func (p *NativeOCRProvider) log(format string, args ...interface{}) {
	if p.logger != nil {
		p.logger(fmt.Sprintf("[ocr-native] "+format, args...))
	}
}
