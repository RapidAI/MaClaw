package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
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
// itself, and a hand-off has its own message. noDesktop marks a bot with no
// computer: its fallback sentence must not point at a screen it never had.
func desktopEnsureReceipt(scope agentruntime.Scope, run *agentservice.Run, msg *agentservice.Message, runErr error, handoff, noDesktop bool) *agentservice.Message {
	if run == nil {
		return msg
	}
	defer desktopWatchdogEnd(run.ID)
	defer agentservice.ClearRunRuntimeStatus(run.ID)
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
		before := outcome
		outcome, why = desktopReconcileDelivery(run, msg, outcome, why, runErr, false)
		if !applied || outcome != before {
			desktopApplyOutcome(msg, outcome, why)
			if fix := desktopPostCorrection(run.ID, msg.Content); fix != "" && !strings.Contains(msg.Content, fix) {
				msg.Content = fix + "\n" + msg.Content
			} else if posted := desktopPostedNotice(run.ID, msg.Content); posted != "" && !strings.Contains(msg.Content, posted) {
				msg.Content = posted + "\n" + msg.Content
			}
		}
		desktopSyncVisible(msg)
		status = string(run.Status)
		if outcome != "" {
			status += " outcome=" + outcome
		}
		if msg.Metadata["publish_state"] == "submitted_unconfirmed" {
			status += " publish_unconfirmed"
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
			if noDesktop {
				text = "这件我没做成，中途出了问题。你说一声我再试一次。"
			}
		} else {
			if msg == nil {
				msg = &agentservice.Message{SessionID: run.SessionID, InstanceID: run.InstanceID, Role: agentservice.MessageRoleAssistant, CreatedAt: time.Now()}
			}
			outcome, why = desktopReconcileDelivery(run, msg, outcome, why, runErr, true)
			text = msg.Content
		}
	}
	if msg == nil {
		msg = &agentservice.Message{SessionID: run.SessionID, InstanceID: run.InstanceID, Role: agentservice.MessageRoleAssistant, CreatedAt: time.Now()}
	}
	if strings.TrimSpace(msg.Content) == "" {
		msg.Content = text
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]string{}
	}
	msg.Metadata["receipt"] = "srv"
	desktopApplyOutcome(msg, outcome, why)
	desktopSyncVisible(msg)
	desktopLedgerAdd(key, desktopLedgerEntry{Kind: "run_end", Run: run.ID, Detail: status + " receipt_by_srv"})
	log.Printf("[watchdog] run=%s ended without a reply; srv receipt sent (status=%s)", run.ID, status)
	return msg
}

// desktopUndeliveredSentence is the visible result when a run ends without
// a requested output. It replaces a success claim that the evidence does not support.
const desktopUndeliveredSentence = "这件没做完：结束时没有交回可核对的结果，我没有把它算作完成。"

// desktopUnverifiedSentence is the visible line when execution finished but
// the task effect was not verified. The reply's own text stays after it.
const desktopUnverifiedSentence = "这件还不能确认已经完成，请以实际结果为准。"

// deliveredOutputNames is files the deliver tool placed on the reply, with
// bytes. A process screenshot is not one, and neither is a name or a media
// id without bytes. The request text is not consulted.
func deliveredOutputNames(msg *agentservice.Message) []string {
	if msg == nil {
		return nil
	}
	var names []string
	for _, item := range msg.Attachments {
		if item.Delivery != agent.AttachmentDeliveryFile || strings.TrimSpace(item.Data) == "" {
			continue
		}
		name := strings.TrimSpace(item.FileName)
		if name == "" {
			name = "文件"
		}
		names = append(names, name)
		if len(names) == 3 {
			break
		}
	}
	return names
}

func desktopRunHasConfirmedPost(runID string) bool {
	for _, key := range desktopLedgerKeysForRun(runID) {
		if _, ok := desktopLedgerRunPost(desktopLedgerEntries(key), runID); ok {
			return true
		}
	}
	if rec, ok := desktopPublishRecordOf(runID); ok && rec.posted() {
		return true
	}
	return false
}

// desktopVisibleAnswer is text the person can read. A file is not required
// for an answer to count. send_to_user is that text when the body is empty.
func desktopVisibleAnswer(msg *agentservice.Message) string {
	if msg == nil {
		return ""
	}
	if text := strings.TrimSpace(msg.Content); text != "" {
		return text
	}
	if msg.Metadata == nil {
		return ""
	}
	return strings.TrimSpace(msg.Metadata["send_to_user"])
}

// desktopReconcileDelivery turns tool and contract records into an outcome.
// A text answer completes a turn that asked for no artifact. An open output
// contract or an unposted publish stays unverified, whatever the prose says.
// A process screenshot is not one of those records. A file does not replace
// a measured not-done outcome or an unposted publish.
func desktopReconcileDelivery(run *agentservice.Run, msg *agentservice.Message, outcome, why string, runErr error, _ bool) (string, string) {
	if msg == nil || run == nil || runErr != nil || run.Status == agentservice.RunStatusFailed || run.Status == agentservice.RunStatusCancelled || run.Status == agentservice.RunStatusPaused {
		return outcome, why
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]string{}
	}
	if msg.Metadata["response_source"] == "ask_user" {
		return outcome, why
	}
	switch outcome {
	case agentservice.TaskOutcomePartial, agentservice.TaskOutcomeBlocked, agentservice.TaskOutcomeNotSubmitted, agentservice.TaskOutcomeUnconfirmed, agentservice.TaskOutcomeUnverified, agentservice.TaskOutcomeAwaitingUser, agentservice.TaskOutcomeRunning:
		return outcome, why
	}
	files := desktopDeliveredFileCount(run.ID, msg)
	complete, incomplete, other := desktopPostEvidence(run.ID)
	contract, resolved := desktopWaitForContract(run.ID)
	if resolved && contract.failed {
		// Extraction failure is a read failure, not a business failure: the
		// run stays unverified, never an empty-done. The concrete cause can
		// carry a local path or token from the transport/model error, so it
		// goes to task_outcome_evidence (and the log) — never the person-facing
		// sentence. Only the generic line is the `why` the reply carries. This
		// message is persisted through PersistRunReceipt, so writing evidence
		// here — not a courier key the earlier metadata hook already missed —
		// is what actually lands it on the stored receipt.
		if contract.failWhy != "" {
			msg.Metadata["task_outcome_evidence"] = desktopRuneHead("deliverable extraction failed: "+contract.failWhy, 300)
			log.Printf("[watchdog] run=%s deliverable extraction failed: %s", run.ID, contract.failWhy)
		}
		return agentservice.TaskOutcomeUnverified, "deliverable extraction failed"
	}
	if resolved && contract.typed {
		verdict, why := desktopAttributedVerdict(run.ID, msg, contract.parsed)
		noteDeliveredFiles(run.ID, msg, files)
		rememberContractOutcome(run, msg, contract.parsed, verdict)
		return verdict, why
	}
	if resolved {
		if verdict, why := desktopContractVerdict(contract.items, contract.slots, files, complete, incomplete, other); verdict != "" {
			noteDeliveredFiles(run.ID, msg, files)
			return verdict, why
		}
	}
	if desktopExtractionUnresolved(run.ID) {
		noteDeliveredFiles(run.ID, msg, files)
		return agentservice.TaskOutcomeUnverified, "deliverable extraction unfinished"
	}
	if desktopPlanTurn(run) {
		answered := desktopVisibleAnswer(msg) != ""
		noteDeliveredFiles(run.ID, msg, files)
		if answered {
			return agentservice.TaskOutcomeDone, "text answer"
		}
		return agentservice.TaskOutcomeUnverified, "plan is not delivery"
	}
	if other > 0 && complete == 0 {
		noteDeliveredFiles(run.ID, msg, files)
		return agentservice.TaskOutcomeUnverified, "publish not verified"
	}
	if incomplete > 0 {
		noteDeliveredFiles(run.ID, msg, files)
		return agentservice.TaskOutcomePartial, "publish incomplete"
	}
	if complete > 0 {
		noteDeliveredFiles(run.ID, msg, files)
		return agentservice.TaskOutcomeDone, "published"
	}
	if files > 0 {
		noteDeliveredFiles(run.ID, msg, files)
		return agentservice.TaskOutcomeDone, "file delivered"
	}
	if desktopVisibleAnswer(msg) != "" && (!desktopRunTookSteps(run.ID) || resolved) {
		return agentservice.TaskOutcomeDone, "text answer"
	}
	if desktopVisibleAnswer(msg) != "" {
		return agentservice.TaskOutcomeUnverified, "desktop effect not verified"
	}
	if msg.Metadata[agentservice.TaskOutcomeKey] == agentservice.TaskOutcomeBlocked &&
		strings.Contains(msg.Content, desktopUndeliveredSentence) {
		return agentservice.TaskOutcomeBlocked, why
	}
	msg.Content = desktopUndeliveredSentence
	msg.Metadata["send_to_user"] = desktopUndeliveredSentence
	msg.Metadata["send_to_user_kind"] = "report"
	return agentservice.TaskOutcomeBlocked, "nothing was said and nothing was delivered"
}

const (
	desktopContractFile = "file"
	desktopContractPost = "post"
	desktopContractText = "text"
)

type desktopOutputContract struct {
	items    []string
	slots    []string
	parsed   []agent.DesktopDeliverable
	failed   bool
	failWhy  string
	typed    bool
	resolved bool
}

var desktopOutputContracts sync.Map

// noteDesktopOutputContract stores a resolved checklist whose items have no
// kind. An empty list is a resolved "no artifact" contract. Without a kind,
// more than one item cannot be proved complete by counting evidence.
func noteDesktopOutputContract(runID string, items []string) {
	noteDesktopOutputPlan(runID, items, nil)
}

// noteDesktopOutputSlots stores a structured contract. Each slot is "file"
// or "post". Completion needs that many delivered files and fully live posts.
func noteDesktopOutputSlots(runID string, slots []string) {
	noteDesktopOutputPlan(runID, nil, slots)
}

func noteDesktopOutputPlan(runID string, items, slots []string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	desktopOutputContracts.Store(runID, desktopOutputContract{
		items:    append([]string(nil), items...),
		slots:    append([]string(nil), slots...),
		resolved: true,
	})
}

func noteParsedDeliverableContract(runID string, items []agent.DesktopDeliverable, failed bool, cause string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	typed := false
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != "" || item.Kind != "" {
			typed = true
		}
		if text := strings.TrimSpace(item.Text); text != "" {
			texts = append(texts, text)
		}
	}
	desktopOutputContracts.Store(runID, desktopOutputContract{
		items:    texts,
		parsed:   append([]agent.DesktopDeliverable(nil), items...),
		failed:   failed,
		failWhy:  strings.TrimSpace(cause),
		typed:    typed && !failed,
		resolved: true,
	})
}

func desktopOutputContractOf(runID string) (desktopOutputContract, bool) {
	value, ok := desktopOutputContracts.Load(strings.TrimSpace(runID))
	if !ok {
		return desktopOutputContract{}, false
	}
	contract, _ := value.(desktopOutputContract)
	if !contract.resolved && !contract.failed {
		contract.resolved = true
	}
	return contract, true
}

func deliveredFileCount(msg *agentservice.Message) int {
	if msg == nil {
		return 0
	}
	n := 0
	for _, item := range msg.Attachments {
		if item.Delivery == agent.AttachmentDeliveryFile && strings.TrimSpace(item.Data) != "" {
			n++
		}
	}
	return n
}

func desktopDeliveredFileCount(runID string, msg *agentservice.Message) int {
	seen := map[string]bool{}
	if msg != nil {
		for _, item := range msg.Attachments {
			if item.Delivery != agent.AttachmentDeliveryFile || strings.TrimSpace(item.Data) == "" {
				continue
			}
			seen[desktopFileClaimKey(fileFingerprint(item.Data), "")] = true
		}
	}
	for _, claim := range desktopFileClaims(runID) {
		if key := desktopFileClaimKey(claim.Fingerprint, claim.Ref); key != "" {
			seen[key] = true
		}
	}
	return len(seen)
}

func noteDeliveredFiles(runID string, msg *agentservice.Message, files int) {
	if files == 0 || msg == nil || strings.TrimSpace(msg.Content) != "" {
		return
	}
	names := deliveredOutputNames(msg)
	for _, claim := range desktopFileClaims(runID) {
		if claim.Name == "" {
			continue
		}
		dup := false
		for _, name := range names {
			if name == claim.Name {
				dup = true
				break
			}
		}
		if !dup {
			names = append(names, claim.Name)
		}
		if len(names) >= 3 {
			break
		}
	}
	if len(names) == 0 {
		return
	}
	msg.Content = "结果文件已经附在这条回复上：" + strings.Join(names, "、") + "。"
}

type desktopPostSnapshot struct {
	complete, incomplete, other int
	ok                          bool
}

var desktopPostSnapshots sync.Map

// desktopSnapshotPosts remembers the run's publish evidence before the reply
// path takes the records. The receipt still has to see them.
func desktopSnapshotPosts(runID string) {
	complete, incomplete, other := desktopPostEvidenceLive(runID)
	desktopPostSnapshots.Store(runID, desktopPostSnapshot{complete: complete, incomplete: incomplete, other: other, ok: true})
}

// desktopPostEvidence counts publish records. A fully live post is complete.
// A live-but-incomplete post is not. When the run has no publish record, one
// confirmed ledger entry counts as one complete post.
func desktopPostEvidence(runID string) (complete, incomplete, other int) {
	if value, ok := desktopPostSnapshots.Load(runID); ok {
		snap, _ := value.(desktopPostSnapshot)
		if snap.ok {
			return snap.complete, snap.incomplete, snap.other
		}
	}
	return desktopPostEvidenceLive(runID)
}

func desktopPostEvidenceLive(runID string) (complete, incomplete, other int) {
	recs := desktopPublishRunRecords(runID)
	if len(recs) == 0 {
		if desktopRunHasConfirmedPost(runID) {
			return 1, 0, 0
		}
		return 0, 0, 0
	}
	for _, rec := range recs {
		switch rec.State {
		case browser.PublishLive:
			complete++
		case browser.PublishLiveIncomplete:
			incomplete++
		default:
			other++
		}
	}
	return complete, incomplete, other
}

// desktopContractVerdict checks every required slot against its own evidence.
// Opaque items have no kind: one item can be closed by one complete file or
// one fully live post, and more than one item stays incomplete because the
// evidence cannot be attributed. An empty checklist is not a verdict.
func desktopContractVerdict(items, slots []string, files, complete, incomplete, other int) (string, string) {
	if len(slots) > 0 {
		// A count of files or posts is not proof that each required item
		// was delivered. Without an item id on the evidence, the contract
		// stays incomplete.
		if files+complete+incomplete+other == 0 {
			return agentservice.TaskOutcomeUnverified, "output contract open"
		}
		return agentservice.TaskOutcomePartial, "output contract unattributed"
	}
	switch len(items) {
	case 0:
		return "", ""
	case 1:
		if incomplete == 0 && other == 0 && complete == 0 && files == 1 {
			return agentservice.TaskOutcomeDone, "file delivered"
		}
		if incomplete == 0 && other == 0 && files == 0 && complete == 1 {
			return agentservice.TaskOutcomeDone, "published"
		}
		if files+complete+incomplete+other == 0 {
			return agentservice.TaskOutcomeUnverified, "output contract open"
		}
		return agentservice.TaskOutcomePartial, "output contract incomplete"
	default:
		if files+complete+incomplete+other == 0 {
			return agentservice.TaskOutcomeUnverified, "output contract open"
		}
		return agentservice.TaskOutcomePartial, "output contract incomplete"
	}
}

type desktopPostClaim struct {
	ItemID   string
	Identity string
	Complete bool
}

var (
	desktopPostClaimMu     sync.Mutex
	desktopPostClaimsByRun = map[string][]desktopPostClaim{}
)

func desktopNotePostClaim(runID string, claim desktopPostClaim) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	desktopPostClaimMu.Lock()
	desktopPostClaimsByRun[runID] = append(desktopPostClaimsByRun[runID], claim)
	desktopPostClaimMu.Unlock()
}

func desktopPostClaims(runID string) []desktopPostClaim {
	desktopPostClaimMu.Lock()
	defer desktopPostClaimMu.Unlock()
	return append([]desktopPostClaim(nil), desktopPostClaimsByRun[runID]...)
}

func fileFingerprint(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// desktopAttributedVerdict marks a typed item done only when one piece of
// evidence names its id and that evidence has not already satisfied a
// different item. The same file bytes or the same post cannot close two items.
// One text item is the reply itself. Several text items are not, because one
// reply has no per-item identity.
func desktopAttributedVerdict(runID string, msg *agentservice.Message, items []agent.DesktopDeliverable) (string, string) {
	if len(items) == 0 {
		return "", ""
	}
	byID := map[string]agent.DesktopDeliverable{}
	textItems := 0
	onlyText := ""
	fileItems := 0
	onlyFile := ""
	for _, item := range items {
		if item.ID == "" {
			return agentservice.TaskOutcomeUnverified, "output contract open"
		}
		byID[item.ID] = item
		if item.Kind == desktopContractText {
			textItems++
			onlyText = item.ID
		}
		if item.Kind == desktopContractFile {
			fileItems++
			onlyFile = item.ID
		}
	}
	satisfied := map[string]bool{}
	usedFile := map[string]string{}
	usedPost := map[string]string{}
	if msg != nil {
		for _, att := range msg.Attachments {
			if att.Delivery != agent.AttachmentDeliveryFile || strings.TrimSpace(att.Data) == "" {
				continue
			}
			id := strings.TrimSpace(att.ItemID)
			item, ok := byID[id]
			if !ok || item.Kind != desktopContractFile || satisfied[id] {
				continue
			}
			key := desktopFileClaimKey(fileFingerprint(att.Data), "")
			if prev, taken := usedFile[key]; taken && prev != id {
				continue
			}
			usedFile[key] = id
			satisfied[id] = true
		}
	}
	for _, claim := range desktopFileClaims(runID) {
		id := strings.TrimSpace(claim.ItemID)
		item, ok := byID[id]
		key := desktopFileClaimKey(claim.Fingerprint, claim.Ref)
		if !ok || item.Kind != desktopContractFile || satisfied[id] || key == "" {
			continue
		}
		if prev, taken := usedFile[key]; taken && prev != id {
			continue
		}
		usedFile[key] = id
		satisfied[id] = true
	}
	for _, claim := range desktopPostClaims(runID) {
		if !claim.Complete {
			continue
		}
		id := strings.TrimSpace(claim.ItemID)
		item, ok := byID[id]
		if !ok || item.Kind != desktopContractPost || satisfied[id] {
			continue
		}
		ident := strings.TrimSpace(claim.Identity)
		if ident == "" {
			continue
		}
		if prev, taken := usedPost[ident]; taken && prev != id {
			continue
		}
		usedPost[ident] = id
		satisfied[id] = true
	}
	if fileItems == 1 && !satisfied[onlyFile] {
		type fileEvidence struct {
			id  string
			key string
		}
		var evidence []fileEvidence
		if msg != nil {
			for _, att := range msg.Attachments {
				if att.Delivery != agent.AttachmentDeliveryFile || strings.TrimSpace(att.Data) == "" {
					continue
				}
				evidence = append(evidence, fileEvidence{id: strings.TrimSpace(att.ItemID), key: desktopFileClaimKey(fileFingerprint(att.Data), "")})
			}
		}
		for _, claim := range desktopFileClaims(runID) {
			evidence = append(evidence, fileEvidence{id: strings.TrimSpace(claim.ItemID), key: desktopFileClaimKey(claim.Fingerprint, claim.Ref)})
		}
		unique := evidence[:0]
		seen := map[string]bool{}
		for _, item := range evidence {
			if item.key == "" || seen[item.key] {
				continue
			}
			seen[item.key] = true
			unique = append(unique, item)
		}
		if len(unique) == 1 && unique[0].id == "" {
			usedFile[unique[0].key] = onlyFile
			satisfied[onlyFile] = true
		}
	}
	if textItems == 1 && desktopVisibleAnswer(msg) != "" {
		satisfied[onlyText] = true
	}
	if desktopVisibleAnswer(msg) != "" {
		for _, id := range desktopTextClaims(runID) {
			item, ok := byID[id]
			if ok && item.Kind == desktopContractText {
				satisfied[id] = true
			}
		}
	}
	if len(satisfied) == len(items) {
		return agentservice.TaskOutcomeDone, "outputs delivered"
	}
	if len(satisfied) == 0 {
		return agentservice.TaskOutcomeUnverified, "output contract open"
	}
	return agentservice.TaskOutcomePartial, "output contract incomplete"
}

func rememberContractOutcome(run *agentservice.Run, msg *agentservice.Message, items []agent.DesktopDeliverable, outcome string) {
	payload, err := json.Marshal(items)
	if err != nil {
		return
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]string{}
	}
	msg.Metadata["output_contract"] = string(payload)
	msg.Metadata["output_contract_outcome"] = outcome
	if run.Metadata == nil {
		run.Metadata = map[string]string{}
	}
	run.Metadata["output_contract"] = string(payload)
	run.Metadata["output_contract_outcome"] = outcome
}

var desktopExtractionPending sync.Map

// desktopContractWait is how long a receipt waits for an extraction that
// already started. A lone file is not completion while that read is open.
var desktopContractWait = 5 * time.Second

func noteDesktopExtractionPending(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	desktopExtractionPending.Store(runID, struct{}{})
}

func desktopExtractionUnresolved(runID string) bool {
	if _, pending := desktopExtractionPending.Load(runID); !pending {
		return false
	}
	_, resolved := desktopOutputContractOf(runID)
	return !resolved
}

func desktopWaitForContract(runID string) (desktopOutputContract, bool) {
	if contract, ok := desktopOutputContractOf(runID); ok {
		return contract, true
	}
	if _, pending := desktopExtractionPending.Load(runID); !pending {
		return desktopOutputContract{}, false
	}
	deadline := time.Now().Add(desktopContractWait)
	for time.Now().Before(deadline) {
		if contract, ok := desktopOutputContractOf(runID); ok {
			return contract, true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return desktopOutputContract{}, false
}

func desktopPlanTurn(run *agentservice.Run) bool {
	return run != nil && run.Metadata != nil && strings.TrimSpace(run.Metadata["bot_phase"]) == "plan"
}

var (
	desktopTextClaimMu     sync.Mutex
	desktopTextClaimsByRun = map[string][]string{}
)

type desktopFileClaim struct {
	ItemID      string
	Fingerprint string
	Ref         string
	Name        string
}

var (
	desktopFileClaimMu     sync.Mutex
	desktopFileClaimsByRun = map[string][]desktopFileClaim{}
)

func desktopNoteFileClaim(runID, itemID string, raw []byte, names ...string) {
	runID = strings.TrimSpace(runID)
	itemID = strings.TrimSpace(itemID)
	if runID == "" || len(raw) == 0 {
		return
	}
	fp := fileFingerprint(base64.StdEncoding.EncodeToString(raw))
	desktopFileClaimMu.Lock()
	desktopFileClaimsByRun[runID] = append(desktopFileClaimsByRun[runID], desktopFileClaim{ItemID: itemID, Fingerprint: fp, Name: firstDesktopFileClaimName(names)})
	desktopFileClaimMu.Unlock()
}

func firstDesktopFileClaimName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimSpace(names[0])
}

func desktopNoteFileRefClaim(runID, itemID, ref, name string) {
	runID = strings.TrimSpace(runID)
	itemID = strings.TrimSpace(itemID)
	ref = strings.TrimSpace(ref)
	if runID == "" || ref == "" {
		return
	}
	desktopFileClaimMu.Lock()
	desktopFileClaimsByRun[runID] = append(desktopFileClaimsByRun[runID], desktopFileClaim{ItemID: itemID, Ref: ref, Name: strings.TrimSpace(name)})
	desktopFileClaimMu.Unlock()
}

func desktopFileClaimKey(fingerprint, ref string) string {
	if strings.TrimSpace(fingerprint) != "" {
		return "bytes:" + strings.TrimSpace(fingerprint)
	}
	if strings.TrimSpace(ref) != "" {
		return "ref:" + strings.TrimSpace(ref)
	}
	return ""
}

func desktopFileClaims(runID string) []desktopFileClaim {
	desktopFileClaimMu.Lock()
	defer desktopFileClaimMu.Unlock()
	return append([]desktopFileClaim(nil), desktopFileClaimsByRun[strings.TrimSpace(runID)]...)
}

func desktopNoteTextClaims(runID string, ids []string) {
	runID = strings.TrimSpace(runID)
	if runID == "" || len(ids) == 0 {
		return
	}
	desktopTextClaimMu.Lock()
	defer desktopTextClaimMu.Unlock()
	seen := map[string]bool{}
	for _, id := range desktopTextClaimsByRun[runID] {
		seen[id] = true
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		desktopTextClaimsByRun[runID] = append(desktopTextClaimsByRun[runID], id)
	}
}

func desktopTextClaims(runID string) []string {
	desktopTextClaimMu.Lock()
	defer desktopTextClaimMu.Unlock()
	return append([]string(nil), desktopTextClaimsByRun[strings.TrimSpace(runID)]...)
}

func desktopRunTookSteps(runID string) bool {
	desktopRecStateMu.Lock()
	steps := desktopRecRunSteps[runID]
	desktopRecStateMu.Unlock()
	return steps
}

// desktopSyncVisible copies a receipt sentence into send_to_user when that
// field is still empty. An ordinary reply with no measured outcome is left
// alone: clients show send_to_user, and this must not invent one.
func desktopSyncVisible(msg *agentservice.Message) {
	if msg == nil {
		return
	}
	if msg.Metadata == nil {
		msg.Metadata = map[string]string{}
	}
	outcome := strings.TrimSpace(msg.Metadata[agentservice.TaskOutcomeKey])
	if outcome == agentservice.TaskOutcomeUnverified || outcome == agentservice.TaskOutcomePartial {
		content := strings.TrimSpace(msg.Content)
		send := strings.TrimSpace(msg.Metadata["send_to_user"])
		notice := desktopUnverifiedSentence
		if outcome == agentservice.TaskOutcomePartial {
			notice = "这件还没完全做成"
		}
		if content != "" && !strings.Contains(send, notice) {
			msg.Metadata["send_to_user"] = content
			msg.Metadata["send_to_user_kind"] = "report"
		}
		return
	}
	if strings.TrimSpace(msg.Metadata["send_to_user"]) != "" || strings.TrimSpace(msg.Content) == "" {
		return
	}
	content := strings.TrimSpace(msg.Content)
	outcome = strings.TrimSpace(msg.Metadata[agentservice.TaskOutcomeKey])
	receipt := outcome != "" || content == desktopUndeliveredSentence || strings.HasPrefix(content, "结果文件已经附在这条回复上：") || strings.HasPrefix(content, "这件我没做成")
	if !receipt {
		return
	}
	msg.Metadata["send_to_user"] = content
	if strings.TrimSpace(msg.Metadata["send_to_user_kind"]) == "" {
		msg.Metadata["send_to_user_kind"] = "report"
	}
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
		"unverified":    desktopUnverifiedSentence,
		"running":       "后台任务还在跑，我会定时查看进度并告诉你。",
	}[outcome]
	// A still-running job's why names the shell, the job id and a log tail.
	// That stays in task_outcome_evidence, not in the sentence. Other
	// outcomes keep a short cause the person can read.
	if line != "" && why != "" && outcome != "running" {
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
	asked := steps && msg != nil && msg.Metadata["response_source"] == "ask_user"
	if outcome == "" && asked {
		return "awaiting_user", "waiting for the person's answer"
	}
	// A declared finish is the turn's outcome. A missing finish is not a
	// failure: the loop already offered one finish-only step, and the
	// measured result stands with evidence "undeclared".
	switch desktopDeclaredOutcome(runID) {
	case "done":
		if outcome != "" {
			break
		}
		if !steps {
			// No desktop step: this is a chat reply, not a half-finished task.
			return "", ""
		}
		if desktopRunWorked(runID) {
			return agentservice.TaskOutcomeDone, ""
		}
		return agentservice.TaskOutcomePartial, "finish said done but the run did no successful desktop work"
	case "ask_user":
		if outcome == "" && !asked {
			return agentservice.TaskOutcomeBlocked, "finish said ask_user but this turn did not call ask_user"
		}
	case "failed":
		if outcome == "" {
			return agentservice.TaskOutcomeBlocked, "the turn declared failed"
		}
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
	if outcome == "" && steps && msg != nil && strings.TrimSpace(msg.Content) != "" && desktopRunWorked(runID) {
		if _, stopped := desktopWatchdogStopped(runID); !stopped {
			return agentservice.TaskOutcomeDone, "undeclared"
		}
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

// noDesktopTaskOutcomeMetadata measures a computerless bot's turn the way
// desktopTaskOutcomeMetadata measures a desktop one. There is no goal
// ledger and no finish action without the desktop tool, so the reply
// itself is the outcome: done when the run answered, awaiting_user when it
// stopped to ask. The stamp is what lets the service promote the reply
// into send_to_user, the sentence the feed shows (hub bot_69ee2c14,
// run_03efc35d 2026-11-16: a complete answer stored on the run never
// became a bubble, only the intake ack did).
func noDesktopTaskOutcomeMetadata(msg agentservice.Message) map[string]string {
	if strings.TrimSpace(msg.Metadata["response_source"]) == "ask_user" {
		return map[string]string{agentservice.TaskOutcomeKey: agentservice.TaskOutcomeAwaitingUser}
	}
	if strings.TrimSpace(msg.Content) != "" {
		return map[string]string{agentservice.TaskOutcomeKey: agentservice.TaskOutcomeDone}
	}
	return nil
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
	// The concrete extraction error (which may name a local path or a token)
	// lives in the contract store the extractor already wrote — this hook runs
	// at run completion, before the receipt reconcile, so it cannot rely on a
	// metadata key the reconcile only stamps later. Reading the contract
	// directly lands the cause in evidence while the generic `why` stays the
	// person-facing line.
	evidence := why
	if contract, ok := desktopOutputContractOf(runID); ok && contract.failed && strings.TrimSpace(contract.failWhy) != "" {
		evidence = "deliverable extraction failed: " + strings.TrimSpace(contract.failWhy)
	}
	if evidence != "" {
		out["task_outcome_evidence"] = desktopRuneHead(evidence, 300)
	}
	if fin, ok := desktopComposeRunFinish(runID, msg, outcome, why); ok {
		fin.Record = desktopRunFinishRecordPath(runID)
		desktopStoreRunFinish(runID, msg.Content, why, fin)
		out["outcome_status"] = fin.Status
		out["outcome_results"] = strings.Join(fin.Results, "\n")
		out["outcome_blocker"] = fin.Blocker
		out["outcome_unblock"] = strings.Join(fin.Unblock, "\n")
		out["outcome_next"] = fin.Next
		if fin.Plan != "" {
			out["outcome_plan"] = fin.Plan
		}
		if fin.Remediated {
			out["outcome_remediated"] = "true"
		}
		if fin.Record != "" {
			out["outcome_record"] = fin.Record
		}
		out["send_to_user"] = fin.Sentence()
		if out["send_to_user_kind"] == "" {
			out["send_to_user_kind"] = "report"
		}
		if fin.Next != "improve" {
			desktopLongJobReconcileRun(runID, fin.Status)
		}
	} else if sentence, ok := desktopFinishSentence(runID); ok && sentence != "" {
		out["send_to_user"] = sentence
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
			show, proved := desktopListProof(e.Host, e.URL)
			if e.Kind != "posted" || e.Run != runID || e.Detail == "submitted_unconfirmed" || !proved || seen[show] {
				continue
			}
			seen[show] = true
			if desktopReplyMentionsLink(content, show) {
				continue
			}
			line := "已确认发布：" + e.Host
			if e.Title != "" {
				line += "《" + e.Title + "》"
			}
			lines = append(lines, line+" "+show)
		}
	}
	return strings.Join(lines, "\n")
}

// desktopReplyLinkBare drops the landing query and hash. ?just_published=2
// is the same article as the path alone.
func desktopReplyLinkBare(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	return strings.TrimRight(raw, "/")
}

// desktopConfirmedArticleLink is the public article URL without its landing
// query. A creator console is not an article, so it is not announced.
func desktopConfirmedArticleLink(raw string) string {
	pub := browser.PublicPostURL(strings.TrimSpace(raw))
	if pub == "" || browser.PublishURLIsBareOrigin(pub) {
		return ""
	}
	return desktopReplyLinkBare(pub)
}

// desktopReplyMentionsLink reports whether the reply already names this
// article. A longer path that only starts with the URL is a different link.
func desktopReplyMentionsLink(content, raw string) bool {
	raw = desktopReplyLinkBare(raw)
	if raw == "" {
		return false
	}
	rest := content
	for {
		i := strings.Index(rest, raw)
		if i < 0 {
			return false
		}
		end := i + len(raw)
		if end >= len(rest) || !desktopURLContinues(rest[end]) {
			return true
		}
		rest = rest[end:]
	}
}

func desktopURLContinues(b byte) bool {
	return b == '/' || b == '%' || b == '_' || b == '-' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
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
