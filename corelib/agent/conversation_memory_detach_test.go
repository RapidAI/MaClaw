package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetachedPetSessionStaysOutOfMainTranscript(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "pet_companion_conversation.json")
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	if err := cm.UseSeparateSessionFile("desktop-pet", sidePath); err != nil {
		t.Fatal(err)
	}
	cm.Append("desktop-user", ConversationEntry{Role: "user", Content: "主窗口的话"})
	cm.Append("desktop-pet", ConversationEntry{Role: "user", Content: "宠物的话"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	main, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(main), "desktop-pet") || strings.Contains(string(main), "宠物的话") {
		t.Fatalf("pet transcript landed in the main file: %s", main)
	}
	if !strings.Contains(string(main), "desktop-user") || !strings.Contains(string(main), "主窗口的话") {
		t.Fatalf("main transcript missing: %s", main)
	}
	side, err := os.ReadFile(sidePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(side), "desktop-pet") || !strings.Contains(string(side), "宠物的话") {
		t.Fatalf("pet file missing the pet turn: %s", side)
	}
	if strings.Contains(string(side), "desktop-user") || strings.Contains(string(side), "主窗口的话") {
		t.Fatalf("main transcript landed in the pet file: %s", side)
	}
}
