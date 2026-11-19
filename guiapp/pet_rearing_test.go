package guiapp

import (
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestPetRearingWindowSizeClampsAndKeepsTheSavedSize(t *testing.T) {
	if got := rearingWindowSize(224, 1); got != 240 {
		t.Fatalf("scale 1 window = %d, want 240", got)
	}
	if got := rearingWindowSize(200, 1.15); got != 230+16 {
		t.Fatalf("grown window = %d, want 246", got)
	}
	if got := rearingWindowSize(240, 1.5); got != 256 {
		t.Fatalf("cap window = %d, want 256", got)
	}
	if got := rearingWindowSize(80, 0.5); got != 56+16 {
		t.Fatalf("small window = %d, want 72", got)
	}
}

func TestPetRearingStoreRemembersLoadedTime(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closePetRearingDBLocked)
	app.petRearing.pack = "ragdoll"
	app.petRearing.loaded = 7200000
	app.petRearing.announced = "coat"
	app.savePetRearingLocked()
	if app.petRearing.dbPath != filepath.Join(app.GetDataDir(), petRearingDBFile) {
		t.Fatalf("db path = %s", app.petRearing.dbPath)
	}
	loaded, announced := app.loadPetRearingLocked("ragdoll")
	if loaded != 7200000 || announced != "coat" {
		t.Fatalf("loaded=%d announced=%q", loaded, announced)
	}
}

func TestPetRearingPlanIsSilentUntilTheNextStage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closePetRearingDBLocked)
	app.petRearing.pack = "ragdoll"
	app.petRearing.loaded = 0
	app.petRearing.userSize = 224
	first := app.planPetRearingLocked()
	if first.cue != "" || first.windowSize != 240 || len(first.adornments) != 0 {
		t.Fatalf("first plan = %+v", first)
	}
	app.petRearing.loaded += int64(time.Hour / time.Millisecond)
	second := app.planPetRearingLocked()
	if second.cue != "" || second.refreshBuddy {
		t.Fatalf("one hour is below the first stage: %+v", second)
	}
}

func TestPetRearingStageChangeRefreshesBuddyOnce(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closePetRearingDBLocked)
	app.petRearing.pack = "ragdoll"
	app.petRearing.userSize = 224
	app.petRearing.loaded = 7_200_000
	first := app.planPetRearingLocked()
	if first.refreshBuddy || first.cue != "" || first.stageID != "familiar" {
		t.Fatalf("first sight = %+v", first)
	}
	if first.windowSize != 249 {
		t.Fatalf("familiar window = %d, want 249", first.windowSize)
	}
	if len(first.adornments) != 1 || first.adornments[0] != "mouse_charm" {
		t.Fatalf("familiar adornments = %v", first.adornments)
	}
	app.petRearing.loaded += int64(time.Hour / time.Millisecond)
	still := app.planPetRearingLocked()
	if still.refreshBuddy || still.cue != "" || still.stageID != "familiar" {
		t.Fatalf("still familiar = %+v", still)
	}
	app.petRearing.loaded = 86_400_000
	next := app.planPetRearingLocked()
	if !next.refreshBuddy || next.cue != "celebrate" || next.stageID != "grown" {
		t.Fatalf("grown crossing = %+v", next)
	}
	if len(next.adornments) != 3 || next.adornments[0] != "mouse_charm" || next.adornments[1] != "slipper_l" || next.adornments[2] != "slipper_r" {
		t.Fatalf("grown adornments = %v", next.adornments)
	}
	repeat := app.planPetRearingLocked()
	if repeat.refreshBuddy || repeat.cue != "" || repeat.stageID != "grown" {
		t.Fatalf("repeat = %+v", repeat)
	}
	app.petRearing.loaded = 259_200_000
	settled := app.planPetRearingLocked()
	if !settled.refreshBuddy || settled.cue != "celebrate" || settled.stageID != "settled" || settled.windowSize != 256 {
		t.Fatalf("settled crossing = %+v", settled)
	}
	if len(settled.adornments) != 4 || settled.adornments[3] != "mouse_pet" {
		t.Fatalf("settled adornments = %v", settled.adornments)
	}
	if !sameAdornmentSlots(settled.adornments[:3], next.adornments) {
		t.Fatalf("settled dropped the grown props: %v", settled.adornments)
	}
}

func TestPetRearingLookReadsSavedTimeWhenIdle(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closePetRearingDBLocked)
	app.petRearing.pack = "ragdoll"
	app.petRearing.loaded = 86_400_000
	app.petRearing.announced = "grown"
	app.savePetRearingLocked()
	app.closePetRearingDBLocked()
	app.petRearing.pack = ""
	app.petRearing.loaded = 0
	app.petRearing.running = false
	variant, scale := app.petRearingLook("ragdoll")
	if variant != "" || math.Abs(scale-1.1) > 0.0001 {
		t.Fatalf("saved look variant=%q scale=%v", variant, scale)
	}
	if app.petRearing.db != nil {
		t.Fatal("look left the rearing database open")
	}
}

func TestRearedWindowUsesTheStageScale(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closePetRearingDBLocked)
	app.petRearing.pack = "ragdoll"
	app.petRearing.running = true
	app.petRearing.loaded = 0
	if got := rearedWindowSize(app, "ragdoll", 224); got != 240 {
		t.Fatalf("base window = %d, want 240", got)
	}
	app.petRearing.loaded = 86_400_000
	if got := rearedWindowSize(app, "ragdoll", 224); got != 256 {
		t.Fatalf("grown window = %d, want 256", got)
	}
}

func TestPetRearingExitFlushesTheOpenInterval(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closePetRearingDBLocked)
	start := time.Unix(1_700_000_000, 0)
	app.syncPetRearingAt("ragdoll", true, 224, start)
	app.flushPetRearingForExitAt(start.Add(20 * time.Second))
	if app.petRearing.loaded != 20_000 {
		t.Fatalf("exit flush loaded = %d, want 20000", app.petRearing.loaded)
	}
	if app.petRearing.db != nil {
		t.Fatal("exit flush left the database open")
	}
	loaded, _ := app.loadPetRearingLocked("ragdoll")
	if loaded != 20_000 {
		t.Fatalf("saved loaded = %d, want 20000", loaded)
	}
	app.flushPetRearingForExitAt(start.Add(20 * time.Second))
	if app.petRearing.loaded != 20_000 {
		t.Fatalf("second exit flush loaded = %d, want 20000", app.petRearing.loaded)
	}
}

func TestPetRearingHiddenTimeDoesNotCount(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closePetRearingDBLocked)
	start := time.Unix(1_700_000_000, 0)
	app.syncPetRearingAt("ragdoll", true, 224, start)
	app.syncPetRearingAt("ragdoll", true, 224, start.Add(90*time.Second))
	if app.petRearing.loaded != 60_000 {
		t.Fatalf("visible minute = %d, want 60000", app.petRearing.loaded)
	}
	app.syncPetRearingAt("ragdoll", false, 224, start.Add(2*time.Minute))
	if app.petRearing.running || !app.petRearing.last.IsZero() {
		t.Fatal("hiding the pet left the clock open")
	}
	parked := app.petRearing.loaded
	app.syncPetRearingAt("ragdoll", true, 224, start.Add(30*time.Minute))
	app.syncPetRearingAt("ragdoll", true, 224, start.Add(30*time.Minute+5*time.Second))
	if app.petRearing.loaded != parked+5_000 {
		t.Fatalf("loaded = %d, want %d", app.petRearing.loaded, parked+5_000)
	}
	ragdollMS := app.petRearing.loaded
	app.syncPetRearingAt("other", true, 224, start.Add(30*time.Minute+5*time.Second))
	app.syncPetRearingAt("other", true, 224, start.Add(30*time.Minute+20*time.Second))
	if app.petRearing.loaded != 15_000 || app.petRearing.pack != "other" {
		t.Fatalf("other pack loaded = %d pack = %s", app.petRearing.loaded, app.petRearing.pack)
	}
	app.syncPetRearingAt("other", false, 224, start.Add(31*time.Minute))
	app.syncPetRearingAt("ragdoll", true, 224, start.Add(40*time.Minute))
	app.syncPetRearingAt("ragdoll", true, 224, start.Add(40*time.Minute+time.Second))
	if app.petRearing.loaded != ragdollMS+1_000 {
		t.Fatalf("ragdoll after another skin = %d, want %d", app.petRearing.loaded, ragdollMS+1_000)
	}
}
