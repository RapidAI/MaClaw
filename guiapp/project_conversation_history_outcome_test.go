package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestProjectConversationHistoryKeepsTerminalOutcome(t *testing.T) {
	t.Parallel()
	fromClass := withConversationHistoryOutcome(ProjectConversationHistoryItem{
		Role:    "assistant",
		Content: "先从 GitHub 上取一份目录清单。",
	}, agent.ErrNoProgress, "")
	if fromClass.TerminalClass != agent.ErrNoProgress || fromClass.ResultStatus != "incomplete" {
		t.Fatalf("class projection = %+v", fromClass)
	}
	sentence := withConversationHistoryOutcome(ProjectConversationHistoryItem{
		Role:    "assistant",
		Content: "写入日志失败",
	}, "写入日志失败", "写入日志失败")
	if sentence.TerminalClass != "" || sentence.ResultStatus != "" {
		t.Fatalf("a sentence became an outcome: %+v", sentence)
	}

	tab := projectConversationHistoryFromTabSession([]interface{}{
		map[string]interface{}{
			"role":         "assistant",
			"content":      "先从 GitHub 上取一份目录清单。",
			"resultStatus": "incomplete",
		},
		map[string]interface{}{
			"role":           "assistant",
			"content":        "预算已用尽。",
			"terminal_class": "daily_llm_budget_exceeded",
		},
		map[string]interface{}{
			"role":    "user",
			"content": "读内容",
		},
	})
	if len(tab) != 3 {
		t.Fatalf("tab items = %d, want 3", len(tab))
	}
	if tab[0].ResultStatus != "incomplete" || tab[0].TerminalClass != "" {
		t.Fatalf("stored status = %+v", tab[0])
	}
	if tab[1].TerminalClass != "daily_llm_budget_exceeded" || tab[1].ResultStatus != "incomplete" {
		t.Fatalf("class from snapshot = %+v", tab[1])
	}
	if tab[2].ResultStatus != "" || tab[2].TerminalClass != "" {
		t.Fatalf("user turn gained an outcome: %+v", tab[2])
	}
}

func TestVisibleProjectConversationHistoryOmitsReasoningEcho(t *testing.T) {
	t.Parallel()
	monologue := "The user says \"读内容\"."
	cleanup := "I need to clean up the parameters."
	start := "开始动手，先把一个真实的 DWG 文件取到工作区。"
	github := "先从 GitHub 上取一份目录清单。"
	items := []ProjectConversationHistoryItem{
		{Role: "user", Content: "older"},
		{Role: "assistant", Content: "older answer"},
		{Role: "user", Content: "读内容"},
		{Role: "assistant", Content: monologue, ReasoningContent: monologue, TerminalClass: agent.ErrNoProgress, ResultStatus: "incomplete", planningEcho: true},
		{Role: "assistant", Content: start, ReasoningContent: "Pick a real file.", TerminalClass: agent.ErrNoProgress, ResultStatus: "incomplete"},
		{Role: "system", Content: "note"},
		{Role: "assistant", Content: "  " + github + "  ", ReasoningContent: "List the repo."},
		{Role: "user", Content: "[系统] 检测到重复调用 download_file。", hostContinuation: true},
		{Role: "assistant", Content: "  " + cleanup + " ", ReasoningContent: cleanup, TerminalClass: agent.ErrNoProgress, ResultStatus: "incomplete", planningEcho: true},
		{Role: "user", Content: "继续"},
		{Role: "assistant", Content: cleanup, ReasoningContent: cleanup, TerminalClass: agent.ErrNoProgress, ResultStatus: "incomplete", planningEcho: true},
	}
	got := visibleProjectConversationHistory(items)
	want := []string{"older", "older answer", "读内容", start, "note", "  " + github + "  ", "继续", cleanup}
	if len(got) != len(want) {
		t.Fatalf("visible lines = %#v", contentsOf(got))
	}
	for i, content := range want {
		if got[i].Content != content {
			t.Fatalf("line %d = %q, want %q", i, got[i].Content, content)
		}
	}
	if got[3].Role != "assistant" || got[3].ResultStatus != "incomplete" {
		t.Fatalf("real reply lost its outcome: %+v", got[3])
	}
	// The GitHub sentence was stored without a class. The hidden echo carried it.
	if got[5].TerminalClass != agent.ErrNoProgress || got[5].ResultStatus != "incomplete" {
		t.Fatalf("visible reply did not keep the turn class: %+v", got[5])
	}
	if got[7].Content != cleanup || got[7].ResultStatus != "incomplete" {
		t.Fatalf("echo-only turn was dropped: %+v", got[7])
	}
	if got[1].TerminalClass != "" || got[1].ResultStatus != "" {
		t.Fatalf("older turn inherited this turn's class: %+v", got[1])
	}

	tab := projectConversationHistoryFromTabSession([]interface{}{
		map[string]interface{}{"role": "user", "content": "读内容"},
		map[string]interface{}{
			"role":              "assistant",
			"content":           monologue,
			"reasoning_content": monologue,
			"finish_reason":     "tool_calls",
			"tool_calls":        []interface{}{map[string]interface{}{"id": "c1"}},
			"terminal_class":    agent.ErrNoProgress,
		},
		map[string]interface{}{
			"role":      "assistant",
			"content":   github,
			"reasoning": "List the repo.",
		},
		map[string]interface{}{
			"role":              "user",
			"content":           "[系统] 检测到重复调用 download_file。",
			"host_continuation": true,
		},
		map[string]interface{}{
			"role":              "assistant",
			"content":           cleanup,
			"reasoning_content": cleanup,
			"finishReason":      "tool_calls",
			"result_status":     "incomplete",
		},
		map[string]interface{}{
			"role":              "assistant",
			"content":           "上海今天晴",
			"reasoning_content": "上海今天晴",
			"finish_reason":     "stop",
		},
	})
	if len(tab) != 3 || tab[0].Content != "读内容" || tab[1].Content != github || tab[2].Content != "上海今天晴" {
		t.Fatalf("tab projection = %#v", contentsOf(tab))
	}
	if tab[1].TerminalClass != agent.ErrNoProgress || tab[1].ResultStatus != "incomplete" {
		t.Fatalf("tab reply lost the class that was only on the echo: %+v", tab[1])
	}
	if tab[2].TerminalClass != agent.ErrNoProgress || tab[2].ResultStatus != "incomplete" {
		t.Fatalf("closing answer lost the turn class: %+v", tab[2])
	}

	closing := visibleProjectConversationHistory([]ProjectConversationHistoryItem{
		{Role: "user", Content: "天气"},
		{Role: "assistant", Content: "我先查。", ReasoningContent: "search"},
		{Role: "assistant", Content: monologue, ReasoningContent: monologue, planningEcho: true},
		{Role: "assistant", Content: "上海今天晴", ReasoningContent: "上海今天晴"},
	})
	if len(closing) != 3 || closing[1].Content != "我先查。" || closing[2].Content != "上海今天晴" {
		t.Fatalf("closing answer was treated as planning: %#v", contentsOf(closing))
	}

	bothEchoes := visibleProjectConversationHistory([]ProjectConversationHistoryItem{
		{Role: "user", Content: "读内容"},
		{Role: "assistant", Content: monologue, ReasoningContent: monologue, planningEcho: true},
		{Role: "assistant", Content: cleanup, ReasoningContent: cleanup, planningEcho: true},
	})
	if len(bothEchoes) != 3 {
		t.Fatalf("a turn with no real reply must keep its echoes, got %#v", contentsOf(bothEchoes))
	}

	split := visibleProjectConversationHistory([]ProjectConversationHistoryItem{
		{Role: "user", Content: "读内容"},
		{Role: "assistant", Content: github, ReasoningContent: "List the repo."},
		{Role: "assistant", Content: cleanup, ReasoningContent: cleanup, TerminalClass: agent.ErrNoProgress, ResultStatus: "incomplete", planningEcho: true},
		{Role: "assistant", Content: "另一句", ReasoningContent: "other", TerminalClass: "daily_llm_budget_exceeded", ResultStatus: "incomplete"},
	})
	if len(split) != 3 || split[1].TerminalClass != "" || split[1].ResultStatus != "" {
		t.Fatalf("two classes must not be painted onto a blank reply: %+v", split[1])
	}
}

func contentsOf(items []ProjectConversationHistoryItem) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Role + ":" + item.Content
	}
	return out
}
