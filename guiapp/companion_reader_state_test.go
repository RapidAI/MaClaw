package guiapp

import (
	"encoding/json"
	"strings"
	"testing"
)

func newCompanionReaderTestApp(t *testing.T) *App {
	t.Helper()
	return &App{testHomeDir: t.TempDir()}
}

func loadCompanionReaderTestState(t *testing.T, app *App, legacyPDFMarks, legacyHistory string) companionReaderStatePayload {
	t.Helper()
	raw, err := app.LoadCompanionReaderState(legacyPDFMarks, "", legacyHistory)
	if err != nil {
		t.Fatalf("LoadCompanionReaderState: %v", err)
	}
	var payload companionReaderStatePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload
}

func TestCompanionReaderStatePersistsPdfMarks(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	marks := `[{"id":"m1","page":1,"color":"yellow","rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.04}],"text":"Hello"}]`
	if err := app.SaveCompanionPdfMarks(`C:\Docs\Paper.PDF`, marks); err != nil {
		t.Fatalf("SaveCompanionPdfMarks: %v", err)
	}
	payload := loadCompanionReaderTestState(t, app, "", "")
	got, ok := payload.PDFMarks["c:/docs/paper.pdf"]
	if !ok || !strings.Contains(string(got), "yellow") {
		t.Fatalf("marks = %v", payload.PDFMarks)
	}
	if _, ok := payload.PDFMarks["c:/docs/other.pdf"]; ok {
		t.Fatalf("unexpected second path: %v", payload.PDFMarks)
	}

	// An empty list is "clear my highlights for this document".
	if err := app.SaveCompanionPdfMarks(`C:\Docs\Paper.PDF`, "[]"); err != nil {
		t.Fatalf("SaveCompanionPdfMarks empty: %v", err)
	}
	payload = loadCompanionReaderTestState(t, app, "", "")
	if _, ok := payload.PDFMarks["c:/docs/paper.pdf"]; ok {
		t.Fatalf("cleared marks remained: %v", payload.PDFMarks)
	}
}

func TestCompanionReaderStatePersistsPdfBookmarks(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	bookmarks := `[{"id":"b1","page":7,"x":0.2,"y":0.4,"color":"teal","name":"基线"}]`
	if err := app.SaveCompanionPdfBookmarks(`C:\Docs\Paper.PDF`, bookmarks); err != nil {
		t.Fatalf("SaveCompanionPdfBookmarks: %v", err)
	}
	payload := loadCompanionReaderTestState(t, app, "", "")
	got, ok := payload.PDFBookmarks["c:/docs/paper.pdf"]
	if !ok || !strings.Contains(string(got), "基线") {
		t.Fatalf("bookmarks = %v", payload.PDFBookmarks)
	}
	if err := app.SaveCompanionPdfBookmarks(`C:\Docs\Paper.PDF`, "[]"); err != nil {
		t.Fatalf("SaveCompanionPdfBookmarks empty: %v", err)
	}
	payload = loadCompanionReaderTestState(t, app, "", "")
	if _, ok := payload.PDFBookmarks["c:/docs/paper.pdf"]; ok {
		t.Fatalf("cleared bookmarks remained: %v", payload.PDFBookmarks)
	}
	if err := app.SaveCompanionPdfBookmarks("/tmp/a.pdf", `{"not":"an array"}`); err == nil {
		t.Fatal("object bookmarks were accepted")
	}
}

func TestCompanionReaderStatePersistsPdfDrawings(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	drawings := `[{"id":"d1","page":1,"color":"yellow","tool":"line","points":[{"x":0.1,"y":0.2},{"x":0.3,"y":0.4}]}]`
	if err := app.SaveCompanionPdfDrawings(`C:\Docs\Paper.PDF`, drawings); err != nil {
		t.Fatalf("SaveCompanionPdfDrawings: %v", err)
	}
	payload := loadCompanionReaderTestState(t, app, "", "")
	got, ok := payload.PDFDrawings["c:/docs/paper.pdf"]
	if !ok || !strings.Contains(string(got), `"tool":"line"`) {
		t.Fatalf("drawings = %v", payload.PDFDrawings)
	}

	// An empty list is "clear my drawings for this document".
	if err := app.SaveCompanionPdfDrawings(`C:\Docs\Paper.PDF`, "[]"); err != nil {
		t.Fatalf("SaveCompanionPdfDrawings empty: %v", err)
	}
	payload = loadCompanionReaderTestState(t, app, "", "")
	if _, ok := payload.PDFDrawings["c:/docs/paper.pdf"]; ok {
		t.Fatalf("cleared drawings remained: %v", payload.PDFDrawings)
	}
}

func TestCompanionReaderStatePersistsChatMarks(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	marks := `[{"message":1,"color":"green","start":3,"end":8,"text":"lo wo"}]`
	if err := app.SaveCompanionChatMarks(`C:\Docs\Paper.PDF`, marks); err != nil {
		t.Fatalf("SaveCompanionChatMarks: %v", err)
	}
	payload := loadCompanionReaderTestState(t, app, "", "")
	got, ok := payload.ChatMarks["c:/docs/paper.pdf"]
	if !ok || !strings.Contains(string(got), "green") {
		t.Fatalf("chat marks = %v", payload.ChatMarks)
	}
	// Chat marks and PDF marks are different stores on the same document.
	if _, ok := payload.PDFMarks["c:/docs/paper.pdf"]; ok {
		t.Fatalf("chat marks leaked into pdf marks: %v", payload.PDFMarks)
	}
	if err := app.SaveCompanionChatMarks(`C:\Docs\Paper.PDF`, "[]"); err != nil {
		t.Fatalf("clear chat marks: %v", err)
	}
	if payload := loadCompanionReaderTestState(t, app, "", ""); len(payload.ChatMarks) != 0 {
		t.Fatalf("chat marks after clear = %v", payload.ChatMarks)
	}
	if err := app.SaveCompanionChatMarks("/tmp/a.md", `{"not":"an array"}`); err == nil {
		t.Fatal("non-array chat marks must be refused")
	}
}

func TestCompanionReaderStatePersistsPdfTranslations(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	translations := `[{"id":"t1","text":"方法","translation":"method","parts":[{"page":1,"rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.04}]}]}]`
	if err := app.SaveCompanionPdfTranslations(`C:\Docs\Paper.PDF`, translations); err != nil {
		t.Fatalf("SaveCompanionPdfTranslations: %v", err)
	}
	payload := loadCompanionReaderTestState(t, app, "", "")
	got, ok := payload.PDFTranslations["c:/docs/paper.pdf"]
	if !ok || !strings.Contains(string(got), "method") {
		t.Fatalf("translations = %v", payload.PDFTranslations)
	}
	if err := app.SaveCompanionPdfTranslations(`C:\Docs\Paper.PDF`, "[]"); err != nil {
		t.Fatalf("clear translations: %v", err)
	}
	if payload := loadCompanionReaderTestState(t, app, "", ""); len(payload.PDFTranslations) != 0 {
		t.Fatalf("translations after clear = %v", payload.PDFTranslations)
	}
}

func TestCompanionReaderStateNormalizesInputHistory(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	entries := make([]string, 0, companionReaderEntryMax+5)
	entries = append(entries, "   ", strings.Repeat("x", companionReaderEntryRunesMax+1))
	for i := 0; i < companionReaderEntryMax+5; i++ {
		entries = append(entries, " 问 "+string(rune('a'+i%26)))
	}
	body, _ := json.Marshal(entries)
	if err := app.SaveCompanionInputHistory(string(body)); err != nil {
		t.Fatalf("SaveCompanionInputHistory: %v", err)
	}
	payload := loadCompanionReaderTestState(t, app, "", "")
	if len(payload.InputHistory) != companionReaderEntryMax {
		t.Fatalf("history %d want %d", len(payload.InputHistory), companionReaderEntryMax)
	}
	for _, entry := range payload.InputHistory {
		if strings.TrimSpace(entry) != entry || entry == "" {
			t.Fatalf("unnormalized entry %q", entry)
		}
	}
	if err := app.SaveCompanionInputHistory("[]"); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if got := loadCompanionReaderTestState(t, app, "", "").InputHistory; len(got) != 0 {
		t.Fatalf("history after clear = %v", got)
	}
}

func TestCompanionReaderStateImportsLegacyDocuments(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	legacyMarks := `{"C:\\Docs\\Paper.PDF":[{"id":"m1","page":1,"color":"yellow","rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.04}],"text":"Hello"}],"":{"id":"blank"}}`
	legacyHistory := `["旧问题","  旧问题  ",""]`
	payload := loadCompanionReaderTestState(t, app, legacyMarks, legacyHistory)
	if _, ok := payload.PDFMarks["c:/docs/paper.pdf"]; !ok {
		t.Fatalf("legacy marks not imported: %v", payload.PDFMarks)
	}
	if len(payload.InputHistory) != 2 || payload.InputHistory[1] != "旧问题" {
		t.Fatalf("legacy history = %v", payload.InputHistory)
	}

	// State saved in SQLite wins over a leftover browser document.
	if err := app.SaveCompanionInputHistory(`["新问题"]`); err != nil {
		t.Fatalf("save new history: %v", err)
	}
	payload = loadCompanionReaderTestState(t, app, legacyMarks, legacyHistory)
	if len(payload.InputHistory) != 1 || payload.InputHistory[0] != "新问题" {
		t.Fatalf("import overwrote stored history: %v", payload.InputHistory)
	}
}

func TestCompanionReaderStateSkipsNonStringHistoryEntries(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	payload := loadCompanionReaderTestState(t, app, "", `["旧问题",42,null,{"x":1},"  另一个  "]`)
	if len(payload.InputHistory) != 2 || payload.InputHistory[0] != "旧问题" || payload.InputHistory[1] != "另一个" {
		t.Fatalf("history = %v", payload.InputHistory)
	}
}

func TestCompanionReaderStateSkipsCorruptRows(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	marks := `[{"id":"m1","page":1,"color":"yellow","rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.04}],"text":"Hello"}]`
	if err := app.SaveCompanionPdfMarks("/tmp/a.pdf", marks); err != nil {
		t.Fatalf("SaveCompanionPdfMarks: %v", err)
	}
	db, err := openCompanionReaderStateDB(app.companionReaderStateDBPath())
	if err != nil {
		t.Fatalf("open reader state database: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO reader_state (kind, key, value, updated_at) VALUES (?, ?, ?, ?)`,
		companionReaderPDFMarksKind, "/tmp/b.pdf", "{broken", companionReaderStateNow(),
	); err != nil {
		_ = db.Close()
		t.Fatalf("insert corrupt row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close reader state database: %v", err)
	}

	// One unreadable row must not take the whole reader state down with it.
	payload := loadCompanionReaderTestState(t, app, "", "")
	if _, ok := payload.PDFMarks["/tmp/a.pdf"]; !ok {
		t.Fatalf("valid marks missing: %v", payload.PDFMarks)
	}
	if _, ok := payload.PDFMarks["/tmp/b.pdf"]; ok {
		t.Fatalf("corrupt row returned: %v", payload.PDFMarks)
	}
}

func TestCompanionReaderStateRejectsBrokenInput(t *testing.T) {
	app := newCompanionReaderTestApp(t)
	if err := app.SaveCompanionPdfMarks("/tmp/a.pdf", `{"not":"an array"}`); err == nil {
		t.Fatal("object marks accepted")
	}
	if err := app.SaveCompanionInputHistory(`{"not":"an array"}`); err == nil {
		t.Fatal("object history accepted")
	}
	if _, err := app.LoadCompanionReaderState(`{"path":`, "", ""); err == nil {
		t.Fatal("broken legacy marks accepted")
	}
	if got := loadCompanionReaderTestState(t, app, "", ""); len(got.PDFMarks) != 0 || len(got.InputHistory) != 0 {
		t.Fatalf("state changed after rejected input: %+v", got)
	}
}
