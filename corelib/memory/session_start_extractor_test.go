package memory

import (
	"context"
	"path/filepath"
	"strings"
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
	extractor.extract(context.Background(), "owner-a", []ConversationMessage{
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

type contextualSessionStartExtractorTestLLM struct {
	called bool
}

func (l *contextualSessionStartExtractorTestLLM) ChatCall(messages []map[string]string) (string, error) {
	return "", nil
}

func (l *contextualSessionStartExtractorTestLLM) ChatCallContext(ctx context.Context, messages []map[string]string) (string, error) {
	l.called = true
	return "# Previous session\n- Important workflow evidence for future recall.", ctx.Err()
}

func (l *contextualSessionStartExtractorTestLLM) IsConfigured() bool { return true }

func TestSessionStartExtractorHonorsCanceledContext(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	llm := &contextualSessionStartExtractorTestLLM{}
	extractor := NewSessionStartExtractor(store, llm)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	extractor.MaybeExtract(ctx, "owner-a", []ConversationMessage{
		{Role: "user", Content: "continue the memory consolidation work"},
		{Role: "assistant", Content: strings.Repeat("substantial assistant result ", 8)},
		{Role: "user", Content: "next"},
		{Role: "assistant", Content: strings.Repeat("another substantial result ", 8)},
		{Role: "tool", Content: "go test ./corelib/memory"},
		{Role: "assistant", Content: strings.Repeat("final substantial result ", 8)},
	})
	if llm.called {
		t.Fatal("canceled context still reached LLM")
	}
}
