package guiapp

import (
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

// petDragThresholdPx matches the press distance Windows, macOS, and Linux
// already treat as a drag. A shorter move can still resolve as a click.
const petDragThresholdPx = 5

// Click credit is rearing time added to the same loaded-ms counter the stage
// planner already reads. One gesture stays under the minimum legal stage
// (60s) so a single click cannot skip a stage by itself.
const (
	petClickSingleCreditMS int64 = 5_000
	petClickDoubleCreditMS int64 = 10_000
	petClickMultiCreditMS  int64 = 20_000
	petClickCreditCapMS    int64 = 60_000
	// A hover visit is one tick on the same rearing counter. It stays under a
	// single click, and it is not a click class: it does not open the window.
	petHoverCreditMS int64 = 2_000
)

type petClickClass int

const (
	petClickNone petClickClass = iota
	petClickSingle
	petClickDouble
	petClickMulti
)

// petClickBurst is one unresolved run of non-drag releases. It closes when
// the host double-click interval elapses after the latest release.
type petClickBurst struct {
	open  bool
	count int
	last  time.Time
}

func petPointerPastDrag(dx, dy int) bool {
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return dx > petDragThresholdPx || dy > petDragThresholdPx
}

func petClickNormalizeInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 500 * time.Millisecond
	}
	return interval
}

func petClickClassOf(count int) petClickClass {
	switch {
	case count <= 0:
		return petClickNone
	case count == 1:
		return petClickSingle
	case count == 2:
		return petClickDouble
	default:
		return petClickMulti
	}
}

// petClickNote records one non-drag release. A gap at least as long as the
// interval closes the previous burst first, then this release starts the next.
func petClickNote(b petClickBurst, now time.Time, interval time.Duration) (petClickBurst, petClickClass) {
	interval = petClickNormalizeInterval(interval)
	var resolved petClickClass
	if b.open {
		gap := now.Sub(b.last)
		if gap < 0 {
			gap = 0
		}
		if gap >= interval {
			resolved = petClickClassOf(b.count)
			b = petClickBurst{}
		}
	}
	if !b.open {
		return petClickBurst{open: true, count: 1, last: now}, resolved
	}
	b.count++
	b.last = now
	return b, resolved
}

// petClickAdvance closes the burst once the interval has elapsed with no
// further release. The caller uses it from the host timer.
func petClickAdvance(b petClickBurst, now time.Time, interval time.Duration) (petClickBurst, petClickClass) {
	interval = petClickNormalizeInterval(interval)
	if !b.open {
		return b, petClickNone
	}
	gap := now.Sub(b.last)
	if gap < 0 {
		gap = 0
	}
	if gap < interval {
		return b, petClickNone
	}
	return petClickBurst{}, petClickClassOf(b.count)
}

// petPointerCaptureChange is what happens when the pet stops owning the
// pointer. ownRelease is the reentrant notice from this window's own
// ReleaseCapture during button-up; that press is still resolved there.
// A grab stolen by someone else is not a click. If the pointer had already
// crossed the drag threshold, the drag ends instead of sticking to later moves.
func petPointerCaptureChange(dragging, dragMoved, ownRelease bool) (keepDragging, keepMoved, endDrag bool) {
	if ownRelease || !dragging {
		return dragging, dragMoved, false
	}
	return false, false, dragMoved
}

// petClickOnRelease is the mouse-up decision. A press that already traveled
// past the drag threshold is not a click and does not change the open burst.
func petClickOnRelease(b petClickBurst, now time.Time, interval time.Duration, dragged bool) (petClickBurst, petClickClass) {
	if dragged {
		return b, petClickNone
	}
	return petClickNote(b, now, interval)
}

func petClickCreditMS(class petClickClass) int64 {
	var credit int64
	switch class {
	case petClickSingle:
		credit = petClickSingleCreditMS
	case petClickDouble:
		credit = petClickDoubleCreditMS
	case petClickMulti:
		credit = petClickMultiCreditMS
	default:
		return 0
	}
	if credit <= 0 || credit >= petClickCreditCapMS {
		return 0
	}
	return credit
}

func petClickWantsMainWindow(class petClickClass) bool {
	return class == petClickSingle
}

func petClickClipName(class petClickClass) string {
	switch class {
	case petClickSingle:
		return "react_click_greet"
	case petClickDouble:
		return "react_click_notice"
	case petClickMulti:
		return "react_task_celebrate"
	default:
		return ""
	}
}

// petClickReactionClip is the body clip for this gesture. Quiet mode, reduced
// motion, and a pack that does not ship the clip return an empty name. The
// gesture itself still counts.
func petClickReactionClip(packID string, class petClickClass, motion bool) string {
	if !motion {
		return ""
	}
	name := petClickClipName(class)
	if name == "" || !petPackHasClip(packID, name) {
		return ""
	}
	return name
}

var (
	petClipSetMu sync.Mutex
	petClipSets  = map[string]map[string]bool{}
)

func petPackHasClip(packID, clip string) bool {
	if clip == "" {
		return false
	}
	set, ok := petPackClipSet(packID)
	return ok && set[clip]
}

func petPackClipSet(packID string) (map[string]bool, bool) {
	packID = strings.TrimSpace(packID)
	if packID == "" {
		packID = petpack.DefaultPackID
	}
	petClipSetMu.Lock()
	if set, ok := petClipSets[packID]; ok {
		petClipSetMu.Unlock()
		return set, true
	}
	petClipSetMu.Unlock()

	reg := petpack.EnsureGlobal()
	if reg == nil {
		return nil, false
	}
	resolved, err := reg.Resolve(packID, petpack.VariantDefault)
	if err != nil || resolved == nil {
		return nil, false
	}
	rig, err := petpack.LoadRig(resolved)
	if err != nil {
		// A missing file during a pack install is not a stable "no clips"
		// answer. Remembering it would keep the pet silent after the rig lands.
		petClipSetMu.Lock()
		existing, ok := petClipSets[packID]
		petClipSetMu.Unlock()
		if ok {
			return existing, true
		}
		return nil, false
	}
	set := map[string]bool{}
	if rig != nil {
		for name, clip := range rig.Clips {
			if clip.DurationMS > 0 {
				set[name] = true
			}
		}
	}
	petClipSetMu.Lock()
	if existing, ok := petClipSets[packID]; ok {
		petClipSetMu.Unlock()
		return existing, true
	}
	petClipSets[packID] = set
	petClipSetMu.Unlock()
	return set, true
}

// forgetPetClickClipSets drops the memo of which packs ship which clips.
// Pack install and removal already broadcast InvalidatePetPackAssets; the
// memo has to follow that, or an updated rig keeps the previous clip list.
func forgetPetClickClipSets() {
	petClipSetMu.Lock()
	petClipSets = map[string]map[string]bool{}
	petClipSetMu.Unlock()
}

// petClickIntervalFn is the host double-click window. Windows replaces it
// with GetDoubleClickTime. Other platforms keep the 500ms default.
var petClickIntervalFn = func() time.Duration { return 500 * time.Millisecond }

func petHostClickInterval() time.Duration {
	if petClickIntervalFn == nil {
		return 500 * time.Millisecond
	}
	return petClickNormalizeInterval(petClickIntervalFn())
}

type petClickResult struct {
	Class    petClickClass
	Clip     string
	CreditMS int64
	OpenMain bool
	Stage    rearingWindow
}

// recordDesktopPetClick is the non-drag mouse-up entry. The returned class is
// a previous burst that this release closed. The new burst stays open until
// the interval elapses.
func (a *App) recordDesktopPetClick(now time.Time) (petClickClass, uint64) {
	if a == nil {
		return petClickNone, 0
	}
	interval := petHostClickInterval()
	a.petClickMu.Lock()
	if a.petClickClosed {
		epoch := a.petClickEpoch.Load()
		a.petClickMu.Unlock()
		return petClickNone, epoch
	}
	epoch := a.petClickEpoch.Load()
	next, class := petClickNote(a.petClickBurst, now, interval)
	a.petClickBurst = next
	if next.open {
		a.armPetClickTimerLocked(next.last, interval)
	}
	a.petClickMu.Unlock()
	return class, epoch
}

func (a *App) armPetClickTimerLocked(last time.Time, interval time.Duration) {
	a.petClickGen++
	gen := a.petClickGen
	if a.petClickTimer != nil {
		a.petClickTimer.Stop()
		a.petClickTimer = nil
	}
	wait := interval - time.Since(last)
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	a.petClickTimer = time.AfterFunc(wait, func() {
		a.pollDesktopPetClick(time.Now(), gen)
	})
}

func (a *App) stopPetClickTimer() {
	if a == nil {
		return
	}
	a.petClickMu.Lock()
	a.petClickGen++
	if a.petClickTimer != nil {
		a.petClickTimer.Stop()
		a.petClickTimer = nil
	}
	a.petClickMu.Unlock()
}

// retirePetClickBurst ends the open gesture because the pet session that
// received it is gone: hidden, switched to another pack, or leaving the
// process. Callers that already hold petRearing.mu may call this. It takes
// petClickMu only. The reverse order is not used: finish drops petClickMu
// before it takes petRearing.mu.
func (a *App) retirePetClickBurst() {
	if a == nil {
		return
	}
	a.petClickMu.Lock()
	a.petClickClosed = true
	a.petClickEpoch.Add(1)
	a.petClickGen++
	a.petClickBurst = petClickBurst{}
	if a.petClickTimer != nil {
		a.petClickTimer.Stop()
		a.petClickTimer = nil
	}
	a.petClickMu.Unlock()
}

func (a *App) allowDesktopPetClick() {
	if a == nil {
		return
	}
	a.petClickMu.Lock()
	// Quit seals the gate. A rearing sync that already decided to show must
	// not open it again on the way out.
	if !a.petClickExit {
		a.petClickClosed = false
	}
	a.petClickMu.Unlock()
}

// sealDesktopPetClick rejects every later click, including one whose sync
// has not reached allow yet. Hide and pack switches stay reopenable.
func (a *App) sealDesktopPetClick() {
	if a == nil {
		return
	}
	a.petClickMu.Lock()
	a.petClickExit = true
	a.petClickMu.Unlock()
	a.retirePetClickBurst()
}

func (a *App) pollDesktopPetClick(now time.Time, gen uint64) {
	if a == nil {
		return
	}
	a.petClickMu.Lock()
	if gen != a.petClickGen || a.petClickClosed {
		a.petClickMu.Unlock()
		return
	}
	epoch := a.petClickEpoch.Load()
	next, class := petClickAdvance(a.petClickBurst, now, petHostClickInterval())
	a.petClickBurst = next
	if class == petClickNone && next.open {
		a.armPetClickTimerLocked(next.last, petHostClickInterval())
	}
	a.petClickMu.Unlock()
	if class != petClickNone {
		a.finishDesktopPetClick(class, epoch)
	}
}

// finishDesktopPetClick plays the gesture, adds rearing credit, and opens the
// main window only for a resolved single click.
func (a *App) finishDesktopPetClick(class petClickClass, epoch uint64) petClickResult {
	result := petClickResult{Class: class}
	if a == nil || class == petClickNone || a.petClickEpoch.Load() != epoch {
		return petClickResult{}
	}
	pack, motion := a.desktopPetClickLook()
	if a.petClickEpoch.Load() != epoch {
		return petClickResult{}
	}
	result.Clip = petClickReactionClip(pack, class, motion)
	result.OpenMain = petClickWantsMainWindow(class)
	added, stage := a.applyPetClickGesture(pack, class, epoch)
	result.CreditMS = added
	result.Stage = stage
	if a.petClickEpoch.Load() != epoch {
		result.OpenMain = false
		result.Clip = ""
		return result
	}
	if result.Clip != "" {
		// Playing the clip can load a renderer. Hide, disable, and quit retire
		// the session while that work runs; the side effect below has to see it.
		a.playDesktopPetClickClip(result.Clip)
	}
	if a.petClickEpoch.Load() != epoch {
		result.OpenMain = false
		result.Clip = ""
		return result
	}
	if result.OpenMain {
		a.onFloatingButtonClicked()
	}
	return result
}

func (a *App) desktopPetClickLook() (string, bool) {
	pack := petpack.DefaultPackID
	motion := true
	if a == nil {
		return pack, false
	}
	if fa := a.existingFloatingAssistant(); fa != nil {
		fa.mu.Lock()
		win := fa.window
		fa.mu.Unlock()
		if look, ok := win.(interface{ PetClickLook() (string, bool) }); ok && look != nil {
			if skin, allowed := look.PetClickLook(); skin != "" {
				pack = skin
				motion = allowed
			}
		}
	}
	a.petRearing.mu.Lock()
	if a.petRearing.running && strings.TrimSpace(a.petRearing.pack) != "" {
		pack = a.petRearing.pack
	}
	a.petRearing.mu.Unlock()
	return pack, motion
}

func (a *App) playDesktopPetClickClip(clip string) {
	if a == nil || clip == "" {
		return
	}
	fa := a.existingFloatingAssistant()
	if fa == nil {
		return
	}
	fa.mu.Lock()
	win := fa.window
	fa.mu.Unlock()
	player, ok := win.(interface{ PlayPetClickClip(string) })
	if !ok || player == nil {
		return
	}
	player.PlayPetClickClip(clip)
}

// applyPetClickGesture adds this gesture's credit to the live visible pack and
// runs the existing stage planner. A hidden pet and a drag add nothing.
func (a *App) applyPetClickGesture(packID string, class petClickClass, epoch uint64) (int64, rearingWindow) {
	credit := petClickCreditMS(class)
	if a == nil || credit <= 0 || a.petClickEpoch.Load() != epoch {
		return 0, rearingWindow{}
	}
	packID = strings.TrimSpace(packID)
	if packID == "" {
		packID = petpack.DefaultPackID
	}
	a.petRearing.mu.Lock()
	if a.petClickEpoch.Load() != epoch || !a.petRearing.running || a.petRearing.pack != packID {
		a.petRearing.mu.Unlock()
		return 0, rearingWindow{}
	}
	a.petRearing.loaded += credit
	stage := a.planPetRearingLocked()
	a.petRearing.mu.Unlock()
	a.deliverPetRearing(stage)
	return credit, stage
}

// creditDesktopPetHover adds one hover tick for the live visible pack. The
// message thread calls it on a goroutine because the rearing save writes
// sqlite. A hidden pet and a pack that is not the one on screen add nothing.
func (a *App) creditDesktopPetHover() {
	if a == nil {
		return
	}
	a.petRearing.mu.Lock()
	running := a.petRearing.running
	pack := strings.TrimSpace(a.petRearing.pack)
	a.petRearing.mu.Unlock()
	if !running || pack == "" {
		return
	}
	a.applyPetHoverCredit(pack)
}

// applyPetHoverCredit adds the hover tick. It is not a click, so it does not
// classify a gesture and does not open the main window.
func (a *App) applyPetHoverCredit(packID string) int64 {
	credit := petHoverCreditMS
	if a == nil || credit <= 0 || credit >= petClickSingleCreditMS || credit >= petClickCreditCapMS {
		return 0
	}
	packID = strings.TrimSpace(packID)
	if packID == "" {
		return 0
	}
	a.petRearing.mu.Lock()
	if !a.petRearing.running || a.petRearing.pack != packID {
		a.petRearing.mu.Unlock()
		return 0
	}
	a.petRearing.loaded += credit
	stage := a.planPetRearingLocked()
	a.petRearing.mu.Unlock()
	a.deliverPetRearing(stage)
	return credit
}
