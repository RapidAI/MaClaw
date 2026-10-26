package guiapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitConversationViewKeepsAUserTurnAndOpenCard(t *testing.T) {
	messages := make([]interface{}, 0, 50)
	for i := 0; i < 50; i++ {
		messages = append(messages, map[string]interface{}{
			"id":   fmt.Sprintf("m%d", i),
			"role": "user",
		})
	}
	hot, earlier := splitConversationViewAny(messages)
	if len(hot) != conversationViewHotMessages || len(earlier) != 10 {
		t.Fatalf("hot=%d earlier=%d", len(hot), len(earlier))
	}

	// The cut lands on an assistant row. Walk back to the user row that owns it.
	paired := []interface{}{
		map[string]interface{}{"id": "u", "role": "user"},
		map[string]interface{}{"id": "a", "role": "assistant"},
	}
	for i := 0; i < conversationViewHotMessages-1; i++ {
		paired = append(paired, map[string]interface{}{"id": fmt.Sprintf("x%d", i), "role": "user"})
	}
	hot, earlier = splitConversationViewAny(paired)
	if len(earlier) != 0 || len(hot) != len(paired) {
		t.Fatalf("turn align hot=%d earlier=%d total=%d", len(hot), len(earlier), len(paired))
	}

	withCard := make([]interface{}, 0, conversationViewHotMessages+8)
	for i := 0; i < conversationViewHotMessages+8; i++ {
		message := map[string]interface{}{"id": fmt.Sprintf("c%d", i), "role": "user"}
		if i == conversationViewHotMessages+8-conversationViewHotMessages-4 {
			message["unfinishedSlot"] = map[string]interface{}{"status": "interrupted"}
		}
		withCard = append(withCard, message)
	}
	hot, earlier = splitConversationViewAny(withCard)
	if conversationMessageID(hot[0]) != "c4" {
		t.Fatalf("open card was archived, hot starts at %s", conversationMessageID(hot[0]))
	}
	if len(earlier) != 4 {
		t.Fatalf("earlier=%d, want 4", len(earlier))
	}
}

func TestSavingTheHotTailKeepsTheUIArchive(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	messages := make([]map[string]interface{}, 0, 45)
	for i := 0; i < 45; i++ {
		messages = append(messages, map[string]interface{}{
			"id":      fmt.Sprintf("m%d", i),
			"role":    "user",
			"content": fmt.Sprintf("turn %d", i),
		})
	}
	if err := app.SaveAIAssistantUIState(AIAssistantUIState{Messages: messages}); err != nil {
		t.Fatal(err)
	}
	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != conversationViewHotMessages {
		t.Fatalf("hot=%d", len(loaded.Messages))
	}
	if err := app.SaveAIAssistantUIState(AIAssistantUIState{Messages: loaded.Messages}); err != nil {
		t.Fatal(err)
	}
	archived := app.LoadAIAssistantUIArchive()
	if len(archived) != 5 || archived[0]["id"] != "m0" || archived[4]["id"] != "m4" {
		t.Fatalf("archive=%v", conversationViewIDs(mapsToAny(archived)))
	}
	if err := app.ClearAIAssistantUIState(); err != nil {
		t.Fatal(err)
	}
	if again := app.LoadAIAssistantUIArchive(); len(again) != 0 {
		t.Fatalf("clear left %d archived messages", len(again))
	}
	if _, err := os.Stat(app.aiAssistantUIArchivePath()); !os.IsNotExist(err) {
		t.Fatalf("archive file still present: %v", err)
	}
}

func TestProjectTabConversationArchivesThePrefix(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	conversation := make([]interface{}, 0, 45)
	for i := 0; i < 45; i++ {
		conversation = append(conversation, map[string]interface{}{
			"id":      fmt.Sprintf("p%d", i),
			"role":    "user",
			"content": fmt.Sprintf("project %d", i),
		})
	}
	app.SaveProjectTabConversation("tab-window", conversation)
	loaded := app.LoadProjectTabConversation("tab-window")
	if len(loaded) != conversationViewHotMessages {
		t.Fatalf("loaded=%d", len(loaded))
	}
	if conversationMessageID(loaded[0]) != "p5" {
		t.Fatalf("hot starts at %s", conversationMessageID(loaded[0]))
	}
	archived := app.LoadProjectTabConversationArchive("tab-window")
	if len(archived) != 5 || conversationMessageID(archived[0]) != "p0" {
		t.Fatalf("archive=%v", conversationViewIDs(archived))
	}
	if app.ConversationViewArchiveCount("tab:tab-window") != 5 {
		t.Fatalf("count=%d", app.ConversationViewArchiveCount("tab:tab-window"))
	}
	// A later save of only the hot tail must not drop the archive.
	app.SaveProjectTabConversation("tab-window", loaded)
	if again := app.LoadProjectTabConversationArchive("tab-window"); len(again) != 5 {
		t.Fatalf("resave archive=%d", len(again))
	}
	app.SaveProjectTabConversation("tab-window", nil)
	if leftover := app.LoadProjectTabConversationArchive("tab-window"); len(leftover) != 0 {
		t.Fatalf("clear left %d", len(leftover))
	}
}

func TestConversationViewArchiveCountReadsStampedCountAndFallsBack(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	rows := []interface{}{
		map[string]interface{}{"id": "a", "role": "user", "content": "x", "sessionKey": "desktop-user"},
		map[string]interface{}{"id": "b", "role": "user", "content": "y", "sessionKey": "tab-1"},
	}
	// A desktop archive written before the count metadata existed must still
	// report the local rows, not 0.
	legacy, err := json.Marshal(conversationViewArchiveDocument{Messages: rows})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(app.aiAssistantUIArchivePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.aiAssistantUIArchivePath(), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if count := app.ConversationViewArchiveCount("desktop"); count != 1 {
		t.Fatalf("legacy desktop count=%d", count)
	}
	// A current write stamps the count; the endpoint reports it as before.
	if err := app.writeAIAssistantUIArchiveLocked(rows); err != nil {
		t.Fatal(err)
	}
	if count := app.ConversationViewArchiveCount("desktop"); count != 1 {
		t.Fatalf("stamped desktop count=%d", count)
	}

	// Tab archives fall back the same way, then report the stamped count.
	persist := app.ensureProjectTabSessionPersist()
	path, err := persist.conversationViewArchivePath("tab-count")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	if count := app.ConversationViewArchiveCount("tab:tab-count"); count != 2 {
		t.Fatalf("legacy tab count=%d", count)
	}
	if err := persist.SaveConversationViewArchive("tab-count", rows); err != nil {
		t.Fatal(err)
	}
	if count := app.ConversationViewArchiveCount("tab:tab-count"); count != 2 {
		t.Fatalf("stamped tab count=%d", count)
	}
}
