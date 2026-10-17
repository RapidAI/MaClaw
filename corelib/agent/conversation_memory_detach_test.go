package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestDetachedCheckpointDoesNotRewriteMainStore(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "weather.json")
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()

	cm.Append("desktop-user", ConversationEntry{Role: "user", Content: "主会话保持不动"})
	cm.Append("desktop-user:D:/tasks/weather", ConversationEntry{Role: "user", Content: "旧任务"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := cm.UseSeparateSessionFile("desktop-user:D:/tasks/weather", sidePath); err != nil {
		t.Fatal(err)
	}
	if err := cm.PersistInFlightCheckpoint(
		"desktop-user:D:/tasks/weather",
		[]ConversationEntry{{Role: "user", Content: "工具检查点"}},
		"北京天气",
		"D:/tasks/weather",
		"run-1",
		InFlightCheckpoint{Sequence: 1, LastToolName: "send_file"},
	); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("main store was rewritten by a detached checkpoint")
	}
	side, err := os.ReadFile(sidePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(side), "工具检查点") || !strings.Contains(string(side), "send_file") {
		t.Fatalf("side file missing checkpoint: %s", side)
	}

	cm.Stop()
	reloaded := NewPersistentConversationMemory(mainPath)
	defer reloaded.Stop()
	got := reloaded.Load("desktop-user:D:/tasks/weather")
	found := false
	for _, entry := range got {
		if entry.Content == "工具检查点" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reloaded task session = %+v, want checkpoint text", got)
	}
	main := reloaded.Load("desktop-user")
	if len(main) != 1 || main[0].Content != "主会话保持不动" {
		t.Fatalf("main session = %+v", main)
	}
}

func TestOlderDetachedFileDoesNotReplaceNewerMainSession(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "weather.json")
	userID := "desktop-user:D:/tasks/weather"
	cm := NewPersistentConversationMemory(mainPath)
	cm.Append(userID, ConversationEntry{Role: "user", Content: "主库里的新内容"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	cm.Stop()

	if err := os.MkdirAll(filepath.Dir(sidePath), 0o755); err != nil {
		t.Fatal(err)
	}
	side, err := json.Marshal(memorySnapshot{Sessions: map[string]persistedSession{
		userID: {
			Entries:    []ConversationEntry{{Role: "user", Content: "旁路旧检查点"}},
			LastAccess: time.Now().Add(-2 * time.Hour),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidePath, side, 0o644); err != nil {
		t.Fatal(err)
	}
	index, err := json.Marshal(detachedSessionIndex{Sessions: map[string]string{userID: sidePath}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath+".detached.json", index, 0o644); err != nil {
		t.Fatal(err)
	}

	reloaded := NewPersistentConversationMemory(mainPath)
	defer reloaded.Stop()
	got := reloaded.Load(userID)
	for _, entry := range got {
		if entry.Content == "旁路旧检查点" {
			t.Fatalf("older side file replaced the newer shared session: %+v", got)
		}
	}
	found := false
	for _, entry := range got {
		if entry.Content == "主库里的新内容" {
			found = true
		}
	}
	if !found {
		t.Fatalf("shared session missing: %+v", got)
	}
}

func TestUnrelatedFlushDoesNotRewriteDetachedSideFile(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "weather.json")
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()

	cm.Append("desktop-user", ConversationEntry{Role: "user", Content: "主会话"})
	cm.Append("desktop-user:D:/tasks/weather", ConversationEntry{Role: "user", Content: "任务"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if err := cm.UseSeparateSessionFile("desktop-user:D:/tasks/weather", sidePath); err != nil {
		t.Fatal(err)
	}
	if err := cm.PersistInFlightCheckpoint(
		"desktop-user:D:/tasks/weather",
		[]ConversationEntry{{Role: "user", Content: "工具检查点"}},
		"北京天气",
		"D:/tasks/weather",
		"run-1",
		InFlightCheckpoint{Sequence: 1, LastToolName: "send_file"},
	); err != nil {
		t.Fatal(err)
	}
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(sidePath)
	if err != nil {
		t.Fatal(err)
	}
	cm.Append("desktop-user", ConversationEntry{Role: "assistant", Content: "另一会话"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(sidePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("unrelated flush rewrote the detached side file")
	}
	main, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(main), "工具检查点") {
		t.Fatalf("detached checkpoint returned to the shared store: %s", main)
	}
}

func TestFailedDetachedSaveDoesNotDropSharedCopy(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "blocked-side")
	if err := os.MkdirAll(sidePath, 0o755); err != nil {
		t.Fatal(err)
	}
	userID := "desktop-user:D:/tasks/weather"
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	cm.Append(userID, ConversationEntry{Role: "user", Content: "必须留下"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if err := cm.UseSeparateSessionFile(userID, sidePath); err == nil {
		t.Fatal("expected binding a directory as the side file to fail")
	}
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "必须留下"}, {Role: "assistant", Content: "旁路写失败"}})
	cm.Append("desktop-user", ConversationEntry{Role: "user", Content: "其他会话"})
	if err := cm.FlushNow(); err == nil {
		t.Fatal("expected the blocked side file to fail the flush")
	}
	main, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), "必须留下") {
		t.Fatalf("shared flush dropped the detached session: %s", main)
	}
}

func TestClearDetachedSessionDoesNotResurrect(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "session-transcripts", "weather.json")
	userID := "desktop-user:D:/tasks/weather"
	cm := NewPersistentConversationMemory(mainPath)
	cm.Append(userID, ConversationEntry{Role: "user", Content: "清空前的内容"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if err := cm.UseSeparateSessionFile(userID, sidePath); err != nil {
		t.Fatal(err)
	}
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "清空前的内容"}})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	cm.Clear(userID)
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	cm.Stop()

	reloaded := NewPersistentConversationMemory(mainPath)
	defer reloaded.Stop()
	if got := reloaded.Load(userID); len(got) != 0 {
		t.Fatalf("cleared session resurrected: %+v", got)
	}
	if _, err := os.Stat(sidePath); !os.IsNotExist(err) {
		t.Fatalf("side file still present: %v", err)
	}
}

func TestEvictExpiredKeepsDetachedPetTranscript(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "ai_assistant_conversation.json")
	sidePath := filepath.Join(dir, "pet_companion_conversation.json")
	cm := NewPersistentConversationMemory(mainPath)
	defer cm.Stop()
	if err := cm.UseSeparateSessionFile("desktop-pet", sidePath); err != nil {
		t.Fatal(err)
	}
	cm.Append("desktop-pet", ConversationEntry{Role: "user", Content: "宠物的话"})
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	sh := cm.shard("desktop-pet")
	sh.mu.Lock()
	if session := sh.sessions["desktop-pet"]; session != nil {
		session.lastAccess = time.Now().Add(-MemoryTTL - time.Hour)
	}
	sh.mu.Unlock()

	cm.EvictExpired()
	if err := cm.FlushNow(); err != nil {
		t.Fatal(err)
	}
	if got := cm.Load("desktop-pet"); len(got) != 1 {
		t.Fatalf("pet session was evicted: %#v", got)
	}
	side, err := os.ReadFile(sidePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(side), "宠物的话") {
		t.Fatalf("eviction wiped the pet file: %s", side)
	}
}
