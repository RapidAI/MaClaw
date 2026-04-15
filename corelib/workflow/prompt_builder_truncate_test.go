package workflow

import (
	"strings"
	"testing"
)

func TestTruncateRunesSmart_ShortString(t *testing.T) {
	// String shorter than limit — returned as-is.
	s := "hello world"
	got := truncateRunesSmart(s, 100)
	if got != s {
		t.Errorf("expected unchanged string, got %q", got)
	}
}

func TestTruncateRunesSmart_ExactLimit(t *testing.T) {
	s := "abcde"
	got := truncateRunesSmart(s, 5)
	if got != s {
		t.Errorf("expected unchanged string, got %q", got)
	}
}

func TestTruncateRunesSmart_ParagraphBreak(t *testing.T) {
	// Build a string with a paragraph break in the last 20%.
	// 100 runes total, limit=80. Search zone = 64..80.
	// Place a paragraph break at rune 70.
	before := strings.Repeat("a", 70)
	after := strings.Repeat("b", 30)
	s := before + "\n\n" + after // total 102 runes

	got := truncateRunesSmart(s, 80)
	if !strings.HasSuffix(got, "…（已截断）") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	// Should cut at the paragraph break, keeping the 70 'a's.
	if !strings.HasPrefix(got, before) {
		t.Errorf("expected content before paragraph break to be preserved")
	}
	// Should not contain any 'b's.
	if strings.Contains(got, "b") {
		t.Errorf("expected content after paragraph break to be removed")
	}
}

func TestTruncateRunesSmart_LineBreak(t *testing.T) {
	// No paragraph break, but a single line break in the search zone.
	before := strings.Repeat("a", 70)
	after := strings.Repeat("b", 30)
	s := before + "\n" + after // total 101 runes

	got := truncateRunesSmart(s, 80)
	if !strings.HasSuffix(got, "…（已截断）") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	if !strings.HasPrefix(got, before) {
		t.Errorf("expected content before line break to be preserved")
	}
	if strings.Contains(got, "b") {
		t.Errorf("expected content after line break to be removed")
	}
}

func TestTruncateRunesSmart_HardCut(t *testing.T) {
	// No breaks at all — hard cut.
	s := strings.Repeat("x", 200)
	got := truncateRunesSmart(s, 100)
	if !strings.HasSuffix(got, "…（已截断）") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	// The content part should be exactly 100 'x's.
	content := strings.TrimSuffix(got, "…（已截断）")
	if len([]rune(content)) != 100 {
		t.Errorf("expected 100 runes of content, got %d", len([]rune(content)))
	}
}

func TestTruncateRunesSmart_ChineseText(t *testing.T) {
	// Ensure rune-based truncation works with multi-byte characters.
	s := strings.Repeat("中", 50) + "\n\n" + strings.Repeat("文", 50) // 102 runes
	got := truncateRunesSmart(s, 60)
	if !strings.HasSuffix(got, "…（已截断）") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	// Should cut at the paragraph break (rune 50).
	if !strings.HasPrefix(got, strings.Repeat("中", 50)) {
		t.Errorf("expected Chinese content before break to be preserved")
	}
	if strings.Contains(got, "文") {
		t.Errorf("expected content after break to be removed")
	}
}

func TestTruncateRunesSmart_BreakOutsideSearchZone(t *testing.T) {
	// Paragraph break exists but outside the search zone (too early).
	// Search zone is last 20% = runes 80..100.
	// Place break at rune 30 — outside search zone.
	before := strings.Repeat("a", 30)
	middle := strings.Repeat("b", 70)
	s := before + "\n\n" + middle // 102 runes

	got := truncateRunesSmart(s, 100)
	// The break at 30 is outside the search zone (80..100), so hard cut.
	if !strings.HasSuffix(got, "…（已截断）") {
		t.Errorf("expected truncation marker, got %q", got)
	}
	// Should be a hard cut at 100 runes since no break in search zone.
	content := strings.TrimSuffix(got, "…（已截断）")
	if len([]rune(content)) != 100 {
		t.Errorf("expected 100 runes of content (hard cut), got %d", len([]rune(content)))
	}
}

func TestTruncateRunesSmart_PrefersParagraphOverLine(t *testing.T) {
	// Both paragraph break and line break in search zone.
	// Paragraph break should be preferred.
	// With limit=80, search zone is 64..80.
	// Place \n\n at position 68 and \n at position 75.
	part1 := strings.Repeat("a", 68)  // 0-67
	part2 := strings.Repeat("b", 5)   // 70-74 (after \n\n at 68-69)
	part3 := strings.Repeat("c", 30)  // 76-105 (after \n at 75)
	// Layout: aaa(68) + \n\n + bbb(5) + \n + ccc(30) = 105 runes
	s := part1 + "\n\n" + part2 + "\n" + part3

	got := truncateRunesSmart(s, 80)
	// Scanning backwards from 79: hits \n at 75 (single, bestBreak=75),
	// then hits \n at 69, checks runes[68]='\n' → paragraph break → return.
	if !strings.HasPrefix(got, part1) {
		t.Errorf("expected to cut at paragraph break, preserving part1")
	}
	if strings.Contains(got, "b") {
		t.Errorf("expected content after paragraph break to be removed")
	}
}

func TestBuildPhaseSystemPrompt_PrevOutputsTruncated(t *testing.T) {
	registry := NewWorkflowRegistry()
	tmpl := registry.Match(WorkflowCoding)
	if tmpl == nil {
		t.Fatal("coding template not found")
	}

	// Create a state at task_breakdown phase (index 2) with large previous outputs.
	longRequirements := strings.Repeat("需求内容段落。\n\n", 200) // ~2000 runes
	longDesign := strings.Repeat("设计内容段落。\n\n", 200)       // ~2000 runes

	state := &WorkflowState{
		ID:           "wf-test",
		UserID:       "u1",
		Type:         WorkflowCoding,
		Intent:       StructuredIntent{Category: WorkflowCoding, Summary: "测试"},
		CurrentPhase: "task_breakdown",
		PhaseIndex:   2,
		PhaseOutputs: map[string]string{
			"requirements": longRequirements,
			"tech_design":  longDesign,
		},
	}

	phase := &tmpl.Phases[2] // task_breakdown
	prompt := BuildPhaseSystemPrompt(state, phase, registry)

	// The prompt should contain truncation markers.
	if !strings.Contains(prompt, "已截断") {
		t.Error("expected truncation markers in prompt for large previous outputs")
	}

	// The prompt should contain "摘要" labels.
	if !strings.Contains(prompt, "（摘要）") {
		t.Error("expected summary labels in prompt")
	}

	// The prompt should NOT contain the full previous outputs.
	// Each output is ~2000 runes; the prompt should be much shorter.
	promptRunes := len([]rune(prompt))
	// Requirements (600 limit) + Design (1200 limit) + overhead < 3000 runes
	if promptRunes > 5000 {
		t.Errorf("prompt too long (%d runes), previous outputs may not be truncated", promptRunes)
	}

	// The prompt should still contain the current phase instruction.
	if !strings.Contains(prompt, "任务拆分") {
		t.Error("expected current phase name in prompt")
	}
}
