package toolresult

import "encoding/json"

// PreviewCodeNavigation keeps the source and continue_with of a code_navigation
// payload. Candidates repeat lines already in that source, so they are removed
// before any of the source is cut. A payload that is not this JSON returns ""
// and the caller uses the generic preview.
func PreviewCodeNavigation(content string, budget int) string {
	if budget < 512 {
		budget = 512
	}
	if len(content) <= budget {
		return content
	}
	var payload struct {
		Backend      string          `json:"backend"`
		Operation    string          `json:"operation"`
		Query        string          `json:"query"`
		Output       string          `json:"output"`
		Truncated    bool            `json:"truncated,omitempty"`
		ContinueWith string          `json:"continue_with,omitempty"`
		Candidates   json.RawMessage `json:"candidates,omitempty"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil || payload.Backend == "" {
		return ""
	}
	const note = "\n\n[... middle of this codegraph result omitted ...]\n\n"
	original := payload.Output
	wasTruncated := payload.Truncated
	candidates := payload.Candidates
	fit := func(head, tail string, cands json.RawMessage) (string, bool) {
		out, omitted := codeNavigationPreviewWindows(original, head, tail, note)
		payload.Output = out
		payload.Truncated = wasTruncated || omitted
		payload.Candidates = cands
		encoded, err := json.Marshal(&payload)
		if err != nil {
			return "", false
		}
		return string(encoded), true
	}
	if encoded, ok := fit(original, "", candidates); ok && len(encoded) <= budget {
		return encoded
	}
	if len(candidates) > 0 {
		if encoded, ok := fit(original, "", nil); ok && len(encoded) <= budget {
			return encoded
		}
	}

	tailKeep := 1800
	if maxTail := budget / 2; tailKeep > maxTail {
		tailKeep = maxTail
	}
	tail := utf8Suffix(original, tailKeep)
	headRoom := len(original)
	if headRoom > budget {
		headRoom = budget
	}
	// The full source did not fit even after the candidates were removed.
	// Putting the candidates back would cut the lines the model edits.
	for attempt := 0; attempt < 16; attempt++ {
		encoded, ok := fit(utf8Prefix(original, headRoom), tail, nil)
		if !ok {
			return ""
		}
		if len(encoded) <= budget {
			return encoded
		}
		if headRoom == 0 {
			if len(tail) <= 64 {
				return ""
			}
			tail = utf8Suffix(original, len(tail)/2)
			continue
		}
		over := len(encoded) - budget + 32
		if headRoom <= over {
			headRoom /= 2
		} else {
			headRoom -= over
		}
	}
	return ""
}

// codeNavigationPreviewWindows joins a head and a tail of output. A real gap
// keeps the omission note between them. Windows that meet return the original text.
func codeNavigationPreviewWindows(original, head, tail, note string) (string, bool) {
	if len(head) >= len(original) {
		return original, false
	}
	if tail != "" && len(head)+len(tail) >= len(original) {
		return original, false
	}
	return head + note + tail, true
}
