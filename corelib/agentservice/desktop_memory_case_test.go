package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestDesktopBotMemoryCases(t *testing.T) {
	memory.ResetDesktopBooks()
	agent.ResetPromptPrefixPins()
	t.Cleanup(func() {
		memory.ResetDesktopBooks()
		agent.ResetPromptPrefixPins()
	})

	cb := &coreAgentCallbacks{
		botID:     "bot-mem",
		session:   Session{ID: "conv-mem"},
		user:      User{ID: "user-mem"},
		principal: Principal{UserID: "user-mem"},
		llmCfg:    corelib.MaclawLLMConfig{ProviderName: "openai", Model: "case"},
	}
	first := cb.promptMemorySnapshot()
	if !strings.Contains(first, memory.DesktopMemoryFenceOpen) || !strings.Contains(first, memory.DesktopMemoryFenceHeader) || !strings.Contains(first, "笔记: 无") {
		t.Fatalf("excerpt = %q", first)
	}
	book := memory.DesktopBook("user-mem")
	if res := book.Write(memory.WriteRequest{Layer: memory.LayerProfile, Content: "住在杭州", Source: "user"}); res.Error != "" {
		t.Fatal(res.Error)
	}
	second := cb.promptMemorySnapshot()
	if second != first {
		t.Fatalf("excerpt changed\nfirst %q\nsecond %q", first, second)
	}
	if strings.Contains(second, "住在杭州") {
		t.Fatal("pinned excerpt absorbed the write")
	}
	if !strings.Contains(book.Render(), "住在杭州") {
		t.Fatal("hot copy missed the write")
	}
	if agent.PromptPrefixUnexpectedTotal(agent.PromptSegmentMemorySnapshot, "openai", "case") != 0 {
		t.Fatal("a stable excerpt was reported as a prefix bug")
	}

	sent := cb.pinDesktopSystemPrompt("系统规则")
	sentAgain := cb.pinDesktopSystemPrompt("系统规则")
	if sent != "系统规则" || sentAgain != sent {
		t.Fatalf("system pin = %q then %q", sent, sentAgain)
	}

	tools := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "desktop", "description": "desktop"}},
		{"type": "function", "function": map[string]interface{}{"name": "memory", "description": "themes scenes derived_surgery"}},
	}
	pinnedTools := cb.PinModelTools(tools)
	raw := agent.CanonicalToolJSON(pinnedTools)
	for _, name := range []string{"memory_write", "memory_forget", "memory_search"} {
		if !strings.Contains(raw, `"name":"`+name+`"`) {
			t.Fatalf("pinned tools missing %s: %s", name, raw)
		}
	}
	if strings.Contains(raw, `"name":"memory"`) || strings.Contains(raw, "themes") || strings.Contains(raw, "derived_surgery") || strings.Contains(raw, "scenes") {
		t.Fatalf("old memory tool still pinned: %s", raw)
	}
	changed := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "desktop", "description": "desktop"}},
		{"type": "function", "function": map[string]interface{}{"name": "memory", "description": "different"}},
	}
	if agent.CanonicalToolJSON(cb.PinModelTools(changed)) != raw {
		t.Fatal("tool pin moved")
	}
	if agent.PromptPrefixUnexpectedTotal(agent.PromptSegmentTools, "openai", "case") != 0 {
		t.Fatal("stable tool swap was reported as a prefix bug")
	}

	cb.messageMetadata = map[string]string{"computer_helper": "1", "helper_id": "h9"}
	got := cb.desktopMemoryCall("memory_write", map[string]interface{}{
		"layer": "profile", "content": "帮手改画像", "source": "user",
	})
	if got != memory.HelperProfileError {
		t.Fatalf("helper write = %q", got)
	}
	if strings.Contains(cb.promptMemorySnapshot(), "帮手改画像") {
		t.Fatal("helper write entered the pinned excerpt")
	}
}
