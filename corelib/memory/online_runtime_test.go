package memory

import (
	"context"
	"testing"
	"time"
)

func TestExtractOnlineConversationNoopsWithoutRuntime(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	result := store.ExtractOnlineConversation(context.Background(), []ConversationMessage{{Role: "user", Content: "remember api endpoint"}, {Role: "assistant", Content: "ok"}}, "", time.Now(), "user-1")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Added != 0 || result.Updated != 0 || result.Deleted != 0 || result.Errors != 0 {
		t.Fatalf("expected no-op result without online runtime, got %+v", result)
	}
}
