package agentservice

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestDesktopBotSummaryCases(t *testing.T) {
	memory.ResetDesktopBooks()
	agent.ResetPromptPrefixPins()
	agent.ResetIdleSummaries()
	t.Cleanup(func() {
		memory.ResetDesktopBooks()
		agent.ResetPromptPrefixPins()
		agent.ResetIdleSummaries()
	})
	t.Run("commit_refreshes_excerpt", func(t *testing.T) {
		cb := summaryCallbacks("conv-sum", "user-sum")
		first := cb.promptMemorySnapshot()
		book := memory.DesktopBook("user-sum")
		if res := book.Write(memory.WriteRequest{Layer: memory.LayerProfile, Content: "住在杭州", Source: "user"}); res.Error != "" {
			t.Fatal(res.Error)
		}
		if cb.promptMemorySnapshot() != first || strings.Contains(first, "住在杭州") {
			t.Fatal("excerpt moved before the summary committed")
		}
		if cb.pinDesktopSystemPrompt("系统规则") != "系统规则" {
			t.Fatal("system pin")
		}
		cb.noteDesktopIdleSummary(agent.LoopResult{Usage: agent.TurnUsage{InputTokens: 100001}})
		before, ok := agent.PinnedPrefix(cb.botID, cb.session.ID)
		if !ok {
			t.Fatal("pin missing")
		}
		got := agent.TickIdleSummary(cb.botID, cb.session.ID, time.Now().Add(3*time.Minute))
		if !got.Built || got.Request == nil {
			t.Fatalf("request = %+v", got)
		}
		if got.Request.SystemHash != agent.PromptSegmentHash(before.System) || got.Request.MemoryHash != agent.PromptSegmentHash(before.MemorySnapshot) || got.Request.ToolsHash != agent.PromptSegmentHash(before.Tools) || got.Request.FirstHash != agent.PromptSegmentHash(before.FirstMessage) {
			t.Fatalf("request hashes drifted: %#v", got.Request)
		}
		if got.Request.System != before.System || strings.Contains(got.Request.System, "你是一个对话摘要助手") {
			t.Fatalf("system = %q", got.Request.System)
		}
		if got.Woke || got.Ensured || got.LastUsed || got.MachineHeld || memory.DesktopMemoryWakesMachine() {
			t.Fatal("summary touched a machine")
		}
		committed := agent.CommitIdleSummary(cb.botID, cb.session.ID, agent.IdleSummaryCommit{Summary: "短摘要"})
		if committed.State != "committed" || len(committed.Prewarms) != 1 || committed.Prewarms[0].Reason != "after_summary" || committed.Prewarms[0].SendToUser {
			t.Fatalf("commit = %+v", committed)
		}
		if !strings.Contains(book.Excerpt(), "住在杭州") || cb.promptMemorySnapshot() != book.Excerpt() {
			t.Fatalf("excerpt = %q", book.Excerpt())
		}
		if agent.PromptPrefixUnexpectedTotal(agent.PromptSegmentMemorySnapshot, "openai", "case") != 0 {
			t.Fatal("refreshed excerpt was a prefix bug")
		}
		after, _ := agent.PinnedPrefix(cb.botID, cb.session.ID)
		if after.FirstMessage != "短摘要" || after.System != before.System || after.MemorySnapshot != book.Excerpt() {
			t.Fatalf("pin = %#v", after)
		}
	})
	t.Run("return_keeps_excerpt", func(t *testing.T) {
		cb := summaryCallbacks("conv-back", "user-back")
		first := cb.promptMemorySnapshot()
		book := memory.DesktopBook("user-back")
		if res := book.Write(memory.WriteRequest{Layer: memory.LayerProfile, Content: "临时地址", Source: "user"}); res.Error != "" {
			t.Fatal(res.Error)
		}
		if cb.pinDesktopSystemPrompt("系统规则") != "系统规则" {
			t.Fatal("system pin")
		}
		cb.noteDesktopIdleSummary(agent.LoopResult{Usage: agent.TurnUsage{InputTokens: 150000}})
		agent.NoteUserReturned(cb.botID, cb.session.ID, time.Now())
		got := agent.TickIdleSummary(cb.botID, cb.session.ID, time.Now().Add(3*time.Minute))
		if got.Built || got.Reason != "user_returned" {
			t.Fatalf("return = %+v", got)
		}
		agent.CommitIdleSummary(cb.botID, cb.session.ID, agent.IdleSummaryCommit{Summary: "不该装上", NextMemory: "新摘录"})
		if book.Excerpt() != first || strings.Contains(book.Excerpt(), "临时地址") || cb.promptMemorySnapshot() != first {
			t.Fatalf("excerpt moved: %q", book.Excerpt())
		}
		if !strings.Contains(book.Render(), "临时地址") {
			t.Fatal("hot copy lost the write")
		}
	})
	body, err := os.ReadFile("desktop_idle_summary.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, forbidden := range []string{"desktopd", "last_used", "Ensure(", "AutoCompressConversation"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("turn note refers to %s", forbidden)
		}
	}
}

func summaryCallbacks(sessionID, userID string) *coreAgentCallbacks {
	return &coreAgentCallbacks{
		botID:     "bot-sum",
		session:   Session{ID: sessionID},
		user:      User{ID: userID},
		principal: Principal{UserID: userID},
		llmCfg:    corelib.MaclawLLMConfig{ProviderName: "openai", Model: "case"},
	}
}
