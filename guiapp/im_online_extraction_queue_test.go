package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

type guiExtractionQueueLLM struct{}

func (guiExtractionQueueLLM) ChatCall([]map[string]string) (string, error) { return `[]`, nil }
func (guiExtractionQueueLLM) IsConfigured() bool                           { return true }

func TestPostConversationPersistsExtractionBeforeCancellableWork(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	extractor := memory.NewOnlineExtractor(store, guiExtractionQueueLLM{})
	extractor.SetQueuePauseChecker(func(string) bool { return true })
	defer extractor.Stop()
	store.SetOnlineExtractor(extractor)
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}
	handler := &IMMessageHandler{app: app, memoryStore: store}
	history := []agent.ConversationEntry{
		{ID: "u1", Role: "user", Content: "remember the project convention"},
		{ID: "a1", Role: "assistant", Content: "understood"},
		{ID: "u2", Role: "user", Content: "use concise answers"},
		{ID: "a2", Role: "assistant", Content: "done"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler.runPostConversationProcessing(ctx, desktopUserID, "request-1", history, nil)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(store.Path()), "online-extraction-queue.json"))
	if err != nil {
		t.Fatalf("extraction outbox was not persisted before cancellation: %v", err)
	}
	for _, id := range []string{"u1", "a1", "u2", "a2"} {
		if !strings.Contains(string(data), id) {
			t.Fatalf("outbox missing message %s: %s", id, data)
		}
	}
}

func TestConversationExtractionIDsAreStableForLegacyEntries(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "first", Timestamp: 123},
		{Role: "assistant", Content: "answer", Timestamp: 124},
	}
	first := convertHistoryToMessages(history, 10)
	second := convertHistoryToMessages(history, 10)
	for i := range first {
		if first[i].ID == "" || first[i].ID != second[i].ID {
			t.Fatalf("legacy extraction ID is not stable: %q vs %q", first[i].ID, second[i].ID)
		}
	}
	withID := convertHistoryToMessages([]agent.ConversationEntry{{ID: "stable-id", Role: "user", Content: "hello"}}, 10)
	if len(withID) != 1 || withID[0].ID != "stable-id" {
		t.Fatalf("existing conversation ID was not preserved: %+v", withID)
	}
}
