//go:build windows

package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

func TestPetMissOccupiedPoseDoesNotComplain(t *testing.T) {
	left := time.Now().Add(-3 * time.Hour)
	working := &windowsFloatingWindow{
		petRuntimeState:      "thinking",
		dutyLine:             "正在做",
		presenceLine:         "正在做",
		petLastInteractionAt: left,
		petMissTier:          2,
		missLine:             "你是不是把我忘了",
		petMissExpression:    "expr_sulk",
		petMissGaze:          "gaze_center",
		petMissMood:          "tired",
	}
	working.advancePetMiss(3*time.Hour, true)
	if working.petMissTier != 0 || working.missLine != "" || working.presenceLine != "正在做" || working.petMissExpression != "" || working.petMissMood != "" {
		t.Fatalf("work kept the complaint tier=%d line=%q presence=%q face=%q", working.petMissTier, working.missLine, working.presenceLine, working.petMissExpression)
	}
	if time.Since(working.petLastInteractionAt) > time.Second {
		t.Fatal("work must keep the wait from running")
	}
	ended := time.Now().Add(-3 * time.Hour)
	settled := &windowsFloatingWindow{
		petRuntimeState:      "done",
		dutyLine:             "做好了",
		presenceLine:         "做好了",
		petStateDeadline:     ended,
		petLastInteractionAt: ended.Add(-time.Second),
		petSkin:              "ragdoll",
	}
	if !settled.expirePetPoseLocked(time.Now()) || !settled.petLastInteractionAt.Equal(ended) || settled.petRuntimeState != "idle" || settled.dutyLine != "" {
		t.Fatalf("deadline end state=%s duty=%q at=%s", settled.petRuntimeState, settled.dutyLine, settled.petLastInteractionAt)
	}
	settled.advancePetMiss(0, false)
	if settled.petMissTier != 3 || settled.missLine != "我在这儿等了好久" || settled.petMissExpression != "expr_pout" || settled.petMissMood != "tired" || !settled.petLastInteractionAt.Equal(ended) {
		t.Fatalf("time after the deadline still counts tier=%d line=%q face=%q at=%s", settled.petMissTier, settled.missLine, settled.petMissExpression, settled.petLastInteractionAt)
	}
	clicked := time.Now()
	after := &windowsFloatingWindow{
		petRuntimeState:      "alert",
		dutyLine:             "这件没做成",
		presenceLine:         "这件没做成",
		petStateDeadline:     ended,
		petLastInteractionAt: clicked,
		petSkin:              "ragdoll",
	}
	after.expirePetPoseLocked(time.Now())
	after.advancePetMiss(0, true)
	if !after.petLastInteractionAt.Equal(clicked) || after.petMissTier != 0 || after.missLine != "" {
		t.Fatalf("a click after the deadline stays the start tier=%d line=%q", after.petMissTier, after.missLine)
	}

	queued := &windowsFloatingWindow{
		petRuntimeState:      "idle",
		dutyLine:             "还没开始",
		presenceLine:         "还没开始",
		petLastInteractionAt: left,
	}
	queued.advancePetMiss(3*time.Hour, true)
	if queued.petMissTier != 0 || queued.missLine != "" || queued.presenceLine != "还没开始" {
		t.Fatalf("a queued sentence still held the wait tier=%d line=%q presence=%q", queued.petMissTier, queued.missLine, queued.presenceLine)
	}

	alone := &windowsFloatingWindow{
		petRuntimeState:      "idle",
		petQuietMode:         true,
		petLastInteractionAt: left,
		petSkin:              "ragdoll",
	}
	alone.advancePetMiss(3*time.Hour, false)
	if alone.petMissTier != 3 || alone.missLine != "我在这儿等了好久" || alone.presenceLine != "我在这儿等了好久" || alone.petMissExpression != "expr_pout" {
		t.Fatalf("quiet idle still says she was left tier=%d line=%q presence=%q face=%q", alone.petMissTier, alone.missLine, alone.presenceLine, alone.petMissExpression)
	}
}

func TestPetMissOverlayExpiryRestoresDuty(t *testing.T) {
	covered := &windowsFloatingWindow{
		petRuntimeState:      "thinking",
		dutyLine:             "正在做",
		presenceLine:         "正在做",
		petLastInteractionAt: time.Now().Add(-3 * time.Hour),
		petMissTier:          1,
		missLine:             "好久没理我了",
	}
	covered.SetPetRuntimeState("alert", 1500)
	if !covered.petOverlay || covered.petBaseState != "thinking" || !covered.petBaseDeadline.IsZero() || covered.dutyLine != "正在做" || covered.petMissTier != 0 {
		t.Fatalf("overlay base=%s duty=%q tier=%d overlay=%v", covered.petBaseState, covered.dutyLine, covered.petMissTier, covered.petOverlay)
	}
	covered.petStateDeadline = time.Now().Add(-time.Hour)
	if covered.expirePetPoseLocked(time.Now()) || covered.petRuntimeState != "thinking" || covered.dutyLine != "正在做" || covered.petOverlay {
		t.Fatalf("overlay end state=%s duty=%q overlay=%v", covered.petRuntimeState, covered.dutyLine, covered.petOverlay)
	}
	covered.advancePetMiss(3*time.Hour, true)
	if covered.petMissTier != 0 || covered.missLine != "" || covered.presenceLine != "正在做" || time.Since(covered.petLastInteractionAt) > time.Second {
		t.Fatalf("work under an overlay is not time she was left tier=%d line=%q", covered.petMissTier, covered.missLine)
	}

	ended := time.Now().Add(-3 * time.Hour)
	voice := &windowsFloatingWindow{
		petRuntimeState:      "thinking",
		petOverlay:           true,
		petBaseState:         "idle",
		petStateDeadline:     ended,
		petLastInteractionAt: ended.Add(-time.Second),
		petSkin:              "ragdoll",
	}
	if voice.expirePetPoseLocked(time.Now()) || voice.petRuntimeState != "idle" || voice.petOverlay || !voice.petLastInteractionAt.Equal(ended) {
		t.Fatalf("voice timeout state=%s at=%s overlay=%v", voice.petRuntimeState, voice.petLastInteractionAt, voice.petOverlay)
	}
	voice.advancePetMiss(0, false)
	if voice.petMissTier != 3 || voice.missLine != "我在这儿等了好久" || !voice.petLastInteractionAt.Equal(ended) {
		t.Fatalf("time after a voice timeout still counts tier=%d line=%q", voice.petMissTier, voice.missLine)
	}

	duty := &windowsFloatingWindow{
		petOverlay:       true,
		petBaseState:     "thinking",
		petRuntimeState:  "alert",
		dutyLine:         "正在做",
		presenceLine:     "正在做",
		petStateDeadline: time.Now().Add(time.Second),
	}
	duty.ApplyPetDuty("done", 1600, "", "")
	if duty.petOverlay || duty.petBaseState != "" || duty.petRuntimeState != "done" || duty.petStateDeadline.IsZero() {
		t.Fatalf("duty replaces an overlay state=%s base=%q overlay=%v", duty.petRuntimeState, duty.petBaseState, duty.petOverlay)
	}
}

func TestPetMissTranscriptionCoverReturnsToDuty(t *testing.T) {
	ended := time.Now().Add(-3 * time.Hour)
	working := &windowsFloatingWindow{
		petRuntimeState: "listening",
		dutyLine:        "做好了",
		presenceLine:    "做好了",
	}
	working.SetPetTimedCover("thinking", 15000, "thinking", 0, "正在做")
	if !working.petOverlay || working.petBaseState != "thinking" || working.petRuntimeState != "thinking" || working.dutyLine != "正在做" {
		t.Fatalf("cover base=%s state=%s duty=%q overlay=%v", working.petBaseState, working.petRuntimeState, working.dutyLine, working.petOverlay)
	}
	working.petStateDeadline = ended
	working.petLastInteractionAt = ended.Add(-time.Second)
	if working.expirePetPoseLocked(time.Now()) || working.petRuntimeState != "thinking" || working.dutyLine != "正在做" || working.petOverlay {
		t.Fatalf("cover end state=%s duty=%q overlay=%v", working.petRuntimeState, working.dutyLine, working.petOverlay)
	}
	working.advancePetMiss(3*time.Hour, true)
	if working.petMissTier != 0 || working.missLine != "" || working.presenceLine != "正在做" || time.Since(working.petLastInteractionAt) > time.Second {
		t.Fatalf("a task under transcription is not time she was left tier=%d line=%q", working.petMissTier, working.missLine)
	}

	missed := &windowsFloatingWindow{
		petRuntimeState: "listening",
		dutyLine:        "做好了",
		presenceLine:    "做好了",
		petSkin:         "ragdoll",
	}
	missed.SetPetTimedCover("thinking", 15000, "", 0, "")
	if missed.petBaseState != "idle" || missed.dutyLine != "" || missed.presenceLine != "" {
		t.Fatalf("no task base=%s duty=%q presence=%q", missed.petBaseState, missed.dutyLine, missed.presenceLine)
	}
	missed.petStateDeadline = ended
	missed.petLastInteractionAt = ended.Add(-time.Second)
	if missed.expirePetPoseLocked(time.Now()) || missed.petRuntimeState != "idle" || !missed.petLastInteractionAt.Equal(ended) {
		t.Fatalf("missed follow-up state=%s at=%s", missed.petRuntimeState, missed.petLastInteractionAt)
	}
	missed.advancePetMiss(0, false)
	if missed.petMissTier != 3 || missed.missLine != "我在这儿等了好久" || !missed.petLastInteractionAt.Equal(ended) {
		t.Fatalf("time after a missed transcription still counts tier=%d line=%q", missed.petMissTier, missed.missLine)
	}
}

// TestPetMissDutyPublishesClearedFace checks the renderer copy, not only the
// window fields. SetCompanionFeel reports false when that exact face is
// already held, so a true result means the duty path published the clear.
func TestPetMissDutyPublishesClearedFace(t *testing.T) {
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(petpack.DefaultPackID, petpack.VariantDefault)
	if err != nil || resolved == nil {
		t.Fatalf("resolve ragdoll: %v", err)
	}
	renderer, err := petpack.NewCharacterRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatalf("ragdoll renderer: %v", err)
	}
	key := petpack.DefaultPackID + "\x00" + petpack.VariantDefault
	hold := func(w *windowsFloatingWindow) {
		w.petMissTier = 3
		w.missLine = "我在这儿等了好久"
		w.petMissExpression = "expr_pout"
		w.petMissGaze = "gaze_center"
		w.petMissMood = "tired"
		w.presenceLine = w.missLine
		renderer.SetCompanionFeel("", "", "")
		if !renderer.SetCompanionFeel("expr_pout", "gaze_center", "tired") {
			t.Fatal("pout face did not apply")
		}
	}
	cleared := func(name string, w *windowsFloatingWindow) {
		t.Helper()
		if w.petMissExpression != "" || w.petMissGaze != "" || w.petMissMood != "" || w.petMissTier != 0 || w.missLine != "" {
			t.Fatalf("%s kept the window complaint face=%q gaze=%q mood=%q tier=%d line=%q", name, w.petMissExpression, w.petMissGaze, w.petMissMood, w.petMissTier, w.missLine)
		}
		if !renderer.SetCompanionFeel("expr_pout", "gaze_center", "tired") {
			t.Fatalf("%s left the pout on the renderer", name)
		}
	}
	newWindow := func() *windowsFloatingWindow {
		return &windowsFloatingWindow{
			petSkin:            petpack.DefaultPackID,
			petRuntimeState:    "idle",
			characterRenderers: map[string]*petpack.CharacterRenderer{key: renderer},
		}
	}

	runtime := newWindow()
	hold(runtime)
	runtime.SetPetRuntimeState("thinking", 0)
	if runtime.petRuntimeState != "thinking" {
		t.Fatalf("runtime state=%s", runtime.petRuntimeState)
	}
	cleared("runtime", runtime)

	cover := newWindow()
	hold(cover)
	cover.SetPetTimedCover("alert", 1500, "thinking", 0, "正在做")
	if !cover.petOverlay || cover.petRuntimeState != "alert" || cover.dutyLine != "正在做" {
		t.Fatalf("cover state=%s duty=%q overlay=%v", cover.petRuntimeState, cover.dutyLine, cover.petOverlay)
	}
	cleared("cover", cover)

	duty := newWindow()
	hold(duty)
	duty.ApplyPetDuty("thinking", 0, "task_started", "")
	if duty.petRuntimeState != "thinking" {
		t.Fatalf("duty state=%s", duty.petRuntimeState)
	}
	cleared("duty", duty)
}

// TestPetMissPublishHoldsTheWindowLock checks that the renderer write cannot
// observe a face the window has already replaced. TryLock fails while the
// same goroutine holds the mutex.
func TestPetMissPublishHoldsTheWindowLock(t *testing.T) {
	reg := petpack.NewRegistry(t.TempDir(), petpack.BundledFS())
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	resolved, err := reg.Resolve(petpack.DefaultPackID, petpack.VariantDefault)
	if err != nil || resolved == nil {
		t.Fatalf("resolve ragdoll: %v", err)
	}
	renderer, err := petpack.NewCharacterRenderer(resolved)
	if err != nil || renderer == nil {
		t.Fatalf("ragdoll renderer: %v", err)
	}
	w := &windowsFloatingWindow{
		petSkin:           petpack.DefaultPackID,
		petRuntimeState:   "idle",
		petMissExpression: "expr_pout",
		petMissGaze:       "gaze_center",
		petMissMood:       "tired",
		characterRenderers: map[string]*petpack.CharacterRenderer{
			petpack.DefaultPackID + "\x00" + petpack.VariantDefault: renderer,
		},
	}
	sawWrite := false
	w.testPetMissPublish = func() {
		sawWrite = true
		if w.mu.TryLock() {
			w.mu.Unlock()
			t.Fatal("face publish dropped the window lock before writing the renderer")
		}
	}
	w.pushPetMissFeel()
	if !sawWrite {
		t.Fatal("face publish did not write the renderer")
	}
	if renderer.SetCompanionFeel("expr_pout", "gaze_center", "tired") {
		t.Fatal("publish did not keep the pout that was still on the window")
	}
	w.SetPetRuntimeState("thinking", 0)
	if !renderer.SetCompanionFeel("expr_pout", "gaze_center", "tired") {
		t.Fatal("clearing the window left the pout on the renderer")
	}
}
