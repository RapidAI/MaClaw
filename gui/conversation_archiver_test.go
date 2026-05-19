package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestNewConversationArchiverReusesMaintenanceKnowledgeExtractor(t *testing.T) {
	store, err := memory.NewStoreWithMode(t.TempDir(), memory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	maintenance := memory.NewMaintenance(store, nil, nil)
	app := &App{memoryMaintenance: maintenance}
	archiver := NewConversationArchiver(store, app)
	if archiver.knowledgeExtractor == nil {
		t.Fatal("knowledge extractor was not initialized")
	}
	if archiver.knowledgeExtractor != maintenance.KnowledgeExtractor() {
		t.Fatal("conversation archiver must reuse corelib/memory Maintenance knowledge extractor")
	}
}

func TestConversationArchiverWithoutAppIsNoop(t *testing.T) {
	store, err := memory.NewStoreWithMode(t.TempDir(), memory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	archiver := NewConversationArchiver(store, nil)
	if archiver.knowledgeExtractor != nil {
		t.Fatal("nil app archiver should not create an LLM-backed fallback extractor")
	}
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "remember alpha"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "remember beta"},
		{Role: "assistant", Content: "ok"},
	}
	if err := archiver.Archive("user-1", entries); err != nil {
		t.Fatalf("Archive without app should be a noop: %v", err)
	}
}
