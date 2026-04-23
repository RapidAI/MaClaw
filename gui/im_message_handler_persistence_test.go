package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"path/filepath"
	"testing"
)

func TestNewIMMessageHandlerLoadsPersistedDesktopConversation(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	storePath := filepath.Join(app.GetDataDir(), "ai_assistant_conversation.json")
	seed := agent.NewPersistentConversationMemory(storePath)
	seed.Save("desktop-user", []agent.ConversationEntry{
		{Role: "user", Content: "persisted user"},
		{Role: "assistant", Content: "persisted assistant"},
	})
	seed.Stop()

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	defer h.memory.Stop()

	got := h.memory.Load("desktop-user")
	if len(got) != 2 {
		t.Fatalf("history length = %d, want 2", len(got))
	}
	if got[0].Content != "persisted user" || got[1].Content != "persisted assistant" {
		t.Fatalf("unexpected restored history: %+v", got)
	}
}
