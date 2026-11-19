package agent

import "strings"

// BrowserProbeFingerprintPrefix marks a compacted historical browser page
// probe. The latest probe stays in full; older ones are replaced with this.
// The fingerprint keeps the page identity and omits the street and the payable
// total, so a confirmation card cannot copy an address from a page that has
// already changed.
const BrowserProbeFingerprintPrefix = "[browser probe fingerprint]"

// FoldBrowserObservations keeps the latest browser page probe in full and
// replaces earlier probes with a short fingerprint. Click, type, and navigate
// results stay: they record what happened, and the next probe is the page.
func FoldBrowserObservations(msgs []interface{}) []interface{} {
	if len(msgs) == 0 {
		return msgs
	}
	idxs := browserProbeMessageIndexes(msgs)
	if len(idxs) < 2 {
		return msgs
	}
	out := append([]interface{}(nil), msgs...)
	for _, i := range idxs[:len(idxs)-1] {
		_, content := ExtractRoleContent(out[i])
		if isBrowserProbeFingerprint(content) {
			continue
		}
		out[i] = replaceMessageContent(out[i], browserProbeFingerprint(content))
	}
	return out
}

// FoldBrowserObservationEntries is the ConversationEntry form used by
// TrimHistory, so a reload still sees folded probes.
func FoldBrowserObservationEntries(entries []ConversationEntry) []ConversationEntry {
	if len(entries) == 0 {
		return entries
	}
	idxs := make([]int, 0, 4)
	for i, e := range entries {
		if isBrowserProbeEntry(e) {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) < 2 {
		return entries
	}
	out := append([]ConversationEntry(nil), entries...)
	for _, i := range idxs[:len(idxs)-1] {
		content := entryContentString(out[i])
		if isBrowserProbeFingerprint(content) {
			continue
		}
		out[i].Content = browserProbeFingerprint(content)
	}
	return out
}

func isBrowserProbeFingerprint(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), BrowserProbeFingerprintPrefix)
}

func isBrowserProbeEntry(e ConversationEntry) bool {
	if e.Role != "tool" {
		return false
	}
	return isBrowserProbeResult(e.ToolName, entryContentString(e))
}

func browserProbeMessageIndexes(msgs []interface{}) []int {
	names := map[string]string{}
	var idxs []int
	for i, msg := range msgs {
		if MsgRole(msg) == "assistant" && MsgHasToolCalls(msg) {
			for id, name := range toolCallNamesFromMessage(msg) {
				names[id] = name
			}
		}
		if MsgRole(msg) != "tool" {
			continue
		}
		name := names[checkpointToolMessageID(msg)]
		if strings.TrimSpace(name) == "" {
			name = toolMessageName(msg)
		}
		_, content := ExtractRoleContent(msg)
		if isBrowserProbeResult(name, content) {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

func toolMessageName(msg interface{}) string {
	switch m := msg.(type) {
	case map[string]interface{}:
		name, _ := m["name"].(string)
		return name
	case map[string]string:
		return m["name"]
	default:
		return ""
	}
}

func isBrowserProbeResult(name, content string) bool {
	named := browserProbeName(name)
	if isBrowserProbeFingerprint(content) {
		return named || strings.TrimSpace(name) == ""
	}
	if !strings.Contains(content, "page_text:\n") {
		return false
	}
	if named {
		return true
	}
	// No tool name. A file or a fetch that quotes the marker is not a probe.
	// An observation always names how many elements it saw.
	if strings.TrimSpace(name) != "" {
		return false
	}
	return strings.Contains(content, "interactive elements:")
}

// browserProbeName admits the managed tool and a legacy browser_* observation.
// A click result has no page_text marker, so the name alone does not fold it.
func browserProbeName(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "browser" || strings.HasPrefix(name, "browser_")
}

func browserProbeFingerprint(content string) string {
	var b strings.Builder
	b.WriteString(BrowserProbeFingerprintPrefix)
	b.WriteByte('\n')
	if line := browserProbeFingerprintDisplay(content); line != "" {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteString("(full page omitted; use the latest browser probe)\n")
	return b.String()
}

func browserProbeFingerprintDisplay(content string) string {
	const mark = "page_text:\n"
	head := content
	if at := strings.LastIndex(content, mark); at >= 0 {
		head = content[:at]
	}
	for _, line := range strings.Split(head, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || trim == "refs:" || strings.HasPrefix(trim, "@e") || strings.HasPrefix(trim, "refs truncated") {
			continue
		}
		if browserProbeLineHasCheckoutFact(trim) {
			// The observation display mixes a free-text title with the URL.
			// The title may be the street or a payable word. Keep the URL
			// and the element count; drop a line that is only the street.
			if kept := browserProbeIdentityLine(trim); kept != "" && !browserProbeLineHasCheckoutFact(kept) {
				return utf8PrefixRunes(kept, 120)
			}
			continue
		}
		return utf8PrefixRunes(trim, 120)
	}
	return ""
}

// browserProbeIdentityLine reads "probed page TITLE (URL), interactive elements: N"
// and returns the same line without TITLE. An address line has no URL and
// returns empty, so the fingerprint cannot keep a stale street.
func browserProbeIdentityLine(line string) string {
	const mark = ", interactive elements:"
	at := strings.Index(line, mark)
	if at < 0 {
		return ""
	}
	head, tail := line[:at], line[at:]
	open := strings.LastIndex(head, "(")
	close := strings.LastIndex(head, ")")
	pageAt := strings.Index(head, " page ")
	if open < 0 || close < open || pageAt < 0 || open < pageAt {
		return ""
	}
	verb := strings.TrimSpace(head[:pageAt])
	if verb == "" {
		return ""
	}
	return verb + " page " + head[open:close+1] + tail
}

func browserProbeLineHasCheckoutFact(line string) bool {
	for _, marker := range []string{
		"收货地址", "默认地址", "配送至", "送至",
		"应付总额", "应付金额", "实付款", "实付", "应付", "订单总额", "合计", "总计",
	} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}
