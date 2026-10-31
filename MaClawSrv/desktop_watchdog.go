package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// Independent run watchdog (Super-agent prototype: a supervisor outside the
// worker that sees every step). The per-step guards each refuse one kind of
// bad call; the watchdog looks at the run as a whole and intervenes when it
// is looping, keeps failing, keeps being refused, has stalled, or has run
// past its budget: first a warning in the tool result, then it ends the run
// at the next step boundary with an honest first-person result, so a stuck
// run never ends silently or loops until the hard limit.

const (
	watchdogLoopWarn    = 4 // same call this many times in the window: warn
	watchdogLoopStop    = 6 // ... and stop
	watchdogLoopWindow  = 12
	watchdogErrStop     = 8 // consecutive failed steps
	watchdogRefuseStop  = 10
	watchdogMaxSteps    = 160
	watchdogMaxWall     = 60 * time.Minute
	watchdogStallAfter  = 15 * time.Minute
	watchdogSweepPeriod = time.Minute
)

type watchdogRun struct {
	started  time.Time
	lastStep time.Time
	steps    int
	sigs     []string
	errRun   int
	refused  int
	warned   map[string]bool
	stop     string // why the run is to end ("" = keep going)
}

var (
	watchdogMu    sync.Mutex
	watchdogRuns  = map[string]*watchdogRun{}
	watchdogSweep sync.Once
)

func watchdogSig(action string, args map[string]any) string {
	clean := map[string]any{}
	for k, v := range args {
		if k == "reason" || k == "note" {
			continue
		}
		clean[k] = v
	}
	raw, _ := json.Marshal(clean) // map keys are sorted
	sum := sha1.Sum(append([]byte(action+"|"), raw...))
	return hex.EncodeToString(sum[:8])
}

func watchdogGet(runID string) *watchdogRun {
	w, ok := watchdogRuns[runID]
	if !ok {
		now := time.Now()
		w = &watchdogRun{started: now, lastStep: now, warned: map[string]bool{}}
		watchdogRuns[runID] = w
	}
	return w
}

// desktopWatchdogBefore sees a step before it runs. It returns a warning to
// add to that step's result, or an error that refuses it.
func desktopWatchdogBefore(scope agentruntime.Scope, action string, args map[string]any) (string, error) {
	if scope.RunID == "" {
		return "", nil
	}
	watchdogSweep.Do(func() { go desktopWatchdogLoop() })
	sig := watchdogSig(action, args)
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w := watchdogGet(scope.RunID)
	w.steps++
	w.lastStep = time.Now()
	w.sigs = append(w.sigs, sig)
	if len(w.sigs) > watchdogLoopWindow {
		w.sigs = w.sigs[len(w.sigs)-watchdogLoopWindow:]
	}
	same := 0
	for _, s := range w.sigs {
		if s == sig {
			same++
		}
	}
	switch {
	case same >= watchdogLoopStop:
		w.stop = "loop"
	case w.steps >= watchdogMaxSteps:
		w.stop = "steps"
	case time.Since(w.started) >= watchdogMaxWall:
		w.stop = "wall"
	}
	if w.stop != "" {
		log.Printf("[watchdog] run=%s stop=%s steps=%d", scope.RunID, w.stop, w.steps)
		return "", errWatchdogStop
	}
	if same >= watchdogLoopWarn && !w.warned["loop:"+sig] {
		w.warned["loop:"+sig] = true
		return "\nWATCHDOG: this exact " + action + " call has now run " + itoa(same) + " times in this run without moving the task on. Do not repeat it: use a different method, or end the turn and say plainly what is blocking.", nil
	}
	return "", nil
}

var errWatchdogStop = watchdogError("refused: watchdog_stop — this run is being ended by the watchdog (looping, repeated failures or over its budget). Do not call any more tools; give your final result now in one or two short first-person sentences: what is done, what is not, and what you need.")

type watchdogError string

func (e watchdogError) Error() string { return string(e) }

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// desktopWatchdogAfter counts how the step ended.
func desktopWatchdogAfter(scope agentruntime.Scope, err error) {
	if scope.RunID == "" {
		return
	}
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w := watchdogGet(scope.RunID)
	if err == nil {
		w.errRun = 0
		return
	}
	if err == errWatchdogStop {
		return
	}
	w.errRun++
	if strings.HasPrefix(strings.TrimSpace(err.Error()), "refused") {
		w.refused++
	}
	if w.errRun >= watchdogErrStop {
		w.stop = "errors"
	} else if w.refused >= watchdogRefuseStop {
		w.stop = "refused"
	}
}

// desktopWatchdogVerdict: the run must end now, with this result.
func desktopWatchdogVerdict(runID string) (string, bool) {
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w, ok := watchdogRuns[runID]
	if !ok || w.stop == "" {
		return "", false
	}
	return desktopWatchdogReply(w.stop), true
}

// desktopWatchdogReply is the bot's own short result for a run it stops.
func desktopWatchdogReply(why string) string {
	switch why {
	case "loop":
		return "这件我卡住了：同一步试了好几次都没推进，我先停在当前页面，没有重复提交任何东西。你看一眼或者换个说法告诉我，我接着做。"
	case "errors", "refused":
		return "这件我没做成：接连几步都失败了，我先停下，没有重复提交任何东西。你看一下桌面，告诉我怎么继续。"
	case "stalled":
		return "这件我停住了：很久没有进展，我先收手，页面保持原样。你说一声我接着做。"
	}
	return "这件花的时间太长了，我先停在当前进度，没有重复提交任何东西。要继续的话跟我说一声。"
}

func desktopWatchdogEnd(runID string) {
	watchdogMu.Lock()
	delete(watchdogRuns, runID)
	watchdogMu.Unlock()
}

// desktopWatchdogLoop flags runs that took no step for a long time (a hung
// model call or tool). The flag ends them at their next step boundary.
func desktopWatchdogLoop() {
	t := time.NewTicker(watchdogSweepPeriod)
	defer t.Stop()
	for now := range t.C {
		watchdogMu.Lock()
		for id, w := range watchdogRuns {
			if now.Sub(w.lastStep) > 6*time.Hour {
				delete(watchdogRuns, id)
				continue
			}
			if w.stop == "" && now.Sub(w.lastStep) > watchdogStallAfter {
				w.stop = "stalled"
				log.Printf("[watchdog] run=%s stalled %s without a step", id, now.Sub(w.lastStep).Round(time.Second))
			}
		}
		watchdogMu.Unlock()
	}
}

// desktopEnsureReceipt makes every finished desktop run end with exactly one
// result message to the person, on success and on failure: a run that ended
// without text gets one from its publish record, the task ledger or its
// error. A cancelled run (the person said stop) is answered by the stop
// itself, and a hand-off has its own message.
func desktopEnsureReceipt(scope agentruntime.Scope, run *agentservice.Run, msg *agentservice.Message, runErr error, handoff bool) *agentservice.Message {
	if run == nil {
		return msg
	}
	defer desktopWatchdogEnd(run.ID)
	key := desktopLedgerKey(scope)
	status := string(run.Status)
	if msg != nil && strings.TrimSpace(msg.Content) != "" {
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "run_end", Run: run.ID, Detail: status})
		return msg
	}
	if handoff || run.Status == agentservice.RunStatusCancelled {
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "run_end", Run: run.ID, Detail: status + " no_receipt_needed"})
		return msg
	}
	text := ""
	if r, ok := desktopPublishRecordOf(run.ID); ok {
		text = desktopReplyTemplate(r)
	}
	if text == "" {
		if e, ok := desktopLedgerRunPost(desktopLedgerEntries(key), run.ID); ok {
			text = "已经发出去了"
			if e.Title != "" {
				text += "《" + e.Title + "》"
			}
			if e.URL != "" {
				text += "\n链接：" + e.URL
			}
		}
	}
	if text == "" {
		if v, ok := desktopWatchdogVerdict(run.ID); ok {
			text = v
		}
	}
	if text == "" {
		if runErr != nil || run.Status == agentservice.RunStatusFailed {
			text = "这件我没做成，中途出了问题，我停在当前页面，没有重复提交任何东西。你说一声我再试一次。"
		} else {
			text = "这件做完了，桌面上就是结果，你看一眼。"
		}
	}
	if msg == nil {
		msg = &agentservice.Message{SessionID: run.SessionID, InstanceID: run.InstanceID, Role: agentservice.MessageRoleAssistant, CreatedAt: time.Now()}
	}
	msg.Content = text
	if msg.Metadata == nil {
		msg.Metadata = map[string]string{}
	}
	msg.Metadata["receipt"] = "srv"
	desktopLedgerAdd(key, desktopLedgerEntry{Kind: "run_end", Run: run.ID, Detail: status + " receipt_by_srv"})
	log.Printf("[watchdog] run=%s ended without a reply; srv receipt sent (status=%s)", run.ID, status)
	return msg
}

func desktopLedgerRunPost(entries []desktopLedgerEntry, runID string) (desktopLedgerEntry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "posted" && entries[i].Run == runID {
			return entries[i], true
		}
	}
	return desktopLedgerEntry{}, false
}
