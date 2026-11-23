package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestBuildCompactionSummarizerInputKeepsGoalAndLatestOutcome(t *testing.T) {
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "ORIGINAL_GOAL: preserve the public API"},
		{Role: "assistant", Content: strings.Repeat("initial plan ", 50)},
	}
	for i := 0; i < 40; i++ {
		entries = append(entries,
			agent.ConversationEntry{Role: "user", Content: strings.Repeat("requirement ", 60)},
			agent.ConversationEntry{Role: "assistant", Content: strings.Repeat("implementation plan ", 40)},
			agent.ConversationEntry{Role: "assistant", Content: strings.Repeat("verification outcome ", 40)},
		)
	}
	entries = append(entries,
		agent.ConversationEntry{Role: "assistant", Content: "LATEST_OUTCOME: retry with the bounded reader"},
		agent.ConversationEntry{Role: "user", Content: "continue"})

	material := buildCompactionSummarizerInput(entries)
	if !strings.Contains(material, "ORIGINAL_GOAL") {
		t.Fatal("summarizer input lost the original goal")
	}
	if !strings.Contains(material, "LATEST_OUTCOME") {
		t.Fatal("summarizer input lost the latest outcome")
	}
	if !strings.Contains(material, "中间工具细节已省略") {
		t.Fatalf("long input was not compacted explicitly: runes=%d tail=%q", len([]rune(material)), material)
	}
	if got := len([]rune(material)); got > 12050 {
		t.Fatalf("compacted material is still too large: %d runes", got)
	}
}
