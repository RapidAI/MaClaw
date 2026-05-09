package memory

import (
	"strings"
	"testing"
)

func TestExperienceDistillerAnalyzeClassifiesAndProtects(t *testing.T) {
	distiller := &ExperienceDistiller{LayeredThreshold: 10}
	entries := []Entry{
		{ID: "conversation", Content: "plain", Category: CategoryConversationSummary},
		{ID: "instruction", Content: "always do x", Category: CategoryInstruction},
		{ID: "a2a", Content: "expert objection", Category: CategoryProjectKnowledge, SourceType: "group_discussion"},
		{ID: "tool", Content: "browser click failed", Category: CategoryProjectKnowledge, SourceType: "tool_usage"},
		{ID: "dormant", Content: "old", Category: CategoryProjectKnowledge, SourceType: "swarm", Status: StatusDormant},
	}

	got := distiller.Analyze(entries)
	if got.ScannedEntries != 5 || got.ActiveEntries != 4 {
		t.Fatalf("counts = %+v", got)
	}
	if got.SourceCounts[string(ExperienceSourceConversation)] != 2 {
		t.Fatalf("conversation source count = %d, want 2", got.SourceCounts[string(ExperienceSourceConversation)])
	}
	if got.SourceCounts[string(ExperienceSourceA2A)] != 1 || got.SourceCounts[string(ExperienceSourceToolUsage)] != 1 {
		t.Fatalf("source counts = %+v", got.SourceCounts)
	}
	if got.ProtectedCandidates != 3 {
		t.Fatalf("ProtectedCandidates = %d, want 3", got.ProtectedCandidates)
	}
}

func TestExperienceDistillerRecommendsLayeredForLargeBatch(t *testing.T) {
	distiller := &ExperienceDistiller{LayeredThreshold: 3}
	got := distiller.Analyze([]Entry{
		{ID: "a", Content: "a"},
		{ID: "b", Content: "b"},
		{ID: "c", Content: "c"},
	})
	if !got.LayeredRecommended || got.Reason == "" {
		t.Fatalf("expected layered recommendation, got %+v", got)
	}
}

func TestClassifyExperienceSource(t *testing.T) {
	tests := map[string]ExperienceSource{
		"":                 ExperienceSourceConversation,
		"task_artifact":    ExperienceSourceWorkflow,
		"subagent":         ExperienceSourceSwarm,
		"group_discussion": ExperienceSourceA2A,
		"usage":            ExperienceSourceToolUsage,
		"manual":           ExperienceSourceManual,
		"mystery":          ExperienceSourceUnknown,
	}
	for source, want := range tests {
		if got := ClassifyExperienceSource(Entry{SourceType: source}); got != want {
			t.Fatalf("source %q classified as %q, want %q", source, got, want)
		}
	}
}

func TestExperienceDistillerProtectedSamples(t *testing.T) {
	distiller := NewExperienceDistiller()
	got := distiller.Analyze([]Entry{{ID: "a2a-risk", Title: "A2A risk", Content: "risk with rollback trigger", SourceType: "group_discussion", Category: CategoryProjectKnowledge, Tags: []string{"discussion:1"}}})
	if got.ProtectedCandidates != 1 || len(got.ProtectedSamples) != 1 {
		t.Fatalf("protected result = %+v", got)
	}
	if got.ProtectedSamples[0].Reason != "a2a_discussion" || got.ProtectedSamples[0].ID != "a2a-risk" {
		t.Fatalf("protected sample = %+v", got.ProtectedSamples[0])
	}
	if got.ProtectedSamples[0].Title != "A2A risk" || !strings.Contains(got.ProtectedSamples[0].Summary, "rollback") || len(got.ProtectedSamples[0].Tags) != 1 {
		t.Fatalf("protected sample should expose bounded inspection fields: %+v", got.ProtectedSamples[0])
	}
	if got.ProtectedReasonCounts["a2a_discussion"] != 1 || got.ProtectedSourceCounts[string(ExperienceSourceA2A)] != 1 {
		t.Fatalf("protected breakdowns = reason %+v source %+v", got.ProtectedReasonCounts, got.ProtectedSourceCounts)
	}
}

func TestExperienceDistillerProtectedSamplesPrioritizePinnedAndRespectLimit(t *testing.T) {
	distiller := NewExperienceDistiller()
	entries := []Entry{}
	for i := 0; i < 4; i++ {
		entries = append(entries, Entry{ID: string(rune('a' + i)), Content: "tool trace", SourceType: "tool_usage", Category: CategoryProjectKnowledge})
	}
	entries = append(entries, Entry{ID: "pinned-last", Content: "important pinned instruction", Category: CategoryProjectKnowledge, Pinned: true})

	got := distiller.AnalyzeWithSampleLimit(entries, 2)
	if got.ProtectedCandidates != 5 || len(got.ProtectedSamples) != 2 {
		t.Fatalf("protected result = %+v", got)
	}
	if got.ProtectedSamples[0].ID != "pinned-last" || got.ProtectedSamples[0].Reason != "pinned" {
		t.Fatalf("pinned candidate should lead samples: %+v", got.ProtectedSamples)
	}
}

func TestCompressionProtectionHint(t *testing.T) {
	a2aHint := CompressionProtectionHint(Entry{SourceType: "group_discussion", Category: CategoryProjectKnowledge})
	if a2aHint == "" || !strings.Contains(a2aHint, "objections") || !strings.Contains(a2aHint, "minority") {
		t.Fatalf("A2A hint missing discussion protections: %q", a2aHint)
	}
	toolHint := CompressionProtectionHint(Entry{SourceType: "tool_usage", Category: CategoryProjectKnowledge})
	if toolHint == "" || !strings.Contains(toolHint, "tool names") || !strings.Contains(toolHint, "recovery") {
		t.Fatalf("tool hint missing usage protections: %q", toolHint)
	}
	if plain := CompressionProtectionHint(Entry{Category: CategoryConversationSummary}); plain != "" {
		t.Fatalf("plain conversation hint = %q, want empty", plain)
	}
}

func TestFormatExperiencePromptEntryAddsProtectionHint(t *testing.T) {
	entry := Entry{Content: "expert objection with rollback command", SourceType: "group_discussion", Category: CategoryProjectKnowledge}
	got := formatExperiencePromptEntry(2, entry, 20)
	if !strings.Contains(got, "[2]") || !strings.Contains(got, "experience_protection:") || !strings.Contains(got, "A2A/group discussion") {
		t.Fatalf("prompt entry missing protection hint: %q", got)
	}

	plain := formatExperiencePromptEntry(0, Entry{Content: "plain conversation", Category: CategoryConversationSummary}, 20)
	if strings.Contains(plain, "experience_protection:") {
		t.Fatalf("plain prompt entry should not include protection hint: %q", plain)
	}
}

func TestFormatExperienceProtectionPromptSummarizesRetentionAnchors(t *testing.T) {
	got := FormatExperienceProtectionPrompt([]ProtectedExperienceCandidate{{
		ID:       "a2a-release",
		Title:    "release disagreement",
		Summary:  "minority view says wait for rollback gate",
		Category: string(CategoryProjectKnowledge),
		Source:   string(ExperienceSourceA2A),
		Reason:   "a2a_discussion",
		Tags:     []string{"topic:release"},
	}})
	if !strings.Contains(got, "Protected experience candidates") || !strings.Contains(got, "release disagreement") || !strings.Contains(got, "retention anchors") {
		t.Fatalf("protection prompt missing retention context: %q", got)
	}
	if empty := FormatExperienceProtectionPrompt(nil); empty != "" {
		t.Fatalf("empty protection prompt = %q, want empty", empty)
	}
}
