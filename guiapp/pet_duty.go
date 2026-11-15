package guiapp

import (
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

const petDutyTerminalTTLMS = 1600

// petDutyDecision is one edge of the desktop pet's task pose.
// The pose follows work that is still open. The cue fires once for what
// just changed. Unchanged means this note does not touch the window.
type petDutyDecision struct {
	Pose      string
	TTLMS     int
	Event     string
	Cue       string
	Unchanged bool
	// since is when the manager stored this decision. Terminal poses expire
	// from that instant. The reducer leaves it zero so decision comparisons
	// stay about the pose and the cue.
	since time.Time
}

type petDutyState struct {
	mu    sync.Mutex
	open  map[string]string
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
		return petDutyDecision{Pose: last.Pose}
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
	return petDutyDecision{Pose: last.Pose, TTLMS: remain}
}

func petDutyPose(open map[string]string) string {
	if len(open) == 0 {
		return string(petpack.StateIdle)
	}
	for _, status := range open {
		if status == string(chatTaskNeedsInput) {
			return string(petpack.StateListening)
		}
	}
	return string(petpack.StateThinking)
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
	if a == nil {
		return
	}
	a.petDuty.mu.Lock()
	next, decision := reducePetDuty(a.petDuty.open, id, status, hydrate)
	a.petDuty.open = next
	if decision.Cue != "" {
		a.petDuty.cueAt = time.Now()
	}
	a.petDuty.seen = true
	a.petDuty.mu.Unlock()
	if decision.Unchanged {
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
	if a == nil || decision.Unchanged {
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
			a.notePetDuty("task:"+item[0], item[1], true)
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
