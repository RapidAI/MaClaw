package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestDesktopBotSummaryCases(t *testing.T) {
	for _, id := range []string{
		"summary_reuses_prefix",
		"user_returns_aborts",
		"summary_does_not_ensure",
		"cold_cache_skips",
	} {
		t.Run(id, func(t *testing.T) {
			ResetPromptPrefixPins()
			ResetIdleSummaries()
			t.Cleanup(func() {
				ResetPromptPrefixPins()
				ResetIdleSummaries()
			})
			runSummaryCase(t, readSummaryCase(t, id))
		})
	}
	t.Run("history_mismatch_drops", runHistoryMismatchDrops)
	t.Run("cold_window_clamped", runCacheWindowClamped)
	t.Run("below_threshold_skips", runBelowThresholdSkips)
}

type summaryCase struct {
	ID         string `json:"id"`
	Expect     string `json:"expect"`
	Early      bool   `json:"early"`
	Tokens     int    `json:"tokens"`
	Provider   string `json:"provider"`
	TTL        string `json:"ttl"`
	Idle       string `json:"idle"`
	Margin     string `json:"margin"`
	TurnAt     string `json:"turn_at"`
	ReturnAt   string `json:"return_at"`
	TickAt     string `json:"tick_at"`
	Tools      string `json:"tools"`
	System     string `json:"system"`
	Memory     string `json:"memory"`
	First      string `json:"first"`
	History    string `json:"history"`
	Summary    string `json:"summary"`
	NextTools  string `json:"next_tools"`
	NextMemory string `json:"next_memory"`
}

func readSummaryCase(t *testing.T, id string) summaryCase {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c summaryCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runSummaryCase(t *testing.T, c summaryCase) {
	t.Helper()
	meta := PromptPrefixMeta{
		ConversationID: c.ID,
		BotID:          "bot-" + c.ID,
		Provider:       c.Provider,
		Model:          "case",
	}
	PrepareConversationPrefix(meta, fullPrefixInput(c.Tools, c.System, c.Memory, c.First, c.History))
	before, ok := PinnedPrefix(meta.BotID, meta.ConversationID)
	if !ok {
		t.Fatal("pin missing")
	}
	beforePrefix := PromptPrefixHash(before.Tools, before.System, before.MemorySnapshot, before.FirstMessage)
	turnAt := mustSummaryTime(t, c.TurnAt)
	NoteIdleTurn(IdleTurnNote{
		ConversationID: meta.ConversationID,
		BotID:          meta.BotID,
		Provider:       c.Provider,
		Model:          "case",
		PromptTokens:   c.Tokens,
		TurnEnded:      true,
		At:             turnAt,
		CacheTTL:       mustSummaryDuration(t, c.TTL),
		Idle:           mustSummaryDuration(t, c.Idle),
		Margin:         mustSummaryDuration(t, c.Margin),
	})
	if c.Early {
		early := TickIdleSummary(meta.BotID, meta.ConversationID, turnAt)
		if early.Built || early.State != idleDue {
			t.Fatalf("early tick = %+v", early)
		}
	}
	if c.ReturnAt != "" {
		NoteUserReturned(meta.BotID, meta.ConversationID, mustSummaryTime(t, c.ReturnAt))
	}
	got := TickIdleSummary(meta.BotID, meta.ConversationID, mustSummaryTime(t, c.TickAt))
	if got.Woke || got.Ensured || got.LastUsed || got.MachineHeld {
		t.Fatalf("scheduler touched a machine: %+v", got)
	}
	switch c.Expect {
	case "send":
		assertSummaryReusesPrefix(t, c, meta, before, beforePrefix, got)
	case "abort":
		assertSummaryAborted(t, c, meta, before, got)
	case "skip":
		assertSummarySkipped(t, c, meta, before, got)
	default:
		t.Fatalf("expect %s", c.Expect)
	}
	if c.ID == "summary_does_not_ensure" {
		assertSummaryLeavesTheDesktop(t)
	}
}

func assertSummaryReusesPrefix(t *testing.T, c summaryCase, meta PromptPrefixMeta, before PromptPrefixSegments, beforePrefix string, got IdleSummaryStatus) {
	t.Helper()
	if !got.Built || got.Request == nil {
		t.Fatalf("summary was not built: %+v", got)
	}
	req := got.Request
	if req.Tools != before.Tools || req.System != before.System || req.MemorySnapshot != before.MemorySnapshot || req.FirstMessage != before.FirstMessage || req.History != before.History {
		t.Fatalf("request moved the prefix: %#v", req)
	}
	if req.ToolsHash != PromptSegmentHash(before.Tools) || req.SystemHash != PromptSegmentHash(before.System) || req.MemoryHash != PromptSegmentHash(before.MemorySnapshot) || req.FirstHash != PromptSegmentHash(before.FirstMessage) || req.PrefixHash != beforePrefix {
		t.Fatalf("request hashes != pin: %#v", req)
	}
	if req.System != c.System || strings.Contains(req.System, "Summarize the following conversation excerpt") || strings.Contains(req.System, "你是一个对话摘要助手") {
		t.Fatalf("summary started another system prompt: %q", req.System)
	}
	if req.Instruction == "" || req.Instruction == req.System || !strings.Contains(req.Instruction, "不调用工具") || strings.Contains(req.Instruction, "Summarize the following") {
		t.Fatalf("instruction = %q", req.Instruction)
	}
	committed := CommitIdleSummary(meta.BotID, meta.ConversationID, IdleSummaryCommit{
		Summary:    c.Summary,
		NextTools:  c.NextTools,
		NextMemory: c.NextMemory,
	})
	if committed.State != idleCommitted || committed.Woke || committed.Ensured || committed.LastUsed || committed.MachineHeld {
		t.Fatalf("commit = %+v", committed)
	}
	if len(committed.Prewarms) != 1 {
		t.Fatalf("prewarms = %+v", committed.Prewarms)
	}
	again := CommitIdleSummary(meta.BotID, meta.ConversationID, IdleSummaryCommit{Summary: "第二遍"})
	if len(again.Prewarms) != 1 || again.Prewarms[0].PrefixHash != committed.Prewarms[0].PrefixHash {
		t.Fatalf("second commit warmed again: %+v", again.Prewarms)
	}
	after, ok := PinnedPrefix(meta.BotID, meta.ConversationID)
	if !ok {
		t.Fatal("pin lost")
	}
	if after.System != c.System || after.Tools != c.NextTools || after.MemorySnapshot != c.NextMemory || after.FirstMessage != c.Summary {
		t.Fatalf("installed prefix = %#v", after)
	}
	warm := committed.Prewarms[0]
	if warm.Reason != "after_summary" || warm.MaxTokens != PrefixPrewarmMaxTokens || warm.SendToUser || warm.Tail == "" || warm.Tail == after.System {
		t.Fatalf("prewarm = %+v", warm)
	}
	if warm.PrefixHash == beforePrefix || warm.SystemHash != PromptSegmentHash(c.System) {
		t.Fatalf("prewarm hash old=%s warm=%+v", beforePrefix, warm)
	}
	if warm.PrefixHash != PromptPrefixHash(after.Tools, after.System, after.MemorySnapshot, after.FirstMessage) {
		t.Fatal("prewarm hash is not the new prefix")
	}
	if PromptPrefixUnexpectedTotal(PromptSegmentSystem, c.Provider, "case") != 0 {
		t.Fatal("idle summary was counted as a prefix bug")
	}
}

func assertSummaryAborted(t *testing.T, c summaryCase, meta PromptPrefixMeta, before PromptPrefixSegments, got IdleSummaryStatus) {
	t.Helper()
	if got.Built || got.Request != nil || got.State != idleAborted || got.Reason != "user_returned" {
		t.Fatalf("abort = %+v", got)
	}
	committed := CommitIdleSummary(meta.BotID, meta.ConversationID, IdleSummaryCommit{Summary: c.Summary, NextTools: "tools-new", NextMemory: "新记忆"})
	if committed.Built || len(committed.Prewarms) != 0 {
		t.Fatalf("aborted commit installed a summary: %+v", committed)
	}
	after, _ := PinnedPrefix(meta.BotID, meta.ConversationID)
	if after != before {
		t.Fatalf("prefix moved\n before %#v\n after %#v", before, after)
	}
}

func assertSummarySkipped(t *testing.T, c summaryCase, meta PromptPrefixMeta, before PromptPrefixSegments, got IdleSummaryStatus) {
	t.Helper()
	if got.Built || got.Request != nil || got.State != idleSkipped || got.Reason != "cache_expired" {
		t.Fatalf("skip = %+v", got)
	}
	committed := CommitIdleSummary(meta.BotID, meta.ConversationID, IdleSummaryCommit{Summary: c.Summary, NextMemory: "新记忆"})
	if len(committed.Prewarms) != 0 {
		t.Fatalf("cold commit warmed: %+v", committed.Prewarms)
	}
	after, _ := PinnedPrefix(meta.BotID, meta.ConversationID)
	if after != before || strings.Contains(after.FirstMessage, c.Summary) {
		t.Fatalf("cold summary installed: %#v", after)
	}
}

func assertSummaryLeavesTheDesktop(t *testing.T) {
	t.Helper()
	if memory.DesktopMemoryWakesMachine() {
		t.Fatal("a summary woke a machine")
	}
	body, err := os.ReadFile("idle_summary.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, forbidden := range []string{"desktopd", "last_used", "Ensure(", "AutoCompressConversation", "prompt_cache_flight", "context_compressor"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("idle summary refers to %s", forbidden)
		}
	}
}

func runHistoryMismatchDrops(t *testing.T) {
	ResetPromptPrefixPins()
	ResetIdleSummaries()
	t.Cleanup(func() {
		ResetPromptPrefixPins()
		ResetIdleSummaries()
	})
	meta := PromptPrefixMeta{ConversationID: "history-mismatch", BotID: "bot-hist", Provider: "openai", Model: "case"}
	PrepareConversationPrefix(meta, fullPrefixInput("tools-v1", "系统规则", "记忆", "user:第一句", "assistant:原来"))
	before, _ := PinnedPrefix(meta.BotID, meta.ConversationID)
	turnAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	NoteIdleTurn(IdleTurnNote{
		ConversationID: meta.ConversationID,
		BotID:          meta.BotID,
		Provider:       "openai",
		Model:          "case",
		PromptTokens:   100001,
		TurnEnded:      true,
		At:             turnAt,
		CacheTTL:       5 * time.Minute,
	})
	SetIdleSummaryMemoryRefresh(meta.BotID, meta.ConversationID, func() string {
		t.Fatal("a dropped summary refreshed the excerpt")
		return "不该换"
	})
	got := TickIdleSummary(meta.BotID, meta.ConversationID, turnAt.Add(2*time.Minute))
	if !got.Built || got.Request == nil || got.Request.PrefixHash != PromptPrefixHash(before.Tools, before.System, before.MemorySnapshot, before.FirstMessage) {
		t.Fatalf("request = %+v", got)
	}
	PrepareConversationPrefix(meta, fullPrefixInput("tools-v1", "系统规则", "记忆", "user:第一句", "assistant:原来\nuser:回来了"))
	dropped := CommitIdleSummary(meta.BotID, meta.ConversationID, IdleSummaryCommit{Summary: "不该装上", NextTools: "tools-v2", NextMemory: "新记忆"})
	if dropped.State != idleAborted || dropped.Reason != "history_mismatch" || len(dropped.Prewarms) != 0 {
		t.Fatalf("drop = %+v", dropped)
	}
	after, _ := PinnedPrefix(meta.BotID, meta.ConversationID)
	if after.Tools != before.Tools || after.System != before.System || after.MemorySnapshot != before.MemorySnapshot || after.FirstMessage != before.FirstMessage {
		t.Fatalf("prefix moved: %#v", after)
	}
	if !strings.Contains(after.History, "回来了") {
		t.Fatalf("history = %q", after.History)
	}
}

func runCacheWindowClamped(t *testing.T) {
	ResetIdleSummaries()
	t.Cleanup(ResetIdleSummaries)
	if ClampPromptCacheTTL(0) != PromptCacheTTLFloor || ClampPromptCacheTTL(time.Second) != 5*time.Minute || ClampPromptCacheTTL(3*time.Hour) != time.Hour {
		t.Fatalf("clamp 0=%s 1s=%s 3h=%s", ClampPromptCacheTTL(0), ClampPromptCacheTTL(time.Second), ClampPromptCacheTTL(3*time.Hour))
	}
	if PromptCacheTTL("unknown-vendor") != 5*time.Minute || PromptCacheTTL("anthropic") != 5*time.Minute {
		t.Fatal("an unconfigured provider did not use the floor")
	}
	restore := SetPromptCacheTTL("openai", 30*time.Minute)
	if PromptCacheTTL("openai") != 30*time.Minute {
		t.Fatalf("openai ttl = %s", PromptCacheTTL("openai"))
	}
	restore()
	restore = SetPromptCacheTTL("anthropic", 2*time.Hour)
	if PromptCacheTTL("anthropic") != time.Hour {
		t.Fatalf("anthropic ttl = %s", PromptCacheTTL("anthropic"))
	}
	restore()
}

func runBelowThresholdSkips(t *testing.T) {
	ResetPromptPrefixPins()
	ResetIdleSummaries()
	t.Cleanup(func() {
		ResetPromptPrefixPins()
		ResetIdleSummaries()
	})
	meta := PromptPrefixMeta{ConversationID: "below", BotID: "bot-below", Provider: "openai", Model: "case"}
	PrepareConversationPrefix(meta, fullPrefixInput("tools", "系统规则", "记忆", "user:短", ""))
	turnAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	NoteIdleTurn(IdleTurnNote{
		ConversationID: meta.ConversationID,
		BotID:          meta.BotID,
		PromptTokens:   PromptSummaryMinTokens,
		TurnEnded:      true,
		At:             turnAt,
	})
	got := TickIdleSummary(meta.BotID, meta.ConversationID, turnAt.Add(time.Hour))
	if got.Built || got.State != "" {
		t.Fatalf("threshold = %+v", got)
	}
	due := PromptPrefixMeta{ConversationID: "pending", BotID: "bot-pending", Provider: "openai", Model: "case"}
	PrepareConversationPrefix(due, fullPrefixInput("tools", "系统规则", "记忆", "user:长", "assistant:还在"))
	NoteIdleTurn(IdleTurnNote{
		ConversationID: due.ConversationID,
		BotID:          due.BotID,
		PromptTokens:   PromptSummaryMinTokens + 1,
		TurnEnded:      true,
		At:             turnAt,
		CacheTTL:       5 * time.Minute,
	})
	NoteIdleTurn(IdleTurnNote{
		ConversationID: due.ConversationID,
		BotID:          due.BotID,
		PromptTokens:   PromptSummaryMinTokens + 1,
		TurnEnded:      true,
		PendingAsk:     true,
		At:             turnAt.Add(time.Minute),
	})
	held := TickIdleSummary(due.BotID, due.ConversationID, turnAt.Add(2*time.Minute))
	if held.Built || held.Reason != "pending_ask" {
		t.Fatalf("pending ask sent: %+v", held)
	}
}

func mustSummaryTime(t *testing.T, text string) time.Time {
	t.Helper()
	got, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustSummaryDuration(t *testing.T, text string) time.Duration {
	t.Helper()
	if text == "" {
		return 0
	}
	got, err := time.ParseDuration(text)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
