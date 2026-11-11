package botmgmt

import (
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
