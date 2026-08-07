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
		typ := el.Type
		if typ == "" {
			typ = "interactable"
		}
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
