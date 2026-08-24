package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ComputerObserveFingerprintPrefix marks a compacted historical computer_observe
// result. The latest observe stays in full; older ones are replaced with this.
const ComputerObserveFingerprintPrefix = "[computer_observe fingerprint]"

// FoldComputerUseObserves keeps the latest computer_observe tool result in full
// and replaces earlier ones with a short fingerprint. It also drops stale
// observe screenshots, leaving the last vision image.
func FoldComputerUseObserves(msgs []interface{}) []interface{} {
	if len(msgs) == 0 {
		return msgs
	}
	idxs := computerObserveMessageIndexes(msgs)
	if len(idxs) >= 2 {
		out := append([]interface{}(nil), msgs...)
		for _, i := range idxs[:len(idxs)-1] {
			_, content := ExtractRoleContent(out[i])
			if isComputerObserveFingerprint(content) {
				continue
			}
			out[i] = replaceMessageContent(out[i], computerObserveFingerprint(content))
		}
		msgs = out
	}
	return pruneOlderComputerUseVisionImages(msgs)
}

// FoldComputerUseObserveEntries is the ConversationEntry form used by
// TrimHistory / ConversationMemory so a reload still sees folded observes.
func FoldComputerUseObserveEntries(entries []ConversationEntry) []ConversationEntry {
	if len(entries) == 0 {
		return entries
	}
	idxs := make([]int, 0, 4)
	for i, e := range entries {
		if isComputerObserveEntry(e) {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) < 2 {
		return entries
	}
	out := append([]ConversationEntry(nil), entries...)
	for _, i := range idxs[:len(idxs)-1] {
		content := entryContentString(out[i])
		if isComputerObserveFingerprint(content) {
			continue
		}
		out[i].Content = computerObserveFingerprint(content)
	}
	return out
}

func isComputerObserveFingerprint(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), ComputerObserveFingerprintPrefix)
}

func isComputerObserveEntry(e ConversationEntry) bool {
	if e.Role != "tool" {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(e.ToolName))
	if name == "computer_observe" {
		return true
	}
	if name != "" {
		return false
	}
	return looksLikeComputerObserveResult(entryContentString(e))
}

func entryContentString(e ConversationEntry) string {
	if s, ok := e.Content.(string); ok {
		return s
	}
	_, content := ExtractRoleContent(e.ToMessage())
	return content
}

func computerObserveMessageIndexes(msgs []interface{}) []int {
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
		id := checkpointToolMessageID(msg)
		name := strings.ToLower(strings.TrimSpace(names[id]))
		_, content := ExtractRoleContent(msg)
		switch {
		case name == "computer_observe":
			idxs = append(idxs, i)
		case name == "" && looksLikeComputerObserveResult(content):
			idxs = append(idxs, i)
		}
	}
	return idxs
}

func toolCallNamesFromMessage(message interface{}) map[string]string {
	mm, ok := message.(map[string]interface{})
	if !ok {
		return nil
	}
	raw, err := json.Marshal(mm["tool_calls"])
	if err != nil {
		return nil
	}
	var decoded []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	out := make(map[string]string, len(decoded))
	for _, call := range decoded {
		id := strings.TrimSpace(call.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			name = strings.TrimSpace(call.Name)
		}
		if name != "" {
			out[id] = name
		}
	}
	return out
}

func looksLikeComputerObserveResult(content string) bool {
	s := strings.TrimSpace(content)
	if s == "" {
		return false
	}
	if isComputerObserveFingerprint(s) {
		return true
	}
	if strings.HasPrefix(s, "computer_observe:") {
		return true
	}
	if computerObserveHasLabeledLine(s, "ocr_excerpt:") {
		return true
	}
	if computerObserveHasLabeledLine(s, "perception=llm_vision") {
		return true
	}
	return strings.HasPrefix(s, "mode=") && strings.Contains(s, "computer_click")
}

func computerObserveHasLabeledLine(content, prefix string) bool {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}

func computerObserveFingerprint(content string) string {
	var b strings.Builder
	b.WriteString(ComputerObserveFingerprintPrefix)
	b.WriteByte('\n')
	if win := computerObserveFingerprintWindow(content); win != "" {
		fmt.Fprintf(&b, "windows: %s\n", win)
	}
	if n := computerObserveFingerprintElements(content); n >= 0 {
		fmt.Fprintf(&b, "elements: %d\n", n)
	}
	if ocr := computerObserveFingerprintOCR(content); ocr != "" {
		fmt.Fprintf(&b, "ocr: %s\n", ocr)
	}
	b.WriteString("(full dump omitted; use the latest computer_observe below)\n")
	return b.String()
}

func computerObserveFingerprintWindow(content string) string {
	inWindows := false
	crop := ""
	for _, line := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "windows:" {
			inWindows = true
			continue
		}
		if inWindows {
			if strings.HasPrefix(trim, "- ") {
				return strings.TrimSpace(strings.TrimPrefix(trim, "- "))
			}
			if trim != "" {
				inWindows = false
			}
		}
		if crop == "" {
			const prefix = `crop="`
			if i := strings.Index(line, prefix); i >= 0 {
				rest := line[i+len(prefix):]
				if end := strings.IndexByte(rest, '"'); end > 0 {
					crop = rest[:end]
				}
			}
		}
	}
	return crop
}

func computerObserveFingerprintElements(content string) int {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		var n int
		if _, err := fmt.Sscanf(line, "elements (%d", &n); err == nil {
			return n
		}
		if _, err := fmt.Sscanf(line, "som_marks=%d", &n); err == nil {
			return n
		}
	}
	return -1
}

func computerObserveFingerprintOCR(content string) string {
	const key = "ocr_excerpt:"
	idx := strings.Index(content, key)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(content[idx+len(key):])
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return utf8PrefixRunes(strings.TrimSpace(rest), 80)
}

func utf8PrefixRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "..."
}

func replaceMessageContent(msg interface{}, content string) interface{} {
	switch m := msg.(type) {
	case map[string]interface{}:
		cp := make(map[string]interface{}, len(m)+1)
		for k, v := range m {
			cp[k] = v
		}
		cp["content"] = content
		return cp
	case map[string]string:
		cp := make(map[string]string, len(m)+1)
		for k, v := range m {
			cp[k] = v
		}
		cp["content"] = content
		return cp
	default:
		return map[string]interface{}{"role": MsgRole(msg), "content": content}
	}
}
