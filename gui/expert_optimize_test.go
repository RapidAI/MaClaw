package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestExpertStoreFindOptimizedFor(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))

	source := testExpert("expert-src", "2026-02-01T10:00:00Z")
	other := testExpert("expert-other", "2026-02-02T10:00:00Z")
	optimized := testExpert("expert-opt", "2026-02-03T10:00:00Z")
	optimized.OptimizedFromID = source.ID
	for _, def := range []ExpertDefinition{source, other, optimized} {
		if err := store.Save(def); err != nil {
			t.Fatalf("save %s: %v", def.ID, err)
		}
	}

	found, ok, err := store.FindOptimizedFor(source.ID)
	if err != nil || !ok {
		t.Fatalf("FindOptimizedFor(%q) ok=%v err=%v", source.ID, ok, err)
	}
	if found.ID != optimized.ID {
		t.Fatalf("FindOptimizedFor returned %q, want %q", found.ID, optimized.ID)
	}

	if _, ok, err := store.FindOptimizedFor(other.ID); err != nil || ok {
		t.Fatalf("source without optimized expert: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.FindOptimizedFor("  "); err != nil || ok {
		t.Fatalf("blank origin: ok=%v err=%v", ok, err)
	}
}

func TestExpertStoreFindOptimizedForPrefersLatest(t *testing.T) {
	store := newExpertStore(filepath.Join(t.TempDir(), "experts", "experts.json"))

	older := testExpert("expert-opt-old", "2026-02-01T10:00:00Z")
	older.OptimizedFromID = "expert-src"
	newer := testExpert("expert-opt-new", "2026-02-04T10:00:00Z")
	newer.OptimizedFromID = "expert-src"
	// Save newest first so lookup order does not decide the winner.
	if err := store.Save(newer); err != nil {
		t.Fatalf("save newer: %v", err)
	}
	if err := store.Save(older); err != nil {
		t.Fatalf("save older: %v", err)
	}

	found, ok, err := store.FindOptimizedFor("expert-src")
	if err != nil || !ok {
		t.Fatalf("FindOptimizedFor ok=%v err=%v", ok, err)
	}
	if found.ID != newer.ID {
		t.Fatalf("duplicate lineage should resolve to latest updated, got %q", found.ID)
	}
}

func TestParseExpertOptimizeResponse(t *testing.T) {
	valid := "```json\n{\"name\":\"论文润色·优化\",\"description\":\"d\",\"icon\":\"📝\",\"system_prompt\":\"prompt\"}\n```"
	out, err := parseExpertOptimizeResponse(valid)
	if err != nil {
		t.Fatalf("parse fenced response: %v", err)
	}
	if out.Name != "论文润色·优化" || out.SystemPrompt != "prompt" {
		t.Fatalf("unexpected parse result: %+v", out)
	}

	if _, err := parseExpertOptimizeResponse(""); err == nil {
		t.Fatal("empty response must fail")
	}
	if _, err := parseExpertOptimizeResponse("no json here"); err == nil {
		t.Fatal("response without JSON object must fail")
	}
	if _, err := parseExpertOptimizeResponse(`{"name":"x"}`); err == nil {
		t.Fatal("missing system_prompt must fail")
	}
}

func TestBuildExpertOptimizeDraft(t *testing.T) {
	source := &ExpertDefinition{
		ID:           "expert-src",
		Name:         "论文润色",
		Description:  "源简介",
		Icon:         "📝",
		SystemPrompt: "源 prompt",
		Tools:        []string{"read_file"},
		Skills:       []string{"pptx-gen"},
	}
	output := expertOptimizeLLMOutput{
		Name:         "论文精修",
		Description:  "优化简介",
		Icon:         "✨",
		SystemPrompt: "优化 prompt",
	}

	// New optimized expert: fresh id, lineage recorded, tools/skills inherited.
	draft := buildExpertOptimizeDraft(source, output, nil)
	if draft.UpdateExisting || draft.ID != "" {
		t.Fatalf("new draft must not reference an existing expert: %+v", draft)
	}
	if draft.OptimizedFromID != source.ID || draft.SourceName != source.Name {
		t.Fatalf("lineage not recorded: %+v", draft)
	}
	if draft.Name != "论文精修" || draft.SystemPrompt != "优化 prompt" {
		t.Fatalf("distillation output not applied: %+v", draft)
	}
	if len(draft.Tools) != 1 || draft.Tools[0] != "read_file" || len(draft.Skills) != 1 || draft.Skills[0] != "pptx-gen" {
		t.Fatalf("tools/skills must be inherited from source: %+v", draft)
	}

	// LLM echoed the source name (or left it empty) → forced distinct default.
	echo := buildExpertOptimizeDraft(source, expertOptimizeLLMOutput{Name: source.Name, SystemPrompt: "p"}, nil)
	if echo.Name != source.Name+"·优化" {
		t.Fatalf("same-name draft must be renamed, got %q", echo.Name)
	}
	blank := buildExpertOptimizeDraft(source, expertOptimizeLLMOutput{SystemPrompt: "p"}, nil)
	if blank.Name != source.Name+"·优化" || blank.Icon != source.Icon || blank.Description != source.Description {
		t.Fatalf("blank fields must fall back to source: %+v", blank)
	}

	// Existing optimized expert: id, (possibly user-renamed) name, tools/skills
	// and about are kept; distilled fields refresh.
	existing := &ExpertDefinition{
		ID:     "expert-opt",
		Name:   "用户改的名",
		Tools:  []string{"custom_tool"},
		Skills: []string{"custom_skill"},
		About:  "作者：李四",
	}
	update := buildExpertOptimizeDraft(source, output, existing)
	if !update.UpdateExisting || update.ID != "expert-opt" {
		t.Fatalf("update draft must target the existing optimized expert: %+v", update)
	}
	if update.Name != "用户改的名" {
		t.Fatalf("existing optimized expert name must be kept, got %q", update.Name)
	}
	if update.SystemPrompt != "优化 prompt" || update.OptimizedFromID != source.ID {
		t.Fatalf("update draft must refresh distilled fields: %+v", update)
	}
	if len(update.Tools) != 1 || update.Tools[0] != "custom_tool" || len(update.Skills) != 1 || update.Skills[0] != "custom_skill" {
		t.Fatalf("update draft must keep existing tools/skills: %+v", update)
	}
	if update.About != "作者：李四" {
		t.Fatalf("update draft must keep existing about: %+v", update)
	}
}

func TestBuildExpertOptimizeTranscript(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	userID := expertSessionUserID("expert-src")

	// Empty session → empty transcript (the binding turns this into the
	// "还没有会话经验可提炼" error).
	if got := buildExpertOptimizeTranscript(h, userID); got != "" {
		t.Fatalf("empty session transcript = %q, want empty", got)
	}

	entries := []agent.ConversationEntry{
		{Role: "system", Content: "persona"},
		{Role: "user", Content: "把语气改正式一些"},
		{Role: "assistant", Content: "好的，后续都用正式书面语。"},
	}
	h.memory.Save(userID, entries)

	got := buildExpertOptimizeTranscript(h, userID)
	if !strings.Contains(got, "用户：把语气改正式一些") || !strings.Contains(got, "专家：好的，后续都用正式书面语。") {
		t.Fatalf("transcript missing conversation lines: %q", got)
	}
	if strings.Contains(got, "persona") {
		t.Fatalf("system entries must be excluded: %q", got)
	}

	// Only the tail is kept when the session exceeds the message cap.
	many := make([]agent.ConversationEntry, 0, expertOptimizeMaxMessages+5)
	for i := 0; i < expertOptimizeMaxMessages+5; i++ {
		many = append(many, agent.ConversationEntry{Role: "user", Content: strings.Repeat("问", i+1)})
	}
	h.memory.Save(userID, many)
	tail := buildExpertOptimizeTranscript(h, userID)
	if strings.Contains(tail, "用户：问\n") { // the 5 oldest 1-rune messages must be dropped
		t.Fatalf("oldest messages must be dropped beyond the cap: %q", tail[:80])
	}
	if got := len([]rune(tail)); got > expertOptimizeMaxTranscriptRunes {
		t.Fatalf("transcript exceeds rune cap: %d > %d", got, expertOptimizeMaxTranscriptRunes)
	}
}
