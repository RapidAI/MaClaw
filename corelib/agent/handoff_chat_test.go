package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func handoffChatFixture(n, payload int) []interface{} {
	conversation := []interface{}{map[string]string{"role": "system", "content": "system policy"}}
	for i := 0; i < n; i++ {
		conversation = append(conversation,
			map[string]string{"role": "user", "content": "constraint " + strings.Repeat("u", payload)},
			map[string]string{"role": "assistant", "content": "progress " + strings.Repeat("a", payload)},
		)
	}
	return conversation
}

func handoffChatLimit(t *testing.T, conversation []interface{}) (int, int) {
	t.Helper()
	probe := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: 1000000,
		Tools:        []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})},
		Root:         t.TempDir(),
	})
	before := probe.BeforeTokens
	if before < 2000 {
		t.Fatalf("fixture too small: %d tokens", before)
	}
	// before is above 40% and below 50% of this limit.
	return before*2 + before/4, before
}

func TestCheckpointAllSummarizesOnceAfterThreshold(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	calls := 0
	conversation := handoffChatFixture(8, 500)
	conversation[11] = map[string]string{"role": "user", "content": "ship the report"}
	opts := ContextCheckpointOptions{
		ContextLimit: 6000,
		SessionKey:   "sum-once",
		Tools:        []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})},
		Root:         t.TempDir(),
		Summarize: func(material string) string {
			calls++
			if !strings.Contains(material, "ship the report") {
				t.Errorf("summarizer missed the covered span: %s", material)
			}
			return "<handoff-memo>\n## progress\nmedian 17.4\n</handoff-memo>"
		},
	}
	first := CheckpointConversation(conversation, opts)
	body := checkpointVisibleText(first.Conversation)
	if !first.Applied || calls != 1 || !strings.Contains(body, "median 17.4") || strings.Count(body, "<handoff-memo>") != 1 {
		t.Fatalf("first llm memo applied=%v calls=%d reason=%s body=%s", first.Applied, calls, first.Reason, body)
	}
	quiet := CheckpointConversation(handoffChatFixture(2, 20), ContextCheckpointOptions{
		ContextLimit: 1000000,
		Tools:        opts.Tools,
		Root:         t.TempDir(),
		Summarize: func(string) string {
			calls++
			return "<handoff-memo>\n## task\nshould not run\n</handoff-memo>"
		},
	})
	if quiet.Reason != "below_threshold" || calls != 1 {
		t.Fatalf("short transcript called the summarizer: reason=%s calls=%d", quiet.Reason, calls)
	}
	next := append([]interface{}{}, first.Conversation...)
	for i := 0; i < 6; i++ {
		next = append(next, map[string]string{"role": "user", "content": "later constraint " + strings.Repeat("q", 1200)})
	}
	second := CheckpointConversation(next, opts)
	secondBody := checkpointVisibleText(second.Conversation)
	if calls != 1 || !strings.Contains(secondBody, "median 17.4") {
		t.Fatalf("second pass calls=%d reason=%s body=%s", calls, second.Reason, secondBody)
	}
}

func TestCheckpointFlagOffIgnoresMemoSummarizer(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	calls := 0
	result := CheckpointConversation(handoffChatFixture(8, 500), ContextCheckpointOptions{
		ContextLimit: 4000,
		KeepGroups:   6,
		Tools:        []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})},
		Root:         t.TempDir(),
		Summarize: func(string) string {
			calls++
			return "<handoff-memo>\n## progress\nmedian 17.4\n</handoff-memo>"
		},
	})
	if calls != 0 || strings.Contains(checkpointVisibleText(result.Conversation), "<handoff-memo>") {
		t.Fatalf("flag off used the memo summarizer calls=%d reason=%s", calls, result.Reason)
	}
}

func TestCheckpointFlagOffStaysAtHalf(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	conversation := handoffChatFixture(8, 400)
	limit, _ := handoffChatLimit(t, conversation)
	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: limit,
		Tools:        []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})},
		Root:         t.TempDir(),
	})
	if result.Applied || result.Reason != "below_threshold" {
		t.Fatalf("flag off at mid budget: applied=%v reason=%s before=%d", result.Applied, result.Reason, result.BeforeTokens)
	}
}

func TestCheckpointAllUsesMemoTail(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	conversation := handoffChatFixture(8, 400)
	conversation[11] = map[string]string{"role": "user", "content": "ship the report"}
	limit, _ := handoffChatLimit(t, conversation)
	root := t.TempDir()
	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: limit,
		SessionKey:   "owner-memo",
		Tools:        []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})},
		Root:         root,
	})
	if !result.Applied {
		t.Fatalf("flag all did not checkpoint: %+v", result)
	}
	if got := result.Conversation[0].(map[string]string)["content"]; got != "system policy" || strings.Contains(got, "<handoff-memo>") {
		t.Fatalf("system prompt changed: %q", got)
	}
	body := checkpointVisibleText(result.Conversation)
	if strings.Count(body, "<handoff-memo>") != 1 {
		t.Fatalf("memo blocks=%d\n%s", strings.Count(body, "<handoff-memo>"), body)
	}
	if strings.Contains(body, "before guessing") || strings.Contains(body, "\n...\n") || strings.Contains(body, "好的，我已了解") {
		t.Fatalf("old checkpoint note leaked: %s", body)
	}
	if !strings.Contains(body, "ship the report") {
		t.Fatalf("task missing from memo: %s", body)
	}
	// The live tail starts with a user, so the memo is inside that user.
	if MsgRole(result.Conversation[1]) != "user" {
		t.Fatalf("tail role=%s", MsgRole(result.Conversation[1]))
	}
	first := result.Conversation[1].(map[string]string)["content"]
	if !strings.Contains(first, "<handoff-memo>") {
		t.Fatalf("memo was not folded into the first user: %s", first)
	}
	if len(result.Conversation) > 2 && MsgRole(result.Conversation[2]) == "user" && strings.Contains(checkpointVisibleText(result.Conversation[2:3]), "<handoff-memo>") {
		t.Fatal("folded memo left a second user memo")
	}
}

func TestCheckpointAllKeepsPinsAndImages(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	toolDef := tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})
	root := t.TempDir()
	conversation := []interface{}{map[string]string{"role": "system", "content": "system policy"}}
	conversation = append(conversation, map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "look"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "data:image/png;base64,AAA"}},
		},
	})
	conversation = append(conversation, map[string]string{"role": "user", "content": "please confirm the plan"})
	for i := 0; i < 6; i++ {
		conversation = append(conversation,
			map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{
				map[string]interface{}{"id": "old", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}},
			}},
			map[string]interface{}{"role": "tool", "tool_call_id": "old", "name": "bash", "content": "SECRET_OLD_BODY " + strings.Repeat("z", 2500)},
		)
	}
	conversation = append(conversation,
		map[string]string{"role": "user", "content": "[系统续轮] continue the wave"},
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{
			map[string]interface{}{"id": "wave", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}},
		}},
		map[string]interface{}{"role": "tool", "tool_call_id": "wave", "name": "bash", "content": "WAVE_TOOL_BODY " + strings.Repeat("w", 8000)},
	)
	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit:   8000,
		SessionKey:     "owner-pins",
		Tools:          []map[string]interface{}{toolDef},
		Root:           root,
		KeepUserEquals: []string{"please confirm the plan"},
	})
	if !result.Applied {
		t.Fatalf("pins checkpoint: %+v", result)
	}
	body := checkpointVisibleText(result.Conversation)
	if strings.Contains(body, "SECRET_OLD_BODY") {
		t.Fatal("covered tool body stayed in the model transcript")
	}
	if !strings.Contains(body, "WAVE_TOOL_BODY") || !strings.Contains(body, "[系统续轮] continue the wave") {
		t.Fatal("session-round wave was dropped")
	}
	foundConfirm := false
	foundImage := false
	for _, message := range result.Conversation {
		text, ok := plainStringContent(message)
		if ok && text == "please confirm the plan" {
			foundConfirm = true
		}
		mm, mapOK := message.(map[string]interface{})
		if !mapOK {
			continue
		}
		blocks, ok := mm["content"].([]interface{})
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
	if !foundConfirm {
		t.Fatal("confirmation original text was rewritten")
	}
	if !foundImage {
		t.Fatal("image block was rewritten")
	}
	if strings.Contains(body, "before guessing") || strings.Contains(body, "\n...\n") {
		t.Fatal("checkpoint preview kept the old instruction")
	}
}

func TestCheckpointAllPendingAskUnchanged(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	conversation := handoffChatFixture(8, 400)
	conversation = append(conversation, map[string]interface{}{
		"role": "tool", "tool_call_id": "ask", "content": "Asked user: which file?",
	})
	result := CheckpointConversation(conversation, ContextCheckpointOptions{
		ContextLimit: 4000,
		Tools:        []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})},
		Root:         t.TempDir(),
	})
	if result.Applied || result.Reason != "pending_ask" || len(result.Conversation) != len(conversation) {
		t.Fatalf("pending ask was rewritten: %+v", result)
	}
}

func TestCheckpointAllSecondPassReplacesMemo(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	conversation := handoffChatFixture(8, 500)
	opts := ContextCheckpointOptions{
		ContextLimit: 6000,
		SessionKey:   "owner-replace",
		Tools:        []map[string]interface{}{tooldef.BuildToolDef("read_tool_result", "read", map[string]interface{}{"type": "object"})},
		Root:         t.TempDir(),
	}
	first := CheckpointConversation(conversation, opts)
	if !first.Applied {
		t.Fatalf("first checkpoint: %+v", first)
	}
	next := append([]interface{}{}, first.Conversation...)
	for i := 0; i < 6; i++ {
		next = append(next, map[string]string{"role": "user", "content": "later constraint " + strings.Repeat("q", 1200)})
	}
	second := CheckpointConversation(next, opts)
	if !second.Applied {
		t.Fatalf("second checkpoint: %+v", second)
	}
	body := checkpointVisibleText(second.Conversation)
	if strings.Count(body, "<handoff-memo>") != 1 {
		t.Fatalf("stacked memos=%d\n%s", strings.Count(body, "<handoff-memo>"), body)
	}
}

func TestTrimHistoryFlagSwitch(t *testing.T) {
	entries := make([]ConversationEntry, 0, 48)
	entries = append(entries, ConversationEntry{Role: "user", Content: "[系统续轮] keep the wave"})
	for i := 0; i < 46; i++ {
		entries = append(entries, ConversationEntry{Role: "user", Content: "request " + strings.Repeat("r", 20)})
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "coding")
	coding := TrimHistory(entries)
	codingText := entryVisibleText(coding)
	if !strings.Contains(codingText, "用户要求:") || !strings.Contains(codingText, "好的，我已了解之前被省略的工作。") {
		t.Fatalf("coding flag changed the trim note: %s", codingText)
	}
	if strings.Contains(codingText, "<handoff-memo>") {
		t.Fatal("coding flag emitted a handoff memo")
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	all := TrimHistory(entries)
	allText := entryVisibleText(all)
	if !strings.Contains(allText, "<handoff-memo>") || strings.Contains(allText, "用户要求:") || strings.Contains(allText, "好的，我已了解") {
		t.Fatalf("all flag trim note: %s", allText)
	}
	if !strings.Contains(allText, "[系统续轮] keep the wave") {
		t.Fatal("trim dropped the session-round wave")
	}
	pending := append(append([]ConversationEntry{}, entries...), ConversationEntry{Role: "tool", Content: "Asked user: which file?"})
	if got := TrimHistory(pending); len(got) != len(pending) {
		t.Fatalf("pending ask trimmed %d -> %d", len(pending), len(got))
	}
}

func TestAutoCompressAllUsesMemoAndRefusesToolLoop(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	cfg := corelib.MaclawLLMConfig{ContextLength: 2000}
	toolLoop := []interface{}{
		map[string]string{"role": "user", "content": strings.Repeat("task ", 400)},
		map[string]interface{}{"role": "assistant", "content": "", "tool_calls": []interface{}{
			map[string]interface{}{"id": "t1", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": "{}"}},
		}},
		map[string]interface{}{"role": "tool", "tool_call_id": "t1", "content": strings.Repeat("output ", 400)},
	}
	if got := AutoCompressConversation(toolLoop, cfg, nil); len(got) != len(toolLoop) {
		t.Fatalf("tool loop compressed %d -> %d", len(toolLoop), len(got))
	}
	plain := []interface{}{map[string]string{"role": "system", "content": "policy"}}
	for i := 0; i < 8; i++ {
		plain = append(plain,
			map[string]string{"role": "user", "content": "goal " + strings.Repeat("g", 500)},
			map[string]string{"role": "assistant", "content": "note " + strings.Repeat("n", 500)},
		)
	}
	got := AutoCompressConversation(plain, cfg, nil)
	text := checkpointVisibleText(got)
	if !strings.Contains(text, "<handoff-memo>") || strings.Contains(text, "[compressed]") {
		t.Fatalf("auto compress result: %s", text)
	}
}

func TestTrimHistoryAllMergesConstraintIntoKeptMemo(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	const constraint = "keep the postgres constraint"
	entries := make([]ConversationEntry, 0, 90)
	for i := 0; i < 50; i++ {
		entries = append(entries, ConversationEntry{Role: "user", Content: constraint})
	}
	entries = append(entries, ConversationEntry{Role: "user", Content: "<handoff-memo>\n## progress\nmedian 17.4\n</handoff-memo>"})
	for i := 0; i < 39; i++ {
		entries = append(entries, ConversationEntry{Role: "user", Content: "recent " + strings.Repeat("r", 20)})
	}
	trimmed := TrimHistory(entries)
	text := entryVisibleText(trimmed)
	if strings.Count(text, "<handoff-memo>") != 1 {
		t.Fatalf("memo count:\n%s", text)
	}
	parsed, ok := ParseHandoffMemo(text)
	if !ok || !strings.Contains(parsed.Task, constraint) || !strings.Contains(parsed.Progress, "median 17.4") {
		t.Fatalf("merged memo task=%q progress=%q", parsed.Task, parsed.Progress)
	}
	for _, entry := range trimmed {
		body, _ := entry.Content.(string)
		if body == constraint {
			t.Fatal("covered constraint stayed as a raw user message")
		}
	}
}

func TestTrimConversationAllMergesConstraintIntoKeptMemo(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	const constraint = "keep the postgres constraint"
	large := strings.Repeat("q", 3000) + " " + constraint
	msgs := []interface{}{map[string]string{"role": "system", "content": "policy"}}
	for i := 0; i < 6; i++ {
		msgs = append(msgs, map[string]string{"role": "user", "content": large})
	}
	msgs = append(msgs, map[string]string{"role": "user", "content": "<handoff-memo>\n## progress\nmedian 17.4\n</handoff-memo>"})
	for i := 0; i < 3; i++ {
		msgs = append(msgs, map[string]string{"role": "user", "content": "recent " + strings.Repeat("r", 40)})
	}
	calls := 0
	got := TrimConversation(msgs, 4000, 0, func(string) string {
		calls++
		return "<handoff-memo>\n## task\nwiped\n</handoff-memo>"
	})
	text := checkpointVisibleText(got)
	if calls != 0 {
		t.Fatalf("summarizer ran over a kept memo, calls=%d", calls)
	}
	if strings.Count(text, "<handoff-memo>") != 1 || strings.Contains(text, "wiped") || strings.Contains(text, strings.Repeat("q", 2000)) {
		t.Fatalf("merged transcript:\n%s", text)
	}
	parsed, ok := ParseHandoffMemo(text)
	if !ok || !strings.Contains(parsed.Task, constraint) || !strings.Contains(parsed.Progress, "median 17.4") {
		t.Fatalf("merged memo task=%q progress=%q", parsed.Task, parsed.Progress)
	}
}

func TestTrimConversationAllKeepsRecoveryMeasurementOnce(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	handoff := longRecoveryHandoff()
	if RecoveryHandoffProgress(handoff) == "" || strings.Contains(handoff, "<handoff-memo>") || utf8.RuneCountInString(RecoveryHandoffProgress(handoff)) <= 1400 {
		t.Fatal("fixture is not a long four-section recovery handoff")
	}
	msgs := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": handoff},
	}
	for i := 0; i < 6; i++ {
		msgs = append(msgs, map[string]string{"role": "user", "content": strings.Repeat("q", 3000)})
	}
	for i := 0; i < 3; i++ {
		msgs = append(msgs, map[string]string{"role": "user", "content": "recent " + strings.Repeat("r", 40)})
	}
	longTask := strings.Repeat("约束", 2250)
	summary := "<handoff-memo>\n## task\n" + longTask + "\n</handoff-memo>"
	if taskRunes, summaryRunes := utf8.RuneCountInString(longTask), utf8.RuneCountInString(summary); taskRunes < 4500 || summaryRunes > 5000 {
		t.Fatalf("summarizer fixture task=%d summary=%d", taskRunes, summaryRunes)
	}
	calls := 0
	got := TrimConversation(msgs, 4000, 0, func(string) string {
		calls++
		return summary
	})
	text := checkpointVisibleText(got)
	parsed, ok := ParseHandoffMemo(text)
	if calls != 1 || !ok || strings.Count(text, "<handoff-memo>") != 1 || strings.Count(parsed.Progress, "median 17.4") != 1 {
		t.Fatalf("calls=%d progress=%q text=%s", calls, parsed.Progress, text)
	}
	if taskRunes, progressRunes, renderRunes := utf8.RuneCountInString(parsed.Task), utf8.RuneCountInString(parsed.Progress), utf8.RuneCountInString(parsed.Render()); taskRunes > 600 || progressRunes > 1400 || renderRunes > 5000 {
		t.Fatalf("task=%d progress=%d render=%d", taskRunes, progressRunes, renderRunes)
	}
	firstProgress := utf8.RuneCountInString(parsed.Progress)
	again := append(got, map[string]string{"role": "user", "content": strings.Repeat("n", 3000)})
	second := TrimConversation(again, 4000, 0, func(string) string {
		calls++
		return "<handoff-memo>\n## task\nwiped again\n</handoff-memo>"
	})
	parsed, ok = ParseHandoffMemo(checkpointVisibleText(second))
	if calls != 1 || !ok || strings.Count(parsed.Progress, "median 17.4") != 1 || strings.Count(checkpointVisibleText(second), "<handoff-memo>") != 1 {
		t.Fatalf("second calls=%d progress=%q", calls, parsed.Progress)
	}
	if againRunes, renderRunes := utf8.RuneCountInString(parsed.Progress), utf8.RuneCountInString(parsed.Render()); againRunes > firstProgress || againRunes > 1400 || renderRunes > 5000 {
		t.Fatalf("progress grew %d -> %d render=%d", firstProgress, againRunes, renderRunes)
	}
}

func longRecoveryHandoff() string {
	pad := strings.Repeat("later-section ", 120)
	body := "1. **当前进度**: median 17.4\n2. **重要上下文**: " + pad + "\n3. **待完成工作**: ship\n4. **关键数据**: keep-postgres\n"
	return CompactionRecoveryLead + "\n\n以下是之前模型产出的交接摘要:\n\n" + body
}

func checkpointVisibleText(messages []interface{}) string {
	var b strings.Builder
	for _, message := range messages {
		if text, ok := plainStringContent(message); ok {
			b.WriteString(text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func entryVisibleText(entries []ConversationEntry) string {
	var b strings.Builder
	for _, entry := range entries {
		text, _ := entry.Content.(string)
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.String()
}
