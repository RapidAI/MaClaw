package toolresult

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// StructuredPreview builds a tool-aware compact preview within limit bytes.
// OpenSquilla-inspired structured projection: different shapes keep the most
// useful slices for the model (terminal tail, JSON keys, diff headers, …).
func StructuredPreview(toolName, content string, limit int) string {
	if limit <= 0 {
		limit = 4096
	}
	if content == "" {
		return ""
	}
	if len(content) <= limit {
		return content
	}

	tool := strings.ToLower(strings.TrimSpace(toolName))
	switch {
	case tool == "bash" || tool == "get_session_output" ||
		strings.HasPrefix(tool, "ssh") || tool == "run_terminal_command":
		return previewTerminal(content, limit)
	case tool == "web_fetch":
		return previewWebFetch(content, limit)
	case strings.HasPrefix(tool, "browser"):
		return previewHeadTail(content, limit, 0.45, 0.55)
	case looksLikeDiff(content):
		return previewDiff(content, limit)
	case looksLikeJSON(content):
		if p := previewJSON(content, limit); p != "" {
			return p
		}
	}
	return DefaultPreview(content, limit)
}

// previewTerminal keeps a short head and a long tail (recent log lines matter).
func previewTerminal(s string, limit int) string {
	return previewHeadTail(s, limit, 0.25, 0.75)
}

func previewHeadTail(s string, limit int, headFrac, tailFrac float64) string {
	if len(s) <= limit {
		return s
	}
	sep := fmt.Sprintf("\n\n... (已截断，共 %d 字节) ...\n\n", len(s))
	budget := limit - len(sep)
	if budget < 64 {
		return s[:limit]
	}
	if headFrac < 0 {
		headFrac = 0
	}
	if tailFrac < 0 {
		tailFrac = 0
	}
	sum := headFrac + tailFrac
	if sum <= 0 {
		return DefaultPreview(s, limit)
	}
	headLen := int(float64(budget) * headFrac / sum)
	if headLen < 32 {
		headLen = 32
	}
	if headLen > budget-32 {
		headLen = budget - 32
	}
	tailLen := budget - headLen
	return s[:headLen] + sep + s[len(s)-tailLen:]
}

// previewWebFetch prefers keeping trailing integrity markers when present.
func previewWebFetch(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	marker := "\n\n--- 完整性信号 ---\n"
	idx := strings.LastIndex(s, marker)
	if idx < 0 {
		return previewHeadTail(s, limit, 0.66, 0.34)
	}
	meta := s[idx:]
	head := s[:idx]
	sep := fmt.Sprintf("\n\n... (已截断，共 %d 字节) ...\n\n", len(s))
	if len(meta)+len(sep) >= limit {
		keep := limit - len(sep)
		if keep < 1 {
			return s[len(s)-limit:]
		}
		if keep > len(meta) {
			keep = len(meta)
		}
		return sep + meta[len(meta)-keep:]
	}
	headBudget := limit - len(meta) - len(sep)
	if headBudget <= 0 {
		return sep + meta
	}
	if len(head) > headBudget {
		head = head[:headBudget]
	}
	return head + sep + meta
}

func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	return (t[0] == '{' && strings.Contains(t, "}")) || (t[0] == '[' && strings.Contains(t, "]"))
}

func previewJSON(s string, limit int) string {
	t := strings.TrimSpace(s)
	var v interface{}
	if err := json.Unmarshal([]byte(t), &v); err != nil {
		return ""
	}
	// Compact summary of top-level structure for the model.
	var b strings.Builder
	b.WriteString("[json_preview]\n")
	switch x := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		// Stable-ish: sort would need import; keep map iteration + cap.
		fmt.Fprintf(&b, "type: object keys=%d\n", len(keys))
		n := 0
		for k, val := range x {
			if n >= 24 {
				fmt.Fprintf(&b, "… +%d more keys\n", len(keys)-n)
				break
			}
			fmt.Fprintf(&b, "- %s: %s\n", k, jsonValueSketch(val, 80))
			n++
		}
	case []interface{}:
		fmt.Fprintf(&b, "type: array len=%d\n", len(x))
		for i := 0; i < len(x) && i < 12; i++ {
			fmt.Fprintf(&b, "[%d]: %s\n", i, jsonValueSketch(x[i], 80))
		}
		if len(x) > 12 {
			fmt.Fprintf(&b, "… +%d more items\n", len(x)-12)
		}
	default:
		return ""
	}
	out := b.String()
	if len(out) > limit {
		return out[:limit-20] + "\n…(json preview truncated)\n"
	}
	// If summary is tiny, append a short raw head for context.
	if len(out) < limit/3 {
		rawBudget := limit - len(out) - 40
		if rawBudget > 64 {
			raw := t
			if len(raw) > rawBudget {
				raw = raw[:rawBudget] + "…"
			}
			out = out + "\n[raw_head]\n" + raw
		}
	}
	return out
}

func jsonValueSketch(v interface{}, max int) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return fmt.Sprintf("%v", x)
	case float64:
		return fmt.Sprintf("%g", x)
	case string:
		s := x
		if len(s) > max {
			s = s[:max] + "…"
		}
		return fmt.Sprintf("%q", s)
	case map[string]interface{}:
		return fmt.Sprintf("{object keys=%d}", len(x))
	case []interface{}:
		return fmt.Sprintf("[array len=%d]", len(x))
	default:
		s := fmt.Sprintf("%v", x)
		if len(s) > max {
			s = s[:max] + "…"
		}
		return s
	}
}

func looksLikeDiff(s string) bool {
	// Unified diff or git status-ish.
	if strings.HasPrefix(strings.TrimSpace(s), "diff --git ") {
		return true
	}
	lines := 0
	plus, minus, at := 0, 0, 0
	for _, line := range strings.Split(s, "\n") {
		lines++
		if lines > 40 {
			break
		}
		if strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- ") {
			plus++
		}
		if strings.HasPrefix(line, "@@") {
			at++
		}
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			plus++
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			minus++
		}
	}
	return at >= 1 && (plus+minus) >= 3
}

func previewDiff(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// Keep file headers / hunk headers and a sample of change lines.
	var kept []string
	size := 0
	sep := fmt.Sprintf("\n… (diff truncated, %d bytes total) …\n", len(s))
	budget := limit - len(sep) - 80
	if budget < 128 {
		return DefaultPreview(s, limit)
	}
	for _, line := range strings.Split(s, "\n") {
		keep := false
		switch {
		case strings.HasPrefix(line, "diff --git "),
			strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "--- "),
			strings.HasPrefix(line, "+++ "),
			strings.HasPrefix(line, "@@"):
			keep = true
		case strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-"):
			// Sample change lines but don't keep pure noise forever.
			keep = true
		case isMostlySpace(line):
			keep = false
		default:
			// Context lines: keep sparingly.
			keep = len(kept)%5 == 0
		}
		if !keep {
			continue
		}
		if size+len(line)+1 > budget {
			break
		}
		kept = append(kept, line)
		size += len(line) + 1
	}
	if len(kept) < 3 {
		return DefaultPreview(s, limit)
	}
	out := strings.Join(kept, "\n") + sep
	// Append tail of original (often last file/hunk).
	tailBudget := limit - len(out)
	if tailBudget > 64 {
		tail := s
		if len(tail) > tailBudget {
			tail = tail[len(tail)-tailBudget:]
		}
		out += tail
	}
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

func isMostlySpace(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
