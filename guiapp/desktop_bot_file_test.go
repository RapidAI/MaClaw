package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestSaveDesktopBotDeliveredFileWritesOnce(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	botFilesMu.Lock()
	botFilesCached = nil
	botFilesMu.Unlock()
	t.Cleanup(func() {
		botFilesMu.Lock()
		botFilesCached = nil
		botFilesMu.Unlock()
		corelib.SetMaclawBaseDir(prev)
	})
	app := &App{}
	data := base64.StdEncoding.EncodeToString([]byte("word-bytes"))
	if _, err := app.SaveDesktopBotDeliveredFile("bot_1", "../secret.pdf", "application/pdf", data); err == nil {
		t.Fatal("path name was saved")
	}
	first, err := app.SaveDesktopBotDeliveredFile("bot_1", "北京天气.pdf", "application/pdf", data)
	if err != nil || filepath.Base(first) != "北京天气.pdf" {
		t.Fatalf("path=%q err=%v", first, err)
	}
	raw, err := os.ReadFile(first)
	if err != nil || string(raw) != "word-bytes" {
		t.Fatalf("body=%q err=%v", raw, err)
	}
	again, err := app.SaveDesktopBotDeliveredFile("bot_1", "北京天气.pdf", "application/pdf", data)
	if err != nil || again != first {
		t.Fatalf("again=%q first=%q err=%v", again, first, err)
	}
	// The index is gone, but the bytes at the original name are the same file.
	if err := os.Remove(botFilesIndexPath()); err != nil {
		t.Fatal(err)
	}
	botFilesMu.Lock()
	botFilesCached = nil
	botFilesMu.Unlock()
	kept, err := app.SaveDesktopBotDeliveredFile("bot_1", "北京天气.pdf", "application/pdf", data)
	if err != nil || kept != first {
		t.Fatalf("kept=%q first=%q err=%v", kept, first, err)
	}
	// Same length, different bytes: the file already there stays, and the
	// delivery is written beside it.
	if err := os.WriteFile(first, []byte("word-byteX"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(botFilesIndexPath()); err != nil {
		t.Fatal(err)
	}
	botFilesMu.Lock()
	botFilesCached = nil
	botFilesMu.Unlock()
	stamped, err := app.SaveDesktopBotDeliveredFile("bot_1", "北京天气.pdf", "application/pdf", data)
	if err != nil || stamped == first || filepath.Base(stamped) == "北京天气.pdf" {
		t.Fatalf("stamped=%q first=%q err=%v", stamped, first, err)
	}
	left, err := os.ReadFile(first)
	if err != nil || string(left) != "word-byteX" {
		t.Fatalf("canonical=%q err=%v", left, err)
	}
}

func TestDesktopBotArtifactSizes(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "北京天气.pdf")
	if err := os.WriteFile(pdf, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := (&App{}).DesktopBotArtifactSizes([]string{pdf, filepath.Join(dir, "missing.pdf"), filepath.Join(dir, "subdir"), ""})
	if len(got) != 4 || got[0] != 5 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Fatalf("sizes=%v", got)
	}
	// A blank in front must not shift the file's size onto another row.
	shifted := make([]string, 201)
	shifted[200] = pdf
	got = (&App{}).DesktopBotArtifactSizes(shifted)
	if len(got) != 201 || got[200] != 5 || got[0] != 0 {
		t.Fatalf("shifted len=%d first=%d last=%d", len(got), got[0], got[200])
	}
	// Past the stat cap the slot stays -1, so the table can ask again later
	// instead of treating that file as missing.
	capped := make([]string, 501)
	got = (&App{}).DesktopBotArtifactSizes(capped)
	if len(got) != 501 || got[0] != 0 || got[500] != -1 {
		t.Fatalf("capped len=%d first=%d tail=%d", len(got), got[0], got[500])
	}
}

func TestCleanDesktopBotFilesKeepsAnyType(t *testing.T) {
	data := "d29yZA=="
	got := cleanDesktopBotFiles([]DesktopBotFile{
		{Name: "../secret.pdf", MIME: "application/pdf", Data: data},
		{Name: "北京天气.pdf", MIME: "application/pdf", Data: data},
	})
	if len(got) != 1 || got[0].Name != "北京天气.pdf" || got[0].MIME != "application/pdf" || got[0].Data != data {
		t.Fatalf("pdf=%#v", got)
	}
	got = cleanDesktopBotFiles([]DesktopBotFile{{Name: "Makefile", MIME: "", Data: data}})
	if len(got) != 1 || got[0].Name != "Makefile" || got[0].MIME != "application/octet-stream" || got[0].Data != data {
		t.Fatalf("makefile=%#v", got)
	}
	if got := cleanDesktopBotFiles([]DesktopBotFile{{Name: "note.pdf", MIME: "text/html\r\nX", Data: data}}); got != nil {
		t.Fatalf("hostile mime kept: %#v", got)
	}
}

func TestCleanDesktopBotFilesKeepsEveryDocument(t *testing.T) {
	data := "d29yZA=="
	got := cleanDesktopBotFiles([]DesktopBotFile{
		{Name: "snake.cpp", MIME: "text/plain", Data: data},
		{Name: "Makefile", MIME: "", Data: data},
	})
	if len(got) != 2 || got[0].Name != "snake.cpp" || got[1].Name != "Makefile" {
		t.Fatalf("files=%#v", got)
	}
	many := make([]DesktopBotFile, 0, 10)
	for i := 0; i < 10; i++ {
		many = append(many, DesktopBotFile{Name: "f" + strconv.Itoa(i) + ".txt", MIME: "text/plain", Data: data})
	}
	got = cleanDesktopBotFiles(many)
	if len(got) != 8 || got[0].Name != "f2.txt" || got[7].Name != "f9.txt" {
		t.Fatalf("cap=%#v", got)
	}
}

func TestLocalizeDesktopPathsRewritesTheCloudPath(t *testing.T) {
	local := []string{`C:\Users\me\.maclaw\data\bot-files\bot_1\snake.cpp`}
	got := localizeDesktopPaths("源码在 /home/desktop/work/bot_1/cc/snake.cpp，同目录可玩。", local)
	if strings.Contains(got, "/home/desktop") || !strings.Contains(got, local[0]) || !strings.Contains(got, "，同目录可玩。") {
		t.Fatalf("sentence=%q", got)
	}
	if localizeDesktopPaths("没有路径", local) != "没有路径" {
		t.Fatal("plain sentence changed")
	}
}

func TestReadDesktopBotBodyStopsWhenTheDeadlinePasses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	start := time.Now()
	_, err := readDesktopBotBody(ctx, pr, 1<<20)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("blocked for %s", time.Since(start))
	}
	if err == nil || !strings.Contains(err.Error(), "没有在时限内") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadDesktopBotBodyReturnsTheReply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := readDesktopBotBody(ctx, io.NopCloser(strings.NewReader(`{"ok":true}`)), 100)
	if err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestReadDesktopBotBodyRejectsABodyPastTheLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := readDesktopBotBody(ctx, io.NopCloser(strings.NewReader(`{"data":"0123456789"}`)), 8)
	if err == nil || !strings.Contains(err.Error(), "超出了可读取的大小") {
		t.Fatalf("err=%v", err)
	}
}

func TestInlineMailFileSavesPastTheChatAttachmentCap(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })
	raw := bytes.Repeat([]byte("x"), 600_000)
	encoded := base64.StdEncoding.EncodeToString(raw)
	if len(encoded) <= desktop.FileDeliverBase64Max || desktopBotImageDataSized(encoded, desktop.FileDeliverBase64Max) {
		t.Fatal("fixture still fits the chat attachment cap")
	}
	long := strings.Repeat("报", 90) + ".pdf"
	if desktopBotDeliverName(long) {
		t.Fatal("chat name check accepted a hub-length name")
	}
	path, err := saveInlineMailFile("note_498071131fe0d1f2", long, raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("read err=%v", err)
	}
	again, err := saveInlineMailFile("note_498071131fe0d1f2", long, raw)
	if err != nil || again != path {
		t.Fatalf("again=%q err=%v", again, err)
	}
	replaced, err := saveInlineMailFile("note_498071131fe0d1f2", long, []byte("next"))
	if err != nil || replaced != path {
		t.Fatalf("replaced=%q err=%v", replaced, err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "next" {
		t.Fatalf("replaced body=%q err=%v", got, err)
	}
	if _, err := saveInlineMailFile("note_498071131fe0d1f2", "../secret.pdf", raw[:16]); err == nil {
		t.Fatal("path name was saved")
	}
}

func TestMailFileSavedCopyOpensTheFileHubAlreadyDelivered(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	botFilesMu.Lock()
	botFilesCached = nil
	botFilesMu.Unlock()
	t.Cleanup(func() {
		botFilesMu.Lock()
		botFilesCached = nil
		botFilesMu.Unlock()
		corelib.SetMaclawBaseDir(prev)
	})
	dir := filepath.Join(base, "Bot 4")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	weather := filepath.Join(dir, "北京天气报告-20261115-0813.pdf")
	if err := os.WriteFile(weather, []byte("%PDF-1.6"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.pdf")
	if err := os.WriteFile(other, []byte("%PDF-other"), 0o644); err != nil {
		t.Fatal(err)
	}
	rememberNewDelivery("bot_68952d5d158dae10", "北京天气报告-20261115-0813.pdf", weather)
	rememberNewDelivery("bot_other", "other.pdf", other)
	saved, ok := mailFileSavedCopy("note_498071131fe0d1f2", "北京天气报告-20261115-0813.pdf")
	if !ok || saved.LocalPath != weather || saved.Data != "" {
		t.Fatalf("saved=%#v ok=%v", saved, ok)
	}
	if _, ok := mailFileSavedCopy("note_498071131fe0d1f2", "missing.pdf"); ok {
		t.Fatal("missing file was opened")
	}
	if got := deliveredFileForOpen("bot_other", "北京天气报告-20261115-0813.pdf"); got != "" {
		t.Fatalf("other bot opened %q", got)
	}
	older := filepath.Join(dir, "北京天气-older.pdf")
	newer := filepath.Join(dir, "北京天气-newer.pdf")
	if err := os.WriteFile(older, []byte("%PDF-old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("%PDF-new"), 0o644); err != nil {
		t.Fatal(err)
	}
	rememberMailNote("note_old", "北京天气.pdf", older)
	rememberMailNote("note_new", "北京天气.pdf", newer)
	rememberNewDelivery("bot_68952d5d158dae10", "北京天气.pdf", newer)
	oldSaved, ok := mailFileSavedCopy("note_old", "北京天气.pdf")
	if !ok || oldSaved.LocalPath != older {
		t.Fatalf("older=%#v ok=%v", oldSaved, ok)
	}
	newSaved, ok := mailFileSavedCopy("note_new", "北京天气.pdf")
	if !ok || newSaved.LocalPath != newer {
		t.Fatalf("newer=%#v ok=%v", newSaved, ok)
	}
}
