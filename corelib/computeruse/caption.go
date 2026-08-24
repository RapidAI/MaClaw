package computeruse

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// Caption is a short label from a dedicated vision/caption model for one
// unlabeled SoM box. It is local observe metadata, never sent as a chat image.
type Caption struct {
	Name string
	Type string
}

// DefaultCaptionMaxBoxes caps how many unlabeled crops are sent per observe.
const DefaultCaptionMaxBoxes = 12

// NeedsCaption reports whether a mark still lacks a human label after YOLO
// synthetic names have been stripped and OCR/a11y association has run.
func NeedsCaption(name string) bool {
	return isSyntheticName(strings.TrimSpace(name))
}

const captionMinBoxArea = 16

// UnlabeledCaptionIndices returns marks that still need a caption. YOLO boxes
// come first (they are the ones OCR/a11y usually miss); dust-sized boxes are skipped.
func UnlabeledCaptionIndices(marks []MarkedElement, max int) []int {
	if max <= 0 {
		max = DefaultCaptionMaxBoxes
	}
	var yolo, rest []int
	for i, m := range marks {
		if !NeedsCaption(m.Name) {
			continue
		}
		if m.BBox[2] <= 0 || m.BBox[3] <= 0 || int64(m.BBox[2])*int64(m.BBox[3]) < captionMinBoxArea {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(m.Source), "yolo") {
			yolo = append(yolo, i)
		} else {
			rest = append(rest, i)
		}
	}
	out := append(yolo, rest...)
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func inferCaptionType(m MarkedElement, name string) string {
	return InferElementType(taskengine.UIElement{
		Type:     m.Type,
		BBox:     m.BBox,
		Patterns: m.Patterns,
		Source:   m.Source,
	}, name)
}

func closedCaptionType(raw string) string {
	t := normalizeElementType(raw)
	switch t {
	case "button", "edit", "icon", "checkbox", "radio", "link", "menu", "tab", "listitem", "treeitem", "combo", "slider":
		return t
	default:
		return ""
	}
}

// ApplyCaption writes a caption-model result onto a mark. Empty/synthetic
// names are ignored so a failed caption cannot clobber heuristic type.
// Closed accessibility types (checkbox, edit, …) are kept; YOLO geometry
// guesses (icon/interactable on unlabeled boxes) may still be replaced.
func ApplyCaption(m *MarkedElement, cap Caption) bool {
	if m == nil {
		return false
	}
	changed := false
	hadSyntheticName := NeedsCaption(m.Name)
	name := strings.TrimSpace(cap.Name)
	if name != "" && !NeedsCaption(name) {
		name = capOCRLabel(name)
		if name != m.Name {
			m.Name = name
			changed = true
		}
	}
	if t := closedCaptionType(cap.Type); t != "" {
		if captionMayReplaceType(*m, hadSyntheticName) && t != m.Type {
			m.Type = t
			changed = true
		}
	} else if m.Name != "" && !NeedsCaption(m.Name) {
		if inf := inferCaptionType(*m, m.Name); inf != "" && inf != m.Type {
			m.Type = inf
			changed = true
		}
	}
	return changed
}

func captionMayReplaceType(m MarkedElement, hadSyntheticName bool) bool {
	existing := closedCaptionType(m.Type)
	if existing == "" {
		return true
	}
	// Unlabeled YOLO boxes are typed from size (icon) or left interactable.
	// Caption may correct those guesses; it must not overwrite a11y roles.
	return hadSyntheticName && existing == "icon"
}

// ParseCaptionResponse accepts JSON {"name","type"} (possibly wrapped in prose
// or a markdown fence) or a short plain label.
func ParseCaptionResponse(raw string) Caption {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Caption{}
	}
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```JSON")
		raw = strings.TrimPrefix(raw, "```")
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
	}
	var parsed struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Label string `json:"label"`
	}
	payload := raw
	if obj := extractJSONObject(raw); obj != "" {
		payload = obj
	}
	if json.Unmarshal([]byte(payload), &parsed) == nil {
		name := strings.TrimSpace(parsed.Name)
		if name == "" {
			name = strings.TrimSpace(parsed.Label)
		}
		if NeedsCaption(name) {
			name = ""
		}
		typ := closedCaptionType(parsed.Type)
		if name == "" && typ == "" {
			return Caption{}
		}
		return Caption{Name: name, Type: typ}
	}
	line := strings.TrimSpace(strings.SplitN(raw, "\n", 2)[0])
	line = strings.Trim(line, `"'`)
	if n := utf8.RuneCountInString(line); n > 40 {
		line = string([]rune(line)[:40])
	}
	if NeedsCaption(line) || strings.Contains(line, "{") {
		return Caption{}
	}
	return Caption{Name: line}
}

func extractJSONObject(raw string) string {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

// InferElementType maps a detection to a coarse UI kind so text models see
// button/edit/icon instead of a generic "interactable" YOLO class.
func InferElementType(el taskengine.UIElement, name string) string {
	t := normalizeElementType(el.Type)
	if t != "" && t != "interactable" {
		return t
	}
	for _, p := range el.Patterns {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "value":
			return "edit"
		case "invoke":
			if name != "" {
				return "button"
			}
		case "toggle":
			return "checkbox"
		case "select":
			return "listitem"
		case "expand":
			return "treeitem"
		}
	}
	w, h := el.BBox[2], el.BBox[3]
	if w > 0 && h > 0 && w <= 48 && h <= 48 && name == "" {
		return "icon"
	}
	n := strings.ToLower(strings.TrimSpace(name))
	if looksLikeEditName(n) || (h > 0 && h <= 36 && w >= 80 && hasPattern(el, "value")) {
		return "edit"
	}
	if n != "" {
		return "button"
	}
	if w > 0 && h > 0 && w <= 64 && h <= 64 {
		return "icon"
	}
	return "interactable"
}

func normalizeElementType(raw string) string {
	t := strings.ToLower(strings.TrimSpace(raw))
	t = strings.TrimPrefix(t, "ax")
	t = strings.TrimPrefix(t, "controltype.")
	switch t {
	case "button", "pushbutton":
		return "button"
	case "edit", "textfield", "textarea", "document":
		return "edit"
	case "checkbox", "check":
		return "checkbox"
	case "radiobutton", "radio":
		return "radio"
	case "link", "hyperlink":
		return "link"
	case "menuitem", "menu":
		return "menu"
	case "tab", "tabitem":
		return "tab"
	case "listitem":
		return "listitem"
	case "treeitem":
		return "treeitem"
	case "combo", "combobox", "dropdown":
		return "combo"
	case "slider":
		return "slider"
	case "image", "icon":
		return "icon"
	case "interactable":
		return "interactable"
	default:
		return t
	}
}

func looksLikeEditName(n string) bool {
	if n == "" {
		return false
	}
	for _, k := range []string{"search", "搜索", "查找", "输入", "edit", "input"} {
		if strings.Contains(n, k) {
			return true
		}
	}
	return false
}

func hasPattern(el taskengine.UIElement, want string) bool {
	want = strings.ToLower(want)
	for _, p := range el.Patterns {
		if strings.ToLower(p) == want {
			return true
		}
	}
	return false
}
