package desktopbot_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// The offline cases for PR-2. Each file names one behavior. The runner calls
// the prompt functions the product uses. It does not call a model.

func TestDesktopBotPromptPrefixCases(t *testing.T) {
	cases := []string{
		"prefix_stable_two_turns",
		"clock_not_in_system",
		"catalog_not_in_system",
		"agents_md_pinned",
	}
	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			agent.ResetPromptPrefixPins()
			t.Cleanup(agent.ResetPromptPrefixPins)
			raw := readCase(t, id)
			switch id {
			case "prefix_stable_two_turns":
				runPrefixStable(t, raw)
			case "agents_md_pinned":
				runAgentsPinned(t, raw)
			case "clock_not_in_system":
				runClockAbsent(t, raw)
			case "catalog_not_in_system":
				runCatalogAbsent(t, raw)
			default:
				t.Fatalf("no runner for %s", id)
			}
		})
	}
}

type promptCase struct {
	ID     string     `json:"id"`
	Forbid []string   `json:"forbid"`
	Turns  []caseTurn `json:"turns"`
}

type caseTurn struct {
	Tools          string `json:"tools"`
	System         string `json:"system"`
	MemorySnapshot string `json:"memory_snapshot"`
	FirstMessage   string `json:"first_message"`
	History        string `json:"history"`
}

func readCase(t *testing.T, id string) promptCase {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c promptCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runPrefixStable(t *testing.T, c promptCase) {
	t.Helper()
	if len(c.Turns) != 2 {
		t.Fatalf("turns = %d", len(c.Turns))
	}
	meta := agent.PromptPrefixMeta{ConversationID: c.ID, BotID: "bot-case", Provider: "openai", Model: "case"}
	turn1 := agent.PrepareConversationPrefix(meta, caseInput(c.Turns[0]))
	turn2 := agent.PrepareConversationPrefix(meta, caseInput(c.Turns[1]))
	if len(turn1.Bugs) != 0 || len(turn2.Bugs) != 0 {
		t.Fatalf("bugs turn1=%v turn2=%v", turn1.Bugs, turn2.Bugs)
	}
	if turn1.ToolsHash != turn2.ToolsHash || turn1.SystemHash != turn2.SystemHash || turn1.MemoryHash != turn2.MemoryHash || turn1.FirstHash != turn2.FirstHash || turn1.PrefixHash != turn2.PrefixHash {
		t.Fatalf("prefix changed between turns")
	}
	if turn1.HistoryHash == turn2.HistoryHash {
		t.Fatal("history hash did not change")
	}
}

func runAgentsPinned(t *testing.T, c promptCase) {
	t.Helper()
	if len(c.Turns) != 2 {
		t.Fatalf("turns = %d", len(c.Turns))
	}
	meta := agent.PromptPrefixMeta{ConversationID: c.ID, BotID: "bot-case", Provider: "openai", Model: "case"}
	turn1 := agent.PrepareConversationPrefix(meta, caseInput(c.Turns[0]))
	turn2 := agent.PrepareConversationPrefix(meta, caseInput(c.Turns[1]))
	if turn2.Send.System != c.Turns[0].System || turn2.Send.Tools != c.Turns[0].Tools {
		t.Fatalf("sent system %q tools %q", turn2.Send.System, turn2.Send.Tools)
	}
	if turn1.SystemHash != turn2.SystemHash || turn1.ToolsHash != turn2.ToolsHash {
		t.Fatal("tool or system hash moved")
	}
	if agent.PromptPrefixUnexpectedTotal(agent.PromptSegmentSystem, "openai", "case") != 1 {
		t.Fatal("system change was not counted")
	}
	if agent.PromptPrefixUnexpectedTotal(agent.PromptSegmentTools, "openai", "case") != 1 {
		t.Fatal("tool change was not counted")
	}
}

func runClockAbsent(t *testing.T, c promptCase) {
	t.Helper()
	report := agent.BuildSituationReport(agent.SituationContext{
		InFlightTask: "打开首页",
		CurrentTime:  time.Date(2026, 10, 18, 9, 41, 0, 0, time.UTC),
	})
	light := agentruntime.BuildLightPrompt(agentruntime.LightPromptRequest{
		RoleName:    "MaClaw",
		UserText:    "现在几点",
		CurrentTime: time.Date(2026, 10, 18, 9, 41, 0, 0, time.UTC),
	})
	text := report + "\n" + light
	for _, forbidden := range c.Forbid {
		if strings.Contains(text, forbidden) {
			t.Fatalf("system text contains %q:\n%s", forbidden, text)
		}
	}
}

func runCatalogAbsent(t *testing.T, c promptCase) {
	t.Helper()
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
	bundle := agent.BuildPromptBundle(agent.SystemPromptDeps{MemoryStore: store}, "打开首页", true)
	text := bundle.String()
	if strings.Contains(text, index) {
		t.Fatalf("catalog %q entered the system prompt", index)
	}
	for _, forbidden := range c.Forbid {
		if strings.Contains(text, forbidden) {
			t.Fatalf("system prompt contains %q", forbidden)
		}
	}
	again := agent.BuildPromptBundle(agent.SystemPromptDeps{MemoryStore: store}, "再看一眼", false)
	if again.String() != text {
		t.Fatal("the second turn rebuilt the system prompt")
	}
}

func caseInput(turn caseTurn) agent.PromptPrefixInput {
	return agent.PromptPrefixInput{
		Segments: agent.PromptPrefixSegments{
			Tools:          turn.Tools,
			System:         turn.System,
			MemorySnapshot: turn.MemorySnapshot,
			FirstMessage:   turn.FirstMessage,
			History:        turn.History,
		},
		ProvideTools:   true,
		ProvideSystem:  true,
		ProvideMemory:  true,
		ProvideFirst:   true,
		ProvideHistory: true,
	}
}
