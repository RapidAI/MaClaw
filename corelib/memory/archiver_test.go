package memory

import (
	"path/filepath"
	"testing"
	"time"
)

type archiverTestSummarizer struct {
	response string
	calls    int
}

func (s *archiverTestSummarizer) Summarize(prompt string) (string, error) {
	s.calls++
	if s.response != "" {
		return s.response, nil
	}
	return "user prefers evidence chains", nil
}

func (s *archiverTestSummarizer) IsConfigured() bool { return true }

func TestArchiverSkipsWhenOnlineExtractorRecentlySucceeded(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	store.SetOnlineExtractor(&OnlineExtractor{lastActivity: time.Now()})
	summarizer := &archiverTestSummarizer{}
	archiver := NewArchiver(store, summarizer)

	err = archiver.Archive("owner-1", []ConversationEntry{
		{Role: "user", Content: "remember to keep evidence chains"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "avoid unbounded merges"},
		{Role: "assistant", Content: "understood"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summarizer.calls != 0 {
		t.Fatalf("archiver should skip while online extractor is active, calls=%d", summarizer.calls)
	}
	if got := store.List(CategoryConversationSummary, ""); len(got) != 0 {
		t.Fatalf("archiver should not write fallback summaries when online extractor is active: %+v", got)
	}
}

func TestArchiverWritesFallbackSummaryWhenOnlineExtractorInactive(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	summarizer := &archiverTestSummarizer{}
	archiver := NewArchiver(store, summarizer)

	err = archiver.Archive("owner-1", []ConversationEntry{
		{Role: "user", Content: "remember to keep evidence chains"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "avoid unbounded merges"},
		{Role: "assistant", Content: "understood"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summarizer.calls != 1 {
		t.Fatalf("archiver should call fallback summarizer once, calls=%d", summarizer.calls)
	}
	got := store.List(CategoryConversationSummary, "")
	if len(got) != 1 {
		t.Fatalf("expected one fallback conversation summary, got %+v", got)
	}
	if got[0].SourceType != "conversation_summary" || got[0].OwnerID != "owner-1" {
		t.Fatalf("unexpected summary metadata: %+v", got[0])
	}
}

func TestArchiverSkipsNoopSummary(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	summarizer := &archiverTestSummarizer{response: "NONE"}
	archiver := NewArchiver(store, summarizer)

	err = archiver.Archive("owner-1", []ConversationEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "thanks"},
		{Role: "assistant", Content: "done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summarizer.calls != 1 {
		t.Fatalf("archiver should call fallback summarizer once, calls=%d", summarizer.calls)
	}
	if got := store.List(CategoryConversationSummary, ""); len(got) != 0 {
		t.Fatalf("noop fallback summary should not be persisted: %+v", got)
	}
}

func TestIsEmptyConversationSummary(t *testing.T) {
	for _, input := range []string{"", "NONE", " none. ", "No memory", "\u65e0", "\u6ca1\u6709\u503c\u5f97\u8bb0\u5f55\u7684\u4fe1\u606f"} {
		if !IsEmptyConversationSummary(input) {
			t.Fatalf("%q should be treated as an empty fallback summary", input)
		}
	}
	if IsEmptyConversationSummary("user prefers evidence chains") {
		t.Fatal("meaningful summary should not be treated as empty")
	}
}
