package toolresult

import (
	"encoding/json"
	"fmt"
	"sort"
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
	// A search listing is one fact per hit. A head/tail byte window deletes
	// the middle hits, which is where the page that states the fact often
	// sits once a long direct answer leads the list.
	if strings.HasPrefix(strings.TrimSpace(content), "Public web results for ") {
		return previewWebSearch(content, limit)
	}

	tool := strings.ToLower(strings.TrimSpace(toolName))
	if tool == "code_navigation" {
		if p := PreviewCodeNavigation(content, limit); p != "" {
			return p
		}
	}
	switch {
	case tool == "bash" || tool == "get_session_output" ||
		strings.HasPrefix(tool, "ssh") || tool == "run_terminal_command":
		return previewTerminal(content, limit)
	case tool == "web_fetch":
		return previewWebFetch(content, limit)
	case tool == "computer_observe":
		return previewComputerObserve(content, limit)
	case tool == "tools_search":
		return previewToolsSearch(content, limit)
	case tool == "desktop":
		// One tool carries file bytes and command transcripts. A command's
		// exit line is the last line; a file's beginning is what an edit matches.
		if desktopProgramTail(content) {
			return previewTerminal(content, limit)
		}
		return previewHeadTail(content, limit, 0.8, 0.2)
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

// desktopProgramTail reports a container command result. ProgramOutput puts
// the exit or the timeout on the last line, after the transcript.
func desktopProgramTail(content string) bool {
	line := content
	if i := strings.LastIndex(content, "\n"); i >= 0 {
		line = content[i+1:]
	}
	line = strings.TrimSpace(line)
	if line == "(command timed out)" {
		return true
	}
	return strings.HasPrefix(line, "(exit ") && strings.HasSuffix(line, ")")
}

// CommandOutputMarker separates a session preamble (login banner, MOTD) from
// the command the model asked to run. Hosts that concatenate both into one
// tool result should use this exact line so the preview can keep the command
// when the combined blob exceeds the budget.
const CommandOutputMarker = "--- 命令输出 ---"

// previewTerminal keeps a short head and a long tail (recent log lines matter).
// When a command section is present, the login banner is not allowed to occupy
// the head: that is what hid uptime, memory, and disk behind a MOTD.
func previewTerminal(s string, limit int) string {
	if strings.Contains(s, CommandOutputMarker) {
		return previewTerminalCommandSection(s, limit)
	}
	return previewHeadTail(s, limit, 0.25, 0.75)
}

func previewTerminalCommandSection(s string, limit int) string {
	idx := strings.Index(s, CommandOutputMarker)
	if idx < 0 {
		return previewHeadTail(s, limit, 0.25, 0.75)
	}
	headerBudget := limit / 5
	if headerBudget > 512 {
		headerBudget = 512
	}
	if headerBudget < 64 && limit > 64 {
		headerBudget = 64
	}
	if headerBudget > limit {
		headerBudget = limit
	}
	header := s[:idx]
	if len(header) > headerBudget {
		header = utf8Prefix(header, headerBudget)
	}
	body := s[idx:]
	joined := header + body
	if len(joined) <= limit {
		return joined
	}
	remain := limit - len(header)
	if remain < 64 {
		return previewHeadTailCited(body, limit, 0.45, 0.55, len(s))
	}
	if header == "" {
		return previewHeadTailCited(body, remain, 0.45, 0.55, len(s))
	}
	// A newline between a truncated header and the command marker is only
	// needed when the header does not already end at a line boundary.
	glue := ""
	if !strings.HasSuffix(header, "\n") {
		glue = "\n"
	}
	if len(glue) >= remain {
		return previewHeadTailCited(body, limit, 0.45, 0.55, len(s))
	}
	return header + glue + previewHeadTailCited(body, remain-len(glue), 0.45, 0.55, len(s))
}

func previewHeadTail(s string, limit int, headFrac, tailFrac float64) string {
	return previewHeadTailCited(s, limit, headFrac, tailFrac, len(s))
}

// previewHeadTailCited is previewHeadTail with an explicit size in the
// truncation marker. Command-section previews cut the body only, but the
// marker must cite the original payload so the model does not treat the
// shorter body as the full result.
func previewHeadTailCited(s string, limit int, headFrac, tailFrac float64, citedBytes int) string {
	if len(s) <= limit {
		return s
	}
	if citedBytes <= 0 {
		citedBytes = len(s)
	}
	sep := fmt.Sprintf("\n\n... (已截断，共 %d 字节) ...\n\n", citedBytes)
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

// previewToolsSearch keeps the surface decision and whole directory rows.
// A generic head/tail cut deletes the middle of an alphabetical catalog,
// which is where a planned name such as generate_pdf sits, and leaves the
// petition sentence in the tail as the only instruction.
func previewToolsSearch(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	lines := strings.Split(s, "\n")
	firstRow, lastRow := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, "- ") {
			if firstRow < 0 {
				firstRow = i
			}
			lastRow = i
		}
	}
	if firstRow < 0 {
		return DefaultPreview(s, limit)
	}
	prefix := strings.Join(lines[:firstRow], "\n")
	footer := ""
	if lastRow+1 < len(lines) {
		footer = strings.Join(lines[lastRow+1:], "\n")
	}
	if len(prefix) >= limit {
		return utf8Prefix(prefix, limit)
	}
	footerKeep := footer
	remain := limit - len(prefix)
	if footerKeep != "" {
		footerBudget := remain / 3
		if footerBudget < 64 {
			footerBudget = 64
		}
		if len(footerKeep) > footerBudget {
			footerKeep = utf8Suffix(footer, footerBudget)
		}
		if len(footerKeep) >= remain {
			footerKeep = ""
		} else {
			remain -= len(footerKeep) + 1
		}
	}
	var rows []string
	used := 0
	for _, line := range lines[firstRow : lastRow+1] {
		need := len(line) + 1
		if used+need > remain {
			break
		}
		rows = append(rows, line)
		used += need
	}
	var b strings.Builder
	b.WriteString(prefix)
	if len(rows) > 0 {
		if prefix != "" && !strings.HasSuffix(prefix, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Join(rows, "\n"))
	}
	if footerKeep != "" {
		b.WriteByte('\n')
		b.WriteString(strings.TrimPrefix(footerKeep, "\n"))
	}
	out := b.String()
	if len(out) > limit {
		return utf8Prefix(out, limit)
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

type webSearchHit struct {
	title   string
	url     string
	snippet string
}

func previewWebSearch(s string, limit int) string {
	preamble, hits, ok := splitWebSearchHits(s)
	if !ok {
		return DefaultPreview(s, limit)
	}
	full := make([]string, len(hits))
	longest := 0
	for i := range hits {
		full[i] = hits[i].snippet
		if len(full[i]) > longest {
			longest = len(full[i])
		}
	}
	best := renderWebSearch(preamble, hits, nil)
	if len(best) > limit {
		return DefaultPreview(s, limit)
	}
	lo, hi := 0, longest
	for lo <= hi {
		mid := (lo + hi) / 2
		snippets := make([]string, len(hits))
		for i := range full {
			snippets[i] = shrinkWebSnippet(full[i], mid)
		}
		rendered := renderWebSearch(preamble, hits, snippets)
		if len(rendered) <= limit {
			best = rendered
			lo = mid + 1
			continue
		}
		hi = mid - 1
	}
	return best
}

func splitWebSearchHits(s string) (string, []webSearchHit, bool) {
	lines := strings.Split(s, "\n")
	start := -1
	for i, line := range lines {
		if webSearchHitTitle(line) {
			start = i
			break
		}
	}
	if start < 0 {
		return "", nil, false
	}
	preamble := strings.Join(lines[:start], "\n")
	var hits []webSearchHit
	var cur *webSearchHit
	flush := func() {
		if cur == nil {
			return
		}
		hits = append(hits, *cur)
		cur = nil
	}
	for _, line := range lines[start:] {
		if webSearchHitTitle(line) {
			flush()
			cur = &webSearchHit{title: strings.TrimSpace(line)}
			continue
		}
		if cur == nil {
			continue
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if cur.url == "" && strings.Contains(trim, "://") {
			cur.url = trim
			continue
		}
		if cur.snippet != "" {
			cur.snippet += "\n" + trim
			continue
		}
		cur.snippet = trim
	}
	flush()
	if len(hits) < 2 {
		return "", nil, false
	}
	return preamble, hits, true
}

func webSearchHitTitle(line string) bool {
	trim := strings.TrimSpace(line)
	i := 0
	for i < len(trim) && trim[i] >= '0' && trim[i] <= '9' {
		i++
	}
	return i > 0 && i+2 <= len(trim) && trim[i] == '.' && trim[i+1] == ' '
}

func renderWebSearch(preamble string, hits []webSearchHit, snippets []string) string {
	var b strings.Builder
	if text := strings.TrimSpace(preamble); text != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	for i, hit := range hits {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(hit.title)
		b.WriteByte('\n')
		if hit.url != "" {
			b.WriteString("   ")
			b.WriteString(hit.url)
			b.WriteByte('\n')
		}
		snippet := ""
		if i < len(snippets) {
			snippet = snippets[i]
		}
		if snippet != "" {
			b.WriteString("   ")
			b.WriteString(snippet)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// webSnippetFactFieldMax is the largest "; "-separated piece treated as a
// measured field rather than a paragraph. Direct answers emit prose first
// and the fields after it; folding appends the page fact last.
const webSnippetFactFieldMax = 160

// shrinkWebSnippet keeps measured fields and both ends of a folded snippet.
// A byte window on the whole string keeps the opening paragraph and the
// closing paragraph, and drops the fields that sit between them. Spending
// the entire remainder on the last paragraph drops the answer at the start
// of the lead.
func shrinkWebSnippet(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if parts := strings.Split(s, "; "); len(parts) > 1 {
		if kept := shrinkWebSnippetFields(parts, max); kept != "" {
			return kept
		}
	}
	return shrinkWebSnippetEdges(s, max)
}

type webSnippetPiece struct {
	idx  int
	text string
}

func shrinkWebSnippetFields(parts []string, max int) string {
	chosen := make([]webSnippetPiece, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if part == "" || len(part) > webSnippetFactFieldMax {
			continue
		}
		trial := append(append([]webSnippetPiece(nil), chosen...), webSnippetPiece{idx: i, text: part})
		if len(joinWebSnippetPieces(trial)) > max {
			continue
		}
		chosen = trial
	}
	var longs []webSnippetPiece
	for i, part := range parts {
		if part == "" || len(part) <= webSnippetFactFieldMax {
			continue
		}
		longs = append(longs, webSnippetPiece{idx: i, text: part})
	}
	if len(longs) == 0 {
		return joinWebSnippetPieces(chosen)
	}
	// The lead keeps its head, where a direct answer starts. The last piece
	// keeps its own head and tail, where a folded page fact lands. Middle
	// paragraphs are the blurb between those two.
	ends := []webSnippetPiece{longs[0]}
	if len(longs) > 1 {
		ends = append(ends, longs[len(longs)-1])
	}
	room := max - len(joinWebSnippetPieces(chosen))
	if len(chosen) > 0 {
		room -= len(ends) * len("; ")
	} else if len(ends) == 2 {
		room -= len("; ")
	}
	if room <= 0 {
		return joinWebSnippetPieces(chosen)
	}
	firstKeep, lastKeep := room, room
	if len(ends) == 2 {
		lastKeep = room * 2 / 3
		firstKeep = room - lastKeep
	}
	placed := append([]webSnippetPiece(nil), chosen...)
	if text := cutWebSnippetPiece(ends[0].text, firstKeep, len(ends) == 1); text != "" {
		placed = append(placed, webSnippetPiece{idx: ends[0].idx, text: text})
	}
	if len(ends) == 2 {
		if text := cutWebSnippetPiece(ends[1].text, lastKeep, true); text != "" {
			placed = append(placed, webSnippetPiece{idx: ends[1].idx, text: text})
		}
	}
	if joined := joinWebSnippetPieces(placed); len(joined) <= max {
		return joined
	}
	// Rune boundaries can leave the lead one byte over. The fields and the
	// folded page outrank it.
	leadIdx := ends[0].idx
	kept := make([]webSnippetPiece, 0, len(placed))
	for _, piece := range placed {
		if piece.idx == leadIdx {
			continue
		}
		kept = append(kept, piece)
	}
	if joined := joinWebSnippetPieces(kept); len(joined) <= max {
		return joined
	}
	return joinWebSnippetPieces(chosen)
}

func cutWebSnippetPiece(text string, budget int, edges bool) string {
	if budget <= 0 || text == "" {
		return ""
	}
	if len(text) <= budget {
		return text
	}
	if edges {
		return shrinkWebSnippetEdges(text, budget)
	}
	return utf8Prefix(text, budget)
}

func joinWebSnippetPieces(pieces []webSnippetPiece) string {
	ordered := append([]webSnippetPiece(nil), pieces...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].idx < ordered[j].idx })
	parts := make([]string, len(ordered))
	for i := range ordered {
		parts[i] = ordered[i].text
	}
	return strings.Join(parts, "; ")
}

func shrinkWebSnippetEdges(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	if len(s) <= max {
		return s
	}
	const mark = " … "
	tail := max / 3
	if tail < len(mark)+4 {
		return utf8Suffix(s, max)
	}
	head := max - tail - len(mark)
	if head < 4 {
		return utf8Suffix(s, max)
	}
	return utf8Prefix(s, head) + mark + utf8Suffix(s, tail)
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
