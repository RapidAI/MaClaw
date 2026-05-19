package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestSynthesizerPersistsSchemaEvidenceMetadata(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	entries := []Entry{
		{ID: "ev-a", Content: "User prefers concise Chinese replies", Category: CategoryConversationSummary, SourceType: "conversation", OwnerID: "user-1", Status: StatusActive},
		{ID: "ev-b", Content: "User asked for concise Chinese final answer", Category: CategoryConversationSummary, SourceType: "conversation", OwnerID: "user-1", Status: StatusActive},
		{ID: "ev-c", Content: "User rejected long verbose summaries", Category: CategoryConversationSummary, SourceType: "conversation", OwnerID: "user-1", Status: StatusActive},
		{ID: "ev-d", Content: "Unrelated release note", Category: CategoryConversationSummary, SourceType: "conversation", OwnerID: "user-1", Status: StatusActive},
		{ID: "ev-e", Content: "Unrelated build note", Category: CategoryConversationSummary, SourceType: "conversation", OwnerID: "user-1", Status: StatusActive},
		{ID: "ev-f", Content: "Unrelated test note", Category: CategoryConversationSummary, SourceType: "conversation", OwnerID: "user-1", Status: StatusActive},
	}
	store.SetEntries(entries)
	llm := &recordingExperienceProtectionLLM{response: `[{"source":"recurring","content":"User prefers concise Chinese responses","category":"preference","evidence_count":3,"evidence_ids":["ev-a","ev-b","ev-c"]}]`}
	synthesizer := NewSynthesizer(store, llm)
	synthesizer.SetMinEntries(1)

	result, err := synthesizer.Synthesize(context.Background())
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if result.Promoted != 1 {
		t.Fatalf("expected one promoted schema, got %+v", result)
	}
	entries = store.List(CategoryPreference, "concise Chinese")
	if len(entries) != 1 {
		t.Fatalf("expected promoted preference, got %+v", entries)
	}
	got := entries[0]
	if got.DerivedKind != "schema:recurring" || got.SourceType != "schema_consolidation" {
		t.Fatalf("missing schema metadata: %+v", got)
	}
	if strings.Join(got.EvidenceIDs, ",") != "ev-a,ev-b,ev-c" || strings.Join(got.RelatedIDs, ",") != "ev-a,ev-b,ev-c" {
		t.Fatalf("missing evidence ids: %+v", got)
	}
	if got.Boundary == nil || got.Boundary.OwnerID != "user-1" || got.Boundary.SourceScope != "conversation" {
		t.Fatalf("unexpected boundary: %+v", got.Boundary)
	}
}

func TestInferMemoryBoundaryUsesNestedDerivedBoundary(t *testing.T) {
	since := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	until := since.Add(2 * time.Hour)
	boundary := InferMemoryBoundary([]Entry{
		{
			ID:          "tmt-session-1",
			SourceType:  "tmt_consolidation",
			DerivedKind: "tmt:session",
			Boundary: &MemoryBoundary{
				OwnerID:     "owner-a",
				ProjectPath: `D:\workprj\alpha`,
				SourceScope: "conversation",
				Since:       &since,
				Until:       &until,
			},
		},
		{
			ID:          "theme-1",
			SourceType:  "theme_rebuild",
			DerivedKind: "theme",
			Boundary: &MemoryBoundary{
				OwnerID:     "owner-a",
				ProjectPath: `D:\workprj\alpha`,
				SourceScope: "conversation",
				Since:       &since,
				Until:       &until,
			},
		},
	})
	if boundary.OwnerID != "owner-a" || boundary.ProjectPath != `D:\workprj\alpha` || boundary.SourceScope != "conversation" {
		t.Fatalf("nested boundary was not preserved: %+v", boundary)
	}
	if boundary.Since == nil || !boundary.Since.Equal(since) || boundary.Until == nil || !boundary.Until.Equal(until) {
		t.Fatalf("nested boundary time window was not preserved: %+v", boundary)
	}
}
func TestConsolidationGateAllowsSharedEvidenceWithSingleOwner(t *testing.T) {
	decision := AssessConsolidationGate([]Entry{
		{ID: "a", OwnerID: "user-a", SourceType: "conversation"},
		{ID: "b", SourceType: "conversation"},
		{ID: "c", OwnerID: "user-a", SourceType: "conversation"},
	}, ConsolidationGateOptions{MinEvidence: 3})
	if !decision.Allowed || decision.Boundary.OwnerID != "user-a" {
		t.Fatalf("expected shared evidence to inherit single non-empty owner boundary, got %+v", decision)
	}
}
func TestConsolidationGateRejectsMixedOwnerEvidence(t *testing.T) {
	decision := AssessConsolidationGate([]Entry{
		{ID: "a", OwnerID: "user-a", SourceType: "conversation"},
		{ID: "b", OwnerID: "user-b", SourceType: "conversation"},
		{ID: "c", OwnerID: "user-a", SourceType: "conversation"},
	}, ConsolidationGateOptions{MinEvidence: 3})
	if decision.Allowed || !containsGateReason(decision.Reasons, "mixed_owner_boundary") {
		t.Fatalf("expected mixed owner rejection, got %+v", decision)
	}
}

func containsGateReason(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
