package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// desktopVisionEncodedKey carries the PNG base64 desktopCapture already
// built for a vision-model attachment, so screen vision does not encode
// those same bytes again. A JPEG attachment is not stored here.
type desktopVisionEncodedKey struct{}

// desktopVisionMaxPixels matches the OCR prepare limit. A larger claim is
// not a desktop screenshot this process should decode.
const desktopVisionMaxPixels int64 = 50_000_000

func desktopPixelsTooLarge(w, h int) bool {
	if w <= 0 || h <= 0 {
		return true
	}
	return int64(w)*int64(h) > desktopVisionMaxPixels
}

func desktopPNGTooLarge(data []byte) bool {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return true
	}
	return desktopPixelsTooLarge(cfg.Width, cfg.Height)
}

// desktopScreenVision turns a desktop PNG into text the bot can act on when
// the chat model cannot see images. Nil leaves the screenshot caption unchanged.
var desktopScreenVision func(ctx context.Context, png []byte, clickPixels bool) string

// desktopOCRLines reads the text lines of one screenshot with local OCR
// (no grounding model fallback). Nil when OCR is not installed.
var desktopOCRLines func(ctx context.Context, img []byte) []taskengine.OCRResult

func installDesktopScreenVision(m *srvAIModelManager) {
	desktopOCRLines = func(ctx context.Context, img []byte) []taskengine.OCRResult {
		if m == nil || len(img) == 0 || !m.ocrTierReady() {
			return nil
		}
		found, err := m.recognizeOCR(ctx, base64.StdEncoding.EncodeToString(img))
		if err != nil {
			return nil
		}
		return taskOCRFromBrowser(found)
	}
	desktopScreenVision = func(ctx context.Context, png []byte, clickPixels bool) string {
		if m == nil {
			return ""
		}
		return m.screenVision(ctx, png, clickPixels)
	}
}

func desktopVisionText(ctx context.Context, png []byte, clickPixels bool) string {
	// A vision-capable chat model sees the attached screenshot. Local OCR and
	// OmniParser stay idle, matching GUI Computer Use.
	if desktopContextSeesImages(ctx) || desktopScreenVision == nil || len(png) == 0 {
		return ""
	}
	if ctx != nil && ctx.Err() != nil {
		return ""
	}
	return strings.TrimSpace(desktopScreenVision(ctx, png, clickPixels))
}

// screenVision runs the shared PP-OCRv6 and OmniParser weights on one
// screenshot. Missing weights produce no text; the startup download fills
// them in the same way as the embedding model.
func (m *srvAIModelManager) screenVision(ctx context.Context, png []byte, clickPixels bool) string {
	if m == nil || len(png) == 0 {
		return ""
	}
	if ctx != nil && ctx.Err() != nil {
		return ""
	}
	ocrReady := m.ocrTierReady()
	yoloReady, _ := yolowFileReady(m.modelPath(srvOmniParserFilename))
	yoloReady = yoloReady && desktopScreenYOLOEnabled()
	if !ocrReady && !yoloReady {
		return ""
	}
	if desktopPNGTooLarge(png) {
		return ""
	}
	// One inference at a time. A step that timed out leaves its inference
	// running; stacking another OCR+OmniParser pass on top of it holds two
	// sets of im2col buffers and pushed papers (8 GB) into the OOM killer.
	// A screenshot taken while one is still running goes without vision.
	if !acquireDesktopVision(ctx) {
		log.Printf("[desktop-vision] skipped: another screen_vision is still running")
		return ""
	}
	encoded := desktopVisionEncodedPNG(ctx, png)
	var (
		wg       sync.WaitGroup
		elements []taskengine.UIElement
		lines    []taskengine.OCRResult
	)
	// OmniParser and OCR run one after the other, not side by side: the two
	// passes together peak near 1 GB, more than the service limit allows on
	// top of the loaded models.
	yoloDone := make(chan struct{})
	if yoloReady {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer close(yoloDone)
			if ctx != nil && ctx.Err() != nil {
				return
			}
			found, err := m.detectUI(ctx, encoded)
			if err != nil {
				log.Printf("[desktop-vision] omniparser: %v", err)
				return
			}
			elements = found
		}()
	} else {
		close(yoloDone)
	}
	if ocrReady {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-yoloDone
			if ctx != nil && ctx.Err() != nil {
				return
			}
			found, err := m.recognizeOCR(ctx, encoded)
			if err != nil {
				log.Printf("[desktop-vision] ocr: %v", err)
				return
			}
			lines = taskOCRFromBrowser(found)
		}()
	}
	if ctx == nil {
		wg.Wait()
		releaseDesktopVision()
		return finishScreenVision(elements, lines, png, clickPixels)
	}
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		releaseDesktopVision()
		close(finished)
	}()
	select {
	case <-finished:
		if ctx.Err() != nil {
			return ""
		}
		return finishScreenVision(elements, lines, png, clickPixels)
	case <-ctx.Done():
		// Inference may still be running. The turn is over, so this tool
		// result does not wait for it.
		return ""
	}
}

func desktopVisionEncodedPNG(ctx context.Context, png []byte) string {
	if ctx != nil {
		if encoded, ok := ctx.Value(desktopVisionEncodedKey{}).(string); ok && encoded != "" {
			return encoded
		}
	}
	return base64.StdEncoding.EncodeToString(png)
}

func finishScreenVision(elements []taskengine.UIElement, lines []taskengine.OCRResult, image []byte, clickPixels bool) string {
	if cfg, err := png.DecodeConfig(bytes.NewReader(image)); err == nil {
		elements, lines = clipVisionInputs(elements, lines, cfg.Width, cfg.Height)
	}
	return formatDesktopScreenVision(elements, lines, clickPixels)
}

// clipVisionInputs drops a box that misses the screenshot and pulls an edge
// box back inside it. The center is what app_run click uses, and a center
// past the last pixel is rejected.
func clipVisionInputs(elements []taskengine.UIElement, lines []taskengine.OCRResult, w, h int) ([]taskengine.UIElement, []taskengine.OCRResult) {
	if w <= 0 || h <= 0 {
		return elements, lines
	}
	kept := make([]taskengine.UIElement, 0, len(elements))
	for _, el := range elements {
		box, ok := clipVisionBox(el.BBox, w, h)
		if !ok {
			continue
		}
		el.BBox = box
		kept = append(kept, el)
	}
	elements = kept
	keptLines := make([]taskengine.OCRResult, 0, len(lines))
	for _, line := range lines {
		box, ok := clipVisionBox(line.BBox, w, h)
		if !ok {
			continue
		}
		line.BBox = box
		keptLines = append(keptLines, line)
	}
	return elements, keptLines
}

func clipVisionBox(box [4]int, w, h int) ([4]int, bool) {
	x, y, bw, bh := box[0], box[1], box[2], box[3]
	if bw <= 0 || bh <= 0 || x >= w || y >= h || x+bw <= 0 || y+bh <= 0 {
		return box, false
	}
	if x < 0 {
		bw += x
		x = 0
	}
	if y < 0 {
		bh += y
		y = 0
	}
	if x+bw > w {
		bw = w - x
	}
	if y+bh > h {
		bh = h - y
	}
	if bw <= 0 || bh <= 0 {
		return box, false
	}
	cx, cy := x+bw/2, y+bh/2
	if cx > desktopClickMax || cy > desktopClickMax {
		return box, false
	}
	return [4]int{x, y, bw, bh}, true
}

func taskOCRFromBrowser(lines []browser.OCRResult) []taskengine.OCRResult {
	out := make([]taskengine.OCRResult, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		out = append(out, taskengine.OCRResult{
			Text:       text,
			Confidence: line.Confidence,
			BBox:       line.BBox,
		})
	}
	return out
}

// formatDesktopScreenVision lists OmniParser controls and OCR text with the
// pixel centers app_run click uses. A browser probe passes clickPixels false
// so the text does not invite a pixel click on the page.
func formatDesktopScreenVision(elements []taskengine.UIElement, lines []taskengine.OCRResult, clickPixels bool) string {
	elements = mergeUnboxedOCR(elements, lines)
	marks := computeruse.BuildMarks(elements, lines)
	if len(marks) == 0 && len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("screen_vision:\n")
	if len(marks) > 0 {
		order := visionMarksToShow(marks)
		for _, i := range order {
			el := marks[i]
			name := el.Name
			if name == "" {
				name = "(no label)"
			}
			fmt.Fprintf(&b, "  %s [%s] %q center=%d,%d bbox=%d,%d,%d,%d\n",
				el.Ref, el.Type, name, el.CenterX, el.CenterY,
				el.BBox[0], el.BBox[1], el.BBox[2], el.BBox[3])
		}
		if len(order) < len(marks) {
			fmt.Fprintf(&b, "  … %d more controls\n", len(marks)-len(order))
		}
	}
	if excerpt := visionOCRExcerpt(lines, 400); excerpt != "" {
		b.WriteString("ocr: ")
		b.WriteString(excerpt)
		b.WriteByte('\n')
	}
	if clickPixels {
		b.WriteString("app_run click uses these pixel coordinates. Prefer the center of a labeled control.\n")
	} else {
		b.WriteString("These pixels describe the page. Web clicks still use probe refs.\n")
	}
	return b.String()
}

// mergeUnboxedOCR keeps OmniParser boxes and adds an OCR line only when its
// center is not already inside one of those boxes. Otherwise a label YOLO
// missed has text in the excerpt and no pixel the next click can use.
func mergeUnboxedOCR(elements []taskengine.UIElement, lines []taskengine.OCRResult) []taskengine.UIElement {
	extra := elementsFromOCR(lines)
	if len(elements) == 0 {
		return extra
	}
	if len(extra) == 0 {
		return elements
	}
	out := make([]taskengine.UIElement, len(elements), len(elements)+len(extra))
	copy(out, elements)
	for _, line := range extra {
		if ocrCoveredByElement(line.BBox, elements) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func ocrCoveredByElement(bbox [4]int, elements []taskengine.UIElement) bool {
	cx := bbox[0] + bbox[2]/2
	cy := bbox[1] + bbox[3]/2
	textArea := int64(bbox[2]) * int64(bbox[3])
	if textArea < 1 {
		textArea = 1
	}
	for _, el := range elements {
		if el.BBox[2] <= 0 || el.BBox[3] <= 0 {
			continue
		}
		// A window-sized box is not the click for the words inside it.
		// Same-scale boxes still absorb their own label.
		if int64(el.BBox[2])*int64(el.BBox[3]) > 4*textArea {
			continue
		}
		// associateOCRLabel uses this pad when it sticks text onto a box.
		if cx >= el.BBox[0]-24 && cx <= el.BBox[0]+el.BBox[2]+24 &&
			cy >= el.BBox[1]-24 && cy <= el.BBox[1]+el.BBox[3]+24 {
			return true
		}
	}
	return false
}

func elementsFromOCR(lines []taskengine.OCRResult) []taskengine.UIElement {
	elements := make([]taskengine.UIElement, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line.Text) == "" || line.BBox[2] <= 0 || line.BBox[3] <= 0 {
			continue
		}
		// A reported low score is usually a speckle, not a control. Zero means
		// the engine did not score the line, so it still gets a center.
		if line.Confidence > 0 && line.Confidence < 0.4 {
			continue
		}
		elements = append(elements, taskengine.UIElement{
			Type:         "text",
			Name:         line.Text,
			BBox:         line.BBox,
			Interactable: true,
			Confidence:   line.Confidence,
			Source:       "ocr",
		})
	}
	return elements
}

const desktopVisionMarkLimit = 40

// visionMarksToShow keeps named controls first, then leaves a share of the
// rows for words OmniParser did not box. A full page of either kind must not
// be the only thing the next click can see.
func visionMarksToShow(marks []computeruse.MarkedElement) []int {
	limit := desktopVisionMarkLimit
	if len(marks) < limit {
		limit = len(marks)
	}
	var labeled, plain, text []int
	for i := range marks {
		switch visionMarkRank(marks[i]) {
		case 0:
			labeled = append(labeled, i)
		case 1:
			plain = append(plain, i)
		default:
			text = append(text, i)
		}
	}
	textShare := limit / 4
	if textShare < 8 {
		textShare = 8
	}
	if textShare > len(text) {
		textShare = len(text)
	}
	if textShare > limit {
		textShare = limit
	}
	out := make([]int, 0, limit)
	controlRoom := limit - textShare
	out = appendCapped(out, labeled, controlRoom)
	out = appendCapped(out, plain, controlRoom)
	out = appendCapped(out, text, limit)
	if len(out) < limit {
		seen := make([]bool, len(marks))
		for _, i := range out {
			seen[i] = true
		}
		for _, group := range [][]int{labeled, plain, text} {
			for _, i := range group {
				if len(out) >= limit {
					return out
				}
				if !seen[i] {
					out = append(out, i)
					seen[i] = true
				}
			}
		}
	}
	return out
}

func appendCapped(dst, src []int, limit int) []int {
	for _, i := range src {
		if len(dst) >= limit {
			break
		}
		dst = append(dst, i)
	}
	return dst
}

func visionMarkRank(mark computeruse.MarkedElement) int {
	if mark.Source == "ocr" {
		return 2
	}
	if strings.TrimSpace(mark.Name) == "" {
		return 1
	}
	return 0
}

func visionOCRExcerpt(lines []taskengine.OCRResult, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 400
	}
	var b strings.Builder
	runes := 0
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		n := utf8.RuneCountInString(text)
		sep := 0
		if b.Len() > 0 {
			sep = 3
		}
		if runes+sep+n > maxChars {
			if sep > 0 && runes+sep < maxChars {
				b.WriteString(" | ")
			}
			if rem := maxChars - runes - sep; rem > 0 {
				b.WriteString(string([]rune(text)[:rem]))
			}
			b.WriteString("…")
			break
		}
		if sep > 0 {
			b.WriteString(" | ")
			runes += sep
		}
		b.WriteString(text)
		runes += n
	}
	return b.String()
}

// desktopVisionSlot admits one screen_vision inference process-wide.
var desktopVisionSlot = make(chan struct{}, 1)

// desktopVisionWait is how long a screenshot waits for the slot before it
// goes without vision.
var desktopVisionWait = 15 * time.Second

func acquireDesktopVision(ctx context.Context) bool {
	select {
	case desktopVisionSlot <- struct{}{}:
		return true
	default:
	}
	timer := time.NewTimer(desktopVisionWait)
	defer timer.Stop()
	var done <-chan struct{}
	if ctx != nil {
		done = ctx.Done()
	}
	select {
	case desktopVisionSlot <- struct{}{}:
		return true
	case <-timer.C:
		return false
	case <-done:
		return false
	}
}

// releaseDesktopVision frees the slot and hands the inference buffers
// (hundreds of MB of im2col scratch) back to the OS at once, so a desktop
// run does not sit near the service memory limit between screenshots.
func releaseDesktopVision() {
	select {
	case <-desktopVisionSlot:
	default:
	}
	debug.FreeOSMemory()
}
