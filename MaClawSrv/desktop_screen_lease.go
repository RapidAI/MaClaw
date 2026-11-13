package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Screen lease (Daniel 11-12 06:29 「改」).
//
// One bot can run several tasks at once. Their shell, file, training and code
// steps run side by side, but they share one screen and one browser: two runs
// clicking, typing or switching tabs at the same time wreck each other's flow.
// So a step that uses the screen or the browser first takes the lease of that
// screen (the desktop user key: an isolated bot's own display, or the owner's
// shared desktop). Another run of the same screen waits in FIFO order; its
// non-screen steps never wait.
//
// Granularity: the lease belongs to the RUN, not to a single click. Once a
// run holds it, its next screen steps go straight through, so a login, a
// form or a publish flow is not interleaved with another task between
// steps. It passes on when:
//   - the run ends (round finish or the run's receipt), or
//   - another run is waiting and the holder has had no screen step in flight
//     for screenLeaseIdle (it is thinking for long or doing non-screen work), or
//   - another run is waiting and the holder has held it for screenLeaseMaxHold
//     (taken only between its steps, never mid-step), or
//   - the holder's step has been in flight for screenLeaseStale (a hung or
//     crashed step must not keep the screen forever).
// A waiter gives up after screenLeaseMaxWait with an error the model reads.
// A run that gets the screen back after another task used it is told to
// look again before reusing earlier refs.
// MACLAW_SCREEN_LEASE=off turns the lease off.

var (
	screenLeaseIdle    = desktopEnvSeconds("MACLAW_SCREEN_LEASE_IDLE_SECONDS", 120*time.Second)
	screenLeaseMaxHold = desktopEnvMinutes("MACLAW_SCREEN_LEASE_MAX_HOLD_MINUTES", 15*time.Minute)
	screenLeaseStale   = desktopEnvMinutes("MACLAW_SCREEN_LEASE_STALE_MINUTES", 20*time.Minute)
	screenLeaseMaxWait = desktopEnvMinutes("MACLAW_SCREEN_LEASE_WAIT_MINUTES", 30*time.Minute)
	screenLeaseTick    = 2 * time.Second
	screenLeaseNow     = time.Now
)

// desktopScreenWaitPhrase is the working-row text while a run waits.
const desktopScreenWaitPhrase = "正在等待屏幕"

// desktopScreenQueueSentence is the queued state a person and Hub can read.
// It is not a timeout to lengthen.
const desktopScreenQueueSentence = "正在等待屏幕，这个 bot 的另一个任务正在用"

var desktopScreenHalts sync.Map // run id -> *agentservice.RunHaltedError

// desktopScreenLeaseOn is false in this package's tests (most tests start
// many runs on one user without ending them); the lease tests turn it on.
var desktopScreenLeaseOn = true

func desktopScreenLeaseEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_SCREEN_LEASE")))
	return desktopScreenLeaseOn && v != "off" && v != "0" && v != "false"
}

// desktopScreenLeaseNonScreen are the actions that never touch the screen or
// the browser. Every other action (including an unknown one) takes the lease.
var desktopScreenLeaseNonScreen = map[string]bool{
	"file_read": true, "file_write": true, "file_edit": true, "file_list": true,
	"bash": true, "install": true, "http": true, "deliver": true, "skill_list": true,
	"job_status": true, "job_list": true, "request_takeover": true,
	"notify_main": true, "report": true, "say": true, "check_inbox": true, "delegate": true,
	"list_assignments": true, "accept_result": true, "return_result": true,
	"shop_compare_offers": true, "proxy_mode": true, "recover": true, "goal_check": true, "finish": true,
}

// desktopScreenLeaseNeeded reports a step that uses the screen or browser.
// A bash that starts a GUI program draws on the screen too.
func desktopScreenLeaseNeeded(action string, args map[string]any) bool {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "" {
		return false
	}
	if action == "bash" {
		return desktopBashLaunchesGUI(desktopArg(args, "command"))
	}
	return !desktopScreenLeaseNonScreen[action]
}

type screenLeaseWaiter struct {
	run     string
	ch      chan struct{}
	granted bool
	aborted bool
	since   time.Time
}

type screenLease struct {
	holder        string
	since         time.Time // holder got it
	lastUse       time.Time // holder's last screen step ended (or began)
	inflight      int
	inflightSince time.Time
	prev          string          // holder before the current one
	held          map[string]bool // runs that held this screen
	waiters       []*screenLeaseWaiter
}

type screenLeases struct {
	mu     sync.Mutex
	leases map[string]*screenLease
	// waiting: run id -> screen key, for the status line.
	waiting map[string]string
}

var desktopScreenLeases = &screenLeases{leases: map[string]*screenLease{}, waiting: map[string]string{}}

func (m *screenLeases) heldBy(key, run string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.leases[key]
	return l != nil && l.holder == run
}

func (m *screenLeases) get(key string) *screenLease {
	l := m.leases[key]
	if l == nil {
		l = &screenLease{held: map[string]bool{}}
		m.leases[key] = l
	}
	return l
}

// gc drops an idle entry so the map does not grow with old screens.
func (m *screenLeases) gc(key string, l *screenLease) {
	if l.holder == "" && len(l.waiters) == 0 && len(l.held) == 0 {
		delete(m.leases, key)
	}
}

// promote hands the screen to the first waiter when it is free or the holder
// may be passed over. Called with m.mu held.
func (m *screenLeases) promote(key string, l *screenLease, now time.Time) {
	if len(l.waiters) == 0 {
		return
	}
	if l.holder != "" {
		why := ""
		switch {
		case l.inflight > 0 && now.Sub(l.inflightSince) >= screenLeaseStale:
			why = "stale_step"
		case l.inflight == 0 && now.Sub(l.lastUse) >= screenLeaseIdle:
			why = "idle"
		case l.inflight == 0 && now.Sub(l.since) >= screenLeaseMaxHold:
			why = "max_hold"
		}
		if why == "" {
			return
		}
		log.Printf("[screen-lease] screen=%s passed from run=%s reason=%s held=%s waiting=%d", screenLeaseKeyLabel(key), l.holder, why, now.Sub(l.since).Round(time.Second), len(l.waiters))
		l.prev, l.holder, l.inflight = l.holder, "", 0
	}
	head := l.waiters[0]
	l.holder, l.since, l.lastUse = head.run, now, now
	l.held[head.run] = true
	kept := l.waiters[:0]
	for _, w := range l.waiters {
		if w.run == head.run {
			w.granted = true
			if l.inflight == 0 {
				l.inflightSince = now
			}
			l.inflight++
			delete(m.waiting, w.run)
			close(w.ch)
			continue
		}
		kept = append(kept, w)
	}
	l.waiters = kept
}

// screenLeaseKeyLabel keeps log lines short; the key is no secret.
func screenLeaseKeyLabel(key string) string {
	if len(key) > 40 {
		return key[:40]
	}
	return key
}

// acquire takes the screen for one step of run. It returns the release for
// that step, how long the run waited, and a note for the model when another
// task used the screen since this run's last look.
func (m *screenLeases) acquire(ctx context.Context, key, run string, onWait func(holder string, pos int)) (func(), time.Duration, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	start := screenLeaseNow()
	m.mu.Lock()
	l := m.get(key)
	if l.holder == run {
		if l.inflight == 0 {
			l.inflightSince = start
		}
		l.inflight++
		m.mu.Unlock()
		return m.releaser(key, run), 0, "", nil
	}
	if l.holder == "" && len(l.waiters) == 0 {
		prev := l.prev
		note := m.takeLocked(l, run, start)
		m.mu.Unlock()
		log.Printf("[screen-lease] screen=%s run=%s acquired (free; previous holder %q)", screenLeaseKeyLabel(key), run, prev)
		return m.releaser(key, run), 0, note, nil
	}
	w := &screenLeaseWaiter{run: run, ch: make(chan struct{}), since: start}
	l.waiters = append(l.waiters, w)
	m.waiting[run] = key
	m.promote(key, l, start)
	if w.granted {
		note := m.grantedNoteLocked(l, run, 0)
		m.mu.Unlock()
		return m.releaser(key, run), 0, note, nil
	}
	holder, pos := l.holder, len(l.waiters)
	m.mu.Unlock()
	desktopScreenQueues.Store(run, desktopScreenQueueSentence)
	log.Printf("[screen-lease] screen=%s run=%s waits: run=%s has the screen, queue position %d", screenLeaseKeyLabel(key), run, holder, pos)
	if onWait != nil {
		onWait(holder, pos)
	}
	tick := time.NewTicker(screenLeaseTick)
	defer tick.Stop()
	for {
		select {
		case <-w.ch:
			waited := screenLeaseNow().Sub(start)
			desktopScreenQueues.Delete(run)
			m.mu.Lock()
			if w.aborted {
				m.mu.Unlock()
				return func() {}, waited, "", fmt.Errorf("screen_busy: this run ended while it waited for the screen")
			}
			note := m.grantedNoteLocked(m.get(key), run, waited)
			m.mu.Unlock()
			log.Printf("[screen-lease] screen=%s run=%s acquired after waiting %s", screenLeaseKeyLabel(key), run, waited.Round(time.Second))
			return m.releaser(key, run), waited, note, nil
		case <-ctx.Done():
			waited := screenLeaseNow().Sub(start)
			m.abandon(key, w)
			log.Printf("[screen-lease] screen=%s run=%s stopped waiting (%v) after %s", screenLeaseKeyLabel(key), run, ctx.Err(), waited.Round(time.Second))
			return func() {}, waited, "", ctx.Err()
		case <-tick.C:
			now := screenLeaseNow()
			m.mu.Lock()
			l := m.get(key)
			m.promote(key, l, now)
			granted := w.granted
			timedOut := !granted && now.Sub(start) >= screenLeaseMaxWait
			holder := l.holder
			m.mu.Unlock()
			if granted {
				continue // the closed channel answers on the next turn
			}
			if timedOut {
				m.abandon(key, w)
				desktopScreenQueues.Delete(run)
				log.Printf("[screen-lease] screen=%s run=%s gave up after %s: run=%s still has the screen", screenLeaseKeyLabel(key), run, now.Sub(start).Round(time.Second), holder)
				halt := &agentservice.RunHaltedError{Code: "screen_busy", Sentence: desktopScreenQueueSentence}
				desktopScreenHalts.Store(run, halt)
				return func() {}, now.Sub(start), "", halt
			}
		}
	}
}

// takeLocked gives a free screen to run.
func (m *screenLeases) takeLocked(l *screenLease, run string, now time.Time) string {
	l.holder, l.since, l.lastUse, l.inflight, l.inflightSince = run, now, now, 1, now
	note := ""
	if l.held[run] && l.prev != "" && l.prev != run {
		note = desktopScreenLeaseNote(0)
	}
	l.held[run] = true
	return note
}

func (m *screenLeases) grantedNoteLocked(l *screenLease, run string, waited time.Duration) string {
	if l.prev != "" && l.prev != run && (waited > 0 || l.held[run]) {
		return desktopScreenLeaseNote(waited)
	}
	return ""
}

func desktopScreenLeaseNote(waited time.Duration) string {
	if waited >= time.Second {
		return fmt.Sprintf("(another task of this bot was using the screen; this step waited %ds for it. Windows, tabs or the page may have changed since your last look: check the screen before reusing earlier refs or coordinates.)\n", int(waited.Round(time.Second).Seconds()))
	}
	return "(another task of this bot used the screen since your last look. Windows, tabs or the page may have changed: check the screen before reusing earlier refs or coordinates.)\n"
}

// abandon removes a waiter that stopped waiting. A grant that raced with the
// stop is handed back.
func (m *screenLeases) abandon(key string, w *screenLeaseWaiter) {
	if w != nil {
		desktopScreenQueues.Delete(w.run)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.get(key)
	if w.granted {
		if l.holder == w.run && l.inflight > 0 {
			l.inflight--
			l.lastUse = screenLeaseNow()
			if l.inflight == 0 && len(l.waiters) > 0 {
				// It never used the screen: the next waiter need not wait out the idle time.
				l.prev, l.holder = l.holder, ""
			}
		}
	} else {
		kept := l.waiters[:0]
		for _, x := range l.waiters {
			if x != w {
				kept = append(kept, x)
			}
		}
		l.waiters = kept
	}
	if m.waiting[w.run] == key {
		delete(m.waiting, w.run)
	}
	m.promote(key, l, screenLeaseNow())
	m.gc(key, l)
}

// releaser ends one step. The run keeps the screen; it passes on by the
// rules above. A step that was passed over (stale) changes nothing.
func (m *screenLeases) releaser(key, run string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			l := m.get(key)
			if l.holder != run {
				return
			}
			if l.inflight > 0 {
				l.inflight--
			}
			now := screenLeaseNow()
			l.lastUse = now
			m.promote(key, l, now)
		})
	}
}

// endRun frees every screen the run holds and drops its waits. Safe to call
// more than once and for a run that never used the screen.
func (m *screenLeases) endRun(run, why string) {
	run = strings.TrimSpace(run)
	if run == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := screenLeaseNow()
	delete(m.waiting, run)
	for key, l := range m.leases {
		if l.holder == run {
			log.Printf("[screen-lease] screen=%s run=%s released (%s) after %s, waiting=%d", screenLeaseKeyLabel(key), run, why, now.Sub(l.since).Round(time.Second), len(l.waiters))
			l.prev, l.holder, l.inflight = run, "", 0
		}
		kept := l.waiters[:0]
		for _, w := range l.waiters {
			if w.run == run {
				w.aborted = true
				close(w.ch)
				continue
			}
			kept = append(kept, w)
		}
		l.waiters = kept
		delete(l.held, run)
		if l.prev == run && l.holder == "" && len(l.waiters) == 0 {
			l.prev = ""
		}
		m.promote(key, l, now)
		m.gc(key, l)
	}
}

// waitingFor reports whether run is queued for a screen, and who holds it.
func (m *screenLeases) waitingFor(run string) (string, int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.waiting[strings.TrimSpace(run)]
	if !ok {
		return "", 0, false
	}
	l := m.leases[key]
	if l == nil {
		return "", 0, false
	}
	for i, w := range l.waiters {
		if w.run == run {
			return l.holder, i + 1, true
		}
	}
	return l.holder, 0, true
}

// desktopScreenLeaseEndRun is called when a run ends (or its round finishes).
func desktopScreenLeaseEndRun(runID, why string) {
	desktopScreenLeases.endRun(runID, why)
	runID = strings.TrimSpace(runID)
	desktopScreenHalts.Delete(runID)
	desktopScreenQueues.Delete(runID)
}

// desktopScreenLeaseStatus is the status-line text of a run that waits.
var desktopScreenQueues sync.Map // run id -> sentence

func desktopScreenQueue(runID string) string {
	v, ok := desktopScreenQueues.Load(strings.TrimSpace(runID))
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func init() {
	agentservice.RunHaltReason = func(runID string) error {
		v, ok := desktopScreenHalts.Load(strings.TrimSpace(runID))
		if !ok {
			return nil
		}
		err, _ := v.(error)
		return err
	}
	agentservice.ResumeProbe = desktopResumeProbe
	agentservice.RunEnded = func(runID string) {
		runID = strings.TrimSpace(runID)
		desktopScreenHalts.Delete(runID)
		desktopScreenQueues.Delete(runID)
	}
}

func desktopResumeProbe(ctx context.Context, tenantID, userID, instanceID, sessionID, runID string, call agentservice.RestartInFlight) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(call.Args), &args)
	action := strings.ToLower(strings.TrimSpace(desktopArg(args, "action")))
	if action == "bash" {
		return desktopBashStateProbe(ctx, call)
	}
	scope := agentruntime.Scope{TenantID: tenantID, UserID: userID, InstanceID: instanceID, SessionID: sessionID, RunID: runID}
	out, err := operateDesktop(ctx, scope, map[string]any{"action": "probe"})
	if err != nil {
		return "probe: " + err.Error()
	}
	return out
}

func desktopBashStateProbe(ctx context.Context, call agentservice.RestartInFlight) string {
	j := desktopLongJobForToolCall(call.ID)
	if j == nil {
		// A foreground bash has no job row. The result stays outcome unknown.
		return ""
	}
	out, err := desktopLongJobRun(ctx, desktopLongJobScope(j), desktopLongJobPollScript(j))
	if err != nil {
		return "bash poll failed: " + err.Error()
	}
	p := desktopLongJobParsePoll(out)
	exit := "-"
	if p.ExitCode != nil {
		exit = strconv.Itoa(*p.ExitCode)
	}
	return fmt.Sprintf("bash job %s alive=%t exit=%s", j.ID, p.Alive, exit)
}

func desktopScreenLeaseStatus(runID string) string {
	_, pos, ok := desktopScreenLeases.waitingFor(runID)
	if !ok {
		return ""
	}
	if pos > 1 {
		return fmt.Sprintf("等待屏幕：另一任务正在使用（排第 %d）", pos)
	}
	return "等待屏幕：另一任务正在使用"
}

// desktopMemoryPressured reports whether this desktop's container is over
// the admission line. Nil means the stat is unknown and the lease proceeds.
// The second result is the status line shown while the lease waits.
var desktopMemoryPressured func(ctx context.Context, tenantID, userID string) (bool, string)

// desktopRemoteMemory is set by tests. Production asks Hub, which asks desktopd.
var desktopRemoteMemory func(ctx context.Context, tenantID, userID string) (used, limit int64, err error)

// readDesktopMemoryPressure is the production gauge. A missing Hub, or a
// read that fails, is not pressure: the lease proceeds.
func readDesktopMemoryPressure(ctx context.Context, tenantID, userID string) (bool, string) {
	var used, limit int64
	var err error
	switch {
	case desktopRemoteMemory != nil:
		used, limit, err = desktopRemoteMemory(ctx, tenantID, userID)
	default:
		client := desktopHubClientFromEnv()
		if client == nil {
			return false, ""
		}
		used, limit, err = client.Memory(ctx, tenantID, userID)
	}
	if err != nil || !desktop.MemoryPressured(used, limit) {
		return false, ""
	}
	return true, desktopMemoryWaitLine
}

// desktopMemoryRecheck is how often a waiting lease re-reads the gauge.
var desktopMemoryRecheck = 2 * time.Second

// desktopMemoryWaitMax is how long a new admission waits for memory. After
// that the step fails instead of waiting for the rest of the run.
var desktopMemoryWaitMax = 2 * time.Minute

const desktopMemoryWaitLine = "这台电脑内存紧张，这项先排着，空出来再做。"

const desktopMemoryGiveUpLine = desktop.MemoryBusyOutcome

// waitDesktopMemory holds a new screen lease until the container is under
// the line. Cancelling the run ends the wait. The wait has a ceiling: past
// it the admission fails with a sentence the person can read.
func waitDesktopMemory(ctx context.Context, tenantID, userID, runID string) error {
	if desktopMemoryPressured == nil {
		return nil
	}
	deadline := time.Now().Add(desktopMemoryWaitMax)
	for {
		pressed, line := desktopMemoryPressured(ctx, tenantID, userID)
		if !pressed {
			return nil
		}
		if strings.TrimSpace(line) == "" {
			line = desktopMemoryWaitLine
		}
		_ = line
		if !time.Now().Before(deadline) {
			return &desktop.MemoryAdmission{Sentence: desktopMemoryGiveUpLine}
		}
		// The working row only accepts a short phrase. The full sentence is
		// the queue acknowledgement the person sees.
		setDesktopActivity(runID, "正在等待内存")
		timer := time.NewTimer(desktopMemoryRecheck)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// desktopScreenLeaseStep takes the screen for one desktop step of a run when
// that step needs it. The returned release must run when the step returns.
func desktopScreenLeaseStep(ctx context.Context, key, runID, action string, args map[string]any) (func(), string, error) {
	runID, key = strings.TrimSpace(runID), strings.TrimSpace(key)
	if runID == "" || key == "" || !desktopScreenLeaseEnabled() || !desktopScreenLeaseNeeded(action, args) {
		return func() {}, "", nil
	}
	prevActivity := loadDesktopActivity(runID)
	waitedOnce := false
	release, waited, note, err := desktopScreenLeases.acquire(ctx, key, runID, func(holder string, pos int) {
		waitedOnce = true
		setDesktopActivity(runID, desktopScreenWaitPhrase)
	})
	if waitedOnce && loadDesktopActivity(runID) == desktopScreenWaitPhrase {
		if prevActivity == "" || prevActivity == desktopScreenWaitPhrase {
			prevActivity = desktopActivityThinking
		}
		setDesktopActivity(runID, prevActivity)
	}
	if waited > 0 {
		// Queueing for the screen is not a stall of this run.
		desktopWatchdogCredit(runID, waited)
	}
	return release, note, err
}

// desktopScreenKey names the screen a step of this scope uses: the isolated
// bot's own display, or the owner's shared desktop ("" when unknown).
func desktopScreenKey(scope agentruntime.Scope) string {
	key, err := desktopUserKey(desktopOwner(scope))
	if err != nil {
		return ""
	}
	return key
}
