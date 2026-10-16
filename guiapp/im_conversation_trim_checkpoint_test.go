package guiapp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestTrimCheckpointHistoryKeepsHandoffWithoutOrphanTools(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "实现登录"},
		{
			Role:    "assistant",
			Content: "先读文件",
			ToolCalls: []llm.ToolCall{{
				ID:       "r1",
				Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"login.go"}`},
			}},
		},
		{Role: "tool", ToolCallID: "r1", Content: "package login"},
	}
	for i := 0; i < agent.MaxConversationTurns; i++ {
		history = append(history, agent.ConversationEntry{
			Role: "assistant", Content: fmt.Sprintf("step %d", i),
		})
	}
	history = append(history, agent.ConversationEntry{Role: "user", Content: "KEEP_TAIL"})
	cb.checkpointHistory = history
	trimmed := cb.trimCheckpointHistory()

	foundTail := false
	foundPath := false
	foundSystem := false
	for i, entry := range trimmed {
		text, _ := entry.Content.(string)
		if strings.Contains(text, "KEEP_TAIL") {
			foundTail = true
		}
		if strings.Contains(text, "login.go") {
			foundPath = true
		}
		if entry.Role == "system" {
			foundSystem = true
		}
		if entry.Role == "tool" {
			if i == 0 || (trimmed[i-1].Role != "assistant" && trimmed[i-1].Role != "tool") {
				t.Fatalf("orphaned tool at %d, previous role %q", i, trimmed[i-1].Role)
			}
		}
	}
	if !foundTail {
		t.Fatal("checkpoint trim dropped the recent user request")
	}
	if !foundPath {
		t.Fatal("checkpoint handoff did not keep the read file path")
	}
	if foundSystem {
		t.Fatal("checkpoint history must not keep a mid-transcript system separator")
	}
}

func TestCompactionSummaryUnusableRejectsIncompleteOutput(t *testing.T) {
	if !compactionSummaryUnusable("", "stop") {
		t.Fatal("empty summary should be rejected")
	}
	if !compactionSummaryUnusable("partial", "length") {
		t.Fatal("length stop should be rejected")
	}
	if !compactionSummaryUnusable("partial", "max_tokens") {
		t.Fatal("max_tokens stop should be rejected")
	}
	if !compactionSummaryUnusable(`{"tool_calls":[]}`, "stop") {
		t.Fatal("tool call output should be rejected")
	}
	if !compactionSummaryUnusable(strings.Repeat("长", compactionSummaryRuneCap+1), "stop") {
		t.Fatal("over-long summary should be rejected")
	}
	if compactionSummaryUnusable("## 当前进度\n- 已完成", "stop") {
		t.Fatal("complete summary should be kept")
	}
}

func TestCompactionPromptUpdatesPreviousSummary(t *testing.T) {
	material := buildCompactionSource("目标: 修好登录", "## 对话轮次\n\n用户: 继续")
	prompt := compactionPromptFor(material)
	if !strings.Contains(prompt, "保留上一份摘要") {
		t.Fatal("update prompt was not selected")
	}
	if !strings.Contains(prompt, "目标: 修好登录") {
		t.Fatal("previous summary was dropped from the update prompt")
	}
	if strings.Contains(compactionPromptFor("普通对话"), "保留上一份摘要") {
		t.Fatal("initial prompt should not use the update rules")
	}
}

func TestTrimHistoryCarriesPreviousSummaryAndFiles(t *testing.T) {
	entries := []agent.ConversationEntry{
		makeEntry("user", "开发登录"),
		makeEntry("assistant", "开始做登录"),
		{
			Role: "system",
			Content: compactionHandoffContent("目标: 修好登录", formatCompactionFileLists(
				[]string{"old_read.go"}, []string{"old_edit.go"})),
		},
		{
			Role:    "assistant",
			Content: "read it",
			ToolCalls: []llm.ToolCall{{
				ID: "r1",
				Function: llm.ToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path":"new_read.go"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "r1", Content: "package main"},
		{
			Role:    "assistant",
			Content: "write it",
			ToolCalls: []llm.ToolCall{{
				ID: "w1",
				Function: llm.ToolCallFunction{
					Name:      "write_file",
					Arguments: `{"path":"new_edit.go"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "w1", Content: "ok"},
	}
	for i := 0; i < 50; i++ {
		entries = append(entries, makeEntry("assistant", "step"))
	}
	result := trimHistoryWithSummary(entries, nil, nil, 20, 0)
	var checkpoint agent.ConversationEntry
	found := false
	for _, entry := range result {
		if entry.CompactionID != "" {
			checkpoint = entry
			found = true
		}
	}
	if !found {
		t.Fatal("expected a checkpoint separator with source material")
	}
	if !strings.Contains(checkpoint.CompactionSource, "目标: 修好登录") {
		t.Fatalf("previous summary missing from source: %s", checkpoint.CompactionSource)
	}
	if !strings.Contains(checkpoint.CompactionFiles, "old_read.go") || !strings.Contains(checkpoint.CompactionFiles, "new_read.go") {
		t.Fatalf("read files were not accumulated: %s", checkpoint.CompactionFiles)
	}
	if !strings.Contains(checkpoint.CompactionFiles, "old_edit.go") || !strings.Contains(checkpoint.CompactionFiles, "new_edit.go") {
		t.Fatalf("modified files were not accumulated: %s", checkpoint.CompactionFiles)
	}
	text, _ := checkpoint.Content.(string)
	if !strings.Contains(text, "已省略") {
		t.Fatal("placeholder should stay until the async summary lands")
	}
}

func TestCompactionSourceDoesNotInflateTokenBudget(t *testing.T) {
	entries := []agent.ConversationEntry{
		makeEntry("user", "请改 login.go"),
		makeEntry("assistant", "好"),
		{
			Role:             "system",
			Content:          compactionPlaceholder,
			CompactionID:     "cmp",
			CompactionSource: strings.Repeat("已完成的隐藏材料 ", 8000),
		},
	}
	if got := estimateConversationEntryTokens(entries); got > 5000 {
		t.Fatalf("hidden compaction source counted as model context: %d tokens", got)
	}
	trimmed := trimHistoryWithSummary(entries, nil, nil, 0, 8000)
	if len(trimmed) != len(entries) {
		t.Fatalf("hidden source triggered compaction: %d -> %d", len(entries), len(trimmed))
	}
}

func TestVisibleCheckpointHandoffIncludesPendingSummary(t *testing.T) {
	entry := agent.ConversationEntry{
		Role:             "system",
		Content:          compactionPlaceholder,
		CompactionID:     "cmp",
		CompactionSource: buildCompactionSource("目标: 修好登录", "请改 login.go"),
	}
	once := visibleCheckpointHandoff(entry)
	if !strings.Contains(once, "目标: 修好登录") || !strings.Contains(once, "请改 login.go") {
		t.Fatalf("checkpoint hid the carried summary: %s", once)
	}
	entry.Content = once
	twice := visibleCheckpointHandoff(entry)
	if twice != once {
		t.Fatalf("checkpoint handoff grew on the second save: %d -> %d", len(once), len(twice))
	}
}

func TestPreviousSummaryKeepsFinishedBodyAndLaterPendingSource(t *testing.T) {
	entries := []agent.ConversationEntry{
		{
			Role:    "system",
			Content: compactionHandoffContent("登录校验已修好", ""),
		},
		{
			Role:             "system",
			Content:          compactionPlaceholder,
			CompactionID:     "cmp-new",
			CompactionSource: buildCompactionSource("", "请补 session.go 的测试"),
		},
	}
	got := previousCompactionSummary(entries)
	if !strings.Contains(got, "登录校验已修好") || !strings.Contains(got, "session.go") {
		t.Fatalf("later pending source replaced the finished summary: %s", got)
	}
}

func TestSecondCheckpointDoesNotRepeatVisibleSource(t *testing.T) {
	marker := "UNIQUE_GOAL_修好登录_9f3a"
	entry := agent.ConversationEntry{
		Role:             "system",
		Content:          compactionPlaceholder,
		CompactionID:     "cmp-old",
		CompactionSource: buildCompactionSource(marker, "请改 login.go 的过期时间"),
	}
	entry.Role = "user"
	entry.Content = visibleCheckpointHandoff(entry)
	entries := []agent.ConversationEntry{entry, makeEntry("user", "请补 session.go"), makeEntry("assistant", "好")}
	for i := 0; i < 30; i++ {
		entries = append(entries, makeEntry("user", "继续"), makeEntry("assistant", "ok"))
	}
	result := trimHistoryWithSummary(entries, nil, nil, 8, 0)
	contentCopies := 0
	sourceCopies := 0
	var checkpoint agent.ConversationEntry
	for _, item := range result {
		text, _ := item.Content.(string)
		contentCopies += strings.Count(text, marker)
		sourceCopies += strings.Count(item.CompactionSource, marker)
		if item.CompactionID != "" {
			checkpoint = item
		}
	}
	if sourceCopies != 1 {
		t.Fatalf("pending source copies = %d, want 1", sourceCopies)
	}
	if contentCopies != 0 {
		t.Fatalf("visible handoff was kept beside the new source (%d copies)", contentCopies)
	}
	visible := visibleCheckpointHandoff(checkpoint)
	if strings.Count(visible, marker) != 1 || !strings.Contains(visible, "请改 login.go") {
		t.Fatalf("new checkpoint did not carry the pending source once:\n%s", visible)
	}
}

func TestSecondCompactionCarriesPendingSource(t *testing.T) {
	entries := []agent.ConversationEntry{{
		Role:             "system",
		Content:          compactionPlaceholder,
		CompactionID:     "cmp-old",
		CompactionSource: buildCompactionSource("目标: 修好登录", "请改 login.go 的过期时间"),
	}}
	for i := 0; i < 30; i++ {
		entries = append(entries,
			makeEntry("user", "继续"),
			makeEntry("assistant", "ok"),
		)
	}
	result := trimHistoryWithSummary(entries, nil, nil, 8, 0)
	var source string
	for _, entry := range result {
		if entry.CompactionSource != "" {
			source = entry.CompactionSource
		}
	}
	if !strings.Contains(source, "目标: 修好登录") || !strings.Contains(source, "请改 login.go") {
		t.Fatalf("new checkpoint lost the pending source:\n%s", source)
	}
}

func TestCompactionFileListReadsHandoffBullets(t *testing.T) {
	entries := []agent.ConversationEntry{{
		Role:    "user",
		Content: "[上下文恢复] 更早的对话因长度限制被省略。\n\n涉及文件:\n- login.go\n- session.go\n",
	}}
	read, modified := compactionFileLists(entries)
	if len(read) != 2 || read[0] != "login.go" || read[1] != "session.go" || len(modified) != 0 {
		t.Fatalf("read=%v modified=%v", read, modified)
	}
}

func TestCompactionFileListReadsMapArguments(t *testing.T) {
	entries := []agent.ConversationEntry{{
		Role: "assistant",
		ToolCalls: []map[string]interface{}{{
			"id": "r1",
			"function": map[string]interface{}{
				"name":      "read_file",
				"arguments": map[string]interface{}{"file_path": "login.go"},
			},
		}},
	}}
	read, modified := compactionFileLists(entries)
	if len(read) != 1 || read[0] != "login.go" {
		t.Fatalf("read=%v modified=%v", read, modified)
	}
}

func TestTrimHistoryTokenBudgetShrinksRecentWindow(t *testing.T) {
	entries := make([]agent.ConversationEntry, 0, 45)
	entries = append(entries, makeEntry("user", "task"))
	entries = append(entries, makeEntry("assistant", "plan"))
	for i := 0; i < 43; i++ {
		entries = append(entries, makeEntry("assistant", strings.Repeat("内容", 2000)))
	}
	result := trimHistoryWithSummary(entries, nil, nil, agent.MaxConversationTurns, 12000)
	recent := 0
	for i := len(result) - 1; i >= 0; i-- {
		text, _ := result[i].Content.(string)
		if strings.Contains(text, "已省略") || result[i].CompactionID != "" {
			break
		}
		recent++
	}
	if recent >= 20 {
		t.Fatalf("token budget kept %d recent entries, want a much smaller window", recent)
	}
}

func TestTrimConversationUsesPreviousSummaryAndFileList(t *testing.T) {
	conversation := []interface{}{
		map[string]string{"role": "system", "content": "policy"},
		map[string]string{"role": "user", "content": compactionRecoveryPrefix + "目标: 保住登录修复"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "read the file",
			"tool_calls": []llm.ToolCall{{
				ID:       "r1",
				Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"login.go"}`},
			}},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "r1", "content": "package login"},
	}
	for i := 0; i < 6; i++ {
		conversation = append(conversation, map[string]string{
			"role":    "assistant",
			"content": strings.Repeat("archived work ", 1000),
		})
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": "CURRENT_REQUEST"})
	var got string
	trimmed := trimConversation(conversation, 4000, 0, func(material string) string {
		got = material
		return "继续登录修复"
	})
	if !strings.Contains(got, "保住登录修复") || !strings.Contains(got, "<previous-summary>") {
		t.Fatalf("live trim did not pass the previous summary: %s", got)
	}
	found := false
	for _, message := range trimmed {
		m, ok := message.(map[string]string)
		if !ok {
			continue
		}
		if strings.Contains(m["content"], "继续登录修复") && strings.Contains(m["content"], "login.go") {
			found = true
		}
	}
	if !found {
		t.Fatalf("summary did not keep the handoff and file list: %#v", trimmed)
	}
}

func TestSplitGroupSuffixTruncatesOversizedTool(t *testing.T) {
	assistant := agent.ConversationEntry{
		Role:    "assistant",
		Content: "calling",
		ToolCalls: []llm.ToolCall{
			{ID: "1", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"ls"}`}},
		},
	}
	entries := []agent.ConversationEntry{
		assistant,
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("log ", 8000)},
	}
	budget := entryTokenCount(assistant) + 80
	out, start := splitGroupSuffix(entries, agent.EntryGroup{Start: 0, End: len(entries)}, budget)
	if start < 0 || start >= len(out) || out[start].Role != "assistant" {
		t.Fatalf("truncated suffix should keep the assistant, start=%d len=%d", start, len(out))
	}
	text, _ := out[start+1].Content.(string)
	if !strings.Contains(text, "截断") {
		t.Fatalf("oversized tool result should be truncated, got len=%d", len(text))
	}
	if len(text) >= len(entries[1].Content.(string)) {
		t.Fatal("truncated tool result was not shortened")
	}
}

func TestSplitGroupSuffixKeepsMapShapedTrailingTool(t *testing.T) {
	assistant := agent.ConversationEntry{
		Role:    "assistant",
		Content: "calling",
		ToolCalls: []map[string]interface{}{
			{"id": "1", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"a.go"}`}},
			{"id": "2", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"b.go"}`}},
			{"id": "3", "function": map[string]interface{}{"name": "read_file", "arguments": `{"path":"c.go"}`}},
		},
	}
	big := strings.Repeat("x", 8000)
	entries := []agent.ConversationEntry{
		assistant,
		{Role: "tool", ToolCallID: "1", Content: big},
		{Role: "tool", ToolCallID: "2", Content: big},
		{Role: "tool", ToolCallID: "3", Content: "kept tail"},
	}
	budget := entryTokenCount(assistant) + entryTokenCount(entries[3]) + 20
	out, start := splitGroupSuffix(entries, agent.EntryGroup{Start: 0, End: len(entries)}, budget)
	if start <= 0 || start >= len(out) || out[start].Role != "assistant" {
		t.Fatalf("map-shaped turn was dropped whole, start=%d len=%d", start, len(out))
	}
	if out[start+1].ToolCallID != "3" {
		t.Fatalf("expected trailing tool 3, got %s", out[start+1].ToolCallID)
	}
	calls, ok := out[start].ToolCalls.([]map[string]interface{})
	if !ok || len(calls) != 1 || calls[0]["id"] != "3" {
		t.Fatalf("assistant kept unpaired calls: %#v", out[start].ToolCalls)
	}
}

func TestSplitGroupSuffixKeepsTrailingToolResult(t *testing.T) {
	assistant := agent.ConversationEntry{
		Role:    "assistant",
		Content: "calling",
		ToolCalls: []llm.ToolCall{
			{ID: "1", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.go"}`}},
			{ID: "2", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"b.go"}`}},
			{ID: "3", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"c.go"}`}},
		},
	}
	big := strings.Repeat("x", 8000)
	entries := []agent.ConversationEntry{
		assistant,
		{Role: "tool", ToolCallID: "1", Content: big},
		{Role: "tool", ToolCallID: "2", Content: big},
		{Role: "tool", ToolCallID: "3", Content: big},
	}
	budget := entryTokenCount(assistant) + entryTokenCount(entries[3]) + 20
	out, start := splitGroupSuffix(entries, agent.EntryGroup{Start: 0, End: len(entries)}, budget)
	if start <= 0 || start >= len(out) || out[start].Role != "assistant" {
		t.Fatalf("kept suffix should start at the assistant, start=%d len=%d", start, len(out))
	}
	if out[start+1].ToolCallID != "3" {
		t.Fatalf("expected only the trailing tool result, got %s", out[start+1].ToolCallID)
	}
	calls := out[start].ToolCalls.([]llm.ToolCall)
	if len(calls) != 1 || calls[0].ID != "3" {
		t.Fatalf("assistant tool calls were not filtered to the kept result: %+v", calls)
	}
}
