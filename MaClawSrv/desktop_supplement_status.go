package main

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// A message the person sends while a run is going is folded into that run
// as a supplement. The run takes it in at its next step; until now nothing
// on screen said so, and the person could not tell whether their words had
// been used (Daniel 11-12, rule 3: never silent). The run's status line now
// shows the moment it was taken in, for a while after.

// desktopSupplementShownFor is how long the status line keeps the note.
const desktopSupplementShownFor = 10 * time.Minute

type desktopSupplementNote struct {
	at    time.Time
	text  string
	count int
}

var (
	desktopSupplementMu    sync.Mutex
	desktopSupplementNotes = map[string]desktopSupplementNote{}
	desktopSupplementNow   = time.Now
)

func init() {
	agentservice.SetRunSupplementAppliedObserver(noteDesktopSupplementApplied)
}

// noteDesktopSupplementApplied records supplements a run just took in.
func noteDesktopSupplementApplied(_ string, runID string, applied []agentservice.RunSupplement) {
	runID = strings.TrimSpace(runID)
	if runID == "" || len(applied) == 0 {
		return
	}
	last := strings.TrimSpace(applied[len(applied)-1].Content)
	now := desktopSupplementNow()
	desktopSupplementMu.Lock()
	defer desktopSupplementMu.Unlock()
	note := desktopSupplementNotes[runID]
	note.at = now
	note.text = last
	note.count += len(applied)
	desktopSupplementNotes[runID] = note
	for id, n := range desktopSupplementNotes {
		if now.Sub(n.at) > 2*desktopSupplementShownFor {
			delete(desktopSupplementNotes, id)
		}
	}
}

// desktopSupplementStatusPhrase is the status-line part for a run that took
// in a supplement recently, or "".
func desktopSupplementStatusPhrase(runID string) string {
	desktopSupplementMu.Lock()
	note, ok := desktopSupplementNotes[strings.TrimSpace(runID)]
	desktopSupplementMu.Unlock()
	if !ok || desktopSupplementNow().Sub(note.at) > desktopSupplementShownFor {
		return ""
	}
	text := strings.TrimPrefix(note.text, "【改变做法】")
	text = strings.Join(strings.Fields(text), " ")
	head := "已按你的补充调整"
	if strings.HasPrefix(note.text, "【改变做法】") {
		head = "已按你的新要求改做法"
	}
	if note.count > 1 {
		head += "（共 " + strconv.Itoa(note.count) + " 条）"
	}
	return note.at.Format("15:04") + " " + head + "：" + desktopRuneHead(text, 30)
}
