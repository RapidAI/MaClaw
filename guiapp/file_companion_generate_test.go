package guiapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// The model reaches the writer through the executor a companion turn really
// uses: the shared loop callbacks, which hand a companion tool to the
// companion handler before the legacy chain. The other entry point
// (executeAgentLoopToolCall) refuses every companion tool by policy, so a
// test written against it would report a tool that works as unreachable.
func TestFileCompanionTurnExecutorReachesThePDFWriter(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		t.Fatal(err)
	}
	userID := "file-companion:" + fileCompanionSessionID(canonical)
	if err := app.ensureFileCompanionHandler(userID, doc.SessionID); err != nil {
		t.Fatal(err)
	}
	callbacks := &sharedAgentLoopCallbacks{userID: userID, handler: app.imHandler}
	text := callbacks.ExecuteTool(fileCompanionToolGenerate, `{"markdown":"# 复习笔记\n\n第一章的定理。"}`)
	if !strings.Contains(text, "已生成 PDF") {
		t.Fatalf("companion executor = %q", text)
	}
	files := app.fileCompanionTurnFiles(userID)
	if len(files) != 1 {
		t.Fatalf("turn files = %d, want 1", len(files))
	}
	raw, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatalf("generated file missing: %v", err)
	}
	if !strings.HasPrefix(string(raw), "%PDF") {
		t.Fatal("result is not a pdf")
	}

	// The structured executor is the other half of the same path.
	app.seedFileCompanionTurnFiles(userID)
	result := callbacks.ExecuteToolStructured(fileCompanionToolGenerate, `{"markdown":"# 讲义\n"}`)
	if result.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("structured executor = %+v result=%q", result, result.Result)
	}
	if files := app.fileCompanionTurnFiles(userID); len(files) != 1 {
		t.Fatalf("turn files after the structured call = %d, want 1", len(files))
	}
}

func TestFileCompanionHostToolGenerateWritesAPDF(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate,
		`{"markdown":"# 复习笔记\n\n第一章的定理与证明。","title":"复习笔记"}`)
	if !ok {
		t.Fatalf("generate = %q ok=%v", text, ok)
	}
	files := app.fileCompanionTurnFiles(userID)
	if len(files) != 1 {
		t.Fatalf("turn files = %d, want 1", len(files))
	}
	raw, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatalf("generated file missing: %v", err)
	}
	if !strings.HasPrefix(string(raw), "%PDF") {
		t.Fatal("result is not a pdf")
	}
	if !strings.Contains(text, files[0].Path) {
		t.Fatalf("tool text %q does not name the file", text)
	}
	// The open document is never the output.
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(original) != "%PDF-1.4\n" {
		t.Fatalf("source changed: %q", original)
	}
	results := filepath.Join(app.GetDataDir(), "file-companion", "results")
	if !strings.HasPrefix(filepath.Clean(files[0].Path), filepath.Clean(results)) {
		t.Fatalf("pdf left the companion results dir: %s", files[0].Path)
	}
}

func TestFileCompanionHostToolGenerateRefusesAMissingBody(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"title":"只有标题"}`)
	if ok {
		t.Fatalf("generate without a body = %q ok=%v", text, ok)
	}
	if !strings.Contains(text, "markdown") {
		t.Fatalf("refusal %q does not name the missing argument", text)
	}
	if files := app.fileCompanionTurnFiles(userID); len(files) != 0 {
		t.Fatalf("a refused call left %d files behind", len(files))
	}
}

// A note longer than the bound is refused with the number, so the model can
// split it instead of retrying the same body.
func TestFileCompanionHostToolGenerateRefusesATooLongBody(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	body := strings.Repeat("字", fileCompanionGenerateMaxRunes+1)
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":`+strconv.Quote(body)+`}`)
	if ok {
		t.Fatalf("generate with an oversized body = %q ok=%v", text, ok)
	}
	if !strings.Contains(text, "太长") {
		t.Fatalf("refusal %q does not say the body is too long", text)
	}
	if files := app.fileCompanionTurnFiles(userID); len(files) != 0 {
		t.Fatalf("a refused call left %d files behind", len(files))
	}
}

// A name is a name. The model cannot steer the writer out of the session
// directory with a path in the file name.
func TestFileCompanionHostToolGenerateIgnoresAPathInTheFileName(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	outside := filepath.Join(t.TempDir(), "escaped.pdf")
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate,
		`{"markdown":"# 笔记\n","filename":"..\\..\\escaped.pdf"}`)
	if !ok {
		t.Fatalf("generate = %q ok=%v", text, ok)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("the writer followed a path out of the results directory")
	}
	files := app.fileCompanionTurnFiles(userID)
	if len(files) != 1 {
		t.Fatalf("turn files = %d, want 1", len(files))
	}
	results := filepath.Join(app.GetDataDir(), "file-companion", "results")
	if !strings.HasPrefix(filepath.Clean(files[0].Path), filepath.Clean(results)) {
		t.Fatalf("pdf left the companion results dir: %s", files[0].Path)
	}
}

// Two notes in one session are two files: the first one is never overwritten.
func TestFileCompanionHostToolGenerateNeverOverwrites(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	first, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":"# 复习笔记\n","filename":"复习笔记"}`)
	if !ok {
		t.Fatalf("first generate = %q ok=%v", first, ok)
	}
	second, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":"# 复习笔记（改）\n","filename":"复习笔记"}`)
	if !ok {
		t.Fatalf("second generate = %q ok=%v", second, ok)
	}
	files := app.fileCompanionTurnFiles(userID)
	if len(files) != 2 {
		t.Fatalf("turn files = %d, want 2", len(files))
	}
	if files[0].Path == files[1].Path {
		t.Fatalf("both calls wrote %s", files[0].Path)
	}
	raw, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), "%PDF") {
		t.Fatal("the first file is not a pdf")
	}
}

// A turn that keeps asking for another file is not answering.
func TestFileCompanionHostToolGenerateStopsAfterTheTurnCap(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	for i := 0; i < fileCompanionGenerateMaxPerTurn; i++ {
		if text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":"# 笔记\n"}`); !ok {
			t.Fatalf("call %d = %q ok=%v", i, text, ok)
		}
	}
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":"# 再来一份\n"}`)
	if ok {
		t.Fatalf("call past the cap = %q ok=%v", text, ok)
	}
	if files := app.fileCompanionTurnFiles(userID); len(files) != fileCompanionGenerateMaxPerTurn {
		t.Fatalf("turn files = %d, want %d", len(files), fileCompanionGenerateMaxPerTurn)
	}
}

// A new turn starts with an empty list: an earlier turn's file belongs to that
// turn's reply.
func TestFileCompanionSeedTurnFilesClearsThePreviousTurn(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	h := &IMMessageHandler{app: app}
	if text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":"# 笔记\n"}`); !ok {
		t.Fatalf("generate = %q ok=%v", text, ok)
	}
	if files := app.fileCompanionTurnFiles(userID); len(files) != 1 {
		t.Fatalf("turn files = %d, want 1", len(files))
	}
	app.seedFileCompanionTurnFiles(userID)
	if files := app.fileCompanionTurnFiles(userID); len(files) != 0 {
		t.Fatalf("turn files after reseed = %d, want 0", len(files))
	}
}

// A restored transcript also holds assistant bubbles made of a pending plan
// alone. Those are not answers, so the file must not count them: otherwise it
// lands one bubble too early.
func TestFileCompanionAttachGeneratedFilesSkipsReasoningOnlyBubbles(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	sessionID := doc.SessionID
	produced := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID, "笔记.pdf")
	if err := os.MkdirAll(filepath.Dir(produced), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(produced, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The second answer produced the file.
	app.fileCompanionRecordGeneratedFile(sessionID, FileCompanionFile{Label: "笔记.pdf", Path: produced}, 2)

	entries := []agent.ConversationEntry{
		{Role: "user", Content: "第一个问题"},
		{Role: "assistant", Content: "第一个回答"},
		{Role: "user", Content: "第二个问题"},
		{Role: "assistant", Content: "", ReasoningContent: "先想一下", ToolCalls: []map[string]string{
			{"id": "c1", "type": "function", "name": "pdf_read_pages"},
		}},
		{Role: "user", Content: "第三个问题"},
		{Role: "assistant", Content: "第三个回答"},
	}
	messages := fileCompanionDisplayMessages(entries, nil)
	restored := app.fileCompanionAttachGeneratedFiles(sessionID, messages)
	answers := 0
	for _, message := range restored {
		if message.Role != "assistant" || strings.TrimSpace(message.Text) == "" {
			continue
		}
		answers++
		want := 0
		if answers == 2 {
			want = 1
		}
		if len(message.Files) != want {
			t.Fatalf("answer %d carries %d files, want %d", answers, len(message.Files), want)
		}
	}
	if answers != 2 {
		t.Fatalf("restored answers = %d, want 2", answers)
	}
}

// Without a title of its own the file is named after the document, and the
// second line does not repeat it.
func TestFileCompanionGenerateTitleDoesNotRepeatTheSource(t *testing.T) {
	title, subtitle := fileCompanionGenerateTitle(map[string]interface{}{}, "D:/books/信息安全数学基础.pdf")
	if title != "信息安全数学基础 笔记" || subtitle != "" {
		t.Fatalf("default title = %q subtitle = %q", title, subtitle)
	}
	title, subtitle = fileCompanionGenerateTitle(map[string]interface{}{"title": "复习提纲"}, "D:/books/信息安全数学基础.pdf")
	if title != "复习提纲" || subtitle != "信息安全数学基础" {
		t.Fatalf("given title = %q subtitle = %q", title, subtitle)
	}
}

// The manifest is read, changed and written back. Two records that land
// together must both survive, and a reader must never see half a file.
func TestFileCompanionGeneratedManifestKeepsEveryFile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	sessionID := doc.SessionID
	dir := filepath.Join(app.GetDataDir(), "file-companion", "results", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := filepath.Join(dir, fmt.Sprintf("并发-%d.pdf", i))
			if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
				return
			}
			app.fileCompanionRecordGeneratedFile(sessionID, FileCompanionFile{Label: filepath.Base(path), Path: path}, i+1)
		}(i)
	}
	wg.Wait()
	manifest := app.fileCompanionGeneratedManifestBySession(sessionID)
	if len(manifest.Files) != 6 {
		t.Fatalf("manifest kept %d files, want 6", len(manifest.Files))
	}
	// No half-written leftovers next to the manifest.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

// The window reads the produced files out of the reply by these names. A
// renamed field would silently drop every card, and the two sides live in
// different languages, so the contract is pinned here.
func TestFileCompanionFilesRideOnTheReplyWireFormat(t *testing.T) {
	resp := &IMAgentResponse{
		Text:               "笔记 PDF 已经生成。",
		FileCompanionFiles: []FileCompanionFile{{Label: "复习笔记.pdf", Path: "C:/out/复习笔记.pdf", Size: 2048}},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	files, ok := decoded["file_companion_files"].([]interface{})
	if !ok || len(files) != 1 {
		t.Fatalf("files on the wire = %#v", decoded["file_companion_files"])
	}
	entry, ok := files[0].(map[string]interface{})
	if !ok {
		t.Fatalf("file entry = %#v", files[0])
	}
	if entry["label"] != "复习笔记.pdf" || entry["path"] != "C:/out/复习笔记.pdf" {
		t.Fatalf("file entry = %#v", entry)
	}
	// An ordinary reply carries no file field at all.
	plain, err := json.Marshal(&IMAgentResponse{Text: "没有产出物。"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "file_companion_files") {
		t.Fatalf("an ordinary reply carries the files field: %s", plain)
	}
	// A reply that carries a file is never cached: the path is a path on this
	// disk, and a cached answer would hand it to whoever asks next.
	if answerCacheResponseEligible(resp) {
		t.Fatal("a reply with a produced file must not be cacheable")
	}
}

// One real companion turn, end to end: the model asks for a notes PDF, the
// writer runs, and the file travels on the reply the window receives. The
// model is stood in for, but everything else is the production path.
func TestFileCompanionTurnCarriesTheProducedFileToTheWindow(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	prev := fileCompanionTurnModelHook
	fileCompanionTurnModelHook = func(msg IMUserMessage) *IMAgentResponse {
		if _, ok := app.imHandler.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":"# 复习笔记\n\n第一章的定理。"}`); !ok {
			t.Error("the writer refused the model's body")
		}
		return &IMAgentResponse{Text: "笔记 PDF 已经生成。"}
	}
	t.Cleanup(func() { fileCompanionTurnModelHook = prev })
	st := app.companionRuntime()
	st.mu.Lock()
	st.recordEvents = true
	st.mu.Unlock()
	if err := app.SendFileCompanionMessage(FileCompanionTurnRequest{Path: path, Text: "整理成笔记 PDF"}); err != nil {
		t.Fatalf("companion turn failed: %v", err)
	}
	st.mu.Lock()
	events := append([]recordedCompanionEvent(nil), st.events...)
	st.mu.Unlock()
	found := false
	for _, ev := range events {
		if ev.Name != "ai-assistant-response" || !strings.Contains(ev.Data, "file_companion_files") {
			continue
		}
		found = true
		var decoded map[string]interface{}
		if err := json.Unmarshal([]byte(ev.Data), &decoded); err != nil {
			t.Fatal(err)
		}
		files, _ := decoded["file_companion_files"].([]interface{})
		if len(files) != 1 {
			t.Fatalf("event files = %#v", decoded["file_companion_files"])
		}
		entry, _ := files[0].(map[string]interface{})
		produced, _ := entry["path"].(string)
		if produced == "" {
			t.Fatalf("event file = %#v", entry)
		}
		if _, err := os.Stat(produced); err != nil {
			t.Fatalf("the reply points at a file that is not there: %v", err)
		}
	}
	if !found {
		t.Fatalf("the reply carried no produced file: %#v", events)
	}
}

// The picture is really in the PDF, not only in the Markdown that went in.
// "/Subtype /Image" is the marker that counts: a plain "/Image" also matches
// /ProcSet [/PDF /Text /ImageB /ImageC /ImageI] and would say yes to a PDF
// that carries no picture at all.
func TestFileCompanionGeneratedPDFCarriesTheDrawing(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	dir := filepath.Join(app.GetDataDir(), "file-companion", "figures", doc.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 240, 160))
	for x := 0; x < 240; x++ {
		for y := 0; y < 160; y++ {
			picture.Set(x, y, color.RGBA{uint8(x % 255), uint8(y % 255), 120, 255})
		}
	}
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, picture); err != nil {
		t.Fatal(err)
	}
	figure := filepath.Join(dir, "pdf-01.png")
	if err := os.WriteFile(figure, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	app.seedFileCompanionTurnFigures(userID, []companionFigure{{path: figure, caption: "整体架构"}})
	h := &IMMessageHandler{app: app}
	generate := func(body string) []byte {
		app.seedFileCompanionTurnFiles(userID)
		quoted, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":`+string(quoted)+`}`); !ok {
			t.Fatalf("generate failed for %q", body)
		}
		raw, err := os.ReadFile(app.fileCompanionTurnFiles(userID)[0].Path)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	withFigure := generate("## 第一章\n\n整体架构如图1所示。\n")
	if got := bytes.Count(withFigure, []byte("/Subtype /Image")); got != 1 {
		t.Fatalf("the drawing is not in the PDF: /Subtype /Image count = %d", got)
	}
	without := generate("## 第一章\n\n没有配图。\n")
	if got := bytes.Count(without, []byte("/Subtype /Image")); got != 0 {
		t.Fatalf("a note without pictures carries %d images", got)
	}
}

// A caption comes from the document. A "]" or a newline in it would end the
// Markdown image early and leave the rest of the line as a broken link.
func TestFileCompanionGenerateAltTextKeepsTheImageLineWhole(t *testing.T) {
	alt := fileCompanionGenerateAltText("图1", "整体]架构\n第二行")
	if strings.ContainsAny(alt, "[]<>\n\r\t") {
		t.Fatalf("alt text still holds a markdown break: %q", alt)
	}
	if !strings.Contains(alt, "整体") || !strings.Contains(alt, "架构") {
		t.Fatalf("alt text lost the caption: %q", alt)
	}
	if got := fileCompanionGenerateAltText("图1", ""); got != "图1" {
		t.Fatalf("alt text without a caption = %q", got)
	}
}

// The whole chain on a real document: a PDF with an embedded drawing, the
// inventory the turn attaches, and a notes PDF that carries the drawing. The
// stub PDFs other tests use hold no picture at all, so only this one proves
// the extraction, the figure directory and the writer agree.
func TestFileCompanionNotesPDFCarriesADrawingFromTheOpenDocument(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	path := filepath.Join(t.TempDir(), "教材.pdf")
	writePaperPDFWithFigures(t, path)
	_, doc := seedCompanionTestFileAt(t, app, path)
	userID := "file-companion:" + doc.SessionID

	// The inventory a companion turn attaches: the document's own drawings,
	// extracted into the session's figure directory.
	inventory := app.fileCompanionFigures(path)
	if len(inventory) == 0 {
		t.Fatal("the document holds no drawing to carry")
	}
	for _, fig := range inventory {
		if _, err := os.Stat(fig.path); err != nil {
			t.Fatalf("the extracted drawing is missing: %v", err)
		}
	}
	app.seedFileCompanionTurnFigures(userID, inventory)

	h := &IMMessageHandler{app: app}
	body := "## 1.1 整除关系\n\n整体结构如图1所示。\n"
	quoted, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":`+string(quoted)+`}`); !ok {
		t.Fatal("the writer refused the notes")
	}
	files := app.fileCompanionTurnFiles(userID)
	if len(files) != 1 {
		t.Fatalf("turn files = %d, want 1", len(files))
	}
	raw, err := os.ReadFile(files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(raw, []byte("/Subtype /Image")); got < 1 {
		t.Fatalf("the notes PDF carries no drawing: /Subtype /Image count = %d", got)
	}
}

// A paper turn already gets its PDF from the host. The writer is not offered
// there, so one click cannot leave two PDFs behind.
func TestFileCompanionPaperTurnHasNoWriter(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	h := &IMMessageHandler{app: app}
	tools := h.fileCompanionClosedSurfaceTools("file-companion:" + doc.SessionID)
	kept := fileCompanionWithoutGenerateTool(tools)
	for _, tool := range kept {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if name == fileCompanionToolGenerate {
			t.Fatal("the paper turn still offers the writer")
		}
	}
	if len(kept) != len(tools)-1 {
		t.Fatalf("paper turn tools = %d, want %d", len(kept), len(tools)-1)
	}
	// The read tools stay: a paper interpretation may still read a page.
	if len(kept) == 0 {
		t.Fatal("the paper turn lost every tool")
	}
	// A typed question is not a paper turn.
	if fileCompanionPaperTurnText("帮我整理成笔记 PDF") {
		t.Fatal("a typed question was taken for a paper turn")
	}
	if !fileCompanionPaperTurnText(fileCompanionPaperUserTurn("D:/x.pdf")) {
		t.Fatal("the paper turn was not recognized")
	}
}

// The drawings the notes point at ride along, and they are the same ones the
// window shows under the answer.
func TestFileCompanionGenerateFiguresAppendsThePicturesTheNotesPointAt(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	dir := filepath.Join(app.GetDataDir(), "file-companion", "figures", doc.SessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	figure := filepath.Join(dir, "pdf-01.png")
	if err := os.WriteFile(figure, []byte("\x89PNG\r\n\x1a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.seedFileCompanionTurnFigures(userID, []companionFigure{{path: figure, caption: "整体架构"}})

	body := "## 第一章\n\n整体架构如图1所示。\n"
	out, added := app.fileCompanionGenerateFigures(userID, body)
	if added != 1 {
		t.Fatalf("added %d figures, want 1", added)
	}
	if !strings.Contains(out, "## 原文配图") || !strings.Contains(out, "整体架构") {
		t.Fatalf("figures not appended:\n%s", out)
	}
	if !strings.Contains(out, filepath.ToSlash(figure)) {
		t.Fatalf("the picture path is missing:\n%s", out)
	}
	// A note that points at no picture stays as it is.
	plain, added := app.fileCompanionGenerateFigures(userID, "## 第一章\n\n没有配图。\n")
	if added != 0 || plain != "## 第一章\n\n没有配图。\n" {
		t.Fatalf("plain note = %q added = %d", plain, added)
	}
	// A picture that is gone from disk is skipped rather than linked.
	app.seedFileCompanionTurnFigures(userID, []companionFigure{{path: filepath.Join(dir, "gone.png"), caption: "没了"}})
	out, added = app.fileCompanionGenerateFigures(userID, body)
	if added != 0 || strings.Contains(out, "原文配图") {
		t.Fatalf("a missing picture was linked: added=%d\n%s", added, out)
	}
}

// An unclosed code fence would swallow the pictures.
func TestFileCompanionMarkdownOpenFence(t *testing.T) {
	if fileCompanionMarkdownOpenFence("正文\n\n```go\ncode()\n") != true {
		t.Fatal("an unclosed fence was not detected")
	}
	if fileCompanionMarkdownOpenFence("正文\n\n```go\ncode()\n```\n") != false {
		t.Fatal("a closed fence was reported as open")
	}
	if fileCompanionMarkdownOpenFence("正文\n") != false {
		t.Fatal("plain text was reported as an open fence")
	}
}

// The produced file travels on the reply, and a reopened window puts it back
// under the answer it belongs to.
func TestFileCompanionGeneratedFilesRideOnTheReply(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.seedFileCompanionPaths(nil)
	_, doc := seedCompanionTestFile(t, app, "report.pdf", "%PDF-1.4\n")
	userID := "file-companion:" + doc.SessionID
	mem := agent.NewConversationMemory()
	t.Cleanup(mem.Stop)
	app.imHandler = &IMMessageHandler{app: app}
	app.imHandler.memory = mem
	// One answer already answered, so the file belongs to the second one.
	mem.Append(userID, agent.ConversationEntry{Role: "user", Content: "第一个问题"})
	mem.Append(userID, agent.ConversationEntry{Role: "assistant", Content: "第一个回答"})
	h := &IMMessageHandler{app: app}
	text, ok := h.executeFileCompanionHostTool(userID, fileCompanionToolGenerate, `{"markdown":"# 笔记\n"}`)
	if !ok {
		t.Fatalf("generate = %q ok=%v", text, ok)
	}
	files := app.fileCompanionTurnFiles(userID)
	if len(files) != 1 {
		t.Fatalf("turn files = %d, want 1", len(files))
	}
	if got := app.fileCompanionAnswerOrdinal(userID); got != 2 {
		t.Fatalf("answer ordinal = %d, want 2", got)
	}
	messages := []FileCompanionMessage{
		{Role: "assistant", Text: "第一个回答"},
		{Role: "assistant", Text: "第二个回答"},
	}
	restored := app.fileCompanionAttachGeneratedFiles(doc.SessionID, messages)
	if len(restored[0].Files) != 0 {
		t.Fatalf("the file landed on the wrong answer: %+v", restored[0].Files)
	}
	if len(restored[1].Files) != 1 || restored[1].Files[0].Path != files[0].Path {
		t.Fatalf("restored files = %+v, want %s", restored[1].Files, files[0].Path)
	}
	// A file that is gone from disk is not offered.
	if err := os.Remove(files[0].Path); err != nil {
		t.Fatal(err)
	}
	gone := app.fileCompanionAttachGeneratedFiles(doc.SessionID, []FileCompanionMessage{{Role: "assistant", Text: "第二个回答"}})
	if len(gone[0].Files) != 0 {
		t.Fatalf("a missing file was offered: %+v", gone[0].Files)
	}
}
