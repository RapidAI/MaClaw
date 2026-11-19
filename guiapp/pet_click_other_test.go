//go:build !windows

package guiapp

import "testing"

// The Windows floating window is not part of this build. Motion-off clip
// choice and the main-window predicate still run through the shared helpers.
func testPetClickWindowMotion(t *testing.T) {
	if petClickReactionClip("ragdoll", petClickSingle, false) != "" ||
		petClickReactionClip("ragdoll", petClickMulti, false) != "" {
		t.Fatal("motion off still named a clip")
	}
}

func testPetClickSideEffect(t *testing.T) {
	if !petClickWantsMainWindow(petClickSingle) ||
		petClickWantsMainWindow(petClickDouble) ||
		petClickWantsMainWindow(petClickMulti) {
		t.Fatal("side-effect predicate changed")
	}
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopPetClickTimer()
		app.closePetRearingDBLocked()
	})
	app.petRearing.pack = "ragdoll"
	app.petRearing.running = true
	app.petRearing.userSize = 224

	single := app.finishDesktopPetClick(petClickSingle, app.petClickEpoch.Load())
	if !single.OpenMain || single.Clip == "" || single.CreditMS != petClickCreditMS(petClickSingle) {
		t.Fatalf("single = %+v", single)
	}
	double := app.finishDesktopPetClick(petClickDouble, app.petClickEpoch.Load())
	multi := app.finishDesktopPetClick(petClickMulti, app.petClickEpoch.Load())
	if double.OpenMain || multi.OpenMain {
		t.Fatalf("repeat click opened the window: double %+v multi %+v", double, multi)
	}
	if double.Clip == "" || multi.Clip == "" || double.Clip == multi.Clip || double.Clip == single.Clip {
		t.Fatalf("motion clips double=%q multi=%q", double.Clip, multi.Clip)
	}
	if double.CreditMS != petClickCreditMS(petClickDouble) || multi.CreditMS != petClickCreditMS(petClickMulti) {
		t.Fatalf("credits double=%d multi=%d", double.CreditMS, multi.CreditMS)
	}
}
