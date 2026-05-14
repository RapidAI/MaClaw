package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type recordingExperienceProtectionLLM struct {
	response string
	messages []map[string]string
}

func (r *recordingExperienceProtectionLLM) ChatCall(messages []map[string]string) (string, error) {
	r.messages = append([]map[string]string(nil), messages...)
	if r.response != "" {
		return r.response, nil
	}
	return "[]", nil
}

func (r *recordingExperienceProtectionLLM) IsConfigured() bool { return true }

func TestCompressorMergeBatchIncludesExperienceProtectionAnchors(t *testing.T) {
	llm := &recordingExperienceProtectionLLM{response: "[]"}
	compressor := NewCompressor(nil, llm, nil)
	compressor.SetExperienceProtectionSamples([]ProtectedExperienceCandidate{{
		ID:       "a2a-release",
		Title:    "release disagreement",
		Summary:  "minority view says wait for rollback gate",
		Category: string(CategoryProjectKnowledge),
		Source:   string(ExperienceSourceA2A),
		Reason:   "a2a_discussion",
		Tags:     []string{"topic:release"},
	}})

	if _, err := compressor.mergeBatch(context.Background(), []Entry{{
		Content:    "A2A result: ship option A; minority says wait.",
		Category:   CategoryProjectKnowledge,
		SourceType: "group_discussion",
	}}); err != nil {
		t.Fatalf("mergeBatch: %v", err)
	}
	if len(llm.messages) != 2 {
		t.Fatalf("expected LLM messages, got %#v", llm.messages)
	}
	userPrompt := llm.messages[1]["content"]
	if !strings.Contains(userPrompt, "Protected experience candidates") || !strings.Contains(userPrompt, "release disagreement") || !strings.Contains(userPrompt, "experience_protection:") {
		t.Fatalf("merge prompt missing protection anchors:\n%s", userPrompt)
	}
}

func TestSynthesizerIncludesExperienceProtectionAnchors(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	entries := make([]Entry, 0, 6)
	for i := 0; i < 6; i++ {
		entries = append(entries, Entry{ID: string(rune('a' + i)), Content: "tool recovery path: retry browser click after observe", Category: CategoryConversationSummary, SourceType: "tool_usage"})
	}
	store.SetEntries(entries)
	llm := &recordingExperienceProtectionLLM{response: "[]"}
	synthesizer := NewSynthesizer(store, llm)
	synthesizer.SetMinEntries(1) // lower guard for testing
	synthesizer.SetExperienceProtectionSamples([]ProtectedExperienceCandidate{{
		ID:       "tool-retry",
		Title:    "browser retry recovery",
		Summary:  "retry browser click after observe",
		Category: string(CategoryConversationSummary),
		Source:   string(ExperienceSourceToolUsage),
		Reason:   "tool_usage",
		Tags:     []string{"tool:browser"},
	}})

	if _, err := synthesizer.Synthesize(context.Background()); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(llm.messages) != 2 {
		t.Fatalf("expected LLM messages, got %#v", llm.messages)
	}
	userPrompt := llm.messages[1]["content"]
	if !strings.Contains(userPrompt, "Protected experience candidates") || !strings.Contains(userPrompt, "browser retry recovery") || !strings.Contains(userPrompt, "experience_protection:") {
		t.Fatalf("synthesis prompt missing protection anchors:\n%s", userPrompt)
	}
}
