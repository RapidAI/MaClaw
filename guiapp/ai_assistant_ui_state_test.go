package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestAIAssistantUIStatePersistsUnderDataDir(t *testing.T) {
	home := t.TempDir()
	app := &App{testHomeDir: home}

	state := AIAssistantUIState{
		Messages: []map[string]interface{}{{
			"id":      "m1",
			"role":    "user",
			"content": "hello",
		}},
		Prompts:                  []string{"hello"},
		ContextBoundaryMessageID: " boundary ",
	}

	if err := app.SaveAIAssistantUIState(state); err != nil {
		t.Fatalf("SaveAIAssistantUIState() error = %v", err)
	}

	wantPath := filepath.Join(home, ".maclaw", "data", "ai_assistant_ui_state.db")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("state file not written at %s: %v", wantPath, err)
	}

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if loaded.StoragePath != wantPath {
		t.Fatalf("StoragePath = %q, want %q", loaded.StoragePath, wantPath)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0]["content"] != "hello" {
		t.Fatalf("Messages = %#v", loaded.Messages)
	}
	if len(loaded.Prompts) != 1 || loaded.Prompts[0] != "hello" {
		t.Fatalf("Prompts = %#v", loaded.Prompts)
	}
	if loaded.ContextBoundaryMessageID != "boundary" {
		t.Fatalf("ContextBoundaryMessageID = %q", loaded.ContextBoundaryMessageID)
	}

	if err := app.ClearAIAssistantUIState(); err != nil {
		t.Fatalf("ClearAIAssistantUIState() error = %v", err)
	}
	cleared, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() after clear error = %v", err)
	}
	if len(cleared.Messages) != 0 || len(cleared.Prompts) != 0 || cleared.ContextBoundaryMessageID != "" {
		t.Fatalf("clear left state behind: %#v", cleared)
	}
	if _, err := os.Stat(app.aiAssistantUIStatePath()); !os.IsNotExist(err) {
		t.Fatalf("legacy state file still exists after clear, stat err = %v", err)
	}
}

func TestAIAssistantUIStateInjectsStartupCrashRecoveryCard(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	app.aiConversationMemory = mem
	mem.UpsertUnfinishedSlot(desktopUserID, &agent.UnfinishedTaskSlot{
		SlotID:          "crash-slot",
		Status:          agent.UnfinishedTaskSlotStatusInterrupted,
		LastTask:        "finish implementation",
		Source:          agent.UnfinishedTaskSlotSourceInFlightRecovery,
		SideEffectState: "external_uncertain",
		RecoveryMode:    "requires_review",
	})
	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if len(loaded.Messages) != 1 {
		t.Fatalf("messages=%#v", loaded.Messages)
	}
	raw, ok := loaded.Messages[0]["unfinishedSlot"].(map[string]interface{})
	if !ok || raw["slotID"] != "crash-slot" || raw["recoveryMode"] != "requires_review" {
		t.Fatalf("startup recovery payload=%#v", raw)
	}
}

func TestAIAssistantUIStateInjectsEveryPendingStartupRecoveryCard(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	app.aiConversationMemory = mem
	mem.UpsertUnfinishedSlot(desktopUserID, &agent.UnfinishedTaskSlot{
		SlotID: "desktop-recovery", Status: agent.UnfinishedTaskSlotStatusInterrupted,
		Source: agent.UnfinishedTaskSlotSourceInFlightRecovery,
	})
	mem.UpsertUnfinishedSlot(desktopUserID+":project", &agent.UnfinishedTaskSlot{
		SlotID: "project-recovery", Status: agent.UnfinishedTaskSlotStatusInterrupted,
		Source: agent.UnfinishedTaskSlotSourceInFlightRecovery,
	})
	// A completed recovery is historical evidence, not an actionable startup card.
	mem.UpsertUnfinishedSlot("completed-user", &agent.UnfinishedTaskSlot{
		SlotID: "completed-recovery", Status: agent.UnfinishedTaskSlotStatusCompleted,
		Source: agent.UnfinishedTaskSlotSourceInFlightRecovery,
	})

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("messages=%#v, want two pending recovery cards", loaded.Messages)
	}
	got := map[string]string{}
	for _, message := range loaded.Messages {
		raw, ok := message["unfinishedSlot"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing unfinishedSlot payload: %#v", message)
		}
		got[raw["slotID"].(string)] = message["sessionKey"].(string)
	}
	if got["desktop-recovery"] != desktopUserID || got["project-recovery"] != desktopUserID+":project" {
		t.Fatalf("recovery cards were not projected to their sessions: %#v", got)
	}
}

func TestAIAssistantUIStateNormalizesAndBoundsPayload(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	messages := make([]map[string]interface{}, 0, 205)
	for i := 0; i < 205; i++ {
		messages = append(messages, map[string]interface{}{
			"id":      fmt.Sprintf("m%d", i),
			"role":    "user",
			"content": i,
		})
	}
	messages = append(messages,
		map[string]interface{}{"id": "progress", "role": "progress", "content": "skip"},
		map[string]interface{}{"id": "system", "role": "system", "content": "skip"},
		map[string]interface{}{"role": "user", "content": "skip"},
	)
	prompts := make([]string, 0, 104)
	for i := 0; i < 104; i++ {
		prompts = append(prompts, " prompt ")
	}
	prompts = append(prompts, " ")

	if err := app.SaveAIAssistantUIState(AIAssistantUIState{
		Messages:                 messages,
		Prompts:                  prompts,
		ContextBoundaryMessageID: " boundary ",
	}); err != nil {
		t.Fatalf("SaveAIAssistantUIState() error = %v", err)
	}

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if len(loaded.Messages) != conversationViewHotMessages {
		t.Fatalf("messages len = %d, want %d", len(loaded.Messages), conversationViewHotMessages)
	}
	if loaded.Messages[0]["content"] != float64(205-conversationViewHotMessages) {
		t.Fatalf("oldest retained message content = %#v, want %d", loaded.Messages[0]["content"], 205-conversationViewHotMessages)
	}
	if loaded.ArchivedCount != 205-conversationViewHotMessages {
		t.Fatalf("archived count = %d, want %d", loaded.ArchivedCount, 205-conversationViewHotMessages)
	}
	archived := app.LoadAIAssistantUIArchive()
	if len(archived) != 205-conversationViewHotMessages {
		t.Fatalf("archive len = %d, want %d", len(archived), 205-conversationViewHotMessages)
	}
	if archived[0]["id"] != "m0" {
		t.Fatalf("oldest archived id = %#v", archived[0]["id"])
	}
	if len(loaded.Prompts) != maxAIAssistantUIStatePrompts {
		t.Fatalf("prompts len = %d, want %d", len(loaded.Prompts), maxAIAssistantUIStatePrompts)
	}
	for _, prompt := range loaded.Prompts {
		if prompt != "prompt" {
			t.Fatalf("prompt = %q, want trimmed prompt", prompt)
		}
	}
	if loaded.ContextBoundaryMessageID != "boundary" {
		t.Fatalf("ContextBoundaryMessageID = %q", loaded.ContextBoundaryMessageID)
	}
}

func TestAIAssistantUIStateStripsAssistantHiddenAndToolCallContent(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := app.aiAssistantUIStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	raw := `{"messages":[{"id":"a1","role":"assistant","content":"visible\n<details><summary>思考过程</summary>hidden</details>\n<tool_call[]>\n{\"name\":\"write_file\",\"arguments\":{\"path\":\"a.txt\",\"content\":\"x\"}}"},{"id":"u1","role":"user","content":"<tool_call[]> keep user text"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if got := loaded.Messages[0]["content"]; got != "visible" {
		t.Fatalf("assistant content = %#v, want visible", got)
	}
	if got := loaded.Messages[1]["content"]; got != "<tool_call[]> keep user text" {
		t.Fatalf("user content = %#v, want untouched user content", got)
	}
	aiAssistantUIStateMu.Lock()
	app.closeAIAssistantUIStateDBLocked()
	aiAssistantUIStateMu.Unlock()
	persisted, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() after reopen error = %v", err)
	}
	if got := persisted.Messages[0]["content"]; got != "visible" {
		t.Fatalf("persisted assistant content = %#v, want visible", got)
	}
}

func TestNormalizeAIAssistantUIMessageRemovesControlCharacters(t *testing.T) {
	message := map[string]interface{}{
		"role":      "assistant",
		"content":   "visible\x00 answer",
		"reasoning": "\x01private\u0085 thought",
	}
	normalizeAIAssistantUIMessage(message)
	if got := message["content"]; got != "visible answer" {
		t.Fatalf("content = %#v", got)
	}
	if got := message["reasoning"]; got != "private thought" {
		t.Fatalf("reasoning = %#v", got)
	}
}

func TestLoadAIAssistantUIStateMigratesControlCharacters(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := app.aiAssistantUIStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	raw := "{\"messages\":[{\"id\":\"a1\",\"role\":\"assistant\",\"content\":\"visible\\u0000 answer\",\"reasoning\":\"\\u0001private\\u0085 thought\"}]}"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if got := loaded.Messages[0]["content"]; got != "visible answer" {
		t.Fatalf("content = %#v", got)
	}
	if got := loaded.Messages[0]["reasoning"]; got != "private thought" {
		t.Fatalf("reasoning = %#v", got)
	}
	aiAssistantUIStateMu.Lock()
	app.closeAIAssistantUIStateDBLocked()
	aiAssistantUIStateMu.Unlock()
	persisted, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() after reopen error = %v", err)
	}
	if got := persisted.Messages[0]["content"]; got != "visible answer" {
		t.Fatalf("persisted content = %#v", got)
	}
	if got := persisted.Messages[0]["reasoning"]; got != "private thought" {
		t.Fatalf("persisted reasoning = %#v", got)
	}
}

func TestAIAssistantUIStateKeepsPromptAttachmentsAligned(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveAIAssistantUIState(AIAssistantUIState{
		Prompts: []string{" see this ", " ", "only text", "   "},
		PromptAttachments: [][]AIAssistantPromptAttachment{
			{
				{FilePath: " D:/shots/see.png ", FileName: "see.png", Extension: ".png", IsImage: true},
				{FilePath: "D:/shots/see.png", FileName: "duplicate.png", Extension: ".png", IsImage: true},
			},
			{{FilePath: "D:/shots/alone.png", FileName: "", Extension: ".png", IsImage: true}},
			nil,
			{},
		},
	}); err != nil {
		t.Fatalf("SaveAIAssistantUIState() error = %v", err)
	}

	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if len(loaded.Prompts) != 3 || loaded.Prompts[0] != "see this" || loaded.Prompts[1] != "" || loaded.Prompts[2] != "only text" {
		t.Fatalf("Prompts = %#v", loaded.Prompts)
	}
	if len(loaded.PromptAttachments) != 3 {
		t.Fatalf("PromptAttachments = %#v", loaded.PromptAttachments)
	}
	if got := loaded.PromptAttachments[0]; len(got) != 1 || got[0].FilePath != "D:/shots/see.png" || !got[0].IsImage {
		t.Fatalf("first attachments = %#v", got)
	}
	if got := loaded.PromptAttachments[1]; len(got) != 1 || got[0].FilePath != "D:/shots/alone.png" || got[0].FileName != "alone.png" {
		t.Fatalf("image-only attachments = %#v", got)
	}
	if len(loaded.PromptAttachments[2]) != 0 {
		t.Fatalf("text-only attachments = %#v", loaded.PromptAttachments[2])
	}
}

func TestAIAssistantUIStateDropsPromptAttachmentsOutsideTheKeptWindow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	prompts := make([]string, 0, maxAIAssistantUIStatePrompts+4)
	rows := make([][]AIAssistantPromptAttachment, 0, maxAIAssistantUIStatePrompts+4)
	prompts = append(prompts, "old image")
	rows = append(rows, []AIAssistantPromptAttachment{{FilePath: "D:/old.png", FileName: "old.png", IsImage: true}})
	for i := 0; i < maxAIAssistantUIStatePrompts+3; i++ {
		prompts = append(prompts, "later")
		rows = append(rows, nil)
	}
	prompts = append(prompts, "newest image")
	rows = append(rows, []AIAssistantPromptAttachment{{FilePath: "D:/new.png", FileName: "new.png", IsImage: true}})

	if err := app.SaveAIAssistantUIState(AIAssistantUIState{Prompts: prompts, PromptAttachments: rows}); err != nil {
		t.Fatalf("SaveAIAssistantUIState() error = %v", err)
	}
	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatalf("LoadAIAssistantUIState() error = %v", err)
	}
	if len(loaded.Prompts) != maxAIAssistantUIStatePrompts || len(loaded.PromptAttachments) != maxAIAssistantUIStatePrompts {
		t.Fatalf("len prompts=%d attachments=%d", len(loaded.Prompts), len(loaded.PromptAttachments))
	}
	if loaded.Prompts[0] == "old image" {
		t.Fatalf("oldest image prompt was kept: %#v", loaded.Prompts[:2])
	}
	last := len(loaded.Prompts) - 1
	if loaded.Prompts[last] != "newest image" || len(loaded.PromptAttachments[last]) != 1 || loaded.PromptAttachments[last][0].FilePath != "D:/new.png" {
		t.Fatalf("newest prompt = %q attachments = %#v", loaded.Prompts[last], loaded.PromptAttachments[last])
	}
	if len(loaded.PromptAttachments[0]) != 0 {
		t.Fatalf("kept window inherited a dropped image: %#v", loaded.PromptAttachments[0])
	}
}

func TestAIAssistantUIStateSQLiteStoresNormalizedFields(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	details := map[string]interface{}{
		"items": []interface{}{map[string]interface{}{"name": "first", "done": true}},
		"empty": map[string]interface{}{},
	}
	if err := app.SaveAIAssistantUIState(AIAssistantUIState{
		Messages: []map[string]interface{}{{
			"id": "normalized", "role": "assistant", "content": "database column",
			"sessionKey": "desktop-user", "timestamp": float64(1234), "details": details,
		}},
		Prompts: []string{"with attachment"},
		PromptAttachments: [][]AIAssistantPromptAttachment{{
			{FilePath: "D:/docs/a.pdf", FileName: "a.pdf", Extension: ".pdf"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	db, err := app.aiAssistantUIStateDBLocked()
	if err != nil {
		t.Fatal(err)
	}
	var content string
	if err := db.QueryRow(`SELECT content FROM ai_assistant_ui_messages WHERE message_id='normalized'`).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "database column" {
		t.Fatalf("content column = %q", content)
	}
	fieldPath := sqliteArrayFieldPath(sqliteMapFieldPath(sqliteMapFieldPath("", "details"), "items"), 0)
	fieldPath = sqliteMapFieldPath(fieldPath, "name")
	var fieldValue string
	if err := db.QueryRow(`SELECT value_text FROM ai_assistant_ui_message_fields WHERE field_path=?`, fieldPath).Scan(&fieldValue); err != nil {
		t.Fatal(err)
	}
	if fieldValue != "first" {
		t.Fatalf("nested field value = %q", fieldValue)
	}
	var attachmentPath string
	if err := db.QueryRow(`SELECT file_path FROM ai_assistant_ui_prompt_attachments`).Scan(&attachmentPath); err != nil {
		t.Fatal(err)
	}
	if attachmentPath != "D:/docs/a.pdf" {
		t.Fatalf("attachment path = %q", attachmentPath)
	}
	var jsonColumns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE sql LIKE '%payload%' OR sql LIKE '%_json%'`).Scan(&jsonColumns); err != nil {
		t.Fatal(err)
	}
	if jsonColumns != 0 {
		t.Fatalf("SQLite schema still contains JSON document columns: %d", jsonColumns)
	}

	aiAssistantUIStateMu.Lock()
	app.closeAIAssistantUIStateDBLocked()
	aiAssistantUIStateMu.Unlock()
	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Messages[0]["details"]; !reflect.DeepEqual(got, details) {
		t.Fatalf("nested details = %#v, want %#v", got, details)
	}
}

func TestAIAssistantUIStateClearDoesNotReimportLegacyJSON(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := app.aiAssistantUIStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"messages":[{"id":"legacy","role":"user","content":"must stay cleared"}],"prompts":["legacy"]}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := app.LoadAIAssistantUIState()
	if err != nil || len(loaded.Messages) != 1 {
		t.Fatalf("legacy import = %#v, %v", loaded.Messages, err)
	}
	if err := app.ClearAIAssistantUIState(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	aiAssistantUIStateMu.Lock()
	app.closeAIAssistantUIStateDBLocked()
	aiAssistantUIStateMu.Unlock()
	loaded, err = app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 0 || len(loaded.Prompts) != 0 {
		t.Fatalf("legacy JSON resurrected cleared state: %#v", loaded)
	}
}

func TestACPAssistantUIStateRoundTripsThroughSQLite(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.appendACPRoundToUIState("from ACP", "request-1", desktopUserID)
	app.patchACPAssistantUIState("request-1", "ACP answer")
	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[0]["content"] != "from ACP" || loaded.Messages[1]["content"] != "ACP answer" {
		t.Fatalf("ACP messages = %#v", loaded.Messages)
	}
	if _, err := os.Stat(app.aiAssistantUIStatePath()); !os.IsNotExist(err) {
		t.Fatalf("ACP created a legacy JSON state file: %v", err)
	}
	db, err := app.aiAssistantUIStateDBLocked()
	if err != nil {
		t.Fatal(err)
	}
	var content string
	if err := db.QueryRow(`SELECT content FROM ai_assistant_ui_messages WHERE message_id='acp-asst-request-1'`).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "ACP answer" {
		t.Fatalf("ACP database content = %q", content)
	}
}

func TestAIAssistantUIStateIncrementalWrites(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	state := AIAssistantUIState{
		Messages: []map[string]interface{}{
			{"id": "m1", "role": "user", "content": "first"},
			{"id": "m2", "role": "assistant", "content": "second", "details": map[string]interface{}{"step": 1}},
		},
		Prompts: []string{"first"},
	}
	if err := app.SaveAIAssistantUIState(state); err != nil {
		t.Fatal(err)
	}
	db, err := app.aiAssistantUIStateDBLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE ui_write_audit(changes INTEGER NOT NULL);
INSERT INTO ui_write_audit VALUES(0);
CREATE TRIGGER ui_audit_insert AFTER INSERT ON ai_assistant_ui_messages BEGIN UPDATE ui_write_audit SET changes=changes+1; END;
CREATE TRIGGER ui_audit_update AFTER UPDATE ON ai_assistant_ui_messages BEGIN UPDATE ui_write_audit SET changes=changes+1; END;
CREATE TRIGGER ui_audit_delete AFTER DELETE ON ai_assistant_ui_messages BEGIN UPDATE ui_write_audit SET changes=changes+1; END;
`); err != nil {
		t.Fatal(err)
	}
	changes := func() int {
		var count int
		if err := db.QueryRow(`SELECT changes FROM ui_write_audit`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if err := app.SaveAIAssistantUIState(state); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 0 {
		t.Fatalf("identical save rewrote %d message rows", got)
	}
	state.Messages[1]["content"] = "second edited"
	if err := app.SaveAIAssistantUIState(state); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 2 {
		t.Fatalf("single message edit changed %d rows, want delete+insert of one row", got)
	}
	state.Messages = append(state.Messages, map[string]interface{}{"id": "m3", "role": "user", "content": "third"})
	if err := app.SaveAIAssistantUIState(state); err != nil {
		t.Fatal(err)
	}
	if got := changes(); got != 3 {
		t.Fatalf("append changed %d total rows, want one additional insert", got)
	}
	state.Messages[0], state.Messages[2] = state.Messages[2], state.Messages[0]
	if err := app.SaveAIAssistantUIState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 3 || loaded.Messages[0]["id"] != "m3" || loaded.Messages[1]["id"] != "m2" || loaded.Messages[2]["id"] != "m1" {
		t.Fatalf("reordered messages = %#v", loaded.Messages)
	}
	if loaded.Messages[1]["content"] != "second edited" {
		t.Fatalf("message fields were lost during reorder: %#v", loaded.Messages[1])
	}
}

func TestAIAssistantUIStateBackfillsMessageSignatures(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	state := AIAssistantUIState{Messages: []map[string]interface{}{{
		"id": "legacy-row", "role": "assistant", "content": "keep", "details": map[string]interface{}{"step": 2},
	}}}
	if err := app.SaveAIAssistantUIState(state); err != nil {
		t.Fatal(err)
	}
	db, err := app.aiAssistantUIStateDBLocked()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE ai_assistant_ui_messages DROP COLUMN signature`); err != nil {
		t.Fatal(err)
	}
	aiAssistantUIStateMu.Lock()
	app.closeAIAssistantUIStateDBLocked()
	aiAssistantUIStateMu.Unlock()
	db, err = app.aiAssistantUIStateDBLocked()
	if err != nil {
		t.Fatal(err)
	}
	var signature string
	if err := db.QueryRow(`SELECT signature FROM ai_assistant_ui_messages WHERE message_id='legacy-row'`).Scan(&signature); err != nil {
		t.Fatal(err)
	}
	if signature == "" {
		t.Fatal("legacy message signature was not backfilled")
	}
	loaded, err := app.LoadAIAssistantUIState()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0]["content"] != "keep" {
		t.Fatalf("message changed during signature backfill: %#v", loaded.Messages)
	}
}
