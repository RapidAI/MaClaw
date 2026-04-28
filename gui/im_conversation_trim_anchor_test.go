package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// makeEntry creates a ConversationEntry with the given role and content.
func makeEntry(role, content string) agent.ConversationEntry {
	return agent.ConversationEntry{Role: role, Content: content}
}

// makeToolEntry creates a tool-role ConversationEntry.
func makeToolEntry(content string) agent.ConversationEntry {
	return agent.ConversationEntry{Role: "tool", Content: content, ToolCallID: "tc-1"}
}

func TestTrimHistory_SmallHistory_NoTrimming(t *testing.T) {
	entries := []agent.ConversationEntry{
		makeEntry("user", "开发一个贪吃蛇游戏"),
		makeEntry("assistant", "好的，我来生成需求文档"),
	}
	result := trimHistory(entries)
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
}

func TestTrimHistory_TwoTierPreservesTurnBoundaries(t *testing.T) {
	// Simulate: user request → assistant plan → 70+ tool calls
	// The tier-1 entries (user request + assistant plan) should survive.
	entries := make([]agent.ConversationEntry, 0, 80)

	// Turn 1: user request + assistant response (tier-1)
	entries = append(entries, makeEntry("user", "开发一个 RegistryAPITester 项目"))
	entries = append(entries, makeEntry("assistant", "好的，我来为你生成需求文档。这个项目需要..."))

	// Turn 2: user confirmation + assistant design (tier-1)
	entries = append(entries, makeEntry("user", "确认"))
	entries = append(entries, makeEntry("assistant", "开始技术设计。架构采用 C++ 单体应用..."))

	// Entries 4-79: many tool calls (tier-2 detail)
	for i := 4; i < 80; i++ {
		if i%3 == 0 {
			entries = append(entries, makeEntry("assistant", fmt.Sprintf("执行步骤 %d", i)))
		} else if i%3 == 1 {
			entries = append(entries, makeToolEntry(fmt.Sprintf("tool result %d: ok", i)))
		} else {
			entries = append(entries, makeEntry("user", "继续"))
		}
	}

	result := trimHistory(entries)

	// Should be within budget. The three-tier compaction produces:
	// tier-1 (turn boundaries) + tier-user (preserved user messages, max 5)
	// + separator (1) + recent window. Total may exceed MaxConversationTurns
	// by the tier-1 count + tier-user count + 1 separator.
	maxExpected := agent.MaxConversationTurns + 10 // tier-1 + tier-user + separator
	if len(result) > maxExpected {
		t.Errorf("result too large: %d entries (max %d)", len(result), maxExpected)
	}

	// The structural invariant: first user message and first assistant
	// response of each turn should be preserved — regardless of content.
	foundOriginalTask := false
	foundAssistantPlan := false
	for _, e := range result {
		text, ok := e.Content.(string)
		if !ok {
			continue
		}
		if text == "开发一个 RegistryAPITester 项目" {
			foundOriginalTask = true
		}
		if strings.Contains(text, "我来为你生成需求文档") {
			foundAssistantPlan = true
		}
	}

	if !foundOriginalTask {
		t.Error("tier-1: original user task request was not preserved")
	}
	if !foundAssistantPlan {
		t.Error("tier-1: first assistant response was not preserved")
	}
}

func TestTrimHistory_StructuralInvariant_NoKeywordDependency(t *testing.T) {
	// This test verifies the mechanism is structural, not keyword-based.
	// Use completely arbitrary content — no "需求文档" or "技术设计" keywords.
	entries := make([]agent.ConversationEntry, 0, 60)

	// Turn 1: arbitrary content
	entries = append(entries, makeEntry("user", "帮我分析一下这个数据集的统计特征"))
	entries = append(entries, makeEntry("assistant", "好的，我先看看数据集的基本信息"))

	// Turn 2: arbitrary content
	entries = append(entries, makeEntry("user", "用 matplotlib 画个图"))
	entries = append(entries, makeEntry("assistant", "我来生成可视化代码"))

	// 50+ tool calls
	for i := 4; i < 55; i++ {
		entries = append(entries, makeEntry("assistant", fmt.Sprintf("processing chunk %d", i)))
	}

	result := trimHistory(entries)

	// Turn boundaries should be preserved even without any project keywords.
	foundDataset := false
	foundMatplotlib := false
	for _, e := range result {
		text, ok := e.Content.(string)
		if !ok {
			continue
		}
		if strings.Contains(text, "数据集的统计特征") {
			foundDataset = true
		}
		if strings.Contains(text, "matplotlib") {
			foundMatplotlib = true
		}
	}

	if !foundDataset {
		t.Error("tier-1: first turn user message not preserved (no keyword dependency)")
	}
	if !foundMatplotlib {
		t.Error("tier-1: second turn user message not preserved (no keyword dependency)")
	}
}

func TestTrimHistory_NoOrphanedToolMessages(t *testing.T) {
	entries := make([]agent.ConversationEntry, 0, 60)
	for i := 0; i < 60; i++ {
		if i < 5 {
			entries = append(entries, makeToolEntry(fmt.Sprintf("orphan tool %d", i)))
		} else if i%2 == 0 {
			entries = append(entries, makeEntry("assistant", fmt.Sprintf("step %d", i)))
		} else {
			entries = append(entries, makeToolEntry(fmt.Sprintf("result %d", i)))
		}
	}

	result := trimHistory(entries)
	if len(result) > 0 && result[0].Role == "tool" {
		t.Error("result starts with orphaned tool message")
	}
}

func TestTrimHistory_SeparatorBetweenTiers(t *testing.T) {
	entries := make([]agent.ConversationEntry, 0, 60)
	entries = append(entries, makeEntry("user", "开发一个系统"))
	entries = append(entries, makeEntry("assistant", "好的，开始规划"))

	for i := 2; i < 60; i++ {
		entries = append(entries, makeEntry("assistant", fmt.Sprintf("step %d", i)))
	}

	result := trimHistory(entries)

	foundSeparator := false
	for _, e := range result {
		text, ok := e.Content.(string)
		if ok && strings.Contains(text, "已省略") {
			foundSeparator = true
			break
		}
	}
	if !foundSeparator {
		t.Error("expected separator between tier-1 and tier-2 entries")
	}
}

func TestTrimHistory_FallsBackToFIFO_WhenNoTier1Outside(t *testing.T) {
	// When all tier-1 entries are within the recent window, simple FIFO.
	// Use 42 entries (just over MaxConversationTurns) with the first 2
	// being turn boundaries that fall within the recent window.
	entries := make([]agent.ConversationEntry, 0, 42)
	entries = append(entries, makeEntry("user", "hello"))
	entries = append(entries, makeEntry("assistant", "hi"))
	// Fill with non-turn-boundary entries (consecutive assistant messages).
	for i := 2; i < 42; i++ {
		entries = append(entries, makeEntry("assistant", fmt.Sprintf("step %d", i)))
	}

	result := trimHistory(entries)
	if len(result) > agent.MaxConversationTurns+1 {
		t.Errorf("FIFO fallback: expected <= %d entries, got %d", agent.MaxConversationTurns+1, len(result))
	}
}
