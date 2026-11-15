//go:build windows

package guiapp

import (
	"testing"
	"time"
)

// Quiet and reduced-motion windows live on the Windows floating type. The
// shared burst and credit tests stay untagged so Linux CI still compiles them.
func testPetClickWindowMotion(t *testing.T) {
	quiet := &windowsFloatingWindow{petSkin: "ragdoll", petMotionEnabled: true, petQuietMode: true}
	if _, motion := quiet.PetClickLook(); motion || petClickReactionClip("ragdoll", petClickSingle, motion) != "" {
		t.Fatal("quiet mode would move")
	}
	reduced := &windowsFloatingWindow{petSkin: "ragdoll", petMotionEnabled: true, petReducedMotion: true}
	if _, motion := reduced.PetClickLook(); motion || petClickReactionClip("ragdoll", petClickMulti, motion) != "" {
		t.Fatal("reduced motion would move")
	}

	// A release arms the hover clock the message loop consults. Hover is a
	// soft interrupt, so a later pixel of jitter must not replace the click clip.
	w := &windowsFloatingWindow{}
	released := time.Unix(1_700_000_000, 0)
	w.armPetPointerHover(released)
	if w.petHoverDue(released.Add(time.Second)) {
		t.Fatal("pointer release still started a hover")
	}
	if !w.petHoverDue(released.Add(petHoverCooldown)) {
		t.Fatal("hover cooldown did not elapse")
	}

	// The click clip starts after the double-click wait, so an expired
	// button-up clock must not leave that clip open to the next hover.
	playing := &windowsFloatingWindow{petSkin: "ragdoll", petMotionEnabled: true}
	playing.armPetPointerHover(time.Now().Add(-petHoverCooldown - time.Second))
	playing.triggerCharacterClip("react_click_greet")
	if playing.petHoverDue(time.Now().Add(time.Second)) {
		t.Fatal("click clip left hover free to replace it")
	}

	dragged := &windowsFloatingWindow{petSkin: "ragdoll", petMotionEnabled: true}
	dragged.armPetPointerHover(time.Now().Add(-petHoverCooldown - time.Second))
	dragged.finishPetDrag()
	if dragged.petHoverDue(time.Now().Add(time.Second)) {
		t.Fatal("drag end left hover free to replace it")
	}
}

func testPetClickSideEffect(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopPetClickTimer()
		app.closePetRearingDBLocked()
	})
	app.petRearing.pack = "ragdoll"
	app.petRearing.running = true
	app.petRearing.userSize = 224
	app.floatingAssistant = &FloatingAssistantManager{app: app, window: &windowsFloatingWindow{
		app: app, petSkin: "ragdoll", petMotionEnabled: true, petQuietMode: true,
	}}

	single := app.finishDesktopPetClick(petClickSingle, app.petClickEpoch.Load())
	if !single.OpenMain || !petClickWantsMainWindow(petClickSingle) {
		t.Fatal("single click skipped the main-window side effect")
	}
	if single.Clip != "" || single.CreditMS != petClickCreditMS(petClickSingle) {
		t.Fatalf("quiet single = %+v", single)
	}

	app.floatingAssistant.window = &windowsFloatingWindow{app: app, petSkin: "ragdoll", petMotionEnabled: true}
	double := app.finishDesktopPetClick(petClickDouble, app.petClickEpoch.Load())
	multi := app.finishDesktopPetClick(petClickMulti, app.petClickEpoch.Load())
	if double.OpenMain || multi.OpenMain || petClickWantsMainWindow(petClickDouble) || petClickWantsMainWindow(petClickMulti) {
		t.Fatalf("repeat click opened the window: double %+v multi %+v", double, multi)
	}
	if double.Clip == "" || multi.Clip == "" || double.Clip == multi.Clip || double.Clip == single.Clip {
		t.Fatalf("motion clips double=%q multi=%q", double.Clip, multi.Clip)
	}
	if double.CreditMS != petClickCreditMS(petClickDouble) || multi.CreditMS != petClickCreditMS(petClickMulti) {
		t.Fatalf("credits double=%d multi=%d", double.CreditMS, multi.CreditMS)
	}
}
