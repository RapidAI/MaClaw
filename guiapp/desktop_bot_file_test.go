package guiapp

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

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
