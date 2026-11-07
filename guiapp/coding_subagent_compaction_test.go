package guiapp

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestSubAgentCompactionIncludesReadFilesAndKeepsEarlierModified(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	compactor := NewSubAgentCompactor(8000,
		func() []string { return []string{"new.go"} },
		func() []string { return nil },
		func() []string { return []string{"fresh_read.go"} },
		nil,
	)
	task := "实现功能\n\n## 已读取文件\n- earlier_read.go\n\n## 已修改文件\n- earlier.go\n"
	summary := compactor.buildCompactionSummary(nil, task)
	for _, want := range []string{"earlier_read.go", "fresh_read.go", "earlier.go", "new.go"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %s:\n%s", want, summary)
		}
	}
}

func TestSubAgentCompactionSplitsOversizedTurn(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	compactor := NewSubAgentCompactor(8000, nil, nil, nil, nil)
	prefix := strings.Repeat("output ", 8000)
	suffix := strings.Repeat("kept ", 200)
	conversation := []interface{}{
		map[string]interface{}{"role": "system", "content": "sys"},
		map[string]interface{}{"role": "user", "content": "do the task"},
		map[string]interface{}{
			"role":    "assistant",
			"content": "working",
			"tool_calls": []llm.ToolCall{
				{ID: "1", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"one"}`}},
				{ID: "2", Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"two"}`}},
			},
		},
		map[string]interface{}{"role": "tool", "tool_call_id": "1", "content": prefix},
		map[string]interface{}{"role": "tool", "tool_call_id": "2", "content": suffix},
	}
	if !compactor.ShouldCompact(conversation) {
		t.Fatal("expected oversized turn to trigger compaction")
	}
	compacted := compactor.Compact(conversation)
	if len(compacted) >= len(conversation) {
		t.Fatalf("expected a shorter conversation, got %d -> %d", len(conversation), len(compacted))
	}
	foundKept := false
	for _, msg := range compacted {
		m, ok := msg.(map[string]interface{})
		if !ok || m["role"] != "tool" {
			continue
		}
		if m["tool_call_id"] == "2" {
			foundKept = true
		}
		if m["tool_call_id"] == "1" {
			t.Fatal("prefix tool result should have been summarized, not kept")
		}
	}
	if !foundKept {
		t.Fatal("trailing tool result should stay in the kept suffix")
	}
}

func codingCompactionTurns(task string, n int) []interface{} {
	conv := []interface{}{
		map[string]interface{}{"role": "system", "content": "sys"},
		map[string]interface{}{"role": "user", "content": task},
	}
	for i := 0; i < n; i++ {
		id := strconv.Itoa(i)
		conv = append(conv,
			map[string]interface{}{
				"role":    "assistant",
				"content": "turn " + id,
				"tool_calls": []llm.ToolCall{{
					ID:       id,
					Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo"}`},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": id, "content": "ok " + id},
		)
	}
	return conv
}

func insertCodingTurns(conv []interface{}, n int) []interface{} {
	out := []interface{}{conv[0], conv[1]}
	for i := 0; i < n; i++ {
		id := "old" + strconv.Itoa(i)
		out = append(out,
			map[string]interface{}{
				"role":    "assistant",
				"content": "old " + id,
				"tool_calls": []llm.ToolCall{{
					ID:       id,
					Function: llm.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo"}`},
				}},
			},
			map[string]interface{}{"role": "tool", "tool_call_id": id, "content": "old ok " + id},
		)
	}
	return append(out, conv[2:]...)
}

func taskContentOf(conv []interface{}) string {
	msg, _ := conv[1].(map[string]interface{})
	content, _ := msg["content"].(string)
	return content
}

func TestSubAgentCompactionHandoffMemoReplacesOneBlock(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "coding")
	commands := []CodingSubAgentCommandResult{
		{Command: "go test ./bench", WorkingDir: "D:/b", Succeeded: false, Summary: "exit status 1\nCOMPILER_LOG_TOKEN line"},
		{Command: "go test ./bench -count=1", WorkingDir: "D:/b", Succeeded: false, Summary: "exit status 1\nCOMPILER_LOG_TOKEN again"},
		{Command: "go test ./corelib/agent", WorkingDir: "D:/ok", Succeeded: true, Summary: "error FAIL but this succeeded\nCOMPILER_LOG_TOKEN ok"},
	}
	compactor := NewSubAgentCompactor(8000,
		func() []string { return []string{"new.go"} },
		func() []string { return nil },
		func() []string { return []string{"fresh_read.go"} },
		func() []CodingSubAgentCommandResult { return commands },
	)
	task := "不要重跑基准\n<handoff-memo>\nold unclosed body"
	conv := codingCompactionTurns(task, 4)
	beforeStats := agent.CurrentHandoffMemoStats()
	first := compactor.Compact(conv)
	afterStats := agent.CurrentHandoffMemoStats()
	if afterStats.Applied != beforeStats.Applied || afterStats.Fallback <= beforeStats.Fallback {
		t.Fatalf("deterministic coding memo counted as applied: before=%+v after=%+v", beforeStats, afterStats)
	}
	body := taskContentOf(first)
	if strings.Count(body, "<handoff-memo>") != 1 || strings.Count(body, "</handoff-memo>") != 1 {
		t.Fatalf("first compact did not leave one block: %q", body)
	}
	if strings.Contains(body, "old unclosed") || strings.Contains(body, "[上下文压缩]") {
		t.Fatalf("old summary style remained: %q", body)
	}
	if strings.Contains(body, "COMPILER_LOG_TOKEN") {
		t.Fatalf("memo kept the compiler log: %q", body)
	}
	memo, ok := agent.ParseHandoffMemo(body)
	if !ok {
		t.Fatalf("memo did not parse: %q", body)
	}
	foundRepeat := false
	for _, end := range memo.DeadEnds {
		if strings.Contains(end.Outcome, "不要用同一方式再试") {
			foundRepeat = true
		}
		if strings.Contains(end.Attempt, "不要重跑基准") || strings.Contains(end.Outcome, "不要重跑基准") || strings.Contains(end.Attempt, "go test ./corelib/agent") {
			t.Fatalf("user text or the successful verify became a dead end: %+v", end)
		}
	}
	if !foundRepeat {
		t.Fatalf("repeated failure missing the structural prefix: %+v", memo.DeadEnds)
	}
	if !strings.Contains(memo.Task, "不要重跑基准") {
		t.Fatalf("task dropped the user sentence: %q", memo.Task)
	}
	if !strings.Contains(memo.Progress, "go test ./corelib/agent exit 0") || strings.Contains(memo.Progress, "COMPILER_LOG_TOKEN") {
		t.Fatalf("progress=%q", memo.Progress)
	}

	second := compactor.Compact(insertCodingTurns(first, 2))
	third := compactor.Compact(insertCodingTurns(second, 2))
	finalBody := taskContentOf(third)
	if strings.Count(finalBody, "<handoff-memo>") != 1 || strings.Count(finalBody, "</handoff-memo>") != 1 {
		t.Fatalf("third compact stacked blocks: %q", finalBody)
	}
	if strings.Contains(finalBody, "old unclosed") {
		t.Fatalf("unclosed body came back: %q", finalBody)
	}
}

func TestSubAgentCompactionKeepsRecoveryMeasurementOnce(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	if !strings.HasPrefix(compactionRecoveryPrefix, agent.CompactionRecoveryLead) {
		t.Fatal("recovery lead drifted from the compaction prefix")
	}
	pad := strings.Repeat("later-section ", 120)
	task := compactionRecoveryPrefix + "1. **当前进度**: median 17.4\n2. **重要上下文**: " + pad + "\n3. **待完成工作**: ship\n4. **关键数据**: keep-postgres\n"
	if strings.Contains(task, "<handoff-memo>") || utf8.RuneCountInString(agent.RecoveryHandoffProgress(task)) <= 1400 {
		t.Fatal("fixture is not a long four-section recovery handoff")
	}
	compactor := NewSubAgentCompactor(8000, nil, nil, nil, nil)
	first := compactor.Compact(codingCompactionTurns(task, 4))
	body := taskContentOf(first)
	memo, ok := agent.ParseHandoffMemo(body)
	if !ok || strings.Count(body, "<handoff-memo>") != 1 || strings.Count(memo.Progress, "median 17.4") != 1 {
		t.Fatalf("progress=%q body=%s", memo.Progress, body)
	}
	if progressRunes, renderRunes := utf8.RuneCountInString(memo.Progress), utf8.RuneCountInString(memo.Render()); progressRunes > 1400 || renderRunes > 5000 {
		t.Fatalf("progress=%d render=%d", progressRunes, renderRunes)
	}
	firstProgress := utf8.RuneCountInString(memo.Progress)
	second := compactor.Compact(insertCodingTurns(first, 2))
	body = taskContentOf(second)
	memo, ok = agent.ParseHandoffMemo(body)
	if !ok || strings.Count(body, "<handoff-memo>") != 1 || strings.Count(memo.Progress, "median 17.4") != 1 {
		t.Fatalf("second progress=%q body=%s", memo.Progress, body)
	}
	if againRunes, renderRunes := utf8.RuneCountInString(memo.Progress), utf8.RuneCountInString(memo.Render()); againRunes > firstProgress || againRunes > 1400 || renderRunes > 5000 {
		t.Fatalf("progress grew %d -> %d render=%d", firstProgress, againRunes, renderRunes)
	}
}

func TestSubAgentCompactionFlagOffStillAppends(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	compactor := NewSubAgentCompactor(8000, nil, nil, nil, nil)
	first := compactor.Compact(codingCompactionTurns("do the task", 6))
	body := taskContentOf(first)
	if strings.Contains(body, "<handoff-memo>") || strings.Count(body, "[上下文压缩]") != 1 {
		t.Fatalf("flag off should append one legacy summary: %q", body)
	}
	second := compactor.Compact(insertCodingTurns(first, 2))
	if strings.Count(taskContentOf(second), "[上下文压缩]") != 2 {
		t.Fatalf("flag off should keep appending: %q", taskContentOf(second))
	}
}

func TestSubAgentCompactionHandoffTriggerAndPendingAsk(t *testing.T) {
	conv := codingCompactionTurns("do the task", 2)
	conv = append(conv, map[string]interface{}{"role": "tool", "content": strings.Repeat("x", 4000)})
	tokens := estimateConversationTokensForSubAgent(conv)
	if tokens < 10 {
		t.Fatalf("token estimate=%d", tokens)
	}
	window := tokens * 2
	compactor := NewSubAgentCompactor(window, nil, nil, nil, nil)

	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	if compactor.ShouldCompact(conv) {
		t.Fatal("flag off must stay at 75%, not fire at 50% of the window")
	}
	over := codingCompactionTurns("task", 1)
	over = append(over, map[string]interface{}{"role": "tool", "content": strings.Repeat("y", 20000)})
	capped := NewSubAgentCompactor(1000, nil, nil, nil, nil)
	capped.compactionCount = subAgentMaxCompactionsPerTask
	if capped.ShouldCompact(over) {
		t.Fatal("flag off must stop after 3 compactions")
	}

	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	if !compactor.ShouldCompact(conv) {
		t.Fatal("flag all should compact above 45% of the window")
	}
	capped.compactionCount = subAgentMaxCompactionsPerTask
	if !capped.ShouldCompact(over) {
		t.Fatal("flag on must not keep the 3-compaction cap")
	}
	pending := append(conv, map[string]interface{}{"role": "tool", "content": "Asked user: which file?"})
	if compactor.ShouldCompact(pending) {
		t.Fatal("pending ask must refuse compaction")
	}
	if compacted := compactor.Compact(pending); len(compacted) != len(pending) {
		t.Fatalf("pending ask compact rewrote history: %d -> %d", len(pending), len(compacted))
	}
}

func TestCodingMemoPerRoundCollapsesToolBodies(t *testing.T) {
	commands := []CodingSubAgentCommandResult{
		{Command: "go test ./bench", WorkingDir: "D:/b", Succeeded: false, Summary: "exit status 1"},
		{Command: "go test ./bench -count=1", WorkingDir: "D:/b", Succeeded: false, Summary: "exit status 1"},
		{Command: "go test ./pkg p50=12.5", Succeeded: true, Summary: "ok"},
	}
	compactor := NewSubAgentCompactor(8000, nil, nil, nil, func() []CodingSubAgentCommandResult { return commands })
	conv := codingCompactionTurns("measure the bench", 2)
	conv[len(conv)-1].(map[string]interface{})["content"] = "SECRET_TOOL_BODY"
	hooks := &codingSubAgentHooks{compactor: compactor}

	t.Setenv("MACLAW_HANDOFF_MEMO", "coding")
	t.Setenv("MACLAW_CODING_MEMO_PER_ROUND", "")
	if got := hooks.TransformConversation(conv); got != nil {
		t.Fatal("per-round defaults off")
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "off")
	t.Setenv("MACLAW_CODING_MEMO_PER_ROUND", "1")
	if got := hooks.TransformConversation(conv); got != nil {
		t.Fatal("per-round must not fire when the main flag is off")
	}

	t.Setenv("MACLAW_HANDOFF_MEMO", "all")
	t.Setenv("MACLAW_CODING_MEMO_PER_ROUND", "on")
	got := hooks.TransformConversation(conv)
	if got == nil || len(got) != 2 {
		t.Fatalf("per-round should leave system plus the strip, got %#v", got)
	}
	body := taskContentOf(got)
	if strings.Contains(body, "SECRET_TOOL_BODY") {
		t.Fatalf("next call still sees the tool body: %q", body)
	}
	if !strings.Contains(body, "p50=12.5") || !strings.Contains(body, "不要用同一方式再试") {
		t.Fatalf("strip lost the number or the dead end: %q", body)
	}

	pending := append(append([]interface{}{}, conv...), map[string]interface{}{"role": "tool", "content": "Asked user: which file?"})
	kept := hooks.TransformConversation(pending)
	if kept == nil || len(kept) != len(pending) {
		t.Fatalf("pending ask collapsed: %d", len(kept))
	}
	foundSecret := false
	for _, msg := range kept {
		m, _ := msg.(map[string]interface{})
		if content, _ := m["content"].(string); strings.Contains(content, "SECRET_TOOL_BODY") {
			foundSecret = true
		}
	}
	if !foundSecret {
		t.Fatal("pending ask dropped the tool body")
	}

	round := codingCompactionTurns(agent.SessionRoundPrefix+" continue the wave", 2)
	if got := hooks.TransformConversation(round); got == nil || len(got) != len(round) {
		t.Fatal("session-round wave must not be cleared per round")
	}
}
