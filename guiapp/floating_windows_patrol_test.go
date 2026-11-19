//go:build windows

package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

// A stored miss must outlive the next paint. The pet paints at 20 FPS, and
// searching again on every frame enumerates every top-level window while the
// main window is hidden.
func TestPetPatrolMissIsRemembered(t *testing.T) {
	petPatrolLookup.mu.Lock()
	prevHWND := petPatrolLookup.hwnd
	prevMiss := petPatrolLookup.missUntil
	hold := time.Now().Add(time.Minute)
	petPatrolLookup.hwnd = 1
	petPatrolLookup.missUntil = hold
	petPatrolLookup.mu.Unlock()
	t.Cleanup(func() {
		petPatrolLookup.mu.Lock()
		petPatrolLookup.hwnd = prevHWND
		petPatrolLookup.missUntil = prevMiss
		petPatrolLookup.mu.Unlock()
	})

	if got := petPatrolHWND(2, time.Now()); got != 0 {
		t.Fatalf("held miss returned %d", got)
	}
	petPatrolLookup.mu.Lock()
	gotHWND := petPatrolLookup.hwnd
	gotMiss := petPatrolLookup.missUntil
	petPatrolLookup.mu.Unlock()
	if gotHWND != 1 || !gotMiss.Equal(hold) {
		t.Fatalf("miss was searched again: hwnd=%d until=%v", gotHWND, gotMiss)
	}
}

// A failed search must keep the hwnd it already knew. The next paint then
// sees that window become visible without enumerating, instead of roaming
// for the whole hold and snapping onto the border when the hold ends.
func TestPetPatrolMissKeepsTheWindow(t *testing.T) {
	petPatrolLookup.mu.Lock()
	prevHWND := petPatrolLookup.hwnd
	prevMiss := petPatrolLookup.missUntil
	petPatrolLookup.hwnd = 42
	petPatrolLookup.missUntil = time.Time{}
	petPatrolLookup.mu.Unlock()
	prevFind := findPetMainWindow
	searches := 0
	findPetMainWindow = func() uintptr {
		searches++
		return 0
	}
	t.Cleanup(func() {
		findPetMainWindow = prevFind
		petPatrolLookup.mu.Lock()
		petPatrolLookup.hwnd = prevHWND
		petPatrolLookup.missUntil = prevMiss
		petPatrolLookup.mu.Unlock()
	})

	now := time.Now()
	if got := petPatrolHWND(7, now); got != 0 {
		t.Fatalf("miss returned %d", got)
	}
	petPatrolLookup.mu.Lock()
	gotHWND := petPatrolLookup.hwnd
	gotMiss := petPatrolLookup.missUntil
	petPatrolLookup.mu.Unlock()
	if searches != 1 || gotHWND != 42 || !gotMiss.After(now) {
		t.Fatalf("searches=%d hwnd=%d until=%v", searches, gotHWND, gotMiss)
	}
	if got := petPatrolHWND(7, now.Add(time.Millisecond)); got != 0 || searches != 1 {
		t.Fatalf("hold searched again: got=%d searches=%d", got, searches)
	}
}

func TestAdoptLivePetOriginKeepsAnotherMonitor(t *testing.T) {
	prev := petWorkAreaAt
	petWorkAreaAt = func(x, y, sz int) (petpack.WindowRect, bool) {
		return petpack.WindowRect{Left: -1920, Top: 0, Right: 0, Bottom: 1080}, true
	}
	t.Cleanup(func() { petWorkAreaAt = prev })

	w := &windowsFloatingWindow{created: true, hwnd: 1, windowStartX: -500, windowStartY: 80}
	m := &FloatingAssistantManager{window: w, posX: 100, posY: 100}
	_, _, live, changed := m.adoptLivePetOriginLocked(200)
	if !live || !changed || m.posX != -500 || m.posY != 80 {
		t.Fatalf("adopted (%d,%d) live=%v changed=%v", m.posX, m.posY, live, changed)
	}
	_, _, live, changed = m.adoptLivePetOriginLocked(200)
	if !live || changed || m.posX != -500 || m.posY != 80 {
		t.Fatalf("second adopt (%d,%d) live=%v changed=%v", m.posX, m.posY, live, changed)
	}
}

// Tray quit and a Wails shutdown never call the pet-menu quit. The origin
// she walked to has to be on disk before the process goes away, and a
// second exit must not wipe it once the window is already gone.
func TestProcessExitKeepsTheLivePetOrigin(t *testing.T) {
	prev := petWorkAreaAt
	petWorkAreaAt = func(x, y, sz int) (petpack.WindowRect, bool) {
		return petpack.WindowRect{Left: -1920, Top: 0, Right: 0, Bottom: 1080}, true
	}
	t.Cleanup(func() { petWorkAreaAt = prev })

	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.FloatingBtnX = 100
		cfg.FloatingBtnY = 100
		cfg.FloatingBtnPositionSet = true
	}); err != nil {
		t.Fatal(err)
	}

	app.persistLivePetOriginForExit()
	if cfg := readPersistedConfigForTest(t, tmpHome); cfg.FloatingBtnX != 100 || cfg.FloatingBtnY != 100 {
		t.Fatalf("no pet wrote (%d,%d)", cfg.FloatingBtnX, cfg.FloatingBtnY)
	}

	w := &windowsFloatingWindow{created: true, hwnd: 1, windowStartX: -500, windowStartY: 80}
	app.floatingAssistant = &FloatingAssistantManager{app: app, window: w, posX: 100, posY: 100}
	app.persistLivePetOriginForExit()
	cfg := readPersistedConfigForTest(t, tmpHome)
	if cfg.FloatingBtnX != -500 || cfg.FloatingBtnY != 80 || !cfg.FloatingBtnPositionSet {
		t.Fatalf("exit saved (%d,%d) set=%v", cfg.FloatingBtnX, cfg.FloatingBtnY, cfg.FloatingBtnPositionSet)
	}
	if app.floatingAssistant.posX != -500 || app.floatingAssistant.posY != 80 {
		t.Fatalf("manager (%d,%d)", app.floatingAssistant.posX, app.floatingAssistant.posY)
	}

	app.persistLivePetOriginForExit()
	cfg = readPersistedConfigForTest(t, tmpHome)
	if cfg.FloatingBtnX != -500 || cfg.FloatingBtnY != 80 {
		t.Fatalf("second exit saved (%d,%d)", cfg.FloatingBtnX, cfg.FloatingBtnY)
	}

	w.created = false
	w.hwnd = 0
	w.windowStartX = 0
	w.windowStartY = 0
	app.floatingAssistant.posX = 0
	app.floatingAssistant.posY = 0
	app.persistLivePetOriginForExit()
	cfg = readPersistedConfigForTest(t, tmpHome)
	if cfg.FloatingBtnX != -500 || cfg.FloatingBtnY != 80 {
		t.Fatalf("gone window saved (%d,%d)", cfg.FloatingBtnX, cfg.FloatingBtnY)
	}
}

func TestPetMonitorWorkAreaIsReadable(t *testing.T) {
	work, ok := petMonitorWorkArea(0, 0, 100)
	if !ok {
		t.Fatal("no monitor work area")
	}
	if work.Right <= work.Left || work.Bottom <= work.Top {
		t.Fatalf("work = %+v", work)
	}
}

// A pinned pet keeps the border she was walking and does not ask where the
// main window is. The lookup is what enumerated every top-level window.
func TestPinnedPetDoesNotQueryTheMainWindow(t *testing.T) {
	prev := petMainWindowEdge
	called := 0
	petMainWindowEdge = func(uintptr) (petpack.WindowRect, petpack.WindowRect, bool) {
		called++
		return petpack.WindowRect{Left: 10, Top: 10, Right: 400, Bottom: 300},
			petpack.WindowRect{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
			true
	}
	t.Cleanup(func() { petMainWindowEdge = prev })

	w := &windowsFloatingWindow{
		hwnd:              0,
		windowStartX:      40,
		windowStartY:      50,
		petPositionLocked: true,
		petRoam:           petpack.DesktopRoam{EdgeOn: true, Along: 80, EdgeDir: -1, RemX: 1.5, RemY: -0.25},
	}
	w.stepPetLocomotion(1, 0, 200)
	if called != 0 {
		t.Fatalf("pinned step queried the main window %d times", called)
	}
	if !w.petRoam.EdgeOn || w.petRoam.Along != 80 || w.petRoam.EdgeDir != -1 {
		t.Fatalf("patrol memory = %+v", w.petRoam)
	}
	if w.petRoam.RemX != 0 || w.petRoam.RemY != 0 {
		t.Fatalf("remainder = %v,%v", w.petRoam.RemX, w.petRoam.RemY)
	}
	if w.windowStartX != 40 || w.windowStartY != 50 {
		t.Fatalf("origin = %d,%d", w.windowStartX, w.windowStartY)
	}
}
