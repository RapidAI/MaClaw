package memory

import (
	"path/filepath"
	"testing"
)

type sessionStartExtractorTestLLM struct {
	response string
}

func (l sessionStartExtractorTestLLM) ChatCall(messages []map[string]string) (string, error) {
	return l.response, nil
}

func (l sessionStartExtractorTestLLM) IsConfigured() bool { return true }

func TestSessionStartExtractorStoresDerivedArtifactMetadata(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	extractor := NewSessionStartExtractor(store, sessionStartExtractorTestLLM{
		response: "# Previous session\n- Important workflow evidence for future recall.",
	})
	extractor.extract("owner-a", []ConversationMessage{
		{Role: "user", Content: "continue the memory consolidation work"},
		{Role: "assistant", Content: "I inspected the consolidation code and tests."},
		{Role: "tool", Content: "go test ./corelib/memory"},
		{Role: "assistant", Content: "The tests passed and the next step is metadata coverage."},
	})

	entries := store.List(CategoryTaskArtifact, "workflow evidence")
	if len(entries) != 1 {
		t.Fatalf("expected one session-start artifact, got %+v", entries)
	}
	entry := entries[0]
	if entry.SourceType != "session_start_extraction" || entry.DerivedKind != "summary:session_start" {
		t.Fatalf("unexpected source/derived metadata: %+v", entry)
	}
	if entry.Boundary == nil || entry.Boundary.OwnerID != "owner-a" || entry.Boundary.SourceScope != "session_start_extraction" {
		t.Fatalf("unexpected boundary: %+v", entry.Boundary)
	}
}
