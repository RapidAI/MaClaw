package computeruse

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// BuildMarks assigns e0..eN refs, associates OCR text to boxes, and computes centers.
// YOLO boxes already covered by a same-size accessibility element are dropped so
// dense screens don't waste the text budget on duplicate refs.
func BuildMarks(elements []taskengine.UIElement, ocr []taskengine.OCRResult) []MarkedElement {
	if len(elements) == 0 {
		return nil
	}
	marks := make([]MarkedElement, 0, len(elements))
	for _, el := range elements {
		name := strings.TrimSpace(el.Name)
		// Drop synthetic YOLO names like element_0 so OCR can fill them.
		if isSyntheticName(name) {
			name = ""
		}
		if name == "" && el.Value != "" {
			name = strings.TrimSpace(el.Value)
		}
		if name == "" && len(ocr) > 0 {
			name = associateOCRLabel(el.BBox, ocr)
		}
		cx := el.BBox[0] + el.BBox[2]/2
		cy := el.BBox[1] + el.BBox[3]/2
		typ := InferElementType(el, name)
		marks = append(marks, MarkedElement{
			Type:         typ,
			Name:         name,
			Value:        el.Value,
			BBox:         el.BBox,
			CenterX:      cx,
			CenterY:      cy,
			Confidence:   el.Confidence,
			Source:       el.Source,
			Interactable: el.Interactable || el.Source == "yolo",
			Handle:       el.Handle,
			Patterns:     append([]string(nil), el.Patterns...),
		})
	}
	marks = dedupeCoveredYoloMarks(marks)
	out := make([]MarkedElement, len(marks))
	for i, m := range marks {
		m.Ref = fmt.Sprintf("e%d", i)
		out[i] = m
	}
	return out
}

// dedupeCoveredYoloMarks drops YOLO marks whose center lies inside an
// accessibility mark of comparable size (the a11y entry carries the semantics).
// Large container panes are ignored via the area ratio guard so buttons inside
// a named panel survive.
func dedupeCoveredYoloMarks(marks []MarkedElement) []MarkedElement {
	out := marks[:0]
	for _, m := range marks {
		if m.Source == "yolo" && coveredByA11yMark(marks, m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func coveredByA11yMark(marks []MarkedElement, m MarkedElement) bool {
	mArea := int64(m.BBox[2]) * int64(m.BBox[3])
	if mArea <= 0 {
		mArea = 1
	}
	for _, other := range marks {
		if other.Source != "accessibility" {
			continue
		}
		// A nameless a11y node carries no semantics — never sacrifice a labeled
		// YOLO mark (e.g. OCR-tagged) for it.
		if other.Name == "" && m.Name != "" {
			continue
		}
		// Skip large containers: only same-scale elements dedupe.
		if otherArea := int64(other.BBox[2]) * int64(other.BBox[3]); otherArea > 4*mArea {
			continue
		}
		if m.CenterX >= other.BBox[0] && m.CenterX <= other.BBox[0]+other.BBox[2] &&
			m.CenterY >= other.BBox[1] && m.CenterY <= other.BBox[1]+other.BBox[3] {
			return true
		}
	}
	return false
}

func isSyntheticName(name string) bool {
	if name == "" {
		return true
	}
	// YOLOScreenParser uses element_%d
	if strings.HasPrefix(name, "element_") {
		rest := strings.TrimPrefix(name, "element_")
		if rest == "" {
			return true
		}
		for _, c := range rest {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// associateOCRLabel prefers the OCR box with the highest IoU against the
// element, then falls back to nearest-center among nearby lines.
func associateOCRLabel(bbox [4]int, ocr []taskengine.OCRResult) string {
	best := ""
	bestIoU := 0.0
	for _, r := range ocr {
		text := strings.TrimSpace(r.Text)
		if text == "" || r.BBox[2] <= 0 || r.BBox[3] <= 0 {
			continue
		}
		iou := bboxIoU(bbox, r.BBox)
		if iou > bestIoU {
			bestIoU = iou
			best = text
		}
	}
	if bestIoU >= 0.1 {
		return capOCRLabel(best)
	}

	ex := bbox[0] + bbox[2]/2
	ey := bbox[1] + bbox[3]/2
	bestDist := int64(1 << 62)
	pad := 24
	minX, minY := bbox[0]-pad, bbox[1]-pad
	maxX, maxY := bbox[0]+bbox[2]+pad, bbox[1]+bbox[3]+pad
	best = ""

	for _, r := range ocr {
		text := strings.TrimSpace(r.Text)
		if text == "" || r.BBox[2] <= 0 || r.BBox[3] <= 0 {
			continue
		}
		ox := r.BBox[0] + r.BBox[2]/2
		oy := r.BBox[1] + r.BBox[3]/2
		inside := ox >= minX && ox <= maxX && oy >= minY && oy <= maxY
		dx := int64(ox - ex)
		dy := int64(oy - ey)
		dist := dx*dx + dy*dy
		if inside {
			dist = dist / 4
		} else if dist > int64(120*120) {
			continue
		}
		if dist < bestDist {
			bestDist = dist
			best = text
		}
	}
	return capOCRLabel(best)
}

func capOCRLabel(best string) string {
	if len([]rune(best)) > 40 {
		return string([]rune(best)[:40]) + "…"
	}
	return best
}

func bboxIoU(a, b [4]int) float64 {
	x1 := a[0]
	if b[0] > x1 {
		x1 = b[0]
	}
	y1 := a[1]
	if b[1] > y1 {
		y1 = b[1]
	}
	x2 := a[0] + a[2]
	if b[0]+b[2] < x2 {
		x2 = b[0] + b[2]
	}
	y2 := a[1] + a[3]
	if b[1]+b[3] < y2 {
		y2 = b[1] + b[3]
	}
	if x2 <= x1 || y2 <= y1 {
		return 0
	}
	inter := float64((x2 - x1) * (y2 - y1))
	areaA := float64(a[2] * a[3])
	areaB := float64(b[2] * b[3])
	union := areaA + areaB - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

// FormatOCRExcerpt joins OCR results into a bounded text block for the model.
// maxChars is a rune budget (not bytes) so CJK screens get their full allowance.
func FormatOCRExcerpt(ocr []taskengine.OCRResult, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 2000
	}
	if len(ocr) == 0 {
		return ""
	}
	var b strings.Builder
	runes := 0
	for _, r := range ocr {
		t := strings.TrimSpace(r.Text)
		if t == "" {
			continue
		}
		n := utf8.RuneCountInString(t)
		if b.Len() > 0 {
			b.WriteByte(' ')
			runes++
		}
		if runes+n > maxChars {
			if rem := maxChars - runes; rem > 0 {
				b.WriteString(string([]rune(t)[:rem]))
			}
			return b.String() + "…"
		}
		b.WriteString(t)
		runes += n
	}
	return b.String()
}

// RenderTextObserve builds the pure-text tool result for text-primary Computer Use.
func RenderTextObserve(res *ObserveResult, maxElements int) string {
	if res == nil {
		return "computer_observe: empty result"
	}
	if maxElements <= 0 {
		maxElements = 80
	}
	var b strings.Builder
	writeObserveMetaHeader(&b, res)
	if len(res.Windows) > 0 {
		b.WriteString("windows:\n")
		for _, w := range res.Windows {
			b.WriteString("  - ")
			b.WriteString(w)
			b.WriteByte('\n')
		}
	}
	n := len(res.Elements)
	show := n
	if show > maxElements {
		show = maxElements
	}
	b.WriteString(fmt.Sprintf("elements (%d", n))
	if show < n {
		b.WriteString(fmt.Sprintf(", showing %d, labeled first", show))
	}
	b.WriteString("):\n")
	// Render labeled elements first: on dense screens the unlabeled YOLO boxes
	// are the least useful entries and the first to be cut by the budget.
	order := make([]int, 0, n)
	for pass := 0; pass < 2; pass++ {
		for i := range res.Elements {
			labeled := res.Elements[i].Name != ""
			if (pass == 0) == labeled {
				order = append(order, i)
			}
		}
	}
	for _, i := range order[:show] {
		el := res.Elements[i]
		name := el.Name
		if name == "" {
			name = "(no label)"
		}
		b.WriteString(fmt.Sprintf("  %s [%s] %q conf=%.2f bbox=%d,%d,%d,%d center=%d,%d src=%s\n",
			el.Ref, el.Type, name, el.Confidence,
			el.BBox[0], el.BBox[1], el.BBox[2], el.BBox[3],
			el.CenterX, el.CenterY, el.Source))
	}
	if res.OCRExcerpt != "" {
		b.WriteString("ocr_excerpt: ")
		b.WriteString(res.OCRExcerpt)
		b.WriteByte('\n')
	}
	if hint := MatchAdapter(res.Windows, res.Meta.CropTitle); hint.Kind != "" {
		fmt.Fprintf(&b, "adapter=%s %s\n", hint.Kind, hint.Advice)
	}
	b.WriteString("hint: Use computer_click with ref=eN (or computer_type/key/scroll/select). ")
	b.WriteString("Do NOT invent pixel coordinates. Re-observe after every action. ")
	b.WriteString("You may be a text-only model — screenshots are NOT included in this result. ")
	b.WriteString("Default observe crops to the focused window; pass screen_index=-1 for all monitors, or screen_index=N for a full monitor.\n")
	return b.String()
}

// RenderVisionObserve builds the text tool result when a screenshot is attached
// for a vision-capable chat model. A compact SoM mark list may be included so
// the attached image's numbered boxes can be clicked as ref=eN.
func RenderVisionObserve(res *ObserveResult) string {
	if res == nil {
		return "computer_observe: empty result"
	}
	var b strings.Builder
	writeObserveMetaHeader(&b, res)
	if res.Meta.VisionWidth > 0 && res.Meta.VisionHeight > 0 &&
		(res.Meta.VisionWidth != res.Meta.Width || res.Meta.VisionHeight != res.Meta.Height) {
		b.WriteString(fmt.Sprintf("image=%dx%d (click x,y in this image; mapped to screen)\n",
			res.Meta.VisionWidth, res.Meta.VisionHeight))
	}
	if len(res.Windows) > 0 {
		b.WriteString("windows:\n")
		for _, w := range res.Windows {
			b.WriteString("  - ")
			b.WriteString(w)
			b.WriteByte('\n')
		}
	}
	if hint := MatchAdapter(res.Windows, res.Meta.CropTitle); hint.Kind != "" {
		fmt.Fprintf(&b, "adapter=%s %s\n", hint.Kind, hint.Advice)
	}
	b.WriteString("perception=llm_vision (OmniParser/OCR skipped; a11y marks drawn on screenshot)\n")
	if n := len(res.Elements); n > 0 {
		show := n
		if show > 40 {
			show = 40
		}
		fmt.Fprintf(&b, "som_marks=%d (boxes drawn on the attached image)\n", n)
		for i := 0; i < show; i++ {
			el := res.Elements[i]
			name := el.Name
			if name == "" {
				name = "(no label)"
			}
			fmt.Fprintf(&b, "  %s [%s] %q center=%d,%d\n", el.Ref, el.Type, name, el.CenterX, el.CenterY)
		}
	}
	b.WriteString("screenshot: attached in the following message. Look at the image.\n")
	b.WriteString("hint: Click with computer_click x,y in screenshot pixel space, or ref=eN for a drawn mark. ")
	b.WriteString("Then re-observe.\n")
	return b.String()
}

func writeObserveMetaHeader(b *strings.Builder, res *ObserveResult) {
	fmt.Fprintf(b, "mode=%s screen=%dx%d scale=%.2f screen_index=%d",
		res.Mode, res.Meta.Width, res.Meta.Height, res.Meta.ScaleFactor, res.Meta.ScreenIndex)
	if res.Meta.CropTitle != "" {
		fmt.Fprintf(b, " crop=%q origin=%d,%d", res.Meta.CropTitle, res.Meta.OriginX, res.Meta.OriginY)
	}
	b.WriteByte('\n')
}

// MapVisionClick converts vision-image coordinates into capture/screen space.
func MapVisionClick(meta ScreenMeta, x, y int) (int, int) {
	vw, vh := meta.VisionWidth, meta.VisionHeight
	if vw <= 0 || vh <= 0 || meta.Width <= 0 || meta.Height <= 0 {
		return x, y
	}
	if vw == meta.Width && vh == meta.Height {
		return x, y
	}
	return x * meta.Width / vw, y * meta.Height / vh
}

// ResolveRef finds a marked element by ref (e.g. "e3" or "3").
func ResolveRef(elements []MarkedElement, ref string) (*MarkedElement, error) {
	ref = strings.TrimSpace(strings.ToLower(ref))
	if ref == "" {
		return nil, fmt.Errorf("empty ref")
	}
	if !strings.HasPrefix(ref, "e") {
		ref = "e" + ref
	}
	for i := range elements {
		if strings.ToLower(elements[i].Ref) == ref {
			el := elements[i]
			return &el, nil
		}
	}
	return nil, fmt.Errorf("stale_ref or unknown ref %q — call computer_observe again", ref)
}
