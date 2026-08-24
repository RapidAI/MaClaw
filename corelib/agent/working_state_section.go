package agent

import (
	"strings"
	"unicode/utf8"
)

// ApplyWorkingStateSection splices or strips the working-state tail on conversation[0].
// attach=false or a nil/empty render deletes a previous section. Other messages are untouched.
func ApplyWorkingStateSection(conversation []interface{}, state *WorkingState, attach bool) []interface{} {
	if len(conversation) == 0 {
		return conversation
	}
	role, content, kind := systemPromptContent(conversation[0])
	if role != "system" {
		return conversation
	}
	stripped := stripWorkingStateSection(content)
	section := ""
	if attach {
		section = RenderWorkingState(state)
	}
	next := stripped
	if section != "" {
		if strings.TrimSpace(stripped) == "" {
			next = section
		} else {
			next = strings.TrimRight(stripped, "\n") + "\n\n" + section
		}
	}
	conversation[0] = replaceSystemContent(conversation[0], kind, next)
	return conversation
}

// RenderWorkingState formats a non-empty tail. Empty fields are omitted.
// Budget is 400 runes; Goal / Next / LastAction are never dropped.
func RenderWorkingState(state *WorkingState) string {
	body := fitWorkingStateBudget(state, renderWorkingStateBody(state))
	if strings.TrimSpace(body) == "" {
		return ""
	}
	return WorkingStateMarker + "\n" + body
}

func renderWorkingStateBody(state *WorkingState) string {
	if state == nil {
		return ""
	}
	var lines []string
	if g := strings.TrimSpace(state.Goal); g != "" {
		lines = append(lines, "目标: "+g)
	}
	if n := strings.TrimSpace(state.Next); n != "" {
		lines = append(lines, "下一步: "+n)
	}
	if state.LastAction != "" {
		lines = append(lines, "动作: "+string(state.LastAction))
	}
	if live := renderLiveLines(state.Live); len(live) > 0 {
		lines = append(lines, "台上:")
		lines = append(lines, live...)
	}
	if open := renderOpenLines(state.Open); len(open) > 0 {
		lines = append(lines, "未决:")
		lines = append(lines, open...)
	}
	if settled := renderSettledLines(state.Settled); len(settled) > 0 {
		lines = append(lines, "已证实:")
		lines = append(lines, settled...)
	}
	return strings.Join(lines, "\n")
}

func renderLiveLines(items []FocusItem) []string {
	var out []string
	for _, item := range items {
		label := strings.TrimSpace(item.Label)
		fact := strings.TrimSpace(item.Fact)
		if label == "" && fact == "" {
			continue
		}
		if fact == "" {
			out = append(out, "- "+label)
			continue
		}
		out = append(out, "- "+label+": "+fact)
	}
	return out
}

func renderOpenLines(items []OpenItem) []string {
	var out []string
	for _, item := range items {
		if strings.TrimSpace(item.ClosedBy) != "" {
			continue
		}
		q := strings.TrimSpace(item.Question)
		if q == "" {
			q = strings.TrimSpace(item.SettleBy)
		}
		if q == "" {
			continue
		}
		if tool := strings.TrimSpace(item.Tool); tool != "" {
			out = append(out, "- "+tool+": "+q)
			continue
		}
		out = append(out, "- "+q)
	}
	return out
}

func renderSettledLines(items []Settled) []string {
	var out []string
	for _, item := range items {
		claim := strings.TrimSpace(item.Claim)
		if claim == "" {
			claim = strings.TrimSpace(item.Label)
		}
		if claim == "" {
			continue
		}
		out = append(out, "- "+claim)
	}
	return out
}

func fitWorkingStateBudget(state *WorkingState, body string) string {
	if workingStateRenderLen(body) <= workingStateMaxRunes {
		return body
	}
	// Drop oldest settled first so a later write does not hide an earlier one
	// until the 400-rune cap actually requires it.
	if state != nil && len(state.Settled) > 1 {
		trimmed := *state
		trimmed.Settled = append([]Settled(nil), state.Settled[1:]...)
		body = renderWorkingStateBody(&trimmed)
		if workingStateRenderLen(body) <= workingStateMaxRunes {
			return body
		}
	}
	// Drop remaining settled, then shrink live facts; keep Goal / Next / LastAction.
	core := WorkingState{}
	if state != nil {
		core.Goal = state.Goal
		core.Next = state.Next
		core.LastAction = state.LastAction
		core.Live = shrinkLiveFacts(state.Live)
		core.Open = state.Open
	}
	body = renderWorkingStateBody(&core)
	budget := workingStateMaxRunes - utf8.RuneCountInString(WorkingStateMarker+"\n")
	if budget < 0 {
		budget = 0
	}
	return clipRunes(body, budget)
}

func workingStateRenderLen(body string) int {
	if body == "" {
		return 0
	}
	return utf8.RuneCountInString(WorkingStateMarker + "\n" + body)
}

func shrinkLiveFacts(items []FocusItem) []FocusItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]FocusItem, len(items))
	copy(out, items)
	for i := range out {
		out[i].Fact = clipRunes(out[i].Fact, 24)
	}
	return out
}

// StripWorkingStateFromVisible removes a line-start working-state block from
// user-visible text (bubbles, HistoryDelta). Inline mentions stay.
func StripWorkingStateFromVisible(text string) string {
	if text == "" || !strings.Contains(text, WorkingStateMarker) {
		return text
	}
	return strings.TrimRight(stripWorkingStateSection(text), "\n")
}

func stripWorkingStateContent(content interface{}) interface{} {
	switch c := content.(type) {
	case string:
		return StripWorkingStateFromVisible(c)
	case []interface{}:
		for i := range c {
			c[i] = stripWorkingStateContent(c[i])
		}
		return c
	case []map[string]interface{}:
		for i := range c {
			if text, ok := c[i]["text"].(string); ok {
				c[i]["text"] = StripWorkingStateFromVisible(text)
			}
		}
		return c
	case map[string]interface{}:
		if text, ok := c["text"].(string); ok {
			c["text"] = StripWorkingStateFromVisible(text)
		}
		return c
	case map[string]string:
		if text, ok := c["text"]; ok {
			c["text"] = StripWorkingStateFromVisible(text)
		}
		return c
	default:
		return content
	}
}

func stripWorkingStateSection(content string) string {
	idx := lastLineStartMarker(content, WorkingStateMarker)
	if idx < 0 {
		return content
	}
	return strings.TrimRight(content[:idx], "\n")
}

func lastLineStartMarker(content, marker string) int {
	if content == "" || marker == "" {
		return -1
	}
	idx := -1
	search := content
	offset := 0
	for {
		i := strings.Index(search, marker)
		if i < 0 {
			return idx
		}
		abs := offset + i
		if abs == 0 || content[abs-1] == '\n' {
			idx = abs
		}
		next := i + len(marker)
		search = search[next:]
		offset += next
	}
}

func systemPromptContent(msg interface{}) (role, content, kind string) {
	switch m := msg.(type) {
	case map[string]string:
		return m["role"], m["content"], "string"
	case map[string]interface{}:
		role, _ = m["role"].(string)
		content, _ = m["content"].(string)
		return role, content, "any"
	default:
		return "", "", ""
	}
}

func replaceSystemContent(msg interface{}, kind, content string) interface{} {
	switch kind {
	case "string":
		m, _ := msg.(map[string]string)
		if m == nil {
			m = map[string]string{}
		}
		m["role"] = "system"
		m["content"] = content
		return m
	default:
		m, _ := msg.(map[string]interface{})
		if m == nil {
			m = map[string]interface{}{}
		}
		m["role"] = "system"
		m["content"] = content
		return m
	}
}
