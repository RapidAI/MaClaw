package guiapp

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileCompanionProjectRoundTripKeepsThePDFHighlightsAndChat(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	source := filepath.Join(dir, "论文.pdf")
	payload := []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n")
	if err := os.WriteFile(source, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	before := hashFile(t, source)
	opened, err := app.FileCompanionOpen(source)
	if err != nil || opened.Error != "" {
		t.Fatalf("open: %v %s", err, opened.Error)
	}
	writeCompanionSession(t, app, opened.SessionID, []map[string]any{
		{"role": "user", "content": "这段在说什么？"},
		{"role": "assistant", "content": "这是摘要。", "reasoning_content": "先看标题"},
	})
	highlights := `[{"id":"m1","page":1,"color":"pink","rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.05}],"text":"方法"}]`
	chatMarks := `[{"message":1,"color":"green","start":0,"end":2,"text":"摘要"}]`
	annotations := `[{"id":"a1","kind":"annotation","text":"方法","translation":"这里要复核","parts":[{"page":1,"rects":[{"x":0.1,"y":0.2,"w":0.3,"h":0.05}]}]}]`
	drawings := `[{"id":"d1","page":1,"color":"blue","tool":"line","points":[{"x":0.1,"y":0.2},{"x":0.3,"y":0.4}]}]`
	exportPayload := `{"highlights":` + highlights + `,"chatMarks":` + chatMarks + `,"annotations":` + annotations + `,"drawings":` + drawings + `}`

	previous := fileCompanionSaveProjectDialog
	defer func() { fileCompanionSaveProjectDialog = previous }()
	dest := filepath.Join(dir, "out.zip")
	fileCompanionSaveProjectDialog = func(_ *App, defaultFilename string) (string, error) {
		if defaultFilename == "" || defaultFilename == "document-伴读项目.zip" {
			t.Fatalf("default name: %q", defaultFilename)
		}
		return dest, nil
	}
	written, err := app.FileCompanionExportProject(source, exportPayload)
	if err != nil {
		t.Fatal(err)
	}
	if written != dest {
		t.Fatalf("dest: %s", written)
	}
	if got := hashFile(t, source); got != before {
		t.Fatal("export changed the open PDF")
	}
	if got := readFile(t, source); string(got) != string(payload) {
		t.Fatal("export rewrote the PDF bytes")
	}

	manifest, document := readProjectZip(t, dest)
	if manifest.Kind != fileCompanionProjectKind || manifest.Version != 1 {
		t.Fatalf("manifest: %+v", manifest)
	}
	if string(document) != string(payload) {
		t.Fatal("zip document is not the original PDF")
	}
	if len(manifest.Highlights) == 0 || manifest.Highlights[0].Color != "pink" {
		t.Fatalf("highlights: %+v", manifest.Highlights)
	}
	if len(manifest.Messages) != 2 || manifest.Messages[1].Text != "这是摘要。" || manifest.Messages[1].Reasoning != "先看标题" {
		t.Fatalf("messages: %+v", manifest.Messages)
	}

	imported, err := app.FileCompanionOpen(dest)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Error != "" || !imported.Imported {
		t.Fatalf("import: %+v", imported)
	}
	if imported.Path == "" || imported.Path == dest {
		t.Fatalf("imported path: %s", imported.Path)
	}
	if string(readFile(t, imported.Path)) != string(payload) {
		t.Fatal("imported PDF bytes differ")
	}
	if len(imported.Messages) != 2 || imported.Messages[0].Text != "这段在说什么？" || imported.Messages[1].Text != "这是摘要。" || imported.Messages[1].Reasoning != "先看标题" {
		t.Fatalf("restored chat: %+v", imported.Messages)
	}
	var marks []fileCompanionProjectMark
	if err := json.Unmarshal(imported.Highlights, &marks); err != nil || len(marks) != 1 || marks[0].Color != "pink" {
		t.Fatalf("restored highlights: %s %v", imported.Highlights, err)
	}
	if len(imported.ChatMarks) == 0 || len(imported.Annotations) == 0 || len(imported.Drawings) == 0 {
		t.Fatalf("restored marks: chat=%s annotations=%s drawings=%s", imported.ChatMarks, imported.Annotations, imported.Drawings)
	}
	var restoredDrawings []fileCompanionDrawing
	if err := json.Unmarshal(imported.Drawings, &restoredDrawings); err != nil || len(restoredDrawings) != 1 || restoredDrawings[0].Tool != "line" {
		t.Fatalf("restored drawings: %s %v", imported.Drawings, err)
	}
}

func TestFileCompanionProjectPaperCardComesBack(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	source := filepath.Join(dir, "paper.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1.4 paper"), 0o644); err != nil {
		t.Fatal(err)
	}
	opened, err := app.FileCompanionOpen(source)
	if err != nil || opened.Error != "" {
		t.Fatalf("open: %v %s", err, opened.Error)
	}
	writeCompanionSession(t, app, opened.SessionID, []map[string]any{
		{"role": "user", "content": "论文解读\n" + fileCompanionPaperMarker},
		{"role": "assistant", "content": "方法原理写在这里"},
	})
	resultDir := filepath.Join(app.GetDataDir(), "file-companion", "results", opened.SessionID)
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	resultPDF := filepath.Join(resultDir, "论文解读.pdf")
	resultBody := []byte("%PDF-1.4 result")
	if err := os.WriteFile(resultPDF, resultBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resultDir, "latest.txt"), []byte(resultPDF), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resultDir, "latest-turn.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	previous := fileCompanionSaveProjectDialog
	defer func() { fileCompanionSaveProjectDialog = previous }()
	dest := filepath.Join(dir, "paper.zip")
	fileCompanionSaveProjectDialog = func(_ *App, _ string) (string, error) { return dest, nil }
	if _, err := app.FileCompanionExportProject(source, "[]"); err != nil {
		t.Fatal(err)
	}
	imported, err := app.FileCompanionOpen(dest)
	if err != nil || imported.Error != "" {
		t.Fatalf("import: %v %+v", err, imported)
	}
	if len(imported.Messages) < 2 || imported.Messages[1].Text != "已生成论文解读" || imported.Messages[1].ResultPath == "" {
		t.Fatalf("paper card: %+v", imported.Messages)
	}
	if string(readFile(t, imported.Messages[1].ResultPath)) != string(resultBody) {
		t.Fatal("paper PDF was not packed")
	}
	if imported.Messages[1].ResultPath == resultPDF {
		t.Fatal("imported card still points at the original result")
	}
}

func TestFileCompanionProjectRejectsAPlainZip(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	path := filepath.Join(t.TempDir(), "notes.zip")
	writePlainZip(t, path, "hello.txt", []byte("hello"))
	opened, err := app.FileCompanionOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Error != "不是伴读项目" {
		t.Fatalf("error: %q", opened.Error)
	}
}

func TestFileCompanionProjectCancelWritesNothing(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	source := filepath.Join(dir, "notes.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	previous := fileCompanionSaveProjectDialog
	defer func() { fileCompanionSaveProjectDialog = previous }()
	fileCompanionSaveProjectDialog = func(_ *App, _ string) (string, error) { return "", nil }
	dest, err := app.FileCompanionExportProject(source, "[]")
	if err != nil || dest != "" {
		t.Fatalf("cancel: %q %v", dest, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir grew: %d", len(entries))
	}
}

func TestFileCompanionProjectRefusesTheOpenFileAndUngrantedPaths(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	app.ctx = ctx
	app.seedFileCompanionPaths(nil)

	dir := t.TempDir()
	source := filepath.Join(dir, "notes.pdf")
	if err := os.WriteFile(source, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.FileCompanionOpen(source); err != nil {
		t.Fatal(err)
	}
	previous := fileCompanionSaveProjectDialog
	defer func() { fileCompanionSaveProjectDialog = previous }()
	fileCompanionSaveProjectDialog = func(_ *App, _ string) (string, error) { return source, nil }
	if _, err := app.FileCompanionExportProject(source, "[]"); err == nil || err.Error() != "不能覆盖当前文件" {
		t.Fatalf("overwrite: %v", err)
	}
	if _, err := app.FileCompanionExportProject(filepath.Join(dir, "other.pdf"), "[]"); err == nil {
		t.Fatal("unopened file exported")
	}
	if _, err := app.FileCompanionExportProject(source, "{"); err == nil {
		t.Fatal("bad highlights exported")
	}
}

func writeCompanionSession(t *testing.T, app *App, sessionID string, entries []map[string]any) {
	t.Helper()
	dir := filepath.Join(app.GetDataDir(), "file-companion", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	snapshot := map[string]any{
		"sessions": map[string]any{
			"file-companion:" + sessionID: map[string]any{
				"entries":     entries,
				"last_access": time.Now().UTC(),
			},
		},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func hashFile(t *testing.T, path string) [32]byte {
	t.Helper()
	raw := readFile(t, path)
	return sha256.Sum256(raw)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type projectZipManifest struct {
	Kind       string                     `json:"kind"`
	Version    int                        `json:"version"`
	Highlights []fileCompanionProjectMark `json:"highlights"`
	Messages   []FileCompanionMessage     `json:"messages"`
}

func readProjectZip(t *testing.T, path string) (projectZipManifest, []byte) {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var manifest projectZipManifest
	var document []byte
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if name == fileCompanionProjectManifestName {
			if err := json.Unmarshal(raw, &manifest); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if strings.HasPrefix(name, "document/") {
			document = raw
		}
	}
	if document == nil {
		t.Fatal("zip has no document")
	}
	return manifest, document
}

func writePlainZip(t *testing.T, path, name string, body []byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}
