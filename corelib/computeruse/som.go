package computeruse

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// BuildMarks assigns e0..eN refs, associates OCR text to boxes, and computes centers.
func BuildMarks(elements []taskengine.UIElement, ocr []taskengine.OCRResult) []MarkedElement {
	if len(elements) == 0 {
		return nil
	}
	out := make([]MarkedElement, 0, len(elements))
	for i, el := range elements {
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
		typ := el.Type
		if typ == "" {
			typ = "interactable"
		}
		out = append(out, MarkedElement{
			Ref:          fmt.Sprintf("e%d", i),
			Type:         typ,
			Name:         name,
			Value:        el.Value,
			BBox:         el.BBox,
			CenterX:      cx,
			CenterY:      cy,
			Confidence:   el.Confidence,
			Source:       el.Source,
			Interactable: el.Interactable || el.Source == "yolo",
		})
	}
	return out
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

// associateOCRLabel picks the OCR string whose box center is closest to the element
// and lies roughly inside/near the element bbox.
func associateOCRLabel(bbox [4]int, ocr []taskengine.OCRResult) string {
	ex := bbox[0] + bbox[2]/2
	ey := bbox[1] + bbox[3]/2
	best := ""
	bestDist := int64(1 << 62)
	// Expand search region slightly around the element.
	pad := 24
	minX, minY := bbox[0]-pad, bbox[1]-pad
	maxX, maxY := bbox[0]+bbox[2]+pad, bbox[1]+bbox[3]+pad

	for _, r := range ocr {
		text := strings.TrimSpace(r.Text)
		if text == "" {
			continue
		}
		ox := r.BBox[0] + r.BBox[2]/2
		oy := r.BBox[1] + r.BBox[3]/2
		// Prefer OCR whose center is inside expanded element box.
		inside := ox >= minX && ox <= maxX && oy >= minY && oy <= maxY
		dx := int64(ox - ex)
		dy := int64(oy - ey)
		dist := dx*dx + dy*dy
		if inside {
			dist = dist / 4 // bias toward inside
		} else if dist > int64(120*120) {
			continue
		}
		if dist < bestDist {
			bestDist = dist
			best = text
		}
	}
	// Cap label length for tool text.
	if len([]rune(best)) > 40 {
		best = string([]rune(best)[:40]) + "…"
	}
	return best
}

// FormatOCRExcerpt joins OCR results into a bounded text block for the model.
func FormatOCRExcerpt(ocr []taskengine.OCRResult, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 2000
	}
	if len(ocr) == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range ocr {
		t := strings.TrimSpace(r.Text)
		if t == "" {
			continue
		}
		if i > 0 && b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t)
		if b.Len() >= maxChars {
			s := b.String()
			if len(s) > maxChars {
				return s[:maxChars] + "…"
			}
			return s + "…"
		}
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
	b.WriteString(fmt.Sprintf("mode=%s screen=%dx%d scale=%.2f screen_index=%d\n",
		res.Mode, res.Meta.Width, res.Meta.Height, res.Meta.ScaleFactor, res.Meta.ScreenIndex))
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
		b.WriteString(fmt.Sprintf(", showing first %d", show))
	}
	b.WriteString("):\n")
	for i := 0; i < show; i++ {
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
	b.WriteString("hint: Use computer_click with ref=eN (or computer_type/key/scroll). ")
	b.WriteString("Do NOT invent pixel coordinates. Re-observe after every action. ")
	b.WriteString("You may be a text-only model — screenshots are NOT included in this result. ")
	b.WriteString("Default observe uses primary monitor (screen_index=0); pass -1 only for all monitors stitched.\n")
	return b.String()
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
