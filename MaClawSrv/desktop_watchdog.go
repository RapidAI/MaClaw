package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
	"regexp"
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
	// graced: the model already had its one turn after the stop to say
	// what it reached (see desktopWatchdogVerdict).
	graced bool
	// wrote: desktop files this run wrote or edited, for the stop reply.
	wrote []string
	// ok: steps that succeeded.
	ok int
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
	if action == "file_write" || action == "file_edit" || action == "deliver" {
		if p := strings.TrimSpace(desktopArg(args, "path")); p != "" && len(w.wrote) < 50 {
			seen := false
			for _, x := range w.wrote {
				seen = seen || x == p
			}
			if !seen {
				w.wrote = append(w.wrote, p)
			}
		}
	}
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
		w.ok++
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
	// A run over its time or step budget was making progress: the model
	// gets one turn after the stop to say what is done and what is not,
	// which only it knows (a training stage, a metric). A run that keeps
	// calling tools ends here. Loops and failures end at once.
	if !w.graced && (w.stop == "wall" || w.stop == "steps") {
		w.graced = true
		return "", false
	}
	return desktopWatchdogReplyFor(w), true
}

// desktopWatchdogStopped reports a run the watchdog ended and why.
func desktopWatchdogStopped(runID string) (string, bool) {
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w, ok := watchdogRuns[runID]
	if !ok || w.stop == "" {
		return "", false
	}
	return w.stop, true
}

var desktopWatchdogWhy = map[string]string{
	"loop":    "同一步反复执行没有推进",
	"errors":  "接连几步失败",
	"refused": "接连几步被拒绝",
	"stalled": "很久没有进展",
	"steps":   "步数用完",
	"wall":    "超过了单次任务的时间上限",
}

// desktopWatchdogReplyFor is the stop reply with what the run concretely
// left behind (steps, time, files) and that the rest is not done.
func desktopWatchdogReplyFor(w *watchdogRun) string {
	text := desktopWatchdogReply(w.stop)
	if w.stop != "wall" && w.stop != "steps" {
		return text
	}
	mins := int(time.Since(w.started).Minutes())
	text = "这件还没做完：" + desktopWatchdogWhy[w.stop] + "，我先停下了，没有重复提交任何东西。"
	text += "已做 " + itoa(w.steps) + " 步（成功 " + itoa(w.ok) + " 步），用时约 " + itoa(mins) + " 分钟。"
	if len(w.wrote) > 0 {
		files := w.wrote
		if len(files) > 8 {
			files = files[len(files)-8:]
		}
		text += "已写出的文件：" + strings.Join(files, "、") + "。"
	}
	return text + "剩下的部分还没做，要继续的话跟我说一声，我从这里接着做。"
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
	// The run's event stream ends with its end, so an observer of the
	// stream does not mistake a finished run for a hung one (run_854d0758
	// ended at 21:27:45 with no last event after its last refused step).
	defer func() {
		es := scope
		es.RunID = run.ID
		desktopEmit(es, desktopEvent{Event: "run_end", Result: status})
	}()
	// The run status stays as it is (compatibility); what the task really
	// reached is task_outcome, from the goals' measured facts.
	defer desktopRecoveryEndRun(run.ID)
	outcome, why := "", ""
	applied := msg != nil && msg.Metadata[agentservice.TaskOutcomeKey] != ""
	if applied {
		// Set at completion by the service (desktopTaskOutcomeMetadata).
		outcome = msg.Metadata[agentservice.TaskOutcomeKey]
	} else {
		outcome, why = desktopRunOutcome(run.ID, msg)
	}
	if outcome != "" {
		status += " outcome=" + outcome
	}
	if msg != nil && strings.TrimSpace(msg.Content) != "" {
		if msg.Metadata["publish_state"] == "submitted_unconfirmed" {
			// The run finished; its post is not confirmed (not a success).
			status += " publish_unconfirmed"
		}
		if !applied {
			desktopApplyOutcome(msg, outcome, why)
			if fix := desktopPostCorrection(run.ID, msg.Content); fix != "" {
				msg.Content = fix + "\n" + msg.Content
			} else if posted := desktopPostedNotice(run.ID, msg.Content); posted != "" {
				msg.Content = posted + "\n" + msg.Content
			}
		}
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "run_end", Run: run.ID, Detail: status})
		return msg
	}
	if handoff || run.Status == agentservice.RunStatusCancelled {
		desktopLedgerAdd(key, desktopLedgerEntry{Kind: "run_end", Run: run.ID, Detail: status + " no_receipt_needed"})
		return msg
	}
	text := ""
	if base := desktopTaskSpecBase(run.ID); len(base.Sites) > 1 {
		if recs := desktopPublishRunRecords(run.ID); len(recs) > 0 {
			text = desktopReplyMulti(base, recs, desktopTaskSitesPending(run.ID), "")
		}
	} else if r, ok := desktopPublishRecordOf(run.ID); ok {
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
	desktopApplyOutcome(msg, outcome, why)
	desktopLedgerAdd(key, desktopLedgerEntry{Kind: "run_end", Run: run.ID, Detail: status + " receipt_by_srv"})
	log.Printf("[watchdog] run=%s ended without a reply; srv receipt sent (status=%s)", run.ID, status)
	return msg
}

func desktopLedgerRunPost(entries []desktopLedgerEntry, runID string) (desktopLedgerEntry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		// An unconfirmed submit is not "sent" (run_6840f5a1 would have
		// been answered 已经发出去了).
		if entries[i].Kind == "posted" && entries[i].Run == runID && entries[i].Detail != "submitted_unconfirmed" {
			return entries[i], true
		}
	}
	return desktopLedgerEntry{}, false
}

// desktopOutcomeSaysNotDone: the reply already tells the person it is not done.
var desktopOutcomeSaysNotDone = regexp.MustCompile(`没做成|没做完|未完成|没有完成|还没完成|没发出|未发布|没有发布|没发布|未提交|没有提交|未确认|还差|未执行|没有执行|没执行|没能完成|没有启动|没启动`)

// desktopApplyOutcome marks a reply whose task is not done (blocked,
// partial, not_submitted, unconfirmed, awaiting_user) and, unless the reply
// already says so, starts it with one plain line and the measured evidence.
func desktopApplyOutcome(msg *agentservice.Message, outcome, why string) {
	if msg == nil || outcome == "" {
		return
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]string{}
	}
	msg.Metadata["task_outcome"] = outcome
	if line := desktopOutcomeNotice(outcome, why, msg.Content); line != "" {
		msg.Content = line + "\n" + msg.Content
	}
}

// desktopOutcomeNotice is the line put in front of a reply whose task is not
// done ("" when the reply already says so, or nothing needs saying).
func desktopOutcomeNotice(outcome, why, content string) string {
	if outcome == "awaiting_user" || outcome == "done" || desktopOutcomeSaysNotDone.MatchString(content) {
		return ""
	}
	line := map[string]string{
		"partial":       "这件还没完全做成，只完成了一部分。",
		"blocked":       "这件没做成，我已经停下，没有再重复尝试。",
		"not_submitted": "这件还没提交，内容没有发出去。",
		"unconfirmed":   "提交后没能确认结果，我没有重复提交，请以页面上的实际状态为准。",
	}[outcome]
	if line != "" && why != "" {
		line += "（依据：" + why + "）"
	}
	return line
}

// desktopRunOutcome: the measured outcome of a run's goals, or — for a run
// that worked on the desktop and ended with a question — awaiting_user. A
// question-only chat (no desktop step) has no outcome.
func desktopRunOutcome(runID string, msg *agentservice.Message) (string, string) {
	outcome, why := desktopRunOutcomeMeasured(runID, msg)
	// A run the watchdog ended (time or step budget, a loop, failures) is
	// not done, whatever its last step was (macmini training, Nov 11 11:09,
	// was recorded done after its time limit). A more specific not-done
	// cause found above stays.
	if stop, ok := desktopWatchdogStopped(runID); ok && (outcome == "" || outcome == agentservice.TaskOutcomeDone) {
		if desktopRunEverWorked(runID) {
			return agentservice.TaskOutcomePartial, "stopped: " + desktopWatchdogWhy[stop]
		}
		return agentservice.TaskOutcomeBlocked, "stopped: " + desktopWatchdogWhy[stop]
	}
	return outcome, why
}

func desktopRunOutcomeMeasured(runID string, msg *agentservice.Message) (string, string) {
	outcome, why := desktopRecoveryOutcome(runID)
	// A post the site itself confirmed (from any step) settles the publish
	// part, whatever the publish action's own result was.
	outcome, why = desktopReconcileOutcome(runID, outcome, why)
	desktopRecStateMu.Lock()
	steps := desktopRecRunSteps[runID]
	desktopRecStateMu.Unlock()
	if outcome == "" && steps && msg != nil && msg.Metadata["response_source"] == "ask_user" {
		return "awaiting_user", "waiting for the person's answer"
	}
	if outcome == "" && steps && msg != nil && msg.Metadata["response_source"] != "ask_user" {
		// Nothing was done and the last step failed: the run stopped on a
		// failure, so it is blocked, never a quiet success (desktop_outage.go).
		if why, ok := desktopRunNeverWorked(runID); ok {
			return agentservice.TaskOutcomeBlocked, why
		}
		// Some work done, the last step failed and the reply itself says
		// it is not done: a failure reply always carries an outcome.
		if strings.TrimSpace(msg.Content) != "" && desktopOutcomeSaysNotDone.MatchString(msg.Content) && !desktopRunWorked(runID) {
			return agentservice.TaskOutcomePartial, "the last step failed"
		}
	}
	if outcome == "" && msg != nil && strings.TrimSpace(msg.Content) != "" && desktopRunWorked(runID) && !desktopOutcomeSaysNotDone.MatchString(msg.Content) {
		// A run that did its work and answered without a measured gap is
		// done: every finished desktop run carries an outcome.
		return agentservice.TaskOutcomeDone, ""
	}
	return outcome, why
}

// desktopRunWorked: the run executed desktop steps and its last one did not fail.
func desktopRunWorked(runID string) bool {
	desktopRecStateMu.Lock()
	steps := desktopRecRunSteps[runID]
	desktopRecStateMu.Unlock()
	if !steps {
		return false
	}
	if st := desktopRunForID(runID); st != nil {
		st.mu.Lock()
		failed := st.lastFailed
		st.mu.Unlock()
		return !failed
	}
	return true
}

// desktopTaskOutcomeMetadata is the assistant metadata the service turns
// into the run's task outcome at completion.
func desktopTaskOutcomeMetadata(runID string, msg agentservice.Message) map[string]string {
	if runID == "" {
		return nil
	}
	outcome, why := desktopRunOutcome(runID, &msg)
	if outcome == "" {
		desktopSideTake(runID)
		return nil
	}
	out := map[string]string{agentservice.TaskOutcomeKey: outcome}
	n := desktopOutcomeNotice(outcome, why, msg.Content)
	if outcome != agentservice.TaskOutcomeDone && outcome != "awaiting_user" {
		// The cause and what the run left behind (a draft, a click on
		// publish), even when the reply already says it is not done.
		if side := desktopSideNotice(runID); side != "" {
			n = strings.TrimSpace(n + "\n" + side)
		}
	} else {
		desktopSideTake(runID)
	}
	if fix := desktopPostCorrection(runID, msg.Content); fix != "" {
		n = strings.TrimSpace(fix + "\n" + n)
	} else if posted := desktopPostedNotice(runID, msg.Content); posted != "" {
		n = strings.TrimSpace(n + "\n" + posted)
	}
	if n != "" {
		out[agentservice.TaskOutcomeNoticeKey] = n
	}
	if why != "" {
		out["task_outcome_evidence"] = desktopRuneHead(why, 300)
	}
	return out
}

// desktopPostedNotice names the posts this run verifiably published that the
// reply does not mention ("" when it mentions them all). A reply written
// after the run got stuck said 「三个平台都没有发布成功」 while the ledger had the
// 小红书 post as published (run_854d0758).
func desktopPostedNotice(runID, content string) string {
	if runID == "" {
		return ""
	}
	var lines []string
	seen := map[string]bool{}
	for _, key := range desktopLedgerKeysForRun(runID) {
		for _, e := range desktopLedgerEntries(key) {
			if e.Kind != "posted" || e.Run != runID || e.Detail == "submitted_unconfirmed" || e.URL == "" || seen[e.URL] {
				continue
			}
			seen[e.URL] = true
			if strings.Contains(content, e.URL) {
				continue
			}
			line := "已确认发布：" + e.Host
			if e.Title != "" {
				line += "《" + e.Title + "》"
			}
			lines = append(lines, line+" "+e.URL)
		}
	}
	return strings.Join(lines, "\n")
}
