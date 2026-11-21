package computeruse

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// FindMatches searches the last observation for query, case-insensitively and
// ignoring whitespace differences (CJK-friendly). It searches two layers:
//  1. marked element labels (Name/Value) — the existing ref is returned;
//  2. raw OCR lines kept in-session — hits not covered by any element are
//     returned as synthesized clickable elements (Source="ocr") so text that
//     no YOLO/a11y element covers (e.g. a chat-list row) stays clickable.
//
// Returned elements carry no Ref except those already marked; callers assign
// refs via Session.AppendElements. At most limit results (default 10).
func FindMatches(obs *ObserveResult, query string, limit int) []MarkedElement {
	if obs == nil {
		return nil
	}
	q := normalizeFindQuery(query)
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	out := make([]MarkedElement, 0, limit)

	// Layer 1: existing marked elements whose label matches.
	for _, el := range obs.Elements {
		if len(out) >= limit {
			return out
		}
		if strings.Contains(normalizeFindQuery(el.Name), q) ||
			(el.Value != "" && strings.Contains(normalizeFindQuery(el.Value), q)) {
			out = append(out, el)
		}
	}

	// Layer 2: raw OCR lines not covered by any existing element.
	for _, line := range obs.OCRLines {
		if len(out) >= limit {
			return out
		}
		// Zero-size bbox means the recognizer could not locate the text
		// (typical for vision-LLM OCR). Such a line would synthesize a
		// "clickable" element at (0,0) — a real misclick risk — so it only
		// contributes to the OCR text excerpt, never to clickable matches.
		if line.BBox[2] <= 0 || line.BBox[3] <= 0 {
			continue
		}
		text := strings.TrimSpace(line.Text)
		if text == "" || !strings.Contains(normalizeFindQuery(text), q) {
			continue
		}
		cx := line.BBox[0] + line.BBox[2]/2
		cy := line.BBox[1] + line.BBox[3]/2
		if coveredByAnyElement(obs.Elements, cx, cy) {
			continue
		}
		out = append(out, MarkedElement{
			Type:         "text",
			Name:         truncateRunes(text, 40),
			BBox:         line.BBox,
			CenterX:      cx,
			CenterY:      cy,
			Confidence:   line.Confidence,
			Source:       "ocr",
			Interactable: false,
		})
	}
	return out
}

// normalizeFindQuery lowercases and strips all whitespace so queries match
// across OCR line breaks and spacing artifacts.
func normalizeFindQuery(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}

// coveredByAnyElement reports whether point (x,y) lies inside any element bbox
// (small padding included so borderline OCR hits reuse the element instead).
func coveredByAnyElement(elements []MarkedElement, x, y int) bool {
	const pad = 4
	for _, el := range elements {
		if x >= el.BBox[0]-pad && x <= el.BBox[0]+el.BBox[2]+pad &&
			y >= el.BBox[1]-pad && y <= el.BBox[1]+el.BBox[3]+pad {
			return true
		}
	}
	return false
}

// OCRLinesUsable reports that at least one line has text and a positive box.
// A line with no box cannot be hit-tested, so it must not mark labels current.
func OCRLinesUsable(lines []taskengine.OCRResult) bool {
	for _, line := range lines {
		if strings.TrimSpace(line.Text) != "" && line.BBox[2] > 0 && line.BBox[3] > 0 {
			return true
		}
	}
	return false
}

// OCRClickElements turns boxed OCR lines into clickable controls for a
// lightweight text refresh. A line longer than 80 runes is a paragraph, and a
// line with no box would click the origin, so both are left out. limit caps
// the list; non-positive limit means 24.
func OCRClickElements(lines []taskengine.OCRResult, limit int) []taskengine.UIElement {
	if limit <= 0 {
		limit = 24
	}
	out := make([]taskengine.UIElement, 0, limit)
	for _, line := range lines {
		if len(out) >= limit {
			break
		}
		text := strings.TrimSpace(line.Text)
		if text == "" || line.BBox[2] <= 0 || line.BBox[3] <= 0 || len([]rune(text)) > 80 {
			continue
		}
		out = append(out, taskengine.UIElement{
			Type:         "text",
			Name:         truncateRunes(text, 40),
			BBox:         line.BBox,
			Confidence:   line.Confidence,
			Source:       "ocr",
			Interactable: true,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// truncateRunes caps s at max runes, appending an ellipsis when truncated.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
