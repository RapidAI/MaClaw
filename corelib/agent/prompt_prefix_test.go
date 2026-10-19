package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestPromptPrefixStableTwoTurns(t *testing.T) {
	ResetPromptPrefixPins()
	t.Cleanup(ResetPromptPrefixPins)
	meta := PromptPrefixMeta{ConversationID: "conv-stable", BotID: "bot-1", Provider: "openai", Model: "m"}
	first := fullPrefixInput("tools-v1", "system-v1", "memory-v1", "打开首页", "")
	turn1 := PrepareConversationPrefix(meta, first)
	if !turn1.Fresh || len(turn1.Bugs) != 0 {
		t.Fatalf("first turn fresh=%v bugs=%v", turn1.Fresh, turn1.Bugs)
	}
	second := fullPrefixInput("tools-v1", "system-v1", "memory-v1", "打开首页", "user:再看一眼")
	turn2 := PrepareConversationPrefix(meta, second)
	if turn2.Fresh || turn2.Refreshed || len(turn2.Bugs) != 0 {
		t.Fatalf("second turn fresh=%v refreshed=%v bugs=%v", turn2.Fresh, turn2.Refreshed, turn2.Bugs)
	}
	if turn1.ToolsHash != turn2.ToolsHash || turn1.SystemHash != turn2.SystemHash || turn1.MemoryHash != turn2.MemoryHash || turn1.FirstHash != turn2.FirstHash || turn1.PrefixHash != turn2.PrefixHash {
		t.Fatalf("prefix moved:\n 1 %#v\n 2 %#v", turn1, turn2)
	}
	if turn1.HistoryHash == turn2.HistoryHash {
		t.Fatal("history hash stayed put after an append")
	}
	if turn2.Send.History != "user:再看一眼" {
		t.Fatalf("history = %q", turn2.Send.History)
	}
}

func TestPromptPrefixKeepsAgentsAndToolsWhenTheRebuildChanges(t *testing.T) {
	ResetPromptPrefixPins()
	t.Cleanup(ResetPromptPrefixPins)
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("rule v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := PromptPrefixMeta{ConversationID: "conv-agents", BotID: "bot-1", Provider: "anthropic", Model: "m"}
	system := SystemWithAgentsMD("base", ReadAgentsMD(path))
	turn1 := PrepareConversationPrefix(meta, fullPrefixInput("mcp-on", system, "snap", "第一句", ""))
	if err := os.WriteFile(path, []byte("rule v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	rebuilt := SystemWithAgentsMD("base", ReadAgentsMD(path))
	if rebuilt == system {
		t.Fatal("the file edit did not change the rebuilt system segment")
	}
	turn2 := PrepareConversationPrefix(meta, fullPrefixInput("mcp-off", rebuilt, "snap", "第一句", "user:继续"))
	if turn2.Send.System != system || turn2.Send.Tools != "mcp-on" {
		t.Fatalf("sent system %q tools %q", turn2.Send.System, turn2.Send.Tools)
	}
	if turn1.SystemHash != turn2.SystemHash || turn1.ToolsHash != turn2.ToolsHash {
		t.Fatal("pinned hashes moved")
	}
	if PromptPrefixUnexpectedTotal(PromptSegmentSystem, "anthropic", "m") != 1 || PromptPrefixUnexpectedTotal(PromptSegmentTools, "anthropic", "m") != 1 {
		t.Fatalf("counters system=%d tools=%d", PromptPrefixUnexpectedTotal(PromptSegmentSystem, "anthropic", "m"), PromptPrefixUnexpectedTotal(PromptSegmentTools, "anthropic", "m"))
	}
	seen := map[string]bool{}
	for _, bug := range turn2.Bugs {
		if bug.Kind != "prompt_prefix_bug" {
			t.Fatalf("bug kind %s", bug.Kind)
		}
		seen[bug.Segment] = true
	}
	if !seen[PromptSegmentSystem] || !seen[PromptSegmentTools] {
		t.Fatalf("bugs = %+v", turn2.Bugs)
	}
}

func TestPromptPrefixSummaryIsNotABug(t *testing.T) {
	ResetPromptPrefixPins()
	t.Cleanup(ResetPromptPrefixPins)
	meta := PromptPrefixMeta{ConversationID: "conv-sum", BotID: "bot-1", Provider: "openai", Model: "m"}
	PrepareConversationPrefix(meta, fullPrefixInput("tools", "system", "memory", "第一句", "user:旧"))
	meta.Summarize = true
	got := PrepareConversationPrefix(meta, fullPrefixInput("tools-new", "system-new", "memory-new", "摘要", ""))
	if !got.Refreshed || len(got.Bugs) != 0 {
		t.Fatalf("refreshed=%v bugs=%v", got.Refreshed, got.Bugs)
	}
	if got.Send.System != "system-new" || got.Send.FirstMessage != "摘要" {
		t.Fatalf("summary did not become the pin: %#v", got.Send)
	}
	if PromptPrefixUnexpectedTotal(PromptSegmentSystem, "openai", "m") != 0 {
		t.Fatal("summary incremented the unexpected-change counter")
	}
}

func TestPromptPrefixHistoryRewriteIsNotASilentEdit(t *testing.T) {
	ResetPromptPrefixPins()
	t.Cleanup(ResetPromptPrefixPins)
	meta := PromptPrefixMeta{ConversationID: "conv-hist", BotID: "bot-1", Provider: "openai", Model: "m"}
	PrepareConversationPrefix(meta, fullPrefixInput("tools", "system", "memory", "第一句", "user:原来的"))
	got := PrepareConversationPrefix(meta, fullPrefixInput("tools", "system", "memory", "第一句", "user:被改写"))
	if len(got.Bugs) != 1 || got.Bugs[0].Kind != "prompt_history_rewrite" {
		t.Fatalf("bugs = %+v", got.Bugs)
	}
	if got.Send.System != "system" {
		t.Fatalf("system moved on a history rewrite: %q", got.Send.System)
	}
}

func TestClockStaysOutOfSituationAndCatalogStaysOutOfSystem(t *testing.T) {
	report := BuildSituationReport(SituationContext{
		InFlightTask: "打开首页",
		CurrentTime:  time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC),
	})
	if strings.Contains(report, "时间:") {
		t.Fatalf("clock in situation report: %s", report)
	}
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Stop() })
	if err := store.Save(memory.Entry{Content: "喜欢短句", Category: memory.CategoryUserFact, Status: memory.StatusActive}); err != nil {
		t.Fatal(err)
	}
	index := store.MemoryIndexForPrompt(false, "", "entries")
	if index == "" {
		t.Fatal("catalog helper returned empty")
	}
	one := BuildPromptBundle(SystemPromptDeps{MemoryStore: store}, "第一句", true)
	two := BuildPromptBundle(SystemPromptDeps{MemoryStore: store}, "第二句", false)
	if one.String() != two.String() {
		t.Fatal("isFirstTurn or the user line changed the system prompt")
	}
	if strings.Contains(one.String(), index) || strings.Contains(one.String(), "[记忆索引]") || strings.Contains(one.String(), "[Memory Index]") {
		t.Fatalf("catalog entered the system prompt: %s", one.String())
	}
	if one.StableCacheKey() == "" || one.StableCacheKey() != two.StableCacheKey() {
		t.Fatalf("debug stable key = %q / %q", one.StableCacheKey(), two.StableCacheKey())
	}
}

func TestMissingAgentsMDIsAnEmptySegment(t *testing.T) {
	if got := ReadAgentsMD(filepath.Join(t.TempDir(), "missing.md")); got != "" {
		t.Fatalf("missing file = %q", got)
	}
	if SystemWithAgentsMD("base", "") != "base" {
		t.Fatal("empty agents changed the system segment")
	}
}

func fullPrefixInput(tools, system, memorySnapshot, first, history string) PromptPrefixInput {
	return PromptPrefixInput{
		Segments: PromptPrefixSegments{
			Tools:          tools,
			System:         system,
			MemorySnapshot: memorySnapshot,
			FirstMessage:   first,
			History:        history,
		},
		ProvideTools:   true,
		ProvideSystem:  true,
		ProvideMemory:  true,
		ProvideFirst:   true,
		ProvideHistory: true,
	}
}
