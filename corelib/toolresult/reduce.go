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
	case tool == "computer_observe":
		return previewComputerObserve(content, limit)
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
		return utf8Prefix(s, limit)
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
	return utf8Prefix(s, headLen) + sep + utf8Suffix(s, tailLen)
}

// previewComputerObserve keeps meta/windows/OCR and a head+tail sample of eN
// refs so a size cap cannot strip every click target. Older observes are
// folded separately; this is only the overflow first cut.
func previewComputerObserve(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	var meta []string
	var elements []string
	for _, line := range strings.Split(s, "\n") {
		if isComputerObserveElementLine(strings.TrimSpace(line)) {
			elements = append(elements, line)
			continue
		}
		meta = append(meta, line)
	}
	if len(elements) == 0 {
		return previewHeadTail(s, limit, 0.55, 0.45)
	}
	noteBudget := 64
	elShare := limit * 2 / 5
	if elShare < 128 {
		elShare = limit / 3
	}
	if elShare < 64 {
		elShare = limit / 2
	}
	metaBudget := limit - elShare - noteBudget
	if metaBudget < 64 {
		metaBudget = limit / 2
		if metaBudget < 32 {
			metaBudget = 32
		}
	}
	metaText := strings.Join(meta, "\n")
	if len(metaText) > metaBudget {
		metaText = previewHeadTail(metaText, metaBudget, 0.55, 0.45)
	}
	elBudget := limit - len(metaText) - noteBudget - 1
	if elBudget < 8 {
		elBudget = 8
	}
	kept := sampleLinesHeadTail(elements, elBudget)
	omitted := len(elements) - len(kept)
	var b strings.Builder
	b.WriteString(metaText)
	if len(kept) > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Join(kept, "\n"))
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "\n... (%d observe elements omitted)", omitted)
	}
	out := b.String()
	if out == "" {
		return previewHeadTail(s, limit, 0.55, 0.45)
	}
	if len(out) > limit {
		return previewHeadTail(out, limit, 0.6, 0.4)
	}
	return out
}

func isComputerObserveElementLine(trim string) bool {
	return strings.HasPrefix(trim, "e") && strings.Contains(trim, "[") &&
		(strings.Contains(trim, "bbox=") || strings.Contains(trim, "center="))
}

func sampleLinesHeadTail(lines []string, budget int) []string {
	if budget < 8 || len(lines) == 0 {
		return nil
	}
	total := 0
	for _, line := range lines {
		total += len(line) + 1
	}
	if total <= budget {
		return append([]string(nil), lines...)
	}
	headBudget := budget * 2 / 3
	if headBudget < 8 {
		headBudget = budget / 2
	}
	tailBudget := budget - headBudget
	var head []string
	used := 0
	for _, line := range lines {
		need := len(line) + 1
		if used+need > headBudget {
			break
		}
		head = append(head, line)
		used += need
	}
	var tailRev []string
	usedT := 0
	for i := len(lines) - 1; i >= len(head); i-- {
		need := len(lines[i]) + 1
		if usedT+need > tailBudget {
			break
		}
		tailRev = append(tailRev, lines[i])
		usedT += need
	}
	if len(tailRev) == 0 && len(lines) > len(head) {
		last := lines[len(lines)-1]
		need := len(last) + 1
		for len(head) > 0 && used+need > budget {
			used -= len(head[len(head)-1]) + 1
			head = head[:len(head)-1]
		}
		if used+need <= budget {
			tailRev = []string{last}
		}
	}
	for i, j := 0, len(tailRev)-1; i < j; i, j = i+1, j-1 {
		tailRev[i], tailRev[j] = tailRev[j], tailRev[i]
	}
	return append(head, tailRev...)
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
			return utf8Suffix(s, limit)
		}
		if keep > len(meta) {
			keep = len(meta)
		}
		return sep + utf8Suffix(meta, keep)
	}
	headBudget := limit - len(meta) - len(sep)
	if headBudget <= 0 {
		return sep + meta
	}
	if len(head) > headBudget {
		head = utf8Prefix(head, headBudget)
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
		return utf8PrefixWithSuffix(out, "\n…(json preview truncated)\n", limit)
	}
	// If summary is tiny, append a short raw head for context.
	if len(out) < limit/3 {
		rawBudget := limit - len(out) - 40
		if rawBudget > 64 {
			raw := t
			if len(raw) > rawBudget {
				raw = utf8PrefixWithSuffix(raw, "…", rawBudget)
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
			s = utf8PrefixWithSuffix(s, "…", max)
		}
		return fmt.Sprintf("%q", s)
	case map[string]interface{}:
		return fmt.Sprintf("{object keys=%d}", len(x))
	case []interface{}:
		return fmt.Sprintf("[array len=%d]", len(x))
	default:
		s := fmt.Sprintf("%v", x)
		if len(s) > max {
			s = utf8PrefixWithSuffix(s, "…", max)
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
			tail = utf8Suffix(tail, tailBudget)
		}
		out += tail
	}
	if len(out) > limit {
		return utf8Prefix(out, limit)
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
