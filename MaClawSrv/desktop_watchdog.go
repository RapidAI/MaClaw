package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strconv"
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
	watchdogLoopWarn    = 4 // same call with the same result this many times in the window: warn
	watchdogLoopStop    = 6 // ... and stop
	watchdogLoopWindow  = 12
	watchdogErrStop     = 8 // consecutive failed steps
	watchdogRefuseStop  = 10
	watchdogSweepPeriod = time.Minute
)

// A run is not stopped by the clock while it is making real progress
// (Daniel, Nov 11 16:32: 只要在推进就可以不停，除非用户主动要求). It stops on a
// genuine stall (no measurable progress for watchdogStallAfter), a loop,
// repeated failures, the person's stop, or — only as a runaway guard — the
// very high ceilings below. All three are configurable.
var (
	watchdogStallAfter = desktopEnvMinutes("MACLAW_RUN_STALL_MINUTES", 20*time.Minute)
	watchdogMaxWall    = desktopEnvMinutes("MACLAW_RUN_MAX_MINUTES", 12*time.Hour)
	watchdogMaxSteps   = desktopEnvInt("MACLAW_RUN_MAX_STEPS", 2000, 50, 1000000)
	// watchdogWeakOnly: clicks that succeed and screens that change are
	// progress, but a run with only those (no file, goal check, log line
	// or metric) for this long is wandering, not working.
	watchdogWeakOnly = desktopEnvMinutes("MACLAW_RUN_WEAK_PROGRESS_MINUTES", time.Hour)
	// watchdogNow is the clock; tests move it.
	watchdogNow = time.Now
)

func desktopEnvMinutes(name string, def time.Duration) time.Duration {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && v > 0 {
		return time.Duration(v) * time.Minute
	}
	return def
}

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
	// lastProgress is when a step last changed something measurable
	// (state, a file, a goal check, a log or metric, the screen after an
	// action); progressWhy says what. A run is stalled when it has not
	// progressed for watchdogStallAfter, whatever its age.
	lastProgress time.Time
	lastStrong   time.Time
	progressWhy  string
	progressN    int
	// stageN/stageWhy: meaningful progress the person hears about — a new
	// measured metric/epoch line or a changed goal check, not a step count,
	// a new screen or new command output.
	stageN   int
	stageWhy string
	// seen: normalized results already observed in this run.
	seen        map[string]bool
	lastMetrics string
	lastGoal    string
	lastAction  string
	lastLook    bool
	// pending: the call Before saw whose result has not been reported.
	pending  string
	inflight bool
	// still: the last few steps that changed nothing, for the stall reply.
	still []string
	scope agentruntime.Scope
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
		now := watchdogNow()
		w = &watchdogRun{started: now, lastStep: now, lastProgress: now, lastStrong: now, warned: map[string]bool{}, seen: map[string]bool{}}
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
	w.scope = scope
	now := watchdogNow()
	if w.lastProgress.IsZero() {
		w.lastProgress = w.started
	}
	if w.lastStrong.IsZero() {
		w.lastStrong = w.started
	}
	if w.pending != "" {
		// The last call never reported a result: it moved nothing.
		watchdogPushSig(w, w.pending)
	}
	w.steps++
	w.lastStep = now
	w.pending, w.inflight = sig, true
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
	// same: how often this exact call already ran in the window without
	// moving anything, plus this one. A call that keeps returning new
	// output (tailing a training log) is not a loop.
	same := 1
	for _, s := range w.sigs {
		if s == sig {
			same++
		}
	}
	switch {
	case w.stop != "":
	case same >= watchdogLoopStop:
		w.stop = "loop"
	case now.Sub(w.lastProgress) >= watchdogStallAfter, now.Sub(w.lastStrong) >= watchdogWeakOnly:
		w.stop = "stalled"
	case w.steps >= watchdogMaxSteps:
		w.stop = "steps"
	case now.Sub(w.started) >= watchdogMaxWall:
		w.stop = "wall"
	}
	if w.stop != "" {
		w.pending, w.inflight = "", false
		log.Printf("[watchdog] run=%s stop=%s steps=%d since_progress=%s", scope.RunID, w.stop, w.steps, now.Sub(w.lastProgress).Round(time.Second))
		return "", errWatchdogStop
	}
	if same >= watchdogLoopWarn && !w.warned["loop:"+sig] {
		w.warned["loop:"+sig] = true
		return "\nWATCHDOG: this exact " + action + " call has now run " + itoa(same) + " times in this run with the same result. Do not repeat it: use a different method, or end the turn and say plainly what is blocking.", nil
	}
	if idle := now.Sub(w.lastProgress); idle >= watchdogStallAfter/2 && !w.warned["stall"] {
		w.warned["stall"] = true
		return "\nWATCHDOG: nothing measurable has changed for " + itoa(int(idle.Minutes())) + " minutes (no new state, file, goal-check result, log line or screen). If the task is stuck, stop and say where and why; at " + itoa(int(watchdogStallAfter.Minutes())) + " minutes without progress this run is ended.", nil
	}
	return "", nil
}

// desktopWatchdogCredit moves the run's progress clocks forward by d: time a
// step spent queued for the screen behind another task is not a stall.
func desktopWatchdogCredit(runID string, d time.Duration) {
	if runID == "" || d <= 0 {
		return
	}
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w, ok := watchdogRuns[runID]
	if !ok {
		return
	}
	now := watchdogNow()
	shift := func(t time.Time) time.Time {
		if t = t.Add(d); t.After(now) {
			return now
		}
		return t
	}
	w.lastProgress = shift(w.lastProgress)
	w.lastStrong = shift(w.lastStrong)
	w.lastStep = shift(w.lastStep)
}

func watchdogPushSig(w *watchdogRun, sig string) {
	w.sigs = append(w.sigs, sig)
	if len(w.sigs) > watchdogLoopWindow {
		w.sigs = w.sigs[len(w.sigs)-watchdogLoopWindow:]
	}
}

// desktopWatchdogStopByUser ends a run the person stopped or cancelled: no
// further step runs, whatever progress it was making.
func desktopWatchdogStopByUser(runID string) {
	if runID == "" {
		return
	}
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w := watchdogGet(runID)
	if w.stop == "" {
		w.stop = "user"
		log.Printf("[watchdog] run=%s stop=user", runID)
	}
}

// watchdogTimeRe: clock times and dates, which change in any output and are
// not progress by themselves.
var watchdogTimeRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]?|\b\d{1,2}:\d{2}(:\d{2}(\.\d+)?)?\b|\b1[6-9]\d{8}(\d{3})?\b`)

// desktopWatchdogResult judges whether a finished step made measurable
// progress. Progress: a successful step whose result is new in this run
// (changed state), a new or changed file, a changed goal check, a new
// measured log/metric line, or a new screen right after an action.
func desktopWatchdogResult(scope agentruntime.Scope, action string, args map[string]any, out string, err error) {
	if scope.RunID == "" {
		return
	}
	action = strings.ToLower(strings.TrimSpace(action))
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w := watchdogGet(scope.RunID)
	sig := w.pending
	w.pending, w.inflight = "", false
	if sig == "" {
		sig = watchdogSig(action, args)
	}
	why, strong := "", false
	if err == nil {
		norm := watchdogTimeRe.ReplaceAllString(out, "#")
		sum := sha1.Sum([]byte(action + "|" + norm))
		h := hex.EncodeToString(sum[:8])
		fresh := !w.seen[h]
		if len(w.seen) < 8192 {
			w.seen[h] = true
		}
		look := desktopLookActions[action]
		switch {
		case action == "file_write" || action == "file_edit" || action == "deliver":
			ps := sha1.Sum([]byte(action + "|" + desktopArg(args, "path") + "|" + desktopArg(args, "content") + "|" + desktopArg(args, "new_text")))
			k := "w:" + hex.EncodeToString(ps[:8])
			if !w.seen[k] {
				w.seen[k] = true
				why, strong = "写出文件 "+desktopRuneHead(strings.TrimSpace(desktopArg(args, "path")), 80), true
			}
		case action == "goal_check":
			if g := strings.TrimSpace(norm); g != "" && g != w.lastGoal {
				w.lastGoal = g
				why, strong = "目标检查结果有变化", true
				w.stageN++
				w.stageWhy = why
			}
		case action == "bash" || action == "job_status":
			if m := desktopRunProgressLines(out); m != "" && m != w.lastMetrics {
				w.lastMetrics = m
				lines := strings.Split(m, "\n")
				why, strong = "指标推进："+desktopRuneHead(lines[len(lines)-1], 120), true
				w.stageN++
				w.stageWhy = why
			} else if fresh && watchdogNewShape(w, action, norm) {
				why, strong = action+" 有新的输出", true
			}
		case look:
			if fresh && w.lastAction != "" && !w.lastLook {
				why = "操作后屏幕有新状态"
			}
		default:
			if fresh {
				why = action + " 成功"
			}
		}
		w.lastLook = look
	}
	w.lastAction = action
	if why != "" {
		// Moving again: a later stall is warned about again.
		delete(w.warned, "stall")
		if strong {
			w.lastStrong = watchdogNow()
		}
		w.lastProgress = watchdogNow()
		w.progressWhy = why
		w.progressN++
		w.still = nil
		watchdogPushSig(w, "+")
		return
	}
	watchdogPushSig(w, sig)
	label := action
	if err != nil {
		label += "（失败）"
	}
	w.still = append(w.still, label)
	if len(w.still) > 6 {
		w.still = w.still[len(w.still)-6:]
	}
}

var errWatchdogStop = watchdogError("refused: watchdog_stop — this run is being ended by the watchdog (stopped by the person, no measurable progress for a long time, looping, repeated failures, or its runaway ceiling). Do not call any more tools; give your final result now in a few short first-person sentences: what is done (with measured evidence such as metrics or files), where it got stuck and why, what is left, and whether a background job is still running and how its result will reach the person.")

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
	if !w.graced && (w.stop == "wall" || w.stop == "steps" || w.stop == "stalled") {
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
	"stalled": "很久没有可测的进展",
	"steps":   "到了单次任务的步数安全上限",
	"wall":    "到了单次任务的安全时间上限",
	"user":    "任务被取消",
}

// desktopWatchdogReplyFor is the stop reply with what the run concretely
// left behind (steps, time, files) and that the rest is not done.
func desktopWatchdogReplyFor(w *watchdogRun) string {
	text := desktopWatchdogReply(w.stop)
	if w.stop != "wall" && w.stop != "steps" && w.stop != "stalled" {
		return text
	}
	mins := int(watchdogNow().Sub(w.started).Minutes())
	if w.stop == "stalled" {
		idle := int(watchdogNow().Sub(w.lastProgress).Minutes())
		text = "这件我停住了：已经 " + itoa(idle) + " 分钟没有可测的进展，我先停下，没有重复提交任何东西。"
		if len(w.still) > 0 {
			text += "卡在这里：最近 " + itoa(len(w.still)) + " 步（" + strings.Join(w.still, "、") + "）结果都没有变化。"
		}
	} else {
		limit := itoa(watchdogMaxSteps) + " 步"
		if w.stop == "wall" {
			limit = itoa(int(watchdogMaxWall.Hours())) + " 小时"
		}
		text = "这件还没做完：" + desktopWatchdogWhy[w.stop] + "（" + limit + "），我先停下了，没有重复提交任何东西。"
	}
	text += "已做 " + itoa(w.steps) + " 步（成功 " + itoa(w.ok) + " 步），用时约 " + itoa(mins) + " 分钟。"
	if w.progressWhy != "" {
		text += "最后一次可测的进展在 " + w.lastProgress.Format("15:04") + "：" + w.progressWhy + "。"
	}
	if len(w.wrote) > 0 {
		files := w.wrote
		if len(files) > 8 {
			files = files[len(files)-8:]
		}
		text += "已写出的文件：" + strings.Join(files, "、") + "。"
	}
	if j := desktopLongJobForRun(w.scope.RunID); j != nil && (j.Status == "running" || j.Status == "stalled") {
		text += "后台任务没有被停掉，还在跑：" + desktopLongJobLabel(j.Command) + "（任务 " + desktopLongJobShortID(j.ID) + "），有进展、结束或失败时我会告诉你。"
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
		return "这件我停住了：很久没有可测的进展，我先收手，页面保持原样。你说一声我接着做。"
	case "user":
		return "这件已经停下（任务被取消），之后没有再操作，也没有重复提交任何东西。"
	}
	return "这件花的时间太长了，我先停在当前进度，没有重复提交任何东西。要继续的话跟我说一声。"
}

func desktopWatchdogEnd(runID string) {
	desktopScreenLeaseEndRun(runID, "run_end")
	watchdogMu.Lock()
	delete(watchdogRuns, runID)
	watchdogMu.Unlock()
	desktopRunProgressMu.Lock()
	delete(desktopRunProgressSeen, runID)
	desktopRunProgressMu.Unlock()
}

// desktopWatchdogLoop flags runs that took no step for a long time (a hung
// model call or tool). The flag ends them at their next step boundary.
func desktopWatchdogLoop() {
	t := time.NewTicker(watchdogSweepPeriod)
	defer t.Stop()
	for range t.C {
		desktopWatchdogSweepOnce(watchdogNow())
	}
}

// desktopWatchdogSweepOnce flags stalled runs and gives each live run its
// periodic status (desktop_run_progress.go), also while one long step runs.
func desktopWatchdogSweepOnce(now time.Time) {
	var live []agentruntime.Scope
	watchdogMu.Lock()
	for id, w := range watchdogRuns {
		if now.Sub(w.lastStep) > watchdogMaxWall+time.Hour {
			delete(watchdogRuns, id)
			continue
		}
		idle := now.Sub(w.lastProgress)
		// A step still running is judged when it returns; only a step
		// hung far past the stall window flags the run.
		if w.stop == "" && ((!w.inflight && idle > watchdogStallAfter) || (w.inflight && now.Sub(w.lastStep) > 3*watchdogStallAfter)) {
			w.stop = "stalled"
			log.Printf("[watchdog] run=%s stalled %s without measurable progress", id, idle.Round(time.Second))
		}
		if w.stop == "" && w.scope.RunID != "" {
			live = append(live, w.scope)
		}
	}
	watchdogMu.Unlock()
	for _, sc := range live {
		desktopRunStatusTick(sc, now)
	}
}

// desktopWatchdogSnapshot is what the status line reports for a run.
type desktopWatchdogSnap struct {
	started, lastProgress time.Time
	steps, ok, progressN  int
	progressWhy, metrics  string
	stageN                int
	stageWhy              string
	stallWarned           bool
	wrote                 []string
	stop                  string
}

func desktopWatchdogSnapshot(runID string) (desktopWatchdogSnap, bool) {
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w, ok := watchdogRuns[runID]
	if !ok {
		return desktopWatchdogSnap{}, false
	}
	return desktopWatchdogSnap{started: w.started, lastProgress: w.lastProgress, steps: w.steps, ok: w.ok, progressN: w.progressN, progressWhy: w.progressWhy, metrics: w.lastMetrics, wrote: append([]string(nil), w.wrote...), stop: w.stop, stageN: w.stageN, stageWhy: w.stageWhy, stallWarned: w.warned["stall"]}, true
}

// desktopWatchdogGrant gives a run that is still progressing more model
// iterations, so the loop's iteration budget is no clock either. A stopped,
// stalled or runaway run gets none.
func desktopWatchdogGrant(runID string, used, toolCalls int) int {
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	w, ok := watchdogRuns[runID]
	if !ok || w.stop != "" || toolCalls <= 0 {
		return 0
	}
	now := watchdogNow()
	if now.Sub(w.lastProgress) >= watchdogStallAfter || w.steps >= watchdogMaxSteps || now.Sub(w.started) >= watchdogMaxWall {
		return 0
	}
	log.Printf("[watchdog] run=%s still progressing (%s); %d more iterations after %d", runID, w.progressWhy, 100, used)
	return 100
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
		"running":       "后台任务还在跑，我会每 10 分钟左右把进度发给你，结束、失败或卡住时马上告诉你。",
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
	if oc, why, ok := desktopLongJobOutcome(runID); ok {
		// The work was handed off: a time-limit on this chat turn is not
		// a failure of the background job, which the watcher still polls.
		return oc, why
	}
	outcome, why := desktopRunOutcomeMeasured(runID, msg)
	// A run the watchdog ended (time or step budget, a loop, failures) is
	// not done, whatever its last step was (macmini training, Nov 11 11:09,
	// was recorded done after its time limit). A more specific not-done
	// cause found above stays.
	if stop, ok := desktopWatchdogStopped(runID); ok && (outcome == "" || outcome == agentservice.TaskOutcomeDone) {
		why := "stopped: " + desktopWatchdogWhy[stop]
		if snap, ok := desktopWatchdogSnapshot(runID); ok && snap.progressWhy != "" {
			why += "；最后一次可测的进展 " + snap.lastProgress.Format("15:04") + "：" + snap.progressWhy
		}
		if desktopRunEverWorked(runID) {
			return agentservice.TaskOutcomePartial, why
		}
		return agentservice.TaskOutcomeBlocked, why
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

var watchdogNumberRe = regexp.MustCompile(`[0-9]+(\.[0-9]+)?`)

// watchdogNewShape reports output whose form, numbers aside, this run has
// not printed before. Re-running a check that prints the same lines with
// other numbers (a pixel diff, a pid probe, a file count) measures, it does
// not move the task: Bot 2 spent ~30 steps on "N px changed" after its
// screenshot existed, and each one counted as progress (papers 11-11
// 20:38-20:53). Measured results are judged before this, as metrics.
func watchdogNewShape(w *watchdogRun, action, norm string) bool {
	shape := watchdogNumberRe.ReplaceAllString(strings.Join(strings.Fields(norm), " "), "N")
	sum := sha1.Sum([]byte("shape|" + action + "|" + shape))
	k := "s:" + hex.EncodeToString(sum[:8])
	if w.seen[k] {
		return false
	}
	if len(w.seen) < 8192 {
		w.seen[k] = true
	}
	return true
}
