package agent

import "testing"

func TestResolveAssistantFinishReason(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		finishReason string
		hasToolCalls bool
		want         string
	}{
		{name: "preserve stop", finishReason: "stop", want: "stop"},
		{name: "preserve tool_calls", finishReason: "tool_calls", hasToolCalls: true, want: "tool_calls"},
		{name: "preserve length", finishReason: "length", want: "length"},
		{name: "trim whitespace", finishReason: "  stop  ", want: "stop"},
		{name: "default stop", finishReason: "", want: "stop"},
		{name: "default tool_calls", finishReason: "", hasToolCalls: true, want: "tool_calls"},
		{name: "whitespace only with tools", finishReason: "   ", hasToolCalls: true, want: "tool_calls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveAssistantFinishReason(tc.finishReason, tc.hasToolCalls)
			if got != tc.want {
				t.Fatalf("ResolveAssistantFinishReason(%q, %v) = %q, want %q",
					tc.finishReason, tc.hasToolCalls, got, tc.want)
			}
		})
	}
}

func TestConversationEntryToMessageOmitsFinishReason(t *testing.T) {
	t.Parallel()
	entry := ConversationEntry{
		Role:          "assistant",
		Content:       "hello",
		FinishReason:  "stop",
		TerminalClass: ErrNoProgress,
	}
	msg, ok := entry.ToMessage().(map[string]interface{})
	if !ok {
		t.Fatalf("ToMessage type = %T", entry.ToMessage())
	}
	if _, exists := msg["finish_reason"]; exists {
		t.Fatalf("ToMessage must not include finish_reason for the LLM API: %#v", msg)
	}
	if _, exists := msg["terminal_class"]; exists {
		t.Fatalf("ToMessage must not include terminal_class for the LLM API: %#v", msg)
	}
	hint := HostUserEntry("[系统] 还有未关闭问题。")
	if !hint.HostContinuation || hint.Role != "user" {
		t.Fatalf("host continuation = %+v", hint)
	}
	hintMsg, ok := hint.ToMessage().(map[string]interface{})
	if !ok {
		t.Fatalf("host ToMessage type = %T", hint.ToMessage())
	}
	if _, exists := hintMsg["host_continuation"]; exists {
		t.Fatalf("ToMessage must not include host_continuation: %#v", hintMsg)
	}
}

func TestAssistantContentRepeatsReasoning(t *testing.T) {
	t.Parallel()
	if !AssistantContentRepeatsReasoning("  I need to clean up the parameters. ", "I need to clean up the parameters.") {
		t.Fatal("trimmed copy of the thinking trace must repeat it")
	}
	if AssistantContentRepeatsReasoning("先从 GitHub 上取一份目录清单。", "List the repo.") {
		t.Fatal("a reply with different reasoning is not an echo")
	}
	if AssistantContentRepeatsReasoning("", "only thinking") || AssistantContentRepeatsReasoning("answer", "") {
		t.Fatal("an empty side is not an echo")
	}
	if !AssistantPlanningEcho("I need to clean up the parameters.", "I need to clean up the parameters.", "tool_calls", nil) {
		t.Fatal("a tool turn that repeats reasoning is planning")
	}
	if !AssistantPlanningEcho("I need to clean up the parameters.", "I need to clean up the parameters.", "", []any{map[string]any{"id": "c1"}}) {
		t.Fatal("a tool call list marks the repeated line as planning")
	}
	if AssistantPlanningEcho("上海今天晴", "上海今天晴", "stop", nil) {
		t.Fatal("a closing answer that repeats reasoning is still the reply")
	}
	if AssistantPlanningEcho("先从 GitHub 上取一份目录清单。", "List the repo.", "tool_calls", []any{map[string]any{"id": "c1"}}) {
		t.Fatal("a different sentence beside a tool call is not planning")
	}
}

func TestStampTurnTerminalClass(t *testing.T) {
	t.Parallel()
	entries := []ConversationEntry{
		{Role: "assistant", Content: "older turn"},
		{Role: "user", Content: "读内容"},
		{Role: "assistant", Content: "先从 GitHub 上取一份目录清单。"},
		{Role: "tool", Content: "HTTP 404", ToolName: "download_file"},
		{Role: "assistant", Content: "I need to clean up the parameters."},
		{Role: "assistant", Content: "   "},
	}
	StampTurnTerminalClass(entries, 1, ErrNoProgress)
	if entries[0].TerminalClass != "" {
		t.Fatalf("earlier turn stamped: %+v", entries[0])
	}
	if entries[2].TerminalClass != ErrNoProgress || entries[4].TerminalClass != ErrNoProgress {
		t.Fatalf("turn assistants = %q, %q", entries[2].TerminalClass, entries[4].TerminalClass)
	}
	if entries[3].TerminalClass != "" || entries[5].TerminalClass != "" {
		t.Fatalf("tool or blank assistant stamped: %+v", entries)
	}
	StampTurnTerminalClass(entries, 1, "max iterations reached")
	if entries[2].TerminalClass != ErrNoProgress {
		t.Fatalf("a sentence replaced the class: %q", entries[2].TerminalClass)
	}
	StampTurnTerminalClass(entries, 1, "")
	if entries[2].TerminalClass != ErrNoProgress {
		t.Fatalf("empty code cleared the class: %q", entries[2].TerminalClass)
	}
}

func TestSaveKeepsTerminalClassAfterCheckpoint(t *testing.T) {
	cm := NewConversationMemory()
	user := "u-terminal-class"
	checkpoint := []ConversationEntry{
		{Role: "user", Content: "读内容"},
		{Role: "assistant", Content: "先从 GitHub 上取一份目录清单。", ToolCalls: []any{map[string]any{"id": "c1"}}},
		{Role: "tool", Content: "HTTP 404", ToolCallID: "c1", ToolName: "download_file"},
	}
	cm.Save(user, checkpoint)
	stamped := append([]ConversationEntry(nil), checkpoint...)
	StampTurnTerminalClass(stamped, 0, ErrNoProgress)
	cm.Save(user, stamped)
	var got string
	for _, entry := range cm.Load(user) {
		if entry.Role == "assistant" {
			got = entry.TerminalClass
		}
	}
	if got != ErrNoProgress {
		t.Fatalf("reloaded class = %q", got)
	}
	cm.Save(user, checkpoint)
	got = ""
	for _, entry := range cm.Load(user) {
		if entry.Role == "assistant" {
			got = entry.TerminalClass
		}
	}
	if got != ErrNoProgress {
		t.Fatalf("checkpoint resave cleared the class: %q", got)
	}
}

func TestTerminalClassSurvivesCheckpointThatAlreadyHasIDs(t *testing.T) {
	cm := NewConversationMemory()
	user := "u-terminal-ids"
	cm.Save(user, []ConversationEntry{
		{Role: "user", Content: "上一句"},
		{Role: "assistant", Content: "上一答"},
	})
	prior := cm.Load(user)
	if len(prior) != 2 || prior[0].ID == "" || prior[1].ID == "" {
		t.Fatalf("prior history = %+v", prior)
	}
	turn := []ConversationEntry{
		{Role: "user", Content: "读内容"},
		{Role: "assistant", Content: "先从 GitHub 上取一份目录清单。", ToolCalls: []any{map[string]any{"id": "c1"}}},
		{Role: "tool", Content: "HTTP 404", ToolCallID: "c1", ToolName: "download_file"},
	}
	cm.Save(user, append(append([]ConversationEntry(nil), prior...), turn...))
	stamped := append([]ConversationEntry(nil), turn...)
	StampTurnTerminalClass(stamped, 0, ErrNoProgress)
	cm.Save(user, append(append([]ConversationEntry(nil), prior...), stamped...))

	loaded := cm.Load(user)
	reads := 0
	var got string
	for _, entry := range loaded {
		if entry.Role == "user" && entry.Content == "读内容" {
			reads++
		}
		if entry.Role == "assistant" && entry.Content == "先从 GitHub 上取一份目录清单。" {
			got = entry.TerminalClass
		}
	}
	if reads != 1 {
		t.Fatalf("visible copies of this turn = %d, history=%+v", reads, loaded)
	}
	if got != ErrNoProgress {
		t.Fatalf("active branch class = %q", got)
	}
}
