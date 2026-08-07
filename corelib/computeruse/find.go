package computeruse

import (
	"strings"
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

// truncateRunes caps s at max runes, appending an ellipsis when truncated.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
