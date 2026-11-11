package botmgmt

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestDesktopResultUnfetchedOnlyWhileNotAcked(t *testing.T) {
	s := &Service{desktopAdmits: map[string]*desktopAdmitState{}}
	key := desktopAdmitKey("t", "u", "bot_1", "run_1")
	s.desktopAdmits[key] = &desktopAdmitState{done: true, finished: time.Now()}
	if !s.desktopResultUnfetched("t", "u", "bot_1", "run_1") {
		t.Fatal("finished and not acked is unfetched")
	}
	s.desktopAdmits[key].delivered = true
	if s.desktopResultUnfetched("t", "u", "bot_1", "run_1") {
		t.Fatal("acked result is fetched")
	}
	delete(s.desktopAdmits, key)
	if s.desktopResultUnfetched("t", "u", "bot_1", "run_1") {
		t.Fatal("removed admit is fetched")
	}
	// A quiet finish (no sentence) never starts the wait.
	s.sayUnfetchedResultLater("t", "u", "bot_1", "run_2", Reply{Text: "prose only"})
}

func TestUnfetchedAttachmentNoteNamesFiles(t *testing.T) {
	note, has := unfetchedAttachmentNote(Reply{Files: []ReplyFile{{Name: "snake.html"}}, Images: []ReplyImage{{MIME: "image/png"}}})
	if !has || !strings.Contains(note, "snake.html") || !strings.Contains(note, "1 张图片") {
		t.Fatalf("note %q has %v", note, has)
	}
	if n, h := unfetchedAttachmentNote(Reply{Text: "x"}); n != "" || h {
		t.Fatalf("text-only %q %v", n, h)
	}
}

// papers 11-11 22:12: the said copy of Bot 2's snake result lost its files.
// Now the files and pictures ride on the said note itself.
func TestUnfetchedResultFilesRideOnTheSaidNote(t *testing.T) {
	s := &Service{MailFileDir: t.TempDir()}
	reply := Reply{
		Files:  []ReplyFile{{Name: "snake.html", MIME: "text/html", Data: base64.StdEncoding.EncodeToString([]byte("<html>"))}},
		Images: []ReplyImage{{MIME: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("png"))}},
	}
	files, ok := s.storeReplyFiles("t", reply)
	if !ok || len(files) != 2 || files[0].Name != "snake.html" || files[1].Name != "截图1.png" {
		t.Fatalf("files = %+v %v", files, ok)
	}
	bad := Reply{Files: []ReplyFile{{Name: "x", Data: "%%%"}}}
	if _, ok := s.storeReplyFiles("t", bad); ok {
		t.Fatal("undecodable file counted as stored")
	}
	if f, ok := s.storeReplyFiles("t", Reply{}); !ok || f != nil {
		t.Fatal("no files")
	}
}
