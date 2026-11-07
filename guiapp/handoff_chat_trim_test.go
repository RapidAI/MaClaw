package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestTrimHistoryFlagOffKeepsPlaceholder(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	entries := make([]agent.ConversationEntry, 0, 60)
	entries = append(entries, agent.ConversationEntry{Role: "user", Content: "开发一个系统"})
	entries = append(entries, agent.ConversationEntry{Role: "assistant", Content: "好的，开始规划"})
	for i := 2; i < 60; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "assistant", Content: "step"})
	}
	trimmed := trimHistory(entries)
	text := trimmedEntryText(trimmed)
	if !strings.Contains(text, compactionPlaceholder) {
		t.Fatalf("flag off lost the placeholder: %s", text)
	}
	if strings.Contains(text, "<handoff-memo>") {
		t.Fatal("flag off emitted a handoff memo")
	}
}

func TestTrimHistoryAllReplacesWithOneMemo(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "older goal"},
		{Role: "assistant", Content: "SECRET_ASSISTANT_PROSE " + strings.Repeat("s", 800)},
		{Role: "user", Content: "ship the report"},
	}
	for i := 0; i < 6; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "user", Content: "recent " + strings.Repeat("r", 30)})
	}
	calls := 0
	trimmed := trimHistoryWithSummary(entries, func(string) string {
		calls++
		return "this summary is not a memo"
	}, func(string, []string) {
		t.Fatal("memory sink ran under flag all")
	}, 4, 0)
	text := trimmedEntryText(trimmed)
	if strings.Contains(text, compactionPlaceholder) || strings.Contains(text, "SECRET_ASSISTANT_PROSE") {
		t.Fatalf("covered prose or placeholder remained: %s", text)
	}
	if strings.Count(text, "<handoff-memo>") != 1 || !strings.Contains(text, "ship the report") {
		t.Fatalf("memo calls=%d text=%s", calls, text)
	}
	if calls != 1 {
		t.Fatalf("summarizer calls=%d", calls)
	}
	again := append(trimmed, agent.ConversationEntry{Role: "user", Content: "one more"}, agent.ConversationEntry{Role: "user", Content: "two more"})
	second := trimHistoryWithSummary(again, func(string) string {
		calls++
		return "<handoff-memo>\n## task\nshould not replace via a second summary\n</handoff-memo>"
	}, nil, 4, 0)
	secondText := trimmedEntryText(second)
	if calls != 1 {
		t.Fatalf("covered prefix summarized again, calls=%d", calls)
	}
	if strings.Count(secondText, "<handoff-memo>") != 1 {
		t.Fatalf("stacked memos: %s", secondText)
	}
}

func TestTrimHistoryAllKeepsProgressInKeptMemo(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	entries := make([]agent.ConversationEntry, 0, 10)
	for i := 0; i < 6; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "user", Content: "old work " + strings.Repeat("o", 80)})
	}
	entries = append(entries, agent.ConversationEntry{Role: "user", Content: "<handoff-memo>\n## progress\nmedian 17.4\n</handoff-memo>"})
	for i := 0; i < 3; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "user", Content: "recent " + strings.Repeat("r", 40)})
	}
	calls := 0
	trimmed := trimHistoryWithSummary(entries, func(string) string {
		calls++
		return "<handoff-memo>\n## task\nwiped\n</handoff-memo>"
	}, nil, 4, 0)
	text := trimmedEntryText(trimmed)
	if calls != 0 {
		t.Fatalf("summarizer ran over a kept memo, calls=%d", calls)
	}
	if !strings.Contains(text, "median 17.4") || strings.Contains(text, "wiped") || strings.Contains(text, "old work") {
		t.Fatalf("kept progress was replaced: %s", text)
	}
	if strings.Count(text, "<handoff-memo>") != 1 {
		t.Fatalf("memo count:\n%s", text)
	}
}

func TestTrimHistoryAllEmptyMemoKeepsExtraGroup(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	entries := make([]agent.ConversationEntry, 0, 6)
	for i := 0; i < 6; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "assistant", Content: "note " + string(rune('a'+i))})
	}
	trimmed := trimHistoryWithSummary(entries, nil, nil, 3, 0)
	text := trimmedEntryText(trimmed)
	if strings.Contains(text, compactionPlaceholder) || strings.Contains(text, "<handoff-memo>") {
		t.Fatalf("empty memo still wrote a note: %s", text)
	}
	if !strings.Contains(text, "note b") || strings.Contains(text, "note a") {
		t.Fatalf("extra group was not the newest dropped assistant: %s", text)
	}
}

func TestTrimHistoryAllKeepsPins(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	image := []interface{}{
		map[string]interface{}{"type": "text", "text": "see"},
		map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,BBB"}},
	}
	entries := []agent.ConversationEntry{
		{Role: "user", Content: image},
		{Role: "user", Content: "please confirm the plan"},
		{Role: "assistant", Content: "", ToolCalls: []interface{}{
			map[string]interface{}{"id": "old", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": `{"path":"D:\\work\\pkg\\a.go"}`}},
		}},
		{Role: "tool", ToolCallID: "old", Content: "SECRET_OLD_BODY " + strings.Repeat("z", 4000)},
	}
	for i := 0; i < 4; i++ {
		entries = append(entries, agent.ConversationEntry{Role: "user", Content: "filler " + strings.Repeat("f", 200)})
	}
	entries = append(entries,
		agent.ConversationEntry{Role: "user", Content: "[系统续轮] continue the wave"},
		agent.ConversationEntry{Role: "tool", Content: "WAVE_TOOL_BODY " + strings.Repeat("w", 6000)},
	)
	trimmed := trimHistoryWithSummaryPrecomputed(entries, nil, nil, 0, 4000, 0, []string{"please confirm the plan"})
	text := trimmedEntryText(trimmed)
	if strings.Contains(text, "SECRET_OLD_BODY") || strings.Contains(text, compactionPlaceholder) {
		t.Fatalf("covered body or placeholder remained: %s", text)
	}
	if !strings.Contains(text, "[系统续轮] continue the wave") || !strings.Contains(text, "WAVE_TOOL_BODY") {
		t.Fatal("session-round wave was dropped")
	}
	foundConfirm := false
	foundImage := false
	for _, entry := range trimmed {
		if text, ok := entry.Content.(string); ok && text == "please confirm the plan" {
			foundConfirm = true
		}
		blocks, ok := entry.Content.([]interface{})
		if !ok {
			continue
		}
		for _, block := range blocks {
			bm, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if kind, _ := bm["type"].(string); kind == "image_url" {
				foundImage = true
			}
		}
	}
	if !foundConfirm || !foundImage {
		t.Fatalf("confirm=%v image=%v", foundConfirm, foundImage)
	}
	anchors := handoffPageAnchorEntries(trimmed)
	blob := ""
	for _, anchor := range anchors {
		blob += anchor.Content + "\n"
	}
	if strings.Contains(blob, "SECRET_OLD_BODY") {
		t.Fatalf("page anchors kept the tool body: %s", blob)
	}
	if !strings.Contains(blob, `D:\work\pkg\a.go`) {
		t.Fatalf("page anchors missed the memo path: %#v", anchors)
	}
}

func TestHandoffPageAnchorsIgnoreRawProse(t *testing.T) {
	entries := []agent.ConversationEntry{{
		Role:    "system",
		Content: "<handoff-memo>\n## files\nread:\n- D:\\work\\pkg\\a.go\n## progress\ndecided the measurement\n</handoff-memo>",
	}}
	anchors := handoffPageAnchorEntries(entries)
	if len(anchors) == 0 {
		t.Fatal("no anchors")
	}
	for _, anchor := range anchors {
		if strings.Contains(anchor.Content, "SECRET_ASSISTANT_PROSE") {
			t.Fatal(anchor.Content)
		}
		if anchor.Category != memory.Category("handoff_memo") {
			t.Fatalf("category %s", anchor.Category)
		}
	}
}

func trimmedEntryText(entries []agent.ConversationEntry) string {
	var b strings.Builder
	for _, entry := range entries {
		text, _ := entry.Content.(string)
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.String()
}
