package guiapp

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

const petDutyTerminalTTLMS = 1600

// petDutyDecision is one edge of the desktop pet's task pose.
// The pose follows work that is still open. The cue fires once for what
// just changed. Unchanged means this note does not touch the window.
type petDutyDecision struct {
	Pose        string
	TTLMS       int
	Event       string
	Cue         string
	Line        string
	LineChanged bool
	Unchanged   bool
	// since is when the manager stored this decision. Terminal poses expire
	// from that instant. The reducer leaves it zero so decision comparisons
	// stay about the pose and the cue.
	since time.Time
}

type petPresenceFact struct {
	Text   string
	Action bool
}

type petDutyState struct {
	mu    sync.Mutex
	open  map[string]string
	facts map[string]petPresenceFact
	line  string
	cueAt time.Time
	seen  bool
}

func petDutyTerminal(status string) bool {
	switch status {
	case string(chatTaskDone), string(chatTaskFailed), string(chatTaskCanceled), string(chatTaskInterrupted):
		return true
	default:
		return false
	}
}

func petDutyOpenStatus(status string) bool {
	switch status {
	case string(chatTaskQueued), string(chatTaskRunning), string(chatTaskNeedsInput):
		return true
	default:
		return false
	}
}

func clonePetDuty(prev map[string]string) map[string]string {
	next := make(map[string]string, len(prev)+1)
	for id, status := range prev {
		next[id] = status
	}
	return next
}

// dutyDecisionStillLive is the pose a companion release may restore.
// A terminal celebration or alert keeps only the time still left. Once that
// window has passed, the pet wanders. The reminder is never played again.
func dutyDecisionStillLive(last petDutyDecision, now time.Time) petDutyDecision {
	if last.Pose == "" || last.Unchanged {
		return petDutyDecision{}
	}
	if last.TTLMS <= 0 {
		return petDutyDecision{Pose: last.Pose, Line: last.Line}
	}
	if last.since.IsZero() {
		return petDutyDecision{Pose: string(petpack.StateIdle)}
	}
	deadline := last.since.Add(time.Duration(last.TTLMS) * time.Millisecond)
	if !now.Before(deadline) {
		return petDutyDecision{Pose: string(petpack.StateIdle)}
	}
	remain := int(deadline.Sub(now).Milliseconds())
	if remain < 1 {
		remain = 1
	}
	return petDutyDecision{Pose: last.Pose, TTLMS: remain, Line: last.Line}
}

func petDutyPose(open map[string]string) string {
	if len(open) == 0 {
		return string(petpack.StateIdle)
	}
	working := false
	for _, status := range open {
		switch status {
		case string(chatTaskNeedsInput):
			// Waiting on the user. The lean is the ask, not a status icon.
			return string(petpack.StateListening)
		case string(chatTaskRunning):
			working = true
		}
	}
	if working {
		// Running work holds the still thinking pose, so she stops roaming
		// and stays with it. Queued work has not started: she stays calm,
		// and task_started is only the acknowledgement.
		return string(petpack.StateThinking)
	}
	return string(petpack.StateIdle)
}

// reducePetDuty applies one item's new status to the open set.
// hydrate keeps the pose and does not remind. A terminal status for an item
// that was not open is ignored, so a late duplicate finish cannot beep.
func reducePetDuty(prev map[string]string, id, status string, hydrate bool) (map[string]string, petDutyDecision) {
	id = strings.TrimSpace(id)
	status = strings.TrimSpace(status)
	if id == "" || status == "" {
		return prev, petDutyDecision{Unchanged: true}
	}
	old, had := "", false
	if prev != nil {
		old, had = prev[id]
	}
	if had && old == status {
		return prev, petDutyDecision{Unchanged: true}
	}
	if !had && petDutyTerminal(status) {
		return prev, petDutyDecision{Unchanged: true}
	}
	if !had && !petDutyOpenStatus(status) && !petDutyTerminal(status) {
		return prev, petDutyDecision{Unchanged: true}
	}
	next := clonePetDuty(prev)
	if petDutyTerminal(status) || !petDutyOpenStatus(status) {
		delete(next, id)
	} else {
		next[id] = status
	}
	decision := petDutyDecision{Pose: petDutyPose(next)}
	if len(next) == 0 {
		switch status {
		case string(chatTaskFailed):
			decision.Pose = string(petpack.StateAlert)
			decision.TTLMS = petDutyTerminalTTLMS
		case string(chatTaskDone):
			decision.Pose = string(petpack.StateDone)
			decision.TTLMS = petDutyTerminalTTLMS
		default:
			decision.Pose = string(petpack.StateIdle)
		}
	}
	if hydrate {
		return next, decision
	}
	switch {
	case status == string(chatTaskNeedsInput) && old != string(chatTaskNeedsInput):
		decision.Event = "task_needs_you"
		decision.Cue = "notice"
	case !had && petDutyOpenStatus(status):
		decision.Event = "task_started"
		decision.Cue = "focus"
	case had && status == string(chatTaskFailed):
		decision.Event = "task_failed"
		decision.Cue = "concern"
	case had && status == string(chatTaskDone):
		decision.Event = "task_done"
		decision.Cue = "celebrate"
	}
	return next, decision
}

type petDutyFold struct {
	open  map[string]string
	facts map[string]petPresenceFact
	line  string
}

// foldPetDuty records one duty note and the one sentence hover may show.
// A repeated status can still change that sentence. The pose and the cue
// stay with reducePetDuty.
func foldPetDuty(prev petDutyFold, id, status, fact string, action, hydrate bool) (petDutyFold, petDutyDecision) {
	id = strings.TrimSpace(id)
	status = strings.TrimSpace(status)
	nextOpen, decision := reducePetDuty(prev.open, id, status, hydrate)
	if id == "" || status == "" || !petDutyNoteAccepted(prev.open, id, status, decision) {
		return prev, decision
	}
	next := petDutyFold{open: nextOpen, facts: clonePetFacts(prev.facts)}
	if decision.Unchanged {
		next.open = prev.open
	}
	if text := sanitizePresenceFact(fact); text != "" {
		next.facts[id] = petPresenceFact{Text: text, Action: action}
	} else {
		delete(next.facts, id)
	}
	if !decision.Unchanged {
		for fid := range next.facts {
			if _, ok := next.open[fid]; !ok {
				delete(next.facts, fid)
			}
		}
	}
	pose := decision.Pose
	if decision.Unchanged {
		pose = petDutyPose(next.open)
	}
	line := prev.line
	if !(decision.Unchanged && len(next.open) == 0) {
		line = petPresenceLine(next.open, next.facts, pose)
	}
	next.line = line
	decision.Line = line
	decision.LineChanged = line != prev.line
	return next, decision
}

func petDutyNoteAccepted(open map[string]string, id, status string, decision petDutyDecision) bool {
	if !decision.Unchanged {
		return true
	}
	if open == nil {
		return false
	}
	old, had := open[id]
	return had && old == status
}

func clonePetFacts(prev map[string]petPresenceFact) map[string]petPresenceFact {
	next := make(map[string]petPresenceFact, len(prev)+1)
	for id, fact := range prev {
		next[id] = fact
	}
	return next
}

// petPresenceFactFor picks the one true phrase a task already has.
// A running progress step is the current action. A title is only a name.
// Waiting and queued work do not reuse an old step.
func petPresenceFactFor(status, title, progress string) (string, bool) {
	if status == string(chatTaskRunning) {
		if text := sanitizePresenceFact(progress); text != "" {
			return text, true
		}
	}
	if text := sanitizePresenceFact(title); text != "" {
		return text, false
	}
	return "", false
}

func sanitizePresenceFact(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

const petPresenceRunes = 32

func clipPresence(text string) string {
	text = sanitizePresenceFact(text)
	if text == "" || utf8.RuneCountInString(text) <= petPresenceRunes {
		return text
	}
	return string([]rune(text)[:petPresenceRunes-1]) + "…"
}

// petPresenceLine is the single hover sentence. It follows the same priority
// as the pose: she needs you, else she is working, else the work has not
// started. An empty open set uses the settle pose. It never invents a step.
func petPresenceLine(open map[string]string, facts map[string]petPresenceFact, pose string) string {
	if len(open) == 0 {
		switch petpack.NormalizeState(pose) {
		case petpack.StateAlert:
			return "这件没做成"
		case petpack.StateDone:
			return "做好了"
		default:
			return ""
		}
	}
	status := ""
	switch {
	case petDutyHas(open, string(chatTaskNeedsInput)):
		status = string(chatTaskNeedsInput)
	case petDutyHas(open, string(chatTaskRunning)):
		status = string(chatTaskRunning)
	case petDutyHas(open, string(chatTaskQueued)):
		status = string(chatTaskQueued)
	default:
		return ""
	}
	id := petPresencePrimary(open, facts, status)
	fact := petPresenceFact{}
	if facts != nil {
		fact = facts[id]
	}
	n := petDutyCount(open, status)
	if fact.Text != "" {
		switch status {
		case string(chatTaskNeedsInput):
			return clipPresence("要你看一下：" + fact.Text)
		case string(chatTaskQueued):
			return clipPresence("还没开始：" + fact.Text)
		default:
			if fact.Action {
				return clipPresence(fact.Text)
			}
			return clipPresence("正在做：" + fact.Text)
		}
	}
	switch status {
	case string(chatTaskNeedsInput):
		if n > 1 {
			return fmt.Sprintf("有 %d 件事要你看", n)
		}
		return "要你看一下"
	case string(chatTaskQueued):
		if n > 1 {
			return fmt.Sprintf("有 %d 件事还没开始", n)
		}
		return "还没开始"
	default:
		if n > 1 {
			return fmt.Sprintf("正在做 %d 件事", n)
		}
		return "正在做"
	}
}

func petDutyHas(open map[string]string, status string) bool {
	for _, item := range open {
		if item == status {
			return true
		}
	}
	return false
}

func petDutyCount(open map[string]string, status string) int {
	n := 0
	for _, item := range open {
		if item == status {
			n++
		}
	}
	return n
}

func petPresencePrimary(open map[string]string, facts map[string]petPresenceFact, status string) string {
	best := ""
	bestRank := 3
	for id, item := range open {
		if item != status {
			continue
		}
		rank := 2
		if facts != nil {
			if fact, ok := facts[id]; ok && fact.Text != "" {
				if fact.Action {
					rank = 0
				} else {
					rank = 1
				}
			}
		}
		if best == "" || rank < bestRank || (rank == bestRank && id < best) {
			best = id
			bestRank = rank
		}
	}
	return best
}

// petPresenceTipPoint places the one-line hover note above the pet, or below
// her when the work area has no room above. The note stays off her body: a
// work-area clamp that would cover her is refused, and the note may sit
// slightly outside the work area instead. ok is false when the point does
// not fit in a 16-bit tooltip coordinate, or when every placement covers her.
func petPresenceTipPoint(petLeft, petTop, petRight, petBottom, tipW, tipH, workLeft, workTop, workRight, workBottom int) (x, y int, ok bool) {
	if tipW < 1 {
		tipW = 1
	}
	if tipH < 1 {
		tipH = 1
	}
	petW := petRight - petLeft
	petH := petBottom - petTop
	x = petLeft + (petW-tipW)/2
	above := petTop - tipH - 8
	below := petBottom + 8
	hasWork := workRight > workLeft && workBottom > workTop
	y = petPresenceTipSide(above, below, tipH, workTop, workBottom, hasWork)
	if hasWork && tipW <= workRight-workLeft {
		if x < workLeft {
			x = workLeft
		}
		if x+tipW > workRight {
			x = workRight - tipW
		}
	}
	if hasWork {
		clamped := y
		if clamped+tipH > workBottom {
			clamped = workBottom - tipH
		}
		if clamped < workTop {
			clamped = workTop
		}
		if !petPresenceCoversPet(x, clamped, tipW, tipH, petLeft, petTop, petW, petH) {
			y = clamped
		}
	}
	if petPresenceCoversPet(x, y, tipW, tipH, petLeft, petTop, petW, petH) {
		y = above
	}
	if petPresenceCoversPet(x, y, tipW, tipH, petLeft, petTop, petW, petH) {
		return 0, 0, false
	}
	if x < -32768 || y < -32768 || x > 32767 || y > 32767 {
		return 0, 0, false
	}
	return x, y, true
}

// petPresenceTipSide picks a side that does not cover the pet. Above is the
// usual place. Below is used when above does not fit in the work area and
// below does. When neither fits, the side that keeps more of the note inside
// the work area wins. Both candidates already sit fully above or fully below.
func petPresenceTipSide(above, below, tipH, workTop, workBottom int, hasWork bool) int {
	if !hasWork {
		return above
	}
	aboveFits := above >= workTop && above+tipH <= workBottom
	belowFits := below >= workTop && below+tipH <= workBottom
	switch {
	case aboveFits:
		return above
	case belowFits:
		return below
	case petPresenceVisibleSpan(below, tipH, workTop, workBottom) > petPresenceVisibleSpan(above, tipH, workTop, workBottom):
		return below
	default:
		return above
	}
}

func petPresenceVisibleSpan(y, h, workTop, workBottom int) int {
	top := y
	bot := y + h
	if top < workTop {
		top = workTop
	}
	if bot > workBottom {
		bot = workBottom
	}
	if bot <= top {
		return 0
	}
	return bot - top
}

func petPresenceCoversPet(tipX, tipY, tipW, tipH, petLeft, petTop, petW, petH int) bool {
	if tipW < 1 || tipH < 1 || petW < 1 || petH < 1 {
		return false
	}
	return tipX < petLeft+petW && tipX+tipW > petLeft && tipY < petTop+petH && tipY+tipH > petTop
}

func desktopPetDutySession(h *IMMessageHandler, userID string) bool {
	userID = strings.TrimSpace(userID)
	if h == nil || userID == "" || !chatTaskSessionAllowed(userID, false) {
		return false
	}
	return !h.sessionIsCodingWorkbench(userID)
}

func desktopPetTurnCounts(h *IMMessageHandler, userID string, ctx *LoopContext) bool {
	if !desktopPetDutySession(h, userID) {
		return false
	}
	if ctx != nil && (ctx.Kind == LoopKindBackground || strings.TrimSpace(ctx.ChatTaskID) != "") {
		return false
	}
	return true
}

func (a *App) notePetDuty(id, status string, hydrate bool) {
	a.notePetDutyWithLine(id, status, "", false, hydrate)
}

func (a *App) notePetDutyWithLine(id, status, fact string, action, hydrate bool) {
	if a == nil {
		return
	}
	a.petDuty.mu.Lock()
	next, decision := foldPetDuty(petDutyFold{
		open: a.petDuty.open, facts: a.petDuty.facts, line: a.petDuty.line,
	}, id, status, fact, action, hydrate)
	a.petDuty.open = next.open
	a.petDuty.facts = next.facts
	a.petDuty.line = next.line
	if decision.Cue != "" {
		a.petDuty.cueAt = time.Now()
	}
	a.petDuty.seen = true
	a.petDuty.mu.Unlock()
	if decision.Unchanged && !decision.LineChanged {
		return
	}
	a.pushPetDuty(decision)
}

func dutyCueAllowed(app *App, soundEnabled, quiet, reduced bool) bool {
	if !soundEnabled || quiet || reduced {
		return false
	}
	if app == nil {
		return true
	}
	fa := app.existingFloatingAssistant()
	if fa != nil && fa.companion != nil && fa.companion.PanelMicBusy() {
		return false
	}
	return true
}

func (a *App) petDutyCueRecent() bool {
	if a == nil {
		return false
	}
	a.petDuty.mu.Lock()
	defer a.petDuty.mu.Unlock()
	return !a.petDuty.cueAt.IsZero() && time.Since(a.petDuty.cueAt) < time.Second
}

func (a *App) pushPetDuty(decision petDutyDecision) {
	if a == nil || (decision.Unchanged && !decision.LineChanged) {
		return
	}
	fa := a.existingFloatingAssistant()
	if fa == nil {
		return
	}
	fa.applyDuty(decision)
}

func (a *App) hydratePetDutyFromRuntime() {
	if a == nil || a.imHandler == nil {
		return
	}
	h := a.imHandler
	h.sessionLoops.Range(func(key, value any) bool {
		userID, _ := key.(string)
		state, _ := value.(*sessionLoopState)
		if state == nil || !desktopPetTurnCounts(h, userID, nil) {
			return true
		}
		state.stateMu.RLock()
		ctx := state.loopCtx
		state.stateMu.RUnlock()
		if ctx == nil || ctx.IsDone() || ctx.IsCancelled() || ctx.Kind == LoopKindBackground || strings.TrimSpace(ctx.ChatTaskID) != "" {
			return true
		}
		a.notePetDuty("turn:"+userID, string(chatTaskRunning), true)
		return true
	})
	if reg := h.peekChatTasks(); reg != nil {
		for _, item := range reg.dutyOpen() {
			fact, action := petPresenceFactFor(item.Status, item.Title, item.Progress)
			a.notePetDutyWithLine("task:"+item.ID, item.Status, fact, action, true)
		}
	}
}

func (h *IMMessageHandler) noteDesktopPetTurnStart(userID string, ctx *LoopContext) {
	if h == nil || h.app == nil || !desktopPetTurnCounts(h, userID, ctx) {
		return
	}
	h.app.notePetDuty("turn:"+strings.TrimSpace(userID), string(chatTaskRunning), false)
}

func (h *IMMessageHandler) noteDesktopPetTurnEnd(userID string, ctx *LoopContext, result *IMAgentResponse) {
	if h == nil || h.app == nil || !desktopPetTurnCounts(h, userID, ctx) {
		return
	}
	status := string(chatTaskDone)
	switch {
	case ctx != nil && (ctx.UserCancelled() || ctx.IsCancelled()):
		status = string(chatTaskCanceled)
	case result != nil && (trayResponseNeedsConfirm(result) || trayResponseWaitsForReply(result)):
		status = string(chatTaskNeedsInput)
	case result != nil && strings.TrimSpace(result.Error) != "":
		status = string(chatTaskFailed)
	}
	h.app.notePetDuty("turn:"+strings.TrimSpace(userID), status, false)
}

func (h *IMMessageHandler) noteDesktopPetTurnCanceled(userID string) {
	if h == nil || h.app == nil || !desktopPetDutySession(h, userID) {
		return
	}
	h.app.notePetDuty("turn:"+strings.TrimSpace(userID), string(chatTaskCanceled), false)
}
