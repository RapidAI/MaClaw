//go:build windows

package guiapp

import "testing"

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
