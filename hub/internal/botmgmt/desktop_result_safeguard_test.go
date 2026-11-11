package botmgmt

import (
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
