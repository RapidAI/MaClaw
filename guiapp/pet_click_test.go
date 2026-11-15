package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestPetClickGesture(t *testing.T) {
	t.Run("burst", testPetClickBurst)
	t.Run("drag", testPetClickDrag)
	t.Run("clips", testPetClickClips)
	t.Run("credit", testPetClickCredit)
	t.Run("sideEffect", testPetClickSideEffect)
	t.Run("hostWait", testPetClickHostWaits)
	t.Run("session", testPetClickSession)
	t.Run("clipCache", testPetClickClipCache)
	t.Run("showGate", testPetClickShowGate)
	t.Run("deliveryOrder", testPetClickDeliveryOrder)
	t.Run("stageCue", testPetClickStageCue)
	t.Run("capture", testPetClickCapture)
	t.Run("hideDuringClip", testPetClickHideDuringClip)
}

// petClickClipWindow ends the click session inside the clip. That is the
// window between "the gesture was accepted" and "open the main window".
type petClickClipWindow struct {
	mockFloatingWindow
	app *App
}

func (w *petClickClipWindow) PetClickLook() (string, bool) {
	return "ragdoll", true
}

func (w *petClickClipWindow) PlayPetClickClip(string) {
	if w == nil || w.app == nil {
		return
	}
	w.app.retirePetClickBurst()
}

func testPetClickHideDuringClip(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopPetClickTimer()
		app.closePetRearingDBLocked()
	})
	app.petRearing.pack = "ragdoll"
	app.petRearing.running = true
	app.petRearing.userSize = 224
	app.floatingAssistant = &FloatingAssistantManager{app: app, window: &petClickClipWindow{app: app}}

	got := app.finishDesktopPetClick(petClickSingle, app.petClickEpoch.Load())
	if got.OpenMain || got.Clip != "" {
		t.Fatalf("hide during the click clip still finished the side effect: %+v", got)
	}
	if got.CreditMS != petClickCreditMS(petClickSingle) {
		t.Fatalf("credit = %d", got.CreditMS)
	}
}

func testPetClickCapture(t *testing.T) {
	keepDragging, keepMoved, endDrag := petPointerCaptureChange(true, false, true)
	if !keepDragging || keepMoved || endDrag {
		t.Fatalf("own release consumed the press: dragging=%v moved=%v end=%v", keepDragging, keepMoved, endDrag)
	}
	keepDragging, keepMoved, endDrag = petPointerCaptureChange(true, false, false)
	if keepDragging || keepMoved || endDrag {
		t.Fatalf("stolen press became a gesture: dragging=%v moved=%v end=%v", keepDragging, keepMoved, endDrag)
	}
	keepDragging, keepMoved, endDrag = petPointerCaptureChange(true, true, false)
	if keepDragging || keepMoved || !endDrag {
		t.Fatalf("stolen drag stuck: dragging=%v moved=%v end=%v", keepDragging, keepMoved, endDrag)
	}
	keepDragging, keepMoved, endDrag = petPointerCaptureChange(false, false, false)
	if keepDragging || keepMoved || endDrag {
		t.Fatalf("idle capture change started a gesture: dragging=%v moved=%v end=%v", keepDragging, keepMoved, endDrag)
	}
}

func testPetClickBurst(t *testing.T) {
	interval := 500 * time.Millisecond
	t0 := time.Unix(1_700_000_000, 0)

	var b petClickBurst
	var class petClickClass
	b, class = petClickNote(b, t0, interval)
	if class != petClickNone || !b.open || b.count != 1 {
		t.Fatalf("first release = %+v class %d", b, class)
	}
	b, class = petClickAdvance(b, t0.Add(interval-time.Millisecond), interval)
	if class != petClickNone || b.count != 1 {
		t.Fatalf("interval still open: %+v class %d", b, class)
	}
	b, class = petClickAdvance(b, t0.Add(interval), interval)
	if class != petClickSingle || b.open {
		t.Fatalf("single = %+v class %d", b, class)
	}

	b = petClickBurst{}
	b, _ = petClickNote(b, t0, interval)
	b, class = petClickNote(b, t0.Add(interval-time.Millisecond), interval)
	if class != petClickNone || b.count != 2 {
		t.Fatalf("second release = %+v class %d", b, class)
	}
	b, class = petClickAdvance(b, t0.Add(interval-time.Millisecond+interval), interval)
	if class != petClickDouble || b.open {
		t.Fatalf("double = %+v class %d", b, class)
	}

	b = petClickBurst{}
	for i := 0; i < 3; i++ {
		b, class = petClickNote(b, t0.Add(time.Duration(i)*100*time.Millisecond), interval)
		if class != petClickNone {
			t.Fatalf("multi release %d resolved early as %d", i, class)
		}
	}
	b, class = petClickAdvance(b, t0.Add(200*time.Millisecond+interval), interval)
	if class != petClickMulti || b.open {
		t.Fatalf("multi = %+v class %d", b, class)
	}

	b = petClickBurst{}
	for i := 0; i < 4; i++ {
		b, class = petClickNote(b, t0.Add(time.Duration(i)*50*time.Millisecond), interval)
		if class != petClickNone {
			t.Fatalf("fourth release resolved early as %d", class)
		}
	}
	if b.count != 4 {
		t.Fatalf("count = %d", b.count)
	}
	b, class = petClickAdvance(b, t0.Add(150*time.Millisecond+interval), interval)
	if class != petClickMulti {
		t.Fatalf("four releases = %d", class)
	}

	b = petClickBurst{}
	b, _ = petClickNote(b, t0, interval)
	b, class = petClickNote(b, t0.Add(interval), interval)
	if class != petClickSingle || !b.open || b.count != 1 {
		t.Fatalf("late click = %+v class %d", b, class)
	}
	b, class = petClickAdvance(b, t0.Add(2*interval), interval)
	if class != petClickSingle || b.open {
		t.Fatalf("new burst = %+v class %d", b, class)
	}
}

func testPetClickDrag(t *testing.T) {
	interval := 500 * time.Millisecond
	t0 := time.Unix(1_700_000_000, 0)
	if petPointerPastDrag(petDragThresholdPx, 0) || petPointerPastDrag(0, -petDragThresholdPx) {
		t.Fatal("the existing drag slack is a click")
	}
	if !petPointerPastDrag(petDragThresholdPx+1, 0) || !petPointerPastDrag(0, -(petDragThresholdPx+1)) {
		t.Fatal("one pixel past the slack is a drag")
	}

	var b petClickBurst
	b, class := petClickOnRelease(b, t0, interval, true)
	if class != petClickNone || b.open {
		t.Fatalf("drag release = %+v class %d", b, class)
	}
	if petClickCreditMS(class) != 0 {
		t.Fatal("drag credited rearing time")
	}

	b, _ = petClickNote(b, t0, interval)
	b, class = petClickOnRelease(b, t0.Add(10*time.Millisecond), interval, petPointerPastDrag(8, -2))
	if class != petClickNone || !b.open || b.count != 1 {
		t.Fatalf("drag changed the open burst: %+v class %d", b, class)
	}
	b, class = petClickAdvance(b, t0.Add(10*time.Millisecond+interval), interval)
	if class != petClickSingle {
		t.Fatalf("open click after a later drag = %d", class)
	}
}

func testPetClickClips(t *testing.T) {
	single := petClickReactionClip("ragdoll", petClickSingle, true)
	double := petClickReactionClip("ragdoll", petClickDouble, true)
	multi := petClickReactionClip("ragdoll", petClickMulti, true)
	if single == "" || double == "" || multi == "" || single == double || double == multi || single == multi {
		t.Fatalf("ragdoll clips single=%q double=%q multi=%q", single, double, multi)
	}
	if petClickReactionClip("ragdoll", petClickSingle, false) != "" ||
		petClickReactionClip("ragdoll", petClickDouble, false) != "" ||
		petClickReactionClip("ragdoll", petClickMulti, false) != "" {
		t.Fatal("motion off still named a clip")
	}
	testPetClickWindowMotion(t)

	reg := petpack.EnsureGlobal()
	if reg == nil {
		t.Fatal("no pet registry")
	}
	resolved, err := reg.Resolve("ragdoll", petpack.VariantDefault)
	if err != nil || resolved == nil {
		t.Fatalf("resolve ragdoll: %v", err)
	}
	renderer, err := petpack.NewCharacterRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatalf("ragdoll renderer: %v", err)
	}
	for i, clip := range []string{single, double, multi} {
		if !renderer.TriggerClip(clip, int64(i*20)) {
			t.Fatalf("clip %s did not play", clip)
		}
	}
	if renderer.TriggerClip("not-a-real-clip", 80) || renderer.TriggerClip("", 90) {
		t.Fatal("missing clip was played")
	}
	if petClickReactionClip("mini-claw", petClickSingle, true) != "" ||
		petClickReactionClip("mini-claw", petClickDouble, true) != "" ||
		petClickReactionClip("mini-claw", petClickMulti, true) != "" {
		t.Fatal("mini-claw was given a ragdoll clip")
	}
}

func testPetClickCredit(t *testing.T) {
	single := petClickCreditMS(petClickSingle)
	double := petClickCreditMS(petClickDouble)
	multi := petClickCreditMS(petClickMulti)
	if single <= 0 || double < single || multi < double {
		t.Fatalf("credit order single=%d double=%d multi=%d", single, double, multi)
	}
	if single >= petpack.RearingMinAfterMS || double >= petpack.RearingMinAfterMS || multi >= petpack.RearingMinAfterMS {
		t.Fatalf("one gesture reaches a legal stage: %d %d %d", single, double, multi)
	}
	if petClickCreditMS(petClickNone) != 0 {
		t.Fatal("empty gesture has credit")
	}

	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopPetClickTimer()
		app.closePetRearingDBLocked()
	})
	app.petRearing.pack = "ragdoll"
	app.petRearing.running = true
	app.petRearing.userSize = 224
	now := time.Now()
	app.petRearing.last = now.Add(-5 * time.Second)
	app.petRearing.mu.Lock()
	app.flushPetRearingLocked(now)
	app.petRearing.mu.Unlock()
	if app.petRearing.loaded != int64(5*time.Second/time.Millisecond) {
		t.Fatalf("visible wall clock = %d", app.petRearing.loaded)
	}
	added, stage := app.applyPetClickGesture("ragdoll", petClickSingle, app.petClickEpoch.Load())
	if added != single || stage.cue != "" || stage.stageID != "" {
		t.Fatalf("early click added=%d stage=%+v", added, stage)
	}
	if app.petRearing.loaded != int64(5*time.Second/time.Millisecond)+single {
		t.Fatalf("click stacked on wall clock: %d", app.petRearing.loaded)
	}

	app.petRearing.running = false
	app.petRearing.last = now.Add(-time.Hour)
	hidden := app.petRearing.loaded
	app.petRearing.mu.Lock()
	app.flushPetRearingLocked(now.Add(time.Hour))
	app.petRearing.mu.Unlock()
	if added, _ := app.applyPetClickGesture("ragdoll", petClickMulti, app.petClickEpoch.Load()); added != 0 || app.petRearing.loaded != hidden {
		t.Fatalf("hidden progress changed to %d (added %d)", app.petRearing.loaded, added)
	}
	app.petRearing.running = true
	if added, _ := app.applyPetClickGesture("ragdoll", petClickNone, app.petClickEpoch.Load()); added != 0 || app.petRearing.loaded != hidden {
		t.Fatal("drag credit changed the counter")
	}

	app.petRearing.mu.Lock()
	app.closePetRearingDBLocked()
	app.petRearing.loaded = 0
	loaded, _ := app.loadPetRearingLocked("ragdoll")
	app.petRearing.mu.Unlock()
	if loaded != hidden {
		t.Fatalf("reloaded progress = %d, want %d", loaded, hidden)
	}

	reg := petpack.EnsureGlobal()
	manifest, ok := reg.Get("ragdoll")
	if !ok || manifest == nil || len(manifest.Rearing.Stages) == 0 {
		t.Fatal("ragdoll has no rearing stages")
	}
	threshold := manifest.Rearing.Stages[0].AfterMS
	if threshold < petpack.RearingMinAfterMS {
		t.Fatalf("first stage %d is under the legal minimum", threshold)
	}
	app.petRearing.running = true
	app.petRearing.pack = "ragdoll"
	app.petRearing.userSize = 224
	app.petRearing.loaded = threshold - single
	app.petRearing.announced = ""
	app.petRearing.observed = false
	app.petRearing.mu.Lock()
	pre := app.planPetRearingLocked()
	app.petRearing.mu.Unlock()
	if pre.cue != "" || pre.stageID != "" || len(pre.adornments) != 0 {
		t.Fatalf("before the crossing = %+v", pre)
	}
	added, crossed := app.applyPetClickGesture("ragdoll", petClickSingle, app.petClickEpoch.Load())
	if added != single {
		t.Fatalf("crossing credit = %d", added)
	}
	want, found := petpack.ActiveRearingStage(manifest.Rearing.Stages, app.petRearing.loaded)
	if !found {
		t.Fatal("summed progress did not reach a stage")
	}
	if crossed.stageID != want.ID || crossed.cue != want.Cue {
		t.Fatalf("crossing = %+v, stage cue %q id %q", crossed, want.Cue, want.ID)
	}
	if !sameAdornmentSlots(crossed.adornments, petpack.ActiveAdornmentSlots(manifest, app.petRearing.loaded)) {
		t.Fatalf("adornments = %v", crossed.adornments)
	}
	if crossed.windowSize != rearingWindowSize(224, petpack.StageScale(&want)) {
		t.Fatalf("window = %d", crossed.windowSize)
	}
	app.petRearing.mu.Lock()
	again := app.planPetRearingLocked()
	app.petRearing.mu.Unlock()
	if again.cue != "" || again.stageID != want.ID {
		t.Fatalf("cue repeated = %+v", again)
	}

	bare := &App{testHomeDir: t.TempDir()}
	t.Cleanup(bare.closePetRearingDBLocked)
	bare.petRearing.pack = "mini-claw"
	bare.petRearing.running = true
	bare.petRearing.userSize = 224
	added, _ = bare.applyPetClickGesture("mini-claw", petClickDouble, bare.petClickEpoch.Load())
	if added != double || bare.petRearing.loaded != double {
		t.Fatalf("pack without the clip rejected the gesture: added %d loaded %d", added, bare.petRearing.loaded)
	}
}

func testPetClickHostWaits(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopPetClickTimer()
		app.closePetRearingDBLocked()
	})
	app.petRearing.pack = "ragdoll"
	app.petRearing.running = true
	app.petRearing.userSize = 224
	interval := petHostClickInterval()
	t0 := time.Now()
	if class, _ := app.recordDesktopPetClick(t0); class != petClickNone {
		t.Fatalf("first click resolved as %d", class)
	}
	if class, _ := app.recordDesktopPetClick(t0.Add(interval / 4)); class != petClickNone {
		t.Fatalf("second click resolved as %d", class)
	}
	app.petClickMu.Lock()
	gen := app.petClickGen
	app.petClickMu.Unlock()
	app.pollDesktopPetClick(t0.Add(interval/4+interval), gen)
	if app.petRearing.loaded != petClickCreditMS(petClickDouble) {
		t.Fatalf("host burst credited %d", app.petRearing.loaded)
	}
	app.petClickMu.Lock()
	open := app.petClickBurst.open
	app.petClickMu.Unlock()
	if open {
		t.Fatal("burst stayed open after the interval")
	}
}

func testPetClickSession(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopPetClickTimer()
		app.closePetRearingDBLocked()
	})
	now := time.Now()
	app.syncPetRearingAt("ragdoll", true, 224, now)

	if class, _ := app.recordDesktopPetClick(now); class != petClickNone {
		t.Fatalf("first click resolved as %d", class)
	}
	app.syncPetRearingAt("ragdoll", true, 224, now.Add(time.Millisecond))
	app.petClickMu.Lock()
	open := app.petClickBurst.open
	gen := app.petClickGen
	app.petClickMu.Unlock()
	if !open {
		t.Fatal("a same-pack refresh closed the click")
	}
	app.pollDesktopPetClick(now.Add(petHostClickInterval()), gen)

	_, epoch := app.recordDesktopPetClick(now.Add(petHostClickInterval() + time.Millisecond))
	app.syncPetRearingAt("ragdoll", false, 224, now.Add(petHostClickInterval()+2*time.Millisecond))
	afterHide := app.petRearing.loaded
	hidden := app.finishDesktopPetClick(petClickSingle, epoch)
	if hidden.OpenMain || hidden.CreditMS != 0 || hidden.Clip != "" {
		t.Fatalf("hidden pet still finished the click: %+v", hidden)
	}
	if app.petRearing.loaded != afterHide {
		t.Fatalf("a retired click changed loaded time from %d to %d", afterHide, app.petRearing.loaded)
	}
	app.petClickMu.Lock()
	open = app.petClickBurst.open
	app.petClickMu.Unlock()
	if open {
		t.Fatal("hide left the burst open")
	}
	if class, _ := app.recordDesktopPetClick(now.Add(2 * petHostClickInterval())); class != petClickNone {
		t.Fatal("a hidden pet accepted a click")
	}
	app.petClickMu.Lock()
	open = app.petClickBurst.open
	app.petClickMu.Unlock()
	if open {
		t.Fatal("a hidden pet opened a burst")
	}

	app.syncPetRearingAt("ragdoll", true, 224, now.Add(3*petHostClickInterval()))
	before := app.petRearing.loaded
	clickAt := now.Add(3 * petHostClickInterval())
	if class, _ := app.recordDesktopPetClick(clickAt); class != petClickNone {
		t.Fatal("show did not accept a click")
	}
	app.petClickMu.Lock()
	gen = app.petClickGen
	app.petClickMu.Unlock()
	app.pollDesktopPetClick(clickAt.Add(petHostClickInterval()), gen)
	if app.petRearing.loaded != before+petClickCreditMS(petClickSingle) {
		t.Fatalf("shown pet credited %d, want %d more than %d", app.petRearing.loaded, petClickCreditMS(petClickSingle), before)
	}

	_, epoch = app.recordDesktopPetClick(clickAt.Add(petHostClickInterval() + time.Millisecond))
	app.syncPetRearingAt("mini-claw", true, 224, clickAt.Add(petHostClickInterval()+2*time.Millisecond))
	switched := app.finishDesktopPetClick(petClickDouble, epoch)
	if switched.CreditMS != 0 || app.petRearing.pack != "mini-claw" || app.petRearing.loaded != 0 {
		t.Fatalf("switch kept the old click: result %+v pack %s loaded %d", switched, app.petRearing.pack, app.petRearing.loaded)
	}
}

// clickGateWindow records whether Create ran against an already-open click
// session. The production window is visible inside Create, before Show returns.
type clickGateWindow struct {
	mockFloatingWindow
	app                 *App
	creates             int
	ready               []bool
	rewind              bool
	reloads             int
	reloadPack          string
	reloadOpen          bool
	destroyCalls        int
	closedBeforeDestroy int
}

func (w *clickGateWindow) Destroy() {
	w.app.petClickMu.Lock()
	closed := w.app.petClickClosed
	w.app.petClickMu.Unlock()
	w.mu.Lock()
	w.destroyCalls++
	if closed {
		w.closedBeforeDestroy++
	}
	w.mu.Unlock()
	w.mockFloatingWindow.Destroy()
}

func (w *clickGateWindow) sessionReady() bool {
	w.app.petClickMu.Lock()
	closed := w.app.petClickClosed || w.app.petClickExit
	w.app.petClickMu.Unlock()
	w.app.petRearing.mu.Lock()
	running := w.app.petRearing.running && w.app.petRearing.pack != ""
	w.app.petRearing.mu.Unlock()
	return running && !closed
}

func (w *clickGateWindow) Create(x, y, width, height int) error {
	w.creates++
	w.ready = append(w.ready, w.sessionReady())
	if w.rewind {
		w.app.petRearing.mu.Lock()
		if w.app.petRearing.running {
			w.app.petRearing.last = time.Now().Add(-5 * time.Second)
		}
		w.app.petRearing.mu.Unlock()
	}
	return w.mockFloatingWindow.Create(x, y, width, height)
}

func (w *clickGateWindow) ReloadAppearance(x, y, size int) error {
	w.reloads++
	w.app.petRearing.mu.Lock()
	w.reloadPack = w.app.petRearing.pack
	w.app.petRearing.mu.Unlock()
	w.app.petClickMu.Lock()
	w.reloadOpen = !w.app.petClickClosed && !w.app.petClickExit
	w.app.petClickMu.Unlock()
	w.mu.Lock()
	w.x, w.y = x, y
	w.created = true
	w.shown = true
	w.mu.Unlock()
	_ = size
	return nil
}

func testPetClickShowGate(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	win := &clickGateWindow{app: app}
	manager := &FloatingAssistantManager{app: app, window: win}
	app.floatingAssistant = manager
	t.Cleanup(func() {
		app.stopPetClickTimer()
		app.petRearing.mu.Lock()
		app.closePetRearingDBLocked()
		app.petRearing.mu.Unlock()
	})

	manager.ShowFloatingButton()
	if win.creates != 1 || len(win.ready) != 1 || !win.ready[0] {
		t.Fatalf("first show Create ready=%v creates=%d", win.ready, win.creates)
	}
	before := app.petRearing.loaded
	clickAt := time.Now()
	if class, _ := app.recordDesktopPetClick(clickAt); class != petClickNone {
		t.Fatalf("first click resolved as %d", class)
	}
	app.petClickMu.Lock()
	gen := app.petClickGen
	app.petClickMu.Unlock()
	app.pollDesktopPetClick(clickAt.Add(petHostClickInterval()), gen)
	if app.petRearing.loaded != before+petClickCreditMS(petClickSingle) {
		t.Fatalf("shown pet credited %d, want %d more than %d", app.petRearing.loaded, petClickCreditMS(petClickSingle), before)
	}

	manager.HideFloatingButton()
	if win.destroyCalls != 1 || win.closedBeforeDestroy != 1 {
		t.Fatalf("hide destroyed the window before the click session ended: destroys=%d closedFirst=%d", win.destroyCalls, win.closedBeforeDestroy)
	}
	if class, _ := app.recordDesktopPetClick(time.Now()); class != petClickNone {
		t.Fatal("a hidden pet accepted a click")
	}
	app.petClickMu.Lock()
	if app.petClickBurst.open {
		app.petClickMu.Unlock()
		t.Fatal("a hidden pet opened a burst")
	}
	app.petClickMu.Unlock()

	win.rewind = true
	before = app.petRearing.loaded
	manager.ShowFloatingButton()
	win.rewind = false
	if win.creates != 2 || len(win.ready) != 2 || !win.ready[1] {
		t.Fatalf("show after hide Create ready=%v creates=%d", win.ready, win.creates)
	}
	if gap := app.petRearing.loaded - before; gap < 0 || gap >= 1000 {
		t.Fatalf("showing the pet counted %dms from before the window existed", gap)
	}
	before = app.petRearing.loaded
	clickAt = time.Now()
	if class, _ := app.recordDesktopPetClick(clickAt); class != petClickNone {
		t.Fatal("show after hide did not accept a click")
	}
	app.petClickMu.Lock()
	gen = app.petClickGen
	app.petClickMu.Unlock()
	app.pollDesktopPetClick(clickAt.Add(petHostClickInterval()), gen)
	if app.petRearing.loaded != before+petClickCreditMS(petClickSingle) {
		t.Fatalf("reshown pet credited %d, want %d more than %d", app.petRearing.loaded, petClickCreditMS(petClickSingle), before)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.PetSkin = "mini-claw"
	manager.RefreshAppearance(cfg)
	if win.reloads != 1 || win.reloadPack != "mini-claw" || !win.reloadOpen {
		t.Fatalf("switch reload pack=%q open=%v reloads=%d", win.reloadPack, win.reloadOpen, win.reloads)
	}
	if win.destroyCalls != 1 {
		t.Fatalf("skin switch destroyed the window: destroys=%d", win.destroyCalls)
	}

	disabled := cfg
	disabled.PetEnabled = false
	manager.RefreshAppearance(disabled)
	if win.destroyCalls != 2 || win.closedBeforeDestroy != 2 {
		t.Fatalf("disable destroyed the window before the click session ended: destroys=%d closedFirst=%d", win.destroyCalls, win.closedBeforeDestroy)
	}

	_, epoch := app.recordDesktopPetClick(time.Now())
	app.sealDesktopPetClick()
	app.allowDesktopPetClick()
	sealed := app.finishDesktopPetClick(petClickSingle, epoch)
	if sealed.OpenMain || sealed.CreditMS != 0 || sealed.Clip != "" {
		t.Fatalf("exit still finished the click: %+v", sealed)
	}
	if class, _ := app.recordDesktopPetClick(time.Now()); class != petClickNone {
		t.Fatal("exit accepted a click")
	}
	app.petClickMu.Lock()
	open := app.petClickBurst.open
	exit := app.petClickExit
	app.petClickMu.Unlock()
	if open || !exit {
		t.Fatalf("exit gate open=%v exit=%v", open, exit)
	}
}

func testPetClickClipCache(t *testing.T) {
	if petClickReactionClip("ragdoll", petClickSingle, true) == "" {
		t.Fatal("ragdoll clip missing")
	}
	petClipSetMu.Lock()
	petClipSets["ragdoll"] = map[string]bool{}
	petClipSetMu.Unlock()
	if petClickReactionClip("ragdoll", petClickSingle, true) != "" {
		t.Fatal("an empty clip memo still named a reaction")
	}
	(&FloatingAssistantManager{}).InvalidatePetPackAssets()
	if petClickReactionClip("ragdoll", petClickSingle, true) == "" {
		t.Fatal("pack invalidation left the empty clip memo in place")
	}
}

type rearingOrderWindow struct {
	mockFloatingWindow
	applies []rearingOrderApply
}

type rearingOrderApply struct {
	variant    string
	windowSize int
	cue        string
	adornments []string
}

func (w *rearingOrderWindow) ApplyPetRearing(variant string, windowSize int, cue string, adornments []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.applies = append(w.applies, rearingOrderApply{
		variant:    variant,
		windowSize: windowSize,
		cue:        cue,
		adornments: append([]string(nil), adornments...),
	})
}

func (w *rearingOrderWindow) applied() []rearingOrderApply {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]rearingOrderApply, len(w.applies))
	copy(out, w.applies)
	return out
}

func testPetClickDeliveryOrder(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	win := &rearingOrderWindow{}
	app.floatingAssistant = &FloatingAssistantManager{app: app, window: win, visible: true}
	t.Cleanup(func() {
		app.petRearing.mu.Lock()
		app.closePetRearingDBLocked()
		app.petRearing.mu.Unlock()
	})

	app.petRearing.mu.Lock()
	app.petRearing.pack = "ragdoll"
	app.petRearing.running = true
	app.petRearing.userSize = 224
	app.petRearing.loaded = 0
	base := app.planPetRearingLocked()
	app.petRearing.loaded = 7_200_000
	familiar := app.planPetRearingLocked()
	app.petRearing.mu.Unlock()
	if base.seq == 0 || familiar.seq <= base.seq {
		t.Fatalf("plans are not ordered: base %d familiar %d", base.seq, familiar.seq)
	}
	if base.windowSize == familiar.windowSize || familiar.windowSize < base.windowSize {
		t.Fatalf("familiar window %d does not grow past %d", familiar.windowSize, base.windowSize)
	}
	if familiar.cue == "" || len(familiar.adornments) == 0 || base.cue != "" {
		t.Fatalf("fixture cues base=%+v familiar=%+v", base, familiar)
	}

	app.deliverPetRearing(base)
	app.deliverPetRearing(familiar)
	got := win.applied()
	if len(got) == 0 {
		t.Fatal("newer plan never reached the window")
	}
	if last := got[len(got)-1]; last.windowSize != familiar.windowSize || !sameAdornmentSlots(last.adornments, familiar.adornments) {
		t.Fatalf("newer plan did not replace the look: %+v", last)
	}

	win.mu.Lock()
	win.applies = nil
	win.mu.Unlock()
	app.deliverPetRearing(familiar)
	app.deliverPetRearing(base)
	got = win.applied()
	if len(got) != 2 {
		t.Fatalf("reverse applies = %d", len(got))
	}
	if got[0].windowSize != familiar.windowSize || got[0].cue != familiar.cue {
		t.Fatalf("newer plan was rewritten on the way in: %+v", got[0])
	}
	last := got[len(got)-1]
	if last.windowSize != familiar.windowSize || !sameAdornmentSlots(last.adornments, familiar.adornments) {
		t.Fatalf("older plan reverted the look: size %d adornments %v", last.windowSize, last.adornments)
	}
	if last.cue != "" {
		t.Fatalf("older plan grew a cue: %q", last.cue)
	}

	app.petRearing.mu.Lock()
	later := app.planPetRearingLocked()
	app.petRearing.mu.Unlock()
	if later.cue != "" || later.seq <= familiar.seq || later.windowSize != familiar.windowSize {
		t.Fatalf("same-stage plan = %+v", later)
	}
	win.mu.Lock()
	win.applies = nil
	win.mu.Unlock()
	app.deliverPetRearing(later)
	app.deliverPetRearing(familiar)
	got = win.applied()
	if len(got) == 0 {
		t.Fatal("late crossing never reached the window")
	}
	last = got[len(got)-1]
	if last.windowSize != familiar.windowSize || last.cue != familiar.cue || !sameAdornmentSlots(last.adornments, familiar.adornments) {
		t.Fatalf("late crossing lost its cue or look: %+v", last)
	}
}

func testPetClickStageCue(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	win := &rearingOrderWindow{}
	if err := win.Create(0, 0, 240, 240); err != nil {
		t.Fatal(err)
	}
	app.floatingAssistant = &FloatingAssistantManager{app: app, window: win, visible: true}
	t.Cleanup(func() {
		app.petRearing.mu.Lock()
		app.closePetRearingDBLocked()
		app.petRearing.mu.Unlock()
	})

	reg := petpack.EnsureGlobal()
	manifest, ok := reg.Get("ragdoll")
	if !ok || manifest == nil || len(manifest.Rearing.Stages) == 0 {
		t.Fatal("ragdoll has no rearing stages")
	}
	threshold := manifest.Rearing.Stages[0].AfterMS
	app.petRearing.mu.Lock()
	app.petRearing.pack = "ragdoll"
	app.petRearing.running = true
	app.petRearing.userSize = 224
	app.petRearing.loaded = threshold - 10_000
	app.petRearing.announced = ""
	app.petRearing.observed = true
	app.petRearing.last = time.Now().Add(-30 * time.Second)
	app.petRearing.mu.Unlock()

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.PetEnabled = true
	cfg.PetSkin = "ragdoll"
	cfg.PetSize = 224
	app.floatingAssistant.RefreshAppearance(cfg)

	want, found := petpack.ActiveRearingStage(manifest.Rearing.Stages, app.petRearing.loaded)
	if !found {
		t.Fatalf("refresh did not cross a stage, loaded %d", app.petRearing.loaded)
	}
	got := win.applied()
	if len(got) != 1 {
		t.Fatalf("stage applies = %d %+v", len(got), got)
	}
	if got[0].cue != want.Cue || got[0].windowSize != rearingWindowSize(224, petpack.StageScale(&want)) || !sameAdornmentSlots(got[0].adornments, petpack.ActiveAdornmentSlots(manifest, app.petRearing.loaded)) {
		t.Fatalf("crossing did not reach the window: %+v", got[0])
	}
	app.petRearing.mu.Lock()
	again := app.planPetRearingLocked()
	app.petRearing.mu.Unlock()
	if again.cue != "" {
		t.Fatalf("crossing cue still pending: %+v", again)
	}
}
